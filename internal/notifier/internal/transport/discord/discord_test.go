package discord_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ReidMason/relay/internal/event"
	"github.com/ReidMason/relay/internal/notifier/internal/transport/discord"
)

type embed struct {
	Title  string `json:"title"`
	Color  int    `json:"color"`
	Footer struct {
		Text string `json:"text"`
	} `json:"footer"`
	Timestamp string `json:"timestamp"`
	Fields    []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"fields"`
}

type webhookBody struct {
	Embeds []embed `json:"embeds"`
}

func TestSend_PostsHumanReadableEmbed(t *testing.T) {
	var captured webhookBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode webhook body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	adapter := discord.New(server.URL)

	ts := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	e := event.Event{
		Source:    "unraid",
		Type:      "array.event",
		Severity:  event.SeverityCritical,
		Timestamp: ts,
		Data: map[string]any{
			"Subject":     "Disk failure",
			"Description": "disk1 SMART error",
		},
	}

	if err := adapter.Send(e); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if len(captured.Embeds) != 1 {
		t.Fatalf("expected 1 embed, got %d", len(captured.Embeds))
	}
	got := captured.Embeds[0]

	if got.Title != "unraid: array.event" {
		t.Errorf("title = %q, want %q", got.Title, "unraid: array.event")
	}
	if got.Footer.Text != "unraid" {
		t.Errorf("footer = %q, want %q", got.Footer.Text, "unraid")
	}
	if got.Color != 0xE74C3C {
		t.Errorf("color = %#x, want %#x (critical/red)", got.Color, 0xE74C3C)
	}

	if len(got.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d: %+v", len(got.Fields), got.Fields)
	}
	// Sorted alphabetically by formatted key: "Description" before "Subject".
	if got.Fields[0].Name != "Description" || got.Fields[0].Value != "disk1 SMART error" {
		t.Errorf("field[0] = %+v, want Description/disk1 SMART error", got.Fields[0])
	}
	if got.Fields[1].Name != "Subject" || got.Fields[1].Value != "Disk failure" {
		t.Errorf("field[1] = %+v, want Subject/Disk failure", got.Fields[1])
	}
}

func TestSend_PascalCaseKeySplitIntoTitleCase(t *testing.T) {
	var captured webhookBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	adapter := discord.New(server.URL)

	e := event.Event{
		Source:   "sonarr",
		Type:     "download.completed",
		Severity: event.SeverityInfo,
		Data: map[string]any{
			"SeriesTitle":   "Foo",
			"EpisodeNumber": float64(3),
		},
	}

	if err := adapter.Send(e); err != nil {
		t.Fatalf("Send: %v", err)
	}

	names := map[string]string{}
	for _, f := range captured.Embeds[0].Fields {
		names[f.Name] = f.Value
	}
	if names["Series Title"] != "Foo" {
		t.Errorf("expected 'Series Title' field = Foo, got fields: %+v", names)
	}
	if names["Episode Number"] != "3" {
		t.Errorf("expected 'Episode Number' field = 3, got fields: %+v", names)
	}
}

func TestSend_NonNestedValue_JSONFallback(t *testing.T) {
	var captured webhookBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	adapter := discord.New(server.URL)

	e := event.Event{
		Source:   "sonarr",
		Type:     "grab",
		Severity: event.SeverityInfo,
		Data: map[string]any{
			"Episodes": []any{"S01E01", "S01E02"},
		},
	}

	if err := adapter.Send(e); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if captured.Embeds[0].Fields[0].Value != `["S01E01","S01E02"]` {
		t.Errorf("got %q", captured.Embeds[0].Fields[0].Value)
	}
}

func TestSend_NonSuccessResponse_ReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := discord.New(server.URL)
	e := event.Event{Source: "unraid", Type: "array.event", Severity: event.SeverityCritical}

	if err := adapter.Send(e); err == nil {
		t.Fatal("expected error for non-2xx response, got nil")
	}
}
