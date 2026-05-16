package game

import "webrts/server/pkg/protocol"

// playerBuffAggregateLocked returns the summed PlayerBuffModifiers for the
// active buffs of the unit's owning player. Returns zero modifiers for
// enemy/AI players. When a buff's AllowedUnitTypes is non-empty, the unit's
// UnitType must appear in that list for the buff to contribute.
// Must be called under s.mu lock.
func (s *GameState) playerBuffAggregateLocked(unit *Unit) PlayerBuffModifiers {
	if unit == nil || unit.OwnerID == enemyPlayerID {
		return PlayerBuffModifiers{}
	}
	player, ok := s.Players[unit.OwnerID]
	if !ok || len(player.ProfileBuffIDs) == 0 {
		return PlayerBuffModifiers{}
	}

	var agg PlayerBuffModifiers
	for _, buffID := range player.ProfileBuffIDs {
		def := playerBuffDefByID(buffID)
		if def == nil {
			continue
		}
		// "enemyUnits" buffs are applied via playerEnemyDamageMultiplierLocked, not here.
		if def.AppliesTo == "enemyUnits" {
			continue
		}
		if len(def.AllowedUnitTypes) > 0 && !containsString(def.AllowedUnitTypes, unit.UnitType) {
			continue
		}
		agg.HPBonus += def.Modifiers.HPBonus
		agg.DamageBonus += def.Modifiers.DamageBonus
		agg.ArmorBonus += def.Modifiers.ArmorBonus
		agg.AttackSpeedBonus += def.Modifiers.AttackSpeedBonus
		agg.MoveSpeedMultBonus += def.Modifiers.MoveSpeedMultBonus
		agg.BonusDamageMult += def.Modifiers.BonusDamageMult
	}
	return agg
}

// enemyFacingDamageMultLocked sums the "enemyUnits" BonusDamageMult from all
// real players. Used by Snapshot() to show enemy unit damage correctly in the
// HUD when a player has equipped an enemy-boosting buff like enemy_empowered.
// Must be called under s.mu lock.
func (s *GameState) enemyFacingDamageMultLocked() float64 {
	var total float64
	for _, player := range s.Players {
		if player.ID == enemyPlayerID {
			continue
		}
		for _, buffID := range player.ProfileBuffIDs {
			def := playerBuffDefByID(buffID)
			if def == nil || def.AppliesTo != "enemyUnits" {
				continue
			}
			total += def.Modifiers.BonusDamageMult
		}
	}
	return total
}

// playerEnemyDamageMultiplierLocked returns an additive damage multiplier for
// an enemy attacker sourced from "appliesTo: enemyUnits" buffs equipped by the
// defending player. Returns 0.0 when the attacker is not hostile or the
// defender has no such buffs.
// Must be called under s.mu lock.
func (s *GameState) playerEnemyDamageMultiplierLocked(attacker, defender *Unit) float64 {
	if attacker == nil || defender == nil {
		return 0
	}
	if !s.playersAreHostileLocked(attacker.OwnerID, defender.OwnerID) {
		return 0
	}
	if defender.OwnerID == enemyPlayerID {
		return 0
	}
	player, ok := s.Players[defender.OwnerID]
	if !ok || len(player.ProfileBuffIDs) == 0 {
		return 0
	}
	var total float64
	for _, buffID := range player.ProfileBuffIDs {
		def := playerBuffDefByID(buffID)
		if def == nil || def.AppliesTo != "enemyUnits" {
			continue
		}
		total += def.Modifiers.BonusDamageMult
	}
	return total
}

// applyPlayerBuffsAtSpawnLocked applies spawn-time stat bonuses (HP, damage,
// armor) from the player's equipped buffs. Called immediately after
// applyPlayerUpgradesAtSpawnLocked in state_spawn.go. Modifies Base* stats
// BEFORE applyRankModifiersLocked runs.
// Must be called under s.mu write lock.
func (s *GameState) applyPlayerBuffsAtSpawnLocked(unit *Unit) {
	if unit == nil || unit.OwnerID == enemyPlayerID {
		return
	}
	mods := s.playerBuffAggregateLocked(unit)
	if mods.HPBonus != 0 {
		unit.BaseMaxHP += mods.HPBonus
		unit.HP += mods.HPBonus
		unit.MaxHP += mods.HPBonus
	}
	if mods.DamageBonus != 0 {
		unit.BaseDamage += mods.DamageBonus
	}
	if mods.ArmorBonus != 0 {
		unit.BaseArmor += mods.ArmorBonus
	}
}

// activePlayerBuffIconsLocked returns icon entries for the player's equipped
// buffs to populate PlayerSnapshot.ActiveBuffs. Uses ActiveEffectIcon with
// Stacks=1 for each distinct buff.
// Must be called under s.mu lock.
func (s *GameState) activePlayerBuffIconsLocked(playerID string) []protocol.ActiveEffectIcon {
	if playerID == enemyPlayerID {
		return nil
	}
	player, ok := s.Players[playerID]
	if !ok || len(player.ProfileBuffIDs) == 0 {
		return nil
	}
	icons := make([]protocol.ActiveEffectIcon, 0, len(player.ProfileBuffIDs))
	for _, buffID := range player.ProfileBuffIDs {
		def := playerBuffDefByID(buffID)
		if def == nil {
			continue
		}
		icons = append(icons, protocol.ActiveEffectIcon{ID: def.IconKey, Stacks: 1})
	}
	if len(icons) == 0 {
		return nil
	}
	return icons
}

// rollLegendPointDropLocked rolls a legend-point drop for the attacker's
// player when a unit is killed. Uses s.rngLoot for determinism. No-op if
// the killer is the enemy AI player, the same team, or no attacker.
// Must be called under s.mu write lock.
func (s *GameState) rollLegendPointDropLocked(attackerOwnerID string, deadUnit *Unit) {
	if attackerOwnerID == "" || deadUnit == nil {
		return
	}
	if attackerOwnerID == enemyPlayerID {
		return
	}
	if attackerOwnerID == deadUnit.OwnerID {
		return
	}

	tuning := gameplayTuning()

	// Determine drop chance and amount using precedence:
	//   1. Non-zero values on the dead unit's UnitDef
	//   2. Per-unit-type override in gameplay_tuning.json
	//   3. Base tuning values
	chance := tuning.LegendPoints.PerKillBaseDropChance
	amount := tuning.LegendPoints.PerKillBaseAmount

	if override, ok := tuning.UnitOverrides[deadUnit.UnitType]; ok {
		chance = override.LegendPointDropChance
		amount = override.LegendPointAmount
	}

	if def, ok := getUnitDef(deadUnit.UnitType); ok {
		if def.LegendPointDropChance > 0 {
			chance = def.LegendPointDropChance
		}
		if def.LegendPointAmount > 0 {
			amount = def.LegendPointAmount
		}
	}

	if chance <= 0 || amount <= 0 {
		return
	}

	if s.rngLoot.Float64() < chance {
		if player, ok := s.Players[attackerOwnerID]; ok {
			player.RunLegendPointDrops += amount
		}
	}
}

