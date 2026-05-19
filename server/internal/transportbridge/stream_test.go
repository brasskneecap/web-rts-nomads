package transportbridge

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeStream is the test-double MessageStream: bytes pushed into incoming
// surface from ReadMessage; bytes written via WriteMessage are recorded.
type fakeStream struct {
	name string

	incoming chan []byte
	outMu    sync.Mutex
	out      [][]byte
	closed   atomic.Bool
}

func newFakeStream(name string, buf int) *fakeStream {
	return &fakeStream{name: name, incoming: make(chan []byte, buf)}
}

func (f *fakeStream) ReadMessage() ([]byte, error) {
	payload, ok := <-f.incoming
	if !ok {
		return nil, io.EOF
	}
	return payload, nil
}

func (f *fakeStream) WriteMessage(payload []byte) error {
	if f.closed.Load() {
		return errors.New("fake stream closed")
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	f.outMu.Lock()
	f.out = append(f.out, cp)
	f.outMu.Unlock()
	return nil
}

func (f *fakeStream) Close() error {
	if f.closed.Swap(true) {
		return nil
	}
	close(f.incoming)
	return nil
}

func (f *fakeStream) push(p []byte) { f.incoming <- p }

func (f *fakeStream) outSnapshot() [][]byte {
	f.outMu.Lock()
	defer f.outMu.Unlock()
	out := make([][]byte, len(f.out))
	for i, b := range f.out {
		out[i] = append([]byte(nil), b...)
	}
	return out
}

// TestProxyStreams_BidirectionalByteFidelity sends payloads in both
// directions through ProxyStreams and asserts byte-identity at the
// opposite end.
func TestProxyStreams_BidirectionalByteFidelity(t *testing.T) {
	a := newFakeStream("a", 8)
	b := newFakeStream("b", 8)

	done := make(chan struct{})
	go func() {
		ProxyStreams(a, b)
		close(done)
	}()

	// a -> b
	payloads := [][]byte{
		[]byte(`{"type":"hello"}`),
		[]byte("\x00\x01\xff\xfe"),
		[]byte(`{"type":"snapshot","tick":17}`),
	}
	for _, p := range payloads {
		a.push(p)
	}
	waitForLen(t, time.Second, "b received all a payloads", func() int {
		return len(b.outSnapshot())
	}, len(payloads))
	got := b.outSnapshot()
	for i, p := range payloads {
		if string(got[i]) != string(p) {
			t.Errorf("a->b payload %d: got %q want %q", i, got[i], p)
		}
	}

	// b -> a
	reply := []byte("PONG")
	b.push(reply)
	waitForLen(t, time.Second, "a received reply", func() int {
		return len(a.outSnapshot())
	}, 1)
	if string(a.outSnapshot()[0]) != string(reply) {
		t.Errorf("b->a payload: got %q want %q", a.outSnapshot()[0], reply)
	}

	// Tear down.
	_ = a.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ProxyStreams did not return after a.Close")
	}
}

// TestProxyStreams_CloseOnEitherSideTearsDownBoth verifies the close
// propagation contract: closing one side closes the other.
func TestProxyStreams_CloseOnEitherSideTearsDownBoth(t *testing.T) {
	a := newFakeStream("a", 1)
	b := newFakeStream("b", 1)
	done := make(chan struct{})
	go func() {
		ProxyStreams(a, b)
		close(done)
	}()

	_ = a.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ProxyStreams did not return within 1s of a.Close")
	}
	if !b.closed.Load() {
		t.Error("b was not closed when a was closed")
	}
}

// TestSteamSessionStore_SetTakeRoundtrip — single-slot semantics.
func TestSteamSessionStore_SetTakeRoundtrip(t *testing.T) {
	store := NewSteamSessionStore()
	s := newFakeStream("s", 1)

	if store.Has() {
		t.Error("fresh store should be empty")
	}
	store.Set(s)
	if !store.Has() {
		t.Error("store should be non-empty after Set")
	}
	got, ok := store.Take()
	if !ok {
		t.Fatal("Take returned !ok")
	}
	if got != s {
		t.Error("Take returned a different stream")
	}
	if store.Has() {
		t.Error("store should be empty after Take")
	}
}

// TestSteamSessionStore_SetOverwritesAndClosesOld asserts the replacement
// rule: a second Set closes the previous stream so we don't leak handles.
func TestSteamSessionStore_SetOverwritesAndClosesOld(t *testing.T) {
	store := NewSteamSessionStore()
	first := newFakeStream("first", 1)
	store.Set(first)
	second := newFakeStream("second", 1)
	store.Set(second)

	if !first.closed.Load() {
		t.Error("first stream should be closed after second Set")
	}
	got, _ := store.Take()
	if got != second {
		t.Error("Take did not return the second stream")
	}
}

// TestSteamSessionStore_ReapStaleClosesIdleStreams asserts the TTL behaviour.
func TestSteamSessionStore_ReapStaleClosesIdleStreams(t *testing.T) {
	store := NewSteamSessionStore()
	s := newFakeStream("s", 1)
	store.Set(s)

	// now == createdAt → no reap.
	if store.ReapStale(time.Now()) {
		t.Error("fresh stream should not be reaped")
	}
	if !store.Has() {
		t.Error("fresh stream should still be parked after non-reap")
	}

	// past TTL → reap.
	if !store.ReapStale(time.Now().Add(SteamSessionTTL + time.Second)) {
		t.Error("expired stream should be reaped")
	}
	if !s.closed.Load() {
		t.Error("reaped stream should be closed")
	}
	if store.Has() {
		t.Error("store should be empty after reap")
	}
}

// TestSteamSessionStore_TakeOnEmpty returns false.
func TestSteamSessionStore_TakeOnEmpty(t *testing.T) {
	store := NewSteamSessionStore()
	if _, ok := store.Take(); ok {
		t.Error("Take on empty store should return !ok")
	}
}

// TestSteamSessionStore_Close drops and closes a parked stream.
func TestSteamSessionStore_Close(t *testing.T) {
	store := NewSteamSessionStore()
	s := newFakeStream("s", 1)
	store.Set(s)
	store.Close()
	if !s.closed.Load() {
		t.Error("Close should close the parked stream")
	}
	if store.Has() {
		t.Error("store should be empty after Close")
	}
}

// waitForLen polls predicate-returning-int until it equals want or times out.
func waitForLen(t *testing.T, timeout time.Duration, msg string, pred func() int, want int) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pred() == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out: %s (got %d, want %d)", msg, pred(), want)
}
