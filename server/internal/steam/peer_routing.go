package steam

// Peer-event routing for the Steam Networking Sockets transport bridge.
// Lives in its own file to keep ipc.go focused on the synchronous
// request/response side. The wire format and the four request methods are
// defined in §12 of the standalone-desktop-app change (see tasks.md header).
//
// Threading:
//   - new_peer_transport / peer_message / peer_disconnected arrive on the
//     IPCBridge reader goroutine. The dispatchers below are registered via
//     OnEvent at construction time.
//   - Notification handlers MUST NOT block (the contract from ipc.go) — the
//     handlers here perform map lookups + a non-blocking channel send into
//     the per-peer PeerSink. The sink chooses whether to buffer or drop.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"strconv"
)

// PeerRole identifies which end of the connection this Go process is for a
// given peer. Used for diagnostics; routing does not branch on it.
type PeerRole string

const (
	PeerRoleHost   PeerRole = "host"
	PeerRoleJoiner PeerRole = "joiner"
)

// PeerSink is the per-peer callback set the upstream layer (ws.steamTransport)
// implements. The IPCBridge invokes Deliver on each inbound message and
// Disconnect when the peer leaves. Implementations MUST be safe for
// concurrent calls and MUST NOT block — they are invoked on the bridge's
// reader goroutine.
type PeerSink interface {
	// Deliver routes an inbound message payload to this peer's transport.
	// The payload byte slice is freshly allocated and owned by the callee;
	// the bridge does not retain a reference.
	Deliver(payload []byte)
	// Disconnect signals that the peer's underlying connection has closed.
	// The bridge will not call Deliver again for this peer after this; the
	// upstream layer should close the transport so the hub's cleanup runs.
	Disconnect(reason int32)
}

// NewPeerHandler is invoked when the shell reports a new peer transport.
// The handler builds the upstream transport (typically a ws.steamTransport),
// registers it with the hub, and returns the PeerSink to which the bridge
// will deliver subsequent peer events. Returning nil rejects the peer and
// causes the bridge to fire ClosePeer.
type NewPeerHandler func(peerID uint64, steamID64 uint64, role PeerRole) PeerSink

// SetPeerHandler installs the new-peer handler and registers the three peer
// event dispatchers with the bridge's IPC reader. Safe to call once at
// bridge construction time; calling more than once replaces the handler.
//
// The bridge holds the PeerSink for every active peer in a private map; the
// upstream layer SHALL call ClosePeer (which fires close_peer over IPC) when
// it tears down a transport so the bridge can drop its sink reference.
func (b *IPCBridge) SetPeerHandler(handler NewPeerHandler) {
	b.peerMu.Lock()
	b.peerHandler = handler
	if b.peers == nil {
		b.peers = make(map[uint64]PeerSink)
	}
	b.peerMu.Unlock()

	b.OnEvent("new_peer_transport", b.handleNewPeerTransport)
	b.OnEvent("peer_message", b.handlePeerMessage)
	b.OnEvent("peer_disconnected", b.handlePeerDisconnected)
}

func (b *IPCBridge) handleNewPeerTransport(params json.RawMessage) {
	var p struct {
		PeerID    string `json:"peerId"`
		SteamID64 string `json:"steamId64"`
		Role      string `json:"role"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		log.Printf("steam ipc: new_peer_transport: bad params: %v", err)
		return
	}
	peerID, err := strconv.ParseUint(p.PeerID, 10, 64)
	if err != nil {
		log.Printf("steam ipc: new_peer_transport: bad peerId %q: %v", p.PeerID, err)
		return
	}
	steamID, err := strconv.ParseUint(p.SteamID64, 10, 64)
	if err != nil {
		log.Printf("steam ipc: new_peer_transport: bad steamId64 %q: %v", p.SteamID64, err)
		return
	}
	role := PeerRole(p.Role)
	if role != PeerRoleHost && role != PeerRoleJoiner {
		log.Printf("steam ipc: new_peer_transport: unknown role %q (defaulting to host)", p.Role)
		role = PeerRoleHost
	}

	b.peerMu.RLock()
	handler := b.peerHandler
	b.peerMu.RUnlock()
	if handler == nil {
		log.Printf("steam ipc: new_peer_transport: no handler set; closing peer %d", peerID)
		// Best-effort close. The bridge call may block briefly; do it in a
		// goroutine because we're on the reader thread.
		go func(id uint64) {
			if err := b.ClosePeer(id); err != nil {
				log.Printf("steam ipc: ClosePeer(%d) after no-handler reject failed: %v", id, err)
			}
		}(peerID)
		return
	}

	sink := handler(peerID, steamID, role)
	if sink == nil {
		go func(id uint64) {
			if err := b.ClosePeer(id); err != nil {
				log.Printf("steam ipc: ClosePeer(%d) after handler reject failed: %v", id, err)
			}
		}(peerID)
		return
	}

	b.peerMu.Lock()
	b.peers[peerID] = sink
	b.peerMu.Unlock()
}

func (b *IPCBridge) handlePeerMessage(params json.RawMessage) {
	var p struct {
		PeerID  string `json:"peerId"`
		Payload string `json:"payload"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		log.Printf("steam ipc: peer_message: bad params: %v", err)
		return
	}
	peerID, err := strconv.ParseUint(p.PeerID, 10, 64)
	if err != nil {
		log.Printf("steam ipc: peer_message: bad peerId %q: %v", p.PeerID, err)
		return
	}
	payload, err := base64.StdEncoding.DecodeString(p.Payload)
	if err != nil {
		log.Printf("steam ipc: peer_message peer=%d: payload not base64: %v", peerID, err)
		return
	}

	b.peerMu.RLock()
	sink, ok := b.peers[peerID]
	b.peerMu.RUnlock()
	if !ok {
		log.Printf("steam ipc: peer_message: no sink for peer %d (dropped)", peerID)
		return
	}
	sink.Deliver(payload)
}

func (b *IPCBridge) handlePeerDisconnected(params json.RawMessage) {
	var p struct {
		PeerID string `json:"peerId"`
		Reason int32  `json:"reason"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		log.Printf("steam ipc: peer_disconnected: bad params: %v", err)
		return
	}
	peerID, err := strconv.ParseUint(p.PeerID, 10, 64)
	if err != nil {
		log.Printf("steam ipc: peer_disconnected: bad peerId %q: %v", p.PeerID, err)
		return
	}

	b.peerMu.Lock()
	sink, ok := b.peers[peerID]
	delete(b.peers, peerID)
	b.peerMu.Unlock()
	if !ok {
		return
	}
	sink.Disconnect(p.Reason)
}

// ForgetPeer drops the bridge's sink reference for peerID. Called by the
// upstream transport's Close path so a Go-side close (transport.Close())
// doesn't leave the bridge holding a dangling sink. The shell-side close is
// handled separately by ClosePeer.
func (b *IPCBridge) ForgetPeer(peerID uint64) {
	b.peerMu.Lock()
	delete(b.peers, peerID)
	b.peerMu.Unlock()
}

// activePeerCount is used by tests to verify the bridge's peer map state.
func (b *IPCBridge) activePeerCount() int {
	b.peerMu.RLock()
	defer b.peerMu.RUnlock()
	return len(b.peers)
}

// ----- Request methods (Go → Rust shell) -----------------------------------

// OpenListener asks the shell to open a Steam Networking Sockets listener
// on virtualPort. Idempotent on the shell side. Host-only.
func (b *IPCBridge) OpenListener(virtualPort int) error {
	return b.call(context.Background(), "open_listener", map[string]int{"virtualPort": virtualPort}, nil)
}

// ConnectTo asks the shell to ConnectP2P to a remote Steam user. Returns
// the opaque peerID that subsequent SendPeerMessage / ClosePeer calls use
// to address this peer. Joiner-only.
func (b *IPCBridge) ConnectTo(steamID64 uint64, virtualPort int) (uint64, error) {
	var raw struct {
		PeerID string `json:"peerId"`
	}
	params := map[string]any{
		"steamId64":   strconv.FormatUint(steamID64, 10),
		"virtualPort": virtualPort,
	}
	if err := b.call(context.Background(), "connect_to", params, &raw); err != nil {
		return 0, err
	}
	return strconv.ParseUint(raw.PeerID, 10, 64)
}

// SendPeerMessage asks the shell to forward `payload` to the peer addressed
// by peerID. The shell always uses Reliable + Ordered send mode (§12.0).
func (b *IPCBridge) SendPeerMessage(peerID uint64, payload []byte) error {
	params := map[string]string{
		"peerId":  strconv.FormatUint(peerID, 10),
		"payload": base64.StdEncoding.EncodeToString(payload),
	}
	return b.call(context.Background(), "send_peer_message", params, nil)
}

// ClosePeer asks the shell to close the Steam Sockets connection for
// peerID. Returns true when the shell reports the peer was found and
// closed; false when no such peer existed (treated as a no-op success).
func (b *IPCBridge) ClosePeer(peerID uint64) error {
	var raw struct {
		Closed bool `json:"closed"`
	}
	params := map[string]string{"peerId": strconv.FormatUint(peerID, 10)}
	return b.call(context.Background(), "close_peer", params, &raw)
}

// Note: peerMu, peers, and peerHandler are declared on IPCBridge in ipc.go
// (Go structs can't be extended across files). The fields are documented at
// their declaration; this file owns the methods that operate on them.
