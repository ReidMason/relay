# 02 — notifier routing bypass and Discord resolution styling

**What to build:** `notifier` delivers a matched Route's Channels an Event
that's below `MinSeverity` if `Resolution == true`, and the Discord channel
renders resolution Events in a distinct green color with a visual marker,
instead of the standard Severity-derived grey.

**Blocked by:** 01 (needs `event.Event.Resolution` to exist)

**Status:** ready-for-agent

- [ ] `internal/notifier/internal/core/core.go`: `Service.Handle`'s per-Route
      match condition becomes `severityRank[e.Severity] >=
      severityRank[route.MinSeverity] || e.Resolution` — no new field on
      `Route`, this is an unconditional bypass for any Event with
      `Resolution: true` whose `Source` matches an existing Route.
- [ ] `internal/notifier/internal/transport/discord/discord.go`: when
      `e.Resolution`, embed `color` is a new green constant (consistent with
      the existing red/`0xE74C3C`, yellow/`0xF1C40F`, grey/`0x95A5A6`
      palette, e.g. `0x2ECC71`) instead of the Severity-derived color, and
      the embed gets a visual marker that it's a resolution (e.g. a ✅ prefix
      on `title`) so it's distinguishable from a routine info embed at a
      glance.
- [ ] Integration tests (existing embedded-NATS + fake-Discord-server harness
      in `internal/notifier/internal/transport/nats`):
      - Publish an Unraid Event at `info` severity with `Resolution: true` →
        asserts it *is* delivered to the fake Discord server (Unraid's Route
        floor is `warning`, so this only passes because of the Resolution
        bypass) and that the captured embed's `color` is the green
        resolution color, not grey.
      - Existing "Unraid at `info`, `Resolution: false` — no delivery" case
        continues to pass unchanged (already covered, just confirms no
        regression from the routing change).
