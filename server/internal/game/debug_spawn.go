package game

import (
	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// DEBUG SPAWN — DEV TOOL
//
// Entry point for the `debug_spawn_unit` WebSocket command. Spawns a fully
// configured enemy unit at a chosen world position with a chosen path / rank
// / perk loadout. Only honored when MapConfig.Debug.DebugSpawn == true so
// production maps cannot be exploited.
//
// Unlike the normal rank-up flow, this helper bypasses:
//   - Random path assignment (path is set verbatim from the request)
//   - Perk pool filtering (perks are appended verbatim — a Silver with
//     requiresPerk: "fire_pit" will land even without the Bronze, because
//     the whole point of the debug tool is to test arbitrary combos)
//   - XP / rank-up progression (rank is set directly and stats applied once)
//
// Stat pipeline order is identical to a naturally-ranked unit though:
//   1. Spawn via spawnUnitFromDefLocked (base stats + Rank=base)
//   2. Overwrite ProgressionPath + Rank
//   3. Append PerkIDs
//   4. applyRankModifiersLocked(unit, false) — path/rank modifiers apply
//   5. Optional HP override (after rank scaling so % overrides don't get lost)
// ═════════════════════════════════════════════════════════════════════════════

// DebugSpawnEnabled reports whether the active map allows debug spawns.
func (s *GameState) DebugSpawnEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.MapConfig.Debug != nil && s.MapConfig.Debug.DebugSpawn
}

// DebugSpawnUnit handles a `debug_spawn_unit` command. Returns true when the
// spawn landed; returns false (and is a no-op) when the map does not have
// the debug flag set or when the requested unit type cannot be resolved.
//
// callerPlayerID is the WebSocket-authenticated player sending the command.
// It is the default owner when msg.Team is empty or "mine"; msg.Team == "enemy"
// routes ownership to the NPC/wave player instead.
func (s *GameState) DebugSpawnUnit(msg protocol.DebugSpawnUnitMessage, callerPlayerID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.MapConfig.Debug == nil || !s.MapConfig.Debug.DebugSpawn {
		return false
	}
	if msg.UnitType == "" {
		return false
	}

	// Resolve ownership from the Team field. Default to the caller ("mine")
	// so debug spawns join the sender's army and respond to their commands —
	// the common case when iterating on your own perk loadouts. Team="enemy"
	// falls back to the wave/NPC player.
	ownerPlayerID := callerPlayerID
	ownerColor := ""
	if msg.Team == "enemy" {
		s.ensureEnemyPlayerLocked()
		ownerPlayerID = enemyPlayerID
		ownerColor = enemyPlayerColor
	} else {
		if player, ok := s.Players[callerPlayerID]; ok {
			ownerColor = player.Color
		}
	}
	if ownerPlayerID == "" {
		// Caller isn't actually in a match (shouldn't happen — the WS handler
		// guards on MatchID before dispatching). Bail rather than creating an
		// orphan unit with empty OwnerID.
		return false
	}

	spawn := protocol.Vec2{X: msg.X, Y: msg.Y}

	// Reuse the regular PLAYER spawn path (spawnPlayerUnitLocked, not the lower
	// level spawnUnitFromDefLocked) so archetype / capabilities / XP plumbing
	// AND the owning player's advancement-effective UnitDef both light up — a
	// debug-spawned archer gets the same advancement buffs (HP/damage/attack-
	// speed/spawn-EXP/bonus-arrows/trap mods) a naturally-trained one would.
	// For enemy-owned spawns the player has no advancements, so this degrades
	// to the raw catalog def. Returns nil for an unknown unit type.
	unit := s.spawnPlayerUnitLocked(msg.UnitType, ownerPlayerID, ownerColor, spawn)
	if unit == nil {
		return false
	}

	// Apply the requested path and rank directly — no random roll, no
	// progression-gate. Empty strings map to the "none" / "base" defaults
	// that spawnUnitFromDefLocked already set.
	if msg.Path != "" {
		unit.ProgressionPath = msg.Path
	}
	if msg.Rank != "" {
		unit.Rank = msg.Rank
	}

	// Roll archetype ability-pool picks for the debug-set rank (§11) before the
	// recompute reads them — debug spawn can jump straight to gold, so this
	// rolls every reached rank once. No-op when the archetype has no pool.
	s.rollUnitPoolAbilitiesLocked(unit)

	// Fire path-ability grants for the chosen (path, rank). The natural
	// rank-up path runs this from addUnitXPLocked, but debug spawn sets the
	// rank directly without going through XP, so the grant has to be invoked
	// here too. Without this, a debug-spawned Bronze Cleric would keep base
	// "heal" instead of receiving "greater_heal" via the path-ability grant.
	s.assignUnitPathAbilitiesLocked(unit)

	// Append perks verbatim. The UI is expected to pass them in rank order
	// (bronze, silver, gold) — the runtime hooks iterate PerkIDs as an
	// unordered set so order only matters for tie-breaks in the HUD.
	//
	// applyPerkGrantedHooksLocked is the post-grant extension seam. It
	// currently has no per-perk consumers (the heal → greater_heal swap
	// moved to the path-ability path above), but is invoked here so future
	// ability-replacing perks pick up debug-spawned units automatically.
	for _, perkID := range msg.PerkIDs {
		if perkID == "" {
			continue
		}
		unit.PerkIDs = append(unit.PerkIDs, perkID)
		s.applyPerkGrantedHooksLocked(unit, perkID)
	}

	// Re-run path-ability derivation now that perks are appended so any
	// perk-granted abilities (PerkDef.GrantsAbilities — Siphoner bronze
	// lingering_hex / mark_of_weakness, future ability-granting perks)
	// land on unit.Abilities. The earlier call above runs BEFORE perks
	// are appended (path-level override + rank grants seed the list);
	// this second call layers perk-grants on top idempotently.
	s.assignUnitPathAbilitiesLocked(unit)

	// Reapply rank / path modifiers so the debug-set rank actually scales
	// MaxHP / Damage / MoveSpeed. preserveHealthPercent=false because the
	// spawn is fresh (HP == MaxHP at this point).
	s.applyRankModifiersLocked(unit, false)

	// Re-derive inventory size from the debug-set rank. spawnPlayerUnitLocked
	// already sized the inventory, but it ran while the unit was still base
	// rank (the rank override above happens afterward), and applyRankModifiers
	// Locked does not touch inventory. Without this a Gold debug spawn keeps
	// the base-rank single slot. Guarded to player-owned units to mirror the
	// spawn-time guard — enemy units carry no inventory.
	if ownerPlayerID != enemyPlayerID && ownerPlayerID != neutralPlayerID {
		s.setInventorySizeForRankLocked(unit)
	}

	// Custom HP override (after rank scaling so e.g. "spawn a Bronze at 50
	// HP for last_stand testing" works even when the rank multiplied MaxHP).
	if msg.CustomHP > 0 {
		if msg.CustomHP > unit.MaxHP {
			unit.MaxHP = msg.CustomHP
		}
		unit.HP = msg.CustomHP
	}

	return true
}
