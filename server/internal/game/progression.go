package game

import (
	"math"
	"math/rand"
)

const (
	unitRankBase   = "base"
	unitRankBronze = "bronze"
	unitRankSilver = "silver"
	unitRankGold   = "gold"
)

// Soldier promotion paths. Assigned randomly at Bronze and fixed for the unit's lifetime.
const (
	unitPathNone      = "none"
	unitPathVanguard  = "vanguard"
	unitPathBerserker = "berserker"
)

const (
	// Tuning points for first-pass progression. These are intentionally simple
	// and deterministic so rank gain is easy to debug before perks/branching exist.
	xpGainMultiplier               = 10
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

// rankProgressionTable MUST be sorted by XPThreshold ascending — rankDefForXP relies on it.
var rankProgressionTable = []rankProgressionDef{
	{Rank: unitRankBase, XPThreshold: 0, MaxHPMultiplier: 1.00, DamageMultiplier: 1.00, AttackSpeedMultiplier: 1.00},
	{Rank: unitRankBronze, XPThreshold: 100, MaxHPMultiplier: 1.10, DamageMultiplier: 1.10, AttackSpeedMultiplier: 1.00},
	{Rank: unitRankSilver, XPThreshold: 350, MaxHPMultiplier: 1.20, DamageMultiplier: 1.25, AttackSpeedMultiplier: 1.10},
	{Rank: unitRankGold, XPThreshold: 750, MaxHPMultiplier: 1.35, DamageMultiplier: 1.50, AttackSpeedMultiplier: 1.25},
}

// pathModifierDef describes per-rank stat multipliers and flat armor bonus for a
// promotion path. Multipliers stack multiplicatively on top of rank multipliers:
//
//	effectiveStat = baseStat × rankMult × pathMult
//
// Armor is a defense rating producing percentage damage reduction via
// applyArmorMitigation (diminishing returns — see armorMitigationK).
// MoveSpeedMultiplier is path-only (no rank scaling) — mirrors Armor.
type pathModifierDef struct {
	Path                  string
	Rank                  string
	MaxHPMultiplier       float64
	DamageMultiplier      float64
	AttackSpeedMultiplier float64
	MoveSpeedMultiplier   float64
	Armor                 int
}

// Armor tuning. Damage reduction follows reduction = armor / (armor + armorMitigationK).
//
//	armor   reduction
//	   18       15.3%   (berserker baseline)
//	   33       24.8%   (soldier pre-promotion)
//	   54       35.1%   (vanguard baseline)
//	  100       50.0%
//	  200       66.7%
//	  300       75.0%
//
// soldierBaseArmor is applied to soldiers that have not yet been assigned a
// promotion path (rank below Bronze). Once pathed, pathModifierDef.Armor takes
// over and this value is no longer used.
const (
	armorMitigationK = 100
	soldierBaseArmor = 33
)

// identityPathModifier is returned for units with no path or unknown path/rank combos.
var identityPathModifier = pathModifierDef{
	MaxHPMultiplier: 1.0, DamageMultiplier: 1.0, AttackSpeedMultiplier: 1.0, MoveSpeedMultiplier: 1.0, Armor: 0,
}

// pathModifierTable defines how each path modifies stats at each rank.
// All multipliers are applied ON TOP of the existing rank multipliers.
//
// Vanguard — sturdier frontliner: more HP and armor, slight attack speed cost early.
// Berserker — aggressive damage dealer: more damage, attack speed, and move speed; less HP.
//
// Armor values produce ~35% reduction for Vanguard and ~15% for Berserker
// (vs. the soldier pre-promotion baseline of ~25%). Further armor scaling
// across ranks is intentionally left to perks so flat-reduction perks (future
// vanguard tier) can stack predictably on top of the diminishing-returns curve.
var pathModifierTable = []pathModifierDef{
	// vanguard
	{Path: unitPathVanguard, Rank: unitRankBronze, MaxHPMultiplier: 1.10, DamageMultiplier: 1.00, AttackSpeedMultiplier: 0.95, MoveSpeedMultiplier: 1.00, Armor: 54},
	{Path: unitPathVanguard, Rank: unitRankSilver, MaxHPMultiplier: 1.20, DamageMultiplier: 1.00, AttackSpeedMultiplier: 1.00, MoveSpeedMultiplier: 1.00, Armor: 54},
	{Path: unitPathVanguard, Rank: unitRankGold, MaxHPMultiplier: 1.30, DamageMultiplier: 1.10, AttackSpeedMultiplier: 1.00, MoveSpeedMultiplier: 1.00, Armor: 54},
	// berserker — flat +15% move speed at every rank.
	{Path: unitPathBerserker, Rank: unitRankBronze, MaxHPMultiplier: 0.90, DamageMultiplier: 1.10, AttackSpeedMultiplier: 1.10, MoveSpeedMultiplier: 1.15, Armor: 18},
	{Path: unitPathBerserker, Rank: unitRankSilver, MaxHPMultiplier: 0.95, DamageMultiplier: 1.20, AttackSpeedMultiplier: 1.15, MoveSpeedMultiplier: 1.15, Armor: 18},
	{Path: unitPathBerserker, Rank: unitRankGold, MaxHPMultiplier: 1.00, DamageMultiplier: 1.30, AttackSpeedMultiplier: 1.25, MoveSpeedMultiplier: 1.15, Armor: 18},
}

// armorDamageReduction returns the fractional damage reduction produced by the
// given armor rating, using a diminishing-returns curve anchored on armorMitigationK.
func armorDamageReduction(armor int) float64 {
	if armor <= 0 {
		return 0
	}
	a := float64(armor)
	return a / (a + float64(armorMitigationK))
}

// applyArmorMitigation reduces `damage` by the target's armor rating and returns
// the post-mitigation integer damage (never negative).
func applyArmorMitigation(damage, armor int) int {
	if damage <= 0 {
		return 0
	}
	return maxInt(0, int(math.Round(float64(damage)*(1.0-armorDamageReduction(armor)))))
}

// pathModifierFor returns the path modifier for the given path and rank.
// Returns identityPathModifier for base rank or unrecognised combinations so
// that units without a path are unaffected.
func pathModifierFor(path, rank string) pathModifierDef {
	if path == unitPathNone || rank == unitRankBase {
		return identityPathModifier
	}
	for _, def := range pathModifierTable {
		if def.Path == path && def.Rank == rank {
			return def
		}
	}
	return identityPathModifier
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
	// Advance one rank at a time so every crossed tier gets its own path /
	// perk / modifier application. Matters when a single XP gain jumps the
	// unit past multiple thresholds (e.g. debug-boosted XP rates).
	finalRank := rankDefForXP(unit.XP)
	for unit.Rank != finalRank.Rank {
		next, ok := nextRankDef(unit.Rank)
		if !ok {
			break
		}
		unit.Rank = next.Rank
		// Assign path before applying modifiers so the first applyRankModifiersLocked
		// call already uses the correct path multipliers.
		s.assignUnitPathOnRankUpLocked(unit)
		// Assign perk after path so eligibility filtering can match against the
		// correct ProgressionPath. Must run before applyRankModifiersLocked in case
		// a future perk modifies base stats at assignment time.
		//
		// Perk definitions: catalog/perk-defs.json
		// Perk runtime/handlers + assignment rules: perks.go
		s.assignUnitPerkLocked(unit)
		s.applyRankModifiersLocked(unit, true)
		s.onUnitRankUpLocked(unit)
	}
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

// assignUnitPathOnRankUpLocked randomly assigns a promotion path to a Soldier the
// first time it reaches Bronze rank. Path is fixed for the unit's lifetime.
func (s *GameState) assignUnitPathOnRankUpLocked(unit *Unit) {
	if unit.ProgressionPath != unitPathNone {
		return // already assigned
	}
	if unit.UnitType != "soldier" {
		return // only soldiers get paths for now
	}
	if unit.Rank == unitRankBase {
		return // shouldn't happen here, but guard anyway
	}
	paths := [2]string{unitPathVanguard, unitPathBerserker}
	unit.ProgressionPath = paths[rand.Intn(2)]
}

func (s *GameState) applyRankModifiersLocked(unit *Unit, preserveHealthPercent bool) {
	if unit == nil {
		return
	}
	rankDef := rankDefByName(unit.Rank)
	pathDef := pathModifierFor(unit.ProgressionPath, unit.Rank)

	currentHPFraction := 1.0
	if preserveHealthPercent && unit.MaxHP > 0 {
		currentHPFraction = clampFloat(float64(unit.HP)/float64(unit.MaxHP), 0, 1)
	}

	unit.MaxHP = maxInt(1, int(math.Round(float64(unit.BaseMaxHP)*rankDef.MaxHPMultiplier*pathDef.MaxHPMultiplier)))
	// Apply flat max HP bonus from hold_the_line (and any future flat-HP perks).
	// Called after rank/path multipliers so the bonus is always the authored value.
	// Tuning point: bonusMaxHP in perk-defs.json → hold_the_line.config.
	if bonus := s.perkFlatMaxHPBonusLocked(unit); bonus > 0 {
		unit.MaxHP += bonus
	}
	unit.Damage = maxInt(0, int(math.Round(float64(unit.BaseDamage)*rankDef.DamageMultiplier*pathDef.DamageMultiplier)))
	unit.AttackSpeed = math.Max(0.1, unit.BaseAttackSpeed*rankDef.AttackSpeedMultiplier*pathDef.AttackSpeedMultiplier)
	// Move speed: path-only scaling for now (rank doesn't affect move speed yet).
	unit.MoveSpeed = math.Max(1.0, unit.BaseMoveSpeed*pathDef.MoveSpeedMultiplier)
	unit.Armor = pathDef.Armor
	if unit.UnitType == "soldier" && unit.ProgressionPath == unitPathNone {
		unit.Armor = soldierBaseArmor
	}

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

// recordDamageDealtLocked banks damage an attacker has dealt to a unit. The XP
// is not awarded until the target dies (via payoutDamageDealtXPLocked), so
// damage contributed to enemies that never die earns no XP. If the attacker
// dies first, removeUnitLocked strips their entry — forfeiting the banked XP.
func (s *GameState) recordDamageDealtLocked(attacker, target *Unit, damage int) {
	if attacker == nil || target == nil || damage <= 0 {
		return
	}
	if !s.unitCanGainXPLocked(attacker) {
		return
	}
	if target.DamageDealtByUnit == nil {
		target.DamageDealtByUnit = map[int]int{}
	}
	target.DamageDealtByUnit[attacker.ID] += damage
}

// payoutDamageDealtXPLocked pays banked damage XP to each surviving attacker
// when the target dies. Called alongside awardKillXPLocked.
func (s *GameState) payoutDamageDealtXPLocked(target *Unit) {
	if target == nil || len(target.DamageDealtByUnit) == 0 {
		return
	}
	for attackerID, damage := range target.DamageDealtByUnit {
		attacker := s.getUnitByIDLocked(attackerID)
		if attacker == nil || damage <= 0 {
			continue
		}
		s.addUnitXPFloatLocked(attacker, float64(damage)*xpPerDamageDealt)
	}
	target.DamageDealtByUnit = map[int]int{}
}

// recordDamageDealtBuildingLocked mirrors recordDamageDealtLocked for buildings.
// Banked XP is paid out on destruction.
func (s *GameState) recordDamageDealtBuildingLocked(attacker *Unit, buildingID string, damage int) {
	if attacker == nil || buildingID == "" || damage <= 0 {
		return
	}
	if !s.unitCanGainXPLocked(attacker) {
		return
	}
	if s.buildingDamageDealt == nil {
		s.buildingDamageDealt = map[string]map[int]int{}
	}
	m, ok := s.buildingDamageDealt[buildingID]
	if !ok {
		m = map[int]int{}
		s.buildingDamageDealt[buildingID] = m
	}
	m[attacker.ID] += damage
}

// payoutBuildingDamageDealtXPLocked pays banked damage XP to each surviving
// contributor when a building is destroyed.
func (s *GameState) payoutBuildingDamageDealtXPLocked(buildingID string) {
	m, ok := s.buildingDamageDealt[buildingID]
	if !ok {
		return
	}
	for attackerID, damage := range m {
		attacker := s.getUnitByIDLocked(attackerID)
		if attacker == nil || damage <= 0 {
			continue
		}
		s.addUnitXPFloatLocked(attacker, float64(damage)*xpPerDamageDealt)
	}
	delete(s.buildingDamageDealt, buildingID)
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
