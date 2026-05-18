package steam

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeShell implements a minimal Rust-shell IPC counterpart for tests:
// reads JSON requests from the bridge end of a net.Pipe, dispatches to
// user-supplied handler functions, writes JSON responses back.
type fakeShell struct {
	conn     net.Conn
	handlers map[string]func(params json.RawMessage) (any, *ipcError)
	wg       sync.WaitGroup
}

func newFakeShell(conn net.Conn) *fakeShell {
	return &fakeShell{
		conn:     conn,
		handlers: make(map[string]func(params json.RawMessage) (any, *ipcError)),
	}
}

func (f *fakeShell) on(method string, handler func(params json.RawMessage) (any, *ipcError)) {
	f.handlers[method] = handler
}

func (f *fakeShell) start() {
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		reader := bufio.NewReader(f.conn)
		encoder := json.NewEncoder(f.conn)
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
			h, ok := f.handlers[req.Method]
			var resp ipcResponse
			resp.ID = req.ID
			if !ok {
				resp.Error = &ipcError{Code: "unknown_method", Message: req.Method}
			} else {
				result, herr := h(req.Params)
				if herr != nil {
					resp.Error = herr
				} else {
					raw, _ := json.Marshal(result)
					resp.Result = raw
				}
			}
			_ = encoder.Encode(&resp)
		}
	}()
}

func (f *fakeShell) close() {
	_ = f.conn.Close()
	f.wg.Wait()
}

func newTestBridge(t *testing.T) (*IPCBridge, *fakeShell) {
	t.Helper()
	bridgeConn, shellConn := net.Pipe()
	shell := newFakeShell(shellConn)
	shell.start()
	bridge := newIPCBridgeFromConn(bridgeConn)
	t.Cleanup(func() {
		_ = bridge.Close()
		shell.close()
	})
	return bridge, shell
}

// TestIPCBridge_LocalPlayer_Roundtrip covers the happy path: SPA-side
// LocalPlayer call → shell handler → response decoded into LocalPlayer.
func TestIPCBridge_LocalPlayer_Roundtrip(t *testing.T) {
	bridge, shell := newTestBridge(t)
	shell.on("local_player", func(_ json.RawMessage) (any, *ipcError) {
		return map[string]any{
			"steamId64":   "76561197960287930",
			"personaName": "gabe",
		}, nil
	})

	got, err := bridge.LocalPlayer()
	if err != nil {
		t.Fatalf("LocalPlayer: %v", err)
	}
	if !got.Available {
		t.Errorf("Available = false, want true")
	}
	if got.SteamID64 != 76561197960287930 {
		t.Errorf("SteamID64 = %d, want 76561197960287930", got.SteamID64)
	}
	if got.PersonaName != "gabe" {
		t.Errorf("PersonaName = %q, want gabe", got.PersonaName)
	}
}

// TestIPCBridge_ReportAchievement_Roundtrip exercises a void-result method.
func TestIPCBridge_ReportAchievement_Roundtrip(t *testing.T) {
	bridge, shell := newTestBridge(t)
	gotID := make(chan string, 1)
	shell.on("report_achievement", func(params json.RawMessage) (any, *ipcError) {
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &ipcError{Code: "bad_params", Message: err.Error()}
		}
		gotID <- p.ID
		return nil, nil
	})

	if err := bridge.ReportAchievement("ACH_FIRST_WAVE_CLEARED"); err != nil {
		t.Fatalf("ReportAchievement: %v", err)
	}
	select {
	case id := <-gotID:
		if id != "ACH_FIRST_WAVE_CLEARED" {
			t.Errorf("shell received id = %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("shell never received the achievement id")
	}
}

// TestIPCBridge_ErrorResponseSurfacedToCaller covers steam_unavailable etc.
func TestIPCBridge_ErrorResponseSurfacedToCaller(t *testing.T) {
	bridge, shell := newTestBridge(t)
	shell.on("local_player", func(_ json.RawMessage) (any, *ipcError) {
		return nil, &ipcError{Code: "steam_unavailable", Message: "no Steam"}
	})

	_, err := bridge.LocalPlayer()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "steam_unavailable") {
		t.Errorf("error %q should contain steam_unavailable", err)
	}
}

// TestIPCBridge_UnknownMethodSurfacesShellError covers the wire-level
// unknown_method response from the shell.
func TestIPCBridge_UnknownMethodSurfacesShellError(t *testing.T) {
	bridge, _ := newTestBridge(t)
	// No handlers registered → shell replies with unknown_method.
	err := bridge.ReportAchievement("ACH_NOPE")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown_method") {
		t.Errorf("err = %q, want unknown_method", err)
	}
}
