// Package unraid implements core.Parser for Unraid's webhook payloads.
package unraid

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ReidMason/relay/internal/event"
)

const source = "unraid"

const eventType = "array.event"

type webhookPayload struct {
	Importance  string `json:"importance"`
	Subject     string `json:"subject"`
	Description string `json:"description"`
}

// Data is the flattened Data payload for Unraid Events. Unraid already
// writes Subject/Description as human-readable text, so it carries through
// largely as-is — only Importance is translated into event.Severity.
type Data struct {
	Subject     string
	Description string
}

// Parser implements core.Parser for Unraid webhook payloads.
type Parser struct{}

func New() Parser {
	return Parser{}
}

func (Parser) Parse(raw []byte) (event.Event, error) {
	var payload webhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return event.Event{}, fmt.Errorf("unraid: parse payload: %w", err)
	}

	severity, err := severityFor(payload.Importance)
	if err != nil {
		return event.Event{}, err
	}

	return event.Event{
		Source:    source,
		Type:      eventType,
		Severity:  severity,
		Timestamp: time.Now().UTC(),
		Data: Data{
			Subject:     payload.Subject,
			Description: payload.Description,
		},
	}, nil
}

func severityFor(importance string) (event.Severity, error) {
	switch importance {
	case "alert":
		return event.SeverityCritical, nil
	case "warning":
		return event.SeverityWarning, nil
	case "normal":
		return event.SeverityInfo, nil
	default:
		return "", fmt.Errorf("unraid: unrecognized importance %q", importance)
	}
}
