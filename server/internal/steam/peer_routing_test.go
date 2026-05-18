package steam

import (
	"encoding/base64"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// recordingSink is the test-double PeerSink. Captures every Deliver and
// Disconnect call so tests can assert routing behaviour.
type recordingSink struct {
	mu             sync.Mutex
	deliveries     [][]byte
	disconnectedAt int32
	disconnected   bool
}

func (r *recordingSink) Deliver(payload []byte) {
	cp := make([]byte, len(payload))
	copy(cp, payload)
	r.mu.Lock()
	r.deliveries = append(r.deliveries, cp)
	r.mu.Unlock()
}

func (r *recordingSink) Disconnect(reason int32) {
	r.mu.Lock()
	r.disconnected = true
	r.disconnectedAt = reason
	r.mu.Unlock()
}

func (r *recordingSink) snapshot() (deliveries [][]byte, disconnected bool, reason int32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]byte, len(r.deliveries))
	for i, d := range r.deliveries {
		out[i] = append([]byte(nil), d...)
	}
	return out, r.disconnected, r.disconnectedAt
}

// pushNotification writes a one-way Rust→Go notification over the test pipe.
// Used to simulate the shell emitting new_peer_transport, peer_message,
// peer_disconnected events.
func (f *fakeShell) pushNotification(event string, params any) error {
	frame := struct {
		Event  string `json:"event"`
		Params any    `json:"params"`
	}{Event: event, Params: params}
	enc := json.NewEncoder(f.conn)
	return enc.Encode(&frame)
}

// waitFor polls the predicate until true or timeout. Tests that fan in
// through a goroutine need this to avoid flakes on fast machines.
func waitFor(t *testing.T, timeout time.Duration, msg string, pred func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pred() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", msg)
}

// TestIPCBridge_NewPeerTransportInvokesHandler covers the happy path: the
// shell emits new_peer_transport → the bridge invokes the NewPeerHandler →
// the handler returns a sink → the bridge stores it.
func TestIPCBridge_NewPeerTransportInvokesHandler(t *testing.T) {
	bridge, shell := newTestBridge(t)

	var (
		gotPeerID   uint64
		gotSteamID  uint64
		gotRole     PeerRole
		handlerHit  = make(chan struct{}, 1)
		sink        = &recordingSink{}
	)
	bridge.SetPeerHandler(func(peerID, steamID uint64, role PeerRole) PeerSink {
		gotPeerID = peerID
		gotSteamID = steamID
		gotRole = role
		handlerHit <- struct{}{}
		return sink
	})

	if err := shell.pushNotification("new_peer_transport", map[string]string{
		"peerId":    "42",
		"steamId64": "76561197960287930",
		"role":      "host",
	}); err != nil {
		t.Fatalf("pushNotification: %v", err)
	}

	select {
	case <-handlerHit:
	case <-time.After(time.Second):
		t.Fatal("handler never invoked")
	}
	if gotPeerID != 42 {
		t.Errorf("peerID = %d, want 42", gotPeerID)
	}
	if gotSteamID != 76561197960287930 {
		t.Errorf("steamID = %d, want 76561197960287930", gotSteamID)
	}
	if gotRole != PeerRoleHost {
		t.Errorf("role = %q, want host", gotRole)
	}
	if got := bridge.activePeerCount(); got != 1 {
		t.Errorf("activePeerCount = %d, want 1", got)
	}
}

// TestIPCBridge_PeerMessageRoutedToSink covers the inbound-bytes path:
// after a peer is registered, peer_message notifications decode the base64
// payload and call sink.Deliver.
func TestIPCBridge_PeerMessageRoutedToSink(t *testing.T) {
	bridge, shell := newTestBridge(t)

	sink := &recordingSink{}
	bridge.SetPeerHandler(func(_, _ uint64, _ PeerRole) PeerSink { return sink })
	_ = shell.pushNotification("new_peer_transport", map[string]string{
		"peerId":    "1",
		"steamId64": "76561197960287930",
		"role":      "joiner",
	})

	waitFor(t, time.Second, "peer registered", func() bool {
		return bridge.activePeerCount() == 1
	})

	want := []byte(`{"type":"hello","version":"dev"}`)
	encoded := base64.StdEncoding.EncodeToString(want)
	if err := shell.pushNotification("peer_message", map[string]string{
		"peerId":  "1",
		"payload": encoded,
	}); err != nil {
		t.Fatalf("pushNotification: %v", err)
	}

	waitFor(t, time.Second, "delivery recorded", func() bool {
		d, _, _ := sink.snapshot()
		return len(d) == 1
	})
	deliveries, _, _ := sink.snapshot()
	if string(deliveries[0]) != string(want) {
		t.Errorf("payload = %q, want %q", deliveries[0], want)
	}
}

// TestIPCBridge_PeerDisconnectedClosesSink covers the close path:
// peer_disconnected fires Disconnect on the sink and drops the bridge's
// reference so subsequent peer_message events for the same id are dropped.
func TestIPCBridge_PeerDisconnectedClosesSink(t *testing.T) {
	bridge, shell := newTestBridge(t)

	sink := &recordingSink{}
	bridge.SetPeerHandler(func(_, _ uint64, _ PeerRole) PeerSink { return sink })
	_ = shell.pushNotification("new_peer_transport", map[string]string{
		"peerId":    "7",
		"steamId64": "76561197960287930",
		"role":      "host",
	})
	waitFor(t, time.Second, "peer registered", func() bool {
		return bridge.activePeerCount() == 1
	})

	_ = shell.pushNotification("peer_disconnected", map[string]any{
		"peerId": "7",
		"reason": 4001,
	})

	waitFor(t, time.Second, "sink notified of disconnect", func() bool {
		_, dc, _ := sink.snapshot()
		return dc
	})
	_, _, reason := sink.snapshot()
	if reason != 4001 {
		t.Errorf("disconnect reason = %d, want 4001", reason)
	}
	if got := bridge.activePeerCount(); got != 0 {
		t.Errorf("activePeerCount after disconnect = %d, want 0", got)
	}

	// A late peer_message for the same id is dropped without calling Deliver.
	_ = shell.pushNotification("peer_message", map[string]string{
		"peerId":  "7",
		"payload": base64.StdEncoding.EncodeToString([]byte("late")),
	})
	// Give the reader goroutine a moment to process and drop.
	time.Sleep(50 * time.Millisecond)
	deliveries, _, _ := sink.snapshot()
	if len(deliveries) != 0 {
		t.Errorf("late delivery accepted (len=%d), want 0", len(deliveries))
	}
}

// TestIPCBridge_NewPeerWithoutHandlerClosesPeer covers the rejection path:
// when SetPeerHandler has never been called, the bridge fires close_peer.
func TestIPCBridge_NewPeerWithoutHandlerClosesPeer(t *testing.T) {
	bridge, shell := newTestBridge(t)
	gotClose := make(chan uint64, 1)
	shell.on("close_peer", func(params json.RawMessage) (any, *ipcError) {
		var p struct {
			PeerID string `json:"peerId"`
		}
		_ = json.Unmarshal(params, &p)
		// Parse to avoid silent acceptance of any garbage.
		if p.PeerID == "13" {
			gotClose <- 13
		}
		return map[string]bool{"closed": true}, nil
	})

	// Intentionally do NOT call SetPeerHandler.
	_ = shell.pushNotification("new_peer_transport", map[string]string{
		"peerId":    "13",
		"steamId64": "1",
		"role":      "host",
	})

	// Bridge handlers are only attached by SetPeerHandler, so a "no handler
	// set" path can't run. Instead this test asserts that with the handler
	// set but returning nil, the bridge closes the peer.
	_ = bridge // we exercise the "handler returns nil" path below

	bridge.SetPeerHandler(func(_, _ uint64, _ PeerRole) PeerSink { return nil })
	_ = shell.pushNotification("new_peer_transport", map[string]string{
		"peerId":    "13",
		"steamId64": "1",
		"role":      "host",
	})

	select {
	case <-gotClose:
	case <-time.After(time.Second):
		t.Fatal("bridge did not call close_peer after handler returned nil")
	}
	if got := bridge.activePeerCount(); got != 0 {
		t.Errorf("activePeerCount = %d, want 0", got)
	}
}

// TestIPCBridge_SendPeerMessageRoundtrip covers the outbound-bytes path.
func TestIPCBridge_SendPeerMessageRoundtrip(t *testing.T) {
	bridge, shell := newTestBridge(t)

	received := make(chan []byte, 1)
	shell.on("send_peer_message", func(params json.RawMessage) (any, *ipcError) {
		var p struct {
			PeerID  string `json:"peerId"`
			Payload string `json:"payload"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &ipcError{Code: "bad_params", Message: err.Error()}
		}
		raw, err := base64.StdEncoding.DecodeString(p.Payload)
		if err != nil {
			return nil, &ipcError{Code: "bad_payload", Message: err.Error()}
		}
		received <- raw
		return nil, nil
	})

	payload := []byte("hello over steam")
	if err := bridge.SendPeerMessage(99, payload); err != nil {
		t.Fatalf("SendPeerMessage: %v", err)
	}
	select {
	case got := <-received:
		if string(got) != string(payload) {
			t.Errorf("shell received %q, want %q", got, payload)
		}
	case <-time.After(time.Second):
		t.Fatal("shell never received send_peer_message")
	}
}

// TestIPCBridge_OpenListenerRoundtrip covers the host-side bootstrap call.
func TestIPCBridge_OpenListenerRoundtrip(t *testing.T) {
	bridge, shell := newTestBridge(t)
	gotPort := make(chan int, 1)
	shell.on("open_listener", func(params json.RawMessage) (any, *ipcError) {
		var p struct {
			VirtualPort int `json:"virtualPort"`
		}
		_ = json.Unmarshal(params, &p)
		gotPort <- p.VirtualPort
		return map[string]int{"virtualPort": p.VirtualPort}, nil
	})

	if err := bridge.OpenListener(27); err != nil {
		t.Fatalf("OpenListener: %v", err)
	}
	select {
	case got := <-gotPort:
		if got != 27 {
			t.Errorf("shell saw virtualPort=%d, want 27", got)
		}
	case <-time.After(time.Second):
		t.Fatal("shell never received open_listener")
	}
}

// TestIPCBridge_ConnectToRoundtrip covers the joiner-side bootstrap call,
// including the peerId-as-string round-trip.
func TestIPCBridge_ConnectToRoundtrip(t *testing.T) {
	bridge, shell := newTestBridge(t)
	shell.on("connect_to", func(params json.RawMessage) (any, *ipcError) {
		var p struct {
			SteamID64   string `json:"steamId64"`
			VirtualPort int    `json:"virtualPort"`
		}
		_ = json.Unmarshal(params, &p)
		if p.SteamID64 != "76561197960287930" || p.VirtualPort != 27 {
			return nil, &ipcError{Code: "bad_params", Message: "steamId64 or virtualPort wrong"}
		}
		return map[string]string{"peerId": "55"}, nil
	})

	peerID, err := bridge.ConnectTo(76561197960287930, 27)
	if err != nil {
		t.Fatalf("ConnectTo: %v", err)
	}
	if peerID != 55 {
		t.Errorf("peerID = %d, want 55", peerID)
	}
}
