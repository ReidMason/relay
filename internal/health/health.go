// Package health provides a ticking, cached readiness check: a Checker pings
// a dependency on a fixed interval and caches the result, so readyz reads
// from cache instead of triggering a fresh dependency call per request. See
// ADR-0002.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Pinger is a dependency that can be actively probed.
type Pinger interface {
	Ping(ctx context.Context) error
}

// result is the outcome of the most recent ping.
type result struct {
	err error
}

// Checker pings a Pinger on a fixed interval and caches the last result.
type Checker struct {
	pinger   Pinger
	interval time.Duration
	timeout  time.Duration

	mu     sync.RWMutex
	result *result // nil until the first check completes
}

// NewChecker builds a Checker. Call Run to start ticking.
func NewChecker(pinger Pinger, interval, timeout time.Duration) *Checker {
	return &Checker{pinger: pinger, interval: interval, timeout: timeout}
}

// Run ticks the checker every interval until ctx is cancelled, pinging once
// immediately on entry so a result is cached as soon as possible.
func (c *Checker) Run(ctx context.Context) {
	c.check(ctx)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.check(ctx)
		}
	}
}

func (c *Checker) check(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	err := c.pinger.Ping(ctx)

	c.mu.Lock()
	c.result = &result{err: err}
	c.mu.Unlock()
}

// ready reports the cached result. ok is false if no check has completed
// yet, in which case the dependency isn't considered ready.
func (c *Checker) ready() (err error, checked bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.result == nil {
		return nil, false
	}
	return c.result.err, true
}

// readyzResponse is the JSON body served by ReadyzHandler.
type readyzResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// ReadyzHandler serves readiness as JSON, 200 if every named Checker's
// cached result is a successful ping, 503 otherwise (including before any
// checker has completed its first ping).
func ReadyzHandler(checks map[string]*Checker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := readyzResponse{Status: "ok", Checks: map[string]string{}}
		ready := true

		for name, c := range checks {
			err, checked := c.ready()
			switch {
			case !checked:
				ready = false
				resp.Checks[name] = "pending"
			case err != nil:
				ready = false
				resp.Checks[name] = "error: " + err.Error()
			default:
				resp.Checks[name] = "ok"
			}
		}

		if !ready {
			resp.Status = "unavailable"
		}

		w.Header().Set("Content-Type", "application/json")
		if !ready {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// LivezHandler unconditionally reports the process as alive.
func LivezHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
