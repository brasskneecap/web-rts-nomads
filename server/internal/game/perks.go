package game

// ═════════════════════════════════════════════════════════════════════════════
// PERK RUNTIME — BEHAVIOUR LAYER
//
// This file owns the mutable perk state that lives on each unit and the hooks
// that apply perk effects during gameplay. It is intentionally kept free of
// perk definition data.
//
// ┌─────────────────────────────────────────────────────────────────────────┐
// │  TO ADD A NEW PERK:                                                     │
// │    1. Add the definition to  catalog/perk-defs.json  (data layer).     │
// │    2. Add a case in whichever hook(s) below the effect needs:           │
// │         • tickUnitPerkStateLocked     — timers, decay, passive ticks   │
// │         • perkAttackSpeedBonusLocked  — if it modifies attack speed     │
// │         • onPerkAttackFiredLocked     — fires on every attack           │
// │         • onPerkKillLocked            — fires on every kill             │
// │                                                                         │
// │  No other files need to change for a new perk.                         │
// └─────────────────────────────────────────────────────────────────────────┘
//
// CALL SITES (where these hooks are wired into the game loop):
//   • state.go  Update()               — tickUnitPerkStateLocked (per-unit)
//   • state.go  tickUnitCombatLocked() — onPerkAttackFiredLocked, onPerkKillLocked,
//                                        perkAttackSpeedBonusLocked
//   • progression.go addUnitXPLocked() — assignUnitPerkLocked (on rank-up)
// ═════════════════════════════════════════════════════════════════════════════

import (
	"math"
	"math/rand"
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

	// frenzy_core   — no stored state; bonus derived from current HP% on demand.
	// cleaving_rage — no stored state; triggers unconditionally on every attack.
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
// Current behaviour: Silver and Gold rank-ups draw from the Bronze pool for now
// (until rank-specific perks exist) but filter out any perk already on the unit
// so no perk is received twice.
func (s *GameState) assignUnitPerkLocked(unit *Unit) {
	if unit == nil || unit.Rank == unitRankBase {
		return
	}
	// Until Silver/Gold perk pools are authored, draw every rank-up perk from
	// the Bronze pool.
	pool := eligiblePerksForUnitAtRank(unit, unitRankBronze)
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
	if len(filtered) == 0 {
		return
	}
	unit.PerkIDs = append(unit.PerkIDs, filtered[rand.Intn(len(filtered))].ID)
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
					actualDmg := maxInt(0, bonusDmg-primaryTarget.Armor)
					if actualDmg > 0 {
						primaryTarget.HP -= actualDmg
						s.onUnitDamagedLocked(attacker, primaryTarget, actualDmg)
						s.recordDamageDealtLocked(attacker, primaryTarget, actualDmg)
						// Primary target death is handled by the caller — do NOT append here.
					}
				}
			}

		case "cleaving_rage":
			s.applyCleaveHitLocked(attacker, primaryTarget, def.Config["splashRadius"], deadUnitIDs)

		// ── add cases for new on-attack perks below this line ───────────────
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
	damage := maxInt(0, attacker.Damage-secondary.Armor)
	if damage == 0 {
		return
	}
	secondary.HP -= damage
	s.onUnitDamagedLocked(attacker, secondary, damage)
	s.recordDamageDealtLocked(attacker, secondary, damage)
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
