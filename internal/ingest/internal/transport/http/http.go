// Package http is the HTTP transport adapter for ingest: it wires
// POST /webhooks/{source}, GET /livez, and GET /readyz onto core.Service.
package http

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/ReidMason/relay/internal/health"
	"github.com/ReidMason/relay/internal/ingest/internal/core"
)

// Handler is the http.Handler serving ingest's routes.
type Handler struct {
	mux *http.ServeMux
}

func NewHandler(service *core.Service, logger *slog.Logger, checks map[string]*health.Checker) *Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /webhooks/{source}", handleWebhook(service, logger))
	mux.HandleFunc("GET /livez", health.LivezHandler)
	mux.HandleFunc("GET /readyz", health.ReadyzHandler(checks))

	return &Handler{mux: mux}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func handleWebhook(service *core.Service, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		source := r.PathValue("source")

		raw, err := io.ReadAll(r.Body)
		if err != nil {
			logger.Error("read webhook body", "source", source, "error", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		logger.Info("received webhook", "source", source, "body", string(raw))

		err = service.Handle(source, raw)
		switch {
		case err == nil:
			logger.Info("published event", "source", source)
			w.WriteHeader(http.StatusOK)
		case errors.Is(err, core.ErrSkip):
			logger.Info("skipped webhook", "source", source)
			w.WriteHeader(http.StatusOK)
		case errors.Is(err, core.ErrUnknownSource):
			logger.Warn("unknown source", "source", source)
			w.WriteHeader(http.StatusNotFound)
		case errors.Is(err, core.ErrPublish):
			logger.Error("publish failed", "source", source, "error", err)
			w.WriteHeader(http.StatusInternalServerError)
		default:
			logger.Warn("parse failed", "source", source, "error", err)
			w.WriteHeader(http.StatusBadRequest)
		}
	}
}
