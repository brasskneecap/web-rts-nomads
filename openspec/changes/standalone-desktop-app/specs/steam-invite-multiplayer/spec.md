## ADDED Requirements

### Requirement: Steam-host flow creates a Steam Matchmaking lobby

The Rust shell SHALL expose a `create_lobby` IPC command that the SPA invokes via `desktopBridge.openLobby({mode, maxPlayers})`. The shell SHALL call `SteamMatchmaking::CreateLobby` with `LobbyType = FriendsOnly` and the requested max-player count. On `LobbyCreated_t` callback, the shell SHALL notify the Go server via IPC that the local server is the host for the resulting Steam lobby id.

#### Scenario: Host creates a friends-only lobby

- **WHEN** the host invokes `desktopBridge.openLobby({mode: "default", maxPlayers: 4})`
- **THEN** the shell calls `SteamMatchmaking::CreateLobby(LobbyType::FriendsOnly, 4)`
- **AND** on a successful `LobbyCreated_t` the shell records the Steam lobby id
- **AND** the shell notifies the Go server via IPC `{"op":"lobby_host","steam_lobby_id":<id>}`
- **AND** the Go server creates a regular lobby in its existing `LobbyManager`
- **AND** the SPA navigates to the lobby screen using the existing route

#### Scenario: Lobby creation fails

- **WHEN** `SteamMatchmaking::CreateLobby` invokes the `LobbyCreated_t` callback with a failure result
- **THEN** the shell returns an error to the SPA via the IPC response
- **AND** the SPA displays a "Could not create Steam lobby" message without crashing

### Requirement: Friend invite uses the Steam overlay

The Rust shell SHALL expose an `open_invite_overlay` IPC command that calls `ISteamFriends::ActivateGameOverlayInviteDialog` with the current lobby id.

#### Scenario: Host clicks "Invite friend"

- **WHEN** the host clicks "Invite friend" in the SPA
- **THEN** the shell calls `ActivateGameOverlayInviteDialog` with the lobby id stored at lobby creation
- **AND** the Steam overlay UI for inviting friends is displayed to the user

### Requirement: Steam friend-join opens a Networking Sockets connection

When a player accepts a friend invite, the joiner's shell SHALL call `SteamMatchmaking::JoinLobby` and, on `LobbyEnter_t`, SHALL read the host's Steam ID from the lobby metadata and open a Steam Networking Sockets connection to that host.

#### Scenario: Friend invite accepted while running

- **WHEN** the joiner's already-running shell receives `GameLobbyJoinRequested_t`
- **THEN** the shell calls `SteamMatchmaking::JoinLobby` with the invite's lobby id
- **AND** on `LobbyEnter_t` the shell reads the host's Steam ID from the lobby metadata
- **AND** the shell opens a Steam Networking Sockets connection to that Steam ID
- **AND** the shell hands the resulting connection handle to the local Go server via IPC

#### Scenario: Friend invite accepted from cold launch

- **WHEN** Steam launches the game with `+connect_lobby <id>` in argv (see `desktop-shell`)
- **THEN** after the SPA signals readiness, the shell runs the same `JoinLobby` + Networking Sockets flow as the warm-start case

### Requirement: Joiner-as-proxy forwarding via `transportbridge`

The Go server SHALL implement a `server/internal/transportbridge/` package that lets a joiner's local server act as a transparent proxy between the joiner's local SPA and the host's authoritative server. Joiners SHALL NOT run the simulation.

#### Scenario: Joiner forwards SPA intents upstream

- **WHEN** the joiner's SPA sends an intent message over its local WebSocket
- **THEN** the joiner's local server forwards the message bytes over the Steam Networking Sockets transport to the host
- **AND** the joiner's local server does not modify the message bytes
- **AND** the joiner's local server preserves the ordering of bytes as received (no reordering, no coalescing across application-protocol message boundaries)

#### Scenario: Joiner forwards host state downstream

- **WHEN** the host's server sends a game-state message addressed to the joiner over the Steam Networking Sockets transport
- **THEN** the joiner's local server forwards the message bytes over the joiner's local WebSocket to the SPA
- **AND** the joiner's local server does not modify the message bytes
- **AND** the joiner's local server preserves the ordering of bytes as received

### Requirement: Steam lobby ↔ Go `LobbyManager` state sync is one-way (Steam → Go)

The Steam Matchmaking lobby SHALL be the source of truth for membership, game mode, and max-player count. The Go-side `LobbyManager` lobby SHALL mirror those values; changes to the mirror that did not originate from a Steam callback SHALL NOT propagate back to the Steam lobby.

#### Scenario: Player joins via Steam

- **WHEN** the Rust shell receives a `LobbyChatUpdate_t` callback indicating a new member entered the Steam lobby
- **THEN** the shell sends an IPC message to the Go server adding that player to the mirrored Go lobby
- **AND** the Go lobby's membership matches the Steam lobby's membership after the call returns

#### Scenario: Player leaves via Steam

- **WHEN** the Rust shell receives a `LobbyChatUpdate_t` callback indicating a member left the Steam lobby
- **THEN** the shell sends an IPC message to the Go server removing that player from the mirrored Go lobby
- **AND** any transports for the departed player are closed

#### Scenario: Mode or max-player metadata changes

- **WHEN** the host changes `mode` or `max_players` via the SPA
- **THEN** the SPA invokes a `desktopBridge` IPC that updates the Steam lobby metadata first
- **AND** the SPA shows a spinner / disabled-state on the changed control during the round-trip (pessimistic UI — no optimistic snap-to-new-value)
- **AND** on the resulting `LobbyDataUpdate_t` callback the shell mirrors the change into the Go lobby
- **AND** the SPA clears the spinner and reflects the new value only after the callback round-trip completes
- **AND** the Go lobby never holds a `mode` or `max_players` value that the Steam lobby has not committed

#### Scenario: Sync mismatch detected

- **WHEN** the shell receives a Steam lobby callback whose membership does not match the Go lobby's mirrored state (e.g., due to a missed callback after a Steam network blip)
- **THEN** the shell sends a full-state IPC `{"op":"lobby_resync", ...}` containing the current Steam lobby membership and metadata
- **AND** the Go server replaces its mirrored lobby state with the message contents

### Requirement: Steam Sockets connection drop triggers existing reconnect path

A `Closed`/`Problem` callback on a Steam Networking Sockets transport SHALL be surfaced to the WS hub identically to a WebSocket close (see `pluggable-mp-transport`). The SPA's existing reconnect UI SHALL apply.

#### Scenario: Joiner network blip

- **WHEN** the Steam Networking Sockets transport between joiner and host reports closed because the joiner's network dropped
- **THEN** the joiner's SPA sees a transport disconnect equivalent to a WebSocket close
- **AND** the joiner's SPA shows the existing "Reconnecting…" banner
- **AND** if the connection cannot be reestablished within 30 seconds, the SPA shows the existing "Disconnected" terminal state

### Requirement: Host disappearance ends the match (no migration)

When the authoritative host disconnects mid-match in a Steam-invite lobby (network drop, crash, intentional quit), every joiner SHALL receive the same "Match ended — host disconnected" terminal state described in `pluggable-mp-transport`. The Steam lobby SHALL be left for Steam's normal cleanup (lobby times out when its owner is gone); no joiner is promoted to host.

#### Scenario: Host quits a Steam-invite match

- **WHEN** the host closes the game window during a Steam-invite MP match
- **THEN** each joiner sees the "Match ended — host disconnected" terminal state
- **AND** no joiner's SPA offers a "promote to host" option
- **AND** no joiner's Go server attempts to take over authoritative simulation
