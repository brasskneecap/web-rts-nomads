## Why

The game currently runs only in a web browser against a local Go server started by a dev script — it is not installable, not distributable, and cannot ship on Steam. To reach Steam (the stated end goal) the game must become a single double-clickable desktop application that bundles its own server, persists saves in an OS-standard user-data directory, and supports both Steam friend-invite multiplayer and a Steam-independent "direct connect" path so we can run closed multiplayer playtests before any Steam access is available.

## What Changes

- Introduce a Tauri (Rust) desktop shell at `desktop/` that owns the window, the system webview, the Steamworks SDK lifecycle, and supervision of the Go server as a sidecar child process.
- Embed the built Vue SPA into the Go server binary via `embed.FS` behind an `embed_spa` build tag, so the packaged server can serve both the API and the static client from a single origin.
- Add a free-port discovery handshake: the Go server listens on a system-assigned port and emits a single machine-readable line (`NOMADS_READY url=… version=…`) on stdout for the shell to parse.
- Move the per-player profile directory from `./profiles` (current default) to the OS user-data dir (`%APPDATA%/Nomads/profiles` on Windows, `~/Library/Application Support/Nomads/profiles` on macOS, `~/.local/share/Nomads/profiles` on Linux). No format change — only the path changes, set via the existing `WEBRTS_PROFILES_DIR` env var.
- Add a small Steam IPC bridge package (`server/internal/steam/`) with a `SteamBridge` interface and a `NoopBridge` default. The Go server gets the real bridge only when launched by the shell.
- Make the WebSocket hub's transport pluggable. The existing WebSocket-over-TCP becomes one transport; Steam Networking Sockets (relayed by the Rust shell over IPC) becomes a second transport. The host's simulation logic is untouched.
- Add a "Direct connect" multiplayer path: a host UI flow that exposes the local Go server on the non-loopback interface (gated behind an opt-in "Allow LAN/Internet connections" toggle) and a joiner flow that accepts a `host:port` string. Works without Steam.
- Add a "Steam friend invite" multiplayer path: the Rust shell drives Steam Matchmaking lobby create/join, opens a Steam Networking Sockets connection to the host, and hands the connection handle to the Go server, which registers it as a regular client.
- Add a `desktopBridge.ts` TS client (the only file in the SPA that imports `@tauri-apps/api`) that auto-detects whether it is running inside the Tauri shell and gracefully degrades to no-ops or HTTP equivalents in a plain browser.
- Add per-platform packaging configuration (`tauri.conf.json` per target) and an additive dev workflow (`npm run tauri:dev`) that does not interfere with the existing `npm run dev` + `air` browser dev loop.
- Joiners in MP run their local Go server as a transparent proxy to the host's server. Host stays the single authoritative simulator.

**Non-goals (deliberately deferred to Phase 3 or future changes):**

- Code signing / notarisation, `steamcmd` upload pipeline, Steam Cloud depot configuration, store-page assets, anti-cheat, auto-update outside Steam — Phase 3.
- Gamepad / Steam Input mapping for Steam Deck — out of scope for this change. The desktop build must run on the Deck; full controller support is a separate capability tracked as a future change.
- Lobby chat, voice chat, in-game text chat — out of scope. Players coordinate over Steam friends chat or external voice today; nothing here changes that.
- Host migration in MP. When the host disconnects or crashes mid-match, the match ends for everyone; joiners do not get promoted. Captured as an explicit terminal scenario in `pluggable-mp-transport` / `steam-invite-multiplayer`.
- NAT traversal for Direct connect (UPnP, STUN, hole-punching). Direct connect is an "open a port or use Tailscale" path; the user is responsible for reachability. Steam Networking Sockets handles NAT traversal in the Steam path.
- Passcode / per-session authentication on the Direct connect listener. The threat model is "share like a Discord link" — described in the `direct-connect-multiplayer` spec body. Optional passcode auth is a deferred enhancement, not in this change.

### Phase mapping

`tasks.md` is organised so that **Phase 1 (foundation)** = task sections 1–7, 10, 13, 15, 17–20; **Phase 2 (Steamworks)** = task sections 4 (partial — `IPCBridge` only), 8, 9, 11, 12, 14, 16. Phase markers appear on each section heading.

## Capabilities

### New Capabilities

- `desktop-shell`: Tauri-based Rust shell that owns the window, system webview, Steam SDK lifecycle, and Go-server child-process supervision. Includes startup handshake, lifecycle/shutdown, single-instance lock, and crash-recovery behaviour.
- `embedded-spa-serving`: Go server can be built with an `embed_spa` build tag that bakes the built Vue SPA into the binary and serves it at `/` from the same origin as the API. Includes asset caching, fall-through to the SPA's `index.html` for client-side routes, and a no-tag build mode that preserves the current API-only behaviour.
- `local-user-data-storage`: Per-platform user-data directory conventions for profiles and (future) save files, plumbed through the existing `WEBRTS_PROFILES_DIR` env var with shell-side directory creation and writable-check at startup.
- `steam-bridge`: Go-side `SteamBridge` interface and IPC client + `NoopBridge` fallback. Surface includes `LocalPlayer()`, `ReportAchievement(id)`, `OpenInviteOverlay()`, and a `Transport` registration hook for Steam Networking Sockets.
- `pluggable-mp-transport`: WebSocket hub accepts any `Transport` implementation, not only WebSocket-over-TCP. Steam Networking Sockets becomes a second transport; the host's authoritative game logic is transport-agnostic.
- `direct-connect-multiplayer`: Steam-independent host/join flow over the existing WebSocket transport. Host exposes the listener on the non-loopback interface behind an opt-in toggle; joiner connects via `host:port`. Stays in the shipped Steam build as the always-available fallback.
- `steam-invite-multiplayer`: Steam Matchmaking lobby create/join driven by the Rust shell, Networking Sockets connection setup, and joiner-as-proxy forwarding through a new `server/internal/transportbridge/` package so joiners do not run the simulation.
- `steam-achievements`: Achievement reporting from Go game logic via the `SteamBridge`. Achievement IDs live in one Go file (`server/internal/steam/achievements.go`) to stay grep-able and aligned with the Steam dashboard config.
- `diagnostics-logging`: Per-OS log file locations, rotation policy, and content rules for each process (shell, Go server, SPA). Lets us diagnose crashes from a user-supplied diagnostic bundle without recreating the bug live.

### Modified Capabilities

<!-- No existing OpenSpec capabilities have requirements that change. The existing
`ability-category` and `caster-combat-profile` specs are unrelated to packaging /
shell / networking and are not touched by this change. -->

## Impact

- **New code (Rust):** `desktop/` crate (~500–1000 LOC: Tauri config, child supervisor, IPC framing, Steam SDK wrappers, callback pump). Adds Rust toolchain to dev and CI for packaging.
- **New code (Go):** `server/internal/embedded/`, `server/internal/steam/`, `server/internal/transportbridge/`, plus changes to `server/cmd/api/main.go` (free-port flag, ready-line emit, stdin-EOF shutdown). Adds a build tag (`embed_spa`) to the Go build.
- **New code (TS):** `client/src/game-portal/src/services/desktopBridge.ts` plus host/join UI panels in the lobby views. Adds `@tauri-apps/api` as a client dependency (tree-shaken out in browser dev).
- **Network surface:** No new server-side ports by default. Direct-connect MP optionally exposes the existing port on `0.0.0.0` only when the host enables it explicitly.
- **Persistence:** Profile file location changes per OS; format and schema unchanged.
- **External deps:** Tauri + Rust toolchain; Steamworks SDK headers/libs at build time; `steamworks-rs` Rust crate. None of these are required to build or run the Go server alone — the existing dev loop (`npm run dev` + `air`) is unaffected.
- **Determinism:** Untouched. The Rust shell and IPC bridge are not on the tick path. The Steam Networking transport is a byte pipe only; the same WS protocol bytes flow over it. The project's ID-based-targeting invariants in `.claude/rules/AI_RULES.md` are not affected.
- **CI:** New Rust build job for packaging; existing Go and TS jobs unchanged.
- **Out of scope:** Phase 3 work — code signing / notarisation, `steamcmd` depot upload, Steam Cloud depot configuration, store-page assets, anti-cheat, auto-update outside Steam.
