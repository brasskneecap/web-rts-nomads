package game

import "testing"

// TestExecutorDamageScalingMatchesLegacy proves effectiveAbilityDamageLocked
// (the executor's deal_damage scaling seam) computes EXACTLY what
// effectiveSpellLocked computes for the same caster/def/base — the Phase 4
// Task 3 parity requirement. Unmodified casters get raw-amount identity,
// which is why existing golden executor tests (none of which set
// SpellModifiers) are unaffected by wiring this in.
func TestExecutorDamageScalingMatchesLegacy(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 100, 100)
	def := AbilityDef{ID: "x", DamageType: DamageFire, DamageAmount: 100}

	t.Run("multiply modifier matches legacy", func(t *testing.T) {
		caster.SpellModifiers = []SpellModifier{
			{Target: SpellModTarget{School: "fire"}, Field: SpellModFieldDamage, Operation: SpellModMultiply, Value: 1.5},
		}
		legacy := s.effectiveSpellLocked(caster, def).Damage
		if legacy != 150 {
			t.Fatalf("legacy fixture drifted: effectiveSpellLocked damage = %d; want 150", legacy)
		}
		got := s.effectiveAbilityDamageLocked(caster, def, 100)
		if got != legacy {
			t.Fatalf("scaling mismatch: executor=%d legacy=%d", got, legacy)
		}
	})

	t.Run("additive modifier matches legacy", func(t *testing.T) {
		caster.SpellModifiers = []SpellModifier{
			{Target: SpellModTarget{School: "fire"}, Field: SpellModFieldDamage, Value: 20}, // no Operation ⇒ add
		}
		legacy := s.effectiveSpellLocked(caster, def).Damage // (100+20) = 120
		if legacy != 120 {
			t.Fatalf("legacy fixture drifted: effectiveSpellLocked damage = %d; want 120", legacy)
		}
		got := s.effectiveAbilityDamageLocked(caster, def, 100)
		if got != legacy {
			t.Fatalf("scaling mismatch: executor=%d legacy=%d", got, legacy)
		}
	})

	t.Run("no modifiers is identity", func(t *testing.T) {
		caster.SpellModifiers = nil
		got := s.effectiveAbilityDamageLocked(caster, def, 140)
		if got != 140 {
			t.Fatalf("identity case: got=%d want=140 (unmodified caster must not change the base amount)", got)
		}
	})

	t.Run("non-matching school does not scale", func(t *testing.T) {
		caster.SpellModifiers = []SpellModifier{
			// Cold modifier on a fire-school ability: appliesTo must be false.
			{Target: SpellModTarget{School: "cold"}, Field: SpellModFieldDamage, Operation: SpellModMultiply, Value: 2.0},
		}
		legacy := s.effectiveSpellLocked(caster, def).Damage
		if legacy != 100 {
			t.Fatalf("legacy fixture drifted: effectiveSpellLocked damage = %d; want 100 (mismatch school inert)", legacy)
		}
		got := s.effectiveAbilityDamageLocked(caster, def, 100)
		if got != legacy {
			t.Fatalf("scaling mismatch: executor=%d legacy=%d", got, legacy)
		}
	})
}
