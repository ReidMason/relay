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
The origin system that produced an Event, e.g. `sonarr`, `unraid`. Identifies
where an Event came from; consumers must never branch their logic on it — see
Type and Severity, which exist so they don't have to.
_Avoid_: Vendor, Integration, Provider

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
