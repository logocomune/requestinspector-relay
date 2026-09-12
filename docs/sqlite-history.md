# SQLite history and maintenance

SQLite mode keeps RAM as the live SSE source and persists terminal exchanges for restart-safe history. RequestInspector Relay uses the pure-Go `modernc.org/sqlite` driver, schema migrations, WAL mode, `synchronous=NORMAL`, foreign keys, a bounded busy timeout, and incremental auto-vacuum for newly created databases. Store the database on a local filesystem; WAL mode is not supported on network filesystems.

## Writer admission

All exchange writes and maintenance mutations pass through one writer goroutine. Traffic reserves writer capacity before request-body reads or proxy side effects. Reservations are bounded at 256 exchanges and 512 MiB using each exchange's configured maximum body and normalized-header exposure. Saturation or full-VACUUM maintenance returns `503 persistence_capacity_exhausted` before traffic processing.

Terminal exchanges enter RAM with `persistence.state=queued`. Successful commits change the live value to `persisted`; failures change it to `failed` with an error while the RAM copy remains available. Batches contain at most 50 exchanges and wait at most 25 ms. Each batch uses one short transaction; HTTP exchanges retain separate authoritative request and optional response body columns.

UDP payloads use the forward-only schema migration `v2` and the explicit `udp_datagrams` detail table. They never occupy HTTP request or header columns. Existing HTTP databases migrate in place; existing rows remain readable. UDP detail reads remain lazy, and raw payload downloads use the same bounded byte-Range behavior as HTTP request bodies.

## Lazy history

Startup never hydrates persisted history into RAM. `GET /api/v1/exchanges` merges and deduplicates live RAM entries with newest-first SQLite summaries. Opaque completion-time/ID cursors remain stable when newer rows arrive across HTTP and UDP rows. Detail reads load metadata only; body endpoints query HTTP or UDP payload storage on demand and retain Range behavior.

UDP datagram payloads are sensitive retained data: apply authenticated access, deletion, retention, and filesystem controls described in [UDP security and operations](udp-security.md).

`DELETE /api/v1/exchanges/{id}` drains pending persistence writes, then removes that terminal exchange from SQLite and RAM. It never deletes active exchanges.

RAM-only restart starts with empty history. SQLite restart exposes persisted summaries immediately. Clearing RAM through `DELETE /api/v1/exchanges` never deletes SQLite rows.

## Retention and cleanup

`storage.retention_days=0` disables scheduled retention. Positive values run hourly. Cutoffs use `now.UTC().AddDate(0, 0, -days)` and delete only terminal rows strictly older than the cutoff. Runtime configuration updates change the scheduler without restarting.

Cleanup preview reports UTC cutoff, matching row count, and current database size. Confirmed cleanup deletes oldest matching rows in primary-key batches of 500. Each batch is a short writer operation; traffic may proceed between batches. Request cancellation stops subsequent batches. Only one cleanup, compact, or VACUUM operation may run at once. Matching terminal RAM entries are removed; cutoff-boundary and active entries remain.

## Compaction and full VACUUM

Compact performs a passive WAL checkpoint and at most 256 incremental-vacuum pages. It reports free pages before/after and estimated reclaimed bytes. Repeated calls can continue reclaiming reusable pages without one unbounded maintenance operation.

Full VACUUM requires `{"confirm":true}`. It enters `vacuum` maintenance state, rejects new traffic reservations, drains accepted/queued exchanges through the request context deadline, verifies free space of at least twice current database size, truncates WAL, runs `VACUUM`, then requires `PRAGMA quick_check` to return `ok`. Failure leaves normal admission enabled after maintenance exits and records the storage error.

## Status and operating limits

`GET /api/v1/storage/sqlite` and the storage member of `/api/v1/status` expose database/WAL bytes, page size/count, freelist pages, retained range/count, queue and reservation utilization, write/failure/busy counters, last write latency/error, retention, and maintenance state.

Reference benchmark command:

```sh
go test ./internal/sqlite -run '^$' -bench BenchmarkRepositorySustainedWrites -benchtime=2s -benchmem
```

Profile measured 2026-09-07: Linux amd64, AMD Ryzen 7 5800H, local temporary filesystem, Go 1.26, WAL plus `synchronous=NORMAL`, 1 KiB request body plus 8-byte response body, 512-entry/64 MiB benchmark queue. Result: 99,471 ns/exchange (about 10,053 exchanges/second), 10.29 MiB/s, 12,535 B/op, 67 allocs/op. This exceeds the 100 exchanges/second phase gate for this payload. It is not a guarantee for slower disks, larger bodies, `fsync` policies, proxy latency, or concurrent readers.
