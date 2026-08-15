// Command ingest runs the ingest HTTP service: it receives vendor webhooks,
// translates them into Events, and publishes them to NATS JetStream.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/ReidMason/relay/internal/event"
	"github.com/ReidMason/relay/internal/ingest/internal/core"
	"github.com/ReidMason/relay/internal/ingest/internal/sonarr"
	transporthttp "github.com/ReidMason/relay/internal/ingest/internal/transport/http"
	transportnats "github.com/ReidMason/relay/internal/ingest/internal/transport/nats"
	"github.com/ReidMason/relay/internal/ingest/internal/unraid"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	natsURL := envOrDefault("NATS_URL", "nats://127.0.0.1:4222")
	port := envOrDefault("PORT", "8080")

	publisher, err := transportnats.Connect(context.Background(), natsURL)
	if err != nil {
		logger.Error("connect to nats", "error", err)
		os.Exit(1)
	}

	parsers := map[event.Source]core.Parser{
		sonarr.Source: sonarr.New(),
		unraid.Source: unraid.New(),
	}

	service := core.NewService(parsers, publisher)
	handler := transporthttp.NewHandler(service, logger)

	addr := ":" + port
	logger.Info("starting ingest", "addr", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
