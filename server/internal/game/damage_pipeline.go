package game

// ═════════════════════════════════════════════════════════════════════════════
// Centralized death pipeline
//
// Problem: indirect damage paths (Shared Pain, pain_share redirect,
// retaliation) kill units other than the primary target. The outer call site
// only checks HP on the primary, so secondarily-killed units sat at HP=0
// forever — the regen loop skips them (HP>0 gate) and they were never cleaned
// up. Players saw standing corpses.
//
// Solution: every damage entry point calls enqueueDeathLocked when a unit
// reaches HP<=0. drainPendingDeathsLocked runs once per tick from Update(),
// after all combat/trap/projectile ticks finish. It handles kill bookkeeping
// for attributed kills and removeUnitLocked for all enqueued deaths.
//
// Dedup rule: first-to-kill wins. pendingDeathsSet prevents double-XP for the
// same unit from both the call-site manual bookkeeping and the drain.
// ═════════════════════════════════════════════════════════════════════════════

// DamageSource identifies who caused a damage event for kill attribution.
// The zero value (anonymous) means the call site is doing its own kill
// bookkeeping — the drain will only do removal, not XP/stats.
type DamageSource struct {
	AttackerUnitID     int    // 0 if not from a unit
	AttackerBuildingID string // "" if not from a building
	AttackerTrapID     string // "" if not from a trap
	// Kind is a human-readable label used for debugging/telemetry.
	// Examples: "melee", "projectile", "building", "savage_strikes",
	// "whirlwind", "cleave", "shared_pain", "pain_share_redirect", "retaliation".
	Kind string
	// DamageType is the element / school of this damage event, set from the
	// attacker's attack or the ability definition (NOT the projectile). The
	// zero value means "unspecified"; ResolvedDamageType() maps it to
	// DamagePhysical so the many existing DamageSource{} call sites keep
	// behaving as physical with no edits. Flavor/metadata only today —
	// see damage_type.go.
	DamageType DamageType
}

// ResolvedDamageType returns the damage event's element, defaulting an unset
// DamageType to DamagePhysical. Read damage type through this rather than the
// raw field so "unspecified" is always explicit.
func (d DamageSource) ResolvedDamageType() DamageType {
	return d.DamageType.OrPhysical()
}

// IsAnonymous returns true when the source carries no attacker attribution.
// Anonymous deaths are still cleaned up by the drain but do not award XP.
func (d DamageSource) IsAnonymous() bool {
	return d.AttackerUnitID == 0 && d.AttackerBuildingID == "" && d.AttackerTrapID == ""
}

// pendingDeath is a single entry in the per-tick death queue.
type pendingDeath struct {
	UnitID int
	Source DamageSource
}

// enqueueDeathLocked records a unit that hit HP<=0 during the current tick.
// First-to-enqueue wins (the pendingDeathsSet prevents double-entries so the
// XP credit goes to whoever killed the unit first).
//
// Safe to call on a nil target or a still-alive target — both are no-ops.
// Must be called under s.mu write lock.
func (s *GameState) enqueueDeathLocked(target *Unit, src DamageSource) {
	if target == nil || target.HP > 0 {
		return
	}
	if s.pendingDeathsSet[target.ID] {
		return // first-to-kill wins; ignore subsequent enqueues this tick
	}
	s.pendingDeathsSet[target.ID] = true
	s.pendingDeaths = append(s.pendingDeaths, pendingDeath{UnitID: target.ID, Source: src})
}

// drainPendingDeathsLocked processes the per-tick death queue built up by
// applyUnitDamageWithSourceLocked. For each entry:
//
//   - If the unit is already gone (call site ran removeUnitLocked itself before
//     the drain), skip — that call site already handled XP/stats. This is the
//     safe coexistence path for legacy call sites.
//   - If still present with HP<=0, run full kill bookkeeping using the
//     DamageSource attribution, then removeUnitLocked.
//   - If HP>0 (re-healed — hypothetical; no revive perk exists yet), skip.
//
// Must be called once per tick from Update(), AFTER all combat/trap/projectile
// ticks have run and BEFORE the per-unit loop that assumes dead units are gone.
// Placing it here prevents HP=0 units from entering the per-unit regen loop.
//
// Determinism: we iterate over the slice (insertion order). The set is only
// used for membership checks — it is never iterated.
func (s *GameState) drainPendingDeathsLocked() {
	if len(s.pendingDeaths) == 0 {
		return
	}
	// Snapshot and reset the queue so any re-entrant kills (none expected, but
	// defensively) would land in a fresh queue rather than extending our loop.
	deaths := s.pendingDeaths
	s.pendingDeaths = nil
	s.pendingDeathsSet = make(map[int]bool)

	for _, d := range deaths {
		target := s.getUnitByIDLocked(d.UnitID)
		if target == nil {
			// Already removed by the primary call site — skip.
			continue
		}
		if target.HP > 0 {
			// Re-healed before drain (no such perk exists today, but be safe).
			continue
		}

		if !d.Source.IsAnonymous() {
			// Resolve attacker and run kill bookkeeping.
			if d.Source.AttackerUnitID != 0 {
				attackerUnit := s.getUnitByIDLocked(d.Source.AttackerUnitID)
				if attackerUnit != nil {
					s.awardKillXPLocked(attackerUnit)
					s.payoutDamageDealtXPLocked(target)
					s.awardSoldierTankKillXPLocked(target.ID)
					s.onPerkKillLocked(attackerUnit)
					s.trackBattleKillLocked(battleSourceFromUnit(attackerUnit), target)
					s.rollLegendPointDropLocked(attackerUnit.OwnerID, target)
					if target.ObjectiveID != "" {
						s.markObjectiveKillLocked(target.ObjectiveID)
					}
				}
			} else if d.Source.AttackerBuildingID != "" {
				building := s.getBuildingByIDLocked(d.Source.AttackerBuildingID)
				if building != nil {
					s.trackBattleKillLocked(battleSourceFromBuilding(building), target)
					if target.ObjectiveID != "" {
						s.markObjectiveKillLocked(target.ObjectiveID)
					}
				}
			} else if d.Source.AttackerTrapID != "" {
				// Resolve trap and its owner unit — mirrors the pattern used
				// in detonateExplosiveTrapLocked and other trap kill paths.
				var trap *Trap
				for _, t := range s.Traps {
					if t != nil && t.ID == d.Source.AttackerTrapID {
						trap = t
						break
					}
				}
				if trap != nil {
					ownerUnit := s.getUnitByIDLocked(trap.OwnerUnitID)
					if ownerUnit != nil && ownerUnit.HP <= 0 {
						ownerUnit = nil
					}
					if ownerUnit != nil {
						s.awardKillXPLocked(ownerUnit)
						s.payoutDamageDealtXPLocked(target)
						s.awardSoldierTankKillXPLocked(target.ID)
						s.trackBattleKillLocked(battleSourceFromTrap(trap), target)
					} else {
						// Trapper died; still track the kill under the trap source
						// for battle telemetry.
						s.trackBattleKillLocked(battleSourceFromTrap(trap), target)
					}
					if target.ObjectiveID != "" {
						s.markObjectiveKillLocked(target.ObjectiveID)
					}
				}
			}
		}
		// Anonymous or after bookkeeping: remove the unit. If the unit was
		// already removed by the call site above, removeUnitLocked is safe
		// (removeUnitByIDLocked is a no-op for unknown IDs).
		s.removeUnitLocked(d.UnitID)
	}
}
