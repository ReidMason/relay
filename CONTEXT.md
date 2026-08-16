# Relay

Homelab event bus. Vendor systems (Sonarr, Unraid, ...) emit webhooks; `ingest`
translates them into a common vocabulary and publishes to NATS; `notifier`
(and later consumers) act on that vocabulary without needing to know which
vendor produced it.

## Language

**Event**:
The canonical, self-contained envelope (`Source`, `Type`, `Severity`,
`Timestamp`, `Data`) published to NATS. A raw webhook payload is not an Event
until a parser has translated it — vendor-specific codes/enums never survive
into an Event's `Data`.
_Avoid_: Message, Notification, Payload

**Source**:
The origin system that produced an Event, e.g. `sonarr`, `unraid`. Consumers
may use it as a plain routing key (e.g. a declarative per-source severity
table) but must never branch on it *semantically* — interpreting what a
vendor's raw payload meant is ingest's job, never a consumer's.
_Avoid_: Vendor, Integration, Provider

**Channel**:
A notifier's pluggable delivery mechanism for an Event, e.g. `discord`. Like
a Parser is ingest's pluggable translation mechanism, a Channel is notifier's
pluggable delivery mechanism — notifier routes an Event to one or more
Channels based on Source/Severity, and a Channel only ever formats and sends,
never interprets.
_Avoid_: Engine (the implementing type, e.g. `discord.Adapter`, follows the
project's usual adapter-naming convention — "Adapter" isn't avoided as a type
name, just as a synonym for the Channel concept itself)

**Type**:
The intent an Event represents, e.g. `download.completed`, `disk.warning`.
Names a thing that happened, not a field that changed — `backup.failed`, not
`backup.updated`.
_Avoid_: Event name, Kind

**Severity**:
The generic urgency dimension (`info` | `warning` | `critical`) consumers
route on instead of inspecting `Source` or `Type` directly.
_Avoid_: Priority, Level

**Webhook payload**:
The raw, vendor-shaped JSON a Source POSTs to `ingest`. Exists only inside a
parser; never forwarded downstream and never stored in an Event's `Data`
un-translated.
_Avoid_: Request body, Raw event

**Resolution**:
A boolean dimension on Event, orthogonal to Severity, marking that the Event
represents a prior problem ending (e.g. Unraid's disk-temperature warning
being followed by a "returned to normal" message) rather than a new
occurrence. Severity stays whatever urgency the vendor reports (usually
`info`) — Resolution doesn't change what happened, it flags *why* a
low-severity Event is still worth delivering. A parser decides Resolution
using whatever real signal that vendor gives; notifier never infers it from
Source/Type.
_Avoid_: Recovery, Clear, Resolved-flag

**Test** (`Event.IsTest`):
A boolean dimension on Event, orthogonal to Severity and Resolution, marking
that the Event represents a vendor's "send a test webhook" button (e.g.
Sonarr's Connect "Test" button, Unraid's Settings > Notifications "Test")
rather than something that actually happened. notifier delivers a Test Event
regardless of a route's MinSeverity, the same way it always delivers a
Resolution — the whole point of a test is proving the pipe works
end-to-end, independent of severity routing config. A parser decides IsTest
using whatever real signal that vendor gives (an explicit `eventType`, a
fixed placeholder title, ...); notifier never infers it from Source/Type.
_Avoid_: Ping, Healthcheck (this repo already uses Liveness/Readiness for
service health — a Test Event is a vendor's webhook-connectivity check, an
unrelated concept)

**Liveness** (`/livez`):
Unconditional signal that a service's process is up and serving HTTP — no
dependency checks, always 200. Answers "should this be restarted," never
"should this receive traffic." See ADR-0002.
_Avoid_: Healthz, Health check (too generic — this repo distinguishes
Liveness from Readiness, so "health check" alone is ambiguous about which)

**Readiness** (`/readyz`):
Signal that a service can currently do its job — i.e. its required
dependencies (NATS JetStream) are reachable. Answers "should this receive
traffic." Backed by a cached `Checker` result (see Checker), not a live
per-request probe. Excludes best-effort/outbound dependencies like Discord —
see ADR-0002 for why. Returns 503 with the failing check(s) named until the
first successful probe completes.
_Avoid_: Healthz, Health check

**Checker**:
The `internal/health` component that pings a dependency on a fixed interval
(2s) and caches the last result for `readyz` to read, so request volume never
multiplies load on the dependency being checked. Pings through the small
`Pinger` interface, not a concrete client type — see ADR-0002.
_Avoid_: Health monitor, Prober
