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
// │                                                                         │
// │    FILE ORGANIZATION CONVENTION                                         │
// │      Two axes in play. A perk's behaviour gets split across them:       │
// │                                                                         │
// │      1. By HOOK (concern) — shared, cross-cutting files:                │
// │           perks.go          UnitPerkState, assignment, tick dispatch    │
// │           perks_attack.go   attack-speed, on-fire, on-hit, on-kill,     │
// │                             whirlwind, cleave, bonus-damage             │
// │           perks_defense.go  damage application, healing, shields,       │
// │                             armor, on-damage-taken, flat reduction,    │
// │                             max-HP bonus                                │
// │           perks_movement.go perkMoveSpeedMultiplierLocked               │
// │           perks_icons.go    activeBuff/DebuffIconsLocked (HUD),         │
// │                             perkCooldownsLocked                         │
// │           perks_auras.go    perks_crit.go  perks_vision.go              │
// │                                                                         │
// │      2. By PATH — one file per promotion path, holds heavy helpers:    │
// │           perks_cleric.go    Silver + Gold (Bronze is light → shared)   │
// │           perks_marksman.go  every Marksman perk                        │
// │           perks_siphoner.go  every Siphoner perk                        │
// │           perks_trapper.go   Trapper modifier pipeline (trap data       │
// │                              model + lifecycle lives in trap.go)        │
// │                                                                         │
// │      ADDING A NEW PERK                                                  │
// │        - Lightweight perk (only a few case arms in the shared hook      │
// │          functions, no helper state machine): add cases inline in the   │
// │          shared hook files. No path file needed.                        │
// │        - Heavy perk (helper functions, state machines, multi-helper     │
// │          stamps, novel pipelines like the cleric heal funnel): put the  │
// │          helpers in perks_<path>.go. If the file doesn't exist yet,     │
// │          create it. The shared hook files still dispatch case arms      │
// │          to this file's helpers.                                        │
// │                                                                         │
// │      WHY THE SPLIT — hook files keep cross-cutting concerns (every       │
// │      attack-speed source in one place) easy to scan; path files keep    │
// │      a path's authoring story (the helpers and state for cleric heal    │
// │      routing, say) co-located. Forcing everything onto one axis loses   │
// │      one of these benefits.                                             │
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
// │         onPerkAbilityResolvedLocked          per resolved ability target│
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
// │    6. Cross-unit buffs/debuffs (WeakenedRemaining, MarkedRemaining,     │
// │       BattlePrayerRemaining) decay in state.go Update() alongside       │
// │       TauntRemaining — not in tickUnitPerkStateLocked — because they    │
// │       live on units that may not own the perk themselves (e.g. a        │
// │       Soldier carrying Battle Prayer's attack-speed buff from a Cleric).│
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
//   • ability_cast.go    resolveAbilityCastOnTargetLocked — onPerkAbilityResolvedLocked
//                                                          (per ability target; battle_prayer
//                                                          uses this to stamp the cross-unit buff)
//   • perks.go           assignUnitPerkLocked   — applyPerkGrantedHooksLocked (post-grant side-effects;
//                                                  currently no consumers — kept as extension seam)
//   • debug_spawn.go     DebugSpawnUnit         — applyPerkGrantedHooksLocked (per appended perk;
//                                                  forwards to the same seam)
//   • path_ability_defs.go assignUnitPathAbilitiesLocked — derives unit.Abilities from path-level
//                                                  override (path JSON's "abilities" field) plus any
//                                                  rank-specific grants; called on every promotion
//                                                  and from DebugSpawnUnit.
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
	SourceID    string
	OwnerUnitID int
	Remaining   float64
	DPS         float64
	Accumulator float64
	// ReactiveAccumulator gates this stack's Reactive Flames AoE on a 1-second
	// cadence, separate from the burn's per-chunk damage stream. Without it
	// the AoE would compound with burn-DPS rank scaling — same hidden
	// double-scaling issue the in-zone fire_pit Infusion has.
	ReactiveAccumulator float64
	ReactiveRadius      float64
	ReactiveDamage      int
	// TrapKey groups stacks that share a parent trap so the maxDebuffStacks
	// cap is applied PER TRAP rather than globally per victim. lasting_flames
	// and Flame Collapse from the same fire_pit share a TrapKey (the
	// trap.ID); a second fire_pit's stacks have a different TrapKey and
	// don't count toward the first trap's cap. Empty string = ungrouped
	// (legacy / hypothetical non-trap sources).
	TrapKey string
	// SourceKind tags where the burn came from so the burn tick attributes
	// damage/telemetry correctly. burnSourceTrap (the default) credits the
	// parent trap via AttackerTrapID + a "fire_pit" battle source (fire_pit
	// lasting_flames / Flame Collapse). burnSourceWeapon credits the wielding
	// unit directly via AttackerUnitID + battleSourceFromUnit (fire_sword
	// proc, etc.) — a sword burn must not masquerade as a trap in kill stats.
	SourceKind burnSource
}

// burnSource identifies the origin of a burn stack for attribution in the burn
// DoT tick (see UnitPerkState.BurnStacks and tickTrapperSilverDebuffsLocked).
type burnSource string

const (
	burnSourceTrap   burnSource = ""       // fire_pit lasting_flames / Flame Collapse (default)
	burnSourceWeapon burnSource = "weapon" // equipment on-hit proc (fire_sword)
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

	// ── per-tick predicate cache ──────────────────────────────────────────────
	// These flags are refreshed once per tick by recomputePerkPredicateCache-
	// Locked. They replace the O(N) full-unit scans (countEnemiesInRangeLocked,
	// hasAllyInRangeLocked) that were being called once per unit per snapshot
	// per player from BOTH effectiveArmorLocked (perkBonusArmorLocked) AND
	// activeBuffIconsLocked — duplicating the work. Cached value reflects the
	// unit's state at end of the previous Update tick; in-tick damage paths
	// that need fresh data can still call the underlying helpers, but the hot
	// snapshot path reads only this cache.
	BraceActive     bool
	InterlockActive bool

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

	// TrapInfusionAccumulator drives Reactive Flames cadence (fire_pit zone).
	// Per zone-iteration: accumulator += reactiveFlamesDamage × dt; on
	// overflow we fire one AoE chunk and carry the fraction. Result: the
	// authored reactiveFlamesDamage equals the total reactive DPS regardless
	// of host trap rank — gold-only perks shouldn't compound with rank.
	TrapInfusionAccumulator float64

	// ElectrifiedBonusAccumulator drives Electrified Caltrops bonus-damage
	// cadence (caltrops zone). Same dt-timer pattern as TrapInfusionAccumulator
	// but keyed off electrifiedBonusDamagePerTick so total bonus DPS is the
	// authored value regardless of host caltrops DPS or amplified_effects
	// scaling. The stun roll piggybacks on this chunk firing — they share
	// the same cadence so stuns happen "alongside the damage popup".
	ElectrifiedBonusAccumulator float64

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
	// victim leaves the trap's zone (handled in tickTrapEffectsLocked), the
	// burst is detonated via fireFinalExposureLocked which damages the victim
	// AND every other unit currently carrying a mark stack from the same
	// trap. FinalExposureTrapID is the key used to match marked siblings.
	FinalExposureDamage      int
	FinalExposureOwnerUnitID int
	FinalExposureTrapID      string

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

	// ── cleric heal-buff perks (bronze) ──────────────────────────────────────
	// Cross-unit buffs applied to every target a Cleric heals when the Cleric
	// owns the corresponding perk. The buff lives on the HEALED TARGET's
	// PerkState (not the Cleric's), matching the cross-unit debuff convention
	// for WeakenedRemaining / TauntRemaining. Decays in state.go Update() per-
	// unit loop, not in tickUnitPerkStateLocked, because the buffed unit may
	// not own the originating perk itself.
	//
	// Refresh semantics for both: refresh-longer (max of current vs new
	// duration) and refresh-stronger (max of current vs new bonus value) —
	// same as the existing mark-stack refresh rules. The two buffs are
	// independent fields: a unit healed by two Clerics (one of each flavor)
	// gains both simultaneously.

	// battle_prayer — temporary attack-speed bonus.
	//   BattlePrayerRemaining:  seconds left on the buff. 0 = inactive.
	//   BattlePrayerMultiplier: attack-speed fraction (e.g. 0.25 = +25%).
	//     Set to Config["attackSpeedMultiplier"] on application; carried on
	//     the buff so the value travels with it independent of perk re-tuning.
	BattlePrayerRemaining  float64
	BattlePrayerMultiplier float64

	// bolstering_prayer — temporary flat armor bonus.
	//   BolsteringPrayerRemaining: seconds left on the buff. 0 = inactive.
	//   BolsteringPrayerArmor:     flat armor bonus while the buff is active.
	//     Set to Config["armorBonus"] on application; carried on the buff so
	//     the value travels with it independent of perk re-tuning. Stored as
	//     float64 to mirror the BattlePrayer pair; rounded to int in
	//     effectiveArmorLocked.
	BolsteringPrayerRemaining float64
	BolsteringPrayerArmor     float64

	// ── cleric silver perks ─────────────────────────────────────────────────
	// divine_aegis splits state across two roles:
	//   • Owner-side (Cleric with the perk):
	//     DivineAegisPulseRemaining — seconds until the next aura pulse. Set
	//     to intervalSeconds whenever a pulse fires; decays in
	//     tickUnitPerkStateLocked. The initial value is 0 so the first pulse
	//     fires immediately on grant — by design, allies under a freshly-
	//     summoned cleric should not have to wait the full interval for
	//     their first shield.
	//   • Recipient-side (any ally inside a pulse radius):
	//     DivineAegisRemaining — seconds left on the protection charge. Set
	//     to protectionDurationSeconds when a pulse stamps the recipient;
	//     decays alongside the other cross-unit buff timers in state.go
	//     Update(). Consumed (set to 0) the moment any damage instance
	//     lands on the recipient — the damage pipeline checks this field
	//     before mark amplification / sanctuary / shield / HP, and a non-
	//     zero charge zeroes the incoming damage. Refresh semantics are
	//     refresh-longer (max of current vs new duration) so two clerics
	//     do not waste each other's shield by overwriting a stronger one.
	//     This is a single-charge field — overlapping clerics do not grant
	//     two consecutive blocks; the design constraint is one absorption
	//     per pulse.
	DivineAegisPulseRemaining float64
	DivineAegisRemaining      float64

	// ── restoration_aura (silver cleric) — owner pulse only ────────────────
	// RestorationPulseRemaining is the seconds until the next pulse. Set to
	// intervalSeconds whenever a pulse fires; decays in
	// tickUnitPerkStateLocked. Initial value of 0 means the first pulse
	// fires on the first tick after the perk is granted (same pattern as
	// DivineAegisPulseRemaining). The heal itself routes through the
	// healing-event path (recordHealEventLocked) so the floating +N pops on
	// the client, and is scaled by divine_healer when the owner has it.
	RestorationPulseRemaining float64

	// ── cleric gold perks ───────────────────────────────────────────────────
	// divine_intervention is split across owner-side cooldown and recipient-
	// side invulnerability window:
	//   • Owner-side (Cleric with the perk):
	//     DivineInterventionCooldownRemaining — seconds until the cleric may
	//     fire another intervention. Set to cooldownSeconds when an
	//     intervention fires; decays every tick in tickUnitPerkStateLocked.
	//     Zero allows the next save. The cooldown is per-cleric — multiple
	//     clerics with the perk each have their own gate.
	//   • Recipient-side (any saved unit):
	//     InvulnerabilityRemaining — seconds left on the brief protection
	//     window stamped onto the rescued unit. The damage pipeline checks
	//     this AT THE TOP of applyUnitDamageWithSourceLocked and returns 0
	//     immediately if > 0 (no mitigation, no shared pain, no death). This
	//     is true invulnerability rather than a damage-instance absorb so
	//     burst follow-up cannot kill a freshly-saved unit. Decays alongside
	//     the other cross-unit buff timers in state.go Update(). Future
	//     perks that grant temporary invulnerability can reuse this field.
	DivineInterventionCooldownRemaining float64
	InvulnerabilityRemaining            float64

	// beacon_of_life and divine_judgement do not need persistent per-unit
	// state — beacon's "no recursive splash" rule and judgement's "no
	// recursive damage" rule are both enforced via the HealMeta flags
	// threaded through applyClericHealLocked (see perks_cleric_gold.go).
	// Per-unit recursion guards would be redundant with the metadata flags
	// and would risk leaving stale `true` values if a panic/return skipped
	// the reset path.

	// ── Siphoner bronze perks ───────────────────────────────────────────────
	// All four Siphoner Bronze perks layer onto the existing channel /
	// ability / damage / heal pipelines. Soul Leech is stateless (read-only
	// multiplier in the channel tick) so it has no PerkState fields.
	//
	// Withering Beam splits state across two roles:
	//   • Caster-side (Siphoner owning the perk):
	//       WitheringBeamChannelTargetID — id of the unit the caster has been
	//         continuously siphoning. Reset to 0 when the channel ends or the
	//         target changes; the accumulator below resets at the same time.
	//       WitheringBeamChannelAccum — seconds of continuous siphon since the
	//         last stack tick on the current target. Every `secondsPerStack`
	//         it crosses the threshold the helper stamps one more stack on
	//         the current target and subtracts the threshold.
	//   • Target-side (any enemy carrying live stacks):
	//       WitheringBeamStacks — current stack count (0..maxStacks).
	//       WitheringBeamRemaining — seconds until the entire stack list
	//         expires. Refreshed to `lingerSeconds` every time a new stack
	//         lands; decays in the cross-unit loop in state.go alongside
	//         WeakenedRemaining. When 0, WitheringBeamStacks and
	//         WitheringBeamReductionPerStack are also cleared.
	//       WitheringBeamReductionPerStack — outgoing-damage reduction
	//         contributed by each stack. Carried with the affliction (set
	//         at stamp time) so re-tuning the perk later doesn't retroact-
	//         ively change live debuffs on units in flight.
	WitheringBeamChannelTargetID  int
	WitheringBeamChannelAccum     float64
	WitheringBeamStacks           int
	WitheringBeamRemaining        float64
	WitheringBeamReductionPerStack float64

	// Lingering Hex (autonomous AoE slow). The state splits across two
	// roles:
	//
	//   • Owner-side (Siphoner with the perk):
	//       LingeringHexCooldownRemaining — seconds until the perk may fire
	//         again. Set to cooldownSeconds when the AoE pulses; decays in
	//         tickUnitPerkStateLocked via tickLingeringHexPerkLocked. Zero
	//         means "armed and waiting for an anchor enemy".
	//
	//   • Recipient-side (any hexed enemy):
	//       LingeringHexRemaining — seconds left on the affliction. Decays
	//         in the state.go cross-unit loop alongside WeakenedRemaining.
	//       LingeringHexMoveMult / LingeringHexAttackSpeedMult — the two
	//         multipliers carried with the affliction. Authored with
	//         different defaults (0.75 move vs 0.80 attack) and consumed
	//         by independent stat hooks (movement step + attack cadence).
	//         Cleared when Remaining hits 0.
	LingeringHexCooldownRemaining float64
	LingeringHexRemaining         float64
	LingeringHexMoveMult          float64
	LingeringHexAttackSpeedMult   float64

	// Mark of Weakness (autonomous AoE armor + healing-received debuff).
	// Owner-side cooldown / recipient-side affliction split mirrors
	// Lingering Hex above. ArmorReduction subtracts from effectiveArmor-
	// Locked while Remaining > 0; HealingReceivedMult scales every
	// incoming heal applied via healUnitLocked / applyClericHealLocked.
	// Both affliction values are cleared in the state.go cross-unit
	// decay when Remaining reaches 0.
	MarkOfWeaknessCooldownRemaining   float64
	MarkOfWeaknessRemaining           float64
	MarkOfWeaknessArmorReduction      int
	MarkOfWeaknessHealingReceivedMult float64

	// ── Siphoner silver perks ───────────────────────────────────────────────
	// chain_siphon — damage / heal are stateless (resolved fresh each tick),
	// but the SECONDARY BEAM VISUALS need to persist across ticks to avoid
	// 4-fps flicker at the 0.25s channel cadence. The chain BOUNCES — each
	// secondary beam starts from the previous chain victim, not the primary
	// (x — x — x, not x ⟵ x x). The tracking is therefore ORDERED: the
	// slice index is the bounce position, and beam visuals are anchored to
	// (chain[i-1].unit, chain[i].unit) where chain[-1] is the primary target.
	//
	// The per-tick sync compares the freshly-bounced chain against the
	// recorded chain prefix-wise: every leading entry whose TargetID still
	// matches is reused (no respawn → no flicker), and the first divergence
	// despawns that beam and every beam after it (the tail's source units
	// have shifted, so those beams would now be drawn from the wrong place).
	//
	// ChainSiphonPrimaryTargetID remembers WHO the bouncing chain currently
	// roots from. When the Siphoner swaps primary target, every link roots
	// from a stale chain and is rebuilt from scratch. Both fields zero out
	// when the channel ends, via clearChainSiphonBeamsLocked called from
	// clearChannelStateLocked.
	ChainSiphonPrimaryTargetID int
	ChainSiphonLinks           []ChainSiphonLink

	// amplify_damage — autonomous AoE affliction with the same shape as
	// Lingering Hex / Mark of Weakness (owner-side cooldown + recipient-side
	// affliction). The damage-taken multiplier is read by the damage pipeline
	// to amplify every incoming damage instance. Refresh-longer for
	// duration, refresh-stronger (max-wins) for the multiplier.
	AmplifyDamageCooldownRemaining float64
	AmplifyDamageRemaining         float64
	AmplifyDamageMultiplier        float64

	// shared_suffering — recursion guard. Set true on the caster's PerkState
	// while a shared-damage echo is being applied so a victim with their
	// own future on-damage perk cannot chain-trigger another echo. The
	// echo damage is also tagged DamageSource.Kind = "shared_suffering"
	// for telemetry / debug filtering — see applySharedSufferingLocked.
	SharedSufferingActive bool

	// ── Source-specific shield pools ────────────────────────────────────────
	// A unit can carry multiple independent shield pools at the same time
	// (e.g. dark_renewal + a future cleric barrier perk). Each pool tracks
	// its own current value, max value, and source identity. The damage
	// pipeline drains pools oldest-first (slice order) before falling
	// through to the legacy Unit.Shield (blood_engine) and finally HP.
	//
	// Pools persist until depleted unless a future source defines its own
	// expiration (no decay path today). Dark Renewal pools never expire.
	//
	// Tags are intentionally free-form ("corruption", "siphoner", …) so
	// future perks that want to interact with shield categories (e.g. a
	// debuff that strips shadow-tagged shields) can filter without
	// hard-coding source ids.
	ShieldPools []ShieldPool
}

// ShieldPool is a single source-specific shield pool carried on
// UnitPerkState. Add new pool types by allocating one (via
// applyShieldFromSourceLocked) and the damage pipeline picks them up
// automatically — pools are agnostic to their producer.
//
// Fields:
//
//	SourceType   — namespacing tag, e.g. "dark_renewal". Used for keying
//	               so re-applying from the same producer tops up the
//	               existing pool rather than allocating a new one.
//	SourceUnitID — unit that granted this pool (Siphoner id for
//	               dark_renewal). Carried for telemetry / future
//	               source-aware interactions; not used for stacking.
//	CurrentValue — current shield value, drained by the damage pipeline.
//	MaxValue     — per-source cap. Top-ups beyond this are wasted (the
//	               total "displayed shield" on the unit is the sum across
//	               every pool, but each pool respects its own cap).
//	Tags         — free-form category tags. Not consumed by the damage
//	               pipeline today; reserved for future perk interactions
//	               (e.g. shield-strip mechanics that filter by tag).
type ShieldPool struct {
	SourceType   string
	SourceUnitID int
	CurrentValue int
	MaxValue     int
	Tags         []string
}

// ChainSiphonLink is one position in the bouncing Siphon Life chain. Slice
// index is the bounce position (0 = first hop off the primary target). Each
// beam animates from the unit one position earlier in the chain (or the
// primary for index 0) to the unit referenced by TargetID. BeamID is the
// stable wire id assigned by spawnBeamLocked so the sync helper can drop a
// specific link without disturbing earlier or later beams.
type ChainSiphonLink struct {
	TargetID int
	BeamID   string
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
// land as separate stacks when their zones overlap. trapKey groups stacks
// for cap purposes — the maxDebuffStacks ceiling applies per-trapKey rather
// than globally, so two different fire_pits each get their own pair of
// stacks (lasting_flames + Flame Collapse) instead of fighting over the cap.
// Pass "" for trapKey to keep legacy global cap semantics.
func (ps *UnitPerkState) applyBurnStack(sourceID string, trapKey string, ownerUnitID int, dps, duration, reactiveRadius float64, reactiveDamage int, sourceKind burnSource) bool {
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
			ps.BurnStacks[i].TrapKey = trapKey
			ps.BurnStacks[i].SourceKind = sourceKind
			return true
		}
	}
	// Per-trapKey cap. Stacks with a different TrapKey (or empty key) don't
	// count toward this group's ceiling, so a second trap can fully populate
	// its own pair regardless of how full the victim's overall list is.
	count := 0
	for i := range ps.BurnStacks {
		if ps.BurnStacks[i].TrapKey == trapKey {
			count++
		}
	}
	if count >= maxDebuffStacks {
		return false
	}
	ps.BurnStacks = append(ps.BurnStacks, burnStack{
		SourceID:       sourceID,
		OwnerUnitID:    ownerUnitID,
		Remaining:      duration,
		DPS:            dps,
		ReactiveRadius: reactiveRadius,
		ReactiveDamage: reactiveDamage,
		TrapKey:        trapKey,
		SourceKind:     sourceKind,
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
	perkID := pool[s.rngPerks.Intn(len(pool))].ID
	unit.PerkIDs = append(unit.PerkIDs, perkID)
	s.applyPerkGrantedHooksLocked(unit, perkID)

	// Extra-slot pass (Twin Bronze and any future unitExtraPerkSlot advancement).
	// Draws from the same rngPerks stream immediately after the primary pick so
	// replay determinism is preserved. The pool re-query automatically excludes
	// the perk just appended via the "already owned" filter in
	// eligiblePerksAfterFiltersLocked.
	s.maybeAssignExtraPerkLocked(unit)
}

// maybeAssignExtraPerkLocked appends one additional perk to unit.PerkIDs when
// the owning player holds a unitExtraPerkSlot advancement for this unit type
// at the unit's current rank tier. Silently no-ops when the owner is absent,
// the advancement isn't owned, or the post-dedup pool is empty. Caller holds
// s.mu.
func (s *GameState) maybeAssignExtraPerkLocked(unit *Unit) {
	if unit == nil {
		return
	}
	player, ok := s.Players[unit.OwnerID]
	if !ok || player.ExtraPerkSlots == nil {
		return
	}
	tiers, hasUnit := player.ExtraPerkSlots[unit.UnitType]
	if !hasUnit || !tiers[unit.Rank] {
		return
	}
	pool := s.perkPoolForRankLocked(unit, unit.Rank)
	if len(pool) == 0 {
		return // bronze pool exhausted by RequiresPerk filters or pool size 1
	}
	perkID := pool[s.rngPerks.Intn(len(pool))].ID
	unit.PerkIDs = append(unit.PerkIDs, perkID)
	s.applyPerkGrantedHooksLocked(unit, perkID)
}

// applyPerkGrantedHooksLocked runs the post-grant side-effects for one perk id
// that was just appended to unit.PerkIDs. Centralised so every path that adds
// a perk (rank-up roll, debug spawn, future scripted grants) picks up the same
// behaviour — most perks are runtime-only and don't need a hook here, but
// future ability-replacing perks may need to mutate the unit's kit at grant
// time and MUST fire regardless of how the perk got onto the unit.
//
// Currently no perk consumes this seam — the heal → greater_heal swap that
// used to live here moved to assignUnitPathAbilitiesLocked when Greater Heal
// became a Cleric path baseline rather than a perk. The function is kept as
// the documented extension point for future ability-replacing perks.
//
// Caller holds s.mu.
func (s *GameState) applyPerkGrantedHooksLocked(unit *Unit, perkID string) {
	if unit == nil {
		return
	}
	_ = perkID // no per-perk hooks currently; switch case added here when needed
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

		case "divine_aegis":
			// Silver cleric: pulse a one-hit protection charge onto nearby
			// allies on a fixed cadence. Lives in perks_cleric_silver.go to
			// keep the silver cleric perks colocated.
			s.tickDivineAegisPulseLocked(unit, def, dt)

		case "restoration_aura":
			// Silver cleric: pulse a small heal to every nearby ally on a
			// fixed cadence. Routes through applyClericHealLocked so gold
			// triggers (divine_judgement) can fire on every pulse.
			s.tickRestorationAuraPulseLocked(unit, def, dt)

		case "divine_intervention":
			// Gold cleric: owner-side cooldown decay. The save itself fires
			// from the damage pipeline (tryDivineInterventionLocked) when an
			// allied unit's HP would hit 0; this branch only ticks the
			// cooldown down so the perk re-arms.
			if unit.PerkState.DivineInterventionCooldownRemaining > 0 {
				unit.PerkState.DivineInterventionCooldownRemaining = math.Max(0, unit.PerkState.DivineInterventionCooldownRemaining-dt)
			}

		// ── siphoner bronze autonomous-fire perks ────────────────────────────
		// Lingering Hex and Mark of Weakness pulse their AoE on a per-unit
		// cooldown, similar in spirit to the Trapper's auto-trap placement.
		// The handlers gate on cooldown, mana, and a valid anchor enemy
		// (preferring the Siphoner's current channel target, else nearest
		// hostile in castRange). No cast time, no projectile — purely an
		// affliction stamp + cooldown reset on success.
		case "lingering_hex":
			s.tickLingeringHexPerkLocked(unit, def, dt)

		case "mark_of_weakness":
			s.tickMarkOfWeaknessPerkLocked(unit, def, dt)

		case "amplify_damage":
			// Siphoner silver: same autonomous AoE pattern as the bronze
			// affliction perks above — gated on cooldown + anchor enemy
			// (current channel target preferred). No mana cost per the perk
			// spec. The stamped damage-taken multiplier is read by the
			// damage pipeline via amplifyDamageTakenMultiplierLocked.
			s.tickAmplifyDamagePerkLocked(unit, def, dt)

		// mana_conduit intentionally has NO case here. It used to be a
		// self-only per-tick regen, but it's now an aura — every mana
		// regen tick (tickUnitManaRegenLocked in mana.go) folds in the
		// recipient-queried aura bonus via manaConduitAuraBonusLocked, so
		// the cleric and any covered ally automatically benefit. Keeping
		// the perk out of this dispatch ensures the bonus isn't double-
		// applied (per-tick AND via regen).
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook: on ability resolved (per resolved target)
// ─────────────────────────────────────────────────────────────────────────────

// onPerkAbilityResolvedLocked is called once per (caster, target) pair
// immediately after an ability effect has been applied in
// resolveAbilityCastLocked. It fires perk side-effects that are gated on
// ability resolution rather than on the attack cycle.
//
// Cleric heal-buff perks (battle_prayer, bolstering_prayer): if the caster
// owns the perk and the ability is heal-category, stamp the corresponding
// buff onto the target using refresh-max semantics. Each (remaining, bonus)
// pair is set independently to max(current, configured value), so a re-cast
// on an already-buffed target extends the duration without ever reducing it
// and never weakens an existing stronger buff.
//
// Both buffs live on target.PerkState (cross-unit, same as WeakenedRemaining)
// so the consuming sites (perkAttackSpeedBonusLocked, effectiveArmorLocked)
// can read them even when the target does not own the originating perk itself.
//
// Caller holds s.mu.
func (s *GameState) onPerkAbilityResolvedLocked(caster *Unit, def AbilityDef, target *Unit) {
	if caster == nil || target == nil {
		return
	}
	if def.Category != AbilityCategoryHeal {
		return
	}
	// divine_healer (silver cleric) scales every heal-triggered buff's
	// strength and duration. Returns 1.0 when the caster lacks the perk so
	// the multiply is inert in the common case. Disjoint from the heal-
	// AMOUNT multiplier (which scales raw HP restored) so the two effects
	// can be tuned independently in the perk JSON.
	triggerMult := s.perkClericHealTriggeredMultiplierLocked(caster)
	for _, perkID := range caster.PerkIDs {
		switch perkID {
		case "battle_prayer":
			pDef := perkDefByID("battle_prayer")
			if pDef == nil {
				continue
			}
			cfg := pDef.ConfigForRank(caster.Rank)
			duration := cfg["buffDurationSeconds"] * triggerMult
			mult := cfg["attackSpeedMultiplier"] * triggerMult
			if duration <= 0 || mult <= 0 {
				continue
			}
			if duration > target.PerkState.BattlePrayerRemaining {
				target.PerkState.BattlePrayerRemaining = duration
			}
			if mult > target.PerkState.BattlePrayerMultiplier {
				target.PerkState.BattlePrayerMultiplier = mult
			}

		case "bolstering_prayer":
			pDef := perkDefByID("bolstering_prayer")
			if pDef == nil {
				continue
			}
			cfg := pDef.ConfigForRank(caster.Rank)
			duration := cfg["buffDurationSeconds"] * triggerMult
			armor := cfg["armorBonus"] * triggerMult
			if duration <= 0 || armor <= 0 {
				continue
			}
			if duration > target.PerkState.BolsteringPrayerRemaining {
				target.PerkState.BolsteringPrayerRemaining = duration
			}
			if armor > target.PerkState.BolsteringPrayerArmor {
				target.PerkState.BolsteringPrayerArmor = armor
			}
		}
	}
}
