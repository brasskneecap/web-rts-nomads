# Standalone Desktop App (Steam-ready) — Design

**Date:** 2026-05-17
**Status:** Approved, ready for implementation planning
**Scope:** Phase 1 (desktop shell foundation) + Phase 2 (Steamworks integration). Phase 3 (signing / `steamcmd` upload / store assets) is a separate spec.

## 1. Purpose

The game today runs in a web browser against a local Go server. The end goal is to ship on Steam as a standalone desktop application that:

1. Installs and runs like any other Steam game (no separate server, no browser, no terminal).
2. Stores saves in an OS-standard user-data directory.
3. Supports single-player offline.
4. Supports player-hosted multiplayer via two paths:
   - **Direct connect (host IP:port)** — works without Steam; used for closed playtests and as an always-available fallback in the shipped game.
   - **Steam friend invite (Matchmaking + Networking Sockets)** — primary Steam UX once Phase 2 is complete.
5. Targets Windows, Linux (including Steam Deck), and macOS.

This spec defines the architecture, components, data flow, failure handling, and testing strategy needed to reach that state without rewriting any of the existing simulation, AI, or rendering code.

## 2. Non-goals

- Steam Cloud depot configuration, code signing, notarisation, `steamcmd` upload pipeline — all Phase 3.
- A central matchmaking server, NAT-traversal relay, or P2P infrastructure beyond what Steam provides.
- Replacing the existing simulation, WebSocket protocol, profile schema, or any AI/combat code.
- Auto-update outside Steam. Non-Steam builds are dev/test artifacts only.
- Anti-cheat. Host is authoritative; the threat model for player-hosted MP is "play with friends."

## 3. High-level architecture

The release artifact is a single installable desktop app per OS (`.msi` / `.dmg` / `.AppImage`). At runtime it is three cooperating processes inside one Steam app:

```
┌────────────────────── Tauri shell (Rust) ─────────────────────────┐
│   • Window + system webview                                       │
│   • Owns Steamworks SDK (init, callbacks, lifecycle)              │
│   • Owns Steam Networking Sockets (P2P transport, NAT relay)      │
│   • Owns Steam Matchmaking (lobby discovery / friend invites)     │
│   • Spawns + supervises the Go server child process               │
│   • Exposes IPC commands to the SPA via Tauri's invoke() bridge   │
└──────────────────┬───────────────────────────────┬────────────────┘
                   │ local IPC                     │ webview navigates to
                   │ (Unix socket / named pipe)    │ http://127.0.0.1:<port>
                   ▼                               ▼
   ┌─────── Go server (child process) ──────┐   ┌── Vue 3 SPA (in webview) ──┐
   │  • Same authoritative simulation       │   │  Same client code as today  │
   │  • Same HTTP/WS API                    │   │  Same fetch/WS calls        │
   │  • Now also embeds the built SPA       │   │  Plus a thin Tauri-IPC      │
   │    via embed.FS and serves it          │   │  wrapper for Steam-only     │
   │  • Reads profile from OS user-data dir │   │  features (achievements,    │
   │  • Binds to a free localhost port      │   │  friend invites, etc.)      │
   │  • Pluggable transport: WebSocket      │   └─────────────────────────────┘
   │    today, Steam Net Sockets when shell │
   │    relays a peer connection            │
   └────────────────────────────────────────┘
```

Three load-bearing decisions:

1. **The Go server keeps its existing HTTP/WS API; nothing client-side changes for local single-player.** The webview navigates to `http://127.0.0.1:<port>` and the SPA runs unmodified.
2. **All Steam SDK calls live in the Rust shell, not in Go.** Go talks to the shell over a small local IPC for the handful of operations that need Steam: "fetch the local Steam ID + persona," "report achievement," "relay this packet to peer X." Go stays Steam-agnostic and runs without Steam (critical for tests, CI, headless dev).
3. **MP transport is pluggable.** Default transport is raw WebSocket — used for dev, CI, LAN play, "Direct connect" online play, and as the always-available fallback in the shipped Steam build. Steam Networking Sockets is registered as an additional transport when the shell hands the Go server a peer-connection handle. The game-state protocol stays identical; only the byte pipe underneath changes.

## 4. Components & module boundaries

Five new/changed components, each with one clear purpose and a well-defined interface.

### 4.1 `desktop/` — Tauri shell (new, Rust)

- One small Rust crate at `desktop/` in the repo root.
- Responsibilities: open the webview window; spawn and supervise the Go server child process; own Steamworks init/shutdown; expose Tauri `#[command]` functions to JS; pump Steam callbacks on a background thread; relay Steam Networking Sockets packets to the Go server.
- Hard boundary: **no game logic, no profile logic, no map data.** The shell is a window + a Steam wrapper + a process supervisor.
- Build artifacts: `nomads.exe` (Windows), `Nomads.app` (macOS), `nomads.AppImage` (Linux). Each contains a bundled Go binary as a Tauri sidecar resource.

### 4.2 `server/internal/embedded/` — SPA embedding (new, Go)

- Tiny Go package that exposes `//go:embed dist/*` of the built Vue SPA as an `embed.FS` and a `http.Handler` that serves it.
- Used by `server/cmd/api/main.go` when a build tag (`embed_spa`) is set. Without the tag the server runs in "API only" mode just like today (so `air` dev loop is unchanged).
- Hard boundary: this is the only place `embed.FS` lives.

### 4.3 `server/internal/steam/` — Steam IPC bridge (new, Go)

- Tiny package with two responsibilities: open a local IPC channel to the Rust shell (Unix socket on Mac/Linux, named pipe on Windows) and provide a Go-side typed client for the shell's Steam operations.
- Surface: `SteamBridge` interface with methods like `LocalPlayer()`, `ReportAchievement(id)`, `OpenInviteOverlay()`, plus a `Transport` interface for Steam Networking Sockets that the WS hub can register as an additional transport.
- Has a `NoopBridge` implementation used when the shell is not present (dev, tests, CI). This is what lets the Go server still build/run standalone.

### 4.4 `client/src/game-portal/src/services/desktopBridge.ts` — Tauri IPC client (new, TS)

- Thin TS wrapper around `@tauri-apps/api/core`'s `invoke()`. Exposes typed functions: `getSteamPlayer()`, `inviteFriend()`, `reportAchievement(id)`, `openLobby()`, `joinLobby(id)`.
- At runtime, detects whether `window.__TAURI__` exists; in the dev browser it returns a stub that no-ops or routes to the Go server's existing HTTP for the local-only equivalent. This keeps `npm run dev` working in a normal browser.
- Hard boundary: this is the **only** client-side file that imports `@tauri-apps/api`. The rest of the SPA imports `desktopBridge`.

### 4.5 Port discovery + lifecycle (small additions to `server/cmd/api/main.go`)

- Accepts `--port 0` / `WEBRTS_PORT=0` for "pick a free port."
- Prints the chosen URL on stdout in a single machine-readable line: `NOMADS_READY url=http://127.0.0.1:54321 version=<git-sha>`.
- Shuts down cleanly on stdin EOF, so the Rust parent killing the pipe is a clean termination.
- The Rust shell reads that line from the child's stdout to know what URL to load and when the child is ready.

### 4.6 Existing code that does NOT change

- Simulation, AI, combat, pathing.
- HTTP handlers, WS hub, lobby manager. The Steam transport plugs into the WS hub through a new transport interface; it does not replace anything.
- Vue components, Pinia stores, composables. Only `desktopBridge.ts` is new; existing `fetch()` and WS code continues to work against `http://127.0.0.1:<port>`.

### 4.7 Cross-cutting rule

**Steam is optional at every layer.** Tests run without it. CI runs without it. `npm run dev` runs without it. The headless server binary runs without it. Only the packaged `tauri build` artifact links Steamworks.

## 5. Data flow & lifecycle

### 5.1 Cold start (single player or any standalone launch)

```
1. User launches Nomads from Steam (or double-clicks the app outside Steam).
2. Tauri shell starts.
   ├─ Initializes Steamworks (if Steam is running). Records steam_id, persona, etc.
   │  If init fails, shell logs a warning and continues — game still runs offline.
   ├─ Determines OS user-data dir:
   │     Windows: %APPDATA%/Nomads/
   │     macOS:   ~/Library/Application Support/Nomads/
   │     Linux:   ~/.local/share/Nomads/         (also used by Steam Cloud sync)
   ├─ Spawns the Go server binary as a sidecar child process with env:
   │     WEBRTS_PROFILES_DIR = <userdata>/profiles
   │     WEBRTS_PORT         = 0   (free port)
   │     NOMADS_IPC_PATH     = <named-pipe or unix-socket path>
   │  (CORS_ALLOWED_ORIGIN is not set in the shell case: the webview loads
   │  the SPA from the Go server's own origin, so all requests are
   │  same-origin and the existing CORS allow-list does not apply.)
   ├─ Reads "NOMADS_READY url=http://127.0.0.1:54321 version=<sha>" from the
   │  child's stdout.
   └─ Loads the webview at that URL.
3. SPA boots inside the webview — identical code to today. fetch('/api/profile')
   and the WebSocket connect to the same local origin.
4. SPA calls desktopBridge.getSteamPlayer() to learn the local player's Steam
   identity. If Tauri IPC is absent (running in a normal dev browser), the
   bridge returns null and the existing localStorage UUID flow takes over.
```

### 5.2 Profile / save data flow

- Single source of truth: files under `<userdata>/profiles/<player-id>.json` — same format as today, just a different directory. No code changes inside `profile.Manager`; we set the path via the env var the manager already reads.
- When running under Steam, `<player-id>` is the Steam ID. When running outside Steam (dev, non-Steam launch), it remains the existing localStorage UUID. The profile file format itself is identical in both cases.
- Steam Cloud sync (Phase 3) attaches to this directory in the depot config — no code change.

### 5.3 Achievements

- Game logic stays where it is. When the simulation triggers an achievement-relevant event (e.g., "first run win"), it calls `steamBridge.ReportAchievement("ACH_FIRST_WIN")` from Go.
- If the bridge is the `NoopBridge`, the call is a no-op. If it's the real IPC bridge, it round-trips to the Rust shell, which calls `SteamUserStats::SetAchievement` + `StoreStats`.
- Achievement IDs are defined in one Go file (`server/internal/steam/achievements.go`) for grep-ability and to keep the Steam dashboard config in sync.

### 5.4 Multiplayer — direct connect (Option 1)

```
1. Player A opens "Host (Direct connect)" in the lobby UI.
2. With the "Allow LAN/Internet connections" toggle enabled, the Go server
   exposes its listener on the host machine's non-loopback interface
   (mechanism — restart on 0.0.0.0, or gate a 0.0.0.0 listener at accept
   time — is an implementation-plan decision; the spec only requires that
   when the toggle is off, no remote can reach the server).
3. SPA displays the host's local IP and port for the host to share manually
   (Tailscale, LAN, or port-forwarded WAN address).
4. Player B opens "Join (Direct connect)", enters host:port.
5. Joiner's Go server forwards the SPA's WebSocket through a new transport
   tunnel to the host's Go server. Host's server treats the joiner identically
   to a local WebSocket client.
```

This path has no Steam dependency and is the primary tester workflow before Steam access is available. It remains in the shipped build as a fallback.

### 5.5 Multiplayer — Steam friend invite (Option 3, Phase 2)

```
1. Player A clicks "Host (Steam)" in the SPA.
2. SPA calls desktopBridge.openLobby({mode, maxPlayers}).
3. Rust shell calls SteamMatchmaking::CreateLobby (friends-only, N slots).
4. Rust shell receives LobbyCreated_t callback; stores the Steam lobby ID.
5. Rust shell tells Go server via IPC: "you are the host of lobby <id>".
6. Go server creates a regular lobby in its existing LobbyManager.
7. SPA navigates to the lobby screen using the existing route.

Friend join:
1. Player A clicks "Invite friend"; Rust shell opens the Steam overlay UI.
2. Player B accepts. Steam either:
   (a) launches Nomads with command-line +connect_lobby <id>, OR
   (b) sends GameLobbyJoinRequested_t to the already-running shell.
3. Player B's shell calls SteamMatchmaking::JoinLobby(<id>).
4. On LobbyEnter_t, shell reads the host's Steam ID from lobby metadata,
   then opens a Steam Networking Sockets connection to that host.
5. Shell hands the resulting connection handle to the local Go server via IPC.
   Go server registers the connection with its WS hub as if it were a new
   WebSocket client — same client-id assignment, same message routing.
6. SPA on player B connects locally to its own Go server, which forwards
   game-state messages through the Steam-relayed transport to player A's
   Go server.
```

### 5.6 Joiner-as-proxy model

Each player runs a full local Go server. The host's server is authoritative for that match. **Joiners do not run the simulation.** Their local server exists only because the SPA was written to talk to a local server; it forwards the SPA's intents over the transport (WebSocket for direct connect, Steam Sockets for Steam invite) to the host's authoritative server and forwards state messages back. Forwarding logic lives in a new `server/internal/transportbridge/` package inside the WS hub — about 100 lines.

### 5.7 Shutdown

- Closing the window → Tauri sends shutdown signal → Rust shell closes stdin to Go child → Go child sees stdin EOF and calls existing shutdown path → Rust shell waits up to 5 s, then SIGKILL → Steamworks shutdown.
- Steam force-quit or Steam logout → same path via Tauri's window-close handler.

### 5.8 Crash recovery

- If the Go child dies while the shell is running, the shell shows a "Server crashed — click to restart" dialog and respawns the child. Profile data survives because it's on disk.
- If the shell dies, the Go child sees stdin EOF and exits cleanly.

## 6. Failure modes & error handling

| # | Trigger | User-visible behavior | Handling |
|---|---|---|---|
| 1 | Go server child fails to start (binary missing, permissions, panic during init) | Modal: "Game services failed to start" with "Copy diagnostic info" + "Retry"/"Quit" | Rust shell captures stderr to a ring buffer; surfaces it if exit occurs before `NOMADS_READY`. Start attempt has a 10-s timeout. |
| 2 | Go server crashes mid-session | "Connection to game services lost. Reconnecting…" banner; modal if reconnect fails. After successful respawn, SPA returns to the main menu — the in-progress match is not resumable. | Shell respawns child with same env; SPA's existing WS reconnect re-establishes against the fresh server. In-memory game state (active SP run, active MP lobby) is gone — the new server has no knowledge of it. Joiners in MP get dropped (host disappearance terminal state). For SP, the player's run is lost and they return to the main menu. Persistent progression (profile JSON on disk) survives. |
| 3 | Steamworks init fails (Steam not running, missing `steam_appid.txt`, no internet) | Non-blocking toast: "Steam features unavailable — running in offline mode." Single-player and Direct connect still work. | Shell records `steam_initialized = false`; `desktopBridge.getSteamPlayer()` returns null; lobby UI hides "Friend invite" tab. Go server gets the `NoopBridge`. |
| 4 | Port collision when LAN-host opt-in requests a specific port | "Port {n} is in use. Choose another." | Default loopback uses `port 0` (never collides). For LAN host with a user-supplied port, retry once, then fail explicitly. |
| 5 | Direct-connect join fails (wrong address, host firewall, host offline) | "Couldn't reach `host:port`. Check the address and that the host has 'Allow connections' enabled." with TCP error in details | 5-s connect timeout; no auto-retry. Distinguish DNS / refused / timeout in the message. |
| 6 | Steam friend join while game not launched (`+connect_lobby <id>` in argv) | Normal launch, then auto-routes into the lobby join flow once the SPA is ready | Shell parses argv at startup; defers join until SPA signals `desktopBridge.ready()`, then dispatches `join_lobby` IPC. |
| 7 | Steam Networking Sockets connection drops mid-game | "Reconnecting…" banner with countdown; drop after 30 s | Steam transport notifies WS hub of close; identical to a WebSocket disconnect. Joiner can rejoin via the lobby. |
| 8 | Profile dir not writable | "Couldn't save profile to `<path>`. Click for details." Game continues with in-memory profile. | Existing `profile.Manager` returns errors; HTTP surfaces them. New: shell does writable check on userdata dir at startup and surfaces it pre-launch. |
| 9 | Build mismatch (Steam delivers a partial update mid-launch — should not happen given Steam atomic depots) | "Build mismatch — please restart the game." | Go server emits `version` in `NOMADS_READY`; SPA includes its compiled version in its first WS hello; mismatch closes WS with a code rendered as the version-mismatch modal. |

### Non-issues (won't handle, by design)

- Multiple instances: shell uses Tauri's built-in single-instance lock. Second launch focuses the existing window.
- Anti-cheat / packet validation: out of scope. Host is authoritative; trusting friends is the explicit threat model.
- Auto-update: Steam handles it. Outside-Steam builds don't auto-update.

## 7. Testing strategy

### 7.1 Go-side unit tests

- `server/internal/steam/`: unit tests against the `NoopBridge` (no-op semantics) and against a `FakeBridge` (asserts IPC client serialises calls correctly). No real Steam SDK in tests.
- `server/internal/embedded/`: unit test that the embedded `http.Handler` serves a stub asset tree (use a `testdata/dist/` fixture rather than the real Vue build, so this test isn't gated on `npm run build`).
- WS hub transport-pluggability: extend existing hub tests with a fake "Steam-style" transport that implements the same `Transport` interface as the WebSocket transport. Assert message routing is transport-agnostic. **This is the most load-bearing new test.**
- Joiner-as-proxy forwarding (`server/internal/transportbridge/`): table-driven tests for "intent up, state down" round-trip with two fake transports.

### 7.2 Rust shell tests

- `cargo test` for the bits with logic: child-process supervisor, IPC framing, argv parsing for `+connect_lobby`, port-discovery handshake parser.
- The Steam SDK wrapper is mostly thin pass-through; gate those calls behind a `Steam` trait with a mock implementation for tests.

### 7.3 TS bridge tests

- Vitest unit tests covering: detects `window.__TAURI__`, returns the no-op stub in a normal browser context, returns the real-IPC client when Tauri is present. Mock `invoke()` to assert call payloads.
- No new component-level tests — the rest of the SPA imports `desktopBridge` and doesn't know about Tauri.

### 7.4 Integration tests (scoped tight)

- **Server-with-embedded-SPA:** `go test` that starts the server binary (built with `-tags embed_spa`) on a free port and asserts the `/` route returns the embedded `index.html`. Runs in CI; no shell involved.
- **Shell ↔ Go IPC contract:** a Rust integration test that spawns the real Go test binary, exercises every IPC method, asserts framing/serialization. Catches "I changed the IPC schema in one language and not the other."

### 7.5 End-to-end playtest checklist (manual)

Kept as `docs/superpowers/specs/2026-05-17-standalone-desktop-app-playtest-checklist.md` and updated as new failure modes are discovered:

- Cold launch from a packaged build (Win / Mac / Linux) → main menu visible within 5 s.
- Single-player run from start to finish; profile persists across relaunch.
- Direct connect host + remote joiner via Tailscale; play one full match; both clients see the same victory state.
- Steam path: launch via Steam, friend invite accepted from overlay, joiner enters game, full match completes.
- Force-kill the Go child; assert the shell shows the crash dialog and recovers on retry.
- Network drop mid-match (kill Tailscale on joiner); assert reconnect banner, drop after 30 s.

### 7.6 Determinism — what we're protecting

Simulation determinism is the project's load-bearing invariant ([AI_RULES.md](../../../.claude/rules/AI_RULES.md)). Nothing in this spec touches the tick loop or RNG seeding. Two explicit guardrails:

- The Rust shell and IPC bridge MUST NOT be on the tick path. They handle out-of-band concerns (Steam events, lifecycle). Game state messages flow through the existing WS hub.
- The Steam Networking transport is *transport only* — it delivers the same bytes the WebSocket would have. The host's authoritative simulation logic is unchanged.

### 7.7 Out of scope for this spec's tests

- Code signing / notarisation flows (Phase 3).
- Steam Cloud sync correctness (Phase 3; depot config).
- Performance benchmarks of the webview vs. the dev browser (assume parity; revisit only if a player reports a slowdown).
- Steamworks SDK behavior under stress (Steam itself is the system under test there, not us).

## 8. Dev workflow (preserved + extended)

The existing dev loop continues to work unchanged:

- `npm run dev` (port 5173) and `air` (port 8080) for browser-based iteration on UI and game logic. The Vite proxy continues to route `/ws`, `/api`, `/catalog`, `/maps`, `/matches`, `/lobbies`, `/health` to the Go server.

New, additive:

- `pnpm/npm run tauri:dev` (alias to `tauri dev`) — launches the shell against the Vite dev server, so changes to Rust shell code or Tauri IPC contracts can be iterated without packaging.
- `npm run build` (Vue build) followed by `cargo tauri build` to produce the release installer for the host OS.

## 9. Open questions deferred to the implementation plan

- Exact wire format for the local IPC channel: JSON-Lines over a Unix-socket/named-pipe vs. MessagePack vs. gRPC over a local-only listener. Recommendation: JSON-Lines (newline-delimited JSON) for simplicity and debuggability; revisit if message volume forces a binary protocol.
- Whether to bundle the Go binary as a Tauri "sidecar resource" (signed with the shell) or as a separate file in the install directory. Recommendation: sidecar resource so signing/notarisation covers it.
- Whether the joiner-as-proxy forwarding should preserve per-message latency telemetry (useful for diagnosing "Steam relay is slow" complaints). Recommendation: yes, add a small per-message hop counter and forward-time field, exposed in a debug overlay.

These are intentionally left for the implementation-plan stage so the spec doesn't lock in details that should be informed by writing the first prototype.

## 10. Cross-references

- AI / target-by-ID invariants: [`.claude/rules/AI_RULES.md`](../../../.claude/rules/AI_RULES.md). Nothing in this spec changes those rules.
- Existing dev script: [`dev.ps1`](../../../dev.ps1) (browser dev loop).
- Existing server entrypoint: [`server/cmd/api/main.go`](../../../server/cmd/api/main.go).
- Existing client entrypoint: [`client/src/game-portal/src/main.ts`](../../../client/src/game-portal/src/main.ts).
- Profile persistence (already filesystem-backed via `WEBRTS_PROFILES_DIR`): [`server/internal/profile/manager.go`](../../../server/internal/profile/manager.go).
- Prior spec convention reference: [`docs/superpowers/specs/2026-05-15-wave-upgrades-design.md`](2026-05-15-wave-upgrades-design.md).
