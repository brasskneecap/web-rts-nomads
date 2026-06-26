package game

import (
	"math"
	"sort"
)

const (
	unitRankBase   = "base"
	unitRankBronze = "bronze"
	unitRankSilver = "silver"
	unitRankGold   = "gold"
)

// Promotion path constants. Assigned at Bronze rank and fixed for the unit's lifetime.
// Soldiers randomly receive Vanguard or Berserker. Archers randomly receive
// Trapper or Marksman. Acolytes randomly receive Cleric or Siphoner.
const (
	unitPathNone      = "none"
	unitPathVanguard  = "vanguard"
	unitPathBerserker = "berserker"
	unitPathTrapper   = "trapper"
	unitPathMarksman  = "marksman"
	unitPathCleric    = "cleric"
	unitPathSiphoner  = "siphoner"
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

	// Experience-system selector values. The active mode is read from
	// gameplayTuning().Experience.Mode (catalog/tuning/gameplay_tuning.json).
	// "classic" = kill bonus + damage-dealt + soldier-tank payouts (legacy).
	// "split"   = a single per-enemy experience value, divided evenly among
	//             eligible recipients as raw XP, fully replacing the above.
	experienceModeClassic = "classic"
	experienceModeSplit   = "split"

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
//	   33       24.8%   (soldier pre-promotion, from soldier.json "armor": 33)
//	   54       35.1%   (vanguard baseline)
//	  100       50.0%
//	  200       66.7%
//	  300       75.0%
//
// Base armor for unpathed units comes from the unit catalog JSON ("armor" field).
// For promoted units, armor comes from pathModifierDef.Armor plus any per-player
// advancement bonus (the delta between the effective def and the raw catalog def).
const (
	armorMitigationK = 100
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
//	cleric    — support caster (Acolyte); stat tuning deferred, rows
//	            currently mirror the default rank curve. No perks yet.
//	arch_mage — offensive caster (Adept); stat tuning deferred, rows
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
	if unit == nil || unit.HP <= 0 || !unit.Visible {
		return false
	}
	if unit.OwnerID == enemyPlayerID || unit.OwnerID == neutralPlayerID {
		return false
	}
	return true
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

// resolveUnitXPValue returns the raw XP a unit of this def yields when killed
// in "split" mode. Absent experience falls back to the tuned default; an
// explicit 0 means the unit grants no XP. Mode-agnostic — the value is seeded
// at spawn and simply unused in "classic" mode.
func resolveUnitXPValue(def UnitDef) int {
	if def.Experience != nil {
		return *def.Experience
	}
	return gameplayTuning().Experience.SplitDefaultXP
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

// addUnitXPRawFloatLocked is addUnitXPFloatLocked WITHOUT the xpGainMultiplier
// scaling: `amount` is the literal XP, accumulated through the same per-unit
// XPProgressRemainder so sub-1 fractions (e.g. 0.5) eventually form whole XP
// and cross rank thresholds. Used only by "split" mode. Because exactly one
// mode is active per server run, scaled (addUnitXPFloatLocked) and raw
// contributions never mix into the same accumulator.
func (s *GameState) addUnitXPRawFloatLocked(unit *Unit, amount float64) {
	if !s.unitCanGainXPLocked(unit) || amount <= 0 {
		return
	}
	total := unit.XPProgressRemainder + amount
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
// The distribution is data-driven: each unit type declares a "pathChances"
// weight map in its catalog JSON (catalog/units/<faction>/<unit>/<unit>.json),
// e.g. archer → {"trapper":1,"marksman":1} (50/50) or adept → {"arch_mage":1}
// (guaranteed). Unit types with no "pathChances" (workers, raiders) get no
// path and stay on unitPathNone. The roll draws from the deterministic
// rngPerks stream, so a given seed always assigns the same paths.
func (s *GameState) assignUnitPathOnRankUpLocked(unit *Unit) {
	if unit.ProgressionPath != unitPathNone {
		return // already assigned
	}
	if unit.Rank == unitRankBase {
		return // shouldn't happen here, but guard anyway
	}
	def, ok := getUnitDef(unit.UnitType)
	if !ok {
		return
	}
	if path := s.rollProgressionPathLocked(def.PathChances); path != "" {
		unit.ProgressionPath = path
	}
}

// rollProgressionPathLocked picks a promotion path from a weighted
// distribution (path id → relative weight) using the deterministic perk RNG
// stream. Keys are sorted before the roll so map iteration order never drives
// the outcome (simulation determinism invariant). Weights are relative and
// normalized by their sum, so {"a":1,"b":1} is a 50/50 split. Returns "" when
// the distribution is empty or every weight is non-positive — the caller then
// leaves the unit on unitPathNone.
func (s *GameState) rollProgressionPathLocked(chances map[string]float64) string {
	if len(chances) == 0 {
		return ""
	}
	paths := make([]string, 0, len(chances))
	var total float64
	for path, weight := range chances {
		if weight <= 0 {
			continue
		}
		paths = append(paths, path)
		total += weight
	}
	if total <= 0 {
		return ""
	}
	sort.Strings(paths)
	r := s.rngPerks.Float64() * total
	for _, path := range paths {
		r -= chances[path]
		if r < 0 {
			return path
		}
	}
	// Float rounding can leave r exactly at total; fall back to the last path.
	return paths[len(paths)-1]
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
	// (e.g. Cleric → holy_bolt/holy, Arch Mage → shadow_bolt/shadow). Paths
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

	// Armor from catalog and per-player advancement bonus.
	//
	// rawCatalogArmor: the unit's base armor as authored in <unit>.json, before
	// any player advancements are applied.
	//
	// effectiveDefArmor: raw + per-player advancement deltas (e.g. +25 per
	// soldier_armor node). Resolved via EffectiveUnitDefs so the advancement
	// bonus is per-player, identical to what spawnPlayerUnitLocked uses.
	//
	// For unpathed units the catalog base armor IS the unit's armor; advancements
	// stack on top. For promoted units the path/rank table provides the armor
	// (pathDef.Armor above), and only the advancement delta is added on top so
	// that promoting a soldier doesn't lose the armor-advancement bonus.
	rawCatalogArmor := 0
	if rawDef, rawOK := getUnitDef(unit.UnitType); rawOK {
		rawCatalogArmor = rawDef.Armor
	}
	effectiveDefArmor := rawCatalogArmor
	if player, pOK := s.Players[unit.OwnerID]; pOK {
		if effDef, eOK := player.EffectiveUnitDefs[unit.UnitType]; eOK {
			effectiveDefArmor = effDef.Armor
		}
	}
	if unit.ProgressionPath == unitPathNone {
		// No promotion path: catalog base armor is the unit's entire armor
		// contribution from the def layer (replaces the old soldierBaseArmor
		// const and the unit-type-specific special case it required).
		unit.Armor = effectiveDefArmor
	} else {
		// Promoted unit: pathDef.Armor was set above (line: unit.Armor = pathDef.Armor).
		// Add only the advancement delta so the player's armor advancements
		// continue to benefit promoted soldiers.
		unit.Armor += effectiveDefArmor - rawCatalogArmor
	}
	// Stack upgrade-track armor (stored in BaseArmor) on top. BaseArmor is only
	// populated by applyPlayerUpgradesAtSpawnLocked (workshop/armoury upgrade
	// tracks). Advancement armor flows through effectiveDefArmor above, so
	// BaseArmor no longer carries it — avoiding the previous double-count.
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

	// Zone-aura max health: a cached/folded stat (MaxHP is not read on demand),
	// so it is re-baked here as (current MaxHP + add) × mul. recomputeAllZone
	// AuraModifiersLocked re-runs this with preserveHealthPercent=true on any
	// ownership flip, so the cap tracks zone control. Identity (0,1) ⇒ unchanged.
	if hpAdd, hpMul := s.playerStatModifierLocked(unit.OwnerID, statMaxHealth); hpAdd != 0 || hpMul != 1 {
		unit.MaxHP = maxInt(1, int(math.Round((float64(unit.MaxHP)+hpAdd)*hpMul)))
	}

	// Zone-aura max mana: only for units with a mana pool. Always recomputed from
	// the catalog base (MaxMana is otherwise only ever def.MaxMana) so losing the
	// zone reverts the pool; mana fraction is preserved across the change.
	if def, ok := getUnitDef(unit.UnitType); ok && def.MaxMana > 0 {
		mnAdd, mnMul := s.playerStatModifierLocked(unit.OwnerID, statMaxMana)
		newMaxMana := maxInt(0, int(math.Round((float64(def.MaxMana)+mnAdd)*mnMul)))
		if newMaxMana != unit.MaxMana {
			manaFraction := 1.0
			if unit.MaxMana > 0 {
				manaFraction = clampFloat(float64(unit.CurrentMana)/float64(unit.MaxMana), 0, 1)
			}
			unit.MaxMana = newMaxMana
			unit.CurrentMana = int(math.Round(float64(newMaxMana) * manaFraction))
			if unit.CurrentMana > newMaxMana {
				unit.CurrentMana = newMaxMana
			}
		}
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
	if halfHP := unit.MaxHP / 2; unit.HP < halfHP {
		unit.HP = halfHP
	}
	// Grow the inventory to match the new rank. setInventorySizeForRankLocked
	// only ever grows the slice — rank cannot decrease.
	s.setInventorySizeForRankLocked(unit)
	// Metrics: recompute the owner's UnitsByRank map. The semantic is
	// "currently at this rank or higher" (see match_metrics.go), so the
	// recompute walks all alive units for that owner.
	s.recomputeUnitsByRankForOwnerLocked(unit.OwnerID)
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
	if gameplayTuning().Experience.Mode == experienceModeSplit {
		return // buildings grant no XP in split mode
	}
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

// awardSplitDeathXPLocked distributes a dead enemy's raw XPValue evenly among
// every eligible recipient: friendly units (per unitCanGainXPLocked) either
// within SplitEligibilityRadius of the death position OR that ever dealt
// damage to it. No eligible recipients ⇒ the XP is lost (no killer fallback).
// Used only in "split" mode. Recipient IDs are sorted before payout so the
// distribution is deterministic regardless of map iteration order (per the
// determinism invariant) — order does not change the equal share anyway.
func (s *GameState) awardSplitDeathXPLocked(dead *Unit) {
	if dead == nil || dead.XPValue <= 0 {
		return
	}

	recipients := map[int]*Unit{}

	// Proximity: any eligible unit within the radius of the death position.
	radius := gameplayTuning().Experience.SplitEligibilityRadius
	radiusSq := radius * radius
	for _, u := range s.Units {
		if u == nil || !s.unitCanGainXPLocked(u) {
			continue
		}
		dx := u.X - dead.X
		dy := u.Y - dead.Y
		if dx*dx+dy*dy <= radiusSq {
			recipients[u.ID] = u
		}
	}

	// Contributors: any unit that ever dealt damage to this enemy. The ledger
	// is populated in every mode by recordDamageDealtLocked.
	for attackerID := range dead.DamageDealtByUnit {
		if _, seen := recipients[attackerID]; seen {
			continue
		}
		attacker := s.getUnitByIDLocked(attackerID)
		if attacker == nil || !s.unitCanGainXPLocked(attacker) {
			continue
		}
		recipients[attackerID] = attacker
	}

	if len(recipients) == 0 {
		return // no eligible recipients → XP is lost
	}

	ids := make([]int, 0, len(recipients))
	for id := range recipients {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	share := float64(dead.XPValue) / float64(len(ids))
	for _, id := range ids {
		s.addUnitXPRawFloatLocked(recipients[id], share)
	}
}

// awardUnitDeathXPLocked is the single entry point for "a unit just died,
// settle its XP". It replaces the legacy awardKillXPLocked+payoutDamageDealtXPLocked
// pair at every kill site. `killer` may be nil (matching the legacy pair's
// nil-safety) and is ignored in split mode.
//
//   - classic: verbatim relocation of the legacy pair, in the original order.
//   - split:   even per-enemy split (killer intentionally unused).
func (s *GameState) awardUnitDeathXPLocked(dead, killer *Unit) {
	if dead == nil {
		return
	}
	if gameplayTuning().Experience.Mode == experienceModeSplit {
		s.awardSplitDeathXPLocked(dead)
		return
	}
	if killer != nil {
		s.awardKillXPLocked(killer)
	}
	s.payoutDamageDealtXPLocked(dead)
}

func (s *GameState) awardSoldierTankKillXPLocked(defeatedUnitID int) {
	if gameplayTuning().Experience.Mode == experienceModeSplit {
		return // split mode fully replaces classic payouts
	}
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
