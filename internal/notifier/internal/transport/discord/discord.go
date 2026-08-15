// Package discord is the transport adapter implementing core.Channel by
// posting a Discord webhook embed.
package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"
	"unicode"

	"github.com/ReidMason/relay/internal/event"
)

const (
	colorCritical = 0xE74C3C
	colorWarning  = 0xF1C40F
	colorInfo     = 0x95A5A6
)

// Adapter implements core.Channel by POSTing a Discord webhook embed.
type Adapter struct {
	webhookURL string
	httpClient *http.Client
}

func New(webhookURL string) *Adapter {
	return &Adapter{webhookURL: webhookURL, httpClient: &http.Client{Timeout: 10 * time.Second}}
}

type webhookBody struct {
	Embeds []embed `json:"embeds"`
}

type embed struct {
	Title     string  `json:"title"`
	Color     int     `json:"color"`
	Timestamp string  `json:"timestamp,omitempty"`
	Footer    *footer `json:"footer,omitempty"`
	Fields    []field `json:"fields,omitempty"`
}

type footer struct {
	Text string `json:"text"`
}

type field struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Send posts e as a single Discord embed. e.Data is rendered generically —
// no Source-specific field knowledge — via a mechanical PascalCase-to-Title
// Case key split.
func (a *Adapter) Send(e event.Event) error {
	body := webhookBody{Embeds: []embed{{
		Title:  fmt.Sprintf("%s: %s", e.Source, e.Type),
		Color:  colorFor(e.Severity),
		Footer: &footer{Text: string(e.Source)},
		Fields: fieldsFor(e.Data),
	}}}
	if !e.Timestamp.IsZero() {
		body.Embeds[0].Timestamp = e.Timestamp.Format(time.RFC3339)
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("discord: marshal webhook body: %w", err)
	}

	resp, err := a.httpClient.Post(a.webhookURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("discord: post webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord: webhook returned status %d", resp.StatusCode)
	}
	return nil
}

func colorFor(s event.Severity) int {
	switch s {
	case event.SeverityCritical:
		return colorCritical
	case event.SeverityWarning:
		return colorWarning
	default:
		return colorInfo
	}
}

func fieldsFor(data any) []field {
	m, ok := data.(map[string]any)
	if !ok {
		return nil
	}

	fields := make([]field, 0, len(m))
	for k, v := range m {
		fields = append(fields, field{Name: titleCase(k), Value: stringify(v)})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	return fields
}

// titleCase splits a PascalCase key on uppercase boundaries and joins with
// spaces, e.g. "SeriesTitle" -> "Series Title".
func titleCase(key string) string {
	var out []rune
	for i, r := range key {
		if i > 0 && unicode.IsUpper(r) {
			out = append(out, ' ')
		}
		out = append(out, r)
	}
	return string(out)
}

func stringify(v any) string {
	switch v.(type) {
	case map[string]any, []any:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	default:
		return fmt.Sprintf("%v", v)
	}
}
