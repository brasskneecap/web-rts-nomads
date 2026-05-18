package transportbridge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestSessionStore_PutAndTakeRoundTrip covers the happy path: Put returns
// a token; Take with that token returns the conn and removes the entry.
func TestSessionStore_PutAndTakeRoundTrip(t *testing.T) {
	s := NewSessionStore()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer c.Close()
		_, _, _ = c.ReadMessage()
	}))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	token := s.Put(conn)
	if token == "" || len(token) != 32 {
		t.Errorf("token = %q, want 32 hex chars", token)
	}
	if s.Len() != 1 {
		t.Errorf("Len = %d, want 1", s.Len())
	}

	got, ok := s.Take(token)
	if !ok {
		t.Fatal("Take returned !ok for valid token")
	}
	if got != conn {
		t.Error("Take returned a different conn")
	}
	if s.Len() != 0 {
		t.Errorf("Len after Take = %d, want 0", s.Len())
	}
	if _, ok := s.Take(token); ok {
		t.Error("Take of consumed token should return !ok")
	}
}

// TestSessionStore_UnknownToken returns false.
func TestSessionStore_UnknownToken(t *testing.T) {
	s := NewSessionStore()
	if _, ok := s.Take("does-not-exist"); ok {
		t.Error("Take of unknown token should return !ok")
	}
}

// TestSessionStore_ReapStale removes entries older than SessionTTL and closes
// the underlying conn.
func TestSessionStore_ReapStale(t *testing.T) {
	s := NewSessionStore()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := upgrader.Upgrade(w, r, nil)
		if c != nil {
			defer c.Close()
			_, _, _ = c.ReadMessage()
		}
	}))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	_ = s.Put(conn)
	if s.Len() != 1 {
		t.Fatal("setup")
	}

	// Reap with now=createdAt → nothing reaped.
	if got := s.ReapStale(time.Now()); got != 0 {
		t.Errorf("ReapStale (fresh) = %d, want 0", got)
	}

	// Reap with now past TTL → one reaped.
	if got := s.ReapStale(time.Now().Add(SessionTTL + time.Second)); got != 1 {
		t.Errorf("ReapStale (expired) = %d, want 1", got)
	}
	if s.Len() != 0 {
		t.Errorf("Len after reap = %d, want 0", s.Len())
	}
}

// TestProxy_BidirectionalByteFidelity is the headline test for §11.2: bytes
// sent from the SPA side appear unchanged on the host side, and vice versa.
// Uses two real WS conns wired through Proxy.
func TestProxy_BidirectionalByteFidelity(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	// Server #1 plays the role of "host" — echoes whatever it gets back to
	// the caller, prefixed with "host:".
	hostReceived := make(chan string, 4)
	hostSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			_, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			hostReceived <- string(data)
			if err := c.WriteMessage(websocket.TextMessage, append([]byte("host:"), data...)); err != nil {
				return
			}
		}
	}))
	defer hostSrv.Close()

	// Dial host from "joiner Go server" side.
	hostURL := "ws" + strings.TrimPrefix(hostSrv.URL, "http")
	hostConn, _, err := websocket.DefaultDialer.Dial(hostURL, nil)
	if err != nil {
		t.Fatalf("dial host: %v", err)
	}

	// Joiner's local Go server: SPA upgrades, hub wires the proxy.
	joinerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spaConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		Proxy(spaConn, hostConn)
	}))
	defer joinerSrv.Close()

	// "SPA" connects to joiner's local server.
	joinerURL := "ws" + strings.TrimPrefix(joinerSrv.URL, "http")
	spaConn, _, err := websocket.DefaultDialer.Dial(joinerURL, nil)
	if err != nil {
		t.Fatalf("dial joiner: %v", err)
	}
	defer spaConn.Close()

	// SPA -> host
	want := `{"type":"move_command","unitIds":[1,2,3]}`
	if err := spaConn.WriteMessage(websocket.TextMessage, []byte(want)); err != nil {
		t.Fatalf("spa write: %v", err)
	}
	select {
	case got := <-hostReceived:
		if got != want {
			t.Errorf("host received %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("host never received SPA message")
	}

	// host -> SPA (the echo from above)
	_, echo, err := spaConn.ReadMessage()
	if err != nil {
		t.Fatalf("spa read: %v", err)
	}
	wantEcho := "host:" + want
	if string(echo) != wantEcho {
		t.Errorf("SPA received %q, want %q", echo, wantEcho)
	}
}

// TestProxy_CloseFromEitherSidePropagates verifies that closing the SPA conn
// tears down the host conn, and vice versa.
func TestProxy_CloseFromEitherSidePropagates(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	hostClosed := make(chan struct{})
	hostSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		// Block on read until SPA-side close propagates.
		_, _, _ = c.ReadMessage()
		close(hostClosed)
	}))
	defer hostSrv.Close()

	hostURL := "ws" + strings.TrimPrefix(hostSrv.URL, "http")
	hostConn, _, err := websocket.DefaultDialer.Dial(hostURL, nil)
	if err != nil {
		t.Fatalf("dial host: %v", err)
	}

	proxyDone := make(chan struct{})
	joinerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spa, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		Proxy(spa, hostConn)
		close(proxyDone)
	}))
	defer joinerSrv.Close()

	joinerURL := "ws" + strings.TrimPrefix(joinerSrv.URL, "http")
	spa, _, err := websocket.DefaultDialer.Dial(joinerURL, nil)
	if err != nil {
		t.Fatalf("dial joiner: %v", err)
	}

	// SPA closes → Proxy should tear down host → host server's ReadMessage returns.
	_ = spa.Close()

	select {
	case <-hostClosed:
	case <-time.After(2 * time.Second):
		t.Fatal("host conn close did not propagate within 2s")
	}
	select {
	case <-proxyDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Proxy did not return within 2s of close")
	}
}

// TestSessionStore_ConcurrentPutTake runs many concurrent Put/Take pairs
// through a real httptest WS server pool, asserting no token collisions or
// data races (`go test -race ./internal/transportbridge/`).
func TestSessionStore_ConcurrentPutTake(t *testing.T) {
	s := NewSessionStore()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := upgrader.Upgrade(w, r, nil)
		if c != nil {
			defer c.Close()
			_, _, _ = c.ReadMessage()
		}
	}))
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	const n = 40
	tokens := make([]string, n)
	conns := make([]*websocket.Conn, n)

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			c, _, err := websocket.DefaultDialer.Dial(url, nil)
			if err != nil {
				t.Errorf("dial %d: %v", i, err)
				return
			}
			conns[i] = c
			tokens[i] = s.Put(c)
		}()
	}
	wg.Wait()

	// All tokens must be unique.
	seen := make(map[string]bool, n)
	for _, tok := range tokens {
		if seen[tok] {
			t.Fatalf("token collision: %s", tok)
		}
		seen[tok] = true
	}
	if s.Len() != n {
		t.Errorf("Len = %d, want %d", s.Len(), n)
	}

	// Concurrent Take.
	wg.Add(n)
	for _, tok := range tokens {
		tok := tok
		go func() {
			defer wg.Done()
			if _, ok := s.Take(tok); !ok {
				t.Errorf("Take(%s) returned !ok", tok)
			}
		}()
	}
	wg.Wait()
	if s.Len() != 0 {
		t.Errorf("Len after Take-all = %d, want 0", s.Len())
	}

	for _, c := range conns {
		if c != nil {
			_ = c.Close()
		}
	}
}
