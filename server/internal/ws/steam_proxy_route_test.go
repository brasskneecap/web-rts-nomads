package ws

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"webrts/server/internal/game"

	"github.com/gorilla/websocket"
)

// fakeUpstream is a minimal MessageStream stand-in for a parked
// ws.steamTransport. Lives inside the ws-test file so we don't import
// transportbridge's internal test helpers.
type fakeUpstream struct {
	incoming chan []byte
	outMu    sync.Mutex
	out      [][]byte
	closed   atomic.Bool
}

func newFakeUpstream() *fakeUpstream {
	return &fakeUpstream{incoming: make(chan []byte, 8)}
}

func (f *fakeUpstream) ReadMessage() ([]byte, error) {
	payload, ok := <-f.incoming
	if !ok {
		return nil, io.EOF
	}
	return payload, nil
}

func (f *fakeUpstream) WriteMessage(payload []byte) error {
	if f.closed.Load() {
		return errors.New("fake upstream closed")
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	f.outMu.Lock()
	f.out = append(f.out, cp)
	f.outMu.Unlock()
	return nil
}

func (f *fakeUpstream) Close() error {
	if f.closed.Swap(true) {
		return nil
	}
	close(f.incoming)
	return nil
}

func (f *fakeUpstream) push(p []byte)       { f.incoming <- p }
func (f *fakeUpstream) outLen() int         { f.outMu.Lock(); defer f.outMu.Unlock(); return len(f.out) }
func (f *fakeUpstream) outAt(i int) []byte  { f.outMu.Lock(); defer f.outMu.Unlock(); return append([]byte(nil), f.out[i]...) }

// TestSteamProxyRoute_404WhenNoUpstreamParked covers the failure case:
// ?proxy=steam with nothing in steamSessions returns HTTP 502.
func TestSteamProxyRoute_404WhenNoUpstreamParked(t *testing.T) {
	hub := NewHub(game.NewMatchManager(), game.NewLobbyManager())
	defer hub.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.HandleWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Use http.Get against /ws?proxy=steam — the upgrade can't happen
	// without a WS upgrader on the client side, so we instead use a Dial
	// expecting it to fail with a non-101 response.
	u, _ := url.Parse(strings.Replace(srv.URL, "http", "ws", 1))
	u.Path = "/ws"
	u.RawQuery = "proxy=steam"
	_, resp, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err == nil {
		t.Fatal("expected dial to fail with bad gateway, got success")
	}
	if resp == nil {
		t.Fatalf("expected an HTTP response, got nil; err=%v", err)
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
}

// TestSteamProxyRoute_RoutesBytesToAndFromUpstream is the happy-path
// integration: park a fakeUpstream in the Hub's SteamSessionStore, then
// connect a "SPA" WebSocket with ?proxy=steam. Bytes flow both ways.
func TestSteamProxyRoute_RoutesBytesToAndFromUpstream(t *testing.T) {
	hub := NewHub(game.NewMatchManager(), game.NewLobbyManager())
	defer hub.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.HandleWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	upstream := newFakeUpstream()
	hub.SteamSessions().Set(upstream)

	u, _ := url.Parse(strings.Replace(srv.URL, "http", "ws", 1))
	u.Path = "/ws"
	u.RawQuery = "proxy=steam"

	spa, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("SPA dial: %v", err)
	}
	defer spa.Close()

	// SPA -> upstream
	wantUp := []byte(`{"type":"join_match","mapId":"","playerId":"p1"}`)
	if err := spa.WriteMessage(websocket.TextMessage, wantUp); err != nil {
		t.Fatalf("spa write: %v", err)
	}
	waitForLen(t, time.Second, "upstream received SPA write", func() int {
		return upstream.outLen()
	}, 1)
	if got := upstream.outAt(0); string(got) != string(wantUp) {
		t.Errorf("upstream received %q, want %q", got, wantUp)
	}

	// upstream -> SPA
	wantDown := []byte(`{"type":"welcome","matchId":"m1"}`)
	upstream.push(wantDown)
	_ = spa.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := spa.ReadMessage()
	if err != nil {
		t.Fatalf("spa read: %v", err)
	}
	if string(data) != string(wantDown) {
		t.Errorf("SPA received %q, want %q", data, wantDown)
	}

	// The store should be empty (single-slot, Take-and-consume).
	if hub.SteamSessions().Has() {
		t.Error("SteamSessions should be empty after the SPA claimed the slot")
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
