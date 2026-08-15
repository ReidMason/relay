package core_test

import (
	"errors"
	"testing"

	"github.com/ReidMason/relay/internal/event"
	"github.com/ReidMason/relay/internal/notifier/internal/core"
)

type fakeChannel struct {
	sent []event.Event
	err  error
}

func (f *fakeChannel) Send(e event.Event) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, e)
	return nil
}

const (
	sourceUnraid event.Source = "unraid"
	sourceSonarr event.Source = "sonarr"
)

func routesFixture() []core.Route {
	return []core.Route{
		{Source: sourceUnraid, MinSeverity: event.SeverityWarning, Channels: []core.ChannelName{core.ChannelDiscord}},
		{Source: sourceSonarr, MinSeverity: event.SeverityCritical, Channels: []core.ChannelName{core.ChannelDiscord}},
	}
}

func TestHandle_RoutesAboveFloor(t *testing.T) {
	discord := &fakeChannel{}
	svc := core.NewService(routesFixture(), map[core.ChannelName]core.Channel{core.ChannelDiscord: discord})

	e := event.New(sourceUnraid, "array.event", event.SeverityWarning, nil)
	if err := svc.Handle(e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(discord.sent) != 1 {
		t.Fatalf("expected 1 sent event, got %d", len(discord.sent))
	}
}

func TestHandle_BelowFloor_NotSent(t *testing.T) {
	discord := &fakeChannel{}
	svc := core.NewService(routesFixture(), map[core.ChannelName]core.Channel{core.ChannelDiscord: discord})

	e := event.New(sourceUnraid, "array.event", event.SeverityInfo, nil)
	if err := svc.Handle(e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(discord.sent) != 0 {
		t.Fatalf("expected 0 sent events, got %d", len(discord.sent))
	}
}

func TestHandle_SonarrOnlyCritical(t *testing.T) {
	discord := &fakeChannel{}
	svc := core.NewService(routesFixture(), map[core.ChannelName]core.Channel{core.ChannelDiscord: discord})

	for _, sev := range []event.Severity{event.SeverityInfo, event.SeverityWarning} {
		e := event.New(sourceSonarr, "grab", sev, nil)
		if err := svc.Handle(e); err != nil {
			t.Fatalf("Handle: %v", err)
		}
	}
	if len(discord.sent) != 0 {
		t.Fatalf("expected 0 sent events below critical, got %d", len(discord.sent))
	}

	e := event.New(sourceSonarr, "grab", event.SeverityCritical, nil)
	if err := svc.Handle(e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(discord.sent) != 1 {
		t.Fatalf("expected 1 sent event at critical, got %d", len(discord.sent))
	}
}

func TestHandle_UnmatchedSource_NotSent(t *testing.T) {
	discord := &fakeChannel{}
	svc := core.NewService(routesFixture(), map[core.ChannelName]core.Channel{core.ChannelDiscord: discord})

	e := event.New("unknown-source", "whatever", event.SeverityCritical, nil)
	if err := svc.Handle(e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(discord.sent) != 0 {
		t.Fatalf("expected 0 sent events, got %d", len(discord.sent))
	}
}

func TestHandle_ChannelSendFailure_PropagatesError(t *testing.T) {
	sendErr := errors.New("discord unavailable")
	discord := &fakeChannel{err: sendErr}
	svc := core.NewService(routesFixture(), map[core.ChannelName]core.Channel{core.ChannelDiscord: discord})

	e := event.New(sourceUnraid, "array.event", event.SeverityCritical, nil)
	if err := svc.Handle(e); !errors.Is(err, sendErr) {
		t.Fatalf("expected error to wrap %v, got %v", sendErr, err)
	}
}
