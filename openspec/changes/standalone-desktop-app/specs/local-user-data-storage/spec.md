## ADDED Requirements

### Requirement: Shell resolves the per-OS user-data directory at startup

The shell SHALL determine the user-data directory for the running OS before spawning the Go server child, using the following conventions:

- Windows: `%APPDATA%/Nomads/`
- macOS: `~/Library/Application Support/Nomads/`
- Linux: `~/.local/share/Nomads/`

The shell SHALL create this directory and a `profiles/` subdirectory inside it if either does not exist. The shell SHALL pass the `profiles/` path to the Go server child via the `WEBRTS_PROFILES_DIR` environment variable.

#### Scenario: First launch on a fresh system

- **WHEN** the user launches the packaged build on a system where no Nomads user-data directory exists
- **THEN** the shell creates the user-data directory and the `profiles/` subdirectory using OS-appropriate permissions
- **AND** the shell starts the Go server child with `WEBRTS_PROFILES_DIR` set to the absolute path of the `profiles/` subdirectory

#### Scenario: Subsequent launch reuses existing directory

- **WHEN** the user launches the packaged build on a system where the user-data directory already exists from a prior launch
- **THEN** the shell reuses the existing directory without recreating it
- **AND** the previously saved profile is visible to the Go server child

### Requirement: Shell checks user-data directory is writable at startup

The shell SHALL verify that the resolved user-data directory is writable before spawning the Go server child. If the writable check fails, the shell SHALL surface a pre-launch error explaining the path and the underlying OS error.

#### Scenario: Writable check passes

- **WHEN** the shell resolves a user-data directory and the directory is writable
- **THEN** the shell proceeds to spawn the Go server child

#### Scenario: Writable check fails

- **WHEN** the shell resolves a user-data directory and a test-file write to it fails
- **THEN** the shell displays a pre-launch error modal containing the resolved path and the underlying OS error
- **AND** the shell does not spawn the Go server child

### Requirement: Go server reads `WEBRTS_PROFILES_DIR` without code change

The Go server's existing `profile.Manager` SHALL continue to use the value of `WEBRTS_PROFILES_DIR` as its profiles directory when the env var is set, and the existing `./profiles` default when the env var is unset.

#### Scenario: Env var set by shell

- **WHEN** the Go server child receives `WEBRTS_PROFILES_DIR=/path/to/profiles` from the shell at startup
- **THEN** the `profile.Manager` reads and writes profile JSON files under `/path/to/profiles/`

#### Scenario: Env var unset in browser dev loop

- **WHEN** the Go server is started by the existing `air` dev workflow with no `WEBRTS_PROFILES_DIR` set
- **THEN** the `profile.Manager` reads and writes profile JSON files under `./profiles/` as it does today

### Requirement: Client settings persist under the user-data directory

The shell SHALL store user-facing client settings (at minimum: window size and position, audio volume, key bindings) in a single JSON file at `<userdata>/settings.json`. The SPA SHALL be the sole writer of this file. The shell SHALL NOT write to `settings.json` directly; window-state events from the shell SHALL be sent to the SPA via a `desktopBridge` IPC, and the SPA SHALL debounce and persist them along with its own settings.

#### Scenario: Settings persist across launches

- **WHEN** a user changes any setting (e.g., adjusts master volume) and closes the game
- **THEN** the new value is written to `<userdata>/settings.json` before the shell exits
- **AND** the same value is loaded and applied on the next launch

#### Scenario: Settings file missing or unreadable

- **WHEN** `<userdata>/settings.json` is missing, empty, or cannot be parsed
- **THEN** the shell falls back to documented default values for every setting
- **AND** the next save replaces the file with the current (defaulted-or-modified) values

#### Scenario: Settings file shape is forward-compatible

- **WHEN** the SPA loads a `settings.json` containing keys it does not recognise (e.g., from a newer build)
- **THEN** the unrecognised keys are preserved verbatim on the next save
- **AND** known keys are applied normally

#### Scenario: Window-state event arrives before SPA is ready

- **WHEN** the shell receives a window-resize, window-move, or window-close event before the SPA has signalled `desktop_bridge_ready`
- **THEN** the shell holds the most recent window-state values in memory
- **AND** the shell sends them to the SPA via the `apply_window_state` IPC immediately after `desktop_bridge_ready` is received

#### Scenario: Settings flush precedes Go child teardown on window close

- **WHEN** the user closes the window and the shell begins its shutdown sequence
- **THEN** the shell first sends a final `apply_window_state` (with the close-time geometry) to the SPA
- **AND** the shell waits up to 2 seconds for the SPA to acknowledge with a `settings_persisted` IPC indicating the debounced write has been flushed to `<userdata>/settings.json`
- **AND** ONLY AFTER receiving the ack (or hitting the 2-second timeout) does the shell close the Go child's stdin
- **AND** the 5-second Go-child grace period (per `desktop-shell` "User closes the window") runs after this settings-flush phase, not in parallel with it

#### Scenario: Shell hard-crashes before SPA has flushed settings

- **WHEN** the shell process is killed by the OS (panic, SIGKILL, force-quit) before the SPA can flush the in-flight debounce buffer
- **THEN** the most recent unwritten settings changes (window geometry, volume slider mid-drag, etc.) are lost
- **AND** the previously-persisted `settings.json` on disk remains intact and parseable
- **AND** the next launch falls back cleanly to those last-persisted values plus documented defaults for any partial keys

### Requirement: Canonical profile identity is the Steam ID when Steam is initialised

When the Go server is running under the IPC-backed bridge and `LocalPlayer()` returns a real Steam ID, the canonical profile id used to read and write the profile file SHALL be that Steam ID. When the bridge is the `NoopBridge` (Steam unavailable, or non-Steam launch), the canonical profile id SHALL remain the existing `localStorage` UUID.

#### Scenario: Packaged Steam build with Steam initialised

- **WHEN** the Go server starts with the IPC-backed bridge and the bridge reports a Steam ID
- **THEN** profile reads and writes target `<userdata>/profiles/<steam-id>.json`
- **AND** the `X-Player-ID` HTTP header on requests from the SPA is also the Steam ID

#### Scenario: Packaged build with Steam unavailable

- **WHEN** the Go server starts with the IPC-backed bridge but the bridge reports `steam_unavailable`
- **THEN** the SPA continues to use its UUID (sourced from `<userdata>/settings.json` in the packaged build, or `localStorage` in the browser dev loop) as the profile id
- **AND** profile reads and writes target `<userdata>/profiles/<uuid>.json`

### Requirement: One-time legacy-UUID profile migration on first Steam launch

The first time the shell launches with Steam initialised AND a Steam ID profile file does not yet exist AND a legacy UUID profile file is present in `<userdata>/profiles/`, the SPA SHALL display a one-time modal offering two options: "Use my existing progress under this Steam account" and "Start fresh." The choice SHALL be recorded so the modal does not re-appear on subsequent launches.

#### Scenario: Migration prompt: "Use my existing progress"

- **WHEN** a returning playtester launches the packaged Steam build for the first time, has a legacy UUID profile, and no Steam ID profile exists
- **AND** the user clicks "Use my existing progress under this Steam account"
- **THEN** the legacy profile file is renamed to `<steam-id>.json`
- **AND** the SPA records the migration decision in `settings.json` so the modal does not re-appear

#### Scenario: Migration prompt: "Start fresh"

- **WHEN** the same conditions hold
- **AND** the user clicks "Start fresh"
- **THEN** a new empty Steam ID profile is created
- **AND** the legacy UUID profile file is left in place untouched
- **AND** the SPA records the migration decision in `settings.json` so the modal does not re-appear

#### Scenario: No legacy profile

- **WHEN** the user launches the packaged Steam build for the first time and no legacy UUID profile exists
- **THEN** no migration modal is shown
- **AND** the SPA proceeds with normal first-run flow

### Requirement: Player-id is stored in `settings.json` from Phase 1 in the packaged build

The SPA SHALL store the canonical player-id (Steam ID or UUID) in `<userdata>/settings.json`, not in `localStorage`, starting with the first Phase 1 packaged build (any build running inside the Tauri shell, detected via `window.__TAURI__`). The shell-managed user-data dir is the single durable store for client-side persistent state.

Rationale: in the packaged build the Go server binds `port=0` and the kernel assigns a fresh port on every launch (design.md D6). The webview's `localStorage` partition is keyed by `(scheme, host, port)`, so a value written under one launch's port is unreachable under the next launch's port — the legacy `localStorage` UUID would silently reset every relaunch and profiles would appear lost. `settings.json` lives in the shell-managed user-data directory and is unaffected by port changes. The browser dev loop (`npm run dev`, where `__TAURI__` is absent) MAY continue to use `localStorage` because that workflow uses a stable Vite-served origin.

#### Scenario: Packaged build reads player-id from `settings.json`

- **WHEN** the SPA starts up inside the Tauri shell (`window.__TAURI__` is present)
- **THEN** the SPA reads the player-id from `<userdata>/settings.json` via `desktopBridge.getSettings()`
- **AND** the SPA does NOT read the player-id from `localStorage`

#### Scenario: First-run transition from a pre-Phase-1 install

- **WHEN** the SPA starts up in a Phase 1 packaged build, `settings.json` does not contain a player-id, and `localStorage` does contain one (left over from a prior browser-dev install at the same OS user account)
- **THEN** the SPA copies the `localStorage` value into `settings.json` on first save
- **AND** subsequent launches read from `settings.json`

#### Scenario: Player-id survives relaunch across changing ports

- **WHEN** a player in the packaged build closes the game and relaunches (the Go server binds a different free port on the second launch)
- **THEN** the SPA on the second launch reads the same player-id it wrote before the first close
- **AND** the same profile JSON under `<userdata>/profiles/<player-id>.json` is loaded

#### Scenario: Browser dev loop continues to use `localStorage`

- **WHEN** the SPA runs under `npm run dev` (no Tauri shell, `window.__TAURI__` is undefined)
- **THEN** the SPA reads and writes the player-id to `localStorage` as today
- **AND** no `settings.json` IPC is attempted

### Requirement: Legacy `./profiles` directory migration on first launch

When the shell launches for the first time and the resolved user-data directory contains no `profiles/` subdirectory but a legacy `./profiles/` directory exists adjacent to the Go server binary (left over from the pre-desktop dev workflow), the shell SHALL copy the legacy profiles into the user-data directory and SHALL log the migration. The legacy directory SHALL NOT be deleted automatically.

#### Scenario: Migration runs on first launch with legacy profiles present

- **WHEN** the shell launches on a system where `<userdata>/profiles/` does not exist and a legacy `./profiles/` directory adjacent to the Go binary contains one or more profile JSON files
- **THEN** the shell copies every JSON file from the legacy directory into `<userdata>/profiles/`
- **AND** the shell writes a log entry naming both paths
- **AND** the original legacy files are left in place untouched

#### Scenario: No legacy directory present

- **WHEN** the shell launches on a system with no legacy `./profiles/` directory
- **THEN** the shell creates an empty `<userdata>/profiles/` and proceeds normally

#### Scenario: Migration runs only once

- **WHEN** the shell launches on a system where `<userdata>/profiles/` already exists (regardless of whether a legacy directory is also present)
- **THEN** the shell does NOT copy any files from the legacy directory
- **AND** the shell does not log a migration event
