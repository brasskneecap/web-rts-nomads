# server

Go HTTP + WebSocket server for the game. Authoritative for simulation, combat,
pathing, profiles, lobbies, and matches. The client connects over WebSocket and
the server broadcasts state.

## Local development

The browser dev loop runs the server with `air` (live-reload) on `:8137` and
the Vue SPA on `:5173` via Vite. The SPA proxies game traffic (`/ws`, `/api`,
`/catalog`, `/maps`, `/matches`, `/lobbies`, `/health`) to the server. CORS is
allowed for `http://localhost:5173` by default and can be overridden with
`CORS_ALLOWED_ORIGIN`.

The dev port is `8137` (not Go's `:8080` default) because a Chromium/CEF
remote-debugger — e.g. `steamwebhelper` — squats `:8080` and shadows the
server, silently 404-ing the SPA's proxied API calls. `server/dev.bat` and
`dev.ps1` set `WEBRTS_PORT=8137`; the Vite proxy target in
`client/src/game-portal/vite.config.ts` matches. Running `air` bare (without
`WEBRTS_PORT`) still binds `:8080` and will NOT match the proxy — use
`dev.bat` / `start.bat`, or set `WEBRTS_PORT=8137` yourself.

```sh
WEBRTS_PORT=8137 air   # server on :8137 (dev.bat / start.bat set this for you)
npm run dev            # SPA on :5173, in client/src/game-portal/
```

## Build tags

### `embed_spa` — bundle the built Vue SPA into the server binary

Used by the packaged desktop build. With this tag, the server serves the SPA's
static assets at `/` from the same origin as the API:

```sh
# 1. Build the SPA into client/src/game-portal/dist/
cd client/src/game-portal && npm run build

# 2. Stage dist/ into the server module (Go //go:embed cannot reach files
#    outside the module, so the SPA dist must be copied into server/cmd/api/).
rm -rf server/cmd/api/dist
cp -r client/src/game-portal/dist server/cmd/api/dist

# 3. Build the server with the tag
cd server && go build -tags embed_spa ./cmd/api
```

The Makefile / packaging script (added in §18 of the standalone-desktop-app
change) owns the staging step end-to-end. `server/cmd/api/dist/` is
.gitignored.

Without the tag, the server stays API-only and the `air` dev workflow operates
unchanged. The router only mounts the SPA fallthrough at `/` when the
`spa_embed.go` constructor returns a non-nil handler, which happens only under
`-tags embed_spa`.

### SPA serving behaviour (when `embed_spa` is on)

- `GET /` → embedded `index.html`, `Cache-Control: no-cache`
- `GET /assets/*` (fingerprinted JS/CSS) → asset bytes, `Cache-Control: public, max-age=31536000, immutable`
- `GET /favicon.ico`, other top-level static files → asset bytes, `Cache-Control: public, max-age=3600`
- `GET /<any-unmatched-path>` → embedded `index.html`, `Cache-Control: no-cache` (Vue Router fallthrough)
- `GET` requests whose path starts with one of the reserved API/WS prefixes (`/ws`, `/health`, `/api`, `/catalog`, `/maps`, `/matches`, `/lobbies`) are never served as SPA fallthrough — they 404 if they didn't match an actual API route. The reserved-prefix list lives in `internal/embedded/handler.go`; add to it whenever a new top-level API route is introduced.
- Non-GET / non-HEAD methods return `405 Method Not Allowed`.

## Environment variables

| Var                   | Purpose                                                                |
| --------------------- | ---------------------------------------------------------------------- |
| `CORS_ALLOWED_ORIGIN` | CORS allow-list origin. Defaults to `http://localhost:5173`.           |
| `WEBRTS_PROFILES_DIR` | Where the profile manager reads/writes player profile JSON files.      |
