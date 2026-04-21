package game

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// newTrapSilverState returns a minimal GameState with player "p1" registered.
// No units are spawned — callers add via spawnArcher or spawnPlayerUnitLocked.
func newTrapSilverState(t *testing.T) *GameState {
	t.Helper()
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 99)
	s.mu.Lock()
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.mu.Unlock()
	return s
}

// spawnTrapArcher spawns an archer for "p1" at (400,400) with a Bronze trap
// perk already granted, ready for placement (LastCombatSeconds set, cooldown 0).
func spawnTrapArcher(t *testing.T, s *GameState, trapPerkID string) *Unit {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if u == nil {
		u = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	}
	if u == nil {
		t.Fatal("spawnTrapArcher: failed to spawn unit")
	}
	u.Visible = true
	u.PerkIDs = append(u.PerkIDs, trapPerkID)
	u.PerkState.LastCombatSeconds = 1.5
	u.PerkState.TrapPlaceCooldownRemaining = 0
	return u
}

// assertFloatEq fails if got and want differ by more than 1e-6.
func assertFloatEq(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("%s: got %.8f, want %.8f", label, got, want)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. Identity — Bronze caltrops with no Silver modifiers
// ─────────────────────────────────────────────────────────────────────────────

// TestTrapModifiers_Identity_BronzeCaltropsUnchanged verifies that a unit with
// only the caltrops perk and no Silver modifiers gets stats that exactly match
// the Bronze config values — the modifier pipeline is a no-op.
func TestTrapModifiers_Identity_BronzeCaltropsUnchanged(t *testing.T) {
	s := newTrapSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if u == nil {
		u = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	}
	u.PerkIDs = []string{"caltrops"}

	stats, ok := s.DebugEffectiveTrapStats(u)
	if !ok {
		t.Fatal("DebugEffectiveTrapStats returned false for unit with caltrops")
	}

	def := perkDefByID("caltrops")
	if def == nil {
		t.Fatal("caltrops perk def not found")
	}

	assertFloatEq(t, "DurationSeconds", stats.DurationSeconds, def.Config["durationSeconds"])
	assertFloatEq(t, "Radius", stats.Radius, def.Config["radius"])
	assertFloatEq(t, "PlaceInterval", stats.PlaceInterval, def.Config["placeIntervalSeconds"])
	assertFloatEq(t, "DamagePerSecond", stats.DamagePerSecond, def.Config["damagePerSecond"])
	assertFloatEq(t, "SlowMultiplier", stats.SlowMultiplier, def.Config["slowMultiplier"])
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. extended_setup — duration scales by 1.5×
// ─────────────────────────────────────────────────────────────────────────────

// TestTrapModifiers_ExtendedSetup_DurationCaltrops verifies that extended_setup
// alone raises caltrops durationSeconds from 12 → 18.
func TestTrapModifiers_ExtendedSetup_DurationCaltrops(t *testing.T) {
	s := newTrapSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if u == nil {
		u = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	}
	u.PerkIDs = []string{"caltrops", "extended_setup"}

	stats, ok := s.DebugEffectiveTrapStats(u)
	if !ok {
		t.Fatal("DebugEffectiveTrapStats returned false")
	}

	// Bronze: durationSeconds=12, multiplier=1.5 → 18.
	assertFloatEq(t, "DurationSeconds", stats.DurationSeconds, 18.0)
	// Other fields must remain at base values.
	assertFloatEq(t, "Radius", stats.Radius, 60.0)
	assertFloatEq(t, "PlaceInterval", stats.PlaceInterval, 6.0)
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. wider_nets — radius scales by 1.3×; explosive also scales triggerRadius
// ─────────────────────────────────────────────────────────────────────────────

// TestTrapModifiers_WiderNets_RadiusCaltrops verifies caltrops radius 60 → 78.
func TestTrapModifiers_WiderNets_RadiusCaltrops(t *testing.T) {
	s := newTrapSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if u == nil {
		u = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	}
	u.PerkIDs = []string{"caltrops", "wider_nets"}

	stats, ok := s.DebugEffectiveTrapStats(u)
	if !ok {
		t.Fatal("DebugEffectiveTrapStats returned false")
	}

	assertFloatEq(t, "Radius", stats.Radius, 78.0) // 60 * 1.3
}

// TestTrapModifiers_WiderNets_ExplosiveBothRadii verifies that for
// explosive_trap, wider_nets scales both explosionRadius and triggerRadius.
func TestTrapModifiers_WiderNets_ExplosiveBothRadii(t *testing.T) {
	s := newTrapSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if u == nil {
		u = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	}
	u.PerkIDs = []string{"explosive_trap", "wider_nets"}

	stats, ok := s.DebugEffectiveTrapStats(u)
	if !ok {
		t.Fatal("DebugEffectiveTrapStats returned false")
	}

	assertFloatEq(t, "Radius (explosion)", stats.Radius, 104.0)       // 80 * 1.3
	assertFloatEq(t, "TriggerRadius", stats.TriggerRadius, 65.0)       // 50 * 1.3
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. rapid_deployment — cooldown scales by 0.7×; verified via cooldown reset
// ─────────────────────────────────────────────────────────────────────────────

// TestTrapModifiers_RapidDeployment_PlaceIntervalCaltrops verifies that
// placeIntervalSeconds 6 → 4.2 in DebugEffectiveTrapStats.
func TestTrapModifiers_RapidDeployment_PlaceIntervalCaltrops(t *testing.T) {
	s := newTrapSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if u == nil {
		u = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	}
	u.PerkIDs = []string{"caltrops", "rapid_deployment"}
	u.PerkState.LastCombatSeconds = 1.5
	u.PerkState.TrapPlaceCooldownRemaining = 0

	stats, ok := s.DebugEffectiveTrapStats(u)
	if !ok {
		t.Fatal("DebugEffectiveTrapStats returned false")
	}
	assertFloatEq(t, "PlaceInterval", stats.PlaceInterval, 4.2) // 6 * 0.7

	// Also verify TrapPlaceCooldownRemaining after a plant cycle.
	// tickTrapPlacementLocked only decays when the cooldown is > 0 at entry.
	// Starting at 0, the decay block is skipped, the trap plants, and the
	// cooldown is reset to the full scaled interval (4.2). No partial decay
	// is subtracted in the same tick.
	def := perkDefByID("caltrops")
	if def == nil {
		t.Fatal("caltrops perk def not found")
	}
	s.tickTrapPlacementLocked(u, def, 0.05)

	assertFloatEq(t, "CooldownRemaining after plant", u.PerkState.TrapPlaceCooldownRemaining, 4.2)
}

// ─────────────────────────────────────────────────────────────────────────────
// 5. amplified_effects on caltrops
// ─────────────────────────────────────────────────────────────────────────────

// TestTrapModifiers_AmplifiedEffects_Caltrops verifies:
//   - DamagePerSecond 3 → 4.05 (3 * 1.35)
//   - SlowMultiplier uses slow-amount math: 0.7 → 0.595
func TestTrapModifiers_AmplifiedEffects_Caltrops(t *testing.T) {
	s := newTrapSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if u == nil {
		u = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	}
	u.PerkIDs = []string{"caltrops", "amplified_effects"}

	stats, ok := s.DebugEffectiveTrapStats(u)
	if !ok {
		t.Fatal("DebugEffectiveTrapStats returned false")
	}

	assertFloatEq(t, "DamagePerSecond", stats.DamagePerSecond, 4.05)  // 3 * 1.35
	assertFloatEq(t, "SlowMultiplier", stats.SlowMultiplier, 0.595)   // 1 - (0.30 * 1.35)
}

// ─────────────────────────────────────────────────────────────────────────────
// 6. amplified_effects on explosive_trap
// ─────────────────────────────────────────────────────────────────────────────

// TestTrapModifiers_AmplifiedEffects_ExplosiveTrap verifies burstDamage rounds
// correctly: 35 * 1.35 = 47.25 → 47 (int(47.25 + 0.5) = 47).
func TestTrapModifiers_AmplifiedEffects_ExplosiveTrap(t *testing.T) {
	s := newTrapSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if u == nil {
		u = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	}
	u.PerkIDs = []string{"explosive_trap", "amplified_effects"}

	stats, ok := s.DebugEffectiveTrapStats(u)
	if !ok {
		t.Fatal("DebugEffectiveTrapStats returned false")
	}

	// 35 * 1.35 = 47.25 → int(47.25 + 0.5) = 47
	if stats.BurstDamage != 47 {
		t.Errorf("BurstDamage: got %d, want 47", stats.BurstDamage)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 7. amplified_effects on marker_trap — both markMultiplier and markDuration scale
// ─────────────────────────────────────────────────────────────────────────────

// TestTrapModifiers_AmplifiedEffects_MarkerTrap verifies:
//   - MarkMultiplier 0.20 → 0.27 (0.20 * 1.35)
//   - MarkDuration 4 → 5.4 (4 * 1.35)
func TestTrapModifiers_AmplifiedEffects_MarkerTrap(t *testing.T) {
	s := newTrapSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if u == nil {
		u = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	}
	u.PerkIDs = []string{"marker_trap", "amplified_effects"}

	stats, ok := s.DebugEffectiveTrapStats(u)
	if !ok {
		t.Fatal("DebugEffectiveTrapStats returned false")
	}

	assertFloatEq(t, "MarkMultiplier", stats.MarkMultiplier, 0.27) // 0.20 * 1.35
	assertFloatEq(t, "MarkDuration", stats.MarkDuration, 5.4)      // 4 * 1.35
}

// ─────────────────────────────────────────────────────────────────────────────
// 8. Stacking — all four Silver modifiers applied together on caltrops
// ─────────────────────────────────────────────────────────────────────────────

// TestTrapModifiers_AllSilverStack_Caltrops verifies multiplicative stacking:
// caltrops + extended_setup + wider_nets + rapid_deployment + amplified_effects.
func TestTrapModifiers_AllSilverStack_Caltrops(t *testing.T) {
	s := newTrapSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if u == nil {
		u = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	}
	u.PerkIDs = []string{"caltrops", "extended_setup", "wider_nets", "rapid_deployment", "amplified_effects"}

	stats, ok := s.DebugEffectiveTrapStats(u)
	if !ok {
		t.Fatal("DebugEffectiveTrapStats returned false")
	}

	// DurationSeconds: 12 * 1.5 = 18
	assertFloatEq(t, "DurationSeconds", stats.DurationSeconds, 18.0)
	// Radius: 60 * 1.3 = 78
	assertFloatEq(t, "Radius", stats.Radius, 78.0)
	// PlaceInterval: 6 * 0.7 = 4.2
	assertFloatEq(t, "PlaceInterval", stats.PlaceInterval, 4.2)
	// DamagePerSecond: 3 * 1.35 = 4.05
	assertFloatEq(t, "DamagePerSecond", stats.DamagePerSecond, 4.05)
	// SlowMultiplier: 1 - (0.30 * 1.35) = 0.595
	assertFloatEq(t, "SlowMultiplier", stats.SlowMultiplier, 0.595)
}

// ─────────────────────────────────────────────────────────────────────────────
// 9. End-to-end plant — planted Trap struct reflects modifier-scaled values
// ─────────────────────────────────────────────────────────────────────────────

// TestTrapModifiers_PlantEndToEnd_SnapshotScaled verifies that calling
// tickTrapPlacementLocked past the cooldown actually plants a Trap whose
// RemainingSeconds, Radius, and DamagePerSecond reflect the Silver modifiers.
func TestTrapModifiers_PlantEndToEnd_SnapshotScaled(t *testing.T) {
	s := newTrapSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if u == nil {
		u = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	}
	u.Visible = true
	u.PerkIDs = []string{"caltrops", "extended_setup", "wider_nets", "amplified_effects"}
	// Arm for immediate placement.
	u.PerkState.LastCombatSeconds = 1.5
	u.PerkState.TrapPlaceCooldownRemaining = 0

	def := perkDefByID("caltrops")
	if def == nil {
		t.Fatal("caltrops perk def not found")
	}

	trapsBefore := len(s.Traps)

	// Drive one tick past the (already-zero) cooldown; dt > 0 so decay runs.
	s.tickTrapPlacementLocked(u, def, 0.05)

	if len(s.Traps) != trapsBefore+1 {
		t.Fatalf("expected one new trap after placement tick, got %d total (was %d)", len(s.Traps), trapsBefore)
	}

	planted := s.Traps[len(s.Traps)-1]

	// RemainingSeconds: 12 * 1.5 = 18
	assertFloatEq(t, "planted.RemainingSeconds", planted.RemainingSeconds, 18.0)
	// Radius: 60 * 1.3 = 78
	assertFloatEq(t, "planted.Radius", planted.Radius, 78.0)
	// DamagePerSecond: 3 * 1.35 = 4.05
	assertFloatEq(t, "planted.DamagePerSecond", planted.DamagePerSecond, 4.05)
	// SlowMultiplier: 1 - (0.30 * 1.35) = 0.595
	assertFloatEq(t, "planted.SlowMultiplier", planted.SlowMultiplier, 0.595)
	// Ownership
	if planted.OwnerPlayerID != "p1" {
		t.Errorf("planted.OwnerPlayerID: got %q, want p1", planted.OwnerPlayerID)
	}
	if planted.TrapType != "caltrops" {
		t.Errorf("planted.TrapType: got %q, want caltrops", planted.TrapType)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// amplifySlow unit tests
// ─────────────────────────────────────────────────────────────────────────────

// TestAmplifySlow_SlowAmountMath exercises the slow-amount composition helper.
func TestAmplifySlow_SlowAmountMath(t *testing.T) {
	cases := []struct {
		name       string
		baseMult   float64
		effectMult float64
		want       float64
	}{
		{"no slow (baseMult=1.0)", 1.0, 1.35, 1.0},
		{"caltrops + amplified_effects", 0.7, 1.35, 0.595},
		{"extreme slow fully capped", 0.0, 2.0, 0.0},
		{"identity effectMult", 0.5, 1.0, 0.5},
	}
	for _, c := range cases {
		got := amplifySlow(c.baseMult, c.effectMult)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("amplifySlow(%v, %v) [%s]: got %.9f, want %.9f",
				c.baseMult, c.effectMult, c.name, got, c.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Perk def sanity checks — verify catalog loads correctly
// ─────────────────────────────────────────────────────────────────────────────

// TestSilverTrapPerkDefs_AllLoaded verifies all four new Silver perk IDs are
// present in the loaded catalog with the expected config keys.
func TestSilverTrapPerkDefs_AllLoaded(t *testing.T) {
	perks := []struct {
		id        string
		configKey string
		wantValue float64
	}{
		{"extended_setup", "durationMultiplier", 1.5},
		{"wider_nets", "radiusMultiplier", 1.3},
		{"rapid_deployment", "cooldownMultiplier", 0.7},
		{"amplified_effects", "effectMultiplier", 1.35},
	}
	for _, p := range perks {
		def := perkDefByID(p.id)
		if def == nil {
			t.Errorf("perk %q not found in catalog", p.id)
			continue
		}
		got, ok := def.Config[p.configKey]
		if !ok {
			t.Errorf("perk %q: config key %q missing", p.id, p.configKey)
			continue
		}
		if math.Abs(got-p.wantValue) > 1e-9 {
			t.Errorf("perk %q config[%q]: got %v, want %v", p.id, p.configKey, got, p.wantValue)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// explosive_chain — aftershock behavior
// ─────────────────────────────────────────────────────────────────────────────

// newExplosiveChainState returns a GameState with:
//   - player "p1" and player "enemy" registered
//   - an archer for "p1" with explosive_trap + explosive_chain
//   - a planted explosive_trap (via plantTrapLocked) at (400,400)
//   - the returned trap pointer for introspection
func newExplosiveChainState(t *testing.T) (s *GameState, owner *Unit, trap *Trap) {
	t.Helper()
	s = NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 17)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players["enemy"] = &Player{ID: "enemy", Resources: map[string]int{}}

	owner = s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if owner == nil {
		owner = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	}
	owner.Visible = true
	owner.PerkIDs = []string{"explosive_trap", "explosive_chain"}

	def := perkDefByID("explosive_trap")
	if def == nil {
		t.Fatal("explosive_trap perk def not found")
	}
	s.plantTrapLocked(owner, def)

	if len(s.Traps) == 0 {
		t.Fatal("plantTrapLocked did not create a trap")
	}
	trap = s.Traps[len(s.Traps)-1]
	return s, owner, trap
}

// spawnEnemyInRadius spawns a visible, alive enemy for "enemy" player at an
// offset from (cx, cy) that is within the given radius.
func spawnEnemyInRadius(t *testing.T, s *GameState, cx, cy, radius float64) *Unit {
	t.Helper()
	u := s.spawnPlayerUnitLocked("soldier", "enemy", "#e74c3c", protocol.Vec2{
		X: cx + radius*0.5,
		Y: cy,
	})
	u.Visible = true
	u.HP = 500
	u.MaxHP = 500
	return u
}

// TestExplosiveChain_AftershockDelaySnapshotted verifies that planting an
// explosive_trap with explosive_chain yields a trap with AftershockDelaySeconds
// equal to the perk config value (2s).
func TestExplosiveChain_AftershockDelaySnapshotted(t *testing.T) {
	s, _, trap := newExplosiveChainState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("explosive_chain")
	if def == nil {
		t.Fatal("explosive_chain perk def not found")
	}
	want := def.Config["aftershockDelaySeconds"]
	if math.Abs(trap.AftershockDelaySeconds-want) > 1e-9 {
		t.Errorf("AftershockDelaySeconds: got %.4f, want %.4f", trap.AftershockDelaySeconds, want)
	}
}

// TestExplosiveChain_AftershockFiresSecondBlast verifies the full sequence:
// 1. Enemy walks into trigger radius → first blast fires, trap becomes AftershockPending.
// 2. A second enemy (outside trigger radius) enters the blast radius but not the trigger.
// 3. After 2s countdown, the aftershock fires and hits the second enemy.
func TestExplosiveChain_AftershockFiresSecondBlast(t *testing.T) {
	s, _, trap := newExplosiveChainState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Place first enemy inside the trigger radius to fire initial blast.
	triggerEnemy := s.spawnPlayerUnitLocked("soldier", "enemy", "#e74c3c", protocol.Vec2{
		X: trap.X + trap.TriggerRadius*0.5,
		Y: trap.Y,
	})
	triggerEnemy.Visible = true
	triggerEnemy.HP = 500
	triggerEnemy.MaxHP = 500
	hpBefore1 := triggerEnemy.HP

	// Tick once to trigger the first blast.
	s.tickTrapEffectsLocked(0.05)

	if !trap.AftershockPending {
		t.Fatal("after first blast, trap should be AftershockPending")
	}
	// Triggered=true is the one-tick VFX flash: it is SET by the initial blast and
	// will be reset at the start of the next tickTrapEffectsLocked pass.
	if !trap.Triggered {
		t.Fatal("after first blast, trap.Triggered should be true (VFX flash for this tick)")
	}
	if triggerEnemy.HP >= hpBefore1 {
		t.Errorf("first blast did not deal damage to trigger enemy: HP unchanged at %d", triggerEnemy.HP)
	}

	// Now place a second enemy within blast radius but NOT trigger radius.
	// This enemy should only be hit by the aftershock.
	aftershockEnemy := s.spawnPlayerUnitLocked("soldier", "enemy", "#e74c3c", protocol.Vec2{
		X: trap.X + trap.TriggerRadius + 5, // outside trigger, inside explosion
		Y: trap.Y,
	})
	aftershockEnemy.Visible = true
	aftershockEnemy.HP = 500
	aftershockEnemy.MaxHP = 500
	hpBefore2 := aftershockEnemy.HP

	// Tick 2 seconds (40 ticks at dt=0.05) — aftershock should fire.
	// Break on PendingCull: that is set only when the final (aftershock) blast
	// fires, not on the initial blast. Triggered is a transient VFX flag that is
	// reset at the top of every tickTrapEffectsLocked pass, so it cannot be used
	// as a loop-break condition across multiple calls.
	for i := 0; i < 40; i++ {
		s.tickTrapEffectsLocked(0.05)
		if trap.PendingCull {
			break
		}
	}

	if !trap.PendingCull {
		t.Fatal("aftershock should have fired (PendingCull=true) within 2s")
	}
	if !trap.Triggered {
		t.Fatal("trap should be Triggered (VFX flash) on the aftershock tick")
	}
	if aftershockEnemy.HP >= hpBefore2 {
		t.Errorf("aftershock did not deal damage: HP unchanged at %d", aftershockEnemy.HP)
	}
}

// TestExplosiveChain_AftershockFiresEvenIfTriggerZoneEmpty verifies that after
// the initial blast, the aftershock fires even when no enemy is in the trigger
// radius — the aftershock is unconditional.
func TestExplosiveChain_AftershockFiresEvenIfTriggerZoneEmpty(t *testing.T) {
	s, _, trap := newExplosiveChainState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Trigger the first blast with an enemy in range.
	triggerEnemy := s.spawnPlayerUnitLocked("soldier", "enemy", "#e74c3c", protocol.Vec2{
		X: trap.X + trap.TriggerRadius*0.5,
		Y: trap.Y,
	})
	triggerEnemy.Visible = true
	triggerEnemy.HP = 500
	triggerEnemy.MaxHP = 500

	s.tickTrapEffectsLocked(0.05)
	if !trap.AftershockPending {
		t.Fatal("trap should be AftershockPending after first blast")
	}

	// Move the trigger enemy outside blast radius — trigger zone is now empty.
	triggerEnemy.X = trap.X + trap.Radius + 100

	// Place a NEW enemy inside the blast radius but not the trigger radius.
	newEnemy := s.spawnPlayerUnitLocked("soldier", "enemy", "#e74c3c", protocol.Vec2{
		X: trap.X + trap.TriggerRadius + 5,
		Y: trap.Y,
	})
	newEnemy.Visible = true
	newEnemy.HP = 500
	newEnemy.MaxHP = 500
	hpBefore := newEnemy.HP

	// Tick 2s — aftershock must fire unconditionally.
	// Break on PendingCull: set only when the final blast fires.
	for i := 0; i < 40; i++ {
		s.tickTrapEffectsLocked(0.05)
		if trap.PendingCull {
			break
		}
	}

	if !trap.PendingCull {
		t.Fatal("aftershock did not fire (PendingCull not set after 2s)")
	}
	if !trap.Triggered {
		t.Fatal("aftershock tick: Triggered must be true (VFX flash)")
	}
	if newEnemy.HP >= hpBefore {
		t.Errorf("aftershock should have hit newEnemy inside blast radius: HP unchanged at %d", newEnemy.HP)
	}
}

// TestExplosiveChain_NoAftershockWithoutPerk verifies that an explosive_trap
// without explosive_chain triggers once and is culled without an aftershock.
func TestExplosiveChain_NoAftershockWithoutPerk(t *testing.T) {
	s := newTrapSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	owner := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if owner == nil {
		owner = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	}
	owner.Visible = true
	owner.PerkIDs = []string{"explosive_trap"} // no explosive_chain

	def := perkDefByID("explosive_trap")
	if def == nil {
		t.Fatal("explosive_trap perk def not found")
	}
	s.plantTrapLocked(owner, def)
	trap := s.Traps[len(s.Traps)-1]

	if trap.AftershockDelaySeconds != 0 {
		t.Errorf("no explosive_chain: AftershockDelaySeconds should be 0, got %.4f", trap.AftershockDelaySeconds)
	}

	// Place enemy in trigger radius.
	enemy := s.spawnPlayerUnitLocked("soldier", "enemy", "#e74c3c", protocol.Vec2{
		X: trap.X + trap.TriggerRadius*0.5,
		Y: trap.Y,
	})
	enemy.Visible = true
	enemy.HP = 500
	enemy.MaxHP = 500

	s.tickTrapEffectsLocked(0.05)

	if trap.AftershockPending {
		t.Error("without explosive_chain, trap should NOT be AftershockPending after trigger")
	}
	if !trap.Triggered {
		t.Error("without explosive_chain, trap should be Triggered (culled) after blast")
	}
}

// TestExplosiveChain_TriggeredFlagVisibleOnBothBlasts verifies the two-tick VFX
// pipeline for explosive_chain, driven via the PRODUCTION Update path so that
// Snapshot is called after all three tick functions run (mirroring loop.go).
//
//  1. Initial blast tick (Update): Triggered=true in post-Update snapshot.
//  2. During aftershock countdown: Triggered=false in snapshots.
//  3. Aftershock blast tick (Update): Triggered=true in post-Update snapshot.
//  4. One tick after aftershock: trap absent from snapshot.
//
// Uses dt=0.05 (20 Hz) and aftershockDelaySeconds=2.0 → ~40 ticks between blasts.
func TestExplosiveChain_TriggeredFlagVisibleOnBothBlasts(t *testing.T) {
	const dt = 0.05

	s, _, trap := newExplosiveChainState(t)
	trapID := trap.ID

	// Spawn enemy inside trigger radius (lock is not held outside newExplosiveChainState).
	s.mu.Lock()
	enemy := s.spawnPlayerUnitLocked("soldier", "enemy", "#e74c3c", protocol.Vec2{
		X: trap.X + trap.TriggerRadius*0.5,
		Y: trap.Y,
	})
	enemy.Visible = true
	enemy.HP = 1000
	enemy.MaxHP = 1000
	s.mu.Unlock()

	// ── Tick 1: initial blast ───────────────────────────────────────────────
	// Update runs effects → banners → traps. Triggered=true must survive
	// tickTrapsLocked (two-phase gate). Snapshot is called after Update.
	s.Update(dt)
	snap1 := s.Snapshot()

	var ts1 *protocol.TrapSnapshot
	for i := range snap1.Traps {
		if snap1.Traps[i].ID == trapID {
			ts1 = &snap1.Traps[i]
			break
		}
	}
	if ts1 == nil {
		t.Fatal("tick 1 snapshot: trap missing — client would not see initial blast VFX")
	}
	if !ts1.Triggered {
		t.Error("tick 1 snapshot: triggered=false — client would miss initial blast VFX")
	}

	// ── Mid-countdown ticks: Triggered must be false ─────────────────────────
	// Run a few ticks without consuming the full delay to verify the flag resets.
	for i := 0; i < 3; i++ {
		s.Update(dt)
	}
	snapMid := s.Snapshot()
	var tsMid *protocol.TrapSnapshot
	for i := range snapMid.Traps {
		if snapMid.Traps[i].ID == trapID {
			tsMid = &snapMid.Traps[i]
			break
		}
	}
	if tsMid == nil {
		t.Fatal("mid-countdown snapshot: trap should still be present during aftershock wait")
	}
	if tsMid.Triggered {
		t.Error("mid-countdown snapshot: triggered=true during countdown — VFX would fire spuriously")
	}

	// ── Run remaining ticks until aftershock fires ───────────────────────────
	// We've already consumed 4 ticks (1 + 3) = 0.20s. aftershockDelaySeconds=2.0.
	// Drive up to 50 more ticks (2.5s); break as soon as the trap is gone from
	// the snapshot (which happens one tick AFTER the aftershock blast tick).
	var aftershockBlastSnap *protocol.TrapSnapshot
	aftershockFound := false
	prevSnap := snapMid
	for i := 0; i < 50; i++ {
		s.Update(dt)
		snap := s.Snapshot()

		// Find the trap in this snapshot.
		var cur *protocol.TrapSnapshot
		for j := range snap.Traps {
			if snap.Traps[j].ID == trapID {
				cur = &snap.Traps[j]
				break
			}
		}

		if cur != nil && cur.Triggered {
			// This is the aftershock blast tick.
			aftershockBlastSnap = cur
			aftershockFound = true
			// Verify previous snapshot had Triggered=false (no spurious flash).
			for j := range prevSnap.Traps {
				if prevSnap.Traps[j].ID == trapID && prevSnap.Traps[j].Triggered {
					t.Error("tick before aftershock blast: triggered=true prematurely")
				}
			}
			// Run one more tick to confirm cull.
			s.Update(dt)
			snapFinal := s.Snapshot()
			for _, ts := range snapFinal.Traps {
				if ts.ID == trapID {
					t.Error("tick after aftershock: trap still present — should be culled")
				}
			}
			break
		}
		prevSnap = snap
	}

	if !aftershockFound {
		t.Fatal("aftershock blast tick not observed — Triggered=true never appeared in a post-Update snapshot")
	}
	_ = aftershockBlastSnap
}

// TestExplosiveChain_LifetimeNotRaceCondition verifies that a trap with a very
// short durationSeconds that fires near end-of-life still completes its aftershock.
// The lifetime decay must be suppressed while AftershockPending is true.
func TestExplosiveChain_LifetimeNotRaceCondition(t *testing.T) {
	s, _, trap := newExplosiveChainState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Hack the remaining lifetime to nearly zero so it would normally expire next tick.
	trap.RemainingSeconds = 0.05

	// Place enemy in trigger radius to fire first blast.
	enemy := s.spawnPlayerUnitLocked("soldier", "enemy", "#e74c3c", protocol.Vec2{
		X: trap.X + trap.TriggerRadius*0.5,
		Y: trap.Y,
	})
	enemy.Visible = true
	enemy.HP = 500
	enemy.MaxHP = 500

	// Fire first blast.
	s.tickTrapEffectsLocked(0.05)
	if !trap.AftershockPending {
		t.Fatal("trap should be AftershockPending after first blast even with low RemainingSeconds")
	}

	// tickTrapsLocked must NOT cull the trap while AftershockPending.
	s.tickTrapsLocked(0.05)
	trapStillAlive := false
	for _, tr := range s.Traps {
		if tr.ID == trap.ID {
			trapStillAlive = true
			break
		}
	}
	if !trapStillAlive {
		t.Fatal("trap was culled while AftershockPending — lifetime should not decay during aftershock window")
	}

	// Tick the full 2s aftershock delay.
	for i := 0; i < 40; i++ {
		s.tickTrapEffectsLocked(0.05)
		s.tickTrapsLocked(0.05)
		if trap.Triggered {
			break
		}
	}

	if !trap.Triggered {
		t.Fatal("trap did not trigger after aftershock delay")
	}
}
