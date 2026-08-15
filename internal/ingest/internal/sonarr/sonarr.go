// Package sonarr implements core.Parser for Sonarr's webhook payloads.
package sonarr

import (
	"encoding/json"
	"fmt"

	"github.com/ReidMason/relay/internal/event"
	"github.com/ReidMason/relay/internal/ingest/internal/core"
)

// Source is the event.Source this parser produces Events for.
const Source event.Source = "sonarr"

const (
	TypeDownloadCompleted event.Type = "download.completed"
	TypeGrab              event.Type = "grab"
	TypeHealthIssue       event.Type = "health.issue"
)

type webhookPayload struct {
	EventType string    `json:"eventType"`
	Series    series    `json:"series"`
	Episodes  []episode `json:"episodes"`
	Message   string    `json:"message"`
	Type      string    `json:"type"`
	WikiURL   string    `json:"wikiUrl"`
}

type series struct {
	Title string `json:"title"`
}

type episode struct {
	Title         string `json:"title"`
	SeasonNumber  int    `json:"seasonNumber"`
	EpisodeNumber int    `json:"episodeNumber"`
}

// DownloadData is the flattened Data payload for download.completed and
// grab Events.
type DownloadData struct {
	SeriesTitle   string
	EpisodeTitle  string
	SeasonNumber  int
	EpisodeNumber int
}

// HealthIssueData is the flattened Data payload for health.issue Events.
type HealthIssueData struct {
	Message string
	Type    string
	WikiURL string
}

// Parser implements core.Parser for Sonarr webhook payloads.
type Parser struct{}

func New() Parser {
	return Parser{}
}

func (Parser) Parse(raw []byte) (event.Event, error) {
	var payload webhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return event.Event{}, fmt.Errorf("sonarr: parse payload: %w", err)
	}

	switch payload.EventType {
	case "Download":
		return event.New(Source, TypeDownloadCompleted, event.SeverityInfo, downloadData(payload)), nil
	case "Grab":
		return event.New(Source, TypeGrab, event.SeverityInfo, downloadData(payload)), nil
	case "HealthIssue":
		return event.New(Source, TypeHealthIssue, event.SeverityWarning, HealthIssueData{
			Message: payload.Message,
			Type:    payload.Type,
			WikiURL: payload.WikiURL,
		}), nil
	case "Test":
		return event.Event{}, core.ErrSkip
	default:
		return event.Event{}, fmt.Errorf("sonarr: unrecognized eventType %q", payload.EventType)
	}
}

func downloadData(payload webhookPayload) DownloadData {
	data := DownloadData{SeriesTitle: payload.Series.Title}
	if len(payload.Episodes) > 0 {
		ep := payload.Episodes[0]
		data.EpisodeTitle = ep.Title
		data.SeasonNumber = ep.SeasonNumber
		data.EpisodeNumber = ep.EpisodeNumber
	}
	return data
}
