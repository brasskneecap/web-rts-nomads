package ws

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
)

// FakeTransport is the in-memory Transport used by hub tests. Reads pull from
// the incoming queue; writes are appended to the outgoing log. Both sides are
// independently inspectable so tests can assert exact byte sequences.
//
// FakeTransport's bytes-in / bytes-out streams are the canonical comparison
// surface for the "Identical bytes across transports" requirement in the
// pluggable-mp-transport spec — the same hub call producing the same payload
// bytes regardless of which Transport implementation is wrapped is the
// definition of a successful refactor.
type FakeTransport struct {
	id string

	incoming chan []byte // ReadMessage pulls from here
	outMu    sync.Mutex
	out      [][]byte // every WriteMessage append (copies)
	pings    atomic.Int64
	closed   atomic.Bool
	pongCb   atomic.Pointer[func()]
}

// NewFakeTransport returns a FakeTransport. id is reflected in PeerIdentity
// for log lines and assertions. bufferSize bounds the incoming queue; tests
// that push more messages than bufferSize will block.
func NewFakeTransport(id string, bufferSize int) *FakeTransport {
	if bufferSize <= 0 {
		bufferSize = 64
	}
	return &FakeTransport{
		id:       id,
		incoming: make(chan []byte, bufferSize),
	}
}

// Push enqueues payload as if it had arrived from the peer. Blocks if the
// incoming buffer is full. Tests use Push to feed scripted client messages
// into the hub.
func (f *FakeTransport) Push(payload []byte) {
	if f.closed.Load() {
		return
	}
	// Copy so callers can reuse their buffer.
	cp := make([]byte, len(payload))
	copy(cp, payload)
	f.incoming <- cp
}

// CloseIncoming signals end-of-input. The next ReadMessage returns io.EOF and
// the read loop exits. Use this in tests to simulate a peer disconnect from
// the client side.
func (f *FakeTransport) CloseIncoming() {
	if f.closed.Swap(true) {
		return
	}
	close(f.incoming)
}

// Outgoing returns a snapshot of every payload the hub has written. The
// slice and its byte slices are copies; safe to retain.
func (f *FakeTransport) Outgoing() [][]byte {
	f.outMu.Lock()
	defer f.outMu.Unlock()
	out := make([][]byte, len(f.out))
	for i, b := range f.out {
		cp := make([]byte, len(b))
		copy(cp, b)
		out[i] = cp
	}
	return out
}

// PingsSent returns how many times WritePing was called.
func (f *FakeTransport) PingsSent() int64 { return f.pings.Load() }

// SimulatePong invokes the registered pong handler, mimicking a peer
// responding to a previous WritePing. Returns false if no handler is set.
func (f *FakeTransport) SimulatePong() bool {
	cbPtr := f.pongCb.Load()
	if cbPtr == nil {
		return false
	}
	(*cbPtr)()
	return true
}

// ReadMessage implements Transport. Returns io.EOF after CloseIncoming.
func (f *FakeTransport) ReadMessage() ([]byte, error) {
	payload, ok := <-f.incoming
	if !ok {
		return nil, io.EOF
	}
	return payload, nil
}

// WriteMessage implements Transport. Records a copy of payload on the
// outgoing log; rejects writes after Close.
func (f *FakeTransport) WriteMessage(payload []byte) error {
	if f.closed.Load() {
		return errors.New("fake transport: closed")
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	f.outMu.Lock()
	f.out = append(f.out, cp)
	f.outMu.Unlock()
	return nil
}

// WritePing implements Transport. Increments the ping counter.
func (f *FakeTransport) WritePing() error {
	if f.closed.Load() {
		return errors.New("fake transport: closed")
	}
	f.pings.Add(1)
	return nil
}

// Close implements Transport. Idempotent; subsequent ReadMessage returns EOF.
func (f *FakeTransport) Close() error {
	if f.closed.Swap(true) {
		return nil
	}
	close(f.incoming)
	return nil
}

// PeerIdentity implements Transport.
func (f *FakeTransport) PeerIdentity() PeerIdentity {
	return PeerIdentity{Kind: PeerKindFake, Addr: f.id}
}

// SetPongHandler implements Transport.
func (f *FakeTransport) SetPongHandler(cb func()) {
	f.pongCb.Store(&cb)
}
