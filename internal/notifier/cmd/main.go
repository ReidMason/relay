// Command notifier runs the notifier service: it durably consumes Events
// from NATS JetStream and delivers matching Events to registered Channels.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/ReidMason/relay/internal/event"
	"github.com/ReidMason/relay/internal/notifier/internal/core"
	"github.com/ReidMason/relay/internal/notifier/internal/transport/discord"
	transportnats "github.com/ReidMason/relay/internal/notifier/internal/transport/nats"
)

// unraid.Source and sonarr.Source live under internal/ingest/internal/... and
// are structurally unimportable here by design (ADR-0001), so their values
// are duplicated as local consts.
const (
	sourceUnraid event.Source = "unraid"
	sourceSonarr event.Source = "sonarr"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	natsURL := envOrDefault("NATS_URL", "nats://127.0.0.1:4222")
	discordWebhookURL := os.Getenv("DISCORD_WEBHOOK_URL")
	port := envOrDefault("PORT", "8080")

	if discordWebhookURL == "" {
		logger.Error("DISCORD_WEBHOOK_URL is required")
		os.Exit(1)
	}

	routes := []core.Route{
		{Source: sourceUnraid, MinSeverity: event.SeverityWarning, Channels: []core.ChannelName{core.ChannelDiscord}},
		{Source: sourceSonarr, MinSeverity: event.SeverityCritical, Channels: []core.ChannelName{core.ChannelDiscord}},
	}
	channels := map[core.ChannelName]core.Channel{
		core.ChannelDiscord: discord.New(discordWebhookURL),
	}
	service := core.NewService(routes, channels)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	consumer, err := transportnats.Connect(ctx, natsURL, service, logger)
	if err != nil {
		logger.Error("connect to nats", "error", err)
		os.Exit(1)
	}

	go func() {
		if err := consumer.Run(ctx); err != nil {
			logger.Error("nats consumer stopped", "error", err)
			os.Exit(1)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	addr := ":" + port
	logger.Info("starting notifier", "addr", addr)
	server := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-ctx.Done()
		server.Close()
	}()
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
