package ws

import (
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// websocketTransport wraps a gorilla/websocket connection as a Transport.
// This is the canonical implementation; bytes flowing through it are the
// reference for byte-identity comparisons against other Transport
// implementations.
type websocketTransport struct {
	conn   *websocket.Conn
	addr   string
	closed atomic.Bool

	writeMu sync.Mutex // serialises WriteMessage and WritePing
}

// newWebSocketTransport constructs a transport over an already-upgraded
// gorilla/websocket connection. addr SHOULD be the remote peer's address
// (host:port) for log lines; an empty string is permitted but discouraged.
func newWebSocketTransport(conn *websocket.Conn, addr string) *websocketTransport {
	conn.SetReadLimit(readLimit)
	if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		log.Println("ws transport: set initial read deadline:", err)
	}
	return &websocketTransport{conn: conn, addr: addr}
}

func (t *websocketTransport) ReadMessage() ([]byte, error) {
	_, data, err := t.conn.ReadMessage()
	if err == nil {
		// Any inbound activity proves the peer is alive; extend the read
		// deadline so the hub's app-level heartbeat (sends a {type:"ping"}
		// every heartbeatInterval, expects a {type:"pong"} reply) is what
		// governs the liveness window. Without this, the gorilla read
		// deadline would expire after pongWait even though valid pong
		// messages were arriving — defeating the heartbeat. The gorilla
		// SetPongHandler also extends the deadline for legacy WS-frame
		// pongs, so that path keeps working for any future code path that
		// still uses WritePing directly.
		_ = t.conn.SetReadDeadline(time.Now().Add(pongWait))
	}
	return data, err
}

func (t *websocketTransport) WriteMessage(payload []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if err := t.conn.SetWriteDeadline(time.Now().Add(writeDeadline)); err != nil {
		return err
	}
	return t.conn.WriteMessage(websocket.TextMessage, payload)
}

func (t *websocketTransport) WritePing() error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if err := t.conn.SetWriteDeadline(time.Now().Add(writeDeadline)); err != nil {
		return err
	}
	return t.conn.WriteMessage(websocket.PingMessage, nil)
}

func (t *websocketTransport) Close() error {
	if t.closed.Swap(true) {
		return nil
	}
	return t.conn.Close()
}

func (t *websocketTransport) PeerIdentity() PeerIdentity {
	return PeerIdentity{Kind: PeerKindWebSocket, Addr: t.addr}
}

// SetPongHandler installs cb as the gorilla pong handler and extends the read
// deadline on each pong. The hub uses this to refresh per-client liveness.
func (t *websocketTransport) SetPongHandler(cb func()) {
	t.conn.SetPongHandler(func(string) error {
		if cb != nil {
			cb()
		}
		return t.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
}
