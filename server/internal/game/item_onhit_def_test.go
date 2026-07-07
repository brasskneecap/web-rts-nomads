package game

import "testing"

func TestValidateItemDef_OnHitFields(t *testing.T) {
	good := &ItemDef{
		ID:   "fire_ring",
		Kind: ItemKindEquipment,
		OnHitElemental: []ItemElementalDamage{{Type: DamageFire, Amount: 5}},
	}
	if err := validateItemDef(good); err != nil {
		t.Fatalf("valid item def rejected: %v", err)
	}

	// A proc is now a REFERENCE to a catalog effect (+ optional overrides).
	goodProc := &ItemDef{
		ID:        "fire_sword",
		Kind:      ItemKindEquipment,
		OnHitProc: &ItemOnHitProc{Chance: 0.05, Effect: "fire_bolt_ignite"},
	}
	if err := validateItemDef(goodProc); err != nil {
		t.Fatalf("valid proc def rejected: %v", err)
	}

	goodOverride := &ItemDef{
		ID:        "heavy_fire_sword",
		Kind:      ItemKindEquipment,
		OnHitProc: &ItemOnHitProc{Chance: 0.05, Effect: "fire_bolt_ignite", ProcEffectOverrides: ProcEffectOverrides{Damage: 40}},
	}
	if err := validateItemDef(goodOverride); err != nil {
		t.Fatalf("valid proc override rejected: %v", err)
	}

	badType := &ItemDef{ID: "bad", OnHitElemental: []ItemElementalDamage{{Type: DamageType("plasma"), Amount: 5}}}
	if err := validateItemDef(badType); err == nil {
		t.Fatalf("expected error for unregistered elemental damage type, got nil")
	}

	badChance := &ItemDef{ID: "bad2", OnHitProc: &ItemOnHitProc{Chance: 1.5, Effect: "fire_bolt_ignite"}}
	if err := validateItemDef(badChance); err == nil {
		t.Fatalf("expected error for proc chance > 1, got nil")
	}

	noEffect := &ItemDef{ID: "bad3", OnHitProc: &ItemOnHitProc{Chance: 0.1}}
	if err := validateItemDef(noEffect); err == nil {
		t.Fatalf("expected error for missing onHitProc.effect, got nil")
	}

	unknownEffect := &ItemDef{ID: "bad4", OnHitProc: &ItemOnHitProc{Chance: 0.1, Effect: "no_such_effect"}}
	if err := validateItemDef(unknownEffect); err == nil {
		t.Fatalf("expected error for unregistered onHitProc.effect, got nil")
	}
}

// TestItemOnHitProc_ResolveParams: an item reference resolves to the catalog
// def's payload with the item's non-zero overrides applied.
func TestItemOnHitProc_ResolveParams(t *testing.T) {
	plain := &ItemOnHitProc{Chance: 0.1, Effect: "lightning_chain"}
	p, ok := plain.ResolveParams()
	if !ok {
		t.Fatal("lightning_chain should resolve")
	}
	def, _ := getProcEffectDef("lightning_chain")
	if p != def.ProcEffectParams {
		t.Errorf("no overrides ⇒ def payload verbatim:\n got %+v\nwant %+v", p, def.ProcEffectParams)
	}

	tuned := &ItemOnHitProc{Chance: 0.1, Effect: "lightning_chain", ProcEffectOverrides: ProcEffectOverrides{Damage: 40, BounceCount: 4}}
	p2, _ := tuned.ResolveParams()
	if p2.Damage != 40 || p2.BounceCount != 4 {
		t.Errorf("overrides not applied: %+v", p2)
	}
	if p2.DamageType != def.DamageType || p2.ProjectileID != def.ProjectileID || p2.BounceRange != def.BounceRange {
		t.Errorf("non-overridden/identity fields must keep def values: %+v", p2)
	}

	missing := &ItemOnHitProc{Chance: 0.1, Effect: "no_such_effect"}
	if _, ok := missing.ResolveParams(); ok {
		t.Error("unknown effect must resolve ok=false")
	}
}
