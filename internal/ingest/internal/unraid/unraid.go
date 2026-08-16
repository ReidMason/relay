// Package unraid implements core.Parser for Unraid's webhook payloads.
//
// Unraid has no generic custom-webhook notification agent: its "Discord"
// agent hardcodes Discord's own embed webhook shape and POSTs it wherever
// it's configured to point, including here when someone puts relay's URL in
// the agent's "Webhook URL" field. So the payload this parser sees is a
// literal Discord embed, not a bespoke Unraid schema.
package unraid

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ReidMason/relay/internal/event"
)

// resolutionPhrases are matched case-insensitively against a normal-severity
// webhook's title to detect that it's a Resolution (see CONTEXT.md) rather
// than routine info noise (array started, parity check finished, ...). This
// is a best-effort v1, not a settled rule — expected to be tuned as real
// Unraid payloads are observed (see the raw webhook request logging in
// transport/http).
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

type embedPayload struct {
	Embeds []embed `json:"embeds"`
}

type embed struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Fields      []field `json:"fields"`
}

type field struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Data is the flattened Data payload for Unraid Events. Unraid already
// writes title/description as human-readable text, so it carries through
// largely as-is — only the embed's Priority field is translated into
// event.Severity.
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
	var payload embedPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return event.Event{}, fmt.Errorf("unraid: parse payload: %w", err)
	}

	if len(payload.Embeds) == 0 {
		return event.Event{}, fmt.Errorf("unraid: payload has no embeds")
	}
	e := payload.Embeds[0]

	if e.Title == "" {
		return event.Event{}, fmt.Errorf("unraid: embed missing title")
	}
	if e.Description == "" {
		return event.Event{}, fmt.Errorf("unraid: embed missing description")
	}

	severity := severityFor(unraidPriorityField(e.Fields))

	data := Data{
		Subject:     e.Title,
		Description: e.Description,
	}

	if isResolution(severity, e.Title) {
		return event.NewResolution(Source, TypeArrayEvent, severity, data), nil
	}
	return event.New(Source, TypeArrayEvent, severity, data), nil
}

// unraidPriorityField returns the value of the embed field Unraid names
// "Priority" (case-insensitive), or "" if there isn't one. This is vendor
// wire vocabulary, translated into the canonical event.Severity by
// severityFor — it never reaches Data or downstream consumers.
func unraidPriorityField(fields []field) string {
	for _, f := range fields {
		if strings.EqualFold(f.Name, "Priority") {
			return f.Value
		}
	}
	return ""
}

// isResolution reports whether a normal-severity embed's title reads like a
// prior problem ending, per resolutionPhrases.
func isResolution(severity event.Severity, title string) bool {
	if severity != event.SeverityInfo {
		return false
	}
	lower := strings.ToLower(title)
	for _, phrase := range resolutionPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// severityFor maps Unraid's Priority values (normal/warning/alert) to
// event.Severity. An unrecognized or missing value defaults to info rather
// than erroring — Priority classification is best-effort, unlike title and
// description which carry the notification's actual content.
func severityFor(priority string) event.Severity {
	switch strings.ToLower(priority) {
	case "alert":
		return event.SeverityCritical
	case "warning":
		return event.SeverityWarning
	default:
		return event.SeverityInfo
	}
}
