// Package nats is the NATS JetStream transport adapter: it durably consumes
// Events from homelab.> and hands each to an EventHandler (core.Service).
package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/ReidMason/relay/internal/event"
)

const (
	streamName     = "HOMELAB_EVENTS"
	streamSubject  = "homelab.>"
	consumerName   = "notifier"
	consumerFilter = "homelab.>"
	maxDeliver     = 5
	ackWait        = 10 * time.Second
	fetchBatch     = 10
	fetchWait      = 5 * time.Second
)

// EventHandler processes a decoded Event. A non-nil error means the message
// should not be acked, so JetStream redelivers it (up to maxDeliver).
type EventHandler interface {
	Handle(e event.Event) error
}

// Adapter consumes Events from the HOMELAB_EVENTS stream via a durable pull
// consumer shared by all notifier replicas (JetStream distributes messages
// across concurrent pullers on the same durable, so running multiple
// replicas needs no extra configuration).
type Adapter struct {
	consumer jetstream.Consumer
	handler  EventHandler
	logger   *slog.Logger
}

// Connect dials natsURL, idempotently ensures the HOMELAB_EVENTS stream and
// the "notifier" durable consumer exist, and returns an Adapter ready to
// Run. A nil logger is replaced with a discard logger.
func Connect(ctx context.Context, natsURL string, handler EventHandler, logger *slog.Logger) (*Adapter, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	nc, err := nats.Connect(natsURL)
	if err != nil {
		return nil, fmt.Errorf("nats: connect: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("nats: init jetstream: %w", err)
	}

	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{streamSubject},
	})
	if err != nil {
		return nil, fmt.Errorf("nats: ensure stream %s: %w", streamName, err)
	}

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       consumerName,
		FilterSubject: consumerFilter,
		DeliverPolicy: jetstream.DeliverNewPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    maxDeliver,
		AckWait:       ackWait,
	})
	if err != nil {
		return nil, fmt.Errorf("nats: ensure consumer %s: %w", consumerName, err)
	}

	return &Adapter{consumer: consumer, handler: handler, logger: logger}, nil
}

// Run pulls messages in a loop until ctx is cancelled. Each message is
// decoded into an Event and passed to the handler; on success the message
// is acked, on failure it's left unacked for JetStream to redeliver.
func (a *Adapter) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		msgs, err := a.consumer.Fetch(fetchBatch, jetstream.FetchMaxWait(fetchWait))
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, nats.ErrTimeout) {
				continue
			}
			return fmt.Errorf("nats: fetch: %w", err)
		}

		for msg := range msgs.Messages() {
			a.process(msg)
		}
		if err := msgs.Error(); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("nats: fetch: %w", err)
		}
	}
}

func (a *Adapter) process(msg jetstream.Msg) {
	var e event.Event
	if err := json.Unmarshal(msg.Data(), &e); err != nil {
		a.logger.Error("malformed event on bus, terminating message", "error", err)
		_ = msg.Term()
		return
	}

	if err := a.handler.Handle(e); err != nil {
		meta, metaErr := msg.Metadata()
		if metaErr == nil && meta.NumDelivered >= maxDeliver {
			a.logger.Error("redelivery exhausted, giving up on event",
				"source", e.Source, "type", e.Type, "deliveries", meta.NumDelivered, "error", err)
			_ = msg.Term()
			return
		}
		a.logger.Warn("handle event failed, will redeliver", "source", e.Source, "type", e.Type, "error", err)
		return
	}

	if err := msg.Ack(); err != nil {
		a.logger.Error("ack failed", "source", e.Source, "type", e.Type, "error", err)
	}
}
