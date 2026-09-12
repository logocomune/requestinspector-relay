# Capture Mode

Capture mode records each accepted incoming request and returns the configured local response. It never creates or sends an upstream request.

## Response behavior

Configure the response under `capture_response`:

```yaml
capture_response:
  status: 201
  headers:
    Content-Type:
      - application/json
    X-RequestInspector Relay:
      - captured
  body: '{"accepted":true}'
```

Status accepts values from `200` through `599`. Header values remain multivalued. `Content-Length`, `Transfer-Encoding`, `Trailer`, and `Connection` are rejected because HTTP transport owns response framing. Header names and values containing line breaks are also rejected.

The configured body is delivered exactly for ordinary requests and statuses. RequestInspector Relay suppresses body bytes for `HEAD` and status codes `204`, `205`, and `304`, as required by HTTP semantics.

## Recorded data

RAM history and lifecycle events contain the incoming request only. The generated local response is not stored as a captured response, does not emit `response.completed`, and will not be available from the management response-body endpoint. Exchange metadata retains only downstream status, delivered body-byte count, and any delivery error.

Requests rejected for concurrency, header size, or body size never reach the capture handler. An oversized request receives `413` and cannot cause response-generation or upstream side effects.
