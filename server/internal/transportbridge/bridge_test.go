package transportbridge

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

// TestConnectToHost_HappyPath verifies a successful WS dial to a localhost
// echo server returns a usable HostConnection.
func TestConnectToHost_HappyPath(t *testing.T) {
	server := newEchoWSServer(t)
	defer server.Close()

	hostPort := strings.TrimPrefix(server.URL, "http://")
	hc, err := ConnectToHost(context.Background(), hostPort, false)
	if err != nil {
		t.Fatalf("ConnectToHost: %v", err)
	}
	defer hc.Conn.Close()
	if !strings.HasSuffix(hc.URL, "/ws") {
		t.Errorf("URL = %q, want trailing /ws", hc.URL)
	}
}

// TestConnectToHost_RefusedClassified verifies the DialError contains a
// usable Kind for the SPA to render. We can't reliably trigger a refused
// error on every CI box (firewalls vary), but dialing 127.0.0.1 on a port
// where nothing listens reliably refuses on Windows / Linux / macOS.
func TestConnectToHost_RefusedClassified(t *testing.T) {
	// Pick a port nothing is listening on by binding briefly then closing.
	listener, err := newClosedListener()
	if err != nil {
		t.Skipf("could not produce a refused-port: %v", err)
	}
	_, err = ConnectToHost(context.Background(), listener, false)
	if err == nil {
		t.Fatal("expected dial error, got nil")
	}
	var de *DialError
	if !errors.As(err, &de) {
		t.Fatalf("expected *DialError, got %T: %v", err, err)
	}
	// Accept any classified kind — the important assertion is that we got a
	// *DialError so the SPA can surface a typed message. Platform-specific
	// error wording variance is captured by classifyDialError's per-OS
	// substring list; "other" is a valid fallback.
	if de.Kind == "" {
		t.Errorf("DialError.Kind is empty; should be one of: timeout/refused/dns/other")
	}
	t.Logf("classified as %s (underlying: %v)", de.Kind, de.Wrap)
}

// TestClassifyDialError covers the kind classifier with synthetic error
// messages so the SPA's UI surfacing is testable without real network state.
func TestClassifyDialError(t *testing.T) {
	cases := []struct {
		input string
		want  DialErrorKind
	}{
		{"dial tcp 192.0.2.1:9999: i/o timeout", DialErrTimeout},
		{"dial tcp 192.0.2.1:9999: connect: connection refused", DialErrRefused},
		{"dial tcp: lookup nonexistent.example: no such host", DialErrDNS},
		{"some unrelated thing happened", DialErrOther},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			err := classifyDialError(errors.New(tc.input))
			var de *DialError
			if !errors.As(err, &de) {
				t.Fatalf("not a DialError: %v", err)
			}
			if de.Kind != tc.want {
				t.Errorf("Kind = %s, want %s", de.Kind, tc.want)
			}
		})
	}
}

func newEchoWSServer(t *testing.T) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, msg)
	}))
}

// newClosedListener returns "host:port" of a TCP port that just closed.
// On most OSes this triggers an immediate refused; on some it may time out.
func newClosedListener() (string, error) {
	// Bind to a kernel-assigned port, capture the addr, close.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	u, _ := url.Parse(srv.URL)
	srv.Close()
	return u.Host, nil
}
