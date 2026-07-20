package game

import "testing"

// TestRestoreHealth_AmountRef_UsesBoundScalarRaw_NotRescaled proves the
// AmountRef path (mirroring deal_damage's AmountRef exactly, see that
// action's doc comment and TestDealDamage_AmountRef_UsesBoundScalarRaw_NotRescaled):
// a restore_health that names a bound ctxScalar applies round(scalar*mult)
// RAW — it must NOT be re-folded through effectiveAbilityHealLocked's
// divine_healer multiplier, even though the caster owns divine_healer (which
// would otherwise scale a static-amount heal) and ctx.abilityDef is set (the
// precondition the static-Amount path requires to scale). This is the whole
// point of the field: the bound scalar (e.g. "trigger_damage", bound by a
// lifesteal passive's on_damage_dealt trigger) is already a FINAL number from
// the triggering event, so re-folding it would double-apply the caster's
// heal-output modifiers. abilityDef.Category is deliberately Offensive, not
// Heal — the shape a real lifesteal passive would have — to also exercise
// that onPerkAbilityResolvedLocked's self-gate (def.Category != heal) makes
// firing the hook a no-op rather than a spurious side effect (see the
// restore_health Execute doc comment).
func TestRestoreHealth_AmountRef_UsesBoundScalarRaw_NotRescaled(t *testing.T) {
	s, caster, target := buildGoldenHealScenePerk(t, "divine_healer")
	defer s.mu.Unlock()

	dhDef := perkDefByID("divine_healer")
	if dhDef == nil {
		t.Fatal(`perkDefByID("divine_healer") = nil`)
	}
	mult := dhDef.ConfigForRank(caster.Rank)["healMultiplier"]
	if mult <= 1.0 {
		t.Fatalf("divine_healer healMultiplier = %v, want > 1.0 for this test to be meaningful", mult)
	}

	def := AbilityDef{ID: "test_lifesteal", Category: AbilityCategoryOffensive}
	ctx := &RuntimeAbilityContext{
		CasterID:   caster.ID,
		AbilityID:  def.ID,
		abilityDef: &def,
		Named: map[string]ContextValue{
			"trigger_damage": {Kind: ctxScalar, Scalar: 25},
		},
	}
	cfg := restoreHealthConfig{AmountRef: "trigger_damage", AmountMult: 0.2}

	desc, ok := lookupActionDescriptor(ActionRestoreHealth)
	if !ok {
		t.Fatal("restore_health action not registered")
	}
	preHP := target.HP
	desc.Execute(s, ctx, cfg, []int{target.ID})

	const want = 5 // round(25 * 0.2) = 5, RAW — not scaled by divine_healer
	gotHeal := target.HP - preHP
	if gotHeal != want {
		t.Fatalf("healed = %d, want %d (raw scalar*mult, no divine_healer re-fold)", gotHeal, want)
	}
	if target.PerkState.BattlePrayerRemaining != 0 {
		t.Fatalf("onPerkAbilityResolvedLocked stamped BattlePrayer on an Offensive-category ability's heal: BattlePrayerRemaining = %v, want 0 (hook's own def.Category!=heal gate must no-op here)", target.PerkState.BattlePrayerRemaining)
	}
}

// TestRestoreHealth_AmountRef_UnboundScalar_AppliesZero covers the "ref set
// but nothing bound it" case: per the field's doc comment, this is a no-op
// heal (amount 0) rather than a fallback to the static Amount field.
func TestRestoreHealth_AmountRef_UnboundScalar_AppliesZero(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	target := spawnProjTestUnit(t, s, "p1", 40, 0)
	target.HP = 400 // damaged, so a static-fallback bug would be visible

	ctx := &RuntimeAbilityContext{
		CasterID: caster.ID,
		Named:    map[string]ContextValue{}, // "trigger_damage" deliberately absent
	}
	cfg := restoreHealthConfig{AmountRef: "trigger_damage", AmountMult: 0.2, Amount: 999}

	desc, ok := lookupActionDescriptor(ActionRestoreHealth)
	if !ok {
		t.Fatal("restore_health action not registered")
	}
	healed := desc.Execute(s, ctx, cfg, []int{target.ID})

	if target.HP != 400 {
		t.Fatalf("target.HP = %d, want unchanged 400 (unbound AmountRef must apply 0, not fall back to Amount=999)", target.HP)
	}
	if len(healed) != 1 || healed[0] != target.ID {
		t.Fatalf("healed = %v, want a single no-op resolution against %d", healed, target.ID)
	}
}

// TestRestoreHealth_StaticAmount_Unchanged is the byte-identical regression
// guard: a restore_health authored the old way (no AmountRef) must apply
// EXACTLY the same heal it always has — effectiveAbilityHealLocked's
// divine_healer fold still runs.
func TestRestoreHealth_StaticAmount_Unchanged(t *testing.T) {
	s, caster, target := buildGoldenHealScenePerk(t, "divine_healer")
	defer s.mu.Unlock()

	dhDef := perkDefByID("divine_healer")
	if dhDef == nil {
		t.Fatal(`perkDefByID("divine_healer") = nil`)
	}
	mult := dhDef.ConfigForRank(caster.Rank)["healMultiplier"]

	def := AbilityDef{ID: "test_static_heal", Category: AbilityCategoryHeal}
	ctx := &RuntimeAbilityContext{
		CasterID:   caster.ID,
		AbilityID:  def.ID,
		abilityDef: &def,
		Named:      map[string]ContextValue{},
	}
	cfg := restoreHealthConfig{Amount: 20} // 20*divine_healer(2) = 40, within the ally's 50 HP of headroom (HP=50/MaxHP=100)

	desc, ok := lookupActionDescriptor(ActionRestoreHealth)
	if !ok {
		t.Fatal("restore_health action not registered")
	}
	preHP := target.HP
	desc.Execute(s, ctx, cfg, []int{target.ID})

	want := int(float64(20)*mult + 0.5) // round-half-up, matches math.Round for positive values
	gotHeal := target.HP - preHP
	if gotHeal != want {
		t.Fatalf("healed = %d, want %d (static-Amount path must be byte-identical to pre-AmountRef behavior: 20 * divine_healer %v)", gotHeal, want, mult)
	}
}

// TestRestoreHealthValidate_AmountRef covers the two new Validate branches: a
// config with AmountRef set is valid even at Amount==0, and a config with
// NEITHER Amount nor AmountRef is still rejected.
func TestRestoreHealthValidate_AmountRef(t *testing.T) {
	d, ok := lookupActionDescriptor(ActionRestoreHealth)
	if !ok {
		t.Fatal("restore_health action not registered")
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
