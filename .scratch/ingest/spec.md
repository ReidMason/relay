# ingest service

## Problem Statement

Homelab systems (Sonarr, Unraid, and more later) each emit notifications in their
own vendor-specific shape. There's no single place that turns "Sonarr grabbed an
episode" and "Unraid disk is failing" into the same kind of thing, so any
consumer that wants to act on these (send a push notification, log them, alert
on them) has to understand every vendor's payload itself. That knowledge would
end up duplicated and drifting across every consumer that's ever added.

## Solution

Build `ingest`: an HTTP service that receives vendor webhooks, translates each
one into a single canonical **Event** vocabulary (`Source`, `Type`, `Severity`,
`Timestamp`, `Data`), and publishes it onto NATS JetStream. All vendor-specific
translation happens exactly once, inside `ingest`. Everything downstream
(`notifier`, and later consumers such as an event-logger) only ever sees Events
— never raw vendor payloads — so they never need vendor-specific branching.

## User Stories

1. As Sonarr, I want to POST my webhook payload to `ingest`, so that a completed
   download becomes a visible Event on the bus.
2. As Sonarr, I want a `Grab` event to become a `grab` Event with `info`
   severity, so that consumers can distinguish "queued" from "completed"
   downloads.
3. As Sonarr, I want a `HealthIssue` event to become a `health.issue` Event with
   `warning` severity, so that consumers can treat it with more urgency than a
   routine download.
4. As Sonarr, I want a `Test` connectivity-check webhook to be accepted (200)
   but produce no Event, so that Sonarr's "Test" button in its UI doesn't spam
   the event bus with meaningless pings.
5. As Unraid, I want to POST my webhook payload to `ingest`, so that array/disk
   events become visible Events on the bus.
6. As Unraid, I want my `importance` field (`alert`/`warning`/`normal`) mapped
   to the generic `Severity` vocabulary (`critical`/`warning`/`info`), so that
   consumers route on the same dimension regardless of which vendor sent it.
7. As a future consumer (e.g. `notifier`), I want every Event's `Data` to
   already be translated into plain fields, so that I never need to parse or
   understand a vendor's raw JSON shape myself.
8. As a future consumer, I want to never see `Source`-specific codes or enums
   leak into `Data`, so that I can add support for a brand-new vendor source
   without changing my own logic.
9. As a developer adding a third vendor source later (e.g. a future monitoring
   tool), I want to add a new `Parser` and register it in `ingest`'s startup
   wiring, so that I don't need to touch the HTTP transport, the NATS
   transport, or any existing parser.
10. As a developer, I want an unrecognized `{source}` in the webhook URL to
    return a 404, so that misconfigured webhook senders get an obvious signal
    instead of a silent no-op.
11. As a developer, I want a genuinely malformed/unparseable payload for a
    known source to return a 400, so that the sending vendor can see the
    request failed and (depending on the vendor) retry or surface the error.
12. As a developer, I want a NATS publish failure to return a 500, so that the
    sending vendor's webhook-retry behavior (where it has one) naturally
    retries instead of silently losing the event.
13. As an operator, I want `GET /healthz` to report liveness/readiness, so that
    Kubernetes can gate traffic and restarts on it.
14. As an operator, I want `ingest` to idempotently ensure the `HOMELAB_EVENTS`
    JetStream stream (capturing subject `homelab.>`) exists on startup, so
    that I don't have to provision it out-of-band before first deploy.
15. As an operator, I want `ingest` configured entirely by environment
    variables (e.g. `NATS_URL`, `PORT`), so that it fits the existing
    Helm/GitOps deployment pattern without mounted config files.
16. As an operator, I want structured (`log/slog`) logs for every received
    webhook, parse outcome, and publish outcome, so that I can debug a
    misbehaving vendor integration from logs alone.
17. As a developer, I want webhook requests trusted purely at the network
    level (in-cluster, no auth token), so that Sonarr/Unraid's built-in
    webhook senders (which don't support custom auth) can call `ingest`
    without extra configuration.
18. As a developer maintaining `ingest`, I want its internal packages
    structurally unimportable by the future `notifier` binary (and vice
    versa), so that cross-service coupling is a compile error, not a code
    review catch.

## Implementation Decisions

- **Module layout**: existing `github.com/ReidMason/relay` module (go 1.26.4).
  Shared, module-wide `internal/event` package holds the `Event` struct and the
  `homelab.<source>.<type>` NATS subject helper — the one package intentionally
  shared by `ingest` and the future `notifier`.
- **Per-service nested `internal/`**: `ingest` lives under
  `internal/ingest/`, with its own guarded internals at
  `internal/ingest/internal/...` (see ADR 0001). `internal/ingest/cmd/main.go`
  is the binary entrypoint, deliberately placed inside `ingest`'s own tree
  (not a top-level `cmd/ingest`) so it sits within the one tree allowed to
  import `ingest`'s guarded internals. This is what makes cross-service
  isolation a compile-time guarantee rather than a discipline (per ADR 0001).
- **Ports and adapters, one service**: `internal/ingest/internal/core` defines
  the ports:
  - `Parser` — `Parse(raw []byte) (event.Event, error)`
  - `Publisher` — publishes an `event.Event`
  - `Service` — orchestrator: `Handle(source string, raw []byte) error`, looks
    up the `Parser` for `source`, calls it, and on success calls `Publisher`.
  `transport/http` and `transport/nats` are adapters. `core` never imports
  either transport package — enforced for free by Go's import-cycle rule,
  since both transports already import `core` (see ADR 0001).
- **Skip vs. error**: a sentinel `ingest.ErrSkip` (checked via `errors.Is`)
  signals "valid webhook, nothing to publish" (Sonarr's `Test` eventType). The
  HTTP adapter maps `ErrSkip` → 200, any other parse error → 400, publish
  failure → 500.
- **Parser registry**: a literal `map[string]core.Parser` built explicitly in
  `main.go` (keys: `"sonarr"`, `"unraid"`), not a dynamic/config-driven
  registry. Adding a vendor means adding a map entry, not a config change.
- **Routing**: stdlib `net/http` with Go 1.22+ method+path patterns — no
  router library. Routes: `POST /webhooks/{source}`, `GET /healthz`.
- **Sonarr parser** (`eventType` field switch):
  | Sonarr `eventType` | Type | Severity |
  |---|---|---|
  | `Download` | `download.completed` | info |
  | `Grab` | `grab` | info |
  | `HealthIssue` | `health.issue` | warning |
  | `Test` | — | `ErrSkip` |

  `Data` is a flattened, human-readable struct pulled out of Sonarr's nested
  `series`/`episodes` objects (e.g. `SeriesTitle`, `EpisodeTitle`,
  `SeasonNumber`, `EpisodeNumber`) — never Sonarr's raw payload passed through.
- **Unraid parser** (`importance` field mapping):
  | Unraid `importance` | Severity |
  |---|---|
  | `alert` | critical |
  | `warning` | warning |
  | `normal` | info |

  Unraid's own `subject`/`description` text is carried into `Data` largely
  as-is (Unraid already writes it human-readable); only `importance` needs
  translation.
- **NATS/JetStream adapter**: synchronous `js.Publish` (blocks for ack; publish
  failure propagates as an error up through `core.Service` to the HTTP
  adapter's 500) — chosen over async publish for correctness at homelab-scale
  volume, so a webhook sender's retry behavior is the recovery mechanism for
  transient publish failures. On startup, idempotently ensures stream
  `HOMELAB_EVENTS` (subjects `homelab.>`) exists (create-if-absent), rather
  than relying on out-of-band provisioning.
- **Config**: environment variables only (`NATS_URL`, `PORT`, ...), no config
  files — 12-factor style, consistent with the existing GitOps/Helm pattern.
- **Wiring**: manual constructor-based dependency injection in `main.go` — no
  DI framework.
- **Auth**: none. Webhook endpoints are trusted at the network level
  (in-cluster only) — matches ADR-recorded decision and the fact that
  Sonarr/Unraid's built-in webhook senders don't support custom auth headers.
- **Logging**: `log/slog`, structured, for received webhooks and
  parse/publish outcomes.
- **Not part of this spec**: the `notifier` binary, Dockerfile, Helm charts,
  and wiring Sonarr/Unraid's actual webhook settings to a deployed URL — all
  explicitly deferred (see Out of Scope).

## Testing Decisions

- Integration-style over unit-style, per project preference: tests enter
  through the real application entry point (the HTTP transport, i.e.
  `POST /webhooks/{source}`) and exercise as many real implementations as
  possible — real `core.Service`, real `Sonarr`/`Unraid` parsers, real routing
  — rather than mocking `core.Service` or the parsers individually.
- The one seam that gets a test double is the true external-infrastructure
  boundary: `Publisher` (NATS/JetStream). Tests use a fake `Publisher` that
  captures published `event.Event`s in memory, so assertions check "what event
  came out" rather than inspecting NATS wire traffic.
- Each test: POST a fixture vendor payload (real Sonarr/Unraid JSON shapes) to
  `/webhooks/{source}` against a real `httptest.Server` wrapping the real
  transport/http adapter + real core.Service + real parser + fake Publisher,
  then assert on: the HTTP status code returned, and (when a publish should
  happen) the captured `event.Event`'s `Source`/`Type`/`Severity`/`Data`.
- Cases to cover per source: each `eventType`/`importance` row in the mapping
  tables above, the `Test`-eventType skip path (200, nothing captured), a
  malformed-JSON payload (400, nothing captured), and an unknown `{source}`
  path segment (404).
- A `Publisher` failure case (fake `Publisher` returns an error) asserts a 500
  is returned.
- No prior test code exists in this repo yet (first tests being written
  alongside `ingest` itself) — no existing prior-art pattern to follow within
  `relay`.

## Out of Scope

- The `notifier` binary and any severity-routed delivery channel (ntfy,
  Discord, Pushover, etc.) — separate future spec.
- Dockerfile and Helm charts for deploying `ingest` — separate future work,
  following the existing chart pattern (`kubernetes-v2/charts/nats/` etc.) in
  the `homelab` repo.
- Actually configuring Sonarr's "Connect > Webhook" setting or Unraid's
  webhook notification agent to point at a deployed `ingest` URL.
- A web UI, dashboard, or any way to browse published Events (that's the
  future `event-logger` consumer's job).
- Authentication/authorization on the webhook endpoints.
- Any vendor source beyond Sonarr and Unraid.

## Further Notes

- Full architecture rationale (broker choice, design principles, repo-vs-
  multi-repo reasoning) lives in
  `/Users/reid/Documents/repos/homelab/scratch/event-bus-architecture.md`.
- The nested-`internal/` compile-time isolation mechanism and the
  ports-and-adapters split are recorded in
  `docs/adr/0001-ports-and-adapters-with-nested-internal-isolation.md` in this
  repo — read it before implementing, it explains the *why* behind the
  directory layout this spec assumes.
- Domain vocabulary (Event, Source, Type, Severity, Webhook payload) is
  defined in this repo's `CONTEXT.md` — use those terms, not synonyms, in any
  code/comments/PR description that comes out of this spec.
- This repo hasn't run `/setup-matt-pocock-skills` yet and `gh` isn't
  authenticated in this environment, so this spec is published as local
  markdown (`.scratch/ingest/spec.md`) rather than a GitHub issue with a
  `ready-for-agent` label. If GitHub Issues becomes available later, this
  file's content should be filed as an issue and labeled `ready-for-agent`.
