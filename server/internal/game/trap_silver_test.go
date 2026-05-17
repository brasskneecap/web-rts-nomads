package game

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// hasExplosionEffectAt reports whether an "explosion" EffectSnapshot exists
// in the active set whose fallback position is within ~1px of (x, y). Used by
// explosive_chain / overload_protocol aftershock tests to verify the new
// sprite-based detonation visual landed at the expected coordinates.
func hasExplosionEffectAt(s *GameState, x, y float64) bool {
	const tolSq = 1.0
	for _, e := range s.activeEffects {
		if e.Name != "explosion" {
			continue
		}
		dx := e.FallbackX - x
		dy := e.FallbackY - y
		if dx*dx+dy*dy <= tolSq {
			return true
		}
	}
	return false
}

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

	caltrops := perkDefByID("caltrops").Config
	durMult := perkDefByID("extended_setup").Config["durationMultiplier"]

	// extended_setup scales durationSeconds by durationMultiplier.
	assertFloatEq(t, "DurationSeconds", stats.DurationSeconds, caltrops["durationSeconds"]*durMult)
	// Other fields must remain at base values.
	assertFloatEq(t, "Radius", stats.Radius, caltrops["radius"])
	assertFloatEq(t, "PlaceInterval", stats.PlaceInterval, caltrops["placeIntervalSeconds"])
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. wider_nets — radius scales by 1.5×; explosive also scales triggerRadius
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

	caltropsRadius := perkDefByID("caltrops").Config["radius"]
	widerNets := perkDefByID("wider_nets").Config["radiusMultiplier"]
	assertFloatEq(t, "Radius", stats.Radius, caltropsRadius*widerNets)
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

	explosive := perkDefByID("explosive_trap").Config
	widerNets := perkDefByID("wider_nets").Config["radiusMultiplier"]
	assertFloatEq(t, "Radius (explosion)", stats.Radius, explosive["explosionRadius"]*widerNets)
	assertFloatEq(t, "TriggerRadius", stats.TriggerRadius, explosive["triggerRadius"]*widerNets)
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

	caltropsInterval := perkDefByID("caltrops").Config["placeIntervalSeconds"]
	cooldownMult := perkDefByID("rapid_deployment").Config["cooldownMultiplier"]
	wantInterval := caltropsInterval * cooldownMult
	assertFloatEq(t, "PlaceInterval", stats.PlaceInterval, wantInterval)

	// Spawn a hostile inside the trapper's AttackRange so the placement gate
	// fires (idle trappers no longer drop traps without an enemy nearby).
	hostile := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: u.X + u.AttackRange*0.5, Y: u.Y})
	if hostile == nil {
		t.Fatal("hostile spawn failed")
	}

	// Verify TrapPlaceCooldownRemaining after a plant cycle.
	// tickTrapPlacementLocked only decays when the cooldown is > 0 at entry.
	// Starting at 0, the decay block is skipped, the trap plants, and the
	// cooldown is reset to the full scaled interval. No partial decay
	// is subtracted in the same tick.
	def := perkDefByID("caltrops")
	if def == nil {
		t.Fatal("caltrops perk def not found")
	}
	s.tickTrapPlacementLocked(u, def, 0.05)

	assertFloatEq(t, "CooldownRemaining after plant", u.PerkState.TrapPlaceCooldownRemaining, wantInterval)
}

// ─────────────────────────────────────────────────────────────────────────────
// 5. amplified_effects on caltrops
// ─────────────────────────────────────────────────────────────────────────────

// TestTrapModifiers_AmplifiedEffects_Caltrops verifies:
//   - DamagePerSecond 3 → 4.05 (3 * 1.35)
//   - SlowMultiplier uses slow-amount math: 0.35 → 0.1225
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

	caltrops := perkDefByID("caltrops").Config
	effectMult := perkDefByID("amplified_effects").Config["effectMultiplier"]

	assertFloatEq(t, "DamagePerSecond", stats.DamagePerSecond, caltrops["damagePerSecond"]*effectMult)
	// SlowMultiplier composes through the slow-amount helper, not a flat scale.
	assertFloatEq(t, "SlowMultiplier", stats.SlowMultiplier, amplifySlow(caltrops["slowMultiplier"], effectMult))
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

	burst := perkDefByID("explosive_trap").Config["burstDamage"]
	effectMult := perkDefByID("amplified_effects").Config["effectMultiplier"]
	wantBurst := int(burst*effectMult + 0.5)
	if stats.BurstDamage != wantBurst {
		t.Errorf("BurstDamage: got %d, want %d", stats.BurstDamage, wantBurst)
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

	marker := perkDefByID("marker_trap").Config
	effectMult := perkDefByID("amplified_effects").Config["effectMultiplier"]

	assertFloatEq(t, "MarkMultiplier", stats.MarkMultiplier, marker["markMultiplier"]*effectMult)
	assertFloatEq(t, "MarkDuration", stats.MarkDuration, marker["markDuration"]*effectMult)
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

	caltrops := perkDefByID("caltrops").Config
	durMult := perkDefByID("extended_setup").Config["durationMultiplier"]
	radiusMult := perkDefByID("wider_nets").Config["radiusMultiplier"]
	cooldownMult := perkDefByID("rapid_deployment").Config["cooldownMultiplier"]
	effectMult := perkDefByID("amplified_effects").Config["effectMultiplier"]

	assertFloatEq(t, "DurationSeconds", stats.DurationSeconds, caltrops["durationSeconds"]*durMult)
	assertFloatEq(t, "Radius", stats.Radius, caltrops["radius"]*radiusMult)
	assertFloatEq(t, "PlaceInterval", stats.PlaceInterval, caltrops["placeIntervalSeconds"]*cooldownMult)
	assertFloatEq(t, "DamagePerSecond", stats.DamagePerSecond, caltrops["damagePerSecond"]*effectMult)
	assertFloatEq(t, "SlowMultiplier", stats.SlowMultiplier, amplifySlow(caltrops["slowMultiplier"], effectMult))
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

	// Spawn a hostile inside the trapper's AttackRange so the placement gate
	// fires (idle trappers no longer drop traps without an enemy nearby).
	hostile := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: u.X + u.AttackRange*0.5, Y: u.Y})
	if hostile == nil {
		t.Fatal("hostile spawn failed")
	}

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

	caltrops := perkDefByID("caltrops").Config
	durMult := perkDefByID("extended_setup").Config["durationMultiplier"]
	radiusMult := perkDefByID("wider_nets").Config["radiusMultiplier"]
	effectMult := perkDefByID("amplified_effects").Config["effectMultiplier"]

	assertFloatEq(t, "planted.RemainingSeconds", planted.RemainingSeconds, caltrops["durationSeconds"]*durMult)
	assertFloatEq(t, "planted.Radius", planted.Radius, caltrops["radius"]*radiusMult)
	assertFloatEq(t, "planted.DamagePerSecond", planted.DamagePerSecond, caltrops["damagePerSecond"]*effectMult)
	assertFloatEq(t, "planted.SlowMultiplier", planted.SlowMultiplier, amplifySlow(caltrops["slowMultiplier"], effectMult))
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
// present in the loaded catalog with their expected config keys. The exact
// tuning magnitudes live in the catalog JSON and are free to change with
// balance passes, so this asserts the design-level invariant for each
// multiplier (amplifiers stay > 1; the cooldown reducer stays in (0, 1))
// rather than pinning a magic number that would break on every tweak.
func TestSilverTrapPerkDefs_AllLoaded(t *testing.T) {
	perks := []struct {
		id        string
		configKey string
		// amplify=true → value must be > 1 (extends/grows the effect).
		// amplify=false → value must be in (0, 1) (shrinks the cooldown).
		amplify bool
	}{
		{"extended_setup", "durationMultiplier", true},
		{"wider_nets", "radiusMultiplier", true},
		{"rapid_deployment", "cooldownMultiplier", false},
		{"amplified_effects", "effectMultiplier", true},
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
		if p.amplify {
			if got <= 1.0 {
				t.Errorf("perk %q config[%q] = %v; an amplifying multiplier must be > 1", p.id, p.configKey, got)
			}
		} else {
			if got <= 0.0 || got >= 1.0 {
				t.Errorf("perk %q config[%q] = %v; a cooldown-reducing multiplier must be in (0, 1)", p.id, p.configKey, got)
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// explosive_chain — aftershock behavior
// ─────────────────────────────────────────────────────────────────────────────

// newExplosiveChainState returns a GameState with:
//   - player "p1" and the wave-enemy faction (enemyPlayerID) registered as the
//     hostile party — two real players are allies under playersAreHostile
//   - an archer for "p1" with explosive_trap + explosive_chain
//   - a planted explosive_trap (via plantTrapLocked) at (400,400)
//   - the returned trap pointer for introspection
func newExplosiveChainState(t *testing.T) (s *GameState, owner *Unit, trap *Trap) {
	t.Helper()
	s = NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 17)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

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

// spawnEnemyInRadius spawns a visible, alive enemy under the wave-enemy
// faction (enemyPlayerID) at an offset from (cx, cy) within the given radius.
func spawnEnemyInRadius(t *testing.T, s *GameState, cx, cy, radius float64) *Unit {
	t.Helper()
	u := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{
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
	triggerEnemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{
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
	aftershockEnemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{
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
	// Aftershock visual is now the generic sprite-based "explosion" effect
	// instead of re-flashing the trap's own Triggered animation.
	if !hasExplosionEffectAt(s, trap.X, trap.Y) {
		t.Fatal("aftershock tick: expected an 'explosion' EffectSnapshot at the trap's position")
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
	triggerEnemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{
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
	newEnemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{
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
	if !hasExplosionEffectAt(s, trap.X, trap.Y) {
		t.Fatal("aftershock tick: expected an 'explosion' EffectSnapshot at the trap's position")
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
	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{
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

// TestExplosiveChain_InitialBlastTriggeredAndAftershockEmitsEffect verifies
// the two-blast VFX pipeline driven via the PRODUCTION Update path so that
// Snapshot is called after all three tick functions run (mirroring loop.go):
//
//  1. Initial blast tick: trap.Triggered=true in post-Update snapshot.
//  2. During aftershock countdown: Triggered=false; no aftershock effect yet.
//  3. Aftershock blast tick: trap.Triggered stays false; an "explosion"
//     EffectSnapshot appears at the trap's position (sprite-based detonation
//     replaces re-flashing the trap's own animation).
//  4. One tick after aftershock: trap absent from snapshot.
//
// Uses dt=0.05 (20 Hz) and aftershockDelaySeconds=2.0 → ~40 ticks between blasts.
func TestExplosiveChain_InitialBlastTriggeredAndAftershockEmitsEffect(t *testing.T) {
	const dt = 0.05

	s, _, trap := newExplosiveChainState(t)
	trapID := trap.ID
	trapX, trapY := trap.X, trap.Y

	// Spawn enemy inside trigger radius (lock is not held outside newExplosiveChainState).
	s.mu.Lock()
	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{
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

	// ── Mid-countdown ticks: trap is hidden during the aftershock wait so
	// the client doesn't see an "intact" trap sprite for 2 seconds after it
	// already detonated.
	for i := 0; i < 3; i++ {
		s.Update(dt)
	}
	snapMid := s.Snapshot()
	for i := range snapMid.Traps {
		if snapMid.Traps[i].ID == trapID {
			t.Errorf("mid-countdown snapshot: trap should be hidden during aftershock wait, got triggered=%v", snapMid.Traps[i].Triggered)
		}
	}

	// ── Run remaining ticks until aftershock fires ───────────────────────────
	// Aftershock signals via an "explosion" EffectSnapshot, NOT trap.Triggered.
	// Trap is culled one tick after the aftershock blast.
	aftershockFound := false
	for i := 0; i < 50; i++ {
		s.Update(dt)

		s.mu.Lock()
		hasEffect := hasExplosionEffectAt(s, trapX, trapY)
		s.mu.Unlock()

		if hasEffect {
			aftershockFound = true

			// Verify trap.Triggered did NOT flip on the aftershock — replacing
			// the trap's own re-flash is the whole point of the migration.
			snap := s.Snapshot()
			for j := range snap.Traps {
				if snap.Traps[j].ID == trapID && snap.Traps[j].Triggered {
					t.Error("aftershock tick: trap.Triggered=true — should be replaced by 'explosion' effect")
				}
			}
			break
		}
	}

	if !aftershockFound {
		t.Fatal("aftershock blast tick not observed — no 'explosion' EffectSnapshot appeared")
	}
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
	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{
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

	// Tick the full 2s aftershock delay. Aftershock no longer flips
	// trap.Triggered — it emits an "explosion" EffectSnapshot — so break on
	// PendingCull (set when the aftershock blast fires).
	for i := 0; i < 40; i++ {
		s.tickTrapEffectsLocked(0.05)
		s.tickTrapsLocked(0.05)
		if trap.PendingCull {
			break
		}
	}

	if !trap.PendingCull {
		t.Fatal("trap did not detonate aftershock after delay (PendingCull not set)")
	}
	if !hasExplosionEffectAt(s, trap.X, trap.Y) {
		t.Fatal("aftershock tick: expected an 'explosion' EffectSnapshot at the trap's position")
	}
}
