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
	Source     Source
	Type       Type
	Severity   Severity
	Resolution bool
	IsTest     bool
	Timestamp  time.Time
	Data       any
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

// NewResolution builds an Event stamped with the current time, marked as a
// Resolution (see CONTEXT.md) — a prior problem ending, rather than a new
// occurrence.
func NewResolution(source Source, eventType Type, severity Severity, data any) Event {
	e := New(source, eventType, severity, data)
	e.Resolution = true
	return e
}

// NewTest builds an Event stamped with the current time, marked as a
// connectivity test (see CONTEXT.md) — a vendor's "send a test webhook"
// button, rather than something that actually happened. notifier delivers
// it regardless of a route's MinSeverity, the same way it always delivers a
// Resolution, since the point of a test is to prove the pipe works
// end-to-end independent of severity routing config.
func NewTest(source Source, eventType Type, severity Severity, data any) Event {
	e := New(source, eventType, severity, data)
	e.IsTest = true
	return e
}

// Subject returns the NATS subject an Event of the given source and type is
// published under: homelab.<source>.<type>.
func Subject(source Source, eventType Type) string {
	return "homelab." + string(source) + "." + string(eventType)
}
