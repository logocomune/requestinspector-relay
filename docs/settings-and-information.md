# Settings and information pages

## Configuration editor

`/settings` loads the secret-free effective configuration and shows each field's source: default, file, environment, CLI, startup, or persistent UI override. When authentication is configured, Administration shows the username and a fixed `*****` password mask; the actual password is never returned.

Every field includes an operator-facing hint describing its effect, relevant units or limits, and whether saving hot-reloads a runtime plane or requires a process restart.

Edits are submitted together with the displayed configuration revision. The server validates the complete effective configuration before atomically persisting overrides. Clearing either authentication credential clears both values in the same save, which disables authentication without an invalid intermediate state. Security password input is masked by default and has an eye control to reveal or hide its current draft. A red reset button marks an override for reset; it is removed only when the operator selects Save settings. Resetting either authentication credential marks both authentication overrides for atomic removal. Other reset buttons remove their individual UI override and reveal lower-priority values, including false, zero, and empty values. A conflicting revision reloads current values for review instead of overwriting another operator's update.

The management listener and authentication fields reload only the dashboard and Management API. Authentication reload invalidates existing sessions. A management port change redirects a direct HTTP browser session when the destination is unambiguous; wildcard, reverse-proxy, or otherwise ambiguous access shows the bound endpoint for manual navigation. HTTP and UDP ingest settings reload only their respective plane and show the active bound address in a toast. An unavailable management, HTTP traffic, or UDP listener rejects the complete save, leaves the current endpoint active, and shows an error toast. UDP upstream resolution failure follows the same rollback. Storage mode and SQLite path remain visibly restart-required. Runtime-safe settings apply to newly admitted exchanges. Existing exchanges retain their configuration snapshot. Switching HTTP to proxy mode requires a non-empty validated HTTP upstream URL; the interface applies the change immediately and shows an informational toast when the mode changes.

Configuration fields are grouped into `HTTP`, `UDP`, `Storage`, and `Administration` tabs. The HTTP tab starts with operating mode, traffic listener, request header bytes, and request body bytes, followed by separate `HTTP Capture` and `HTTP Proxy` sections. The UDP tab starts with enabled, mode, listener, and maximum datagram bytes, followed by `UDP Capture` and `UDP Proxy` sections. Within proxy settings, response header bytes precede response body bytes. When UDP is disabled, every UDP input except Enabled is disabled and shown with muted gray styling. Selecting the disabled UDP transport shows a `UDP disabled.` toast. Administration places Management listener beside Session lifetime, then Username beside Password. Tabs retain one shared draft and save operation; changing tabs never discards edits. Tabs with invalid fields show an error indicator. The primary UDP transport control remains disabled until the UDP listener is active. Management settings reload the web plane immediately; traffic-listener changes reload the HTTP ingest plane immediately. UDP mode and Settings edits persist independent `udp.*` overrides and hot-reload UDP ingest. Existing UDP proxy sessions close during reload. UDP proxy mode requires its UDP upstream in the same valid configuration revision.

Browser CORS is disabled by default. When enabled, cross-origin HTTP ingest requests receive reflected `Origin` CORS headers and preflight requests return `204 No Content` without credentials.

Operators enabling UDP should review [UDP security and operations](udp-security.md), especially listener exposure, fixed upstream routing, retention, and load limits.

## SQLite maintenance

When SQLite is active, Settings shows database and WAL sizes, exchange count, writer queue/reservations, failures, retention, and current maintenance state.

- Cleanup first calculates a cutoff and exact matching exchange count. Deletion requires confirmation and uses the previewed retention period.
- Incremental compaction checkpoints WAL and reclaims a bounded page batch.
- Full VACUUM requires an acknowledgement and a second confirmation. The backend rejects new persistence reservations while draining accepted work, checks free disk space, rebuilds the database, and verifies integrity.
- Compaction and full VACUUM show a completion toast after storage refresh; maintenance failures show an error toast.

Maintenance results and failures remain visible. Full VACUUM reports no artificial percentage because the SQLite driver exposes no reliable progress value.

## About and Credits

`/about` and `/credits` are static, offline-capable routes reachable before login. They contain no captured exchanges, listener addresses, storage paths, credentials, or configuration values. About requests sanitized build version, commit, and build date only when the current session is authenticated; unavailable build fields display `Unknown`.

Credits are compiled from `web/src/lib/credits.ts`. Every card includes locked version, canonical HTTPS project URL, SPDX-style license identifier, and purpose. `npm run check:credits` compares this catalog with direct `go.mod`/`go.sum` and `package-lock.json` entries; CI fails on missing or drifting metadata. No dependency metadata is fetched at runtime.

## Help

The Help menu provides separate Overview, HTTP Capture, UDP Capture, HTTP Proxy, and UDP Proxy pages. Each page contains an operator-facing flow diagram, current Settings path, setup sequence, configuration example, and relevant reload or safety limits. Help is static and available before authentication; it does not request configuration, captured traffic, or storage information.

## Accessibility

Primary and secondary navigation support keyboard use and visible focus. About and Credits move into a native accessible overflow menu below the desktop breakpoint. Semantic light/dark tokens have automated WCAG AA contrast checks. Layout tests cover 320-pixel width and 200% zoom; reduced-motion preferences suppress animation and transition duration.
