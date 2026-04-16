package game

import (
	"math"

	"webrts/server/pkg/protocol"
)

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

type combatSpatialKey struct {
	X int
	Y int
}

type combatSpatialIndex struct {
	bucketSize float64
	cells      map[combatSpatialKey][]*Unit
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

var combatProfiles = map[string]CombatProfile{
	"soldier": {
		Name:                       "soldier",
		DetectionRange:             240,
		RetargetIntervalTicks:      6,
		SwitchThreshold:            12,
		ThreatDecayPerSecond:       14,
		ThreatFromDamage:           1.1,
		ThreatGenerationMultiplier: 1.45,
		PassiveMeleeThreat:         14,
		LeashDistance:              230,
		MaxChaseDistance:           220,
		Frontline:                  true,
		Melee:                      true,
		DangerTolerance:            1.2,
		Weights: TargetWeights{
			Distance:         24,
			InRange:          32,
			Threat:           18,
			TargetValue:      8,
			TypePreference:   14,
			Taunt:            1,
			ProtectAllies:    38,
			StructureDefense: 34,
			Reachability:     18,
			Stickiness:       14,
			DangerPenalty:    8,
			HealthFinish:     4,
		},
	},
	"archer": {
		Name:                       "archer",
		DetectionRange:             320,
		RetargetIntervalTicks:      5,
		SwitchThreshold:            14,
		ThreatDecayPerSecond:       16,
		ThreatFromDamage:           0.8,
		ThreatGenerationMultiplier: 0.85,
		PassiveMeleeThreat:         6,
		LeashDistance:              260,
		MaxChaseDistance:           180,
		PreferMaxRange:             true,
		Backline:                   true,
		DangerTolerance:            0.65,
		Weights: TargetWeights{
			Distance:         18,
			InRange:          36,
			Threat:           10,
			TargetValue:      18,
			TypePreference:   24,
			Taunt:            1,
			ProtectAllies:    12,
			StructureDefense: 10,
			Reachability:     14,
			Stickiness:       10,
			DangerPenalty:    36,
			HealthFinish:     16,
		},
	},
	"mage": {
		Name:                       "mage",
		DetectionRange:             310,
		RetargetIntervalTicks:      5,
		SwitchThreshold:            12,
		ThreatDecayPerSecond:       16,
		ThreatFromDamage:           0.95,
		ThreatGenerationMultiplier: 0.95,
		PassiveMeleeThreat:         6,
		LeashDistance:              240,
		MaxChaseDistance:           160,
		RetreatDistance:            130,
		RetreatTriggerMeleeRange:   100,
		PreferMaxRange:             true,
		Backline:                   true,
		DangerTolerance:            0.6,
		AoERadius:                  80,
		Weights: TargetWeights{
			Distance:         16,
			InRange:          30,
			Threat:           12,
			TargetValue:      22,
			TypePreference:   20,
			Taunt:            1,
			ProtectAllies:    10,
			StructureDefense: 10,
			Reachability:     12,
			Stickiness:       9,
			DangerPenalty:    34,
			AoECluster:       28,
			HealthFinish:     10,
		},
	},
	"cavalry": {
		Name:                       "cavalry",
		DetectionRange:             330,
		RetargetIntervalTicks:      4,
		SwitchThreshold:            10,
		ThreatDecayPerSecond:       18,
		ThreatFromDamage:           1.05,
		ThreatGenerationMultiplier: 1.1,
		PassiveMeleeThreat:         10,
		LeashDistance:              420,
		MaxChaseDistance:           420,
		RetreatDistance:            90,
		RetreatTriggerMeleeRange:   70,
		Melee:                      true,
		DangerTolerance:            1.0,
		Weights: TargetWeights{
			Distance:         18,
			InRange:          24,
			Threat:           8,
			TargetValue:      20,
			TypePreference:   32,
			Taunt:            1,
			ProtectAllies:    14,
			StructureDefense: 8,
			Reachability:     22,
			Stickiness:       8,
			DangerPenalty:    14,
			HealthFinish:     12,
		},
	},
	"catapult": {
		Name:                       "catapult",
		DetectionRange:             430,
		RetargetIntervalTicks:      8,
		SwitchThreshold:            20,
		ThreatDecayPerSecond:       14,
		ThreatFromDamage:           1.25,
		ThreatGenerationMultiplier: 1.0,
		PassiveMeleeThreat:         3,
		LeashDistance:              140,
		MaxChaseDistance:           60,
		RetreatDistance:            150,
		RetreatTriggerMeleeRange:   110,
		TargetBuildings:            true,
		PreferStructures:           true,
		PreferMaxRange:             true,
		Backline:                   true,
		DangerTolerance:            0.45,
		AoERadius:                  110,
		Weights: TargetWeights{
			Distance:         18,
			InRange:          42,
			Threat:           8,
			TargetValue:      24,
			TypePreference:   26,
			Taunt:            1,
			ProtectAllies:    6,
			StructureDefense: 8,
			Reachability:     8,
			Stickiness:       24,
			DangerPenalty:    44,
			AoECluster:       34,
			HealthFinish:     4,
		},
	},
	"raider": {
		Name:                       "raider",
		DetectionRange:             240,
		RetargetIntervalTicks:      6,
		SwitchThreshold:            10,
		ThreatDecayPerSecond:       15,
		ThreatFromDamage:           1.0,
		ThreatGenerationMultiplier: 1.0,
		PassiveMeleeThreat:         12,
		LeashDistance:              260,
		MaxChaseDistance:           250,
		TargetBuildings:            true,
		PreferStructures:           true,
		Frontline:                  true,
		Melee:                      true,
		DangerTolerance:            1.0,
		Weights: TargetWeights{
			Distance:         24,
			InRange:          28,
			Threat:           16,
			TargetValue:      8,
			TypePreference:   10,
			Taunt:            1,
			ProtectAllies:    12,
			StructureDefense: 24,
			Reachability:     16,
			Stickiness:       12,
			DangerPenalty:    8,
			HealthFinish:     6,
		},
	},
	"bruiser": {
		Name:                       "bruiser",
		DetectionRange:             250,
		RetargetIntervalTicks:      8,
		SwitchThreshold:            18,
		ThreatDecayPerSecond:       12,
		ThreatFromDamage:           1.1,
		ThreatGenerationMultiplier: 1.3,
		PassiveMeleeThreat:         16,
		LeashDistance:              260,
		MaxChaseDistance:           240,
		TargetBuildings:            true,
		Frontline:                  true,
		Melee:                      true,
		DangerTolerance:            1.2,
		Weights: TargetWeights{
			Distance:         20,
			InRange:          34,
			Threat:           22,
			TargetValue:      8,
			TypePreference:   12,
			Taunt:            1,
			ProtectAllies:    8,
			StructureDefense: 18,
			Reachability:     14,
			Stickiness:       24,
			DangerPenalty:    6,
			HealthFinish:     4,
		},
	},
	"skirmisher": {
		Name:                       "skirmisher",
		DetectionRange:             300,
		RetargetIntervalTicks:      4,
		SwitchThreshold:            8,
		ThreatDecayPerSecond:       18,
		ThreatFromDamage:           0.95,
		ThreatGenerationMultiplier: 1.0,
		PassiveMeleeThreat:         10,
		LeashDistance:              360,
		MaxChaseDistance:           360,
		RetreatDistance:            80,
		RetreatTriggerMeleeRange:   65,
		TargetBuildings:            true,
		Melee:                      true,
		DangerTolerance:            0.95,
		Weights: TargetWeights{
			Distance:         18,
			InRange:          22,
			Threat:           10,
			TargetValue:      20,
			TypePreference:   28,
			Taunt:            1,
			ProtectAllies:    10,
			StructureDefense: 12,
			Reachability:     24,
			Stickiness:       8,
			DangerPenalty:    16,
			HealthFinish:     10,
		},
	},
	"enemy_archer": {
		Name:                       "enemy_archer",
		DetectionRange:             320,
		RetargetIntervalTicks:      5,
		SwitchThreshold:            12,
		ThreatDecayPerSecond:       16,
		ThreatFromDamage:           0.9,
		ThreatGenerationMultiplier: 0.85,
		PassiveMeleeThreat:         4,
		LeashDistance:              140,
		MaxChaseDistance:           90,
		PreferMaxRange:             true,
		Backline:                   true,
		DangerTolerance:            0.55,
		Weights: TargetWeights{
			Distance:         18,
			InRange:          34,
			Threat:           10,
			TargetValue:      22,
			TypePreference:   28,
			Taunt:            1,
			ProtectAllies:    6,
			StructureDefense: 8,
			Reachability:     14,
			Stickiness:       10,
			DangerPenalty:    36,
			HealthFinish:     18,
		},
	},
	"enemy_siege": {
		Name:                       "enemy_siege",
		DetectionRange:             430,
		RetargetIntervalTicks:      8,
		SwitchThreshold:            20,
		ThreatDecayPerSecond:       14,
		ThreatFromDamage:           1.2,
		ThreatGenerationMultiplier: 1.0,
		PassiveMeleeThreat:         2,
		LeashDistance:              180,
		MaxChaseDistance:           80,
		RetreatDistance:            140,
		RetreatTriggerMeleeRange:   110,
		TargetBuildings:            true,
		PreferStructures:           true,
		PreferMaxRange:             true,
		Backline:                   true,
		DangerTolerance:            0.4,
		AoERadius:                  110,
		Weights: TargetWeights{
			Distance:         16,
			InRange:          40,
			Threat:           8,
			TargetValue:      26,
			TypePreference:   28,
			Taunt:            1,
			ProtectAllies:    4,
			StructureDefense: 12,
			Reachability:     8,
			Stickiness:       26,
			DangerPenalty:    44,
			AoECluster:       30,
			HealthFinish:     2,
		},
	},
	"support": {
		Name:                       "support",
		DetectionRange:             300,
		RetargetIntervalTicks:      5,
		SwitchThreshold:            10,
		ThreatDecayPerSecond:       16,
		ThreatFromDamage:           0.8,
		ThreatGenerationMultiplier: 0.95,
		PassiveMeleeThreat:         4,
		LeashDistance:              160,
		MaxChaseDistance:           110,
		RetreatDistance:            120,
		RetreatTriggerMeleeRange:   90,
		PreferMaxRange:             true,
		Backline:                   true,
		DangerTolerance:            0.55,
		AoERadius:                  70,
		Weights: TargetWeights{
			Distance:         16,
			InRange:          30,
			Threat:           12,
			TargetValue:      22,
			TypePreference:   26,
			Taunt:            1,
			ProtectAllies:    8,
			StructureDefense: 8,
			Reachability:     14,
			Stickiness:       10,
			DangerPenalty:    34,
			AoECluster:       18,
			HealthFinish:     10,
		},
	},
	"boss": {
		Name:                       "boss",
		DetectionRange:             380,
		RetargetIntervalTicks:      4,
		SwitchThreshold:            8,
		ThreatDecayPerSecond:       10,
		ThreatFromDamage:           1.3,
		ThreatGenerationMultiplier: 1.4,
		PassiveMeleeThreat:         16,
		LeashDistance:              480,
		MaxChaseDistance:           480,
		TargetBuildings:            true,
		PreferStructures:           true,
		Frontline:                  true,
		DangerTolerance:            1.4,
		AoERadius:                  120,
		Weights: TargetWeights{
			Distance:         18,
			InRange:          26,
			Threat:           20,
			TargetValue:      24,
			TypePreference:   22,
			Taunt:            1,
			ProtectAllies:    18,
			StructureDefense: 20,
			Reachability:     18,
			Stickiness:       16,
			DangerPenalty:    6,
			AoECluster:       20,
			HealthFinish:     12,
		},
	},
}

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

func (s *GameState) decayThreatLocked(unit *Unit, dt float64, index *combatSpatialIndex) {
	profile := resolveCombatProfile(unit)
	if unit.TauntRemaining > 0 {
		unit.TauntRemaining = math.Max(0, unit.TauntRemaining-dt)
		if unit.TauntRemaining == 0 {
			unit.TauntedByUnitID = 0
		}
	}

	for hostileID, entry := range unit.ThreatTable {
		hostile := s.getUnitByIDLocked(hostileID)
		if hostile == nil || !hostile.Visible || hostile.HP <= 0 || hostile.OwnerID == unit.OwnerID {
			delete(unit.ThreatTable, hostileID)
			continue
		}

		if distanceSquared(unit.X, unit.Y, hostile.X, hostile.Y) <= profile.DetectionRange*profile.DetectionRange {
			entry.LastSeenTick = s.Tick
		}

		decayRate := profile.ThreatDecayPerSecond
		if s.Tick-entry.LastSeenTick > combatThreatVisibilityForgetTicks {
			decayRate *= 2
		}
		entry.Value = math.Max(0, entry.Value-decayRate*dt)
		if entry.Value <= 0.01 {
			delete(unit.ThreatTable, hostileID)
		}
	}

	if !profile.Melee || profile.PassiveMeleeThreat <= 0 {
		return
	}
	for _, hostile := range index.query(unit.X, unit.Y, combatMeleeProximityRadius) {
		if hostile.OwnerID == unit.OwnerID || hostile.HP <= 0 {
			continue
		}
		s.addThreatLocked(unit, hostile, profile.PassiveMeleeThreat*dt, false)
	}
}

func (s *GameState) evaluateCombatLocked(unit *Unit, ctx combatEvalContext) {
	profile := resolveCombatProfile(unit)
	if s.shouldDropCurrentTargetLocked(unit, profile, ctx) {
		s.clearCombatTargetLocked(unit)
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

func (s *GameState) shouldDropCurrentTargetLocked(unit *Unit, profile CombatProfile, ctx combatEvalContext) bool {
	if unit.AttackTargetID != 0 {
		target := s.getUnitByIDLocked(unit.AttackTargetID)
		if target == nil || !target.Visible || target.HP <= 0 || target.OwnerID == unit.OwnerID {
			return true
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
		for _, hostile := range ctx.index.query(unit.X, unit.Y, profile.DetectionRange*0.75) {
			if hostile.OwnerID == unit.OwnerID || hostile.HP <= 0 {
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

	for _, hostile := range ctx.index.query(unit.X, unit.Y, profile.DetectionRange) {
		if hostile == unit || hostile.OwnerID == unit.OwnerID || hostile.HP <= 0 || !hostile.Visible {
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

	if profile.TargetBuildings {
		for i := range s.MapConfig.Buildings {
			building := &s.MapConfig.Buildings[i]
			if !s.isValidHostileBuildingTarget(unit, building) {
				continue
			}
			center := s.buildingCenterLocked(building)
			if distanceSquared(unit.X, unit.Y, center.X, center.Y) > profile.DetectionRange*profile.DetectionRange {
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
		if taunter != nil && taunter.Visible && taunter.HP > 0 && taunter.OwnerID != unit.OwnerID {
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
	distanceScore := 1 - clamp01(dist/profile.DetectionRange)
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
	distanceScore := 1 - clamp01(dist/profile.DetectionRange)
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
	unit.CurrentTargetScore = 0
	if !unit.Moving {
		unit.Status = "Idle"
	}
}

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

func (s *GameState) getThreatValueLocked(unit *Unit, hostileID int) float64 {
	if entry, ok := unit.ThreatTable[hostileID]; ok {
		return entry.Value
	}
	return 0
}

func (s *GameState) addThreatLocked(unit, hostile *Unit, amount float64, forceSeen bool) {
	if unit == nil || hostile == nil || unit.OwnerID == hostile.OwnerID || amount <= 0 {
		return
	}
	s.initializeCombatUnitLocked(unit)
	entry := unit.ThreatTable[hostile.ID]
	if entry == nil {
		entry = &ThreatEntry{}
		unit.ThreatTable[hostile.ID] = entry
	}
	entry.Value += amount
	if forceSeen || distanceSquared(unit.X, unit.Y, hostile.X, hostile.Y) <= resolveCombatProfile(unit).DetectionRange*resolveCombatProfile(unit).DetectionRange {
		entry.LastSeenTick = s.Tick
	}
	entry.LastActiveTick = s.Tick
}

func (s *GameState) onUnitDamagedLocked(attacker, target *Unit, damage int) {
	if attacker == nil || target == nil || damage <= 0 {
		return
	}
	amount := float64(damage) * resolveCombatProfile(target).ThreatFromDamage * resolveCombatProfile(attacker).ThreatGenerationMultiplier
	s.addThreatLocked(target, attacker, amount, true)

	for _, ally := range s.Units {
		if ally.OwnerID != target.OwnerID || ally.ID == target.ID || ally.HP <= 0 || !ally.Visible {
			continue
		}
		if distanceSquared(ally.X, ally.Y, target.X, target.Y) > combatBacklineDefenseRadius*combatBacklineDefenseRadius {
			continue
		}
		bonus := float64(damage) * 0.2
		if resolveCombatProfile(ally).Frontline {
			bonus *= 1.5
		}
		s.addThreatLocked(ally, attacker, bonus, true)
	}
}

func (s *GameState) onBuildingDamagedLocked(attacker *Unit, building *protocol.BuildingTile, damage int) {
	if attacker == nil || building == nil || damage <= 0 || building.OwnerID == nil {
		return
	}
	for _, ally := range s.Units {
		if ally.OwnerID != *building.OwnerID || ally.HP <= 0 || !ally.Visible {
			continue
		}
		if distanceSquared(ally.X, ally.Y, attacker.X, attacker.Y) > combatThreatStructureSplashRadius*combatThreatStructureSplashRadius {
			continue
		}
		bonus := float64(damage) * 0.35
		if resolveCombatProfile(ally).Frontline {
			bonus *= 1.35
		}
		s.addThreatLocked(ally, attacker, bonus, true)
	}
}

func (s *GameState) AddSupportThreatLocked(source *Unit, center protocol.Vec2, radius, baseThreat float64) {
	if source == nil || baseThreat <= 0 {
		return
	}
	for _, unit := range s.Units {
		if unit.OwnerID == source.OwnerID || unit.HP <= 0 || !unit.Visible {
			continue
		}
		if distanceSquared(unit.X, unit.Y, center.X, center.Y) > radius*radius {
			continue
		}
		s.addThreatLocked(unit, source, baseThreat*resolveCombatProfile(source).ThreatGenerationMultiplier, true)
	}
}

func (s *GameState) ApplyTauntLocked(targetUnitID, taunterUnitID int, duration float64) {
	target := s.getUnitByIDLocked(targetUnitID)
	taunter := s.getUnitByIDLocked(taunterUnitID)
	if target == nil || taunter == nil || target.OwnerID == taunter.OwnerID || duration <= 0 {
		return
	}
	target.TauntedByUnitID = taunterUnitID
	target.TauntRemaining = duration
	s.addThreatLocked(target, taunter, 60, true)
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
		if hostile.OwnerID == unit.OwnerID || hostile.HP <= 0 {
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

func (s *GameState) countNearbyHostilesLocked(target *Unit, radius float64, index *combatSpatialIndex) int {
	count := 0
	for _, hostile := range index.query(target.X, target.Y, radius) {
		if hostile.OwnerID == target.OwnerID || hostile.HP <= 0 {
			continue
		}
		count++
	}
	return count
}

func (s *GameState) countHostilesAroundPointLocked(ownerID string, x, y, radius float64, index *combatSpatialIndex) int {
	count := 0
	for _, hostile := range index.query(x, y, radius) {
		if hostile.OwnerID == ownerID || hostile.HP <= 0 {
			continue
		}
		count++
	}
	return count
}

func (s *GameState) isValidHostileBuildingTarget(unit *Unit, building *protocol.BuildingTile) bool {
	if building == nil || !building.Visible || building.OwnerID == nil || *building.OwnerID == unit.OwnerID {
		return false
	}
	hp, _, ok := getBuildingHP(building)
	return ok && hp > 0
}

func resolveCombatProfile(unit *Unit) CombatProfile {
	key := unit.Archetype
	if key == "" {
		key = inferCombatArchetype(unit)
	}
	if profile, ok := combatProfiles[key]; ok {
		return profile
	}
	return combatProfiles["soldier"]
}

func inferCombatArchetype(unit *Unit) string {
	if unit.OwnerID == enemyPlayerID {
		switch unit.UnitType {
		case "raider":
			return "raider"
		case "skirmisher":
			return "skirmisher"
		case "archer":
			return "enemy_archer"
		case "siege", "catapult":
			return "enemy_siege"
		case "support", "caster", "mage":
			return "support"
		case "boss":
			return "boss"
		case "bruiser":
			return "bruiser"
		default:
			return "raider"
		}
	}

	switch unit.UnitType {
	case "worker", "soldier":
		return "soldier"
	case "archer":
		return "archer"
	case "mage":
		return "mage"
	case "cavalry":
		return "cavalry"
	case "raider":
		return "raider"
	case "catapult", "siege":
		return "catapult"
	default:
		return "soldier"
	}
}

func newCombatSpatialIndex(bucketSize float64) *combatSpatialIndex {
	return &combatSpatialIndex{
		bucketSize: bucketSize,
		cells:      map[combatSpatialKey][]*Unit{},
	}
}

func (i *combatSpatialIndex) add(unit *Unit) {
	key := combatSpatialKey{
		X: int(math.Floor(unit.X / i.bucketSize)),
		Y: int(math.Floor(unit.Y / i.bucketSize)),
	}
	i.cells[key] = append(i.cells[key], unit)
}

func (i *combatSpatialIndex) query(x, y, radius float64) []*Unit {
	minX := int(math.Floor((x - radius) / i.bucketSize))
	maxX := int(math.Floor((x + radius) / i.bucketSize))
	minY := int(math.Floor((y - radius) / i.bucketSize))
	maxY := int(math.Floor((y + radius) / i.bucketSize))
	radiusSq := radius * radius
	results := make([]*Unit, 0, 8)
	for by := minY; by <= maxY; by++ {
		for bx := minX; bx <= maxX; bx++ {
			for _, unit := range i.cells[combatSpatialKey{X: bx, Y: by}] {
				if distanceSquared(x, y, unit.X, unit.Y) <= radiusSq {
					results = append(results, unit)
				}
			}
		}
	}
	return results
}

func clamp01(v float64) float64 {
	return clampFloat(v, 0, 1)
}
