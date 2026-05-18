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
	// PreferClosestTarget collapses target scoring to "pick the geometrically
	// closest valid candidate." Use for enemy combat profiles that should
	// engage whatever is in front of them rather than walking past nearer
	// targets to chase a higher-scored one further out. Validity, leash, and
	// unreachable-memo filters still apply; only the score function changes.
	PreferClosestTarget        bool
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
	// enemyObjectiveSearchCooldownTicks throttles enemyAdvanceToObjectiveLocked
	// after a fruitless search. 1 second at 20Hz — long enough to keep the
	// per-tick cost under control, short enough that a freshly-built player
	// building gets attacked promptly.
	enemyObjectiveSearchCooldownTicks = 20
	// unreachableTargetCooldownTicks is how long a unit ignores a target whose
	// A* path came back empty, preventing per-tick pathfinding storms when many
	// units crowd around an inaccessible enemy (~2 seconds at 20Hz).
	unreachableTargetCooldownTicks = 40
	// approachRepathCooldownTicks throttles the forced repath in tickUnitCombatLocked
	// when the sub-cell A* fails (unit surrounded by a crowd). 3 ticks = 0.15s —
	// short enough that the unit retries almost immediately as the crowd shifts,
	// long enough to prevent running full sub-cell A* every tick on a permanently
	// blocked unit. The separation system (applyUnitSeparationLocked) nudges
	// units apart between retries, so paths typically clear within 1-2 windows.
	approachRepathCooldownTicks = 3
	// retargetStaggerTicks spreads "I just lost my target → pick a new one"
	// work across consecutive ticks when many units drop their target the
	// same tick (wave clear, mass kill). With N=10 units all losing targets
	// simultaneously, the unstaggered path runs 10 sub-cell A*s in one tick
	// (~100ms freeze). Staggering by unit.ID modulo this constant spreads
	// the work across 5 ticks for an average ~50ms delay before any given
	// unit re-engages — invisible to the player but huge for tick budget.
	retargetStaggerTicks = 5
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
	profileSection("combatAI.indexBuild", func() {
		for _, unit := range s.Units {
			if unit == nil || !unit.Visible || unit.HP <= 0 {
				continue
			}
			s.initializeCombatUnitLocked(unit)
			index.add(unit)
		}
	})

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
	profileSection("combatAI.anchorSlide", func() {
		for _, unit := range s.Units {
			if unit == nil || !unit.Visible || unit.HP <= 0 || unit.AttackTargetID != 0 ||
				(unit.OwnerID == enemyPlayerID && unit.ObjectiveID != "") {
				continue
			}
			if unit.GuardMode {
				// Guard anchor is pinned at the authored position — do not slide.
			} else if unit.OwnerID == enemyPlayerID ||
				unit.Order.Type == OrderAttackMove ||
				unit.Order.Type == OrderPatrol {
				unit.CombatAnchorX = unit.X
				unit.CombatAnchorY = unit.Y
			}
		}
	})

	profileSection("combatAI.decayThreat", func() {
		for _, unit := range s.Units {
			if !s.unitUsesCombatAI(unit) {
				continue
			}
			s.decayThreatLocked(unit, dt, index)
		}
	})

	profileSection("combatAI.evaluate", func() {
		for _, unit := range s.Units {
			if !s.unitUsesCombatAI(unit) {
				continue
			}
			if unit.Order.Type == OrderMove && unit.AttackTargetID == 0 && unit.AttackBuildingTargetID == "" {
				continue
			}
			// Committed swing: once the windup begins the target is locked
			// in until damage applies (see tickUnitCombatLocked + apply-
			// DelayedAttackLocked). Skipping the AI re-evaluation here
			// prevents shouldDropCurrentTargetLocked from clearing the
			// target mid-windup — without this the AI clears AttackTargetID
			// while the unit is animating its swing and the damage call
			// then whiffs because the target is gone, producing the
			// "swing-but-no-damage" symptom slow attackers (raider_brute,
			// big melee) feel most strongly because their 1s windup gives
			// the AI ~20 ticks to second-guess the commitment.
			if unit.AttackWindupRemaining > 0 {
				continue
			}
			// Non-combat units (workers) never auto-acquire. They only engage when
			// the player explicitly issues OrderAttackTarget via AttackWithUnits —
			// once that order is set, combat evaluation runs normally (the sticky-
			// attack short-circuit inside evaluateCombatLocked handles the rest).
			// When the target is cleared, clearCombatTargetLocked demotes the
			// order back to OrderIdle and this gate skips them again on the next tick.
			if unit.NonCombat && unit.Order.Type != OrderAttackTarget {
				continue
			}
			s.evaluateCombatLocked(unit, ctx)
		}
	})

	profileSection("combatAI.guardReturn", func() { s.tickGuardReturnLocked(blocked) })
}

func (s *GameState) unitUsesCombatAI(unit *Unit) bool {
	return unit != nil && unit.Visible && unit.HP > 0 && unit.Damage > 0 && containsString(unit.Capabilities, "attack")
}

func (s *GameState) evaluateCombatLocked(unit *Unit, ctx combatEvalContext) {
	profile := resolveCombatProfile(unit)
	if s.shouldDropCurrentTargetLocked(unit, profile, ctx) {
		s.clearCombatTargetLocked(unit)
		// Stagger re-acquisition across consecutive ticks when many units lose
		// targets in the same tick (wave clear, AoE kill, mass aggro). Without
		// this, N units all run selectBestTargetLocked + applyCombatTargetLocked
		// (each with its own sub-cell A*) in one tick, producing a 100+ms
		// freeze. The ID-modulo spread is deterministic (seeded replays stay
		// reproducible) and bounded — at most retargetStaggerTicks-1 ticks
		// (~0.2s) before the highest-ID unit re-evaluates. Per-tick load drops
		// from N units retargeting to N/retargetStaggerTicks. Applies to both
		// player and enemy units since both run evaluateCombatLocked.
		unit.NextCombatEvalTick = s.Tick + (unit.ID % retargetStaggerTicks)
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

	hasTarget := unit.AttackTargetID != 0 || unit.AttackBuildingTargetID != ""
	isTaunted := unit.TauntedByUnitID != 0 && unit.TauntRemaining > 0

	// Stickiness: while a unit holds a valid target, do not switch to a
	// "preferred" alternative. Score-based mid-fight retargeting reads as
	// indecision to the player ("why did my soldier stop hitting that orc to
	// chase a different orc?") and produces drop-then-pick-same-target loops
	// when the predicate that triggered the drop also makes the same target
	// rank highest. The target is only released by shouldDropCurrentTargetLocked
	// on validity grounds (death, leash, unreachable, building destroyed).
	// Taunts are the one mid-fight override.
	if hasTarget && !isTaunted {
		return
	}

	// A unit with no target must be able to acquire one, but a unit whose last
	// acquisition just failed (no reachable target) is throttled by
	// NextCombatEvalTick so it doesn't cycle through unreachable candidates
	// every tick. Without this throttle, a unit whose only-in-detection-range
	// buildings are all unreachable runs selectBestTargetLocked +
	// applyCombatTargetLocked + escalation every tick, dominating tick budget.
	if !hasTarget && s.Tick < unit.NextCombatEvalTick {
		return
	}
	shouldEvaluate := !hasTarget
	if !shouldEvaluate {
		// Reaches here only when hasTarget && isTaunted.
		if profile.RetargetIntervalTicks <= 0 {
			shouldEvaluate = true
		} else {
			shouldEvaluate = s.Tick-unit.LastTargetEvalTick >= profile.RetargetIntervalTicks
		}
	}
	if !shouldEvaluate {
		return
	}
	unit.LastTargetEvalTick = s.Tick

	best := s.selectBestTargetLocked(unit, profile, ctx)
	if best.Kind == combatTargetNone {
		if unit.OwnerID == enemyPlayerID && unit.AttackBuildingTargetID == "" && unit.AttackTargetID == 0 && unit.ObjectiveID == "" {
			// Guards and Hold-order enemies have an authored post — they must
			// not go objective-hunting. Without this gate every placed-enemy
			// guard with no in-range target re-runs map-wide A* to the player
			// townhall every tick, pulls itself off its anchor, then
			// tickGuardReturnLocked tries to walk it back — burning the entire
			// simulation budget on placed-enemy maps (e.g. exploration).
			if unit.GuardMode || unit.Order.Type == OrderHold {
				return
			}
			// Skip if the unit is already advancing on a path — enemyAdvanceToObjectiveLocked
			// would re-pick the same townhall and rerun A* from a position one step further
			// along the same route, doing real work for zero behavioural difference.
			// Also honour the per-unit cooldown so a fruitless previous search (no live
			// player buildings, no townhall reachable) does not retry every single tick.
			if unit.Moving {
				return
			}
			if s.Tick < unit.NextObjectiveSearchTick {
				return
			}
			// Global gate: cap A* objective searches to one per 5 ticks regardless
			// of army size. Per-unit cooldown acts as the secondary guard.
			if s.Tick < s.nextGlobalObjectiveSearchTick {
				return
			}
			s.nextGlobalObjectiveSearchTick = s.Tick + 5
			s.enemyAdvanceToObjectiveLocked(unit, ctx.blocked)
			// Back off after a search so units that complete a townhall path don't
			// immediately re-enter the search next tick. Per-unit cooldown must be
			// inside this success path — otherwise globally-gated units advance it
			// to Tick+20 while skipped, making the 5-tick global cadence degrade.
			unit.NextObjectiveSearchTick = s.Tick + enemyObjectiveSearchCooldownTicks
			return
		}
		// Gate D: resume standing order (AttackMove / Patrol) when no target.
		s.resumeStandingOrderLocked(unit, ctx.blocked)
		return
	}

	// Skip re-apply when the chosen target is the one we already hold (e.g.
	// taunt re-evaluation that picks the current target again) — avoids a
	// wasted assignUnitPath every cooldown cycle.
	switch best.Kind {
	case combatTargetUnit:
		if best.Unit.ID == unit.AttackTargetID {
			return
		}
	case combatTargetBuilding:
		if best.Building.ID == unit.AttackBuildingTargetID {
			return
		}
	}

	s.applyCombatTargetLocked(unit, best, ctx.blocked)
	unit.CurrentTargetScore = best.Score
	// Acquired a real target — reset the no-objective backoff so the next loss
	// re-evaluates immediately.
	unit.NextObjectiveSearchTick = 0
	// If acquisition failed (no AttackTargetID, no AttackBuildingTargetID, not
	// Moving), throttle re-evaluation so we don't cycle through unreachable
	// candidates next tick. AI-acquired unit-target A* failures now call
	// dropUnreachableAITargetLocked (clear + memo, not drift), so they land
	// here with no target and !Moving. Player-issued (OrderAttackTarget) unit
	// failures still drift, so those units are Moving and skip this branch.
	// Building-target nil-pos failures from applyCombatTargetLocked above also
	// reach this branch.
	if !unit.Moving && unit.AttackTargetID == 0 && unit.AttackBuildingTargetID == "" {
		interval := profile.RetargetIntervalTicks
		if interval <= 0 {
			interval = enemyObjectiveSearchCooldownTicks
		}
		unit.NextCombatEvalTick = s.Tick + interval
	} else {
		unit.NextCombatEvalTick = 0
	}
}

func (s *GameState) applyCombatTargetLocked(unit *Unit, target combatTarget, blocked map[gridPoint]bool) {
	// New target — clear the approach-repath cooldown so the first attempt
	// runs immediately instead of waiting out a previous backoff.
	unit.NextApproachRepathTick = 0

	// Gate C: Hold units fire from current position — never path toward a target.
	// Guards are an explicit exception: GuardMode enemies are spawned with
	// OrderHold so they suppress retreat / retaliation / objective hunting, but
	// the design contract is that they actively chase intruders within
	// GuardLeashRange of their anchor. shouldDropCurrentTargetLocked enforces
	// the leash from GuardAnchorX/Y, and tickGuardReturnLocked walks them home
	// once a target is dropped.
	holdUnit := unit.Order.Type == OrderHold && !unit.GuardMode

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
		unit.Attacking = false
		unit.Status = "Moving To Attack"
		if !holdUnit && s.distanceToBuilding(unit.X, unit.Y, target.Building) > unit.AttackRange {
			if pos := s.findBestBuildingAttackPositionLocked(unit, target.Building, blocked); pos != nil {
				unit.AttackBuildingTargetID = target.Building.ID
				unit.UnreachableBuildingTargetID = ""
				unit.UnreachableBuildingStrikeCount = 0
				s.assignUnitPath(unit, *pos, blocked, nil)
			} else {
				unit.AttackBuildingTargetID = ""
				s.applyBuildingUnreachableEscalationLocked(unit, target.Building.ID, blocked)
			}
		} else {
			unit.AttackBuildingTargetID = target.Building.ID
		}
	}
}

// applyBuildingUnreachableEscalationLocked handles the tiered backoff when A*
// fails to reach a building target. Strike 1 = 40-tick cooldown, strike 2 = 120
// ticks, strike 3+ = clear target and fall back to objective search.
func (s *GameState) applyBuildingUnreachableEscalationLocked(unit *Unit, buildingID string, blocked map[gridPoint]bool) {
	if unit.UnreachableBuildingTargetID == buildingID {
		unit.UnreachableBuildingStrikeCount++
	} else {
		unit.UnreachableBuildingStrikeCount = 1
	}
	unit.UnreachableBuildingTargetID = buildingID

	switch {
	case unit.UnreachableBuildingStrikeCount >= 3:
		s.clearCombatTargetLocked(unit)
		unit.UnreachableBuildingStrikeCount = 0
		if !unit.GuardMode && unit.Order.Type != OrderHold {
			// The building is sealed off by units, not terrain. Rather than
			// loop forever on a route that cannot exist (the freeze-at-spawn
			// deadlock), delegate to enemyAdvanceToObjectiveLocked which
			// re-resolves the objective and plain-moves toward it. Only when
			// the objective is fully partitioned (no path at all) does it fall
			// back to engaging the nearest blocking hostile — killing through
			// the wall reopens the route and drop-on-death resumes the advance.
			s.enemyAdvanceToObjectiveLocked(unit, blocked)
		}
	case unit.UnreachableBuildingStrikeCount == 2:
		unit.UnreachableUntilTick = s.Tick + 120
	default:
		unit.UnreachableUntilTick = s.Tick + unreachableTargetCooldownTicks
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
	defer profileStart("cmd.SetUnitStance")()

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
	defer profileStart("cmd.PatrolUnits")()

	blocked := s.getBlockedCellsLocked()
	orderID := s.nextMovementOrderIDLocked()

	// Two-pass shared-OrderID assignment so peers see each other as
	// same-group during pathfinding. See MoveUnits for the rationale.
	groupUnits := make([]*Unit, 0, len(unitIDs))
	for _, unitID := range unitIDs {
		unit := s.getUnitByIDLocked(unitID)
		if unit == nil || unit.OwnerID != playerID || !unitHasCapability(unit.UnitType, "attack") {
			continue
		}
		groupUnits = append(groupUnits, unit)
	}
	for _, unit := range groupUnits {
		s.resetUnitMovementLocked(unit, orderID)
	}

	groundSubBlocked, flyerSubBlocked := s.buildGroupSubBlockedLocked(groupUnits, blocked)

	// Leader-follower group pathing. Patrol destinations collapse onto the
	// single click point (no per-unit formation slot here), so all unit
	// targets are the same.
	dests := make([]protocol.Vec2, len(groupUnits))
	for i, unit := range groupUnits {
		unit.Order = OrderState{
			Type:          OrderPatrol,
			DestX:         dest.X,
			DestY:         dest.Y,
			PatrolReturnX: unit.X,
			PatrolReturnY: unit.Y,
		}
		unit.CombatAnchorX = dest.X
		unit.CombatAnchorY = dest.Y
		dests[i] = dest
	}
	s.assignGroupPathsLocked(groupUnits, dests, blocked, groundSubBlocked, flyerSubBlocked)
	for _, unit := range groupUnits {
		if unit.Moving {
			unit.Status = "Patrolling"
		} else {
			unit.Status = "Patrol Blocked"
		}
	}
}

// tickGuardReturnLocked handles return-to-anchor movement for guard units that
// currently have no attack target. Units with an active target are managed by
// the normal combat system; this function only acts once the target is gone.
// Must be called under s.mu write lock.
func (s *GameState) tickGuardReturnLocked(blocked map[gridPoint]bool) {
	// Two sub-cells: covers the diagonal goal-snap distance (~22.6px) created
	// by the static-obstacle inflation in buildUnitPathBlockedLocked, so guards
	// painted next to buildings/trees don't loop forever trying to reach an
	// anchor the pathfinder can't quite land on.
	const guardArrivalEpsilon = unitPathSubCellSize * 2

	for _, unit := range s.Units {
		if !unit.GuardMode || unit.HP <= 0 || !unit.Visible {
			continue
		}
		if unit.AttackTargetID != 0 || unit.AttackBuildingTargetID != "" {
			// Combat system owns movement while a target is held.
			continue
		}
		// Grace window after a target drop — let the retarget cooldown try to
		// pick a replacement before yanking the guard back to its anchor.
		if s.Tick < unit.NextGuardReturnTick {
			continue
		}
		dx := unit.GuardAnchorX - unit.X
		dy := unit.GuardAnchorY - unit.Y
		distSq := dx*dx + dy*dy
		if distSq <= guardArrivalEpsilon*guardArrivalEpsilon {
			// At anchor: clear any stale movement, mark as Guarding.
			if unit.Moving {
				unit.Path = nil
				unit.Moving = false
			}
			unit.Status = "Guarding"
			continue
		}
		// Not at anchor: path home if not already moving there.
		if !unit.Moving {
			dest := protocol.Vec2{X: unit.GuardAnchorX, Y: unit.GuardAnchorY}
			s.assignUnitPath(unit, dest, blocked, nil)
			unit.Status = "Returning"
		}
	}
}

func (s *GameState) clearCombatTargetLocked(unit *Unit) {
	unit.AttackTargetID = 0
	unit.AttackBuildingTargetID = ""
	unit.AttackDrifting = false
	// Abort any in-flight swing — the target is gone and a leftover windup
	// would keep the unit stuck in Status="Attacking" via the windup-at-top
	// block in tickUnitCombatLocked. The AI gate prevents this path from
	// firing mid-windup today, but defending here keeps the invariant robust
	// against future callers added outside the AI tick.
	unit.AttackWindupRemaining = 0
	unit.Attacking = false
	unit.ActionFacingDX = 0
	unit.ActionFacingDY = 0
	// Demote sticky-attack order to Idle when the target is cleared.
	// AttackMove and Patrol keep their order so they can resume movement.
	if unit.Order.Type == OrderAttackTarget {
		unit.Order = OrderState{Type: OrderIdle}
	}
	unit.CurrentTargetScore = 0
	// Honor RetargetIntervalTicks after dropping a target so re-acquisition
	// can't fire on the very next tick — otherwise two unreachable enemies in
	// range cause per-tick A* oscillation as the single-slot memo flips.
	unit.LastTargetEvalTick = s.Tick
	// Grace window for guards: don't snap home before the retarget cooldown
	// has a chance to pick a replacement. Scale to the profile's interval so
	// profiles with short RetargetIntervalTicks don't still flicker.
	unit.NextGuardReturnTick = s.Tick + resolveCombatProfile(unit).RetargetIntervalTicks + 5
	if !unit.Moving {
		unit.Status = "Idle"
	}
}
