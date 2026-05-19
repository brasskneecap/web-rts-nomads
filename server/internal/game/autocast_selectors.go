package game

// Auto-cast target selectors.
//
// An AutoCastSelector picks the unit an auto-cast-enabled ability should fire
// at this evaluation, or nil for "no valid target right now". Selectors are
// looked up by name (AbilityDef.AutoCastTargetSelector) from an extensible
// registry, so new abilities can introduce new targeting strategies without
// touching the auto-cast loop (built in the action-bar part).
//
// Determinism: selectors iterate s.Units (a slice — stable order) and use
// fully-ordered tiebreakers, never map iteration, so a given state always
// yields the same pick under a fixed seed.

// AutoCastSelector returns the auto-cast target for caster using ability def,
// or nil when there is no valid target. Caller holds s.mu (selectors resolve
// live units and must read consistent state).
type AutoCastSelector func(s *GameState, caster *Unit, def AbilityDef) *Unit

// autoCastSelectors is the name→selector registry. Written only at init /
// via RegisterAutoCastSelector; read-only during simulation, so it adds no
// determinism or concurrency concern (lookups are by explicit name).
var autoCastSelectors = map[string]AutoCastSelector{}

// RegisterAutoCastSelector adds (or replaces) a selector under name. Intended
// for init-time registration. Panics on an empty name (the empty selector
// name means "no auto-cast targeting" and must not be registerable).
func RegisterAutoCastSelector(name string, fn AutoCastSelector) {
	if name == "" {
		panic("RegisterAutoCastSelector: selector name must be non-empty")
	}
	if fn == nil {
		panic("RegisterAutoCastSelector: nil selector for " + name)
	}
	autoCastSelectors[name] = fn
}

// getAutoCastSelector returns the selector registered under name. ok is false
// for an unknown (or empty) name — callers treat that as "no target".
func getAutoCastSelector(name string) (AutoCastSelector, bool) {
	fn, ok := autoCastSelectors[name]
	return fn, ok
}

// resolveAutoCastTargetLocked resolves the auto-cast target for caster's use
// of def: looks up def.AutoCastTargetSelector and runs it. Returns nil when
// the ability names no selector, the name is unregistered, or the selector
// finds no valid target. This is the single entry point the auto-cast loop
// calls. Caller holds s.mu.
func (s *GameState) resolveAutoCastTargetLocked(caster *Unit, def AbilityDef) *Unit {
	if caster == nil || def.AutoCastTargetSelector == "" {
		return nil
	}
	fn, ok := getAutoCastSelector(def.AutoCastTargetSelector)
	if !ok {
		return nil
	}
	return fn(s, caster, def)
}

func init() {
	RegisterAutoCastSelector("lowest_hp_percentage_ally_in_range", selectLowestHPPercentageAllyInRange)
	RegisterAutoCastSelector("closest_enemy_in_range", selectClosestEnemyInRange)
	RegisterAutoCastSelector("self", selectSelf)
}

// selectLowestHPPercentageAllyInRange returns the friendly unit (same owner
// as the caster — includes the caster itself iff the ability can target
// self) that is in cast range, below 100% HP, and has the lowest
// current/max HP ratio. Ties are broken by closest distance, then by lowest
// unit ID (final deterministic tiebreak). Returns nil when no friendly unit
// in range is below full HP.
func selectLowestHPPercentageAllyInRange(s *GameState, caster *Unit, def AbilityDef) *Unit {
	if caster == nil {
		return nil
	}
	var best *Unit
	var bestDistSq float64
	for _, u := range s.Units {
		if u == nil || u.MaxHP <= 0 {
			continue
		}
		// Same-team allies only (alliance is Player.TeamID, not ownership) —
		// filters out enemies and non-allied owners regardless of the
		// ability's flags. The ability must still be able to target it
		// (handles self-vs-ally permission and the alive check).
		if !s.unitsFriendlyLocked(caster, u) {
			continue
		}
		if !s.canAbilityTargetUnitLocked(def, caster, u) {
			continue
		}
		if u.HP >= u.MaxHP { // not below 100%
			continue
		}
		if !def.WithinCastRange(caster, u) {
			continue
		}

		distSq := distanceSquared(caster.X, caster.Y, u.X, u.Y)
		if best == nil {
			best, bestDistSq = u, distSq
			continue
		}
		// Lower HP% wins. Compare a.HP/a.MaxHP < b.HP/b.MaxHP without floats:
		// a.HP*b.MaxHP < b.HP*a.MaxHP (MaxHP > 0). int64 avoids overflow.
		lhs := int64(u.HP) * int64(best.MaxHP)
		rhs := int64(best.HP) * int64(u.MaxHP)
		switch {
		case lhs < rhs:
			best, bestDistSq = u, distSq
		case lhs == rhs:
			// Tie on HP% → closest distance, then lowest unit ID.
			if distSq < bestDistSq || (distSq == bestDistSq && u.ID < best.ID) {
				best, bestDistSq = u, distSq
			}
		}
	}
	return best
}

// selectClosestEnemyInRange is the offensive auto-cast selector. The Arch Mage
// path's `arcane_bolt` (Phase 2) uses it. Returns the closest visible hostile
// unit the ability can target within cast range, ties broken by lowest unit ID.
// TODO: revisit tuning (threat weighting, target priority) — currently pure
// closest-in-range; the priority scorer (ability_priority.go) handles
// heal-vs-offensive selection, this only resolves the offensive target.
func selectClosestEnemyInRange(s *GameState, caster *Unit, def AbilityDef) *Unit {
	if caster == nil {
		return nil
	}
	var best *Unit
	var bestDistSq float64
	for _, u := range s.Units {
		if u == nil || u.HP <= 0 || !u.Visible {
			continue
		}
		if !s.unitsHostileLocked(caster, u) { // hostiles only (different team)
			continue
		}
		if !s.canAbilityTargetUnitLocked(def, caster, u) || !def.WithinCastRange(caster, u) {
			continue
		}
		distSq := distanceSquared(caster.X, caster.Y, u.X, u.Y)
		if best == nil || distSq < bestDistSq || (distSq == bestDistSq && u.ID < best.ID) {
			best, bestDistSq = u, distSq
		}
	}
	return best
}

// selectSelf is a placeholder for future self-buff auto-cast abilities (none
// use it yet). Returns the caster when the ability can target self, else nil.
// TODO: revisit when a real self-buff auto-cast ability exists (e.g. only
// when a buff is not already active).
func selectSelf(s *GameState, caster *Unit, def AbilityDef) *Unit {
	if caster == nil || !s.canAbilityTargetUnitLocked(def, caster, caster) {
		return nil
	}
	return caster
}
