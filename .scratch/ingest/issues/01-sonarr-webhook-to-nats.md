# 01 — Sonarr webhook → Event → NATS publish (core scaffolding)

**What to build:** `ingest` becomes a real, runnable service. A `POST
/webhooks/sonarr` request carrying a Sonarr webhook payload results in a
translated `Event` being published to NATS JetStream, and a `GET /healthz`
request reports liveness/readiness. This ticket stands up the whole
ports-and-adapters skeleton the service will run on — everything downstream
(other vendor sources) plugs into it without touching this layer.

**Blocked by:** None — can start immediately

**Status:** ready-for-agent

- [ ] Module layout follows ADR 0001: `internal/event` (shared `Event` struct
      + `homelab.<source>.<type>` subject helper) at the module-wide
      `internal/`; `ingest` itself under `internal/ingest/`, with its own
      guarded internals at `internal/ingest/internal/...` and its binary
      entrypoint at `internal/ingest/cmd/main.go`.
- [ ] `internal/ingest/internal/core` defines the `Parser`
      (`Parse(raw []byte) (event.Event, error)`) and `Publisher` ports, and a
      `Service` orchestrator (`Handle(source string, raw []byte) error`) that
      looks up the source's `Parser` in a registry, calls it, and on success
      calls `Publisher`. `core` does not import either transport package.
- [ ] A sentinel `ErrSkip` (checked via `errors.Is`) exists for "valid
      webhook, nothing to publish."
- [ ] Sonarr parser (`internal/ingest/internal/core` or a sibling package)
      implements `Parser`, switching on Sonarr's `eventType`:
      | Sonarr `eventType` | Type | Severity |
      |---|---|---|
      | `Download` | `download.completed` | info |
      | `Grab` | `grab` | info |
      | `HealthIssue` | `health.issue` | warning |
      | `Test` | — | `ErrSkip` |
      `Data` is a flattened struct (e.g. `SeriesTitle`, `EpisodeTitle`,
      `SeasonNumber`, `EpisodeNumber`) pulled from Sonarr's nested
      `series`/`episodes` objects — never Sonarr's raw payload passed
      through.
- [ ] `transport/http` adapter: stdlib `net/http` with Go 1.22+ patterns
      (no router library). Routes: `POST /webhooks/{source}` dispatches via
      `core.Service`, `GET /healthz`. Status mapping: `ErrSkip` → 200,
      unknown `{source}` → 404, other parse error → 400, publish error → 500,
      success → 200.
- [ ] `transport/nats` adapter implements `Publisher` with a synchronous
      `js.Publish` (blocks for ack; failure propagates as an error). On
      startup, idempotently ensures the `HOMELAB_EVENTS` JetStream stream
      (subjects `homelab.>`) exists (create-if-absent).
- [ ] Parser registry is a literal `map[string]core.Parser` built explicitly
      in `main.go`, containing `"sonarr"` for now. Wiring (Service, HTTP
      server, NATS adapter) is manual constructor injection — no DI
      framework.
- [ ] Config is env-var only (e.g. `NATS_URL`, `PORT`) — no config files.
- [ ] `log/slog` structured logging for received webhooks and
      parse/publish outcomes.
- [ ] No auth on webhook endpoints (network-level trust only, in-cluster).
- [ ] Integration tests: real `httptest.Server` wrapping the real
      `transport/http` adapter + real `core.Service` + real Sonarr parser,
      with only `Publisher` faked (captures published `Event`s in memory).
      Cover: each `eventType` row above, the `Test`-skip path (200, nothing
      captured), malformed JSON (400, nothing captured), unknown `{source}`
      (404), and a `Publisher` failure (500).
