// Package nats is the NATS JetStream transport adapter implementing
// core.Publisher.
package nats

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/ReidMason/relay/internal/event"
)

const (
	streamName    = "HOMELAB_EVENTS"
	streamSubject = "homelab.>"
)

// Adapter implements core.Publisher with a synchronous JetStream publish:
// it blocks for the broker's ack, so a publish failure propagates as an
// error rather than being silently dropped.
type Adapter struct {
	js jetstream.JetStream
}

// Connect dials natsURL and idempotently ensures the HOMELAB_EVENTS stream
// (subjects homelab.>) exists.
func Connect(ctx context.Context, natsURL string) (*Adapter, error) {
	nc, err := nats.Connect(natsURL)
	if err != nil {
		return nil, fmt.Errorf("nats: connect: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("nats: init jetstream: %w", err)
	}

	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{streamSubject},
	})
	if err != nil {
		return nil, fmt.Errorf("nats: ensure stream %s: %w", streamName, err)
	}

	return &Adapter{js: js}, nil
}

// Publish blocks for the JetStream ack; a failure is returned as an error.
func (a *Adapter) Publish(e event.Event) error {
	payload, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("nats: marshal event: %w", err)
	}

	subject := event.Subject(e.Source, e.Type)
	if _, err := a.js.Publish(context.Background(), subject, payload); err != nil {
		return fmt.Errorf("nats: publish to %s: %w", subject, err)
	}

	return nil
}
