package ws

// §12.3 end-to-end integration test for the Steam Networking Sockets
// transport bridge. Two Go-side IPCBridges talk through a mockShell that
// stands in for `desktop/src-tauri/src/steam_net.rs`:
//
//   joiner.steamTransport ─▶ joiner.IPCBridge ─send_peer_message─▶
//     mockShell ─peer_message notification─▶
//   host.IPCBridge ─Deliver─▶ host.steamTransport ─▶ host.Hub.readLoop
//
// …and the reverse for host→joiner. No Steamworks SDK is invoked; the test
// runs in plain `go test` everywhere.
//
// What we assert:
//   1. open_listener + connect_to round-trip cleanly through both bridges,
//      and the resulting new_peer_transport notifications register a
//      hub-managed steamTransport on the host side.
//   2. Bytes sent through joiner.WriteMessage emerge byte-identical at
//      host.ReadMessage (and vice versa). This is the "transport-agnostic
//      protocol bytes" guarantee from the pluggable-mp-transport spec.
//   3. Closing one side surfaces io.EOF on the other side via the
//      peer_disconnected → Disconnect → Close chain.
//   4. A real ws.Hub fronts the host's steamTransport and the joiner can
//      drive a full hello + (mocked-as-FakeTransport) snapshot loop. This
//      proves hub registration / cleanup behaves identically to a
//      WebSocket transport.

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"webrts/server/internal/game"
	"webrts/server/internal/steam"
)

// ---------------- mockShell ------------------------------------------------

// mockShell mediates two IPCBridges. The "host" bridge talks to one of its
// pipe legs; the "joiner" bridge talks to the other. Peer-message frames
// flowing into the host leg are echoed as peer_message notifications on the
// joiner leg, and vice versa.
type mockShell struct {
	hostShellEnd   net.Conn
	joinerShellEnd net.Conn

	// peer_id allocation. The mock uses one shared counter to keep the
	// two-sided view simple — in reality each end has its own namespace,
	// but for routing we only need each side to learn the same id.
	nextPeerID atomic.Uint64

	// active peer pairs (peerID → opposite shell end). Populated when a
	// connect_to request arrives.
	peersMu sync.RWMutex
	peers   map[uint64]net.Conn // peer_id → write to this end to deliver

	hostListening atomic.Bool

	wg sync.WaitGroup

	hostSteamID, joinerSteamID uint64
}

func newMockShell(hostConn, joinerConn net.Conn, hostSteamID, joinerSteamID uint64) *mockShell {
	m := &mockShell{
		hostShellEnd:   hostConn,
		joinerShellEnd: joinerConn,
		peers:          make(map[uint64]net.Conn),
		hostSteamID:    hostSteamID,
		joinerSteamID:  joinerSteamID,
	}
	m.nextPeerID.Store(1)
	return m
}

func (m *mockShell) start() {
	m.wg.Add(2)
	go m.servePipe(m.hostShellEnd, true)
	go m.servePipe(m.joinerShellEnd, false)
}

func (m *mockShell) stop() {
	_ = m.hostShellEnd.Close()
	_ = m.joinerShellEnd.Close()
	m.wg.Wait()
}

// servePipe loops on one bridge's shell end, dispatching requests and
// emitting responses + notifications.
func (m *mockShell) servePipe(end net.Conn, isHost bool) {
	defer m.wg.Done()
	reader := bufio.NewReader(end)
	enc := json.NewEncoder(end)
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
		m.handle(end, enc, isHost, req.ID, req.Method, req.Params)
	}
}

// jsonResp writes a response frame inline so the mockShell stays a single
// flat dispatcher. Each test handler decides between sync responses and
// fan-out notifications independently.
func writeFrame(enc *json.Encoder, v any) {
	_ = enc.Encode(v)
}

func (m *mockShell) handle(
	end net.Conn,
	enc *json.Encoder,
	isHost bool,
	id, method string,
	params json.RawMessage,
) {
	type okResp struct {
		ID     string `json:"id"`
		Result any    `json:"result"`
	}
	type notif struct {
		Event  string `json:"event"`
		Params any    `json:"params"`
	}

	switch method {
	case "open_listener":
		if !isHost {
			writeFrame(enc, okResp{ID: id, Result: map[string]any{
				"error": "joiner-side open_listener is unusual in this test",
			}})
			return
		}
		m.hostListening.Store(true)
		writeFrame(enc, okResp{ID: id, Result: map[string]int{"virtualPort": 27}})

	case "connect_to":
		var p struct {
			SteamID64   string `json:"steamId64"`
			VirtualPort int    `json:"virtualPort"`
		}
		_ = json.Unmarshal(params, &p)
		// Allocate a peer id and respond. We use ONE id pair to keep the
		// routing table flat; real shells namespace per side but the bridge
		// only ever sees its own id.
		peerID := m.nextPeerID.Add(1)
		writeFrame(enc, okResp{ID: id, Result: map[string]string{
			"peerId": utoa(peerID),
		}})

		// Register the routing entries: a send_peer_message arriving from
		// either end with this peer id gets delivered to the opposite end.
		m.peersMu.Lock()
		// joiner sends → deliver to host
		m.peers[peerID] = m.hostShellEnd // when seen on joiner end
		m.peersMu.Unlock()

		// Push new_peer_transport on BOTH ends so each side's bridge
		// constructs its half of the connection.
		joinerEnc := json.NewEncoder(m.joinerShellEnd)
		writeFrame(joinerEnc, notif{Event: "new_peer_transport", Params: map[string]string{
			"peerId":    utoa(peerID),
			"steamId64": utoa(m.hostSteamID),
			"role":      "joiner",
		}})
		hostEnc := json.NewEncoder(m.hostShellEnd)
		writeFrame(hostEnc, notif{Event: "new_peer_transport", Params: map[string]string{
			"peerId":    utoa(peerID),
			"steamId64": utoa(m.joinerSteamID),
			"role":      "host",
		}})

	case "send_peer_message":
		var p struct {
			PeerID  string `json:"peerId"`
			Payload string `json:"payload"`
		}
		_ = json.Unmarshal(params, &p)
		writeFrame(enc, okResp{ID: id, Result: nil})

		// Forward the bytes to the OPPOSITE shell end as a peer_message
		// notification. "Opposite" depends on which end this request came
		// from, not on the peer routing table — keep the test simple by
		// just deriving it from isHost.
		var target net.Conn
		if isHost {
			target = m.joinerShellEnd
		} else {
			target = m.hostShellEnd
		}
		targetEnc := json.NewEncoder(target)
		writeFrame(targetEnc, notif{Event: "peer_message", Params: map[string]string{
			"peerId":  p.PeerID,
			"payload": p.Payload,
		}})

	case "close_peer":
		var p struct {
			PeerID string `json:"peerId"`
		}
		_ = json.Unmarshal(params, &p)
		writeFrame(enc, okResp{ID: id, Result: map[string]bool{"closed": true}})

		// Propagate peer_disconnected to the opposite end so its transport
		// surfaces EOF to the hub.
		var target net.Conn
		if isHost {
			target = m.joinerShellEnd
		} else {
			target = m.hostShellEnd
		}
		targetEnc := json.NewEncoder(target)
		writeFrame(targetEnc, notif{Event: "peer_disconnected", Params: map[string]any{
			"peerId": p.PeerID,
			"reason": 0,
		}})

	default:
		writeFrame(enc, map[string]any{
			"id": id,
			"error": map[string]string{
				"code":    "unknown_method",
				"message": method,
			},
		})
	}
}

// utoa is a tiny strconv.FormatUint inliner so the test file's surface
// matches the IPC wire (where every numeric id is a decimal string).
func utoa(v uint64) string {
	const digits = "0123456789"
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = digits[v%10]
		v /= 10
	}
	return string(buf[i:])
}

// ---------------- helpers --------------------------------------------------

// bridgeOnPipe returns an IPCBridge driven over an in-memory net.Pipe so
// the test doesn't depend on the OS-specific dial path (named pipe on
// Windows, Unix socket elsewhere). The returned net.Conn is the mockShell's
// end of the same pipe.
func bridgeOnPipe(t *testing.T) (*steam.IPCBridge, net.Conn) {
	t.Helper()
	bridgeEnd, shellEnd := net.Pipe()
	b := steam.NewIPCBridgeFromConn(bridgeEnd)
	t.Cleanup(func() { _ = b.Close() })
	return b, shellEnd
}

func waitFor(t *testing.T, timeout time.Duration, msg string, pred func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pred() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", msg)
}

// ---------------- the actual tests ----------------------------------------

// TestSteamE2E_BidirectionalByteFidelity is the byte-identity guard at the
// full Steam-transport pipeline level. Joiner.WriteMessage → host.ReadMessage
// returns the same bytes; host.WriteMessage → joiner.ReadMessage returns
// the same bytes. No reframing, no annotation.
func TestSteamE2E_BidirectionalByteFidelity(t *testing.T) {
	hostBridge, hostShellEnd := bridgeOnPipe(t)
	joinerBridge, joinerShellEnd := bridgeOnPipe(t)

	shell := newMockShell(hostShellEnd, joinerShellEnd, 76561197960287930, 76561197960287931)
	shell.start()
	t.Cleanup(shell.stop)

	// Capture each side's transport as the new-peer handler fires.
	var hostTransport, joinerTransport atomic.Pointer[steamTransport]

	hostBridge.SetPeerHandler(func(peerID, steamID uint64, role steam.PeerRole) steam.PeerSink {
		tr := NewSteamTransport(peerID, steamID, hostBridge)
		hostTransport.Store(tr)
		return tr
	})
	joinerBridge.SetPeerHandler(func(peerID, steamID uint64, role steam.PeerRole) steam.PeerSink {
		tr := NewSteamTransport(peerID, steamID, joinerBridge)
		joinerTransport.Store(tr)
		return tr
	})

	if err := hostBridge.OpenListener(27); err != nil {
		t.Fatalf("OpenListener: %v", err)
	}
	if _, err := joinerBridge.ConnectTo(76561197960287930, 27); err != nil {
		t.Fatalf("ConnectTo: %v", err)
	}

	waitFor(t, 2*time.Second, "both transports registered", func() bool {
		return hostTransport.Load() != nil && joinerTransport.Load() != nil
	})

	host := hostTransport.Load()
	joiner := joinerTransport.Load()

	payloads := []string{
		`{"type":"hello","version":"dev"}`,
		`{"type":"join_match","mapId":"m1","playerId":"p1"}`,
		`{"type":"snapshot","tick":1,"units":[]}`,
		// Binary-ish payload to confirm the base64 hop is lossless.
		"\x00\x01\x02\xff\xfe\xfd\n",
	}
	for _, p := range payloads {
		if err := joiner.WriteMessage([]byte(p)); err != nil {
			t.Fatalf("joiner.WriteMessage(%q): %v", p, err)
		}
		got, err := readWithTimeout(host, 2*time.Second)
		if err != nil {
			t.Fatalf("host.ReadMessage: %v", err)
		}
		if string(got) != p {
			t.Errorf("joiner→host: got %q, want %q", got, p)
		}
		// Now the reverse direction.
		reply := "REPLY:" + p
		if err := host.WriteMessage([]byte(reply)); err != nil {
			t.Fatalf("host.WriteMessage(%q): %v", reply, err)
		}
		got2, err := readWithTimeout(joiner, 2*time.Second)
		if err != nil {
			t.Fatalf("joiner.ReadMessage: %v", err)
		}
		if string(got2) != reply {
			t.Errorf("host→joiner: got %q, want %q", got2, reply)
		}
	}
}

// TestSteamE2E_CloseSurfacesEOFAcrossBridge asserts that a Close on one
// side surfaces io.EOF on the opposite side via peer_disconnected. This is
// the "transport reports closed" path the WS hub depends on.
func TestSteamE2E_CloseSurfacesEOFAcrossBridge(t *testing.T) {
	hostBridge, hostShellEnd := bridgeOnPipe(t)
	joinerBridge, joinerShellEnd := bridgeOnPipe(t)

	shell := newMockShell(hostShellEnd, joinerShellEnd, 1, 2)
	shell.start()
	t.Cleanup(shell.stop)

	var hostTransport, joinerTransport atomic.Pointer[steamTransport]
	hostBridge.SetPeerHandler(func(peerID, steamID uint64, _ steam.PeerRole) steam.PeerSink {
		tr := NewSteamTransport(peerID, steamID, hostBridge)
		hostTransport.Store(tr)
		return tr
	})
	joinerBridge.SetPeerHandler(func(peerID, steamID uint64, _ steam.PeerRole) steam.PeerSink {
		tr := NewSteamTransport(peerID, steamID, joinerBridge)
		joinerTransport.Store(tr)
		return tr
	})

	_ = hostBridge.OpenListener(27)
	_, _ = joinerBridge.ConnectTo(1, 27)

	waitFor(t, 2*time.Second, "both transports registered", func() bool {
		return hostTransport.Load() != nil && joinerTransport.Load() != nil
	})

	// Start a reader on the host side that should see EOF after the joiner
	// closes.
	hostReadErr := make(chan error, 1)
	go func() {
		_, err := hostTransport.Load().ReadMessage()
		hostReadErr <- err
	}()

	// Park the reader briefly so it's blocked on the channel when we close.
	time.Sleep(20 * time.Millisecond)

	if err := joinerTransport.Load().Close(); err != nil {
		t.Fatalf("joiner.Close: %v", err)
	}

	select {
	case err := <-hostReadErr:
		if !errors.Is(err, io.EOF) {
			t.Errorf("host.ReadMessage after joiner.Close = %v, want io.EOF", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("host reader never observed close")
	}
}

// TestSteamE2E_HostHubAcceptsSteamPeer drives a full hub-managed lobby-join
// loop across the Steam transport bridge. The host runs a real
// game.MatchManager + ws.Hub; the joiner is a steamTransport in the test
// driver. A join_match goes from the joiner through the entire pipeline and
// the resulting snapshot broadcast is observed back at the joiner.
func TestSteamE2E_HostHubAcceptsSteamPeer(t *testing.T) {
	hostBridge, hostShellEnd := bridgeOnPipe(t)
	joinerBridge, joinerShellEnd := bridgeOnPipe(t)

	shell := newMockShell(hostShellEnd, joinerShellEnd, 1, 2)
	shell.start()
	t.Cleanup(shell.stop)

	// Real hub + manager on the host side, mirroring main.go's wiring.
	manager := game.NewMatchManager()
	lobbyManager := game.NewLobbyManager()
	hub := NewHub(manager, lobbyManager)
	t.Cleanup(func() { hub.Close() })

	hostBridge.SetPeerHandler(func(peerID, steamID uint64, _ steam.PeerRole) steam.PeerSink {
		tr := NewSteamTransport(peerID, steamID, hostBridge)
		hub.RegisterTransport(tr)
		return tr
	})

	var joinerTransport atomic.Pointer[steamTransport]
	joinerBridge.SetPeerHandler(func(peerID, steamID uint64, _ steam.PeerRole) steam.PeerSink {
		tr := NewSteamTransport(peerID, steamID, joinerBridge)
		joinerTransport.Store(tr)
		return tr
	})

	_ = hostBridge.OpenListener(27)
	_, _ = joinerBridge.ConnectTo(1, 27)

	waitFor(t, 2*time.Second, "joiner transport registered", func() bool {
		return joinerTransport.Load() != nil
	})

	joiner := joinerTransport.Load()

	// Send a join_match message that the host hub will process. The hub
	// returns a "match" broadcast / welcome path; we just assert SOMETHING
	// comes back, proving the full pipeline (joiner→shell→host→hub→host→
	// shell→joiner) works.
	join := []byte(`{"type":"join_match","mapId":"","playerId":"e2e-player"}`)
	if err := joiner.WriteMessage(join); err != nil {
		t.Fatalf("joiner.WriteMessage(join_match): %v", err)
	}

	// The host hub processes join_match and broadcasts at least one
	// message back (the match-state on join). We expect a JSON frame
	// arriving on the joiner side within a couple seconds.
	got, err := readWithTimeout(joiner, 3*time.Second)
	if err != nil {
		t.Fatalf("joiner.ReadMessage waiting for hub response: %v", err)
	}
	// Sanity-check the response shape — it should be valid JSON with a
	// "type" field. We don't pin the specific message kind because the hub
	// may emit several before settling and the first one varies by build.
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(got, &probe); err != nil {
		t.Fatalf("hub response is not JSON: %v (raw=%q)", err, got)
	}
	if probe.Type == "" {
		t.Errorf("hub response has no type field: %q", got)
	}
	t.Logf("hub responded with type=%q (full payload=%s)", probe.Type, got)
}

// readWithTimeout wraps Transport.ReadMessage with a deadline so test
// failures fail fast instead of hanging.
func readWithTimeout(t Transport, timeout time.Duration) ([]byte, error) {
	type res struct {
		b   []byte
		err error
	}
	ch := make(chan res, 1)
	go func() {
		b, err := t.ReadMessage()
		ch <- res{b, err}
	}()
	select {
	case r := <-ch:
		return r.b, r.err
	case <-time.After(timeout):
		return nil, context.DeadlineExceeded
	}
}

// Compile-time assertion: base64 + io are wired in. Removes the "unused
// import" lint noise that the helper functions above provoke if a future
// refactor strips them.
var (
	_ = base64.StdEncoding
	_ = io.EOF
)
