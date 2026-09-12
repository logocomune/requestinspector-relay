# Configuration

RequestInspector Relay combines configuration sources in this order, from lowest to highest priority:

```text
built-in defaults -> YAML base -> environment -> CLI flags -> persistent UI overrides
```

Explicit `false`, `0`, empty strings, and empty collections remain effective values. UI overrides use dotted field names and affect only named fields.

Runtime management uses `GET /api/v1/config`, `PUT /api/v1/config`, and `DELETE /api/v1/config/overrides/{field}`. Updates require the current revision. Successful saves validate the complete effective configuration, preflight HTTP and UDP listener changes, write a protected same-directory temporary file, atomically replace the YAML file, then activate runtime-safe fields. Failed validation, listener binding, upstream resolution, or persistence keeps the running revision unchanged.

The management listener and authentication settings reload only the management web plane. Inspected HTTP settings reload only the HTTP ingest plane. Every `udp.*` setting reloads only the UDP ingest plane. Storage mode and SQLite path remain restart-required. HTTP and UDP processing settings apply to newly admitted traffic. RAM history capacity changes immediately; active exchanges retain their admission snapshot. Positive SQLite retention runs hourly; zero disables scheduled cleanup.

`cors.enabled` enables browser CORS on the HTTP traffic ingest listener. It reflects the caller `Origin` like the hosted ingest funnel and answers preflight requests with `204 No Content`; credentials remain disabled. The setting is disabled by default and applies immediately after saving.

## Listener hot reload

Saving `listeners.management` binds the candidate TCP address before configuration persistence. A successful bind starts a new management HTTP server, closes management SSE streams from the previous generation, and gracefully drains that generation. Inspected HTTP traffic, UDP, RAM history, and SQLite remain active. If the address is occupied or unavailable, RequestInspector Relay logs the operating-system error, returns `409 management_listener_unavailable`, preserves the current endpoint and revision, and does not persist any field from that save.

Saving `listeners.traffic` uses the same transaction boundary. RequestInspector Relay pre-binds the candidate TCP address, persists the complete update only after binding succeeds, starts a new HTTP ingest generation, then drains the previous generation for up to `shutdown_timeout`. Already admitted requests may complete with their original configuration snapshots. Management, UDP, RAM history, and SQLite remain active. Bind failure logs the operating-system error, returns `409 traffic_listener_unavailable`, preserves the active ingest endpoint and revision, and persists nothing from that save.

Saving `authentication.username`, `authentication.password`, or `authentication.session_ttl` replaces the management handler and invalidates every existing dashboard session. When authentication remains enabled, users must sign in with the new credentials. Clearing both credentials disables authentication without restarting the process. Password values are never returned or logged.

Listener replacement uses portable Go TCP behavior without reuse-port socket options. A different port can be opened before the previous listener closes. Address changes that overlap the current socket on the same port, such as `127.0.0.1:8080` to `0.0.0.0:8080`, are rejected for either HTTP plane; choose another port or restart the process. External firewall, reverse-proxy, and container port-publishing rules are not changed automatically.

Saving any `udp.*` field prepares the complete UDP generation before configuration persistence. Enabling UDP or changing `udp.listen` binds the candidate address first. Successful saves activate the candidate, close the previous UDP socket and proxy sessions, and leave both HTTP planes, RAM history, and SQLite active. Changes that keep the configured listener address reuse its socket but still replace UDP proxy sessions so mode, upstream, TTL, capacity, payload, capture-reply, and rate limits apply together. Disabling UDP closes its socket.

An unavailable UDP address or unresolved proxy upstream logs the operating-system or resolver error, returns `409 udp_listener_unavailable`, preserves the current UDP endpoint and revision, and persists nothing from that save. UDP replacement uses portable Go socket operations without platform-specific reuse-port options. A different address can bind before the previous socket closes; overlapping bindings on the same port may be rejected by Linux, macOS, or Windows and require choosing another port. Firewall, container port publishing, and network routing remain external operator responsibilities.

UDP exposure, reply, proxy, retention, and load-profile limits are documented in [UDP security and operations](udp-security.md).

## Configuration path

Configuration path selection uses `--config`, then `REQRELAY_CONFIG`, then platform default. A missing explicitly selected file stops startup. A missing default file uses built-in defaults.

| Platform | Default YAML | Default SQLite |
| --- | --- | --- |
| Linux | `${XDG_CONFIG_HOME:-$HOME/.config}/requestinspector-relay/reqrelay.yaml` | `${XDG_DATA_HOME:-$HOME/.local/share}/requestinspector-relay/reqrelay.db` |
| macOS | `$HOME/Library/Application Support/RequestInspectorRelay/reqrelay.yaml` | `$HOME/Library/Application Support/RequestInspectorRelay/reqrelay.db` |
| Windows | `%APPDATA%\RequestInspectorRelay\reqrelay.yaml` | `%LOCALAPPDATA%\RequestInspectorRelay\reqrelay.db` |

A relative non-empty `storage.sqlite_path` resolves from the YAML file directory. An empty path selects the platform default.

## Built-in defaults

| Setting | Default |
| --- | --- |
| Mode | `capture` |
| Management listener | `127.0.0.1:8080` |
| Traffic listener | `0.0.0.0:8081` |
| Capture status/headers/body | `200` / `Content-Type: application/json` / `{"ok":true}` |
| Request and response body limits | `1048576` bytes |
| Request and response header limits | `65536` bytes |
| Concurrent exchanges | `128` |
| Preview | enabled, `16384` bytes |
| RAM history | `100` exchanges |
| Storage | `sqlite` |
| Retention | disabled (`0` days) |
| Upstream timeout | `30s` |
| Authentication session lifetime | `24h` |
| Graceful shutdown timeout | `10s` |
| UDP | disabled; `0.0.0.0:12050`; `capture`; 65507-byte datagrams; 1024 sessions; TTL up to 30 seconds; no capture reply |
| UDP proxy reply budgets | 100 replies/second and 1048576 bytes/second per session |

## YAML structure

Use [`reqrelay.example.yaml`](reqrelay.example.yaml) as a complete starting point. `base` stores file-controlled values. `ui_overrides` stores durable values created through the management configuration API.

Supported override names:

```text
mode
listeners.management
listeners.traffic
upstream.url
upstream.timeout
capture_response.status
capture_response.headers
capture_response.body
cors.enabled
limits.request_body_bytes
limits.response_body_bytes
limits.request_header_bytes
limits.response_header_bytes
limits.concurrent_exchanges
preview.enabled
preview.bytes
history.max_exchanges
storage.mode
storage.sqlite_path
storage.retention_days
authentication.username
authentication.password
authentication.session_ttl
udp.enabled
udp.listen
udp.mode
udp.upstream
udp.max_datagram_bytes
udp.max_sessions
udp.session_ttl
udp.capture_response
udp.max_replies_per_second
udp.max_reply_bytes_per_second
shutdown_timeout
```

Unknown override names stop startup. Authentication username may be shown in Settings; passwords are never returned by status or configuration endpoints.

## Environment and CLI examples

```sh
REQRELAY_MODE=proxy \
REQRELAY_UPSTREAM_URL=https://api.example.test/service \
go run ./cmd/reqrelay
```

```sh
go run ./cmd/reqrelay \
  --config ./reqrelay.yaml \
  --management-listen 127.0.0.1:9080 \
  --traffic-listen 127.0.0.1:9081
```

```sh
REQRELAY_UDP_ENABLED=true \
REQRELAY_UDP_MODE=proxy \
REQRELAY_UDP_UPSTREAM=resolver.example.test:9000 \
go run ./cmd/reqrelay
```

UDP is configured independently from HTTP and starts or stops when Settings are saved. In proxy mode, each canonical client address gets one connected upstream socket. Sessions expire after the configured TTL (30 seconds maximum), evict at `max_sessions`, and apply per-session reply and byte budgets. UDP reload closes existing proxy sessions; new datagrams create sessions using the new configuration.

Run `go run ./cmd/reqrelay --help` for all environment variables and flags.

## Startup output

After both listeners start, RequestInspector Relay prints a plain retro terminal startup banner to standard output:

```text
+--------------------------------------------------+
|  REQUESTINSPECTOR RELAY                          |
|  HTTP REQUEST INSPECTOR / REVERSE PROXY          |
+--------------------------------------------------+
VERSION  dev
BUILT    unknown
Ingest: http://0.0.0.0:8081
Web interface: http://127.0.0.1:8080
```

The URLs reflect the effective bound listener addresses. Successful HTTP/UDP listener reloads and rejected bind attempts use the standard Go `log` package.

On `SIGINT` or `SIGTERM`, RequestInspector Relay immediately closes SSE streams plus inspected HTTP and UDP traffic, including active connections. It then flushes and closes SQLite before gracefully shutting down remaining management requests. `shutdown_timeout` bounds management-server draining; SQLite shutdown waits for every admitted write to complete before the process exits.

## Current endpoints

| Listener | Endpoint | Behavior |
| --- | --- | --- |
| Management | `GET /api/v1/health` | Returns `{"status":"ok"}` |
| Management | `GET /api/v1/status` | Returns build, mode, listeners, and uptime |
| Management | `GET /api/v1/storage/sqlite` | Returns SQLite, WAL, queue, failure, retention, and maintenance status |
| Management | `/` | Serves embedded placeholder interface |
| Traffic | any path in capture mode | Captures through the bounded shared pipeline, then returns the configured local response |
| Traffic | any path in proxy mode | Captures, forwards to the configured upstream, then relays the bounded upstream response |

`upstream.url` accepts only absolute HTTP/HTTPS URLs without user information, fragments, or queries. An optional escaped path acts as a prefix. Proxy timeouts and body/header limits apply from each admitted exchange's configuration snapshot.

Capture response semantics are documented in [`capture-mode.md`](capture-mode.md). Proxy forwarding is documented in [`proxy-mode.md`](proxy-mode.md). Shared pipeline behavior and RAM/event bounds are documented in [`request-pipeline.md`](request-pipeline.md). SQLite writer, retention, maintenance, and benchmark behavior is documented in [`sqlite-history.md`](sqlite-history.md).
