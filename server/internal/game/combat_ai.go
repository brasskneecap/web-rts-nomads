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

	// Enemy units advancing on an objective (no active unit target) slide their
	// combat anchor to their current position each tick. This keeps the leash
	// centred on where they are now, so player units they encounter along the
	// way are within leash range and can be scored normally.
	for _, unit := range s.Units {
		if unit.OwnerID == enemyPlayerID && unit.Visible && unit.HP > 0 && unit.AttackTargetID == 0 {
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
		if unit.ManualMove && unit.AttackTargetID == 0 && unit.AttackBuildingTargetID == "" {
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
	if unit.ManualAttackTarget && unit.AttackTargetID != 0 {
		return
	}

	if s.shouldRetreatLocked(unit, profile, ctx) {
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
		}
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
	switch target.Kind {
	case combatTargetUnit:
		unit.AttackTargetID = target.Unit.ID
		unit.AttackBuildingTargetID = ""
		unit.Attacking = false
		unit.Status = "Moving To Attack"
		if distanceSquared(unit.X, unit.Y, target.Unit.X, target.Unit.Y) > unit.AttackRange*unit.AttackRange {
			s.refreshUnitAttackApproachLocked(unit, target.Unit, resolveCombatProfile(unit), blocked, true)
		}
	case combatTargetBuilding:
		unit.AttackTargetID = 0
		unit.AttackBuildingTargetID = target.Building.ID
		unit.Attacking = false
		unit.Status = "Moving To Attack"
		if s.distanceToBuilding(unit.X, unit.Y, target.Building) > unit.AttackRange {
			if pos := s.findBestBuildingAttackPositionLocked(unit, target.Building, blocked); pos != nil {
				s.assignUnitPath(unit, *pos, blocked, nil)
			}
		}
	}
}

func (s *GameState) clearCombatTargetLocked(unit *Unit) {
	unit.AttackTargetID = 0
	unit.AttackBuildingTargetID = ""
	unit.Attacking = false
	unit.ManualAttackTarget = false
	unit.CurrentTargetScore = 0
	if !unit.Moving {
		unit.Status = "Idle"
	}
}
