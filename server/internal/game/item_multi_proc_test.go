package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// registerTestItem publishes def into the runtime overlay for the duration of
// the test, so getItemDef resolves it exactly like a catalog item.
func registerTestItem(t *testing.T, def *ItemDef) {
	t.Helper()
	if err := validateItemDef(def); err != nil {
		t.Fatalf("test item %q is invalid: %v", def.ID, err)
	}
	runtimeItemsMu.Lock()
	runtimeItems[def.ID] = def
	runtimeItemsMu.Unlock()
	t.Cleanup(func() {
		runtimeItemsMu.Lock()
		delete(runtimeItems, def.ID)
		runtimeItemsMu.Unlock()
	})
}

// TestEquipmentBonus_MultipleProcsPerTrigger: one item may carry several procs,
// including more than one on the SAME trigger. Each becomes its own
// EquipmentProc (they roll independently in combat) in catalog order, and
// triggers are routed to the matching bonus list.
func TestEquipmentBonus_MultipleProcsPerTrigger(t *testing.T) {
	registerTestItem(t, &ItemDef{
		ID: "test_storm_brand", DisplayName: "Storm Brand", IconKey: "test_storm_brand",
		Kind: ItemKindEquipment, Tier: ItemTierRare, Category: "Weapon",
		Procs: []ItemProc{
			{Trigger: ProcOnHit, Chance: 0.1, Ability: "fire_bolt"},
			{Trigger: ProcOnHit, Chance: 0.25, Ability: "chain_lightning"},
			{Trigger: ProcOnStruck, Chance: 0.5, Ability: "frost_bolt"},
		},
	})

	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x9A17)
	s.mu.Lock()
	defer s.mu.Unlock()
	unit := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	equipForTest(s, unit, "test_storm_brand")

	onHit := unit.EquipmentBonus.OnHitProcs
	if len(onHit) != 2 {
		t.Fatalf("both onHit procs must reach the unit, got %d: %+v", len(onHit), onHit)
	}
	if onHit[0].Chance != 0.1 || onHit[1].Chance != 0.25 {
		t.Errorf("onHit procs must keep catalog order and their own chances, got %+v", onHit)
	}
	// Each proc keeps its OWN ability — the second must not inherit the first's.
	if onHit[0].Ability != "fire_bolt" || onHit[1].Ability != "chain_lightning" {
		t.Errorf("onHit proc abilities = [%q %q], want [fire_bolt chain_lightning]", onHit[0].Ability, onHit[1].Ability)
	}

	onStruck := unit.EquipmentBonus.OnStruckProcs
	if len(onStruck) != 1 {
		t.Fatalf("expected the single onStruck proc, got %d: %+v", len(onStruck), onStruck)
	}
	if onStruck[0].Chance != 0.5 {
		t.Errorf("onStruck chance = %v, want 0.5", onStruck[0].Chance)
	}
}

// TestMultipleProcs_AllRollOnOneHit: with two guaranteed onHit procs on one
// weapon, a single landed attack fires BOTH — they are independent rolls, not
// a one-of-N pick.
func TestMultipleProcs_AllRollOnOneHit(t *testing.T) {
	registerTestItem(t, &ItemDef{
		ID: "test_double_proc", DisplayName: "Double Proc", IconKey: "test_double_proc",
		Kind: ItemKindEquipment, Tier: ItemTierRare, Category: "Weapon",
		Procs: []ItemProc{
			{Trigger: ProcOnHit, Chance: 1.0, Ability: "fire_bolt"},
			{Trigger: ProcOnHit, Chance: 1.0, Ability: "frost_bolt"},
		},
	})

	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x9A18)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	equipForTest(s, attacker, "test_double_proc")

	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 1_000_000, MaxHP: 1_000_000, X: 10, Y: 0}
	s.nextUnitID++
	s.addUnitLocked(target)
	disableEvasion(target)

	dead := []int{}
	s.resolveAttackHitLocked(attacker, target, 10, &dead)

	// Both guaranteed procs cast their ability; fire_bolt and frost_bolt each
	// launch a projectile, so a single hit produces two distinct bolts.
	if len(s.Projectiles) != 2 {
		t.Fatalf("both guaranteed procs must fire on one hit, got %d projectiles", len(s.Projectiles))
	}
	got := map[string]bool{}
	for _, p := range s.Projectiles {
		got[p.Variant] = true
	}
	if !got["fire_bolt"] || !got["frost_bolt"] {
		t.Errorf("expected one fire_bolt and one frost_bolt, got %v", got)
	}
}
