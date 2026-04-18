package ws

import (
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// writeDeadline is the maximum time allowed to complete a single write.
	writeDeadline = 10 * time.Second

	// readLimit caps the size of an incoming WebSocket message.
	// Client commands are small JSON; 64 KB is a generous ceiling.
	readLimit = 64 * 1024

	// pongWait is how long the server will wait for the next pong before
	// considering the connection dead.
	pongWait = 75 * time.Second
)

type Client struct {
	Conn *websocket.Conn

	mu       sync.Mutex
	matchID  string
	playerID string
	lastPong time.Time
}

func NewClient(conn *websocket.Conn) *Client {
	conn.SetReadLimit(readLimit)

	c := &Client{
		Conn:     conn,
		lastPong: time.Now(),
	}

	// Extend the read deadline each time the client sends a pong.
	conn.SetPongHandler(func(string) error {
		c.TouchPong()
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	// Arm the initial read deadline.
	if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		log.Println("set initial read deadline:", err)
	}

	return c
}

// MatchID returns the client's current match ID, safe for concurrent access.
func (c *Client) MatchID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.matchID
}

// SetMatchID sets the client's current match ID, safe for concurrent access.
func (c *Client) SetMatchID(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.matchID = id
}

// PlayerID returns the client's player ID, safe for concurrent access.
func (c *Client) PlayerID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.playerID
}

// SetPlayerID sets the client's player ID, safe for concurrent access.
func (c *Client) SetPlayerID(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.playerID = id
}

// WriteJSON serializes v to JSON and sends it as a single WebSocket text
// frame. Applies a write deadline before every send.
func (c *Client) WriteJSON(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.Conn.SetWriteDeadline(time.Now().Add(writeDeadline)); err != nil {
		return err
	}
	return c.Conn.WriteJSON(v)
}

// WritePing sends a WebSocket ping control frame with a write deadline.
func (c *Client) WritePing() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.Conn.SetWriteDeadline(time.Now().Add(writeDeadline)); err != nil {
		return err
	}
	return c.Conn.WriteMessage(websocket.PingMessage, nil)
}

func (c *Client) TouchPong() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastPong = time.Now()
}

func (c *Client) LastPong() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastPong
}

func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.Conn.Close(); err != nil {
		log.Println("close ws:", err)
	}
}
