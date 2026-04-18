package game

// ═════════════════════════════════════════════════════════════════════════════
// PERK RUNTIME — BEHAVIOUR LAYER
//
// This file owns the mutable perk state that lives on each unit and the hooks
// that apply perk effects during gameplay. It is intentionally kept free of
// perk definition data.
//
// ┌─────────────────────────────────────────────────────────────────────────┐
// │  WHERE THINGS LIVE                                                      │
// │                                                                         │
// │    PERK DEFINITIONS (data, tuning, eligibility)                         │
// │      → catalog/perk-defs.json                                           │
// │        Hierarchy is  units → <unitType> → paths → <path> → <rank> → [] │
// │        Adding a perk means appending an entry under the correct keys;   │
// │        UnitType / Path / Rank are inferred from the position.           │
// │                                                                         │
// │    PERK RUNTIME BEHAVIOUR (effects, hooks, state)                       │
// │      → this file (perks.go) — assignment + all seven hook functions     │
// │                                                                         │
// │    PERK ICONS (HUD artwork)                                             │
// │      → catalog/action-icons.json  (id: "perk-<name>")                   │
// └─────────────────────────────────────────────────────────────────────────┘
//
// ┌─────────────────────────────────────────────────────────────────────────┐
// │  TO ADD A NEW PERK (any path/rank)                                      │
// │    1. Add the definition to  catalog/perk-defs.json.                   │
// │    2. Add an icon to         catalog/action-icons.json.                │
// │    3. Add a case to whichever of the hooks below the effect needs:      │
// │         tickUnitPerkStateLocked              timers, decay, passive    │
// │         perkAttackSpeedBonusLocked           attack-speed bonus        │
// │         perkMoveSpeedMultiplierLocked        move-speed bonus          │
// │         perkBonusDamageMultiplierLocked      outgoing damage scaler    │
// │         onPerkAttackFiredLocked              on every attack (attacker)│
// │         onPerkAttackDamageAppliedLocked      on-hit / lifesteal        │
// │         onPerkDamageTakenLocked              on damage received (def.) │
// │         onPerkKillLocked                     on every kill             │
// │         perkFlatDamageReductionLocked        flat per-hit reduction    │
// │         perkFlatMaxHPBonusLocked             flat max HP bonus         │
// │         unitMaxShieldLocked                  shield pool contributor   │
// │         healUnitLocked                       overheal routing          │
// │         activeBuffIconsLocked                add buff icon to the HUD  │
// │    4. If the perk needs persistent per-unit state, add a field to      │
// │       UnitPerkState below.                                             │
// │                                                                         │
// │  No other files need to change for a new perk.                         │
// └─────────────────────────────────────────────────────────────────────────┘
//
// CALL SITES (where these hooks are wired into the game loop):
//   • state.go  Update()               — tickUnitPerkStateLocked (per-unit)
//                                        perkMoveSpeedMultiplierLocked (movement)
//   • state.go  tickUnitCombatLocked() — perkBonusDamageMultiplierLocked,
//                                        onPerkAttackFiredLocked,
//                                        onPerkAttackDamageAppliedLocked,
//                                        onPerkDamageTakenLocked,
//                                        onPerkKillLocked,
//                                        perkAttackSpeedBonusLocked
//   • perks.go  (savage_strikes, cleave, whirlwind secondary hits) —
//                                        onPerkAttackDamageAppliedLocked,
//                                        onPerkDamageTakenLocked
//   • perks.go  applyUnitDamageLocked  — perkFlatDamageReductionLocked
//   • progression.go addUnitXPLocked() — assignUnitPerkLocked (on rank-up)
//   • progression.go applyRankModifiersLocked() — perkFlatMaxHPBonusLocked
// ═════════════════════════════════════════════════════════════════════════════

import (
	"math"
)

// ─────────────────────────────────────────────────────────────────────────────
// Unit perk state
//
// UnitPerkState holds all mutable runtime data produced by perk effects.
// Add a new field here if a new perk needs persistent per-unit state that
// cannot be derived on-the-fly from the unit's other fields.
//
// A single state struct is shared across every perk the unit owns — the
// fields below are disjoint per perk, and shared fields (e.g. TimeSinceLastAttack)
// are fine as long as every reader treats them as a common resource rather than
// owning them. Fields unused by the unit's current perks stay at their zero
// values and cost nothing.
// ─────────────────────────────────────────────────────────────────────────────

type UnitPerkState struct {
	// ── shared ────────────────────────────────────────────────────────────────
	// TimeSinceLastAttack advances every tick and resets to 0 each time the unit
	// fires an attack. Currently read by bloodlust to decide when to clear its
	// stack; future idle-sensitive perks can reuse it for free.
	TimeSinceLastAttack float64

	// ── bloodlust ─────────────────────────────────────────────────────────────
	// Accumulated attack-speed bonus from consecutive attacks. Falls back to 0
	// when TimeSinceLastAttack exceeds the perk's resetAfterSeconds config value.
	BloodlustBonus float64

	// ── savage_strikes ────────────────────────────────────────────────────────
	// Running count of attacks fired since the last trigger reset.
	AttackCounter int

	// ── relentless ────────────────────────────────────────────────────────────
	// Temporary post-kill attack-speed boost; both fields reset to 0 when
	// RelentlessRemaining reaches 0.
	RelentlessBonus     float64
	RelentlessRemaining float64

	// ── momentum (silver berserker) ───────────────────────────────────────────
	// Temporary post-attack move-speed bonus expressed as a multiplier
	// (0.25 = +25% speed). Refreshed on every attack and decays to 0 when
	// MomentumRemaining reaches 0.
	MomentumBonus     float64
	MomentumRemaining float64

	// ── whirlwind_core (gold berserker) ───────────────────────────────────────
	// Two-phase timer: ActiveRemaining > 0 means the whirlwind window is ON
	// (attacks trigger AoE); otherwise CooldownRemaining counts down to the
	// next activation. Seeded to cooldown on first tick so the very first
	// proc happens `cooldownSeconds` after the perk is granted rather than
	// on the exact rank-up tick.
	WhirlwindActiveRemaining   float64
	WhirlwindCooldownRemaining float64

	// frenzy_core   — no stored state; bonus derived from current HP% on demand.
	// cleaving_rage — no stored state; triggers unconditionally on every attack.
	// blood_sustain — no stored state; heals from damage dealt on demand.
	// executioner   — no stored state; bonus derived from target HP% on demand.
	// blood_engine  — no stored state; shield pool lives on Unit.Shield.
	// berserk_state — no stored state; bonus derived from current HP% on demand.

	// ── retaliation (bronze vanguard) ─────────────────────────────────────────
	// RetaliationActive is a recursion guard: set true before applying reflected
	// damage so the reflected hit does not trigger another reflection loop.
	// Reset to false immediately after the reflected damage call returns.
	RetaliationActive bool
}

// ─────────────────────────────────────────────────────────────────────────────
// Perk assignment
// ─────────────────────────────────────────────────────────────────────────────

// assignUnitPerkLocked grants one new perk to a unit that just ranked up and
// appends it to unit.PerkIDs. The slice is ordered by rank-up order so index 0
// corresponds to the Bronze grant, index 1 to Silver, and index 2 to Gold.
//
// Call AFTER assignUnitPathOnRankUpLocked so ProgressionPath is already set.
//
// The pool is drawn from the perk catalog filtered by (unitType, path, rank)
// where rank matches the unit's *current* rank. If the exact rank pool is empty
// (e.g. Gold is not yet authored) we fall back to the Bronze pool so the unit
// still receives a perk. Perks already on the unit are filtered out so no perk
// can be received twice.
//
// ── FUTURE EXPANSION — where to add more perks ─────────────────────────────
//
//	Soldier → Berserker → Bronze    catalog/perk-defs.json  units.soldier.paths.berserker.bronze
//	Soldier → Berserker → Silver    catalog/perk-defs.json  units.soldier.paths.berserker.silver
//	Soldier → Berserker → Gold      catalog/perk-defs.json  units.soldier.paths.berserker.gold
//	Soldier → Vanguard  → Bronze    catalog/perk-defs.json  units.soldier.paths.vanguard.bronze
//	Soldier → Vanguard  → Silver    catalog/perk-defs.json  units.soldier.paths.vanguard.silver
//	Soldier → Vanguard  → Gold      catalog/perk-defs.json  units.soldier.paths.vanguard.gold
//	<other unit> → <path> → <rank>  catalog/perk-defs.json  units.<unit>.paths.<path>.<rank>
//
// No code changes needed in this file for adding perks at any existing slot —
// the assignment, pool filter, and eligibility check all key off the hierarchy
// in the JSON. You only touch this file to author RUNTIME EFFECT logic (the
// hook cases further down).
func (s *GameState) assignUnitPerkLocked(unit *Unit) {
	if unit == nil || unit.Rank == unitRankBase {
		return
	}
	pool := s.perkPoolForRankLocked(unit, unit.Rank)
	if len(pool) == 0 {
		return
	}
	unit.PerkIDs = append(unit.PerkIDs, pool[s.rngPerks.Intn(len(pool))].ID)
}

// perkPoolForRankLocked returns the list of perk defs a unit is eligible to be
// granted at the given rank, excluding any perk the unit already owns. If the
// rank-specific pool is empty, falls back to the Bronze pool so rank-ups still
// produce a grant while higher-tier perks are still being authored.
func (s *GameState) perkPoolForRankLocked(unit *Unit, rank string) []*PerkDef {
	pool := eligiblePerksForUnitAtRank(unit, rank)
	if len(pool) == 0 && rank != unitRankBronze {
		pool = eligiblePerksForUnitAtRank(unit, unitRankBronze)
	}
	if len(pool) == 0 {
		return nil
	}
	owned := make(map[string]struct{}, len(unit.PerkIDs))
	for _, id := range unit.PerkIDs {
		owned[id] = struct{}{}
	}
	filtered := pool[:0]
	for _, def := range pool {
		if _, has := owned[def.ID]; has {
			continue
		}
		filtered = append(filtered, def)
	}
	return filtered
}

// ═════════════════════════════════════════════════════════════════════════════
// EXTENSION POINT — PERK RUNTIME HANDLERS
//
// Each function below iterates the unit's PerkIDs and switches on each id.
// To add a new perk's behaviour, add a `case "your_perk_id":` in whichever
// of the four hooks the effect needs, then document any new UnitPerkState
// fields above.
//
// The four hooks cover every timing you are likely to need:
//   tickUnitPerkStateLocked    — called every tick; for timers and decay
//   perkAttackSpeedBonusLocked — called when resetting the attack cooldown
//   onPerkAttackFiredLocked    — called immediately after an attack fires
//   onPerkKillLocked           — called when the unit scores a kill
// ═════════════════════════════════════════════════════════════════════════════

// ─────────────────────────────────────────────────────────────────────────────
// Hook 1 of 4 — per-tick state tick
// ─────────────────────────────────────────────────────────────────────────────

// tickUnitPerkStateLocked advances all time-based perk state for one unit.
// Called every tick per unit from state.go Update(), after combat resolution.
//
// ADD NEW PERK TIMER / DECAY LOGIC HERE.
func (s *GameState) tickUnitPerkStateLocked(unit *Unit, dt float64) {
	if len(unit.PerkIDs) == 0 {
		return
	}

	// Advance idle timer once per tick — every perk that tracks
	// TimeSinceLastAttack reads this shared field.
	unit.PerkState.TimeSinceLastAttack += dt

	for _, perkID := range unit.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}

		switch perkID {

		case "bloodlust":
			// Reset accumulated stack once the unit has been idle long enough.
			if unit.PerkState.BloodlustBonus > 0 &&
				unit.PerkState.TimeSinceLastAttack >= def.Config["resetAfterSeconds"] {
				unit.PerkState.BloodlustBonus = 0
			}

		case "relentless":
			// Decay the post-kill attack-speed boost.
			if unit.PerkState.RelentlessRemaining > 0 {
				unit.PerkState.RelentlessRemaining = math.Max(0, unit.PerkState.RelentlessRemaining-dt)
				if unit.PerkState.RelentlessRemaining == 0 {
					unit.PerkState.RelentlessBonus = 0
				}
			}

		case "momentum":
			// Decay the post-attack move-speed buff.
			if unit.PerkState.MomentumRemaining > 0 {
				unit.PerkState.MomentumRemaining = math.Max(0, unit.PerkState.MomentumRemaining-dt)
				if unit.PerkState.MomentumRemaining == 0 {
					unit.PerkState.MomentumBonus = 0
				}
			}

		case "whirlwind_core":
			// Two-phase cycle: active window → cooldown → active window → …
			// When state is fresh (both zero) seed the cooldown so the first
			// proc fires after cooldownSeconds rather than instantly.
			if unit.PerkState.WhirlwindActiveRemaining > 0 {
				unit.PerkState.WhirlwindActiveRemaining = math.Max(0, unit.PerkState.WhirlwindActiveRemaining-dt)
				if unit.PerkState.WhirlwindActiveRemaining == 0 {
					unit.PerkState.WhirlwindCooldownRemaining = def.Config["cooldownSeconds"]
				}
			} else if unit.PerkState.WhirlwindCooldownRemaining > 0 {
				unit.PerkState.WhirlwindCooldownRemaining = math.Max(0, unit.PerkState.WhirlwindCooldownRemaining-dt)
				if unit.PerkState.WhirlwindCooldownRemaining == 0 {
					unit.PerkState.WhirlwindActiveRemaining = def.Config["activeSeconds"]
				}
			} else {
				unit.PerkState.WhirlwindCooldownRemaining = def.Config["cooldownSeconds"]
			}

		// ── add cases for new perks with timer/decay needs below this line ──
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 2 of 4 — attack speed bonus query
// ─────────────────────────────────────────────────────────────────────────────

// perkAttackSpeedBonusLocked returns the current attack-speed bonus from the
// unit's perk. Recomputed fresh on every call so dynamic perks (frenzy_core)
// always reflect live game state. Returns 0 for units with no relevant perk.
//
// Used in state.go tickUnitCombatLocked():
//
//	effectiveSpeed := unit.AttackSpeed + s.perkAttackSpeedBonusLocked(unit)
//	unit.AttackCooldown = 1.0 / math.Max(0.1, effectiveSpeed)
//
// ADD NEW ATTACK-SPEED-MODIFYING PERKS HERE.
func (s *GameState) perkAttackSpeedBonusLocked(unit *Unit) float64 {
	if len(unit.PerkIDs) == 0 {
		return 0
	}

	total := 0.0
	for _, perkID := range unit.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}

		switch perkID {

		case "bloodlust":
			total += unit.PerkState.BloodlustBonus

		case "frenzy_core":
			// Bonus scales linearly from 0 at full HP to maxBonus at 0 HP.
			if unit.MaxHP > 0 {
				hpFraction := clampFloat(float64(unit.HP)/float64(unit.MaxHP), 0, 1)
				total += (1.0 - hpFraction) * def.Config["maxBonus"]
			}

		case "relentless":
			total += unit.PerkState.RelentlessBonus

		case "berserk_state":
			// Passive: bonus active only while the unit's own HP is below the
			// threshold. Mirrors the damage multiplier above.
			if unit.MaxHP > 0 {
				hpFraction := float64(unit.HP) / float64(unit.MaxHP)
				if hpFraction <= def.Config["hpThresholdPercent"] {
					total += def.Config["attackSpeedBonus"]
				}
			}

		// ── add cases for new attack-speed perks below this line ────────────
		}

		// savage_strikes and cleaving_rage do not modify attack speed.
	}
	return total
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 3 of 4 — on attack fired
// ─────────────────────────────────────────────────────────────────────────────

// onPerkAttackFiredLocked is called immediately after a unit fires a normal
// attack at a target unit, before the caller checks whether the target died.
//
// Rules for this hook:
//   - Reset TimeSinceLastAttack to 0 here (already done below for all perks).
//   - If a perk kills a SECONDARY target (e.g. cleaving_rage), append its ID
//     to *deadUnitIDs — the caller handles removal.
//   - If a perk deals extra damage to the PRIMARY target (e.g. savage_strikes),
//     do NOT append the primary to *deadUnitIDs — the caller checks target.HP <= 0
//     after this function returns and handles it there.
//
// ADD NEW ON-ATTACK PERKS HERE.
// primaryDamage is the raw damage dealt by the triggering attack. Not consumed
// by current Bronze Berserker perks, but rename from _ when a future perk
// needs to scale off or react to the hit value.
func (s *GameState) onPerkAttackFiredLocked(attacker, primaryTarget *Unit, _ int, deadUnitIDs *[]int) {
	if attacker == nil || len(attacker.PerkIDs) == 0 {
		return
	}

	// Reset idle timer once per attack — shared across all the attacker's perks.
	attacker.PerkState.TimeSinceLastAttack = 0

	for _, perkID := range attacker.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}

		switch perkID {

		case "bloodlust":
			// Accumulate attack-speed bonus, capped at maxBonus.
			attacker.PerkState.BloodlustBonus = math.Min(
				attacker.PerkState.BloodlustBonus+def.Config["bonusPerAttack"],
				def.Config["maxBonus"],
			)

		case "savage_strikes":
			attacker.PerkState.AttackCounter++
			n := int(def.Config["everyNthAttack"])
			if n > 0 && attacker.PerkState.AttackCounter >= n {
				attacker.PerkState.AttackCounter = 0
				// Fire the bonus hit only if the primary target survived the normal hit.
				if primaryTarget != nil && primaryTarget.HP > 0 {
					bonusDmg := maxInt(0, int(math.Round(float64(attacker.Damage)*def.Config["bonusMultiplier"])))
					actualDmg := applyArmorMitigation(bonusDmg, primaryTarget.Armor)
					if actualDmg > 0 {
						s.applyUnitDamageLocked(primaryTarget, actualDmg)
						s.onUnitDamagedLocked(attacker, primaryTarget, actualDmg)
						s.onPerkDamageTakenLocked(primaryTarget, attacker, actualDmg)
						s.recordDamageDealtLocked(attacker, primaryTarget, actualDmg)
						// Let on-damage perks (blood_sustain) react to the extra hit.
						s.onPerkAttackDamageAppliedLocked(attacker, primaryTarget, actualDmg)
						// Primary target death is handled by the caller — do NOT append here.
					}
				}
			}

		case "cleaving_rage":
			s.applyCleaveHitLocked(attacker, primaryTarget, def.Config["splashRadius"], deadUnitIDs)

		case "momentum":
			// Refresh the post-attack move-speed buff. Overwrites any remaining
			// duration so consecutive attacks keep the buff at full value.
			attacker.PerkState.MomentumBonus = def.Config["moveSpeedBonus"]
			attacker.PerkState.MomentumRemaining = def.Config["durationSeconds"]

		case "whirlwind_core":
			// While the whirlwind window is active, every attack also hits all
			// other enemies within the configured radius of the attacker.
			if attacker.PerkState.WhirlwindActiveRemaining > 0 {
				s.applyWhirlwindHitLocked(attacker, primaryTarget, def.Config["radius"], deadUnitIDs)
			}

		case "taunting_strike":
			// On proc, apply a taunt to the primary target for a short duration.
			// The taunted enemy strongly prefers targeting this Vanguard while the
			// taunt is active. Falls off naturally via decayThreatLocked in combat_ai.go.
			// Proc chance and duration are tunable in perk-defs.json (tauntChance, tauntDurationSeconds).
			if primaryTarget != nil && s.rngPerks.Float64() < def.Config["tauntChance"] {
				s.ApplyTauntLocked(primaryTarget.ID, attacker.ID, def.Config["tauntDurationSeconds"])
			}

		// ── add cases for new on-attack perks below this line ───────────────
		}
	}
}

// applyWhirlwindHitLocked deals full attacker damage to every visible enemy
// (other than primaryTarget) within radius of the attacker. Routes through the
// same shield/on-hit/XP pipeline as a normal attack so lifesteal, damage XP,
// and kill XP all work transparently.
func (s *GameState) applyWhirlwindHitLocked(attacker, primaryTarget *Unit, radius float64, deadUnitIDs *[]int) {
	if attacker == nil || radius <= 0 {
		return
	}
	radiusSq := radius * radius
	primaryID := 0
	if primaryTarget != nil {
		primaryID = primaryTarget.ID
	}
	for _, candidate := range s.Units {
		if candidate == nil || candidate.ID == primaryID {
			continue
		}
		if candidate.OwnerID == attacker.OwnerID {
			continue
		}
		if candidate.HP <= 0 || !candidate.Visible {
			continue
		}
		dx := candidate.X - attacker.X
		dy := candidate.Y - attacker.Y
		if dx*dx+dy*dy > radiusSq {
			continue
		}
		damage := applyArmorMitigation(attacker.Damage, candidate.Armor)
		if damage == 0 {
			continue
		}
		s.applyUnitDamageLocked(candidate, damage)
		s.onUnitDamagedLocked(attacker, candidate, damage)
		s.onPerkDamageTakenLocked(candidate, attacker, damage)
		s.recordDamageDealtLocked(attacker, candidate, damage)
		s.onPerkAttackDamageAppliedLocked(attacker, candidate, damage)
		if candidate.HP <= 0 {
			candidate.HP = 0
			s.awardKillXPLocked(attacker)
			s.payoutDamageDealtXPLocked(candidate)
			s.awardSoldierTankKillXPLocked(candidate.ID)
			s.onPerkKillLocked(attacker)
			*deadUnitIDs = append(*deadUnitIDs, candidate.ID)
		}
	}
}

// applyCleaveHitLocked finds the nearest enemy within splashRadius of
// primaryTarget (excluding the primary itself) and applies full damage to it.
// Awards XP and appends to deadUnitIDs if the secondary target dies.
func (s *GameState) applyCleaveHitLocked(attacker, primaryTarget *Unit, splashRadius float64, deadUnitIDs *[]int) {
	if primaryTarget == nil {
		return
	}
	var secondary *Unit
	var secondaryDist float64
	for _, candidate := range s.Units {
		if candidate == nil || candidate.ID == primaryTarget.ID {
			continue
		}
		if candidate.OwnerID == attacker.OwnerID {
			continue // do not cleave friendlies
		}
		if candidate.HP <= 0 || !candidate.Visible {
			continue
		}
		dx := candidate.X - primaryTarget.X
		dy := candidate.Y - primaryTarget.Y
		dist := math.Sqrt(dx*dx + dy*dy)
		if dist > splashRadius {
			continue
		}
		if secondary == nil || dist < secondaryDist {
			secondary = candidate
			secondaryDist = dist
		}
	}
	if secondary == nil {
		return
	}
	damage := applyArmorMitigation(attacker.Damage, secondary.Armor)
	if damage == 0 {
		return
	}
	s.applyUnitDamageLocked(secondary, damage)
	s.onUnitDamagedLocked(attacker, secondary, damage)
	s.onPerkDamageTakenLocked(secondary, attacker, damage)
	s.recordDamageDealtLocked(attacker, secondary, damage)
	// Let on-damage perks (blood_sustain) react to cleave hits.
	s.onPerkAttackDamageAppliedLocked(attacker, secondary, damage)
	if secondary.HP <= 0 {
		secondary.HP = 0
		s.awardKillXPLocked(attacker)
		s.payoutDamageDealtXPLocked(secondary)
		s.awardSoldierTankKillXPLocked(secondary.ID)
		*deadUnitIDs = append(*deadUnitIDs, secondary.ID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 4 of 4 — on kill
// ─────────────────────────────────────────────────────────────────────────────

// onPerkKillLocked is called immediately after a unit lands a killing blow on
// a target. Called alongside awardKillXPLocked in state.go.
//
// ADD NEW ON-KILL PERKS HERE.
func (s *GameState) onPerkKillLocked(attacker *Unit) {
	if attacker == nil || len(attacker.PerkIDs) == 0 {
		return
	}

	for _, perkID := range attacker.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}

		switch perkID {

		case "relentless":
			// Grant the post-kill attack-speed burst; overwrites any remaining duration.
			attacker.PerkState.RelentlessBonus = def.Config["bonus"]
			attacker.PerkState.RelentlessRemaining = def.Config["durationSeconds"]

		// ── add cases for new on-kill perks below this line ─────────────────
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 5 — outgoing damage multiplier (pre-armor)
//
// perkBonusDamageMultiplierLocked returns an additive multiplier applied to
// the attacker's raw damage BEFORE armor mitigation, for attacks against the
// given target. 0 means "no bonus" (final damage = base damage).
//
// Used in state.go tickUnitCombatLocked() primary-attack damage calc:
//
//	raw := float64(unit.Damage) * (1.0 + s.perkBonusDamageMultiplierLocked(unit, target))
//	damage := applyArmorMitigation(int(math.Round(raw)), target.Armor)
//
// Scoped to the PRIMARY attack only — secondary perk hits (savage_strikes
// bonus, cleave) deliberately do not stack this bonus.
//
// ADD NEW OUTGOING-DAMAGE-MODIFYING PERKS HERE.
// ─────────────────────────────────────────────────────────────────────────────
//
// Safe to call with a nil target (e.g. from Snapshot() when computing the
// effective damage to show in the HUD): target-dependent cases like
// executioner no-op, self-based cases like berserk_state still apply.
func (s *GameState) perkBonusDamageMultiplierLocked(attacker, target *Unit) float64 {
	if attacker == nil || len(attacker.PerkIDs) == 0 {
		return 0
	}

	total := 0.0
	for _, perkID := range attacker.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}

		switch perkID {

		case "executioner":
			// Bonus applies only when the target is below the HP threshold
			// at the time damage is dealt. No-op when called without a target.
			if target != nil && target.MaxHP > 0 {
				hpFraction := float64(target.HP) / float64(target.MaxHP)
				if hpFraction <= def.Config["hpThresholdPercent"] {
					total += def.Config["bonusMultiplier"]
				}
			}

		case "berserk_state":
			// Passive: bonus active only while the attacker's own HP is below
			// the threshold. Recomputed live, so the bonus appears/disappears
			// cleanly as HP changes without requiring state updates.
			if attacker.MaxHP > 0 {
				hpFraction := float64(attacker.HP) / float64(attacker.MaxHP)
				if hpFraction <= def.Config["hpThresholdPercent"] {
					total += def.Config["damageMultiplier"]
				}
			}

		// ── add cases for new damage-multiplier perks below this line ───────
		}
	}
	return total
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 6 — after damage applied (on-hit reactions)
//
// onPerkAttackDamageAppliedLocked is called whenever a perk-capable attack
// actually deals damage to a target, for every damage source that comes from
// the attacker's attack resolution:
//   - primary attack (state.go)
//   - savage_strikes bonus hit (perks.go onPerkAttackFiredLocked)
//   - cleaving_rage secondary (perks.go applyCleaveHitLocked)
//
// `damage` is the post-armor damage actually applied. Safe to call with 0 or
// negative damage — the hook early-outs in that case.
//
// ADD NEW ON-HIT REACTION PERKS (lifesteal, on-hit procs) HERE.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) onPerkAttackDamageAppliedLocked(attacker, target *Unit, damage int) {
	if attacker == nil || target == nil || damage <= 0 || len(attacker.PerkIDs) == 0 {
		return
	}
	// Dead attackers don't heal (blood_sustain) — guards against weird edges
	// where a perk hits after the attacker has already died this tick.
	if attacker.HP <= 0 {
		return
	}

	for _, perkID := range attacker.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}

		switch perkID {

		case "blood_sustain":
			// Heal for a percentage of damage dealt. Routed through
			// healUnitLocked so blood_engine (gold) can convert overheal into
			// shield. No recursion risk — healing never triggers damage events.
			heal := int(math.Round(float64(damage) * def.Config["lifestealPercent"]))
			if heal > 0 {
				s.healUnitLocked(attacker, heal)
			}

		// ── add cases for new on-hit reaction perks below this line ─────────
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 7 — move-speed multiplier
//
// perkMoveSpeedMultiplierLocked returns the effective move-speed multiplier
// contributed by the unit's perks. Always returns ≥ 1.0 (no perk = 1.0).
//
// Used in state.go Update() movement step:
//
//	step := unitMoveSpeed * s.perkMoveSpeedMultiplierLocked(unit) * dt
//
// ADD NEW MOVE-SPEED-MODIFYING PERKS HERE.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) perkMoveSpeedMultiplierLocked(unit *Unit) float64 {
	if unit == nil || len(unit.PerkIDs) == 0 {
		return 1.0
	}

	bonus := 0.0
	for _, perkID := range unit.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}

		switch perkID {

		case "momentum":
			// State-driven: the post-attack buff is refreshed/decayed in
			// onPerkAttackFiredLocked / tickUnitPerkStateLocked.
			bonus += unit.PerkState.MomentumBonus

		// ── add cases for new move-speed perks below this line ──────────────
		}
	}
	return 1.0 + bonus
}

// ═════════════════════════════════════════════════════════════════════════════
// SHIELD / HEAL / BUFF HELPERS
//
// These helpers centralize the unit-side state transitions that perks drive.
// Damage intake, heal application, and the list of "active buffs" advertised
// to the client all live here so the integration points from state.go and
// perks.go are one-liners.
//
// EXTENSION POINTS:
//   • applyUnitDamageLocked    — add new damage-intake reducers (armor-
//                                 like, reflective, etc.) before or after
//                                 the shield pool.
//   • healUnitLocked           — add new overheal routings (e.g. future
//                                 gold perks that convert overheal into
//                                 something other than shield).
//   • unitMaxShieldLocked      — aggregate max-shield from multiple perks
//                                 here if future perks also contribute.
//   • activeBuffIconsLocked    — return extra buff icon ids when new timed
//                                 or conditional states are added. Each id
//                                 must match an entry in action-icons.json.
// ═════════════════════════════════════════════════════════════════════════════

// applyUnitDamageLocked applies post-armor damage to a unit, routing through
// flat perk reduction and then the shield pool. Returns the portion that
// actually reduced HP (flat-reduction and shield-absorbed amounts are NOT
// included). Callers should keep using their original `damage` value for XP
// banking, threat, on-hit reactions, etc. so internal reductions don't
// retroactively penalize attackers.
//
// Damage intake order:
//   1. Caller computes post-armor damage (applyArmorMitigation).
//   2. perkFlatDamageReductionLocked reduces it further (reinforced_armor).
//   3. Shield pool absorbs what remains.
//   4. HP takes what the shield didn't absorb.
//
// Called from every unit-damage intake site:
//   - state.go primary attack
//   - state.go building-on-unit attack
//   - perks.go savage_strikes bonus hit
//   - perks.go applyCleaveHitLocked
//   - perks.go applyWhirlwindHitLocked
//
// A damage intake that bypasses this helper will bypass flat reduction and
// shield — avoid it.
func (s *GameState) applyUnitDamageLocked(target *Unit, damage int) int {
	if target == nil || damage <= 0 {
		return 0
	}
	// Flat per-hit reduction from reinforced_armor (and future flat reducers).
	// Applied after caller's armor mitigation, before the shield pool.
	// Tuning point: flatReduction in catalog/perk-defs.json → reinforced_armor.config.
	if reduction := s.perkFlatDamageReductionLocked(target); reduction > 0 {
		damage = maxInt(0, damage-reduction)
		if damage == 0 {
			return 0
		}
	}
	if target.Shield > 0 {
		if target.Shield >= damage {
			target.Shield -= damage
			return 0
		}
		damage -= target.Shield
		target.Shield = 0
	}
	target.HP -= damage
	// Clamp to 0 so HP is never stored as negative. Death detection in callers
	// uses HP <= 0, so 0 is the correct sentinel — not an arbitrary negative.
	if target.HP < 0 {
		target.HP = 0
	}
	return damage
}

// healUnitLocked adds `amount` HP to a unit, clamped to MaxHP. If the unit has
// blood_engine (gold berserker), any excess beyond MaxHP becomes shield up to
// the perk's configured cap. Safe to call with non-positive amounts.
//
// ADD NEW OVERHEAL ROUTINGS HERE (e.g. future perks that convert overheal
// into something other than shield).
func (s *GameState) healUnitLocked(unit *Unit, amount int) {
	if unit == nil || amount <= 0 || unit.HP <= 0 {
		return
	}
	missing := unit.MaxHP - unit.HP
	if amount <= missing {
		unit.HP += amount
		return
	}
	unit.HP = unit.MaxHP
	overheal := amount - missing
	maxShield := s.unitMaxShieldLocked(unit)
	if maxShield <= 0 || overheal <= 0 {
		return
	}
	unit.Shield = minInt(maxShield, unit.Shield+overheal)
}

// unitMaxShieldLocked returns the unit's current shield capacity, aggregated
// from all perks that contribute a shield pool. 0 for units with no such perk.
// ADD NEW SHIELD-GRANTING PERKS HERE.
func (s *GameState) unitMaxShieldLocked(unit *Unit) int {
	if unit == nil || len(unit.PerkIDs) == 0 {
		return 0
	}
	total := 0
	for _, perkID := range unit.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {
		case "blood_engine":
			total += int(def.Config["maxShield"])
		// ── add cases for new shield-granting perks below this line ─────────
		}
	}
	return total
}

// activeBuffIconsLocked returns the perk ids whose timed or conditional buff
// is currently active on the unit, in a stable order. The client uses this
// list to render floating indicator icons near the sprite (see CanvasRenderer
// drawUnitActiveBuffs). Returns nil when nothing is active so the slice is
// omitted from the JSON snapshot.
//
// Kept as a single switch so adding a new active-buff perk only requires one
// case here plus the matching runtime hook case elsewhere in this file.
//
// ADD NEW VISUALLY-INDICATED BUFFS HERE.
func (s *GameState) activeBuffIconsLocked(unit *Unit) []string {
	if unit == nil || len(unit.PerkIDs) == 0 {
		return nil
	}
	var active []string
	for _, perkID := range unit.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {

		case "bloodlust":
			if unit.PerkState.BloodlustBonus > 0 {
				active = append(active, perkID)
			}
		case "relentless":
			if unit.PerkState.RelentlessRemaining > 0 {
				active = append(active, perkID)
			}
		case "momentum":
			if unit.PerkState.MomentumRemaining > 0 {
				active = append(active, perkID)
			}
		case "whirlwind_core":
			if unit.PerkState.WhirlwindActiveRemaining > 0 {
				active = append(active, perkID)
			}
		case "berserk_state":
			// Conditional passive: show while below HP threshold so the
			// player can see the buff kick in and fall off as HP changes.
			if unit.MaxHP > 0 {
				hpFraction := float64(unit.HP) / float64(unit.MaxHP)
				if hpFraction <= def.Config["hpThresholdPercent"] {
					active = append(active, perkID)
				}
			}

		// ── add cases for new visually-indicated buffs below this line ──────
		}
	}
	return active
}

// ═════════════════════════════════════════════════════════════════════════════
// VANGUARD PERK HOOKS
//
// These three functions implement the defender-side perk effects introduced
// for the Vanguard path. They are called from the damage pipeline and the
// rank-modifier application path.
//
// EXTENSION POINTS — adding more perks later:
//   • More Bronze Vanguard perks  → add entries to perk-defs.json under
//                                   units.soldier.paths.vanguard.bronze
//                                   and add cases to the relevant hook(s) below.
//   • Silver/Gold Vanguard perks  → add entries under vanguard.silver / .gold
//                                   in perk-defs.json, then add cases here as
//                                   needed. Same hooks apply.
//   • Perks for other unit types  → add the unit type under units.<type>.paths
//                                   in perk-defs.json and add cases here.
// ═════════════════════════════════════════════════════════════════════════════

// ─────────────────────────────────────────────────────────────────────────────
// Hook 8 — on damage received (defender-side reactions)
//
// onPerkDamageTakenLocked is called after a unit takes damage from an attacker.
// `damage` is the post-armor value that was passed into the damage pipeline
// (i.e. what the attacker intended after armor, before flat reduction or shield).
//
// Called from:
//   - state.go tickUnitCombatLocked     — primary attack
//   - perks.go savage_strikes bonus hit — secondary hit
//   - perks.go applyCleaveHitLocked     — cleave secondary
//   - perks.go applyWhirlwindHitLocked  — whirlwind AoE
//
// ADD NEW DEFENDER-SIDE PERK REACTIONS HERE.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) onPerkDamageTakenLocked(target, attacker *Unit, damage int) {
	if target == nil || attacker == nil || damage <= 0 || len(target.PerkIDs) == 0 {
		return
	}
	// Skip reactions if the unit is already dead this tick.
	if target.HP <= 0 {
		return
	}

	for _, perkID := range target.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}

		switch perkID {

		case "retaliation":
			// Reflect damage equal to (armorPercent × this unit's armor) back to the
			// attacker on each hit. Higher-armor Vanguards punish attackers more.
			//
			// Guard: RetaliationActive prevents recursive reflection if the attacker
			// also has retaliation. The reflected hit goes through applyUnitDamageLocked
			// only — no XP, threat, or further perk hooks — keeping the chain flat.
			//
			// Tuning point: armorPercent in perk-defs.json → retaliation.config.
			if target.PerkState.RetaliationActive {
				continue // already inside a reflection; do not chain
			}
			if attacker.HP <= 0 || attacker.OwnerID == target.OwnerID {
				continue
			}
			reflected := maxInt(0, int(math.Round(float64(target.Armor)*def.Config["armorPercent"])))
			if reflected <= 0 {
				continue
			}
			// Set guard before the call so any path that re-enters this function
			// for this unit is a no-op.
			target.PerkState.RetaliationActive = true
			// Bypass the full damage pipeline — no XP, no threat, no further hooks.
			// This keeps reflected damage simple and prevents infinite chains.
			s.applyUnitDamageLocked(attacker, reflected)
			target.PerkState.RetaliationActive = false

		// ── add cases for new defender-side reactions below this line ────────
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 9 — flat per-hit damage reduction query (defender-side)
//
// perkFlatDamageReductionLocked returns the total flat damage reduction the
// target gets from its perks, applied per hit after armor mitigation and before
// the shield pool. Returns 0 for units with no relevant perk.
//
// Called from applyUnitDamageLocked — covers all damage sources automatically.
//
// ADD NEW FLAT-DAMAGE-REDUCTION PERKS HERE.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) perkFlatDamageReductionLocked(target *Unit) int {
	if target == nil || len(target.PerkIDs) == 0 {
		return 0
	}
	total := 0
	for _, perkID := range target.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {
		case "reinforced_armor":
			// Tuning point: flatReduction in perk-defs.json → reinforced_armor.config.
			total += int(def.Config["flatReduction"])
		// ── add cases for new flat-reduction perks below this line ───────────
		}
	}
	return total
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 10 — flat max HP bonus query (passive stat modifier)
//
// perkFlatMaxHPBonusLocked returns the total flat max HP bonus granted by the
// unit's perks. Applied additively on top of rank × path multipliers inside
// applyRankModifiersLocked (progression.go) so it is always included when stats
// are recalculated. Returns 0 for units with no relevant perk.
//
// Called from progression.go applyRankModifiersLocked.
//
// ADD NEW FLAT-MAX-HP PERKS HERE.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) perkFlatMaxHPBonusLocked(unit *Unit) int {
	if unit == nil || len(unit.PerkIDs) == 0 {
		return 0
	}
	total := 0
	for _, perkID := range unit.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {
		case "hold_the_line":
			// Tuning point: bonusMaxHP in perk-defs.json → hold_the_line.config.
			total += int(def.Config["bonusMaxHP"])
		// ── add cases for new flat max HP perks below this line ──────────────
		}
	}
	return total
}
