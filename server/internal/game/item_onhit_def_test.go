package game

import (
	"encoding/json"
	"testing"
)

func TestValidateItemDef_OnHitFields(t *testing.T) {
	good := &ItemDef{
		ID:             "fire_ring",
		Kind:           ItemKindEquipment,
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

	goodConsumable := &ItemDef{ID: "heal_potion", Kind: ItemKindConsumable, Consumable: &ConsumableEffect{Type: "heal", Amount: 50}}
	if err := validateItemDef(goodConsumable); err != nil {
		t.Fatalf("valid consumable rejected: %v", err)
	}
	badConsumable := &ItemDef{ID: "bad_potion", Kind: ItemKindConsumable, Consumable: &ConsumableEffect{Amount: 50}}
	if err := validateItemDef(badConsumable); err == nil {
		t.Fatalf("expected error for consumable with empty type, got nil")
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
	resolved, ok := def.OnHitProc.ResolveParams()
	if !ok {
		t.Fatalf("fire_sword onHitProc effect %q must resolve", def.OnHitProc.Effect)
	}
	procDef, ok := getProcEffectDef(def.OnHitProc.Effect)
	if !ok {
		t.Fatalf("proc effect %q not in catalog", def.OnHitProc.Effect)
	}

	data, err := json.Marshal(def.OnHitProc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal wire: %v", err)
	}
	if wire["effect"] != procDef.ID {
		t.Errorf("wire effect = %v, want %v", wire["effect"], procDef.ID)
	}
	if wire["chance"] != def.OnHitProc.Chance {
		t.Errorf("wire chance = %v, want %v", wire["chance"], def.OnHitProc.Chance)
	}
	// The legacy client contract fields must be present and equal the
	// RESOLVED (def + overrides) payload, not the raw def.
	if wire["damage"] != float64(resolved.Damage) {
		t.Errorf("wire damage = %v, want %v", wire["damage"], resolved.Damage)
	}
	if wire["damageType"] != string(resolved.DamageType) {
		t.Errorf("wire damageType = %v, want %v", wire["damageType"], resolved.DamageType)
	}
	if wire["projectileID"] != resolved.ProjectileID {
		t.Errorf("wire projectileID = %v, want %v", wire["projectileID"], resolved.ProjectileID)
	}
	if wire["burnDamagePerSecond"] != resolved.BurnDamagePerSecond {
		t.Errorf("wire burnDamagePerSecond = %v, want %v", wire["burnDamagePerSecond"], resolved.BurnDamagePerSecond)
	}
	// Zero-valued optionals stay off the wire.
	if _, present := wire["bounceCount"]; present {
		t.Error("bounceCount should be omitted for a non-chaining effect")
	}
}
