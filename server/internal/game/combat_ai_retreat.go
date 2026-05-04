package game

import (
	"math"
	"webrts/server/pkg/protocol"
)

func (s *GameState) computeApproachPointLocked(unit *Unit, targetX, targetY float64, profile CombatProfile) protocol.Vec2 {
	if !profile.PreferMaxRange && !profile.Melee {
		return protocol.Vec2{X: targetX, Y: targetY}
	}
	dx := targetX - unit.X
	dy := targetY - unit.Y
	dist := math.Sqrt(dx*dx + dy*dy)
	desired := math.Max(unit.AttackRange*0.92, unit.AttackRange-20)
	if profile.Melee {
		// Stop just inside attack range instead of chasing the target's center.
		// This reduces overlap/separation oscillation in melee duels.
		desired = math.Max(unit.AttackRange*0.85, unitSeparationDistance)
	}
	if dist <= desired || dist == 0 {
		return protocol.Vec2{X: unit.X, Y: unit.Y}
	}
	scale := (dist - desired) / dist
	return protocol.Vec2{
		X: clampFloat(unit.X+dx*scale, unitRadius, s.MapWidth-unitRadius),
		Y: clampFloat(unit.Y+dy*scale, unitRadius, s.MapHeight-unitRadius),
	}
}

func (s *GameState) refreshUnitAttackApproachLocked(unit, target *Unit, profile CombatProfile, blocked map[gridPoint]bool, force bool) {
	if target == nil {
		return
	}

	if !force {
		// Skip re-pathing while the current destination still puts us within
		// attack range of where the target is now. This is more permissive
		// than the old "approach-point shifted by < N pixels" check, but it's
		// the right invariant: if we're already heading toward an in-range
		// cell, we don't need a new plan just because the target wiggled.
		if distanceSquared(unit.TargetX, unit.TargetY, target.X, target.Y) <= unit.AttackRange*unit.AttackRange {
			return
		}
	}

	_ = profile // kept on the signature for symmetry with other approach helpers; truncation does not depend on it
	s.assignAttackApproachPathLocked(unit, target, blocked)
}

// assignAttackApproachPathLocked routes `unit` toward `target` using A* on
// the full terrain graph and truncates the path at the first waypoint that
// puts the unit within attack range. Replaces the older "project a single
// approach point on the unit→target line and assignUnitPath to it" flow,
// which wobbled when the projected point landed on the wrong side of a
// blocked region (a melee unit trying to reach a ranged unit across a cliff
// would snap to the cliff face on its own side, can't get any closer,
// re-evaluates, picks a slightly different cliff-edge cell, and oscillates).
//
// With path truncation, A* finds a route around the obstacle and the unit
// stops as soon as it can attack, regardless of which side of the obstacle
// that turns out to be on.
func (s *GameState) assignAttackApproachPathLocked(unit, target *Unit, blocked map[gridPoint]bool) {
	if target == nil {
		unit.Path = nil
		unit.Moving = false
		return
	}

	rangeSq := unit.AttackRange * unit.AttackRange
	if distanceSquared(unit.X, unit.Y, target.X, target.Y) <= rangeSq {
		unit.Path = nil
		unit.Moving = false
		return
	}

	startCell := s.worldToGrid(unit.X, unit.Y)
	if rs, ok := s.findNearestWalkable(startCell, blocked); ok {
		startCell = rs
	}

	targetCell := s.worldToGrid(target.X, target.Y)
	goalCell, ok := s.findNearestWalkable(targetCell, blocked)
	if !ok {
		unit.Path = nil
		unit.Moving = false
		return
	}

	fullPath := s.findPath(startCell, goalCell, blocked, nil)
	if len(fullPath) == 0 {
		unit.Path = nil
		unit.Moving = false
		return
	}

	// Pick the first waypoint already within attack range. The unit only
	// needs to walk that far. If no waypoint is in range (target is in a
	// pocket reachable only by walking onto a non-walkable cell), fall
	// through to the full path so the unit at least makes best-effort
	// progress — better than standing still.
	stopWaypoint := fullPath[len(fullPath)-1]
	for _, p := range fullPath {
		if distanceSquared(p.X, p.Y, target.X, target.Y) <= rangeSq {
			stopWaypoint = p
			break
		}
	}

	s.assignUnitPath(unit, stopWaypoint, blocked, nil)
}

func (s *GameState) shouldRetreatLocked(unit *Unit, profile CombatProfile, ctx combatEvalContext) bool {
	if unit.ObjectiveID != "" {
		return false
	}
	if profile.RetreatDistance <= 0 || profile.RetreatTriggerMeleeRange <= 0 {
		return false
	}
	if unit.AttackTargetID != 0 {
		target := s.getUnitByIDLocked(unit.AttackTargetID)
		if target != nil && target.Visible && target.HP > 0 {
			if distanceSquared(unit.X, unit.Y, target.X, target.Y) <= unit.AttackRange*unit.AttackRange {
				// Ranged units should still take obvious shots instead of panic-walking
				// every evaluation tick.
				return false
			}
		}
	}
	meleeThreats := 0
	for _, hostile := range ctx.index.query(unit.X, unit.Y, profile.RetreatTriggerMeleeRange) {
		if !playersAreHostile(hostile.OwnerID, unit.OwnerID) || hostile.HP <= 0 {
			continue
		}
		hostileProfile := resolveCombatProfile(hostile)
		if hostileProfile.Melee || hostile.AttackRange <= 80 {
			meleeThreats++
		}
	}
	return meleeThreats > 0
}

func (s *GameState) issueRetreatLocked(unit *Unit, profile CombatProfile, blocked map[gridPoint]bool) {
	var awayX, awayY float64
	count := 0.0
	for _, hostile := range s.Units {
		if !playersAreHostile(hostile.OwnerID, unit.OwnerID) || hostile.HP <= 0 || !hostile.Visible {
			continue
		}
		hostileProfile := resolveCombatProfile(hostile)
		if !hostileProfile.Melee && hostile.AttackRange > 80 {
			continue
		}
		distSq := distanceSquared(unit.X, unit.Y, hostile.X, hostile.Y)
		if distSq > profile.RetreatTriggerMeleeRange*profile.RetreatTriggerMeleeRange {
			continue
		}
		awayX += unit.X - hostile.X
		awayY += unit.Y - hostile.Y
		count++
	}
	if count == 0 {
		return
	}
	length := math.Sqrt(awayX*awayX + awayY*awayY)
	if length == 0 {
		return
	}
	dest := protocol.Vec2{
		X: clampFloat(unit.X+(awayX/length)*profile.RetreatDistance, unitRadius, s.MapWidth-unitRadius),
		Y: clampFloat(unit.Y+(awayY/length)*profile.RetreatDistance, unitRadius, s.MapHeight-unitRadius),
	}
	s.assignUnitPath(unit, dest, blocked, nil)
	unit.Status = "Repositioning"
}

func (s *GameState) targetInsideLeashLocked(unit *Unit, targetX, targetY float64, profile CombatProfile) bool {
	leash := effectiveLeashDistance(unit, profile)
	if leash <= 0 {
		return true
	}
	return distanceSquared(unit.CombatAnchorX, unit.CombatAnchorY, targetX, targetY) <= leash*leash
}

func (s *GameState) assignEnemyObjectiveLocked(unit *Unit, blocked map[gridPoint]bool) {
	// If this enemy was spawned to target a specific player, prefer that
	// player's buildings. Falls through to the nearest-anywhere logic only when
	// the targeted player has no live owned buildings (e.g. they've been
	// eliminated). Without this, every wave enemy would unconditionally drift
	// toward the geographically nearest base, defeating per-spawnpoint
	// targetPlayerLabel routing in multi-player matches.
	var building *protocol.BuildingTile
	if unit.TargetPlayerID != "" {
		building = s.findNearestAttackableBuildingForPlayerLocked(unit, unit.TargetPlayerID)
	}
	if building == nil {
		building = s.findNearestAttackablePlayerBuildingLocked(unit)
	}
	if building != nil {
		unit.AttackBuildingTargetID = building.ID
		unit.AttackTargetID = 0
		unit.Attacking = false
		unit.Status = "Moving To Attack"
		if pos := s.findBestBuildingAttackPositionLocked(unit, building, blocked); pos != nil {
			s.assignUnitPath(unit, *pos, blocked, nil)
		}
		return
	}
	target := s.getNearestPlayerTownhallCenterLocked(unit.X, unit.Y)
	if target != nil && !unit.Moving {
		unit.Status = "Advancing"
		s.assignUnitPath(unit, *target, blocked, nil)
	}
}
