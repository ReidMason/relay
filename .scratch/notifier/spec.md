# notifier service

## Problem Statement

Events published to NATS by `ingest` (`homelab.<source>.<type>`) currently have
no consumer. Nothing turns "Unraid disk is failing" or "Sonarr grabbed an
episode" into an actual notification a human sees. We need a service that
subscribes to the event bus and delivers Events somewhere, while staying
extensible to more than one delivery mechanism later without rewriting its
core routing logic.

## Solution

Build `notifier`: a service that runs a durable JetStream pull consumer
against `homelab.>`, routes each Event to zero or more **Channels** based on
its `Source`/`Severity`, and delivers it. Only one Channel exists today
(`discord`, using a Discord webhook), but the routing/delivery split is
designed so adding a second Channel is a registry + routing-table edit, not
new branching logic.

## User Stories

1. As an operator, I want `notifier` to durably consume every Event published
   to `homelab.>`, so that a restart doesn't lose events queued since the last
   ack.
2. As an operator, I want a new `notifier` deployment to only see events
   published after it first starts (not replay the entire event history), so
   that first deploy doesn't cause a flood of stale notifications.
3. As a user, I want Unraid events at `warning` severity or above delivered to
   Discord, so I hear about real array/disk problems.
4. As a user, I want Sonarr events only at `critical` severity delivered to
   Discord, so routine download/grab noise doesn't spam the channel — Sonarr
   has minor issues constantly and I only care when something's seriously
   wrong.
5. As a user, I want a Discord notification to be human-readable (a title,
   color-coded by severity, readable field labels) rather than a raw JSON
   dump, so I can understand what happened at a glance.
6. As a developer adding a third vendor `Source` later, I want to add one
   entry to the routing table (source + minimum severity + channels) rather
   than write new consumer logic, so the routing stays declarative.
7. As a developer adding a second Channel later (e.g. Pushover), I want to
   implement the `Channel` interface and register it in `main.go`, without
   touching `core`'s routing logic or the existing `discord` Channel.
8. As a developer, I want `notifier`'s internal packages structurally
   unimportable by `ingest` (and vice versa), so cross-service coupling stays
   a compile error, matching ADR-0001.
9. As an operator, I want a failed Discord delivery to leave the JetStream
   message unacked (so it's automatically redelivered) up to a bounded number
   of attempts, so a transient webhook failure doesn't silently drop a
   notification, but a permanently broken webhook doesn't retry forever.
10. As an operator, I want `GET /healthz` to report liveness/readiness,
    matching `ingest`'s convention, so Kubernetes can gate traffic/restarts.
11. As an operator, I want `notifier` configured entirely by environment
    variables (`NATS_URL`, `DISCORD_WEBHOOK_URL`, `PORT`), so it fits the
    existing Helm/GitOps deployment pattern.
12. As an operator, I want to be able to run 2+ replicas of `notifier` without
    duplicate deliveries, so the service can be scaled/rolled without a code
    change.

## Implementation Decisions

- **Module/layout**: existing `github.com/ReidMason/relay` module. `notifier`
  lives at `internal/notifier/`, with its own guarded internals at
  `internal/notifier/internal/...` and entrypoint at
  `internal/notifier/cmd/main.go`, mirroring `ingest`'s structure exactly
  (ADR-0001). Uses the shared `internal/event` package.
- **Ports and adapters**: `internal/notifier/internal/core` defines:
  - `Channel` — `Send(event.Event) error`. Implemented by `transport/discord`.
  - `Route` — `{Source event.Source; MinSeverity event.Severity; Channels []ChannelName}`.
  - `ChannelName` — a `string`-based enum/const type (e.g. `ChannelDiscord`),
    not raw strings, for channel identifiers.
  - `Service` — orchestrator: `Handle(e event.Event) error`. For each `Route`
    matching the Event's `Source` where `e.Severity >= Route.MinSeverity`,
    sends to every named `Channel`. Severity ordering: `info < warning <
    critical`.
  `core` never imports `transport/nats` or `transport/discord` — both import
  `core` to implement/call its ports, so the import-cycle rule enforces
  core→adapter direction for free (same mechanism as `ingest`, ADR-0001).
- **Routing table**: a literal `[]core.Route` built explicitly in `main.go`
  (not a dynamic/config-driven registry), e.g.:
  ```go
  routes := []core.Route{
      {Source: unraid.Source, MinSeverity: event.SeverityWarning, Channels: []core.ChannelName{core.ChannelDiscord}},
      {Source: sonarr.Source, MinSeverity: event.SeverityCritical, Channels: []core.ChannelName{core.ChannelDiscord}},
  }
  ```
  Adding a source or changing its floor means editing this table, not writing
  new code.
- **Channel registry**: a literal `map[core.ChannelName]core.Channel` built in
  `main.go` (keys: `core.ChannelDiscord` → the Discord adapter), same
  convention as `ingest`'s parser registry.
- **NATS consumer** (`transport/nats`): durable JetStream pull consumer,
  fixed name `"notifier"` (shared across replicas — multiple processes
  pulling the same durable consumer get messages distributed with no
  duplicate delivery, JetStream's queue-group-equivalent behavior for pull
  consumers), subject `homelab.>`, `DeliverPolicy: New` (skip history on
  first deploy), `MaxDeliver: 5`. Ack only after every matched Channel's
  `Send` succeeds; on any `Send` failure, don't ack (triggers JetStream
  redelivery). On `MaxDeliver` exhaustion, log via `slog` at `warn`/`error`
  and let JetStream drop it — no dead-letter queue.
- **Discord channel** (`transport/discord`): POSTs a Discord webhook payload
  containing one embed:
  - `color`: red (critical) / yellow (warning) / grey (info)
  - `title`: `e.Type`
  - `footer`: `e.Source` + `e.Timestamp`
  - `fields`: one per key in `e.Data` (decoded generically as
    `map[string]any` — `notifier` never has the original Go struct type from
    `ingest`), key formatted from PascalCase to spaced Title Case
    (`SeriesTitle` → `Series Title`), value stringified (nested
    maps/arrays fall back to a compact JSON string within that field), fields
    sorted alphabetically by key for deterministic output.
  Webhook URL from `DISCORD_WEBHOOK_URL` env var. A non-2xx response or
  request error is returned as an error (propagates to the NATS adapter's
  ack-withholding logic above).
- **HTTP surface**: minimal `net/http` server, `GET /healthz` only — same
  convention as `ingest`, needed purely for k8s probes since `notifier`'s
  primary transport is NATS pull, not HTTP.
- **Config**: env vars only — `NATS_URL`, `DISCORD_WEBHOOK_URL`, `PORT`. No
  config files.
- **Wiring**: manual constructor-based dependency injection in `main.go`, no
  DI framework — matches `ingest`.
- **Logging**: `log/slog`, structured, for consumed Events, routing outcome,
  and delivery outcome (success/failure/redelivery-exhausted).
- **CONTEXT.md**: already updated in the grilling session — `Source`'s
  definition now distinguishes declarative routing (fine) from semantic
  branching (still forbidden); `Channel` added as a new glossary term.
- **Not part of this spec**: a health-check-driven external alerting signal
  for sustained delivery failure (raised as a future improvement, not
  required now), Dockerfile, Helm charts, wiring a second Channel.

## Testing Decisions

- Integration-style over unit-style, per project preference and matching
  `ingest`'s existing pattern: tests enter through the real NATS consumer
  (`transport/nats`) using a **real embedded NATS/JetStream server**
  (`github.com/nats-io/nats-server/v2/server` test helpers), exercising the
  real `core.Service` and real routing table.
- The one seam that gets a test double is the true external-infrastructure
  boundary: the Discord webhook. Tests use an `httptest.Server` standing in
  for Discord, capturing POSTed embed JSON in memory, so assertions check
  "what was sent to Discord" rather than inspecting real Discord traffic.
- Each test: publish a fixture `event.Event` (JSON-encoded, matching what
  `ingest` would actually publish) directly to the embedded NATS server on
  `homelab.<source>.<type>`, let the real consumer pick it up, and assert on:
  whether the fake Discord server received a request, and if so, the embed's
  color/title/fields.
- Cases to cover: Unraid at `info` (no delivery — below floor), Unraid at
  `warning` and `critical` (delivered), Sonarr at `info`/`warning` (no
  delivery), Sonarr at `critical` (delivered), a Discord webhook failure
  (fake server returns 500) leaves the message unacked/redelivered, and
  human-readable field formatting on a multi-key `Data` payload.
- No prior notifier test code exists in this repo — first tests written
  alongside implementation.

## Out of Scope

- Additional Channels (Pushover, ntfy, etc.) — separate future work, this
  spec builds the extensibility, not the second implementation.
- Dockerfile and Helm charts for deploying `notifier` — separate future work,
  following the existing chart pattern in the `homelab` repo, same as
  deferred for `ingest`.
- A health-check-driven external alerting signal for sustained delivery
  failure past `MaxDeliver` — noted as a future improvement.
- Wiring anything beyond `NATS_URL`/`DISCORD_WEBHOOK_URL`/`PORT` config.

## Further Notes

- This spec is the direct output of a `/grill-with-docs` session; see
  `CONTEXT.md`'s `Source` and `Channel` entries for the vocabulary this spec
  uses.
- Mirrors `.scratch/ingest/spec.md`'s structure and decisions (ports-and-
  adapters split, nested `internal/`, static registries, integration-style
  testing with one faked external boundary) — read that spec for precedent
  if anything here is ambiguous.
- `docs/adr/0001-ports-and-adapters-with-nested-internal-isolation.md`
  explains the *why* behind the directory layout this spec assumes.
