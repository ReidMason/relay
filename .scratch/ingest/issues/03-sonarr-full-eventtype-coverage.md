# 03 — Sonarr parser: full eventType coverage

**What to build:** The Sonarr parser (`internal/ingest/internal/sonarr`) only
recognizes `Download`, `Grab`, `HealthIssue`, and `Test` — every other
`eventType` Sonarr can actually send (e.g. `EpisodeFileDelete`) hits the
`default` case and 400s, which is exactly what's happening in production
today (see log: `sonarr: unrecognized eventType "EpisodeFileDelete"`). This
ticket expands the parser to cover Sonarr's full `WebhookEventType` enum.

**Blocked by:** None — can start immediately

**Status:** ready-for-agent

## Verified wire mapping

Verified directly against Sonarr's `develop` branch source
(`src/NzbDrone.Core/Notifications/Webhook/WebhookEventType.cs` and
`WebhookBase.cs`), not the UI's display names. Sonarr's actual `eventType`
enum has 11 values: `Test, Grab, Download, Rename, SeriesAdd, SeriesDelete,
EpisodeFileDelete, Health, ApplicationUpdate, HealthRestored,
ManualInteractionRequired`.

Two UI toggles that are **not** separate wire eventTypes:
- "On Upgrade" — same `Download` eventType as Import, distinguished by the
  payload's `IsUpgrade` bool (`IsUpgrade = message.OldFiles.Any()` in
  Sonarr's source).
- "Include Health Warnings" — config-only; gates whether warning-level health
  checks fire the `Health` webhook at all. Not an eventType.

One UI toggle that is **not distinguishable at all** from an existing case:
- "On Import Complete" — also sends `eventType: "Download"`. Only
  distinguishable from Import/Upgrade by payload *shape* (`WebhookImportCompletePayload`
  has a plural `EpisodeFiles` list + `SourcePath`/`DestinationPath`, no
  singular `EpisodeFile`/`IsUpgrade`). Decision: **leave unhandled** —
  inferring intent from payload shape instead of an explicit field is weaker
  than every other case in this table and out of step with CONTEXT.md's
  principle that parsers translate explicit vendor signals. It falls through
  to `download.completed`/`download.upgraded` today, which isn't wrong, just
  imprecise. Revisit if Sonarr ever adds an explicit field for it.

| Sonarr `eventType` | Disambiguator | relay `Type` | Severity |
|---|---|---|---|
| `Grab` | — | `grab` (existing) | info |
| `Download` | `IsUpgrade: false` | `download.completed` (existing) | info |
| `Download` | `IsUpgrade: true` | `download.upgraded` (new) | info |
| `Download` | shape (`EpisodeFiles` list) | — unhandled, see above | — |
| `Rename` | — | `episode.renamed` (new) | info |
| `SeriesAdd` | — | `series.added` (new) | info |
| `SeriesDelete` | — | `series.deleted` (new) | info |
| `EpisodeFileDelete` | `DeleteReason` (compare case-insensitively — older Sonarr versions send lowercase values) | `episode_file.deleted` (new) | info |
| `Health` | — | `health.issue` (existing) | warning |
| `HealthRestored` | — | `health.issue` + `Resolution: true` (see CONTEXT.md's Resolution concept — mirrors the Unraid parser's `event.NewResolution` pattern, same Type as the issue it resolves rather than a new Type) | warning |
| `ApplicationUpdate` | — | `application.updated` (new) | info |
| `ManualInteractionRequired` | — | `manual_interaction.required` (new) | info |
| `Test` | — | skip (existing, `core.ErrSkip`) | — |
| anything else | — | 400 error (existing strict behavior, unchanged) | — |

Existing three Type constants (`download.completed`, `grab`, `health.issue`)
are **not** renamed — renaming would break `homelab.<source>.<type>` NATS
subjects for existing consumers, out of scope here. New Type constants use
dot-namespaced `noun.verb` naming, matching `health.issue`'s style.

## Struct changes

`webhookPayload` (`internal/ingest/internal/sonarr/sonarr.go`) gains two
fields read for the `Download`/`EpisodeFileDelete` cases:
- `IsUpgrade bool` (`json:"isUpgrade"`)
- `DeleteReason string` (`json:"deleteReason"`)

## Data structs

Following the existing pattern (flattened, human-readable, one struct per
event family):

- `grab` → `DownloadData` (existing, unchanged)
- `download.completed` / `download.upgraded` → `DownloadData` (existing,
  unchanged — the Type already encodes upgrade-vs-not, so `Data` doesn't
  duplicate it)
- `episode.renamed` → `RenameData{SeriesTitle string, RenamedCount int}`
- `series.added` → `SeriesAddData{SeriesTitle string}`
- `series.deleted` → `SeriesDeleteData{SeriesTitle string, DeleteFiles bool}`
  (kept as two distinct structs rather than one shared shape, so a
  `series.added` Event's `Data` can never carry a meaningless
  `DeleteFiles: false` that looks like a real "don't delete files" signal)
- `episode_file.deleted` → `EpisodeFileDeleteData{SeriesTitle,
  EpisodeTitle string, SeasonNumber, EpisodeNumber int, Reason string}`
  (`Reason` = Sonarr's `DeleteReason`, normalized)
- `application.updated` → `ApplicationUpdateData{Message, PreviousVersion,
  NewVersion string}`
- `manual_interaction.required` → `ManualInteractionData{SeriesTitle,
  EpisodeTitle, DownloadStatus, Message string}`
- `health.issue` (both `Health` and `HealthRestored`) → `HealthIssueData`
  (existing, unchanged)

## Severity

All new Types default to `info`, matching Sonarr's own UI (it doesn't badge
these as errors). Only `health.issue` (both directions) stays `warning`,
per the existing mapping.

## Unrecognized-eventType policy

Unchanged: any `eventType` outside the table above still hits the `default`
case and returns a 400 (`sonarr: unrecognized eventType %q`). This is a
deliberate strict allowlist — a future new Sonarr eventType should be an
explicit human decision (a new row in this table), not a silent pass-through.

- [ ] Add `IsUpgrade` and `DeleteReason` fields to `webhookPayload`.
- [ ] Add new `event.Type` constants: `TypeRename` (`episode.renamed`),
      `TypeSeriesAdd` (`series.added`), `TypeSeriesDelete`
      (`series.deleted`), `TypeEpisodeFileDelete` (`episode_file.deleted`),
      `TypeApplicationUpdate` (`application.updated`),
      `TypeManualInteractionRequired` (`manual_interaction.required`), and
      `TypeDownloadUpgraded` (`download.upgraded`).
- [ ] Add new Data structs per the mapping above.
- [ ] Extend the `Parse` switch: `Rename`, `SeriesAdd`, `SeriesDelete`,
      `EpisodeFileDelete`, `ApplicationUpdate`, `ManualInteractionRequired`,
      `HealthRestored` cases; split `Download` into upgrade/non-upgrade via
      `IsUpgrade`.
- [ ] `HealthRestored` case uses `event.NewResolution`, not `event.New`.
- [ ] Integration tests (per project preference, real `httptest.Server` +
      real parser + fake `Publisher`, matching the existing pattern in
      `transport/http`): one case per new eventType row above, plus a case
      confirming `Download` + `IsUpgrade: true` produces `download.upgraded`
      and `EpisodeFileDelete` carries `DeleteReason` into `Data.Reason`
      case-insensitively (e.g. lowercase `"manual"` from the real log
      sample still parses correctly).

## Comments

- Addendum (same session, post-implementation): Sonarr's `Test` eventType no
  longer skips (`core.ErrSkip`) — it now publishes an `event.NewTest` Event
  (`Type: test`), and notifier's `MinSeverity` floor bypasses `IsTest`
  Events the same way it already bypasses `Resolution` ones. This was
  needed because notifier's Sonarr route is configured at
  `MinSeverity: Critical` (`internal/notifier/cmd/main.go`), so even a
  published Test Event would otherwise never reach Discord — silently
  defeating the button's purpose of proving the webhook is wired up
  correctly. Unraid's equivalent Test button (Settings > Notifications)
  sends a fixed `"Discord test."` title/description at `normal` priority;
  the Unraid parser now detects that exact string and marks it `IsTest`
  too, for the same reason (Unraid's route floor is `warning`, above
  `normal`'s `info` severity). See `internal/event/event.go`'s `IsTest`
  field and `CONTEXT.md`'s "Test" entry.
