# 01 — Event.Resolution field, Unraid detection heuristic, raw webhook logging

**What to build:** Add the `Resolution` field to the shared `event.Event`
type, teach the Unraid parser to set it via a tunable phrase-match heuristic
on `Subject`, and add generic raw-request logging to `ingest`'s HTTP
transport so real Unraid payloads can be collected to refine that heuristic
over time.

**Blocked by:** None — can start immediately

**Status:** ready-for-agent

- [ ] `internal/event/event.go`: add `Resolution bool` to `Event`. Update
      `event.New` (or add a variant) so callers can set it without breaking
      existing call sites that don't care about it (Sonarr's parser, and
      Unraid's non-resolution paths, should end up with `Resolution: false`
      by default).
- [ ] `internal/ingest/internal/unraid/unraid.go`: when `severityFor` maps
      `importance == "normal"`, additionally check `payload.Subject`
      case-insensitively against a package-level `var` phrase list:
      `"returned to normal"`, `"back to normal"`, `"no longer"`, `"restored"`.
      On match, the returned `event.Event` has `Resolution: true`; otherwise
      `false` (i.e. no behavior change from today for non-matching `normal`
      messages). Keep the phrase list as an easily-editable `var`, not
      inlined — it's expected to be tuned later from real traffic.
- [ ] `internal/ingest/internal/transport/http/http.go`: log the raw request
      body via `slog` (e.g. `slog.Info("received webhook", "source", source,
      "body", string(raw))`) for every received webhook, regardless of
      Source — alongside the existing received/parse/publish outcome
      logging, not replacing it.
- [ ] Integration tests (same `httptest.Server` + fake `Publisher` harness as
      existing Unraid tests in `internal/ingest/internal/transport/http`):
      - `importance: normal` with a `Subject` matching the phrase list (e.g.
        `"Notice [FERN] - Parity disk returned to normal temperature"`) →
        captured `Event.Resolution == true`.
      - `importance: normal` with a `Subject` that doesn't match (e.g. an
        "array started" style message) → captured `Event.Resolution ==
        false`, `Severity == info`, unchanged from current behavior.
      - Existing `alert`/`warning` importance cases continue to assert
        `Resolution == false` (no regression).
