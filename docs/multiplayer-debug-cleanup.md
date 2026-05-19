# Steam MP debug-cleanup follow-ups

Backlog of cleanup items added during the §14 / §14R debug push.
Multiplayer is functional; these are noise/polish, not blockers. Each
item is self-contained — file paths and exact identifiers included so it
can be done without re-reading the original session.

## 1. Strip SPA console.logs

Diagnostic `console.log` calls added during debug. Safe to delete — they
clutter the devtools console for anyone who inspects.

**`client/src/game-portal/src/views/Match.vue`** — remove:
- `console.log('[Match.onMounted]', { urlMatchId, isSteamProxyJoiner })`
- `console.log('[Match.onMounted] preflight', { status: res.status, ok: res.ok })`
- `console.log('[Match.onMounted] preflight body', data)`
- `console.warn('[Match.onMounted] ...')` lines (3 of them, on kick paths)

**`client/src/game-portal/src/views/Lobby.vue`** — remove:
- `console.log('[Lobby] poll', { ... })` inside the `poll()` function
- `console.log('[Lobby] steamLobbyId recompute', { ... })` inside the `steamLobbyId` computed
- `console.warn('[Lobby] fetchLobby threw:', e)` and `console.warn('[Lobby] getSteamLobbyData threw:', e)` — these are useful when something breaks; consider keeping (silent today, loud on regression)

**`client/src/game-portal/src/views/CreateGame.vue`** — remove:
- `console.log('[CreateGame] click received', { ... })`
- `console.log('[CreateGame] guard tripped — early return')`
- `console.log('[CreateGame] POST /lobbies …')`
- `console.log('[CreateGame] local lobby created', created.id)`
- `console.log('[CreateGame] navigating to /lobby/' + created.id)`
- `console.error('[CreateGame] failed:', err)` — keep this one, surfaces real failures
- All `[SteamCreate]` lines inside `runBackgroundSteamLobbyCreate` (5-6 of them); keep the final `console.error('[SteamCreate] failed:', err)` only

**`client/src/game-portal/src/game/core/GameState.ts`** — remove:
- `console.log('[GameState] interpolationDelayMs initialised to', delay)` inside `detectInitialInterpolationDelayMs()`

Keep `console.log('connected as ...')` in `NetworkClient.ts` — pre-existed and is useful.

## 2. Gate or remove shell-log diagnostics

The Rust shell writes diagnostic lines to `<ts>-shell.log` via
`crate::logs::current_shell_log()`. They're load-bearing during MP debug
but noisy in normal play. Pick a strategy:

**Option A (recommended): wrap each diagnostic write in a check on a
`DEBUG_STEAM` env var.** Add a helper `fn steam_debug_enabled() -> bool`
that checks `std::env::var("DEBUG_STEAM").is_ok()`; gate each
`sl.write_line(...)` call inside `create_lobby`, `join_lobby`,
`list_steam_lobbies`, `open_invite_overlay`, `leave_steam_lobby`, and
the `steam_net stats over` line in `steam_net.rs::maybe_log_stats`.

**Option B: just delete most lines.** Keep these load-bearing markers
that signal real state transitions:
- `steam: initialised as <persona> (steamid=<id>)` (lib.rs, on init)
- `steam: hosting lobby=<id>` / `steam: joining lobby=<id> host=<id>` (cmd/api/steam_lobby.go — already concise, fine to keep)
- `steam_net: peer connected (host-side)` / `steam_net: outbound connect completed (joiner-side)` (steam_net.rs — fires once per connection)
- Any `[WARN]` or `[ERROR]` line

Delete the per-call/per-poll lines:
- All `create_lobby: entered ... / bridge OK ... / registering callback ... / awaiting ... / set X → true ...` lines in `lib.rs::create_lobby`
- All `list_steam_lobbies: lobby=<id> ... / skipping lobby ...` lines in `lib.rs::list_steam_lobbies`
- `open_invite_overlay: entered ...` / `ActivateGameOverlayInviteDialog called ...` (lib.rs)
- `steam_net stats over ...` line in `steam_net.rs::maybe_log_stats`

Option A is more useful long-term (one env-var flip to bring all of it
back during the next debug session). Option B is faster to commit.

## 3. Tidy `LobbyType::Public` comment

`desktop/src-tauri/src/lib.rs` around `create_lobby`. Currently has a
"TEMP DEBUG" comment block claiming this will be reverted to
`LobbyType::FriendsOnly` once discovery works. The current decision is
to KEEP `Public` for now — playtests rely on it bypassing the Steam
friend graph.

Rewrite the comment to say something like: "Public so the lobby is
visible to anyone running the same appid — chosen over `FriendsOnly`
for playtest convenience until we have a real paid appid. Switch back
to `FriendsOnly` when the friend-graph-scoped discovery is what we
want." Just remove the "revert me" framing.

## 4. Disable Tauri devtools in shipped builds (ship-prep, not now)

`desktop/src-tauri/Cargo.toml`:

```toml
tauri = { version = "=2.11.2", features = ["devtools"] }
```

Was added during the debug push so right-click → Inspect works in
release builds. Fine for current playtest, but for a real ship build
(Phase 3), change back to `features = []`. Slightly smaller binary +
hides the inspector from end users.

Don't do this yet — devtools is useful while iterating.

## 5. `--steam-net-selftest` CLI path

`desktop/src-tauri/src/lib.rs` argv parser + `server/cmd/api/selftest.go`
+ `NOMADS_SELFTEST` env passthrough in `supervisor.rs`.

Built as a pre-§14 smoke test for the Steam Sockets transport. The SPA
flow now exercises the same code path end-to-end, so the CLI flag is
redundant in normal use. Still useful for byte-level debugging without
UI involvement (e.g., when investigating Steam SDK regressions).

Three options:
- **Keep** — it's behind a flag, costs nothing when unused.
- **Move behind a `selftest` Cargo feature** — keeps the code, removes
  it from default builds.
- **Delete** — `selftest.go`, the `installSteamNetSelftest` branch in
  `cmd/api/main.go`, the `--steam-net-selftest` argv parse in
  `lib.rs::parse_selftest_mode_from_args`, the `selftest` param on
  `supervisor::spawn_and_wait_ready`, and the `NOMADS_SELFTEST` env
  passthrough.

Lean toward Keep unless the readme entry for it becomes confusing.

## 6. Dead Go-side `IPCBridge` lobby methods

`server/internal/steam/ipc.go` — `(*IPCBridge).CreateLobby(maxPlayers)`
and `(*IPCBridge).JoinLobby(lobbyID)`. Nothing in Go calls them; the
SPA reaches Steam Matchmaking through Tauri commands. The
corresponding `"create_lobby"` / `"join_lobby"` handlers in `ipc.rs`
are also unreached by production code (Tauri commands handle the SPA's
calls directly).

Safe to delete both Go methods and the dead IPC-channel handlers if
you want; doesn't hurt anything to leave them.

## 7. `steam_appid.txt` auto-create

`build.ps1` auto-generates `desktop/src-tauri/steam_appid.txt` with
`480` when missing. Helpful for fresh clones; one-line revert if you
ever move to a real paid appid in `lib.rs`'s `STEAM_APPID` constant.
No cleanup needed — leave as is.

## Where the cluster boundaries break for a focused commit

A "post-debug noise reduction" commit naturally bundles items **1 + 2
(Option A) + 3**. 15-30 minutes of work. Items 4, 5, 6 are
ship-prep and out of scope for this kind of cleanup pass.
