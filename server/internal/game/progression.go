package game

import (
	"math"
)

const (
	unitRankBase   = "base"
	unitRankBronze = "bronze"
	unitRankSilver = "silver"
	unitRankGold   = "gold"
)

// Promotion path constants. Assigned at Bronze rank and fixed for the unit's lifetime.
// Soldiers randomly receive Vanguard or Berserker. Archers randomly receive
// Trapper or Marksman. Apprentices randomly receive Cleric or Arch Mage.
const (
	unitPathNone      = "none"
	unitPathVanguard  = "vanguard"
	unitPathBerserker = "berserker"
	unitPathTrapper   = "trapper"
	unitPathMarksman  = "marksman"
	unitPathCleric    = "cleric"
	unitPathArchMage  = "arch_mage"
)

const (
	// Tuning points for first-pass progression. These are intentionally simple
	// and deterministic so rank gain is easy to debug before perks/branching exist.
	xpGainMultiplier               = 0.2
	xpPerDamageDealt               = 1.0
	xpPerKillBonus                 = 25.0
	xpPerSoldierDamageTankedOnKill = 0.5
	rankUpFxDurationSecs           = 1.4

	// Wave completion XP intentionally omitted — future reward mechanics
	// (e.g. loot, bounties) will cover this instead.
)

// rankProgressionDef is the XP-threshold record for a rank. Stat multipliers
// are NOT here — they live per-path in pathModifierTable so Vanguard tuning
// can't accidentally contaminate Berserker (or vice-versa). This table
// answers only "when does a unit promote?" and "what's the next rank?".
type rankProgressionDef struct {
	Rank        string
	XPThreshold int
}

// rankProgressionTable MUST be sorted by XPThreshold ascending — rankDefForXP relies on it.
var rankProgressionTable = []rankProgressionDef{
	{Rank: unitRankBase, XPThreshold: 0},
	{Rank: unitRankBronze, XPThreshold: 100},
	{Rank: unitRankSilver, XPThreshold: 350},
	{Rank: unitRankGold, XPThreshold: 750},
}

// pathModifierDef describes per-rank stat multipliers and flat armor bonus for a
// promotion path. Multipliers stack multiplicatively on top of rank multipliers:
//
//	effectiveStat = baseStat × rankMult × pathMult
//
// Armor is a defense rating producing percentage damage reduction via
// applyArmorMitigation (diminishing returns — see armorMitigationK).
// MoveSpeedMultiplier is path-only (no rank scaling) — mirrors Armor.
//
// Attack-range tuning has two knobs:
//
//	AttackRange           — flat override (in world pixels). When > 0, replaces
//	                        unit.BaseAttackRange entirely. Use when you want
//	                        an absolute reach regardless of the unit's catalog
//	                        base (e.g. Marksman: "this path shoots 1520px").
//	AttackRangeMultiplier — multiplier on top of unit.BaseAttackRange (or the
//	                        flat override above when both are set). Defaults
//	                        to 1.0 (no change). Use when you want range to
//	                        scale with the catalog base across unit types.
//
// Perk-driven bonuses (eagle_spirit, bullseye, etc.) ALWAYS stack on top of
// whatever the path resolves to, via perkAttackRangeMultiplierLocked.
type pathModifierDef struct {
	Path                  string
	Rank                  string
	MaxHPMultiplier       float64
	DamageMultiplier      float64
	AttackSpeedMultiplier float64
	MoveSpeedMultiplier   float64
	AttackRange           float64
	AttackRangeMultiplier float64
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
	MaxHPMultiplier: 1.0, DamageMultiplier: 1.0, AttackSpeedMultiplier: 1.0, MoveSpeedMultiplier: 1.0, AttackRangeMultiplier: 1.0, Armor: 0,
}

// Per-path stat multipliers live in JSON — one file per path under
// catalog/paths/<path>.json. Loaded at init time into pathModifiersByKey (see
// path_defs.go). Rebalancing is a JSON edit followed by a server restart; no
// Go rebuild required.
//
// Paths shipped today:
//
//	vanguard  — sturdy frontliner: more HP, high armor (~35% reduction),
//	            small AS cost early.
//	berserker — aggressive: more damage / AS, +15% move speed, less HP,
//	            light armor (~15% reduction).
//	trapper   — utility through traps; stat tuning deferred, rows currently
//	            mirror the default rank curve so promotions still grant
//	            baseline HP/damage/AS bumps.
//	marksman  — precision-ranged archer; stat tuning deferred, rows currently
//	            mirror the default rank curve.
//	cleric    — support caster (Apprentice); stat tuning deferred, rows
//	            currently mirror the default rank curve. No perks yet.
//	arch_mage — offensive caster (Apprentice); stat tuning deferred, rows
//	            currently mirror the default rank curve. No perks yet.
//	none      — default rank curve for units that earn XP without ever being
//	            assigned a path (workers, future utility units).
//
// Armor values feed the diminishing-returns curve in armorDamageReduction
// (reduction = armor / (armor + 100)). Further armor scaling across ranks is
// intentionally left to perks so flat-reduction perks (e.g. Vanguard gold)
// can stack predictably on top.
//
// The values currently in the JSON files preserve the exact in-game behavior
// of the pre-JSON `rankProgression × pathModifier` stacking scheme. Those
// products aren't pretty (e.g. 1.755, 1.5625) — round them when the balance
// team does a formal tuning pass. Because rows are per-path, rounding one
// path cannot silently perturb another.

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

// pathModifierFor returns the stat multipliers for the given (path, rank).
//
// Resolution order:
//  1. Base rank → identityPathModifier. Pre-promotion units are on their raw
//     base stats regardless of path.
//  2. unitPathNone → defaultRankCurve (see path_defs.go). Covers utility
//     units (workers etc.) that earn XP without ever being assigned a path.
//     Lives in code because "none" is a system fallback, not a tunable path.
//  3. Bronze/Silver/Gold with a known path → the JSON catalog at
//     catalog/units/<faction>/<unit>/paths/<path>/<path>.json, loaded into pathModifiersByKey.
//  4. Anything else (unknown path id, missing rank row) → identity, so a typo
//     fails loud in-game (unit shows unmodified base stats) rather than
//     silently matching an unintended row.
func pathModifierFor(path, rank string) pathModifierDef {
	if rank == unitRankBase {
		return identityPathModifier
	}
	if path == unitPathNone {
		if def, ok := defaultRankCurve[rank]; ok {
			return def
		}
		return identityPathModifier
	}
	if def, ok := pathModifiersByKey[pathModifierKey(path, rank)]; ok {
		return def
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
		// Perk definitions: catalog/perks/<unit>/<path>/<rank>.json
		// Perk runtime/handlers + assignment rules: perks.go
		s.assignUnitPerkLocked(unit)
		// Grant path-specific abilities for the new (path, rank) after the perk
		// (same ordering rationale: path is already assigned). Idempotent,
		// ordered, RNG-free — see assignUnitPathAbilitiesLocked.
		s.assignUnitPathAbilitiesLocked(unit)
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

// assignUnitPathOnRankUpLocked assigns a promotion path to a unit the first
// time it reaches Bronze rank. The path is fixed for the unit's lifetime.
//
//   - Soldiers: randomly choose Vanguard or Berserker (50/50 via rngPerks).
//   - Archers: randomly choose Trapper or Marksman (50/50 via rngPerks).
//   - Apprentices: randomly choose Cleric or Arch Mage (50/50 via rngPerks).
//   - Other unit types: no path assignment (return early).
func (s *GameState) assignUnitPathOnRankUpLocked(unit *Unit) {
	if unit.ProgressionPath != unitPathNone {
		return // already assigned
	}
	if unit.Rank == unitRankBase {
		return // shouldn't happen here, but guard anyway
	}
	switch unit.UnitType {
	case "soldier":
		paths := [2]string{unitPathVanguard, unitPathBerserker}
		unit.ProgressionPath = paths[s.rngPerks.Intn(2)]
	case "archer":
		paths := [2]string{unitPathTrapper, unitPathMarksman}
		unit.ProgressionPath = paths[s.rngPerks.Intn(2)]
	case "apprentice":
		paths := [2]string{unitPathCleric, unitPathArchMage}
		unit.ProgressionPath = paths[s.rngPerks.Intn(2)]
	default:
		return
	}
}

func (s *GameState) applyRankModifiersLocked(unit *Unit, preserveHealthPercent bool) {
	if unit == nil {
		return
	}
	// One lookup — pathDef now carries the FULL rank multiplier (no stacking
	// with a separate rankProgressionTable). Edit the matching row in
	// pathModifierTable to rebalance a single (path, rank) cell with no risk
	// of contaminating another path.
	pathDef := pathModifierFor(unit.ProgressionPath, unit.Rank)

	currentHPFraction := 1.0
	if preserveHealthPercent && unit.MaxHP > 0 {
		currentHPFraction = clampFloat(float64(unit.HP)/float64(unit.MaxHP), 0, 1)
	}

	unit.MaxHP = maxInt(1, int(math.Round(float64(unit.BaseMaxHP)*pathDef.MaxHPMultiplier)))
	// Apply flat max HP bonus from hold_the_line (and any future flat-HP perks).
	// Called after path multipliers so the bonus is always the authored value.
	// Tuning point: bonusMaxHP in perk-defs.json → hold_the_line.config.
	if bonus := s.perkFlatMaxHPBonusLocked(unit); bonus > 0 {
		unit.MaxHP += bonus
	}
	unit.Damage = maxInt(0, int(math.Round(float64(unit.BaseDamage)*pathDef.DamageMultiplier)))
	unit.AttackSpeed = math.Max(0.1, unit.BaseAttackSpeed*pathDef.AttackSpeedMultiplier)
	unit.MoveSpeed = math.Max(1.0, unit.BaseMoveSpeed*pathDef.MoveSpeedMultiplier)
	unit.Armor = pathDef.Armor
	// Bake path × perk attack-range adjustments back onto unit.AttackRange so
	// every consumer (target acquisition, combat scoring, projectile flight,
	// pierce length, snapshot HUD) reads the effective range without a getter.
	// Mirrors the BaseDamage → Damage pattern above.
	//
	// Resolution order:
	//   1. baseRange = pathDef.AttackRange when > 0 (flat override),
	//                  else unit.BaseAttackRange × pathDef.AttackRangeMultiplier.
	//   2. effective = baseRange × (1 + perkAttackRangeMultiplierLocked).
	//
	// Path tuning lives in catalog/units/<faction>/<unit>/paths/<p>/<p>.json. Perk bonus
	// comes from eagle_spirit / bullseye / future range perks.
	if unit.BaseAttackRange > 0 {
		var baseRange float64
		if pathDef.AttackRange > 0 {
			baseRange = pathDef.AttackRange
		} else {
			pathRangeMult := pathDef.AttackRangeMultiplier
			if pathRangeMult <= 0 {
				pathRangeMult = 1.0
			}
			baseRange = unit.BaseAttackRange * pathRangeMult
		}
		unit.AttackRange = math.Max(0, baseRange*(1.0+s.perkAttackRangeMultiplierLocked(unit)))
	}
	baseVision := unit.BaseVisionRange
	if pathVision, ok := pathVisionRangeByPath[unit.ProgressionPath]; ok {
		baseVision = pathVision
	}
	unit.VisionRange = baseVision * s.perkVisionRangeMultiplierLocked(unit)

	// Path-level basic-attack overrides: a promotion path may swap the
	// projectile asset and/or damage-type tag the unit def set at spawn
	// (e.g. Cleric → holy_bolt/holy, Arch Mage → dark_bolt/shadow). Paths
	// without an override are absent from these maps and leave the unit-def
	// values untouched. ProgressionPath is monotonic (none → path, never
	// reverted), so a conditional set suffices — unlike HP/damage there is no
	// per-tick base to re-derive from.
	if pathProjectile, ok := pathProjectileByPath[unit.ProgressionPath]; ok {
		unit.ProjectileID = pathProjectile
	}
	if pathDamageType, ok := pathDamageTypeByPath[unit.ProgressionPath]; ok {
		unit.AttackDamageType = pathDamageType
	}
	if pathProjectileScale, ok := pathProjectileScaleByPath[unit.ProgressionPath]; ok {
		unit.ProjectileScale = pathProjectileScale
	}

	if unit.UnitType == "soldier" && unit.ProgressionPath == unitPathNone {
		unit.Armor = soldierBaseArmor
	}
	// Stack upgrade-sourced armor (stored in BaseArmor) on top of path/rank
	// armor without overwriting it. BaseArmor is 0 for units with no upgrades,
	// so this is a no-op for the vast majority of units.
	unit.Armor += unit.BaseArmor

	// Fold in equipment bonuses after path/rank multipliers and upgrade armor.
	// Equipment grants flat bonuses on top of everything else. No-op when
	// EquipmentBonus is zero (no items equipped).
	// NOTE: HealthRegen is intentionally excluded here because HealthRegenPerSecond
	// has no Base* counterpart to recompute from — the delta is applied by
	// recomputeUnitEquipmentBonusLocked to preserve perk-applied regen correctly.
	unit.Damage += unit.EquipmentBonus.Damage
	unit.MaxHP += unit.EquipmentBonus.HP
	unit.Armor += unit.EquipmentBonus.Armor
	unit.AttackSpeed += unit.EquipmentBonus.AttackSpeed
	unit.MoveSpeed += unit.EquipmentBonus.MoveSpeed

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
	if halfHP := unit.MaxHP / 2; unit.HP < halfHP {
		unit.HP = halfHP
	}
	// Grow the inventory to match the new rank. setInventorySizeForRankLocked
	// only ever grows the slice — rank cannot decrease.
	s.setInventorySizeForRankLocked(unit)
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
