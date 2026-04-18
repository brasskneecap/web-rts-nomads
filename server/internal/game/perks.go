package game

// ═════════════════════════════════════════════════════════════════════════════
// PERK RUNTIME — BEHAVIOUR LAYER
//
// This package owns the mutable perk state that lives on each unit and the
// hooks that apply perk effects during gameplay. Perk DEFINITION data lives
// separately under catalog/perks/.
//
// ┌─────────────────────────────────────────────────────────────────────────┐
// │  WHERE THINGS LIVE                                                      │
// │                                                                         │
// │    PERK DEFINITIONS (data, tuning, eligibility)                         │
// │      → catalog/perks/<unitType>/<path>/<rank>.json                      │
// │        One file per (unit, path, rank) slot. Adding a perk means        │
// │        appending an entry to the right file; UnitType / Path / Rank     │
// │        are inferred from the file path during loading.                  │
// │                                                                         │
// │    PERK RUNTIME BEHAVIOUR (effects, hooks, state)                       │
// │      → perks.go          UnitPerkState, assignUnitPerkLocked,           │
// │                          perkPoolForRankLocked, tickUnitPerkStateLocked │
// │      → perks_attack.go   attack-speed / on-fire / on-hit / on-kill /    │
// │                          whirlwind / cleave / bonus-damage hooks        │
// │      → perks_defense.go  damage application, healing, shields, armor,  │
// │                          incoming-damage mults, on-damage-taken,       │
// │                          flat reduction, max-HP bonus                  │
// │      → perks_movement.go perkMoveSpeedMultiplierLocked                  │
// │      → perks_icons.go    activeBuff/DebuffIconsLocked (HUD)             │
// │                                                                         │
// │    PERK ICONS (HUD artwork)                                             │
// │      → catalog/action-icons.json  (id: "perk-<name>")                   │
// └─────────────────────────────────────────────────────────────────────────┘
//
// ┌─────────────────────────────────────────────────────────────────────────┐
// │  TO ADD A NEW PERK (any path/rank)                                      │
// │    1. Add the definition to catalog/perks/<unit>/<path>/<rank>.json.    │
// │    2. Add an icon to         catalog/action-icons.json.                 │
// │    3. Add a case to whichever of the hooks below the effect needs:      │
// │         tickUnitPerkStateLocked              timers, decay, passive     │
// │         perkAttackSpeedBonusLocked           attack-speed bonus         │
// │         perkMoveSpeedMultiplierLocked        move-speed bonus           │
// │         perkBonusDamageMultiplierLocked      outgoing damage scaler     │
// │         perkIncomingDamageMultiplierLocked   incoming dmg reduction     │
// │         perkOutgoingDamageDebuffMultiplierLocked  attacker debuff       │
// │         onPerkAttackFiredLocked              on every attack (attacker) │
// │         onPerkAttackDamageAppliedLocked      on-hit / lifesteal         │
// │         onPerkDamageTakenLocked              on damage received (def.)  │
// │         onPerkKillLocked                     on every kill              │
// │         perkFlatDamageReductionLocked        flat per-hit reduction     │
// │         perkBonusArmorLocked                 conditional armor bonus    │
// │         perkFlatMaxHPBonusLocked             flat max HP bonus          │
// │         unitMaxShieldLocked                  shield pool contributor    │
// │         healUnitLocked                       overheal routing           │
// │         activeBuffIconsLocked                add buff icon to the HUD   │
// │         activeDebuffIconsLocked              add debuff icon to the HUD │
// │    4. If the perk needs persistent per-unit state, add a field to       │
// │       UnitPerkState below.                                              │
// │    5. Cross-unit debuffs (WeakenedRemaining, MarkedRemaining) decay in  │
// │       state.go Update() alongside TauntRemaining — not in                │
// │       tickUnitPerkStateLocked — because they live on units that may     │
// │       not own the perk themselves.                                      │
// │                                                                         │
// │  No other files need to change for a new perk.                          │
// └─────────────────────────────────────────────────────────────────────────┘
//
// CALL SITES (where these hooks are wired into the game loop):
//   • state.go           Update()               — tickUnitPerkStateLocked (per-unit)
//                                                 perkMoveSpeedMultiplierLocked (movement)
//                                                 WeakenedRemaining decay (cross-unit)
//                                                 MarkedRemaining decay (cross-unit)
//   • state_combat.go    tickUnitCombatLocked() — perkBonusDamageMultiplierLocked,
//                                                 perkOutgoingDamageDebuffMultiplierLocked,
//                                                 onPerkAttackFiredLocked,
//                                                 onPerkAttackDamageAppliedLocked,
//                                                 onPerkDamageTakenLocked,
//                                                 onPerkKillLocked,
//                                                 perkAttackSpeedBonusLocked
//   • perks_attack.go    savage_strikes/cleave/whirlwind secondary hits —
//                                                 onPerkAttackDamageAppliedLocked,
//                                                 onPerkDamageTakenLocked
//   • perks_defense.go   applyUnitDamageLocked  — perkIncomingDamageMultiplierLocked,
//                                                 MarkedRemaining amplification,
//                                                 perkFlatDamageReductionLocked
//   • progression.go     addUnitXPLocked()      — assignUnitPerkLocked (on rank-up)
//   • progression.go     applyRankModifiersLocked() — perkFlatMaxHPBonusLocked
// ═════════════════════════════════════════════════════════════════════════════

import "math"

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

	// ── last_stand (silver vanguard) ──────────────────────────────────────────
	// LastStandTriggered tracks whether the one-shot taunt-on-threshold-entry has
	// already fired during the current below-threshold period. Reset to false when
	// the unit heals back above the threshold so the taunt can re-fire if the unit
	// dips below again later.
	LastStandTriggered bool

	// ── punishing_guard (silver vanguard) ─────────────────────────────────────
	// WeakenedRemaining is the seconds left on this unit's outgoing-damage debuff,
	// stamped onto attackers by Punishing Guard. Decays in the main Update loop
	// (cross-unit debuff, same pattern as TauntRemaining). WeakenedMultiplier is
	// the fractional damage reduction (e.g. 0.30 = 30% less outgoing damage) set
	// at the same time and cleared when WeakenedRemaining reaches 0.
	WeakenedRemaining float64
	WeakenedMultiplier float64

	// ── bulwark (silver vanguard) ─────────────────────────────────────────────
	// StationarySeconds accumulates each tick the unit has not moved. Reset to 0
	// on any tick where the unit moves. When it reaches stationaryThresholdSeconds
	// the perk grants the unit a one-time shield up to maxShield.
	// BulwarkShieldGranted gates the grant so the shield is set exactly once per
	// stationary period — once granted, damage chips it down and it does NOT
	// regenerate until the unit moves (clearing the flag) and re-plants.
	StationarySeconds    float64
	BulwarkShieldGranted bool

	// ── challengers_mark (silver vanguard) ────────────────────────────────────
	// MarkedRemaining is the seconds left on this unit's incoming-damage
	// amplification mark, stamped onto targets by Challenger's Mark. Decays in the
	// main Update loop (cross-unit state, same pattern as TauntRemaining).
	// MarkedMultiplier is the fractional bonus (e.g. 0.15 = 15% more damage taken)
	// set when the mark is applied and cleared when MarkedRemaining reaches 0.
	MarkedRemaining float64
	MarkedMultiplier float64
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
//	Soldier → Berserker → Bronze    catalog/perks/soldier/berserker/bronze.json
//	Soldier → Berserker → Silver    catalog/perks/soldier/berserker/silver.json
//	Soldier → Berserker → Gold      catalog/perks/soldier/berserker/gold.json
//	Soldier → Vanguard  → Bronze    catalog/perks/soldier/vanguard/bronze.json
//	Soldier → Vanguard  → Silver    catalog/perks/soldier/vanguard/silver.json
//	Soldier → Vanguard  → Gold      catalog/perks/soldier/vanguard/gold.json
//	<other unit> → <path> → <rank>  catalog/perks/<unit>/<path>/<rank>.json
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

		case "last_stand":
			// Detect HP threshold crossings to fire the one-shot taunt.
			// The taunt fires once per below-threshold entry; the flag resets
			// when the unit heals back above the threshold so it can re-trigger.
			if unit.MaxHP <= 0 {
				continue
			}
			hpFrac := float64(unit.HP) / float64(unit.MaxHP)
			threshold := def.Config["hpThresholdPercent"]
			if hpFrac > threshold {
				// Above threshold — reset so next dip can trigger again.
				unit.PerkState.LastStandTriggered = false
			} else if !unit.PerkState.LastStandTriggered {
				// Just crossed below — fire the one-shot AoE taunt.
				unit.PerkState.LastStandTriggered = true
				radius := def.Config["tauntRadius"]
				radiusSq := radius * radius
				duration := def.Config["tauntDurationSeconds"]
				for _, candidate := range s.Units {
					if candidate == nil || candidate.ID == unit.ID {
						continue
					}
					if candidate.OwnerID == unit.OwnerID {
						continue
					}
					if candidate.HP <= 0 || !candidate.Visible {
						continue
					}
					dx := candidate.X - unit.X
					dy := candidate.Y - unit.Y
					if dx*dx+dy*dy <= radiusSq {
						s.ApplyTauntLocked(candidate.ID, unit.ID, duration)
					}
				}
			}

		case "bulwark":
			// Track how long the unit has been stationary. When the threshold is
			// reached, grant the shield ONCE up to maxShield — subsequent damage
			// chips it down and it does not regenerate until the unit moves and
			// re-plants. When the unit moves, reset the counter, drop any
			// existing shield, and clear the granted flag so the next stationary
			// period can re-arm. Bulwark is a planted-play reward — breaking
			// formation forfeits the protection AND any partial shield is lost.
			//
			// Assumption: no other perk grants shield to a Vanguard today
			// (blood_engine is Berserker-only). If a future Vanguard perk adds
			// shield from another source, this zero-on-move will need to
			// subtract only Bulwark's portion.
			if unit.Moving {
				unit.PerkState.StationarySeconds = 0
				unit.PerkState.BulwarkShieldGranted = false
				unit.Shield = 0
			} else {
				unit.PerkState.StationarySeconds += dt
				if !unit.PerkState.BulwarkShieldGranted &&
					unit.PerkState.StationarySeconds >= def.Config["stationaryThresholdSeconds"] {
					unit.Shield = int(def.Config["maxShield"])
					unit.PerkState.BulwarkShieldGranted = true
				}
			}

		// ── add cases for new perks with timer/decay needs below this line ──
		}
	}
}
