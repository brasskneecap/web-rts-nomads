## ADDED Requirements

### Requirement: Per-OS log directory under the user-data directory

The shell SHALL write logs to a `logs/` subdirectory of the user-data directory defined in `local-user-data-storage`:

- Windows: `%APPDATA%/Nomads/logs/`
- macOS: `~/Library/Application Support/Nomads/logs/`
- Linux: `~/.local/share/Nomads/logs/`

The shell SHALL create the directory if it does not exist.

#### Scenario: Logs directory exists after first launch

- **WHEN** the shell completes its first launch on any supported OS
- **THEN** the per-OS `logs/` directory exists with OS-appropriate permissions
- **AND** at least one log file from the most recent run is present in it

### Requirement: Three log files per run

Each launch SHALL produce three log files in the `logs/` directory, named with the launch timestamp and a process suffix:

- `<timestamp>-shell.log` — written by the Rust shell.
- `<timestamp>-server.log` — written by the Go server child. The child's stdout/stderr (after the `NOMADS_READY` line is consumed) is teed into this file by the shell.
- `<timestamp>-spa.log` — produced by an in-SPA log buffer that the SPA flushes to the shell over a `desktopBridge.appendLog(entries)` IPC at a regular interval and on window close.

#### Scenario: All three log files present after a clean run

- **WHEN** the user launches the game, plays a single-player match, and exits cleanly
- **THEN** the `logs/` directory contains exactly one file matching each of the three suffixes, all sharing the same `<timestamp>` prefix
- **AND** each file is non-empty

#### Scenario: SPA log file populated on crash

- **WHEN** the SPA hits an uncaught exception before window close
- **THEN** the SPA flushes its current log buffer to the shell synchronously as part of its error boundary
- **AND** the corresponding `<timestamp>-spa.log` contains at least the unhandled error

### Requirement: Log content is scoped to actionable diagnostic information

Logs SHALL contain only information useful for diagnosing crashes and bugs. Logs SHALL NOT contain: full game-state snapshots, per-tick simulation data, raw network packets, profile JSON contents, or any value that could identify a player beyond their Steam ID and persona name.

#### Scenario: Per-tick data is not logged

- **WHEN** a 30-minute match is played
- **THEN** none of the three log files contain per-tick simulation data
- **AND** total combined log size for the run is under 5 MB in typical play

#### Scenario: Sensitive data is redacted

- **WHEN** the shell logs Steam SDK errors
- **THEN** auth tickets, session ids, and any other Steam-issued secret are redacted or omitted
- **AND** Steam ID and persona name MAY appear (they are public information)

### Requirement: Log rotation by run and by size cap

Each launch produces its own three log files (rotation-by-run). In addition, the shell SHALL cap the total size of the `logs/` directory at 200 MB by deleting the oldest complete run-triple (all three files sharing a timestamp) until the cap is satisfied, on every launch.

#### Scenario: Cap enforced at launch

- **WHEN** the shell launches on a system where the `logs/` directory exceeds 200 MB
- **THEN** the shell deletes oldest run-triples one at a time, in timestamp order, until the directory is at or below 200 MB
- **AND** files from the current run are never deleted

#### Scenario: Rotation is per-run

- **WHEN** the shell launches
- **THEN** prior runs' log files are left untouched (subject to the size cap)
- **AND** the current run writes to fresh files

### Requirement: Rust shell panic hook writes a final log line

The Rust shell SHALL install a panic hook that, on panic or unrecoverable error, writes a final log entry to the current `<timestamp>-shell.log` describing the panic location and message before the process exits. Operating-system-level crashes (segfault outside Rust's safety net, OS kill) are explicitly out of scope and SHALL NOT be expected to produce a log line.

#### Scenario: Rust panic captured

- **WHEN** the Rust shell panics during a launch
- **THEN** the most recent line in the corresponding `-shell.log` describes the panic location and message
- **AND** the file is flushed and closed before the process exits

#### Scenario: Native segfault is not captured

- **WHEN** the Rust shell process is killed by the OS without unwinding (e.g., SIGSEGV from a misbehaving native dependency)
- **THEN** no final log line is expected (OS-native crash dumps are out of scope for this change)

### Requirement: `transportbridge` per-hop telemetry is debug-overlay only

The `transportbridge` package MAY annotate forwarded messages with a hop counter and forward-time microsecond field for diagnostic purposes. When this annotation is enabled, the resulting values SHALL be displayed only in an in-SPA debug overlay (gated behind an existing debug toggle). The values SHALL NOT be written to log files, telemetry endpoints, or any remote service.

#### Scenario: Hop counter shown in debug overlay

- **WHEN** a developer enables the debug overlay in a Phase 2 MP match
- **THEN** the overlay shows per-message hop counts and forward times for messages routed through `transportbridge`
- **AND** the same values do not appear in any of the three log files
- **AND** no network request is made to any remote service to ship these values

### Requirement: Playtest workarounds for unsigned binaries documented

The playtest checklist SHALL document the following workarounds for unsigned builds, because Phases 1 and 2 ship before Phase 3 (signing/notarisation):

- **Windows:** SmartScreen "Windows protected your PC" → "More info" → "Run anyway."
- **macOS:** Gatekeeper "X.app is damaged and can't be opened" → either `xattr -d com.apple.quarantine /Applications/Nomads.app` from Terminal OR right-click → Open (twice).
- **Linux:** Mark `.AppImage` executable (`chmod +x`) before first run.

#### Scenario: Playtest checklist contains the workarounds

- **WHEN** a new playtester is onboarded
- **THEN** the playtest checklist they read contains the per-OS workaround for unsigned binaries
- **AND** the checklist makes clear these are expected for Phases 1 and 2 and resolved by Phase 3 signing

### Requirement: Log path is discoverable from the SPA

The SPA SHALL surface the `logs/` directory path in its support / about UI, so users reporting a bug can find and attach the log files without searching the filesystem.

#### Scenario: Support UI shows the log path

- **WHEN** the user opens the support / about screen in the SPA
- **THEN** the UI displays the absolute path to the `logs/` directory
- **AND** the UI provides a button that opens that directory in the OS file manager (via a `desktopBridge.openLogsDirectory()` IPC)
