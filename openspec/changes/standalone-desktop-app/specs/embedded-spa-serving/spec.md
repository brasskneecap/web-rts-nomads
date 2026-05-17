## ADDED Requirements

### Requirement: Go server embeds the built Vue SPA under an opt-in build tag

The Go server SHALL embed the built Vue SPA (the contents of `client/src/game-portal/dist/`) into its binary via `//go:embed` when built with the `embed_spa` build tag. Without the tag, the server SHALL build and run unchanged from its current behaviour.

#### Scenario: Build without the tag preserves current behaviour

- **WHEN** the Go server is built without `-tags embed_spa`
- **THEN** the resulting binary does not contain any embedded SPA assets
- **AND** the binary still exposes only the existing API and WebSocket endpoints
- **AND** the `air` dev workflow operates identically to its current behaviour

#### Scenario: Build with the tag bundles the SPA

- **WHEN** the Go server is built with `-tags embed_spa` after the SPA has been built via `npm run build`
- **THEN** the resulting binary contains the SPA assets in its embedded filesystem
- **AND** the binary fails to build if `client/src/game-portal/dist/index.html` is missing

### Requirement: Embedded SPA is served from the same origin as the API

When the server is built with `-tags embed_spa`, it SHALL serve the SPA's static assets at paths under `/`, sharing the same host and port as the existing API endpoints. Cross-origin configuration SHALL NOT be required for the packaged Tauri build.

Note: the `tauri:dev` workflow is a deliberately different setup — the webview loads from `http://localhost:5173` (Vite) while the Go server runs at `http://localhost:8080`, so same-origin does NOT hold in dev. The existing `CORS_ALLOWED_ORIGIN=http://localhost:5173` default in the Go server remains required for that workflow. Only the packaged build (server with `-tags embed_spa`, webview loading from the server) is same-origin.

#### Scenario: SPA root request

- **WHEN** the webview navigates to `http://127.0.0.1:<port>/`
- **THEN** the server responds with the embedded `index.html`

#### Scenario: SPA asset request

- **WHEN** the webview requests `/assets/<filename>` for a JS, CSS, or image asset present in the SPA's `dist/assets/` directory
- **THEN** the server responds with the asset's bytes and a content-type matching its extension

#### Scenario: SPA client-side route fallthrough

- **WHEN** the webview requests a path under `/` that does not match any API route, any embedded asset path, or any of `/ws`, `/health`, `/api`, `/catalog`, `/maps`, `/matches`, `/lobbies`
- **THEN** the server responds with the embedded `index.html` so the Vue Router can resolve the path client-side

### Requirement: Embedded SPA serves correct cache headers per asset class

The embedded SPA `http.Handler` SHALL set HTTP cache headers according to the asset class:

- **Fingerprinted assets** under `/assets/` (Vue's build produces filenames like `assets/index-abc12345.js` with a content hash in the name): `Cache-Control: public, max-age=31536000, immutable`. These can be cached indefinitely because a change to the file produces a new filename.
- **`index.html`** (whether served directly at `/` or via SPA-route fallthrough): `Cache-Control: no-cache` (allows the cache to store it but forces revalidation on every load). `index.html` references the fingerprinted assets by name and therefore MUST always reflect the current build's filenames.
- **All other embedded assets** (favicons, top-level static files that aren't fingerprinted): `Cache-Control: public, max-age=3600` (one hour). A safe default; can be tightened per-asset later if a real cause arises.

Rationale: WebView2 / WebKit caches HTTP responses keyed by URL. With the packaged build's `port=0` policy, the cache effectively resets across launches (origin changes), so cache headers are not load-bearing for first-run correctness. They become load-bearing inside a single launch (avoid re-fetching big JS bundles on internal navigations) and across Steam-delivered patches that happen to land on the same kernel-assigned port. The `immutable` header on fingerprinted assets is the standard pattern; getting it wrong leaves measurable perf on the floor inside long-running sessions.

#### Scenario: Fingerprinted asset has immutable cache header

- **WHEN** the webview requests `/assets/index-abc12345.js` from the embedded handler
- **THEN** the response includes `Cache-Control: public, max-age=31536000, immutable`

#### Scenario: `index.html` is not cached without revalidation

- **WHEN** the webview requests `/` from the embedded handler
- **THEN** the response includes `Cache-Control: no-cache`
- **AND** subsequent requests for `/` revalidate (`If-Modified-Since` / `ETag` round-trip) before being served from cache

#### Scenario: SPA fallthrough route returns `index.html` with the same no-cache header

- **WHEN** the webview requests `/some-spa-route` (resolved by SPA-route fallthrough)
- **THEN** the response body is `index.html` AND the response includes `Cache-Control: no-cache`

### Requirement: API and WebSocket routes take precedence over SPA routes

API and WebSocket routes SHALL take precedence over SPA asset serving. Any route registered with the existing HTTP router before the SPA fallthrough handler is mounted SHALL be handled by that route's existing handler. The SPA fallthrough handler SHALL run only for requests that did not match any prior route, regardless of which routes those are (so adding a new API mount point in the router automatically takes precedence without changes to embedded-SPA code).

#### Scenario: API route under embed_spa build

- **WHEN** the webview issues a `GET /api/profile` request against a server built with `-tags embed_spa`
- **THEN** the server invokes the existing profile API handler
- **AND** the server does not serve the embedded `index.html` for that path

#### Scenario: WebSocket upgrade under embed_spa build

- **WHEN** the webview opens a WebSocket connection to `/ws` against a server built with `-tags embed_spa`
- **THEN** the server performs the WebSocket upgrade through the existing WS hub
- **AND** the server does not serve the embedded `index.html` for that path

#### Scenario: Newly-added API route automatically wins precedence

- **WHEN** a future PR registers a new HTTP route (e.g., `GET /api/replays/:id`) with the existing router prior to the SPA fallthrough
- **THEN** requests to that route are served by the new handler
- **AND** no change to embedded-SPA code is required for the new route to take precedence

### Requirement: SPA compiled version is injected at build time

The SPA's compiled version SHALL be made available at runtime via a Vite `define` injection (e.g., `__APP_VERSION__`) in `vite.config.ts`. The injection value SHALL be resolved in this priority order at build time:

1. **`NOMADS_VERSION` environment variable** if set and non-empty (overrides everything; lets CI inject a tag like `v0.4.0` or a build number).
2. Otherwise, the **short git SHA** of `HEAD` if `git` is available AND the build runs inside a git checkout.
3. Otherwise, the literal string `"unknown"`.

The literal string `"dev"` SHALL be used only by `npm run dev` and `tauri:dev` (set explicitly in the Vite dev-server config), not by `npm run build`. The Go server's `NOMADS_READY` line uses the same priority order for its own version field (via build-time `-ldflags "-X main.version=..."`), so the SPA-↔-server version comparison stays well-defined for tagged releases, ad-hoc git builds, and tarball/CI-without-git builds alike.

The Vue router's unknown-route handler SHALL render a "Page not found — Return to main menu" view rather than silently landing on the menu.

#### Scenario: Packaged build with `NOMADS_VERSION` env

- **WHEN** the SPA is built via `NOMADS_VERSION=v0.4.0 npm run build` (typical CI release tag)
- **THEN** the injected `__APP_VERSION__` value is `"v0.4.0"`
- **AND** the Go binary built in the same CI step with `-ldflags "-X main.version=v0.4.0"` emits `version=v0.4.0` in `NOMADS_READY`

#### Scenario: Packaged build from a git checkout, no env override

- **WHEN** the SPA is built via `npm run build` from a git checkout with no `NOMADS_VERSION` set
- **THEN** the injected `__APP_VERSION__` value is the short git SHA
- **AND** the SPA's first WS hello message contains this value

#### Scenario: Build outside git with no env override (tarball / clean CI)

- **WHEN** the SPA is built via `npm run build` outside a git checkout (extracted tarball, container build, no `git` on PATH) and `NOMADS_VERSION` is not set
- **THEN** the injected `__APP_VERSION__` value is the literal string `"unknown"`
- **AND** the Vite build does NOT fail (running `git` is best-effort, not a build prerequisite)
- **AND** the Go server, built under the same conditions, also reports `"unknown"`; the version-mismatch check treats two `"unknown"` values as matching (since they were built together)

#### Scenario: Dev build uses the literal `dev`

- **WHEN** the SPA is run via `npm run dev` or `tauri:dev`
- **THEN** the injected `__APP_VERSION__` value is `"dev"`
- **AND** the Go server (also reporting `"dev"` in `NOMADS_READY`) treats this as a matching version

#### Scenario: Unknown SPA route lands on a "not found" view

- **WHEN** the webview navigates (e.g., via deep link or via the SPA's own routing) to a path the Vue router does not recognise
- **THEN** the SPA renders a "Page not found" view containing a "Return to main menu" button
- **AND** the SPA does not silently redirect to the main menu without user interaction

### Requirement: Free-port discovery and readiness handshake

The server SHALL accept `--port 0` (CLI) or `WEBRTS_PORT=0` (env) as a request to bind to a free OS-assigned port. After it has bound and is ready to accept connections, the server SHALL print exactly one line to stdout in the format `NOMADS_READY url=<url> version=<version>` and SHALL not print any line with that prefix at any other time.

#### Scenario: Free-port binding

- **WHEN** the server is started with `WEBRTS_PORT=0`
- **THEN** the server binds to a free OS-assigned localhost port
- **AND** the server emits one line `NOMADS_READY url=http://127.0.0.1:<port> version=<sha>` on stdout
- **AND** subsequent connections to that port succeed

#### Scenario: Stdin-EOF shutdown

- **WHEN** the parent process closes the server's stdin while the server is running
- **THEN** the server initiates its standard shutdown path
- **AND** the server exits cleanly within 5 seconds
