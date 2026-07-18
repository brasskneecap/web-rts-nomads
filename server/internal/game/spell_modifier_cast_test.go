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
	// arcane_bolt is schemaVersion:2 as of the composable-abilities migration:
	// DamageAmount is cleared on the raw def (the compiled launch_projectile
	// action's Config.Amount — the exact same number the executor's modifier
	// fold uses, see ability_exec_projectile.go — is the sole authority now).
	// Recovered via abilityMechanicsShadow so this end-to-end expectation still
	// tracks the catalog instead of reading the now-zeroed flat field directly.
	baseDamage := abilityMechanicsShadow(boltDef).DamageAmount
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
		enemy.Armor = 0
		enemy.MaxHP, enemy.HP = 5000, 5000 // survive even a doubled hit

		startMana := caster.CurrentMana
		startHP := enemy.HP
		before := len(s.Projectiles)
		if ok, reason := s.beginAbilityCastLocked(caster, "arcane_bolt", enemy); !ok {
			t.Fatalf("beginAbilityCastLocked failed: %q", reason)
		}
		// arcane_bolt has a non-zero cast time; drive it to resolution.
		s.tickUnitCastLocked(caster, boltDef.CastTime)
		if len(s.Projectiles) != before+1 {
			t.Fatalf("expected 1 bolt fired; projectiles %d→%d", before, len(s.Projectiles))
		}
		// arcane_bolt is schemaVersion:2's composable-impact shape: the bolt no
		// longer bakes Damage on the in-flight Projectile — it resolves only
		// when the nested on_projectile_impact trigger fires at landing (see
		// Projectile.ImpactActions' doc comment). Tick to impact and read the
		// modifier-scaled damage off the target's HP delta instead.
		for i := 0; i < 80 && len(s.Projectiles) > 0; i++ {
			s.tickProjectilesLocked(0.05)
		}
		if len(s.Projectiles) != 0 {
			t.Fatal("arcane_bolt projectile never landed")
		}
		return startMana - caster.CurrentMana, startHP - enemy.HP
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
