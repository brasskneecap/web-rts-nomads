package game

import "testing"

// TestSiphonHealAction_HealsCasterFromAppliedDamage exercises the siphon_heal
// action's Execute in isolation: it heals the CASTER for
// round(lastAppliedDamage × healingMultiplier × healMult) via the Siphon
// distributor, ignores its target set, and records the heal in
// ctx.lastAppliedHeal for the channel loop's chain hook. healMult is the
// field-mod knob that replaced AbilityModifier.HealMult.
func TestSiphonHealAction_HealsCasterFromAppliedDamage(t *testing.T) {
	s, siphoner, _ := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Below max HP so the distributor self-heals (rather than seeking an ally).
	siphoner.MaxHP = 1000
	siphoner.HP = 100

	desc, ok := lookupActionDescriptor(ActionSiphonHeal)
	if !ok {
		t.Fatal("siphon_heal action descriptor not registered")
	}

	// tickDamage=40, healingMultiplier=1, healMult=2 → heal = 80.
	ctx := &RuntimeAbilityContext{CasterID: siphoner.ID, lastAppliedDamage: 40}
	cfg := siphonHealConfig{HealingMultiplier: 1, HealMult: 2, AllyHealRadius: 220}

	// A non-empty target set is deliberately passed to prove it is ignored —
	// the action heals the caster, not the "targets".
	desc.Execute(s, ctx, cfg, []int{siphoner.ID})

	if ctx.lastAppliedHeal != 80 {
		t.Errorf("lastAppliedHeal = %d, want 80 (round(40 * 1 * 2))", ctx.lastAppliedHeal)
	}
	if siphoner.HP != 180 {
		t.Errorf("caster HP = %d, want 180 (100 + 80 self-heal)", siphoner.HP)
	}
}

// TestSiphonHealAction_ZeroDamageIsNoop confirms a tick that applied no damage
// heals nothing and banks a zero heal — the distributor's amount<=0 guard.
func TestSiphonHealAction_ZeroDamageIsNoop(t *testing.T) {
	s, siphoner, _ := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.MaxHP = 1000
	siphoner.HP = 100

	desc, _ := lookupActionDescriptor(ActionSiphonHeal)
	ctx := &RuntimeAbilityContext{CasterID: siphoner.ID, lastAppliedDamage: 0}
	desc.Execute(s, ctx, siphonHealConfig{HealingMultiplier: 1, HealMult: 2}, nil)

	if ctx.lastAppliedHeal != 0 {
		t.Errorf("lastAppliedHeal = %d, want 0", ctx.lastAppliedHeal)
	}
	if siphoner.HP != 100 {
		t.Errorf("caster HP = %d, want 100 (unchanged)", siphoner.HP)
	}
}
