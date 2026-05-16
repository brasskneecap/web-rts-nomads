package game

import "math"

// applyUpgradeLocked applies the chosen upgrade to playerID's army.
// targetUnitID is only used for upgradeEffectTypeXP upgrades — it identifies
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
	if !def.Unlimited {
		player.UpgradeState.UpgradeStacks[def.Group]++
	}

	switch def.Effect.Type {
	case upgradeEffectTypeXP:
		unit := s.getUnitByIDLocked(targetUnitID)
		if unit != nil && unit.OwnerID == playerID && unit.HP > 0 {
			s.addUnitXPLocked(unit, def.Effect.Amount)
		}
	case upgradeEffectTypeEquipment:
		if itemDef, ok := itemCatalogSingleton[def.Effect.ItemID]; ok {
			s.addItemToVaultLocked(player, itemDef)
		}
	case upgradeEffectTypeResources:
		player.Resources["gold"] += def.Effect.Gold
		player.Resources["wood"] += def.Effect.Wood
	default: // upgradeEffectTypeStat
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
		// Persist the multiplier so units spawned after this choice also benefit.
		accumulateWaveStatBuff(&player.UpgradeState, def)
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
	case upgradeEffectStatAttackSpeed:
		unit.BaseAttackSpeed *= m
	case upgradeEffectStatDamage:
		unit.BaseDamage = int(math.Round(float64(unit.BaseDamage) * m))
	case upgradeEffectStatHP:
		unit.BaseMaxHP = int(math.Round(float64(unit.BaseMaxHP) * m))
	case upgradeEffectStatMoveSpeed:
		unit.BaseMoveSpeed *= m
	case upgradeEffectStatAttackRange:
		unit.BaseAttackRange *= m
	}
}

// accumulateWaveStatBuff appends one entry per upgrade application so that
// units spawned later receive each multiplier applied sequentially, producing
// the same rounded result as units that were alive when the upgrade was chosen.
func accumulateWaveStatBuff(state *PlayerUpgradeState, def UpgradeDef) {
	state.WaveStatBuffs = append(state.WaveStatBuffs, WaveStatBuff{
		Scope:      def.Scope,
		Archetype:  def.Archetype,
		UnitType:   def.UnitType,
		Stat:       def.Effect.Stat,
		Multiplier: def.Effect.Multiplier,
	})
}

// unitMatchesWaveStatBuff reports whether buff's scope includes unit.
func unitMatchesWaveStatBuff(buff WaveStatBuff, unit *Unit) bool {
	switch buff.Scope {
	case upgradeScopeArmy:
		return true
	case upgradeScopeArchetype:
		return unit.Archetype == buff.Archetype
	case upgradeScopeUnitType:
		return unit.UnitType == buff.UnitType
	default:
		return false
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
