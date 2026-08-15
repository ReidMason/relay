# resolution notifications

## Problem Statement

`notifier` filters Events per-Source by a `MinSeverity` floor (Unraid: `warning`).
Unraid reports its disk-temperature alert at `warning`, then later reports the
same disk back to normal at `info` (Unraid `importance: normal`) — that
follow-up Event is filtered out today, so an operator sees "disk is hot" but
never "disk is fine again." That specific `info`-severity Event is valuable
precisely because it closes a loop a higher-severity Event opened; routine
`info`-severity noise (array started, parity check finished, ...) is not, and
should stay filtered. The two are indistinguishable by Severity alone.

## Solution

Add a `Resolution` field to `event.Event`, orthogonal to `Severity` (see
`CONTEXT.md`'s `Resolution` entry). Each parser decides `Resolution` using
whatever real signal its vendor gives — for Unraid, a curated phrase match
against `Subject` when `importance: normal`. `notifier`'s routing becomes:
deliver if `Severity >= Route.MinSeverity` **or** `Resolution == true`,
unconditionally, no new `Route` field. Discord renders `Resolution` Events in
a distinct green, instead of the standard severity color, so they read as
good news rather than another grey info embed.

## User Stories

1. As a user, I want Unraid's "disk returned to normal temperature" message
   delivered to Discord even though it's `info` severity, so I know a problem
   I was alerted about is actually resolved.
2. As a user, I want other `info`-severity Unraid messages (array started,
   parity check finished, ...) to stay filtered, so Discord doesn't fill up
   with routine noise just because the resolution heuristic exists.
3. As a user, I want a resolution Event to look visually distinct in Discord
   (green, not grey) from a routine info-severity Event, so I can tell "this
   closes a problem" apart from "this is just FYI" at a glance.
4. As a developer adding a future source with an explicit vendor-native
   resolved/up signal (e.g. Uptime Kuma's heartbeat `status`), I want to set
   `Resolution` directly from that field, so I don't need Unraid's
   text-heuristic approach or any correlation/state store.
5. As a developer maintaining the Unraid resolution heuristic, I want the raw
   webhook body logged for every request `ingest` receives, so I can collect
   real examples over time and tune the phrase list against them instead of
   guessing.
6. As a developer, I want `Resolution` to default to `false` on every
   existing parser (Sonarr, and Unraid's non-matching paths) with no schema
   migration concerns, so this is additive and doesn't change existing
   routing behavior for anything already delivered today.

## Implementation Decisions

- **`event.Event` schema** (`internal/event/event.go`): add `Resolution
  bool`. Zero value `false` — existing parsers need no changes to keep
  current behavior. `event.New` gets a variant or option for setting it (see
  issue for exact signature — keep `New`'s existing call sites for Sonarr
  unaffected).
- **Unraid parser** (`internal/ingest/internal/unraid/unraid.go`): when
  `importance == "normal"`, additionally check `Subject` (not `Description`)
  case-insensitively against a small `var` phrase list: `"returned to
  normal"`, `"back to normal"`, `"no longer"`, `"restored"`. Match → `Event`
  built with `Resolution: true`. This is deliberately a tunable `var`, not a
  one-shot heuristic — expected to need adjustment once more real Unraid
  payloads are observed via the raw-request logging below. Confirmed against
  one real sample: Unraid's own Discord agent showed `Subject: "Notice
  [FERN] - Parity disk returned to normal temperature"` for a
  temperature-warning's resolution.
- **Raw webhook logging** (`internal/ingest/internal/transport/http/http.go`):
  log the raw request body via `slog` for every received webhook, every
  Source — generic to the HTTP transport, not Unraid-specific — alongside
  (not replacing) the existing received/parse/publish outcome logs. This is
  the mechanism for collecting real Unraid resolution-message examples to
  refine the phrase list.
- **`notifier` routing** (`internal/notifier/internal/core/core.go`):
  `Service.Handle` delivers to a matched Route's Channels when
  `severityRank[e.Severity] >= severityRank[route.MinSeverity]` **or**
  `e.Resolution`. No new field on `Route` — unconditional bypass, matching
  the current Route's existing per-Source floor design and the story that a
  resolution is inherently notify-worthy whenever its Source is routed at
  all.
- **Discord channel** (`internal/notifier/internal/transport/discord/discord.go`):
  when `e.Resolution`, embed `color` is green (pick a value consistent with
  the existing red/`0xE74C3C`, yellow/`0xF1C40F`, grey/`0x95A5A6` palette,
  e.g. `0x2ECC71`) instead of the Severity-derived color, and the embed
  carries a visual marker that it's a resolution (e.g. a ✅ prefix on
  `title`, or a footer note) — exact placement decided at implementation
  time, doesn't need to match Severity's footer/timestamp layout.
- **Routing table**: no change — Unraid's existing `MinSeverity: warning`
  Route in `main.go` is what a `Resolution` Event bypasses; no new Route
  entry needed.

## Testing Decisions

- Integration-style, matching `ingest`/`notifier`'s existing pattern.
- `ingest` (Unraid parser): fixture payloads for `importance: normal` with a
  `Subject` matching the phrase list (asserts `Resolution: true`) and one
  that doesn't (asserts `Resolution: false`, unchanged from today) — same
  `httptest.Server` + fake `Publisher` harness as existing Unraid tests,
  asserting on the captured `event.Event.Resolution` field.
- `ingest` (raw logging): not asserted via a specific test case — logging is
  operational, verified by inspection once deployed, not a behavioral
  contract worth pinning in a test.
- `notifier`: extend the existing embedded-NATS + fake-Discord-server harness
  with a case publishing an Unraid Event at `info` severity with
  `Resolution: true` — asserts it *is* delivered (unlike a same-severity,
  `Resolution: false` Event, already covered by the existing "Unraid at
  info — no delivery" case) — and asserts the captured embed's `color`
  reflects the green resolution color rather than the grey info color.

## Out of Scope

- Any correlation/state store linking a resolution Event back to the specific
  alert it resolves (e.g. matching disk serial, closing an "open alert"
  record) — this spec is stateless/vendor-trusted per `CONTEXT.md`'s
  `Resolution` entry; revisit only if the text-heuristic proves unreliable in
  practice.
- A second source's `Resolution` detection (e.g. Uptime Kuma) — separate
  future work; this spec only implements Unraid's heuristic, but the
  `Event.Resolution` field and `notifier` routing change are source-agnostic
  so a future source is a parser addition only.
- Editing/suppressing the original alert's Discord message when its
  resolution arrives (e.g. via Discord message editing or threads) — each
  resolution is delivered as an independent embed, no linkage to the prior
  alert's message.

## Further Notes

- Direct output of a `/grill-with-docs` session; `CONTEXT.md`'s `Resolution`
  entry (added in this session) is the vocabulary this spec uses — read it
  before implementing.
- Mirrors `.scratch/ingest/spec.md` and `.scratch/notifier/spec.md`'s
  structure/decisions (ports-and-adapters split, integration-style testing
  with one faked external boundary) — read those specs for precedent if
  anything here is ambiguous.
- The Unraid phrase list is explicitly a v1 best-effort, not a settled rule —
  the raw-request logging exists specifically to let it be revisited once
  real traffic is observed.
