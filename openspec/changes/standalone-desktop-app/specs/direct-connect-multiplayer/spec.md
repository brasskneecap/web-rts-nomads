## ADDED Requirements

### Requirement: Host UI exposes "Allow LAN/Internet connections" toggle

The SPA SHALL provide a host-mode toggle in the lobby UI labelled "Allow LAN/Internet connections" (exact wording is a design decision; the requirement is the presence and behaviour). The toggle SHALL be opt-in and persistently default to off.

#### Scenario: Toggle off (default)

- **WHEN** the host loads the lobby UI for the first time on a given install
- **THEN** the toggle is off
- **AND** the Go server's listener is not reachable from any non-loopback address

#### Scenario: Toggle on

- **WHEN** the host enables the toggle
- **THEN** the Go server's WebSocket upgrade handler begins accepting non-loopback peers (the listener was already bound `0.0.0.0` at startup; only the accept-time gate changes — see design D11 for the rationale against a listener rebind)
- **AND** the SPA enumerates all of the host machine's non-loopback IPv4 addresses and displays them in priority order (Tailscale CGNAT `100.64.0.0/10` first, then RFC1918 private ranges `10.0.0.0/8`/`172.16.0.0/12`/`192.168.0.0/16`, then everything else)
- **AND** the first item in the sort order is preselected for the host to copy/share
- **AND** the SPA exposes a "copy" affordance for each enumerated address

#### Scenario: Toggle off after being on

- **WHEN** the host disables the toggle after it was on
- **THEN** subsequent connection attempts from non-loopback addresses are refused
- **AND** existing non-loopback connections established while the toggle was on are NOT terminated by the toggle change
- **AND** the host UI shows a one-line note that toggling off does not kick already-connected joiners ("Toggle does not end this lobby — close the lobby to do that.")

### Requirement: Joiner UI accepts a `host:port` connection string

The SPA SHALL provide a joiner-mode entry point labelled "Direct connect" in the lobby UI that accepts a `host:port` string entered by the user.

#### Scenario: Successful join

- **WHEN** the joiner enters a `host:port` reachable over the network and submits the form
- **THEN** the joiner's local Go server opens a WebSocket connection to the host's server using the entered address
- **AND** the joiner is registered as a client of the host's WS hub via the transport-bridge proxy (see `steam-invite-multiplayer` for the proxy mechanism; the same proxy is reused here)
- **AND** the SPA navigates to the in-lobby view

#### Scenario: Join attempt fails to reach host

- **WHEN** the joiner enters a `host:port` that cannot be reached within 5 seconds
- **THEN** the SPA displays an error message containing the address and the underlying TCP error class (DNS failure, refused, timeout)
- **AND** no automatic retry is performed

#### Scenario: Join attempt to host with toggle off

- **WHEN** the joiner attempts to connect to a `host:port` whose host has the "Allow LAN/Internet connections" toggle off
- **THEN** the connection attempt fails the same way it would for an unreachable host

### Requirement: Direct connect works without Steamworks

The Direct connect host and join flows SHALL function correctly when Steamworks is not initialised, including: when the game is run outside Steam entirely, when Steam is not running, and when the shipped Steam build runs without Steam network access.

#### Scenario: Direct connect with Steam not running

- **WHEN** two users launch the packaged build with Steam not running on either machine
- **THEN** one user can host with the "Allow LAN/Internet connections" toggle on
- **AND** the other user can join via the entered `host:port`
- **AND** a full match can be played to completion

### Requirement: Transport-bridge handshake validates server versions

Before the joiner's local Go server registers a remote `host:port` connection as a proxy client, it SHALL exchange a small handshake that includes each side's compiled server version (the same `version` value emitted in the `NOMADS_READY` line). On mismatch, the joiner's Go server SHALL close the connection with the same close code used for the SPA-↔-server version mismatch (failure mode #9 in the design doc), and the SPA SHALL render the same "Build mismatch — please restart the game" modal.

#### Scenario: Versions match

- **WHEN** a joiner submits a `host:port` and the host's server reports the same compiled version as the joiner's
- **THEN** the handshake succeeds and the joiner is registered as a proxy client

#### Scenario: Versions mismatch

- **WHEN** a joiner submits a `host:port` and the host's server reports a different compiled version
- **THEN** the joiner's Go server closes the transport with the documented version-mismatch close code
- **AND** the joiner's SPA renders the "Build mismatch — please restart the game" modal
- **AND** no game state is exchanged before the close

### Requirement: Direct-connect threat model is "share like a Discord link"

The Direct connect listener SHALL NOT require authentication beyond the address itself. Anyone who knows the host's `host:port` and is on a network with line-of-sight to it can join the host's lobby; this is the explicit threat model. The SPA host UI SHALL surface this as a one-line note next to the "Allow LAN/Internet connections" toggle (e.g., "Anyone with this address can join — share like a Discord link"). Optional per-session passcode authentication is out of scope for this change and SHALL be tracked as a follow-up enhancement.

Additionally: the Direct connect transport is **plaintext WebSocket** (`ws://`), not TLS (`wss://`). Player intents and game-state messages flowing between joiner and host across the network are visible to any party on the path (Wi-Fi neighbour, ISP, transit). For the LAN / Tailscale / friends-only use case this is acceptable; for a future "Direct connect over open Internet against an untrusted route" use case it is not. TLS support (certificate management, hostname verification, joiner UX for self-signed certs) is deliberately out of scope for this change and SHALL be tracked alongside the passcode enhancement.

#### Scenario: Plaintext disclosure is visible in the UI

- **WHEN** the host enables the "Allow LAN/Internet connections" toggle
- **THEN** the one-line warning next to the toggle conveys both the no-auth and the no-encryption properties (e.g., "Anyone with this address can join — and traffic is unencrypted, so share over private channels only")

#### Scenario: Toggle UI shows the threat model

- **WHEN** the host enables the "Allow LAN/Internet connections" toggle in the SPA
- **THEN** a one-line note next to the toggle warns the user that anyone with the displayed address can join

#### Scenario: No passcode prompt on join

- **WHEN** a joiner submits a reachable `host:port`
- **THEN** the joiner is added to the lobby (subject to version handshake) without any passcode prompt

### Requirement: NAT traversal is explicitly out of scope for Direct connect

The Direct connect path SHALL NOT attempt UPnP, NAT-PMP, STUN, TURN, or any other form of NAT traversal or hole-punching. The user is responsible for reachability — either via LAN, a VPN-like overlay network (e.g., Tailscale), or a port-forwarded WAN address.

#### Scenario: Host behind NAT without port forwarding

- **WHEN** the host enables the toggle while behind a NAT without a forwarded port and joiners attempt to connect from outside the LAN
- **THEN** the join attempts fail with the existing "Couldn't reach `host:port`" error
- **AND** no automatic NAT-traversal behaviour is attempted

### Requirement: Direct connect remains available in the shipped Steam build

The Direct connect host and join flows SHALL remain available in the shipped Steam build alongside the Steam friend-invite flow.

#### Scenario: Both flows available in shipped build

- **WHEN** the user opens the multiplayer host UI in the shipped Steam build
- **THEN** the UI offers both "Friend invite (Steam)" and "Direct connect (Address)" as host modes

### Requirement: WebSocket upgrade validates `Origin`

The Go server's WebSocket upgrade handler SHALL validate the `Origin` header on every upgrade request: requests without an `Origin` header (typical for non-browser WebSocket clients including the joiner's local Go server connecting via `transportbridge`) SHALL be accepted; requests whose `Origin` host is a loopback host (`127.0.0.1`, `localhost`, or `[::1]` — any port) SHALL be accepted; all other `Origin` values SHALL be rejected with HTTP 403. The check applies unconditionally in both loopback-only and non-loopback ("Allow LAN/Internet connections" toggle on) modes — the rule is identical; only the threat surface differs.

Port-agnostic loopback acceptance is deliberate: it covers the packaged build's webview (`http://127.0.0.1:<server-port>`), the browser dev loop's Vite-proxied connection (`Origin: http://localhost:5173` proxied to Go on `:8080`), and any future local devtool, without leaving a loopback-only exception that would otherwise need per-port allowlisting. A malicious browser extension running on the host could still forge a loopback `Origin`, but such an extension already has full browser-level control of the user account; that is out of scope.

Rationale: today the WS upgrade uses `CheckOrigin: func(_ *http.Request) bool { return true }` ([server/internal/ws/handlers.go:32](../../../server/internal/ws/handlers.go#L32)), which permits any browser-originated upgrade. With the listener on `0.0.0.0`, any web page the host loads in any browser on the same network can open a WebSocket to `http://<host-ip>:<port>/ws` and feed the host's authoritative server arbitrary intents. Constraining `Origin` blocks that vector with no impact on `transportbridge` (Go-to-Go traffic carries no `Origin` header) and no impact on the host's own SPA (loopback `Origin` is allowed).

#### Scenario: Browser upgrade from unrelated origin is rejected

- **WHEN** a WebSocket upgrade request arrives with `Origin: https://malicious.example` regardless of toggle state
- **THEN** the upgrade is rejected with HTTP 403
- **AND** no client is registered with the WS hub
- **AND** the rejection is logged once (not per attempt — log-flood guard) with the offending origin redacted to its registrable domain

#### Scenario: `transportbridge` Go-to-Go forwarding is accepted

- **WHEN** the joiner's local Go server opens a WebSocket connection to the host's `ws://host:port/ws` via `transportbridge` and sends no `Origin` header
- **THEN** the upgrade succeeds
- **AND** the joiner registers as a proxy client

#### Scenario: Host's own SPA continues to work

- **WHEN** the host's webview at `http://127.0.0.1:<port>` opens a WebSocket to `/ws`
- **THEN** the upgrade succeeds (loopback `Origin` host matches)

#### Scenario: Browser dev loop continues to work

- **WHEN** the SPA running under `npm run dev` at `http://localhost:5173` opens a WebSocket to the Go server's `/ws` (which the Vite proxy forwards, preserving the original `Origin: http://localhost:5173`)
- **THEN** the upgrade succeeds because the `Origin` host is `localhost` regardless of port
- **AND** the existing `CORS_ALLOWED_ORIGIN=http://localhost:5173` allow-list for HTTP requests is unchanged
