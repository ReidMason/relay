# Split `/healthz` into `/livez` and `/readyz`, with a ticking cached dependency check

Status: accepted

Both `ingest` and `notifier` exposed a bare `GET /healthz` that always
returned 200 — useful as a liveness signal (process is up and serving HTTP)
but useless as a readiness signal, since it said nothing about whether the
service could actually do its job (reach NATS JetStream). We split it into
`GET /livez` (unconditional 200, the old `/healthz` behavior, which it
replaces) and `GET /readyz` (200 only if the service's required dependencies
are reachable, 503 with a JSON body naming which check failed otherwise).

`notifier` also depends on Discord webhooks, but that dependency is excluded
from `readyz`. Discord delivery is best-effort and already retried
per-message by the notifier's business logic; a Discord outage shouldn't pull
the pod out of rotation and stop it from consuming/acking NATS messages it
could otherwise process. `readyz` only asks "can I receive and process
events," which for both services reduces to "is NATS reachable."

A naive readyz that pings NATS synchronously on every request doesn't scale:
multiple external callers (k8s kubelet, load balancers, uptime checks) hitting
the endpoint would each trigger a fresh round-trip to NATS, multiplying load
on the dependency with request volume. Instead, a new shared
`internal/health` package runs a `Checker` that pings on a fixed 2-second
ticker (each attempt bounded by a 1-second timeout) and caches the last
result; `readyz` just reads the cache. This decouples check cost from request
volume — one dependency probe per tick, regardless of how many callers hit
the endpoint. Before the first tick completes, the cache holds no result and
`readyz` reports not-ready, so the service never claims readiness it hasn't
verified.

The check itself is an active round trip (`nats.Conn.RTT`/`FlushTimeout`),
not a passive read of the connection's cached status — the goal is to catch a
server that's still holding the TCP socket open but not actually responding.
Each service's `nats.Adapter` gained a `Ping(ctx) error` method, and
`internal/health` depends only on a small `Pinger` interface it defines, not
on `nats.Conn` directly — keeping the nats.go dependency out of the shared
health package.

The `Checker`'s ticker goroutine is started and stopped alongside each
service's own shutdown context, so it never outlives the HTTP server. This
required adding `signal.NotifyContext`-based graceful shutdown to
`ingest/cmd/main.go`, which had none before (`notifier` already had it) —
bringing both services' shutdown handling in line was a prerequisite, not a
separate goal.

No Kubernetes manifests exist in this repo yet, so no probe configuration
needed updating alongside this change.
