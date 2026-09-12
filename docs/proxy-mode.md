# Proxy Mode

Proxy mode forwards each accepted traffic request to one configured HTTP or HTTPS upstream. It uses the same admission, request capture, RAM history, lifecycle, and event pipeline as capture mode.

## Target mapping

`upstream.url` must contain an `http` or `https` scheme, an authority, and an optional path prefix. User information, fragments, and query strings, including an empty trailing `?`, are rejected. Incoming absolute URLs cannot choose a different destination.

The configured escaped prefix is joined with the incoming escaped path. Incoming raw query text is copied without parsing or reordering:

```text
upstream.url: https://api.example.test/service
incoming:     /orders%2Fopen?id=7&id=8
outgoing:     https://api.example.test/service/orders%2Fopen?id=7&id=8
```

## Forwarding

RequestInspector Relay preserves the method, accepted encoded body bytes, content encoding, raw query, and repeated end-to-end headers. It rewrites `Host` to the upstream authority. Caller-supplied forwarding metadata is discarded; `X-Forwarded-For`, `X-Forwarded-Host`, and `X-Forwarded-Proto` are rebuilt from the actual peer and accepted request.

Hop-by-hop headers are removed in both directions, including header names nominated by `Connection`. Upstream redirects and application errors are returned without follow-up requests. No application retry loop is added, and implicit response decompression is disabled. Default HTTPS certificate verification remains enabled.

Request and response bodies are buffered. Request overflow returns `413` before any upstream side effect. Upstream response headers and bodies are checked before downstream headers are committed. Response overflow returns `502`.

CONNECT and HTTP upgrades, including WebSocket, return `501 unsupported_upgrade`. Indefinite streaming and tunnels are outside the version 1 buffered contract.

## Encoded representations and previews

Authoritative request and response bodies retain the encoded HTTP representation bytes. Transport framing and chunk boundaries are not retained. `Content-Length` is recalculated from relayed body bytes where a response body is allowed.

Inspection decodes separate bounded copies for `gzip`, `x-gzip`, and zlib-wrapped `deflate`, including stacked encodings in reverse order. Decoded copies cannot exceed the configured body limit or a 100:1 expansion ratio. Unsupported, corrupt, or limit-exceeding encodings leave forwarding unchanged and expose a preview error plus a bounded raw preview.

## Results and timing

Captured upstream responses use origin `upstream`. Locally generated proxy failures use origin `proxy_error`. Effective upstream URL, normalized response-header estimate, observed response bytes, downstream delivery errors, and available timestamps remain attached to the same exchange.

Proxy timing fields use monotonic clock differences and microseconds:

- `upstream_dispatch_duration_us`: upstream start through request write
- `upstream_wait_duration_us`: request write through first response byte
- `upstream_receive_duration_us`: first byte through complete bounded response capture
- `upstream_total_duration_us`: upstream start through complete response capture
- `downstream_write_duration_us`: buffered downstream relay
- `proxy_total_duration_us`: admission through downstream completion or failure

The web interface shows all six timing values in three rows, with two values per row, converted to milliseconds with three decimal places. A help icon beside each value explains its measurement on hover or focus. API fields remain in microseconds.

Unreached phases remain absent. UTC phase timestamps support display and correlation.

## Failure mapping

| Condition | Downstream result |
| --- | --- |
| Connection or TLS failure | `502 upstream_unavailable` |
| Response header limit | `502 response_headers_too_large` |
| Response body limit | `502 response_body_too_large` |
| Response read failure | `502 response_body_read_failed` |
| Upstream timeout | `504 upstream_timeout` |
| Client cancellation | cancelled exchange |
| Upstream 3xx, 4xx, or 5xx | original response |
