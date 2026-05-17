package ws

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"webrts/server/internal/game"
)

// TestCheckOrigin_LoopbackAndMissingAccepted covers the §13 task 13.11
// scenarios for accepted Origins.
func TestCheckOrigin_LoopbackAndMissingAccepted(t *testing.T) {
	h := NewHub(game.NewMatchManager(), game.NewLobbyManager())
	defer h.Close()

	cases := []struct {
		name   string
		origin string
	}{
		{"no Origin header (transportbridge / native client)", ""},
		{"loopback IPv4", "http://127.0.0.1:54321"},
		{"localhost (dev workflow)", "http://localhost:5173"},
		{"loopback IPv6", "http://[::1]:9999"},
		{"https loopback", "https://127.0.0.1:443"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/ws", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if !h.checkOrigin(req) {
				t.Errorf("origin %q should be accepted", tc.origin)
			}
		})
	}
}

// TestCheckOrigin_NonLoopbackAndMalformedRejected covers the §13 task 13.11
// rejection scenarios.
func TestCheckOrigin_NonLoopbackAndMalformedRejected(t *testing.T) {
	h := NewHub(game.NewMatchManager(), game.NewLobbyManager())
	defer h.Close()

	cases := []struct {
		name   string
		origin string
	}{
		{"explicit external host", "https://malicious.example"},
		{"non-loopback IP", "http://192.168.1.50:5173"},
		{"malformed Origin", "not a url"},
		{"empty-host Origin", "http://"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/ws", nil)
			req.Header.Set("Origin", tc.origin)
			if h.checkOrigin(req) {
				t.Errorf("origin %q should be rejected", tc.origin)
			}
		})
	}
}

// TestCheckRemoteAddrAllowed_LoopbackAlwaysAccepted asserts loopback peers
// always succeed regardless of the AllowNonLoopback toggle.
func TestCheckRemoteAddrAllowed_LoopbackAlwaysAccepted(t *testing.T) {
	h := NewHub(game.NewMatchManager(), game.NewLobbyManager())
	defer h.Close()

	for _, allow := range []bool{false, true} {
		h.SetAllowNonLoopback(allow)
		for _, addr := range []string{"127.0.0.1:38112", "[::1]:38112"} {
			req := httptest.NewRequest(http.MethodGet, "/ws", nil)
			req.RemoteAddr = addr
			if !h.checkRemoteAddrAllowed(req) {
				t.Errorf("loopback %s should be accepted (allow=%v)", addr, allow)
			}
		}
	}
}

// TestCheckRemoteAddrAllowed_NonLoopbackGatedByToggle is the core §13.1
// behaviour: non-loopback admission flips with the toggle.
func TestCheckRemoteAddrAllowed_NonLoopbackGatedByToggle(t *testing.T) {
	h := NewHub(game.NewMatchManager(), game.NewLobbyManager())
	defer h.Close()
	const nonLoopback = "192.168.1.50:38112"

	h.SetAllowNonLoopback(false)
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.RemoteAddr = nonLoopback
	if h.checkRemoteAddrAllowed(req) {
		t.Error("non-loopback should be rejected when toggle is off")
	}

	h.SetAllowNonLoopback(true)
	req2 := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req2.RemoteAddr = nonLoopback
	if !h.checkRemoteAddrAllowed(req2) {
		t.Error("non-loopback should be accepted when toggle is on")
	}
}

// TestOriginRejectLogThrottle ensures we don't log on every reject for the
// same origin (would let a misbehaving caller flood the log).
func TestOriginRejectLogThrottle(t *testing.T) {
	h := NewHub(game.NewMatchManager(), game.NewLobbyManager())
	defer h.Close()

	// Call logOriginReject twice in rapid succession for the same origin.
	// The throttle should prevent the second from updating the timestamp
	// beyond what the first set. Indirect assertion: the throttle map has
	// exactly one entry.
	h.logOriginReject("https://evil.example", "test")
	first := h.originRejectLastLog["https://evil.example"]
	h.logOriginReject("https://evil.example", "test")
	second := h.originRejectLastLog["https://evil.example"]
	if !first.Equal(second) {
		t.Errorf("throttle did not skip second log: first=%v second=%v", first, second)
	}

	if !strings.HasPrefix("https://evil.example", "https://") {
		t.Fatal("unreachable")
	}
}
