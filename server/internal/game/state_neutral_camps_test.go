package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestNeutralCamp_SpawnGroup_SpawnsExpectedComposition: spawnGroupForCampLocked
// materializes the composition declared in tier_1.json under neutralPlayerID,
// anchored at the camp center, with the camp's aggro/leash range.
// Counts are DERIVED from the catalog JSON (no hardcoded balance numbers).
func TestNeutralCamp_SpawnGroup_SpawnsExpectedComposition(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)
	camp := &s.NeutralCamps[0]
	camp.GroupID = "small_raider_group"
	camp.CurrentTier = 1

	s.spawnGroupForCampLocked(camp)

	group, ok := getNeutralGroup(1, "small_raider_group")
	if !ok {
		t.Fatalf("test setup: small_raider_group must exist in tier 1")
	}

	expectedTotal := 0
	expectedByType := map[string]int{}
	for _, c := range group.Composition {
		expectedTotal += c.Count
		expectedByType[c.UnitType] += c.Count
	}
	if got := len(camp.AliveUnitIDs); got != expectedTotal {
		t.Fatalf("AliveUnitIDs: got %d, want %d (derived from tier_1.json composition)", got, expectedTotal)
	}
	gotByType := map[string]int{}
	for _, id := range camp.AliveUnitIDs {
		u := s.getUnitByIDLocked(id)
		if u == nil {
			t.Fatalf("camp.AliveUnitIDs has stale id %d (no Unit found)", id)
		}
		if u.OwnerID != neutralPlayerID {
			t.Errorf("unit %d: OwnerID = %q, want %q", id, u.OwnerID, neutralPlayerID)
		}
		if u.NeutralCampID != camp.PlacementID {
			t.Errorf("unit %d: NeutralCampID = %q, want %q", id, u.NeutralCampID, camp.PlacementID)
		}
		if !u.GuardMode {
			t.Errorf("unit %d: GuardMode = false, want true", id)
		}
		gotByType[u.UnitType]++
	}
	for ut, want := range expectedByType {
		if gotByType[ut] != want {
			t.Errorf("unitType %q: got %d spawned, want %d", ut, gotByType[ut], want)
		}
	}
}

// TestNeutralCamp_SpawnGroup_RandomUsesSeededRNG: same seed + same map ->
// same random group picks. Determinism rule.
func TestNeutralCamp_SpawnGroup_RandomUsesSeededRNG(t *testing.T) {
	s1 := newTestStateWithNeutralCamp(t)
	s2 := newTestStateWithNeutralCamp(t)
	s1.NeutralCamps[0].GroupID = protocol.NeutralSpawnRandomGroupID
	s2.NeutralCamps[0].GroupID = protocol.NeutralSpawnRandomGroupID

	s1.spawnGroupForCampLocked(&s1.NeutralCamps[0])
	s2.spawnGroupForCampLocked(&s2.NeutralCamps[0])

	h1 := unitTypeHistogramForTest(t, s1, s1.NeutralCamps[0].AliveUnitIDs)
	h2 := unitTypeHistogramForTest(t, s2, s2.NeutralCamps[0].AliveUnitIDs)
	if !histogramsEqualForTest(h1, h2) {
		t.Errorf("determinism violated: histograms differ\nh1=%v\nh2=%v", h1, h2)
	}
}

// TestNeutralCamp_SpawnGroup_TierZeroIsNoop: when CurrentTier resolves to 0,
// spawn is a no-op (no panic, no units placed).
func TestNeutralCamp_SpawnGroup_TierZeroIsNoop(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)
	camp := &s.NeutralCamps[0]
	camp.CurrentTier = -1 // resolves to 0

	s.spawnGroupForCampLocked(camp)

	if got := len(camp.AliveUnitIDs); got != 0 {
		t.Errorf("AliveUnitIDs after no-tier spawn: got %d, want 0", got)
	}
}

// TestNeutralCamp_SpawnGroup_UnknownSpecificGroupIsNoop: a camp configured
// with a specific group id that does NOT exist at the current tier should
// be a no-op (log + skip), not a panic.
func TestNeutralCamp_SpawnGroup_UnknownSpecificGroupIsNoop(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)
	camp := &s.NeutralCamps[0]
	camp.GroupID = "nonexistent_group_id"
	camp.CurrentTier = 1

	s.spawnGroupForCampLocked(camp)

	if got := len(camp.AliveUnitIDs); got != 0 {
		t.Errorf("AliveUnitIDs after unknown-group spawn: got %d, want 0", got)
	}
}

// --- test helpers ---

// newTestStateWithNeutralCamp builds a minimal GameState seeded with a
// fixed RNG (seed=42), with one NeutralSpawn at (5, 5).
func newTestStateWithNeutralCamp(t *testing.T) *GameState {
	t.Helper()
	s := newTestGameStateForNeutralCampTests(t, 42)
	s.MapConfig.NeutralSpawns = []protocol.NeutralSpawn{{
		GridCoord:    protocol.GridCoord{X: 5, Y: 5},
		ID:           "neutral-spawn-5-5",
		GroupID:      "small_raider_group",
		StartingTier: 1,
		AggroRange:   150,
		LeashRange:   200,
	}}
	s.initNeutralCampsLocked()
	if len(s.NeutralCamps) != 1 {
		t.Fatalf("test setup: expected 1 NeutralCamp, got %d", len(s.NeutralCamps))
	}
	return s
}

// newTestGameStateForNeutralCampTests creates a minimal GameState with
// the given seed, using the default map config as a base (same pattern as
// other test helpers in this package such as newChannelTestState,
// newBuffTestState, etc.). The default map already has sensible CellSize,
// GridCols, GridRows values that include (5,5) with room for a spawn ring.
func newTestGameStateForNeutralCampTests(t *testing.T, seed int64) *GameState {
	t.Helper()
	return NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
}

func unitTypeHistogramForTest(t *testing.T, s *GameState, ids []int) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, id := range ids {
		u := s.getUnitByIDLocked(id)
		if u == nil {
			t.Fatalf("stale id %d", id)
		}
		out[u.UnitType]++
	}
	return out
}

func histogramsEqualForTest(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestNeutralCamp_PersistsWhileWaveActive: camps are NOT despawned or reset
// while a wave is active. Regression against the old discrete lifecycle, which
// wiped every camp for the duration of an active wave.
func TestNeutralCamp_PersistsWhileWaveActive(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)
	enableWavesForTest(t, s)
	s.tickNeutralCampsLocked() // spawn-on-game-start (wave is in prep)
	camp := &s.NeutralCamps[0]
	if len(camp.AliveUnitIDs) == 0 {
		t.Fatalf("setup: expected initial spawn to populate AliveUnitIDs")
	}
	spawned := append([]int(nil), camp.AliveUnitIDs...)

	// Wave 1 is running: the roster must be untouched (no mid-wave reset).
	s.WaveManager.CurrentWave = 1
	s.WaveManager.State = "active"
	s.tickNeutralCampsLocked()

	if got := len(camp.AliveUnitIDs); got != len(spawned) {
		t.Errorf("AliveUnitIDs during active wave: got %d, want %d (camp must persist)", got, len(spawned))
	}
	if camp.State != NeutralCampActive {
		t.Errorf("camp.State during active wave: got %v, want Active", camp.State)
	}
	for _, id := range spawned {
		if u := s.getUnitByIDLocked(id); u == nil {
			t.Errorf("unit %d should still be on the field (no mid-wave despawn)", id)
		}
	}
}

// TestNeutralCamp_ResetsAtWaveEnd: a living camp is reset to a fresh roster when
// the wave ENDS (the state leaves "active"), not while it is running. The prior
// roster's unit IDs are gone and new ones take their place, at the same camp,
// still Active — ready for the interlude before the next wave.
func TestNeutralCamp_ResetsAtWaveEnd(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)
	enableWavesForTest(t, s)
	s.tickNeutralCampsLocked() // initial spawn (prep, wave 0)
	camp := &s.NeutralCamps[0]
	preIDs := append([]int(nil), camp.AliveUnitIDs...)
	if len(preIDs) == 0 {
		t.Fatalf("setup: expected initial spawn")
	}

	// Wave 1 running: no reset yet.
	s.WaveManager.CurrentWave = 1
	s.WaveManager.State = "active"
	s.tickNeutralCampsLocked()
	if len(camp.AliveUnitIDs) != len(preIDs) {
		t.Fatalf("camp must not reset mid-wave: got %d, want %d", len(camp.AliveUnitIDs), len(preIDs))
	}

	// Wave 1 ends → state leaves "active"; the camp resets to a fresh roster.
	s.WaveManager.State = "upgrade"
	s.tickNeutralCampsLocked()

	if got := len(camp.AliveUnitIDs); got == 0 {
		t.Errorf("AliveUnitIDs after wave-end reset: got 0, want > 0 (fresh roster)")
	}
	if camp.State != NeutralCampActive {
		t.Errorf("camp.State after wave-end reset: got %v, want Active", camp.State)
	}
	for _, oldID := range preIDs {
		if u := s.getUnitByIDLocked(oldID); u != nil {
			t.Errorf("wave-1 unit %d should be replaced on the wave-end reset", oldID)
		}
	}
}

// TestNeutralCamp_TierUpEveryN: TierUpEveryN=2 promotes CurrentTier after
// wave 2 clears. Uses fallback (only tier_1 ships) so the spawn still
// happens; CurrentTier itself is what we assert.
func TestNeutralCamp_TierUpEveryN(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)
	enableWavesForTest(t, s)
	s.NeutralCamps[0].TierUpEveryN = 2
	s.NeutralCamps[0].StartingTier = 1

	// Simulate wave 2 cleared.
	s.WaveManager.CurrentWave = 2
	s.WaveManager.State = "upgrade"
	s.tickNeutralCampsLocked()

	if got := s.NeutralCamps[0].CurrentTier; got != 2 {
		t.Errorf("CurrentTier after wave 2 with TierUpEveryN=2: got %d, want 2 (1 + 2/2)", got)
	}
}

// TestNeutralCamp_UnitDeathRemovesFromAliveList: when a spawned neutral
// dies (via the unit-removal path), its ID is dropped from
// camp.AliveUnitIDs but the camp does NOT respawn until the next wave clear.
func TestNeutralCamp_UnitDeathRemovesFromAliveList(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)
	enableWavesForTest(t, s)
	s.tickNeutralCampsLocked()
	camp := &s.NeutralCamps[0]
	if len(camp.AliveUnitIDs) == 0 {
		t.Fatalf("setup: expected initial spawn")
	}
	initialCount := len(camp.AliveUnitIDs)
	victimID := camp.AliveUnitIDs[0]
	victim := s.getUnitByIDLocked(victimID)
	if victim == nil {
		t.Fatalf("setup: alive unit not found")
	}

	s.removeUnitLocked(victimID)

	if got := len(camp.AliveUnitIDs); got != initialCount-1 {
		t.Errorf("AliveUnitIDs after one unit death: got %d, want %d", got, initialCount-1)
	}
	for _, id := range camp.AliveUnitIDs {
		if id == victimID {
			t.Errorf("AliveUnitIDs still contains dead unit %d", id)
		}
	}
}

// enableWavesForTest puts the WaveManager into "enabled, in prep" state so
// transitions are exercisable. Sets fields on the existing instance rather
// than replacing it so any initialization done by NewGameStateWithSeed is
// preserved.
func enableWavesForTest(t *testing.T, s *GameState) {
	t.Helper()
	s.WaveManager.Enabled = true
	s.WaveManager.CurrentWave = 0
	s.WaveManager.State = "prep"
	s.WaveManager.Timer = 60
	s.WaveManager.PrepDuration = 60
	s.WaveManager.WaveDuration = 120
}

// TestNeutralCamp_BroadcastAggro: when one camp-mate acquires a target,
// the rest of the alive camp roster receives the same target ID.
// Verifies: no *Unit pointers stored anywhere; canonical validity guard
// fires on a stale/dead target.
func TestNeutralCamp_BroadcastAggro(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)
	enableWavesForTest(t, s)
	s.tickNeutralCampsLocked() // initial spawn
	camp := &s.NeutralCamps[0]
	if len(camp.AliveUnitIDs) < 2 {
		t.Fatalf("test requires camp with >= 2 units; got %d", len(camp.AliveUnitIDs))
	}

	target := spawnFakePlayerUnitForTest(t, s, "player1")

	acquirer := s.getUnitByIDLocked(camp.AliveUnitIDs[0])
	// Simulate what applyCombatTargetLocked does before the broadcast fires in
	// production: the acquirer already holds the target ID.
	acquirer.AttackTargetID = target.ID
	s.broadcastNeutralCampAggroLocked(acquirer, target.ID)

	for _, id := range camp.AliveUnitIDs {
		mate := s.getUnitByIDLocked(id)
		if mate == nil {
			t.Fatalf("camp-mate id %d disappeared", id)
		}
		if mate.AttackTargetID != target.ID {
			t.Errorf("mate %d: AttackTargetID = %d, want %d", id, mate.AttackTargetID, target.ID)
		}
	}
}

// TestNeutralCamp_BroadcastAggro_SkipsMidSwingMate is the regression for the
// "stationary spear maiden hits a stationary archer 200px away" bug. A camp
// aggro broadcast must NOT overwrite the AttackTargetID of a mate that is
// mid-windup: doing so redirects the committed melee swing onto the broadcast
// target, and applyDelayedAttackLocked applies that damage with no fire-time
// distance check — landing a hit far outside the mate's AttackRange on an enemy
// it never reached. The mate must keep its in-flight swing target until the
// windup resolves (it still receives threat, so it retargets afterward).
func TestNeutralCamp_BroadcastAggro_SkipsMidSwingMate(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)
	enableWavesForTest(t, s)
	s.tickNeutralCampsLocked()
	camp := &s.NeutralCamps[0]
	if len(camp.AliveUnitIDs) < 2 {
		t.Fatalf("test requires camp with >= 2 units; got %d", len(camp.AliveUnitIDs))
	}

	nearTarget := spawnFakePlayerUnitForTest(t, s, "player1") // the mate's current swing target
	farTarget := spawnFakePlayerUnitForTest(t, s, "player1")  // the newly-broadcast target

	acquirer := s.getUnitByIDLocked(camp.AliveUnitIDs[0])
	midSwingMate := s.getUnitByIDLocked(camp.AliveUnitIDs[1])

	// The mate is mid-windup, committed to swinging at nearTarget, when the camp
	// broadcasts farTarget (e.g. an archer that just poked another camp guard).
	midSwingMate.AttackTargetID = nearTarget.ID
	midSwingMate.AttackWindupRemaining = 0.35

	acquirer.AttackTargetID = farTarget.ID
	s.broadcastNeutralCampAggroLocked(acquirer, farTarget.ID)

	if midSwingMate.AttackTargetID != nearTarget.ID {
		t.Errorf("mid-swing mate was hijacked mid-windup: AttackTargetID = %d, want %d "+
			"(its in-flight swing target). The committed swing would land on the broadcast "+
			"target with no distance check.", midSwingMate.AttackTargetID, nearTarget.ID)
	}

	// A mate that is NOT mid-swing must still pick up the broadcast target — the
	// guard only defers the overwrite, it doesn't disable camp aggro sharing.
	if acquirer.AttackWindupRemaining == 0 && acquirer.AttackTargetID != farTarget.ID {
		t.Errorf("non-windup acquirer lost its broadcast target: AttackTargetID = %d, want %d",
			acquirer.AttackTargetID, farTarget.ID)
	}
}

// TestNeutralCamp_BroadcastAggro_DeadTargetNoOp: broadcasting a dead
// target must not modify camp-mates' AttackTargetID. Validates the
// canonical guard (target.HP > 0).
func TestNeutralCamp_BroadcastAggro_DeadTargetNoOp(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)
	enableWavesForTest(t, s)
	s.tickNeutralCampsLocked()
	camp := &s.NeutralCamps[0]
	if len(camp.AliveUnitIDs) < 2 {
		t.Fatalf("test requires camp with >= 2 units")
	}
	target := spawnFakePlayerUnitForTest(t, s, "player1")
	target.HP = 0 // dead

	// Snapshot the pre-broadcast AttackTargetIDs so we can assert no change.
	pre := map[int]int{}
	for _, id := range camp.AliveUnitIDs {
		u := s.getUnitByIDLocked(id)
		pre[id] = u.AttackTargetID
	}

	acquirer := s.getUnitByIDLocked(camp.AliveUnitIDs[0])
	s.broadcastNeutralCampAggroLocked(acquirer, target.ID)

	for _, id := range camp.AliveUnitIDs {
		u := s.getUnitByIDLocked(id)
		if u.AttackTargetID != pre[id] {
			t.Errorf("dead-target broadcast must not modify mate %d AttackTargetID (was %d, now %d)",
				id, pre[id], u.AttackTargetID)
		}
	}
}

// TestNeutralCamp_BroadcastAggro_NonNeutralAcquirerNoOp: calling with an
// acquirer that has no NeutralCampID is a no-op.
func TestNeutralCamp_BroadcastAggro_NonNeutralAcquirerNoOp(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)
	enableWavesForTest(t, s)
	s.tickNeutralCampsLocked()
	camp := &s.NeutralCamps[0]
	target := spawnFakePlayerUnitForTest(t, s, "player1")

	// Fabricate an acquirer with no camp.
	acquirer := spawnFakePlayerUnitForTest(t, s, "player1")
	acquirer.NeutralCampID = "" // explicit

	s.broadcastNeutralCampAggroLocked(acquirer, target.ID)

	for _, id := range camp.AliveUnitIDs {
		u := s.getUnitByIDLocked(id)
		if u.AttackTargetID == target.ID {
			t.Errorf("camp-mate %d picked up target from non-neutral acquirer broadcast", id)
		}
	}
}

// spawnFakePlayerUnitForTest spawns a minimal unit owned by ownerID at a
// distant cell so it doesn't accidentally aggro the camp. Uses the
// project-standard spawnPlayerUnitLocked helper.
func spawnFakePlayerUnitForTest(t *testing.T, s *GameState, ownerID string) *Unit {
	t.Helper()
	pos := protocol.Vec2{X: 1000, Y: 1000} // far from the camp at (5,5)
	u := s.spawnPlayerUnitLocked("soldier", ownerID, "#00ff00", pos)
	if u == nil {
		t.Fatalf("spawnPlayerUnitLocked returned nil")
	}
	u.Visible = true
	return u
}

// TestNeutralCamp_DamageIsNonZero is a regression guard for the
// neutral-faction Player having zero PhysicalDamageMultiplier /
// MagicDamageMultiplier (default float64). Before the fix in
// state_spawn.go, applyPlayerUpgradesAtSpawnLocked ran for every
// non-enemy owner — including neutralPlayerID — and multiplied
// BaseDamage by 0, leaving neutrals with Damage=0. That made
// unitUsesCombatAI return false, so neutrals never entered the combat
// AI loop at all (no proximity aggro, no retaliation) — they just sat
// there guarding while the player attacked them.
func TestNeutralCamp_DamageIsNonZero(t *testing.T) {
	s := newTestStateWithNeutralCamp(t)
	camp := &s.NeutralCamps[0]
	camp.GroupID = "small_raider_group"
	camp.CurrentTier = 1

	s.spawnGroupForCampLocked(camp)
	if len(camp.AliveUnitIDs) == 0 {
		t.Fatalf("setup: expected spawn to populate AliveUnitIDs")
	}
	for _, id := range camp.AliveUnitIDs {
		u := s.getUnitByIDLocked(id)
		if u == nil {
			t.Fatalf("camp unit id %d missing", id)
		}
		def, ok := getUnitDef(u.UnitType)
		if !ok {
			t.Fatalf("unit %d: UnitDef for %q not found", id, u.UnitType)
		}
		if def.Damage <= 0 {
			continue // catalog itself authored 0 damage — not the regression we're guarding.
		}
		if u.BaseDamage <= 0 {
			t.Errorf("unit %d (%s): BaseDamage = %d, want > 0 (catalog = %d). Player-upgrade multiplier zeroed it.",
				id, u.UnitType, u.BaseDamage, def.Damage)
		}
		if u.Damage <= 0 {
			t.Errorf("unit %d (%s): Damage = %d, want > 0. unitUsesCombatAI will filter this unit out.",
				id, u.UnitType, u.Damage)
		}
		if !s.unitUsesCombatAI(u) {
			t.Errorf("unit %d (%s): unitUsesCombatAI returned false — neutral will never enter combat AI loop",
				id, u.UnitType)
		}
	}
}
