package game

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
		PreferClosestTarget:        true,
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
		TargetBuildings:            true,
		PreferClosestTarget:        true,
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
	"caster": {
		// Phase 1 caster profile: a faithful clone of "support" (backline +
		// retreat, which the Acolyte's old "archer" profile lacked) with
		// three intentional deltas, written inline:
		//   1. Name: "caster" — distinct, independently-tunable identity.
		//   2. MaxChaseDistance: 180 (the "archer" envelope) instead of
		//      support's 110. Leash self-clamps up to AttackRange via
		//      effectiveLeashDistance, but MaxChaseDistance has no such clamp,
		//      so inheriting support's 110 would silently shrink the
		//      Acolyte's (AttackRange 220) pursuit range.
		//   3. AoERadius / Weights.AoECluster zeroed — the Acolyte's
		//      current kit is single-target (basic attack = fire_bolt
		//      projectile; only ability = heal). A future AoE caster ability
		//      would warrant re-tuning this profile then.
		// Do not "fix" these toward support's values; they are the design.
		Name:                       "caster",
		DetectionRange:             300,
		RetargetIntervalTicks:      5,
		SwitchThreshold:            10,
		ThreatDecayPerSecond:       16,
		ThreatFromDamage:           0.8,
		ThreatGenerationMultiplier: 0.95,
		PassiveMeleeThreat:         4,
		LeashDistance:              160,
		MaxChaseDistance:           180,
		RetreatDistance:            120,
		RetreatTriggerMeleeRange:   90,
		PreferMaxRange:             true,
		Backline:                   true,
		DangerTolerance:            0.55,
		AoERadius:                  0,
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
			AoECluster:       0,
			HealthFinish:     10,
		},
	},
	"flyer_skirmisher": {
		// Airborne ranged harasser. Behaves like an enemy_archer but with a
		// longer leash so the unit can range freely over terrain its ground
		// peers can't cross, and slightly faster retargeting so it stays
		// responsive when picking new targets at altitude.
		Name:                       "flyer_skirmisher",
		DetectionRange:             280,
		RetargetIntervalTicks:      4,
		SwitchThreshold:            10,
		ThreatDecayPerSecond:       16,
		ThreatFromDamage:           0.9,
		ThreatGenerationMultiplier: 0.85,
		PassiveMeleeThreat:         2,
		LeashDistance:              360,
		MaxChaseDistance:           320,
		TargetBuildings:            true,
		PreferClosestTarget:        true,
		PreferMaxRange:             true,
		Backline:                   true,
		DangerTolerance:            0.7,
		Weights: TargetWeights{
			Distance:         18,
			InRange:          34,
			Threat:           10,
			TargetValue:      20,
			TypePreference:   26,
			Taunt:            1,
			ProtectAllies:    6,
			StructureDefense: 8,
			Reachability:     16,
			Stickiness:       10,
			DangerPenalty:    28,
			HealthFinish:     16,
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
		PreferClosestTarget:        true,
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

// effectiveDetectionRange returns the unit's actual target-acquisition range,
// expanded so a unit can always see targets at the edge of its own attack
// range. Without this expansion, a Marksman with eagle_spirit / bullseye
// (or any unit whose AttackRange has been pushed above the profile baseline)
// would happily fire to 1200px but never *acquire* anything past the profile
// DetectionRange (e.g. archer 320). Returns max(profile.DetectionRange,
// unit.AttackRange) — if base detection already exceeds attack range
// (melee with long sight), the base wins.
func effectiveDetectionRange(unit *Unit, profile CombatProfile) float64 {
	if unit == nil || unit.AttackRange <= profile.DetectionRange {
		return profile.DetectionRange
	}
	return unit.AttackRange
}

// effectiveLeashDistance mirrors effectiveDetectionRange for the leash gate
// in targetInsideLeashLocked. A unit's leash must be at least its own
// AttackRange or the leash check rejects targets the unit could otherwise
// shoot at — effectively re-imposing the old detection cap one tick later.
func effectiveLeashDistance(unit *Unit, profile CombatProfile) float64 {
	if unit == nil || unit.AttackRange <= profile.LeashDistance {
		return profile.LeashDistance
	}
	return unit.AttackRange
}

func resolveCombatProfile(unit *Unit) CombatProfile {
	// Data-driven override: UnitDef.CombatProfile picks the profile directly.
	// Validated at catalog load (unit_defs.go init), so the lookup is safe.
	if def, ok := getUnitDef(unit.UnitType); ok && def.CombatProfile != "" {
		if profile, ok := combatProfiles[def.CombatProfile]; ok {
			return profile
		}
	}
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
