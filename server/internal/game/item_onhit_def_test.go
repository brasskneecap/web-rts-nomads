package game

import (
	"encoding/json"
	"testing"
)

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

// TestItemOnHitProc_MarshalEmitsResolvedPayload guards the client wire
// contract: the SPA tooltip reads onHitProc.damage / damageType /
// projectileID off /catalog/items, so marshaling must emit the RESOLVED
// payload alongside the effect reference.
func TestItemOnHitProc_MarshalEmitsResolvedPayload(t *testing.T) {
	def, ok := getItemDef("fire_sword")
	if !ok {
		t.Fatal("fire_sword not in catalog")
	}
	data, err := json.Marshal(def.OnHitProc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal wire: %v", err)
	}
	if wire["effect"] != "fire_bolt_ignite" {
		t.Errorf("wire effect = %v, want fire_bolt_ignite", wire["effect"])
	}
	if wire["chance"] != 0.1 {
		t.Errorf("wire chance = %v, want 0.1", wire["chance"])
	}
	// The legacy client contract fields must be present and RESOLVED.
	if wire["damage"] != float64(25) {
		t.Errorf("wire damage = %v, want 25", wire["damage"])
	}
	if wire["damageType"] != "fire" {
		t.Errorf("wire damageType = %v, want fire", wire["damageType"])
	}
	if wire["projectileID"] != "fire_bolt" {
		t.Errorf("wire projectileID = %v, want fire_bolt", wire["projectileID"])
	}
	if wire["burnDamagePerSecond"] != float64(8) {
		t.Errorf("wire burnDamagePerSecond = %v, want 8", wire["burnDamagePerSecond"])
	}
	// Zero-valued optionals stay off the wire.
	if _, present := wire["bounceCount"]; present {
		t.Error("bounceCount should be omitted for a non-chaining effect")
	}
}
