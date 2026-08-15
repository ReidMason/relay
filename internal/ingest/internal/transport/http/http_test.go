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
	parsers := map[string]core.Parser{
		"sonarr": sonarr.New(),
		"unraid": unraid.New(),
	}
	service := core.NewService(parsers, publisher)
	handler := transporthttp.NewHandler(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return httptest.NewServer(handler)
}

func postWebhook(t *testing.T, srv *httptest.Server, source, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(srv.URL+"/webhooks/"+source, "application/json", strings.NewReader(body))
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
  "eventType": "HealthIssue",
  "level": "warning",
  "message": "Indexer RSS sync failed",
  "type": "IndexerRssCheck",
  "wikiUrl": "https://wiki.example/health"
}`

const sonarrTestPayload = `{"eventType": "Test"}`

const unraidAlertPayload = `{
  "importance": "alert",
  "subject": "Disk 1 SMART error",
  "description": "Disk 1 (sdb) has a SMART error"
}`

const unraidWarningPayload = `{
  "importance": "warning",
  "subject": "Array usage high",
  "description": "Array usage is at 92%"
}`

const unraidNormalPayload = `{
  "importance": "normal",
  "subject": "Array started",
  "description": "The array was started successfully"
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

func TestSonarrTestEventSkipped(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "sonarr", sonarrTestPayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(pub.events()) != 0 {
		t.Fatalf("published %d events, want 0", len(pub.events()))
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

	resp := postWebhook(t, srv, "sonarr", `{"eventType": "SeriesAdd"}`)
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

func TestUnraidUnrecognizedImportance(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp := postWebhook(t, srv, "unraid", `{"importance": "critical", "subject": "s", "description": "d"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if len(pub.events()) != 0 {
		t.Fatalf("published %d events, want 0", len(pub.events()))
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

func TestHealthz(t *testing.T) {
	pub := &fakePublisher{}
	srv := newTestServer(pub)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("get healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
