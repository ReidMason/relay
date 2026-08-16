# Version ingest's webhook route as `/api/v1/webhooks/{source}`

Status: accepted

`ingest` served `POST /webhooks/{source}` with no version marker. There was
no concrete breaking change driving a version bump — this is adopting `/v1`
as a REST convention now, while only two vendor configs (Sonarr Connect,
Unraid's notification agent) need repointing, rather than later once more
sources exist.

Versioning is scoped to `/api/v1/webhooks/{source}` only. `GET /livez` and
`GET /readyz` on both `ingest` and `notifier` stay unversioned — per
ADR-0002 they're an operational convention read by infra (kubelet, uptime
checks), not part of the app's request/response contract, so they don't
share the webhook route's versioning lifecycle.

No `/api` prefix beyond the version segment was considered necessary
elsewhere, but `/api/v1` (rather than a bare `/v1`) was chosen so the
version segment reads unambiguously as part of an API path if this service
ever gains a second, non-API surface.

The old `/webhooks/{source}` route was replaced outright, not dual-served
alongside the new one. There's no automated client to break a migration
window for — Sonarr and Unraid are manually configured by the same person
deploying this change — so a temporary alias would only be dead code to
remember to delete later.
