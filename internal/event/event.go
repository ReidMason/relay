// Package event defines the canonical Event vocabulary shared by ingest and
// notifier — the one package intentionally shared across services.
package event

import "time"

// Severity is the generic urgency dimension consumers route on instead of
// inspecting Source or Type directly.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Source identifies the origin system that produced an Event, e.g. "sonarr",
// "unraid". Each parser package declares its own Source constant.
type Source string

// Type names the intent an Event represents, e.g. "download.completed",
// "disk.warning". Each parser package declares its own Type constants.
type Type string

// Event is the canonical, self-contained envelope published to NATS. A raw
// webhook payload is not an Event until a parser has translated it —
// vendor-specific codes/enums never survive into Data.
type Event struct {
	Source    Source
	Type      Type
	Severity  Severity
	Timestamp time.Time
	Data      any
}

// New builds an Event stamped with the current time.
func New(source Source, eventType Type, severity Severity, data any) Event {
	return Event{
		Source:    source,
		Type:      eventType,
		Severity:  severity,
		Timestamp: time.Now().UTC(),
		Data:      data,
	}
}

// Subject returns the NATS subject an Event of the given source and type is
// published under: homelab.<source>.<type>.
func Subject(source Source, eventType Type) string {
	return "homelab." + string(source) + "." + string(eventType)
}
