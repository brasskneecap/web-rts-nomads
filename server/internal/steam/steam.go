// Package steam is the Go-side surface for the Tauri shell's Steamworks IPC.
// In Phase 1 the only implementation is NoopBridge — calls are no-ops and
// LocalPlayer reports the Steam-unavailable sentinel. Phase 2 adds IPCBridge
// (Unix socket / named pipe to the Rust shell). Game code MUST hold a
// SteamBridge, never reach into a specific implementation.
//
// Design rule (.claude/rules/AI_RULES.md + design D3): no `steamworks::*`
// symbol may appear in any Go file. All Steam SDK interaction lives in the
// Rust shell; this package only speaks newline-delimited JSON to it.
package steam

// LocalPlayer identifies the active Steam user as reported by the shell. The
// zero value (Available=false) is the canonical "no Steam, no signed-in user,
// or shell not present" state — callers branch on Available, never on
// SteamID64 being zero.
type LocalPlayer struct {
	Available   bool
	SteamID64   uint64
	PersonaName string
}

// SteamBridge is the surface the game uses to talk to the Tauri shell's
// Steamworks layer. Implementations:
//
//   - NoopBridge: every method returns the documented "unavailable" sentinel.
//     Used when NOMADS_IPC_PATH is unset (server run bare, or shell with Steam
//     uninitialised). Safe in tests, safe in the air dev loop.
//   - IPCBridge (Phase 2, task 4.4): newline-delimited JSON over the IPC
//     channel to the Rust shell. Adds timeouts, size cap, and terminal-on-close
//     semantics (D24).
//   - FakeBridge (tests): records calls so test code can assert wiring.
//
// RegisterTransport's parameter type is `any` until §10 (pluggable-mp-transport)
// lands and defines the canonical ws.Transport interface. The Phase 1 bridge
// implementations don't introspect the value — they pass it through or, in
// NoopBridge's case, drop it.
type SteamBridge interface {
	LocalPlayer() (LocalPlayer, error)
	ReportAchievement(id string) error
	OpenInviteOverlay(lobbyID string) error
	RegisterTransport(t any) error
}
