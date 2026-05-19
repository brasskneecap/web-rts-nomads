package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"webrts/server/internal/game"
	"webrts/server/internal/transportbridge"
	"webrts/server/pkg/protocol"

	"github.com/gorilla/websocket"
)

// TestDirectConnect_EndToEnd is the smoke test for §11.2 + §11.5 + §13.4:
// two independent Hub instances stand in for "host" and "joiner" servers.
// The joiner dials the host via transportbridge, stashes the resulting conn
// in the joiner's session store, then a "SPA" client connects to the
// joiner's WS with ?proxy=<token>. join_match bytes from the SPA reach the
// host's hub and the host's welcome + snapshot bytes flow back to the SPA —
// proving the proxy wire-up doesn't break the game's existing message flow.
func TestDirectConnect_EndToEnd(t *testing.T) {
	// --- "Host" server ----------------------------------------------------
	hostHub := NewHub(game.NewMatchManager(), game.NewLobbyManager())
	defer hostHub.Close()
	hostMux := http.NewServeMux()
	hostMux.HandleFunc("/ws", hostHub.HandleWS)
	hostSrv := httptest.NewServer(hostMux)
	defer hostSrv.Close()
	hostPort := strings.TrimPrefix(hostSrv.URL, "http://")

	// --- "Joiner" server --------------------------------------------------
	joinerHub := NewHub(game.NewMatchManager(), game.NewLobbyManager())
	defer joinerHub.Close()
	joinerMux := http.NewServeMux()
	joinerMux.HandleFunc("/ws", joinerHub.HandleWS)
	joinerSrv := httptest.NewServer(joinerMux)
	defer joinerSrv.Close()

	// --- Joiner dials host and stashes the conn ---------------------------
	hostConn, err := transportbridge.ConnectToHost(
		context.Background(),
		hostPort,
		false,
	)
	if err != nil {
		t.Fatalf("ConnectToHost: %v", err)
	}
	token := joinerHub.DirectSessions().Put(hostConn.Conn)
	if token == "" {
		t.Fatal("empty token")
	}

	// --- "SPA" connects to the joiner with ?proxy=<token> -----------------
	joinerWsURL, _ := url.Parse(strings.Replace(joinerSrv.URL, "http", "ws", 1))
	joinerWsURL.Path = "/ws"
	joinerWsURL.RawQuery = "proxy=" + token

	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 3 * time.Second
	spa, _, err := dialer.Dial(joinerWsURL.String(), nil)
	if err != nil {
		t.Fatalf("SPA dial: %v", err)
	}
	defer spa.Close()

	// --- SPA sends join_match (travels: SPA → joiner proxy → host hub) ----
	join := protocol.JoinMatchMessage{
		Type:            "join_match",
		PlayerID:        "joiner-player-1",
		EquippedBuffIDs: []string{},
	}
	rawJoin, _ := json.Marshal(join)
	if err := spa.WriteMessage(websocket.TextMessage, rawJoin); err != nil {
		t.Fatalf("SPA write join_match: %v", err)
	}

	// --- Expect welcome + snapshot back through the proxy -----------------
	spa.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, raw, err := spa.ReadMessage()
	if err != nil {
		t.Fatalf("SPA read welcome: %v", err)
	}
	var welcome protocol.WelcomeMessage
	if err := json.Unmarshal(raw, &welcome); err != nil {
		t.Fatalf("unmarshal welcome: %v\nraw: %s", err, raw)
	}
	if welcome.Type != "welcome" || welcome.PlayerID != "joiner-player-1" {
		t.Fatalf("unexpected welcome: %+v", welcome)
	}

	_, raw2, err := spa.ReadMessage()
	if err != nil {
		t.Fatalf("SPA read snapshot: %v", err)
	}
	var snapBase map[string]any
	if err := json.Unmarshal(raw2, &snapBase); err != nil {
		t.Fatalf("unmarshal snapshot: %v\nraw: %s", err, raw2)
	}
	if snapBase["type"] != "match_snapshot" {
		t.Errorf("second message type = %v, want match_snapshot", snapBase["type"])
	}

	// --- Token is consumed (Take is one-shot) -----------------------------
	if joinerHub.DirectSessions().Len() != 0 {
		t.Errorf("session store should be empty after consume; len = %d",
			joinerHub.DirectSessions().Len())
	}

	// --- Clean close from SPA tears down host conn ------------------------
	if err := spa.Close(); err != nil {
		t.Logf("spa close: %v", err)
	}
	// Allow proxy goroutines to drain.
	time.Sleep(100 * time.Millisecond)
}

// TestDirectConnect_UnknownTokenReturns502 — proxy mode with a token the
// session store doesn't know about (expired, never issued, typo) must
// return 502 with a JSON error body.
func TestDirectConnect_UnknownTokenReturns502(t *testing.T) {
	hub := NewHub(game.NewMatchManager(), game.NewLobbyManager())
	defer hub.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.HandleWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	wsURL, _ := url.Parse(strings.Replace(srv.URL, "http", "ws", 1))
	wsURL.Path = "/ws"
	wsURL.RawQuery = "proxy=this-token-does-not-exist"

	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 2 * time.Second
	_, resp, err := dialer.Dial(wsURL.String(), nil)
	if err == nil {
		t.Fatal("dial unexpectedly succeeded for unknown token")
	}
	if resp == nil {
		t.Fatalf("no HTTP response on dial failure: %v", err)
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 BadGateway", resp.StatusCode)
	}
}
