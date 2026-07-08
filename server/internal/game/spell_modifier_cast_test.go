package game

import "testing"

// End-to-end: a spell cast under an active modifier spends the modified mana
// and delivers the modified damage; with no modifiers it matches the base def.
// Uses arcane_bolt (a real catalog offensive ability) and inspects the bolt it
// fires, so the assertion rides the real begin→resolve→projectile path.
func TestSpellModifier_CastUsesEffectiveValues(t *testing.T) {
	boltDef, ok := getAbilityDef("arcane_bolt")
	if !ok {
		t.Fatal(`getAbilityDef("arcane_bolt") missing`)
	}
	baseMana := boltDef.ManaCost
	baseDamage := boltDef.DamageAmount
	if baseMana <= 0 || baseDamage <= 0 {
		t.Fatalf("arcane_bolt base mana=%d damage=%d; both must be > 0 for this test", baseMana, baseDamage)
	}

	cast := func(t *testing.T, mods []SpellModifier) (manaSpent int, boltDamage int) {
		t.Helper()
		s := newProjectileTestState(t)
		s.mu.Lock()
		defer s.mu.Unlock()
		caster := spawnProjTestUnit(t, s, "p1", 100, 100)
		caster.Abilities = []string{"arcane_bolt"}
		caster.AttackRange = 300
		caster.CurrentMana = 100
		caster.MaxMana = 100
		caster.SpellModifiers = mods
		enemy := spawnProjTestUnit(t, s, enemyPlayerID, 200, 100) // 100px away, in range

		startMana := caster.CurrentMana
		before := len(s.Projectiles)
		if ok, reason := s.beginAbilityCastLocked(caster, "arcane_bolt", enemy); !ok {
			t.Fatalf("beginAbilityCastLocked failed: %q", reason)
		}
		// arcane_bolt has a non-zero cast time; drive it to resolution.
		s.tickUnitCastLocked(caster, boltDef.CastTime)
		if len(s.Projectiles) != before+1 {
			t.Fatalf("expected 1 bolt fired; projectiles %d→%d", before, len(s.Projectiles))
		}
		return startMana - caster.CurrentMana, s.Projectiles[len(s.Projectiles)-1].Damage
	}

	// No modifiers → base values.
	if mana, dmg := cast(t, nil); mana != baseMana || dmg != baseDamage {
		t.Errorf("unmodified: manaSpent=%d dmg=%d; want %d/%d", mana, dmg, baseMana, baseDamage)
	}

	// Modified: double damage, mana cost reduced by 4.
	mods := []SpellModifier{
		{Target: SpellModTarget{SpellID: "arcane_bolt"}, Field: SpellModFieldDamage, Operation: SpellModMultiply, Value: 2},
		{Target: SpellModTarget{SpellID: "arcane_bolt"}, Field: SpellModFieldManaCost, Value: -4},
	}
	if mana, dmg := cast(t, mods); mana != baseMana-4 || dmg != baseDamage*2 {
		t.Errorf("modified: manaSpent=%d dmg=%d; want %d/%d", mana, dmg, baseMana-4, baseDamage*2)
	}
}
