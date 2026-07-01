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

	goodProc := &ItemDef{
		ID:        "fire_sword",
		Kind:      ItemKindEquipment,
		OnHitProc: &ItemOnHitProc{Chance: 0.05, Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"},
	}
	if err := validateItemDef(goodProc); err != nil {
		t.Fatalf("valid proc def rejected: %v", err)
	}

	badType := &ItemDef{ID: "bad", OnHitElemental: []ItemElementalDamage{{Type: DamageType("plasma"), Amount: 5}}}
	if err := validateItemDef(badType); err == nil {
		t.Fatalf("expected error for unregistered elemental damage type, got nil")
	}

	badChance := &ItemDef{ID: "bad2", OnHitProc: &ItemOnHitProc{Chance: 1.5, Damage: 10, DamageType: DamageFire}}
	if err := validateItemDef(badChance); err == nil {
		t.Fatalf("expected error for proc chance > 1, got nil")
	}

	badProcType := &ItemDef{ID: "bad3", OnHitProc: &ItemOnHitProc{Chance: 0.1, Damage: 10, DamageType: DamageType("void")}}
	if err := validateItemDef(badProcType); err == nil {
		t.Fatalf("expected error for unregistered proc damage type, got nil")
	}
}
