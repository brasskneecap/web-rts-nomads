package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// findMinorEvent returns the minor (side-popup) damage event matching
// (unitID, variant) in the per-tick queue, or nil if absent.
func findMinorEvent(s *GameState, unitID int, variant string) *minorDamageEvent {
	for i := range s.minorDamageEventsThisTick {
		e := &s.minorDamageEventsThisTick[i]
		if e.UnitID == unitID && e.Variant == variant {
			return e
		}
	}
	return nil
}

func TestOnHitElemental_AppliesSeparateTypedInstance(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xE1E)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	attacker.AttackDamageType = "" // physical basic attack
	// Give the attacker a +5 fire on-hit bonus directly.
	attacker.EquipmentBonus.OnHitElemental = map[DamageType]int{DamageFire: 5}

	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 100, MaxHP: 100}
	s.nextUnitID++
	s.addUnitLocked(target)

	s.resetDamageTypeHintsThisTickLocked()
	s.resetMinorDamageEventsThisTickLocked()
	deadUnitIDs := []int{}
	// Physical hit of 8; armor 0 so the physical lands as 8 and fire as 5 → HP 87.
	s.resolveAttackHitLocked(attacker, target, 8, &deadUnitIDs)

	if target.HP != 87 {
		t.Fatalf("expected HP 87 (100 - 8 physical - 5 fire), got %d", target.HP)
	}
	// The fire instance must render as its OWN "fire" side-popup (minor event),
	// NOT tint the main number — so it emits a minor event, not a hint.
	if e := findMinorEvent(s, target.ID, "fire"); e == nil || e.Damage != 5 {
		t.Fatalf("expected a fire minor (side) popup of 5 from the elemental on-hit; queue: %+v", s.minorDamageEventsThisTick)
	}
	// And it must NOT emit a major-number hint (that would mis-color the
	// physical remainder of the combined HP-diff).
	if hint := findHint(s, target.ID, "fire"); hint != nil {
		t.Fatalf("elemental on-hit must not emit a major color hint; got %+v", hint)
	}
}
