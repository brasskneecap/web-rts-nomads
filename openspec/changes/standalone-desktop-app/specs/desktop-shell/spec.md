## ADDED Requirements

### Requirement: Tauri shell owns the application window and lifecycle

The system SHALL ship a Tauri-based Rust shell at `desktop/` that is the user-facing executable for the packaged build. The shell SHALL open exactly one top-level window containing a system webview and SHALL be the parent process for all other game processes.

#### Scenario: Cold launch outside Steam

- **WHEN** the user double-clicks the packaged binary on Windows, macOS, or Linux
- **THEN** the shell opens a single window containing the SPA
- **AND** the shell starts a single Go server child process
- **AND** the main menu is visible to the user within 5 seconds of launch on a typical desktop

#### Scenario: Cold launch via Steam

- **WHEN** the user launches the game from the Steam library
- **THEN** the shell starts the same as a cold launch outside Steam
- **AND** the shell additionally initialises Steamworks before opening the window

### Requirement: Shell supervises the Go server child process

The shell SHALL spawn the Go server binary as a sidecar child process, supervise its lifecycle, and terminate it cleanly on shutdown.

#### Scenario: Successful child startup

- **WHEN** the shell spawns the Go server child
- **THEN** the shell reads the child's stdout line by line
- **AND** the shell parses the first line matching `^NOMADS_READY url=(\S+) version=(\S+)$` to determine the loopback URL and build version
- **AND** the shell loads the webview at that URL

#### Scenario: Child fails to start within timeout

- **WHEN** the Go server child does not print `NOMADS_READY` within 10 seconds of spawn
- **THEN** the shell terminates the child
- **AND** the shell displays a modal containing the captured stderr ring buffer plus actions "Copy diagnostic info", "Retry", and "Quit"

#### Scenario: Child crashes mid-session

- **WHEN** the Go server child exits unexpectedly while the window is open
- **THEN** the shell displays a "Server crashed — click to restart" dialog
- **AND** on user-initiated retry the shell respawns the child with the same environment

#### Scenario: Single-player run is lost after child crash + restart

- **WHEN** the Go server child crashes mid-run in single-player and the user retries
- **THEN** the SPA reconnects to the freshly respawned server
- **AND** the SPA navigates to the main menu — the in-progress run is NOT resumable (server held the only copy of the active simulation state)
- **AND** the SPA renders a one-time toast explaining the run was lost ("The game crashed and your current run could not be recovered. Profile progress is intact.")
- **AND** the player's persistent profile JSON on disk is unchanged

#### Scenario: Multiplayer match is terminated for joiners after host child crash

- **WHEN** the host's Go server child crashes mid-match in multiplayer
- **THEN** every joiner sees the existing "Match ended — host disconnected" terminal state (per `pluggable-mp-transport` "Host disconnect ends the match")
- **AND** the host, after restart, lands on the main menu — the active lobby is gone

#### Scenario: User closes the window

- **WHEN** the user closes the application window
- **THEN** the shell first runs the settings-flush handshake (per `local-user-data-storage` "Settings flush precedes Go child teardown on window close") — sends final `apply_window_state` to the SPA and waits up to 2 seconds for `settings_persisted` ack
- **AND** the shell then closes the Go server child's stdin
- **AND** the shell waits up to 5 seconds for the child to exit cleanly
- **AND** the shell force-terminates the child if it has not exited after 5 seconds
- **AND** the shell shuts down Steamworks before exiting

### Requirement: Shell exposes typed IPC commands to the SPA

The shell SHALL expose Tauri `#[command]` functions invokable from the SPA via `@tauri-apps/api/core`'s `invoke()` mechanism, covering: local-player identity, achievement reporting, friend-invite overlay, and lobby create/join.

#### Scenario: SPA requests local Steam player

- **WHEN** the SPA invokes `get_steam_player`
- **THEN** the shell returns the local Steam ID and persona name if Steamworks initialised successfully
- **AND** the shell returns null if Steamworks failed to initialise or Steam is not running

#### Scenario: SPA requests friend-invite overlay

- **WHEN** the SPA invokes `open_invite_overlay` with a lobby id
- **THEN** the shell calls `ISteamFriends::ActivateGameOverlayInviteDialog` with that lobby id
- **AND** the shell returns success when Steamworks is initialised
- **AND** the shell returns an "unavailable" error code when Steamworks is not initialised

### Requirement: Shell enforces single-instance behaviour in shipped builds

The shipped Steam build SHALL acquire a single-instance lock at startup. A second launch attempt while a first instance is running SHALL focus the existing window rather than spawn a second instance. A separate dev/test build configuration SHALL disable the lock to support running multiple clients on one machine for QA.

#### Scenario: Second launch while game is already running

- **WHEN** a user launches the shipped build while another instance of it is already running
- **THEN** the existing window is brought to the foreground
- **AND** no second Go server child process is spawned

#### Scenario: Two instances in dev build

- **WHEN** a developer launches the dev/test build configuration twice on one machine
- **THEN** both instances start independently, each with its own Go server child

### Requirement: Renderer-parity smoke check is a Phase 1 gate

Before Phase 2 work begins, the implementation team SHALL run a renderer-parity smoke check on a Steam Deck or SteamOS VM, on the Phase 1 packaged build. The check SHALL be documented in the playtest checklist.

#### Scenario: Phase 1 smoke check passes

- **WHEN** the smoke check is run against the first complete Phase 1 packaged build on a Steam Deck or SteamOS VM
- **THEN** cold launch reaches the main menu within 10 seconds on stock Deck hardware
- **AND** at least one combat scene renders without obvious visual regressions relative to the Windows build (a single screenshot pair is captured for the playtest record)
- **AND** the result is documented in the playtest checklist before any Phase 2 work begins

#### Scenario: Phase 1 smoke check fails

- **WHEN** the smoke check reveals a Linux-only rendering regression severe enough to block Steam Deck shipping
- **THEN** the failure is escalated to a design change before Phase 2 work begins
- **AND** alternative shell choices (Wails, Electron) are reconsidered

### Requirement: Renderer-parity full check + macOS smoke check is a Phase 2 acceptance gate

Before Phase 2 of this change is declared complete, the implementation team SHALL run a full renderer-parity acceptance check on a Steam Deck or SteamOS VM and a smoke check on macOS. Both SHALL be documented in the playtest checklist.

#### Scenario: Steam Deck full check passes

- **WHEN** the full acceptance check is run on a Steam Deck or SteamOS VM
- **THEN** a single-player run completes start to end with no obviously missing or garbled visuals relative to the Windows build
- **AND** sustained framerate during typical mid-game combat is 60 fps (or the Deck's configured frame cap, whichever is lower)

#### Scenario: macOS smoke check passes

- **WHEN** a macOS smoke check is run on the Phase 2 packaged build on a current macOS version (M-series silicon if available)
- **THEN** cold launch reaches the main menu within 10 seconds
- **AND** at least one combat scene renders without obvious visual regressions

#### Scenario: Renderer parity fails on either platform

- **WHEN** any of these checks reveals a regression that would block shipping on that platform
- **THEN** the failure is escalated back to the design (shell choice or renderer code) before further Phase 2 work proceeds

### Requirement: Tauri capability/permission allowlist is enumerated explicitly

The Tauri `capabilities` configuration SHALL enumerate every `#[command]` AND every Tauri built-in plugin permission the shell exposes. Wildcard or "allow-all" permissions SHALL NOT be used.

Required custom `#[command]` entries: `get_steam_player`, `report_achievement`, `open_invite_overlay`, `create_lobby`, `join_lobby`, `desktop_bridge_ready`, `apply_window_state`, `settings_persisted`, `get_settings`, `set_settings`, `append_log`, `open_logs_directory`, plus any others added by this change.

Required built-in plugin permissions (scoped):

- `core:event:allow-listen` and `core:event:allow-unlisten` for the Tauri-event channel (window-state events shell → SPA, server-crashed event shell → SPA).
- `shell:allow-open` scoped to **the specific log directory path resolved at startup**. The capability SHALL NOT grant `shell:allow-open` with a glob like `**` or a path outside `<userdata>/logs/`. The scoped value is computed at startup and registered into the capability set before the SPA is loaded.
- Any window-management permissions required by the shell-side window-state events (e.g., `core:window:allow-set-size`, `-set-position`, `-close` if the SPA invokes them) SHALL be enumerated individually. If the SPA does not need a given window API, the corresponding permission SHALL NOT be added.

Permissions NOT required and therefore explicitly forbidden in the capability set: `fs:*` (no SPA-side filesystem access — file ops go through typed `getSettings`/`setSettings`/`appendLog` commands), `http:*` (no SPA-side HTTP via Tauri — the SPA uses its own `fetch` against the loopback Go server), `process:*` (no SPA-side process spawning).

#### Scenario: Capability config lists every command

- **WHEN** the shell is built
- **THEN** the `tauri.conf.json` capabilities section enumerates exactly the IPC commands used by the SPA (`get_steam_player`, `report_achievement`, `open_invite_overlay`, `create_lobby`, `join_lobby`, `desktop_bridge_ready`, `apply_window_state`, `settings_persisted`, `get_settings`, `set_settings`, `append_log`, `open_logs_directory`, plus any others added by this change)
- **AND** any command not listed there is unreachable from the SPA at runtime

#### Scenario: `openLogsDirectory` uses scoped `shell:allow-open`

- **WHEN** the SPA invokes `desktopBridge.openLogsDirectory()` at runtime
- **THEN** the underlying Tauri call is a `shell.open` on the resolved logs directory path
- **AND** the capability set permits exactly that path, not a broader glob

#### Scenario: A request to open an arbitrary path is denied

- **WHEN** code in the SPA attempts to call the equivalent `shell.open` against a non-logs path (e.g., the profile directory or `/etc/passwd`)
- **THEN** Tauri rejects the call at the capability layer

#### Scenario: Unused permissions are not present

- **WHEN** a developer inspects `tauri.conf.json` capabilities
- **THEN** none of `fs:*`, `http:*`, `process:*` appear in the capability set

### Requirement: `steam_appid.txt` placement for development is defined

In development (running `tauri:dev` or a non-packaged shell binary), the Steamworks SDK requires a `steam_appid.txt` file in the working directory containing the Steam appid. The repo SHALL define a single canonical placement and SHALL gitignore the file.

#### Scenario: Development with Steam features

- **WHEN** a developer runs `tauri:dev` against an unreleased Steam appid
- **THEN** the file lives at `desktop/steam_appid.txt`
- **AND** `desktop/steam_appid.txt` is listed in `.gitignore`
- **AND** the file is excluded from packaged release builds (the packaged build embeds the appid via Tauri config or Steamworks `SteamAPI_RestartAppIfNecessary`)

### Requirement: Window focus loss or minimize pauses single-player only

When the application window loses focus OR is minimised, the shell SHALL signal the SPA, which SHALL pause the simulation only when the current match is single-player. In any multiplayer match (Steam or Direct connect) the simulation SHALL NOT pause for either event.

#### Scenario: Focus loss in single-player

- **WHEN** the user clicks another window during a single-player match
- **THEN** the simulation pauses
- **AND** an unobtrusive "Paused" overlay is shown
- **AND** the simulation resumes when the user clicks back into the game window

#### Scenario: Minimize in single-player

- **WHEN** the user minimises the window during a single-player match
- **THEN** the simulation pauses the same as for focus loss
- **AND** the simulation resumes when the window is restored

#### Scenario: Focus loss or minimize in multiplayer

- **WHEN** the user clicks another window or minimises the window during a multiplayer match
- **THEN** the simulation continues uninterrupted on the host
- **AND** the joiner's local SPA continues to render incoming state messages

### Requirement: WebView2 bootstrap policy on Windows is install-on-demand

The Windows installer SHALL include the Tauri Evergreen WebView2 bootstrapper rather than bundling a fixed WebView2 runtime copy.

#### Scenario: First launch on Windows without WebView2 runtime

- **WHEN** a user installs and launches the game on a Windows machine where the Evergreen WebView2 runtime is not present
- **THEN** the bootstrapper silently installs the Evergreen runtime from Microsoft before the main window appears
- **AND** subsequent launches use the now-installed runtime with no additional download

#### Scenario: Launch on Windows with WebView2 runtime already installed

- **WHEN** a user launches the game on a Windows machine that already has the Evergreen WebView2 runtime
- **THEN** the bootstrapper detects it and the main window opens without any additional download

### Requirement: IPC channel is restricted to the current user account

The Rust shell SHALL create the local IPC channel (Windows named pipe, Unix socket on macOS/Linux) with operating-system-level access restricted to the current user account:

- **Windows named pipe:** the pipe's security descriptor (DACL) SHALL grant read/write only to the calling user's SID (e.g., via `SECURITY_ATTRIBUTES` with a DACL allowing only `OWNER_SID`). The pipe SHALL NOT use a NULL DACL, the world-allow well-known SID, or any "Authenticated Users" group.
- **Unix socket (macOS / Linux):** the socket file SHALL live under the user-data directory (e.g., `<userdata>/runtime/shell.sock`) in a directory with permissions `0700`, and SHALL be created with `umask 0177` so the resulting socket file has `0600` permissions. Linux **abstract namespace sockets** (those starting with a null byte) SHALL NOT be used because they have no filesystem permissions and are reachable by any process in the same network namespace.

Rationale: with the default permissions, a second local process running as the same user (a misbehaving browser extension's helper, a malicious npm-installed binary, a sandboxed child of another app the user runs) could connect to the IPC channel and impersonate either the shell or the Go server. That would let it report arbitrary achievements, trigger lobby joins, or feed crafted Steam IDs back into the Go server. Restricting the channel to the user's account doesn't defend against malware running as the same user (which can do this and worse), but it does defend against same-machine-different-user scenarios (shared family PC, lab machine) without operational overhead.

#### Scenario: Windows named-pipe DACL excludes other users

- **WHEN** the shell creates the named pipe at startup
- **THEN** the pipe's DACL grants access only to the calling user's SID
- **AND** a second process running as a different local user account cannot open the pipe (open returns `ERROR_ACCESS_DENIED`)

#### Scenario: Unix socket file is mode 0600 in a 0700 directory

- **WHEN** the shell creates the Unix socket on macOS or Linux
- **THEN** the socket file is created at `<userdata>/runtime/shell.sock` (or equivalent) with mode `0600`
- **AND** the parent `runtime/` directory exists with mode `0700`
- **AND** a process running under a different uid cannot connect (`connect` returns `EACCES`)

#### Scenario: Linux abstract-namespace sockets are not used

- **WHEN** the shell creates the IPC socket on Linux
- **THEN** the socket path is a filesystem path, NOT an abstract-namespace name (the first byte of the address path is not a null byte)

### Requirement: macOS packaged build is a universal binary covering Apple Silicon and Intel

The macOS build target SHALL produce a universal `.dmg` containing arm64 and x86_64 slices for both the Tauri shell binary AND the bundled Go sidecar binary, joined via `lipo` (or the equivalent `cargo tauri build --target universal-apple-darwin` pathway plus a Go cross-compile of both arches). A single-arch macOS build is acceptable ONLY as a developer-machine build for local iteration, never as a playtest or release artefact.

Rationale: although Apple Silicon is the current default for development, a non-trivial fraction of playtester / Steam-user macOS machines are still Intel. A single-arch build silently fails to launch on the other arch ("you can't open this application"). Catching this in the build configuration is cheap; catching it via a playtester bug report is expensive and reputation-damaging.

#### Scenario: Universal `.dmg` runs on Apple Silicon

- **WHEN** an Apple Silicon (M-series) macOS user opens the packaged `.dmg`
- **THEN** the app launches and runs the arm64 slice for both the shell and the Go sidecar

#### Scenario: Universal `.dmg` runs on Intel

- **WHEN** an Intel macOS user opens the same packaged `.dmg`
- **THEN** the app launches and runs the x86_64 slice for both the shell and the Go sidecar

#### Scenario: Single-arch macOS build is rejected for distribution

- **WHEN** a developer runs `cargo tauri build` for one arch only and attempts to share the artefact for playtest
- **THEN** the build / release checklist surfaces this as a release-blocking gap

### Requirement: Tauri and `steamworks-rs` versions are pinned

The `desktop/Cargo.toml` SHALL pin both Tauri and `steamworks-rs` to specific versions. The pinned versions SHALL be reviewed and updated only intentionally, not via automatic dependency-update tooling.

#### Scenario: Versions pinned in Cargo.toml

- **WHEN** an engineer reads `desktop/Cargo.toml`
- **THEN** the `tauri` and `steamworks-rs` dependencies have explicit version pins (no `*` or `^` ranges)
- **AND** a `desktop/README.md` note explains the policy for updating either pin

### Requirement: Steamworks SDK license review precedes Phase 2 binary distribution

Before any Phase 2 packaged binary leaves the development team (including for closed playtest), the implementation team SHALL confirm Steamworks SDK license compliance: the SDK header / library files used by `steamworks-rs` are not redistributed in source form, the appid configuration is correct, and `steam_appid.txt` is not bundled into release artifacts.

#### Scenario: License review checklist completed before playtest

- **WHEN** the first Phase 2 packaged build is prepared for closed playtest
- **THEN** a license-review checklist has been completed and stored alongside the build artefacts
- **AND** the checklist confirms no Steam SDK source files are bundled in a form prohibited by the SDK license

### Requirement: `steam://joinlobby/<appid>/<lobby>` URL handler is registered on Windows

The Windows installer SHALL register the game as a handler for `steam://joinlobby/<appid>/<lobby>` URLs (or, equivalently, ensure Steam's standard launch-with-lobby invocation works), so that accepting a friend invite from the Steam friends list reliably launches the game with the lobby id available to the shell.

#### Scenario: Friend invite accepted while game closed

- **WHEN** a player accepts a Steam friend invite via the Steam friends list while the game is closed on Windows
- **THEN** Steam launches the game
- **AND** the shell receives the lobby id either via argv (`+connect_lobby <id>`) or via Steam's `LobbyEnter_t` callback after init, and routes into the join flow as defined for cold-launch friend joins

### Requirement: Shell handles Steam friend-join command line on launch

The shell SHALL parse its command-line arguments at startup. If the arguments include `+connect_lobby <lobby-id>`, the shell SHALL defer the join action until the SPA signals readiness and then dispatch a `join_lobby` IPC.

#### Scenario: Steam launches game with connect_lobby argv

- **WHEN** Steam launches the game with `+connect_lobby 12345` because the user accepted a friend invite
- **THEN** the shell stores the lobby id at startup
- **AND** the shell waits for the SPA to invoke `desktop_bridge_ready`
- **AND** the shell then dispatches the join_lobby flow for lobby id 12345
- **AND** the SPA navigates to the lobby join UI without user intervention

### Requirement: Shell calls `SteamAPI_RestartAppIfNecessary` before `SteamAPI_Init`

In packaged Steam builds, the shell SHALL call `SteamAPI_RestartAppIfNecessary(<appid>)` at startup, before any other Steamworks SDK call (including `SteamAPI_Init`). If the call returns true, the shell SHALL exit immediately with no window, no Go child process, and no Steamworks teardown — Steam will relaunch the game under its own management with the appid correctly attributed. If the call returns false, the shell SHALL proceed to `SteamAPI_Init` as normal.

In development (running `tauri:dev` or a non-packaged shell binary) the presence of `desktop/steam_appid.txt` is the Steamworks SDK's signal that the dev workflow is intentional; in that mode `SteamAPI_RestartAppIfNecessary` returns false and startup proceeds normally.

Rationale: omitting this call is a standard Steamworks integration mistake. A user who launches the packaged `.exe` directly (e.g., from File Explorer in the install dir) while Steam is running would otherwise have their session attributed incorrectly, would not receive Steam Cloud sync, and could in some cases have achievement reporting silently fail. The call is the SDK's canonical guard against this and costs one branch at startup.

#### Scenario: Packaged build launched outside Steam while Steam is running

- **WHEN** the user double-clicks the packaged `.exe` from the install directory while Steam is running and the user is logged in
- **THEN** `SteamAPI_RestartAppIfNecessary(<appid>)` returns true
- **AND** the shell exits before opening the window or spawning the Go child
- **AND** Steam relaunches the game under its own management

#### Scenario: Launch via Steam library

- **WHEN** the user launches the game from the Steam library or via a friend invite accepted in the Steam overlay
- **THEN** `SteamAPI_RestartAppIfNecessary(<appid>)` returns false (Steam already attributes the launch correctly)
- **AND** the shell proceeds to `SteamAPI_Init` and normal startup

#### Scenario: Dev launch with `steam_appid.txt`

- **WHEN** the shell is launched in development with `desktop/steam_appid.txt` present
- **THEN** `SteamAPI_RestartAppIfNecessary(<appid>)` returns false (the file is the SDK's documented dev opt-out)
- **AND** the shell proceeds to `SteamAPI_Init`

#### Scenario: Steam not running at all

- **WHEN** the user launches the packaged build with no Steam client running
- **THEN** `SteamAPI_RestartAppIfNecessary(<appid>)` returns false (no Steam process to defer to)
- **AND** the shell proceeds; `SteamAPI_Init` fails; the shell logs the failure and continues offline per the existing "Steamworks init fails" failure mode
