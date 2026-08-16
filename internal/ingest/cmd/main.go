// Command ingest runs the ingest HTTP service: it receives vendor webhooks,
// translates them into Events, and publishes them to NATS JetStream.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ReidMason/relay/internal/event"
	"github.com/ReidMason/relay/internal/health"
	"github.com/ReidMason/relay/internal/ingest/internal/core"
	"github.com/ReidMason/relay/internal/ingest/internal/sonarr"
	transporthttp "github.com/ReidMason/relay/internal/ingest/internal/transport/http"
	transportnats "github.com/ReidMason/relay/internal/ingest/internal/transport/nats"
	"github.com/ReidMason/relay/internal/ingest/internal/unraid"
)

const (
	healthCheckInterval = 2 * time.Second
	healthCheckTimeout  = 1 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	natsURL := envOrDefault("NATS_URL", "nats://127.0.0.1:4222")
	port := envOrDefault("PORT", "8080")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	publisher, err := transportnats.Connect(ctx, natsURL)
	if err != nil {
		logger.Error("connect to nats", "error", err)
		os.Exit(1)
	}

	parsers := map[event.Source]core.Parser{
		sonarr.Source: sonarr.New(),
		unraid.Source: unraid.New(),
	}

	service := core.NewService(parsers, publisher)

	natsChecker := health.NewChecker(publisher, healthCheckInterval, healthCheckTimeout)
	go natsChecker.Run(ctx)

	handler := transporthttp.NewHandler(service, logger, map[string]*health.Checker{"nats": natsChecker})

	addr := ":" + port
	server := &http.Server{Addr: addr, Handler: handler}
	go func() {
		<-ctx.Done()
		server.Close()
	}()

	logger.Info("starting ingest", "addr", addr)
	if err := server.ListenAndServe(); err != nil && ctx.Err() == nil {
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
