package game

import "webrts/server/pkg/protocol"

// Action-bar auto-cast framework (Part 10).
//
// Generic over any AbilityDef with SupportsAutoCast == true. Per unit each
// tick the loop tries, for each auto-cast-enabled ability (in slot order):
// off cooldown? enough mana? a valid target from the ability's auto-cast
// selector? — if so it initiates a normal cast (cast_time, animation, mana,
// effect all via the Part 8 lifecycle). At most one cast is initiated per
// unit per tick, and never while a cast is already in progress, so cast_time
// is respected (no queuing/stacking).
//
// State is per-unit-instance (Unit.AutoCastEnabled / AbilityCooldowns) and
// dies with the unit. The loop iterates the ordered Abilities slice, never
// the maps, so it is deterministic under a fixed seed.

// toggleAutoCastLocked flips auto-cast for abilityID on unit. Returns the new
// enabled state and whether anything changed. It is a no-op (changed=false)
// when the unit does not have the ability, the ability id is unknown, or the
// ability's def has SupportsAutoCast == false (right-clicking a
// non-auto-cast ability has no effect, per spec).
//
// Caller holds s.mu.
func (s *GameState) toggleAutoCastLocked(unit *Unit, abilityID string) (enabled bool, changed bool) {
	if unit == nil || !containsAbility(unit, abilityID) {
		return false, false
	}
	def, ok := getAbilityDef(abilityID)
	if !ok || !def.SupportsAutoCast {
		return s.autoCastEnabledLocked(unit, abilityID), false
	}
	if unit.AutoCastEnabled == nil {
		unit.AutoCastEnabled = make(map[string]bool, 1)
	}
	unit.AutoCastEnabled[abilityID] = !unit.AutoCastEnabled[abilityID]
	return unit.AutoCastEnabled[abilityID], true
}

// autoCastEnabledLocked reports whether auto-cast is currently on for
// abilityID on unit. Caller holds s.mu.
func (s *GameState) autoCastEnabledLocked(unit *Unit, abilityID string) bool {
	return unit != nil && unit.AutoCastEnabled[abilityID]
}

// tickUnitAbilityCooldownsLocked decays this unit's ability cooldowns by dt,
// clamping at 0. Iterates the unit's ordered Abilities slice (not the map)
// so it never depends on map order. No-op when nothing is on cooldown.
//
// Caller holds s.mu.
func (s *GameState) tickUnitAbilityCooldownsLocked(unit *Unit, dt float64) {
	if unit == nil || len(unit.AbilityCooldowns) == 0 {
		return
	}
	for _, id := range unit.Abilities {
		if cd := unit.AbilityCooldowns[id]; cd > 0 {
			cd -= dt
			if cd <= 0 {
				delete(unit.AbilityCooldowns, id)
			} else {
				unit.AbilityCooldowns[id] = cd
			}
		}
	}
}

// tickUnitAutoCastLocked runs the auto-cast loop for one unit for this tick.
// It initiates at most one cast and never while the unit is already casting
// (so cast_time is respected). On a successful initiation the ability's
// cooldown is armed.
//
// Caller holds s.mu.
func (s *GameState) tickUnitAutoCastLocked(unit *Unit) {
	if unit == nil || unit.HP <= 0 || len(unit.AutoCastEnabled) == 0 {
		return
	}
	// A cast in progress blocks auto-cast — don't queue/stack another.
	if unit.CastAbilityID != "" {
		return
	}
	for _, abilityID := range unit.Abilities { // ordered ⇒ deterministic
		if !unit.AutoCastEnabled[abilityID] {
			continue
		}
		def, ok := getAbilityDef(abilityID)
		if !ok || !def.SupportsAutoCast {
			continue
		}
		if unit.AbilityCooldowns[abilityID] > 0 {
			continue // on cooldown
		}
		if unit.CurrentMana < def.ManaCost {
			continue // not enough mana
		}
		target := s.resolveAutoCastTargetLocked(unit, def)
		if target == nil {
			continue // no valid target right now
		}
		ok2, _ := s.beginAbilityCastLocked(unit, abilityID, target)
		if !ok2 {
			continue // initiation rejected (race) — try the next ability
		}
		if def.Cooldown > 0 {
			if unit.AbilityCooldowns == nil {
				unit.AbilityCooldowns = make(map[string]float64, 1)
			}
			unit.AbilityCooldowns[abilityID] = def.Cooldown
		}
		return // one auto-cast initiation per unit per tick
	}
}

// abilityStatesLocked builds the per-ability snapshot slice for unit's
// owner-facing UnitSnapshot: each ability the unit has, with its live
// auto-cast toggle and cooldown. Skips ids with no registered AbilityDef.
// Returns nil for units with no abilities (omitempty drops the field).
//
// Caller holds s.mu.
func (s *GameState) abilityStatesLocked(unit *Unit) []protocol.AbilitySnapshot {
	if unit == nil || len(unit.Abilities) == 0 {
		return nil
	}
	out := make([]protocol.AbilitySnapshot, 0, len(unit.Abilities))
	for _, id := range unit.Abilities {
		def, ok := getAbilityDef(id)
		if !ok {
			continue
		}
		out = append(out, protocol.AbilitySnapshot{
			ID:                def.ID,
			DisplayName:       def.DisplayName,
			Icon:              def.Icon,
			ManaCost:          def.ManaCost,
			SupportsAutoCast:  def.SupportsAutoCast,
			AutoCast:          unit.AutoCastEnabled[id],
			CooldownRemaining: unit.AbilityCooldowns[id],
			CooldownTotal:     def.Cooldown,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ── Player-facing entry points (lock-acquiring; mirror AttackWithUnits) ───────

// RequestAbilityCast is the player-issued standard-cast entry point (action
// bar left-click). Validates the caster is owned by playerID, then runs the
// Part 8 cast lifecycle. Returns (false, reason) on failure so the WS layer
// can surface `reason` via protocol.NotificationMessage (same pattern as
// "Not enough resources" on train_unit_command).
func (s *GameState) RequestAbilityCast(playerID string, casterUnitID int, abilityID string, targetUnitID int) (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := s.getUnitByIDLocked(casterUnitID)
	if caster == nil || caster.OwnerID != playerID {
		return false, castFailNotOwned
	}
	target := s.getUnitByIDLocked(targetUnitID)
	return s.beginAbilityCastLocked(caster, abilityID, target)
}

// ToggleAutoCast is the player-issued auto-cast toggle (action bar
// right-click). Validates ownership, then toggles. Returns the new enabled
// state and whether it changed (changed=false ⇒ silently no-op, e.g.
// right-clicking an ability that does not support auto-cast — the spec's
// "no effect"; the WS layer sends no notification in that case).
func (s *GameState) ToggleAutoCast(playerID string, unitID int, abilityID string) (enabled bool, changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	unit := s.getUnitByIDLocked(unitID)
	if unit == nil || unit.OwnerID != playerID {
		return false, false
	}
	return s.toggleAutoCastLocked(unit, abilityID)
}
