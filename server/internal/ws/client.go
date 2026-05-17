package ws

import (
	"encoding/json"
	"sync"
	"time"
)

const (
	// writeDeadline is the maximum time allowed to complete a single transport
	// write. Used by the WebSocket transport; other transports define their own
	// per-call deadlines.
	writeDeadline = 10 * time.Second

	// readLimit caps the size of an incoming WebSocket message. Client commands
	// are small JSON; 64 KB is a generous ceiling. Only the WebSocket transport
	// enforces this directly; others enforce their own size limits per spec
	// (Steam Sockets uses the IPC 1 MiB cap from D24, etc.).
	readLimit = 64 * 1024

	// pongWait is how long any transport waits for the next pong before
	// considering the connection dead. The heartbeat loop's timeout in
	// handlers.go MUST be at least this much.
	pongWait = 75 * time.Second
)

// Client is a hub-side handle for a connected peer. It is independent of the
// underlying byte channel — every transport-specific concern lives behind the
// Transport interface. Game-layer code (match.AddClient, BroadcastSnapshot)
// addresses Client through the MatchClient interface in package game.
type Client struct {
	transport Transport

	mu       sync.Mutex
	matchID  string
	playerID string
	lastPong time.Time
}

// NewClient wraps a Transport in a hub-ready Client. The transport's pong
// handler is wired to refresh the per-client liveness clock; transports that
// don't have a notion of pongs (e.g. Steam Sockets) simply never invoke the
// callback and the heartbeat loop will time the client out — which is the
// documented "transport reports closed" path in the spec.
func NewClient(transport Transport) *Client {
	c := &Client{
		transport: transport,
		lastPong:  time.Now(),
	}
	transport.SetPongHandler(c.TouchPong)
	return c
}

// Transport returns the underlying transport. Exposed (with a return value of
// the interface type) so hub diagnostics and tests can read PeerIdentity
// without reaching into Client internals; game-layer code MUST NOT use this.
func (c *Client) Transport() Transport { return c.transport }

// PeerIdentity is a shortcut for c.Transport().PeerIdentity(); used in log lines.
func (c *Client) PeerIdentity() PeerIdentity { return c.transport.PeerIdentity() }

// Read pulls the next inbound message from the transport. The read loop in
// handlers.go is the sole caller; concurrent reads are not supported and not
// needed.
func (c *Client) Read() ([]byte, error) { return c.transport.ReadMessage() }

// MatchID returns the client's current match ID, safe for concurrent access.
func (c *Client) MatchID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.matchID
}

// SetMatchID sets the client's current match ID, safe for concurrent access.
func (c *Client) SetMatchID(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.matchID = id
}

// PlayerID returns the client's player ID, safe for concurrent access.
func (c *Client) PlayerID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.playerID
}

// SetPlayerID sets the client's player ID, safe for concurrent access.
func (c *Client) SetPlayerID(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.playerID = id
}

// WriteJSON serializes v to JSON and sends it through the underlying transport
// as a single message. The marshalled bytes are the canonical "application-
// protocol payload bytes" the pluggable-mp-transport spec asserts byte-identity
// for across transports — encoding happens here, in transport-agnostic code,
// not inside any individual Transport implementation.
func (c *Client) WriteJSON(v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.transport.WriteMessage(payload)
}

// WritePing delegates to the transport. Returns the transport's error
// directly; the hub's heartbeat loop treats any non-nil error as a dead
// connection and cleans up.
func (c *Client) WritePing() error { return c.transport.WritePing() }

// TouchPong resets the per-client liveness clock. The transport invokes this
// via SetPongHandler on every inbound pong (or its transport-specific
// equivalent).
func (c *Client) TouchPong() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastPong = time.Now()
}

// LastPong returns when the most recent pong arrived (or the construction
// time for a brand-new client).
func (c *Client) LastPong() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastPong
}

// Close closes the underlying transport. Idempotent at the transport layer.
func (c *Client) Close() { _ = c.transport.Close() }
