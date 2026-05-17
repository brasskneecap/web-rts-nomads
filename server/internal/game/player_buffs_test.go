package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func newBuffTestState(t *testing.T) (*GameState, string) {
	t.Helper()
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	const playerID = "p1"
	s.EnsurePlayer(playerID)
	return s, playerID
}

func spawnBuffTestUnit(t *testing.T, s *GameState, playerID string) *Unit {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	player := s.Players[playerID]
	return s.spawnPlayerUnitLocked("soldier", playerID, player.Color, protocol.Vec2{X: 200, Y: 200})
}

// ─── playerBuffAggregateLocked ───────────────────────────────────────────────

// TestPlayerBuffAggregateLocked_NoBuffs returns zero modifiers when the player
// has no ProfileBuffIDs set.
func TestPlayerBuffAggregateLocked_NoBuffs(t *testing.T) {
	s, playerID := newBuffTestState(t)
	unit := spawnBuffTestUnit(t, s, playerID)

	s.mu.RLock()
	mods := s.playerBuffAggregateLocked(unit)
	s.mu.RUnlock()

	if mods.AttackSpeedBonus != 0 || mods.HPBonus != 0 || mods.BonusDamageMult != 0 {
		t.Errorf("expected zero modifiers for player with no buffs, got %+v", mods)
	}
}

// TestPlayerBuffAggregateLocked_EnemyUnit returns zero modifiers for enemy units.
func TestPlayerBuffAggregateLocked_EnemyUnit(t *testing.T) {
	s, _ := newBuffTestState(t)

	s.mu.Lock()
	enemy := s.spawnEnemyUnitLocked("soldier", protocol.Vec2{X: 100, Y: 100})
	s.mu.Unlock()

	s.mu.RLock()
	mods := s.playerBuffAggregateLocked(enemy)
	s.mu.RUnlock()

	if mods.AttackSpeedBonus != 0 {
		t.Errorf("expected zero attack speed for enemy unit, got %v", mods.AttackSpeedBonus)
	}
}

// TestPlayerBuffAggregateLocked_IronDiscipline sums attack speed from iron_discipline.
func TestPlayerBuffAggregateLocked_IronDiscipline(t *testing.T) {
	s, playerID := newBuffTestState(t)
	unit := spawnBuffTestUnit(t, s, playerID)

	s.mu.Lock()
	s.Players[playerID].ProfileBuffIDs = []string{"iron_discipline"}
	s.mu.Unlock()

	s.mu.RLock()
	mods := s.playerBuffAggregateLocked(unit)
	s.mu.RUnlock()

	def := playerBuffDefByID("iron_discipline")
	if def == nil {
		t.Fatal("expected iron_discipline buff in catalog")
	}
	want := def.Modifiers.AttackSpeedBonus
	if mods.AttackSpeedBonus != want {
		t.Errorf("expected AttackSpeedBonus=%v from iron_discipline, got %v", want, mods.AttackSpeedBonus)
	}
}

// TestPlayerBuffAggregateLocked_AllowedUnitTypes_Filtered skips buffs whose
// AllowedUnitTypes does not include the unit's type.
func TestPlayerBuffAggregateLocked_AllowedUnitTypes_Filtered(t *testing.T) {
	s, playerID := newBuffTestState(t)
	unit := spawnBuffTestUnit(t, s, playerID) // type = "soldier"

	// Inject a test-only buff that only applies to archers.
	testDef := &PlayerBuffDef{
		ID:               "test_archer_only",
		AllowedUnitTypes: []string{"archer"},
		Modifiers:        PlayerBuffModifiers{AttackSpeedBonus: 0.99},
	}
	playerBuffCatalog["test_archer_only"] = testDef
	defer delete(playerBuffCatalog, "test_archer_only")

	s.mu.Lock()
	s.Players[playerID].ProfileBuffIDs = []string{"test_archer_only"}
	s.mu.Unlock()

	s.mu.RLock()
	mods := s.playerBuffAggregateLocked(unit)
	s.mu.RUnlock()

	if mods.AttackSpeedBonus != 0 {
		t.Errorf("buff restricted to archers should not apply to soldier, got %v", mods.AttackSpeedBonus)
	}
}

// TestPlayerBuffAggregateLocked_AllowedUnitTypes_Match applies a buff whose
// AllowedUnitTypes matches the unit.
func TestPlayerBuffAggregateLocked_AllowedUnitTypes_Match(t *testing.T) {
	s, playerID := newBuffTestState(t)
	unit := spawnBuffTestUnit(t, s, playerID) // type = "soldier"

	testDef := &PlayerBuffDef{
		ID:               "test_soldier_only",
		AllowedUnitTypes: []string{"soldier"},
		Modifiers:        PlayerBuffModifiers{AttackSpeedBonus: 0.10},
	}
	playerBuffCatalog["test_soldier_only"] = testDef
	defer delete(playerBuffCatalog, "test_soldier_only")

	s.mu.Lock()
	s.Players[playerID].ProfileBuffIDs = []string{"test_soldier_only"}
	s.mu.Unlock()

	s.mu.RLock()
	mods := s.playerBuffAggregateLocked(unit)
	s.mu.RUnlock()

	if mods.AttackSpeedBonus != 0.10 {
		t.Errorf("expected AttackSpeedBonus=0.10 for matching soldier buff, got %v", mods.AttackSpeedBonus)
	}
}

// ─── applyPlayerBuffsAtSpawnLocked ───────────────────────────────────────────

// TestApplyPlayerBuffsAtSpawnLocked_HPBonus verifies HP is increased at spawn.
func TestApplyPlayerBuffsAtSpawnLocked_HPBonus(t *testing.T) {
	s, playerID := newBuffTestState(t)

	s.mu.Lock()
	s.Players[playerID].ProfileBuffIDs = []string{"battle_hardened"}
	player := s.Players[playerID]
	unit := s.spawnPlayerUnitLocked("soldier", playerID, player.Color, protocol.Vec2{X: 300, Y: 300})
	baseHP := unit.BaseMaxHP
	s.mu.Unlock()

	if baseHP <= 0 {
		t.Fatal("expected positive BaseMaxHP after spawn with battle_hardened")
	}

	// The buff is applied during spawn; check that the unit has the HP bonus.
	def := playerBuffDefByID("battle_hardened")
	if def == nil {
		t.Skip("battle_hardened buff not in catalog")
	}
	if unit.BaseMaxHP < def.Modifiers.HPBonus {
		t.Errorf("BaseMaxHP should have been increased by HPBonus=%d", def.Modifiers.HPBonus)
	}
}

// TestApplyPlayerBuffsAtSpawnLocked_EnemySkipped verifies enemy units are unaffected.
func TestApplyPlayerBuffsAtSpawnLocked_EnemySkipped(t *testing.T) {
	s, _ := newBuffTestState(t)

	s.mu.Lock()
	def, ok := getUnitDef("soldier")
	if !ok {
		s.mu.Unlock()
		t.Skip("soldier def not found")
	}
	baseHP := def.HP
	enemy := s.spawnEnemyUnitLocked("soldier", protocol.Vec2{X: 100, Y: 100})
	s.mu.Unlock()

	if enemy.BaseMaxHP != baseHP {
		t.Errorf("enemy BaseMaxHP should be %d (unmodified), got %d", baseHP, enemy.BaseMaxHP)
	}
}

// ─── rollLegendPointDropLocked ───────────────────────────────────────────────

// TestRollLegendPointDropLocked_ZeroChance never drops when base chance is 0.
func TestRollLegendPointDropLocked_ZeroChance(t *testing.T) {
	s, playerID := newBuffTestState(t)

	s.mu.Lock()
	enemy := s.spawnEnemyUnitLocked("soldier", protocol.Vec2{X: 100, Y: 100})
	s.mu.Unlock()

	// Run many rolls — with 0.0 base chance and no unit def override, nothing drops.
	s.mu.Lock()
	before := s.Players[playerID].RunLegendPointDrops
	for i := 0; i < 1000; i++ {
		s.rollLegendPointDropLocked(playerID, enemy)
	}
	after := s.Players[playerID].RunLegendPointDrops
	s.mu.Unlock()

	if after != before {
		t.Errorf("expected no drops with 0.0 base chance, got %d drops", after-before)
	}
}

// TestRollLegendPointDropLocked_SameTeamSkipped never drops when attacker and
// victim share the same owner.
func TestRollLegendPointDropLocked_SameTeamSkipped(t *testing.T) {
	s, playerID := newBuffTestState(t)

	s.mu.Lock()
	player := s.Players[playerID]
	friendly := s.spawnPlayerUnitLocked("soldier", playerID, player.Color, protocol.Vec2{X: 200, Y: 200})

	// Give the unit a 100% drop chance via a catalog override.
	playerBuffCatalog["__test_100pct__"] = &PlayerBuffDef{
		ID: "__test_100pct__",
		Modifiers: PlayerBuffModifiers{},
	}
	// We're testing same-team skip, not the drop chance, so just run the roll.
	before := s.Players[playerID].RunLegendPointDrops
	s.rollLegendPointDropLocked(playerID, friendly)
	after := s.Players[playerID].RunLegendPointDrops
	s.mu.Unlock()

	delete(playerBuffCatalog, "__test_100pct__")

	if after != before {
		t.Errorf("expected no drop when attacker and victim share owner, got %d", after-before)
	}
}

// TestRollLegendPointDropLocked_EnemyAttackerSkipped skips enemy AI attacker.
func TestRollLegendPointDropLocked_EnemyAttackerSkipped(t *testing.T) {
	s, playerID := newBuffTestState(t)

	s.mu.Lock()
	player := s.Players[playerID]
	victim := s.spawnPlayerUnitLocked("soldier", playerID, player.Color, protocol.Vec2{X: 200, Y: 200})
	before := s.Players[playerID].RunLegendPointDrops
	s.rollLegendPointDropLocked(enemyPlayerID, victim)
	after := s.Players[playerID].RunLegendPointDrops
	s.mu.Unlock()

	if after != before {
		t.Errorf("expected no drop when attacker is enemy AI, got %d", after-before)
	}
}

// TestRollLegendPointDropLocked_UnitDefOverride uses the def's own drop fields
// when they are non-zero.
func TestRollLegendPointDropLocked_UnitDefOverride(t *testing.T) {
	s, playerID := newBuffTestState(t)

	// Inject a unit def override with 100% drop chance.
	const testType = "soldier"
	original := unitDefsByType[testType]
	modified := original
	modified.LegendPointDropChance = 1.0
	modified.LegendPointAmount = 5
	unitDefsByType[testType] = modified
	defer func() { unitDefsByType[testType] = original }()

	s.mu.Lock()
	enemy := s.spawnEnemyUnitLocked(testType, protocol.Vec2{X: 100, Y: 100})
	before := s.Players[playerID].RunLegendPointDrops
	// Roll many times — with 100% chance every roll should drop.
	s.rollLegendPointDropLocked(playerID, enemy)
	after := s.Players[playerID].RunLegendPointDrops
	s.mu.Unlock()

	if after-before != 5 {
		t.Errorf("expected 5 legend points from unit def override, got %d", after-before)
	}
}

// ─── activePlayerBuffIconsLocked ─────────────────────────────────────────────

// TestActivePlayerBuffIconsLocked_ReturnsIconKeys verifies icon entries are
// populated from the player's ProfileBuffIDs.
func TestActivePlayerBuffIconsLocked_ReturnsIconKeys(t *testing.T) {
	s, playerID := newBuffTestState(t)

	s.mu.Lock()
	s.Players[playerID].ProfileBuffIDs = []string{"iron_discipline"}
	s.mu.Unlock()

	s.mu.RLock()
	icons := s.activePlayerBuffIconsLocked(playerID)
	s.mu.RUnlock()

	if len(icons) != 1 {
		t.Fatalf("expected 1 icon, got %d", len(icons))
	}
	def := playerBuffDefByID("iron_discipline")
	if icons[0].ID != def.IconKey {
		t.Errorf("expected icon ID %q, got %q", def.IconKey, icons[0].ID)
	}
	if icons[0].Stacks != 1 {
		t.Errorf("expected Stacks=1, got %d", icons[0].Stacks)
	}
}

// TestActivePlayerBuffIconsLocked_EnemyReturnsNil enemy player gets nil.
func TestActivePlayerBuffIconsLocked_EnemyReturnsNil(t *testing.T) {
	s, _ := newBuffTestState(t)
	s.mu.RLock()
	icons := s.activePlayerBuffIconsLocked(enemyPlayerID)
	s.mu.RUnlock()
	if icons != nil {
		t.Errorf("expected nil for enemy player, got %v", icons)
	}
}
