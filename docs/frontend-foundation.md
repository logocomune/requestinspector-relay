# Frontend foundation and PWA

RequestInspector Relay ships a static SvelteKit 5 application embedded in the Go executable. Production needs no Node.js process. Source lives under `web/`; `npm run build` writes the complete static application to `internal/webui/dist`.

## Development and build

Node.js 24 and npm are required for frontend development:

```sh
cd web
npm ci
npm run check
npm test
npm run build
```

Start the Go application after rebuilding to exercise the embedded output. Vite development proxies `/api` to `http://127.0.0.1:8080`.

The frontend uses Svelte runes, TypeScript, Tailwind CSS 4 semantic tokens, and a local typed Material Design Icons renderer. Runtime guards validate frozen API v1 fixtures from `docs/api-v1-fixtures`; the SSE reducer ignores stale event cursors, deduplicates snapshots, and handles eviction, clearing, and resynchronization without storing full bodies.

## Themes and accessibility

System, Light, and Dark preferences use browser-local key `reqrelay.theme.v1`. A small head bootstrap validates and applies the stored preference before CSS loads. System mode follows `prefers-color-scheme` changes. Theme selection never changes server configuration or recreates the SSE connection. Controls live in Settings rather than global headers. The authenticated and public headers place a green Live or pulsing red Offline connection status beside the logo; the authenticated tab bar places Capture/Proxy controls on the right. Successful Settings saves show a five-second toast; proxy URL validation errors use red styling. All toast variants use 40% opacity.

Semantic tokens define page, surface, text, border, action, focus, and error roles for both themes. Controls retain text labels, two-pixel focus indicators, keyboard tab navigation, reduced-motion behavior, and narrow-screen reflow.

## PWA policy

`assets/icon.png` remains the canonical source. The frontend contains generated 192×192 and 512×512 regular and maskable PNG variants. `manifest.webmanifest` provides stable root identity, scope, install colors, description, and standalone display metadata.

The versioned service worker precaches only the application shell, generated Svelte assets, manifest, and icons. Every non-GET request and every normalized `/api` path stays network-only, including sessions, SSE, configuration, history, and body downloads. Offline navigation returns the shell and reports **Offline / live data unavailable**; no captured history is presented as cached live data.

Application code owns service-worker registration; SvelteKit automatic registration is disabled. Vite development registers the worker as an ES module and excludes the production-only `/index.html` fallback from precache. Production registers the generated classic worker and includes the fallback.

Updates remain waiting while the current application runs. The shell displays **Update available** and reloads once only after explicit approval. Initial service-worker activation never forces a reload.

The Go static handler serves navigation HTML, service worker, manifest, and unversioned assets with revalidation. It serves hashed `/_app/immutable/` assets with one-year immutable caching and forces `application/manifest+json` for the manifest. The explicit `_app` embed pattern is required because Go directory patterns otherwise omit underscore-prefixed directories.

Installability requires HTTPS or a localhost secure context. Production deployments should expose the management listener through trusted TLS termination.

## Verification

Vitest covers theme resolution, API fixture guards, and SSE reducer behavior. Deterministic fast-check properties prove API paths and mutations remain network-only. Playwright checks manifest identity and icons, root-scope service-worker control, offline navigation, shell availability, and absence of API entries in Cache Storage. Go tests verify embedded files, content types, and cache headers.
