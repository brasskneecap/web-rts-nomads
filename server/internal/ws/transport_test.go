package ws

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"webrts/server/internal/game"
	"webrts/server/pkg/protocol"

	"github.com/gorilla/websocket"
)

// TestPeerKind_String covers every PeerKind constant so future additions
// won't silently fall through to "unknown" in log lines.
func TestPeerKind_String(t *testing.T) {
	cases := map[PeerKind]string{
		PeerKindUnknown:   "unknown",
		PeerKindWebSocket: "websocket",
		PeerKindSteam:     "steam",
		PeerKindFake:      "fake",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("PeerKind(%d).String() = %q, want %q", k, got, want)
		}
	}
}

// TestPeerIdentity_String pins the canonical log-line format. Hub code lines
// and joiner-id-in-lobby depend on this; changing it requires updating the
// pluggable-mp-transport spec.
func TestPeerIdentity_String(t *testing.T) {
	cases := []struct {
		p    PeerIdentity
		want string
	}{
		{PeerIdentity{Kind: PeerKindWebSocket, Addr: "192.168.1.50:38112"}, "websocket:192.168.1.50:38112"},
		{PeerIdentity{Kind: PeerKindSteam, Addr: "76561198012345678"}, "steam:76561198012345678"},
		{PeerIdentity{Kind: PeerKindFake, Addr: "test-client-1"}, "fake:test-client-1"},
	}
	for _, tc := range cases {
		if got := tc.p.String(); got != tc.want {
			t.Errorf("%+v.String() = %q, want %q", tc.p, got, tc.want)
		}
	}
}

// TestFakeTransport_PushReadOutgoingClose covers FakeTransport's
// happy-path and EOF behaviour so other tests can rely on it.
func TestFakeTransport_PushReadOutgoingClose(t *testing.T) {
	f := NewFakeTransport("c1", 4)

	// Push two messages, read them back in order.
	f.Push([]byte(`{"type":"a"}`))
	f.Push([]byte(`{"type":"b"}`))
	for _, want := range []string{`{"type":"a"}`, `{"type":"b"}`} {
		got, err := f.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage: %v", err)
		}
		if string(got) != want {
			t.Errorf("ReadMessage = %s, want %s", got, want)
		}
	}

	// Write two outgoing, inspect.
	if err := f.WriteMessage([]byte(`{"out":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := f.WriteMessage([]byte(`{"out":2}`)); err != nil {
		t.Fatal(err)
	}
	out := f.Outgoing()
	if len(out) != 2 || string(out[0]) != `{"out":1}` || string(out[1]) != `{"out":2}` {
		t.Errorf("Outgoing snapshot wrong: %s", out)
	}

	// CloseIncoming → next ReadMessage returns EOF.
	f.CloseIncoming()
	if _, err := f.ReadMessage(); err != io.EOF {
		t.Errorf("ReadMessage after close: got err=%v, want io.EOF", err)
	}
}

// TestFakeTransport_PongHandlerInvoked verifies the SetPongHandler / SimulatePong
// pair used by tests of the heartbeat loop.
func TestFakeTransport_PongHandlerInvoked(t *testing.T) {
	f := NewFakeTransport("c1", 1)
	var calls int
	f.SetPongHandler(func() { calls++ })
	if !f.SimulatePong() {
		t.Fatal("SimulatePong returned false")
	}
	if calls != 1 {
		t.Errorf("pong callback fired %d times, want 1", calls)
	}
}

// TestClient_WriteJSON_BytesEqualJSONMarshal is the structural proof of the
// "Transport-agnostic protocol bytes" requirement: Client.WriteJSON marshals
// once and hands the resulting bytes to Transport.WriteMessage unchanged.
// Any transport that mutates those bytes would be in violation of the spec —
// FakeTransport's Outgoing() captures the raw bytes the transport received,
// which we can compare directly against json.Marshal.
func TestClient_WriteJSON_BytesEqualJSONMarshal(t *testing.T) {
	f := NewFakeTransport("c1", 1)
	c := NewClient(f)

	payload := protocol.NotificationMessage{Type: "notification", Message: "hello world"}
	if err := c.WriteJSON(payload); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	expected, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	out := f.Outgoing()
	if len(out) != 1 {
		t.Fatalf("got %d outgoing messages, want 1", len(out))
	}
	if !bytes.Equal(out[0], expected) {
		t.Errorf("transport-bound bytes differ from json.Marshal:\n got: %s\nwant: %s", out[0], expected)
	}
}

// TestByteIdentity_FakeAndWebSocketAgree runs the same WriteJSON value
// through (a) a FakeTransport and (b) a real WebSocketTransport served by
// httptest, and asserts that the application-protocol payload bytes the
// peer side observes are byte-identical. This is the "Identical bytes
// across transports" scenario in the pluggable-mp-transport spec.
func TestByteIdentity_FakeAndWebSocketAgree(t *testing.T) {
	// Set up an httptest server that upgrades to WS, wraps the conn in
	// websocketTransport, and writes a known payload.
	payload := protocol.NotificationMessage{Type: "notification", Message: "byte-identity-test"}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("server upgrade: %v", err)
			return
		}
		defer conn.Close()
		transport := newWebSocketTransport(conn, conn.RemoteAddr().String())
		client := NewClient(transport)
		if err := client.WriteJSON(payload); err != nil {
			t.Errorf("server WriteJSON: %v", err)
		}
		// Hold the conn open briefly so the client read completes.
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	wsURL := strings.Replace(server.URL, "http://", "ws://", 1)
	u, err := url.Parse(wsURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 3 * time.Second
	c, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	_, wsPayloadBytes, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("client ReadMessage: %v", err)
	}

	// Now run the same payload through the fake.
	fake := NewFakeTransport("fake-side", 1)
	fakeClient := NewClient(fake)
	if err := fakeClient.WriteJSON(payload); err != nil {
		t.Fatalf("fake WriteJSON: %v", err)
	}
	fakeOut := fake.Outgoing()
	if len(fakeOut) != 1 {
		t.Fatalf("fake Outgoing len = %d, want 1", len(fakeOut))
	}

	if !bytes.Equal(wsPayloadBytes, fakeOut[0]) {
		t.Errorf("byte-identity violated:\n  ws:   %s\n  fake: %s", wsPayloadBytes, fakeOut[0])
	}
}

// TestRegisterTransport_FakeReadsAndWritesThroughHub plugs a FakeTransport
// directly into Hub.RegisterTransport and verifies the read loop routes a
// join_match message through to the match manager. This is the "Second
// transport registers successfully" scenario in the spec — proving the hub
// has no WebSocket-specific code in its registration path.
func TestRegisterTransport_FakeReadsAndWritesThroughHub(t *testing.T) {
	// Set up a Hub with a real MatchManager + LobbyManager.
	mm := game.NewMatchManager()
	lm := game.NewLobbyManager()
	hub := NewHub(mm, lm)
	defer hub.Close()

	fake := NewFakeTransport("t-client-1", 8)
	client := hub.RegisterTransport(fake)
	if client == nil {
		t.Fatal("RegisterTransport returned nil")
	}
	if got, want := client.PeerIdentity().String(), "fake:t-client-1"; got != want {
		t.Errorf("PeerIdentity = %q, want %q", got, want)
	}

	// Send join_match with a stable player id; expect the hub to respond
	// with welcome + initial snapshot.
	join := protocol.JoinMatchMessage{
		Type:     "join_match",
		PlayerID: "test-player-1",
	}
	rawJoin, err := json.Marshal(join)
	if err != nil {
		t.Fatalf("marshal join: %v", err)
	}
	fake.Push(rawJoin)

	// Wait briefly for the hub to process. The hub writes welcome then
	// snapshot synchronously inside the message handler.
	if !waitForOutgoing(fake, 2, 2*time.Second) {
		t.Fatalf("hub never produced 2 outgoing messages; got %d", len(fake.Outgoing()))
	}
	out := fake.Outgoing()

	// First message is welcome.
	var welcome protocol.WelcomeMessage
	if err := json.Unmarshal(out[0], &welcome); err != nil {
		t.Fatalf("unmarshal welcome: %v\nraw: %s", err, out[0])
	}
	if welcome.Type != "welcome" || welcome.PlayerID != "test-player-1" {
		t.Errorf("welcome wrong: %+v", welcome)
	}

	// Second message is snapshot. Just shape-check the type field.
	var base map[string]any
	if err := json.Unmarshal(out[1], &base); err != nil {
		t.Fatalf("unmarshal snapshot: %v\nraw: %s", err, out[1])
	}
	if base["type"] != "match_snapshot" {
		t.Errorf("second message type = %v, want match_snapshot", base["type"])
	}

	// Close incoming → hub's read loop exits and cleanup runs.
	fake.CloseIncoming()
}

// TestBroadcastSnapshot_BytesIdenticalToSamePlayer asserts that two
// distinct Client instances representing the SAME player connected to the
// SAME match receive byte-identical snapshot bytes from BroadcastSnapshot,
// EXCEPT for the serverNow field which is set per-iteration to time.Now().
// This is the operational meaning of "Identical bytes across transports"
// for the per-player snapshot path.
func TestBroadcastSnapshot_BytesIdenticalToSamePlayer(t *testing.T) {
	mm := game.NewMatchManager()
	lm := game.NewLobbyManager()
	hub := NewHub(mm, lm)
	defer hub.Close()

	fakeA := NewFakeTransport("c-a", 8)
	fakeB := NewFakeTransport("c-b", 8)
	clientA := hub.RegisterTransport(fakeA)
	clientB := hub.RegisterTransport(fakeB)

	// Both clients identify as the same player — legitimate during reconnect
	// or in a multi-window test scenario. SnapshotForPlayer returns identical
	// bytes for the same playerID.
	clientA.SetPlayerID("same-player")
	clientB.SetPlayerID("same-player")

	join := protocol.JoinMatchMessage{
		Type:     "join_match",
		PlayerID: "same-player",
	}
	rawJoin, _ := json.Marshal(join)
	// Drive clientA's join through the read loop so a match exists.
	fakeA.Push(rawJoin)
	if !waitForOutgoing(fakeA, 2, 2*time.Second) {
		t.Fatalf("clientA didn't get welcome+snapshot; got %d", len(fakeA.Outgoing()))
	}

	// Also join clientB into the same match.
	matches := mm.ListMatches()
	if len(matches) != 1 {
		t.Fatalf("want 1 match, got %d", len(matches))
	}
	match := matches[0]
	match.AddClient(clientB)

	// Manually trigger a broadcast.
	// Stash the current outgoing counts so we can isolate the broadcast deltas.
	preA := len(fakeA.Outgoing())
	preB := len(fakeB.Outgoing())
	match.BroadcastSnapshot()

	if !waitForOutgoing(fakeA, preA+1, 1*time.Second) || !waitForOutgoing(fakeB, preB+1, 1*time.Second) {
		t.Fatalf("broadcast did not reach both clients")
	}
	outA := fakeA.Outgoing()
	outB := fakeB.Outgoing()
	bytesA := outA[len(outA)-1]
	bytesB := outB[len(outB)-1]

	// Strip serverNow (per-iteration timestamp) before comparing.
	normA := normalizeServerNow(t, bytesA)
	normB := normalizeServerNow(t, bytesB)
	if !bytes.Equal(normA, normB) {
		t.Errorf("snapshot bytes differ between transports for same player:\nA: %s\nB: %s", normA, normB)
	}
}

func normalizeServerNow(t *testing.T, raw []byte) []byte {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("normalize unmarshal: %v\nraw: %s", err, raw)
	}
	if _, ok := obj["serverNow"]; ok {
		obj["serverNow"] = float64(0)
	}
	norm, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("normalize marshal: %v", err)
	}
	return norm
}

func waitForOutgoing(f *FakeTransport, n int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(f.Outgoing()) >= n {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

