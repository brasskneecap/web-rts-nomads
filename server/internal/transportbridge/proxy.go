package transportbridge

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// SessionTTL bounds how long a dialled host connection sits unclaimed in the
// SessionStore. If the joiner's SPA doesn't re-open its WS within this window
// the host conn is closed and the token discarded.
const SessionTTL = 30 * time.Second

// SessionStore holds host-side WS connections dialled by /api/direct-connect/join
// until the joiner's SPA reconnects with ?proxy=<token>. Concurrency-safe.
//
// Lifecycle:
//   1. /api/direct-connect/join dials → Put(conn) returns a token
//   2. SPA disconnects + reconnects to /ws?proxy=<token>
//   3. Hub.HandleWS calls Take(token) to retrieve the conn and wire the proxy
//   4. If Take is never called within SessionTTL, ReapStale closes the conn
type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]*sessionEntry
}

type sessionEntry struct {
	conn      *websocket.Conn
	createdAt time.Time
}

// NewSessionStore returns a ready-to-use SessionStore. The caller is
// responsible for running ReapStale on a ticker if they want background
// cleanup; the Take path naturally cleans claimed sessions.
func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]*sessionEntry)}
}

// Put registers conn and returns a fresh token. Token is 32 hex chars.
func (s *SessionStore) Put(conn *websocket.Conn) string {
	token := newToken()
	s.mu.Lock()
	s.sessions[token] = &sessionEntry{conn: conn, createdAt: time.Now()}
	s.mu.Unlock()
	return token
}

// Take retrieves and removes the host conn for token. Returns (nil, false)
// when the token is unknown.
func (s *SessionStore) Take(token string) (*websocket.Conn, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.sessions[token]
	if !ok {
		return nil, false
	}
	delete(s.sessions, token)
	return e.conn, true
}

// ReapStale closes connections older than SessionTTL and removes them.
// Intended to be called periodically by a caller-owned ticker. Returns the
// number of sessions reaped (for logging / tests).
func (s *SessionStore) ReapStale(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	reaped := 0
	for token, e := range s.sessions {
		if now.Sub(e.createdAt) >= SessionTTL {
			_ = e.conn.Close()
			delete(s.sessions, token)
			reaped++
		}
	}
	return reaped
}

// Len returns the current number of unclaimed sessions (mostly for tests).
func (s *SessionStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

// Proxy runs two goroutines that pipe WebSocket messages between spa (the
// joiner's local SPA WS connection) and host (the joiner's connection to the
// authoritative host's server). Bytes are forwarded verbatim — no parsing,
// no reframing, no modification. Either side closing or erroring tears down
// the other.
//
// Blocks until both directions have terminated. Caller is responsible for
// holding the goroutine that invoked Proxy (typically the Hub's HandleWS
// returning will release the WS upgrade).
//
// Thin wrapper over ProxyStreams (see stream.go) so the Direct-connect
// callers keep their *websocket.Conn signature; the Steam Sockets path
// (§14) calls ProxyStreams directly with a ws.Transport on the upstream
// side. Both code paths share the same byte-forwarding loop.
func Proxy(spa, host *websocket.Conn) {
	ProxyStreams(NewWSConnStream(spa), NewWSConnStream(host))
}

// WSConnStream adapts a `*websocket.Conn` to the MessageStream interface so
// ProxyStreams can pipe bytes between WS connections and any other
// MessageStream-shaped transport (notably ws.steamTransport for the Steam
// Sockets joiner path).
//
// The adapter assumes text-frame protocol bytes (the existing
// websocketTransport's contract). For binary frames the caller must wrap
// the conn differently.
type WSConnStream struct {
	conn *websocket.Conn
}

// NewWSConnStream wraps conn so it satisfies MessageStream.
func NewWSConnStream(conn *websocket.Conn) *WSConnStream {
	return &WSConnStream{conn: conn}
}

// ReadMessage reads the next WS frame's payload, discarding the message
// type. The proxy doesn't preserve text-vs-binary distinction; the game
// protocol is text-only by convention.
func (w *WSConnStream) ReadMessage() ([]byte, error) {
	_, data, err := w.conn.ReadMessage()
	return data, err
}

// WriteMessage writes payload as a TextMessage. Matches the
// websocketTransport's WriteMessage in ws/transport_websocket.go.
func (w *WSConnStream) WriteMessage(payload []byte) error {
	return w.conn.WriteMessage(websocket.TextMessage, payload)
}

// Close closes the underlying WS conn. Idempotent in practice — calling
// Close on an already-closed gorilla conn returns an error that the
// proxy loop ignores.
func (w *WSConnStream) Close() error { return w.conn.Close() }

func newToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is exceptional; fall back to a time-based
		// token rather than panicking. Collisions are astronomically
		// unlikely either way.
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b[:])
}
