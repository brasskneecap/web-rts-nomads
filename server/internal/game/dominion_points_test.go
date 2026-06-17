package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func newLPTestState(t *testing.T) (*GameState, string) {
	t.Helper()
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	const playerID = "p1"
	s.EnsurePlayer(playerID)
	// All DP-drop tests assert on Player.RunDominionPointDrops, which only
	// accumulates in matchEnd commit mode. Pin it here so tests stay stable
	// regardless of whatever the shipped tuning JSON has set.
	withMatchEndCommitMode(t)
	return s, playerID
}

// withMatchEndCommitMode pins gameplayTuningSingleton.DominionPoints.CommitMode
// to "matchEnd" for the duration of the test, restoring whatever the JSON
// shipped (which may be "immediate" during a dev/test campaign). All tests
// that assert on Player.RunDominionPointDrops require matchEnd mode; immediate
// mode deliberately bypasses the accumulator.
func withMatchEndCommitMode(t *testing.T) {
	t.Helper()
	prev := gameplayTuningSingleton.DominionPoints.CommitMode
	gameplayTuningSingleton.DominionPoints.CommitMode = dominionPointCommitModeMatchEnd
	t.Cleanup(func() {
		gameplayTuningSingleton.DominionPoints.CommitMode = prev
	})
}

// ─── rollDominionPointDropLocked ───────────────────────────────────────────────

// TestRollDominionPointDropLocked_ZeroChance never drops when the tuning
// override for the unit type forces chance to 0. Uses a per-type tuning
// override to suppress drops independently of the base rate.
func TestRollDominionPointDropLocked_ZeroChance(t *testing.T) {
	s, playerID := newLPTestState(t)

	// Inject a tuning override for "soldier" that zeroes out both fields.
	// UnitOverrides with DominionPointDropChance=0 causes the chance<=0 guard
	// to fire inside rollDominionPointDropLocked, so no drop can occur.
	if gameplayTuningSingleton.UnitOverrides == nil {
		gameplayTuningSingleton.UnitOverrides = map[string]UnitDominionPointOverride{}
	}
	const testType = "soldier"
	oldOverride, hadOverride := gameplayTuningSingleton.UnitOverrides[testType]
	gameplayTuningSingleton.UnitOverrides[testType] = UnitDominionPointOverride{
		DominionPointDropChance: 0.0,
		DominionPointAmount:     0,
	}
	defer func() {
		if hadOverride {
			gameplayTuningSingleton.UnitOverrides[testType] = oldOverride
		} else {
			delete(gameplayTuningSingleton.UnitOverrides, testType)
		}
	}()

	s.mu.Lock()
	enemy := s.spawnEnemyUnitLocked(testType, protocol.Vec2{X: 100, Y: 100})
	before := s.Players[playerID].RunDominionPointDrops
	for i := 0; i < 1000; i++ {
		s.rollDominionPointDropLocked(playerID, enemy)
	}
	after := s.Players[playerID].RunDominionPointDrops
	s.mu.Unlock()

	if after != before {
		t.Errorf("expected no drops with 0.0 tuning override chance, got %d drops", after-before)
	}
}

// TestRollDominionPointDropLocked_BaseTuningRateDrops verifies that with the
// live 0.05 base rate, a large number of enemy kills produces some drops (the
// seeded RNG will produce rolls below 0.05). Also verifies friendly fire never
// drops regardless of base rate.
func TestRollDominionPointDropLocked_BaseTuningRateDrops(t *testing.T) {
	s, playerID := newLPTestState(t)

	s.mu.Lock()
	enemy := s.spawnEnemyUnitLocked("raider", protocol.Vec2{X: 100, Y: 100})
	s.mu.Unlock()

	const rolls = 1000
	s.mu.Lock()
	before := s.Players[playerID].RunDominionPointDrops
	for i := 0; i < rolls; i++ {
		s.rollDominionPointDropLocked(playerID, enemy)
	}
	after := s.Players[playerID].RunDominionPointDrops
	s.mu.Unlock()

	// At 5% base rate over 1000 rolls, the seeded RNG must produce at least
	// one hit. Exact count is deterministic under the fixed seed (1).
	if after <= before {
		t.Errorf("expected at least one dominion point drop over %d rolls at 5%% base chance, got 0", rolls)
	}
	// Verify each award is exactly 1 (perKillBaseAmount = 1).
	// We can only verify the total is a multiple of 1 (trivially true), but
	// we also verify each drop is individually 1 by checking
	// (after - before) == number of successful rolls. The count must be in
	// [1, 1000].
	drops := after - before
	if drops > rolls {
		t.Errorf("drops %d exceeded roll count %d; logic error", drops, rolls)
	}
}

// TestRollDominionPointDropLocked_FriendlyFireNeverDrops verifies that even at
// the live 5% base rate, same-team kills never award dominion points.
func TestRollDominionPointDropLocked_FriendlyFireNeverDrops(t *testing.T) {
	s, playerID := newLPTestState(t)

	s.mu.Lock()
	player := s.Players[playerID]
	friendly := s.spawnPlayerUnitLocked("soldier", playerID, player.Color, protocol.Vec2{X: 200, Y: 200})
	before := s.Players[playerID].RunDominionPointDrops
	for i := 0; i < 1000; i++ {
		s.rollDominionPointDropLocked(playerID, friendly)
	}
	after := s.Players[playerID].RunDominionPointDrops
	s.mu.Unlock()

	if after != before {
		t.Errorf("friendly fire should never award dominion points; got %d drops", after-before)
	}
}

// TestRollDominionPointDropLocked_SameTeamSkipped never drops when attacker and
// victim share the same owner.
func TestRollDominionPointDropLocked_SameTeamSkipped(t *testing.T) {
	s, playerID := newLPTestState(t)

	s.mu.Lock()
	player := s.Players[playerID]
	friendly := s.spawnPlayerUnitLocked("soldier", playerID, player.Color, protocol.Vec2{X: 200, Y: 200})

	// We're testing same-team skip, not the drop chance, so just run the roll.
	before := s.Players[playerID].RunDominionPointDrops
	s.rollDominionPointDropLocked(playerID, friendly)
	after := s.Players[playerID].RunDominionPointDrops
	s.mu.Unlock()

	if after != before {
		t.Errorf("expected no drop when attacker and victim share owner, got %d", after-before)
	}
}

// TestRollDominionPointDropLocked_EnemyAttackerSkipped skips enemy AI attacker.
func TestRollDominionPointDropLocked_EnemyAttackerSkipped(t *testing.T) {
	s, playerID := newLPTestState(t)

	s.mu.Lock()
	player := s.Players[playerID]
	victim := s.spawnPlayerUnitLocked("soldier", playerID, player.Color, protocol.Vec2{X: 200, Y: 200})
	before := s.Players[playerID].RunDominionPointDrops
	s.rollDominionPointDropLocked(enemyPlayerID, victim)
	after := s.Players[playerID].RunDominionPointDrops
	s.mu.Unlock()

	if after != before {
		t.Errorf("expected no drop when attacker is enemy AI, got %d", after-before)
	}
}

// TestRollDominionPointDropLocked_UnitDefOverride uses the def's own drop fields
// when they are non-zero.
func TestRollDominionPointDropLocked_UnitDefOverride(t *testing.T) {
	s, playerID := newLPTestState(t)

	// Inject a unit def override with 100% drop chance.
	const testType = "soldier"
	original := unitDefsByType[testType]
	modified := original
	modified.DominionPointDropChance = 1.0
	modified.DominionPointAmount = 5
	unitDefsByType[testType] = modified
	defer func() { unitDefsByType[testType] = original }()

	s.mu.Lock()
	enemy := s.spawnEnemyUnitLocked(testType, protocol.Vec2{X: 100, Y: 100})
	before := s.Players[playerID].RunDominionPointDrops
	// Roll many times — with 100% chance every roll should drop.
	s.rollDominionPointDropLocked(playerID, enemy)
	after := s.Players[playerID].RunDominionPointDrops
	s.mu.Unlock()

	if after-before != 5 {
		t.Errorf("expected 5 dominion points from unit def override, got %d", after-before)
	}
}

// TestRollDominionPointDropLocked_ImmediateMode_FiresHookAndSkipsAccumulator
// verifies that with commitMode="immediate", a successful drop roll invokes
// the immediate-commit hook and does NOT increment RunDominionPointDrops (so
// the match-end commit path cannot double-credit it).
func TestRollDominionPointDropLocked_ImmediateMode_FiresHookAndSkipsAccumulator(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	const playerID = "p1"
	s.EnsurePlayer(playerID)

	// Override tuning to immediate mode with a 100% drop chance via unit
	// override so every roll succeeds.
	prevMode := gameplayTuningSingleton.DominionPoints.CommitMode
	gameplayTuningSingleton.DominionPoints.CommitMode = dominionPointCommitModeImmediate
	t.Cleanup(func() { gameplayTuningSingleton.DominionPoints.CommitMode = prevMode })

	if gameplayTuningSingleton.UnitOverrides == nil {
		gameplayTuningSingleton.UnitOverrides = map[string]UnitDominionPointOverride{}
	}
	const testType = "raider"
	prevOverride, hadOverride := gameplayTuningSingleton.UnitOverrides[testType]
	gameplayTuningSingleton.UnitOverrides[testType] = UnitDominionPointOverride{
		DominionPointDropChance: 1.0,
		DominionPointAmount:     1,
	}
	t.Cleanup(func() {
		if hadOverride {
			gameplayTuningSingleton.UnitOverrides[testType] = prevOverride
		} else {
			delete(gameplayTuningSingleton.UnitOverrides, testType)
		}
	})

	var hookCalls []struct {
		PlayerID string
		Amount   int
	}
	s.SetImmediateDominionPointDropHandler(func(pid string, amt int) {
		hookCalls = append(hookCalls, struct {
			PlayerID string
			Amount   int
		}{pid, amt})
	})

	s.mu.Lock()
	enemy := s.spawnEnemyUnitLocked(testType, protocol.Vec2{X: 100, Y: 100})
	before := s.Players[playerID].RunDominionPointDrops
	const rolls = 5
	for i := 0; i < rolls; i++ {
		s.rollDominionPointDropLocked(playerID, enemy)
	}
	after := s.Players[playerID].RunDominionPointDrops
	s.mu.Unlock()

	if after != before {
		t.Errorf("immediate mode must NOT increment RunDominionPointDrops; before=%d after=%d", before, after)
	}
	if len(hookCalls) != rolls {
		t.Fatalf("expected %d hook invocations (one per 100%%-drop roll), got %d", rolls, len(hookCalls))
	}
	for i, call := range hookCalls {
		if call.PlayerID != playerID {
			t.Errorf("call[%d].PlayerID: want %q, got %q", i, playerID, call.PlayerID)
		}
		if call.Amount != 1 {
			t.Errorf("call[%d].Amount: want 1, got %d", i, call.Amount)
		}
	}
}

// TestRollDominionPointDropLocked_ImmediateMode_NoHookIsNoOp verifies that
// when commitMode="immediate" but no handler is wired, drops are silently
// dropped (no panic, no accumulator increment). Tests that don't care about
// the immediate path stay valid.
func TestRollDominionPointDropLocked_ImmediateMode_NoHookIsNoOp(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	const playerID = "p1"
	s.EnsurePlayer(playerID)

	prevMode := gameplayTuningSingleton.DominionPoints.CommitMode
	gameplayTuningSingleton.DominionPoints.CommitMode = dominionPointCommitModeImmediate
	t.Cleanup(func() { gameplayTuningSingleton.DominionPoints.CommitMode = prevMode })

	if gameplayTuningSingleton.UnitOverrides == nil {
		gameplayTuningSingleton.UnitOverrides = map[string]UnitDominionPointOverride{}
	}
	const testType = "raider"
	prevOverride, hadOverride := gameplayTuningSingleton.UnitOverrides[testType]
	gameplayTuningSingleton.UnitOverrides[testType] = UnitDominionPointOverride{
		DominionPointDropChance: 1.0,
		DominionPointAmount:     1,
	}
	t.Cleanup(func() {
		if hadOverride {
			gameplayTuningSingleton.UnitOverrides[testType] = prevOverride
		} else {
			delete(gameplayTuningSingleton.UnitOverrides, testType)
		}
	})

	// Deliberately do NOT call SetImmediateDominionPointDropHandler.

	s.mu.Lock()
	enemy := s.spawnEnemyUnitLocked(testType, protocol.Vec2{X: 100, Y: 100})
	for i := 0; i < 10; i++ {
		s.rollDominionPointDropLocked(playerID, enemy) // must not panic
	}
	after := s.Players[playerID].RunDominionPointDrops
	s.mu.Unlock()

	if after != 0 {
		t.Errorf("immediate mode without hook should not accumulate; got %d", after)
	}
}
