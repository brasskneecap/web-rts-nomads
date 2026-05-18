package steam

import "sync"

// FakeBridge is a test double that records every method invocation in order.
// All methods are safe to call concurrently. Use in tests for §16
// (achievements wiring), §17 (UI gates), and elsewhere when asserting that
// game code calls into the bridge correctly. Production code MUST NOT depend
// on FakeBridge.
type FakeBridge struct {
	mu                     sync.Mutex
	LocalPlayerValue       LocalPlayer
	LocalPlayerError       error
	ReportAchievementCalls []string
	OpenInviteOverlayCalls []string
	RegisterTransportCalls []any
	CreateLobbyCalls       []int
	JoinLobbyCalls         []string
	ReportAchievementErr   error
	OpenInviteOverlayErr   error
	RegisterTransportErr   error
	CreateLobbyResult      string // returned by CreateLobby on success
	CreateLobbyErr         error
	JoinLobbyErr           error
}

// NewFakeBridge returns a FakeBridge whose LocalPlayer reports the Steam-
// unavailable sentinel by default. Tests that need a "Steam present" view
// should set LocalPlayerValue before exercising code under test.
func NewFakeBridge() *FakeBridge { return &FakeBridge{} }

func (f *FakeBridge) LocalPlayer() (LocalPlayer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.LocalPlayerValue, f.LocalPlayerError
}

func (f *FakeBridge) ReportAchievement(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ReportAchievementCalls = append(f.ReportAchievementCalls, id)
	return f.ReportAchievementErr
}

func (f *FakeBridge) OpenInviteOverlay(lobbyID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.OpenInviteOverlayCalls = append(f.OpenInviteOverlayCalls, lobbyID)
	return f.OpenInviteOverlayErr
}

func (f *FakeBridge) RegisterTransport(t any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RegisterTransportCalls = append(f.RegisterTransportCalls, t)
	return f.RegisterTransportErr
}

func (f *FakeBridge) CreateLobby(maxPlayers int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.CreateLobbyCalls = append(f.CreateLobbyCalls, maxPlayers)
	return f.CreateLobbyResult, f.CreateLobbyErr
}

func (f *FakeBridge) JoinLobby(lobbyID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.JoinLobbyCalls = append(f.JoinLobbyCalls, lobbyID)
	return f.JoinLobbyErr
}

// Snapshot returns immutable copies of the recorded call lists so test
// assertions don't race with concurrent producers.
func (f *FakeBridge) Snapshot() (achievements, overlays []string, transports []any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	achievements = append([]string(nil), f.ReportAchievementCalls...)
	overlays = append([]string(nil), f.OpenInviteOverlayCalls...)
	transports = append([]any(nil), f.RegisterTransportCalls...)
	return
}
