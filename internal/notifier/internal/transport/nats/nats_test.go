package nats_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/ReidMason/relay/internal/event"
	"github.com/ReidMason/relay/internal/notifier/internal/core"
	"github.com/ReidMason/relay/internal/notifier/internal/transport/discord"
	transportnats "github.com/ReidMason/relay/internal/notifier/internal/transport/nats"
)

// fakeHandler captures every Event handed to it in memory, standing in for
// core.Service in these transport-level tests.
type fakeHandler struct {
	mu      sync.Mutex
	handled []event.Event
	err     error
}

func (f *fakeHandler) Handle(e event.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.handled = append(f.handled, e)
	return nil
}

func (f *fakeHandler) events() []event.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]event.Event, len(f.handled))
	copy(out, f.handled)
	return out
}

func startEmbeddedNATS(t *testing.T) string {
	t.Helper()
	opts := &natsserver.Options{
		Host:      "127.0.0.1",
		Port:      -1,
		JetStream: true,
		StoreDir:  t.TempDir(),
	}
	srv, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("start embedded nats: %v", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("embedded nats not ready in time")
	}
	t.Cleanup(srv.Shutdown)
	return srv.ClientURL()
}

func publishRaw(t *testing.T, url, subject string, e event.Event) {
	t.Helper()
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	if _, err := js.CreateOrUpdateStream(context.Background(), jetstream.StreamConfig{
		Name:     "HOMELAB_EVENTS",
		Subjects: []string{"homelab.>"},
	}); err != nil {
		t.Fatalf("create stream: %v", err)
	}

	payload, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if _, err := js.Publish(context.Background(), subject, payload); err != nil {
		t.Fatalf("publish: %v", err)
	}
}

func eventuallyLen(t *testing.T, get func() []event.Event, want int, timeout time.Duration) []event.Event {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		got := get()
		if len(got) >= want {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d events, got %d", want, len(got))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestConsume_DeliversPublishedEventToHandler(t *testing.T) {
	url := startEmbeddedNATS(t)
	handler := &fakeHandler{}

	adapter, err := transportnats.Connect(context.Background(), url, handler, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.Run(ctx)

	e := event.New("unraid", "array.event", event.SeverityCritical, map[string]any{"Subject": "disk down"})
	publishRaw(t, url, "homelab.unraid.array.event", e)

	got := eventuallyLen(t, handler.events, 1, 5*time.Second)
	if got[0].Source != "unraid" || got[0].Type != "array.event" {
		t.Errorf("unexpected event: %+v", got[0])
	}
}

func TestConsume_HandlerFailure_MessageRedelivered(t *testing.T) {
	url := startEmbeddedNATS(t)
	handler := &fakeHandler{err: context.DeadlineExceeded}

	adapter, err := transportnats.Connect(context.Background(), url, handler, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.Run(ctx)

	e := event.New("unraid", "array.event", event.SeverityCritical, nil)
	publishRaw(t, url, "homelab.unraid.array.event", e)

	// Handler always errors, so nothing ever gets acked and JetStream
	// redelivers repeatedly. Assert on repeated delivery, not on any
	// specific message getting through, as a proxy for "not silently acked".
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if len(handler.events()) != 0 {
		t.Fatalf("handler always errors, expected 0 successfully-handled events, got %d", len(handler.events()))
	}
}

// endToEndRoutes mirrors the routing table shape main.go wires. These tests
// exercise the full stack — real transport/nats consumer -> real
// core.Service -> real discord.Adapter — with only the Discord webhook
// itself faked out, per the spec's testing decisions.
func endToEndRoutes() []core.Route {
	return []core.Route{
		{Source: "unraid", MinSeverity: event.SeverityWarning, Channels: []core.ChannelName{core.ChannelDiscord}},
		{Source: "sonarr", MinSeverity: event.SeverityCritical, Channels: []core.ChannelName{core.ChannelDiscord}},
	}
}

func TestEndToEnd_BelowFloor_NoDiscordRequest(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	discordServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer discordServer.Close()

	channels := map[core.ChannelName]core.Channel{core.ChannelDiscord: discord.New(discordServer.URL)}
	service := core.NewService(endToEndRoutes(), channels)

	url := startEmbeddedNATS(t)
	adapter, err := transportnats.Connect(context.Background(), url, service, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.Run(ctx)

	e := event.New("unraid", "array.event", event.SeverityInfo, map[string]any{"Subject": "routine scrub"})
	publishRaw(t, url, "homelab.unraid.array.event", e)

	// Info is below Unraid's warning floor: give the consumer time to have
	// processed it, then assert Discord never got a request.
	time.Sleep(500 * time.Millisecond)
	mu.Lock()
	got := requests
	mu.Unlock()
	if got != 0 {
		t.Fatalf("expected 0 discord requests for below-floor event, got %d", got)
	}
}

func TestEndToEnd_AboveFloor_DeliversHumanReadableEmbedToDiscord(t *testing.T) {
	var mu sync.Mutex
	var lastBody map[string]any
	requests := 0
	discordServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		json.NewDecoder(r.Body).Decode(&lastBody)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer discordServer.Close()

	channels := map[core.ChannelName]core.Channel{core.ChannelDiscord: discord.New(discordServer.URL)}
	service := core.NewService(endToEndRoutes(), channels)

	url := startEmbeddedNATS(t)
	adapter, err := transportnats.Connect(context.Background(), url, service, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.Run(ctx)

	// Unraid's floor is `warning` — publish exactly at the boundary (not
	// `critical`) to prove the boundary itself is inclusive end-to-end, with
	// a multi-key Data payload to verify field formatting/ordering survive
	// the full NATS -> core -> Discord path, not just the discord package in
	// isolation.
	e := event.New("unraid", "array.event", event.SeverityWarning, map[string]any{
		"Subject":     "disk failure",
		"Description": "disk1 SMART error",
	})
	publishRaw(t, url, "homelab.unraid.array.event", e)

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		got := requests
		mu.Unlock()
		if got >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for discord to receive a request")
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	embeds, _ := lastBody["embeds"].([]any)
	if len(embeds) != 1 {
		t.Fatalf("expected 1 embed, got body: %+v", lastBody)
	}
	emb := embeds[0].(map[string]any)
	if emb["title"] != "array.event" {
		t.Errorf("title = %v, want array.event", emb["title"])
	}

	fields, _ := emb["fields"].([]any)
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %+v", fields)
	}
	// Sorted alphabetically by formatted key: "Description" before "Subject".
	f0 := fields[0].(map[string]any)
	f1 := fields[1].(map[string]any)
	if f0["name"] != "Description" || f0["value"] != "disk1 SMART error" {
		t.Errorf("field[0] = %+v, want Description/disk1 SMART error", f0)
	}
	if f1["name"] != "Subject" || f1["value"] != "disk failure" {
		t.Errorf("field[1] = %+v, want Subject/disk failure", f1)
	}
}

func TestEndToEnd_SonarrRouting_OnlyCriticalDelivered(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	discordServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer discordServer.Close()

	channels := map[core.ChannelName]core.Channel{core.ChannelDiscord: discord.New(discordServer.URL)}
	service := core.NewService(endToEndRoutes(), channels)

	url := startEmbeddedNATS(t)
	adapter, err := transportnats.Connect(context.Background(), url, service, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.Run(ctx)

	for _, sev := range []event.Severity{event.SeverityInfo, event.SeverityWarning} {
		e := event.New("sonarr", "grab", sev, nil)
		publishRaw(t, url, "homelab.sonarr.grab", e)
	}
	// Sonarr's floor is `critical` — info/warning should never reach Discord.
	time.Sleep(500 * time.Millisecond)
	mu.Lock()
	got := requests
	mu.Unlock()
	if got != 0 {
		t.Fatalf("expected 0 discord requests for below-floor sonarr events, got %d", got)
	}

	e := event.New("sonarr", "grab", event.SeverityCritical, nil)
	publishRaw(t, url, "homelab.sonarr.grab", e)

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		got := requests
		mu.Unlock()
		if got >= 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for critical sonarr event to reach discord, got %d requests", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestEndToEnd_DiscordFailure_MessageRedelivered(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	discordServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer discordServer.Close()

	channels := map[core.ChannelName]core.Channel{core.ChannelDiscord: discord.New(discordServer.URL)}
	service := core.NewService(endToEndRoutes(), channels)

	url := startEmbeddedNATS(t)
	adapter, err := transportnats.Connect(context.Background(), url, service, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go adapter.Run(ctx)

	e := event.New("unraid", "array.event", event.SeverityCritical, nil)
	publishRaw(t, url, "homelab.unraid.array.event", e)

	// Discord always 500s, so the message is never acked and JetStream keeps
	// redelivering it — assert Discord sees more than one request, proving
	// the failure left the message unacked rather than silently swallowed.
	// Must exceed the consumer's AckWait for a redelivery to occur.
	deadline := time.Now().Add(15 * time.Second)
	for {
		mu.Lock()
		got := requests
		mu.Unlock()
		if got >= 2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected redelivery (>=2 discord requests), got %d", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
