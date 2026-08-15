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

// Event is the canonical, self-contained envelope published to NATS. A raw
// webhook payload is not an Event until a parser has translated it —
// vendor-specific codes/enums never survive into Data.
type Event struct {
	Source    string
	Type      string
	Severity  Severity
	Timestamp time.Time
	Data      any
}

// Subject returns the NATS subject an Event of the given source and type is
// published under: homelab.<source>.<type>.
func Subject(source, eventType string) string {
	return "homelab." + source + "." + eventType
}
