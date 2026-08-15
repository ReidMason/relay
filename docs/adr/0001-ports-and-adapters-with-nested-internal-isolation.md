# Ports-and-adapters core, with nested `internal/` for compile-time service isolation

Status: accepted

`relay` will eventually house two binaries — `ingest` (webhook receiver) and
`notifier` (severity-routed delivery) — that must never share domain logic or
directly depend on each other, per the project's core design principle that
generic consumers stay ignorant of what produced an Event. We chose a
lightweight ports-and-adapters split per service: a `core` package defines the
`Parser` and `Publisher` interfaces plus a `Service` orchestrator, and
`transport/http` / `transport/nats` are adapters that implement or call those
interfaces. Full clean-architecture layering (entities/use-cases/interface
adapters/frameworks) was rejected as more ceremony than two vendor sources and
one publish target justify.

To make service isolation a compiler guarantee rather than a discipline, each
service gets its own **nested** `internal/` directory
(`internal/ingest/internal/...`, later `internal/notifier/internal/...`)
instead of one shared top-level `internal/`. Go's internal-visibility rule is
scoped to the tree rooted at the parent of the *innermost* `internal` segment
in a path — so `internal/notifier/...` is structurally unable to import
`internal/ingest/internal/...`, and vice versa, with no lint tooling required.
As a consequence, each service's `cmd/main.go` had to move inside that
service's own tree (e.g. `internal/ingest/cmd/main.go`) instead of the
conventional top-level `cmd/<binary>` layout, since `main.go` needs to be
within the tree that's allowed to import its own service's guarded internals.
`internal/event` stays at the outer, module-wide `internal/` — it's the one
package intentionally shared by both services.

Within a service, `core` never imports its own `transport/*` packages; if it
tried to, the build would fail with an import cycle, since the adapters
already import `core` to implement its ports. That gets core→adapter
direction enforcement for free from the same no-cycle rule, no extra
structure needed.
