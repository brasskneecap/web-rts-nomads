## ADDED Requirements

### Requirement: `SteamBridge` interface in the Go server

The Go server SHALL define a `SteamBridge` interface in `server/internal/steam/` exposing at minimum the following operations: fetch local Steam player identity, report an achievement, request the Steam friend-invite overlay, register a Steam Networking Sockets transport with the WS hub.

#### Scenario: Bridge surface

- **WHEN** any package in the Go server needs Steam functionality
- **THEN** that package depends only on the `SteamBridge` interface, not on any concrete implementation
- **AND** that package does not import any Steamworks-related symbols directly

### Requirement: `NoopBridge` implementation for non-Steam contexts

The `steam` package SHALL provide a `NoopBridge` implementation of `SteamBridge` whose methods are safe no-ops returning zero values or "unavailable" sentinel errors as appropriate. The `NoopBridge` SHALL be used whenever the server is not running under the Tauri shell.

#### Scenario: Server started by `air` dev workflow

- **WHEN** the Go server is started without `NOMADS_IPC_PATH` set in the environment
- **THEN** the server constructs and uses the `NoopBridge`
- **AND** every `SteamBridge` method invocation returns its no-op result without error

#### Scenario: Server started by shell with Steam unavailable

- **WHEN** the Go server is started with `NOMADS_IPC_PATH` set but the shell reports `steam_initialized = false` over IPC
- **THEN** the server still uses the `NoopBridge`

### Requirement: IPC-backed bridge implementation talks to the shell

The `steam` package SHALL provide an IPC-backed implementation of `SteamBridge` that communicates with the Rust shell over the path given by `NOMADS_IPC_PATH`. The wire format SHALL be newline-delimited JSON, one message per line.

#### Scenario: Synchronous bridge call round-trip

- **WHEN** game code calls a synchronous bridge method (e.g., `LocalPlayer()`, `OpenInviteOverlay()`, lobby create/join)
- **THEN** the bridge serialises a request JSON object to the IPC channel with a unique request id
- **AND** the bridge waits for the matching response JSON object before returning
- **AND** the bridge returns the response payload to the caller

#### Scenario: Shell unavailable during call

- **WHEN** the IPC channel is closed or unreachable while a bridge method is in flight
- **THEN** the bridge returns a `steam_channel_closed` error describing the IPC failure
- **AND** the bridge does not panic or block indefinitely

### Requirement: `ReportAchievement` is non-blocking (fire-and-forget)

The bridge's `ReportAchievement(id string)` method SHALL NOT block its caller on IPC round-trip latency. The method SHALL perform a **non-blocking** send onto a small buffered internal channel (size 64) and return immediately; a dedicated writer goroutine SHALL drain the channel and perform the IPC write. When the channel is full at send time, the report SHALL be dropped on the floor and a drop counter SHALL be incremented atomically; the writer goroutine SHALL emit a single summary warning log per batch of drops (no smaller than every 8 drops). The send SHALL NOT use an unbuffered channel, a blocking send, or any synchronisation primitive that can stall the caller for non-trivial time.

#### Scenario: Caller does not block on IPC latency

- **WHEN** game code calls `bridge.ReportAchievement("ACH_FIRST_WIN")` and the IPC writer is slow (e.g., the shell is paused on a debugger breakpoint) but the internal channel still has space
- **THEN** the call returns within microseconds
- **AND** the report is delivered to the shell when the writer goroutine drains it

#### Scenario: Caller does not block when writer queue is full

- **WHEN** game code calls `bridge.ReportAchievement(...)` and the bridge's internal channel is full (e.g., a burst of achievement-triggering events while IPC writes are stalled)
- **THEN** the call returns within microseconds
- **AND** the report is dropped on the floor
- **AND** the bridge's drop counter is incremented
- **AND** a single summary warning is logged once per batch (not once per dropped report)

#### Scenario: Achievement queued at channel close

- **WHEN** the IPC channel closes while one or more achievement reports are queued but not yet written
- **THEN** the queued reports are dropped
- **AND** a single warning is logged describing the count of dropped reports

#### Scenario: Tick-loop call is non-blocking under all conditions

- **WHEN** the simulation tick loop fires `bridge.ReportAchievement(...)` during a tick — whether the channel is empty, partially full, completely full, or the IPC writer goroutine is paused
- **THEN** the call returns within microseconds in every case
- **AND** the tick proceeds with no observable stall in any case

### Requirement: Per-call timeout for synchronous bridge calls

Every synchronous bridge call SHALL be subject to a 5-second timeout. On timeout, the bridge SHALL return a `steam_timeout` error and SHALL mark the in-flight request id as discarded so that a late response from the shell is ignored.

#### Scenario: Shell does not respond within 5 seconds

- **WHEN** a synchronous bridge call is in flight and 5 seconds elapse with no matching response on the IPC channel
- **THEN** the bridge returns `steam_timeout`
- **AND** a later response with that request id is discarded by the reader without delivering it

### Requirement: IPC message size cap

The bridge SHALL enforce a 1 MiB maximum size per newline-delimited JSON message. Messages exceeding the cap SHALL be dropped on the reader side with a logged error; the IPC channel SHALL survive the drop.

#### Scenario: Oversized message arrives

- **WHEN** a single JSON line on the IPC channel exceeds 1 MiB
- **THEN** the reader discards the line up to and including its terminating newline
- **AND** an error is logged identifying the message size
- **AND** subsequent well-formed messages on the channel continue to be processed normally

### Requirement: Channel close is terminal; bridge does not auto-reconnect

When the IPC channel closes for any reason during the server's lifetime, the bridge SHALL transition to a "closed" state. Every subsequent bridge call SHALL return `steam_channel_closed`. The bridge SHALL NOT attempt to reconnect, and the bridge instance SHALL NOT swap to `NoopBridge` mid-process.

#### Scenario: Channel close mid-session

- **WHEN** the IPC channel closes mid-session (e.g., the shell's pipe is gone)
- **THEN** every synchronous bridge method returns `steam_channel_closed` on its next call
- **AND** `ReportAchievement` silently drops pending and future reports
- **AND** the bridge instance held by other subsystems is unchanged (no swap to `NoopBridge`)

### Requirement: Steam-unavailable handling when shell is reachable but Steam is gone

The IPC-backed bridge SHALL distinguish two failure modes: channel-closed (handled above) and steam-unavailable (shell is alive but reports Steam is not initialised). On steam-unavailable, the bridge SHALL return a `steam_unavailable` error for Steam-side calls.

#### Scenario: User signs out of Steam mid-session

- **WHEN** the shell is alive and the IPC channel is open, but the user has signed out of Steam, and the SPA invokes `LocalPlayer()` via the bridge
- **THEN** the bridge returns `steam_unavailable`
- **AND** the SPA's lobby UI reacts by hiding Steam-mode entries (the same reaction as the cold-start "Steam not running" path)
- **AND** the bridge does not swap to `NoopBridge`

### Requirement: Bridge selection at server startup

The Go server's `main.go` SHALL select between `NoopBridge` and the IPC-backed bridge based on the presence of `NOMADS_IPC_PATH` in the environment at startup. The selection SHALL be made exactly once at startup; the bridge instance SHALL not change for the lifetime of the process.

#### Scenario: Selection branch

- **WHEN** the server starts up
- **THEN** the server inspects `NOMADS_IPC_PATH`
- **AND** the server constructs the IPC-backed bridge if and only if the env var is set and non-empty
- **AND** the server passes the resulting bridge instance to all dependent subsystems
