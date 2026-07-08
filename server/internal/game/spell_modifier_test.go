package game

import "testing"

func fireballFixture() AbilityDef {
	return AbilityDef{
		ID:           "fireball",
		DamageType:   "fire",
		Tags:         []string{"aoe", "projectile"},
		ManaCost:     20,
		Cooldown:     6,
		CastTime:     0.6,
		DamageAmount: 50,
		Radius:       90,
		Duration:     0,
		ChainCount:   0,
		PullStrength: 0,
	}
}

// Additive is the default operation; the base def is not mutated.
func TestSpellModifier_AdditiveDefaultNoMutation(t *testing.T) {
	def := fireballFixture()
	mods := []SpellModifier{
		{Target: SpellModTarget{SpellID: "fireball"}, Field: SpellModFieldDamage, Value: 10}, // no operation ⇒ add
	}
	eff := resolveEffectiveSpell(def, mods)
	if eff.Damage != 60 {
		t.Errorf("effective damage = %d; want 60 (50+10)", eff.Damage)
	}
	if def.DamageAmount != 50 {
		t.Errorf("base def mutated: DamageAmount = %d; want 50", def.DamageAmount)
	}
	// Resolve again with no modifiers ⇒ base values (proves no residue).
	if base := resolveEffectiveSpell(def, nil); base.Damage != 50 {
		t.Errorf("no-modifier resolve damage = %d; want base 50", base.Damage)
	}
}

// Multiply applies AFTER add: (base + add) * mul.
func TestSpellModifier_MultiplyAfterAdd(t *testing.T) {
	def := fireballFixture()
	mods := []SpellModifier{
		{Target: SpellModTarget{School: "fire"}, Field: SpellModFieldDamage, Operation: SpellModAdd, Value: 10},
		{Target: SpellModTarget{Tag: "aoe"}, Field: SpellModFieldDamage, Operation: SpellModMultiply, Value: 1.2},
	}
	eff := resolveEffectiveSpell(def, mods)
	if eff.Damage != 72 { // (50+10)*1.2 = 72
		t.Errorf("effective damage = %d; want 72", eff.Damage)
	}
}

// The result is independent of modifier collection order.
func TestSpellModifier_OrderIndependent(t *testing.T) {
	def := fireballFixture()
	a := SpellModifier{Target: SpellModTarget{School: "fire"}, Field: SpellModFieldRadius, Operation: SpellModMultiply, Value: 1.25}
	b := SpellModifier{Target: SpellModTarget{Tag: "aoe"}, Field: SpellModFieldRadius, Operation: SpellModMultiply, Value: 1.1}
	c := SpellModifier{Target: SpellModTarget{SpellID: "fireball"}, Field: SpellModFieldRadius, Value: 10}
	fwd := resolveEffectiveSpell(def, []SpellModifier{a, b, c})
	rev := resolveEffectiveSpell(def, []SpellModifier{c, b, a})
	if fwd.Radius != rev.Radius {
		t.Errorf("radius order-dependent: fwd=%v rev=%v", fwd.Radius, rev.Radius)
	}
	want := (90.0 + 10.0) * 1.25 * 1.1
	if fwd.Radius != want {
		t.Errorf("radius = %v; want %v", fwd.Radius, want)
	}
}

// A modifier only applies when every specified target field matches.
func TestSpellModifier_Matching(t *testing.T) {
	def := fireballFixture()
	cases := []struct {
		name string
		tgt  SpellModTarget
		want bool
	}{
		{"spellId match", SpellModTarget{SpellID: "fireball"}, true},
		{"spellId miss", SpellModTarget{SpellID: "arcane_orb"}, false},
		{"school match", SpellModTarget{School: "fire"}, true},
		{"school miss", SpellModTarget{School: "lightning"}, false},
		{"tag match", SpellModTarget{Tag: "aoe"}, true},
		{"tag miss", SpellModTarget{Tag: "chain"}, false},
		{"all match", SpellModTarget{SpellID: "fireball", School: "fire", Tag: "aoe"}, true},
		{"one field mismatch fails all", SpellModTarget{SpellID: "fireball", Tag: "chain"}, false},
		{"empty target never applies", SpellModTarget{}, false},
	}
	for _, c := range cases {
		m := SpellModifier{Target: c.tgt, Field: SpellModFieldDamage, Value: 1}
		if got := m.appliesTo(def); got != c.want {
			t.Errorf("%s: appliesTo = %v; want %v", c.name, got, c.want)
		}
	}
}

// An inert field (chainCount on a non-chaining spell) is a no-op, not an error.
func TestSpellModifier_InertFieldNoOp(t *testing.T) {
	def := fireballFixture() // ChainCount 0
	mods := []SpellModifier{{Target: SpellModTarget{SpellID: "fireball"}, Field: SpellModFieldChainCount, Value: 3}}
	eff := resolveEffectiveSpell(def, mods)
	if eff.ChainCount != 3 {
		t.Errorf("chainCount = %d; want 3 (resolved even if the spell ignores it)", eff.ChainCount)
	}
	if eff.Damage != 50 {
		t.Errorf("unrelated field changed: damage = %d; want 50", eff.Damage)
	}
}

// Validation: empty target and unknown field are rejected; add/multiply/"" ok.
func TestSpellModifier_Validate(t *testing.T) {
	if err := (SpellModifier{Field: SpellModFieldDamage, Value: 1}).Validate(); err == nil {
		t.Error("empty target should be rejected")
	}
	if err := (SpellModifier{Target: SpellModTarget{Tag: "aoe"}, Field: "explosionRadius", Value: 1}).Validate(); err == nil {
		t.Error("unknown field should be rejected")
	}
	if err := (SpellModifier{Target: SpellModTarget{Tag: "aoe"}, Field: SpellModFieldRadius, Operation: "divide", Value: 1}).Validate(); err == nil {
		t.Error("unknown operation should be rejected")
	}
	for _, op := range []SpellModOperation{"", SpellModAdd, SpellModMultiply} {
		if err := (SpellModifier{Target: SpellModTarget{Tag: "aoe"}, Field: SpellModFieldRadius, Operation: op, Value: 1}).Validate(); err != nil {
			t.Errorf("operation %q should be valid: %v", op, err)
		}
	}
}

// Resolved values floor at 0.
func TestSpellModifier_FloorsAtZero(t *testing.T) {
	def := fireballFixture()
	mods := []SpellModifier{{Target: SpellModTarget{SpellID: "fireball"}, Field: SpellModFieldManaCost, Value: -1000}}
	if eff := resolveEffectiveSpell(def, mods); eff.ManaCost != 0 {
		t.Errorf("manaCost = %d; want floored 0", eff.ManaCost)
	}
}

// The per-unit modifier source flows through the collector into effective
// values; a unit with no modifiers resolves to base.
func TestSpellModifier_CollectorFromUnit(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := spawnProjTestUnit(t, s, "p1", 100, 100)
	def := fireballFixture()

	// No modifiers ⇒ base values.
	if eff := s.effectiveSpellLocked(caster, def); eff.Damage != 50 {
		t.Fatalf("base collector damage = %d; want 50", eff.Damage)
	}
	// Attach a source modifier (as a buff/item would) and re-resolve.
	caster.SpellModifiers = []SpellModifier{
		{Target: SpellModTarget{School: "fire"}, Field: SpellModFieldDamage, Operation: SpellModMultiply, Value: 1.2},
	}
	if eff := s.effectiveSpellLocked(caster, def); eff.Damage != 60 {
		t.Errorf("modified collector damage = %d; want 60 (50*1.2)", eff.Damage)
	}
	// A nil caster collects nothing and never panics.
	if mods := s.collectSpellModifiersLocked(nil, def); mods != nil {
		t.Errorf("nil caster collect = %v; want nil", mods)
	}
}
