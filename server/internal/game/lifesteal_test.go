package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// lifestealAttacker spawns a wounded attacker (HP below MaxHP so healing is
// observable) with the given base lifesteal, plus a hostile target with deep HP.
func lifestealAttacker(t *testing.T, s *GameState, baseLifesteal float64) (attacker, target *Unit) {
	t.Helper()
	def := baseStatTestDef(nil)
	if baseLifesteal > 0 {
		def.BaseStats = map[string]float64{statLifesteal: baseLifesteal}
	}
	attacker = s.spawnUnitFromDefLocked(def, def.Type, "p1", "#fff", protocol.Vec2{})
	attacker.MaxHP, attacker.HP = 100, 50

	target = &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 1000, MaxHP: 1000, X: 20}
	s.nextUnitID++
	s.addUnitLocked(target)
	return attacker, target
}

// TestLifesteal_HealsAttackerByFraction: an attacker with base lifesteal heals
// for that fraction of the damage that lands.
func TestLifesteal_HealsAttackerByFraction(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x715E)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker, target := lifestealAttacker(t, s, 0.5)
	s.applyUnitDamageWithSourceLocked(target, 40, DamageSource{AttackerUnitID: attacker.ID})

	// 50% of 40 = 20 healed → 50 + 20 = 70.
	if attacker.HP != 70 {
		t.Fatalf("attacker HP = %d, want 70 (50 + 50%% lifesteal of 40)", attacker.HP)
	}
}

// TestLifesteal_NoneByDefault: an attacker that authors no lifesteal heals not
// at all — byte-identical to before the stat existed.
func TestLifesteal_NoneByDefault(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x715F)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker, target := lifestealAttacker(t, s, 0)
	s.applyUnitDamageWithSourceLocked(target, 40, DamageSource{AttackerUnitID: attacker.ID})
	if attacker.HP != 50 {
		t.Fatalf("attacker HP = %d, want 50 unchanged (no lifesteal authored)", attacker.HP)
	}
}

// TestLifesteal_StatusAdds: a status carrying a lifesteal StatModifier stacks
// on top of the unit's base, proving lifesteal folds through the shared
// perk/status/zone engine (effectiveStatLocked), not just the base map.
func TestLifesteal_StatusAdds(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x7160)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker, target := lifestealAttacker(t, s, 0.1)
	spawnTestStatusWithMods(s, attacker, 5, []PerkStatModifier{
		{Stat: statLifesteal, Op: statOpAdd, Value: 0.4},
	})
	s.applyUnitDamageWithSourceLocked(target, 100, DamageSource{AttackerUnitID: attacker.ID})

	// (0.1 base + 0.4 status) × 100 = 50 healed, capped at MaxHP 100 → 50 + 50.
	if attacker.HP != 100 {
		t.Fatalf("attacker HP = %d, want 100 (50 + 50%% lifesteal, capped at MaxHP)", attacker.HP)
	}
}

// TestLifesteal_NoSelfSteal: a unit damaging itself does not lifesteal off its
// own HP loss.
func TestLifesteal_NoSelfSteal(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x7161)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker, _ := lifestealAttacker(t, s, 0.5)
	s.applyUnitDamageWithSourceLocked(attacker, 20, DamageSource{AttackerUnitID: attacker.ID})
	// It took 20 self-damage (50 → 30) and must NOT lifesteal any of it back.
	if attacker.HP != 30 {
		t.Fatalf("attacker HP = %d, want 30 (20 self-damage, no self-lifesteal)", attacker.HP)
	}
}

// TestLifesteal_BaseAuthorableAndValidated guards the registration: lifesteal is
// a base-authorable stat and its per-unit base validates in [0,1] like a
// fraction.
func TestLifesteal_BaseAuthorableAndValidated(t *testing.T) {
	if !isBaseAuthorableStat(statLifesteal) {
		t.Fatal("lifesteal should be base-authorable")
	}
	valid := baseStatTestDef(map[string]float64{statLifesteal: 0.25})
	if err := validateUnitDef(&valid); err != nil {
		t.Fatalf("valid lifesteal base rejected: %v", err)
	}
}
