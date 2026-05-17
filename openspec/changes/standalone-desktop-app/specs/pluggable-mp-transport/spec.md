## ADDED Requirements

### Requirement: WS hub accepts any `Transport` implementation

The WebSocket hub in `server/internal/ws/` SHALL be refactored so that the per-client byte channel is expressed as a `Transport` interface (read messages, write messages, close, peer identity). The existing WebSocket-over-TCP code SHALL be one implementation of this interface; new implementations (including Steam Networking Sockets) SHALL plug in without changes to hub logic, lobby logic, or game-state serialisation.

#### Scenario: WebSocket transport still works

- **WHEN** a SPA opens a WebSocket connection to `/ws`
- **THEN** the hub wraps the upgraded connection in the WebSocket `Transport` implementation
- **AND** existing client message routing, broadcast, and disconnect behaviour are unchanged

#### Scenario: Second transport registers successfully

- **WHEN** the `SteamBridge` registers a Steam Networking Sockets transport for an inbound peer connection
- **THEN** the hub treats the peer as a regular hub client, assigning it a client id and routing messages identically to a WebSocket client
- **AND** code in the hub that handles client lifecycle, message routing, or broadcast does not contain any transport-specific branches

### Requirement: `Transport.PeerIdentity` has a defined typed shape

The `Transport` interface's `PeerIdentity()` method SHALL return a typed value (not a free-form `string` or `interface{}`) with a documented shape that the hub can match against without per-transport branches:

```go
type PeerIdentity struct {
    Kind   PeerKind // "websocket" | "steam" | "fake" (test-only)
    Addr   string   // transport-specific opaque address: "host:port" for websocket, decimal SteamID64 for steam, test-id for fake
}
```

Hub code SHALL log and persist `PeerIdentity` values by their string form (e.g., `"websocket:127.0.0.1:54321"` or `"steam:76561198012345678"`) so log lines, error messages, and joiner-id-in-lobby remain stable across transport choices. Hub code SHALL NOT switch behaviour on `Kind` — the kind is for diagnostics and display only, never for game logic.

#### Scenario: WebSocket transport peer identity

- **WHEN** the hub registers a peer from a WebSocket upgrade with remote address `192.168.1.50:38112`
- **THEN** the peer's `PeerIdentity()` returns `{Kind: PeerKindWebSocket, Addr: "192.168.1.50:38112"}`
- **AND** the hub's log line for the connect event contains `peer=websocket:192.168.1.50:38112`

#### Scenario: Steam Sockets transport peer identity

- **WHEN** the host's hub accepts a Steam Networking Sockets connection from a peer with SteamID 76561198012345678
- **THEN** the peer's `PeerIdentity()` returns `{Kind: PeerKindSteam, Addr: "76561198012345678"}`
- **AND** the hub's log line for the connect event contains `peer=steam:76561198012345678`

#### Scenario: Hub never branches on Kind

- **WHEN** any hub-internal code path (routing, broadcast, disconnect, lobby membership) processes a peer
- **THEN** the code does NOT contain a `switch peer.Kind` or equivalent branch
- **AND** any per-transport branching lives inside the `Transport` implementation, not in the hub

### Requirement: Transport-agnostic protocol bytes

The protocol bytes sent over any `Transport` implementation SHALL be exactly the same as those sent over the WebSocket transport today. No transport SHALL be permitted to modify, reframe, or annotate the game-state byte stream.

#### Scenario: Identical bytes across transports

- **WHEN** the same game event is broadcast to two clients connected via different transport implementations
- **THEN** both clients receive byte-identical message payloads

### Requirement: Determinism is not affected by transport choice

The Rust shell, the IPC channel, and the Steam Networking Sockets transport SHALL NOT be on the tick path. The host's authoritative simulation SHALL be unaware of which transport delivers a given client's messages.

#### Scenario: Tick loop does not invoke shell or IPC

- **WHEN** the tick loop processes a game state update
- **THEN** no code path inside the tick loop reads from or writes to the IPC channel
- **AND** no code path inside the tick loop calls into any `Transport` implementation in a way that blocks on remote I/O

### Requirement: Transport failure handled identically across transports

A close or error on any `Transport` implementation SHALL be handled by the hub identically to a WebSocket close — by treating the affected client as disconnected and applying the existing reconnect/cleanup behaviour.

#### Scenario: Steam Sockets connection drops

- **WHEN** a Steam Networking Sockets transport reports closed because the peer's network dropped
- **THEN** the hub removes the affected client and runs the same cleanup it runs for a WebSocket close
- **AND** other clients connected over any transport are unaffected

### Requirement: Single-player byte-traffic regression guard

The Transport refactor required for pluggability SHALL leave single-player WebSocket *payload* traffic byte-identical to its current behaviour. This is the explicit regression guard that single-player gameplay is not collateral damage from making the hub MP-pluggable. Comparison SHALL be at the application-protocol payload level, NOT at the WebSocket-frame level (which includes timing-dependent control frames and would trivially diverge).

#### Scenario: Baseline captured before refactor

- **WHEN** the preparatory PR landing immediately before the Transport refactor is opened
- **THEN** that PR commits a captured baseline of the application-protocol bytes from a deterministic scripted single-player scenario (fixed seed, fixed intent sequence) to `server/internal/ws/testdata/sp_baseline_*.bin`
- **AND** the Transport refactor PR's CI uses this baseline as the regression target

#### Scenario: Single-player traffic before and after refactor

- **WHEN** the same single-player scripted scenario is run against the post-refactor server binary
- **THEN** the application-protocol payload bytes sent from the server to the SPA are byte-identical to the committed baseline
- **AND** the application-protocol payload bytes sent from the SPA to the server are byte-identical to the committed baseline
- **AND** the test asserting this lives in the WS hub package so the guard is part of the standard CI run

#### Scenario: Rollback path

- **WHEN** the Transport refactor is found to regress single-player behaviour after merge
- **THEN** reverting the refactor PR is sufficient to restore prior behaviour
- **AND** no subsequent PR has built on top of the refactor in a way that would require a more complex rollback (i.e., the refactor lands as a single, isolated PR)

#### Scenario: Baseline regeneration is intentional

- **WHEN** a future PR intentionally changes the application protocol in a way that alters the byte stream
- **THEN** that PR regenerates the baseline and updates the committed testdata file
- **AND** the PR description explains why the protocol change is intentional

### Requirement: Steam Networking Sockets uses Reliable + ordered send mode

When the Steam Networking Sockets transport is used to carry game-state traffic, sends SHALL use `k_nSteamNetworkingSend_Reliable` so that delivery is reliable AND ordered, matching the existing WebSocket transport's semantics.

#### Scenario: Send mode

- **WHEN** the Steam Networking Sockets transport implementation sends a message over a connection
- **THEN** the underlying `ISteamNetworkingSockets::SendMessageToConnection` call uses `k_nSteamNetworkingSend_Reliable` as its send flag
- **AND** no path through this transport uses `k_nSteamNetworkingSend_Unreliable` or `k_nSteamNetworkingSend_ReliableNoNagle`

### Requirement: Direct connect end-to-end keeps working through Phase 2

A CI integration test SHALL exercise the Direct connect path end-to-end (two locally-running Go servers + `transportbridge` between them, one full lobby join + game-state exchange + clean disconnect) and SHALL run on every PR. Phase 2 PRs (Steam Sockets transport, Steam lobby sync IPC) that break this test SHALL NOT land.

#### Scenario: Direct connect integration test runs in CI

- **WHEN** any PR opens against the repository
- **THEN** CI runs an integration test that spawns two Go server instances, connects them via `transportbridge`, runs one scripted match end to end, asserts a clean disconnect
- **AND** the test passes regardless of Phase 2 work in the same or prior PRs

### Requirement: Host disconnect ends the match for joiners

When the authoritative host disconnects (network drop, crash, intentional quit) during an in-progress multiplayer match, every joiner SHALL receive a defined terminal state and the host's Go-side lobby SHALL be torn down. Host migration is NOT performed.

#### Scenario: Host network drop mid-match

- **WHEN** the host's network drops during an in-progress MP match
- **THEN** each joiner's transport reports closed within the existing transport-failure timeout
- **AND** each joiner's SPA renders a "Match ended — host disconnected" terminal state with a single "Return to menu" action
- **AND** no joiner's SPA offers a "continue without host" or "promote to host" option
- **AND** the host's `LobbyManager` entry is removed when (or shortly after) the host process re-launches

#### Scenario: Host intentional quit mid-match

- **WHEN** the host closes the application window during an in-progress MP match
- **THEN** the shell's normal shutdown path tears down the Go server (which closes all transports)
- **AND** every joiner sees the same "Match ended — host disconnected" terminal state as the network-drop case
