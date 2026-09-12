# Management API v1

RequestInspector Relay exposes management routes only on the management listener under `/api/v1`. Unknown `/api/` routes return JSON `404` responses and never fall through to the embedded UI.

Version 1 field names, routes, event names, and semantics are frozen for frontend implementation. Breaking changes require a new API version. Frontend-consumable typed examples live in [`api-v1-fixtures`](api-v1-fixtures/), protected by Go contract tests.

## Common behavior

- JSON responses use `application/json`.
- Errors use `{"error":{"code":"...","message":"..."}}`.
- Captured bytes in JSON summaries and SSE are base64-encoded by standard JSON byte encoding. HTTP body and UDP datagram endpoints return authoritative raw bytes.
- UDP source addresses, ports, metadata, and datagram bodies use the same authenticated routes as HTTP captures; operators must apply the retention and access controls in [UDP security and operations](udp-security.md).
- Body responses use `application/octet-stream`, `Content-Disposition: attachment`, `X-Content-Type-Options: nosniff`, and `Accept-Ranges: bytes`.
- Only one `bytes` range is accepted. Invalid or unsatisfiable ranges return `416` with `Content-Range: bytes */SIZE`.
- List cursors are opaque completion-time/ID boundaries. A malformed cursor returns `400 invalid_cursor`; a boundary absent from both RAM and SQLite returns `409 resync_required`. Newer inserts do not invalidate older-page cursors.
- Configuration responses may include the authentication username when configured; passwords never leave the server.

## Routes

| Method | Route | Result |
| --- | --- | --- |
| `GET` | `/health` | Public process health |
| `GET` | `/session` | Public login requirement and authentication state |
| `POST` | `/session` | Login and create an expiring server-side session |
| `DELETE` | `/session` | Revoke current session |
| `GET` | `/status` | Runtime build version, commit, build date, listeners, mode, uptime, configuration revision, RAM counts, and process-scoped traffic KPIs |
| `GET` | `/config` | Secret-free effective configuration, active YAML path, field origins, active UI overrides, restart-required fields, and revision |
| `PUT` | `/config` | Validate, atomically persist, and activate UI overrides |
| `DELETE` | `/config/overrides/{field}?expected_revision=N` | Remove one persistent override |
| `GET` | `/exchanges?limit=N&cursor=VALUE` | Newest-first deduplicated RAM/SQLite summaries; `limit` is 1 through 200 |
| `GET` | `/exchanges/{id}` | Metadata, headers, previews, and body availability without full bodies |
| `DELETE` | `/exchanges/{id}` | Delete one terminal exchange from RAM and SQLite history; active exchanges return `409 exchange_active` |
| `GET` | `/exchanges/{id}/request/body` | Captured request bytes |
| `GET` | `/exchanges/{id}/response/body` | Authoritative upstream or local proxy-error response bytes; capture mode returns `404 response_not_captured` |
| `GET` | `/exchanges/{id}/datagram/body` | Captured UDP datagram bytes |
| `DELETE` | `/exchanges` | Clear terminal RAM entries; active exchanges remain |
| `GET` | `/events` | SSE snapshot, replay, resynchronization, and live events |
| `GET` | `/storage/sqlite` | Database/WAL/freelist, retained range, queue, latency, failure, retention, and maintenance status |
| `GET` | `/storage/sqlite/cleanup-preview?keep_days=N` | UTC cutoff, matching row estimate, and current database size |
| `POST` | `/storage/sqlite/cleanup` | Confirmed cancellable batched age cleanup |
| `POST` | `/storage/sqlite/compact` | Bounded WAL checkpoint and incremental compaction |
| `POST` | `/storage/sqlite/vacuum` | Confirmed preflighted full VACUUM with traffic exclusion |

SQLite routes return `409 sqlite_disabled` when memory-only mode is active. Cleanup accepts `{"keep_days":N,"confirm":true}`. Full VACUUM accepts `{"confirm":true}`. Missing confirmation returns `412 confirmation_required`; concurrent maintenance returns `409 maintenance_in_progress`.

HTTP request and response body routes reject UDP exchanges with `400 transport_body_invalid`. The UDP datagram body route rejects HTTP exchanges with the same structured error. UDP payloads retain the same Range rules as HTTP bodies.

## Configuration update

`PUT /config` accepts one bounded JSON object:

```json
{
  "expected_revision": 1,
  "overrides": {
    "mode": "capture",
    "capture_response.status": 202,
    "preview.enabled": false
  }
}
```

A stale revision returns `409 revision_conflict`. Validation, HTTP/UDP listener binding, UDP upstream resolution, or persistence failure leaves runtime state and revision unchanged. Saves use a same-directory temporary file, restrictive permissions, synchronization, and atomic replacement. Storage mode and SQLite path are reported in `pending_restart`; request-processing settings apply to newly admitted exchanges. History capacity changes apply immediately.

When management listener or authentication settings reload, the successful response adds:

```json
{
  "management_reload": {
    "listener": "127.0.0.1:9080",
    "address_changed": true,
    "relogin_required": true
  }
}
```

`listener` is the actual bound address, including an assigned ephemeral port. Authentication and session-lifetime changes invalidate existing sessions. A candidate address that cannot bind returns `409 management_listener_unavailable`; no override in that request is persisted and the current management endpoint remains active.

When the inspected HTTP listener reloads, the successful response also adds:

```json
{
  "traffic_reload": {
    "listener": "127.0.0.1:9081",
    "address_changed": true
  }
}
```

`traffic_reload.listener` is the actual bound ingest address. A candidate that cannot bind returns `409 traffic_listener_unavailable`; no override in that request is persisted and the current ingest endpoint remains active. One save may return both reload objects when both listeners change.

When any UDP setting reloads, the successful response also adds:

```json
{
  "udp_reload": {
    "enabled": true,
    "listener": "127.0.0.1:9000",
    "address_changed": true
  }
}
```

`udp_reload.listener` is the active bound address and is empty when UDP becomes disabled. UDP proxy sessions close on every UDP reload. A candidate address or proxy upstream that cannot be prepared returns `409 udp_listener_unavailable`; no override in that request is persisted and the previous UDP endpoint remains active. One save may return management, traffic, and UDP reload objects together.

## Authentication

Authentication remains disabled when both configured credentials are empty. When enabled, every management data route, body route, mutation, and SSE connection requires an unexpired HttpOnly session cookie. Login is rate-limited per peer; session and attempt stores are bounded. Cookies use `SameSite=Strict` and `Secure` on HTTPS. Browser mutations with a present `Origin` must exactly match request scheme and host. Traffic-listener requests never require management authentication.

## SSE

`GET /events` uses schema version `1` and `text/event-stream`:

- A new connection receives an atomic `snapshot` plus its event cursor.
- A valid `Last-Event-ID` receives ordered replay followed by live events.
- A replay gap receives `resync` with a fresh atomic RAM snapshot.
- Heartbeat comments keep idle connections observable.
- Replay and subscriber queues are bounded by event count and encoded bytes. Slow subscribers are disconnected instead of blocking inspected traffic.

Lifecycle event names are `exchange.started`, `request.completed`, `response.completed`, `exchange.failed`, `exchange.evicted`, `exchange.deleted`, `history.cleared`, and `config.changed`. UDP terminal receive events use `request.completed` with `data.transport="udp"` and `data.datagram_bytes`; snapshots and replays contain UDP summaries under the same bounds as HTTP. `exchange.deleted` removes one exchange from Realtime and History in every connected interface. Capture mode never publishes or exposes its generated downstream response.

Proxy exchange details include response origin (`upstream` or `proxy_error`), effective upstream URL, observed response sizes, optional decoded-preview metadata, downstream delivery outcome, and available UTC/microsecond phase timing. `response.completed` remains reserved for fully captured upstream responses; locally generated proxy errors publish `exchange.failed` only.

Status traffic KPIs are additive version 1 fields: `current_requests`, `admission_rejected`, `body_too_large`, and `metrics_started_at`. Admission counts both concurrency and persistence-capacity `503` rejections. Body-too-large counts application-level incoming `413` responses. Requests rejected by Go before reaching the traffic handler cannot increment these counters.

`status.udp` reports enabled state, sanitized listener address, and active proxy-session count. All UDP metadata and payload routes use the same management authentication as HTTP captures and must not be emitted in application logs.
