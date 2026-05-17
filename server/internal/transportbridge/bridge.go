// Package transportbridge implements the joiner-as-proxy MP model: a joiner's
// local Go server runs no simulation but forwards bytes between its SPA's
// WebSocket and a parent transport (the host's WS in Direct connect; Steam
// Networking Sockets in the Steam path).
//
// Phase 1 scope (this file): a Direct-connect WS-to-WS proxy that opens a
// WebSocket to host:port and bridges bytes both directions. Phase 2 (§12)
// adds the Steam Sockets parent transport implementation; §11.5 wires
// proxy-mode client registration into the WS hub.
//
// The bridge MUST NOT modify any payload bytes — the "transport-agnostic
// protocol bytes" invariant in the pluggable-mp-transport spec depends on
// it. Tests for byte-fidelity live alongside this package.
package transportbridge

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

// DialHostTimeout is the connect-attempt timeout used by ConnectToHost for
// the Direct connect joiner flow (§13 task 13.4: "5-s connect timeout").
const DialHostTimeout = 5 * time.Second

// HostConnection wraps the WebSocket connection the joiner's Go server holds
// to the host's WS endpoint. The joiner registers this with its local hub
// as a proxy-client transport; that wiring lands with §11.5 in Phase 2.
type HostConnection struct {
	Conn *websocket.Conn
	URL  string
}

// ConnectToHost opens a WebSocket to ws://hostPort/ws (or wss:// if scheme is
// "https"). Used by the Direct-connect joiner flow. Returns a HostConnection
// or one of the documented error classes so the SPA can surface a useful
// message (§13 task 13.5: "DNS / refused / timeout failure classes").
func ConnectToHost(ctx context.Context, hostPort string, useTLS bool) (*HostConnection, error) {
	scheme := "ws"
	if useTLS {
		scheme = "wss"
	}
	u := url.URL{Scheme: scheme, Host: hostPort, Path: "/ws"}

	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = DialHostTimeout

	dialCtx, cancel := context.WithTimeout(ctx, DialHostTimeout)
	defer cancel()

	conn, _, err := dialer.DialContext(dialCtx, u.String(), nil)
	if err != nil {
		return nil, classifyDialError(err)
	}
	return &HostConnection{Conn: conn, URL: u.String()}, nil
}

// DialError classifies the failure class so the SPA's Direct-connect UI can
// render an actionable message instead of a raw error string. Mirrors the
// SPA-side "DNS / refused / timeout failure classes" requirement.
type DialError struct {
	Kind DialErrorKind
	Wrap error
}

func (e *DialError) Error() string {
	return fmt.Sprintf("transportbridge dial %s: %v", e.Kind, e.Wrap)
}

func (e *DialError) Unwrap() error { return e.Wrap }

type DialErrorKind string

const (
	DialErrTimeout DialErrorKind = "timeout"
	DialErrRefused DialErrorKind = "refused"
	DialErrDNS     DialErrorKind = "dns"
	DialErrOther   DialErrorKind = "other"
)

func (k DialErrorKind) String() string { return string(k) }

func classifyDialError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	lower := toLower(msg)
	switch {
	case errors.Is(err, context.DeadlineExceeded) || containsAny(lower, "timeout", "i/o timeout"):
		return &DialError{Kind: DialErrTimeout, Wrap: err}
	case containsAny(lower,
		"connection refused",                                      // Linux / macOS
		"wsaeconnrefused",                                         // Windows error code
		"no connection could be made because the target machine",  // Windows verbose
		"actively refused it",                                     // Windows fragment
		"forcibly closed",                                         // Windows TCP RST
	):
		return &DialError{Kind: DialErrRefused, Wrap: err}
	case containsAny(lower, "no such host", "lookup", "dns"):
		return &DialError{Kind: DialErrDNS, Wrap: err}
	default:
		return &DialError{Kind: DialErrOther, Wrap: err}
	}
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if len(s) == 0 || len(n) == 0 {
			continue
		}
		// case-sensitive substring; the strings we're matching against
		// (timeout, no such host, etc.) are net package conventions.
		if indexOf(s, n) >= 0 {
			return true
		}
	}
	return false
}

// indexOf is a tiny strings.Index without importing strings to keep this
// file's surface narrow.
func indexOf(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
