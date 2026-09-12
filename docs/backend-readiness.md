# Backend readiness

Backend contract version 1 is frozen for frontend implementation. Breaking field, route, event-name, or semantic changes require a new API version. Additive version-compatible fields still require fixture and documentation updates.

## Contract fixtures

Frontend-consumable examples live in [`api-v1-fixtures`](api-v1-fixtures/). Go contract tests decode each fixture with unknown-field rejection and round-trip it through the production response type. Tests also pin SSE schema version `1` and all lifecycle event names.

Fixtures cover runtime and SQLite status, effective configuration, exchange pages, full exchange details, SSE snapshots, replay-gap resynchronization, and lifecycle envelopes. Authoritative request and response body endpoints remain raw byte streams, so JSON fixtures do not represent them.

## Build matrix

Every target builds with `CGO_ENABLED=0`:

| Target | Go settings |
| --- | --- |
| Linux x86-64 | `GOOS=linux GOARCH=amd64` |
| Linux ARMv7 | `GOOS=linux GOARCH=arm GOARM=7` |
| Linux ARM64 | `GOOS=linux GOARCH=arm64` |
| macOS Intel | `GOOS=darwin GOARCH=amd64` |
| macOS Apple Silicon | `GOOS=darwin GOARCH=arm64` |
| Windows x86-64 | `GOOS=windows GOARCH=amd64` |
| Windows ARM64 | `GOOS=windows GOARCH=arm64` |

The backend workflow runs unit and deterministic property tests on native Linux, macOS, and Windows runners. It runs race and vet checks on Linux, cross-builds the complete matrix, executes every fuzz target for a bounded interval, and runs application plus SQLite smoke tests under ARMv7 and ARM64 QEMU emulation.

## Acceptance coverage

Application smoke tests start real independent listeners and verify health, status, embedded UI delivery, capture responses, proxy forwarding, management history, graceful shutdown, SQLite persistence, and restart reads. ARM smoke tests execute the same application paths plus SQLite schema/WAL, round-trip, pagination, restart, compact, VACUUM, status, and health checks.

Local readiness verification on 2026-09-07 used Go 1.26.7 on Linux amd64. Full tests, race detector, vet, all ten fuzz targets, seven CGO-free cross-builds, and QEMU ARMv7/ARM64 smoke tests passed.

## Operating contract

Executable configuration, source precedence, runtime-safe updates, restart-required fields, platform paths, and defaults are defined in [Configuration](configuration.md). API routes, errors, authentication, body ranges, pagination, and SSE behavior are defined in [Management API v1](api-v1.md).

Traffic admission, body/header bounds, lifecycle, RAM capacity, event replay, subscriber buffering, and slow-client behavior are defined in [Shared request pipeline](request-pipeline.md). Capture semantics are defined in [Capture mode](capture-mode.md); upstream mapping, buffering, compression inspection, and failure behavior are defined in [Proxy mode](proxy-mode.md).

SQLite queue limits, batching, persistence states and failures, retention, cleanup, compaction, full VACUUM, storage metrics, and measured benchmark profile are defined in [SQLite history and maintenance](sqlite-history.md). The benchmark documents payload, queue size, filesystem, CPU, Go version, write policy, throughput, allocation cost, and limits of interpretation.
