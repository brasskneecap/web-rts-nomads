package game

import "webrts/server/pkg/protocol"

type ThreatEntry struct {
	Value          float64
	LastSeenTick   int
	LastActiveTick int
}

type TargetWeights struct {
	Distance         float64
	InRange          float64
	Threat           float64
	TargetValue      float64
	TypePreference   float64
	Taunt            float64
	ProtectAllies    float64
	StructureDefense float64
	Reachability     float64
	Stickiness       float64
	DangerPenalty    float64
	AoECluster       float64
	HealthFinish     float64
}

type CombatProfile struct {
	Name                       string
	DetectionRange             float64
	RetargetIntervalTicks      int
	SwitchThreshold            float64
	ThreatDecayPerSecond       float64
	ThreatFromDamage           float64
	ThreatGenerationMultiplier float64
	PassiveMeleeThreat         float64
	LeashDistance              float64
	MaxChaseDistance           float64
	RetreatDistance            float64
	RetreatTriggerMeleeRange   float64
	TargetBuildings            bool
	PreferStructures           bool
	PreferMaxRange             bool
	Melee                      bool
	Frontline                  bool
	Backline                   bool
	DangerTolerance            float64
	AoERadius                  float64
	Weights                    TargetWeights
}

type combatTargetKind int

const (
	combatTargetNone combatTargetKind = iota
	combatTargetUnit
	combatTargetBuilding
)

type combatTarget struct {
	Kind     combatTargetKind
	Unit     *Unit
	Building *protocol.BuildingTile
	Score    float64
}

type combatEvalContext struct {
	index   *combatSpatialIndex
	blocked map[gridPoint]bool
}

const (
	combatSpatialBucketSize            = 160.0
	combatMeleeProximityRadius         = 72.0
	combatBacklineDefenseRadius        = 260.0
	combatStructureThreatRadius        = 220.0
	combatThreatVisibilityForgetTicks  = 60
	combatThreatStructureSplashRadius  = 240.0
	combatDangerFrontlineSupportRadius = 180.0
	combatTauntBonusScore              = 10000.0
)

func (s *GameState) initializeCombatUnitLocked(unit *Unit) {
	if unit.ThreatTable == nil {
		unit.ThreatTable = map[int]*ThreatEntry{}
	}
	if unit.CombatAnchorX == 0 && unit.CombatAnchorY == 0 {
		unit.CombatAnchorX = unit.X
		unit.CombatAnchorY = unit.Y
	}
}

func (s *GameState) tickCombatAILocked(dt float64, blocked map[gridPoint]bool) {
	index := newCombatSpatialIndex(combatSpatialBucketSize)
	for _, unit := range s.Units {
		if unit == nil || !unit.Visible || unit.HP <= 0 {
			continue
		}
		s.initializeCombatUnitLocked(unit)
		index.add(unit)
	}

	ctx := combatEvalContext{
		index:   index,
		blocked: blocked,
	}

	// Units advancing toward a destination (no active unit target) slide their
	// combat anchor to their current position each tick. This keeps the leash
	// centred on where they are now, so enemies they encounter along the way
	// are within leash range and can be scored normally.
	//
	// Applies to:
	//   - Enemy units advancing on an objective.
	//   - Player units on OrderAttackMove or OrderPatrol — the whole point of
	//     these orders is to engage anything encountered en route. Without
	//     sliding, the anchor sits at the destination and enemies near the
	//     unit (but far from the destination) fail the leash check, so the
	//     unit walks past them silently.
	//
	// Once a target is acquired, the anchor freezes at that position so the
	// standard leash check limits how far the chase can go.
	for _, unit := range s.Units {
		if unit == nil || !unit.Visible || unit.HP <= 0 || unit.AttackTargetID != 0 {
			continue
		}
		if unit.OwnerID == enemyPlayerID ||
			unit.Order.Type == OrderAttackMove ||
			unit.Order.Type == OrderPatrol {
			unit.CombatAnchorX = unit.X
			unit.CombatAnchorY = unit.Y
		}
	}

	for _, unit := range s.Units {
		if !s.unitUsesCombatAI(unit) {
			continue
		}
		s.decayThreatLocked(unit, dt, index)
	}

	for _, unit := range s.Units {
		if !s.unitUsesCombatAI(unit) {
			continue
		}
		if unit.Order.Type == OrderMove && unit.AttackTargetID == 0 && unit.AttackBuildingTargetID == "" {
			continue
		}
		s.evaluateCombatLocked(unit, ctx)
	}
}

func (s *GameState) unitUsesCombatAI(unit *Unit) bool {
	return unit != nil && unit.Visible && unit.HP > 0 && unit.Damage > 0 && containsString(unit.Capabilities, "attack")
}

func (s *GameState) evaluateCombatLocked(unit *Unit, ctx combatEvalContext) {
	profile := resolveCombatProfile(unit)
	if s.shouldDropCurrentTargetLocked(unit, profile, ctx) {
		s.clearCombatTargetLocked(unit)
	}

	// Player-issued attack targets are sticky. The AI must not retarget off
	// them in favor of a closer/higher-scored alternative, and must not
	// retreat — the player explicitly chose this fight. Dropping (target
	// dead/invalid) already cleared the flag in shouldDropCurrentTargetLocked.
	if unit.Order.Type == OrderAttackTarget && unit.AttackTargetID != 0 {
		return
	}

	// Gate A: Hold units never retreat — their contract is "stay here and fire".
	// Move units reaching this point have an existing attack target, so retreat
	// is also suppressed (they are mid-combat; dropping them here loses the fight).
	// shouldRetreatLocked has its own Order-type guard, but the early-return above
	// means we only reach here for Idle/AttackMove/Patrol/Hold-with-no-target.
	if unit.Order.Type != OrderHold && s.shouldRetreatLocked(unit, profile, ctx) {
		s.clearCombatTargetLocked(unit)
		s.issueRetreatLocked(unit, profile, ctx.blocked)
		return
	}

	shouldEvaluate := unit.AttackTargetID == 0 && unit.AttackBuildingTargetID == ""
	if !shouldEvaluate && profile.RetargetIntervalTicks > 0 {
		shouldEvaluate = s.Tick-unit.LastTargetEvalTick >= profile.RetargetIntervalTicks
	}
	if unit.TauntedByUnitID != 0 && unit.TauntRemaining > 0 {
		shouldEvaluate = true
	}
	if !shouldEvaluate {
		return
	}
	unit.LastTargetEvalTick = s.Tick

	best := s.selectBestTargetLocked(unit, profile, ctx)
	if best.Kind == combatTargetNone {
		if unit.OwnerID == enemyPlayerID && unit.AttackBuildingTargetID == "" && unit.AttackTargetID == 0 {
			s.assignEnemyObjectiveLocked(unit, ctx.blocked)
			return
		}
		// Gate D: resume standing order (AttackMove / Patrol) when no target.
		s.resumeStandingOrderLocked(unit, ctx.blocked)
		return
	}

	current := s.currentTargetScoreLocked(unit, profile, ctx)
	threshold := profile.SwitchThreshold
	if unit.AttackTargetID == 0 && unit.AttackBuildingTargetID == "" {
		threshold = 0
	}
	if best.Score < current+threshold {
		return
	}

	s.applyCombatTargetLocked(unit, best, ctx.blocked)
	unit.CurrentTargetScore = best.Score
}

func (s *GameState) applyCombatTargetLocked(unit *Unit, target combatTarget, blocked map[gridPoint]bool) {
	// Gate C: Hold units fire from current position — never path toward a target.
	holdUnit := unit.Order.Type == OrderHold

	switch target.Kind {
	case combatTargetUnit:
		unit.AttackTargetID = target.Unit.ID
		unit.AttackBuildingTargetID = ""
		unit.Attacking = false
		unit.Status = "Moving To Attack"
		if !holdUnit && distanceSquared(unit.X, unit.Y, target.Unit.X, target.Unit.Y) > unit.AttackRange*unit.AttackRange {
			s.refreshUnitAttackApproachLocked(unit, target.Unit, resolveCombatProfile(unit), blocked, true)
		}
	case combatTargetBuilding:
		unit.AttackTargetID = 0
		unit.AttackBuildingTargetID = target.Building.ID
		unit.Attacking = false
		unit.Status = "Moving To Attack"
		if !holdUnit && s.distanceToBuilding(unit.X, unit.Y, target.Building) > unit.AttackRange {
			if pos := s.findBestBuildingAttackPositionLocked(unit, target.Building, blocked); pos != nil {
				s.assignUnitPath(unit, *pos, blocked, nil)
			}
		}
	}
}

// resumeStandingOrderLocked re-issues movement toward the standing order
// destination when a unit on AttackMove or Patrol has no current attack target.
// For Patrol it also flips waypoints when the unit is within arrivalRadius of
// the current destination. Called from evaluateCombatLocked (Gate D).
func (s *GameState) resumeStandingOrderLocked(unit *Unit, blocked map[gridPoint]bool) {
	const patrolArrivalRadius = 20.0

	switch unit.Order.Type {
	case OrderAttackMove:
		if unit.Moving {
			return // already heading to destination
		}
		dest := protocol.Vec2{X: unit.Order.DestX, Y: unit.Order.DestY}
		if distanceSquared(unit.X, unit.Y, dest.X, dest.Y) < patrolArrivalRadius*patrolArrivalRadius {
			// Arrived — order complete, demote to Idle.
			unit.Order = OrderState{Type: OrderIdle}
			unit.Status = "Idle"
			return
		}
		s.assignUnitPath(unit, dest, blocked, nil)
		if unit.Moving {
			unit.Status = "Moving"
		}

	case OrderPatrol:
		dest := protocol.Vec2{X: unit.Order.DestX, Y: unit.Order.DestY}
		distSq := distanceSquared(unit.X, unit.Y, dest.X, dest.Y)
		if distSq < patrolArrivalRadius*patrolArrivalRadius {
			// Reached current waypoint — flip to the other endpoint.
			unit.Order.DestX, unit.Order.PatrolReturnX = unit.Order.PatrolReturnX, unit.Order.DestX
			unit.Order.DestY, unit.Order.PatrolReturnY = unit.Order.PatrolReturnY, unit.Order.DestY
			// Update anchor to new destination so leash is centred correctly.
			unit.CombatAnchorX = unit.Order.DestX
			unit.CombatAnchorY = unit.Order.DestY
			dest = protocol.Vec2{X: unit.Order.DestX, Y: unit.Order.DestY}
		}
		if unit.Moving {
			return // already heading somewhere
		}
		s.assignUnitPath(unit, dest, blocked, nil)
		if unit.Moving {
			unit.Status = "Patrolling"
		} else {
			unit.Status = "Patrol Blocked"
		}
	}
}

// SetUnitStance sets the standing order for the given units to "hold" or "idle".
// "hold" stops the unit in place and restricts target acquisition to AttackRange.
// "idle" releases the unit back to default AI behaviour.
func (s *GameState) SetUnitStance(playerID string, unitIDs []int, stance string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	orderID := s.nextMovementOrderIDLocked()

	for _, unitID := range unitIDs {
		unit := s.getUnitByIDLocked(unitID)
		if unit == nil || unit.OwnerID != playerID {
			continue
		}

		switch stance {
		case "hold":
			s.resetUnitMovementLocked(unit, orderID)
			unit.Order = OrderState{
				Type:  OrderHold,
				HoldX: unit.X,
				HoldY: unit.Y,
			}
			unit.CombatAnchorX = unit.X
			unit.CombatAnchorY = unit.Y
			unit.Status = "Hold"
		case "idle":
			s.resetUnitMovementLocked(unit, orderID)
			// Order already set to Idle by resetUnitMovementLocked.
		}
	}
}

// PatrolUnits issues an OrderPatrol to the given units. The unit's current
// position becomes one waypoint and dest becomes the other (one-click patrol).
func (s *GameState) PatrolUnits(playerID string, unitIDs []int, dest protocol.Vec2) {
	s.mu.Lock()
	defer s.mu.Unlock()

	blocked := s.getBlockedCellsLocked()
	orderID := s.nextMovementOrderIDLocked()

	for _, unitID := range unitIDs {
		unit := s.getUnitByIDLocked(unitID)
		if unit == nil || unit.OwnerID != playerID || !unitHasCapability(unit.UnitType, "attack") {
			continue
		}

		s.resetUnitMovementLocked(unit, orderID)
		unit.Order = OrderState{
			Type:          OrderPatrol,
			DestX:         dest.X,
			DestY:         dest.Y,
			PatrolReturnX: unit.X,
			PatrolReturnY: unit.Y,
		}
		unit.CombatAnchorX = dest.X
		unit.CombatAnchorY = dest.Y
		s.assignUnitPath(unit, dest, blocked, nil)
		if unit.Moving {
			unit.Status = "Patrolling"
		} else {
			unit.Status = "Patrol Blocked"
		}
	}
}

func (s *GameState) clearCombatTargetLocked(unit *Unit) {
	unit.AttackTargetID = 0
	unit.AttackBuildingTargetID = ""
	unit.Attacking = false
	// Demote sticky-attack order to Idle when the target is cleared.
	// AttackMove and Patrol keep their order so they can resume movement.
	if unit.Order.Type == OrderAttackTarget {
		unit.Order = OrderState{Type: OrderIdle}
	}
	unit.CurrentTargetScore = 0
	if !unit.Moving {
		unit.Status = "Idle"
	}
}
