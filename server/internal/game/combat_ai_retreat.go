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

	// Drift mode is the explicit "I can't path; just walk straight-line toward
	// target.X/Y" state. While drifting, the per-tick combat loop must not
	// re-run A* against a moving target — every tick the target wiggles, the
	// in-range check below would fail and a full sub-cell A* would fire,
	// reproducing the per-tick storm we removed elsewhere. AI eval at
	// RetargetIntervalTicks cadence is responsible for re-evaluating drifted
	// units (applyCombatTargetLocked → assignAttackApproachPathLocked retries
	// the path with force=true, exiting drift on success).
	if unit.AttackDrifting && !force {
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
	s.assignAttackApproachPathLockedWithSubBlocked(unit, target, blocked, nil)
}

// assignAttackApproachPathLockedWithSubBlocked is the internal variant that
// accepts a pre-built sub-cell blocked map. Used by batch handlers
// (AttackWithUnits) to share one map across all members of a shared-OrderID
// group instead of rebuilding it per unit. When subBlocked is nil this falls
// back to the standard per-call build path.
func (s *GameState) assignAttackApproachPathLockedWithSubBlocked(unit, target *Unit, blocked map[gridPoint]bool, subBlocked map[gridPoint]bool) {
	if target == nil {
		unit.Path = nil
		unit.Moving = false
		unit.AttackDrifting = false
		return
	}

	rangeSq := unit.AttackRange * unit.AttackRange
	if distanceSquared(unit.X, unit.Y, target.X, target.Y) <= rangeSq {
		unit.Path = nil
		unit.Moving = false
		unit.AttackDrifting = false
		return
	}

	startCell := s.worldToGrid(unit.X, unit.Y)
	if rs, ok := s.findNearestWalkable(startCell, blocked); ok {
		startCell = rs
	}

	targetCell := s.worldToGrid(target.X, target.Y)
	goalCell, ok := s.findNearestWalkable(targetCell, blocked)
	if !ok {
		s.enterAttackDriftLocked(unit, target)
		return
	}

	fullPath := s.findPath(startCell, goalCell, blocked, nil)
	if len(fullPath) == 0 {
		s.enterAttackDriftLocked(unit, target)
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

	unit.AttackDrifting = false
	s.assignUnitPathWithSubBlocked(unit, stopWaypoint, blocked, subBlocked, nil)
	// assignUnitPath can itself fail (sub-cell A* finds no route through unit
	// obstacles). Fall back to drift in that case so the unit still makes
	// directional progress instead of standing idle.
	if !unit.Moving {
		s.enterAttackDriftLocked(unit, target)
	}
}

// assignAttackGroupPathsLocked handles leader-follower pathing for a group of
// units all attacking the same target. The leader is whichever unit has the
// SHORTEST AttackRange — its path stops deepest into the target's vicinity,
// so every follower (with equal or longer range) can truncate the leader's
// path at the first waypoint within their own range. Drops per-command cost
// from O(K) sub-cell A*s to one plus K cheap LoS checks.
//
// Units already within attack range of the target are skipped (no pathing
// needed). Single-attacker case falls through to the existing per-unit path.
// LoS-blocked followers fall back to their own assignAttackApproachPathLocked
// run, preserving correctness when the group is split by an obstacle.
//
// Caller is responsible for assigning OrderID / order / anchor / AttackTargetID
// to each unit beforehand; this helper only writes Path / Moving / TargetX/Y /
// AttackDrifting / stuck-sample state.
func (s *GameState) assignAttackGroupPathsLocked(attackers []*Unit, target *Unit, blocked map[gridPoint]bool, groundSub, flyerSub map[gridPoint]bool) {
	if target == nil || len(attackers) == 0 {
		return
	}

	subFor := func(u *Unit) map[gridPoint]bool {
		if u != nil && u.Flyer {
			return flyerSub
		}
		return groundSub
	}

	// Only units currently out of range need pathing.
	needsPath := make([]*Unit, 0, len(attackers))
	for _, unit := range attackers {
		if unit == nil {
			continue
		}
		if distanceSquared(unit.X, unit.Y, target.X, target.Y) > unit.AttackRange*unit.AttackRange {
			needsPath = append(needsPath, unit)
		}
	}

	if len(needsPath) == 0 {
		return
	}
	if len(needsPath) == 1 {
		s.assignAttackApproachPathLockedWithSubBlocked(needsPath[0], target, blocked, subFor(needsPath[0]))
		return
	}

	// Leader = shortest AttackRange. That unit's stopWaypoint will be the
	// deepest along the approach path, so followers (range >= leader.range)
	// can always find a truncation point earlier in the same path.
	leaderIdx := 0
	minRange := needsPath[0].AttackRange
	for i, u := range needsPath {
		if u.AttackRange < minRange {
			minRange = u.AttackRange
			leaderIdx = i
		}
	}
	leader := needsPath[leaderIdx]

	s.assignAttackApproachPathLockedWithSubBlocked(leader, target, blocked, subFor(leader))

	// Leader couldn't path or entered drift mode — sharing a non-existent path
	// helps no one. Fall back to per-unit pathing for the rest.
	if !leader.Moving || len(leader.Path) == 0 {
		for i, unit := range needsPath {
			if i == leaderIdx {
				continue
			}
			s.assignAttackApproachPathLockedWithSubBlocked(unit, target, blocked, subFor(unit))
		}
		return
	}

	leaderPath := leader.Path
	firstWaypoint := leaderPath[0]

	for i, unit := range needsPath {
		if i == leaderIdx {
			continue
		}

		// LoS gate — follower must be able to reach the leader's first
		// waypoint without crossing impassable terrain. If blocked, the
		// follower is on the wrong side of an obstacle and needs its own A*.
		if !s.lineWalkableLocked(unit.X, unit.Y, firstWaypoint.X, firstWaypoint.Y, blocked, unit.Flyer) {
			s.assignAttackApproachPathLockedWithSubBlocked(unit, target, blocked, subFor(unit))
			continue
		}

		// Walk leader's waypoints; the first one within follower's range is
		// follower's stop. Since leader has shortest range, this index is
		// guaranteed to exist (worst case == leader's final waypoint).
		rangeSq := unit.AttackRange * unit.AttackRange
		stopIdx := len(leaderPath) - 1
		for idx, wp := range leaderPath {
			if distanceSquared(wp.X, wp.Y, target.X, target.Y) <= rangeSq {
				stopIdx = idx
				break
			}
		}

		// Copy leader's path prefix as the follower's path. Copy (not slice-
		// share) so per-unit Path mutations during movement don't trample
		// the leader's path.
		followerPath := make([]protocol.Vec2, stopIdx+1)
		copy(followerPath, leaderPath[:stopIdx+1])
		unit.Path = followerPath
		unit.Moving = true
		unit.AttackDrifting = false
		unit.TargetX = followerPath[len(followerPath)-1].X
		unit.TargetY = followerPath[len(followerPath)-1].Y
		unit.StuckSampleX = unit.X
		unit.StuckSampleY = unit.Y
		unit.StuckSampleAccum = 0
	}
}

// enterAttackDriftLocked puts the unit into drift mode toward target's current
// coordinates. The per-unit movement loop steps straight-line each tick (no A*)
// and silently stops when the next cell is blocked. Replaces the strike-count
// escalation that the unit-target memo system previously used.
func (s *GameState) enterAttackDriftLocked(unit, target *Unit) {
	unit.AttackDrifting = true
	unit.TargetX = target.X
	unit.TargetY = target.Y
	unit.Path = nil
	unit.Moving = true
	unit.Status = "Moving To Attack"
	// Reset the stuck-progress sample so the watchdog measures from this new
	// drift origin if Path is later assigned by a successful repath.
	unit.StuckSampleX = unit.X
	unit.StuckSampleY = unit.Y
	unit.StuckSampleAccum = 0
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
		if !s.playersAreHostileLocked(hostile.OwnerID, unit.OwnerID) || hostile.HP <= 0 {
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
		if !s.playersAreHostileLocked(hostile.OwnerID, unit.OwnerID) || hostile.HP <= 0 || !hostile.Visible {
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
	// Guards override the profile leash with their authored GuardLeashRange so
	// acquisition and chase-drop agree on the same radius. Without this, a
	// player unit sitting in the band between GuardLeashRange and the profile
	// leash gets acquired here, then immediately dropped by
	// shouldDropCurrentTargetLocked which uses GuardLeashRange — producing the
	// chase/drop juggling on melee guards.
	if unit.GuardMode && unit.GuardLeashRange > 0 {
		leash = unit.GuardLeashRange
	}
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
		unit.AttackTargetID = 0
		unit.Attacking = false
		unit.Status = "Moving To Attack"
		if pos := s.findBestBuildingAttackPositionLocked(unit, building, blocked); pos != nil {
			unit.AttackBuildingTargetID = building.ID
			unit.UnreachableBuildingTargetID = ""
			unit.UnreachableBuildingStrikeCount = 0
			s.assignUnitPath(unit, *pos, blocked, nil)
			return
		}
		// No path to this building — apply escalation; strike-3 falls through to
		// the townhall path below instead of returning early.
		if unit.UnreachableBuildingTargetID == building.ID {
			unit.UnreachableBuildingStrikeCount++
		} else {
			unit.UnreachableBuildingStrikeCount = 1
		}
		unit.UnreachableBuildingTargetID = building.ID
		switch {
		case unit.UnreachableBuildingStrikeCount >= 3:
			unit.UnreachableBuildingStrikeCount = 0
			// Fall through to townhall path below.
		case unit.UnreachableBuildingStrikeCount == 2:
			unit.UnreachableUntilTick = s.Tick + 120
			return
		default:
			unit.UnreachableUntilTick = s.Tick + unreachableTargetCooldownTicks
			return
		}
	}
	target := s.getNearestPlayerTownhallCenterLocked(unit.X, unit.Y)
	if target != nil && !unit.Moving {
		unit.Status = "Advancing"
		s.assignUnitPath(unit, *target, blocked, nil)
	}
}

// enemyAdvanceToObjectiveLocked is the routed-enemy analog of
// resumeStandingOrderLocked's OrderAttackMove case: the enemy plain-moves
// toward a sticky objective building and lets normal in-range scoring engage
// whatever it meets (unit or building) on the way. It NEVER sets
// AttackBuildingTargetID, computes a perimeter slot, or runs strike escalation
// — the townhall is destroyed via ordinary scoreBuildingTargetLocked once it
// comes into detection range and commits like any other building.
//
// Fallback chain (see docs/superpowers/specs/2026-05-18-enemy-objective-
// attack-move-design.md):
//  1. Resolve & validate the sticky objective building; re-acquire the nearest
//     player building (honoring TargetPlayerID) when it died/disappeared. With
//     no building at all, fall back to a townhall-center position; with none,
//     idle (the evaluateCombatLocked cooldown guard prevents per-tick retry).
//  2. If already Moving toward it, return — the per-tick anti-churn guard that
//     removes the old re-acquisition stutter.
//  3. assignUnitPath to the objective position (a plain move).
//  4. If that path is impossible (objective fully walled off), engage the
//     nearest hostile anywhere so killing through reopens the route; the
//     drop-on-death -> re-advance flow then resumes.
func (s *GameState) enemyAdvanceToObjectiveLocked(unit *Unit, blocked map[gridPoint]bool) {
	// (1) Resolve / re-acquire the sticky objective building. isValidHostile-
	// BuildingTarget handles a nil building and validates visible/hostile/hp>0.
	building := s.getBuildingByIDLocked(unit.ObjectiveBuildingID)
	if !s.isValidHostileBuildingTarget(unit, building) {
		building = nil
		unit.ObjectiveBuildingID = ""
		if unit.TargetPlayerID != "" {
			building = s.findNearestAttackableBuildingForPlayerLocked(unit, unit.TargetPlayerID)
		}
		if building == nil {
			building = s.findNearestAttackablePlayerBuildingLocked(unit)
		}
		if building != nil {
			unit.ObjectiveBuildingID = building.ID
		}
	}

	var objectivePos protocol.Vec2
	if building != nil {
		objectivePos = s.buildingCenterLocked(building)
	} else if thc := s.getNearestPlayerTownhallCenterLocked(unit.X, unit.Y); thc != nil {
		objectivePos = *thc
	} else {
		// Nothing to advance on. Idle in place; evaluateCombatLocked's
		// NextObjectiveSearchTick / global cooldown stops this re-running
		// every tick.
		return
	}

	// (2) Already advancing — do not recompute. This is the guard that
	// removes the per-tick re-acquisition stutter.
	if unit.Moving {
		return
	}

	// (3) Plain move toward the objective. No attack target, no escalation.
	unit.AttackTargetID = 0
	unit.AttackBuildingTargetID = ""
	unit.Attacking = false
	unit.Status = "Advancing"
	s.assignUnitPath(unit, objectivePos, blocked, nil)
	if unit.Moving {
		return
	}

	// (4) Objective exists but is completely partitioned off by a wall of
	// units/terrain. Push the enemy at the nearest hostile anywhere; normal
	// in-range scoring engages it as the enemy closes, and killing through
	// reopens the route. (acquireNearestBlockingHostileLocked already issues a
	// movement path and ignores the DetectionRange cap.)
	s.acquireNearestBlockingHostileLocked(unit, blocked)
}
