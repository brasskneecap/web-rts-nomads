package ws

import (
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	Conn     *websocket.Conn
	PlayerID string
	MatchID  string

	mu       sync.Mutex
	lastPong time.Time
}

func NewClient(conn *websocket.Conn) *Client {
	return &Client{
		Conn:     conn,
		lastPong: time.Now(),
	}
}

func (c *Client) WriteJSON(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Conn.WriteJSON(v)
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
