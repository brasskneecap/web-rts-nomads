package game

import "testing"

// withStartWaveBonusToggle temporarily overrides the player.json-derived
// StartWaveBonus flag for the duration of a test, restoring it afterward. The
// config is a package singleton loaded once from embedded JSON; flipping the
// singleton (via the shared SetStartWaveBonusForTest seam) is the only way to
// exercise the "enabled" path without editing the catalog file.
func withStartWaveBonusToggle(t *testing.T, enabled bool) {
	t.Helper()
	t.Cleanup(SetStartWaveBonusForTest(enabled))
}

func newStartBonusTestState(t *testing.T) *GameState {
	t.Helper()
	return NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
}

// TestStartWaveBonus_ToggleOn_OffersPickAtStart: with the toggle on, a joining
// player on a wave-enabled map at wave 0 is put into the upgrade phase with a
// fresh, unresolved set of offers.
func TestStartWaveBonus_ToggleOn_OffersPickAtStart(t *testing.T) {
	withStartWaveBonusToggle(t, true)
	s := newStartBonusTestState(t)
	enableWavesForTest(t, s)

	p := continuousTestPlayer("p1")
	s.Players["p1"] = p
	s.maybeGrantStartWaveBonusLocked(p)

	if s.WaveManager.State != "upgrade" {
		t.Fatalf("toggle on: expected wave state \"upgrade\", got %q", s.WaveManager.State)
	}
	if len(p.UpgradeState.CurrentOffers) == 0 {
		t.Fatal("toggle on: expected the player to be given upgrade offers")
	}
	if p.UpgradeState.Resolved {
		t.Fatal("toggle on: offer should start unresolved")
	}
}

// TestStartWaveBonus_ToggleOff_NoPick: with the toggle off, joining leaves the
// manager in prep with no offers.
func TestStartWaveBonus_ToggleOff_NoPick(t *testing.T) {
	withStartWaveBonusToggle(t, false)
	s := newStartBonusTestState(t)
	enableWavesForTest(t, s)

	p := continuousTestPlayer("p1")
	s.Players["p1"] = p
	s.maybeGrantStartWaveBonusLocked(p)

	if s.WaveManager.State != "prep" {
		t.Fatalf("toggle off: expected wave state to stay \"prep\", got %q", s.WaveManager.State)
	}
	if len(p.UpgradeState.CurrentOffers) != 0 {
		t.Fatalf("toggle off: expected no offers, got %d", len(p.UpgradeState.CurrentOffers))
	}
}

// TestStartWaveBonus_NonWaveMap_NoPick: the bonus is scoped to wave-enabled maps
// (the upgrade phase is only ticked when WaveManager.Enabled). Even with the
// toggle on, a non-wave map grants nothing.
func TestStartWaveBonus_NonWaveMap_NoPick(t *testing.T) {
	withStartWaveBonusToggle(t, true)
	s := newStartBonusTestState(t)
	s.WaveManager = WaveManager{} // Enabled == false (legacy always-on map)

	p := continuousTestPlayer("p1")
	s.Players["p1"] = p
	s.maybeGrantStartWaveBonusLocked(p)

	if s.WaveManager.State == "upgrade" || len(p.UpgradeState.CurrentOffers) != 0 {
		t.Fatal("non-wave map: expected no start bonus")
	}
}

// TestStartWaveBonus_LateJoinAfterWave1_NoPick: a player joining after wave 1 has
// begun (CurrentWave > 0) gets no start bonus, and an in-progress wave is not
// yanked back into the upgrade phase.
func TestStartWaveBonus_LateJoinAfterWave1_NoPick(t *testing.T) {
	withStartWaveBonusToggle(t, true)
	s := newStartBonusTestState(t)
	enableWavesForTest(t, s)
	s.WaveManager.CurrentWave = 1
	s.WaveManager.State = "active"

	p := continuousTestPlayer("late")
	s.Players["late"] = p
	s.maybeGrantStartWaveBonusLocked(p)

	if s.WaveManager.State != "active" {
		t.Fatalf("late join: wave state should stay \"active\", got %q", s.WaveManager.State)
	}
	if len(p.UpgradeState.CurrentOffers) != 0 {
		t.Fatal("late join: expected no offers")
	}
}

// TestStartWaveBonus_SnapshotDisplaysWaveOne: the start-of-match offer sits at
// CurrentWave 0 but the snapshot must present it as wave 1 for a sensible modal
// header.
func TestStartWaveBonus_SnapshotDisplaysWaveOne(t *testing.T) {
	withStartWaveBonusToggle(t, true)
	s := newStartBonusTestState(t)
	enableWavesForTest(t, s)

	p := continuousTestPlayer("p1")
	s.Players["p1"] = p
	s.maybeGrantStartWaveBonusLocked(p)

	snap := s.buildWaveUpgradeSnapshotLocked("p1")
	if snap == nil {
		t.Fatal("expected a wave-upgrade snapshot for the offered player")
	}
	if snap.Wave != 1 {
		t.Fatalf("start bonus should display as wave 1, got %d", snap.Wave)
	}
}

// TestStartWaveBonus_ResolutionHandsOffToPrep: after the start-bonus pick
// resolves, a non-continuous wave map returns to the prep countdown for wave 1
// (CurrentWave still 0 → prep will advance it to wave 1 normally).
func TestStartWaveBonus_ResolutionHandsOffToPrep(t *testing.T) {
	withStartWaveBonusToggle(t, true)
	s := newStartBonusTestState(t)
	enableWavesForTest(t, s)

	p := continuousTestPlayer("p1")
	s.Players["p1"] = p
	s.maybeGrantStartWaveBonusLocked(p)

	// Simulate the player having picked.
	p.UpgradeState.Resolved = true
	s.tickUpgradePhaseLocked()

	if s.WaveManager.State != "prep" {
		t.Fatalf("after start-bonus resolution expected \"prep\", got %q", s.WaveManager.State)
	}
	if s.WaveManager.CurrentWave != 0 {
		t.Fatalf("prep hand-off should keep CurrentWave 0 (prep advances to 1), got %d", s.WaveManager.CurrentWave)
	}
}

// TestStartWaveBonus_ContinuousResolutionHandsOffToPrep: on a CONTINUOUS-wave
// map (e.g. forest-1), resolving the START bonus (CurrentWave 0) must also go
// to the prep countdown for wave 1 — it must NOT jump straight into wave 1
// active. The continuous advance-immediately path is only for genuine
// between-wave upgrades (CurrentWave >= 1); using it at match start skipped the
// initial prep entirely ("wave 1 starts right away").
func TestStartWaveBonus_ContinuousResolutionHandsOffToPrep(t *testing.T) {
	withStartWaveBonusToggle(t, true)
	s := newStartBonusTestState(t)
	enableWavesForTest(t, s)
	s.WaveManager.Continuous = true

	p := continuousTestPlayer("p1")
	s.Players["p1"] = p
	s.maybeGrantStartWaveBonusLocked(p)

	// Simulate the player having picked the start bonus.
	p.UpgradeState.Resolved = true
	s.tickUpgradePhaseLocked()

	if s.WaveManager.State != "prep" {
		t.Fatalf("continuous start-bonus resolution must hand off to prep, got %q", s.WaveManager.State)
	}
	if s.WaveManager.CurrentWave != 0 {
		t.Fatalf("start-bonus resolution must NOT advance the wave; got CurrentWave=%d", s.WaveManager.CurrentWave)
	}
	if s.WaveManager.Timer != s.WaveManager.PrepDuration {
		t.Errorf("prep timer should reset to PrepDuration %v, got %v", s.WaveManager.PrepDuration, s.WaveManager.Timer)
	}
}
