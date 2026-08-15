// Package core defines notifier's ports (Channel) and the Service
// orchestrator. core never imports either transport package — both
// transports import core to implement/call its ports, so Go's import-cycle
// rule enforces the core->adapter direction for free.
package core

import (
	"errors"
	"fmt"

	"github.com/ReidMason/relay/internal/event"
)

// Channel is a pluggable delivery mechanism for an Event, e.g. Discord.
type Channel interface {
	Send(e event.Event) error
}

// ChannelName identifies a registered Channel.
type ChannelName string

const ChannelDiscord ChannelName = "discord"

var severityRank = map[event.Severity]int{
	event.SeverityInfo:     0,
	event.SeverityWarning:  1,
	event.SeverityCritical: 2,
}

// Route declares that Events from Source at or above MinSeverity should be
// sent to Channels. Routing is a plain declarative lookup on the canonical
// Source/Severity fields — not semantic branching on vendor payload meaning,
// which stays in ingest (see CONTEXT.md's Source entry).
type Route struct {
	Source      event.Source
	MinSeverity event.Severity
	Channels    []ChannelName
}

// Service routes each Event through routes and delivers it to every matching
// Channel.
type Service struct {
	routes   []Route
	channels map[ChannelName]Channel
}

func NewService(routes []Route, channels map[ChannelName]Channel) *Service {
	return &Service{routes: routes, channels: channels}
}

// Handle sends e to every Channel named by a Route whose Source matches
// e.Source, and whose MinSeverity is at or below e.Severity or which is a
// Resolution (see CONTEXT.md) — a Resolution always bypasses MinSeverity,
// since it's inherently notify-worthy whenever its Source is routed at all.
// Every matched Channel is attempted even if an earlier one fails; a non-nil
// return means at least one Send failed, so callers (e.g. the NATS adapter)
// can decide whether to redeliver.
func (s *Service) Handle(e event.Event) error {
	var errs []error
	for _, route := range s.routes {
		if route.Source != e.Source {
			continue
		}
		if !e.Resolution && severityRank[e.Severity] < severityRank[route.MinSeverity] {
			continue
		}
		for _, name := range route.Channels {
			channel, ok := s.channels[name]
			if !ok {
				continue
			}
			if err := channel.Send(e); err != nil {
				errs = append(errs, fmt.Errorf("core: send via %s: %w", name, err))
			}
		}
	}
	return errors.Join(errs...)
}
