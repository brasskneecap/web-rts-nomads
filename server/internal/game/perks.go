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
// │         perkArmorPercentBonusLocked          %armor bonus (self perks)  │
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
// │    5. (Optional) Set `requiresPerk` in the JSON entry to gate this perk │
// │       on a previously-owned perk ID. The prereq filter runs in          │
// │       perkPoolForRankLocked and silently excludes the perk from the     │
// │       pool until the unit owns the named perk.                          │
// │    6. Cross-unit debuffs (WeakenedRemaining, MarkedRemaining) decay in  │
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
//   • perks_defense.go   applyUnitDamageLocked  — perkRedirectIncomingDamageLocked (pain_share),
//                                                 MarkedRemaining amplification,
//                                                 perkFlatDamageReductionLocked
//   • perks_defense.go   effectiveArmorLocked    — perkBonusArmorLocked,
//                                                 perkArmorPercentBonusLocked,
//                                                 perkBonusArmorFromBannersLocked,
//                                                 perkBonusArmorFromAurasLocked,
//                                                 perkArmorPercentBonusFromAurasLocked
//   • progression.go     addUnitXPLocked()      — assignUnitPerkLocked (on rank-up)
//   • progression.go     applyRankModifiersLocked() — perkFlatMaxHPBonusLocked
// ═════════════════════════════════════════════════════════════════════════════

import (
	"math"
	"strconv"
)

// ─────────────────────────────────────────────────────────────────────────────
// Debuff stacking
//
// Certain debuffs (mark, burn) stack when the incoming source differs from
// every source currently stacked on the victim. Same-source re-application
// refreshes the existing stack (stronger multiplier wins, longer duration
// wins — legacy "refresh-stronger / refresh-longer" semantics). A new source
// only adds a stack when we're below maxDebuffStacks; beyond the cap the
// new application is dropped.
//
// maxDebuffStacks is the ceiling used today (2). It is a package-level const
// so future tuning can bump it — or swap to a per-debuff cap — without
// touching the stack data types.
// ─────────────────────────────────────────────────────────────────────────────

const maxDebuffStacks = 2

// unitMarkSourceID namespaces unit-originated stack sources (e.g. a Vanguard
// attacking with challengers_mark) so they can't collide with trap ids. Trap
// ids are strings like "trap-5"; unit sources are "unit-<id>".
func unitMarkSourceID(unitID int) string {
	return "unit-" + strconv.Itoa(unitID)
}

// markStack is one stack of the mark debuff (challengers_mark / marker_trap).
//
// SourceID keys the stack — same-source re-application refreshes, a new
// source adds a slot. For trap-applied marks, SourceID is the Trap.ID
// (so two marker_traps from one Trapper each count as separate sources
// and both stacks land if an enemy stands in their overlap). For unit-
// applied marks (e.g. challengers_mark), SourceID is "unit-<id>" of the
// attacking unit — same attacker refreshes, different attackers stack.
//
// OwnerUnitID is carried separately for XP/telemetry credit on kill.
// Traps keep their owner ID even after the trapper dies; attacks record
// the current attacker. Not used as the stack key.
type markStack struct {
	SourceID    string
	OwnerUnitID int
	Remaining   float64
	Multiplier  float64
}

// burnStack is one stack of the burn debuff (lasting_flames, Flame Collapse).
// Each stack holds its own DPS + accumulator so multi-source burns deal
// cumulative damage. Keying rules mirror markStack.SourceID (trap-id for
// trap burns, unit-id for any hypothetical future on-attack burn).
// ReactiveRadius / ReactiveDamage piggyback the Reactive Flames gold effect
// per-stack so every ticking source fires its own AoE.
type burnStack struct {
	SourceID       string
	OwnerUnitID    int
	Remaining      float64
	DPS            float64
	Accumulator    float64
	ReactiveRadius float64
	ReactiveDamage int
}

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
	// LastStandTriggered latches to true when HP first crosses below threshold,
	// preventing the perk from re-firing during a single dip. Reset to false when
	// the unit heals back above the threshold so another dip can re-trigger.
	//
	// LastStandRemaining is the seconds left on the combined armor-bonus +
	// taunt window. Set to durationSeconds on trigger, decayed each tick. Both
	// the flat armor bonus (perkBonusArmorLocked) and the buff icon read this
	// directly — HP fraction is no longer consulted once the window is running,
	// so healing out of the threshold mid-window keeps the bonus until the
	// timer expires.
	LastStandTriggered bool
	LastStandRemaining float64

	// ── punishing_guard (silver vanguard) ─────────────────────────────────────
	// WeakenedRemaining is the seconds left on this unit's outgoing-damage debuff,
	// stamped onto attackers by Punishing Guard. Decays in the main Update loop
	// (cross-unit debuff, same pattern as TauntRemaining). WeakenedMultiplier is
	// the fractional damage reduction (e.g. 0.30 = 30% less outgoing damage) set
	// at the same time and cleared when WeakenedRemaining reaches 0.
	WeakenedRemaining float64
	WeakenedMultiplier float64

	// ── rallying_banner (gold vanguard) — stationary counter ─────────────────
	// StationarySeconds accumulates each tick the unit has not moved. Reset to 0
	// on any tick where the unit moves. Rallying_banner reads this to gate when
	// the Vanguard has been planted long enough to drop a banner.
	StationarySeconds float64

	// ── mark debuff (challengers_mark + marker_trap) ─────────────────────────
	// Marks stack per source (up to maxDebuffStacks concurrent stacks). Each
	// stack carries its own OwnerUnitID, Remaining duration, and Multiplier.
	// Refresh semantics:
	//   - Same-source re-application refreshes that stack (stronger multiplier
	//     wins, longer duration wins — same as before).
	//   - New-source application adds a stack if we're below the cap.
	//   - Over-cap applications are dropped (existing stacks run to expiry
	//     and free a slot; common "stack cap" convention).
	// Damage amplification sums across active stacks — two 20% marks = +40%.
	MarkStacks []markStack

	// ── rallying_banner (gold vanguard) ───────────────────────────────────────
	// BannerCooldownRemaining is the seconds left on the replant cooldown. Set
	// to cooldownSeconds (12s) when a banner is planted; decays every tick
	// regardless of whether the unit is moving or stationary. A banner can only
	// be planted when this value is 0. The cooldown persists across movement —
	// moving does NOT reset it, unlike StationarySeconds.
	BannerCooldownRemaining float64

	// ── pain_share (gold vanguard) ────────────────────────────────────────────
	// PainShareActive is a recursion guard: set true before applying redirected
	// damage so a Vanguard currently absorbing a redirect is not an eligible
	// redirect target. Pattern mirrors RetaliationActive.
	PainShareActive bool

	// ── trapper (archer bronze) ───────────────────────────────────────────────
	// TrapPlaceCooldownRemaining is the seconds remaining before the Trapper may
	// plant the next trap. Set to placeIntervalSeconds when a trap is planted;
	// decays each tick in tickTrapPlacementLocked. Shared across all four Bronze
	// trap types — a unit can own at most one trap perk, so no collision.
	TrapPlaceCooldownRemaining float64

	// LastCombatSeconds is the tail-window tracking whether the Archer has
	// recently fired an attack. Set to 1.5s in tickUnitCombatLocked each time
	// the Archer fires; decayed in state.go Update() per-unit loop. Trap
	// placement is gated on this being > 0. Non-archers always have this at 0.
	LastCombatSeconds float64

	// TrapDoTAccumulator banks fractional damage per tick from any trap DoT
	// (caltrops, fire_pit). Per-tick damage = damagePerSecond × dt is typically
	// fractional (e.g. 3 × 0.05 = 0.15). Without accumulation, integer rounding
	// drops it to zero every tick. The accumulator persists across ticks; when it
	// reaches ≥ 1, the integer portion is applied via applyUnitDamageLocked and
	// the fractional remainder stays banked. Multiple traps on the same unit
	// accumulate together, which correctly stacks DoT rate.
	TrapDoTAccumulator float64

	// ── barbed_field (silver trapper) ─────────────────────────────────────────
	// BarbedFieldStaySeconds accumulates the elapsed time the victim has been
	// inside ANY barbed-field caltrops zone without a break. Ramping bonus DPS
	// is computed from this accumulator by the caltrops onStay effect. Resets
	// to 0 in tickTrapperSilverDebuffsLocked on any tick the victim is NOT in
	// a barbed caltrops this tick (one-tick exit window).
	//
	// BarbedFieldInZoneThisTick is a per-tick scratch flag set true by caltrops
	// onStay when the trap has barbed_field armed. Consumed and cleared in
	// tickTrapperSilverDebuffsLocked each tick. Shared across all barbed
	// caltrops hitting the same victim in one tick so the accumulator only
	// advances once per tick regardless of how many overlapping zones hit.
	BarbedFieldStaySeconds    float64
	BarbedFieldInZoneThisTick bool

	// ── burn debuff (lasting_flames + Flame Collapse) ────────────────────────
	// Burn stacks per source (up to maxDebuffStacks). Each stack runs its own
	// DPS/accumulator and credits its own owner for XP on death, so damage
	// from multiple trappers on the same victim is additive.
	//
	// While the victim stands in a lasting_flames fire_pit, the fire_pit
	// branch of tickTrapEffectsLocked refreshes the matching stack's
	// Remaining to the full duration every tick — the countdown only makes
	// progress once the victim leaves the zone. Flame Collapse (overload
	// protocol on fire_pit) stamps via the same helper so its DoT integrates
	// seamlessly.
	//
	// Reactive Flames piggybacks per-stack: each ticking source fires its
	// own secondary AoE, so a victim burning from two trappers with Infusion
	// gets two Reactive Flames explosions per burn-tick-cycle.
	BurnStacks []burnStack

	// ── ascendant_infusion (gold trapper) ────────────────────────────────────
	// Per-target cooldown gating Electrified Caltrops micro-stuns. Set on the
	// victim whenever an Electrified tick stuns them; decayed every tick in
	// state.go Update() alongside the other cross-unit CC decays. A stun can
	// only fire when this is 0 — prevents stun-lock from multiple ticks.
	ElectrifiedStunCooldownRemaining float64

	// Shared Pain recursion guard. Set true while a marked victim's damage is
	// being redistributed to other marked enemies so the redistributed damage
	// cannot itself trigger Shared Pain again. Pattern mirrors PainShareActive.
	SharedPainActive bool

	// Shared Pain activation data stamped on the victim when a marker_trap
	// with ascendant_infusion stamps a mark. SharedPainFraction > 0 means the
	// victim participates in Shared Pain while any mark stack is active;
	// cleared when the last mark stack expires (see state.go decay loop).
	// Not stacked itself — the strongest fraction from any active marker_trap
	// wins (refresh-stronger) to keep Shared Pain redistribution predictable.
	SharedPainFraction float64

	// ── overload_protocol (gold trapper) — Final Exposure ───────────────────
	// Armed when a marker_trap with overload_protocol stamps a mark. When the
	// victim's MarkedRemaining decays to 0 (state.go Update loop), the burst
	// damage + AoE is detonated once via fireFinalExposureLocked.
	FinalExposureDamage      int
	FinalExposureAoeRadius   float64
	FinalExposureOwnerUnitID int
	// Trap that armed this Final Exposure. Read by fireFinalExposureLocked so
	// kill credit and battle telemetry route through the trap bucket rather
	// than the trapper-unit bucket. Cleared alongside the other Final Exposure
	// fields when the mark expires.
	FinalExposureTrapID string

	// ── Marksman: Hunter's Mark stacks (silver hunters_mark + gold explosive_tips)
	// Stacks per source — same Marksman re-applying refreshes its stack;
	// distinct attackers stack up to maxHuntersMarkStacks. Decay happens in
	// state.go Update() per-unit loop because the debuff lives on enemies that
	// don't own the perk themselves (cross-unit pattern, mirrors MarkStacks).
	// Total crit-chance bonus has diminishing returns and is computed in
	// huntersMarkCritBonus() — see perks_marksman.go.
	HuntersMarkStacks []huntersMarkStack

	// ── Marksman: Double Shot deferred-fire bookkeeping (gold double_shot)
	// On every primary attack the attacker arms DoubleShotPendingSeconds with
	// the configured delay and DoubleShotPendingTargetID with the target ID.
	// tickUnitPerkStateLocked decrements the timer and, when it reaches 0,
	// fires the deferred shot via fireDeferredDoubleShotLocked. The recursion
	// guard prevents the second shot from arming a third — mirrors
	// RetaliationActive / PainShareActive patterns.
	DoubleShotPendingSeconds  float64
	DoubleShotPendingTargetID int
	DoubleShotInProgress      bool

	// ── Marksman: Explosive Tips recursion guard (gold explosive_tips)
	// Set true while the AoE damage is being applied so a victim with their
	// own explosive_tips (or a future on-damage perk that re-enters this
	// hook) cannot trigger a chain reaction. Cleared when the explosion call
	// returns. Per-attacker — different attackers explode independently.
	ExplosiveTipsActive bool

	// ── Marksman: fire-time recursion guard (split shot / double shot)
	// Set true while a unit is dispatching its fire-time Marksman effects so
	// that secondary projectiles (split arrows, deferred double-shot fires)
	// don't recurse into the same dispatch and chain into infinite shots.
	// Per-attacker; different attackers' guards are independent.
	MarksmanFireInProgress bool
}

// ─────────────────────────────────────────────────────────────────────────────
// Debuff-stack helpers
//
// The apply* methods encapsulate the stack rules so call sites stay terse:
//   1. If an existing stack matches the incoming source (OwnerUnitID), refresh
//      it (stronger multiplier/DPS wins, longer duration wins).
//   2. Otherwise, if we're under maxDebuffStacks, append a new stack.
//   3. Otherwise drop the incoming application (existing stacks run to expiry
//      before new sources can land — classic stack-cap behavior).
//
// applyMarkStack returns true when the stack landed (refreshed or new slot),
// false when rejected by the cap — callers don't currently read the return
// value but it's exposed so future UI can flash a "debuff rejected" tell.
// ─────────────────────────────────────────────────────────────────────────────

func (ps *UnitPerkState) applyMarkStack(sourceID string, ownerUnitID int, multiplier, duration float64) bool {
	if multiplier <= 0 || duration <= 0 || sourceID == "" {
		return false
	}
	for i := range ps.MarkStacks {
		if ps.MarkStacks[i].SourceID == sourceID {
			if multiplier > ps.MarkStacks[i].Multiplier {
				ps.MarkStacks[i].Multiplier = multiplier
			}
			if duration > ps.MarkStacks[i].Remaining {
				ps.MarkStacks[i].Remaining = duration
			}
			// Refresh the owner ID too so XP credit reflects the latest
			// source — important when an attacker respawns / is replaced
			// by a different unit with the same trap lineage.
			ps.MarkStacks[i].OwnerUnitID = ownerUnitID
			return true
		}
	}
	if len(ps.MarkStacks) >= maxDebuffStacks {
		return false
	}
	ps.MarkStacks = append(ps.MarkStacks, markStack{
		SourceID:    sourceID,
		OwnerUnitID: ownerUnitID,
		Remaining:   duration,
		Multiplier:  multiplier,
	})
	return true
}

// applyBurnStack applies or refreshes a burn stack. reactiveRadius and
// reactiveDamage piggyback the Reactive Flames gold effect — pass zeros when
// the applying trap does not have Infusion. sourceID must be unique per
// source-entity (trap ID for trap burns) so two traps from the same Trapper
// land as separate stacks when their zones overlap.
func (ps *UnitPerkState) applyBurnStack(sourceID string, ownerUnitID int, dps, duration, reactiveRadius float64, reactiveDamage int) bool {
	if dps <= 0 || duration <= 0 || sourceID == "" {
		return false
	}
	for i := range ps.BurnStacks {
		if ps.BurnStacks[i].SourceID == sourceID {
			if dps > ps.BurnStacks[i].DPS {
				ps.BurnStacks[i].DPS = dps
			}
			if duration > ps.BurnStacks[i].Remaining {
				ps.BurnStacks[i].Remaining = duration
			}
			if reactiveRadius > ps.BurnStacks[i].ReactiveRadius {
				ps.BurnStacks[i].ReactiveRadius = reactiveRadius
			}
			if reactiveDamage > ps.BurnStacks[i].ReactiveDamage {
				ps.BurnStacks[i].ReactiveDamage = reactiveDamage
			}
			ps.BurnStacks[i].OwnerUnitID = ownerUnitID
			return true
		}
	}
	if len(ps.BurnStacks) >= maxDebuffStacks {
		return false
	}
	ps.BurnStacks = append(ps.BurnStacks, burnStack{
		SourceID:       sourceID,
		OwnerUnitID:    ownerUnitID,
		Remaining:      duration,
		DPS:            dps,
		ReactiveRadius: reactiveRadius,
		ReactiveDamage: reactiveDamage,
	})
	return true
}

// totalMarkMultiplier is the sum of every active mark stack's multiplier —
// consumed by applyUnitDamageLocked. Two 20% marks become +40% damage taken.
func (ps *UnitPerkState) totalMarkMultiplier() float64 {
	total := 0.0
	for _, s := range ps.MarkStacks {
		total += s.Multiplier
	}
	return total
}

// anyMarkActive reports whether any mark stack remains. Cheap replacement for
// the old `MarkedRemaining > 0` checks scattered across the codebase.
func (ps *UnitPerkState) anyMarkActive() bool {
	return len(ps.MarkStacks) > 0
}

// maxMarkRemaining is the greatest remaining duration across active mark
// stacks — used by HUD icon rendering so the shown duration reflects the
// longest-lasting stack rather than any single one.
func (ps *UnitPerkState) maxMarkRemaining() float64 {
	best := 0.0
	for _, s := range ps.MarkStacks {
		if s.Remaining > best {
			best = s.Remaining
		}
	}
	return best
}

// maxBurnRemaining mirrors maxMarkRemaining for the burn debuff.
func (ps *UnitPerkState) maxBurnRemaining() float64 {
	best := 0.0
	for _, s := range ps.BurnStacks {
		if s.Remaining > best {
			best = s.Remaining
		}
	}
	return best
}

// decayMarkStacks reduces every stack's Remaining by dt and drops expired
// stacks in-place (filter-into-front-of-slice, no allocation). Returns true
// iff the final stack expired this tick — callers use that signal to fire
// once-on-expiry effects like Final Exposure.
func (ps *UnitPerkState) decayMarkStacks(dt float64) (lastExpired bool) {
	hadAny := len(ps.MarkStacks) > 0
	if !hadAny {
		return false
	}
	kept := ps.MarkStacks[:0]
	for _, s := range ps.MarkStacks {
		s.Remaining = math.Max(0, s.Remaining-dt)
		if s.Remaining > 0 {
			kept = append(kept, s)
		}
	}
	ps.MarkStacks = kept
	return hadAny && len(ps.MarkStacks) == 0
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
// granted at the given rank. Filters out perks the unit already owns and perks
// whose RequiresPerk prerequisite isn't satisfied.
//
// Cascading fallback: if the requested rank yields an empty post-filter pool,
// drop to the next lower rank and try again. Gold → Silver → Bronze; Silver →
// Bronze. This keeps rank-ups productive while higher tiers are sparsely
// authored OR while the unit's earlier picks gate them out of every higher-tier
// perk. Returns nil only when even Bronze is empty.
func (s *GameState) perkPoolForRankLocked(unit *Unit, rank string) []*PerkDef {
	for _, tryRank := range perkRankCascade(rank) {
		if pool := s.eligiblePerksAfterFiltersLocked(unit, tryRank); len(pool) > 0 {
			return pool
		}
	}
	return nil
}

// perkRankCascade returns the rank fallback order for a starting rank. Higher
// tiers cascade down through every lower tier so a unit always has a chance at
// a perk grant during development when higher tiers are sparse or fully gated.
func perkRankCascade(rank string) []string {
	switch rank {
	case unitRankGold:
		return []string{unitRankGold, unitRankSilver, unitRankBronze}
	case unitRankSilver:
		return []string{unitRankSilver, unitRankBronze}
	default:
		return []string{unitRankBronze}
	}
}

// eligiblePerksAfterFiltersLocked loads the perk pool for one specific rank
// and applies both the "already owned" and RequiresPerk filters. Returns nil
// when no perks survive — the caller (perkPoolForRankLocked) decides whether
// to cascade down to a lower rank.
func (s *GameState) eligiblePerksAfterFiltersLocked(unit *Unit, rank string) []*PerkDef {
	pool := eligiblePerksForUnitAtRank(unit, rank)
	if len(pool) == 0 {
		return nil
	}
	owned := make(map[string]struct{}, len(unit.PerkIDs))
	for _, id := range unit.PerkIDs {
		owned[id] = struct{}{}
	}
	filtered := make([]*PerkDef, 0, len(pool))
	for _, def := range pool {
		if _, has := owned[def.ID]; has {
			continue
		}
		if def.RequiresPerk != "" && !containsString(unit.PerkIDs, def.RequiresPerk) {
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
			// Visual effect is now driven by EffectSnapshot / queueEffectLocked.
			// No per-tick perk state to decay here.

		case "last_stand":
			// Detect HP threshold crossings to fire the combined armor-bonus +
			// AoE-taunt window. The window fires once per below-threshold entry;
			// the Triggered flag resets when the unit heals back above threshold
			// so the next dip can re-fire. The Remaining timer decays every
			// tick regardless of HP — healing out of the threshold mid-window
			// keeps both the armor bonus and the standing taunts until the
			// timer naturally expires.
			if unit.PerkState.LastStandRemaining > 0 {
				unit.PerkState.LastStandRemaining = math.Max(0, unit.PerkState.LastStandRemaining-dt)
			}
			if unit.MaxHP <= 0 {
				continue
			}
			hpFrac := float64(unit.HP) / float64(unit.MaxHP)
			threshold := def.Config["hpThresholdPercent"]
			if hpFrac > threshold {
				// Above threshold — allow the next dip to re-trigger. The
				// Remaining timer is NOT reset here; an in-flight window
				// keeps running until it decays to 0.
				unit.PerkState.LastStandTriggered = false
			} else if !unit.PerkState.LastStandTriggered {
				// Just crossed below — start the window and fire the taunt.
				duration := def.Config["durationSeconds"]
				unit.PerkState.LastStandTriggered = true
				unit.PerkState.LastStandRemaining = duration
				radius := def.Config["tauntRadius"]
				radiusSq := radius * radius
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

		case "rallying_banner":
			// Plant a banner at the unit's current position once per cooldown
			// window. The banner persists for bannerDurationSeconds even if the
			// Vanguard moves or dies afterward.
			//
			// Cooldown (BannerCooldownRemaining) decays every tick regardless of
			// movement — it persists across moves. StationarySeconds is still used
			// as the plant gate (unit must be stationary long enough), but the
			// cooldown prevents replanting immediately after moving back into
			// position within the same 12s window.
			bannerDef := perkDefByID("rallying_banner")
			if bannerDef == nil {
				continue
			}

			// Cooldown decays every tick regardless of movement.
			unit.PerkState.BannerCooldownRemaining = math.Max(0, unit.PerkState.BannerCooldownRemaining-dt)

			if unit.Moving {
				unit.PerkState.StationarySeconds = 0
			} else {
				unit.PerkState.StationarySeconds += dt
				threshold := bannerDef.Config["stationaryThresholdSeconds"]
				if unit.PerkState.StationarySeconds >= threshold &&
					unit.PerkState.BannerCooldownRemaining <= 0 {
					banner := &Banner{
						ID:               s.nextBannerID,
						OwnerUnitID:      unit.ID,
						OwnerPlayerID:    unit.OwnerID,
						X:                unit.X,
						Y:                unit.Y,
						Radius:           bannerDef.Config["bannerRadius"],
						RemainingSeconds: bannerDef.Config["bannerDurationSeconds"],
						ArmorBonus:       int(bannerDef.Config["bannerArmorBonus"]),
						AttackSpeedBonus: bannerDef.Config["bannerAttackSpeedBonus"],
					}
					s.nextBannerID++
					s.Banners = append(s.Banners, banner)
					unit.PerkState.BannerCooldownRemaining = bannerDef.Config["cooldownSeconds"]
				}
			}

		// ── trapper (archer bronze) ──────────────────────────────────────────
		// All four Bronze trap perks share the same placement timer logic.
		// A unit can own at most one Bronze trap perk, so the shared
		// TrapPlaceCooldownRemaining field has no collision risk.
		// Note: LastCombatSeconds decay is handled in state.go Update() per-unit
		// loop (cross-unit pattern, same as WeakenedRemaining).
		case "caltrops", "fire_pit", "explosive_trap", "marker_trap":
			s.tickTrapPlacementLocked(unit, def, dt)

		case "double_shot":
			// Marksman gold — defer-fire timer. The arming logic lives in
			// onMarksmanAttackFiredLocked; this branch only decrements the
			// timer and fires when it expires. Cleared whether the deferred
			// shot succeeded or not so the next primary attack can re-arm.
			if unit.PerkState.DoubleShotPendingSeconds > 0 {
				unit.PerkState.DoubleShotPendingSeconds = math.Max(0, unit.PerkState.DoubleShotPendingSeconds-dt)
				if unit.PerkState.DoubleShotPendingSeconds == 0 && unit.PerkState.DoubleShotPendingTargetID != 0 {
					s.fireDeferredDoubleShotLocked(unit)
					unit.PerkState.DoubleShotPendingTargetID = 0
				}
			}

		// ── add cases for new perks with timer/decay needs below this line ──
		}
	}
}
