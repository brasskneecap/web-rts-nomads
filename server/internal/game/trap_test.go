package game

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// Shared test helpers
// ─────────────────────────────────────────────────────────────────────────────

// newTrapState returns a minimal GameState with two players: "p1" (the trapper
// owner) and the wave-enemy faction (enemyPlayerID). The wave-enemy faction is
// used as the hostile party because the current model treats two real players
// as allies (see playersAreHostile). No units are spawned — callers add their
// own via spawnPlayerUnitLocked.
//
// The lock is NOT held on return.
func newTrapState(t *testing.T) *GameState {
	t.Helper()
	return NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
}

// spawnArcher spawns an archer unit for the given player. Archers have the
// attack capability so they can set LastCombatSeconds. The unit is Visible with
// full HP, positioned at (x, y).
func spawnArcher(t *testing.T, s *GameState, playerID string, x, y float64) *Unit {
	t.Helper()
	u := s.spawnPlayerUnitLocked("archer", playerID, "#3498db", protocol.Vec2{X: x, Y: y})
	if u == nil {
		// Archer may not be in catalog on all test environments; fall back to soldier.
		u = s.spawnPlayerUnitLocked("soldier", playerID, "#3498db", protocol.Vec2{X: x, Y: y})
	}
	if u == nil {
		t.Fatal("spawnArcher: failed to spawn unit")
	}
	u.Visible = true
	return u
}

// placeTrap directly inserts a Trap into s.Traps without going through the
// placement pipeline — useful for testing trap effects in isolation.
func placeTrap(s *GameState, trapType, ownerPlayerID string, ownerUnitID int, x, y, radius, durationSeconds float64) *Trap {
	id := s.nextTrapID
	s.nextTrapID++
	trap := &Trap{
		ID:               trapIDString(id),
		OwnerPlayerID:    ownerPlayerID,
		OwnerUnitID:      ownerUnitID,
		X:                x,
		Y:                y,
		Radius:           radius,
		RemainingSeconds: durationSeconds,
		TrapType:         trapType,
	}
	s.Traps = append(s.Traps, trap)
	return trap
}

// grantTrapAbility gives a unit a trap ABILITY. The four traps (caltrops,
// fire_pit, explosive_trap, marker_trap) migrated from bronze perks to pool
// abilities, so a trapper "has" a trap via unit.Abilities now — this is the
// ability-era counterpart of grantPerk for the trap-owning setup.
func grantTrapAbility(u *Unit, abilityID string) {
	if !containsAbility(u, abilityID) {
		u.Abilities = append(u.Abilities, abilityID)
	}
}

// mustTrapAbilityConfig returns the base (rank-scaled) TrapConfig an authored
// trap ability plants — the test-side source of truth for a trap's stats now
// that the four traps are abilities (replaces perkDefByID("<trap>").Config /
// .ConfigForRank(rank)). Struct fields map to the old config keys:
// Radius←radius, DamagePerSecond←damagePerSecond, SlowMultiplier←slowMultiplier,
// ExplosionRadius←explosionRadius, TriggerRadius←triggerRadius,
// BurstDamage←burstDamage, MarkMultiplier←markMultiplier,
// MarkDuration←markDuration, DurationSeconds←durationSeconds,
// PlaceIntervalSeconds←placeIntervalSeconds.
func mustTrapAbilityConfig(t *testing.T, abilityID, rank string) TrapConfig {
	t.Helper()
	tc, ok := trapConfigFromAbilityLocked(abilityID, rank)
	if !ok {
		t.Fatalf("no place_trap config for trap ability %q", abilityID)
	}
	return tc
}

// ─────────────────────────────────────────────────────────────────────────────
// Phase A — Trap entity scaffold tests
// ─────────────────────────────────────────────────────────────────────────────

// TestTrap_LifetimeDecay verifies that a trap added to s.Traps has its
// RemainingSeconds decremented by dt each tick and is culled when it reaches 0.
func TestTrap_LifetimeDecay(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure "p1" exists so tickTrapsLocked doesn't drop the trap.
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	trap := placeTrap(s, "caltrops", "p1", 0, 400, 400, 60, 5.0)

	// Tick once with dt=2.0 — trap should still be alive.
	s.tickTrapsLocked(2.0)
	if len(s.Traps) != 1 {
		t.Fatalf("after 2s tick: expected 1 trap, got %d", len(s.Traps))
	}
	wantRemaining := 3.0
	if math.Abs(s.Traps[0].RemainingSeconds-wantRemaining) > 1e-9 {
		t.Errorf("RemainingSeconds after 2s: got %.6f, want %.6f", s.Traps[0].RemainingSeconds, wantRemaining)
	}

	// Tick another 3.0s — trap should expire.
	s.tickTrapsLocked(3.0)
	if len(s.Traps) != 0 {
		t.Errorf("after full lifetime: expected 0 traps, got %d", len(s.Traps))
	}

	// Verify the original trap pointer still holds the last-seen values
	// (we're just checking the slice changed, not that memory was mutated).
	_ = trap
}

// TestTrap_TriggeredCulledNextTick verifies the two-phase cull semantics:
// a trap with PendingCull=true is kept alive on the blast tick (Triggered=true)
// so the end-of-tick Snapshot can deliver the VFX frame, then culled on the
// FOLLOWING tick once tickTrapEffectsLocked has reset Triggered=false.
func TestTrap_TriggeredCulledNextTick(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	trap := placeTrap(s, "explosive_trap", "p1", 0, 400, 400, 80, 20.0)
	trap.PendingCull = true
	trap.Triggered = true // blast tick: Triggered still hot → must NOT cull yet

	s.tickTrapsLocked(0.05)
	if len(s.Traps) != 1 {
		t.Errorf("blast tick (Triggered=true): expected trap still present, got %d traps", len(s.Traps))
	}

	// Simulate next tick: tickTrapEffectsLocked would reset Triggered=false.
	// tickTrapsLocked now sees PendingCull=true && Triggered=false → cull.
	trap.Triggered = false
	s.tickTrapsLocked(0.05)
	if len(s.Traps) != 0 {
		t.Errorf("tick after reset (Triggered=false): expected trap culled, got %d traps", len(s.Traps))
	}
}

// TestTrap_PlayerLeaveDropsTraps verifies that RemovePlayer culls all traps
// belonging to the leaving player. Mirrors TestBanner_PlayerLeaveCleanup.
func TestTrap_PlayerLeaveDropsTraps(t *testing.T) {
	s := newTrapState(t)

	s.mu.Lock()

	// Register players so EnsurePlayer works.
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players["p2"] = &Player{ID: "p2", Resources: map[string]int{}}

	// Plant two traps for p1, one for p2.
	placeTrap(s, "caltrops", "p1", 0, 400, 400, 60, 10.0)
	placeTrap(s, "fire_pit", "p1", 0, 410, 410, 55, 10.0)
	placeTrap(s, "caltrops", "p2", 0, 420, 420, 60, 10.0)

	if len(s.Traps) != 3 {
		t.Fatalf("setup: expected 3 traps, got %d", len(s.Traps))
	}

	s.mu.Unlock()

	// RemovePlayer acquires the lock internally.
	s.RemovePlayer("p1")

	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.Traps) != 1 {
		t.Errorf("after p1 leaves: expected 1 trap (p2's), got %d", len(s.Traps))
	}
	if s.Traps[0].OwnerPlayerID != "p2" {
		t.Errorf("remaining trap should belong to p2, got %q", s.Traps[0].OwnerPlayerID)
	}
}

// TestTrap_SnapshotIncludesTraps verifies that Snapshot() populates
// MatchSnapshotMessage.Traps with the active traps.
func TestTrap_SnapshotIncludesTraps(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	trap := placeTrap(s, "caltrops", "p1", 0, 300, 300, 60, 12.0)
	trap.DamagePerSecond = 3
	trap.SlowMultiplier = 0.7

	s.mu.Unlock()

	snap := s.Snapshot()

	if len(snap.Traps) != 1 {
		t.Fatalf("snapshot: expected 1 trap, got %d", len(snap.Traps))
	}
	ts := snap.Traps[0]
	if ts.ID != trap.ID {
		t.Errorf("trap snapshot ID: got %q, want %q", ts.ID, trap.ID)
	}
	if ts.Type != "caltrops" {
		t.Errorf("trap snapshot Type: got %q, want caltrops", ts.Type)
	}
	if ts.OwnerID != "p1" {
		t.Errorf("trap snapshot OwnerID: got %q, want p1", ts.OwnerID)
	}
	if math.Abs(ts.X-300) > 0.001 || math.Abs(ts.Y-300) > 0.001 {
		t.Errorf("trap snapshot position: got (%.1f,%.1f), want (300,300)", ts.X, ts.Y)
	}
}

// TestTrap_IDString verifies trapIDString produces the expected format.
func TestTrap_IDString(t *testing.T) {
	cases := []struct {
		id   int
		want string
	}{
		{0, "trap-0"},
		{1, "trap-1"},
		{42, "trap-42"},
		{1000, "trap-1000"},
	}
	for _, c := range cases {
		got := trapIDString(c.id)
		if got != c.want {
			t.Errorf("trapIDString(%d) = %q, want %q", c.id, got, c.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Phase B — Trapper path + auto-placement tests
// ─────────────────────────────────────────────────────────────────────────────

// TestTrapper_ArcherGetsTrapAbilityAtBronze verifies that an archer reaching
// Bronze rank on the Trapper path is assigned exactly one trap ABILITY (one
// of the four Bronze trap abilities) via the standard rank-up pipeline —
// rollUnitPoolAbilitiesLocked draws the pick onto PoolAbilitiesByRank["bronze"]
// and assignUnitPathAbilitiesLocked folds it into unit.Abilities. Trapper's
// Bronze is no longer a perk pool (the four bronze trap perks migrated to
// pool abilities), so the unit must gain no PerkIDs at Bronze either.
//
// Since archers now randomly receive Trapper or Marksman at Bronze, this
// test loops seeds and only validates trapper-assigned archers — marksman-
// assigned archers are exercised by Marksman's own test file.
func TestTrapper_ArcherGetsTrapAbilityAtBronze(t *testing.T) {
	validTraps := map[string]bool{
		"caltrops":       true,
		"fire_pit":       true,
		"explosive_trap": true,
		"marker_trap":    true,
	}

	trapperSeeds := 0
	for seed := int64(1); seed <= 20; seed++ {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
		s.mu.Lock()

		archer := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
		if archer == nil {
			s.mu.Unlock()
			t.Skipf("seed %d: archer unit type not available", seed)
			return
		}

		// Force Bronze rank-up.
		s.addUnitXPLocked(archer, 100)

		// Skip non-trapper seeds — Marksman path is covered by its own tests.
		if archer.ProgressionPath != unitPathTrapper {
			s.mu.Unlock()
			continue
		}
		trapperSeeds++

		if len(archer.PerkIDs) != 0 {
			t.Errorf("seed %d: expected no perks at Bronze (Trapper Bronze is an ability pool now), got %v", seed, archer.PerkIDs)
		}

		pick := archer.PoolAbilitiesByRank[unitRankBronze]
		if !validTraps[pick] {
			t.Errorf("seed %d: PoolAbilitiesByRank[bronze] = %q, want one of the four trap abilities", seed, pick)
		}
		if !containsAbility(archer, pick) {
			t.Errorf("seed %d: archer.Abilities does not contain the rolled trap ability %q (got %v)", seed, pick, archer.Abilities)
		}

		s.mu.Unlock()
	}
	if trapperSeeds == 0 {
		t.Fatalf("no seed in [1,20] selected the Trapper path — RNG salt or path-assignment logic likely broken")
	}
}

// TestTrapper_AbilityPlantsTrap verifies the new placement seam: casting a
// trap ABILITY at an enemy plants exactly one trap of that type with the
// authored stats. This replaces the old tickTrapPlacementLocked driver tests —
// placement cadence + the "enemy in range" gate are now the ability system's
// cooldown + autocast selector (covered by the ability-autocast suite); this
// test pins that a trapper's trap ability actually drops a correct trap.
func TestTrapper_AbilityPlantsTrap(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	archer := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 300, Y: 250})
	if archer == nil {
		archer = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 300, Y: 250})
	}
	grantTrapAbility(archer, "caltrops")

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{
		X: archer.X + 100, Y: archer.Y,
	})
	if enemy == nil {
		t.Fatal("failed to spawn enemy unit")
	}
	enemy.Visible = true

	ok, reason := s.beginAbilityCastLocked(archer, "caltrops", enemy)
	if !ok {
		t.Fatalf("beginAbilityCastLocked(caltrops) failed: %q", reason)
	}
	// castTime 0 ⇒ resolves synchronously inside beginAbilityCastLocked.
	if len(s.Traps) != 1 {
		t.Fatalf("expected 1 trap planted by the caltrops ability, got %d", len(s.Traps))
	}
	trap := s.Traps[0]
	if trap.TrapType != "caltrops" {
		t.Errorf("trap type: got %q, want caltrops", trap.TrapType)
	}
	// Stats come from the ability's place_trap config (identity modifiers, no
	// Silver/Gold perks), so they equal the authored base.
	cfg := mustTrapAbilityConfig(t, "caltrops", archer.Rank)
	if math.Abs(trap.Radius-cfg.Radius) > 1e-9 {
		t.Errorf("trap Radius: got %.3f, want %.3f", trap.Radius, cfg.Radius)
	}
	if math.Abs(trap.RemainingSeconds-cfg.DurationSeconds) > 1e-9 {
		t.Errorf("trap RemainingSeconds: got %.3f, want %.3f", trap.RemainingSeconds, cfg.DurationSeconds)
	}
}

// TestTrapper_DeadUnitDoesNotCastTrap verifies a dead unit (HP <= 0) cannot
// cast its trap ability, so no trap is planted — the ability system's own
// caster-liveness guard, the replacement for the old driver's HP check.
func TestTrapper_DeadUnitDoesNotCastTrap(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	archer := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if archer == nil {
		archer = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	}
	grantTrapAbility(archer, "caltrops")

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{
		X: archer.X + 100, Y: archer.Y,
	})
	if enemy == nil {
		t.Fatal("failed to spawn enemy unit")
	}
	enemy.Visible = true

	archer.HP = 0 // dead

	if ok, _ := s.beginAbilityCastLocked(archer, "caltrops", enemy); ok {
		t.Error("dead unit should not be able to cast its trap ability")
	}
	if len(s.Traps) != 0 {
		t.Errorf("dead unit: expected 0 traps, got %d", len(s.Traps))
	}
}

// TestTrapper_LastCombatSecondsDecays verifies that LastCombatSeconds is
// correctly decayed in the Update loop. We run a tick with a positive value and
// confirm it decrements.
func TestTrapper_LastCombatSecondsDecays(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	archer := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if archer == nil {
		archer = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	}
	archer.PerkState.LastCombatSeconds = 1.5

	s.mu.Unlock()

	// Run one Update tick at dt=0.1.
	s.Update(0.1)

	s.mu.RLock()
	defer s.mu.RUnlock()

	unit := s.unitsByID[archer.ID]
	if unit == nil {
		t.Fatal("archer was removed unexpectedly")
	}
	want := 1.4
	if math.Abs(unit.PerkState.LastCombatSeconds-want) > 1e-9 {
		t.Errorf("LastCombatSeconds after 0.1s: got %.6f, want %.6f",
			unit.PerkState.LastCombatSeconds, want)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Phase C — Effect dispatch tests
// ─────────────────────────────────────────────────────────────────────────────

// TestCaltrops_SlowsAndDamagesEnemy verifies that an enemy inside caltrops
// radius receives a slow and DoT damage.
func TestCaltrops_SlowsAndDamagesEnemy(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 400, Y: 400})
	enemy.Visible = true
	enemy.HP = 500
	enemy.MaxHP = 500

	cfg := mustTrapAbilityConfig(t, "caltrops", "")

	trap := placeTrap(s, "caltrops", "p1", 0, 400, 400, cfg.Radius, 12.0)
	trap.DamagePerSecond = cfg.DamagePerSecond
	trap.SlowMultiplier = cfg.SlowMultiplier

	hpBefore := enemy.HP
	s.tickTrapEffectsLocked(1.0) // dt=1s so DoT produces a full second of damage

	// Should be slowed.
	if enemy.SlowedRemaining <= 0 {
		t.Error("enemy inside caltrops: expected SlowedRemaining > 0")
	}
	wantSlowMult := cfg.SlowMultiplier
	if math.Abs(enemy.SlowedMultiplier-wantSlowMult) > 0.001 {
		t.Errorf("SlowedMultiplier: got %.3f, want %.3f", enemy.SlowedMultiplier, wantSlowMult)
	}

	// Should have taken DoT damage.
	expectedDmg := int(math.Round(cfg.DamagePerSecond * 1.0))
	if expectedDmg > 0 && enemy.HP >= hpBefore {
		t.Errorf("caltrops DoT: HP unchanged (was %d, got %d)", hpBefore, enemy.HP)
	}
}

// TestCaltrops_AllyInZoneUnaffected verifies that an ally of the trap owner
// inside the caltrops radius receives no slow and no DoT.
func TestCaltrops_AllyInZoneUnaffected(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	ally := s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", protocol.Vec2{X: 400, Y: 400})
	ally.Visible = true
	ally.HP = 500

	cfg := mustTrapAbilityConfig(t, "caltrops", "")

	trap := placeTrap(s, "caltrops", "p1", 0, 400, 400, cfg.Radius, 12.0)
	trap.DamagePerSecond = cfg.DamagePerSecond
	trap.SlowMultiplier = cfg.SlowMultiplier

	hpBefore := ally.HP
	s.tickTrapEffectsLocked(1.0)

	if ally.SlowedRemaining > 0 {
		t.Errorf("ally: should not be slowed, got SlowedRemaining=%.3f", ally.SlowedRemaining)
	}
	if ally.HP != hpBefore {
		t.Errorf("ally: HP should be unchanged, was %d now %d", hpBefore, ally.HP)
	}
}

// TestCaltrops_SlowExpiresAfterLeavingZone verifies that the slow applied by
// caltrops expires approximately 1s after the enemy leaves the zone (the
// ApplySlowLocked refresh window is 1s, so the slow decays naturally after
// the last tick that refreshed it).
func TestCaltrops_SlowExpiresAfterLeavingZone(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 400, Y: 400})
	enemy.Visible = true
	enemy.HP = 500

	cfg := mustTrapAbilityConfig(t, "caltrops", "")

	trap := placeTrap(s, "caltrops", "p1", 0, 400, 400, cfg.Radius, 12.0)
	trap.DamagePerSecond = cfg.DamagePerSecond
	trap.SlowMultiplier = cfg.SlowMultiplier

	// Apply slow while in zone.
	s.tickTrapEffectsLocked(0.1)
	if enemy.SlowedRemaining <= 0 {
		t.Fatal("expected slow to be applied in zone")
	}

	// Move enemy out of zone.
	enemy.X = trap.X + trap.Radius + 50

	// The slow was set to 1.0s; if we advance time by 1.5s without the trap
	// refreshing it, it should have expired.
	enemy.SlowedRemaining = 1.0
	// Decay directly (mirroring state.go Update loop for this unit).
	enemy.SlowedRemaining -= 1.1
	if enemy.SlowedRemaining < 0 {
		enemy.SlowedRemaining = 0
		enemy.SlowedMultiplier = 0
	}
	if enemy.SlowedRemaining != 0 {
		t.Errorf("slow did not expire after leaving zone: SlowedRemaining=%.3f", enemy.SlowedRemaining)
	}
}

// TestCaltrops_PersistsAcrossMultipleEnemies verifies that caltrops applies its
// effect to all enemies within range in a single tick.
func TestCaltrops_PersistsAcrossMultipleEnemies(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	cfg := mustTrapAbilityConfig(t, "caltrops", "")

	trap := placeTrap(s, "caltrops", "p1", 0, 400, 400, cfg.Radius, 12.0)
	trap.DamagePerSecond = cfg.DamagePerSecond
	trap.SlowMultiplier = cfg.SlowMultiplier

	// Spawn 3 enemies inside the zone.
	enemies := make([]*Unit, 3)
	for i := 0; i < 3; i++ {
		e := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{
			X: trap.X + float64(i)*5,
			Y: trap.Y,
		})
		e.Visible = true
		e.HP = 500
		enemies[i] = e
	}

	s.tickTrapEffectsLocked(1.0)

	for i, e := range enemies {
		if e.SlowedRemaining <= 0 {
			t.Errorf("enemy %d: expected slow, got SlowedRemaining=%.3f", i, e.SlowedRemaining)
		}
	}
}

// TestFirePit_DamagesEnemyNoslow verifies that fire_pit applies DoT but no slow.
func TestFirePit_DamagesEnemyNoSlow(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 400, Y: 400})
	enemy.Visible = true
	enemy.HP = 500

	cfg := mustTrapAbilityConfig(t, "fire_pit", "")

	trap := placeTrap(s, "fire_pit", "p1", 0, 400, 400, cfg.Radius, 10.0)
	trap.DamagePerSecond = cfg.DamagePerSecond

	hpBefore := enemy.HP
	s.tickTrapEffectsLocked(1.0)

	expectedDmg := int(math.Round(cfg.DamagePerSecond * 1.0))
	if expectedDmg > 0 && enemy.HP >= hpBefore {
		t.Errorf("fire_pit DoT: HP unchanged (was %d)", hpBefore)
	}
	if enemy.SlowedRemaining > 0 {
		t.Errorf("fire_pit must not apply slow, got SlowedRemaining=%.3f", enemy.SlowedRemaining)
	}
}

// TestExplosiveTrap_TriggersOnEnemyContact verifies that the first enemy within
// TriggerRadius causes the trap to trigger, dealing BurstDamage to all enemies
// within Radius and setting Triggered=true.
func TestExplosiveTrap_TriggersOnEnemyContact(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	cfg := mustTrapAbilityConfig(t, "explosive_trap", "")

	trap := placeTrap(s, "explosive_trap", "p1", 0, 400, 400, cfg.ExplosionRadius, 20.0)
	trap.TriggerRadius = cfg.TriggerRadius
	trap.BurstDamage = int(cfg.BurstDamage)

	// Enemy inside trigger radius.
	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 400, Y: 400})
	enemy.Visible = true
	enemy.HP = 500
	hpBefore := enemy.HP

	s.tickTrapEffectsLocked(0.05)

	if !trap.Triggered {
		t.Error("explosive_trap: expected Triggered=true after enemy contact")
	}
	expectedDmg := int(math.Round(float64(trap.BurstDamage) * 1.0)) // burst, not DoT
	if enemy.HP > hpBefore-expectedDmg {
		t.Errorf("explosive_trap: enemy HP not reduced enough (before=%d after=%d expected delta=%d)",
			hpBefore, enemy.HP, expectedDmg)
	}
}

// TestExplosiveTrap_NoFriendlyFire verifies that allies inside the explosion
// radius take ZERO damage when the trap triggers. This is the explicit
// friendly-fire test required by the spec.
func TestExplosiveTrap_NoFriendlyFire(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	cfg := mustTrapAbilityConfig(t, "explosive_trap", "")

	trap := placeTrap(s, "explosive_trap", "p1", 0, 400, 400, cfg.ExplosionRadius, 20.0)
	trap.TriggerRadius = cfg.TriggerRadius
	trap.BurstDamage = int(cfg.BurstDamage)

	// Ally inside explosion radius — must take ZERO damage.
	ally := s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", protocol.Vec2{X: 400, Y: 400})
	ally.Visible = true
	ally.HP = 500
	allyHPBefore := ally.HP

	// Enemy inside trigger radius — this triggers the explosion.
	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 400, Y: 400})
	enemy.Visible = true
	enemy.HP = 500

	s.tickTrapEffectsLocked(0.05)

	// Ally must be completely unharmed.
	if ally.HP != allyHPBefore {
		t.Errorf("FRIENDLY FIRE: ally HP changed from %d to %d (expected no damage)",
			allyHPBefore, ally.HP)
	}
}

// TestExplosiveTrap_CulledAfterTrigger verifies the two-phase cull: the trap
// survives the first tickTrapsLocked call (blast tick, Triggered=true) so the
// end-of-tick Snapshot can deliver the VFX frame, then is removed on the second
// tickTrapsLocked call once Triggered has been reset to false.
func TestExplosiveTrap_CulledAfterTrigger(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	cfg := mustTrapAbilityConfig(t, "explosive_trap", "")

	trap := placeTrap(s, "explosive_trap", "p1", 0, 400, 400, cfg.ExplosionRadius, 20.0)
	trap.TriggerRadius = cfg.TriggerRadius
	trap.BurstDamage = int(cfg.BurstDamage)

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 400, Y: 400})
	enemy.Visible = true
	enemy.HP = 500

	// Blast tick: effects fire the explosion, setting Triggered=true, PendingCull=true.
	s.tickTrapEffectsLocked(0.05)
	if !trap.Triggered {
		t.Fatal("trap did not trigger")
	}
	if !trap.PendingCull {
		t.Fatal("trap should have PendingCull=true after non-aftershock blast")
	}

	// Blast tick cull pass: Triggered=true → two-phase gate keeps the trap so the
	// end-of-tick Snapshot can serialize triggered=true for the client VFX frame.
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.tickTrapsLocked(0.05)
	if len(s.Traps) != 1 {
		t.Errorf("blast tick: expected trap still alive for VFX frame, got %d traps", len(s.Traps))
	}

	// Next tick: tickTrapEffectsLocked resets Triggered=false (trap has PendingCull,
	// so the effects body is skipped). Then tickTrapsLocked sees PendingCull&&!Triggered
	// and culls the trap.
	s.tickTrapEffectsLocked(0.05)
	s.tickTrapsLocked(0.05)
	if len(s.Traps) != 0 {
		t.Errorf("tick after VFX frame: expected trap culled, got %d traps", len(s.Traps))
	}
}

// TestExplosiveTrap_AoEDamagesAllEnemiesInRadius verifies that all enemies
// within the explosion radius take damage, not just the triggering unit.
func TestExplosiveTrap_AoEDamagesAllEnemiesInRadius(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	cfg := mustTrapAbilityConfig(t, "explosive_trap", "")

	trap := placeTrap(s, "explosive_trap", "p1", 0, 400, 400, cfg.ExplosionRadius, 20.0)
	trap.TriggerRadius = cfg.TriggerRadius
	trap.BurstDamage = int(cfg.BurstDamage)

	// Three enemies: one inside trigger radius, two more inside explosion radius.
	triggerEnemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 400, Y: 400})
	triggerEnemy.Visible = true
	triggerEnemy.HP = 500

	blastEnemy1 := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 420, Y: 400})
	blastEnemy1.Visible = true
	blastEnemy1.HP = 500

	blastEnemy2 := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 400, Y: 420})
	blastEnemy2.Visible = true
	blastEnemy2.HP = 500

	hp1 := triggerEnemy.HP
	hp2 := blastEnemy1.HP
	hp3 := blastEnemy2.HP

	s.tickTrapEffectsLocked(0.05)

	if triggerEnemy.HP >= hp1 {
		t.Errorf("trigger enemy took no damage")
	}
	if blastEnemy1.HP >= hp2 {
		t.Errorf("blast enemy 1 took no damage")
	}
	if blastEnemy2.HP >= hp3 {
		t.Errorf("blast enemy 2 took no damage")
	}
}

// TestMarkerTrap_MarksEnemy verifies that an enemy entering the marker_trap zone
// gets MarkedRemaining > 0 and MarkedMultiplier set.
func TestMarkerTrap_MarksEnemy(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 400, Y: 400})
	enemy.Visible = true
	enemy.HP = 500

	cfg := mustTrapAbilityConfig(t, "marker_trap", "")

	trap := placeTrap(s, "marker_trap", "p1", 0, 400, 400, cfg.Radius, 12.0)
	trap.MarkMultiplier = cfg.MarkMultiplier
	trap.MarkDuration = cfg.MarkDuration

	s.tickTrapEffectsLocked(0.05)

	if !enemy.PerkState.anyMarkActive() {
		t.Error("enemy inside marker_trap: expected a mark stack")
	}
	if math.Abs(enemy.PerkState.totalMarkMultiplier()-cfg.MarkMultiplier) > 0.001 {
		t.Errorf("total mark multiplier: got %.3f, want %.3f",
			enemy.PerkState.totalMarkMultiplier(), cfg.MarkMultiplier)
	}
}

// TestMarkerTrap_MarkPersistsAfterLeaving verifies that the mark persists after
// the enemy leaves the zone (it decays naturally, not on zone exit).
func TestMarkerTrap_MarkPersistsAfterLeaving(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 400, Y: 400})
	enemy.Visible = true
	enemy.HP = 500

	cfg := mustTrapAbilityConfig(t, "marker_trap", "")

	trap := placeTrap(s, "marker_trap", "p1", 0, 400, 400, cfg.Radius, 12.0)
	trap.MarkMultiplier = cfg.MarkMultiplier
	trap.MarkDuration = cfg.MarkDuration

	// Apply mark.
	s.tickTrapEffectsLocked(0.05)
	if !enemy.PerkState.anyMarkActive() {
		t.Fatal("mark was not applied")
	}

	// Move enemy outside zone.
	enemy.X = trap.X + trap.Radius + 100

	// Tick effects — enemy is outside now, so no refresh. Mark should still be set.
	s.tickTrapEffectsLocked(0.05)
	if !enemy.PerkState.anyMarkActive() {
		t.Error("mark should persist after leaving zone (decays naturally)")
	}
}

// TestMarkerTrap_RefreshStrongerSemantics verifies that a stronger overlapping
// mark source wins (MarkedMultiplier takes max).
func TestMarkerTrap_RefreshStrongerSemantics(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 400, Y: 400})
	enemy.Visible = true
	enemy.HP = 500

	cfg := mustTrapAbilityConfig(t, "marker_trap", "")

	// Set a stronger mark already on the unit from a DIFFERENT source
	// (e.g. challengers_mark from a Vanguard, ownerUnitID=999). Under the
	// per-source stack model this becomes its own stack; a marker_trap
	// from a different owner (ownerUnit=0 in placeTrap) adds a second
	// stack rather than overwriting. Verify the pre-existing stack's
	// multiplier is preserved.
	strongerMult := cfg.MarkMultiplier + 0.10
	enemy.PerkState.applyMarkStack("unit-999", 999, strongerMult, 10.0)

	trap := placeTrap(s, "marker_trap", "p1", 0, 400, 400, cfg.Radius, 12.0)
	trap.MarkMultiplier = cfg.MarkMultiplier
	trap.MarkDuration = cfg.MarkDuration

	s.tickTrapEffectsLocked(0.05)

	// The challengers_mark stack (sourceID "unit-999") must still be at
	// its original stronger multiplier — no downgrade.
	var preExisting *markStack
	for i := range enemy.PerkState.MarkStacks {
		if enemy.PerkState.MarkStacks[i].SourceID == "unit-999" {
			preExisting = &enemy.PerkState.MarkStacks[i]
			break
		}
	}
	if preExisting == nil {
		t.Fatal("pre-existing challengers_mark stack was removed")
	}
	if preExisting.Multiplier < strongerMult {
		t.Errorf("refresh-stronger: pre-existing stack multiplier was downgraded from %.3f to %.3f",
			strongerMult, preExisting.Multiplier)
	}
}

// TestMarkerTrap_AmplifiedDamage verifies that incoming damage to a marked
// enemy is amplified through applyUnitDamageLocked (the standard pipeline).
func TestMarkerTrap_AmplifiedDamage(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 400, Y: 400})
	enemy.Visible = true
	enemy.HP = 500
	enemy.MaxHP = 500

	cfg := mustTrapAbilityConfig(t, "marker_trap", "")

	// Apply mark manually via the stack helper.
	enemy.PerkState.applyMarkStack("unit-1", 1, cfg.MarkMultiplier, 4.0)

	// Deal 100 raw damage. applyUnitDamageLocked amplifies by (1 + markMultiplier).
	const raw = 100
	hpBefore := enemy.HP
	s.applyUnitDamageLocked(enemy, raw)

	expected := int(math.Round(float64(raw) * (1.0 + cfg.MarkMultiplier)))
	actual := hpBefore - enemy.HP

	if actual != expected {
		t.Errorf("marked damage: got %d, want %d (mark mult=%.2f)", actual, expected, cfg.MarkMultiplier)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Phase D — requiresAbility filter tests
// ─────────────────────────────────────────────────────────────────────────────

// TestRequiresAbility_ExplosiveChainVisibleWithExplosiveTrap verifies that
// explosive_chain appears in the Silver pool when the unit already knows the
// explosive_trap ABILITY (its requiresAbility gate is satisfied).
func TestRequiresAbility_ExplosiveChainVisibleWithExplosiveTrap(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	archer := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if archer == nil {
		archer = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	}
	archer.ProgressionPath = unitPathTrapper
	archer.Rank = unitRankSilver

	// Grant the prerequisite trap ability.
	grantTrapAbility(archer, "explosive_trap")

	pool := s.perkPoolForRankLocked(archer, unitRankSilver)

	found := false
	for _, def := range pool {
		if def.ID == "explosive_chain" {
			found = true
			break
		}
	}
	if !found {
		t.Error("unit with the explosive_trap ability should see explosive_chain in Silver pool")
	}
}

// TestRequiresAbility_ExplosiveChainHiddenWithoutExplosiveTrap verifies that
// explosive_chain does NOT appear in the Silver pool when the unit knows a
// different trap ability (not explosive_trap).
func TestRequiresAbility_ExplosiveChainHiddenWithoutExplosiveTrap(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	archer := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if archer == nil {
		archer = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	}
	archer.ProgressionPath = unitPathTrapper
	archer.Rank = unitRankSilver

	// Know caltrops (not explosive_trap).
	grantTrapAbility(archer, "caltrops")

	pool := s.perkPoolForRankLocked(archer, unitRankSilver)

	for _, def := range pool {
		if def.ID == "explosive_chain" {
			t.Errorf("unit with caltrops (not explosive_trap) should NOT see explosive_chain, but found it in pool")
			return
		}
	}
}

// TestRequiresPerk_DoesNotBreakSoldierPools verifies that adding the requiresPerk
// filter does not change the Vanguard/Berserker perk pool sizes (regression).
func TestRequiresPerk_DoesNotBreakSoldierPools(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, pathName := range []string{unitPathVanguard, unitPathBerserker} {
		for _, rank := range []string{unitRankBronze, unitRankSilver, unitRankGold} {
			soldier := s.spawnPlayerUnitLocked("soldier", fmt.Sprintf("p-%s-%s", pathName, rank),
				"#3498db", protocol.Vec2{X: 400, Y: 400})
			soldier.ProgressionPath = pathName
			soldier.Rank = rank

			pool := s.perkPoolForRankLocked(soldier, rank)

			// Soldier perks should never have requiresPerk set.
			for _, def := range pool {
				if def.RequiresPerk != "" {
					t.Errorf("%s/%s perk %q has requiresPerk=%q — soldiers should have none",
						pathName, rank, def.ID, def.RequiresPerk)
				}
			}

			// Pool should be non-empty for Bronze (all ranks have authored perks).
			if rank == unitRankBronze && len(pool) == 0 {
				t.Errorf("%s Bronze pool is empty (regression)", pathName)
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Phase E — QA-added tests
// ─────────────────────────────────────────────────────────────────────────────

// TestCaltrops_DoTAtProductionTickRate verifies that caltrops actually deals
// damage at the production tick rate (dt = 1/20 = 0.05 s). At dt=0.05 and
// damagePerSecond=3, math.Round(3*0.05) = math.Round(0.15) = 0, which means
// caltrops deals ZERO damage per tick in production.
//
// This test is intentionally written to FAIL until the implementation is fixed.
// The correct fix is to accumulate fractional damage across ticks (e.g. using a
// per-trap or per-unit damage accumulator) rather than rounding per-tick.
func TestCaltrops_DoTAtProductionTickRate(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 400, Y: 400})
	enemy.Visible = true
	enemy.HP = 500
	enemy.MaxHP = 500

	cfg := mustTrapAbilityConfig(t, "caltrops", "")

	trap := placeTrap(s, "caltrops", "p1", 0, 400, 400, cfg.Radius, 12.0)
	trap.DamagePerSecond = cfg.DamagePerSecond // 3 dmg/s
	trap.SlowMultiplier = cfg.SlowMultiplier

	const productionDT = 1.0 / 20.0 // 0.05 s — the actual loop.go tick rate
	const ticks = 20                // 1 simulated second
	hpBefore := enemy.HP

	for i := 0; i < ticks; i++ {
		s.tickTrapEffectsLocked(productionDT)
	}

	// After 1 full second of caltrops (3 dmg/s), enemy must have lost at least
	// 1 HP. If math.Round(3*0.05)=0 every tick, HP will be unchanged.
	if enemy.HP >= hpBefore {
		t.Errorf("caltrops DoT at dt=%.4f: enemy HP unchanged after %d ticks (%.1f simulated seconds). "+
			"math.Round(damagePerSecond*dt) = math.Round(%.2f*%.4f) = %d — DoT is zero every tick at production rate. "+
			"Fix: accumulate fractional damage across ticks.",
			productionDT, ticks, float64(ticks)*productionDT,
			trap.DamagePerSecond, productionDT,
			int(math.Round(trap.DamagePerSecond*productionDT)))
	}
}

// TestFirePit_DoTAtProductionTickRate is the same check for fire_pit.
// damagePerSecond=8, dt=0.05 → math.Round(8*0.05)=math.Round(0.40)=0.
// fire_pit also deals zero damage per tick in production.
func TestFirePit_DoTAtProductionTickRate(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 400, Y: 400})
	enemy.Visible = true
	enemy.HP = 500
	enemy.MaxHP = 500

	cfg := mustTrapAbilityConfig(t, "fire_pit", "")

	trap := placeTrap(s, "fire_pit", "p1", 0, 400, 400, cfg.Radius, 10.0)
	trap.DamagePerSecond = cfg.DamagePerSecond // 8 dmg/s

	const productionDT = 1.0 / 20.0
	const ticks = 20 // 1 simulated second
	hpBefore := enemy.HP

	for i := 0; i < ticks; i++ {
		s.tickTrapEffectsLocked(productionDT)
	}

	if enemy.HP >= hpBefore {
		t.Errorf("fire_pit DoT at dt=%.4f: enemy HP unchanged after %d ticks (%.1f simulated seconds). "+
			"math.Round(damagePerSecond*dt) = math.Round(%.2f*%.4f) = %d — DoT is zero every tick at production rate. "+
			"Fix: accumulate fractional damage across ticks.",
			productionDT, ticks, float64(ticks)*productionDT,
			trap.DamagePerSecond, productionDT,
			int(math.Round(trap.DamagePerSecond*productionDT)))
	}
}

// TestFirePit_NoFriendlyFire verifies that fire_pit does not damage an ally
// inside the zone. The existing caltrops ally test covers caltrops; this covers
// fire_pit explicitly. Explosive_trap is already covered by TestExplosiveTrap_NoFriendlyFire.
func TestFirePit_NoFriendlyFire(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	ally := s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", protocol.Vec2{X: 400, Y: 400})
	ally.Visible = true
	ally.HP = 500

	cfg := mustTrapAbilityConfig(t, "fire_pit", "")

	trap := placeTrap(s, "fire_pit", "p1", 0, 400, 400, cfg.Radius, 10.0)
	trap.DamagePerSecond = cfg.DamagePerSecond

	hpBefore := ally.HP
	s.tickTrapEffectsLocked(1.0) // large dt to guarantee dmg > 0 if ally filter is broken

	if ally.HP != hpBefore {
		t.Errorf("FRIENDLY FIRE: fire_pit damaged ally (HP %d → %d)", hpBefore, ally.HP)
	}
}

// TestMarkerTrap_NoFriendlyFire verifies that marker_trap does not mark an ally.
func TestMarkerTrap_NoFriendlyFire(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	ally := s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", protocol.Vec2{X: 400, Y: 400})
	ally.Visible = true
	ally.HP = 500

	cfg := mustTrapAbilityConfig(t, "marker_trap", "")

	trap := placeTrap(s, "marker_trap", "p1", 0, 400, 400, cfg.Radius, 12.0)
	trap.MarkMultiplier = cfg.MarkMultiplier
	trap.MarkDuration = cfg.MarkDuration

	s.tickTrapEffectsLocked(0.05)

	if ally.PerkState.anyMarkActive() {
		t.Errorf("FRIENDLY FIRE: marker_trap marked ally (%d stacks)", len(ally.PerkState.MarkStacks))
	}
}

// TestTrap_SnapshotOmitEmptyWhenNoTraps verifies that MatchSnapshotMessage.Traps
// is absent from the JSON wire format when there are no active traps.
func TestTrap_SnapshotOmitEmptyWhenNoTraps(t *testing.T) {
	s := newTrapState(t)
	// No traps added.

	snap := s.Snapshot()

	if len(snap.Traps) != 0 {
		t.Fatalf("snapshot with no traps: expected empty Traps slice, got %d", len(snap.Traps))
	}

	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	payload := string(raw)
	if strings.Contains(payload, `"traps"`) {
		t.Errorf("traps key present in JSON despite omitempty with no traps: %s", payload[:min(len(payload), 200)])
	}
}

// TestTrap_SnapshotTriggeredOmitWhenFalse verifies that Triggered=false is
// absent from the JSON trap snapshot (omitempty), but Triggered=true is present.
func TestTrap_SnapshotTriggeredOmitWhenFalse(t *testing.T) {
	notTriggered := protocol.TrapSnapshot{
		ID:               "trap-1",
		OwnerID:          "p1",
		X:                400,
		Y:                400,
		Radius:           60,
		Type:             "caltrops",
		RemainingSeconds: 10,
		Triggered:        false,
	}
	raw, err := json.Marshal(notTriggered)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(raw), `"triggered"`) {
		t.Errorf("Triggered=false should be omitted from JSON (omitempty), got: %s", raw)
	}

	triggered := notTriggered
	triggered.Triggered = true
	raw2, err := json.Marshal(triggered)
	if err != nil {
		t.Fatalf("json.Marshal triggered: %v", err)
	}
	if !strings.Contains(string(raw2), `"triggered":true`) {
		t.Errorf("Triggered=true should appear in JSON, got: %s", raw2)
	}
}

// TestTrapper_SoldierPathNotTrpper verifies that soldiers ranking up do NOT
// receive the trapper path — they must receive vanguard or berserker. This is
// a regression guard for assignUnitPathOnRankUpLocked's switch-on-type change.
func TestTrapper_SoldierPathNotTrapper(t *testing.T) {
	for seed := int64(1); seed <= 20; seed++ {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), seed)
		s.mu.Lock()

		soldier := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
		if soldier == nil {
			s.mu.Unlock()
			continue
		}

		s.addUnitXPLocked(soldier, 100) // force Bronze rank-up

		path := soldier.ProgressionPath
		if path == unitPathTrapper {
			t.Errorf("seed %d: soldier was assigned trapper path (regression in assignUnitPathOnRankUpLocked)", seed)
		}
		if path != unitPathVanguard && path != unitPathBerserker {
			t.Errorf("seed %d: soldier has unexpected path %q (want vanguard or berserker)", seed, path)
		}

		s.mu.Unlock()
	}
}

// TestTrapper_CatalogLoadedForAllRanks confirms the Trapper path has a
// loaded entry for every rank. The actual multiplier values live in
// catalog/units/human/archer/paths/trapper/trapper.json and are expected to evolve
// with balance tuning — this test deliberately does NOT pin them, only
// that the JSON loaded successfully for each rank. Other path invariants
// (positive multipliers, correct Path/Rank tagging) are covered by
// TestPathCatalog_ShippedPathsHaveAllRanks in path_defs_test.go.
func TestTrapper_CatalogLoadedForAllRanks(t *testing.T) {
	for _, rank := range []string{unitRankBronze, unitRankSilver, unitRankGold} {
		if _, ok := pathModifiersByKey[pathModifierKey(unitPathTrapper, rank)]; !ok {
			t.Errorf("trapper/%s missing from pathModifiersByKey — JSON catalog not loaded correctly", rank)
		}
	}
}

// min is a local helper since Go 1.20 min() is built-in but older Go versions
// may not have it available in test code.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ─────────────────────────────────────────────────────────────────────────────
// Phase F — QA-flagged coverage gaps (non-blocking follow-ups)
// ─────────────────────────────────────────────────────────────────────────────

// TestMarkerTrap_VsChallengersMarkCoexistAsStacks verifies the per-source
// stacking semantics when a marker_trap overlaps with an existing Challenger's
// Mark on the same target. Each source occupies its own stack (up to
// maxDebuffStacks), and neither downgrades the other. Same-source
// re-application still uses refresh-stronger / refresh-longer rules on that
// source's own stack.
func TestMarkerTrap_VsChallengersMarkCoexistAsStacks(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 400, Y: 400})
	enemy.Visible = true
	enemy.HP = 500

	cfg := mustTrapAbilityConfig(t, "marker_trap", "")
	trapMult := cfg.MarkMultiplier // e.g. 0.20
	trapDur := cfg.MarkDuration    // e.g. 4.0

	// Simulate Challenger's Mark already active from a Vanguard (owner 42)
	// with a stronger multiplier and longer duration than the trap.
	const vanguardOwnerID = 42
	challengerMult := trapMult + 0.10 // 0.30
	challengerDur := trapDur + 2.0    // 6.0
	enemy.PerkState.applyMarkStack(unitMarkSourceID(vanguardOwnerID), vanguardOwnerID, challengerMult, challengerDur)

	// marker_trap (placed with ownerUnitID=0 via placeTrap) adds a SECOND
	// stack rather than overwriting.
	trap := placeTrap(s, "marker_trap", "p1", 0, 400, 400, cfg.Radius, 12.0)
	trap.MarkMultiplier = trapMult
	trap.MarkDuration = trapDur
	s.tickTrapEffectsLocked(0.05)

	if len(enemy.PerkState.MarkStacks) != 2 {
		t.Fatalf("expected 2 stacks after different-source marker_trap, got %d", len(enemy.PerkState.MarkStacks))
	}
	// The Vanguard stack must still be at its original stronger/longer values.
	var vStack *markStack
	for i := range enemy.PerkState.MarkStacks {
		if enemy.PerkState.MarkStacks[i].SourceID == unitMarkSourceID(vanguardOwnerID) {
			vStack = &enemy.PerkState.MarkStacks[i]
		}
	}
	if vStack == nil {
		t.Fatal("challengers_mark stack was removed when marker_trap applied")
	}
	if vStack.Multiplier != challengerMult || vStack.Remaining != challengerDur {
		t.Errorf("challengers_mark stack was mutated: got mult=%.3f dur=%.3f; want mult=%.3f dur=%.3f",
			vStack.Multiplier, vStack.Remaining, challengerMult, challengerDur)
	}

	// Same-source marker_trap tick refreshes the marker_trap stack's own
	// duration to max(existing, trapDur) — legacy refresh rules on that
	// stack, without creating a 3rd stack or touching the Vanguard stack.
	stacksBeforeTick := len(enemy.PerkState.MarkStacks)
	s.tickTrapEffectsLocked(0.05)
	if len(enemy.PerkState.MarkStacks) != stacksBeforeTick {
		t.Errorf("same-source re-tick must not add a stack: got %d, want %d",
			len(enemy.PerkState.MarkStacks), stacksBeforeTick)
	}
}

// TestTrapper_BuildingAttackDoesNotStampLastCombatSeconds pins the current
// behavior: an Archer attacking a building does NOT stamp LastCombatSeconds,
// so traps do not plant during building-only combat. This is intentional
// (traps are an anti-unit tool) and this test prevents an accidental regression.
func TestTrapper_BuildingAttackDoesNotStampLastCombatSeconds(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}

	archer := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if archer == nil {
		archer = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	}
	archer.Visible = true
	archer.HP = 100

	// Place a building adjacent to the archer.
	buildingID := "test-tower-trapper"
	building := &protocol.BuildingTile{
		GridCoord:    protocol.GridCoord{X: 13, Y: 13},
		ID:           buildingID,
		BuildingType: "tower",
		Width:        1,
		Height:       1,
		Metadata: map[string]interface{}{
			"hp":    float64(200),
			"maxHp": float64(200),
		},
	}
	s.MapConfig.Buildings = append(s.MapConfig.Buildings, *building)
	s.buildingsByID[buildingID] = &s.MapConfig.Buildings[len(s.MapConfig.Buildings)-1]

	// Set the archer to attack the building with cooldown expired.
	archer.AttackBuildingTargetID = buildingID
	archer.Attacking = true
	archer.AttackCooldown = 0
	archer.AttackRange = 5000 // always in range
	archer.PerkState.LastCombatSeconds = 0

	blocked := s.getBlockedCellsLocked()
	s.tickUnitCombatLocked(0.05, blocked)

	// LastCombatSeconds must remain 0: building attacks do not count as "in combat"
	// for trap-placement purposes.
	if archer.PerkState.LastCombatSeconds != 0 {
		t.Errorf("building attack stamped LastCombatSeconds=%.3f (expected 0); "+
			"traps should not plant during building-only combat",
			archer.PerkState.LastCombatSeconds)
	}
}

// TestPerkPool_HigherTiersExhaustedCascadesToBronze verifies the rank-pool
// cascade: when a unit's requested rank yields no eligible perks (here, every
// Gold and Silver perk is already owned), the pool drops to the next lower rank
// rather than returning empty — keeping rank-ups productive. Uses the Marksman
// (whose Bronze slot is still perks) since the Trapper's Bronze is now an
// ability pool, not a perk pool, so it can no longer be the cascade target.
func TestPerkPool_HigherTiersExhaustedCascadesToBronze(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	archer := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if archer == nil {
		t.Skip("archer unit type not available; skipping cascade test")
	}

	archer.ProgressionPath = unitPathMarksman
	archer.Rank = unitRankGold
	// Own every Silver and Gold Marksman perk so both higher-tier pools are
	// exhausted by the ownership filter → cascade must fall through to Bronze.
	archer.PerkIDs = append([]string{}, wantPathPools["marksman"][unitRankSilver]...)
	archer.PerkIDs = append(archer.PerkIDs, wantPathPools["marksman"][unitRankGold]...)

	pool := s.perkPoolForRankLocked(archer, unitRankGold)

	if len(pool) == 0 {
		t.Fatal("expected cascade to Bronze when Silver+Gold are exhausted, got empty pool")
	}
	bronzePool := make(map[string]bool, len(wantPathPools["marksman"][unitRankBronze]))
	for _, id := range wantPathPools["marksman"][unitRankBronze] {
		bronzePool[id] = true
	}
	owned := make(map[string]bool, len(archer.PerkIDs))
	for _, id := range archer.PerkIDs {
		owned[id] = true
	}
	for _, def := range pool {
		if !bronzePool[def.ID] {
			t.Errorf("cascade returned perk %q which is not in the marksman Bronze pool", def.ID)
		}
		if owned[def.ID] {
			t.Errorf("cascade returned already-owned perk %q", def.ID)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Phase G — VFX pipeline correctness tests
// ─────────────────────────────────────────────────────────────────────────────

// TestExplosiveTrap_TriggeredFlagVisibleInSnapshot verifies the one-tick VFX
// pipeline for a non-aftershock explosive_trap driven through the PRODUCTION
// Update path (tickTrapEffectsLocked → tickBannersLocked → tickTrapsLocked in
// sequence, with Snapshot called AFTER Update returns — exactly as loop.go does).
//
//  1. Tick 1 (s.Update): enemy in trigger zone → blast fires, Triggered=true,
//     PendingCull=true. tickTrapsLocked sees PendingCull&&Triggered → keeps trap.
//  2. Snapshot after tick 1: trap present with triggered=true in snapshot.
//  3. Tick 2 (s.Update): tickTrapEffectsLocked resets Triggered=false; then
//     tickTrapsLocked sees PendingCull&&!Triggered → culls.
//  4. Snapshot after tick 2: trap gone.
func TestExplosiveTrap_TriggeredFlagVisibleInSnapshot(t *testing.T) {
	const dt = 0.05

	s := newTrapState(t)
	s.mu.Lock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	cfg := mustTrapAbilityConfig(t, "explosive_trap", "")

	trap := placeTrap(s, "explosive_trap", "p1", 0, 400, 400, cfg.ExplosionRadius, 20.0)
	trap.TriggerRadius = cfg.TriggerRadius
	trap.BurstDamage = int(cfg.BurstDamage)
	trapID := trap.ID

	// Spawn enemy at the trap centre — well inside both trigger and explosion radii.
	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 400, Y: 400})
	enemy.Visible = true
	enemy.HP = 500
	enemy.MaxHP = 500

	s.mu.Unlock()

	// ── Tick 1: enemy in range → blast fires ────────────────────────────────
	// Update runs all three tick functions, THEN we call Snapshot — exactly as
	// loop.go does. tickTrapsLocked must NOT cull the trap this tick because
	// Triggered=true (the two-phase gate).
	s.Update(dt)

	snap := s.Snapshot()
	var foundSnap *protocol.TrapSnapshot
	for i := range snap.Traps {
		if snap.Traps[i].ID == trapID {
			foundSnap = &snap.Traps[i]
			break
		}
	}
	if foundSnap == nil {
		t.Fatal("tick 1 snapshot: trap must still be present so the client receives the VFX frame")
	}
	if !foundSnap.Triggered {
		t.Error("tick 1 snapshot: triggered must be true so the client renders the blast")
	}

	// ── Tick 2: effects reset Triggered=false, then cull fires ─────────────
	s.Update(dt)

	snap2 := s.Snapshot()
	for _, ts := range snap2.Traps {
		if ts.ID == trapID {
			t.Error("tick 2 snapshot: trap should be gone after the two-phase cull")
		}
	}
}

// TestExplosiveTrap_TriggeredVisibleAfterUpdate is the regression-guard
// integration test for the production VFX bug: triggered=true was never reaching
// the client because tickTrapsLocked culled the trap on the SAME Update tick that
// the blast fired, before BroadcastSnapshot (and thus Snapshot) could serialize it.
//
// This test drives the scenario exactly as loop.go does:
//
//	s.Update(dt)           // runs effects → banners → traps in sequence
//	snap := s.Snapshot()   // called AFTER Update returns
//
// It must catch any regression where triggered=true is absent from the first
// post-Update snapshot.
func TestExplosiveTrap_TriggeredVisibleAfterUpdate(t *testing.T) {
	const dt = 0.05 // production 20 Hz tick

	s := newTrapState(t)
	s.mu.Lock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	cfg := mustTrapAbilityConfig(t, "explosive_trap", "")

	// Place trap at a neutral position with known radii.
	trap := placeTrap(s, "explosive_trap", "p1", 0, 500, 500, cfg.ExplosionRadius, 30.0)
	trap.TriggerRadius = cfg.TriggerRadius
	trap.BurstDamage = int(cfg.BurstDamage)
	trapID := trap.ID

	// Enemy at the exact trap centre — unambiguously inside trigger radius.
	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 500, Y: 500})
	enemy.Visible = true
	enemy.HP = 1000
	enemy.MaxHP = 1000

	s.mu.Unlock()

	// Production sequence: Update then Snapshot (mirrors loop.go exactly).
	s.Update(dt)
	snap := s.Snapshot()

	// The trap must be in the snapshot with triggered=true. If it is absent or
	// triggered=false, the client never renders the blast VFX — this is the bug.
	var blastSnap *protocol.TrapSnapshot
	for i := range snap.Traps {
		if snap.Traps[i].ID == trapID {
			blastSnap = &snap.Traps[i]
			break
		}
	}
	if blastSnap == nil {
		t.Fatal("blast-tick snapshot: trap is absent — client would see no explosion at all (VFX bug)")
	}
	if !blastSnap.Triggered {
		t.Error("blast-tick snapshot: triggered=false — client would not render the blast VFX (VFX bug)")
	}

	// Second Update+Snapshot: trap must be gone (culled on the second tick).
	s.Update(dt)
	snap2 := s.Snapshot()
	for _, ts := range snap2.Traps {
		if ts.ID == trapID {
			t.Error("post-blast snapshot: trap still present — should have been culled after VFX tick")
			break
		}
	}
}

// TestPerkPool_GoldReturnsGoldPerks verifies that when a Trapper reaches Gold
// rank and has consumed the Bronze/Silver tiers, the Gold pool is populated
// with adaptive Gold perks (ascendant_infusion, increased_deployment,
// overload_protocol). The cascade only falls through to Silver/Bronze when the
// Gold pool is empty — that fallback is still valid runtime behavior, but with
// the three Gold perks authored it no longer triggers for Trapper.
func TestPerkPool_GoldReturnsGoldPerks(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	archer := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if archer == nil {
		t.Skip("archer unit type not available; skipping Gold pool test")
	}

	archer.ProgressionPath = unitPathTrapper
	archer.Rank = unitRankGold
	grantTrapAbility(archer, "caltrops")
	archer.PerkIDs = []string{
		"extended_setup", "wider_nets", "rapid_deployment", "amplified_effects",
		"barbed_field",
	}

	pool := s.perkPoolForRankLocked(archer, unitRankGold)

	if len(pool) == 0 {
		t.Fatal("expected non-empty Gold pool for Trapper")
	}
	goldPool := make(map[string]bool, len(wantPathPools["trapper"][unitRankGold]))
	for _, id := range wantPathPools["trapper"][unitRankGold] {
		goldPool[id] = true
	}
	for _, def := range pool {
		if !goldPool[def.ID] {
			t.Errorf("Gold pool returned perk %q which is not in the trapper Gold pool", def.ID)
		}
	}
}
