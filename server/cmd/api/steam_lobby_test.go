package main

import (
	"bufio"
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"

	"webrts/server/internal/game"
	"webrts/server/internal/steam"
	"webrts/server/internal/ws"
)

// fakeShell mediates an IPCBridge under test: reads requests, writes
// responses, and lets the test push notifications to the bridge.
type fakeShell struct {
	conn net.Conn
	mu   sync.Mutex
	on   map[string]func(json.RawMessage) (any, *fakeErr)
	wg   sync.WaitGroup
}

type fakeErr struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func newFakeShell(conn net.Conn) *fakeShell {
	return &fakeShell{conn: conn, on: map[string]func(json.RawMessage) (any, *fakeErr){}}
}

func (s *fakeShell) handle(method string, fn func(json.RawMessage) (any, *fakeErr)) {
	s.mu.Lock()
	s.on[method] = fn
	s.mu.Unlock()
}

func (s *fakeShell) push(event string, params any) error {
	frame := struct {
		Event  string `json:"event"`
		Params any    `json:"params"`
	}{Event: event, Params: params}
	return json.NewEncoder(s.conn).Encode(&frame)
}

func (s *fakeShell) start() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		reader := bufio.NewReader(s.conn)
		enc := json.NewEncoder(s.conn)
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				return
			}
			var req struct {
				ID     string          `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal(line, &req); err != nil {
				continue
			}
			s.mu.Lock()
			fn := s.on[req.Method]
			s.mu.Unlock()
			resp := struct {
				ID     string  `json:"id"`
				Result any     `json:"result,omitempty"`
				Error  *fakeErr `json:"error,omitempty"`
			}{ID: req.ID}
			if fn == nil {
				resp.Error = &fakeErr{Code: "unknown_method", Message: req.Method}
			} else {
				r, herr := fn(req.Params)
				if herr != nil {
					resp.Error = herr
				} else {
					resp.Result = r
				}
			}
			_ = enc.Encode(&resp)
		}
	}()
}

func (s *fakeShell) stop() {
	_ = s.conn.Close()
	s.wg.Wait()
}

// TestWireSteamLobbyHandlers_LobbyHostedFiresOpenListener: a lobby_hosted
// notification triggers OpenListener and installs a peer handler that
// registers transports with the hub.
func TestWireSteamLobbyHandlers_LobbyHostedFiresOpenListener(t *testing.T) {
	bridgeConn, shellConn := net.Pipe()
	shell := newFakeShell(shellConn)
	shell.start()
	t.Cleanup(shell.stop)

	bridge := steam.NewIPCBridgeFromConn(bridgeConn)
	t.Cleanup(func() { _ = bridge.Close() })

	hub := ws.NewHub(game.NewMatchManager(), game.NewLobbyManager())
	t.Cleanup(hub.Close)

	gotOpenListener := make(chan int, 1)
	shell.handle("open_listener", func(params json.RawMessage) (any, *fakeErr) {
		var p struct {
			VirtualPort int `json:"virtualPort"`
		}
		_ = json.Unmarshal(params, &p)
		gotOpenListener <- p.VirtualPort
		return map[string]int{"virtualPort": p.VirtualPort}, nil
	})

	wireSteamLobbyHandlers(bridge, hub)

	if err := shell.push("lobby_hosted", map[string]string{
		"lobbyId":       "12345",
		"hostSteamId64": "76561197960287930",
	}); err != nil {
		t.Fatalf("push lobby_hosted: %v", err)
	}

	select {
	case got := <-gotOpenListener:
		if got != steamVirtualPort {
			t.Errorf("OpenListener virtualPort = %d, want %d", got, steamVirtualPort)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OpenListener IPC was not fired on lobby_hosted")
	}
}

// TestWireSteamLobbyHandlers_LobbyJoinedFiresConnectTo: a lobby_joined
// notification triggers ConnectTo with the supplied hostSteamId64.
func TestWireSteamLobbyHandlers_LobbyJoinedFiresConnectTo(t *testing.T) {
	bridgeConn, shellConn := net.Pipe()
	shell := newFakeShell(shellConn)
	shell.start()
	t.Cleanup(shell.stop)

	bridge := steam.NewIPCBridgeFromConn(bridgeConn)
	t.Cleanup(func() { _ = bridge.Close() })

	hub := ws.NewHub(game.NewMatchManager(), game.NewLobbyManager())
	t.Cleanup(hub.Close)

	gotConnectTo := make(chan string, 1)
	shell.handle("connect_to", func(params json.RawMessage) (any, *fakeErr) {
		var p struct {
			SteamID64 string `json:"steamId64"`
		}
		_ = json.Unmarshal(params, &p)
		gotConnectTo <- p.SteamID64
		return map[string]string{"peerId": "1"}, nil
	})

	wireSteamLobbyHandlers(bridge, hub)

	if err := shell.push("lobby_joined", map[string]string{
		"lobbyId":       "12345",
		"hostSteamId64": "76561197960287930",
	}); err != nil {
		t.Fatalf("push lobby_joined: %v", err)
	}

	select {
	case got := <-gotConnectTo:
		if got != "76561197960287930" {
			t.Errorf("ConnectTo steamId = %q, want 76561197960287930", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ConnectTo IPC was not fired on lobby_joined")
	}
}

// TestWireSteamLobbyHandlers_JoinerParksTransportInSteamSessions: after
// lobby_joined fires + the shell pushes new_peer_transport, the resulting
// steamTransport is parked in the Hub's SteamSessions store (NOT
// registered as a hub client). This is the joiner-as-proxy invariant.
func TestWireSteamLobbyHandlers_JoinerParksTransportInSteamSessions(t *testing.T) {
	bridgeConn, shellConn := net.Pipe()
	shell := newFakeShell(shellConn)
	shell.start()
	t.Cleanup(shell.stop)

	bridge := steam.NewIPCBridgeFromConn(bridgeConn)
	t.Cleanup(func() { _ = bridge.Close() })

	hub := ws.NewHub(game.NewMatchManager(), game.NewLobbyManager())
	t.Cleanup(hub.Close)

	shell.handle("connect_to", func(params json.RawMessage) (any, *fakeErr) {
		return map[string]string{"peerId": "7"}, nil
	})

	wireSteamLobbyHandlers(bridge, hub)

	_ = shell.push("lobby_joined", map[string]string{
		"lobbyId":       "12345",
		"hostSteamId64": "76561197960287930",
	})

	// Wait for the ConnectTo response to land, then push new_peer_transport.
	time.Sleep(50 * time.Millisecond)
	_ = shell.push("new_peer_transport", map[string]string{
		"peerId":    "7",
		"steamId64": "76561197960287930",
		"role":      "joiner",
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.SteamSessions().Has() {
			return // success
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("joiner peer was not parked in hub.SteamSessions")
}
