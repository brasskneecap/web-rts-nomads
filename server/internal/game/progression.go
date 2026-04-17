package game

import "math"

const (
	unitRankBase   = "base"
	unitRankBronze = "bronze"
	unitRankSilver = "silver"
	unitRankGold   = "gold"
)

const (
	// Tuning points for first-pass progression. These are intentionally simple
	// and deterministic so rank gain is easy to debug before perks/branching exist.
	xpGainMultiplier               = 0.20
	xpPerDamageDealt               = 1.0
	xpPerKillBonus                 = 25.0
	xpPerSoldierDamageTankedOnKill = 0.5
	rankUpFxDurationSecs           = 1.4

	// Wave completion XP intentionally omitted — future reward mechanics
	// (e.g. loot, bounties) will cover this instead.
)

type rankProgressionDef struct {
	Rank                  string
	XPThreshold           int
	MaxHPMultiplier       float64
	DamageMultiplier      float64
	AttackSpeedMultiplier float64
}

var rankProgressionTable = []rankProgressionDef{
	{Rank: unitRankBase, XPThreshold: 0, MaxHPMultiplier: 1.00, DamageMultiplier: 1.00, AttackSpeedMultiplier: 1.00},
	{Rank: unitRankBronze, XPThreshold: 100, MaxHPMultiplier: 1.10, DamageMultiplier: 1.10, AttackSpeedMultiplier: 1.00},
	{Rank: unitRankSilver, XPThreshold: 250, MaxHPMultiplier: 1.20, DamageMultiplier: 1.25, AttackSpeedMultiplier: 1.10},
	{Rank: unitRankGold, XPThreshold: 500, MaxHPMultiplier: 1.35, DamageMultiplier: 1.50, AttackSpeedMultiplier: 1.25},
}

func rankDefForXP(xp int) rankProgressionDef {
	def := rankProgressionTable[0]
	for _, candidate := range rankProgressionTable {
		if xp < candidate.XPThreshold {
			break
		}
		def = candidate
	}
	return def
}

func rankDefByName(rank string) rankProgressionDef {
	for _, candidate := range rankProgressionTable {
		if candidate.Rank == rank {
			return candidate
		}
	}
	return rankProgressionTable[0]
}

func nextRankDef(rank string) (rankProgressionDef, bool) {
	for i, candidate := range rankProgressionTable {
		if candidate.Rank == rank {
			if i+1 < len(rankProgressionTable) {
				return rankProgressionTable[i+1], true
			}
			break
		}
	}
	return rankProgressionDef{}, false
}

func (s *GameState) unitCanGainXPLocked(unit *Unit) bool {
	return unit != nil && unit.OwnerID != enemyPlayerID && unit.HP > 0 && unit.Visible
}

func (s *GameState) addUnitXPLocked(unit *Unit, amount int) {
	if !s.unitCanGainXPLocked(unit) || amount <= 0 {
		return
	}
	unit.XP += amount
	finalRank := rankDefForXP(unit.XP)
	if unit.Rank == finalRank.Rank {
		return
	}
	unit.Rank = finalRank.Rank
	s.applyRankModifiersLocked(unit, true)
	s.onUnitRankUpLocked(unit)
}

func (s *GameState) addUnitXPFloatLocked(unit *Unit, amount float64) {
	if !s.unitCanGainXPLocked(unit) || amount <= 0 {
		return
	}

	total := unit.XPProgressRemainder + amount*xpGainMultiplier
	wholeXP := int(math.Floor(total))
	unit.XPProgressRemainder = total - float64(wholeXP)

	if wholeXP <= 0 {
		return
	}

	s.addUnitXPLocked(unit, wholeXP)
}

func (s *GameState) applyRankModifiersLocked(unit *Unit, preserveHealthPercent bool) {
	if unit == nil {
		return
	}
	def := rankDefByName(unit.Rank)
	currentHPFraction := 1.0
	if preserveHealthPercent && unit.MaxHP > 0 {
		currentHPFraction = clampFloat(float64(unit.HP)/float64(unit.MaxHP), 0, 1)
	}

	unit.MaxHP = maxInt(1, int(math.Round(float64(unit.BaseMaxHP)*def.MaxHPMultiplier)))
	unit.Damage = maxInt(0, int(math.Round(float64(unit.BaseDamage)*def.DamageMultiplier)))
	unit.AttackSpeed = math.Max(0.1, unit.BaseAttackSpeed*def.AttackSpeedMultiplier)

	if preserveHealthPercent {
		unit.HP = maxInt(1, int(math.Round(float64(unit.MaxHP)*currentHPFraction)))
	} else if unit.HP > unit.MaxHP {
		unit.HP = unit.MaxHP
	}
}

func (s *GameState) onUnitRankUpLocked(unit *Unit) {
	if unit == nil {
		return
	}
	unit.RankUpFxRemaining = rankUpFxDurationSecs
}

func (s *GameState) awardDamageXPLocked(attacker *Unit, damage int) {
	if damage <= 0 {
		return
	}
	s.addUnitXPFloatLocked(attacker, float64(damage)*xpPerDamageDealt)
}

func (s *GameState) recordSoldierTankContributionLocked(attacker, target *Unit, damage int) {
	if attacker == nil || target == nil || damage <= 0 {
		return
	}
	if !s.unitCanGainXPLocked(target) {
		return
	}
	if resolveCombatProfile(target).Name != "soldier" {
		return
	}
	if target.TankedDamageByUnit == nil {
		target.TankedDamageByUnit = map[int]float64{}
	}
	target.TankedDamageByUnit[attacker.ID] += float64(damage)
}

func (s *GameState) awardKillXPLocked(attacker *Unit) {
	s.addUnitXPFloatLocked(attacker, xpPerKillBonus)
}

func (s *GameState) awardSoldierTankKillXPLocked(defeatedUnitID int) {
	if defeatedUnitID == 0 {
		return
	}
	for _, unit := range s.Units {
		if unit == nil || unit.TankedDamageByUnit == nil {
			continue
		}
		tankedDamage := unit.TankedDamageByUnit[defeatedUnitID]
		if tankedDamage > 0 {
			s.addUnitXPFloatLocked(unit, tankedDamage*xpPerSoldierDamageTankedOnKill)
			delete(unit.TankedDamageByUnit, defeatedUnitID)
		}
	}
}

func (s *GameState) unitXPToNextRankLocked(unit *Unit) int {
	if unit == nil {
		return 0
	}
	next, ok := nextRankDef(unit.Rank)
	if !ok {
		return 0
	}
	return maxInt(0, next.XPThreshold-unit.XP)
}

func (s *GameState) unitXPIntoCurrentRankLocked(unit *Unit) int {
	if unit == nil {
		return 0
	}
	current := rankDefByName(unit.Rank)
	return maxInt(0, unit.XP-current.XPThreshold)
}
