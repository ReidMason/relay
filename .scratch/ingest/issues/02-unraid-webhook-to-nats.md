# 02 — Unraid webhook → Event → NATS publish

**What to build:** `POST /webhooks/unraid` works end-to-end, reusing all
scaffolding from ticket 01 (core, transports, registry, config, logging).
Unraid array/disk events become the same canonical `Event` vocabulary Sonarr
events already use.

**Blocked by:** 01 — Sonarr webhook → Event → NATS publish (core scaffolding)

**Status:** ready-for-agent

- [ ] Unraid parser implements the `Parser` port, mapping Unraid's
      `importance` field to `Severity`:
      | Unraid `importance` | Severity |
      |---|---|
      | `alert` | critical |
      | `warning` | warning |
      | `normal` | info |
      Unraid's own `subject`/`description` text carries into `Data` largely
      as-is; only `importance` needs translation.
- [ ] Parser registry in `main.go` gains a `"unraid"` entry pointing at the
      new parser — no changes needed to `core`, `transport/http`, or
      `transport/nats`.
- [ ] `POST /webhooks/unraid` behaves per the same status mapping already
      established in ticket 01 (200 on success, 400 on malformed payload,
      500 on publish failure).
- [ ] Integration tests (same style as ticket 01: real HTTP → real Service →
      real Unraid parser, fake `Publisher`) cover: each `importance` row
      above, malformed JSON (400, nothing captured), and a `Publisher`
      failure (500).
