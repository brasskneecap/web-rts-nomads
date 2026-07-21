package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// thornsPair spawns a defender (owner p1) with the given base thorns and a
// hostile attacker (enemy team) with full HP, wired hostile so reflection fires.
func thornsPair(t *testing.T, s *GameState, baseThorns float64) (defender, attacker *Unit) {
	t.Helper()
	setTeam(s, "p1", 0)
	setTeam(s, enemyPlayerID, 1)

	def := baseStatTestDef(nil)
	if baseThorns > 0 {
		def.BaseStats = map[string]float64{statThorns: baseThorns}
	}
	defender = s.spawnUnitFromDefLocked(def, def.Type, "p1", "#fff", protocol.Vec2{})
	defender.MaxHP, defender.HP = 500, 500

	attacker = &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 100, MaxHP: 100, X: 20}
	s.nextUnitID++
	s.addUnitLocked(attacker)
	return defender, attacker
}

// TestThorns_ReflectsToAttacker: a defender with base thorns reflects that
// fraction of an attack's damage back at the attacker.
func TestThorns_ReflectsToAttacker(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x780A)
	s.mu.Lock()
	defer s.mu.Unlock()

	defender, attacker := thornsPair(t, s, 0.5)
	s.onPerkDamageTakenLocked(defender, attacker, 40)

	// 50% of 40 = 20 reflected (attacker has 0 armor) → 100 - 20 = 80.
	if attacker.HP != 80 {
		t.Fatalf("attacker HP = %d, want 80 (100 - 50%% thorns of 40)", attacker.HP)
	}
}

// TestThorns_NoneByDefault: a defender that authors no thorns reflects nothing.
func TestThorns_NoneByDefault(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x780B)
	s.mu.Lock()
	defer s.mu.Unlock()

	defender, attacker := thornsPair(t, s, 0)
	s.onPerkDamageTakenLocked(defender, attacker, 40)
	if attacker.HP != 100 {
		t.Fatalf("attacker HP = %d, want 100 unchanged (no thorns authored)", attacker.HP)
	}
}

// TestThorns_StatusAdds proves thorns folds through the shared perk/status/zone
// engine: a status carrying a thorns StatModifier stacks on the base.
func TestThorns_StatusAdds(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x780C)
	s.mu.Lock()
	defer s.mu.Unlock()

	defender, attacker := thornsPair(t, s, 0.1)
	spawnTestStatusWithMods(s, defender, 5, []PerkStatModifier{
		{Stat: statThorns, Op: statOpAdd, Value: 0.4},
	})
	s.onPerkDamageTakenLocked(defender, attacker, 100)

	// (0.1 base + 0.4 status) × 100 = 50 reflected → 100 - 50 = 50.
	if attacker.HP != 50 {
		t.Fatalf("attacker HP = %d, want 50 (100 - 50%% thorns of 100)", attacker.HP)
	}
}

// TestThorns_NotReflectedToAllies: thorns only punishes hostile attackers.
func TestThorns_NotReflectedToAllies(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x780D)
	s.mu.Lock()
	defer s.mu.Unlock()

	setTeam(s, "p1", 0)
	setTeam(s, "p2", 0) // same team as the defender → allies

	def := baseStatTestDef(map[string]float64{statThorns: 0.5})
	defender := s.spawnUnitFromDefLocked(def, def.Type, "p1", "#fff", protocol.Vec2{})
	defender.MaxHP, defender.HP = 500, 500

	ally := &Unit{ID: s.nextUnitID, OwnerID: "p2", UnitType: "soldier", Visible: true, HP: 100, MaxHP: 100, X: 20}
	s.nextUnitID++
	s.addUnitLocked(ally)

	s.onPerkDamageTakenLocked(defender, ally, 40)
	if ally.HP != 100 {
		t.Fatalf("ally HP = %d, want 100 (thorns must not reflect to allies)", ally.HP)
	}
}

// TestThorns_BaseAuthorableAndValidated guards the registration + fraction clamp.
func TestThorns_BaseAuthorableAndValidated(t *testing.T) {
	if !isBaseAuthorableStat(statThorns) {
		t.Fatal("thorns should be base-authorable")
	}
	valid := baseStatTestDef(map[string]float64{statThorns: 0.25})
	if err := validateUnitDef(&valid); err != nil {
		t.Fatalf("valid thorns base rejected: %v", err)
	}
	bad := baseStatTestDef(map[string]float64{statThorns: 1.5})
	if err := validateUnitDef(&bad); err == nil {
		t.Fatal("thorns > 1 should be rejected (fraction clamp)")
	}
}
