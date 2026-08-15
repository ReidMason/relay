// Package unraid implements core.Parser for Unraid's webhook payloads.
package unraid

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ReidMason/relay/internal/event"
)

// resolutionPhrases are matched case-insensitively against a normal-
// importance webhook's Subject to detect that it's a Resolution (see
// CONTEXT.md) rather than routine info noise (array started, parity check
// finished, ...). This is a best-effort v1, not a settled rule — expected to
// be tuned as real Unraid payloads are observed (see the raw webhook request
// logging in transport/http).
var resolutionPhrases = []string{
	"returned to normal",
	"back to normal",
	"no longer",
	"restored",
}

// Source is the event.Source this parser produces Events for.
const Source event.Source = "unraid"

// TypeArrayEvent is the event.Type for all Unraid array/disk notifications.
const TypeArrayEvent event.Type = "array.event"

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

	data := Data{
		Subject:     payload.Subject,
		Description: payload.Description,
	}

	if isResolution(payload.Importance, payload.Subject) {
		return event.NewResolution(Source, TypeArrayEvent, severity, data), nil
	}
	return event.New(Source, TypeArrayEvent, severity, data), nil
}

// isResolution reports whether a normal-importance webhook's Subject reads
// like a prior problem ending, per resolutionPhrases.
func isResolution(importance, subject string) bool {
	if importance != "normal" {
		return false
	}
	lower := strings.ToLower(subject)
	for _, phrase := range resolutionPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
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
