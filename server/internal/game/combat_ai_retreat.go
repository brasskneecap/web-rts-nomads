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

	dest := s.computeApproachPointLocked(unit, target.X, target.Y, profile)
	if !force {
		if profile.Melee {
			// Melee units should commit to the current chase line longer; otherwise
			// two moving units can keep re-pathing around each other and visibly wobble.
			return
		}
		const retargetMoveThreshold = 18.0
		if distanceSquared(unit.TargetX, unit.TargetY, dest.X, dest.Y) < retargetMoveThreshold*retargetMoveThreshold {
			return
		}
	}

	s.assignUnitPath(unit, dest, blocked, nil)
}

func (s *GameState) shouldRetreatLocked(unit *Unit, profile CombatProfile, ctx combatEvalContext) bool {
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
		if hostile.OwnerID == unit.OwnerID || hostile.HP <= 0 {
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
		if hostile.OwnerID == unit.OwnerID || hostile.HP <= 0 || !hostile.Visible {
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
	if profile.LeashDistance <= 0 {
		return true
	}
	return distanceSquared(unit.CombatAnchorX, unit.CombatAnchorY, targetX, targetY) <= profile.LeashDistance*profile.LeashDistance
}

func (s *GameState) assignEnemyObjectiveLocked(unit *Unit, blocked map[gridPoint]bool) {
	building := s.findNearestAttackablePlayerBuildingLocked(unit)
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
