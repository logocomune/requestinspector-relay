# Realtime and History interface

RequestInspector Relay exposes captured traffic through two connected workspaces. Realtime follows RAM/SSE lifecycle updates; History combines current RAM summaries with cursor-paginated SQLite summaries when persistence is enabled. Both use the same request/response inspector.

## Realtime

Realtime displays only exchanges received through live SSE lifecycle events, complete inspectors in newest-first vertical order. Existing History entries never populate Realtime on startup or refresh. The visible feed retains at most 50 exchanges; each new exchange enters at the top and the oldest visible exchange drops from Realtime without removing it from History. Reconnect snapshots and replay-gap resynchronization update History; live exchanges resume only from newly received lifecycle events. Its `All | HTTP | UDP` control filters only the browser view; it does not change the live feed or retention.

The Realtime tab shows a red dot only when a live exchange arrives while another tab is active. Selecting Realtime marks pending live exchanges as viewed.

Each Realtime header shows the unique exchange ID and two actions. Close removes only that card from the current browser's Realtime feed. Trash requests confirmation, then deletes the terminal exchange from RAM and SQLite so it disappears from Realtime and History for every connected browser. Active exchanges cannot be deleted.

The tab bar trash menu clears the current Realtime feed locally or, with confirmation, clears both Realtime and persisted History.
Actions are disabled when the corresponding feed is empty.
Selecting an action closes the dropdown immediately.

Clear and delete actions report success through stacked top-right dismissible toasts that disappear after five seconds. Each toast uses an icon-only X dismissal control and translucent semantic action-color background.

Realtime omits JSON, Raw, Hex, copy, download, and loading controls when the corresponding request or response body contains zero bytes. History retains its existing payload presentation.

## History

History loads newest summaries first and requests older pages only near the end of its independently scrollable list. Its independent `All | HTTP | UDP` control filters loaded rows without changing pagination or retention. Each row shows a transport chip before its capture or proxy mode icon. UDP rows show source and listener addresses, accepted byte count, timestamp, mode, and terminal state; HTTP rows retain their method and path. The inspector repeats the transport chip before the mode icon. Hovering or focusing either icon identifies the mode. The primary tab displays the total number of retained exchanges as `History (##)`. Opening History automatically selects the most recent visible exchange if no exchange is selected. Fixed-height virtualization bounds rendered list rows. RAM, SSE, and SQLite summaries are deduplicated by exchange ID while retaining the newest revision. Selection remains stable across live inserts. Detail, body, end-of-history, empty, unavailable, loading, and retry states remain explicit.

On desktop, the History list column can be resized by dragging its separator or using Left/Right arrow keys. The width is persisted in browser local storage. Mobile keeps the stacked layout.

## Inspector and payloads

UDP inspection exposes Datagram source, listener, accepted bytes, delivery result, raw payload, and a range-backed download. Text, JSON when payload bytes parse as JSON, Hex, and escaped Raw views stay bounded while full copy and download use authoritative bytes. UDP proxy exchanges show receive, upstream-forward, upstream-reply, and client-forward timeline stages. Capture replies marked truncated and oversized discarded datagrams have explicit warnings. HTTP-only request lines, headers, response status, User-Agent, and HTTP proxy timings remain hidden for UDP.

Request metadata includes localized time, remote address, complete request line, repeated headers, normalized header-size estimate, content type, content encoding, and body sizes. Request-line and header-block copy controls provide visible success or failure feedback. The User-Agent binoculars action opens the external useragents.io parser in a new tab and retains the raw value locally; local inspection reports `Unknown` for fields it cannot classify.

Capture mode intentionally shows no generated response. Proxy mode correlates request and response in one inspector and shows response origin, headers, authoritative encoded body, downstream delivery errors, and available dispatch, wait, receive, upstream-total, downstream-write, and proxy-total durations.

Payload loading uses bounded 64 KiB HTTP Range requests. JSON becomes formatted only after successful parsing; invalid JSON remains available as Raw. Binary data uses a bounded hex/text viewport. Raw and download flows use authoritative captured bytes. Copy-full and download remain explicit user actions; API and body responses remain network-only under the service worker.

## Browser verification

Playwright starts an actual RequestInspector Relay process plus a local upstream. Tests cover newest-first multi-exchange Realtime inspection, stable History selection during inserts, capture-response hiding, proxy correlation/timing, keyboard tab navigation, narrow reflow, PWA scope, offline shell behavior, and API cache exclusion.
