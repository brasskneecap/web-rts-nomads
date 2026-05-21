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
//
// Focus-target reservation: when the caster has FocusTargetID set AND the
// ability is heal-category, the standard selector is BYPASSED. The Cleric
// reserves its mana for the focus target — it only auto-heals the focus
// (or, with battle_prayer, refreshes the buff on the focus once it gets
// stale), never any other ally. This makes the player's Focus Target
// command a real resource-management decision instead of a soft hint.
// Offensive abilities (e.g. arcane_bolt → selectClosestEnemyInRange) are
// not affected — focus is a support-side concept.
func (s *GameState) resolveAutoCastTargetLocked(caster *Unit, def AbilityDef) *Unit {
	if caster == nil || def.AutoCastTargetSelector == "" {
		return nil
	}
	if def.Category == AbilityCategoryHeal && caster.FocusTargetID != 0 {
		return s.resolveFocusHealTargetLocked(caster, def)
	}
	fn, ok := getAutoCastSelector(def.AutoCastTargetSelector)
	if !ok {
		return nil
	}
	return fn(s, caster, def)
}

// resolveFocusHealTargetLocked is the focus-aware heal selector. It returns
// the focus target when a cast is justified, or nil to reserve mana.
//
//   - Focus invalid / out of range / wrong allegiance → nil (the Cleric is
//     following toward the focus; once in range, the next tick reconsiders).
//   - Focus injured (HP < MaxHP) → cast on focus.
//   - Focus at full HP + caster owns battle_prayer + focus's buff is below
//     the recast threshold → cast on focus to refresh.
//   - Otherwise → nil (save mana).
//
// Caller holds s.mu.
func (s *GameState) resolveFocusHealTargetLocked(caster *Unit, def AbilityDef) *Unit {
	focus := s.getUnitByIDLocked(caster.FocusTargetID)
	if focus == nil || focus.HP <= 0 || !focus.Visible {
		return nil
	}
	if !s.unitsFriendlyLocked(caster, focus) {
		return nil
	}
	if !s.canAbilityTargetUnitLocked(def, caster, focus) {
		return nil
	}
	if !def.WithinCastRange(caster, focus) {
		return nil
	}
	// Injured focus: heal it.
	if focus.HP < focus.MaxHP {
		return focus
	}
	// Full-HP focus: only cast to refresh battle_prayer when the buff is
	// stale enough to be worth a recast.
	if !containsString(caster.PerkIDs, "battle_prayer") {
		return nil
	}
	bpDef := perkDefByID("battle_prayer")
	if bpDef == nil {
		return nil
	}
	cfg := bpDef.ConfigForRank(caster.Rank)
	duration := cfg["buffDurationSeconds"]
	thresholdPct := cfg["recastThresholdPercent"]
	if duration <= 0 || thresholdPct <= 0 {
		return nil
	}
	if focus.PerkState.BattlePrayerRemaining < thresholdPct*duration {
		return focus
	}
	return nil
}

func init() {
	RegisterAutoCastSelector("lowest_hp_percentage_ally_in_range", selectLowestHPPercentageAllyInRange)
	RegisterAutoCastSelector("closest_enemy_in_range", selectClosestEnemyInRange)
	RegisterAutoCastSelector("self", selectSelf)
}

// buildCastTargetSetLocked constructs the full []*Unit slice that
// resolveAbilityCastLocked applies the ability's effect to. Handles both
// single-target (def.TargetCount == 1) and multi-target abilities, plus the
// force-include logic for battle_prayer (task 2.3 / 6.6).
//
// For TargetCount == 1 the result is always [primary].
//
// For TargetCount > 1:
//   - Collect valid candidates (alive, visible, friendly, in cast range, below
//     full HP), sorted ascending by HP%, ties broken by ascending unit.ID.
//   - primary is always guaranteed in the set (it was validated by the caller);
//     if not already in the top-N it displaces the worst natural pick.
//   - If the caster owns battle_prayer and FocusTargetID resolves to a valid
//     in-range ally, that unit is force-included (even at full HP), displacing
//     the highest-HP-percent natural pick when the set is full.
//   - Truncate to def.TargetCount.
//
// Caller holds s.mu.
func (s *GameState) buildCastTargetSetLocked(caster *Unit, def AbilityDef, primary *Unit) []*Unit {
	if def.TargetCount <= 1 || def.AutoCastTargetSelector == "" {
		return []*Unit{primary}
	}

	// Collect candidates below full HP, in cast range, friendly, alive/visible.
	// s.Units is a slice so iteration order is deterministic.
	cands := make([]*Unit, 0, def.TargetCount+2)
	for _, u := range s.Units {
		if u == nil || u.HP <= 0 || !u.Visible {
			continue
		}
		if !s.unitsFriendlyLocked(caster, u) {
			continue
		}
		if !s.canAbilityTargetUnitLocked(def, caster, u) {
			continue
		}
		if u.HP >= u.MaxHP {
			continue // full HP skipped by default; force-include handles exceptions
		}
		if !def.WithinCastRange(caster, u) {
			continue
		}
		cands = append(cands, u)
	}

	// Sort ascending by HP%, ties broken by ascending unit.ID.
	castTargetSortByHPPct(cands)

	// Cap to TargetCount before applying force-includes so the set never
	// grows beyond the limit.
	if len(cands) > def.TargetCount {
		cands = cands[:def.TargetCount]
	}

	// Guarantee primary is in the set.
	cands = castTargetForceInclude(cands, primary, def.TargetCount)

	// Force-include focus when caster owns battle_prayer (even at full HP).
	if containsString(caster.PerkIDs, "battle_prayer") && caster.FocusTargetID != 0 {
		focus := s.getUnitByIDLocked(caster.FocusTargetID)
		if focus != nil && focus.HP > 0 && focus.Visible &&
			s.unitsFriendlyLocked(caster, focus) &&
			s.canAbilityTargetUnitLocked(def, caster, focus) &&
			def.WithinCastRange(caster, focus) {
			cands = castTargetForceInclude(cands, focus, def.TargetCount)
		}
	}

	return cands
}

// castTargetSortByHPPct sorts a []*Unit slice in place: ascending HP%, ties
// broken by ascending unit.ID. Uses integer cross-multiplication to avoid
// floating-point. Insertion sort — N is always ≤ TargetCount (≤ ~5).
func castTargetSortByHPPct(cands []*Unit) {
	for i := 1; i < len(cands); i++ {
		for j := i; j > 0; j-- {
			a, b := cands[j-1], cands[j]
			// a.HP/a.MaxHP > b.HP/b.MaxHP  ⟺  a.HP*b.MaxHP > b.HP*a.MaxHP
			lhs := int64(a.HP) * int64(b.MaxHP)
			rhs := int64(b.HP) * int64(a.MaxHP)
			if lhs > rhs || (lhs == rhs && a.ID > b.ID) {
				cands[j-1], cands[j] = cands[j], cands[j-1]
			} else {
				break
			}
		}
	}
}

// castTargetForceInclude ensures unit u is in cands. If u is already present
// (by pointer identity) it is a no-op. If the set has room (len < cap), u is
// appended. If the set is full, the highest-HP-percent unit (last element after
// sorting) is displaced. Returns the (possibly modified) slice.
func castTargetForceInclude(cands []*Unit, u *Unit, cap int) []*Unit {
	if u == nil {
		return cands
	}
	for _, c := range cands {
		if c == u {
			return cands // already present
		}
	}
	if len(cands) < cap {
		return append(cands, u)
	}
	// Displace the worst (highest HP%) — last element after sort.
	cands[len(cands)-1] = u
	// Re-sort so the ordering invariant holds for any subsequent force-includes.
	castTargetSortByHPPct(cands)
	return cands
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
