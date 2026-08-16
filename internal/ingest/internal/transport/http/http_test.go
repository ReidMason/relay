package http_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ReidMason/relay/internal/event"
	"github.com/ReidMason/relay/internal/ingest/internal/core"
	"github.com/ReidMason/relay/internal/ingest/internal/sonarr"
	transporthttp "github.com/ReidMason/relay/internal/ingest/internal/transport/http"
	"github.com/ReidMason/relay/internal/ingest/internal/unraid"
)

// fakePublisher captures published Events in memory instead of talking to
// NATS, per the project's testing decisions: Publisher is the one seam that
// gets a test double.
type fakePublisher struct {
	mu        sync.Mutex
	published []event.Event
	err       error
}

func (f *fakePublisher) Publish(e event.Event) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published = append(f.published, e)
	return nil
}

func (f *fakePublisher) events() []event.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.published
}

func newTestServer(publisher core.Publisher) *httptest.Server {
	parsers := map[event.Source]core.Parser{
		sonarr.Source: sonarr.New(),
		unraid.Source: unraid.New(),
	}
	service := core.NewService(parsers, publisher)
	handler := transporthttp.NewHandler(service, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	return httptest.NewServer(handler)
}

func postWebhook(t *testing.T, srv *httptest.Server, source, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(srv.URL+"/api/v1/webhooks/"+source, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post webhook: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

const sonarrDownloadPayload = `{
  "eventType": "Download",
  "series": {"title": "The Expanse"},
  "episodes": [{"title": "Dulcinea", "seasonNumber": 1, "episodeNumber": 1}]
}`

const sonarrGrabPayload = `{
  "eventType": "Grab",
  "series": {"title": "The Expanse"},
  "episodes": [{"title": "Dulcinea", "seasonNumber": 1, "episodeNumber": 1}]
}`

const sonarrHealthIssuePayload = `{
  "eventType": "Health",
  "level": "warning",
  "message": "Indexer RSS sync failed",
  "type": "IndexerRssCheck",
  "wikiUrl": "https://wiki.example/health"
}`

const sonarrTestPayload = `{"eventType": "Test"}`

const sonarrUpgradePayload = `{
  "eventType": "Download",
  "isUpgrade": true,
  "series": {"title": "The Expanse"},
  "episodes": [{"title": "Dulcinea", "seasonNumber": 1, "episodeNumber": 1}]
}`

const sonarrRenamePayload = `{
  "eventType": "Rename",
  "series": {"title": "The Expanse"}
}`

const sonarrSeriesAddPayload = `{
  "eventType": "SeriesAdd",
  "series": {"title": "The Expanse"}
}`

const sonarrSeriesDeletePayload = `{
  "eventType": "SeriesDelete",
  "series": {"title": "The Expanse"},
  "deleteFiles": true
}`

const sonarrEpisodeFileDeletePayload = `{
  "eventType": "EpisodeFileDelete",
  "series": {"title": "House of the Dragon"},
  "episodes": [{"title": "The Rogue Prince", "seasonNumber": 1, "episodeNumber": 2}],
  "deleteReason": "manual"
}`

const sonarrApplicationUpdatePayload = `{
  "eventType": "ApplicationUpdate",
  "message": "Sonarr updated",
  "previousVersion": "4.0.0.0",
  "newVersion": "4.0.1.0"
}`

const sonarrManualInteractionRequiredPayload = `{
  "eventType": "ManualInteractionRequired",
  "series": {"title": "The Expanse"},
  "episodes": [{"title": "Dulcinea", "seasonNumber": 1, "episodeNumber": 1}],
  "downloadStatus": "warning",
  "message": "Not a Custom Format upgrade for existing episode file"
}`

const sonarrHealthRestoredPayload = `{
  "eventType": "HealthRestored",
  "level": "warning",
  "message": "Indexer RSS sync failed",
  "type": "IndexerRssCheck",
  "wikiUrl": "https://wiki.example/health"
}`

const unraidAlertPayload = `{
  "embeds": [
    {
      "title": "Disk 1 SMART error",
      "description": "Disk 1 (sdb) has a SMART error",
      "fields": [
        {"name": "Description", "value": "Disk 1 (sdb) has a SMART error"},
        {"name": "Priority", "value": "alert", "inline": true}
      ]
    }
  ]
}`

const unraidWarningPayload = `{
  "embeds": [
    {
      "title": "Array usage high",
      "description": "Array usage is at 92%",
      "fields": [
        {"name": "Description", "value": "Array usage is at 92%"},
        {"name": "Priority", "value": "warning", "inline": true}
      ]
    }
  ]
}`

const unraidNormalPayload = `{
  "embeds": [
    {
      "title": "Array started",
      "description": "The array was started successfully",
      "fields": [
        {"name": "Description", "value": "The array was started successfully"},
        {"name": "Priority", "value": "normal", "inline": true}
      ]
    }
  ]
}`

const unraidResolutionPayload = `{
  "embeds": [
    {
      "title": "Notice [FERN] - Parity disk returned to normal temperature",
      "description": "ST14000NM0121_ZKL2T0X9 (sde)",
      "fields": [
        {"name": "Description", "value": "ST14000NM0121_ZKL2T0X9 (sde)"},
        {"name": "Priority", "value": "normal", "inline": true}
      ]
    }
  ]
}`

func TestSonarrDownload(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "sonarr", sonarrDownloadPayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	e := events[0]
	if e.Source != "sonarr" || e.Type != "download.completed" || e.Severity != event.SeverityInfo {
		t.Fatalf("unexpected event: %+v", e)
	}
	data, ok := e.Data.(sonarr.DownloadData)
	if !ok {
		t.Fatalf("Data type = %T, want sonarr.DownloadData", e.Data)
	}
	want := sonarr.DownloadData{SeriesTitle: "The Expanse", EpisodeTitle: "Dulcinea", SeasonNumber: 1, EpisodeNumber: 1}
	if data != want {
		t.Fatalf("Data = %+v, want %+v", data, want)
	}
}

func TestSonarrGrab(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "sonarr", sonarrGrabPayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	if events[0].Type != "grab" || events[0].Severity != event.SeverityInfo {
		t.Fatalf("unexpected event: %+v", events[0])
	}
}

func TestSonarrHealthIssue(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "sonarr", sonarrHealthIssuePayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	e := events[0]
	if e.Type != "health.issue" || e.Severity != event.SeverityWarning {
		t.Fatalf("unexpected event: %+v", e)
	}
	data, ok := e.Data.(sonarr.HealthIssueData)
	if !ok {
		t.Fatalf("Data type = %T, want sonarr.HealthIssueData", e.Data)
	}
	want := sonarr.HealthIssueData{Message: "Indexer RSS sync failed", Type: "IndexerRssCheck", WikiURL: "https://wiki.example/health"}
	if data != want {
		t.Fatalf("Data = %+v, want %+v", data, want)
	}
}

func TestSonarrTestEventPublished(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "sonarr", sonarrTestPayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	e := events[0]
	if e.Type != "test" || !e.IsTest {
		t.Fatalf("unexpected event: %+v", e)
	}
}

func TestSonarrUpgrade(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "sonarr", sonarrUpgradePayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	e := events[0]
	if e.Type != "download.upgraded" || e.Severity != event.SeverityInfo {
		t.Fatalf("unexpected event: %+v", e)
	}
	data, ok := e.Data.(sonarr.DownloadData)
	if !ok {
		t.Fatalf("Data type = %T, want sonarr.DownloadData", e.Data)
	}
	want := sonarr.DownloadData{SeriesTitle: "The Expanse", EpisodeTitle: "Dulcinea", SeasonNumber: 1, EpisodeNumber: 1}
	if data != want {
		t.Fatalf("Data = %+v, want %+v", data, want)
	}
}

func TestSonarrRename(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "sonarr", sonarrRenamePayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	e := events[0]
	if e.Type != "episode.renamed" || e.Severity != event.SeverityInfo {
		t.Fatalf("unexpected event: %+v", e)
	}
	data, ok := e.Data.(sonarr.RenameData)
	if !ok {
		t.Fatalf("Data type = %T, want sonarr.RenameData", e.Data)
	}
	if data.SeriesTitle != "The Expanse" {
		t.Fatalf("Data = %+v, want SeriesTitle 'The Expanse'", data)
	}
}

func TestSonarrSeriesAdd(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "sonarr", sonarrSeriesAddPayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	e := events[0]
	if e.Type != "series.added" || e.Severity != event.SeverityInfo {
		t.Fatalf("unexpected event: %+v", e)
	}
	data, ok := e.Data.(sonarr.SeriesAddData)
	if !ok {
		t.Fatalf("Data type = %T, want sonarr.SeriesAddData", e.Data)
	}
	want := sonarr.SeriesAddData{SeriesTitle: "The Expanse"}
	if data != want {
		t.Fatalf("Data = %+v, want %+v", data, want)
	}
}

func TestSonarrSeriesDelete(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "sonarr", sonarrSeriesDeletePayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	e := events[0]
	if e.Type != "series.deleted" || e.Severity != event.SeverityInfo {
		t.Fatalf("unexpected event: %+v", e)
	}
	data, ok := e.Data.(sonarr.SeriesDeleteData)
	if !ok {
		t.Fatalf("Data type = %T, want sonarr.SeriesDeleteData", e.Data)
	}
	want := sonarr.SeriesDeleteData{SeriesTitle: "The Expanse", DeleteFiles: true}
	if data != want {
		t.Fatalf("Data = %+v, want %+v", data, want)
	}
}

func TestSonarrEpisodeFileDelete(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "sonarr", sonarrEpisodeFileDeletePayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	e := events[0]
	if e.Type != "episode_file.deleted" || e.Severity != event.SeverityInfo {
		t.Fatalf("unexpected event: %+v", e)
	}
	data, ok := e.Data.(sonarr.EpisodeFileDeleteData)
	if !ok {
		t.Fatalf("Data type = %T, want sonarr.EpisodeFileDeleteData", e.Data)
	}
	want := sonarr.EpisodeFileDeleteData{
		SeriesTitle:   "House of the Dragon",
		EpisodeTitle:  "The Rogue Prince",
		SeasonNumber:  1,
		EpisodeNumber: 2,
		Reason:        "manual",
	}
	if data != want {
		t.Fatalf("Data = %+v, want %+v", data, want)
	}
}

func TestSonarrEpisodeFileDeleteReasonCaseInsensitive(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	payload := `{
	  "eventType": "EpisodeFileDelete",
	  "series": {"title": "The Expanse"},
	  "episodes": [{"title": "Dulcinea", "seasonNumber": 1, "episodeNumber": 1}],
	  "deleteReason": "Upgrade"
	}`
	resp := postWebhook(t, srv, "sonarr", payload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	data, ok := events[0].Data.(sonarr.EpisodeFileDeleteData)
	if !ok || data.Reason != "upgrade" {
		t.Fatalf("Data = %+v, want Reason 'upgrade'", events[0].Data)
	}
}

func TestSonarrApplicationUpdate(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "sonarr", sonarrApplicationUpdatePayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	e := events[0]
	if e.Type != "application.updated" || e.Severity != event.SeverityInfo {
		t.Fatalf("unexpected event: %+v", e)
	}
	data, ok := e.Data.(sonarr.ApplicationUpdateData)
	if !ok {
		t.Fatalf("Data type = %T, want sonarr.ApplicationUpdateData", e.Data)
	}
	want := sonarr.ApplicationUpdateData{Message: "Sonarr updated", PreviousVersion: "4.0.0.0", NewVersion: "4.0.1.0"}
	if data != want {
		t.Fatalf("Data = %+v, want %+v", data, want)
	}
}

func TestSonarrManualInteractionRequired(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "sonarr", sonarrManualInteractionRequiredPayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	e := events[0]
	if e.Type != "manual_interaction.required" || e.Severity != event.SeverityInfo {
		t.Fatalf("unexpected event: %+v", e)
	}
	data, ok := e.Data.(sonarr.ManualInteractionData)
	if !ok {
		t.Fatalf("Data type = %T, want sonarr.ManualInteractionData", e.Data)
	}
	want := sonarr.ManualInteractionData{
		SeriesTitle:    "The Expanse",
		EpisodeTitle:   "Dulcinea",
		DownloadStatus: "warning",
		Message:        "Not a Custom Format upgrade for existing episode file",
	}
	if data != want {
		t.Fatalf("Data = %+v, want %+v", data, want)
	}
}

func TestSonarrHealthRestored(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "sonarr", sonarrHealthRestoredPayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	e := events[0]
	if e.Type != "health.issue" || e.Severity != event.SeverityWarning {
		t.Fatalf("unexpected event: %+v", e)
	}
	if !e.Resolution {
		t.Fatalf("expected HealthRestored to be a Resolution, got %+v", e)
	}
	data, ok := e.Data.(sonarr.HealthIssueData)
	if !ok {
		t.Fatalf("Data type = %T, want sonarr.HealthIssueData", e.Data)
	}
	want := sonarr.HealthIssueData{Message: "Indexer RSS sync failed", Type: "IndexerRssCheck", WikiURL: "https://wiki.example/health"}
	if data != want {
		t.Fatalf("Data = %+v, want %+v", data, want)
	}
}

func TestMalformedJSON(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "sonarr", `{not json`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if len(pub.events()) != 0 {
		t.Fatalf("published %d events, want 0", len(pub.events()))
	}
}

func TestUnrecognizedEventType(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "sonarr", `{"eventType": "Bogus"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if len(pub.events()) != 0 {
		t.Fatalf("published %d events, want 0", len(pub.events()))
	}
}

func TestUnraidAlert(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "unraid", unraidAlertPayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	e := events[0]
	if e.Source != "unraid" || e.Type != "array.event" || e.Severity != event.SeverityCritical {
		t.Fatalf("unexpected event: %+v", e)
	}
	data, ok := e.Data.(unraid.Data)
	if !ok {
		t.Fatalf("Data type = %T, want unraid.Data", e.Data)
	}
	want := unraid.Data{Subject: "Disk 1 SMART error", Description: "Disk 1 (sdb) has a SMART error"}
	if data != want {
		t.Fatalf("Data = %+v, want %+v", data, want)
	}
}

func TestUnraidWarning(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "unraid", unraidWarningPayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	if events[0].Severity != event.SeverityWarning {
		t.Fatalf("unexpected event: %+v", events[0])
	}
}

func TestUnraidNormal(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "unraid", unraidNormalPayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	if events[0].Severity != event.SeverityInfo {
		t.Fatalf("unexpected event: %+v", events[0])
	}
	if events[0].Resolution {
		t.Fatalf("expected routine normal-importance event to not be a Resolution, got %+v", events[0])
	}
}

func TestUnraidResolution(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "unraid", unraidResolutionPayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	e := events[0]
	if e.Severity != event.SeverityInfo {
		t.Fatalf("unexpected severity: %+v", e)
	}
	if !e.Resolution {
		t.Fatalf("expected 'returned to normal temperature' subject to be detected as a Resolution, got %+v", e)
	}
}

const unraidTestPayload = `{
  "embeds": [
    {
      "title": "Discord test.",
      "description": "Discord test.",
      "fields": [
        {"name": "Description", "value": "Discord test."},
        {"name": "Priority", "value": "normal", "inline": true}
      ]
    }
  ]
}`

func TestUnraidTestEventPublished(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "unraid", unraidTestPayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	e := events[0]
	if !e.IsTest {
		t.Fatalf("expected 'Discord test.' title to be detected as a test, got %+v", e)
	}
	if e.Resolution {
		t.Fatalf("test event should not also be a Resolution, got %+v", e)
	}
}

func TestUnraidMalformedJSON(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "unraid", `{not json`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if len(pub.events()) != 0 {
		t.Fatalf("published %d events, want 0", len(pub.events()))
	}
}

func TestUnraidUnrecognizedPriorityDefaultsToInfo(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	payload := `{
	  "embeds": [
	    {
	      "title": "s",
	      "description": "d",
	      "fields": [{"name": "Priority", "value": "critical", "inline": true}]
	    }
	  ]
	}`
	resp := postWebhook(t, srv, "unraid", payload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	if events[0].Severity != event.SeverityInfo {
		t.Fatalf("unexpected event: %+v", events[0])
	}
}

func TestUnraidMissingPriorityDefaultsToInfo(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	payload := `{"embeds": [{"title": "s", "description": "d"}]}`
	resp := postWebhook(t, srv, "unraid", payload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	if events[0].Severity != event.SeverityInfo {
		t.Fatalf("unexpected event: %+v", events[0])
	}
}

func TestUnraidNoEmbeds(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "unraid", `{"embeds": []}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if len(pub.events()) != 0 {
		t.Fatalf("published %d events, want 0", len(pub.events()))
	}
}

func TestUnraidMissingTitle(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "unraid", `{"embeds": [{"description": "d"}]}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if len(pub.events()) != 0 {
		t.Fatalf("published %d events, want 0", len(pub.events()))
	}
}

func TestUnraidMissingDescription(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "unraid", `{"embeds": [{"title": "s"}]}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if len(pub.events()) != 0 {
		t.Fatalf("published %d events, want 0", len(pub.events()))
	}
}

func TestUnraidExtraEmbedsIgnored(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	payload := `{
	  "embeds": [
	    {"title": "first", "description": "first desc", "fields": [{"name": "Priority", "value": "alert"}]},
	    {"title": "second", "description": "second desc", "fields": [{"name": "Priority", "value": "warning"}]}
	  ]
	}`
	resp := postWebhook(t, srv, "unraid", payload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	events := pub.events()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	e := events[0]
	if e.Severity != event.SeverityCritical {
		t.Fatalf("unexpected event: %+v", e)
	}
	data, ok := e.Data.(unraid.Data)
	if !ok || data.Subject != "first" {
		t.Fatalf("Data = %+v, want subject 'first'", e.Data)
	}
}

func TestUnraidPublisherFailure(t *testing.T) {
	pub := &fakePublisher{err: errors.New("nats unavailable")}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "unraid", unraidAlertPayload)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}

func TestUnknownSource(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "radarr", sonarrDownloadPayload)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestPublisherFailure(t *testing.T) {
	pub := &fakePublisher{err: errors.New("nats unavailable")}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "sonarr", sonarrDownloadPayload)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}

func TestLivez(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/livez")
	if err != nil {
		t.Fatalf("get livez: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
