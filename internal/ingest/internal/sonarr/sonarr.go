// Package sonarr implements core.Parser for Sonarr's webhook payloads.
package sonarr

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ReidMason/relay/internal/event"
	"github.com/ReidMason/relay/internal/ingest/internal/core"
)

// Source is the event.Source this parser produces Events for.
const Source event.Source = "sonarr"

const (
	TypeDownloadCompleted         event.Type = "download.completed"
	TypeDownloadUpgraded          event.Type = "download.upgraded"
	TypeGrab                      event.Type = "grab"
	TypeHealthIssue               event.Type = "health.issue"
	TypeRename                    event.Type = "episode.renamed"
	TypeSeriesAdd                 event.Type = "series.added"
	TypeSeriesDelete              event.Type = "series.deleted"
	TypeEpisodeFileDelete         event.Type = "episode_file.deleted"
	TypeApplicationUpdate         event.Type = "application.updated"
	TypeManualInteractionRequired event.Type = "manual_interaction.required"
)

type webhookPayload struct {
	EventType       string    `json:"eventType"`
	Series          series    `json:"series"`
	Episodes        []episode `json:"episodes"`
	Message         string    `json:"message"`
	Type            string    `json:"type"`
	WikiURL         string    `json:"wikiUrl"`
	IsUpgrade       bool      `json:"isUpgrade"`
	DeleteFiles     bool      `json:"deleteFiles"`
	DeleteReason    string    `json:"deleteReason"`
	PreviousVersion string    `json:"previousVersion"`
	NewVersion      string    `json:"newVersion"`
	DownloadStatus  string    `json:"downloadStatus"`
}

type series struct {
	Title string `json:"title"`
}

type episode struct {
	Title         string `json:"title"`
	SeasonNumber  int    `json:"seasonNumber"`
	EpisodeNumber int    `json:"episodeNumber"`
}

// DownloadData is the flattened Data payload for download.completed,
// download.upgraded, and grab Events.
type DownloadData struct {
	SeriesTitle   string
	EpisodeTitle  string
	SeasonNumber  int
	EpisodeNumber int
}

// HealthIssueData is the flattened Data payload for health.issue Events
// (both a new issue and a HealthRestored Resolution).
type HealthIssueData struct {
	Message string
	Type    string
	WikiURL string
}

// RenameData is the flattened Data payload for episode.renamed Events.
type RenameData struct {
	SeriesTitle string
}

// SeriesData is the flattened Data payload for series.added and
// series.deleted Events.
type SeriesData struct {
	SeriesTitle string
	DeleteFiles bool
}

// EpisodeFileDeleteData is the flattened Data payload for
// episode_file.deleted Events.
type EpisodeFileDeleteData struct {
	SeriesTitle   string
	EpisodeTitle  string
	SeasonNumber  int
	EpisodeNumber int
	Reason        string
}

// ApplicationUpdateData is the flattened Data payload for
// application.updated Events.
type ApplicationUpdateData struct {
	Message         string
	PreviousVersion string
	NewVersion      string
}

// ManualInteractionData is the flattened Data payload for
// manual_interaction.required Events.
type ManualInteractionData struct {
	SeriesTitle    string
	EpisodeTitle   string
	DownloadStatus string
	Message        string
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
		if payload.IsUpgrade {
			return event.New(Source, TypeDownloadUpgraded, event.SeverityInfo, downloadData(payload)), nil
		}
		return event.New(Source, TypeDownloadCompleted, event.SeverityInfo, downloadData(payload)), nil
	case "Grab":
		return event.New(Source, TypeGrab, event.SeverityInfo, downloadData(payload)), nil
	case "Rename":
		return event.New(Source, TypeRename, event.SeverityInfo, RenameData{SeriesTitle: payload.Series.Title}), nil
	case "SeriesAdd":
		return event.New(Source, TypeSeriesAdd, event.SeverityInfo, SeriesData{SeriesTitle: payload.Series.Title}), nil
	case "SeriesDelete":
		return event.New(Source, TypeSeriesDelete, event.SeverityInfo, SeriesData{
			SeriesTitle: payload.Series.Title,
			DeleteFiles: payload.DeleteFiles,
		}), nil
	case "EpisodeFileDelete":
		data := EpisodeFileDeleteData{SeriesTitle: payload.Series.Title, Reason: strings.ToLower(payload.DeleteReason)}
		if len(payload.Episodes) > 0 {
			ep := payload.Episodes[0]
			data.EpisodeTitle = ep.Title
			data.SeasonNumber = ep.SeasonNumber
			data.EpisodeNumber = ep.EpisodeNumber
		}
		return event.New(Source, TypeEpisodeFileDelete, event.SeverityInfo, data), nil
	case "ApplicationUpdate":
		return event.New(Source, TypeApplicationUpdate, event.SeverityInfo, ApplicationUpdateData{
			Message:         payload.Message,
			PreviousVersion: payload.PreviousVersion,
			NewVersion:      payload.NewVersion,
		}), nil
	case "ManualInteractionRequired":
		data := ManualInteractionData{DownloadStatus: payload.DownloadStatus, Message: payload.Message, SeriesTitle: payload.Series.Title}
		if len(payload.Episodes) > 0 {
			data.EpisodeTitle = payload.Episodes[0].Title
		}
		return event.New(Source, TypeManualInteractionRequired, event.SeverityInfo, data), nil
	case "Health":
		return event.New(Source, TypeHealthIssue, event.SeverityWarning, healthIssueData(payload)), nil
	case "HealthRestored":
		return event.NewResolution(Source, TypeHealthIssue, event.SeverityWarning, healthIssueData(payload)), nil
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

func healthIssueData(payload webhookPayload) HealthIssueData {
	return HealthIssueData{
		Message: payload.Message,
		Type:    payload.Type,
		WikiURL: payload.WikiURL,
	}
}
