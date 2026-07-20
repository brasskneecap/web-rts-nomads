package game

import "testing"

// TestDealDamage_AmountRef_UsesBoundScalarRaw_NotRescaled proves the T3
// AmountRef path: a rider's deal_damage that names a bound ctxScalar applies
// round(scalar*mult) RAW — it must NOT be re-folded through
// effectiveAbilityDamageLocked's spell-modifier fold, even though the caster
// owns a x2 multiply modifier that would otherwise apply to this ability's
// school and ctx.abilityDef is set (the two preconditions the static-Amount
// path requires to scale). This is the whole point of the field: the bound
// scalar (e.g. "trigger_damage") is already a FINAL folded number from the
// triggering event, so re-folding it would double-apply the caster's damage
// modifiers.
func TestDealDamage_AmountRef_UsesBoundScalarRaw_NotRescaled(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	target := spawnProjTestUnit(t, s, enemyPlayerID, 100, 0)

	def := AbilityDef{ID: "test_rider_target", DamageType: DamageShadow}
	// A x2 multiply modifier on the shadow school. If AmountRef's raw path
	// were accidentally re-folded, the applied damage would be 20, not 10.
	caster.SpellModifiers = []SpellModifier{
		{Target: SpellModTarget{School: "shadow"}, Field: SpellModFieldDamage, Operation: SpellModMultiply, Value: 2.0},
	}

	ctx := &RuntimeAbilityContext{
		CasterID:   caster.ID,
		AbilityID:  def.ID,
		abilityDef: &def,
		Named: map[string]ContextValue{
			"trigger_damage": {Kind: ctxScalar, Scalar: 25},
		},
	}
	cfg := dealDamageConfig{AmountRef: "trigger_damage", AmountMult: 0.4, Type: DamageShadow}

	desc, ok := lookupActionDescriptor(ActionDealDamage)
	if !ok {
		t.Fatal("deal_damage action not registered")
	}
	desc.Execute(s, ctx, cfg, []int{target.ID})

	const want = 10 // round(25 * 0.4) = 10, RAW — not doubled by the x2 modifier
	gotDamage := 500 - target.HP
	if gotDamage != want {
		t.Fatalf("applied damage = %d, want %d (raw scalar*mult, no spell-modifier re-fold)", gotDamage, want)
	}
	if ctx.lastAppliedDamage != want {
		t.Fatalf("ctx.lastAppliedDamage = %d, want %d", ctx.lastAppliedDamage, want)
	}
}

// TestDealDamage_AmountRef_UnboundScalar_AppliesZero covers the "ref set but
// nothing bound it" case: per the field's doc comment, this is a no-op hit
// (amount 0) rather than a fallback to the static Amount field — an author
// who authored AmountRef meant "the runtime value," and a missing binding is
// a rider-wiring bug that should surface as 0 damage, not a silently
// different static number.
func TestDealDamage_AmountRef_UnboundScalar_AppliesZero(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	target := spawnProjTestUnit(t, s, enemyPlayerID, 100, 0)

	ctx := &RuntimeAbilityContext{
		CasterID: caster.ID,
		Named:    map[string]ContextValue{}, // "trigger_damage" deliberately absent
	}
	cfg := dealDamageConfig{AmountRef: "trigger_damage", AmountMult: 0.4, Amount: 999, Type: DamagePhysical}

	desc, ok := lookupActionDescriptor(ActionDealDamage)
	if !ok {
		t.Fatal("deal_damage action not registered")
	}
	hit := desc.Execute(s, ctx, cfg, []int{target.ID})

	if target.HP != 500 {
		t.Fatalf("target.HP = %d, want unchanged 500 (unbound AmountRef must apply 0, not fall back to Amount=999)", target.HP)
	}
	if len(hit) != 1 || hit[0] != target.ID {
		t.Fatalf("hit = %v, want a single no-op hit against %d (the action still resolves/registers the target, just with 0 damage)", hit, target.ID)
	}
}

// TestDealDamage_StaticAmount_Unchanged is the byte-identical regression
// guard: a deal_damage authored the old way (no AmountRef) must apply
// EXACTLY the same damage it always has — the spell-modifier fold still
// runs, FlatOffset still applies last.
func TestDealDamage_StaticAmount_Unchanged(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	target := spawnProjTestUnit(t, s, enemyPlayerID, 100, 0)

	def := AbilityDef{ID: "test_static_amount", DamageType: DamageFire}
	caster.SpellModifiers = []SpellModifier{
		{Target: SpellModTarget{School: "fire"}, Field: SpellModFieldDamage, Value: 20}, // additive, no Operation ⇒ add
	}

	ctx := &RuntimeAbilityContext{
		CasterID:   caster.ID,
		AbilityID:  def.ID,
		abilityDef: &def,
		Named:      map[string]ContextValue{},
	}
	cfg := dealDamageConfig{Amount: 100, FlatOffset: -5, Type: DamageFire}

	desc, ok := lookupActionDescriptor(ActionDealDamage)
	if !ok {
		t.Fatal("deal_damage action not registered")
	}
	desc.Execute(s, ctx, cfg, []int{target.ID})

	const want = 115 // (100+20 additive fold) - 5 FlatOffset = 115, unchanged path
	gotDamage := 500 - target.HP
	if gotDamage != want {
		t.Fatalf("applied damage = %d, want %d (static-Amount path must be byte-identical to pre-AmountRef behavior)", gotDamage, want)
	}
}

// TestDealDamageValidate_AmountRef covers the two new Validate branches: a
// config with AmountRef set is valid even at Amount==0, and a config with
// NEITHER Amount nor AmountRef is still rejected.
func TestDealDamageValidate_AmountRef(t *testing.T) {
	d, ok := lookupActionDescriptor(ActionDealDamage)
	if !ok {
		t.Fatal("deal_damage action not registered")
	}

	good, err := d.Decode([]byte(`{"amountRef":"trigger_damage"}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if issues := d.Validate(good, ValidationScope{}); len(issues) != 0 {
		t.Fatalf("amountRef set, amount 0: unexpected issues: %+v", issues)
	}

	bad, err := d.Decode([]byte(`{}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if issues := d.Validate(bad, ValidationScope{}); len(issues) == 0 {
		t.Fatal("neither amount nor amountRef set: want a validation issue, got none")
	}
}
