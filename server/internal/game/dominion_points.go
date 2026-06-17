package game

import "log"

// rollDominionPointDropLocked rolls a dominion-point drop for the attacker's
// player when a unit is killed. Uses s.rngLoot for determinism. No-op if
// the killer is the enemy AI player, the same team, or no attacker.
// Must be called under s.mu write lock.
func (s *GameState) rollDominionPointDropLocked(attackerOwnerID string, deadUnit *Unit) {
	if attackerOwnerID == "" || deadUnit == nil {
		return
	}
	if attackerOwnerID == enemyPlayerID || attackerOwnerID == neutralPlayerID {
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
	chance := tuning.DominionPoints.PerKillBaseDropChance
	amount := tuning.DominionPoints.PerKillBaseAmount

	if override, ok := tuning.UnitOverrides[deadUnit.UnitType]; ok {
		chance = override.DominionPointDropChance
		amount = override.DominionPointAmount
	}

	if def, ok := getUnitDef(deadUnit.UnitType); ok {
		if def.DominionPointDropChance > 0 {
			chance = def.DominionPointDropChance
		}
		if def.DominionPointAmount > 0 {
			amount = def.DominionPointAmount
		}
	}

	if chance <= 0 || amount <= 0 {
		return
	}

	if s.rngLoot.Float64() < chance {
		if tuning.DominionPoints.CommitMode == dominionPointCommitModeImmediate {
			// Fire-and-forget commit; do NOT accumulate on the player so the
			// match-end commit path sees zero and cannot double-credit.
			if s.onDominionPointDropImmediate != nil {
				s.onDominionPointDropImmediate(attackerOwnerID, amount)
			} else {
				log.Printf("[DP] WARNING: commitMode=immediate but no hook wired; drop is lost (attacker=%s amount=%d)",
					attackerOwnerID, amount)
			}
			return
		}
		if player, ok := s.Players[attackerOwnerID]; ok {
			player.RunDominionPointDrops += amount
		}
	}
}

// SetImmediateDominionPointDropHandler wires the fire-and-forget commit hook
// invoked when gameplay tuning's dominionPoints.commitMode is "immediate".
// Passing nil disables the immediate path (the default — tests do not need
// it).
func (s *GameState) SetImmediateDominionPointDropHandler(fn func(playerID string, amount int)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onDominionPointDropImmediate = fn
}
