## Context

The current game runs in a web browser against a local Go server started by a dev script ([`dev.ps1`](../../../dev.ps1)). Vue 3 + Vite serves the client on port 5173 and proxies game traffic (`/ws`, `/api`, `/catalog`, `/maps`, `/matches`, `/lobbies`, `/health`) to the Go server on port 8080. The Go server owns all simulation, AI, combat, and pathing; the client renders server-sent state.

For the game to ship on Steam it must run as a single installable desktop app per OS that:
- starts itself with no terminal and no user-visible dev workflow,
- persists profiles in an OS-standard user-data directory,
- supports single-player offline,
- supports multiplayer both through Steam (friend invites, NAT traversal via Steam Networking Sockets) and through a Steam-independent "direct connect" path used for closed playtests before Steam access is available,
- preserves the current dev loop (`npm run dev` + `air`) unchanged for browser-based iteration,
- preserves the project's hard determinism invariants (see [`.claude/rules/AI_RULES.md`](../../../.claude/rules/AI_RULES.md)) — nothing in this work goes on the tick path.

The full architectural rationale, component boundaries, data-flow diagrams, failure-mode table, and testing strategy live in the approved design document at [`docs/superpowers/specs/2026-05-17-standalone-desktop-app-design.md`](../../../docs/superpowers/specs/2026-05-17-standalone-desktop-app-design.md). This file restates the load-bearing decisions and the risks; consult the design doc for the full diagrams and lifecycle sequences.

## Goals / Non-Goals

**Goals:**

- Ship a packaged desktop app per OS (`.msi` / `.dmg` / `.AppImage`) that wraps the existing Go server + Vue SPA without rewriting either.
- Keep all Steam SDK calls in a single Rust shell layer; the Go server stays Steam-agnostic and continues to build and run without Steam (for CI, tests, and the existing dev loop).
- Make the WS hub's transport pluggable so Steam Networking Sockets can be added as a second transport without touching simulation, AI, combat, or pathing.
- Provide a Steam-independent "Direct connect" MP path that works today (no Steamworks dependency) and stays in the shipped Steam build as the always-available fallback.
- Move profile storage to a per-OS user-data directory via the existing `WEBRTS_PROFILES_DIR` env var — no profile format change.
- Preserve the existing browser dev loop unchanged; the Tauri shell is additive, not replacing.

**Non-Goals:**

- Code signing, notarisation, `steamcmd` upload pipeline, Steam Cloud depot configuration, store-page assets, anti-cheat, auto-update outside Steam — all Phase 3, separate change.
- Replacing the existing simulation, AI, combat, pathing, profile schema, lobby state model, or WebSocket protocol.
- Central matchmaking server, custom NAT-traversal relay, or P2P infrastructure beyond what Steam Networking Sockets provides.
- Performance benchmarks of the system webview vs. the dev browser — assume parity; revisit only on a real complaint.

## Decisions

### D1. Shell choice: Tauri (Rust + system webview)

Tauri is the desktop wrapper. Considered alternatives:

- **Wails (Go shell, Webview).** Pro: single toolchain; no Rust. Con: Steamworks bindings for Go via cgo are sparse and noticeably weaker for Steam Networking Sockets specifically — and Networking Sockets is the load-bearing piece of the Phase 2 MP work. Rejected for the binding gap.
- **Electron (Chromium + Node).** Pro: most precedent, largest ecosystem. Con: ~150 MB install-size tax, weaker Networking Sockets coverage in `steamworks.js` than `steamworks-rs`, slower cold start. Rejected for shipped-game ergonomics on Steam.
- **Tauri (Rust shell + system webview).** Pro: smallest install (~15 MB extra), best Steam Deck (Linux/webkit2gtk) story, `steamworks-rs` is the most maintained Steamworks binding for our specific Networking Sockets needs. Con: adds Rust toolchain to dev/CI. **Chosen.**

The Rust footprint stays small (~500–1000 LOC of glue, no game logic in Rust) because the shell is just a window + a Steam wrapper + a child-process supervisor. The Rust toolchain is paid for once at package time.

### D2. Single bundled architecture, not separate server install

The packaged app starts an in-process child Go server bound to `127.0.0.1` on a system-assigned free port. The webview navigates to `http://127.0.0.1:<port>` and the SPA runs unmodified.

Considered alternative: collapse server and shell into one process via Wails-style Go ↔ JS bindings. Rejected — would require rewriting every client-side `fetch()` and WebSocket call into Tauri/Wails-style IPC, plus the WS hub's transport-pluggability already gives us a clean injection point for Steam Sockets without rewriting the protocol.

### D3. Steamworks lives only in the Rust shell

The Rust shell owns SDK init/shutdown, callback pumping, lobby create/join, achievements, and Networking Sockets. The Go server talks to the shell over a small typed IPC. Considered alternative: link the Steamworks C++ SDK into Go via cgo. Rejected — duplicate SDK binding work in two languages, harder cross-platform build (CGO + Steamworks libs per OS), and the cgo bindings would have to re-implement the callback-pump pattern the Rust crate already gives us for free. The clean rule for code review: a `*steam.Anything` symbol may never appear in the Rust crate's hot path, and a `steamworks::*` import may never appear in any Go file.

### D4. Pluggable WS hub transport, not protocol replacement

Steam Networking Sockets is registered as a second `Transport` implementation alongside the existing WebSocket-over-TCP. The same in-game protocol bytes flow through either. Considered alternative: have the Steam-MP path bypass the WS hub entirely and feed game state through a separate channel. Rejected — would create a second protocol surface to keep in sync with the WebSocket one, and would double the test matrix for every new server-sent event.

### D5. Joiner-as-proxy MP model

In multiplayer, every player still runs a local Go server, but only the host's server runs simulation. Joiners' servers act as transparent transport bridges: SPA intents flow up to the host through `transportbridge`, state messages flow back down. Considered alternative: joiners' clients connect directly to the host's server (skip the local Go process). Rejected — would require the client to have two distinct connection modes (local vs. remote), forking every API call and adding a "talking to remote host" branch through the entire SPA. The proxy model lets the SPA keep its single "always talks to local server" contract.

### D6. Free-port discovery via stdout handshake

The Go server binds `port = 0` (kernel picks a free port), then prints exactly one line to stdout: `NOMADS_READY url=http://127.0.0.1:<port> version=<git-sha>`. The Rust shell reads stdout line-by-line until it sees the prefix. Considered alternatives:

- Fixed port (e.g., 17400). Rejected — collisions are real, especially with player who run multiple instances or who have firewalls blocking that specific port.
- Write the URL to a file the shell polls. Rejected — adds filesystem race and a cleanup-on-crash story we don't need.
- Pre-allocate the port in Rust and pass it as a CLI arg. Rejected — the time-of-check/time-of-use gap is real on Windows in particular; let the kernel give the server its port directly.

### D7. Stdin-EOF shutdown signal

The Rust shell closes stdin on the Go child to trigger shutdown. The Go child blocks reading stdin in a goroutine that, on EOF, fires the same `os/signal` shutdown path the server uses today. Considered alternative: send a JSON shutdown command over the IPC channel. Rejected — pipes already give us a cross-platform "parent died, child should die" semantic with no protocol overhead. The IPC channel stays a domain channel (Steam events, achievements, transport handles), not a lifecycle channel.

### D8. JSON-Lines over Unix socket / named pipe for shell ↔ server IPC

Newline-delimited JSON objects (one message per line) is the wire format. Reasons: trivial to write, trivial to read in both Rust and Go, debuggable with `cat`/`tail`, no schema-compiler tax. Considered alternatives: MessagePack (rejected — premature optimisation for our ~100s-of-messages-per-session rate), gRPC over a local-only listener (rejected — too heavy for a process-local channel; codegen tax in two languages).

If message volume ever forces a switch, the transport is encapsulated inside `server/internal/steam/` and the Rust shell's `ipc` module — single point of change.

### D9. `embed_spa` build tag, default off

The Go server's `main.go` learns one new conditional: with `-tags embed_spa`, it mounts the embedded SPA `http.Handler` at `/`. Without the tag, the server is API-only — identical to today. Reasons: the existing `air` dev workflow stays exactly as it is; CI doesn't have to run `npm run build` before `go test`; the only build that needs the tag is `tauri build`.

### D10. Per-platform profile dir conventions, set by the shell

Shell determines the OS user-data dir at startup:

- Windows: `%APPDATA%/Nomads/profiles`
- macOS: `~/Library/Application Support/Nomads/profiles`
- Linux: `~/.local/share/Nomads/profiles`

These are passed to the Go child as `WEBRTS_PROFILES_DIR`. The existing `profile.Manager` already reads this env var ([`server/internal/profile/manager.go`](../../../server/internal/profile/manager.go)) — no Go code change required for the path.

### D11. Direct-connect MP uses accept-time gating on a `0.0.0.0` listener

When the user enables "Allow LAN/Internet connections," the server permits non-loopback peers to complete the WebSocket upgrade. Mechanism is fixed: the listener binds `0.0.0.0` at server startup, and the toggle state is consulted at WebSocket-upgrade time. When the toggle is off, the upgrade handler rejects any request whose remote address is non-loopback (HTTP 403); when the toggle is on, the existing Origin check (per `direct-connect-multiplayer` "WebSocket upgrade validates `Origin`") is the only remaining gate.

Considered alternative: listener rebind (close the `127.0.0.1` listener and open a fresh `0.0.0.0` listener on toggle-on, reverse on toggle-off). Rejected — closing the listener mid-session also closes the host's own loopback connection, the rebind has an obvious race window where in-flight connect attempts can fail or land on the wrong listener generation, and the port number is not guaranteed to be reusable (TIME_WAIT, OS firewall caches). Accept-time gating has none of these failure modes and combines cleanly with the unconditional Origin check landed in Patch 2.

The accept-time decision SHALL be a single guard at WS upgrade, not a check scattered across handlers; it sits adjacent to the Origin check at the same call site.

### D12. `desktopBridge.ts` is the only SPA file that imports `@tauri-apps/api`

All Tauri-specific code lives in one TS file. Everything else in the SPA imports `desktopBridge` and gets a typed interface. At runtime the bridge probes `window.__TAURI__` and returns either a real-IPC client or a no-op stub — meaning the SPA running in the browser dev loop never crashes on a missing `invoke()`.

### D13. Steam optionality is a build-time AND runtime property

- Build-time: `desktop/` (Rust) is the only artifact that links Steamworks libs. Go and TS builds never see them.
- Runtime: even inside the packaged Steam build, if `SteamAPI_Init` fails, the shell records `steam_initialized = false`, hands the Go server the `NoopBridge` interface, and the SPA's lobby UI hides the Steam-invite tab. Single-player and Direct-connect MP still work.

### D14. WebView2 bootstrap on Windows: install-on-demand (Evergreen)

The Tauri Windows build SHALL use the Evergreen WebView2 bootstrapper bundled into the installer, rather than shipping a fixed WebView2 runtime copy inside the app. Considered alternatives:

- **Fixed-version WebView2 runtime inside the app (~120 MB on disk).** Pro: bit-for-bit reproducibility, no first-run download. Con: bloats the Steam package by ~120 MB and we never benefit from upstream security patches. Rejected for a Steam-distributed game where Steam already covers delta-patching and we don't have a regulatory reason for a pinned runtime.
- **Skip-install (assume present).** Rejected — on a fresh Windows 10 install without recent updates, WebView2 may not be present and the app would fail to start with no remediation UI.
- **Evergreen bootstrapper.** Tauri ships a small bootstrapper that, on first launch, checks for the Evergreen WebView2 runtime and silently installs it from Microsoft if missing. **Chosen.**

### D15. Steam Deck renderer parity is a gate, not an assumption

Tauri was chosen in part for its Steam Deck story (webkit2gtk on SteamOS). However, the SPA renders the game via Canvas2D / WebGL inside the webview, and webkit2gtk's WebGL is not byte-identical to Chromium's. Before Phase 2 is declared complete, a renderer-parity check on a Steam Deck (or a SteamOS VM) MUST verify: cold launch reaches the main menu, a single-player run from start to end visually matches the Windows build at the level of "no obviously missing/garbled visuals" (full pixel-diff parity is not required), and 60 fps is sustained on Deck-equivalent hardware. If parity fails, the implementation plan must surface this back as a design change before further Phase 2 work — switching shells at that point is cheaper than shipping a broken Linux build.

### D16. Host disconnect in MP ends the match; no host migration

When the host disappears (network drop, crash, intentional quit) mid-match, the match ends for every joiner. Joiners see a single defined terminal state ("Match ended — host disconnected") with no choice to continue. Considered alternative: host migration (promote a joiner to authoritative simulator). Rejected — host migration in an authoritative-simulation RTS requires replicating the *full* tick-state to every joiner continuously, which we deliberately don't do under the joiner-as-proxy model. Adding it would compromise the simulation-stays-on-host invariant and is not worth the complexity for a friends-only game.

### D17. Steam lobby ↔ Go `LobbyManager` sync is one-way (Steam → Go)

The Steam Matchmaking lobby is the source of truth for membership, mode, and max-player count. The Go `LobbyManager` mirrors it. Considered alternative: bidirectional sync. Rejected — bidirectional sync invites split-brain (the same player listed twice with different ids, or the Steam UI showing slots the Go server has already filled). One-way sync, with Steam events driving Go-side state changes, keeps a single canonical answer to "who is in this lobby?"

### D18. Mixed-origin in `tauri:dev` keeps the existing CORS allow-list

In `tauri:dev`, the webview loads the Vite dev server at `http://localhost:5173` while the Go server runs at `http://localhost:8080` — same-origin does NOT hold in dev. The existing `CORS_ALLOWED_ORIGIN=http://localhost:5173` default in the Go server is therefore still required for that workflow. The packaged build is same-origin (webview loads from the Go server directly) and does not need a CORS allow-list. The spec body explains the distinction.

### D19. `SteamBridge.ReportAchievement` is fire-and-forget

`ReportAchievement(id)` SHALL NOT block its caller on IPC round-trip latency. Internally the bridge holds a small buffered Go channel (size 64) drained by a dedicated writer goroutine; the caller performs a **non-blocking** send (`select { case ch <- req: default: <atomic drop-counter increment> }`) and returns immediately. When the channel is full at send time, the report is dropped on the floor and the drop counter is incremented; the writer goroutine emits a single summary warning log per N drops (not per dropped report), where N is implementation-chosen but no smaller than 8 to keep the log out of the way during achievement-heavy moments.

Considered alternative: synchronous IPC call. Rejected — game events that trigger achievements are plausibly fired from inside the tick loop, and the project's hard rule is that the shell/IPC is not on the tick path (`pluggable-mp-transport`, "Determinism is not affected by transport choice"). A 100 ms IPC stall during a "first wave cleared" trigger would directly stall a tick.

Considered alternative: an unbuffered channel with a dedicated drainer. Rejected — an unbuffered channel send blocks until a receiver is ready, which is exactly the property we are trying to avoid. The buffered channel + non-blocking send is the correct implementation of "fire-and-forget"; the buffer absorbs short bursts and the drop-on-full behaviour ensures unboundedly slow IPC can never back-pressure the tick.

The fire-and-forget queue makes the worst case "achievement is delivered late by 50 ms" or "delivery dropped during an IPC stall burst," not "tick stalls." Achievements lost because the writer goroutine is still draining when the process exits, or because the buffer was full when the report was enqueued, are explicitly accepted (see D24 and `steam-achievements` "Achievements earned while Steam unavailable are accepted-loss").

### D20. Profile identity: Steam ID canonical when Steam-initialised; one-time auto-migration of legacy UUID

When the Go server is started with the IPC-backed bridge AND the bridge's `LocalPlayer()` returns a real Steam ID, the canonical profile id is the Steam ID. When the bridge is the `NoopBridge` (Steam unavailable, or non-Steam launch), the canonical profile id is the SPA-managed UUID, stored in `<userdata>/settings.json` in the packaged build (from Phase 1 onward) and in `localStorage` only in the browser dev loop. See `local-user-data-storage` "Player-id is stored in `settings.json` from Phase 1 in the packaged build" for the rationale — the packaged build's per-launch port assignment (D6) makes `localStorage` non-durable.

On first packaged launch where (a) the Steam ID profile file does not exist yet AND (b) a legacy UUID profile exists in `<userdata>/profiles/` (post-migration from `./profiles/`, see `local-user-data-storage`), the shell SHALL prompt the user once with a modal containing two choices: "Use my existing progress under this Steam account" (renames the legacy file to the Steam ID's filename) and "Start fresh" (leaves the legacy file in place; a new empty Steam ID profile is created). The choice is recorded so the prompt does not re-appear.

Considered alternatives: auto-merge (rejected — two profile JSONs cannot be safely merged without a content-aware policy), auto-rename without prompting (rejected — a user with a separate UUID profile they did not want to bring over would be surprised), require the user to manually rename the file (rejected — too technical).

### D21. SP byte-traffic baseline is captured before the Transport refactor lands

The single-player byte-identical regression guard (`pluggable-mp-transport` "Single-player byte-traffic regression guard") works by comparing a pre- and post-refactor byte capture. The pre-refactor capture SHALL be produced and committed to `server/internal/ws/testdata/sp_baseline_*.bin` in a preparatory PR that lands BEFORE the Transport refactor PR. The refactor PR's CI then runs the post-refactor capture and asserts byte equivalence against the committed baseline. Comparison is at the WebSocket *payload* level (the application-protocol bytes), not the WebSocket *frame* level (which includes timing-dependent control frames and would trivially fail). The baseline is regenerated only when the application protocol itself intentionally changes, with a code-review note explaining why.

### D22. Steam Networking Sockets uses Reliable + ordered send mode

All game-state traffic over Steam Networking Sockets SHALL use `k_nSteamNetworkingSend_Reliable` (which is reliable AND ordered). Considered alternative: `k_nSteamNetworkingSend_Unreliable` or `k_nSteamNetworkingSend_ReliableNoNagle`. Rejected — the existing WebSocket transport gives reliable+ordered semantics, and the protocol assumes them. Using unreliable would silently corrupt game state; ReliableNoNagle would marginally reduce latency at the cost of bandwidth and is a follow-up optimisation, not a Phase 2 default.

### D23. SPA learns its compiled version via Vite define-injection at build time

The SPA SHALL receive its build version through a Vite `define` injection that maps a constant (e.g., `__APP_VERSION__`) to a value computed at build time from the git SHA. This SHALL be wired in `vite.config.ts`. Considered alternatives: fetching `/health` for the server's version (rejected — that gives the server's version, not the SPA's, defeating the entire build-mismatch check), reading a `version.txt` shipped alongside the SPA (rejected — extra HTTP round-trip on boot). The SPA's hello message to the WS hub includes this constant; the server compares it to its own version emitted in the `NOMADS_READY` line.

In dev (`npm run dev` or `tauri:dev`), the Vite define resolves to `"dev"` and the server's compiled version is similarly `"dev"`; both match by construction, so the build-mismatch modal does not fire during iteration. The build-mismatch check is only meaningful for packaged release builds.

### D24. IPC robustness: timeouts, size caps, terminal-on-close

The Go↔Rust IPC channel has three hardening rules:

- **Per-call timeout.** Every synchronous IPC method (e.g., `LocalPlayer`, `RegisterTransport`, lobby create/join) has a 5-second timeout. On timeout the bridge returns a `steam_timeout` error to the caller; the request id is marked discarded so a late response is ignored.
- **Message size cap.** Each newline-delimited JSON message is capped at 1 MiB. Lines exceeding the cap are dropped on the reader side with a logged error; the channel survives.
- **Terminal-on-close.** When the IPC channel closes for any reason (shell crash, parent exit, OS error), the bridge transitions to a "closed" state and every subsequent call returns a `steam_channel_closed` error. The bridge does NOT attempt to reconnect, and the bridge instance does NOT swap to `NoopBridge` mid-process. The Steam-features-UI in the SPA reacts to channel-closed by hiding Steam options just as it does for `steam_unavailable`. Considered alternative: auto-reconnect. Rejected — a closed IPC channel means the shell is gone; reopening it requires the shell to recreate the named pipe / Unix socket on its end, which is racy and doesn't usefully cover any real failure mode (in practice if the shell is gone, the Go child is about to receive stdin EOF and exit anyway).

`ReportAchievement` is fire-and-forget (D19) and therefore does NOT use the per-call timeout — pending writes are dropped on channel close with a single logged warning, not one per pending message.

### D25. `settings.json` single-writer protocol: SPA owns the file

To avoid file-clobber races between the shell (which would like to persist window size/position on close) and the SPA (which persists everything else), the SPA is the sole writer of `settings.json`. Window-state events from the shell (window resize, window move, window close) are sent to the SPA via a `desktopBridge` IPC; the SPA debounces and persists. Considered alternatives: file locking (rejected — fragile across OSes), atomic temp-file-rename in each writer (rejected — still races on which write lands last; an SPA save with a stale window-size field would clobber the shell's just-written value), `settings.shell.json` + `settings.spa.json` (rejected — two file formats and two load paths for one logical thing).

If the SPA has not booted yet (e.g., the user closes the window during the `NOMADS_READY` wait), the shell holds the window-state update in memory until the SPA signals `desktop_bridge_ready`, then sends it.

### D26. Renderer parity is split: Phase 1 smoke check + Phase 2 full check

The webkit2gtk renderer-parity risk is independent of Steam and should be uncovered before any Phase 2 work is sunk into a doomed shell choice. The Phase 1 smoke check (manual) is: cold launch on a Steam Deck or SteamOS VM reaches the main menu within 10 seconds, and one combat scene renders without obvious visual regressions vs. the Windows build. The Phase 2 acceptance gate (`desktop-shell`) adds: completion of a full single-player run start-to-end, sustained 60 fps in mid-game combat, and a macOS smoke check (cold launch + main menu + one combat scene) to catch WebKit's own quirks.

## Risks / Trade-offs

- **Rust toolchain in dev/CI.** — Mitigation: only the packaging step needs Rust; Go/TS jobs continue without it. Rust shell is small enough that a single dev can hold the whole codebase in their head; we treat it as glue, not as a place for game logic.
- **Tauri sidecar binary signing.** — Mitigation: the Go binary is bundled as a Tauri sidecar resource, so Phase 3 code signing covers it transitively.
- **Joiner-as-proxy adds a hop of latency in MP.** — Mitigation: hop is `joiner SPA ↔ joiner Go server ↔ steam socket ↔ host Go server`. The local-loopback hop is microseconds; the dominant latency is still the Steam relay. Measurable but not user-noticeable for an RTS at our tick rate.
- **Joiner-as-proxy CPU/RAM cost on Steam Deck.** Every joiner runs a full Go server process even though it does no simulation, plus the Tauri shell + webview. On Steam Deck this is ~150–250 MB resident vs. ~80–120 MB if the joiner connected directly. — Mitigation: acceptable headroom on the Deck (1 GB free even under load); revisit only if Deck-specific perf testing surfaces a regression. The structural simplicity of "SPA always talks to a local server" is worth the cost.
- **WS hub transport refactor blast radius hits single-player.** Refactoring the hub to address clients by `Transport` instead of `*websocket.Conn` touches the code path single-player uses today (single-player still goes through the WS hub for client/server messaging). — Mitigation: explicit byte-identical regression guard in the spec (`pluggable-mp-transport`), plus a rollback note (the refactor lives in one PR; reverting it is mechanical).
- **macOS unsigned binaries fail to open with "damaged" message.** Phase 1 / 2 playtest builds are unsigned. Modern macOS Gatekeeper does not show "unidentified developer" — it shows "X.app is damaged and can't be opened" for downloaded `.dmg`s, which testers misread as a real corruption error. — Mitigation: the playtest checklist documents the `xattr -d com.apple.quarantine /Applications/Nomads.app` (or right-click → Open) workaround; same applies for Linux `.AppImage` permissions. Resolved properly in Phase 3 with notarisation.
- **Windows SmartScreen blocks unsigned playtest binaries.** Closed-playtest `.msi`s are unsigned and SmartScreen will show a full-screen "Windows protected your PC" panel. — Mitigation: playtest checklist documents the "More info → Run anyway" path. Resolved in Phase 3 with code signing.
- **Phase 2 changes can silently regress Direct connect.** Direct connect is the always-on fallback. Phase 2 introduces the transport bridge, Steam Sockets transport, and lobby sync IPC — all of which sit close to the Direct-connect code path. — Mitigation: a CI integration test (two locally-running Go servers + a `transportbridge` between them) lands as part of the Direct-connect work in Phase 1 and runs on every PR through Phase 2; a Phase 2 PR that breaks it cannot land.
- **localStorage / IndexedDB inside WebView2 is opaque to us AND non-durable across launches.** The SPA today uses `localStorage` for the player UUID and may use it for other ephemeral state. WebView2 stores this in its own per-app data dir keyed by `(scheme, host, port)`, and the packaged build's `port=0` policy (D6) gives each launch a different port — so any value left in `localStorage` is unreachable on the next launch. We also don't get to inspect that store for diagnostics or include it in user-supplied bug bundles. — Mitigation: in Phase 1, migrate the player-id storage from `localStorage` to `settings.json` via `desktopBridge` (see `local-user-data-storage`). This is non-negotiable for Phase 1 because profile identity depends on it; deferring to Phase 2 would mean every Phase 1 playtester's profile resets on every relaunch. Document the WebView2 caveat in `local-user-data-storage`. Transitionally we accept that pre-migration browser-dev installs may leave orphaned `localStorage` rows.
- **Multiple network adapters: which IP does the host UI display?** Typical dev/playtester machines have several non-loopback addresses (Ethernet, Wi-Fi, Tailscale, VirtualBox host-only, WSL). — Mitigation: the SPA enumerates all non-loopback IPv4 addresses, sorts them with Tailscale CGNAT (`100.64.0.0/10`) first, then RFC1918 private ranges, then everything else, and lets the user pick which to copy. The first item in the sort order is preselected. Specified in `direct-connect-multiplayer`.
- **`embed.FS` adds the SPA size to the Go binary, ~5–15 MB.** — Mitigation: insignificant on Steam download budgets; the gain (single-process for the user's mental model + the shell only needs to ship one resource) is worth it.
- **Steamworks SDK breakage during a Steam update.** — Mitigation: pin `steamworks-rs` and the bundled SDK headers. Steam SDK releases are infrequent and well-announced; we update intentionally, not automatically.
- **Single-instance lock vs. legitimate multi-account testing.** — Mitigation: ship a dev/test build with the single-instance lock disabled, for QA who legitimately need two clients on one machine. Toggle is a Tauri config flag, not runtime UI.
- **Stdin-EOF shutdown doesn't fire if the Rust shell hard-crashes.** — Mitigation: rare, but the Go server also installs the usual SIGINT/SIGTERM handlers; on a hard shell crash the OS reaps the orphaned Go process the next time the shell relaunches and tries to re-bind. Acceptable.
- **Determinism risk from any future Rust-side timing.** — Mitigation: explicit rule that the shell is not on the tick path; any new IPC command that touches game state must be reviewed for tick-path impact. Codified as a code-review checklist item.

## Migration Plan

There is no live deployment to migrate. The change is additive at every layer:

1. The existing browser dev loop (`npm run dev` + `air`) continues to work unchanged throughout the change.
2. The first vertical slice (proposal + design + spec deltas + early tasks) lands behind the `embed_spa` build tag, which defaults off — no behaviour change for anyone running the server today.
3. The Rust crate at `desktop/` is built only when packaging.
4. Profile dir change is gated on `WEBRTS_PROFILES_DIR` being set; when unset the default (`./profiles`) is unchanged.

Rollback for any individual phase is "stop building with `-tags embed_spa`" + "don't ship the `desktop/` artifact." Nothing is destructive.

## Open Questions

1. **IPC wire format.** Locked to JSON-Lines (D8). Revisit only if message volume forces a binary protocol.
2. **Bundle Go binary as Tauri sidecar resource vs. install-dir file.** Recommend sidecar (signing covers it); implementation plan can challenge this if the sidecar mechanism is restrictive on macOS.
3. **Per-message hop counter / forward-time telemetry in `transportbridge`.** Recommend yes — small (a few bytes per message) and useful for diagnosing "Steam relay is slow" complaints in production. Final call deferred to the implementation plan.
4. **Single-instance lock toggle for dev/test builds.** Recommend a Tauri config flag, not runtime UI. Implementation plan to confirm Tauri exposes this cleanly.
5. **First wave of achievement IDs.** Out of scope for this change — we ship the bridge and one smoke-test achievement; the full achievement list is a content-design task on its own.
