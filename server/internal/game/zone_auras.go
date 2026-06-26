package game

import "webrts/server/pkg/protocol"

// ═════════════════════════════════════════════════════════════════════════════
// PLAYER ZONE AURA MANAGER
//
// Zones may grant passive bonuses (auras) to their owner while controlled. This
// file is the single owner of "zone control → player stat modifiers": it
// collects every owned zone's auras, aggregates them into each player's
// PlayerStatModifierSet, and re-applies cached stats — so units never poll zone
// ownership. The existing hot-path stat read sites then fold in one O(1)
// (add, mul) term via playerStatModifierLocked (see stat_modifiers.go).
//
// Recompute is EVENT-DRIVEN, not per-tick: it runs only when a zone's owner
// changes, funnelled through setZoneOwnerLocked → onZoneOwnershipChangedLocked.
// Because a zone's Owner can be the "team" sentinel (not a specific player id),
// a flip recomputes EVERY real player's aggregate rather than trying to resolve
// an old/new player from the sentinel — flips are rare and the player count is
// small, so a full recompute is trivial and avoids sentinel-resolution bugs.
//
// EXTENSION SEAMS (see proposal/design "Future Extensibility"):
//   - Aura kind: collectZoneAuraModifiers switches on aura.Type. New kinds
//     ("periodic", "spawn", "vision", "debuff", …) add a case / their own
//     manager pass; the "stat_modifier" path and this aggregation are untouched.
//   - Aura scope: v1 treats every aura as global (applies to all of the owner's
//     units regardless of position). A future "radius"/"regional" scope reads
//     aura.Scope at evaluation time; the global path here does not change.
//
// All functions must be called under s.mu write lock (they mutate player +
// unit state) and are deterministic: iteration is over the stable authored
// s.Zones slice and the player map is only ever fully rebuilt (no per-stat map
// iteration drives outcomes — adds sum and muls product, both commutative).
// ═════════════════════════════════════════════════════════════════════════════

// setZoneOwnerLocked is the single chokepoint through which a zone's owner is
// reassigned. It captures the previous owner, assigns the new one, and — only
// on an actual change — fires the ownership-change hook that recomputes aura
// modifiers. Every capture mechanic (presence / control_point / clear / claim)
// and install must route owner writes through here so the aura aggregate can
// never drift out of sync with zone control.
//
// Must be called under s.mu write lock.
func (s *GameState) setZoneOwnerLocked(rt *zoneRuntime, newOwner string) {
	if rt == nil {
		return
	}
	old := rt.Owner
	if old == newOwner {
		return
	}
	rt.Owner = newOwner
	s.onZoneOwnershipChangedLocked(rt.Def.ID, old, newOwner)
}

// onZoneOwnershipChangedLocked is invoked whenever a zone's owner actually
// changes. It recomputes every player's aggregated zone-aura modifiers and
// re-applies cached stats (max health / max mana) so bonuses transfer to the
// new owner and drop from the old one immediately. Ownership loss (a flip to
// neutral or an enemy) is handled by the same recompute — the lost zone simply
// no longer contributes to the old owner's aggregate.
//
// oldOwner/newOwner are accepted for future targeted recomputes and telemetry;
// v1 recomputes all real players because owner can be the "team" sentinel.
//
// Must be called under s.mu write lock.
func (s *GameState) onZoneOwnershipChangedLocked(zoneID, oldOwner, newOwner string) {
	s.recomputeAllZoneAuraModifiersLocked()
}

// recomputeAllZoneAuraModifiersLocked rebuilds every real player's zone-aura
// modifier set from current zone ownership and re-applies cached stats. Cheap
// enough to run on any ownership flip (O(players × zones × auras) + a unit
// pass); never call it on the per-tick hot path.
//
// Must be called under s.mu write lock.
func (s *GameState) recomputeAllZoneAuraModifiersLocked() {
	for id, player := range s.Players {
		if player == nil || !isHumanOwner(id) {
			continue // skip the AI / neutral virtual players — they own nothing real
		}
		player.ZoneStatModifiers = s.collectZoneAuraModifiersLocked(id)
	}
	// Cached, folded stats (MaxHP / MaxMana) do not re-derive on demand, so a
	// changed aggregate must be re-baked onto each affected unit. The on-demand
	// stats (armor, speeds, damage, regen, gather, production) pick up the new
	// aggregate on their next read and need no trigger here.
	s.reapplyCachedAuraStatsLocked()
}

// collectZoneAuraModifiersLocked builds the aggregated modifier set a single
// player receives from every zone it (or an ally) currently controls. Iterates
// zones in stable authored order; folds each stat_modifier aura per the shared
// stacking rule. Returns a fresh, non-nil set.
//
// Must be called under s.mu (read or write) lock.
func (s *GameState) collectZoneAuraModifiersLocked(playerID string) PlayerStatModifierSet {
	set := newPlayerStatModifierSet()
	for i := range s.Zones {
		rt := &s.Zones[i]
		if len(rt.Def.Auras) == 0 {
			continue
		}
		if !s.zonesAlliedLocked(rt.Owner, playerID) {
			continue // only zones this player's team controls contribute
		}
		for _, aura := range rt.Def.Auras {
			switch aura.Type {
			case protocol.ZoneAuraTypeStatModifier:
				set.fold(aura.Modifier)
			default:
				// Unknown / future aura kind: ignored by the stat aggregator.
				// Its own manager pass (when added) handles it; the loader has
				// already rejected genuinely unregistered types at load.
			}
		}
	}
	return set
}

// reapplyCachedAuraStatsLocked re-bakes the cached, folded stats (max health /
// max mana) for every real player's units after their aggregate changed,
// preserving each unit's current health/mana fraction. Uses applyRankModifiers
// Locked — the same recompute equip/upgrade already perform.
//
// Must be called under s.mu write lock.
func (s *GameState) reapplyCachedAuraStatsLocked() {
	for _, u := range s.Units {
		if u == nil {
			continue
		}
		if !isHumanOwner(u.OwnerID) {
			continue
		}
		s.applyRankModifiersLocked(u, true)
	}
}
