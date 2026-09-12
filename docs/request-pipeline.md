# Shared Request Pipeline

RequestInspector Relay routes capture and proxy traffic through one bounded HTTP pipeline. Shared admission, request capture, live RAM history, lifecycle state, event publication, and downstream delivery feed either a configured capture response or a bounded upstream proxy operation.

## Admission and snapshots

The pipeline rejects traffic with HTTP `503` when `limits.concurrent_exchanges` active slots are occupied. Rejection happens before exchange ID allocation, request-body reads, or mode-handler work.

Accepted traffic receives an immutable configuration snapshot and a cryptographically random 128-bit hexadecimal exchange ID. Active exchanges continue with that snapshot even when effective configuration changes later.

## Request capture

Request bodies are retained as exact bytes up to `limits.request_body_bytes`. Reading one additional byte detects overflow; oversized traffic receives HTTP `413`, never reaches a mode handler, and records an incomplete body with its observed byte count.

`request_header_bytes_estimate` uses parsed HTTP fields:

```text
Name: value\r\n
```

Each repeated value contributes one line. Request `Host` contributes one line, followed by one terminating CRLF. Request/status lines, original header casing/order, compression, and transport framing are excluded. Requests exceeding the configured estimate receive HTTP `431` before body capture.

Preview truncation never modifies retained body bytes. Enabled truncation exposes at most `preview.bytes`; disabled truncation exposes the complete accepted plain body as preview. Proxy inspection decodes supported content encodings into a separate bounded copy; authoritative stored and forwarded bytes remain encoded.

## Exchange lifecycle

Capture exchanges use:

```text
receiving -> completed | failed | cancelled
```

Proxy exchanges add `forwarding`:

```text
receiving -> forwarding -> completed | failed | cancelled
```

Every mutation increments the exchange revision. UTC timestamps describe start/completion; duration uses Go monotonic time when both timestamps originate from the runtime clock.

Mode handlers receive cloned request data and configuration. Handler mutation cannot change recorded RAM state. Shared orchestration owns lifecycle transitions and downstream delivery outcomes.

Proxy completion records the effective upstream URL, upstream or local-error response origin, normalized header estimates, observed encoded bytes, decoded-preview metadata, and phase timing. A captured upstream response publishes `response.completed` before buffered downstream relay. Downstream write failure changes the terminal state to `failed` without discarding the captured upstream response.

## RAM history

RAM history stores active exchanges plus a bounded number of terminal exchanges. Terminal entries sort newest-first. At capacity, the oldest terminal exchange is evicted; active exchanges are never evicted by history capacity. Capacity reduction evicts immediately. Clear removes terminal entries only.

All repository reads and writes clone mutable headers and body bytes. Concurrent access is synchronized. RAM count limits do not represent exact process-memory limits because active buffers, metadata, event replay, and subscriber queues also consume memory.

SQLite mode adds a count/byte-bounded persistence reservation before body reads. Saturation and full VACUUM reject with `503` before mode-handler effects. Terminal exchanges remain in RAM while one writer goroutine batches atomic SQLite commits. Persistent pages, details, and bodies load lazily; see [SQLite history](sqlite-history.md).

## Events

Every event envelope contains schema version `1`, a monotonic process-local event ID, UTC timestamp, event type, optional exchange ID, and exchange revision. The pipeline publishes:

- `exchange.started`
- `request.completed`
- `response.completed` for captured upstream proxy responses only
- `exchange.failed`
- `exchange.evicted`

Replay and subscriber queues are bounded by event count and encoded bytes. Publication never waits for subscribers. Slow subscribers are disconnected and must reconnect. HTTP SSE provides atomic initial snapshots, `Last-Event-ID` replay, explicit resynchronization after replay gaps, and heartbeat comments. See [Management API v1](api-v1.md).

See [Proxy mode](proxy-mode.md) for URL mapping, header rules, buffering, encoding inspection, timing, and error mapping.
