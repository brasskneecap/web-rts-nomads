package ws

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"webrts/server/internal/steam"
)

// recordingIPC is the test-double SteamPeerIPC: records every SendPeerMessage
// + ClosePeer + ForgetPeer call so transport tests can assert wire-format
// and lifecycle behaviour without standing up a real IPC pipe.
type recordingIPC struct {
	mu        sync.Mutex
	sends     [][]byte
	closes    []uint64
	forgets   []uint64
	sendError error
}

func (r *recordingIPC) SendPeerMessage(_ uint64, payload []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sendError != nil {
		return r.sendError
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	r.sends = append(r.sends, cp)
	return nil
}

func (r *recordingIPC) ClosePeer(peerID uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closes = append(r.closes, peerID)
	return nil
}

func (r *recordingIPC) ForgetPeer(peerID uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.forgets = append(r.forgets, peerID)
}

func (r *recordingIPC) snapshot() (sends [][]byte, closes, forgets []uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]byte, len(r.sends))
	for i, s := range r.sends {
		out[i] = append([]byte(nil), s...)
	}
	return out, append([]uint64(nil), r.closes...), append([]uint64(nil), r.forgets...)
}

// TestSteamTransport_SatisfiesInterfaces is a compile-time check (re-asserted
// at runtime so test runners surface a clear failure if the var-decls are
// removed during refactoring).
func TestSteamTransport_SatisfiesInterfaces(t *testing.T) {
	var _ Transport = (*steamTransport)(nil)
	var _ steam.PeerSink = (*steamTransport)(nil)
}

// TestSteamTransport_PeerIdentity asserts the documented "steam:<id64>"
// log-line format from the pluggable-mp-transport spec.
func TestSteamTransport_PeerIdentity(t *testing.T) {
	tr := NewSteamTransport(42, 76561197960287930, &recordingIPC{})
	got := tr.PeerIdentity()
	if got.Kind != PeerKindSteam {
		t.Errorf("Kind = %v, want PeerKindSteam", got.Kind)
	}
	if got.Addr != "76561197960287930" {
		t.Errorf("Addr = %q, want 76561197960287930", got.Addr)
	}
	if got.String() != "steam:76561197960287930" {
		t.Errorf("String = %q, want steam:76561197960287930", got.String())
	}
}

// TestSteamTransport_WriteMessageGoesToIPC asserts that WriteMessage delegates
// to SendPeerMessage with gzip-compression on the wire (the transport
// compresses to reduce per-message size; see compressForWire). Round-trip
// the compressed bytes through decompressFromWire to assert the payload
// arrived without semantic loss.
func TestSteamTransport_WriteMessageGoesToIPC(t *testing.T) {
	ipc := &recordingIPC{}
	tr := NewSteamTransport(7, 1, ipc)

	payload := []byte(`{"type":"snapshot","tick":42}`)
	if err := tr.WriteMessage(payload); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	sends, _, _ := ipc.snapshot()
	if len(sends) != 1 {
		t.Fatalf("len(sends) = %d, want 1", len(sends))
	}
	got, err := decompressFromWire(sends[0])
	if err != nil {
		t.Fatalf("on-wire bytes did not decompress: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("payload = %q, want %q", got, payload)
	}
}

// TestSteamTransport_WriteAfterCloseFails confirms post-close writes are
// rejected (no UB, no panic-on-send).
func TestSteamTransport_WriteAfterCloseFails(t *testing.T) {
	tr := NewSteamTransport(1, 1, &recordingIPC{})
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := tr.WriteMessage([]byte("late")); err == nil {
		t.Error("WriteMessage after Close returned nil; want error")
	}
}

// TestSteamTransport_DeliverThenRead exercises the inbound path: a Deliver
// call (which the IPCBridge invokes on its reader goroutine) makes
// ReadMessage return that payload.
func TestSteamTransport_DeliverThenRead(t *testing.T) {
	tr := NewSteamTransport(1, 1, &recordingIPC{})

	payload := []byte(`{"type":"intent","unitId":3}`)
	tr.Deliver(payload)

	got, err := tr.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("ReadMessage = %q, want %q", got, payload)
	}
}

// TestSteamTransport_CloseSurfacesEOFToReader asserts the hub's standard
// "transport reports closed" path: Close() → ReadMessage() returns io.EOF.
func TestSteamTransport_CloseSurfacesEOFToReader(t *testing.T) {
	ipc := &recordingIPC{}
	tr := NewSteamTransport(99, 1, ipc)

	var readErr atomic.Value
	readDone := make(chan struct{})
	go func() {
		_, err := tr.ReadMessage()
		readErr.Store(err)
		close(readDone)
	}()
	// Let the reader park on the channel.
	time.Sleep(20 * time.Millisecond)

	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatal("ReadMessage never returned after Close")
	}

	got, _ := readErr.Load().(error)
	if !errors.Is(got, io.EOF) {
		t.Errorf("ReadMessage err = %v, want io.EOF", got)
	}

	// Close fires both ClosePeer (shell-side teardown) and ForgetPeer
	// (bridge-side reference drop). Order is shell-then-bridge.
	_, closes, forgets := ipc.snapshot()
	if len(closes) != 1 || closes[0] != 99 {
		t.Errorf("ClosePeer calls = %v, want [99]", closes)
	}
	if len(forgets) != 1 || forgets[0] != 99 {
		t.Errorf("ForgetPeer calls = %v, want [99]", forgets)
	}
}

// TestSteamTransport_CloseIsIdempotent: per the Transport contract,
// Close after Close returns nil and does not double-fire IPC calls.
func TestSteamTransport_CloseIsIdempotent(t *testing.T) {
	ipc := &recordingIPC{}
	tr := NewSteamTransport(1, 1, ipc)
	if err := tr.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	_, closes, forgets := ipc.snapshot()
	if len(closes) != 1 {
		t.Errorf("ClosePeer fired %d times across two Close()s, want 1", len(closes))
	}
	if len(forgets) != 1 {
		t.Errorf("ForgetPeer fired %d times across two Close()s, want 1", len(forgets))
	}
}

// TestSteamTransport_DisconnectClosesTransport asserts the bridge-driven
// "peer left" path: Disconnect() runs the same cleanup Close() does.
func TestSteamTransport_DisconnectClosesTransport(t *testing.T) {
	ipc := &recordingIPC{}
	tr := NewSteamTransport(5, 1, ipc)

	tr.Disconnect(4001)
	if _, err := tr.ReadMessage(); !errors.Is(err, io.EOF) {
		t.Errorf("ReadMessage after Disconnect = %v, want io.EOF", err)
	}
	_, closes, _ := ipc.snapshot()
	if len(closes) != 1 || closes[0] != 5 {
		t.Errorf("ClosePeer calls = %v, want [5]", closes)
	}
}

// TestSteamTransport_PingIsNoop documents the Steam-keepalive design: pings
// must not error (the hub would treat a non-nil error as a dead connection
// and tear the client down).
func TestSteamTransport_PingIsNoop(t *testing.T) {
	tr := NewSteamTransport(1, 1, &recordingIPC{})
	for i := 0; i < 5; i++ {
		if err := tr.WritePing(); err != nil {
			t.Errorf("WritePing #%d: %v", i, err)
		}
	}
}

// TestSteamTransport_DeliverAfterCloseDropsSilently asserts the contract
// the bridge relies on: a late Deliver (peer_message arriving after we
// already locally Closed) does NOT panic on a closed channel.
func TestSteamTransport_DeliverAfterCloseDropsSilently(t *testing.T) {
	tr := NewSteamTransport(1, 1, &recordingIPC{})
	_ = tr.Close()

	// Should not panic.
	tr.Deliver([]byte("late"))
}
