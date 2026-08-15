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
