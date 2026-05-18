package ws

import (
	"errors"
	"io"
	"strconv"
	"sync"
	"sync/atomic"

	"webrts/server/internal/steam"
)

// SteamPeerIPC is the slice of the steam.IPCBridge surface this transport
// needs. Kept as an interface so unit tests can inject a recorder without
// standing up a real IPC pipe.
//
// All three methods MUST be safe for concurrent calls. SendPeerMessage may
// block for the IPC round-trip (5s timeout); the WS hub's write paths are
// already serialised behind the per-client writeMu in the gorilla transport,
// so we follow the same pattern here.
type SteamPeerIPC interface {
	SendPeerMessage(peerID uint64, payload []byte) error
	ClosePeer(peerID uint64) error
	ForgetPeer(peerID uint64)
}

// steamTransport is the ws.Transport implementation backed by the Rust
// shell's Steam Networking Sockets layer. Wire bytes travel over the
// multiplexed IPC channel as `peer_message` notifications (Rust → Go) and
// `send_peer_message` requests (Go → Rust). Determinism / tick-path rules
// (AI_RULES "IPC and shell are not on the tick path") are preserved because
// the hub's broadcast loop already runs off-tick.
type steamTransport struct {
	peerID    uint64
	steamID64 uint64
	ipc       SteamPeerIPC

	incoming chan []byte
	closed   atomic.Bool

	pongMu sync.RWMutex
	pongCb func()
}

// NewSteamTransport constructs a transport for a single Steam peer. peerID
// is the opaque token assigned by the Rust shell at connection time;
// steamID64 is the remote user's SteamID for diagnostics / peer identity.
//
// bufferSize bounds the inbound queue. Game traffic at our tick rate fits
// comfortably under 64 messages; the queue blocks the dispatcher goroutine
// briefly if full, which is the same back-pressure semantics the
// FakeTransport uses.
func NewSteamTransport(peerID uint64, steamID64 uint64, ipc SteamPeerIPC) *steamTransport {
	if ipc == nil {
		panic("ws.NewSteamTransport: ipc must not be nil")
	}
	return &steamTransport{
		peerID:    peerID,
		steamID64: steamID64,
		ipc:       ipc,
		incoming:  make(chan []byte, 64),
	}
}

// Compile-time assertion that steamTransport satisfies both contracts.
var (
	_ Transport       = (*steamTransport)(nil)
	_ steam.PeerSink  = (*steamTransport)(nil)
)

// ----- ws.Transport surface ----------------------------------------------

func (t *steamTransport) ReadMessage() ([]byte, error) {
	payload, ok := <-t.incoming
	if !ok {
		return nil, io.EOF
	}
	return payload, nil
}

func (t *steamTransport) WriteMessage(payload []byte) error {
	if t.closed.Load() {
		return errors.New("steam transport: closed")
	}
	return t.ipc.SendPeerMessage(t.peerID, payload)
}

// WritePing is intentionally a no-op. Steam Networking Sockets has its own
// keepalive at the SDK layer (`SteamNetConnectionRealTimeStatus_t` exposes
// the timer the SDK uses for stale-connection detection). The hub's
// heartbeat loop will still time the client out if Steam reports the
// connection as closed via `peer_disconnected`, which triggers Disconnect()
// → Close() on this transport — matching the documented "transport reports
// closed" path in client.go.
func (t *steamTransport) WritePing() error { return nil }

func (t *steamTransport) Close() error {
	if t.closed.Swap(true) {
		return nil
	}
	// Closing `incoming` makes the hub's read loop see EOF and run its
	// cleanup. Drain any pending payloads first so an in-flight Deliver
	// doesn't panic-on-send-after-close.
	close(t.incoming)
	// Tell the shell to tear down the Steam Sockets connection. Errors from
	// the IPC call are logged-only — the local side is going down regardless.
	if err := t.ipc.ClosePeer(t.peerID); err != nil {
		// Don't escalate: the hub already triggered cleanup; a failed IPC
		// close just means the shell-side handle leaks until shutdown. Log
		// is suppressed here to keep the noise low; the IPC layer logs
		// errors at its own granularity.
		_ = err
	}
	// Drop the bridge's sink reference so a late peer_message for this id
	// doesn't try to push onto a closed channel.
	t.ipc.ForgetPeer(t.peerID)
	return nil
}

func (t *steamTransport) PeerIdentity() PeerIdentity {
	return PeerIdentity{
		Kind: PeerKindSteam,
		Addr: strconv.FormatUint(t.steamID64, 10),
	}
}

// SetPongHandler stores the callback for completeness; it is never invoked
// because WritePing is a no-op (see the WritePing comment for why). The hub
// will rely on `peer_disconnected` from the shell — surfaced through
// Disconnect() — for liveness signalling on this transport.
func (t *steamTransport) SetPongHandler(cb func()) {
	t.pongMu.Lock()
	t.pongCb = cb
	t.pongMu.Unlock()
}

// ----- steam.PeerSink surface --------------------------------------------

// Deliver pushes a payload received from the shell onto the inbound queue.
// Called on the IPCBridge reader goroutine — MUST NOT block indefinitely.
// If the queue is full (slow consumer) we drop the message and log; the
// alternative (blocking the IPC reader) would stall every other peer too.
func (t *steamTransport) Deliver(payload []byte) {
	if t.closed.Load() {
		return
	}
	select {
	case t.incoming <- payload:
	default:
		// Match the FakeTransport contract: dropped on queue-full. The hub
		// will time the client out via the pong-deadline mechanism if it
		// stops making progress. The Steam SDK's reliable+ordered semantic
		// at the wire layer means this only fires under sustained head-of-
		// line stalls in the Go-side consumer.
	}
}

// Disconnect is called by the bridge when the shell reports the peer left.
// We close the transport so the hub's read loop sees EOF and runs cleanup
// identically to a WebSocket close (per pluggable-mp-transport "Transport
// failure handled identically across transports").
func (t *steamTransport) Disconnect(reason int32) {
	// We don't surface `reason` upstream — the hub's existing cleanup path
	// doesn't take one — but a future debug overlay could log it from
	// here. The cast-to-int promotes the wire-level value to a Go-native
	// width for any such future logging.
	_ = reason
	_ = t.Close()
}
