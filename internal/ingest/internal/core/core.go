// Package core defines ingest's ports (Parser, Publisher) and the Service
// orchestrator. core never imports either transport package — both
// transports import core to implement/call its ports, so Go's import-cycle
// rule enforces the core->adapter direction for free.
package core

import (
	"errors"
	"fmt"

	"github.com/ReidMason/relay/internal/event"
)

// ErrSkip signals a valid webhook that intentionally produces no Event.
// Callers check it via errors.Is. Not currently used by any registered
// Parser — Sonarr's Test webhook used to skip, but now publishes an
// event.NewTest Event instead, since notifier needs an actual Event to
// deliver as proof the pipe works. Kept as a general Parser capability for
// any future case that's genuinely a no-op (unlike a connectivity test).
var ErrSkip = errors.New("skip: no event to publish")

// Parser translates a vendor-shaped webhook payload into a canonical Event.
type Parser interface {
	Parse(raw []byte) (event.Event, error)
}

// Publisher publishes a canonical Event.
type Publisher interface {
	Publish(e event.Event) error
}

// Service looks up the Parser registered for a source, parses the raw
// payload, and publishes the resulting Event.
type Service struct {
	parsers   map[event.Source]Parser
	publisher Publisher
}

// ErrUnknownSource is returned when no Parser is registered for a source.
var ErrUnknownSource = errors.New("unknown source")

// ErrPublish wraps a Publisher failure so transports can distinguish it
// (-> 500) from a Parser failure (-> 400) via errors.Is.
var ErrPublish = errors.New("publish failed")

func NewService(parsers map[event.Source]Parser, publisher Publisher) *Service {
	return &Service{parsers: parsers, publisher: publisher}
}

// Handle looks up the Parser for source, parses raw into an Event, and
// publishes it. Returns ErrUnknownSource if source isn't registered, or
// ErrSkip (wrapped as-is, checkable via errors.Is) if the parser found a
// valid webhook with nothing to publish. source is taken as a raw string
// since it originates as an untrusted URL path segment.
func (s *Service) Handle(source string, raw []byte) error {
	parser, ok := s.parsers[event.Source(source)]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownSource, source)
	}

	e, err := parser.Parse(raw)
	if err != nil {
		return err
	}

	if err := s.publisher.Publish(e); err != nil {
		return fmt.Errorf("%w: %v", ErrPublish, err)
	}

	return nil
}
