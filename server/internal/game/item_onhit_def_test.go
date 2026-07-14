package game

import (
	"encoding/json"
	"testing"
)

// firstProcFor returns the item's first proc on the given trigger, failing the
// test when it has none. Most catalog items carry exactly one proc per
// trigger; tests that care about multiples read def.Procs directly.
func firstProcFor(t *testing.T, def *ItemDef, trigger ItemProcTrigger) *ItemProc {
	t.Helper()
	for i := range def.Procs {
		if def.Procs[i].Trigger == trigger {
			return &def.Procs[i]
		}
	}
	t.Fatalf("item %q has no %s proc", def.ID, trigger)
	return nil
}

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
		ID:    "fire_sword",
		Kind:  ItemKindEquipment,
		Procs: []ItemProc{{Trigger: ProcOnHit, Chance: 0.05, Effect: "fire_bolt_ignite"}},
	}
	if err := validateItemDef(goodProc); err != nil {
		t.Fatalf("valid proc def rejected: %v", err)
	}

	goodOverride := &ItemDef{
		ID:   "heavy_fire_sword",
		Kind: ItemKindEquipment,
		Procs: []ItemProc{{
			Trigger: ProcOnHit, Chance: 0.05, Effect: "fire_bolt_ignite",
			ProcEffectOverrides: ProcEffectOverrides{Damage: 40},
		}},
	}
	if err := validateItemDef(goodOverride); err != nil {
		t.Fatalf("valid proc override rejected: %v", err)
	}

	// Several procs on ONE item, including two on the same trigger.
	multi := &ItemDef{
		ID:   "storm_brand",
		Kind: ItemKindEquipment,
		Procs: []ItemProc{
			{Trigger: ProcOnHit, Chance: 0.1, Effect: "fire_bolt_ignite"},
			{Trigger: ProcOnHit, Chance: 0.2, Effect: "lightning_chain"},
			{Trigger: ProcOnStruck, Chance: 0.05, Effect: "frost_bolt_chill"},
		},
	}
	if err := validateItemDef(multi); err != nil {
		t.Fatalf("multi-proc item rejected: %v", err)
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

	badChance := &ItemDef{ID: "bad2", Procs: []ItemProc{{Trigger: ProcOnHit, Chance: 1.5, Effect: "fire_bolt_ignite"}}}
	if err := validateItemDef(badChance); err == nil {
		t.Fatalf("expected error for proc chance > 1, got nil")
	}

	noEffect := &ItemDef{ID: "bad3", Procs: []ItemProc{{Trigger: ProcOnHit, Chance: 0.1}}}
	if err := validateItemDef(noEffect); err == nil {
		t.Fatalf("expected error for missing procs[0].effect, got nil")
	}

	unknownEffect := &ItemDef{ID: "bad4", Procs: []ItemProc{{Trigger: ProcOnHit, Chance: 0.1, Effect: "no_such_effect"}}}
	if err := validateItemDef(unknownEffect); err == nil {
		t.Fatalf("expected error for unregistered procs[0].effect, got nil")
	}

	noTrigger := &ItemDef{ID: "bad5", Procs: []ItemProc{{Chance: 0.1, Effect: "fire_bolt_ignite"}}}
	if err := validateItemDef(noTrigger); err == nil {
		t.Fatalf("expected error for missing procs[0].trigger, got nil")
	}

	badTrigger := &ItemDef{ID: "bad6", Procs: []ItemProc{{Trigger: "onCrit", Chance: 0.1, Effect: "fire_bolt_ignite"}}}
	if err := validateItemDef(badTrigger); err == nil {
		t.Fatalf("expected error for unknown procs[0].trigger, got nil")
	}
}

// TestItemDef_UnmarshalFoldsLegacyProcKeys: item JSON authored before the proc
// LIST schema used a single "onHitProc"/"onStruckProc" object whose KEY carried
// the trigger. Those files must still load, folded into Procs.
func TestItemDef_UnmarshalFoldsLegacyProcKeys(t *testing.T) {
	raw := []byte(`{
		"id": "legacy_blade",
		"kind": "equipment",
		"onHitProc": { "chance": 0.1, "effect": "fire_bolt_ignite" },
		"onStruckProc": { "chance": 0.2, "effect": "frost_bolt_chill", "damage": 40 }
	}`)
	var def ItemDef
	if err := json.Unmarshal(raw, &def); err != nil {
		t.Fatalf("unmarshal legacy def: %v", err)
	}
	if len(def.Procs) != 2 {
		t.Fatalf("legacy keys should fold into 2 procs, got %d: %+v", len(def.Procs), def.Procs)
	}
	onHit := firstProcFor(t, &def, ProcOnHit)
	if onHit.Effect != "fire_bolt_ignite" || onHit.Chance != 0.1 {
		t.Errorf("folded onHit proc = %+v", onHit)
	}
	onStruck := firstProcFor(t, &def, ProcOnStruck)
	if onStruck.Effect != "frost_bolt_chill" || onStruck.Chance != 0.2 || onStruck.Damage != 40 {
		t.Errorf("folded onStruck proc = %+v (overrides must survive)", onStruck)
	}
	if err := validateItemDef(&def); err != nil {
		t.Fatalf("folded legacy def must validate: %v", err)
	}
}

// TestItemProc_ResolveParams: an item reference resolves to the catalog def's
// payload with the item's non-zero overrides applied.
func TestItemProc_ResolveParams(t *testing.T) {
	plain := &ItemProc{Trigger: ProcOnHit, Chance: 0.1, Effect: "lightning_chain"}
	p, ok := plain.ResolveParams()
	if !ok {
		t.Fatal("lightning_chain should resolve")
	}
	def, _ := getProcEffectDef("lightning_chain")
	if p != def.ProcEffectParams {
		t.Errorf("no overrides ⇒ def payload verbatim:\n got %+v\nwant %+v", p, def.ProcEffectParams)
	}

	tuned := &ItemProc{Trigger: ProcOnHit, Chance: 0.1, Effect: "lightning_chain", ProcEffectOverrides: ProcEffectOverrides{Damage: 40, BounceCount: 4}}
	p2, _ := tuned.ResolveParams()
	if p2.Damage != 40 || p2.BounceCount != 4 {
		t.Errorf("overrides not applied: %+v", p2)
	}
	if p2.DamageType != def.DamageType || p2.ProjectileID != def.ProjectileID || p2.BounceRange != def.BounceRange {
		t.Errorf("non-overridden/identity fields must keep def values: %+v", p2)
	}

	missing := &ItemProc{Trigger: ProcOnHit, Chance: 0.1, Effect: "no_such_effect"}
	if _, ok := missing.ResolveParams(); ok {
		t.Error("unknown effect must resolve ok=false")
	}
}

// TestItemProc_MarshalEmitsResolvedPayload guards the client wire contract: the
// SPA tooltip reads damage / damageType / projectileID off each proc served by
// /catalog/items, so marshaling must emit the RESOLVED payload alongside the
// effect reference and the trigger.
func TestItemProc_MarshalEmitsResolvedPayload(t *testing.T) {
	def, ok := getItemDef("fire_sword")
	if !ok {
		t.Fatal("fire_sword not in catalog")
	}
	proc := firstProcFor(t, def, ProcOnHit)
	resolved, ok := proc.ResolveParams()
	if !ok {
		t.Fatalf("fire_sword proc effect %q must resolve", proc.Effect)
	}
	procDef, ok := getProcEffectDef(proc.Effect)
	if !ok {
		t.Fatalf("proc effect %q not in catalog", proc.Effect)
	}

	data, err := json.Marshal(proc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal wire: %v", err)
	}
	if wire["trigger"] != string(ProcOnHit) {
		t.Errorf("wire trigger = %v, want %v", wire["trigger"], ProcOnHit)
	}
	if wire["effect"] != procDef.ID {
		t.Errorf("wire effect = %v, want %v", wire["effect"], procDef.ID)
	}
	if wire["chance"] != proc.Chance {
		t.Errorf("wire chance = %v, want %v", wire["chance"], proc.Chance)
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
