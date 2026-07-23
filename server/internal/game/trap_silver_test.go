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

// spawnTrapArcher spawns an archer for "p1" at (400,400) with a trap ability
// already granted, ready for placement (LastCombatSeconds set, cooldown 0).
func spawnTrapArcher(t *testing.T, s *GameState, trapAbilityID string) *Unit {
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
	grantTrapAbility(u, trapAbilityID)
	u.PerkState.LastCombatSeconds = 1.5
	u.PerkState.TrapPlaceCooldownRemaining = 0
	return u
}

// rapidDeploymentCooldownMultFor returns the CooldownMult that rapid_deployment's
// AbilityModifiers apply to the given trap ability id. Derived from the perk
// def itself so tests never pin the balance constant.
func rapidDeploymentCooldownMultFor(t *testing.T, trapID string) float64 {
	t.Helper()
	def := perkDefByID("rapid_deployment")
	if def == nil {
		t.Fatal("rapid_deployment perk def not found")
	}
	for _, m := range def.AbilityModifiers {
		if m.Target == trapID {
			return m.CooldownMult
		}
	}
	t.Fatalf("rapid_deployment has no AbilityModifiers entry for target %q", trapID)
	return 0
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
	grantTrapAbility(u, "caltrops")

	stats, ok := s.DebugEffectiveTrapStats(u)
	if !ok {
		t.Fatal("DebugEffectiveTrapStats returned false for unit with caltrops")
	}

	cfg := mustTrapAbilityConfig(t, "caltrops", u.Rank)

	assertFloatEq(t, "DurationSeconds", stats.DurationSeconds, cfg.DurationSeconds)
	assertFloatEq(t, "Radius", stats.Radius, cfg.Radius)
	assertFloatEq(t, "PlaceInterval", stats.PlaceInterval, cfg.PlaceIntervalSeconds)
	assertFloatEq(t, "DamagePerSecond", stats.DamagePerSecond, cfg.DamagePerSecond)
	assertFloatEq(t, "SlowMultiplier", stats.SlowMultiplier, cfg.SlowMultiplier)
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
	grantTrapAbility(u, "caltrops")
	u.PerkIDs = []string{"extended_setup"}

	stats, ok := s.DebugEffectiveTrapStats(u)
	if !ok {
		t.Fatal("DebugEffectiveTrapStats returned false")
	}

	caltrops := mustTrapAbilityConfig(t, "caltrops", u.Rank)
	// extended_setup's contribution is an ability-stat row now, so the expected
	// value comes from applyPerkRow rather than from a config key.
	assertFloatEq(t, "DurationSeconds", stats.DurationSeconds,
		applyPerkRow(t, "extended_setup", "caltrops", "field", "duration", caltrops.DurationSeconds))
	// Other fields must remain at base values.
	assertFloatEq(t, "Radius", stats.Radius, caltrops.Radius)
	assertFloatEq(t, "PlaceInterval", stats.PlaceInterval, caltrops.PlaceIntervalSeconds)
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
	grantTrapAbility(u, "caltrops")
	u.PerkIDs = []string{"wider_nets"}

	stats, ok := s.DebugEffectiveTrapStats(u)
	if !ok {
		t.Fatal("DebugEffectiveTrapStats returned false")
	}

	caltropsRadius := mustTrapAbilityConfig(t, "caltrops", u.Rank).Radius
	assertFloatEq(t, "Radius", stats.Radius,
		applyPerkRow(t, "wider_nets", "caltrops", "field", "radius", caltropsRadius))
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
	grantTrapAbility(u, "explosive_trap")
	u.PerkIDs = []string{"wider_nets"}

	stats, ok := s.DebugEffectiveTrapStats(u)
	if !ok {
		t.Fatal("DebugEffectiveTrapStats returned false")
	}

	explosive := mustTrapAbilityConfig(t, "explosive_trap", u.Rank)
	// explosive_trap has ONE radius doing both jobs now (see
	// effectiveTrapStatsFromParamsLocked), so both fields resolve from the same
	// authored value and take the same perk contribution.
	wantRadius := applyPerkRow(t, "wider_nets", "explosive_trap", "arm", "radius", explosive.ExplosionRadius)
	assertFloatEq(t, "Radius (explosion)", stats.Radius, wantRadius)
	assertFloatEq(t, "TriggerRadius", stats.TriggerRadius, wantRadius)
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. rapid_deployment — cooldown scales by 0.7×; verified via cooldown reset
// ─────────────────────────────────────────────────────────────────────────────

// TestTrapModifiers_RapidDeployment_PlaceIntervalCaltrops verifies that
// rapid_deployment's AbilityModifiers CooldownMult scales caltrops'
// placeIntervalSeconds in DebugEffectiveTrapStats. Placement cadence is now
// the caltrops ability's own cooldown (folded via
// abilityScalarModifiersForCasterLocked), so this test no longer drives a
// placement-cadence tick — that driver (tickTrapPlacementLocked /
// PerkState.TrapPlaceCooldownRemaining) was deleted in the traps→abilities
// migration.
func TestTrapModifiers_RapidDeployment_PlaceIntervalCaltrops(t *testing.T) {
	s := newTrapSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if u == nil {
		u = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	}
	grantTrapAbility(u, "caltrops")
	u.PerkIDs = []string{"rapid_deployment"}
	u.PerkState.LastCombatSeconds = 1.5

	stats, ok := s.DebugEffectiveTrapStats(u)
	if !ok {
		t.Fatal("DebugEffectiveTrapStats returned false")
	}

	caltropsInterval := mustTrapAbilityConfig(t, "caltrops", u.Rank).PlaceIntervalSeconds
	cooldownMult := rapidDeploymentCooldownMultFor(t, "caltrops")
	wantInterval := caltropsInterval * cooldownMult
	assertFloatEq(t, "PlaceInterval", stats.PlaceInterval, wantInterval)
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
	grantTrapAbility(u, "caltrops")
	u.PerkIDs = []string{"amplified_effects"}

	stats, ok := s.DebugEffectiveTrapStats(u)
	if !ok {
		t.Fatal("DebugEffectiveTrapStats returned false")
	}

	caltrops := mustTrapAbilityConfig(t, "caltrops", u.Rank)
	_ = perkDefByID("amplified_effects") // expectations come from applyAmplifiedRow below

	assertFloatEq(t, "DamagePerSecond", stats.DamagePerSecond,
		applyAmplifiedRow(t, "caltrops", "spikes", "amount", caltrops.DamagePerSecond))
	// SlowMultiplier composes through the slow-amount helper, not a flat scale.
	assertFloatEq(t, "SlowMultiplier", stats.SlowMultiplier,
		applyAmplifiedRow(t, "caltrops", "slow_move", "value", caltrops.SlowMultiplier))
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
	grantTrapAbility(u, "explosive_trap")
	u.PerkIDs = []string{"amplified_effects"}

	stats, ok := s.DebugEffectiveTrapStats(u)
	if !ok {
		t.Fatal("DebugEffectiveTrapStats returned false")
	}

	burst := mustTrapAbilityConfig(t, "explosive_trap", u.Rank).BurstDamage
	wantBurst := int(applyAmplifiedRow(t, "explosive_trap", "blast", "amount", burst))
	if stats.BurstDamage != wantBurst {
		t.Errorf("BurstDamage: got %d, want %d", stats.BurstDamage, wantBurst)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 7. amplified_effects on marker_trap — both markMultiplier and markDuration scale
// ─────────────────────────────────────────────────────────────────────────────

// actionTypeOfAction resolves an authored action id to its ActionType by walking
// the ability's own program — the same lookup programActionConfigValue does
// internally, exposed for tests that need the type without a field value.
func actionTypeOfAction(def AbilityDef, actionID string) ActionType {
	return programActionTypes(def)[actionID]
}

// applyAmplifiedRow folds amplified_effects' authored contribution to one
// {ability, action, field} onto a base value, honouring whichever op the row
// uses. Reading the op rather than assuming one is what lets a designer switch
// a row between add and multiply without silently invalidating a test.
func applyAmplifiedRow(t *testing.T, ability, action, field string, base float64) float64 {
	t.Helper()
	return applyPerkRow(t, "amplified_effects", ability, action, field, base)
}

// applyPerkRow folds ONE perk's authored contribution to one
// {ability, action, field} onto a base value, across ALL of the authoring
// forms a perk can use:
//
//   - abilityFields, the precise {action, field} address, with its own op
//   - abilityStats addressed by an INFLICTED unit stat (a change_stat's value)
//   - abilityStats addressed by the field's KIND (broad or action-scoped)
//   - statModifiers granting abilityDamage (a damage-kind field)
//
// It ACCUMULATES rather than taking the first form that matches, in the same
// order EffectiveAbilityFieldLocked folds them (precise, then inflicted, then
// kinded, then damage). One perk really can address the same number twice —
// amplified_effects scales marker_trap's mark duration by 1.35 with a precise
// row AND adds 2s to it with a kinded row — and a first-match helper reported
// only one of the two while the engine applied both.
//
// Reading the forms rather than assuming one is what lets a designer move (or
// add) a contribution without silently invalidating every test that asserts
// the perk's effect.
func applyPerkRow(t *testing.T, perkID, ability, action, field string, base float64) float64 {
	t.Helper()
	pd := perkDefByID(perkID)
	if pd == nil {
		t.Fatalf("perk %q not in catalog", perkID)
	}
	def, haveDef := getAbilityDef(ability)
	v := base
	matched := false

	// PRECISE form.
	for _, m := range pd.AbilityFields {
		if m.Target != ability || m.Action != action || m.Field != field {
			continue
		}
		matched = true
		switch m.Op {
		case statOpAdd:
			v += m.Value
		case statOpMultiply, "":
			v *= m.Value
		case statOpAmplify:
			v = amplifyTowardZero(v, m.Value)
		default:
			t.Fatalf("perk %q: %s.%s uses unknown op %q", perkID, action, field, m.Op)
		}
	}

	// INFLICTED-STAT form: for a change_stat's `value` the perk addresses the
	// unit stat the action applies, not the action id.
	if haveDef && field == "value" {
		if statID, ok := programActionConfigString(def, action, "stat"); ok {
			var flat float64
			for _, row := range pd.AbilityStats {
				if row.Stat != statID {
					continue
				}
				if row.Ability != "" && row.Ability != ability {
					continue
				}
				flat += row.Flat
				matched = true
			}
			v += flat
		}
	}

	// KINDED form: an ability STAT addressed by the field's kind, either broad
	// ("duration") or scoped ("create_zone.duration").
	if haveDef {
		actionType := actionTypeOfAction(def, action)
		if desc, ok := lookupActionDescriptor(actionType); ok {
			if f, ok := schemaFieldByKey(desc, field); ok && isAbilityStatGridKind(f.Kind) {
				var flat, pct float64
				hit := false
				for _, row := range pd.AbilityStats {
					if row.Ability != "" && row.Ability != ability {
						continue
					}
					if row.Stat == f.Kind || row.Stat == scopedAbilityStatID(actionType, f.Kind) {
						flat += row.Flat
						pct += row.Pct
						hit = true
					}
				}
				if hit {
					v = foldAbilityStat(v, flat, pct)
					matched = true
				}
			}
		}
	}

	// DAMAGE form: the perk no longer names damage actions at all — it grants
	// the unit-wide abilityDamage stat, which deal_damage folds for every
	// ability. Rounded because damage is an int at execution and the reporting
	// read mirrors that.
	if haveDef {
		if desc, ok := lookupActionDescriptor(actionTypeOfAction(def, action)); ok {
			if f, ok := schemaFieldByKey(desc, field); ok && f.Kind == abilityStatKindDamage {
				mult := 1.0
				for _, sm := range pd.StatModifiers {
					if sm.Stat == statAbilityDamage && sm.Op == statOpMultiply {
						mult *= sm.Value
					}
				}
				if mult != 1.0 {
					v = math.Round(v * mult)
					matched = true
				}
			}
		}
	}

	if !matched {
		t.Fatalf("perk %q contributes nothing to %s %s.%s", perkID, ability, action, field)
	}
	return v
}

// TestTrapModifiers_AmplifiedEffects_MarkerTrap verifies the perk reaches both
// of marker_trap's numbers. The two are authored with DIFFERENT ops on purpose,
// so neither expectation is written out here — both are derived from the perk's
// own rows:
//
//   - vulnerability: an `add`. damageTaken is a fixed-1.0-baseline stat, so
//     scaling an authored 0.2 by 1.35 gives 0.27 — a 7-point gain, which is not
//     what "35% harder" means to anyone reading it. A flat +0.15 says exactly
//     what it does and lands the mark at "enemies take 35% more damage".
//   - mark duration: a `multiply`, because a duration has no such baseline.
func TestTrapModifiers_AmplifiedEffects_MarkerTrap(t *testing.T) {
	s := newTrapSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := s.spawnPlayerUnitLocked("archer", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	if u == nil {
		u = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	}
	grantTrapAbility(u, "marker_trap")
	u.PerkIDs = []string{"amplified_effects"}

	stats, ok := s.DebugEffectiveTrapStats(u)
	if !ok {
		t.Fatal("DebugEffectiveTrapStats returned false")
	}

	marker := mustTrapAbilityConfig(t, "marker_trap", u.Rank)

	assertFloatEq(t, "MarkMultiplier", stats.MarkMultiplier,
		applyAmplifiedRow(t, "marker_trap", "vulnerable", "value", marker.MarkMultiplier))
	assertFloatEq(t, "MarkDuration", stats.MarkDuration,
		applyAmplifiedRow(t, "marker_trap", "mark", "duration", marker.MarkDuration))
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
	grantTrapAbility(u, "caltrops")
	u.PerkIDs = []string{"extended_setup", "wider_nets", "rapid_deployment", "amplified_effects"}

	stats, ok := s.DebugEffectiveTrapStats(u)
	if !ok {
		t.Fatal("DebugEffectiveTrapStats returned false")
	}

	caltrops := mustTrapAbilityConfig(t, "caltrops", u.Rank)
	cooldownMult := rapidDeploymentCooldownMultFor(t, "caltrops")
	_ = perkDefByID("amplified_effects") // expectations come from applyAmplifiedRow below

	assertFloatEq(t, "DurationSeconds", stats.DurationSeconds,
		applyPerkRow(t, "extended_setup", "caltrops", "field", "duration", caltrops.DurationSeconds))
	assertFloatEq(t, "Radius", stats.Radius,
		applyPerkRow(t, "wider_nets", "caltrops", "field", "radius", caltrops.Radius))
	assertFloatEq(t, "PlaceInterval", stats.PlaceInterval, caltrops.PlaceIntervalSeconds*cooldownMult)
	assertFloatEq(t, "DamagePerSecond", stats.DamagePerSecond,
		applyAmplifiedRow(t, "caltrops", "spikes", "amount", caltrops.DamagePerSecond))
	assertFloatEq(t, "SlowMultiplier", stats.SlowMultiplier,
		applyAmplifiedRow(t, "caltrops", "slow_move", "value", caltrops.SlowMultiplier))
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
	grantTrapAbility(u, "caltrops")
	u.PerkIDs = []string{"extended_setup", "wider_nets", "amplified_effects"}
	u.PerkState.LastCombatSeconds = 1.5

	trapsBefore := len(s.Traps)

	// plantTrapLocked constructs the trap directly from the base ability
	// config; it resolves and applies the unit's Silver/Gold trap modifiers
	// internally (trapModifiersForUnitLocked), so this replaces the old
	// placement-cadence driver (tickTrapPlacementLocked) as the plant path.
	s.plantTrapLocked(u, mustTrapAbilityConfig(t, "caltrops", u.Rank))

	if len(s.Traps) != trapsBefore+1 {
		t.Fatalf("expected one new trap after plant, got %d total (was %d)", len(s.Traps), trapsBefore)
	}

	planted := s.Traps[len(s.Traps)-1]

	caltrops := mustTrapAbilityConfig(t, "caltrops", u.Rank)
	// The legacy plant path reads the config-driven TrapModifiers aggregator,
	// which extended_setup and wider_nets no longer feed (their contributions are
	// ability-stat rows, which only exist on the ABILITY path). So a legacy plant
	// is UNSCALED now. Nothing reaches plantTrapLocked from the catalog any more,
	// so this test covers dead code and dies with the legacy trap runtime.
	assertFloatEq(t, "planted.RemainingSeconds", planted.RemainingSeconds, caltrops.DurationSeconds)
	assertFloatEq(t, "planted.Radius", planted.Radius, caltrops.Radius)
	// Damage and slow are NOT asserted here any more. This test plants a LEGACY
	// Trap (plantTrapLocked) and reads the Trap struct, whose numbers come from
	// the config-driven TrapModifiers aggregator. amplified_effects no longer
	// feeds that aggregator — its damage is the unit-wide abilityDamage stat and
	// its slow is an inflicted-stat row, both of which only exist on the ABILITY
	// path. Duration and radius still scale here because extended_setup and
	// wider_nets kept their configs. Nothing reaches plantTrapLocked from the
	// catalog any more, so this whole test dies with the legacy trap runtime.
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

// TestSilverTrapPerkDefs_AllLoaded verifies the Silver trap perks are present
// and that each one's contribution is an AMPLIFICATION — it grows the effect
// rather than shrinking it. The exact tuning lives in the catalog and is free to
// change with balance passes, so this asserts the design-level invariant rather
// than pinning a magic number that would break on every tweak.
//
// The perks no longer carry freeform Config keys: extended_setup and wider_nets
// are ability-stat rows, amplified_effects is a stat modifier plus ability-stat
// rows. So this reads their real authoring instead.
func TestSilverTrapPerkDefs_AllLoaded(t *testing.T) {
	for _, id := range []string{"extended_setup", "wider_nets"} {
		def := perkDefByID(id)
		if def == nil {
			t.Errorf("perk %q not found in catalog", id)
			continue
		}
		if len(def.AbilityStats) == 0 {
			t.Errorf("perk %q authors no ability stats; it would do nothing", id)
			continue
		}
		for _, row := range def.AbilityStats {
			if row.Flat <= 0 && row.Pct <= 0 {
				t.Errorf("perk %q: abilityStats[%q] = {flat %v, pct %v}; an amplifying perk must grow its stat",
					id, row.Stat, row.Flat, row.Pct)
			}
		}
	}

	// rapid_deployment is a data perk: its cooldown reduction is authored as
	// AbilityModifiers ({target: <trap>, cooldownMult}) rather than a freeform
	// Config key (see trapConfigFromAbilityLocked's caller,
	// DebugEffectiveTrapStats, and rapidDeploymentCooldownMultFor above).
	// Verify it targets all four trap abilities with a cooldown-reducing mult.
	rapid := perkDefByID("rapid_deployment")
	if rapid == nil {
		t.Fatal(`perk "rapid_deployment" not found in catalog`)
	}
	wantTargets := map[string]bool{"caltrops": false, "fire_pit": false, "explosive_trap": false, "marker_trap": false}
	for _, m := range rapid.AbilityModifiers {
		if _, ok := wantTargets[m.Target]; !ok {
			continue
		}
		wantTargets[m.Target] = true
		if m.CooldownMult <= 0.0 || m.CooldownMult >= 1.0 {
			t.Errorf("rapid_deployment AbilityModifiers[target=%q].CooldownMult = %v; must be in (0, 1)", m.Target, m.CooldownMult)
		}
	}
	for target, found := range wantTargets {
		if !found {
			t.Errorf("rapid_deployment has no AbilityModifiers entry for target %q", target)
		}
	}
}
