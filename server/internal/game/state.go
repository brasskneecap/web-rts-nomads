package game

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"log/slog"
	"math"
	mrand "math/rand"
	"sort"
	"sync"
	"time"
	"webrts/server/pkg/protocol"
)

// OrderType is the player's standing order for a unit. It is the single source
// of truth for "what is this unit doing from the player's intent perspective".
// The zero value is OrderIdle so freshly-allocated Units are always valid.
//
// queue-ready: this becomes []OrderState for shift-queue in a future design.
type OrderType int

const (
	OrderIdle         OrderType = iota // no standing order; default acquisition
	OrderMove                          // force-move: ignore enemies en route
	OrderAttackMove                    // a-move: break off to engage acquired enemies
	OrderAttackTarget                  // sticky attack on AttackTargetID/AttackBuildingTargetID
	OrderHold                          // do not move; engage in-range enemies only
	OrderPatrol                        // cycle waypoints; engage acquired enemies; resume
	// OrderFocusFollow: sticky support assignment. The unit follows FocusTargetID
	// (a same-team ally, stored by ID per the project ID-not-pointer rule) and
	// prioritises healing that ally. Cleared whenever any other order replaces it;
	// the clear path always zeroes FocusTargetID alongside the order so the two
	// never diverge. Use RequestSetFocusTargetLocked / clearFocusTargetLocked to
	// set/clear; never mutate FocusTargetID or this order type directly.
	OrderFocusFollow
	// OrderPickupLoot: walk to a treasure chest and collect it. Sticky
	// unit field PickupLootID is stored by ID per AI_RULES; the
	// tickLootDropsLocked path (Batch 4) resolves and validates each
	// tick. Combat AI does NOT engage on the way (matches OrderMove
	// semantics — the player wants the chest, not a fight).
	OrderPickupLoot
	// OrderGuard: hold a commanded position, engage hostiles that enter the
	// unit's guard-aggro radius, then return to the post when the fight ends.
	// This is the player-facing face of the GuardMode machinery already used by
	// neutral camps / placed enemies: GuardUnits sets GuardMode=true with the
	// anchor at the commanded position, and the existing combat AI
	// (selectBestTargetLocked aggro, shouldDropCurrentTargetLocked leash,
	// tickGuardReturnLocked return-to-anchor) drives the behavior. GuardMode is
	// cleared by resetUnitMovementLocked the moment any other order is issued.
	OrderGuard
)

// orderTypeString returns the wire-format string for OrderType, matching the
// constants defined in protocol/messages.go. Used by Snapshot.
func orderTypeString(t OrderType) string {
	switch t {
	case OrderMove:
		return protocol.OrderStringMove
	case OrderAttackMove:
		return protocol.OrderStringAttackMove
	case OrderAttackTarget:
		return protocol.OrderStringAttackTarget
	case OrderHold:
		return protocol.OrderStringHold
	case OrderPatrol:
		return protocol.OrderStringPatrol
	case OrderFocusFollow:
		return protocol.OrderStringFocusFollow
	case OrderPickupLoot:
		return protocol.OrderStringPickupLoot
	case OrderGuard:
		return protocol.OrderStringGuard
	default:
		return protocol.OrderStringIdle
	}
}

// OrderState holds the player's standing order for a unit.
// queue-ready: this becomes []OrderState for shift-queue.
type OrderState struct {
	Type OrderType
	// Destination for Move / AttackMove. Re-used as patrol "current waypoint".
	DestX, DestY float64
	// Patrol-only: the OTHER endpoint of the patrol leg.
	// PatrolWaypoints can grow to a []Vec2 later if N-point patrol is wanted.
	PatrolReturnX, PatrolReturnY float64
	// For Hold: the position where the unit was ordered to hold.
	// For AttackMove/Patrol: the anchor returned to when combat ends.
	HoldX, HoldY float64
}

type Unit struct {
	ID           int
	OwnerID      string
	Color        string
	UnitType     string
	Archetype    string
	Name         string
	Capabilities []string
	Visible      bool
	Status       string
	X            float64
	Y            float64
	// Dead marks a CORPSE: the unit died, was torn down (see
	// tearDownDeadUnitLocked) and is lingering on the field until it decays.
	// It is still in s.Units and still resolvable by ID — that is the point,
	// so a body can be raised or revived — but it is not alive. Never test
	// this field directly; ask unitIsAliveLocked, which is the single
	// definition every attached-to-a-unit system shares.
	// See docs/design/death_and_corpses.md.
	Dead bool
	// CorpseRemaining counts a corpse down to decay, in seconds. Meaningless
	// (and zero) while the unit is alive.
	CorpseRemaining float64
	HP              int
	MaxHP        int
	BaseMaxHP    int
	// HealthRegenPerSecond is the baseline passive HP regeneration rate (HP per
	// real-time second) applied in Update(). Defaults to defaultHealthRegenPerSecond
	// on spawn; future perks / buffs can modify it. HealthRegenAccumulator carries
	// fractional progress between ticks so a sub-1 HP/s rate still heals integer
	// HP on the correct cadence.
	HealthRegenPerSecond float64
	// BaseHealthRegenPerSecond is the unit's regen BEFORE path/rank multipliers
	// and equipment bonuses — the Base* counterpart that lets
	// applyRankModifiersLocked recompute HealthRegenPerSecond from scratch, the
	// same way BaseMaxHP → MaxHP and BaseDamage → Damage work. Seeded at spawn
	// from UnitDef.healthRegenRate (else the global default) and never mutated
	// afterwards.
	BaseHealthRegenPerSecond float64
	HealthRegenAccumulator   float64
	// Mana is an optional resource (spellcasters). All four fields default to
	// 0, which means "this unit has no mana" — MaxMana == 0 makes the regen
	// loop skip the unit entirely (see tickUnitManaRegenLocked in mana.go).
	// MaxMana/CurrentMana are integer (like HP); ManaRegenAccumulator carries
	// fractional regen between ticks so sub-1 mana/s rates still restore
	// integer mana on the right cadence (mirrors the HealthRegen pair above).
	// Named *PerSecond for consistency with HealthRegenPerSecond (the JSON /
	// UnitDef field this is loaded from is "manaRegenRate", wired in Part 7).
	MaxMana              int
	CurrentMana          int
	ManaRegenPerSecond   float64
	ManaRegenAccumulator float64
	// ArcaneCharge accumulates as a unit with the arcane_missiles charge-fire
	// passive spends mana (arch-mage-spell-system). At the passive's
	// ChargeRequired it auto-fires missiles and resets (see spell_charge.go).
	// 0 for every unit without the passive.
	ArcaneCharge        float64
	BaseDamage          int
	BaseArmor           int
	BaseAttackSpeed     float64
	BaseMoveSpeed       float64
	// BaseStats carries per-unit-type base values for registered stats that have
	// no typed field above (critChance, critMultiplier, …). Seeded at spawn from
	// UnitDef.BaseStats; read via unitBaseStat (stat_modifiers.go). Sparse and
	// usually nil — a unit that authors none behaves on the stat's global
	// default. Extends the "unit carries a base for any registered stat" model.
	BaseStats           map[string]float64
	// AbilityStats carries this unit's broad, kind-targeted ability modifiers,
	// copied from UnitDef.AbilityStats at spawn (so an advancement that raised
	// one flows through the same seam). Read via abilityStatFoldLocked; usually
	// nil. See ability_stats.go.
	AbilityStats        map[string]AbilityStatMod
	// AbilityFields carries this unit's precise per-action ability modifiers,
	// copied from UnitDef.AbilityFields at spawn. See ability_field_mods.go.
	AbilityFields       []AbilityFieldModifier
	XP                  int
	XPValue             int // raw XP yielded when killed in "split" mode; seeded at spawn
	XPProgressRemainder float64
	Rank                string
	RankUpFxRemaining   float64
	ProgressionPath     string
	Armor               int
	// PathDodgeChance / PathBlockChance are the progression path's per-rank
	// additive evasion contributions, assigned by applyRankModifiersLocked
	// (zero for pathless units). Combined with the game-wide base and the
	// equipment bonus at read time by evasionForUnit — mirroring how Armor
	// is path-assigned here and equipment-extended in effectiveArmorLocked.
	PathDodgeChance float64
	PathBlockChance float64
	PerkIDs         []string // assigned perk ids, in rank-up order. Length is typically
	// 3 (one per tier). Length 4 indicates the owner had a
	// unitExtraPerkSlot advancement granting a second pick at
	// the same rank as one of the existing tiers (see advancement
	// handler "unitExtraPerkSlot" and assignUnitPerkLocked).
	PerkState UnitPerkState // runtime state shared across the unit's perks

	// Shield is a temporary HP pool consumed before HP by applyUnitDamageLocked.
	// First-pass implementation: only granted by blood_engine (gold berserker)
	// via overheal conversion; no decay — persists until consumed. Extend here
	// if future perks need shield decay or alternate gain mechanics.
	Shield int

	// ── Advancement-granted bonuses (seeded from the effective UnitDef) ──────
	// Set by the Archer "Master Huntsman" advancement; zero on every other
	// unit. See UnitDef.BonusArrows / TrapEffectBonus / TrapRadiusBonus.
	// BonusArrows: extra arrows per attack, fired via the split-shot fan-out.
	BonusArrows int
	// TrapEffectBonus / TrapRadiusBonus: additive fractions folded into the
	// trap modifier pipeline as (1 + bonus) multipliers at plant time.
	TrapEffectBonus float64
	TrapRadiusBonus float64

	// ObjectiveID links this unit to a VictoryCondition. Non-empty when spawned
	// from an enemy-spawnpoint whose metadata["objectiveId"] matches a condition.
	ObjectiveID string

	// IgnoreWaveClear marks this unit as excluded from the wave-completion
	// check in countEnemyUnitsLocked. Set when spawned from an enemy-spawnpoint
	// whose metadata["ignoreWaveClear"] is true (e.g. ambient/background
	// enemies that must not stall wave progression).
	IgnoreWaveClear bool

	// TargetPlayerID is the real player ID this enemy was spawned to attack.
	// Set on wave/enemy-spawnpoint units when the spawn-point's
	// metadata["targetPlayerLabel"] resolved to a joined player. The combat AI's
	// "no current target → pick nearest building" fallback honors this so the
	// enemy keeps heading toward the assigned player even when another player's
	// base is geographically closer. Empty string = no preference, fall back to
	// nearest player building (legacy behavior).
	TargetPlayerID string

	// NeutralCampID, when non-empty, links this unit to a NeutralCamp.
	// Empty for all non-neutral units. Set at spawn by
	// spawnGroupForCampLocked (Batch D); consumed by:
	//   1. the group-aggro broadcast in combat AI (one camp-mate spotting a
	//      player target triggers the rest of the camp to engage; Batch F).
	//   2. removeUnitLocked, which calls onUnitRemovedFromCampLocked to keep
	//      camp.AliveUnitIDs in sync (Batch E).
	NeutralCampID string

	CarriedResourceType string
	CarriedAmount       int
	GatherTargetID      string
	GatherBuildingType  string
	ReturnTargetID      string
	MiningInside        bool
	MiningRemaining     float64
	Gathering           bool
	Returning           bool
	BuildTargetID       string
	Building            bool
	// InsideBuilder marks the unit as the single occupant of an under-construction
	// building footprint. Implies Visible=false and Status="Building" while true.
	// Mirrors MiningInside for goldmine workers.
	InsideBuilder bool
	// RepairChargeAccumulator is the HP this worker has contributed to the
	// current build/repair target since their last 1g+1w charge. Crosses 5 → deduct
	// from owner and reset to 0. Cleared on resetUnitMovementLocked and on
	// build-target completion. Inside builder during construction does not
	// consume this — they build for free.
	RepairChargeAccumulator float64
	TargetX                 float64
	TargetY                 float64
	Moving                  bool
	Path                    []protocol.Vec2
	OrderID                 int64

	// Stuck-progress sample. Position recorded at the start of the most
	// recent watchdog window, plus elapsed seconds in that window. If the
	// unit hasn't displaced at least sqrt(stuckProgressThresholdSq) pixels
	// by the end of stuckSampleInterval seconds, the per-tick movement loop
	// forces a repath to break oscillation loops (two units wedging each
	// other, separation pushing a unit back into a blocked cell, etc.).
	// Reset on every assignUnitPath / resetUnitMovementLocked.
	StuckSampleX     float64
	StuckSampleY     float64
	StuckSampleAccum float64

	// Repath-blocked retry state. A moving unit whose forced repath finds no
	// route (transient crowd, an obstacle dropped into its path, or the fine
	// A* node-budget cutoff) does NOT abandon its order. Instead RepathBlocked
	// is set and it holds the order, retrying pathing every
	// repathBlockedRetryInterval seconds for up to repathBlockedGiveUpSeconds
	// before finally stopping. RepathBlockedAccum measures seconds elapsed in
	// that state and drives both the retry cadence and the give-up deadline.
	// Cleared on every successful assignUnitPath and on resetUnitMovementLocked.
	// This is what lets a unit that snags on a building/tree corner redirect
	// itself once the way clears, instead of sitting wedged forever.
	RepathBlocked      bool
	RepathBlockedAccum float64

	// NonCombat marks the unit as passive: combat AI never auto-acquires
	// targets for it. The unit only engages when the player issues an
	// explicit OrderAttackTarget (sticky attack). Workers are the canonical
	// non-combat unit; see catalog/units/human/worker/worker.json.
	NonCombat bool

	// Flyer marks the unit as airborne. Flyers ignore terrain and ground-unit
	// obstacles in pathing & separation; only other flyers and map bounds
	// constrain them. Populated from UnitDef.Flyer at spawn.
	Flyer bool

	// TargetableTypes is the resolved set of target classes this unit can hit
	// (subset of {"ground","flyer"}). Populated at spawn from UnitDef plus the
	// projectile-attack default. Used by selectBestTargetLocked and
	// combatTargetIsValidLocked to exclude invalid targets up front.
	TargetableTypes []string

	Damage      int
	AttackRange float64
	// BaseAttackRange is the catalog AttackRange before any perk-driven range
	// modifiers (eagle_spirit, bullseye). Treated like BaseDamage / BaseAttackSpeed:
	// recomputed into AttackRange by applyRankModifiersLocked so range-modifying
	// perks (Marksman) flow through to combat acquisition, projectile flight,
	// pierce length, and the HUD without scattered mutation sites.
	BaseAttackRange float64
	AttackSpeed     float64
	// SplashRadius: when > 0, every attack landing on a primary target also
	// damages every other hostile within this radius of the target's position.
	// Populated from UnitDef.SplashRadius at spawn time.
	SplashRadius float64
	// MoveSpeed is the effective pixels-per-second for pathing movement, after
	// rank/path/perk modifiers are applied. Populated by applyRankModifiersLocked.
	MoveSpeed      float64
	AttackCooldown float64
	// AttackWindupRemaining is the seconds left in the swing's animation
	// window before damage actually lands. When > 0 the unit is mid-swing:
	// it stays in status "Attacking" (so the client keeps playing the attack
	// animation), but no damage / projectile is emitted yet. On reaching 0
	// the swing resolves — damage applies if the target is still valid and
	// in range; otherwise the swing whiffs but the cycle still advances.
	// Paused while stunned. Decoupled from AttackCooldown: cooldown ticks the
	// idle gap AFTER the swing lands; windup ticks the animation window
	// BEFORE damage. Total cycle = windup + cooldown = 1/effectiveAttackSpeed.
	AttackWindupRemaining float64
	AttackTargetID        int
	// AttackWindupTargetID is the unit target the CURRENT in-flight swing was
	// committed against — snapshotted from AttackTargetID at the instant the
	// windup begins (0 for a building swing). applyDelayedAttackLocked resolves
	// damage against THIS id, not the live AttackTargetID, so a mid-swing
	// retarget (camp aggro broadcast, a player re-issuing AttackWithUnits, any
	// future path) can't redirect an already-committed swing onto a different —
	// possibly out-of-range — enemy. Without this, the committed swing read the
	// live AttackTargetID at impact and applied full damage to whatever it had
	// become, landing hits far outside the attacker's range. The live retarget
	// still takes effect on the NEXT swing. Reset each windup start so it never
	// carries a stale value between swings or into a building swing.
	AttackWindupTargetID   int
	AttackBuildingTargetID string
	Attacking              bool
	// Casting mirrors Attacking for the spell-cast animation slot: true while
	// the unit is mid-cast so the client plays the "Casting" animation
	// (distinct from "Attacking") and the combat tick skips the unit (it can't
	// attack while casting). Set/cleared via begin/endUnitCastingLocked. The
	// timed cast lifecycle (cast time, mana, interrupt) is layered on this in
	// the ability system; this field is just the animation/-busy primitive.
	Casting bool
	// ProjectileID is the ProjectileDef id this unit's basic ranged attack
	// fires (from UnitDef.Projectile, e.g. "fire_bolt"). Empty ⇒ the default
	// procedural shot. AttackDamageType tags that attack's damage (Part 2
	// flavor/metadata; empty ⇒ physical). Set at spawn from the unit def.
	ProjectileID     string
	AttackDamageType DamageType
	// AttackType is the melee attack-sound key for this unit (from
	// UnitDef.AttackType, optionally overridden by the promotion path in
	// applyRankModifiersLocked). Seeded at spawn; empty for ranged units and
	// workers. Read only at swing resolution (applyDelayedAttackLocked) to emit
	// a meleeAttackEvent. Purely presentational.
	AttackType string
	// ProjectileScale is a per-unit render-size multiplier for this unit's
	// projectile sprite (from UnitDef.ProjectileScale, optionally overridden
	// by the promotion path). It is copied onto every projectile this unit
	// fires and travels to the client as ProjectileSnapshot.Scale, so two
	// units sharing one projectile def can draw it at different sizes.
	// 0 ⇒ the client's default 1× (purely visual; never read by simulation).
	ProjectileScale float64
	// Abilities are the ability ids this unit has (AbilityDef ids, slot order;
	// from UnitDef.Abilities). Per-instance auto-cast / cooldown state is
	// layered on in the action-bar part. Nil for non-caster units.
	Abilities []string
	// OnDamageDealtDispatchActive is the RE-ENTRANCY GUARD for the
	// on_damage_dealt composable trigger (fireOnDamageDealtLocked,
	// ability_damage_dealt.go): set true for the duration of THIS unit's
	// on_damage_dealt fire, cleared (deferred) on return. Every deal_damage
	// action stamps DamageSource.AttackerUnitID = ctx.CasterID (see
	// ability_program_registry.go's deal_damage Execute), so an
	// on_damage_dealt trigger whose own actions deal more damage would, with
	// no guard, immediately re-enter fireOnDamageDealtLocked for the SAME
	// attacker and recurse without bound. Pattern mirrors
	// PerkState.SharedPainActive (trap.go's perkShareDamageToMarkedLocked) —
	// a per-unit flag checked and set BEFORE the recursive call, not a
	// ctx-scoped depth counter, because each fire builds a brand-new
	// RuntimeAbilityContext (ctx.depth/opsUsed reset to 0 every time,
	// mirroring fireOnUnitDeathLocked) so nothing on ctx itself survives
	// across fires to bound the recursion.
	OnDamageDealtDispatchActive bool
	// PoolAbilitiesByRank records the ability rolled from this unit's archetype
	// ability pool at each rank (arch-mage-spell-system §11), keyed by rank slug
	// (bronze/silver/gold). The roll is RNG and happens ONCE at rank-up
	// (rollUnitPoolAbilitiesLocked); assignUnitPathAbilitiesLocked then READS
	// this map to include the pick in unit.Abilities, keeping that recompute
	// idempotent and RNG-free (the split mirrors how ProgressionPath records the
	// path roll). Stores ability id strings, never pointers. Nil ⇒ no pool picks.
	PoolAbilitiesByRank map[string]string
	// SpellModifiers are active per-unit spell modifiers (spell_modifier.go) —
	// the concrete attachment point buffs / items / future perks use to tune a
	// unit's spells. They are FOLDED at cast time into an EffectiveSpell and
	// never mutate the base AbilityDef. Nil ⇒ the unit's spells resolve to
	// their base values. Value data (no pointers), so it is determinism- and
	// snapshot-safe. The collector (collectSpellModifiersLocked) reads this
	// slice alongside any source-method seams.
	SpellModifiers []SpellModifier
	// Active ability cast (mirrors the AttackWindup* timed-state pattern).
	// CastAbilityID == "" ⇒ not casting. CastTimeRemaining counts down in
	// tickUnitCastLocked; on reaching 0 the ability resolves. Target is held
	// by ID and re-resolved/validated every tick. LastCastFailure records the
	// most recent failure reason for player feedback (surfaced by the action
	// bar / WS layer via the existing NotificationMessage pattern).
	CastAbilityID     string
	CastTargetID      int
	CastTimeRemaining float64
	LastCastFailure   string

	// GlobalCooldownRemaining is the shared "global cooldown" (GCD): initiating
	// ANY ability sets it to abilityGlobalCooldownSeconds, and no ability (manual
	// or auto-cast, including the same one) may be initiated until it decays to 0.
	// This spaces out a unit's abilities so a caster with several ready spells
	// uses them one after another rather than simultaneously. Ticked down in
	// tickUnitAbilityCooldownsLocked.
	GlobalCooldownRemaining float64

	// Point (ground-target) cast in progress. When CastIsPoint is true the cast
	// resolves at a world point (CastPointX/Y) rather than a unit target — see
	// tickUnitCastLocked. Set by beginAbilityCastAtPointLocked for timed point
	// casts; cleared by clearUnitCastLocked.
	CastIsPoint bool
	CastPointX  float64
	CastPointY  float64

	// Active channel state (channeled abilities — first in-game: siphon_life).
	// ChannelAbilityID == "" ⇒ not channeling. Channels and one-shot casts
	// are mutually exclusive: a unit with ChannelAbilityID set has no
	// CastAbilityID and vice versa. Target is held by ID and re-resolved /
	// validated every tick (ID-not-pointer rule). ChannelNextTickIn counts
	// down in tickUnitChannelLocked; when it crosses below 0 the next effect
	// tick fires and it is reset by adding ChannelTickInterval. Initialized to
	// ChannelTickInterval so the first tick fires after one full interval.
	ChannelAbilityID    string  // active channel ability id; "" = not channeling
	ChannelTargetID     int     // enemy target unit ID, resolved every tick
	ChannelTickInterval float64 // seconds between channel effect ticks (snapshot of ability def)
	ChannelNextTickIn   float64 // seconds until next effect tick fires
	// Auto-cast state (Part 10), per unit instance — lazily allocated, GC'd
	// with the unit on death (no shared/global state, nothing to clean up).
	// AutoCastEnabled[abilityID] == true ⇒ the unit auto-casts that ability
	// when conditions are met. AbilityCooldowns[abilityID] is seconds of
	// cooldown remaining (decayed each tick). Maps are keyed by ability id;
	// the auto-cast loop iterates the ordered Abilities slice, never these
	// maps, so iteration order never drives outcomes (determinism).
	AutoCastEnabled  map[string]bool
	AbilityCooldowns map[string]float64
	// ActionFacingDX/DY is the world-space delta from this unit to its current
	// attack target, recomputed each tick the unit is in-range and firing.
	// Cleared (0,0) when the unit is not actively attacking. Shipped via
	// UnitSnapshot so the client can orient the sprite toward the exact target
	// the server is shooting, instead of guessing via a local nearest-enemy
	// search (which diverges when targets overlap, are off-screen, or the
	// server's pick differs from the client's).
	ActionFacingDX float64
	ActionFacingDY float64
	// Order is the player's current standing order for this unit. It is the
	// single source of truth for intent — replacing the old ManualMove /
	// ManualAttackTarget bool pair. All combat-AI gates, retreat suppression,
	// and resume-after-combat logic key off Order.Type.
	// queue-ready: becomes []OrderState for shift-queue in a future design.
	Order OrderState

	// FocusTargetID is the ID of the ally this unit is focused on (Cleric only).
	// Zero means no focus target. Stores the ally's unit.ID — never a *Unit
	// pointer, matching the project's ID-not-pointer targeting invariant. Mutated
	// only via RequestSetFocusTargetLocked / clearFocusTargetLocked so all
	// transitions are observable in one place (focus_target.go). Always paired
	// with Order.Type == OrderFocusFollow; any order replacement also zeroes this
	// field via clearFocusTargetLocked.
	FocusTargetID int
	// PickupLootID links this unit to a LootDrop it is walking to
	// collect. Empty when not pickup-bound. Stored as ID (not pointer)
	// per AI_RULES; resolved each tick in tickLootDropsLocked (Batch 4).
	// Cleared by any order-replacement helper that already clears other
	// sticky targets (GatherTargetID etc.) — see Batch 4 task.
	PickupLootID string

	CombatAnchorX      float64
	CombatAnchorY      float64
	LastTargetEvalTick int
	CurrentTargetScore float64
	// NextObjectiveSearchTick gates re-entry into enemyAdvanceToObjectiveLocked
	// for enemy units that found nothing attackable last call. Without this,
	// every idle enemy reruns the building search + A* every tick and dominates
	// the simulation budget. Set forward by enemyObjectiveSearchCooldownTicks
	// after a search; cleared to 0 when the unit acquires any target.
	NextObjectiveSearchTick int
	// UnreachableBuildingTargetID / UnreachableUntilTick memo the last building
	// that failed A* so the scoring loop can skip it for the cooldown window,
	// forcing selection of a different building instead of hammering pathfinding
	// every tick against an inaccessible one.
	UnreachableBuildingTargetID string
	UnreachableUntilTick        int
	// ObjectiveBuildingID is the building this routed enemy is attack-moving
	// toward (its sticky objective — typically the assigned/nearest townhall).
	// Distinct from ObjectiveID (static victory-point lock) and TargetPlayerID
	// (player-routing preference). Resolved + validated at point-of-use every
	// time a repath is needed via getBuildingByIDLocked; never a cached pointer.
	// Re-acquired only when the current objective building dies/disappears, so
	// advancing enemies do not re-pick an objective every tick (anti-churn).
	ObjectiveBuildingID string
	// UnreachableUnitTargetID / UnreachableUnitUntilTick memo the last
	// AI-acquired unit target that failed A*, so selectBestTargetLocked skips it
	// for the cooldown window and the enemy switches to a reachable target
	// instead of drifting. Single-slot (last unreachable unit); with >=2
	// simultaneously unreachable units the staggered re-eval self-corrects.
	// Player-issued (OrderAttackTarget) targets are NOT memoed — they keep
	// drift mode (see assignAttackApproachPathLockedWithSubBlocked).
	UnreachableUnitTargetID  int
	UnreachableUnitUntilTick int
	// NextApproachRepathTick throttles the forced repath that
	// tickUnitCombatLocked issues for unit-vs-building combatants out of attack
	// range with a non-moving state. Without this, units stuck on unreachable
	// building targets run the full sub-cell A* (~65k cells) every tick.
	// Unit-vs-unit no longer needs this throttle because drift mode replaces
	// the per-tick A* retry. Set forward by approachRepathCooldownTicks when
	// assignUnitPath fails; cleared when the unit starts moving.
	NextApproachRepathTick int
	// NextCombatEvalTick throttles evaluateCombatLocked for a unit whose last
	// target acquisition failed (A* couldn't reach the chosen building, no
	// alternative reachable, etc.). Without this gate a unit with no target
	// re-runs selectBestTargetLocked + applyCombatTargetLocked + escalation on
	// every tick, cycling through unreachable buildings and burning ~30ms/tick
	// at army scale. Set forward by RetargetIntervalTicks ticks on failure;
	// cleared on successful target acquisition.
	NextCombatEvalTick int
	// NextGuardReturnTick holds tickGuardReturnLocked off for a brief grace
	// window after a target is dropped. Without this, a guard that loses its
	// target (e.g. target died) is yanked back to its anchor on the same tick
	// before the retarget cooldown can pick a replacement, producing the
	// visible "Moving To Attack ↔ Guarding" flicker at retarget cadence.
	NextGuardReturnTick int
	TauntedByUnitID     int
	TauntRemaining      float64
	// StunnedRemaining is seconds left on the stun CC applied to this unit by
	// ApplyStunLocked. Decays in state.go Update() alongside WeakenedRemaining.
	// While > 0 the unit cannot attack or move along its path, but
	// AttackTargetID and Path are preserved so it resumes cleanly when it expires.
	StunnedRemaining float64
	// ── Forced displacement / pull+push CC (arch-mage-spell-system) ────────
	// PullRemaining > 0 marks a unit under forced displacement (arcane_orb,
	// apply_force): each tick it is moved relative to (PullCenterX,
	// PullCenterY) at PullStrength world-px/sec, and normal path advancement
	// is skipped for that tick (displacement wins). On expiry the unit's
	// stale path is dropped so it re-plans from its displaced position (no
	// snap-back). Pure-math, seed-safe. See spell_pull.go. All zero ⇒ not
	// being displaced.
	PullRemaining float64
	PullCenterX   float64
	PullCenterY   float64
	PullStrength  float64
	// PullPush selects the displacement DIRECTION: false (the zero value —
	// every pre-existing pull, including arcane_orb's vortex) drags the unit
	// TOWARD center and snaps to it on arrival (tickUnitPullLocked's
	// overshoot clamp). true pushes the unit AWAY from center instead, with
	// NO snap clamp — a pushed unit keeps moving outward every tick until
	// PullRemaining expires. Only meaningful while PullRemaining > 0; reset
	// to false by endUnitPullLocked.
	PullPush bool
	// SlowedRemaining is seconds left on the PHYSICAL/generic slow CC (traps,
	// concussive perks) applied by ApplySlowLocked. Decays in state.go Update();
	// when it reaches 0, SlowedMultiplier is also cleared.
	SlowedRemaining float64
	// SlowedMultiplier is the movement speed fraction while slowed (e.g. 0.7 =
	// 70% speed). Set by ApplySlowLocked; 0 when no slow is active. This is the
	// ONE generic slow track — the separate cold/chill track was retired (chill
	// is now a change_stat + apply_color_overlay composition).
	SlowedMultiplier   float64
	ThreatTable        map[int]*ThreatEntry
	TankedDamageByUnit map[int]float64
	// DamageDealtByUnit accumulates damage this unit has taken from each
	// attacker, keyed by attacker ID. On death the map is paid out so
	// contributors earn damage XP only when the target actually dies.
	DamageDealtByUnit map[int]int

	// GuardMode pins the combat anchor at the authored spawn position and
	// overrides aggro/leash ranges with GuardAggroRange/GuardLeashRange.
	// Set on enemy placed units (PlacedUnit.PlayerSlot == "enemy") by
	// spawnPlacedEnemyUnitsLocked. Player-owned placed units do NOT use guard
	// mode — they are normal units that happen to spawn at an authored location.
	GuardMode       bool
	GuardAnchorX    float64
	GuardAnchorY    float64
	GuardAggroRange float64
	GuardLeashRange float64

	// BaseVisionRange is the authored vision radius in world pixels. Set at
	// spawn from UnitDef.VisionRange (defaulting to defaultVisionRange when
	// the def omits it). Never modified after spawn — VisionRange is the
	// live value that perk multipliers write into.
	BaseVisionRange float64
	// VisionRange is the effective vision radius after perk multipliers.
	// Recomputed by applyRankModifiersLocked each tick similar to MoveSpeed.
	VisionRange float64

	// InventorySize is the number of item slots this unit has, determined by
	// rank (0 = none, 1 = bronze, 2 = silver, 3 = gold). Updated by
	// setInventorySizeForRankLocked on every rank-up.
	InventorySize int
	// Equipped holds the items currently in this unit's slots. len == InventorySize;
	// nil entries are empty slots. Items are permanently lost when the unit dies
	// (the slice is discarded with the unit struct during removeUnitLocked).
	Equipped []*EquippedItem
	// EquipmentBonus holds the accumulated flat stat bonuses from all equipped
	// items. Recomputed by recomputeUnitEquipmentBonusLocked and folded into
	// derived stats by applyRankModifiersLocked.
	EquipmentBonus UnitEquipmentBonus

	// UnreachableBuildingStrikeCount tracks consecutive A* failures against the
	// current building target so escalation can increase the cooldown and
	// eventually fall back to an objective. Reset to 0 on successful path.
	// Note: unit-target unreachability uses drift-mode (AttackDrifting) instead
	// of strike escalation — see assignAttackApproachPathLocked.
	UnreachableBuildingStrikeCount int

	// AttackDrifting signals the per-unit movement loop to step straight-line
	// toward TargetX/TargetY each tick (no A*) instead of following a path.
	// Set by assignAttackApproachPathLocked when A* to a unit target fails;
	// cleared on successful repath, target acquisition, target clear, or when
	// the unit reaches attack range / hits an impassable cell. Lets a unit
	// physically advance toward an unreachable target via separation pressure
	// rather than burning A* budget every tick or sitting idle.
	AttackDrifting bool

	// PathDiagnostics carries lightweight per-unit pathing telemetry for debug
	// snapshots. Zero-cost in production paths — just integer increments.
	PathDiagnostics PathDiagnostics

	// SnapshotCache is the per-tick cache of effective stats used by every
	// MatchSnapshot* path. Populated by recomputeUnitSnapshotCacheLocked at
	// the end of Update so the per-viewer snapshot loop reads constant-time
	// fields instead of recomputing every perk hook (perkBonusDamageMulti-
	// plierLocked, perkAttackSpeedBonusLocked, effectiveArmorLocked, etc.)
	// for every unit for every viewer. Freshness is gated on
	// GameState.unitSnapshotCacheTick; readers that find a stale cache fall
	// back to live computation, preserving direct-call test behaviour.
	SnapshotCache unitSnapshotCache
}

// unitSnapshotCache stores the values the snapshot loop used to recompute on
// the fly. Everything here is per-unit (not per-viewer) — even Damage, which
// folds in the global enemy-facing buff multiplier.
type unitSnapshotCache struct {
	EffectiveDamage      int
	EffectiveAttackSpeed float64
	EffectiveMoveSpeed   float64
	EffectiveArmor       int
	MaxShield            int
	CritChance           float64
	CritMultiplier       float64
	ActiveBuffs          []protocol.ActiveEffectIcon
	ActiveDebuffs        []protocol.ActiveEffectIcon
	PerkCooldowns        []protocol.PerkCooldownSnapshot
	Abilities            []protocol.AbilitySnapshot
	XPToNextRank         int
	XPIntoCurrentRank    int
}

// PathDiagnostics holds lightweight per-unit pathing telemetry exposed via the
// debug snapshot. Not duplicated in GameState — lives directly on Unit so it
// is cache-local with the rest of the unit's pathing fields.
type PathDiagnostics struct {
	RepathCount       int
	StuckTriggerCount int
	LastStuckTick     int
}

const (
	// Unit move speed is authored per-type in catalog/units/<faction>/<unit>/<unit>.json
	// (UnitDef.MoveSpeed). Path multipliers (pathModifierTable) and perk
	// multipliers (momentum) stack on top of the per-unit BaseMoveSpeed.
	unitRadius = 10.0
	// unitFormationSpacing is the centre-to-centre distance between
	// formation slots when a multi-unit move command is issued. Set well
	// above unitSeparationDistance (22) so neighbouring slots have
	// breathing room rather than landing right at the separation
	// boundary, which made groups look cramped on arrival.
	unitFormationSpacing   = 40.0
	unitSeparationDistance = 22.0

	// corpseLifetimeSeconds is how long a body lingers before it decays off the
	// field. A feel-check starting point, not a balance decision — it is also
	// the window an eventual revive or raise has to work in, so moving it moves
	// how those play. See docs/design/death_and_corpses.md §8.
	corpseLifetimeSeconds = 20.0

	// guardMinAggroRange is the floor applied to GuardAggroRange at spawn for
	// placed-enemy guard units. Authored values below this are raised so guards
	// reliably notice approaching player units before they're already in melee.
	// Neutral camps are exempt: an authored AggroRange overrides this (see
	// spawnGroupForCampLocked); the value only serves as their default when unset.
	guardMinAggroRange = 275.0

	// guardLeashAggroMultiplier sets how far past its aggro radius a guard will
	// chase before the leash yanks it home. It must be > 1 so a hostile acquired
	// at the edge of the aggro radius (selectBestTargetLocked) is not immediately
	// dropped by the leash check (shouldDropCurrentTargetLocked) — the chase/drop
	// juggling the enemy-guard spawn code warns about. Used by the player-issued
	// GuardUnits command to derive GuardLeashRange from GuardAggroRange.
	guardLeashAggroMultiplier = 1.5

	// guardRetaliationPersistTicks is how long a guard pursues an attacker
	// after the last hit lands, regardless of leash. 4 seconds at 20 Hz. Each
	// new hit refreshes the window via addThreatLocked → entry.LastActiveTick,
	// so a sustained ranged barrage keeps the guard committed to the shooter.
	// Once attacks stop and this window elapses the leash check resumes and
	// the guard returns to its anchor.
	guardRetaliationPersistTicks = 80

	// neutralCampLinkThreat is the threat conferred on every camp-mate when one
	// camp guard is attacked (broadcastNeutralCampAggroLocked). It exists so
	// mates get the same retaliation leash-bypass the directly-hit guard gets;
	// without it a mate whose anchor->attacker distance exceeds GuardLeashRange
	// would immediately drop the broadcast target and stay at its post.
	neutralCampLinkThreat = 30.0

	// defaultHealthRegenPerSecond is the baseline passive regen applied to all
	// units on spawn (1 HP every 5 seconds). Stored as HP-per-second so future
	// perks / buffs can scale it with a multiplier.
	defaultHealthRegenPerSecond = 0.2

	// Movement progress watchdog. If a moving unit hasn't displaced more than
	// sqrt(stuckProgressThresholdSq) pixels over stuckSampleInterval seconds,
	// the per-tick movement loop forces a repath. Catches the case where
	// separation, an obstructing unit, or a stale path wedges a unit into a
	// back-and-forth loop without any net progress toward its destination.
	stuckSampleInterval      = 0.6
	stuckProgressThresholdSq = 6.0 * 6.0

	// Repath-blocked retry. When a moving unit's forced repath finds no route,
	// it holds its order and retries pathing every repathBlockedRetryInterval
	// seconds for up to repathBlockedGiveUpSeconds before finally stopping.
	// Most "no route" results are transient (a passing crowd closes the fine
	// sub-cell corridor for a few ticks, an obstacle is dropped into the path,
	// or the A* node-budget cutoff fires mid-battle), so abandoning the order
	// on the first failure left units wedged against buildings/trees forever.
	// Retrying on a bounded cadence recovers them without an every-tick A*
	// storm and without spinning indefinitely on a truly unreachable goal.
	repathBlockedRetryInterval = 0.5
	repathBlockedGiveUpSeconds = 3.0
)

type Player struct {
	ID    string
	Color string
	// TeamID is the alliance group. 0 = the default shared team, so every
	// existing/zero-value Player is allied (current behavior preserved).
	// Same TeamID ⇒ allies; different ⇒ hostile. __enemy__ is hostile to all
	// teams regardless of this field (the team predicates short-circuit on
	// it). Assign non-zero per-match to enable PvP/FFA — no call-site changes.
	TeamID    int
	Resources map[string]int

	GlobalUnitSpawnTimeMultiplier float64
	UnitSpawnTimeMultipliers      map[string]float64

	// Upgrades holds the current permanent upgrade level per track for this
	// player. Keyed by UpgradeTrack (== UnitType string). Initialized to an
	// empty map on player creation; zero value for missing keys means level 0.
	// In-progress research is NOT stored here — it lives in the global
	// GameState.ActiveUpgrades registry (keyed by source building) until it
	// completes and bumps the level here.
	Upgrades map[UpgradeTrack]int

	// Vault holds items the player has purchased but not yet equipped. One Vault
	// per player (not per townhall). Capacity is tier-gated via
	// vaultCapacityForPlayerLocked. Initialized to an empty (non-nil) slice.
	Vault []*VaultItem

	// RunDominionPointDrops accumulates dominion-point drops during the match.
	// Committed to the profile file at match end.
	RunDominionPointDrops int

	// MatchDominionPointsEarned is the always-on per-match earned total,
	// incremented on every successful drop regardless of commitMode. Unlike
	// RunDominionPointDrops (which stays 0 in immediate mode by design), this
	// is the authoritative per-player total reported to each viewer in the
	// game-over snapshot, for end-of-match display and for a remote joiner to
	// persist into its own local profile (the host commits server-side).
	MatchDominionPointsEarned int

	// ProfileUpgrades is a snapshot of PlayerProfile.OwnedUpgradeRanks taken
	// at match join. Mutations to the profile during the match do not affect
	// this field. Nil / empty means no upgrades purchased.
	ProfileUpgrades map[string]int

	// ActiveUpgradeIDs is a set view of the player's active upgrade IDs.
	// Presence means active; used for O(1) lookup in applyProfileUpgradesToPlayerLocked.
	// Derived from the activeUpgradeIds passed at join time.
	ActiveUpgradeIDs map[string]bool

	// PhysicalDamageMultiplier is the total outgoing-damage multiplier applied
	// to physical attacks by this player's units. Initialized to 1.0 at match
	// join; increased by damageMultiplierByType upgrades with class "physical".
	PhysicalDamageMultiplier float64

	// MagicDamageMultiplier is the total outgoing-damage multiplier applied to
	// non-physical attacks by this player's units. Initialized to 1.0 at match
	// join; increased by damageMultiplierByType upgrades with class "nonPhysical".
	MagicDamageMultiplier float64

	// ExtraStartingUnits tallies additional starting units granted to this
	// player by profile upgrades (extraStartingUnit effect), keyed by
	// unitType → count. Iterated in sorted key order at match start and
	// spawned at the player's assigned spawn-point.
	ExtraStartingUnits map[string]int

	// UpgradeState tracks wave upgrade picks and per-wave offer state.
	UpgradeState PlayerUpgradeState

	// ShopRerollsRemaining is the player's per-match pool of merchant
	// rerolls. Each reroll regenerates one neutral-shop's inventory and
	// decrements this counter. Initialised at match-join from
	// defaultShopRerollsPerPlayer (currently 1); future dominion-point
	// profile upgrades can bump the starting value via the same
	// applyProfileUpgradesToPlayerLocked path used by ExtraStartingUnits.
	ShopRerollsRemaining int

	// ShopItemCountBonus adds to the number of distinct items a neutral
	// merchant rolls into this player's independent view of the shop. The
	// effective count is neutralShopBaseItemCount() + bonus (see
	// shopItemTargetCountForPlayerLocked). Initialised to 0 at match-join;
	// future dominion-point profile upgrades (e.g. "Merchant Expanded
	// Selection: +1 item per rank") bump this via the same
	// applyProfileUpgradesToPlayerLocked registry pattern used by
	// PhysicalDamageMultiplier and ExtraStartingUnits.
	ShopItemCountBonus int

	// NeutralShopInventories is this player's INDEPENDENT view of every neutral
	// shop, keyed by building ID. Neutral shops are per-player: each player
	// samples their own stock (sized to their ShopItemCountBonus) and decrements
	// their own quantities on purchase, so one player's buying / rerolling never
	// affects another's. Populated at join by populatePlayerNeutralShopViewsLocked
	// and refreshed by player-initiated and wave-based rerolls. Owned shops
	// (marketplace) and fixed-inventory player shops are NOT stored here — they
	// stay on the shared BuildingTile.ShopInventory.
	NeutralShopInventories map[string][]protocol.ShopStockEntry

	// CommanderAbilityCooldowns tracks wall-clock seconds remaining on each
	// commander ability (see commander_abilities.go). Keyed by ability id;
	// entries are removed as they decay to 0. Nil/empty = every ability is
	// ready.
	CommanderAbilityCooldowns map[string]float64

	// AcquiredAdvancements is a snapshot of the player's purchased advancement
	// IDs taken at match join. Mutations to the profile during the match do not
	// affect this field. Nil / empty means no advancements purchased.
	AcquiredAdvancements []string

	// UnlockedCraftableIDs is the in-match set of ITEM IDs whose recipes this
	// player has learned and may craft at an Artificer. Seeded at join from the
	// profile's KnownCraftableIDs and grown by purchase_recipe. Sorted, deduped.
	// An item is its own recipe (see ItemDef.Crafting), so these are item IDs.
	UnlockedCraftableIDs []string

	// EffectiveUnitDefs is a per-player map of unitType → modified UnitDef
	// reflecting all owned advancements. Computed once at match start by
	// applyAdvancementsToEffectiveDefsLocked. Only contains entries for unit
	// types the player has at least one advancement for; absent entries fall
	// back to the catalog def. Never mutated after match start.
	EffectiveUnitDefs map[string]UnitDef

	// ExtraPerkSlots records advancement-granted extra perk tiers per unit type.
	// Outer key: unitType (e.g. "soldier"). Inner key: tier ("bronze" / "silver" /
	// "gold"). Value true means assignUnitPerkLocked grants a SECOND perk of that
	// tier at the rank-up moment. Computed once at match start by the
	// unitExtraPerkSlot effect handler in advancementEffectRegistry and never
	// mutated thereafter. Nil-map fast path: a player with no extra-slot
	// advancements has a nil map, and the perk-grant logic short-circuits.
	ExtraPerkSlots map[string]map[string]bool

	// Metrics aggregates per-player gameplay totals during a single match.
	// Bumped by event hooks (deposit, kill, building complete, camp clear,
	// wave clear, unit train, rank-up) and consumed by the objective evaluator
	// + match snapshot. See match_metrics.go for the full surface area.
	// Initialise via NewMatchMetrics() at every player-construction site to
	// guarantee non-nil maps on the wire.
	Metrics MatchMetrics

	// ZoneStatModifiers is the player's aggregated stat-modifier set reduced
	// from the auras of every zone the player (or an ally) currently controls.
	// Server-only (never on the wire); rebuilt event-driven on any zone
	// ownership change by recomputeAllZoneAuraModifiersLocked (zone_auras.go),
	// never per tick. Hot-path read sites resolve it in O(1) via
	// playerStatModifierLocked. Nil/empty ⇒ every stat resolves to identity
	// (0, 1), so behaviour is unchanged when no auras are active.
	ZoneStatModifiers PlayerStatModifierSet
}

const (
	wavePrepDuration   = 60.0
	waveActiveDuration = 120.0
)

type GameState struct {
	mu sync.RWMutex

	Tick int

	MapConfig protocol.MapConfig
	MapID     string
	MapWidth  float64
	MapHeight float64

	// CampaignLevelID, when non-empty, identifies the CampaignLevelDef this
	// match was launched for. Set by Match.SetCampaignLevel before the loop
	// starts ticking. Empty for Custom Game / find-game matches.
	CampaignLevelID string

	// Ephemeral marks a throwaway editor-playtest match: the full sim and
	// objective evaluation run, but reward persistence is suppressed (see the
	// gated hooks in manager.go). Set at construction, never changes.
	Ephemeral bool

	// Objectives holds the per-match runtime state for the launching
	// campaign level's objectives. Empty slice when CampaignLevelID is empty.
	// Evaluated each tick by evaluateObjectivesLocked (§8); written by the
	// snapshot serialiser (§10) and the victory check (§9).
	Objectives []objectiveRuntime

	// Zones holds the per-match runtime control state for MapConfig.Zones.
	// Built by installZonesLocked from setMapConfigLocked; evaluated each tick
	// by tickZonesLocked. Nil/empty on maps without zones. zoneCellIndex is the
	// cell->zoneId index (single-owner membership) used by the build-gate and
	// the capture occupancy scans.
	Zones         []zoneRuntime
	zoneCellIndex map[gridPoint]string

	Units   []*Unit
	Players map[string]*Player

	// Corpses are the bodies of units that have died and not yet decayed.
	//
	// A SEPARATE list from s.Units, deliberately. The alternative — leaving a
	// dead unit in s.Units behind a Dead flag — puts a corpse in front of ~110
	// existing `range s.Units` loops, every one of which was written when a
	// dead unit could not still be there. Several would be wrong in ways that
	// are invisible until someone notices: a body granting fog-of-war vision
	// for 20 seconds, a cleric auto-healing it, an aura counting it as a nearby
	// ally. Keeping the list separate makes every one of those loops correct by
	// construction, and makes reading a corpse an explicit act.
	//
	// The Unit VALUE is untouched — same pointer, same ID, same rank/perks/
	// items — so a revive is a move back into s.Units, not a reconstruction.
	// getUnitByIDLocked deliberately does NOT resolve a corpse; use
	// getCorpseByIDLocked. See docs/design/death_and_corpses.md.
	Corpses     []*Unit
	corpsesByID map[int]*Unit

	Productions      map[string][]*UnitProduction
	EnemySpawnTimers map[string]*EnemySpawnTimer

	// ActiveUpgrades is the global registry of in-progress building-driven
	// upgrades (blacksmith track research today; extensible to other source
	// buildings later). Keyed by the SOURCE building ID; the value is that
	// building's upgrade QUEUE — only index 0 researches, the rest wait behind
	// it (exactly like Productions for unit training). The resulting level
	// applies to the whole player (see Player.Upgrades); the building is just
	// the workshop. A track is locked to a single blacksmith for a player: while
	// any of their blacksmiths has the track in progress OR queued, that track
	// cannot be started at a different blacksmith. See state_upgrades.go.
	ActiveUpgrades map[string][]*ActiveUpgrade

	WaveManager WaveManager

	// NeutralCamps is the runtime state for map-authored NeutralSpawns.
	// Built once by initNeutralCampsLocked from MapConfig.NeutralSpawns and
	// driven by tickNeutralCampsLocked (Batch E). Empty/nil on maps with
	// no neutral spawns.
	NeutralCamps []NeutralCamp

	// LootDrops is the registry of ground-loot chests currently in the
	// world. Keyed by stable string id (allocated via nextLootDropID).
	// Drops persist until collected — no automatic expiry, no wave-start
	// despawn. See state_loot_drops.go for the spawn/pickup lifecycle.
	LootDrops      map[string]*LootDrop
	nextLootDropID int

	// pendingLootNotifications is the per-tick queue of chest pickups
	// that need to be pushed to the collecting player. The match
	// broadcast loop drains this after snapshots are sent. Cleared at
	// each drain; never read from a context that doesn't hold s.mu.
	pendingLootNotifications []protocol.LootCollectedNotification

	nextUnitID         int
	nextBuildingID     int
	nextOrderID        int64
	nextItemInstanceID int64

	// itemCatalog is the per-match snapshot of the item definitions, built by
	// newMatchItemCatalog() from the merged embed+editor-overlay view at match
	// creation. Never mutated after assignment in NewGameStateWithSeed — a
	// running match deliberately does not see mid-match editor saves.
	itemCatalog map[string]*ItemDef

	// matchSeed is the root seed for all per-match RNG streams. Log it on match
	// creation so a bug report with the seed can be reproduced offline.
	matchSeed   int64
	rngPerks    *mrand.Rand // perk selection, path assignment, taunt procs
	rngCosmetic *mrand.Rand // unit colour assignment and other visual randomness
	rngSpawn    *mrand.Rand // reserved for future wave/spawn randomness
	rngLoot     *mrand.Rand // dominion-point drop rolls; seeded with (seed ^ 0x4)
	rngCombat   *mrand.Rand // combat hit-resolution rolls (dodge/block); seeded with (seed ^ 0x5)

	// buildingDamageDealt mirrors Unit.DamageDealtByUnit for buildings.
	// buildingID → attackerID → accumulated damage. Paid out on destruction.
	buildingDamageDealt map[string]map[int]int

	// unitsByID is an O(1) index into s.Units, maintained in lockstep.
	// Use addUnitLocked / removeUnitByIDLocked to mutate — do NOT write to
	// s.Units or unitsByID directly outside those helpers.
	unitsByID map[int]*Unit

	// buildingsByID is an O(1) index into s.MapConfig.Buildings, maintained in
	// lockstep. Use addBuildingLocked / removeBuildingLocked to mutate.
	buildingsByID map[string]*protocol.BuildingTile

	// obstaclesByID is an O(1) index into s.MapConfig.Obstacles. Populated by
	// setMapConfigLocked and maintained by addObstacleLocked /
	// removeObstacleLocked. Obstacles with no id (walls) are not indexed.
	obstaclesByID map[string]*protocol.ObstacleTile

	// blockedCellsCache holds the last computed blocked-cell set.
	// blockedCellsValid is false when any building has been added or removed
	// since the last build. Guarded by s.mu.
	blockedCellsCache map[gridPoint]bool
	blockedCellsValid bool

	// walkableRegionsCache labels each walkable cell with its 4-connected
	// component so spawn placement can reject sealed pockets (see
	// pathing_regions.go). Derived from blockedCellsCache; invalidated by the
	// same hook. Guarded by s.mu.
	walkableRegionsCache *walkableRegions

	// visionBlockingCache holds cells that block line-of-sight: obstacles
	// (trees, rocks, walls) and terrain cliff transitions. Unlike
	// blockedCellsCache it does NOT include buildings — buildings don't
	// occlude vision. Rebuilt alongside blockedCellsCache. Guarded by s.mu.
	visionBlockingCache map[gridPoint]bool

	// Banners is the set of active rallying banners. Persisted as match state.
	// Ticked in tickBannersLocked after combat resolution.
	Banners      []*Banner
	nextBannerID int

	// Traps is the set of active Trapper traps. Ticked each Update:
	//   tickTrapEffectsLocked(dt)  — zone effects, before tickBannersLocked
	//   tickTrapsLocked(dt)        — lifetime decay + triggered cull, after tickBannersLocked
	Traps      []*Trap
	nextTrapID int

	// GroundHazards is the set of active delayed-impact / lingering-burn ground
	// zones (Meteor and future sky-drop spells). Server-only: never serialized —
	// the visual is a client effect and damage rides the authoritative pipeline.
	// Spawned by spawnGroundHazardLocked; ticked by tickGroundHazardsLocked in
	// Update (after traps, before drainPendingDeaths).
	GroundHazards      []*GroundHazard
	nextGroundHazardID int

	// AbilityZones is the set of active composable, tick-driven zones spawned
	// by the create_zone action (ability_zone.go). Server-only: never
	// serialized. Generalizes GroundHazard for spells authored as a program
	// rather than the legacy meteor-specific fields; GroundHazards keeps
	// serving Meteor and stays untouched. Spawned by spawnAbilityZoneLocked;
	// ticked by tickAbilityZonesLocked in Update (immediately after
	// tickGroundHazardsLocked, before drainPendingDeaths).
	AbilityZones      []*AbilityZone
	nextAbilityZoneID int

	// AbilityStatuses is the set of active, tick-driven buff/debuff objects
	// spawned by an AUTHORED apply_status action (ability_status.go). The
	// three legacy CC primitives (slow/stun/burn) do NOT go through this —
	// they keep routing to their existing seams
	// (applyProcSlowLocked/ApplyStunLocked/applyAbilityBurnLocked) exactly as
	// before; only a status carrying Triggers creates one of these. Server-
	// only: never serialized. Spawned by spawnAbilityStatusLocked; ticked by
	// tickAbilityStatusesLocked in Update (immediately after
	// tickAbilityZonesLocked, before drainPendingDeaths — see that function's
	// doc comment for why).
	AbilityStatuses     []*AbilityStatus
	nextAbilityStatusID int

	// previewTrace/previewClock are non-nil/non-zero ONLY during a
	// RunAbilityPreview harness run. When set, the ability executor attaches
	// previewTrace to every RuntimeAbilityContext it builds and stamps
	// events with previewClock (the harness's accumulated sim time). In
	// real matches these stay nil/0 ⇒ the executor's trace is inert
	// (record() no-ops on a nil trace) and there is zero behavior change.
	previewTrace *AbilityExecutionTrace
	previewClock float64

	// previewConditionalOverrides forces individual `conditional` actions to a
	// fixed outcome for the duration of a RunAbilityPreview run, keyed by the
	// conditional action's own id. Present ⇒ that conditional takes the mapped
	// branch without evaluating its conditions at all; absent ⇒ it evaluates
	// normally. This exists because the preview harness's synthetic caster owns
	// no perks, items or advancements, so every `has_perk` branch — the whole
	// point of a perk-gated ability like fire_pit — would always be false and
	// its THEN side would be unreachable in the editor.
	//
	// nil in every real match (the map is only ever populated by
	// RunAbilityPreview from PreviewRequest.ConditionalOverrides), so a
	// production conditional never consults it — a nil-map read is the same
	// two-word lookup the zero value would be.
	previewConditionalOverrides map[string]bool

	// simTime accumulates dt every Update() tick, in production and preview
	// alike (unlike previewClock, which stays 0 in production). It is a
	// plain dt-accumulator, never wall-clock — see the determinism rule in
	// AI_RULES.md. Read-only outside ability_marker.go, where it is the
	// clock the on_animation_marker scheduler fires scheduledMarker entries
	// against (scheduleMarkerTriggersLocked / tickAbilityMarkersLocked).
	simTime float64
	// pendingMarkers holds on_animation_marker triggers enqueued by
	// play_presentation (ability_exec_presentation.go) via
	// scheduleMarkerTriggersLocked, waiting for their fireAtSimTime. Ticked
	// by tickAbilityMarkersLocked in Update, immediately after
	// tickAbilityZonesLocked. Empty in every match today — no ability is
	// authored v2 with a marker-triggered presentation reachable from live
	// play (see ability_marker.go's TestMarkerScheduler_ProductionNoOp).
	pendingMarkers []scheduledMarker

	// pendingLoops holds `loop`-action iterations enqueued by
	// runLoopIterationLocked (ability_exec_loop.go), waiting for their
	// fireAtSimTime — the mechanism that spaces a loop's iterations over time
	// (chain_lightning's per-hop wait). Ticked by tickPendingLoopsLocked in
	// Update, right after tickAbilityMarkersLocked. Empty unless a loop with a
	// wait in its body is mid-run.
	pendingLoops []pendingLoopIteration

	// Projectiles is the set of in-flight ranged attacks. Ticked once per
	// Update() after tickUnitCombatLocked so freshly-fired shots decay on the
	// next tick, not their birth tick. Damage and all on-hit perk triggers
	// fire when a projectile lands; see projectile.go.
	Projectiles      []*Projectile
	nextProjectileID int

	// Beams is the set of active channeled-beam visuals. Each Beam is owned
	// by a unit that is currently channeling a beam-type ability (siphon_life).
	// The channel lifecycle (damage, mana, stop) is driven by the Unit's
	// Channel* fields; Beams is purely the visual entity the client renders.
	// Managed via spawnBeamLocked / removeBeamForUnitLocked / removeBeamForTargetLocked.
	Beams      []*Beam
	nextBeamID int

	// activeEffects is the set of generalized transient visual effects (e.g.
	// "whirlwind" spin overlay). Purely visual — gameplay logic is handled by
	// the perk that queued each entry. Ticked in tickEffectsLocked, dropped
	// when elapsed ticks reach DurationTicks. See state_effects.go.
	activeEffects []effectInstance
	nextEffectID  int

	// critEventsThisTick is the per-tick queue of (target, damage) entries
	// for critical hits that landed this tick. Drained by Snapshot() and
	// truncated immediately after so each tick's queue covers exactly the
	// crits applied during that tick. See crit_events.go.
	critEventsThisTick []critEvent

	// meleeAttackEventsThisTick is the per-tick queue of melee swings that
	// resolved this tick, each carrying the swing's AttackType sound key.
	// Drained by Snapshot() and truncated immediately after, like
	// critEventsThisTick. See melee_attack_events.go.
	meleeAttackEventsThisTick []meleeAttackEvent

	// minorDamageEventsThisTick mirrors critEventsThisTick for ancillary
	// damage hits that should render as a smaller orange floating number
	// (Reactive Flames splash, etc.). See minor_damage_events.go.
	minorDamageEventsThisTick []minorDamageEvent

	// pendingArcaneMissiles are queued Arcane Missiles awaiting their staggered
	// launch: a volley fires one bolt every arcaneMissileStaggerSeconds instead
	// of all at once, for a better rat-a-tat feel. Drained in
	// tickArcaneMissilesLocked. See spell_charge.go.
	pendingArcaneMissiles []pendingArcaneMissile

	// evadeEventsThisTick mirrors minorDamageEventsThisTick for avoided basic
	// attacks (dodge/block) — one entry per whiffed melee/projectile/pierce
	// hit this tick. See evade_events.go.
	evadeEventsThisTick []evadeEvent

	// hitDamageEventsThisTick mirrors critEventsThisTick for individual landed
	// hits. Lets the client split its HP-diff popup into per-hit numbers so
	// two simultaneous strikes read as "12" "12" instead of one "24". See
	// hit_damage_events.go.
	hitDamageEventsThisTick []hitDamageEvent

	// damageTypeHintsThisTick is the parallel channel for COLORING the
	// regular floating-up popup (not a separate popup like minor events).
	// Auto-emitted by applyUnitDamageWithSourceLocked whenever the damage
	// source carries a typed DamageType recognised by
	// damageTypeColorVariant. See damage_type_hints.go.
	damageTypeHintsThisTick []damageTypeHint

	// lethalDamageEventsThisTick mirrors critEventsThisTick for overkill
	// killing-blow amounts. The client uses these to override its synthesized
	// killing-blow popup so overkill displays the real damage instead of the
	// HP-capped value. See lethal_damage_events.go.
	lethalDamageEventsThisTick []lethalDamageEvent

	// healEventsThisTick mirrors critEventsThisTick for intentional heals
	// (heal ability / AbilityDef.HealAmount). Drives the light-green "+N"
	// floating number. Passive regen is intentionally excluded. See
	// heal_events.go.
	healEventsThisTick []healEvent

	// manaRestoreEventsThisTick mirrors healEventsThisTick for intentional
	// mana grants (Repurposed Life on enemy death, future cleric mana
	// abilities). Drives a blue "+N" floating number. Passive mana regen
	// is intentionally excluded (too spammy at the 0.2/s default rate).
	// See mana_restore_events.go.
	manaRestoreEventsThisTick []manaRestoreEvent

	// nextGlobalObjectiveSearchTick gates enemyAdvanceToObjectiveLocked globally so
	// at most one map-wide A* runs per 5 ticks regardless of army size.
	nextGlobalObjectiveSearchTick int

	// combatApproachBudgetRemaining caps the number of AI-driven approach A*
	// runs per combat tick. Reset to combatApproachBudgetPerTick at the start
	// of tickCombatAILocked. When exhausted, refreshUnitAttackApproachLocked
	// drops the unit into drift mode (straight-line, no A*) instead of paying
	// the ~6-14ms sub-cell A* cost. The deferred work picks up on the next
	// tick when budget refills. Player-issued commands bypass this gate
	// entirely (they go through assignAttackGroupPathsLocked / direct
	// assignAttackApproachPathLocked, not refreshUnitAttackApproachLocked).
	combatApproachBudgetRemaining int

	// approachCoarsePathCache memoises the coarse-grid findPath() result
	// inside assignAttackApproachPathLockedWithSubBlocked, keyed by
	// (startCell, goalCell). Cleared at the start of every tickCombatAILocked.
	// Hit rate is highest when multiple units stacked on the same grid cell
	// chase the same target — the sub-cell A* still runs per-unit (subBlocked
	// is per-unit-spawn-state and cannot be safely shared), but the coarse
	// pass is shared. Miss path is a single map lookup, free at runtime.
	approachCoarsePathCache map[approachPathCacheKey][]protocol.Vec2

	// objectiveUnreachableUntil is the ARMY-WIDE unreachable-objective cache:
	// objective building ID → tick the suppression expires. When any enemy's
	// pathfind to an objective fails, every enemy skips re-pathing that
	// objective until the TTL lapses; then one gated enemy re-tests and either
	// clears it (route reopened by killing through the wall → path succeeds)
	// or re-arms it. Replaces the per-unit memo churn that let a large walled
	// army keep re-paying the (budget-bounded but ~9ms) failed A* every gate.
	// Point get/set/delete only — never iterated for outcomes — so it stays
	// deterministic under a seed.
	objectiveUnreachableUntil map[string]int

	// auraStatCache maps (recipient unit ID, stat id) to the aggregated
	// contribution every covering PerkAura emitter grants that stat this
	// tick — the generic, data-driven aura engine (zealous_march,
	// mana_conduit, sanctuary, guardian_aura all resolve through this one
	// cache; guardian_aura's former hand-written guardianAuraCache was
	// deleted when it migrated). Rebuilt once per tick in
	// rebuildAuraStatCacheLocked (perk_aura_stat_cache.go). Absent key = no
	// covering aura for that stat.
	auraStatCache map[auraStatCacheKey]auraStatContribution

	// pendingDeaths is the per-tick queue of units that hit HP<=0 inside
	// applyUnitDamageWithSourceLocked. Drained at end of each Update() tick by
	// drainPendingDeathsLocked, which runs kill bookkeeping then calls
	// removeUnitLocked. Deduped by UnitID — first-to-kill wins.
	//
	// Why a queue: indirect damage paths (Shared Pain, pain_share redirect,
	// retaliation) kill units OTHER than the primary target. The outer caller
	// doesn't know about those kills. Centralizing here ensures every HP=0 unit
	// gets cleaned up and every attributed kill credits XP/stats correctly.
	pendingDeaths    []pendingDeath
	pendingDeathsSet map[int]bool

	// battleTracker is the debug/telemetry damage-and-kill accumulator. Armed
	// only when MapConfig.Debug.BattleTracker is true; otherwise the tracker
	// is allocated but disabled and every track* call is a no-op. Serialized
	// into MatchSnapshotMessage.BattleTracker (omitted when disabled).
	battleTracker *BattleTracker

	// debugPathTracker is the env-var-gated path-debug analyzer. nil when
	// WEBRTS_DEBUG_PATHING is unset — all methods are no-ops on a nil receiver.
	debugPathTracker *debugPathTracker

	// playersWithTownhall tracks which player IDs have ever owned a townhall,
	// so we can distinguish "never had one yet" from "just lost the last one".
	playersWithTownhall map[string]bool
	// lostPlayerIDs is the set of players whose last townhall has been destroyed.
	// Once set, it is never cleared for the duration of the match.
	lostPlayerIDs map[string]bool

	// joinedTargetLabels is the set of authored player-slot labels (e.g.
	// "player1", "player2") that a real player actually joined into. Recorded
	// once at join time in EnsurePlayerWithUpgrades and never cleared. Used by
	// tickEnemySpawnpointsLocked to distinguish a target-labeled enemy
	// spawnpoint whose player never joined (stay dormant) from one whose
	// player joined and later lost their base (keep firing, re-route to the
	// nearest surviving base). See state_spawn.go's targetPlayerLabel gate.
	joinedTargetLabels map[string]bool

	// victoryAchieved is true once the legacy wave/townhall rule is satisfied
	// AND every required objective in `s.Objectives` is completed. See
	// checkVictoryLocked for the AND-gate. Once set, never cleared for the
	// match. The legacy `objectiveCompleted` and `objectiveKillCounts` maps
	// were removed in §9 of campaign-objectives-and-metrics; per-objective
	// progress now lives on `s.Objectives[i].TeamState` / `.PlayerStates`.
	victoryAchieved bool

	// PlacedEnemiesSpawned is set to true after spawnPlacedEnemyUnitsLocked
	// runs for the first time, so guard units are spawned exactly once per
	// match regardless of how many real players join.
	PlacedEnemiesSpawned bool

	// FOW holds the per-player fog-of-war grid. Keyed by real player ID
	// (never by enemyPlayerID). Initialized in EnsurePlayer and rebuilt
	// each tick by recomputeFOWLocked.
	FOW map[string]*PlayerFOW

	// perkPredicateCacheTick is the s.Tick value at which the BraceActive /
	// InterlockActive caches on UnitPerkState were last rebuilt. The hot
	// snapshot path trusts the cache only when this equals s.Tick (i.e.
	// recomputePerkPredicateCacheLocked has been called THIS tick). Any
	// helper called outside the post-recompute window falls back to a live
	// scan so direct-call tests (which bypass Update) keep working.
	perkPredicateCacheTick int

	// unitSnapshotCacheTick is the s.Tick value at which every
	// Unit.SnapshotCache was last rebuilt. Same freshness contract as
	// perkPredicateCacheTick. -1 sentinel until the first Update runs.
	unitSnapshotCacheTick int

	// unitSpatialIndex is rebuilt once per tick at the top of Update and
	// shared across every subsystem that needs proximity queries against
	// living, visible units (combat AI, perk predicate cache, etc.). Bucket
	// size matches combatSpatialBucketSize so queries with radius ~180px
	// only touch ~3×3 buckets. Replaces the per-subsystem newCombatSpatial-
	// Index calls that used to allocate + rebuild N×subsystems per tick.
	//
	// Reset at the top of Update; readers must treat it as undefined
	// outside the tick (it is a tick-local data structure).
	unitSpatialIndex *combatSpatialIndex

	// workersInsideResource is an incremental counter of units currently
	// MiningInside a given resource node (obstacle ID or building ID). It
	// replaces the per-tick s.Units scan that previously rebuilt these
	// counts in refreshObstacleRuntimeMetadataLocked /
	// refreshBuildingRuntimeMetadataLocked. Maintained by
	// setUnitMiningInsideLocked at every MiningInside flip plus
	// removeUnitByIDLocked for dying miners. Zero entries are deleted so
	// len() stays bounded by active nodes.
	workersInsideResource map[string]int

	// Obstacle delta state — drives the snapshot's ObstaclesRemoved /
	// ObstacleMetadata fields. The full obstacle geometry is sent ONCE in
	// the WelcomeMessage; per-tick snapshots send only changes since the
	// previous broadcast. Eliminates the per-tick retransmit of the entire
	// obstacle array (~870KB on the exploration map at 20Hz → ~17 MB/sec
	// of pure static geometry that previously saturated the Steam relay).
	//
	// pendingObstacleRemovals accumulates IDs removed via
	// removeObstacleByIDLocked between broadcasts; drained at the end of
	// BroadcastSnapshot. lastSentObstacleWorkerCounts records the value
	// each tree obstacle's currentWorkers had in the most recent broadcast
	// so we can emit a metadata patch only when the count actually changed.
	pendingObstacleRemovals      []string
	lastSentObstacleWorkerCounts map[string]int

	// Paused, when true, freezes Update() — no simulation, no wave/upgrade
	// timer progression. Set by any player via HandleSetPause. PausedBy is
	// the player ID that initiated the pause (empty when not paused);
	// snapshot consumers use it to render "Paused by <name>" in multiplayer.
	// pausedAtMs is the wall-clock at which the pause began, so resume can
	// shift wave-upgrade OfferDeadlineMs forward by the elapsed pause window.
	Paused     bool
	PausedBy   string
	pausedAtMs int64

	// onDominionPointDropImmediate is fired by rollDominionPointDropLocked when
	// gameplay tuning's dominionPoints.commitMode == "immediate". The hook MUST
	// be safe to invoke while holding s.mu — implementations are required to
	// be fire-and-forget (spawn a goroutine for the actual profile write);
	// blocking I/O on the tick path is forbidden by AI_RULES.
	// Wired by MatchManager.newMatchLocked; nil in unit tests that do not
	// exercise the immediate-commit path.
	onDominionPointDropImmediate func(playerID string, amount int)

	// recipeCraftedHandler is invoked (fire-and-forget) after a successful craft
	// so the recipe can be recorded to the player's persistent profile. nil in
	// tests that don't set it. Off the tick path by construction (craft is a
	// command). Wired by MatchManager.newMatchLocked.
	recipeCraftedHandler func(playerID, recipeID string)
}

const (
	defaultGoldGatherAmount = 20
	defaultWoodGatherAmount = 15
	goldmineWorkerCap       = 3
	goldmineMiningSeconds   = 5.0
	treeWorkerCap           = 1
	treeChoppingSeconds     = 3.0
	minUnitSpawnSeconds     = 0.25

	// defaultVisionRange is the vision radius (in world pixels) granted to
	// every unit that does not have an explicit visionRange in its UnitDef.
	// Phase 1 value — tuned for the current map scale.
	defaultVisionRange = 400.0
)

// newMatchSeed generates a cryptographically-random int64 seed so concurrent
// match creations never collide on the same nanosecond.
func newMatchSeed() int64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fallback: time-based seed. Collision risk is low in practice but
		// possible under rapid match creation; crypto/rand should never fail.
		return time.Now().UnixNano()
	}
	return int64(binary.LittleEndian.Uint64(b[:]))
}

// NewGameState creates a GameState with a freshly generated per-match seed.
// Call-sites that need a reproducible seed (tests, offline replay) should use
// NewGameStateWithSeed instead.
func NewGameState(mapConfig protocol.MapConfig) *GameState {
	return NewGameStateWithSeed(mapConfig, newMatchSeed())
}

// newMatchItemCatalog builds the per-match item catalog snapshot from the
// merged view (embed + editor overlay) at match-creation time. A running
// match never sees mid-match editor saves (the map is copied once here and
// never mutated again), but new matches see everything the editor has
// registered so far — editor-authored items are equippable/purchasable.
func newMatchItemCatalog() map[string]*ItemDef {
	catalog := make(map[string]*ItemDef, 64)
	for _, def := range ListItemDefs() {
		catalog[def.ID] = def
	}
	return catalog
}

// NewGameStateWithSeed creates a GameState whose RNG streams are derived from
// seed. Use seed == 0 only in tests where you intentionally want the zero seed.
// Each stream gets a distinct salt so they advance independently.
func NewGameStateWithSeed(mapConfig protocol.MapConfig, seed int64) *GameState {
	const (
		saltPerks    int64 = 0x1
		saltCosmetic int64 = 0x2
		saltSpawn    int64 = 0x3
		saltLoot     int64 = 0x4
		saltCombat   int64 = 0x5
	)
	state := &GameState{
		Units:                     []*Unit{},
		Players:                   map[string]*Player{},
		Productions:               map[string][]*UnitProduction{},
		EnemySpawnTimers:          map[string]*EnemySpawnTimer{},
		ActiveUpgrades:            map[string][]*ActiveUpgrade{},
		LootDrops:                 map[string]*LootDrop{},
		nextUnitID:                1,
		nextBannerID:              1,
		nextTrapID:                1,
		nextGroundHazardID:        1,
		nextAbilityZoneID:         1,
		nextAbilityStatusID:       1,
		nextProjectileID:          1,
		matchSeed:                 seed,
		rngPerks:                  mrand.New(mrand.NewSource(seed ^ saltPerks)),
		rngCosmetic:               mrand.New(mrand.NewSource(seed ^ saltCosmetic)),
		rngSpawn:                  mrand.New(mrand.NewSource(seed ^ saltSpawn)),
		rngLoot:                   mrand.New(mrand.NewSource(seed ^ saltLoot)),
		rngCombat:                 mrand.New(mrand.NewSource(seed ^ saltCombat)),
		buildingDamageDealt:       map[string]map[int]int{},
		unitsByID:                 map[int]*Unit{},
		buildingsByID:             map[string]*protocol.BuildingTile{},
		obstaclesByID:             map[string]*protocol.ObstacleTile{},
		auraStatCache:             map[auraStatCacheKey]auraStatContribution{},
		objectiveUnreachableUntil: map[string]int{},
		pendingDeathsSet:          map[int]bool{},
		itemCatalog:               newMatchItemCatalog(),
		FOW:                       map[string]*PlayerFOW{},
		workersInsideResource:     map[string]int{},
		joinedTargetLabels:        map[string]bool{},
		// -1 sentinel: no Update tick has run yet, so any helper that checks
		// "is the predicate cache fresh?" (cacheTick == s.Tick) falls back
		// to a live scan. After the first Update, perkPredicateCacheTick is
		// set to the current tick number by recomputePerkPredicateCacheLocked.
		perkPredicateCacheTick: -1,
		// Same sentinel as perkPredicateCacheTick.
		unitSnapshotCacheTick: -1,
	}

	// Arm the battle tracker iff the map opts in via debug.battleTracker. The
	// tracker is still allocated when disabled so call sites can invoke its
	// methods unconditionally (a nil check + flag check short-circuits cheaply).
	state.battleTracker = newBattleTracker(mapConfig.Debug != nil && mapConfig.Debug.BattleTracker)

	// Path debug tracker — nil when WEBRTS_DEBUG_PATHING is unset (zero cost).
	state.debugPathTracker = newDebugPathTracker()

	state.SetMapConfig(mapConfig)
	return state
}

// MatchSeed returns the root seed used to initialise this match's RNG streams.
// Log this value when creating a match so bug reports can reference it.
func (s *GameState) MatchSeed() int64 {
	return s.matchSeed
}

func (s *GameState) SetMapConfig(mapConfig protocol.MapConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.setMapConfigLocked(mapConfig)
}

func (s *GameState) setMapConfigLocked(mapConfig protocol.MapConfig) {
	s.MapConfig = cloneMapConfig(mapConfig)
	s.MapID = s.MapConfig.ID
	s.MapWidth = s.MapConfig.Width
	s.MapHeight = s.MapConfig.Height
	s.Productions = map[string][]*UnitProduction{}
	s.EnemySpawnTimers = map[string]*EnemySpawnTimer{}
	s.initWaveManagerLocked()
	s.initNeutralCampsLocked()
	s.initShopBuildingsLocked()
	s.populateShopInventoriesLocked()
	s.populateRecipeShopInventoriesLocked()
	s.spawnShopGuardsLocked()

	// Rebuild buildingsByID index from the freshly-cloned Buildings slice.
	s.buildingsByID = make(map[string]*protocol.BuildingTile, len(s.MapConfig.Buildings))
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		s.buildingsByID[b.ID] = b
	}
	s.obstaclesByID = make(map[string]*protocol.ObstacleTile, len(s.MapConfig.Obstacles))
	for i := range s.MapConfig.Obstacles {
		o := &s.MapConfig.Obstacles[i]
		if o.ID == "" {
			continue
		}
		s.obstaclesByID[o.ID] = o
	}
	// Stamp tier=1 on any townhall that lacks the key so townhallTierForPlayerLocked
	// always has a baseline to read.
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType != "townhall" {
			continue
		}
		if b.Metadata == nil {
			b.Metadata = map[string]interface{}{}
		}
		if _, hasTier := b.Metadata["tier"]; !hasTier {
			b.Metadata["tier"] = float64(1)
		}
	}
	// Install zone runtime + cell index from the new map config.
	s.installZonesLocked()
	// Blocked cells derived from this new map config are not yet computed.
	s.invalidateBlockedCellsLocked()
}

// ---- Index helpers -------------------------------------------------------

// invalidateBlockedCellsLocked marks the blocked-cells cache as stale.
// Must be called under s.mu write lock whenever a building is added or
// removed, or when obstacles change.
func (s *GameState) invalidateBlockedCellsLocked() {
	s.blockedCellsValid = false
	s.visionBlockingCache = nil
	s.walkableRegionsCache = nil
}

// getBlockedCellsLocked returns the cached blocked-cells map, rebuilding it
// if the cache is stale. The returned map is read-only; callers must NOT
// mutate it. If a call site needs a mutable copy (e.g. to add reserved
// cells for a single pathing pass), copy the map locally.
// Must be called under s.mu lock (read or write).
func (s *GameState) getBlockedCellsLocked() map[gridPoint]bool {
	if !s.blockedCellsValid {
		s.blockedCellsCache = s.buildBlockedCells()
		s.blockedCellsValid = true
	}
	return s.blockedCellsCache
}

// getVisionBlockingCellsLocked returns the cached vision-blocking map,
// rebuilding it if stale. Obstacles + terrain transitions block LOS; buildings
// do not. The returned map is read-only. Must be called under s.mu lock.
func (s *GameState) getVisionBlockingCellsLocked() map[gridPoint]bool {
	if s.visionBlockingCache == nil {
		s.visionBlockingCache = s.buildVisionBlockingCells()
	}
	return s.visionBlockingCache
}

// buildVisionBlockingCells constructs the set of cells that occlude
// line-of-sight. Includes terrain cliff transitions and all obstacles; does
// NOT include buildings (units can see past buildings).
func (s *GameState) buildVisionBlockingCells() map[gridPoint]bool {
	blocking := make(map[gridPoint]bool)
	addTerrainBlocks(blocking, &s.MapConfig)
	for _, o := range s.MapConfig.Obstacles {
		blocking[gridPoint{X: o.X, Y: o.Y}] = true
	}
	return blocking
}

// addUnitLocked appends unit to s.Units and registers it in s.unitsByID.
// Must be called under s.mu write lock.
func (s *GameState) addUnitLocked(u *Unit) {
	s.Units = append(s.Units, u)
	if s.unitsByID == nil {
		s.unitsByID = make(map[int]*Unit)
	}
	s.unitsByID[u.ID] = u
}

// removeUnitByIDLocked removes the unit with the given ID from both s.Units
// and s.unitsByID. Returns true if the unit was found.
// Must be called under s.mu write lock.
func (s *GameState) removeUnitByIDLocked(id int) bool {
	if u, ok := s.unitsByID[id]; ok && u != nil {
		// Release any incremental counters this unit was contributing to so
		// the next tick's tooltip doesn't lie about node occupancy.
		s.setUnitMiningInsideLocked(u, false)
	}
	delete(s.unitsByID, id)
	filtered := make([]*Unit, 0, len(s.Units))
	found := false
	for _, u := range s.Units {
		if u.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, u)
	}
	s.Units = filtered
	return found
}

// setUnitMiningInsideLocked is the single authoritative writer for
// Unit.MiningInside. It keeps the s.workersInsideResource counter in sync so
// the per-tick metadata refresh can read O(1) instead of scanning all units.
//
// Idempotent: a no-op when the value is unchanged. Increment / decrement are
// keyed on the unit's CURRENT GatherTargetID, so callers that ALSO need to
// clear GatherTargetID must call this BEFORE clearing the field — otherwise
// the decrement loses track of which node the unit was occupying.
//
// Caller holds s.mu.
func (s *GameState) setUnitMiningInsideLocked(unit *Unit, mining bool) {
	if unit == nil || unit.MiningInside == mining {
		return
	}
	if s.workersInsideResource == nil {
		s.workersInsideResource = map[string]int{}
	}
	if mining {
		if unit.GatherTargetID != "" {
			s.workersInsideResource[unit.GatherTargetID]++
		}
	} else {
		if unit.GatherTargetID != "" {
			n := s.workersInsideResource[unit.GatherTargetID] - 1
			if n <= 0 {
				delete(s.workersInsideResource, unit.GatherTargetID)
			} else {
				s.workersInsideResource[unit.GatherTargetID] = n
			}
		}
	}
	unit.MiningInside = mining
}

func (s *GameState) GetMapConfig() protocol.MapConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.MapConfig
}

// MarshalWelcomeMessage builds the join-time welcome envelope (player + match
// IDs + the content-addressed map) and marshals it to JSON under s.mu RLock.
// The map is gzip-embedded only on a cache miss (cachedMapHashes does not
// contain the map's contentHash). Marshaling the MapConfig reads Buildings/
// Obstacles slices whose Metadata maps the tick loop mutates, so it MUST run
// under the lock — same architecture as MarshalSnapshot / MarshalSnapshotForPlayer.
// Returning bytes lets the handler release the lock before the transport write.
func (s *GameState) MarshalWelcomeMessage(playerID, matchID string, cachedMapHashes []string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	welcome := protocol.WelcomeMessage{
		Type:        "welcome",
		PlayerID:    playerID,
		MatchID:     matchID,
		MapID:       s.MapConfig.ID,
		ContentHash: s.MapConfig.ContentHash,
	}
	// Content-addressed: embed the gzipped map only on a cache miss — i.e. when
	// the client did not list this map's contentHash among the versions it
	// already holds (or we have no hash to match on). On a hit the client
	// renders from its own cache and no map bytes cross the wire.
	if hash := s.MapConfig.ContentHash; hash == "" || !containsHash(cachedMapHashes, hash) {
		gz, err := gzipMapConfig(s.MapConfig)
		if err != nil {
			return nil, err
		}
		welcome.MapGz = gz
	}
	return json.Marshal(welcome)
}

// Snapshot builds an unfiltered match snapshot. Used by tests and by the
// join handler to seed a freshly-connected client. For the per-tick
// broadcast hot path, use MarshalSnapshotForPlayer / MarshalSnapshot
// instead: those marshal the snapshot to bytes under the same RLock,
// preventing concurrent tick-loop mutations from racing the JSON encoder
// on shared maps like BuildingTile.Metadata.
func (s *GameState) Snapshot() protocol.MatchSnapshotMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

// MarshalSnapshot builds the unfiltered snapshot AND marshals it to JSON
// bytes while holding s.mu RLock. See SnapshotForPlayer / MarshalSnapshotForPlayer
// for the same pattern on the per-player path.
func (s *GameState) MarshalSnapshot(matchID string, serverNow int64) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := s.snapshotLocked()
	snap.MatchID = matchID
	snap.ServerNow = serverNow
	return json.Marshal(snap)
}

// snapshotLocked is the lock-held body of Snapshot. Caller must hold s.mu
// (RLock is sufficient).
func (s *GameState) snapshotLocked() protocol.MatchSnapshotMessage {
	units := make([]protocol.UnitSnapshot, 0, len(s.Units))
	for _, unit := range s.Units {
		// Effective stats for the HUD: base × rank × path (already in
		// unit.Damage/AttackSpeed/MoveSpeed) × live perk multipliers. Kept
		// target-agnostic (target=nil) so only self-based perk bonuses apply
		// here — per-hit situational bonuses like executioner still live in
		// the combat-resolution path.
		effectiveDamage := s.effectiveDamageForSnapshotLocked(unit, 0.0)
		effectiveAttackSpeed := s.effectiveAttackSpeedForSnapshotLocked(unit)
		effectiveMoveSpeed := s.effectiveMoveSpeedForSnapshotLocked(unit)

		// Crit values are reported with the target left nil — the snapshot
		// shows the unit's own crit chance against an unmarked target so the
		// HUD value is stable. Hunter's Mark contribution shows up in combat,
		// not in the static HUD readout. CritChance/CritMultiplier are 0 for
		// units with no crit sources, which omitempty drops from the wire.
		baseCritChance := s.unitCritChanceForSnapshotLocked(unit)
		critMultiplier := s.unitCritMultiplierForSnapshotLocked(unit, baseCritChance)
		snapshot := protocol.UnitSnapshot{
			ObjectiveID:          unit.ObjectiveID,
			FocusTargetID:        unit.FocusTargetID,
			ID:                   unit.ID,
			OwnerID:              unit.OwnerID,
			Color:                unit.Color,
			UnitType:             unit.UnitType,
			Archetype:            unit.Archetype,
			Name:                 unit.Name,
			Capabilities:         append([]string(nil), unit.Capabilities...),
			Flyer:                unit.Flyer,
			Visible:              unit.Visible,
			Status:               unit.Status,
			Order:                orderTypeString(unit.Order.Type),
			X:                    unit.X,
			Y:                    unit.Y,
			HP:                   unit.HP,
			MaxHP:                unit.MaxHP,
			Damage:               effectiveDamage,
			AttackSpeed:          effectiveAttackSpeed,
			AttackRange:          unit.AttackRange,
			MoveSpeed:            effectiveMoveSpeed,
			Armor:                s.effectiveArmorForSnapshotLocked(unit),
			CritChance:           baseCritChance,
			CritMultiplier:       critMultiplier,
			HealthRegen:          unit.HealthRegenPerSecond,
			XP:                   unit.XP,
			Rank:                 unit.Rank,
			XPToNextRank:         s.unitXPToNextRankForSnapshotLocked(unit),
			XPIntoCurrentRank:    s.unitXPIntoCurrentRankForSnapshotLocked(unit),
			RecentRankUpSeconds:  unit.RankUpFxRemaining,
			ProgressionPath:      unit.ProgressionPath,
			PerkIDs:              unit.PerkIDs,
			ExtraPerkSlots:       s.unitExtraPerkSlotsForSnapshotLocked(unit),
			Shield:               s.unitShieldForSnapshotLocked(unit),
			MaxShield:            s.unitMaxShieldForSnapshotLocked(unit),
			ShieldPools:          s.unitShieldPoolsForSnapshotLocked(unit),
			Mana:                 unit.CurrentMana,
			MaxMana:              unit.MaxMana,
			ManaRegen:            s.effectiveManaRegenLocked(unit),
			ActiveBuffs:          s.activeBuffIconsForSnapshotLocked(unit),
			ActiveDebuffs:        s.activeDebuffIconsForSnapshotLocked(unit),
			PerkCooldowns:        s.perkCooldownsForSnapshotLocked(unit),
			Abilities:            s.abilityStatesForSnapshotLocked(unit),
			StunnedRemaining:     unit.StunnedRemaining,
			SlowedRemaining:      unit.SlowedRemaining,
			SlowedMultiplier:     unit.SlowedMultiplier,
			OverlayColor:         s.unitOverlayColorLocked(unit),
			ArcaneCharge:         unit.ArcaneCharge,
			BurningRemaining:     unit.PerkState.maxBurnRemaining(),
			BurningAnchor:        s.burningOverlayAnchorLocked(unit),
			ChannelLoopStart:     s.channelLoopStartForUnitLocked(unit),
			ChannelLoopEnd:       s.channelLoopEndForUnitLocked(unit),
			CarriedResourceType:  unit.CarriedResourceType,
			CarriedAmount:        unit.CarriedAmount,
			Moving:               unit.Moving,
			ActionFacingDX:       unit.ActionFacingDX,
			ActionFacingDY:       unit.ActionFacingDY,
			RepathCount:          unit.PathDiagnostics.RepathCount,
			StuckTriggerCount:    unit.PathDiagnostics.StuckTriggerCount,
			LastStuckTick:        unit.PathDiagnostics.LastStuckTick,
		}

		if unit.Moving {
			snapshot.TargetX = unit.TargetX
			snapshot.TargetY = unit.TargetY
		}

		// Expose the work target so the client can face the worker sprite
		// toward the exact building it is interacting with.
		if unit.Gathering && unit.GatherTargetID != "" {
			snapshot.WorkTargetID = unit.GatherTargetID
		} else if unit.Building && unit.BuildTargetID != "" {
			snapshot.WorkTargetID = unit.BuildTargetID
		}

		if unit.UnitType == "archer" && unit.ProgressionPath == "trapper" {
			snapshot.EffectiveTrap = s.EffectiveTrapSnapshotLocked(unit)
		}

		snapshot.Inventory = s.unitInventorySnapshotLocked(unit)

		units = append(units, snapshot)
	}

	players := make([]protocol.PlayerSnapshot, 0, len(s.Players))
	for _, player := range s.Players {
		if player.ID == enemyPlayerID {
			continue
		}
		players = append(players, s.buildPlayerSnapshotLocked(player))
	}

	wm := s.WaveManager
	buildings := make([]protocol.BuildingTile, len(s.MapConfig.Buildings))
	copy(buildings, s.MapConfig.Buildings)
	obstaclesRemoved, obstacleMetadata := s.snapshotObstacleDeltasLocked()

	var banners []protocol.BannerSnapshot
	for _, b := range s.Banners {
		banners = append(banners, protocol.BannerSnapshot{
			ID:               b.ID,
			OwnerID:          b.OwnerPlayerID,
			X:                b.X,
			Y:                b.Y,
			Radius:           b.Radius,
			RemainingSeconds: b.RemainingSeconds,
		})
	}

	var projectiles []protocol.ProjectileSnapshot
	for _, proj := range s.Projectiles {
		progress := 0.0
		if proj.TotalSeconds > 0 {
			progress = 1.0 - (proj.RemainingSeconds / proj.TotalSeconds)
			if progress < 0 {
				progress = 0
			} else if progress > 1 {
				progress = 1
			}
		}
		projectiles = append(projectiles, protocol.ProjectileSnapshot{
			ID:               proj.ID,
			OwnerUnitID:      proj.OwnerUnitID,
			OwnerID:          proj.OwnerPlayerID,
			TargetUnitID:     proj.TargetUnitID,
			OriginUnitID:     proj.OriginUnitID,
			OriginX:          proj.OriginX,
			OriginY:          proj.OriginY,
			TargetX:          proj.TargetX,
			TargetY:          proj.TargetY,
			Progress:         progress,
			Variant:          proj.Variant,
			DoubleShotSecond: proj.DoubleShotSecond,
			Pierce:           proj.Pierce,
			Scale:            proj.Scale,
		})
	}

	var traps []protocol.TrapSnapshot
	for _, trap := range s.Traps {
		// Hide explosive traps from the client once they've detonated but
		// still have follow-up events queued: the silent window before a
		// chain aftershock fires, and the window before any pending
		// Cataclysm secondary explosions fire. The initial-blast tick is
		// kept visible — that frame has Triggered=true and plays the
		// trap's explode animation — but afterward the trap should be
		// gone from view; chain/Cataclysm secondaries render via
		// sprite-based "explosion" EffectSnapshots independently.
		if !trap.Triggered && (trap.AftershockPending || len(trap.PendingCataclysms) > 0) {
			continue
		}
		traps = append(traps, protocol.TrapSnapshot{
			ID:               trap.ID,
			OwnerID:          trap.OwnerPlayerID,
			X:                trap.X,
			Y:                trap.Y,
			Radius:           trap.Radius,
			TriggerRadius:    trap.TriggerRadius, // explosive_trap only; 0 for others (omitted over the wire)
			Variant:          trapVisualVariant(trap),
			ScaleMultiplier:  trapVisualScaleMultiplier(trap),
			Type:             trap.TrapType,
			RemainingSeconds: trap.RemainingSeconds,
			Triggered:        trap.Triggered, // one-tick VFX flash flag (fires on every detonation)
		})
	}
	// Visible ability zones ride the same array (see visibleZoneSnapshotsLocked).
	traps = append(traps, s.visibleZoneSnapshotsLocked()...)

	var gameOver *protocol.GameOverSnapshot
	if len(s.lostPlayerIDs) > 0 {
		ids := make([]string, 0, len(s.lostPlayerIDs))
		for id := range s.lostPlayerIDs {
			ids = append(ids, id)
		}
		gameOver = &protocol.GameOverSnapshot{LostPlayerIDs: ids}
	}

	// Broadcast / unfiltered snapshot: no viewer identity available here.
	// Team-scope objectives use TeamState (correct for any viewer);
	// player-scope objectives fall back to initial state (Current=0). The
	// per-viewer caller in snapshotForPlayerLocked patches `snap.Victory`
	// with the viewer-specific copy after the unfiltered snapshot is built.
	victory := s.buildVictorySnapshotForViewerLocked("")

	beams := s.beamSnapshotsLocked(nil)

	return protocol.MatchSnapshotMessage{
		Type:               "match_snapshot",
		Tick:               s.Tick,
		ServerNow:          time.Now().UnixMilli(),
		Buildings:          buildings,
		ObstaclesRemoved:   obstaclesRemoved,
		ObstacleMetadata:   obstacleMetadata,
		Players:            players,
		Units:              units,
		Corpses:            s.corpseSnapshotsLocked(nil, ""),
		Banners:            banners,
		Traps:              traps,
		Projectiles:        projectiles,
		Beams:              beams,
		Effects:            s.effectSnapshotsLocked(),
		CritEvents:         s.snapshotCritEventsLocked(),
		MeleeAttackEvents:  s.snapshotMeleeAttackEventsLocked(),
		MinorDamageEvents:  s.snapshotMinorDamageEventsLocked(),
		EvadeEvents:        s.snapshotEvadeEventsLocked(),
		HitDamageEvents:    s.snapshotHitDamageEventsLocked(),
		DamageTypeHints:    s.snapshotDamageTypeHintsLocked(),
		LethalDamageEvents: s.snapshotLethalDamageEventsLocked(),
		HealEvents:         s.snapshotHealEventsLocked(),
		ManaRestoreEvents:  s.snapshotManaRestoreEventsLocked(),
		Wave: protocol.WaveSnapshot{
			Enabled:      wm.Enabled,
			CurrentWave:  wm.CurrentWave,
			TotalWaves:   wm.TotalWaves,
			State:        wm.State,
			Timer:        wm.Timer,
			WaveDuration: wm.WaveDuration,
		},
		// Nil when debug tracker is disabled — `omitempty` drops it from JSON.
		BattleTracker:          s.battleTrackerSnapshotLocked(),
		GameOver:               gameOver,
		Victory:                victory,
		Paused:                 s.Paused,
		PausedBy:               s.PausedBy,
		PersistentlyStuckUnits: s.persistentlyStuckUnitsLocked(),
		NeutralCamps:           s.neutralCampSnapshotsLocked(),
		LootDrops:              s.lootDropSnapshotsLocked(),
		Zones:                  s.zoneSnapshotsLocked(),
	}
}

// buildWaveUpgradeSnapshotLocked returns the per-player upgrade offer snapshot
// for viewerID, or nil when there is no pending offer for that player.
// Caller must hold s.mu (read lock is sufficient).
func (s *GameState) buildWaveUpgradeSnapshotLocked(viewerID string) *protocol.WaveUpgradeOfferSnapshot {
	if s.WaveManager.State != "upgrade" {
		return nil
	}
	player := s.Players[viewerID]
	if player == nil || player.UpgradeState.Resolved {
		return nil
	}
	offers := make([]protocol.UpgradeOffer, 0, len(player.UpgradeState.CurrentOffers))
	for _, def := range player.UpgradeState.CurrentOffers {
		stackCurrent := 0
		stackMax := 0
		if !def.Unlimited {
			stackMax = def.MaxStacks
			if player.UpgradeState.MaxUpgradeStacks > stackMax {
				stackMax = player.UpgradeState.MaxUpgradeStacks
			}
			stackCurrent = player.UpgradeState.UpgradeStacks[def.Group]
		}
		offers = append(offers, protocol.UpgradeOffer{
			ID:                 def.ID,
			Group:              def.Group,
			Name:               def.Name,
			Description:        def.Description,
			Rarity:             def.Rarity,
			Scope:              def.Scope,
			StackCurrent:       stackCurrent,
			StackMax:           stackMax,
			RequiresTargetUnit: def.RequiresTargetUnit(),
		})
	}
	// The start-of-match bonus is offered at CurrentWave == 0 (before wave 1);
	// display it as "Wave 1" so the modal header reads sensibly. During a real
	// upgrade phase CurrentWave is always >= 1, so this is a no-op there.
	displayWave := s.WaveManager.CurrentWave
	if displayWave < 1 {
		displayWave = 1
	}
	return &protocol.WaveUpgradeOfferSnapshot{
		Wave:        displayWave,
		Offers:      offers,
		RerollsLeft: player.UpgradeState.RerollsRemaining,
		DeadlineMs:  player.UpgradeState.OfferDeadlineMs,
	}
}

// SnapshotForPlayer builds a match snapshot filtered through the FOW grid for
// viewerID. Units, buildings, projectiles, traps, effects, and banners outside
// the viewer's vision are excluded or replaced with ghost entries. Returns the
// unfiltered Snapshot() result when the viewer has no FOW entry (e.g. spectator).
//
// Production hot path: prefer MarshalSnapshotForPlayer, which marshals the
// snapshot to bytes under the same RLock. Several wire structs (notably
// BuildingTile.Metadata) intentionally alias live state maps, so the encoder
// MUST run under the lock or it will race with tick-loop mutations and panic
// inside encoding/json's mapEncoder.
func (s *GameState) SnapshotForPlayer(viewerID string) protocol.MatchSnapshotMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotForPlayerLocked(viewerID)
}

// MarshalSnapshotForPlayer builds the per-player snapshot AND marshals it to
// JSON bytes while holding s.mu RLock. Returning bytes (not the snapshot
// struct) lets the broadcast loop release the lock before the transport
// write, while still keeping json.Marshal — which iterates the snapshot's
// map fields via reflection — safely inside the lock. matchID and
// serverNow are stamped before marshaling so the caller doesn't have to
// mutate the bytes.
func (s *GameState) MarshalSnapshotForPlayer(viewerID, matchID string, serverNow int64) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := s.snapshotForPlayerLocked(viewerID)
	snap.MatchID = matchID
	snap.ServerNow = serverNow
	return json.Marshal(snap)
}

// snapshotForPlayerLocked is the lock-held body of SnapshotForPlayer.
// Caller must hold s.mu (RLock is sufficient).
func (s *GameState) snapshotForPlayerLocked(viewerID string) protocol.MatchSnapshotMessage {
	fow := s.FOW[viewerID]

	if fow == nil {
		// No FOW for this viewer — return the full unfiltered snapshot.
		// We cannot call s.Snapshot() here because it acquires RLock again;
		// build inline using the same lock we already hold.
		snap := s.snapshotUnfilteredLocked()
		snap.WaveUpgrade = s.buildWaveUpgradeSnapshotLocked(viewerID)
		// snapshotUnfilteredLocked populated Victory with viewerID="" (no
		// per-viewer identity); override with this viewer's player-scope
		// objective state. Team-scope objectives are unchanged.
		snap.Victory = s.buildVictorySnapshotForViewerLocked(viewerID)
		if snap.GameOver != nil {
			snap.GameOver.YourDominionPointsEarned = s.viewerDominionPointsEarnedLocked(viewerID)
		}
		return snap
	}

	cellSize := s.MapConfig.CellSize

	units := make([]protocol.UnitSnapshot, 0, len(s.Units))
	for _, unit := range s.Units {
		isOwn := unit.OwnerID == viewerID
		if !isOwn && !fow.isClearAtWorld(unit.X, unit.Y, cellSize) {
			continue
		}

		effectiveDamage := s.effectiveDamageForSnapshotLocked(unit, 0.0)
		effectiveAttackSpeed := s.effectiveAttackSpeedForSnapshotLocked(unit)
		effectiveMoveSpeed := s.effectiveMoveSpeedForSnapshotLocked(unit)

		baseCritChance := s.unitCritChanceForSnapshotLocked(unit)
		critMultiplier := s.unitCritMultiplierForSnapshotLocked(unit, baseCritChance)
		snapshot := protocol.UnitSnapshot{
			ObjectiveID:          unit.ObjectiveID,
			FocusTargetID:        unit.FocusTargetID,
			ID:                   unit.ID,
			OwnerID:              unit.OwnerID,
			Color:                unit.Color,
			UnitType:             unit.UnitType,
			Archetype:            unit.Archetype,
			Name:                 unit.Name,
			Capabilities:         append([]string(nil), unit.Capabilities...),
			Flyer:                unit.Flyer,
			Visible:              unit.Visible,
			Status:               unit.Status,
			Order:                orderTypeString(unit.Order.Type),
			X:                    unit.X,
			Y:                    unit.Y,
			HP:                   unit.HP,
			MaxHP:                unit.MaxHP,
			Damage:               effectiveDamage,
			AttackSpeed:          effectiveAttackSpeed,
			AttackRange:          unit.AttackRange,
			MoveSpeed:            effectiveMoveSpeed,
			Armor:                s.effectiveArmorForSnapshotLocked(unit),
			CritChance:           baseCritChance,
			CritMultiplier:       critMultiplier,
			HealthRegen:          unit.HealthRegenPerSecond,
			XP:                   unit.XP,
			Rank:                 unit.Rank,
			XPToNextRank:         s.unitXPToNextRankForSnapshotLocked(unit),
			XPIntoCurrentRank:    s.unitXPIntoCurrentRankForSnapshotLocked(unit),
			RecentRankUpSeconds:  unit.RankUpFxRemaining,
			ProgressionPath:      unit.ProgressionPath,
			PerkIDs:              unit.PerkIDs,
			ExtraPerkSlots:       s.unitExtraPerkSlotsForSnapshotLocked(unit),
			Shield:               s.unitShieldForSnapshotLocked(unit),
			MaxShield:            s.unitMaxShieldForSnapshotLocked(unit),
			ShieldPools:          s.unitShieldPoolsForSnapshotLocked(unit),
			Mana:                 unit.CurrentMana,
			MaxMana:              unit.MaxMana,
			ManaRegen:            s.effectiveManaRegenLocked(unit),
			ActiveBuffs:          s.activeBuffIconsForSnapshotLocked(unit),
			ActiveDebuffs:        s.activeDebuffIconsForSnapshotLocked(unit),
			PerkCooldowns:        s.perkCooldownsForSnapshotLocked(unit),
			Abilities:            s.abilityStatesForSnapshotLocked(unit),
			StunnedRemaining:     unit.StunnedRemaining,
			SlowedRemaining:      unit.SlowedRemaining,
			SlowedMultiplier:     unit.SlowedMultiplier,
			OverlayColor:         s.unitOverlayColorLocked(unit),
			ArcaneCharge:         unit.ArcaneCharge,
			BurningRemaining:     unit.PerkState.maxBurnRemaining(),
			BurningAnchor:        s.burningOverlayAnchorLocked(unit),
			ChannelLoopStart:     s.channelLoopStartForUnitLocked(unit),
			ChannelLoopEnd:       s.channelLoopEndForUnitLocked(unit),
			CarriedResourceType:  unit.CarriedResourceType,
			CarriedAmount:        unit.CarriedAmount,
			Moving:               unit.Moving,
			ActionFacingDX:       unit.ActionFacingDX,
			ActionFacingDY:       unit.ActionFacingDY,
			RepathCount:          unit.PathDiagnostics.RepathCount,
			StuckTriggerCount:    unit.PathDiagnostics.StuckTriggerCount,
			LastStuckTick:        unit.PathDiagnostics.LastStuckTick,
		}
		if unit.Moving {
			snapshot.TargetX = unit.TargetX
			snapshot.TargetY = unit.TargetY
		}
		if unit.Gathering && unit.GatherTargetID != "" {
			snapshot.WorkTargetID = unit.GatherTargetID
		} else if unit.Building && unit.BuildTargetID != "" {
			snapshot.WorkTargetID = unit.BuildTargetID
		}
		if unit.UnitType == "archer" && unit.ProgressionPath == "trapper" {
			snapshot.EffectiveTrap = s.EffectiveTrapSnapshotLocked(unit)
		}
		snapshot.Inventory = s.unitInventorySnapshotLocked(unit)
		units = append(units, snapshot)
	}

	players := make([]protocol.PlayerSnapshot, 0, len(s.Players))
	for _, player := range s.Players {
		if player.ID == enemyPlayerID {
			continue
		}
		players = append(players, s.buildPlayerSnapshotLocked(player))
	}

	wm := s.WaveManager
	obstaclesRemoved, obstacleMetadata := s.snapshotObstacleDeltasLocked()

	// Filter buildings: own→always, clear→as-is, known→ghost, else drop.
	// Viewer's independent per-player neutral-shop views (buildingID → stock).
	// A neutral shop's snapshot ShopInventory is replaced with the viewer's own
	// view so each player sees (and spends from) their own stock; the shared
	// BuildingTile.ShopInventory remains only as a fallback when a view is absent.
	var viewerShopViews map[string][]protocol.ShopStockEntry
	if vp, ok := s.Players[viewerID]; ok {
		viewerShopViews = vp.NeutralShopInventories
	}
	applyNeutralShopView := func(tile *protocol.BuildingTile) {
		if tile.OwnerID == nil || *tile.OwnerID != neutralPlayerID {
			return
		}
		if view, has := viewerShopViews[tile.ID]; has {
			tile.ShopInventory = view
		}
	}

	buildings := make([]protocol.BuildingTile, 0, len(s.MapConfig.Buildings))
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		isOwn := b.OwnerID != nil && *b.OwnerID == viewerID
		isShop := isShopSnapshotBuilding(b)
		_, knownToViewer := fow.KnownBuildings[b.ID]
		if isOwn {
			tile := *b
			if isShop {
				tile.ShopLocked = s.shopLockedLocked(b)
				tile.ShopDiscovered = true
				tile.ShopDisplayName = shopDisplayNameFor(b)
			}
			buildings = append(buildings, tile)
			continue
		}
		if fow.anyFootprintClear(b) {
			tile := *b
			if isShop {
				tile.ShopLocked = s.shopLockedLocked(b)
				tile.ShopDiscovered = knownToViewer || true // currently visible ⇒ discovered
				tile.ShopDisplayName = shopDisplayNameFor(b)
				applyNeutralShopView(&tile)
			}
			buildings = append(buildings, tile)
			continue
		}
		if known, ok := fow.KnownBuildings[b.ID]; ok {
			ghost := *known
			ghost.Ghost = true
			ghost.LastSeenTick = s.Tick
			if isShop {
				ghost.ShopLocked = s.shopLockedLocked(b)
				ghost.ShopDiscovered = true
				ghost.ShopDisplayName = shopDisplayNameFor(b)
				applyNeutralShopView(&ghost)
			}
			buildings = append(buildings, ghost)
		}
	}

	var banners []protocol.BannerSnapshot
	for _, b := range s.Banners {
		if b.OwnerPlayerID == viewerID || fow.isClearAtWorld(b.X, b.Y, cellSize) {
			banners = append(banners, protocol.BannerSnapshot{
				ID:               b.ID,
				OwnerID:          b.OwnerPlayerID,
				X:                b.X,
				Y:                b.Y,
				Radius:           b.Radius,
				RemainingSeconds: b.RemainingSeconds,
			})
		}
	}

	var projectiles []protocol.ProjectileSnapshot
	for _, proj := range s.Projectiles {
		if !fow.isClearAtWorld(proj.OriginX, proj.OriginY, cellSize) &&
			!fow.isClearAtWorld(proj.TargetX, proj.TargetY, cellSize) {
			continue
		}
		progress := 0.0
		if proj.TotalSeconds > 0 {
			progress = 1.0 - (proj.RemainingSeconds / proj.TotalSeconds)
			if progress < 0 {
				progress = 0
			} else if progress > 1 {
				progress = 1
			}
		}
		projectiles = append(projectiles, protocol.ProjectileSnapshot{
			ID:               proj.ID,
			OwnerUnitID:      proj.OwnerUnitID,
			OwnerID:          proj.OwnerPlayerID,
			TargetUnitID:     proj.TargetUnitID,
			OriginUnitID:     proj.OriginUnitID,
			OriginX:          proj.OriginX,
			OriginY:          proj.OriginY,
			TargetX:          proj.TargetX,
			TargetY:          proj.TargetY,
			Progress:         progress,
			Variant:          proj.Variant,
			DoubleShotSecond: proj.DoubleShotSecond,
			Pierce:           proj.Pierce,
			Scale:            proj.Scale,
		})
	}

	var traps []protocol.TrapSnapshot
	for _, trap := range s.Traps {
		if !trap.Triggered && (trap.AftershockPending || len(trap.PendingCataclysms) > 0) {
			continue
		}
		isOwn := trap.OwnerPlayerID == viewerID
		if !isOwn && !fow.isClearAtWorld(trap.X, trap.Y, cellSize) {
			continue
		}
		traps = append(traps, protocol.TrapSnapshot{
			ID:               trap.ID,
			OwnerID:          trap.OwnerPlayerID,
			X:                trap.X,
			Y:                trap.Y,
			Radius:           trap.Radius,
			TriggerRadius:    trap.TriggerRadius,
			Variant:          trapVisualVariant(trap),
			ScaleMultiplier:  trapVisualScaleMultiplier(trap),
			Type:             trap.TrapType,
			RemainingSeconds: trap.RemainingSeconds,
			Triggered:        trap.Triggered,
		})
	}
	// Visible ability zones ride the same array (see visibleZoneSnapshotsLocked)
	// and get the identical own-or-revealed fog test the traps above get.
	for _, zs := range s.visibleZoneSnapshotsLocked() {
		if zs.OwnerID != viewerID && !fow.isClearAtWorld(zs.X, zs.Y, cellSize) {
			continue
		}
		traps = append(traps, zs)
	}

	var effects []protocol.EffectSnapshot
	for _, e := range s.effectSnapshotsLocked() {
		if fow.isClearAtWorld(e.X, e.Y, cellSize) {
			effects = append(effects, e)
		}
	}

	var gameOver *protocol.GameOverSnapshot
	if len(s.lostPlayerIDs) > 0 {
		ids := make([]string, 0, len(s.lostPlayerIDs))
		for id := range s.lostPlayerIDs {
			ids = append(ids, id)
		}
		gameOver = &protocol.GameOverSnapshot{LostPlayerIDs: ids}
		gameOver.YourDominionPointsEarned = s.viewerDominionPointsEarnedLocked(viewerID)
	}

	// Per-viewer Victory snapshot: team-scope objectives read TeamState
	// (identical for every viewer); player-scope objectives read this
	// viewer's PlayerStates entry. Returns nil when no objectives are
	// installed (Custom Game), keeping the wire compact.
	victory := s.buildVictorySnapshotForViewerLocked(viewerID)

	beams := s.beamSnapshotsLocked(fow)

	return protocol.MatchSnapshotMessage{
		Type:               "match_snapshot",
		Tick:               s.Tick,
		ServerNow:          time.Now().UnixMilli(),
		Buildings:          buildings,
		ObstaclesRemoved:   obstaclesRemoved,
		ObstacleMetadata:   obstacleMetadata,
		Players:            players,
		Units:              units,
		Corpses:            s.corpseSnapshotsLocked(fow, viewerID),
		Banners:            banners,
		Traps:              traps,
		Projectiles:        projectiles,
		Beams:              beams,
		Effects:            effects,
		CritEvents:         s.snapshotCritEventsLocked(),
		MeleeAttackEvents:  s.snapshotMeleeAttackEventsLocked(),
		MinorDamageEvents:  s.snapshotMinorDamageEventsLocked(),
		EvadeEvents:        s.snapshotEvadeEventsLocked(),
		HitDamageEvents:    s.snapshotHitDamageEventsLocked(),
		DamageTypeHints:    s.snapshotDamageTypeHintsLocked(),
		LethalDamageEvents: s.snapshotLethalDamageEventsLocked(),
		HealEvents:         s.snapshotHealEventsLocked(),
		ManaRestoreEvents:  s.snapshotManaRestoreEventsLocked(),
		Wave: protocol.WaveSnapshot{
			Enabled:      wm.Enabled,
			CurrentWave:  wm.CurrentWave,
			TotalWaves:   wm.TotalWaves,
			State:        wm.State,
			Timer:        wm.Timer,
			WaveDuration: wm.WaveDuration,
		},
		BattleTracker:          s.battleTrackerSnapshotLocked(),
		GameOver:               gameOver,
		Victory:                victory,
		Fow:                    packFOW(fow, s.Tick),
		WaveUpgrade:            s.buildWaveUpgradeSnapshotLocked(viewerID),
		Paused:                 s.Paused,
		PausedBy:               s.PausedBy,
		PersistentlyStuckUnits: s.persistentlyStuckUnitsLocked(),
		NeutralCamps:           s.neutralCampSnapshotsLocked(),
		LootDrops:              s.lootDropSnapshotsLocked(),
		Zones:                  s.zoneSnapshotsLocked(),
	}
}

// persistentlyStuckUnitsLocked returns the IDs of units whose stuck watchdog
// has fired 4 or more times in the current wave. Used by the debug snapshot to
// surface units that need investigation. Threshold mirrors the diagnostics spec
// (`pathing-diagnostics`: 4+ triggers in the current wave). Returns nil when no
// unit meets the threshold so omitempty drops the field from the wire.
func (s *GameState) persistentlyStuckUnitsLocked() []int {
	var ids []int
	for _, unit := range s.Units {
		if unit == nil {
			continue
		}
		if unit.PathDiagnostics.StuckTriggerCount >= 4 {
			ids = append(ids, unit.ID)
		}
	}
	return ids
}

// snapshotUnfilteredLocked builds the full unfiltered snapshot. Caller must hold
// s.mu in at least read mode. This is the internal version of Snapshot() that
// does not acquire the lock itself, used by SnapshotForPlayer when the viewer
// has no FOW entry.
// recomputeUnitSnapshotCacheLocked refreshes every Unit.SnapshotCache so the
// per-viewer snapshot loop reads constant-time fields instead of paying the
// 13 perk-hook calls per unit per viewer that the historical code did.
//
// Called once per tick at the end of Update, after positions and predicates
// have settled. Sets unitSnapshotCacheTick = s.Tick so readers know the
// cache is fresh; helpers called outside this window fall back to live
// recomputation to preserve direct-call test behaviour.
//
// Caller holds s.mu.
func (s *GameState) recomputeUnitSnapshotCacheLocked() {
	s.unitSnapshotCacheTick = s.Tick
	for _, unit := range s.Units {
		if unit == nil {
			continue
		}
		unit.SnapshotCache.EffectiveDamage = int(math.Round(s.effectiveDamageRawLocked(unit, 0)))
		unit.SnapshotCache.EffectiveAttackSpeed = math.Max(0.1, unit.AttackSpeed+s.perkAttackSpeedBonusLocked(unit))
		unit.SnapshotCache.EffectiveMoveSpeed = unit.MoveSpeed * s.perkMoveSpeedMultiplierLocked(unit)
		unit.SnapshotCache.EffectiveArmor = s.effectiveArmorLocked(unit)
		unit.SnapshotCache.MaxShield = s.unitMaxShieldLocked(unit)
		baseCritChance := s.unitCritChanceLocked(unit, nil)
		unit.SnapshotCache.CritChance = baseCritChance
		critMult := 0.0
		if baseCritChance > 0 || s.perkCritMultiplierBonusLocked(unit) > 0 {
			critMult = s.unitCritMultiplierLocked(unit)
		}
		unit.SnapshotCache.CritMultiplier = critMult
		unit.SnapshotCache.ActiveBuffs = s.activeBuffIconsLocked(unit)
		unit.SnapshotCache.ActiveDebuffs = s.activeDebuffIconsLocked(unit)
		unit.SnapshotCache.PerkCooldowns = s.perkCooldownsLocked(unit)
		unit.SnapshotCache.Abilities = s.abilityStatesLocked(unit)
		unit.SnapshotCache.XPToNextRank = s.unitXPToNextRankLocked(unit)
		unit.SnapshotCache.XPIntoCurrentRank = s.unitXPIntoCurrentRankLocked(unit)
	}
}

// effectiveDamageRawLocked computes the HUD-facing effective damage for unit
// as a float (pre-rounding), for a nil-target context (Snapshot() has no
// live combat target). extraMult is an additional additive bonus folded into
// the same (1 + …) pool as perkBonusDamageMultiplierLocked's Go-handler
// perks (executioner, berserk_state); pass 0 when there is none.
//
// Mirrors state_combat.go's applyDelayedAttackLocked pre-crit raw-damage
// calc, minus the zone-aura fold (the HUD number has never included zone
// auras — that is a pre-existing scope gap this task does not change, not a
// regression introduced here). Data-driven perk stat modifiers on "damage"
// (PerkStatModifier{Stat:"damage"}, e.g. hawk_spirit's/vulture_spirit's
// intrinsic-stage multiplier) fold in via unitPerkStatModifiersLocked so the
// HUD keeps reflecting them now that they no longer flow through
// perkBonusDamageMultiplierLocked.
func (s *GameState) effectiveDamageRawLocked(unit *Unit, extraMult float64) float64 {
	raw := float64(unit.Damage) * (1.0 + s.perkBonusDamageMultiplierLocked(unit, nil) + extraMult)
	// Perk + status "damage" pool (unitStatStagesLocked), NO zone-aura merge —
	// the HUD number has never included zone auras (a pre-existing scope gap, not
	// a regression), so this deliberately uses the perk+status half of the
	// chokepoint rather than effectiveStatLocked. Keeps the HUD reflecting a
	// status-authored damage change_stat, byte-identical today (no status authors
	// "damage").
	if stages := s.unitStatStagesLocked(unit, statDamage); len(stages) > 0 {
		raw = applyStatStages(raw, stages)
	}
	return raw
}

// effectiveDamageForSnapshotLocked returns the effective damage to display
// for a unit, preferring the per-tick SnapshotCache when fresh. extraMult is
// the global enemy-facing bonus that the cache already folded in; pass 0 to
// skip re-folding (the cached value already includes it).
func (s *GameState) effectiveDamageForSnapshotLocked(unit *Unit, extraMult float64) int {
	if s.unitSnapshotCacheTick == s.Tick {
		return unit.SnapshotCache.EffectiveDamage
	}
	return int(math.Round(s.effectiveDamageRawLocked(unit, extraMult)))
}

// effectiveAttackSpeedForSnapshotLocked mirrors effectiveDamageForSnapshotLocked.
func (s *GameState) effectiveAttackSpeedForSnapshotLocked(unit *Unit) float64 {
	if s.unitSnapshotCacheTick == s.Tick {
		return unit.SnapshotCache.EffectiveAttackSpeed
	}
	return math.Max(0.1, unit.AttackSpeed+s.perkAttackSpeedBonusLocked(unit))
}

func (s *GameState) effectiveMoveSpeedForSnapshotLocked(unit *Unit) float64 {
	if s.unitSnapshotCacheTick == s.Tick {
		return unit.SnapshotCache.EffectiveMoveSpeed
	}
	return unit.MoveSpeed * s.perkMoveSpeedMultiplierLocked(unit)
}

func (s *GameState) effectiveArmorForSnapshotLocked(unit *Unit) int {
	if s.unitSnapshotCacheTick == s.Tick {
		return unit.SnapshotCache.EffectiveArmor
	}
	return s.effectiveArmorLocked(unit)
}

func (s *GameState) unitMaxShieldForSnapshotLocked(unit *Unit) int {
	// Returns the AGGREGATE max-shield (legacy blood_engine pool + every
	// source-specific pool on PerkState). The cached MaxShield value covers
	// the legacy slice only — pool totals are summed live because pools mutate
	// inside the tick (dark_renewal pulses) and a cache-tick race would
	// surface stale numbers in the HUD.
	legacy := unit.SnapshotCache.MaxShield
	if s.unitSnapshotCacheTick != s.Tick {
		legacy = s.unitMaxShieldLocked(unit)
	}
	return legacy + totalMaxShieldFromPoolsLocked(unit)
}

// unitShieldForSnapshotLocked is the analogue for the CURRENT shield value:
// legacy Unit.Shield (blood_engine) plus the sum of every source-specific
// shield pool. Used by every snapshot builder so the HUD line "Shield: X / Y"
// reflects the combined value rather than just the legacy pool.
//
// Caller holds s.mu (read or write).
func (s *GameState) unitShieldForSnapshotLocked(unit *Unit) int {
	if unit == nil {
		return 0
	}
	return unit.Shield + totalShieldFromPoolsLocked(unit)
}

// unitShieldPoolsForSnapshotLocked converts the unit's source-specific shield
// pools into the wire-format slice the client renders in the unit-info
// tooltip. The aggregate Shield / MaxShield fields on UnitSnapshot still
// hold the totals; this slice is just the per-source breakdown so the HUD
// can show "Dark Renewal: 20/40" etc.
//
// The legacy single-pool Unit.Shield (blood_engine) is NOT emitted here —
// it's already represented in the aggregate Shield / MaxShield numbers and
// has no per-source identity worth surfacing for now (blood_engine is the
// only contributor today). When more legacy contributors land, finish
// migrating them into the pool system rather than synthesising entries here.
//
// Returns nil for units with no source-specific pools so the omitempty tag
// drops the field from the wire entirely.
//
// Caller holds s.mu (read or write).
func (s *GameState) unitShieldPoolsForSnapshotLocked(unit *Unit) []protocol.ShieldPoolSnapshot {
	if unit == nil || len(unit.PerkState.ShieldPools) == 0 {
		return nil
	}
	out := make([]protocol.ShieldPoolSnapshot, 0, len(unit.PerkState.ShieldPools))
	for i := range unit.PerkState.ShieldPools {
		p := &unit.PerkState.ShieldPools[i]
		if p.CurrentValue <= 0 && p.MaxValue <= 0 {
			continue // defensive: skip degenerate / fully-drained pools mid-cleanup
		}
		// Copy tags so the wire slice does not alias UnitPerkState memory —
		// the snapshot may be serialised after the tick lock is released.
		var tags []string
		if len(p.Tags) > 0 {
			tags = append(tags, p.Tags...)
		}
		out = append(out, protocol.ShieldPoolSnapshot{
			SourceType:   p.SourceType,
			SourceUnitID: p.SourceUnitID,
			Current:      p.CurrentValue,
			Max:          p.MaxValue,
			Tags:         tags,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *GameState) unitCritChanceForSnapshotLocked(unit *Unit) float64 {
	if s.unitSnapshotCacheTick == s.Tick {
		return unit.SnapshotCache.CritChance
	}
	return s.unitCritChanceLocked(unit, nil)
}

func (s *GameState) unitCritMultiplierForSnapshotLocked(unit *Unit, baseCritChance float64) float64 {
	if s.unitSnapshotCacheTick == s.Tick {
		return unit.SnapshotCache.CritMultiplier
	}
	if baseCritChance > 0 || s.perkCritMultiplierBonusLocked(unit) > 0 {
		return s.unitCritMultiplierLocked(unit)
	}
	return 0
}

func (s *GameState) activeBuffIconsForSnapshotLocked(unit *Unit) []protocol.ActiveEffectIcon {
	if s.unitSnapshotCacheTick == s.Tick {
		return unit.SnapshotCache.ActiveBuffs
	}
	return s.activeBuffIconsLocked(unit)
}

func (s *GameState) activeDebuffIconsForSnapshotLocked(unit *Unit) []protocol.ActiveEffectIcon {
	if s.unitSnapshotCacheTick == s.Tick {
		return unit.SnapshotCache.ActiveDebuffs
	}
	return s.activeDebuffIconsLocked(unit)
}

func (s *GameState) perkCooldownsForSnapshotLocked(unit *Unit) []protocol.PerkCooldownSnapshot {
	if s.unitSnapshotCacheTick == s.Tick {
		return unit.SnapshotCache.PerkCooldowns
	}
	return s.perkCooldownsLocked(unit)
}

func (s *GameState) abilityStatesForSnapshotLocked(unit *Unit) []protocol.AbilitySnapshot {
	if s.unitSnapshotCacheTick == s.Tick {
		return unit.SnapshotCache.Abilities
	}
	return s.abilityStatesLocked(unit)
}

func (s *GameState) unitXPToNextRankForSnapshotLocked(unit *Unit) int {
	if s.unitSnapshotCacheTick == s.Tick {
		return unit.SnapshotCache.XPToNextRank
	}
	return s.unitXPToNextRankLocked(unit)
}

func (s *GameState) unitXPIntoCurrentRankForSnapshotLocked(unit *Unit) int {
	if s.unitSnapshotCacheTick == s.Tick {
		return unit.SnapshotCache.XPIntoCurrentRank
	}
	return s.unitXPIntoCurrentRankLocked(unit)
}

// unitExtraPerkSlotsForSnapshotLocked converts Player.ExtraPerkSlots
// (map[string]bool, keyed by tier) into the wire-format map[string]int the
// client uses to render extra locked-or-filled perk slot cells. Returns nil
// when the unit's owner has no extra-slot advancements for this unit type so
// the omitempty JSON tag elides the field from the payload entirely.
func (s *GameState) unitExtraPerkSlotsForSnapshotLocked(unit *Unit) map[string]int {
	player, ok := s.Players[unit.OwnerID]
	if !ok || player == nil || player.ExtraPerkSlots == nil {
		return nil
	}
	tiers, hasUnit := player.ExtraPerkSlots[unit.UnitType]
	if !hasUnit || len(tiers) == 0 {
		return nil
	}
	out := make(map[string]int, len(tiers))
	for tier, granted := range tiers {
		if granted {
			out[tier] = 1
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *GameState) snapshotUnfilteredLocked() protocol.MatchSnapshotMessage {
	units := make([]protocol.UnitSnapshot, 0, len(s.Units))
	for _, unit := range s.Units {
		effectiveDamage := s.effectiveDamageForSnapshotLocked(unit, 0.0)
		effectiveAttackSpeed := s.effectiveAttackSpeedForSnapshotLocked(unit)
		effectiveMoveSpeed := s.effectiveMoveSpeedForSnapshotLocked(unit)

		baseCritChance := s.unitCritChanceForSnapshotLocked(unit)
		critMultiplier := s.unitCritMultiplierForSnapshotLocked(unit, baseCritChance)
		snapshot := protocol.UnitSnapshot{
			ObjectiveID:          unit.ObjectiveID,
			FocusTargetID:        unit.FocusTargetID,
			ID:                   unit.ID,
			OwnerID:              unit.OwnerID,
			Color:                unit.Color,
			UnitType:             unit.UnitType,
			Archetype:            unit.Archetype,
			Name:                 unit.Name,
			Capabilities:         append([]string(nil), unit.Capabilities...),
			Flyer:                unit.Flyer,
			Visible:              unit.Visible,
			Status:               unit.Status,
			Order:                orderTypeString(unit.Order.Type),
			X:                    unit.X,
			Y:                    unit.Y,
			HP:                   unit.HP,
			MaxHP:                unit.MaxHP,
			Damage:               effectiveDamage,
			AttackSpeed:          effectiveAttackSpeed,
			AttackRange:          unit.AttackRange,
			MoveSpeed:            effectiveMoveSpeed,
			Armor:                s.effectiveArmorForSnapshotLocked(unit),
			CritChance:           baseCritChance,
			CritMultiplier:       critMultiplier,
			HealthRegen:          unit.HealthRegenPerSecond,
			XP:                   unit.XP,
			Rank:                 unit.Rank,
			XPToNextRank:         s.unitXPToNextRankForSnapshotLocked(unit),
			XPIntoCurrentRank:    s.unitXPIntoCurrentRankForSnapshotLocked(unit),
			RecentRankUpSeconds:  unit.RankUpFxRemaining,
			ProgressionPath:      unit.ProgressionPath,
			PerkIDs:              unit.PerkIDs,
			ExtraPerkSlots:       s.unitExtraPerkSlotsForSnapshotLocked(unit),
			Shield:               s.unitShieldForSnapshotLocked(unit),
			MaxShield:            s.unitMaxShieldForSnapshotLocked(unit),
			ShieldPools:          s.unitShieldPoolsForSnapshotLocked(unit),
			Mana:                 unit.CurrentMana,
			MaxMana:              unit.MaxMana,
			ManaRegen:            s.effectiveManaRegenLocked(unit),
			ActiveBuffs:          s.activeBuffIconsForSnapshotLocked(unit),
			ActiveDebuffs:        s.activeDebuffIconsForSnapshotLocked(unit),
			PerkCooldowns:        s.perkCooldownsForSnapshotLocked(unit),
			Abilities:            s.abilityStatesForSnapshotLocked(unit),
			StunnedRemaining:     unit.StunnedRemaining,
			SlowedRemaining:      unit.SlowedRemaining,
			SlowedMultiplier:     unit.SlowedMultiplier,
			OverlayColor:         s.unitOverlayColorLocked(unit),
			ArcaneCharge:         unit.ArcaneCharge,
			BurningRemaining:     unit.PerkState.maxBurnRemaining(),
			BurningAnchor:        s.burningOverlayAnchorLocked(unit),
			ChannelLoopStart:     s.channelLoopStartForUnitLocked(unit),
			ChannelLoopEnd:       s.channelLoopEndForUnitLocked(unit),
			CarriedResourceType:  unit.CarriedResourceType,
			CarriedAmount:        unit.CarriedAmount,
			Moving:               unit.Moving,
			ActionFacingDX:       unit.ActionFacingDX,
			ActionFacingDY:       unit.ActionFacingDY,
			RepathCount:          unit.PathDiagnostics.RepathCount,
			StuckTriggerCount:    unit.PathDiagnostics.StuckTriggerCount,
			LastStuckTick:        unit.PathDiagnostics.LastStuckTick,
		}
		if unit.Moving {
			snapshot.TargetX = unit.TargetX
			snapshot.TargetY = unit.TargetY
		}
		if unit.Gathering && unit.GatherTargetID != "" {
			snapshot.WorkTargetID = unit.GatherTargetID
		} else if unit.Building && unit.BuildTargetID != "" {
			snapshot.WorkTargetID = unit.BuildTargetID
		}
		if unit.UnitType == "archer" && unit.ProgressionPath == "trapper" {
			snapshot.EffectiveTrap = s.EffectiveTrapSnapshotLocked(unit)
		}
		snapshot.Inventory = s.unitInventorySnapshotLocked(unit)
		units = append(units, snapshot)
	}

	players := make([]protocol.PlayerSnapshot, 0, len(s.Players))
	for _, player := range s.Players {
		if player.ID == enemyPlayerID {
			continue
		}
		players = append(players, s.buildPlayerSnapshotLocked(player))
	}

	wm := s.WaveManager
	buildings := make([]protocol.BuildingTile, len(s.MapConfig.Buildings))
	copy(buildings, s.MapConfig.Buildings)
	obstaclesRemoved, obstacleMetadata := s.snapshotObstacleDeltasLocked()

	var banners []protocol.BannerSnapshot
	for _, b := range s.Banners {
		banners = append(banners, protocol.BannerSnapshot{
			ID:               b.ID,
			OwnerID:          b.OwnerPlayerID,
			X:                b.X,
			Y:                b.Y,
			Radius:           b.Radius,
			RemainingSeconds: b.RemainingSeconds,
		})
	}

	var projectiles []protocol.ProjectileSnapshot
	for _, proj := range s.Projectiles {
		progress := 0.0
		if proj.TotalSeconds > 0 {
			progress = 1.0 - (proj.RemainingSeconds / proj.TotalSeconds)
			if progress < 0 {
				progress = 0
			} else if progress > 1 {
				progress = 1
			}
		}
		projectiles = append(projectiles, protocol.ProjectileSnapshot{
			ID:               proj.ID,
			OwnerUnitID:      proj.OwnerUnitID,
			OwnerID:          proj.OwnerPlayerID,
			TargetUnitID:     proj.TargetUnitID,
			OriginUnitID:     proj.OriginUnitID,
			OriginX:          proj.OriginX,
			OriginY:          proj.OriginY,
			TargetX:          proj.TargetX,
			TargetY:          proj.TargetY,
			Progress:         progress,
			Variant:          proj.Variant,
			DoubleShotSecond: proj.DoubleShotSecond,
			Pierce:           proj.Pierce,
			Scale:            proj.Scale,
		})
	}

	var traps []protocol.TrapSnapshot
	for _, trap := range s.Traps {
		if !trap.Triggered && (trap.AftershockPending || len(trap.PendingCataclysms) > 0) {
			continue
		}
		traps = append(traps, protocol.TrapSnapshot{
			ID:               trap.ID,
			OwnerID:          trap.OwnerPlayerID,
			X:                trap.X,
			Y:                trap.Y,
			Radius:           trap.Radius,
			TriggerRadius:    trap.TriggerRadius,
			Variant:          trapVisualVariant(trap),
			ScaleMultiplier:  trapVisualScaleMultiplier(trap),
			Type:             trap.TrapType,
			RemainingSeconds: trap.RemainingSeconds,
			Triggered:        trap.Triggered,
		})
	}
	// Visible ability zones ride the same array (see visibleZoneSnapshotsLocked).
	traps = append(traps, s.visibleZoneSnapshotsLocked()...)

	var gameOver *protocol.GameOverSnapshot
	if len(s.lostPlayerIDs) > 0 {
		ids := make([]string, 0, len(s.lostPlayerIDs))
		for id := range s.lostPlayerIDs {
			ids = append(ids, id)
		}
		gameOver = &protocol.GameOverSnapshot{LostPlayerIDs: ids}
	}

	// Broadcast / unfiltered snapshot: no viewer identity available here.
	// Team-scope objectives use TeamState (correct for any viewer);
	// player-scope objectives fall back to initial state (Current=0). The
	// per-viewer caller in snapshotForPlayerLocked patches `snap.Victory`
	// with the viewer-specific copy after the unfiltered snapshot is built.
	victory := s.buildVictorySnapshotForViewerLocked("")

	beams := s.beamSnapshotsLocked(nil)

	return protocol.MatchSnapshotMessage{
		Type:               "match_snapshot",
		Tick:               s.Tick,
		ServerNow:          time.Now().UnixMilli(),
		Buildings:          buildings,
		ObstaclesRemoved:   obstaclesRemoved,
		ObstacleMetadata:   obstacleMetadata,
		Players:            players,
		Units:              units,
		Corpses:            s.corpseSnapshotsLocked(nil, ""),
		Banners:            banners,
		Traps:              traps,
		Projectiles:        projectiles,
		Beams:              beams,
		Effects:            s.effectSnapshotsLocked(),
		CritEvents:         s.snapshotCritEventsLocked(),
		MeleeAttackEvents:  s.snapshotMeleeAttackEventsLocked(),
		MinorDamageEvents:  s.snapshotMinorDamageEventsLocked(),
		EvadeEvents:        s.snapshotEvadeEventsLocked(),
		HitDamageEvents:    s.snapshotHitDamageEventsLocked(),
		DamageTypeHints:    s.snapshotDamageTypeHintsLocked(),
		LethalDamageEvents: s.snapshotLethalDamageEventsLocked(),
		HealEvents:         s.snapshotHealEventsLocked(),
		ManaRestoreEvents:  s.snapshotManaRestoreEventsLocked(),
		Wave: protocol.WaveSnapshot{
			Enabled:      wm.Enabled,
			CurrentWave:  wm.CurrentWave,
			TotalWaves:   wm.TotalWaves,
			State:        wm.State,
			Timer:        wm.Timer,
			WaveDuration: wm.WaveDuration,
		},
		BattleTracker:          s.battleTrackerSnapshotLocked(),
		GameOver:               gameOver,
		Victory:                victory,
		Paused:                 s.Paused,
		PausedBy:               s.PausedBy,
		PersistentlyStuckUnits: s.persistentlyStuckUnitsLocked(),
		NeutralCamps:           s.neutralCampSnapshotsLocked(),
		LootDrops:              s.lootDropSnapshotsLocked(),
		Zones:                  s.zoneSnapshotsLocked(),
	}
}

func (s *GameState) IncrementTick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Tick++
}

func (s *GameState) Update(dt float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// User-initiated pause: every subsystem is frozen, including the
	// wave-upgrade auto-pick deadline (which is wall-clock based, so we
	// extend it on resume to preserve the remaining selection time).
	if s.Paused {
		return
	}

	// Auto-pause during the wave-upgrade selection phase: simulation is
	// frozen but tickUpgradePhaseLocked still advances so deadlines fire
	// and "all resolved → prep" transitions happen.
	if s.WaveManager.State == "upgrade" {
		s.tickUpgradePhaseLocked()
		return
	}

	// One Update == one simulation frame; advance the tick counter here so
	// callers can't forget. IncrementTick is still exposed for tests that
	// drive isolated subsystems (e.g. tickEffectsLocked) without a full
	// Update pass.
	s.Tick++

	// Drop the previous tick's crit events. Damage application this tick will
	// repopulate the queue, and the next snapshot will drain it. Keeps the
	// list scoped exactly to "crits that landed during this tick" so the
	// client can match against its HP-diff damage events.
	s.resetCritEventsThisTickLocked()
	s.resetMeleeAttackEventsThisTickLocked()
	s.resetMinorDamageEventsThisTickLocked()
	s.evadeEventsThisTick = s.evadeEventsThisTick[:0]
	s.resetHitDamageEventsThisTickLocked()
	s.resetDamageTypeHintsThisTickLocked()
	s.resetLethalDamageEventsThisTickLocked()
	s.resetHealEventsThisTickLocked()
	s.resetManaRestoreEventsThisTickLocked()

	profileSection("commanderCooldowns", func() { s.tickCommanderCooldownsLocked(dt) })

	// Build the shared per-tick unit spatial index once. Combat AI, perk
	// predicate cache, FOW vision iteration, and any future radius-query
	// subsystem all read it instead of allocating their own. Bucket size
	// matches combatSpatialBucketSize.
	profileSection("spatialIndex", func() {
		s.unitSpatialIndex = newCombatSpatialIndex(combatSpatialBucketSize)
		for _, u := range s.Units {
			if u == nil || !u.Visible || u.HP <= 0 {
				continue
			}
			s.unitSpatialIndex.add(u)
		}
	})

	profileSection("battleTracker", func() { s.battleTracker.tickLocked(dt) })
	profileSection("unitProductions", func() { s.updateUnitProductionsLocked(dt) })
	profileSection("orphanedPendingBuildings", func() { s.cancelOrphanedPendingBuildingsLocked() })
	profileSection("buildingTierUps", func() { s.tickBuildingTierUpsLocked(dt) })
	profileSection("blacksmithUpgrades", func() { s.tickBlacksmithUpgradesLocked(dt) })
	profileSection("buildingRepairs", func() { s.tickBuildingRepairsLocked(dt) })
	var blocked map[gridPoint]bool
	profileSection("getBlockedCells", func() { blocked = s.getBlockedCellsLocked() })
	profileSection("buildingCombat", func() { s.tickBuildingCombatLocked(dt) })
	profileSection("wave", func() { s.tickWaveLocked(dt) })
	profileSection("neutralCamps", func() { s.tickNeutralCampsLocked() })
	profileSection("shopReroll", func() { s.tickShopRerollLocked() })
	profileSection("auraStatCache", func() { s.rebuildAuraStatCacheLocked() })
	profileSection("combatAI", func() { s.tickCombatAILocked(dt, blocked) })
	profileSection("unitCombat", func() { s.tickUnitCombatLocked(dt, blocked) })
	// Arcane Missiles: auto-fire from accumulated Arcane Charge. After combat so
	// this tick's mana spends (spell resolves) are already reflected in charge.
	profileSection("arcaneMissiles", func() { s.tickArcaneMissilesLocked(dt) })
	// Projectiles tick after combat resolution so shots fired this tick wait
	// a full dt before decaying on the next Update pass.
	profileSection("projectiles", func() { s.tickProjectilesLocked(dt) })
	// Momentary proc beams (one-shot zaps) decay on their own timer; channel
	// beams are owned by the channel state machine and untouched here.
	profileSection("beams", func() { s.tickBeamsLocked(dt) })
	profileSection("effects", func() { s.tickEffectsLocked() })
	profileSection("enemySpawnpoints", func() { s.tickEnemySpawnpointsLocked(dt, blocked) })
	profileSection("trapEffects", func() { s.tickTrapEffectsLocked(dt) })                   // zone effects + trigger detection
	profileSection("trapperSilverDebuffs", func() { s.tickTrapperSilverDebuffsLocked(dt) }) // barbed ramp, lasting_flames exit, burn DoT
	profileSection("banners", func() { s.tickBannersLocked(dt) })
	profileSection("traps", func() { s.tickTrapsLocked(dt) })                 // lifetime decay + triggered cull
	profileSection("groundHazards", func() { s.tickGroundHazardsLocked(dt) }) // delayed-impact + lingering burn zones
	profileSection("abilityZones", func() { s.tickAbilityZonesLocked(dt) })   // composable create_zone zones (no-op until one is spawned)
	// AbilityStatuses tick immediately after zones, for the same reason zones
	// sit here: statuses attach to units that may have taken lethal damage
	// earlier in this same Update pass (combat/trap/projectile/zone ticks,
	// all above) — pendingDeaths is queued but the unit is NOT YET removed
	// from s.unitsByID (drainPendingDeathsLocked runs below), so a status
	// whose target died this tick observes HP<=0 and fires on_status_expire
	// NOW rather than one tick late. See tickAbilityStatusesLocked's own doc
	// comment for the full placement rationale.
	profileSection("abilityStatuses", func() { s.tickAbilityStatusesLocked(dt) }) // composable apply_status(authored) statuses (no-op until one is spawned)
	// simTime advances every tick (production and preview alike) so the
	// on_animation_marker scheduler (ability_marker.go) has a monotonic,
	// dt-accumulated clock to fire scheduledMarker entries against. Must run
	// AFTER tickAbilityZonesLocked (matches the ordering play_presentation's
	// scheduling call sits in relative to zone-spawning actions) and BEFORE
	// tickAbilityMarkersLocked reads it.
	s.simTime += dt
	profileSection("abilityMarkers", func() { s.tickAbilityMarkersLocked() }) // on_animation_marker scheduler (no-op until a marker is scheduled)
	profileSection("abilityLoops", func() { s.tickPendingLoopsLocked() })     // loop-action iteration scheduler (no-op until a loop with a body wait is running)
	// Drain the per-tick death queue. Must run AFTER all combat/trap/projectile
	// ticks have finished so every HP=0 unit from indirect damage paths (Shared
	// Pain, pain_share redirect, retaliation) is cleaned up before the per-unit
	// loop below, which gates regen on HP>0.
	profileSection("drainPendingDeaths", func() { s.drainPendingDeathsLocked() })
	// tickLootDropsLocked runs after death drain so dead carriers are already
	// absent from s.Units, but BEFORE the per-unit movement loop below —
	// proximity checks therefore use positions from the previous tick's
	// movement settle (one-tick pickup lag). The half-cell radius is generous
	// enough that this lag is invisible at 20 Hz. Running before the per-unit
	// loop also ensures collected chests are removed from s.LootDrops before
	// the snapshot builder iterates, keeping the snapshot view consistent.
	profileSection("lootDrops", func() { s.tickLootDropsLocked() })

	stopPerUnitTick := profileStart("perUnitTick")
	for _, unit := range s.Units {
		if unit.RankUpFxRemaining > 0 {
			unit.RankUpFxRemaining = math.Max(0, unit.RankUpFxRemaining-dt)
		}
		// Cross-unit debuff decay — these states are stamped onto ANY unit by
		// perks on OTHER units (Punishing Guard, Challenger's Mark), so they must
		// tick for every unit regardless of that unit's own perk ownership.
		// Mirrors the TauntRemaining pattern in decayThreatLocked (combat_ai.go).
		if unit.PerkState.WeakenedRemaining > 0 {
			unit.PerkState.WeakenedRemaining = math.Max(0, unit.PerkState.WeakenedRemaining-dt)
			if unit.PerkState.WeakenedRemaining == 0 {
				unit.PerkState.WeakenedMultiplier = 0
			}
		}
		// Hunter's Mark stacks (Marksman silver/gold) decay per-source in the
		// cross-unit loop because the debuff lives on enemies that may not
		// own any Marksman perk themselves — same pattern as MarkStacks /
		// WeakenedRemaining.
		unit.PerkState.decayHuntersMarkStacks(dt)
		// Mark stacks decay independently (each source ticks down its own
		// Remaining). lastExpired is true only when the final active stack
		// hits 0 this tick — that's when mark-gone effects (Final Exposure,
		// Shared Pain disarm) fire.
		if unit.PerkState.decayMarkStacks(dt) {
			// Final Exposure now fires when the victim LEAVES the marker
			// trap's zone (handled in tickTrapEffectsLocked → fireTrap-
			// OverloadOnExitLocked) — not on mark expiry. The fields are
			// cleared at firing time, so we don't reset them here. We DO
			// still disarm Shared Pain when the mark fully expires since
			// it has no zone-exit semantics of its own.
			unit.PerkState.SharedPainFraction = 0
		}
		// ascendant_infusion → Electrified Caltrops per-victim stun cooldown.
		// Cross-unit state (lives on any enemy hit by Electrified), same decay
		// pattern as SlowedRemaining / MarkedRemaining.
		if unit.PerkState.ElectrifiedStunCooldownRemaining > 0 {
			unit.PerkState.ElectrifiedStunCooldownRemaining = math.Max(0, unit.PerkState.ElectrifiedStunCooldownRemaining-dt)
		}
		// Generic CC decay — Stun and Slow are general primitives that any perk or
		// ability can stamp onto any unit, so they decay here alongside the other
		// cross-unit debuffs rather than in tickUnitPerkStateLocked.
		wasStunned := unit.StunnedRemaining > 0
		unit.StunnedRemaining = math.Max(0, unit.StunnedRemaining-dt)
		if unit.SlowedRemaining > 0 {
			unit.SlowedRemaining = math.Max(0, unit.SlowedRemaining-dt)
			if unit.SlowedRemaining == 0 {
				unit.SlowedMultiplier = 0
			}
		}
		// Trapper combat tail-window: decay toward 0 each tick regardless of
		// unit type. Only archers set this to 1.5s (in tickUnitCombatLocked),
		// so it is always 0 for non-archers and the check is cheap.
		if unit.PerkState.LastCombatSeconds > 0 {
			unit.PerkState.LastCombatSeconds = math.Max(0, unit.PerkState.LastCombatSeconds-dt)
		}

		// Battle Prayer buff decay — cross-unit: lives on any healed unit even if
		// they don't own the battle_prayer perk. Decays here alongside the other
		// cross-unit timers (WeakenedRemaining, TauntRemaining, etc.).
		if unit.PerkState.BattlePrayerRemaining > 0 {
			unit.PerkState.BattlePrayerRemaining = math.Max(0, unit.PerkState.BattlePrayerRemaining-dt)
			if unit.PerkState.BattlePrayerRemaining == 0 {
				unit.PerkState.BattlePrayerMultiplier = 0
			}
		}

		// Bolstering Prayer buff decay — cross-unit (same pattern as
		// BattlePrayer above). Lives on any healed unit even if they don't own
		// the bolstering_prayer perk. The flat armor bonus stored alongside
		// the timer is zeroed on expiry so a stale field never contributes to
		// effectiveArmorLocked.
		if unit.PerkState.BolsteringPrayerRemaining > 0 {
			unit.PerkState.BolsteringPrayerRemaining = math.Max(0, unit.PerkState.BolsteringPrayerRemaining-dt)
			if unit.PerkState.BolsteringPrayerRemaining == 0 {
				unit.PerkState.BolsteringPrayerArmor = 0
			}
		}

		// Divine Aegis protection decay — cross-unit (same pattern as the
		// prayer buffs above). The recipient's charge expires after the
		// configured window if no damage instance consumes it first. Owner-
		// side pulse timer (DivineAegisPulseRemaining) decays in
		// tickUnitPerkStateLocked because it lives on the cleric that owns
		// the perk, not on the recipient.
		if unit.PerkState.DivineAegisRemaining > 0 {
			unit.PerkState.DivineAegisRemaining = math.Max(0, unit.PerkState.DivineAegisRemaining-dt)
		}

		// Divine Intervention invulnerability window decay — cross-unit
		// (lives on the saved unit, not the saving cleric). Time-based —
		// not consumed on hit. The damage pipeline checks this at the very
		// top of applyUnitDamageWithSourceLocked and returns 0 immediately
		// when > 0, so any number of hits within the window are negated.
		if unit.PerkState.InvulnerabilityRemaining > 0 {
			unit.PerkState.InvulnerabilityRemaining = math.Max(0, unit.PerkState.InvulnerabilityRemaining-dt)
		}

		// ── Siphoner afflictions (cross-unit decay) ──────────────────────
		// Same pattern as WeakenedRemaining above: stamped on any enemy by
		// a Siphoner's perks, decays here regardless of whether the unit
		// owns the originating perk itself. When the timer expires, the
		// per-affliction multiplier(s) and stack count are zeroed so a
		// stale field never contributes to its consuming stat hook.

		// Withering Beam stacks. Set by withering_beam during continuous
		// Siphon Life contact (see applyWitheringBeamStackLocked). The
		// whole stack list shares one timer; when it expires, stacks +
		// per-stack reduction are cleared so perkOutgoingDamageDebuff-
		// MultiplierLocked never reads a partial state.
		if unit.PerkState.WitheringBeamRemaining > 0 {
			unit.PerkState.WitheringBeamRemaining = math.Max(0, unit.PerkState.WitheringBeamRemaining-dt)
			if unit.PerkState.WitheringBeamRemaining == 0 {
				unit.PerkState.WitheringBeamStacks = 0
				unit.PerkState.WitheringBeamReductionPerStack = 0
			}
		}

		// Lingering Hex — move + attack-speed slow.
		if unit.PerkState.LingeringHexRemaining > 0 {
			unit.PerkState.LingeringHexRemaining = math.Max(0, unit.PerkState.LingeringHexRemaining-dt)
			if unit.PerkState.LingeringHexRemaining == 0 {
				unit.PerkState.LingeringHexMoveMult = 0
				unit.PerkState.LingeringHexAttackSpeedMult = 0
			}
		}

		// Mark of Weakness's armor + healing-received debuff used to decay
		// here (a cross-unit PerkState field, mirroring Lingering Hex
		// above). Removed by the perk's migration to a granted ability: the
		// debuff is now an AbilityStatus (ability_status.go), which decays
		// itself via tickAbilityStatusesLocked — see that call site later in
		// this same tick loop.

		// Amplify Damage (Siphoner silver) — damage-taken multiplier. Same
		// cross-unit decay pattern as the Bronze afflictions above; the
		// multiplier is zeroed on expiry so the damage pipeline never reads a
		// stale field.
		if unit.PerkState.AmplifyDamageRemaining > 0 {
			unit.PerkState.AmplifyDamageRemaining = math.Max(0, unit.PerkState.AmplifyDamageRemaining-dt)
			if unit.PerkState.AmplifyDamageRemaining == 0 {
				unit.PerkState.AmplifyDamageMultiplier = 0
			}
		}

		// Focus target validation — clears stale focus (dead, invisible,
		// switched teams) every tick while OrderFocusFollow is active.
		// The unit falls back to idle / auto-heal after clearFocusTargetLocked
		// transitions it to OrderIdle.
		if unit.Order.Type == OrderFocusFollow {
			s.validateFocusTargetLocked(unit)
		}

		// Passive health regen. Accumulator carries fractional HP between ticks
		// so sub-1 rates (the default 0.2 HP/s) still heal integer HP on the
		// correct cadence. Skipped for dead / full-HP units; accumulator resets
		// at full HP so the next hit doesn't instantly trigger banked regen.
		// Zone-aura health regen: folded read-on-demand as (base + add) × mul, so
		// it tracks ownership with no recompute. Computed before the >0 guard so
		// an aura can grant regen to a unit whose base rate is 0. Identity (0,1)
		// ⇒ exactly unit.HealthRegenPerSecond.
		// Fold the perk + status + zone-aura "healthRegen" pool through the
		// shared chokepoint. Computed before the >0 guard so an aura/status can
		// grant regen to a unit whose base rate is 0. Empty pool + no zone aura
		// => identity, exactly unit.HealthRegenPerSecond as before.
		effectiveHealthRegen := s.effectiveStatLocked(unit, unit.HealthRegenPerSecond, statHealthRegen)
		if unit.HP > 0 && effectiveHealthRegen > 0 {
			if unit.HP >= unit.MaxHP {
				unit.HealthRegenAccumulator = 0
			} else {
				unit.HealthRegenAccumulator += effectiveHealthRegen * dt
				if unit.HealthRegenAccumulator >= 1 {
					healAmt := int(unit.HealthRegenAccumulator)
					unit.HealthRegenAccumulator -= float64(healAmt)
					s.healUnitLocked(unit, healAmt)
				}
			}
		}

		// Passive mana regen for spellcasters. No-op for units with no mana
		// pool (MaxMana == 0), so non-caster units are unaffected.
		s.tickUnitManaRegenLocked(unit, dt)

		// Advance an in-progress ability cast (count down, resolve, or cancel
		// if the target became invalid). No-op when not casting.
		s.tickUnitCastLocked(unit, dt)

		// Advance an active channel (Siphon Life etc.). Channels and one-shot
		// casts are mutually exclusive, so at most one of these is a no-op per
		// unit per tick. Channel tick runs AFTER the one-shot tick so that a
		// 0-cast-time ability that resolves to a channel entry this same tick
		// would still have its first channel tick next Update().
		s.tickUnitChannelLocked(unit, dt)

		// Decay ability cooldowns, then run the auto-cast loop (may initiate
		// a new cast if an auto-cast-enabled ability is ready). No-op for
		// units with no cooldowns / no auto-cast toggles.
		s.tickUnitAbilityCooldownsLocked(unit, dt)
		s.tickUnitAutoCastLocked(unit)

		// Advance time-based perk state (idle timers, buff durations).
		s.tickUnitPerkStateLocked(unit, dt)
		s.updateWorkerTaskLocked(unit, dt, blocked)

		if unit.MiningInside {
			continue
		}

		// Cast lock gates all pathing for the cast duration (the caster also
		// cannot attack — see the tickUnitCombatLocked cast guard). Runs after
		// tickUnitCastLocked so the tick a cast completes (Casting cleared)
		// movement resumes immediately. Mirrors the stun pathing gate below.
		if unit.Casting {
			continue
		}

		// Forced displacement (pull) wins the tick over normal path advancement:
		// the unit is dragged toward the pull center and does NOT path this tick.
		// Runs before the stun/movement gates so a stunned unit is still pulled.
		if unit.PullRemaining > 0 {
			s.tickUnitPullLocked(unit, dt)
			continue
		}

		// Stun gates all pathing. Leave Moving and Path intact so the unit
		// resumes exactly where it was once the stun expires.
		// On stun expiry, force a repath: the path may now pass through buildings
		// placed while the unit was stunned.
		if wasStunned && unit.StunnedRemaining <= 0 && unit.Moving && len(unit.Path) > 0 {
			if !s.repathUnitLocked(unit, blocked) {
				// Route momentarily gone (a building may have been placed while
				// stunned) — hold the order and retry rather than abandon it.
				s.enterRepathBlockedLocked(unit)
			}
		}
		if unit.StunnedRemaining > 0 {
			continue
		}

		// Obstacle-escape hatch. A ground unit standing inside a blocked cell
		// (shoved in by knockback / pull / separation, or a building/tree
		// dropped on it) cannot move via normal path advancement: every step's
		// coarse walkability check fails because its OWN cell is blocked, so it
		// repath-storms in place. Step it straight toward the nearest walkable
		// cell centre — ignoring the walkability gate that would otherwise trap
		// it — until it is back on open ground, where normal movement resumes.
		// Flyers ignore terrain entirely, so they can never be embedded. Only
		// eject live, on-field units that are actually trying to move — the
		// repath storm this fixes only afflicts moving units, and gating on
		// motion leaves intentional placements alone (e.g. a unit yanked onto a
		// blocked cell by a Pull and then left idle must stay where it landed,
		// not bounce out). An InsideBuilder worker (Visible=false) legitimately
		// stands on its under-construction footprint; MiningInside gatherers
		// already `continue` above; dead units never move.
		if !unit.Flyer && unit.HP > 0 && unit.Visible && (unit.Moving || unit.RepathBlocked) &&
			!s.isWalkable(s.worldToGrid(unit.X, unit.Y), blocked) {
			if escapeCell, ok := s.findNearestWalkable(s.worldToGrid(unit.X, unit.Y), blocked); ok {
				dest := s.gridToWorldCenter(escapeCell)
				dx := dest.X - unit.X
				dy := dest.Y - unit.Y
				if d := math.Hypot(dx, dy); d > 0 {
					step := unit.MoveSpeed * s.perkMoveSpeedMultiplierLocked(unit) * slowFactorLocked(unit) * dt
					if step > d {
						step = d
					}
					unit.X = clampFloat(unit.X+(dx/d)*step, unitRadius, s.MapWidth-unitRadius)
					unit.Y = clampFloat(unit.Y+(dy/d)*step, unitRadius, s.MapHeight-unitRadius)
				}
				continue
			}
		}

		// Focus follow: when the Cleric has an active focus target, drive its
		// path toward the follow destination each tick (with repath debouncing).
		// Must run before the !unit.Moving check so a stationary Cleric that
		// needs to follow a moving ally will start a new path this tick.
		if unit.Order.Type == OrderFocusFollow && unit.FocusTargetID != 0 {
			s.tickFocusFollowMovementLocked(unit, blocked)
		}

		if !unit.Moving {
			if unit.RepathBlocked {
				// Unit is holding a movement order it currently can't path.
				// Retry on a bounded cadence before giving up, instead of
				// abandoning the order (which wedged units against buildings/
				// trees forever). A successful retry sets Moving again; fall
				// through and advance the unit this tick.
				s.tickRepathBlockedRetryLocked(unit, dt, blocked)
				if !unit.Moving {
					unit.AttackDrifting = false
					continue
				}
			} else {
				// Force-move order is complete when the unit stops moving.
				if unit.Order.Type == OrderMove {
					unit.Order = OrderState{Type: OrderIdle}
				}
				unit.AttackDrifting = false
				continue
			}
		}

		// Capture perk speed multiplier once for both the stuck watchdog and the
		// movement step below — avoids a second map lookup in the hot path.
		perkSpeedMult := s.perkMoveSpeedMultiplierLocked(unit)

		// Drift mode: A* to an attack target failed, so step straight-line toward
		// the target's current coordinates instead of standing idle. Separation
		// resolves ally collisions; impassable terrain silently halts the unit
		// (next AI eval re-acquires). Drift consumes one walkability check per
		// tick — orders of magnitude cheaper than running A* every retry cycle.
		if unit.AttackDrifting && len(unit.Path) == 0 {
			dx := unit.TargetX - unit.X
			dy := unit.TargetY - unit.Y
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist <= unitRadius {
				// Reached the drift target's last known position. Stop here;
				// combat tick / next AI eval handles whatever comes next.
				unit.Moving = false
				unit.AttackDrifting = false
				continue
			}
			step := unit.MoveSpeed * perkSpeedMult * slowFactorLocked(unit) * lingeringHexMoveSpeedFactorLocked(unit) * dt
			if step >= dist {
				step = dist
			}
			nextX := unit.X + (dx/dist)*step
			nextY := unit.Y + (dy/dist)*step
			if !unit.Flyer {
				nextCell := s.worldToGrid(nextX, nextY)
				if !s.isWalkable(nextCell, blocked) {
					// Wall in the way — halt silently. No repath storm.
					unit.Moving = false
					unit.AttackDrifting = false
					continue
				}
			}
			unit.X = clampFloat(nextX, unitRadius, s.MapWidth-unitRadius)
			unit.Y = clampFloat(nextY, unitRadius, s.MapHeight-unitRadius)
			continue
		}

		if len(unit.Path) == 0 {
			unit.Moving = false
			// Force-move order is complete when the path is exhausted.
			if unit.Order.Type == OrderMove {
				unit.Order = OrderState{Type: OrderIdle}
			}
			continue
		}

		// Stuck-progress watchdog. Accumulate dt; once a sample window has
		// elapsed, repath if the unit hasn't displaced past the speed-proportional
		// threshold. Catches separation-vs-path oscillation where the unit is
		// technically Moving=true every tick but its net position barely changes.
		// assignUnitPath resets the sample, so the watchdog gets a fresh window
		// after every repath.
		//
		// Pathing budget: the historical code called repathUnitLocked
		// unconditionally, so a packed melee blob where every member trips the
		// watchdog on the same tick produced an N-wide A* storm. We now gate on
		// combatApproachBudgetRemaining (shared with combat-AI approach
		// pathing): in-budget → repath, over-budget → drift toward the chase
		// target (matching the [[feedback_pathfinding_drift_over_retry]] rule
		// the project already uses on the combat-AI side).
		unit.StuckSampleAccum += dt
		if unit.StuckSampleAccum >= stuckSampleInterval {
			ddx := unit.X - unit.StuckSampleX
			ddy := unit.Y - unit.StuckSampleY
			stuckThreshold := math.Max(8.0, unit.MoveSpeed*perkSpeedMult*lingeringHexMoveSpeedFactorLocked(unit)*stuckSampleInterval*0.4)
			if ddx*ddx+ddy*ddy < stuckThreshold*stuckThreshold {
				unit.PathDiagnostics.StuckTriggerCount++
				unit.PathDiagnostics.LastStuckTick = s.Tick
				if s.combatApproachBudgetRemaining > 0 {
					s.combatApproachBudgetRemaining--
					if !s.repathUnitLocked(unit, blocked) {
						// No route right now — hold the order and retry on the
						// bounded cadence instead of grinding in place forever.
						s.enterRepathBlockedLocked(unit)
					}
				} else if unit.AttackTargetID != 0 {
					// Over budget and chasing a target → drift instead of A*.
					if target := s.getUnitByIDLocked(unit.AttackTargetID); target != nil && target.HP > 0 && target.Visible {
						s.enterAttackDriftLocked(unit, target)
					}
				}
				// No target + over budget: leave the path alone; the watchdog
				// will retry on the next sample window (when budget refills).
			} else {
				unit.StuckSampleX = unit.X
				unit.StuckSampleY = unit.Y
				unit.StuckSampleAccum = 0
			}
			if !unit.Moving || len(unit.Path) == 0 {
				continue
			}
		}

		nextWaypoint := unit.Path[0]
		dx := nextWaypoint.X - unit.X
		dy := nextWaypoint.Y - unit.Y
		dist := math.Sqrt(dx*dx + dy*dy)

		if dist == 0 {
			unit.X = nextWaypoint.X
			unit.Y = nextWaypoint.Y
			unit.Path = unit.Path[1:]
			unit.Moving = len(unit.Path) > 0
			continue
		}

		// Effective move speed: per-unit stat (base × rank × path, already baked
		// into unit.MoveSpeed by applyRankModifiersLocked) × perk multiplier
		// (momentum, future speed perks) × slow multiplier (CC primitive).
		step := unit.MoveSpeed * perkSpeedMult * slowFactorLocked(unit) * dt
		if step >= dist {
			unit.X = nextWaypoint.X
			unit.Y = nextWaypoint.Y
			unit.Path = unit.Path[1:]
			unit.Moving = len(unit.Path) > 0
			continue
		}

		nextX := unit.X + (dx/dist)*step
		nextY := unit.Y + (dy/dist)*step
		// Flyers ignore terrain — only map bounds (already enforced by the
		// per-tick X/Y clamp upstream of this loop) constrain them. Skipping
		// the walkability check avoids repath thrashing when a flyer crosses
		// over impassable terrain.
		if !unit.Flyer {
			nextCell := s.worldToGrid(nextX, nextY)
			if !s.isWalkable(nextCell, blocked) {
				// The straight step toward the waypoint hits a blocked cell.
				// Before giving up to a repath, wall-slide: move along whichever
				// single axis stays walkable so the unit grazes past the obstacle
				// corner and keeps making progress. This is what breaks the
				// "stuck walking into a tree" repath storm — a waypoint whose
				// approach clips an obstacle the sub-cell A* buffered around but
				// the coarse movement check rejects. Without sliding the unit
				// repaths to the same clipping path every tick and never moves.
				if s.isWalkable(s.worldToGrid(nextX, unit.Y), blocked) {
					unit.X = nextX
					continue
				}
				if s.isWalkable(s.worldToGrid(unit.X, nextY), blocked) {
					unit.Y = nextY
					continue
				}
				// Boxed in on both axes — the path stepped into newly-blocked
				// terrain and no slide is available this tick. Hold the order and
				// retry on the bounded cadence instead of abandoning it.
				if !s.repathUnitLocked(unit, blocked) {
					s.enterRepathBlockedLocked(unit)
				}
				continue
			}
		}

		unit.X = nextX
		unit.Y = nextY
	}
	stopPerUnitTick()

	profileSection("corpses", func() { s.tickCorpsesLocked(dt) })
	profileSection("separation", func() { s.applyUnitSeparationLocked(blocked) })
	// Refresh predicate cache AFTER separation so cached flags reflect final
	// end-of-tick positions for the snapshot consumers.
	profileSection("perkPredicateCache", func() { s.recomputePerkPredicateCacheLocked() })
	// Snapshot cache must run AFTER the predicate cache (effectiveArmorLocked
	// reads BraceActive / InterlockActive) so the cached armor value is
	// consistent with the icons.
	profileSection("snapshotCache", func() { s.recomputeUnitSnapshotCacheLocked() })
	profileSection("buildingMetadata", func() { s.refreshBuildingRuntimeMetadataLocked() })
	profileSection("obstacleMetadata", func() { s.refreshObstacleRuntimeMetadataLocked() })
	profileSection("playerLoss", func() { s.checkPlayerLossLocked() })
	// Zone capture evaluation runs after movement/combat settle (so unit
	// positions and building ownership are current) and before objectives so
	// a capture_zone objective sees this tick's ownership.
	profileSection("zones", func() { s.tickZonesLocked(dt) })
	// Objective evaluation runs after all metric-bumping subsystems and
	// before the victory check so checkVictoryLocked (§9) sees current
	// objective state when computing the new AND-gate.
	profileSection("objectives", func() { s.evaluateObjectivesLocked() })
	profileSection("victory", func() { s.checkVictoryLocked() })
	profileSection("fow", func() { s.recomputeFOWLocked() })

	s.reportPathDebugLocked()

	profileTickComplete(s.Tick, len(s.Units))
}

// checkVictoryLocked sets victoryAchieved when both gates are satisfied:
//
//  1. waveOrTownhallConditionMet — today this is the wave manager reaching
//     `state == "complete"`. The legacy townhall-destruction rule existed
//     only via `MapConfig.VictoryConditions`, which was removed in §6, so
//     wave completion is the sole legacy victory path for now. Adding a
//     townhall-destruction handler to the objective registry will let that
//     condition migrate into the new system later.
//  2. allRequiredObjectivesCompleted() — every objective marked
//     `required: true` on the launching campaign level has its TeamState
//     (or every player's PlayerState, for player-scope objectives) marked
//     complete.
//
// Optional objectives never gate victory. A match with no required
// objectives reduces to just the legacy condition (the AND with `true`
// trivially holds), which preserves Custom Game behaviour. Once set,
// victoryAchieved is sticky for the match.
func (s *GameState) checkVictoryLocked() {
	if s.victoryAchieved {
		return
	}
	if !s.waveOrTownhallConditionMetLocked() {
		return
	}
	if !s.allRequiredObjectivesCompletedLocked() {
		return
	}
	s.victoryAchieved = true
	// Game-over cause diagnostics: one line naming the trigger, so an
	// unexpected match end is attributable from the console alone.
	slog.Info("[GAME OVER] victory: wave gate + all required objectives complete",
		"waveState", s.WaveManager.State,
		"wave", s.WaveManager.CurrentWave,
		"totalWaves", s.WaveManager.TotalWaves,
		"tick", s.Tick)
}

// waveOrTownhallConditionMetLocked reports whether the legacy victory rule
// fires this tick. Today: wave manager reached "complete" state. Future
// addition: an `enemy_townhalls_destroyed` objective handler would let
// townhall-destruction levels express victory through the registry, and
// this helper could collapse to a single trivially-true.
func (s *GameState) waveOrTownhallConditionMetLocked() bool {
	// Endless continuous maps (no wave cap) never reach "complete" — the wave
	// system runs until defeat. For these the legacy wave gate is N/A, so let
	// required-objective completion (capture_zone, etc.) drive victory on its
	// own. Bounded continuous maps (TotalWaves > 0) and all discrete maps still
	// require clearing the final wave to "complete".
	wm := s.WaveManager
	if wm.Enabled && wm.Continuous && wm.TotalWaves == 0 {
		return true
	}
	return wm.State == "complete"
}

// allRequiredObjectivesCompletedLocked walks `s.Objectives` and returns true
// when every objective with `Required: true` is complete:
//
//   - team-scope: TeamState.Completed must be true.
//   - player-scope: every non-AI player's PlayerState must exist and have
//     Completed = true. Strictest reading — every player in the match has
//     to satisfy the requirement. (Phase 1 doesn't author any required
//     player-scope objectives, so this branch is defensive.)
//
// Returns true when `s.Objectives` is empty — Custom Game and other
// non-campaign matches degrade to the legacy rule alone.
func (s *GameState) allRequiredObjectivesCompletedLocked() bool {
	for i := range s.Objectives {
		runtime := &s.Objectives[i]
		if !runtime.Def.Required {
			continue
		}
		switch runtime.Def.Scope {
		case ObjectiveScopeTeam:
			if !runtime.TeamState.Completed {
				return false
			}
		case ObjectiveScopePlayer:
			for playerID := range s.Players {
				if playerID == enemyPlayerID || playerID == neutralPlayerID {
					continue
				}
				state, ok := runtime.PlayerStates[playerID]
				if !ok || !state.Completed {
					return false
				}
			}
		}
	}
	return true
}

// checkPlayerLossLocked scans all townhalls each tick to detect players who
// have lost all of theirs. A player can only lose once they have owned at
// least one townhall — this prevents marking players as "lost" before they
// have even claimed a starting position.
func (s *GameState) checkPlayerLossLocked() {
	// Once a continue-play-eligible match has been won, it becomes unlosable:
	// the player banked the victory and chose to keep playing, so a later base
	// loss must not flip the match to defeat (which would freeze the sim and
	// tear the match down out from under them). Matches without required
	// objectives, and all pre-victory ticks, fall through to normal detection.
	if s.victoryAchieved && s.continuePlayEligibleLocked() {
		return
	}
	if s.playersWithTownhall == nil {
		s.playersWithTownhall = map[string]bool{}
	}
	if s.lostPlayerIDs == nil {
		s.lostPlayerIDs = map[string]bool{}
	}

	townhallCounts := map[string]int{}
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType != "townhall" || !b.Visible || b.OwnerID == nil || *b.OwnerID == enemyPlayerID {
			continue
		}
		s.playersWithTownhall[*b.OwnerID] = true
		townhallCounts[*b.OwnerID]++
	}

	// Team-aggregated defeat: a TEAM is eliminated only when every member's
	// townhalls are gone (teammates can carry a downed ally). When that
	// happens, mark all of the team's players lost together, so the existing
	// GameOverSnapshot.LostPlayerIDs wire contract / IsGameOver are unchanged.
	// Map iteration here only sums/sets — order never drives the outcome
	// (sums are commutative; the lost-set is a set). Default single team for
	// a lone player reduces exactly to "this player lost their last townhall".
	teamTownhalls := map[int]int{}
	teamEverHad := map[int]bool{}
	for playerID := range s.playersWithTownhall {
		team := s.playerTeamLocked(playerID)
		teamEverHad[team] = true
		teamTownhalls[team] += townhallCounts[playerID]
	}

	for playerID := range s.playersWithTownhall {
		if s.lostPlayerIDs[playerID] {
			continue
		}
		team := s.playerTeamLocked(playerID)
		if teamEverHad[team] && teamTownhalls[team] == 0 {
			s.lostPlayerIDs[playerID] = true
			// Game-over cause diagnostics — counterpart of the victory log in
			// checkVictoryLocked.
			slog.Info("[GAME OVER] defeat: player's team lost all townhalls",
				"playerID", playerID,
				"team", team,
				"wave", s.WaveManager.CurrentWave,
				"tick", s.Tick)
		}
	}
}

// IsGameOver returns true once the match has reached a terminal outcome —
// either a player has lost all their townhalls, or all victory objectives
// have been completed. This drives the one-shot game-over side effects
// (dominion-point commit, end-screen payload). It is deliberately distinct
// from IsSimulationHalted: a continue-play match keeps simulating after
// victory even though IsGameOver reports the win.
func (s *GameState) IsGameOver() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.lostPlayerIDs) > 0 || s.victoryAchieved
}

// continuePlayEligibleLocked reports whether this match should keep running
// after victory instead of freezing on the game-over tick. True exactly when
// the match carries at least one required objective — i.e. the campaign
// matches whose client offers the "Continue Playing" popup. Custom Game /
// find-game matches (no required objectives) have no such affordance and
// freeze + tear down on victory as before.
func (s *GameState) continuePlayEligibleLocked() bool {
	for i := range s.Objectives {
		if s.Objectives[i].Def.Required {
			return true
		}
	}
	return false
}

// IsSimulationHalted reports whether the tick loop should STOP advancing the
// simulation. This is distinct from IsGameOver: a continue-play-eligible match
// (campaign match with required objectives) keeps ticking after victory so the
// player can pick "Continue Playing" and actually keep playing — otherwise the
// win instantly freezes every unit. The sim halts only on a genuine defeat (a
// player/team lost their townhalls) or on victory in a non-continue match.
//
// The match manager also uses this to decide whether to schedule the 15-second
// teardown: a halted match winds down; a still-running continue-play match is
// kept alive until the player explicitly exits.
func (s *GameState) IsSimulationHalted() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.lostPlayerIDs) > 0 {
		return true
	}
	if s.victoryAchieved && !s.continuePlayEligibleLocked() {
		return true
	}
	return false
}

// MatchSummaryForPlayer returns the end-of-match dominion-point summary for
// playerID. Won is true when the player is not in the lost set. The dominion
// points earned are: kill drops accumulated during the match plus the
// win/loss bonus from tuning.
func (s *GameState) MatchSummaryForPlayer(playerID string) protocol.MatchSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.matchSummaryForPlayerLocked(playerID)
}

func (s *GameState) matchSummaryForPlayerLocked(playerID string) protocol.MatchSummary {
	lost := s.lostPlayerIDs != nil && s.lostPlayerIDs[playerID]
	tuning := gameplayTuning()

	var runDrops int
	if player, ok := s.Players[playerID]; ok {
		runDrops = player.RunDominionPointDrops
	}

	bonus := tuning.DominionPoints.LossConsolation
	if !lost {
		bonus = tuning.DominionPoints.WinBonus
	}

	return protocol.MatchSummary{
		PlayerID:             playerID,
		Won:                  !lost,
		DominionPointsEarned: runDrops + bonus,
	}
}

// viewerDominionPointsEarnedLocked returns the player's full per-match earned
// dominion-point total — per-kill drops (MatchDominionPointsEarned) plus the
// win/loss bonus — matching what the host credits server-side via
// matchSummaryForPlayerLocked. This is the per-viewer value sent in the
// game-over snapshot so a remote joiner persists the same total the host would.
func (s *GameState) viewerDominionPointsEarnedLocked(playerID string) int {
	p, ok := s.Players[playerID]
	if !ok {
		return 0
	}
	lost := s.lostPlayerIDs != nil && s.lostPlayerIDs[playerID]
	tuning := gameplayTuning()
	bonus := tuning.DominionPoints.WinBonus
	if lost {
		bonus = tuning.DominionPoints.LossConsolation
	}
	return p.MatchDominionPointsEarned + bonus
}

// HumanPlayerMatchSummaries returns one MatchSummary per human player in the
// match, skipping the synthetic enemy / neutral entries. Sorted by player ID
// for deterministic iteration. Called once by the match manager when the loop
// transitions to game-over.
func (s *GameState) HumanPlayerMatchSummaries() []protocol.MatchSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	playerIDs := make([]string, 0, len(s.Players))
	for id := range s.Players {
		if id == enemyPlayerID || id == neutralPlayerID {
			continue
		}
		playerIDs = append(playerIDs, id)
	}
	sort.Strings(playerIDs)

	summaries := make([]protocol.MatchSummary, 0, len(playerIDs))
	for _, id := range playerIDs {
		summaries = append(summaries, s.matchSummaryForPlayerLocked(id))
	}
	return summaries
}

func (s *GameState) EnsurePlayer(playerID string) {
	s.EnsurePlayerWithUpgrades(playerID, nil, nil, nil, nil)
}

// EnsurePlayerWithUpgrades is the full match-join path. It creates the Player
// entry and spawns starting units if the player is new. On reconnect it is a
// no-op (multipliers and active upgrades do not reset between connection drops).
// ownedUpgradeRanks is a snapshot of PlayerProfile.OwnedUpgradeRanks taken at
// join time; nil is treated as empty (default multipliers, no extra workers).
// activeUpgradeIDs lists which upgrades are enabled for this match; nil means
// all owned upgrades are active (backwards-compatible default).
// acquiredAdvancementIDs is the sorted list of advancement IDs the player owns
// (from PlayerProfile.AcquiredAdvancements); nil / empty means no advancements.
// knownCraftableIDs is the list of ITEM IDs whose recipes the player has learned,
// seeded from the profile's KnownCraftableIDs; nil / empty means nothing learned.
func (s *GameState) EnsurePlayerWithUpgrades(playerID string, ownedUpgradeRanks map[string]int, activeUpgradeIDs []string, acquiredAdvancementIDs []string, knownCraftableIDs []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.Players[playerID]; exists {
		// Reconnect: multipliers and active set were computed at initial join;
		// they don't change unless the player leaves and rejoins.
		return
	}

	// Snapshot owned upgrade ranks (nil -> empty, never keep nil on Player).
	upgrades := make(map[string]int, len(ownedUpgradeRanks))
	for k, v := range ownedUpgradeRanks {
		if v > 0 {
			upgrades[k] = v
		}
	}

	// Build the active set. nil activeUpgradeIDs means "all owned are active".
	activeSet := make(map[string]bool, len(upgrades))
	if activeUpgradeIDs != nil {
		for _, id := range activeUpgradeIDs {
			activeSet[id] = true
		}
	} else {
		for id := range upgrades {
			activeSet[id] = true
		}
	}

	// Snapshot acquired advancement IDs. Defensive copy; nil -> empty slice.
	advancementIDs := make([]string, 0, len(acquiredAdvancementIDs))
	for _, id := range acquiredAdvancementIDs {
		if id != "" {
			advancementIDs = append(advancementIDs, id)
		}
	}

	// Seed the in-match learned set from the profile's known recipes plus every
	// starter recipe (always craftable, no purchase). Deduped, empties dropped,
	// sorted so playerKnowsRecipeLocked and unlockRecipeForPlayerLocked maintain
	// their invariant.
	craftableSet := make(map[string]struct{}, len(knownCraftableIDs))
	for _, id := range knownCraftableIDs {
		if id != "" {
			craftableSet[id] = struct{}{}
		}
	}
	for _, id := range starterCraftableItemIDs() {
		craftableSet[id] = struct{}{}
	}
	craftableIDs := make([]string, 0, len(craftableSet))
	for id := range craftableSet {
		craftableIDs = append(craftableIDs, id)
	}
	sort.Strings(craftableIDs)

	player := &Player{
		ID: playerID,
		// Color is a fixed per-slot team color, assigned below once the player
		// has claimed a starting townhall (which determines their slot).
		Color:                         "",
		Resources:                     playerConfig().newStartingResources(),
		GlobalUnitSpawnTimeMultiplier: 1,
		UnitSpawnTimeMultipliers:      map[string]float64{},
		Upgrades:                      make(map[UpgradeTrack]int),
		Vault:                         []*VaultItem{},
		UpgradeState:                  newPlayerUpgradeState(1, 3),
		CommanderAbilityCooldowns:     map[string]float64{},
		ProfileUpgrades:               upgrades,
		ActiveUpgradeIDs:              activeSet,
		PhysicalDamageMultiplier:      1.0,
		MagicDamageMultiplier:         1.0,
		ExtraStartingUnits:            map[string]int{},
		ShopRerollsRemaining:          defaultShopRerollsPerPlayer,
		NeutralShopInventories:        map[string][]protocol.ShopStockEntry{},
		AcquiredAdvancements:          advancementIDs,
		UnlockedCraftableIDs:          craftableIDs,
		Metrics:                       NewMatchMetrics(),
		ZoneStatModifiers:             newPlayerStatModifierSet(),
	}
	// Derive precomputed multipliers and extra workers from the upgrade catalog.
	applyProfileUpgradesToPlayerLocked(player)
	// Compute per-player effective unit defs from owned advancements. Must run
	// after applyProfileUpgradesToPlayerLocked so the effective defs are ready
	// before the first unit spawns below.
	applyAdvancementsToEffectiveDefsLocked(player)
	s.Players[playerID] = player
	// Sample this player's independent view of every neutral shop, at their
	// effective item count (now that ShopItemCountBonus is applied). Runs after
	// the player is in s.Players so the stocking helper can resolve it.
	s.populatePlayerNeutralShopViewsLocked(playerID)

	townhall, _ := s.claimPlayerStartLocked(playerID)
	// Now that the slot (townhall/spawn-point label) is known, assign the fixed
	// per-slot team color. Placed unit spawns below stamp this color onto units.
	player.Color = s.slotColorForPlayerLocked(playerID)
	color := player.Color
	s.claimLabeledBuildingsForPlayerLocked(playerID)
	s.spawnPlacedUnitsForPlayerLocked(playerID, color)
	// Spawn upgrade-granted starting units at the player's spawn-point.
	// Iterate in sorted key order for deterministic spawn order across runs.
	extraUnitTypes := make([]string, 0, len(player.ExtraStartingUnits))
	for ut := range player.ExtraStartingUnits {
		extraUnitTypes = append(extraUnitTypes, ut)
	}
	sort.Strings(extraUnitTypes)
	for _, ut := range extraUnitTypes {
		s.spawnUnitsForPlayerAtSpawnPointLocked(player, ut, player.ExtraStartingUnits[ut])
	}
	s.ensurePlacedEnemiesSpawnedLocked()

	// Testing / advancement hook: on wave-enabled maps, optionally present the
	// wave-upgrade pick at match start (before wave 1). No-op unless the
	// player.json toggle is on or the player owns a start-bonus advancement.
	s.maybeGrantStartWaveBonusLocked(player)

	// Seed this player's zone-aura aggregate from any zones their team already
	// controls at join (home/team-locked zones, or zones with a StartingOwner of
	// their slot). Recompute-all is event-driven and cheap; thereafter it only
	// re-fires on an actual ownership flip via setZoneOwnerLocked.
	s.recomputeAllZoneAuraModifiersLocked()

	// Diagnostic: which slot did this player land in? Useful when a map has
	// authored player1/player2 labels and the caller wants to know which
	// side they were assigned to. Logged once per fresh join.
	label := s.findPlayerLabelLocked(playerID)
	// Record this slot label as ever-joined so target-labeled enemy spawnpoints
	// keep firing at the surviving base after this player later loses their
	// townhall (which removes it from the map, breaking label→player resolution).
	// See the targetPlayerLabel gate in tickEnemySpawnpointsLocked.
	if label != "" {
		if s.joinedTargetLabels == nil {
			s.joinedTargetLabels = map[string]bool{}
		}
		s.joinedTargetLabels[label] = true
	}
	var townhallID string
	if townhall != nil {
		townhallID = townhall.ID
	}
	slog.Info("player joined match slot",
		"playerID", playerID,
		"label", label,
		"townhallID", townhallID,
	)

	if s.FOW == nil {
		s.FOW = map[string]*PlayerFOW{}
	}
	s.FOW[playerID] = newPlayerFOW(s.MapConfig.GridCols, s.MapConfig.GridRows)
}

func (s *GameState) RemovePlayer(playerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.Players, playerID)

	// Collect IDs first, then remove via the helper to keep the index in sync.
	// Release any incremental counters (mining occupancy) the doomed units were
	// contributing to BEFORE we drop them — otherwise the tooltip stays stale.
	var toRemove []int
	for _, unit := range s.Units {
		if unit.OwnerID == playerID {
			s.setUnitMiningInsideLocked(unit, false)
			toRemove = append(toRemove, unit.ID)
		}
	}
	for _, id := range toRemove {
		delete(s.unitsByID, id)
	}
	filtered := make([]*Unit, 0, len(s.Units)-len(toRemove))
	for _, unit := range s.Units {
		if unit.OwnerID != playerID {
			filtered = append(filtered, unit)
		}
	}
	s.Units = filtered

	s.releaseTownhallForPlayerLocked(playerID)

	// Drop any traps planted by the leaving player — mirrors banner cleanup.
	if len(s.Traps) > 0 {
		kept := s.Traps[:0]
		for _, trap := range s.Traps {
			if trap.OwnerPlayerID != playerID {
				kept = append(kept, trap)
			}
		}
		s.Traps = kept
	}

	// Drop any in-flight projectiles fired by the leaving player.
	s.cullProjectilesLocked(func(p *Projectile) bool {
		return p.OwnerPlayerID == playerID
	})
}

// removeUnitLocked takes a unit off the field entirely: tear down every
// reference to it, then delete it from the registry.
//
// DEATH does not call this. A unit that dies becomes a CORPSE — same tear-down,
// but it stays in the registry until it decays (see killUnitToCorpseLocked and
// docs/design/death_and_corpses.md). This is still the right call for a unit
// that leaves the field without dying, and it is what corpse decay eventually
// calls. Safe to call twice: every step is idempotent.
func (s *GameState) removeUnitLocked(unitID int) {
	s.tearDownDeadUnitLocked(unitID)
	s.removeUnitByIDLocked(unitID)
}

// killUnitToCorpseLocked turns a unit that has just died into a corpse: run the
// full tear-down (nothing may still point at it, swing at it, or fly toward it —
// docs/design/death_and_corpses.md §6) and leave the body on the field for
// corpseLifetimeSeconds.
//
// Deliberately NOT a partial tear-down. A corpse is inert; a revive restores a
// clean unit rather than the mid-fight state it died in, which is exactly why
// nothing here needs saving and restoring.
//
// Must be called under s.mu.
func (s *GameState) killUnitToCorpseLocked(unit *Unit) {
	if unit == nil || unit.Dead {
		return
	}
	s.tearDownDeadUnitLocked(unit.ID)
	// Out of the live registry and into the corpse list. This is what keeps
	// every existing `range s.Units` loop correct without auditing it: a body
	// is not a unit, and getUnitByIDLocked will not resolve one.
	s.removeUnitByIDLocked(unit.ID)
	unit.Dead = true
	unit.CorpseRemaining = corpseLifetimeSeconds
	if s.corpsesByID == nil {
		s.corpsesByID = make(map[int]*Unit, 8)
	}
	s.Corpses = append(s.Corpses, unit)
	s.corpsesByID[unit.ID] = unit
	// The corpse's own combat/order state, cleared for the same reason the
	// tear-down clears everyone else's references to it: a body must not be
	// carrying a half-finished swing or a standing order when it decays — or,
	// later, when it is revived.
	unit.AttackTargetID = 0
	unit.AttackBuildingTargetID = ""
	unit.AttackWindupRemaining = 0
	unit.AttackCooldown = 0
	unit.Attacking = false
	unit.Casting = false
	unit.Order = OrderState{Type: OrderIdle}
	unit.Status = "Dead"
	unit.Path = nil
}

// tearDownDeadUnitLocked drops every reference the rest of the world holds to
// unitID, without removing the unit from the registry. Shared by
// removeUnitLocked and killUnitToCorpseLocked so a corpse and a despawn tear
// down identically.
func (s *GameState) tearDownDeadUnitLocked(unitID int) {
	// Clean up any active channel the dying unit was running, and clear the
	// channel state on any unit that was targeting this unit's beam.
	// Do this while the unit is still resolvable inside
	// stopUnitChannelLocked / clearChannelStateLocked.
	if u, ok := s.unitsByID[unitID]; ok && u != nil {
		if u.ChannelAbilityID != "" {
			// Caster died — clear without a reason (no UI feedback needed).
			s.clearUnitChannelLocked(u)
		}
	}
	// If this unit was the TARGET of someone else's beam, drop that beam
	// immediately. The channel tick will also catch it next tick, but
	// removing here keeps visual state clean within the same tick.
	s.removeBeamForTargetLocked(unitID)

	// If this unit belongs to a neutral camp, strip it from the camp's
	// alive list before the unit is deleted from the registry.
	if u, ok := s.unitsByID[unitID]; ok && u != nil && u.NeutralCampID != "" {
		s.onUnitRemovedFromCampLocked(unitID, u.NeutralCampID)
	}

	// Drop in-flight projectiles involving this unit so stale IDs don't linger.
	s.cullProjectilesLocked(func(p *Projectile) bool {
		return p.OwnerUnitID == unitID || p.TargetUnitID == unitID
	})

	// Clear attack targets pointing to removed unit
	for _, u := range s.Units {
		if u.AttackTargetID == unitID {
			u.AttackTargetID = 0
			// Cancel any in-flight swing aimed at the dying unit. Without
			// this the attacker keeps a non-zero AttackWindupRemaining that
			// the windup-at-top block in tickUnitCombatLocked keeps decaying
			// and re-asserting Status="Attacking" on, even though the target
			// is gone — producing a desync where the animation visibly
			// continues to swing at nothing, then whiffs at damage time.
			u.AttackWindupRemaining = 0
			// Reset cooldown so the next swing-on-a-new-target's animation
			// hit frame aligns with damage delivery. AttackCooldown does
			// NOT decay while the unit is walking (out of range) with a
			// target set, so without this reset the attacker arrives at
			// the new target carrying the full post-swing cooldown from
			// the previous engagement; the client animation anchors at
			// "Status=Attacking became true" but damage waits for the
			// stale cooldown to bleed off, producing the "popup appears
			// a second after the visible hit" feel that grows with each
			// kill in a chain. Conceptually: the swing that killed already
			// consumed the cooldown, so the next engagement starts fresh.
			// Gameplay note: this is a small DPS uplift for any unit that
			// kills in one hit (chain-killing weak targets).
			u.AttackCooldown = 0
			u.Attacking = false
			u.Status = "Idle"
			// If the player's explicit attack order was on this unit, demote
			// it back to Idle so non-combat attackers (workers) return to
			// passive state and combat attackers resume default AI targeting.
			// Mirrors the demotion in clearCombatTargetLocked.
			if u.Order.Type == OrderAttackTarget {
				u.Order = OrderState{Type: OrderIdle}
			}
		}
		delete(u.ThreatTable, unitID)
		delete(u.TankedDamageByUnit, unitID)
		delete(u.DamageDealtByUnit, unitID)
		if u.TauntedByUnitID == unitID {
			u.TauntedByUnitID = 0
			u.TauntRemaining = 0
		}
	}
	// Forfeit banked damage-dealt XP on any building: if this unit is dead it
	// can no longer earn XP, so strip its entries from every building's map.
	for buildingID, m := range s.buildingDamageDealt {
		delete(m, unitID)
		if len(m) == 0 {
			delete(s.buildingDamageDealt, buildingID)
		}
	}
}
