# 01 — notifier core scaffolding, NATS consumer, severity routing, Discord channel

**What to build:** `notifier` becomes a real, runnable service. It durably
consumes Events from `homelab.>`, routes each one through a per-`Source`
severity floor, and delivers matching Events to Discord as a readable embed.
`GET /healthz` reports liveness/readiness. This ticket stands up the whole
ports-and-adapters skeleton — a second `Channel` later plugs in without
touching this layer.

**Blocked by:** None — can start immediately

**Status:** ready-for-agent

- [ ] Module layout follows ADR-0001: `notifier` lives under
      `internal/notifier/`, guarded internals at
      `internal/notifier/internal/...`, binary entrypoint at
      `internal/notifier/cmd/main.go`. Uses the shared `internal/event`
      package (no changes needed there).
- [ ] `internal/notifier/internal/core` defines:
      - `Channel` interface: `Send(event.Event) error`.
      - `ChannelName` — a `string`-based const type (e.g. `ChannelDiscord
        ChannelName = "discord"`), not raw strings.
      - `Route` struct: `{Source event.Source; MinSeverity event.Severity;
        Channels []ChannelName}`.
      - `Service`: `Handle(e event.Event) error`, orchestrator taking
        `routes []Route` and `channels map[ChannelName]Channel` at
        construction. For each `Route` whose `Source` matches `e.Source` and
        whose `MinSeverity` is `<=` `e.Severity` (ordering `info < warning <
        critical`), calls `Send` on every named `Channel`. Returns an error
        if any `Send` fails (caller — the NATS adapter — uses this to decide
        ack vs. leave-for-redelivery).
      `core` must not import `transport/nats` or `transport/discord`.
- [ ] `transport/nats` adapter: durable JetStream pull consumer, name
      `"notifier"`, subject `homelab.>`, `DeliverPolicy: New`,
      `MaxDeliver: 5`. Decodes each message into `event.Event` (JSON), calls
      `core.Service.Handle`. Acks on success; on failure, does not ack
      (JetStream redelivers up to `MaxDeliver`). Logs via `slog` when a
      message exhausts `MaxDeliver` (JetStream's terminal state for it —
      confirm exact detection mechanism against the `nats.go`/`jetstream`
      API when implementing, e.g. tracking delivery count from message
      metadata).
- [ ] `transport/discord` adapter implements `Channel`:
      - POSTs to `DISCORD_WEBHOOK_URL` (Discord's standard webhook JSON body
        with one `embeds[0]`).
      - `color`: red/`0xE74C3C` (critical), yellow/`0xF1C40F` (warning),
        grey/`0x95A5A6` (info).
      - `title`: `string(e.Type)`.
      - `footer.text`: `string(e.Source)`; embed `timestamp` field:
        `e.Timestamp` (RFC3339).
      - `fields`: decode `e.Data` as `map[string]any`, one field per key.
        Key formatting: PascalCase → spaced Title Case (`SeriesTitle` →
        `Series Title`) via a small regex/rune-scan splitter on uppercase
        boundaries — no source-specific field-name knowledge. Values:
        `fmt.Sprintf("%v", v)` for scalars; `json.Marshal` fallback for
        `map`/`slice` values. Fields sorted alphabetically by (formatted)
        key.
      - Non-2xx response or request error returns a non-nil error.
- [ ] Routing table and channel registry are literal values built in
      `main.go` (not dynamic/config-driven):
      ```go
      routes := []core.Route{
          {Source: unraid.Source, MinSeverity: event.SeverityWarning, Channels: []core.ChannelName{core.ChannelDiscord}},
          {Source: sonarr.Source, MinSeverity: event.SeverityCritical, Channels: []core.ChannelName{core.ChannelDiscord}},
      }
      channels := map[core.ChannelName]core.Channel{
          core.ChannelDiscord: discord.New(discordWebhookURL),
      }
      ```
      `unraid.Source`/`sonarr.Source` live under `internal/ingest/internal/...`
      and are structurally unimportable from `internal/notifier/...` by
      design (ADR-0001) — declare matching `event.Source` consts locally in
      `notifier` (e.g. `"unraid"`, `"sonarr"`) instead of importing them.
- [ ] `GET /healthz` via a minimal `net/http` server (no other routes).
- [ ] Config is env-var only: `NATS_URL`, `DISCORD_WEBHOOK_URL`, `PORT`.
- [ ] Manual constructor-based wiring in `main.go` — no DI framework.
- [ ] `log/slog` structured logging for consumed Events, routing decisions,
      and delivery outcomes (success/failure/redelivery-exhausted).
- [ ] Integration tests: real embedded NATS/JetStream server
      (`nats-server` test helpers), real `transport/nats` consumer, real
      `core.Service` with the real routing table, fake Discord webhook
      (`httptest.Server` capturing posted embed JSON). Cover: Unraid at
      `info` (nothing delivered), Unraid at `warning`/`critical`
      (delivered), Sonarr at `info`/`warning` (nothing delivered), Sonarr at
      `critical` (delivered), a fake-Discord 500 response leaves the message
      unacked/redelivered, and field-formatting on a multi-key `Data`
      payload (verify Title Case conversion + alphabetical order).
