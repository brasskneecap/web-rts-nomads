package steam

import "testing"

// TestNoopBridge_AllMethodsSafe asserts that every NoopBridge method returns
// without error and reports the documented "unavailable" sentinel. The Phase 1
// server runs against this bridge whenever NOMADS_IPC_PATH is unset, so a
// panic or error here would break the air dev loop.
func TestNoopBridge_AllMethodsSafe(t *testing.T) {
	b := NewNoopBridge()

	lp, err := b.LocalPlayer()
	if err != nil {
		t.Errorf("LocalPlayer err = %v, want nil", err)
	}
	if lp.Available {
		t.Errorf("LocalPlayer Available = true, want false (NoopBridge means Steam unavailable)")
	}
	if lp.SteamID64 != 0 || lp.PersonaName != "" {
		t.Errorf("LocalPlayer zero-value violated: %+v", lp)
	}

	if err := b.ReportAchievement(AchievementFirstWaveCleared); err != nil {
		t.Errorf("ReportAchievement err = %v, want nil", err)
	}
	if err := b.OpenInviteOverlay("lobby-abc"); err != nil {
		t.Errorf("OpenInviteOverlay err = %v, want nil", err)
	}
	if err := b.RegisterTransport(struct{}{}); err != nil {
		t.Errorf("RegisterTransport err = %v, want nil", err)
	}
}

// TestFakeBridge_RecordsCallsInOrder asserts the FakeBridge test double
// captures every method invocation so other sections can assert wiring.
func TestFakeBridge_RecordsCallsInOrder(t *testing.T) {
	f := NewFakeBridge()
	f.LocalPlayerValue = LocalPlayer{Available: true, SteamID64: 76561197960287930, PersonaName: "gabe"}

	lp, _ := f.LocalPlayer()
	if !lp.Available || lp.SteamID64 != 76561197960287930 {
		t.Errorf("LocalPlayer round-trip: got %+v", lp)
	}

	_ = f.ReportAchievement("ACH_A")
	_ = f.ReportAchievement("ACH_B")
	_ = f.OpenInviteOverlay("lobby-1")
	_ = f.RegisterTransport("transport-handle")

	ach, ovl, trs := f.Snapshot()
	if len(ach) != 2 || ach[0] != "ACH_A" || ach[1] != "ACH_B" {
		t.Errorf("ReportAchievement calls: got %v", ach)
	}
	if len(ovl) != 1 || ovl[0] != "lobby-1" {
		t.Errorf("OpenInviteOverlay calls: got %v", ovl)
	}
	if len(trs) != 1 || trs[0] != "transport-handle" {
		t.Errorf("RegisterTransport calls: got %v", trs)
	}
}
