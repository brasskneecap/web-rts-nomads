package ws

import "fmt"

// PeerKind identifies which Transport implementation a peer is connected on.
// Hub code uses PeerKind for diagnostics (log lines, peer-display strings)
// ONLY. Game logic, message routing, broadcast, lifecycle, and lobby code
// MUST NOT switch on PeerKind — any per-transport behaviour lives inside the
// Transport implementation itself. See the pluggable-mp-transport spec.
type PeerKind int

const (
	// PeerKindUnknown is the zero value. A peer reporting this kind is a bug;
	// every concrete transport MUST set a meaningful kind.
	PeerKindUnknown PeerKind = iota
	// PeerKindWebSocket — WebSocket-over-TCP, the existing default transport.
	PeerKindWebSocket
	// PeerKindSteam — Steam Networking Sockets, registered by the Rust shell
	// via the SteamBridge (Phase 2).
	PeerKindSteam
	// PeerKindFake — in-memory transport for hub and protocol tests. Never
	// reaches a production code path.
	PeerKindFake
)

// String returns the short transport name used in log lines and the
// PeerIdentity.String() format ("websocket", "steam", "fake", "unknown").
func (k PeerKind) String() string {
	switch k {
	case PeerKindWebSocket:
		return "websocket"
	case PeerKindSteam:
		return "steam"
	case PeerKindFake:
		return "fake"
	default:
		return "unknown"
	}
}

// PeerIdentity is the typed identifier the hub uses for log lines and peer
// display. Hub code log lines, error messages, and joiner-id-in-lobby use the
// String() form so they remain stable across transport choices.
//
// Addr is transport-opaque:
//   - websocket: "host:port" (the remote address of the WebSocket conn)
//   - steam:    decimal SteamID64
//   - fake:     arbitrary test id, e.g. "test-client-1"
type PeerIdentity struct {
	Kind PeerKind
	Addr string
}

// String returns "<kind>:<addr>", e.g. "websocket:192.168.1.50:38112" or
// "steam:76561198012345678". This is the canonical log-line format.
func (p PeerIdentity) String() string {
	return fmt.Sprintf("%s:%s", p.Kind, p.Addr)
}

// Transport is the byte-channel surface the hub addresses each connected peer
// through. Implementations MUST:
//
//   - Be safe for concurrent calls to WriteMessage and WritePing from multiple
//     goroutines (the snapshot broadcaster, the heartbeat loop, etc.).
//   - Serialise ReadMessage calls (single-goroutine read loop; concurrency on
//     reads is not supported by gorilla/websocket and not needed here).
//   - Treat payload bytes as opaque — no reframing, no annotation, no
//     compression. The "Identical bytes across transports" guarantee in the
//     pluggable-mp-transport spec depends on this.
//   - Make Close idempotent.
type Transport interface {
	// ReadMessage blocks until a message is available, returning its payload
	// bytes. The protocol bytes MUST be returned unchanged.
	ReadMessage() ([]byte, error)
	// WriteMessage sends the payload bytes as a single message. Safe for
	// concurrent callers.
	WriteMessage(payload []byte) error
	// WritePing sends a transport-level liveness probe. WebSocket sends a
	// ping control frame; FakeTransport records the call; Steam Sockets is a
	// no-op (Steam handles its own keepalive). Safe for concurrent callers.
	WritePing() error
	// Close closes the underlying channel. Idempotent — calling Close more
	// than once returns nil after the first call.
	Close() error
	// PeerIdentity returns the typed identifier of the peer at the other end.
	PeerIdentity() PeerIdentity
	// SetPongHandler registers a callback fired by the transport when the
	// peer responds to a WritePing. Used by the hub to extend the per-client
	// read deadline. Implementations with no notion of pongs invoke the
	// callback immediately after a successful WriteMessage round-trip, or
	// never (in which case the heartbeat loop tears the client down on
	// timeout — the documented "transport reports closed" path).
	SetPongHandler(cb func())
}
