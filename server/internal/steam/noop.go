package steam

// NoopBridge is the Steam-unavailable bridge. Every method is safe to call
// concurrently from any goroutine; nothing blocks and nothing allocates
// per-call. Used whenever the shell is absent or Steam init failed.
type NoopBridge struct{}

// NewNoopBridge returns a ready-to-use NoopBridge. The zero value also works;
// the constructor exists so main.go has a symmetric call site against the
// future IPCBridge constructor.
func NewNoopBridge() *NoopBridge { return &NoopBridge{} }

// LocalPlayer always returns the unavailable sentinel (Available=false).
// Callers must branch on lp.Available, not on err.
func (*NoopBridge) LocalPlayer() (LocalPlayer, error) {
	return LocalPlayer{}, nil
}

// ReportAchievement silently drops the report. Phase 1 accepts achievement
// loss when Steam is unavailable; documented in §17 task 17.0 and the
// steam-achievements spec.
func (*NoopBridge) ReportAchievement(id string) error { return nil }

// OpenInviteOverlay is a no-op. The UI never invokes this method when
// LocalPlayer().Available is false (the Steam-mode UI is hidden), so this
// path should only be exercised by tests or via a race during sign-out.
func (*NoopBridge) OpenInviteOverlay(lobbyID string) error { return nil }

// RegisterTransport drops the transport. Steam Networking Sockets transport
// registration only happens in Phase 2 via IPCBridge; NoopBridge has nothing
// to do with it.
func (*NoopBridge) RegisterTransport(t any) error { return nil }
