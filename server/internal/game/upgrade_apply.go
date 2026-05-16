package game

import "math"

// applyUpgradeLocked applies the chosen upgrade to playerID's army.
// targetUnitID is only used when the upgrade scope is "xp" — it identifies
// which unit receives the XP grant. Caller must hold s.mu.
func (s *GameState) applyUpgradeLocked(playerID, upgradeID string, targetUnitID int) {
	def, ok := getUpgradeDef(upgradeID)
	if !ok {
		return
	}
	player := s.Players[playerID]
	if player == nil {
		return
	}
	player.UpgradeState.UpgradeStacks[def.Group]++

	switch def.Effect.Type {
	case upgradeScopeXP:
		unit := s.getUnitByIDLocked(targetUnitID)
		if unit != nil && unit.OwnerID == playerID && unit.HP > 0 {
			s.addUnitXPLocked(unit, def.Effect.Amount)
		}
	case upgradeScopeEquipment:
		if itemDef, ok := itemCatalogSingleton[def.Effect.ItemID]; ok {
			s.addItemToVaultLocked(player, itemDef)
		}
	default:
		// Stat multiplier: walk all living player-owned units matching the scope.
		for _, unit := range s.Units {
			if unit.OwnerID != playerID || unit.HP <= 0 {
				continue
			}
			if !matchesUpgradeScope(def, unit) {
				continue
			}
			applyStatMultiplierToUnit(def, unit)
			s.applyRankModifiersLocked(unit, true)
		}
	}
}

// matchesUpgradeScope reports whether unit falls within def's targeting scope.
func matchesUpgradeScope(def UpgradeDef, unit *Unit) bool {
	switch def.Scope {
	case upgradeScopeArmy:
		return true
	case upgradeScopeArchetype:
		return unit.Archetype == def.Archetype
	case upgradeScopeUnitType:
		return unit.UnitType == def.UnitType
	default:
		return false
	}
}

// applyStatMultiplierToUnit multiplies the relevant Base* stat on unit by
// def.Effect.Multiplier. Caller must call applyRankModifiersLocked afterward
// to propagate the change to derived stats.
func applyStatMultiplierToUnit(def UpgradeDef, unit *Unit) {
	m := def.Effect.Multiplier
	switch def.Effect.Stat {
	case "attackSpeed":
		unit.BaseAttackSpeed *= m
	case "damage":
		unit.BaseDamage = int(math.Round(float64(unit.BaseDamage) * m))
	case "hp":
		unit.BaseMaxHP = int(math.Round(float64(unit.BaseMaxHP) * m))
	case "moveSpeed":
		unit.BaseMoveSpeed *= m
	case "attackRange":
		unit.BaseAttackRange *= m
	}
}

// HandleWaveUpgradeChoice processes a player's upgrade selection.
func (s *GameState) HandleWaveUpgradeChoice(playerID, upgradeID string, targetUnitID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	player := s.Players[playerID]
	if player == nil || player.UpgradeState.Resolved {
		return
	}
	if s.WaveManager.State != "upgrade" {
		return
	}
	// Validate that upgradeID is among the current offers.
	valid := false
	for _, offer := range player.UpgradeState.CurrentOffers {
		if offer.ID == upgradeID {
			valid = true
			break
		}
	}
	if !valid {
		return
	}
	s.applyUpgradeLocked(playerID, upgradeID, targetUnitID)
	player.UpgradeState.Resolved = true
}
