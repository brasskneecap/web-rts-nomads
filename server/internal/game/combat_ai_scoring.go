package game

import (
	"math"
	"sort"
	"webrts/server/pkg/protocol"
)

func (s *GameState) shouldDropCurrentTargetLocked(unit *Unit, profile CombatProfile, ctx combatEvalContext) bool {
	if unit.AttackTargetID != 0 {
		target := s.getUnitByIDLocked(unit.AttackTargetID)
		if !s.combatTargetIsValidLocked(unit, target) {
			return true
		}
		// Player-issued targets bypass the leash — the whole point of
		// right-clicking a distant enemy is to chase that enemy, not to stay
		// anchored wherever the unit was when the order was given.
		if unit.Order.Type == OrderAttackTarget {
			return false
		}
		// Retaliation: a hostile in the threat table has dealt damage to this
		// unit recently. Bypass every leash (profile default AND guard leash)
		// so the unit can chase the attacker even if they're firing from beyond
		// the authored perimeter — guards in particular must not silently die
		// at their post to a ranged attacker just out of leash.
		//
		// For non-guards the bypass holds as long as the entry exists; profile
		// ThreatDecayPerSecond eventually deletes it. For guards the bypass is
		// gated on guardRetaliationPersistTicks since the last hit so the chase
		// has a deterministic 4-second window that each new attack refreshes —
		// without this the guard could chase a long-disengaged attacker for
		// many seconds while the threat amount decayed.
		if entry, ok := unit.ThreatTable[target.ID]; ok {
			if !unit.GuardMode || s.Tick-entry.LastActiveTick < guardRetaliationPersistTicks {
				return false
			}
		}
		// Guard units use their authored anchor as the leash origin and their
		// GuardLeashRange as the leash radius, not the profile default. Only
		// reached when the target is NOT in the threat table (e.g. a passive
		// proximity acquisition).
		if unit.GuardMode {
			distSq := distanceSquared(unit.GuardAnchorX, unit.GuardAnchorY, target.X, target.Y)
			leash := unit.GuardLeashRange
			return distSq > leash*leash
		}
		return !s.targetInsideLeashLocked(unit, target.X, target.Y, profile)
	}
	if unit.AttackBuildingTargetID != "" {
		building := s.getBuildingByIDLocked(unit.AttackBuildingTargetID)
		if building == nil {
			return true
		}
		hp, _, ok := getBuildingHP(building)
		if !ok || hp <= 0 {
			return true
		}
		// Buildings are static so there is no leash-chase concern.
		// However, drop the building target when hostile units are close enough
		// to engage — the scoring system will then pick them up instead of the
		// unit running straight past them to hit the building.
		// This mirrors the old tickEnemyAILocked aggroRadius behaviour.
		// For guards the drop radius must match the acquire radius
		// (GuardAggroRange), otherwise hostiles in the gap trigger a drop the
		// scorer can't replace — building target gets re-applied next tick and
		// applyCombatTargetLocked spams A* on every tick.
		dropRadius := effectiveDetectionRange(unit, profile) * 0.75
		if unit.GuardMode && unit.GuardAggroRange > 0 {
			dropRadius = unit.GuardAggroRange
		}
		for _, hostile := range ctx.index.query(unit.X, unit.Y, dropRadius) {
			if !playersAreHostile(hostile.OwnerID, unit.OwnerID) || hostile.HP <= 0 {
				continue
			}
			return true
		}
		return false
	}
	return false
}

func (s *GameState) selectBestTargetLocked(unit *Unit, profile CombatProfile, ctx combatEvalContext) combatTarget {
	best := combatTarget{Kind: combatTargetNone, Score: -math.MaxFloat64}

	// Gate B: Hold units never move, so restrict acquisition to weapons range.
	// This prevents a Hold unit from "pre-acquiring" a target it can't shoot
	// until the enemy closes to AttackRange, which would cause the unit to
	// sit Attacking with 0 damage dealt until the enemy finally steps in range.
	detectionRange := effectiveDetectionRange(unit, profile)
	if unit.GuardMode && unit.GuardAggroRange > 0 {
		// Guard units use their authored aggro range instead of the profile
		// default, measured from their current position (scan radius). The leash
		// is checked separately in shouldDropCurrentTargetLocked using the
		// anchor as origin.
		detectionRange = unit.GuardAggroRange
	} else if unit.Order.Type == OrderHold {
		detectionRange = unit.AttackRange
	}

	for _, hostile := range ctx.index.query(unit.X, unit.Y, detectionRange) {
		if hostile == unit || !playersAreHostile(hostile.OwnerID, unit.OwnerID) || hostile.HP <= 0 || !hostile.Visible {
			continue
		}
		if s.Tick < unit.UnreachableUntilTick && hostile.ID == unit.UnreachableTargetID {
			continue
		}
		if !s.targetInsideLeashLocked(unit, hostile.X, hostile.Y, profile) {
			continue
		}
		score := s.scoreUnitTargetLocked(unit, hostile, profile, ctx)
		if score > best.Score {
			best = combatTarget{Kind: combatTargetUnit, Unit: hostile, Score: score}
		}
	}

	// Retaliation acquisition: any hostile in the threat table has dealt
	// damage to this unit. They are eligible regardless of detection range or
	// leash so a unit can fight back against ranged attackers shooting from
	// beyond its sight (e.g. a perked archer firing at a soldier whose
	// profile DetectionRange is only 240). Pure Hold units (no GuardMode) skip
	// this — their contract is "engage in-range only," already enforced by
	// the AttackRange-capped detectionRange above. Guards DO retaliate without
	// any range cap so a ranged attacker can never silently chip them down
	// at the post; shouldDropCurrentTargetLocked grants the same threat-table
	// bypass on the chase side.
	allowRetaliation := unit.Order.Type != OrderHold || unit.GuardMode
	if allowRetaliation && len(unit.ThreatTable) > 0 {
		// Sort hostile IDs so tie-breaking on equal scores is deterministic.
		// Map iteration order is unspecified in Go and would otherwise let
		// identical inputs pick different targets across runs, breaking the
		// seeded-simulation contract.
		hostileIDs := make([]int, 0, len(unit.ThreatTable))
		for id := range unit.ThreatTable {
			hostileIDs = append(hostileIDs, id)
		}
		sort.Ints(hostileIDs)
		for _, hostileID := range hostileIDs {
			entry := unit.ThreatTable[hostileID]
			// Guards: only retaliate against attackers that hit within the
			// persistence window. Mirrors the chase-side gate in
			// shouldDropCurrentTargetLocked so acquisition and persistence
			// agree on what "actively attacking" means.
			if entry == nil || (unit.GuardMode && s.Tick-entry.LastActiveTick >= guardRetaliationPersistTicks) {
				continue
			}
			hostile := s.getUnitByIDLocked(hostileID)
			if hostile == nil || hostile == unit || hostile.HP <= 0 || !hostile.Visible {
				continue
			}
			if !playersAreHostile(hostile.OwnerID, unit.OwnerID) {
				continue
			}
			if s.Tick < unit.UnreachableUntilTick && hostile.ID == unit.UnreachableTargetID {
				continue
			}
			score := s.scoreUnitTargetLocked(unit, hostile, profile, ctx)
			if score > best.Score {
				best = combatTarget{Kind: combatTargetUnit, Unit: hostile, Score: score}
			}
		}
	}

	if profile.TargetBuildings {
		for i := range s.MapConfig.Buildings {
			building := &s.MapConfig.Buildings[i]
			if !s.isValidHostileBuildingTarget(unit, building) {
				continue
			}
			if s.Tick < unit.UnreachableUntilTick && building.ID == unit.UnreachableBuildingTargetID {
				continue
			}
			center := s.buildingCenterLocked(building)
			if distanceSquared(unit.X, unit.Y, center.X, center.Y) > detectionRange*detectionRange {
				continue
			}
			if !s.targetInsideLeashLocked(unit, center.X, center.Y, profile) {
				continue
			}
			score := s.scoreBuildingTargetLocked(unit, building, profile, ctx)
			if score > best.Score {
				best = combatTarget{Kind: combatTargetBuilding, Building: building, Score: score}
			}
		}
	}

	if unit.TauntedByUnitID != 0 && unit.TauntRemaining > 0 {
		taunter := s.getUnitByIDLocked(unit.TauntedByUnitID)
		if taunter != nil && taunter.Visible && taunter.HP > 0 && playersAreHostile(taunter.OwnerID, unit.OwnerID) {
			score := s.scoreUnitTargetLocked(unit, taunter, profile, ctx) + combatTauntBonusScore
			best = combatTarget{Kind: combatTargetUnit, Unit: taunter, Score: score}
		}
	}

	return best
}

func (s *GameState) currentTargetScoreLocked(unit *Unit, profile CombatProfile, ctx combatEvalContext) float64 {
	if unit.AttackTargetID != 0 {
		target := s.getUnitByIDLocked(unit.AttackTargetID)
		if target == nil || !target.Visible || target.HP <= 0 {
			return -math.MaxFloat64
		}
		return s.scoreUnitTargetLocked(unit, target, profile, ctx)
	}
	if unit.AttackBuildingTargetID != "" {
		building := s.getBuildingByIDLocked(unit.AttackBuildingTargetID)
		if building == nil {
			return -math.MaxFloat64
		}
		return s.scoreBuildingTargetLocked(unit, building, profile, ctx)
	}
	return -math.MaxFloat64
}

func (s *GameState) scoreUnitTargetLocked(unit, target *Unit, profile CombatProfile, ctx combatEvalContext) float64 {
	dist := math.Sqrt(distanceSquared(unit.X, unit.Y, target.X, target.Y))
	inRange := 0.0
	if dist <= unit.AttackRange {
		inRange = 1
	}
	moveNeed := math.Max(0, dist-unit.AttackRange)
	distanceScore := 1 - clamp01(dist/effectiveDetectionRange(unit, profile))
	reachScore := 1 - clamp01(moveNeed/math.Max(profile.MaxChaseDistance, 1))
	danger := s.estimateDangerScoreLocked(unit, target.X, target.Y, profile, ctx)
	threatScore := clamp01(s.getThreatValueLocked(unit, target.ID) / 80)
	targetValue := clamp01(s.unitStrategicValue(target) / 10)
	typePreference := clamp01((s.unitTypePreference(unit, target) + 6) / 12)
	protectScore := clamp01(s.backlineProtectionScoreLocked(unit.OwnerID, target) / 8)
	structureDefense := clamp01(s.structureDefenseScoreLocked(unit, target) / 8)
	cluster := 0.0
	if profile.AoERadius > 0 {
		cluster = clamp01(float64(s.countNearbyHostilesLocked(target, profile.AoERadius, ctx.index)-1) / 4)
	}
	healthFinish := 1 - clamp01(float64(target.HP)/math.Max(float64(target.MaxHP), 1))
	stickiness := 0.0
	if unit.AttackTargetID == target.ID {
		stickiness = 1
	}
	taunt := 0.0
	if unit.TauntedByUnitID == target.ID && unit.TauntRemaining > 0 {
		taunt = 1
	}

	w := profile.Weights
	score := 0.0
	score += distanceScore * w.Distance
	score += inRange * w.InRange
	score += threatScore * w.Threat
	score += targetValue * w.TargetValue
	score += typePreference * w.TypePreference
	score += taunt * w.Taunt * combatTauntBonusScore
	score += protectScore * w.ProtectAllies
	score += structureDefense * w.StructureDefense
	score += reachScore * w.Reachability
	score += stickiness * (w.Stickiness + profile.SwitchThreshold/2)
	score += cluster * w.AoECluster
	score += healthFinish * w.HealthFinish
	score -= danger * w.DangerPenalty
	return score
}

func (s *GameState) scoreBuildingTargetLocked(unit *Unit, building *protocol.BuildingTile, profile CombatProfile, ctx combatEvalContext) float64 {
	dist := s.distanceToBuilding(unit.X, unit.Y, building)
	inRange := 0.0
	if dist <= unit.AttackRange {
		inRange = 1
	}
	moveNeed := math.Max(0, dist-unit.AttackRange)
	reachScore := 1 - clamp01(moveNeed/math.Max(profile.MaxChaseDistance, 1))
	distanceScore := 1 - clamp01(dist/effectiveDetectionRange(unit, profile))
	importance := clamp01(s.buildingStrategicValue(building) / 12)
	cluster := 0.0
	if profile.AoERadius > 0 {
		center := s.buildingCenterLocked(building)
		cluster = clamp01(float64(s.countHostilesAroundPointLocked(unit.OwnerID, center.X, center.Y, profile.AoERadius, ctx.index)) / 4)
	}
	stickiness := 0.0
	if unit.AttackBuildingTargetID == building.ID {
		stickiness = 1
	}
	center := s.buildingCenterLocked(building)
	danger := s.estimateDangerScoreLocked(unit, center.X, center.Y, profile, ctx)

	w := profile.Weights
	score := 0.0
	score += distanceScore * w.Distance
	score += inRange * w.InRange
	score += importance * (w.TargetValue + w.TypePreference)
	score += reachScore * w.Reachability
	score += cluster * w.AoECluster
	score += stickiness * (w.Stickiness + profile.SwitchThreshold/2)
	score -= danger * w.DangerPenalty
	if profile.PreferStructures {
		score += 10
	}
	return score
}

func (s *GameState) unitStrategicValue(unit *Unit) float64 {
	profile := resolveCombatProfile(unit)
	value := 1.0
	if profile.Frontline {
		value += 1
	}
	if profile.Backline {
		value += 2
	}
	if profile.Name == "catapult" || profile.Name == "enemy_siege" {
		value += 3
	}
	if profile.Name == "support" || profile.Name == "mage" {
		value += 2.5
	}
	if profile.Name == "boss" {
		value += 5
	}
	value += (1 - clamp01(float64(unit.HP)/math.Max(float64(unit.MaxHP), 1))) * 0.5
	return value
}

func (s *GameState) buildingStrategicValue(building *protocol.BuildingTile) float64 {
	switch building.BuildingType {
	case "townhall":
		return 12
	case "barracks":
		return 9
	case "Tower":
		return 8
	case "farm":
		return 5
	default:
		return 4
	}
}

func (s *GameState) unitTypePreference(unit, target *Unit) float64 {
	profile := resolveCombatProfile(unit)
	targetProfile := resolveCombatProfile(target)

	switch profile.Name {
	case "soldier":
		if target.AttackBuildingTargetID != "" || s.targetThreatensBacklineLocked(unit.OwnerID, target) {
			return 5
		}
		if targetProfile.Backline {
			return 2
		}
	case "archer":
		if targetProfile.Frontline {
			return -2
		}
		if s.isEngagedByFriendlyFrontlineLocked(unit.OwnerID, target) {
			return 4
		}
		if targetProfile.Name == "support" || targetProfile.Name == "mage" || targetProfile.Name == "catapult" || targetProfile.Name == "enemy_siege" {
			return 5
		}
	case "mage":
		if targetProfile.Name == "support" || targetProfile.Name == "enemy_archer" || targetProfile.Name == "archer" {
			return 3
		}
	case "cavalry", "skirmisher":
		if targetProfile.Backline || targetProfile.Name == "support" || targetProfile.Name == "catapult" || targetProfile.Name == "enemy_siege" || targetProfile.Name == "enemy_archer" || targetProfile.Name == "archer" {
			return 6
		}
		if targetProfile.Frontline {
			return -3
		}
	case "catapult":
		if targetProfile.Name == "boss" {
			return 3
		}
	case "raider", "bruiser":
		if target.AttackBuildingTargetID != "" {
			return 2
		}
		if targetProfile.Backline {
			return 1
		}
	case "enemy_archer", "support":
		if targetProfile.Name == "mage" || targetProfile.Name == "archer" || targetProfile.Name == "catapult" {
			return 5
		}
		if targetProfile.Frontline {
			return -3
		}
	case "enemy_siege":
		if targetProfile.Frontline {
			return -4
		}
	case "boss":
		if targetProfile.Name == "catapult" || targetProfile.Name == "mage" {
			return 4
		}
	}
	return 0
}

func (s *GameState) backlineProtectionScoreLocked(ownerID string, target *Unit) float64 {
	if target.AttackBuildingTargetID != "" {
		return 6
	}
	if target.AttackTargetID == 0 {
		return 0
	}
	ally := s.getUnitByIDLocked(target.AttackTargetID)
	if ally == nil || ally.OwnerID != ownerID {
		return 0
	}
	profile := resolveCombatProfile(ally)
	if profile.Backline {
		return 7
	}
	if ally.UnitType == "worker" {
		return 5
	}
	return 1
}

func (s *GameState) structureDefenseScoreLocked(unit, target *Unit) float64 {
	best := 0.0
	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if building.OwnerID == nil || *building.OwnerID != unit.OwnerID {
			continue
		}
		center := s.buildingCenterLocked(building)
		if distanceSquared(target.X, target.Y, center.X, center.Y) > combatStructureThreatRadius*combatStructureThreatRadius {
			continue
		}
		score := s.buildingStrategicValue(building)
		if score > best {
			best = score
		}
	}
	return best / 2
}

func (s *GameState) targetThreatensBacklineLocked(ownerID string, target *Unit) bool {
	if target.AttackBuildingTargetID != "" {
		return true
	}
	if target.AttackTargetID == 0 {
		return false
	}
	ally := s.getUnitByIDLocked(target.AttackTargetID)
	if ally == nil || ally.OwnerID != ownerID {
		return false
	}
	return resolveCombatProfile(ally).Backline
}

func (s *GameState) isEngagedByFriendlyFrontlineLocked(ownerID string, target *Unit) bool {
	for _, ally := range s.Units {
		if ally.OwnerID != ownerID || ally.HP <= 0 || !ally.Visible {
			continue
		}
		if !resolveCombatProfile(ally).Frontline {
			continue
		}
		if ally.AttackTargetID == target.ID && distanceSquared(ally.X, ally.Y, target.X, target.Y) <= ally.AttackRange*ally.AttackRange*2.25 {
			return true
		}
	}
	return false
}

func (s *GameState) estimateDangerScoreLocked(unit *Unit, targetX, targetY float64, profile CombatProfile, ctx combatEvalContext) float64 {
	attackPoint := s.computeApproachPointLocked(unit, targetX, targetY, profile)
	meleeThreats := 0.0
	rangedThreats := 0.0
	for _, hostile := range ctx.index.query(attackPoint.X, attackPoint.Y, 180) {
		if !playersAreHostile(hostile.OwnerID, unit.OwnerID) || hostile.HP <= 0 {
			continue
		}
		hostileProfile := resolveCombatProfile(hostile)
		dist := math.Sqrt(distanceSquared(attackPoint.X, attackPoint.Y, hostile.X, hostile.Y))
		if hostileProfile.Melee || hostile.AttackRange <= 80 {
			if dist <= hostile.AttackRange+40 {
				meleeThreats++
			}
			continue
		}
		if dist <= hostile.AttackRange {
			rangedThreats += 0.5
		}
	}

	frontlineSupportDist := math.MaxFloat64
	for _, ally := range ctx.index.query(attackPoint.X, attackPoint.Y, combatDangerFrontlineSupportRadius) {
		if ally.OwnerID != unit.OwnerID || ally.HP <= 0 {
			continue
		}
		if !resolveCombatProfile(ally).Frontline {
			continue
		}
		dist := math.Sqrt(distanceSquared(attackPoint.X, attackPoint.Y, ally.X, ally.Y))
		if dist < frontlineSupportDist {
			frontlineSupportDist = dist
		}
	}

	supportPenalty := 1.0
	if frontlineSupportDist <= combatDangerFrontlineSupportRadius {
		supportPenalty = clamp01(frontlineSupportDist / combatDangerFrontlineSupportRadius)
	}

	danger := meleeThreats + rangedThreats + supportPenalty
	return clamp01(danger / math.Max(profile.DangerTolerance*3, 1))
}

func (s *GameState) isValidHostileBuildingTarget(unit *Unit, building *protocol.BuildingTile) bool {
	if building == nil || !building.Visible || building.OwnerID == nil || !playersAreHostile(*building.OwnerID, unit.OwnerID) {
		return false
	}
	hp, _, ok := getBuildingHP(building)
	return ok && hp > 0
}
