> **Phase legend.** `[P1]` = Phase 1 (foundation): packaged desktop build runs offline single-player + Direct-connect MP. `[P2]` = Phase 2 (Steamworks): SDK init, achievements, lobby create/join, Networking Sockets MP. Phase 3 (signing, `steamcmd`, store assets) is a separate change.

## 1. [P1] Go server: embed_spa build tag & SPA serving

- [ ] 1.1 Create `server/internal/embedded/` package with a `Handler()` constructor that returns an `http.Handler` serving a `dist/` `embed.FS` provided by the caller.
- [ ] 1.2 Add an `embed_spa`-tagged file in `server/cmd/api/` that declares `//go:embed client/src/game-portal/dist/*` and wires the resulting `embed.FS` into the server's router at `/` (after the existing API/WS routes).
- [ ] 1.3 Add a no-tag counterpart file in `server/cmd/api/` that wires no SPA handler (preserves current API-only behaviour) so non-tag builds compile without `dist/` existing.
- [ ] 1.4 Implement client-side route fallthrough: any GET request that does not match an API route, embedded asset, or known top-level path (`/ws`, `/health`, `/api`, `/catalog`, `/maps`, `/matches`, `/lobbies`) returns the embedded `index.html`.
- [ ] 1.5 Add `server/internal/embedded/handler_test.go` with a `testdata/dist/` fixture (stub `index.html` and `assets/`); test routes for root, asset, fallthrough, and that API/WS paths are not shadowed.
- [ ] 1.6 Document the new build tag in `server/README.md` (or create one if absent).
- [ ] 1.7 [P1] Implement cache-header policy in the embedded handler: `Cache-Control: public, max-age=31536000, immutable` for fingerprinted assets under `/assets/`, `Cache-Control: no-cache` for `index.html` (both the root route and the SPA-route fallthrough), `Cache-Control: public, max-age=3600` for other embedded assets. Add unit tests for each class using the existing `testdata/dist/` fixture.

## 2. [P1] Go server: free-port discovery, ready-line, stdin-EOF shutdown

- [ ] 2.1 Modify `server/cmd/api/main.go` to honour `WEBRTS_PORT` (env) and `--port` (CLI flag) with value `0` meaning kernel-assigned.
- [ ] 2.2 After the listener is bound, print exactly one line `NOMADS_READY url=http://127.0.0.1:<port> version=<git-sha>` to stdout (compile-time `version` from build flags).
- [ ] 2.3 Add a goroutine that reads stdin and triggers the existing shutdown path on EOF.
- [ ] 2.4 Add an integration test (`server/cmd/api/main_test.go`) that spawns the server binary with `WEBRTS_PORT=0`, scrapes the ready line, hits the port, then closes stdin and asserts the process exits within 5s.
- [ ] 2.5 Ensure existing `air` dev workflow continues to work unchanged (no `WEBRTS_PORT` set → defaults to `:8080`).

## 3. [P1] Go server: profile dir is set by env (verify, no code change expected)

- [ ] 3.1 Confirm `server/internal/profile/manager.go` still respects `WEBRTS_PROFILES_DIR` after any refactors in section 2.
- [ ] 3.2 Add a regression test asserting that when `WEBRTS_PROFILES_DIR` is set to a tempdir, the manager writes there and not under `./profiles`.

## 4. [P1/P2] Go server: SteamBridge interface & NoopBridge

> Tasks 4.1–4.3, 4.5, 4.6, 4.7 are Phase 1 (interface, NoopBridge, FakeBridge, achievement constants file). Task 4.4 (IPCBridge implementation) and live wiring of the Steam IPC handlers are Phase 2.

- [ ] 4.1 Create `server/internal/steam/` package.
- [ ] 4.2 Define the `SteamBridge` interface with methods `LocalPlayer() (LocalPlayer, error)`, `ReportAchievement(id string) error`, `OpenInviteOverlay(lobbyID string) error`, `RegisterTransport(t Transport) error` (Transport is from `pluggable-mp-transport`, section 6).
- [ ] 4.3 Implement `NoopBridge` with no-op / "unavailable" returns and a unit test asserting every method is safe to call.
- [ ] 4.4 Implement `IPCBridge` over a Unix-socket / named-pipe path read from `NOMADS_IPC_PATH`. Wire format: newline-delimited JSON, one request/response pair per call.
- [ ] 4.5 Add a `FakeBridge` test double that records calls; use it in subsequent sections.
- [ ] 4.6 Update `server/cmd/api/main.go` to construct `IPCBridge` if `NOMADS_IPC_PATH` is set and non-empty, else `NoopBridge`, and pass it to subsystems that need it.
- [ ] 4.7 Add `server/internal/steam/achievements.go` with at least one achievement constant (the smoke-test achievement) and a doc comment noting that this file is the single source of truth.
- [ ] 4.8 [P2] Implement `ReportAchievement` as fire-and-forget: bridge method performs a non-blocking send (`select { case ch <- req: default: <atomic drop-counter++> }`) onto a small buffered channel (size 64) and returns immediately; a dedicated writer goroutine drains the channel and performs the IPC write. On channel-close, drop pending writes with a single warning log. On full-channel drops, emit a single summary warning per batch (no more frequently than every 8 drops). Add a unit test that constructs a bridge whose writer goroutine is deliberately blocked, fires 1000 `ReportAchievement` calls in a tight loop, and asserts (a) every call returns under 10 µs and (b) the drop counter ends at 1000 - 64 = 936.
- [ ] 4.9 [P2] Implement per-call timeout (5 s) and request-id discard semantics for synchronous bridge methods (`LocalPlayer`, `OpenInviteOverlay`, `create_lobby`, `join_lobby`, etc.). On timeout return `steam_timeout`; mark the in-flight id discarded so late responses are ignored.
- [ ] 4.10 [P2] Enforce 1 MiB max line size on IPC reads (both Go and Rust sides); oversized lines are skipped to the next newline with a logged error; the channel survives.
- [ ] 4.11 [P2] Implement terminal-on-close semantics in `IPCBridge`: when the channel closes, the bridge transitions to "closed" state and every subsequent call returns `steam_channel_closed`. No reconnect attempt; no swap to `NoopBridge`.
- [ ] 4.12 [P2] Implement `steam_unavailable` distinction in `IPCBridge`: if the channel is open but the shell reports Steam isn't initialised, Steam-side calls return `steam_unavailable`. Add a test scenario for "user signs out of Steam mid-session."

## 5. [P1] Rust shell: scaffold the Tauri crate

- [ ] 5.1 Create `desktop/` directory with a Cargo workspace; pin Tauri v2 in `Cargo.toml`.
- [ ] 5.2 Add minimal `tauri.conf.json` configured for Windows/macOS/Linux targets and a single window.
- [ ] 5.3 Add a sidecar declaration that bundles the Go server binary as a Tauri resource.
- [ ] 5.4 Wire `pnpm/npm run tauri:dev` and `cargo tauri build` scripts.
- [ ] 5.5 Configure the `capabilities` section of `tauri.conf.json` to enumerate every IPC command this change introduces (`get_steam_player`, `report_achievement`, `open_invite_overlay`, `create_lobby`, `join_lobby`, `desktop_bridge_ready`, `append_log`, `open_logs_directory`, settings IPCs). No wildcard or "allow-all" permissions.
- [ ] 5.6 Configure the Windows installer to use the Evergreen WebView2 bootstrapper (Tauri's default install-on-demand mode); add a test-launch smoke check in the playtest checklist for a Windows VM without WebView2 pre-installed.
- [ ] 5.7 Register the Windows installer as a handler for `steam://joinlobby/<appid>/<lobby>` URLs (or verify Steam's standard launch-with-lobby invocation reaches the argv parser); add a manual playtest entry for the cold-launch friend-invite flow.
- [ ] 5.8 [P1] Enumerate Tauri built-in plugin permissions in the capability set: `core:event:allow-listen`, `core:event:allow-unlisten`, `shell:allow-open` scoped to the startup-resolved logs directory path (no globs), and any window-management permissions actually invoked by the SPA. Explicitly omit `fs:*`, `http:*`, `process:*`. Add a build-time check (or a unit test that loads `tauri.conf.json`) asserting none of the forbidden permission prefixes appear.

## 6. [P1] Rust shell: Go child supervisor + ready-line handshake

- [ ] 6.1 Implement spawning the Go binary as a child process with stdin captured, stdout line-buffered, stderr ring-buffer-captured (last 200 lines).
- [ ] 6.2 Parse stdout lines for `NOMADS_READY url=… version=…`; resolve a future when seen; reject with the captured stderr if the child exits or 10 s elapses without seeing it.
- [ ] 6.3 Implement window-close handler that closes the child's stdin and waits up to 5 s for clean exit, then sends SIGKILL.
- [ ] 6.4 Implement child-crash detection: if the child exits unexpectedly while the window is open, raise a Tauri event the SPA renders as the "Server crashed — click to restart" dialog.
- [ ] 6.5 Add `cargo test` coverage for the ready-line parser and the stdin-EOF handshake state machine.
- [ ] 6.6 [P1] Implement the SP "run lost on child crash" UX: on respawned-child reconnect, the SPA navigates to the main menu and renders a one-time toast ("The game crashed and your current run could not be recovered. Profile progress is intact."). Wire via a Tauri event `child_respawned_after_crash` that the shell raises after a successful respawn. Add a Vitest covering the toast render and the navigation.

## 7. [P1] Rust shell: per-OS user-data dir + writable check + settings + log dir + legacy profile migration

- [ ] 7.1 Implement an OS-specific resolver: `%APPDATA%/Nomads/` (Windows), `~/Library/Application Support/Nomads/` (macOS), `~/.local/share/Nomads/` (Linux).
- [ ] 7.2 Ensure the `profiles/` subdirectory exists; pass its absolute path as `WEBRTS_PROFILES_DIR` to the child.
- [ ] 7.3 Implement a write-touch check at startup; on failure, show a pre-launch error modal with the path and underlying OS error before spawning the child.
- [ ] 7.4 Ensure the `logs/` subdirectory exists; pass its absolute path to the shell's logging layer and the Go child via env var (e.g. `WEBRTS_LOGS_DIR`).
- [ ] 7.5 Implement `settings.json` load + save under the user-data directory; expose typed `desktopBridge` IPCs for `getSettings()`, `setSettings(partial)`, with the forward-compatible "preserve unknown keys" behaviour from the spec.
- [ ] 7.6 Implement legacy `./profiles/` → `<userdata>/profiles/` migration on first launch: only when the userdata `profiles/` directory does not yet exist AND a legacy directory is found adjacent to the Go server binary. Copy files (don't move), log the migration, run exactly once.
- [ ] 7.7 Add `cargo test` coverage for the migration once-only behaviour and for the writable-check failure path.
- [ ] 7.8 [P2] Implement profile-identity selection: when the IPC-backed bridge reports a real Steam ID, the profile id used by Go reads/writes is the Steam ID; when the bridge is `NoopBridge` or reports `steam_unavailable`, the SPA's `localStorage` UUID is used. Wire through to `WEBRTS_PROFILES_DIR` reads/writes.
- [ ] 7.9 [P2] Implement one-time legacy-UUID → Steam-ID migration prompt: on first Steam launch where a legacy UUID profile exists but no Steam-ID profile does, SPA shows a modal with "Use my existing progress" / "Start fresh"; record the choice in `settings.json` so it does not re-appear.
- [ ] 7.10 [P1] Move player-id storage from SPA `localStorage` into `settings.json` via `desktopBridge` for any build running inside the Tauri shell (`window.__TAURI__` present). On the first packaged-build run where `settings.json` lacks a player-id and `localStorage` has one, copy the value across. The browser dev loop (`npm run dev`, no Tauri) continues to use `localStorage`. **Rationale (load-bearing for Phase 1):** the packaged build's `port=0` policy assigns a fresh port per launch and `localStorage` is keyed by `(scheme, host, port)`, so deferring this to Phase 2 means every Phase 1 playtester's profile resets on every relaunch. Add Vitest covering: shell-detected transition (copy from `localStorage` to `settings.json` on first run), browser-dev no-op path (no `settings.json` IPC attempted), packaged-build read-from-`settings.json` path on a second simulated launch with a different mock port.
- [ ] 7.11 Implement `settings.json` single-writer protocol: SPA is the only writer; shell sends window-state events via `apply_window_state` IPC, which the SPA debounces and persists. Hold window-state events in shell memory until SPA signals `desktop_bridge_ready`, then flush.
- [ ] 7.12 [P1] Implement the window-close settings-flush handshake: on window close, the shell sends a final `apply_window_state`, then awaits a `settings_persisted` IPC from the SPA for up to 2 seconds, then proceeds to close the Go child's stdin. The 5-second Go-child grace period (task 6.3) runs AFTER this 2-second flush phase, not in parallel. Add a Vitest covering: ack received within 2 s → fast shutdown path; ack not received within 2 s → proceed anyway path; assert in both cases that the shell does not close stdin before the timeout/ack.
- [ ] 7.13 [P1] Add an integration test for the writable-check failure path (task 7.3): point the userdata resolver at a deliberately read-only directory; assert the shell displays the pre-launch error modal with the resolved path and OS error, AND does not spawn the Go child.

## 8. [P2] Rust shell: Steamworks init + IPC channel

- [ ] 8.1 Add `steamworks-rs` as a dependency. At shell startup, **before any other Steam SDK call**, invoke `SteamAPI_RestartAppIfNecessary(<appid>)`; if it returns true, exit the shell immediately (no window opened, no Go child spawned, no Steamworks teardown) so Steam can relaunch the app under its management with the appid correctly attributed. If it returns false, proceed to `SteamAPI_Init`; log a warning and continue on init failure. Add a unit test for the relaunch-true branch (mock the SDK call and assert the shell exits without spawning the Go child); add a doc comment at the call site explaining why this MUST be the first SDK call.
- [ ] 8.2 Spawn a background thread that pumps Steam callbacks at the standard cadence; ensure no callback handler does blocking I/O.
- [ ] 8.3 Create the per-OS IPC channel (named pipe on Windows, Unix socket on macOS/Linux), pick a fresh path per launch, pass it as `NOMADS_IPC_PATH`.
- [ ] 8.4 Implement an IPC message dispatcher: parse newline-JSON requests from the Go child, dispatch to handlers, write JSON responses with matching request ids.
- [ ] 8.5 Implement handlers for: `get_steam_player`, `report_achievement`, `open_invite_overlay`, `create_lobby`, `join_lobby`. If Steamworks is uninitialised, every Steam-side handler returns a documented "steam_unavailable" error code.
- [ ] 8.6 Add `cargo test` coverage for the dispatcher's framing and error paths (with a mocked Steam trait).
- [ ] 8.7 Document and enforce the dev-only `desktop/steam_appid.txt` placement: add to `.gitignore`, add a README note explaining when developers need it, ensure the file is NOT bundled into release artifacts (the appid for release builds is set via Tauri config / `SteamAPI_RestartAppIfNecessary`).
- [ ] 8.8 [P2] Implement IPC channel ACLs: Windows named pipe created with a `SECURITY_ATTRIBUTES` DACL granting access only to the calling user's SID (no NULL DACL, no "Authenticated Users"). Unix socket created at `<userdata>/runtime/shell.sock` inside a `0700` directory with `umask 0177` so the socket file is `0600`. Linux abstract namespace sockets (first byte null) are forbidden. Add `cargo test` coverage that on Linux/macOS confirms the resulting socket file mode and parent dir mode; on Windows confirms the pipe is unreachable from a process running as a different local user (skip the test if a second user account isn't available in the CI environment, but keep the assertion path).
- [ ] 8.9 [P2] Add an integration test for Steam-init-failure propagation end to end: launch the shell with Steamworks deliberately broken (e.g., no `steam_appid.txt` in dev, or mocked `SteamAPI_Init` returning false); assert the shell logs the failure, the IPC channel still opens, the Go server constructs the `IPCBridge` (per task 4.6), `desktopBridge.getSteamPlayer()` returns null, and the SPA's lobby UI hides Steam-mode entries.

## 9. [P2] Rust shell: single-instance lock + connect_lobby argv

- [ ] 9.1 Enable Tauri's single-instance plugin for the shipped build configuration.
- [ ] 9.2 Add a dev/test build configuration with the single-instance lock disabled.
- [ ] 9.3 Parse argv at startup for `+connect_lobby <id>`; store the id; defer dispatching the `join_lobby` IPC until the SPA invokes a new `desktop_bridge_ready` Tauri command.
- [ ] 9.4 [P2] Add an integration test for the single-instance second-launch behaviour: launch the shipped-config shell once; while it is running, launch it again; assert (a) the existing window is brought to the foreground, (b) no second Go server child is spawned, (c) the second shell process exits cleanly.

## 10. [P1] Go server: pluggable WS hub transport

- [ ] 10.1 Introduce a `ws.Transport` interface (Read, Write, Close, PeerIdentity). The `PeerIdentity()` return type is a typed struct `PeerIdentity { Kind PeerKind; Addr string }` with `PeerKind` as a small enum (`PeerKindWebSocket`, `PeerKindSteam`, `PeerKindFake` for tests). `Addr` is transport-opaque: `host:port` for WebSocket, decimal SteamID64 for Steam. Provide a `String()` method that returns `"<kind>:<addr>"` for log lines. Hub code uses the struct for diagnostics only — it MUST NOT branch on `Kind` for game logic; any per-transport behaviour lives inside the `Transport` implementation.
- [ ] 10.2 Refactor existing WebSocket-over-TCP handling to implement `Transport`; update the hub to address clients by `Transport`, not by `*websocket.Conn`.
- [ ] 10.3 Audit hub-internal code for any remaining transport-specific branches; either remove or move into the WebSocket implementation.
- [ ] 10.4 Extend hub tests with a `FakeTransport`; assert message routing, broadcast, and disconnect behave identically regardless of underlying transport.
- [ ] 10.5 Add a `transport_test.go` documenting (in test form) that protocol bytes sent over different transports are byte-identical.
- [ ] 10.6 Add a single-player regression-guard test: run a deterministic scripted single-player scenario against the pre-refactor binary (baseline captured before this task lands) and post-refactor binary; assert byte-identical client↔server WS *payload* streams (not frame-level, which would include timing-dependent control frames). The baseline goes into `server/internal/ws/testdata/sp_baseline_*.bin`. Land this refactor as a single isolated PR so the rollback path is mechanical.
- [ ] 10.7 Pre-refactor capture PR: a preparatory PR (lands BEFORE the Transport refactor PR) captures the SP baseline byte streams from the deterministic scripted scenario and commits them to `server/internal/ws/testdata/sp_baseline_*.bin`. The refactor PR's CI uses these committed bytes as the regression target.
- [ ] 10.8 [P1] Add a Direct connect end-to-end integration test that runs on every PR: spawn two locally-running Go servers, connect them via `transportbridge`, run one scripted match end to end, assert clean disconnect. This guard prevents Phase 2 PRs (Steam Sockets, lobby sync) from silently regressing Direct-connect.

## 11. [P2] Go server: transportbridge proxy for joiners

- [ ] 11.1 Create `server/internal/transportbridge/` package.
- [ ] 11.2 Implement a proxy that pipes the joiner's local SPA WebSocket to a parent `Transport` (Steam Sockets or remote WebSocket), without modifying message bytes.
- [ ] 11.3 Implement close/error propagation in both directions; ensure either side closing triggers the joiner's normal WebSocket disconnect path.
- [ ] 11.4 Add unit tests with two `FakeTransport`s back-to-back; assert lossless byte forwarding and bidirectional close propagation.
- [ ] 11.5 Wire the proxy into the WS hub: a connection that opts into "remote host" mode is registered with the local hub as a proxy client whose upstream is the parent `Transport`.

## 12. [P2] Go ↔ Rust: Steam Networking Sockets transport bridge

- [ ] 12.0 In the Rust shell's Steam Sockets transport: ALL `SendMessageToConnection` calls SHALL use `k_nSteamNetworkingSend_Reliable` (reliable + ordered). Add a `cargo test` (or doc comment + grep-able marker) asserting no other send flag is used in this transport path.

- [ ] 12.1 In the Rust shell: implement a Steam Networking Sockets transport that, given a remote `SteamID`, opens a connection, manages its lifecycle, and forwards bytes to/from a paired Unix-socket/named-pipe leg owned by the Go child.
- [ ] 12.2 In the Go server: implement the corresponding `Transport` that reads/writes that leg; register it with the WS hub when the shell signals `lobby_joined`.
- [ ] 12.3 Add an integration test that uses two locally-running Go servers + a mocked Steam transport in the middle to verify a full lobby-join + state-broadcast loop without any real Steam SDK calls.

## 13. [P1] Direct connect MP UI + Go side

- [ ] 13.1 Add a server-side "expose listener on non-loopback" mode toggled by an HTTP API call from the SPA; default off. Mechanism is accept-time gating on a single `0.0.0.0` listener (per design D11): the listener binds `0.0.0.0` unconditionally at server startup, and the WS upgrade handler consults the toggle state — when off, non-loopback peers are rejected with HTTP 403 at upgrade time; when on, only the unconditional Origin check (task 13.11) applies. Listener rebind on toggle change is explicitly rejected. The toggle state lives in the Go server's in-memory state; persistence across restarts is not required (the SPA UI shows the current state and the user reapplies on relaunch).
- [ ] 13.2 Add SPA UI in the lobby screen: "Allow LAN/Internet connections" toggle (host-side) with a one-line note "Anyone with this address can join — share like a Discord link"; "Direct connect" entry with a `host:port` text field (joiner-side).
- [ ] 13.3 On host toggle on, surface the host's reachable address(es) in the SPA for the user to copy/share.
- [ ] 13.4 On joiner submit, open a WebSocket to the entered `host:port` from the local Go server (using the new `transportbridge`) with a 5-s connect timeout; on success, register as a proxy client.
- [ ] 13.5 Surface DNS / refused / timeout failure classes in the SPA UI with descriptive copy.
- [ ] 13.6 Add an integration test for the host-side toggle (off rejects non-loopback; on accepts) and a manual playtest entry to the checklist.
- [ ] 13.7 Implement the transport-bridge handshake: joiner's Go server exchanges compiled `version` with host's Go server before registering the connection as a proxy client; on mismatch, close the transport with the documented version-mismatch code so the SPA renders the same "Build mismatch — please restart" modal as the SPA/server case (failure mode #9 / task 17.1).
- [ ] 13.8 Confirm in the README / lobby UI copy that NAT traversal is not attempted (no UPnP/STUN/hole-punching); the user is responsible for reachability. No code task — documentation only.
- [ ] 13.9 Enumerate host non-loopback IPv4 addresses in the SPA host UI: sort with Tailscale CGNAT (`100.64.0.0/10`) first, then RFC1918 ranges, then everything else; preselect the top address; render a "copy" affordance per address.
- [ ] 13.10 Toggle-off-while-connected behaviour: when the host disables "Allow LAN/Internet connections" while non-loopback joiners are connected, existing connections are NOT terminated; only new attempts are refused. Host UI shows a one-line clarification ("Toggle does not end this lobby — close the lobby to do that.").
- [ ] 13.11 [P1] Replace `CheckOrigin: func(_ *http.Request) bool { return true }` at [server/internal/ws/handlers.go:32](../../server/internal/ws/handlers.go#L32) with an Origin-validating implementation: accept upgrades with no `Origin` header (non-browser clients including `transportbridge`); accept upgrades whose `Origin` host is a loopback host (`127.0.0.1`, `localhost`, `[::1]` — any port); reject all other Origins with HTTP 403 and a single rate-limited log line (so a misbehaving caller can't flood the log). The check is unconditional — it applies whether or not the listener is exposed on non-loopback, because the rule is correct in both modes and avoids a flag the upgrade handler would otherwise need to consult. Add unit tests for: (a) no-Origin → accept; (b) `Origin: http://127.0.0.1:54321` → accept; (c) `Origin: http://localhost:5173` → accept (dev workflow); (d) `Origin: https://malicious.example` → reject 403; (e) malformed `Origin` → reject 403. Add a Direct-connect end-to-end test asserting `transportbridge` Go-to-Go connections still succeed.

## 14. [P2] Steam invite MP UI + flows

- [ ] 14.1 Add SPA UI for "Friend invite (Steam)" host mode (creates a Steam lobby via `desktopBridge.openLobby`) and an "Invite friend" button that calls `desktopBridge.openInviteOverlay`.
- [ ] 14.2 Wire the IPC command `lobby_host` so the Go server creates a regular `LobbyManager` lobby when notified by the shell.
- [ ] 14.3 Implement the join path: shell join → `LobbyEnter_t` → open Steam Sockets → handoff to Go → Go registers the connection as a proxy client.
- [ ] 14.4 Hide the Steam host/join UI entries when `desktopBridge.getSteamPlayer()` returns null; show Direct connect only.
- [ ] 14.5 Implement Steam→Go lobby state sync: shell pumps `LobbyChatUpdate_t` and `LobbyDataUpdate_t` callbacks; for each event, send an IPC message that updates the Go-side `LobbyManager` lobby. SPA-driven mode/max-players changes go through Steam first (set lobby metadata), then sync into Go on the resulting callback.
- [ ] 14.6 Implement the `lobby_resync` IPC: if the shell detects mismatch between its view of the Steam lobby and the Go-mirrored state (e.g., after a Steam network blip), the shell sends a full-state resync message and the Go server replaces its mirrored state.
- [ ] 14.7 Implement the host-disconnect terminal state: on transport close (Steam or Direct), every joiner's SPA renders "Match ended — host disconnected" with a single "Return to menu" action. No "promote to host" option appears in any code path. Add scenarios to integration tests for host network drop and host intentional quit.
- [ ] 14.8 Pessimistic SPA UI for lobby metadata round-trips: when the host changes `mode` or `max_players`, the changed control shows a spinner / disabled state until the `LobbyDataUpdate_t` round-trip completes; no optimistic snap-to-new-value.
- [ ] 14.9 Steam Deck sleep / wake playtest entry: during the Phase 2 Deck playtest, exercise mid-match suspend-to-RAM → wake and document whether the WS transport survives, whether the host times out joiners during sleep, and any required UX (e.g., "Match ended — disconnected during sleep").

## 15. [P1] TypeScript bridge: `desktopBridge.ts`

- [ ] 15.1 Add `@tauri-apps/api` to `client/src/game-portal/package.json` dependencies.
- [ ] 15.2 Create `client/src/game-portal/src/services/desktopBridge.ts` exposing: `getSteamPlayer()`, `inviteFriend(lobbyId)`, `reportAchievement(id)`, `openLobby(opts)`, `joinLobby(lobbyId)`, `ready()`.
- [ ] 15.3 At runtime, probe `window.__TAURI__`; if absent, return a stub that no-ops or routes to existing HTTP equivalents where applicable.
- [ ] 15.4 Add Vitest tests covering: Tauri-present detection, no-op stub behaviour in browser dev, payload assertion for each invoke call.
- [ ] 15.5 Add an ESLint rule (or a code-review checklist note) prohibiting any file other than `desktopBridge.ts` from importing `@tauri-apps/api`.
- [ ] 15.6 Implement focus-loss / focus-regain handling: shell raises a Tauri event on window blur/focus; SPA pauses the simulation only when the current match is single-player, never in multiplayer.
- [ ] 15.7 Add SPA support / about screen surfacing the absolute `logs/` directory path plus an "Open logs folder" button that calls `desktopBridge.openLogsDirectory()`.
- [ ] 15.8 Configure Vite `define` injection in `vite.config.ts`: `__APP_VERSION__` resolves at build time with this priority order — (1) the `NOMADS_VERSION` env var if set and non-empty; (2) the short git SHA from `git rev-parse --short HEAD` if `git` is available AND the build runs inside a git checkout; (3) the literal string `"unknown"`. `npm run dev` and `tauri:dev` use the literal `"dev"` explicitly. The Go binary uses the same priority order via `-ldflags "-X main.version=..."`. Wire the SPA's first WS hello to include this value. Add a unit test that simulates each of the four resolution paths (env override, git-checkout-with-git, no-git-no-env, dev-mode) and asserts the resulting `__APP_VERSION__`.
- [ ] 15.9 Implement Vue Router unknown-route handler: render a "Page not found — Return to main menu" view with an explicit button, not a silent redirect.

## 16. [P2] Achievements: smoke-test wiring

- [ ] 16.1 Pick one in-game event for the smoke-test achievement (e.g., first wave cleared) and call `steamBridge.ReportAchievement(...)` from the relevant simulation code.
- [ ] 16.2 Configure the smoke-test achievement in the Steam dashboard (operator task — note it in the playtest checklist, not code).
- [ ] 16.3 Add an integration test using `FakeBridge` asserting the call is fired exactly once per qualifying event per run. The smoke-test event ("first wave cleared") is naturally single-fire per run (the run state cannot clear its first wave twice) so the assertion is satisfied without an explicit dedup set; document this in a comment at the call site so future achievements added via multi-fire events know to add the per-run dedup set described in `steam-achievements` "Achievement-trigger idempotency lives at the appropriate layer."

## 17. [P1] Failure-mode handling polish

- [ ] 17.0 Document accept-loss for achievements when Steam is unavailable: surface as a comment in `server/internal/steam/achievements.go` and in the playtest checklist; explicitly do NOT implement a `pending_achievements.json` queue.

- [ ] 17.1 Implement the version-mismatch close code: SPA sends compiled version in its first WS hello; server compares with its own `version` from the ready line; on mismatch, server closes with a documented code that the SPA renders as the "Build mismatch — please restart" modal.
- [ ] 17.2 Implement the user-facing "Server crashed" modal in the SPA, triggered by a Tauri event from the shell (section 6.4).
- [ ] 17.3 Implement the Steam unavailable toast in the SPA, triggered when `desktopBridge.getSteamPlayer()` returns null.

## 18. [P1] Build & packaging

- [ ] 18.1 Add a `Makefile` (or PowerShell equivalent) that runs: `npm run build` → `go build -tags embed_spa` → `cargo tauri build`.
- [ ] 18.2 Verify packaged artifacts on Windows (.msi), macOS (.dmg unsigned), Linux (.AppImage); record any per-OS quirks. The macOS `.dmg` MUST be a universal binary covering arm64 and x86_64 for both the Tauri shell and the Go sidecar (`cargo tauri build --target universal-apple-darwin` plus Go cross-compile of both arches, joined via `lipo` where the Tauri tooling does not do this automatically). Confirm `lipo -info` on each binary inside the bundle lists both architectures. A single-arch `.dmg` is acceptable for local dev iteration only — never as a playtest artefact.
- [ ] 18.3 Confirm the Go binary is bundled as a Tauri sidecar resource (not a separate file the user could relocate).
- [ ] 18.4 Pin Tauri and `steamworks-rs` versions in `desktop/Cargo.toml` (no `*` or `^` ranges). Document the version-update policy in `desktop/README.md`.
- [ ] 18.5 [P2] Record minimum webkit2gtk version supported by the chosen Tauri pin (e.g., webkit2gtk 2.40+); document in `desktop/README.md` and in the Linux installer notes. Verify Steam Deck ships a compatible version; verify a current Ubuntu LTS does too.
- [ ] 18.6 [P2] Steamworks SDK license-compliance review: confirm no Steam SDK source files are redistributed in a form prohibited by the SDK license; confirm `steam_appid.txt` is not bundled into release artefacts; record the review in a checklist alongside the first Phase 2 packaged build.

## 19. [P1] CI

- [ ] 19.1 Add a CI job for `cargo test` and `cargo clippy` on the `desktop/` crate.
- [ ] 19.2 Add a CI job for `go test ./...` covering the new packages and the `embed_spa`-tagged build.
- [ ] 19.3 Add a CI job for Vitest covering `desktopBridge.ts` tests.
- [ ] 19.4 Leave packaging (`cargo tauri build`) as a manual or release-only CI job; not on every push.

## 20. [P1] Documentation & playtest checklist

- [ ] 20.1 Update `CLAUDE.md` (or `.claude/rules/AI_RULES.md`) with the rules: (a) no game logic in `desktop/`; (b) no Steamworks symbols in Go; (c) `desktopBridge.ts` is the only SPA file importing `@tauri-apps/api`; (d) IPC and shell are not on the tick path.
- [ ] 20.2 Create `docs/superpowers/specs/2026-05-17-standalone-desktop-app-playtest-checklist.md` populated from section 7.5 of the design doc; include the renderer-parity check on Steam Deck (task 21.1), the focus-loss pause behaviour check, and the WebView2-bootstrapper VM check from 5.6.
- [ ] 20.3 Add a short `desktop/README.md` explaining the dev workflow (`npm run tauri:dev`) and the packaging workflow; document the `desktop/steam_appid.txt` requirement for developing against an unreleased Steam appid.
- [ ] 20.4 Ensure the README of the repo root surfaces the existence of the desktop build target.

## 21. Renderer-parity gates (Phase 1 smoke + Phase 2 full)

- [ ] 21.0 [P1] Run a renderer-parity smoke check on a Steam Deck or SteamOS VM against the first complete Phase 1 packaged build: cold launch reaches main menu, at least one combat scene renders without obvious regressions vs. the Windows build. Capture one screenshot pair. Result documented in the playtest checklist BEFORE Phase 2 work begins. If the smoke check fails in a Linux-only way severe enough to block Deck shipping, escalate to a design change (reconsider shell choice) before Phase 2 work.
- [ ] 21.1 [P2] Run the full renderer-parity acceptance check on a Steam Deck or SteamOS VM. Document the result in the playtest checklist: cold-launch time, single-player run visual parity (yes/no with screenshots), sustained fps during mid-game combat.
- [ ] 21.2 [P2] Run a renderer-parity smoke check on a current macOS version (M-series silicon if available): cold launch reaches main menu, at least one combat scene renders without obvious regressions. Document in playtest checklist.
- [ ] 21.3 If renderer parity fails on either platform in a way that blocks shipping there, escalate to a design change before continuing further Phase 2 work.

## 22. [P1/P2] Diagnostics logging

- [ ] 22.1 [P1] Shell-side logger: writes `<timestamp>-shell.log` into `<userdata>/logs/` for each launch. Includes startup phase, IPC events, Go child supervisor events, Steam SDK init result (Phase 2 only — log "skipped" in Phase 1).
- [ ] 22.2 [P1] Go-side log tee: shell tees the Go child's stdout/stderr (after consuming the `NOMADS_READY` line) into `<timestamp>-server.log`.
- [ ] 22.3 [P1] SPA log buffer + flush: in-SPA ring buffer collects log entries; flushes to the shell via `desktopBridge.appendLog(entries)` periodically and on window close. Errors in the SPA's error boundary flush synchronously before unmount.
- [ ] 22.4 [P1] Log content rules: enforce in code-review checklist that logs contain no per-tick simulation data, no raw game-state snapshots, no Steam auth tickets. Steam ID and persona name are allowed.
- [ ] 22.5 [P1] Rotation by run + 200 MB cap: on every launch, the shell deletes oldest complete run-triples (all three files sharing a timestamp) until the `logs/` directory is at or below 200 MB. Files from the current run are never deleted.
- [ ] 22.6 [P1] `desktopBridge.openLogsDirectory()` IPC that opens the `logs/` dir in the OS file manager; surfaced in the SPA's support / about screen (see 15.7).
- [ ] 22.7 [P1] Install a Rust panic hook in the shell that, on panic, writes a final log entry to the current `<timestamp>-shell.log` describing the panic location and message; flush and close before process exit. Explicitly does NOT cover native segfaults — those are OS-level crash dumps, out of scope.
- [ ] 22.8 [P2] Gate `transportbridge` hop-counter / forward-time annotations behind the existing debug-overlay toggle; display in the in-SPA debug overlay only. Never write these values to log files or send them to a remote endpoint.
- [ ] 22.9 [P1] Document playtest-build OS workarounds in the playtest checklist (created in 20.2): Windows SmartScreen "More info → Run anyway"; macOS Gatekeeper `xattr -d com.apple.quarantine ...` or right-click → Open; Linux `chmod +x` for `.AppImage`. Make clear these are expected for Phases 1 and 2 and resolved by Phase 3 signing.
- [ ] 22.10 [P1] README note (in `desktop/README.md` and root README): running `tauri:dev` and `air` simultaneously will collide on port 8080 unless `WEBRTS_PORT` is set for the `tauri:dev` Go child; document the workaround.

## 23. Non-goals — explicit acknowledgement in code review

These are not implementation tasks; they are guardrails to keep us from accidentally taking them on. Reviewers SHALL reject PRs that add scope from this list without a separate change proposal:

- Host migration in MP (joiner promotion to authoritative simulator).
- NAT traversal for Direct connect (UPnP, STUN, TURN, hole-punching).
- Gamepad / Steam Input mapping (a separate future change covers Steam Deck Verified).
- Lobby chat / voice chat / in-game text chat.
- Per-session passcode authentication on Direct connect (a possible follow-up enhancement, but not in this change).
- Code signing, notarisation, `steamcmd` upload pipeline, Steam Cloud depot config (Phase 3, separate change).
