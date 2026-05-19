package steam

// IPCBridge talks to the Tauri shell's Steamworks layer over the local
// socket the shell creates (Windows named pipe / Unix socket). Wire format
// is newline-delimited JSON, request/response matched by an `id` field
// (§4.4 + design D8).
//
// Phase 2 scope (this file): synchronous LocalPlayer + OpenInviteOverlay,
// best-effort ReportAchievement. Per spec D24 every synchronous call has a
// 5-second timeout; on timeout we discard the in-flight id and return a
// steam_timeout error. Per D19 ReportAchievement is fire-and-forget but
// for Step 2 we keep it synchronous for simplicity — the buffered-channel
// fire-and-forget pattern lands with Step 3.

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const ipcCallTimeout = 5 * time.Second

// IPCBridge is the Phase 2 SteamBridge that proxies SDK calls into the Tauri
// shell. Construct via NewIPCBridge(path) once at server startup; reuse for
// the process lifetime. Safe for concurrent calls.
//
// On Windows the shell exposes TWO named pipes — one per direction —
// because `interprocess`'s ConcurrencyDetector panics on concurrent
// read+write of the same pipe handle (Windows named pipe constraint).
// The shell stamps the combined path into NOMADS_IPC_PATH as
// "c2s=<path>|s2c=<path>". This bridge dials both and routes traffic:
// writes go out on c2s, reads come in on s2c. On Unix the shell uses the
// same shape; one Unix-socket per direction even though Unix sockets
// handle concurrent I/O fine — keeps the wire contract identical across
// platforms.
type IPCBridge struct {
	// writeConn carries Go → Rust traffic (requests, e.g. local_player,
	// create_lobby, send_peer_message). Locked via writeMu while encoding.
	writeConn net.Conn
	// recvConn carries Rust → Go traffic (responses, notifications). Owned
	// by readLoop, never written to.
	recvConn net.Conn

	writeMu sync.Mutex
	encoder *json.Encoder

	pendingMu sync.Mutex
	pending   map[string]chan ipcResponse

	closedMu sync.Mutex
	closed   bool

	// Notifications pushed from the Rust shell (no corresponding request).
	// Set via OnEvent before any traffic; called on the bridge's reader
	// goroutine, so handlers MUST NOT block.
	eventMu  sync.RWMutex
	handlers map[string]func(params json.RawMessage)

	// Peer-event routing (§12). Populated by SetPeerHandler. Lock order:
	// peerMu may NOT be held across a PeerSink callback (the callback would
	// otherwise deadlock if it tried to ForgetPeer itself). See
	// peer_routing.go.
	peerMu      sync.RWMutex
	peers       map[uint64]PeerSink
	peerHandler NewPeerHandler
}

// parseIPCPath understands both the legacy single-path form and the new
// two-pipe "c2s=<path>|s2c=<path>" form. Returns (c2sPath, s2cPath); for
// the legacy form both are the same.
func parseIPCPath(path string) (string, string) {
	if !strings.Contains(path, "=") {
		return path, path
	}
	var c2s, s2c string
	for _, part := range strings.Split(path, "|") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "c2s":
			c2s = kv[1]
		case "s2c":
			s2c = kv[1]
		}
	}
	return c2s, s2c
}

// NewIPCBridge dials the shell-side pipes named in `path` (the combined
// "c2s=...|s2c=..." form set by the shell). Returns an error when either
// dial fails; on success, the caller MUST call Close at shutdown (or
// accept that the read loop runs until process exit).
func NewIPCBridge(path string) (*IPCBridge, error) {
	c2sPath, s2cPath := parseIPCPath(path)
	writeConn, err := dialIPC(c2sPath)
	if err != nil {
		return nil, fmt.Errorf("ipc dial c2s %q: %w", c2sPath, err)
	}
	recvConn, err := dialIPC(s2cPath)
	if err != nil {
		_ = writeConn.Close()
		return nil, fmt.Errorf("ipc dial s2c %q: %w", s2cPath, err)
	}
	return newIPCBridgeFromConns(writeConn, recvConn), nil
}

// NewIPCBridgeFromConn wraps an already-connected net.Conn. Exported for
// integration tests in other packages (e.g. ws/steam_e2e_test.go) that
// want to drive the bridge through an in-memory pipe. The single-conn
// form passes the same conn for both directions — fine for net.Pipe()
// tests which handle concurrent read+write correctly.
func NewIPCBridgeFromConn(conn net.Conn) *IPCBridge {
	return newIPCBridgeFromConns(conn, conn)
}

func newIPCBridgeFromConns(writeConn, recvConn net.Conn) *IPCBridge {
	b := &IPCBridge{
		writeConn: writeConn,
		recvConn:  recvConn,
		encoder:   json.NewEncoder(writeConn),
		pending:   make(map[string]chan ipcResponse),
		handlers:  make(map[string]func(params json.RawMessage)),
	}
	go b.readLoop()
	return b
}

// OnEvent registers a handler for a push notification (Rust → Go one-way
// frame). Overwrites any previous handler for the same event. Handlers run
// on the IPC reader goroutine — they MUST NOT block; offload to another
// goroutine if doing work that could stall.
func (b *IPCBridge) OnEvent(event string, handler func(params json.RawMessage)) {
	b.eventMu.Lock()
	defer b.eventMu.Unlock()
	b.handlers[event] = handler
}

func (b *IPCBridge) dispatchEvent(event string, params json.RawMessage) {
	b.eventMu.RLock()
	h, ok := b.handlers[event]
	b.eventMu.RUnlock()
	if !ok {
		log.Printf("steam ipc: no handler for event %q", event)
		return
	}
	h(params)
}

// Close terminates the bridge and rejects any in-flight calls. Subsequent
// method invocations return steam_channel_closed (per D24).
func (b *IPCBridge) Close() error {
	b.closedMu.Lock()
	if b.closed {
		b.closedMu.Unlock()
		return nil
	}
	b.closed = true
	b.closedMu.Unlock()
	werr := b.writeConn.Close()
	// recvConn might be the same conn as writeConn (test path with one
	// net.Pipe). Closing twice on the same conn is safe — io.ErrClosedPipe.
	if b.recvConn != b.writeConn {
		_ = b.recvConn.Close()
	}
	return werr
}

func (b *IPCBridge) isClosed() bool {
	b.closedMu.Lock()
	defer b.closedMu.Unlock()
	return b.closed
}

// LocalPlayer requests the current Steam user from the shell.
func (b *IPCBridge) LocalPlayer() (LocalPlayer, error) {
	var raw struct {
		SteamID64   string `json:"steamId64"`
		PersonaName string `json:"personaName"`
	}
	if err := b.call(context.Background(), "local_player", nil, &raw); err != nil {
		return LocalPlayer{}, err
	}
	id, err := strconv.ParseUint(raw.SteamID64, 10, 64)
	if err != nil {
		return LocalPlayer{}, fmt.Errorf("ipc local_player: bad steamId64 %q: %w", raw.SteamID64, err)
	}
	return LocalPlayer{
		Available:   true,
		SteamID64:   id,
		PersonaName: raw.PersonaName,
	}, nil
}

// ReportAchievement asks the shell to fire an achievement. Phase 2 Step 2
// keeps this synchronous; the fire-and-forget refactor lands with Step 3.
func (b *IPCBridge) ReportAchievement(id string) error {
	return b.call(context.Background(), "report_achievement", map[string]string{"id": id}, nil)
}

// OpenInviteOverlay asks the shell to surface the Steam friend-invite UI.
func (b *IPCBridge) OpenInviteOverlay(lobbyID string) error {
	return b.call(context.Background(), "open_invite_overlay", map[string]string{"lobbyId": lobbyID}, nil)
}

// RegisterTransport is a no-op on the IPC side — the Rust shell pushes
// Steam Sockets transports into Go via a different path (§12, Step 4).
func (b *IPCBridge) RegisterTransport(_ any) error { return nil }

// CreateLobby asks the shell to create a Steam Matchmaking lobby. Awaits
// the LobbyCreated_t callback via the shared async IPC dispatcher.
func (b *IPCBridge) CreateLobby(maxPlayers int) (string, error) {
	var raw struct {
		LobbyID string `json:"lobbyId"`
	}
	if err := b.call(context.Background(), "create_lobby", map[string]int{"maxPlayers": maxPlayers}, &raw); err != nil {
		return "", err
	}
	return raw.LobbyID, nil
}

// JoinLobby asks the shell to join an existing lobby by SteamID64 string.
func (b *IPCBridge) JoinLobby(lobbyID string) error {
	var raw struct {
		LobbyID string `json:"lobbyId"`
	}
	return b.call(context.Background(), "join_lobby", map[string]string{"lobbyId": lobbyID}, &raw)
}

// call sends a request, awaits its response by id, and unmarshals result
// into out (when non-nil). Returns errors from the wire, the bridge state,
// or the per-call timeout.
func (b *IPCBridge) call(ctx context.Context, method string, params any, out any) error {
	if b.isClosed() {
		return errors.New("steam_channel_closed")
	}

	id := newRequestID()
	ch := make(chan ipcResponse, 1)
	b.pendingMu.Lock()
	b.pending[id] = ch
	b.pendingMu.Unlock()
	defer func() {
		b.pendingMu.Lock()
		delete(b.pending, id)
		b.pendingMu.Unlock()
	}()

	req := ipcRequest{ID: id, Method: method, Params: params}
	b.writeMu.Lock()
	err := b.encoder.Encode(&req)
	b.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("ipc encode %s: %w", method, err)
	}

	timeout := time.NewTimer(ipcCallTimeout)
	defer timeout.Stop()
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
		}
		if out == nil || len(resp.Result) == 0 || string(resp.Result) == "null" {
			return nil
		}
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("ipc decode %s: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timeout.C:
		return errors.New("steam_timeout")
	}
}

func (b *IPCBridge) readLoop() {
	reader := bufio.NewReader(b.recvConn)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if !errors.Is(err, io.EOF) && !b.isClosed() {
				log.Printf("steam ipc: read error: %v", err)
			}
			b.failPending(err)
			return
		}
		if len(line) == 0 {
			continue
		}
		// Frames are one of: Response (has id+result/error) or Notification
		// (has event+params, no id). Detect by parsing into a permissive
		// shape and branching on which fields appear.
		var frame struct {
			ID     string          `json:"id,omitempty"`
			Event  string          `json:"event,omitempty"`
			Params json.RawMessage `json:"params,omitempty"`
			Result json.RawMessage `json:"result,omitempty"`
			Error  *ipcError       `json:"error,omitempty"`
		}
		if err := json.Unmarshal(line, &frame); err != nil {
			log.Printf("steam ipc: invalid frame JSON: %v: %s", err, line)
			continue
		}
		if frame.Event != "" {
			b.dispatchEvent(frame.Event, frame.Params)
			continue
		}
		// Response path.
		resp := ipcResponse{ID: frame.ID, Result: frame.Result, Error: frame.Error}
		b.pendingMu.Lock()
		ch, ok := b.pending[resp.ID]
		b.pendingMu.Unlock()
		if !ok {
			continue
		}
		select {
		case ch <- resp:
		default:
		}
	}
}

func (b *IPCBridge) failPending(err error) {
	// Close all pending channels so blocked callers wake up with an error
	// via the timeout path (they'll already be waking through the timer).
	// Mark as closed first so new calls don't enqueue.
	b.closedMu.Lock()
	b.closed = true
	b.closedMu.Unlock()
}

// ----- Wire types ----------------------------------------------------------

type ipcRequest struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type ipcResponse struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ipcError       `json:"error,omitempty"`
}

type ipcError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func newRequestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return "req-" + hex.EncodeToString(b[:])
}
