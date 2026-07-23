package game

import (
	"math"
	"testing"
)

// castMarkerTrapOnto casts a marker trap at `victim` and runs the sim long
// enough for the zone to spawn and its occupancy pass to fire. Goes through the
// real cast path — the bug this file exists for was invisible to anything that
// applied the status directly.
func castMarkerTrapOnto(t *testing.T, s *GameState, caster, victim *Unit) {
	t.Helper()
	grantTrapAbility(caster, "marker_trap")
	if ok, reason := s.beginAbilityCastLocked(caster, "marker_trap", victim); !ok {
		t.Fatalf("marker_trap cast failed: %q", reason)
	}
	for i := 0; i < 40; i++ {
		s.mu.Unlock()
		s.Update(0.05)
		s.mu.Lock()
	}
}

func markerTestPair(t *testing.T) (*GameState, *Unit, *Unit) {
	t.Helper()
	s := setupHostileTargetingPair(t)
	caster := teamCombatUnit(t, s, "p1", 0, 0)
	victim := teamCombatUnit(t, s, "p2", 60, 0)
	victim.HP, victim.MaxHP = 5000, 5000
	return s, caster, victim
}

// THE regression. marker_trap's mark is authored as an input ref
// {"key": "current_event"}, and resolveTargetRef had no case for that key — it
// fell through to the Named lookup, found nothing, and resolved to ZERO targets.
// The zone spawned and rendered, nothing was ever marked, and no test noticed
// because every other test applied the status directly.
func TestMarkerTrap_MarksTheEnemyItLandsOn(t *testing.T) {
	s, caster, victim := markerTestPair(t)
	defer s.mu.Unlock()

	if got := s.effectiveStatLocked(victim, 1.0, statDamageTaken); got != 1.0 {
		t.Fatalf("victim starts at damageTaken %v, want 1.0", got)
	}
	castMarkerTrapOnto(t, s, caster, victim)

	if got := s.effectiveStatLocked(victim, 1.0, statDamageTaken); got <= 1.0 {
		t.Fatalf("damageTaken = %v after a marker trap landed on the victim — the mark never applied", got)
	}
}

// The number the mark applies comes from the ability, not from this test.
func TestMarkerTrap_VulnerabilityMatchesTheAuthoredValue(t *testing.T) {
	s, caster, victim := markerTestPair(t)
	defer s.mu.Unlock()

	castMarkerTrapOnto(t, s, caster, victim)

	want, ok := s.EffectiveAbilityFieldLocked(caster, "marker_trap", "vulnerable", "value")
	if !ok {
		t.Fatal("marker_trap has no vulnerable.value field")
	}
	if got := s.effectiveStatLocked(victim, 1.0, statDamageTaken); got != 1.0+want {
		t.Errorf("damageTaken = %v, want %v (1.0 + the authored %v)", got, 1.0+want, want)
	}
}

// A zone's occupancy fires for EVERY unit inside it regardless of team — that is
// deliberate (a healing zone needs it), so the relation filter has to live in
// the program. Without it the Trapper marked itself: standing anywhere within
// the 115-radius zone it just threw was enough.
func TestMarkerTrap_DoesNotMarkTheCaster(t *testing.T) {
	s, caster, victim := markerTestPair(t)
	defer s.mu.Unlock()

	castMarkerTrapOnto(t, s, caster, victim)

	if got := s.effectiveStatLocked(caster, 1.0, statDamageTaken); got != 1.0 {
		t.Errorf("the CASTER's damageTaken = %v, want 1.0 — the trap marked its own thrower", got)
	}
}

// exposed_weakness is a pure DATA perk: a has_perk gate inside marker_trap's
// program adds a Weaken (damageDealt) status onto the mark, so a marked enemy
// ALSO deals less damage. Without the perk the gate is silent and the victim's
// outgoing damage is unchanged.
func TestMarkerTrap_ExposedWeaknessGateIsPerkOwned(t *testing.T) {
	// No perk: the gate does not fire, damageDealt stays at identity.
	func() {
		s, caster, victim := markerTestPair(t)
		defer s.mu.Unlock()
		castMarkerTrapOnto(t, s, caster, victim)
		if got := s.effectiveStatLocked(victim, 1.0, statDamageDealt); got != 1.0 {
			t.Errorf("without exposed_weakness the marked enemy's damageDealt = %v, want 1.0 (gate leaked)", got)
		}
	}()

	// With the perk: the gate fires and the authored Weaken lands. The number
	// comes from the ability, not this test.
	s, caster, victim := markerTestPair(t)
	defer s.mu.Unlock()
	caster.PerkIDs = []string{"exposed_weakness"}
	castMarkerTrapOnto(t, s, caster, victim)

	want, ok := s.EffectiveAbilityFieldLocked(caster, "marker_trap", "weaken", "value")
	if !ok {
		t.Fatal("marker_trap has no weaken.value field")
	}
	if want >= 0 {
		t.Fatalf("weaken.value = %v, want a negative add (deal LESS damage)", want)
	}
	// damageDealt is read at a 1.0 baseline, so the effective multiplier is
	// 1.0 + the authored (negative) add.
	if got := s.effectiveStatLocked(victim, 1.0, statDamageDealt); math.Abs(got-(1.0+want)) > 1e-9 {
		t.Errorf("marked enemy damageDealt = %v, want %v (1.0 + authored %v)", got, 1.0+want, want)
	}
}

// The RUNTIME proof (per the handoff's recurring-failure lesson: verify the real
// behaviour, not the reporting read). A weakened marked enemy's damage is folded
// down at the ONE point every outgoing hit funnels through
// (applyUnitDamageWithSourceLocked), so the landed damage matches its Weaken
// multiplier.
func TestMarkerTrap_ExposedWeaknessReducesLandedDamage(t *testing.T) {
	s, caster, victim := markerTestPair(t)
	defer s.mu.Unlock()
	caster.PerkIDs = []string{"exposed_weakness"}
	castMarkerTrapOnto(t, s, caster, victim)

	weaken := s.effectiveStatLocked(victim, 1.0, statDamageDealt)
	if weaken >= 1.0 {
		t.Fatalf("victim not weakened after the mark: damageDealt = %v", weaken)
	}

	// A fresh punching bag on the caster's team; the weakened victim is the
	// attacker. applyUnitDamageWithSourceLocked takes post-armor damage, so no
	// armor confounds the fold.
	dummy := teamCombatUnit(t, s, "p1", 400, 0)
	dummy.HP, dummy.MaxHP = 100000, 100000
	const swing = 1000
	landed := s.applyUnitDamageWithSourceLocked(dummy, swing, DamageSource{AttackerUnitID: victim.ID})

	wantLanded := int(math.Round(float64(swing) * weaken))
	if landed != wantLanded {
		t.Errorf("weakened enemy dealt %d, want %d (%d x %v)", landed, wantLanded, swing, weaken)
	}
	if landed >= swing {
		t.Errorf("weaken did not reduce landed damage: %d >= %d", landed, swing)
	}
}

// amplified_effects raises the mark by a FLAT amount rather than scaling it:
// damageTaken is a fixed-1.0-baseline stat, so a `multiply` of 1.35 on an
// authored 0.2 gives 0.27 (a 7-point gain), which is not what "35% harder"
// means to anyone reading it. An `add` says exactly what it does.
func TestMarkerTrap_AmplifiedEffectsAddsFlatVulnerability(t *testing.T) {
	base, amplified := 0.0, 0.0

	func() {
		s, caster, victim := markerTestPair(t)
		defer s.mu.Unlock()
		castMarkerTrapOnto(t, s, caster, victim)
		base = s.effectiveStatLocked(victim, 1.0, statDamageTaken)
	}()

	func() {
		s, caster, victim := markerTestPair(t)
		defer s.mu.Unlock()
		caster.PerkIDs = []string{"amplified_effects"}
		castMarkerTrapOnto(t, s, caster, victim)
		amplified = s.effectiveStatLocked(victim, 1.0, statDamageTaken)
	}()

	// Expected value from the perk's own authored row, so a re-tune — or a move
	// between authoring forms — carries the test rather than breaking it. The
	// stat is read at a 1.0 baseline, so the authored value is `base - 1`.
	want := applyAmplifiedRow(t, "marker_trap", "vulnerable", "value", base-1.0) - (base - 1.0)
	if want == 0 {
		t.Fatal("amplified_effects contributes nothing to marker_trap's vulnerability")
	}
	// Tolerance, not equality: these are float64 folds through the stat stages
	// (1.2 - 1.35 is 0.15000000000000013 in binary floating point). Determinism
	// is not at risk — identical inputs always give identical bits.
	if got := amplified - base; math.Abs(got-want) > 1e-9 {
		t.Errorf("amplified_effects added %v to damageTaken, want %v (base %v -> %v)", got, want, base, amplified)
	}
}
