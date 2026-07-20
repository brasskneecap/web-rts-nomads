package game

import (
	"fmt"
	"math"

	"webrts/server/pkg/protocol"
)

// defaultProjectileSpeed is the world-space travel speed in pixels/second.
// Archer AttackRange is ~200–300px, so a max-range shot lands in ~0.4–0.6s.
const defaultProjectileSpeed = 500.0

// minProjectileFlightSeconds guarantees a visible arc even when attacker and
// target are essentially on top of each other.
const minProjectileFlightSeconds = 0.05

// pierceFlightSeconds is the fixed flight time for Marksman silver pierce
// shots. The shot is a green wind streak (not a physical arrow), so flight
// time is decoupled from distance — long-range pierces look as snappy as
// short-range ones. ~0.25s reads as a quick zip without being instant.
const pierceFlightSeconds = 0.25

// Projectile is an in-flight ranged shot. It homes on its target's current
// position each tick and lands damage (+ on-hit perk triggers) when
// RemainingSeconds hits zero. At fire time only the damage snapshot, the
// attacker's cooldown, and the archer combat gate are committed — the full
// damage pipeline is deferred to landProjectileLocked.
//
// Pierce arrows (set Pierce=true) opt into a different flight model: they
// travel in a fixed straight line from origin to (origin + dir × length),
// damaging enemies along the way as the arrow physically passes through
// their position. They never call landProjectileLocked — damage is applied
// during traversal in tickProjectilesLocked.
type Projectile struct {
	ID            string
	OwnerUnitID   int
	OwnerPlayerID string

	TargetUnitID int

	// OriginUnitID is the unit the client anchors this bolt's SPAWN sprite to
	// (its chest), when the launch resolved a spawn origin that IS a unit —
	// e.g. a split bolt spawning FROM the enemy a preceding bolt hit
	// (spawnOrigin=current_event_position). 0 for a pure-position origin (the
	// centroid, a cast point) or an ordinary attack, where the client falls
	// back to OwnerUnitID for the chest lift — byte-identical to old behavior.
	// Mirrors Beam.CasterUnitID (see originUnitForSpawnLocked).
	OriginUnitID int

	OriginX, OriginY float64
	// TargetX/Y is refreshed each tick from the target unit so the client can
	// render a homing arc that doesn't get outrun by moving targets. For
	// pierce arrows TargetX/Y is the FAR endpoint of the line and is NOT
	// refreshed — the arrow flies in a straight line.
	TargetX, TargetY float64

	TotalSeconds     float64
	RemainingSeconds float64

	// Damage is the armor-mitigated final damage snapshotted at fire time.
	Damage int

	// Variant is the client-side sprite key — defaults to attacker.UnitType.
	// Perks may override it at fire time for alternate shot visuals.
	Variant string

	// FollowEffect is the optional effect id (see effect_defs.go) that plays
	// continuously on the projectile while it travels. Empty = none (the
	// projectile sprite is the only visual). Set from
	// ProjectileDef.FollowEffect via followEffectForProjectileDef when
	// projectiles are spawned from a ProjectileDef (Part 7); existing
	// procedurally-fired projectiles leave it "" and are unaffected.
	FollowEffect string

	// ImpactEffect is the optional effect id played on the target unit when
	// this projectile reaches it (see landProjectileLocked). Set from
	// ProjectileDef.ImpactEffect via impactEffectForProjectileDef when
	// projectiles are spawned from a def (Part 7); existing procedurally-fired
	// projectiles leave it "" and are unaffected.
	ImpactEffect string

	// DamageType is the element/school of this shot (Part 2), copied from the
	// attacker's AttackDamageType at fire time. Flavor/metadata only today —
	// it rides on the projectile as the seam future resistance logic / client
	// tinting will read. Empty ⇒ physical (DamageType.OrPhysical()).
	DamageType DamageType

	// Scale is the render-size multiplier for the projectile sprite, snapshot
	// from the firing unit's ProjectileScale at fire time and forwarded to
	// the client as ProjectileSnapshot.Scale. Purely visual — never read by
	// the simulation, flight, or damage logic. 0 ⇒ the client's default 1×.
	Scale float64

	// ── Pierce (Marksman silver) ───────────────────────────────────────────
	// When true the arrow flies in a fixed straight line and damages enemies
	// as it passes through them rather than homing on a single target. All
	// pierce fields are zero on regular projectiles.
	Pierce bool
	// PierceMaxHits caps how many distinct enemies the arrow can damage
	// (primary + secondaries) before despawning. Prevents runaway DPS through
	// a packed line of enemies.
	PierceMaxHits int
	// PierceSecondaryMult scales damage on enemies other than the original
	// targeted unit. The original target takes full Damage.
	PierceSecondaryMult float64
	// PierceCorridorWidth is the perpendicular-distance window (in world px)
	// that counts as "in the line of fire" for hit detection.
	PierceCorridorWidth float64
	// PierceLength is the total length of the line in world px.
	PierceLength float64
	// PierceDirX/PierceDirY is the unit-vector direction of the arrow.
	PierceDirX, PierceDirY float64
	// PierceHits records unit IDs already damaged by this arrow so the same
	// enemy is never double-hit, even if they re-enter the corridor.
	PierceHits []int

	// ── Double Shot (Marksman gold) — visual flag ─────────────────────────
	// When true this projectile is the SECOND of a Double Shot pair. The
	// client uses the flag to render a yellow combined damage number after
	// the second arrow lands (sum of both arrows' damage to the same target).
	DoubleShotSecond bool

	// ── Crit visual flag ───────────────────────────────────────────────────
	// When true the projectile's damage was rolled as a critical hit at fire
	// time (rollCritDamage returned > 1.0). Server-only — when the projectile
	// lands, the damage application site queues a critEvent so the client can
	// render a red circle behind the floating damage number. Pierce arrows
	// propagate this flag to every victim along the line.
	IsCrit bool

	// SkipOnHitEffects marks a projectile that must NOT run the on-hit reaction
	// hub when it lands — its damage is applied directly via
	// applyUnitDamageWithSourceLocked. Set on equipment-proc bolts so a proc
	// cannot trigger another proc (mirrors the non-recursion discipline of
	// base-stat splash, which also bypasses resolveAttackHitLocked). Also set on
	// ability bolts (arcane_bolt) so the spell's damage is self-contained.
	SkipOnHitEffects bool

	// SourceKind overrides the DamageSource.Kind used when a SkipOnHitEffects
	// bolt lands. Empty ⇒ "item-proc" (the original equipment-proc behaviour).
	// Ability bolts set "ability" so battle-tracker metrics and attribution
	// classify the damage as spell damage, matching the instant-cast path.
	SourceKind string

	// SourceAbilityID names the ability that spawned an ability bolt (set by
	// fireAbilityProjectileLocked = def.ID). Empty on basic-attack and proc
	// bolts. Used on land to identify which spell landed — e.g. Arcane Missile
	// bolts (a charge-fire passive) route through the Arch Mage on-hit perk
	// hooks. Informational only; does not affect damage.
	SourceAbilityID string

	// MinorDamage: when set on a SkipOnHitEffects (ability) bolt, its landed
	// damage renders as a smaller side-falling popup (colored by DamageType)
	// instead of the main floating number — see Arcane Missiles. The major
	// color hint is suppressed and a minor damage event is recorded on land.
	MinorDamage bool

	// SlowMultiplier / SlowDurationSeconds: an on-hit chill carried from a
	// proc's config (see ItemOnHitProc). When both are set, landing this bolt
	// slows the unit it hits (attack + move speed × SlowMultiplier for the
	// duration) via ApplySlowLocked. Zero on ordinary projectiles.
	SlowMultiplier      float64
	SlowDurationSeconds float64
	// BurnDamagePerSecond / BurnDurationSeconds: an on-hit burn carried from a
	// proc bolt (fire_sword). On land (SkipOnHitEffects branch) the bolt
	// ignites the unit it hits with a fire DoT for BurnDurationSeconds. Zero ⇒
	// no burn.
	BurnDamagePerSecond float64
	BurnDurationSeconds float64

	// AbilitySplashRadius: when > 0, this (ability) bolt deals its Damage as
	// AREA splash on impact — every hostile within the radius of the impact
	// point is damaged, reusing the base-stat splash payload
	// (applyAbilitySplashDamageLocked). Set from the effective spell's Radius
	// (fireball). 0 ⇒ single-target impact (the prior behaviour). Only honoured
	// on SkipOnHitEffects ability bolts.
	AbilitySplashRadius float64

	// ── Arcane Orb (arch-mage-spell-system) ────────────────────────────────
	// When ArcaneOrb is true this projectile is a slow-moving vortex: it flies
	// a straight line (like a pierce arrow — PierceLength / PierceDirX/Y / Origin
	// drive the path) but deals NO impact damage. Instead, each tick it drags
	// every hostile within ArcaneOrbRadius of its CURRENT position toward the
	// orb at ArcaneOrbPullStrength (a moving vortex — see
	// tickArcaneOrbProjectileLocked). It despawns when it reaches the end of its
	// path. Fired unit-targeted; the direction is caster→target at cast time.
	ArcaneOrb             bool
	ArcaneOrbRadius       float64
	ArcaneOrbPullStrength float64
	// ArcaneOrbDamagePerSecond is the DoT rate dealt to hostiles within the
	// orb's radius as it travels. RENDERING/LEGACY-FALLBACK ONLY as of the
	// genuine-composition fix (see TickActions below): fireProjectileTickLocked
	// reads this (and ArcaneOrbRadius/ArcaneOrbPullStrength/ArcaneOrbDamageType)
	// ONLY when TickActions is empty — the legacy (pre-migration, SchemaVersion
	// <2) cast leg, and any hand-built Projectile a test constructs directly.
	// When TickActions is populated (the composable executor leg), the actual
	// tick math reads the AUTHORED actions instead — this field is still set
	// for parity/introspection (TestArcaneOrb_* asserts against it) but is no
	// longer consulted for damage on that leg. Applied on a fixed cadence
	// (arcaneOrbDamageIntervalSeconds / TickInterval below) so the per-second
	// total is exactly the authored value regardless of tick rate — a moving
	// damage-over-time vortex. 0 ⇒ pure CC. ArcaneOrbDamageTickTimer
	// accumulates elapsed time toward the next damage tick (legacy-fallback
	// leg only — the composed leg's per-action throttle state lives in
	// TickActionTimers instead).
	ArcaneOrbDamagePerSecond float64
	ArcaneOrbDamageTickTimer float64
	// ArcaneOrbDamageType is the school of the orb's vortex damage (legacy-
	// fallback leg only — see ArcaneOrbDamagePerSecond's doc comment).
	ArcaneOrbDamageType DamageType
	// TickInterval is the on_projectile_tick damage cadence (seconds) — the
	// authored/compiled value from launchProjectileConfig.TickInterval (see
	// that field's doc comment, ability_compile.go), stamped here by
	// spawnArcaneOrbLocked so tickArcaneOrbProjectileLocked never has to fall
	// back to the package constant arcaneOrbDamageIntervalSeconds directly.
	// Both the frozen-legacy-fixture leg (which always passes
	// arcaneOrbDamageIntervalSeconds) and the executor leg (which passes
	// c.TickInterval, baked from the same constant at compile time) set the
	// SAME value today, so this is not a behavior change — just threading a
	// previously-implicit constant through explicit data, matching every
	// other composable field's discipline (AI_RULES: no hidden state).
	TickInterval float64

	// ── Composable tick (launch_projectile's TickInterval>0 vortex shape) ──
	// TickActions is non-nil exactly for a vortex spawned by the composable
	// launch_projectile executor (executeTickingVortexShimLocked): the
	// AUTHORED on_projectile_tick trigger's own actions (select_targets/
	// apply_force/deal_damage — including apply_force's Mode, e.g. "push"),
	// with select_targets' radius and apply_force's strength already frozen
	// to their launch-time-folded values (freezeVortexTickActions,
	// ability_exec_projectile.go — apply_force has no fold seam of its own),
	// carried across tick boundaries as plain data (AbilityActionDef — never
	// *Unit, per AI_RULES). nil for the legacy (pre-migration) cast leg and
	// any hand-built Projectile a test constructs directly — those fall back
	// to the frozen ArcaneOrbRadius/PullStrength/DamagePerSecond fields'
	// hardcoded math in fireProjectileTickLocked, unchanged from before this
	// fix. See fireProjectileTickLocked's doc comment for the full dispatch.
	TickActions []AbilityActionDef
	// TickActionTimers accumulates elapsed time, per TickActions entry (keyed
	// by AbilityActionDef.ID), toward that action's next due firing when it
	// declares Timing.TickInterval > 0 (arcane_orb's "dmg" action — see
	// AbilityActionDef.Timing's doc comment). Lazily initialized by
	// fireProjectileTickLocked's composed branch on first use; nil for every
	// projectile that never runs a throttled composed action.
	TickActionTimers map[string]float64
	// TickOpsBudget is the shared, cross-tick op-budget counter this orb's
	// composed tick firings decrement every call — mirrors ImpactOpsBudget's
	// identical role for the impact path (see ability_exec_projectile.go's
	// CROSS-TICK OP BUDGET section): bounds the TOTAL executor work this one
	// orb can do across its ENTIRE flight (however many ticks that spans),
	// not just per-call. nil exactly when TickActions is nil (the legacy/
	// hand-built leg does no executor work here at all).
	TickOpsBudget *int

	// ── Composable impact (launch_projectile's redesign) ──────────────────
	// ImpactActions is non-nil exactly for a bolt spawned by the composable
	// launch_projectile action for a NON-chain ability (arcane_bolt/
	// fireball's migrated shape, and any future ability authored the same
	// way): the compiled on_projectile_impact trigger's actions, carried
	// across tick boundaries as plain data (AbilityActionDef — never *Unit,
	// per AI_RULES), same discipline as AbilityZone.Triggers /
	// scheduledMarker.actions. landProjectileLocked branches on this BEFORE
	// the legacy SkipOnHitEffects baked-damage path (see that function) —
	// this bolt's Damage field is unused/0; all of its impact behavior lives
	// here instead.
	ImpactActions []AbilityActionDef
	// ImpactOpsBudget is the shared, cross-tick op-budget counter this bolt's
	// impact will decrement when it fires — see
	// ability_exec_projectile.go's CROSS-TICK OP BUDGET section. Shared BY
	// POINTER (not copied) with every other projectile descended from the
	// same original cast, so the WHOLE lineage's total work is bounded by
	// one number. nil only for a projectile that isn't part of a composable
	// impact lineage at all (ImpactActions is also nil then).
	ImpactOpsBudget *int
	// ImpactDamageMultiplier carries the LAUNCHING ctx's
	// damageEffectivenessMultiplier (ctx.effectiveDamageMultiplier()) forward
	// to the impact ctx fireProjectileImpactLocked builds on a later tick —
	// a fresh ctx otherwise has no way to know a caller (e.g. Unstable
	// Magic's reduced-effectiveness free proc) customized the launching
	// cast's EffectiveSpell. 0 is treated as 1.0 (no scaling) by
	// RuntimeAbilityContext.effectiveDamageMultiplier(), matching every
	// ordinary cast.
	ImpactDamageMultiplier float64
	// DirectionalImpact marks a launch_projectile "direction" travelMode
	// bolt: it flies a fixed straight line (PierceLength/PierceDirX/PierceDirY/
	// Origin, reusing arcane_orb's geometry) rather than homing on
	// TargetUnitID (0 for this bolt), and fires its impact on the FIRST
	// hostile its path crosses, or at the end of its flight if none — see
	// launchDirectionalProjectileLocked / tickDirectionalImpactProjectileLocked
	// for the full design.
	DirectionalImpact bool
}

func (s *GameState) fireProjectileLocked(attacker, target *Unit, damage int) {
	// Pierce arrows fly in a fixed straight line out to attacker.AttackRange
	// rather than homing on the target — see firePierceProjectileLocked.
	if containsString(attacker.PerkIDs, "pierce") {
		s.firePierceProjectileLocked(attacker, target, damage)
	} else {
		s.fireHomingProjectileLocked(attacker, target, damage, "")
	}
	// Marksman fire-time effects (split shot, double shot timer arm) run AFTER
	// the primary projectile is queued so the player sees the primary leave
	// the bow first, then split arrows fan out alongside it. The recursion
	// guard inside onMarksmanProjectileFiredLocked prevents the spawned
	// secondary projectiles from re-entering this dispatch.
	s.onMarksmanProjectileFiredLocked(attacker, target, damage)
}

// fireHomingProjectileLocked spawns a single homing projectile that retargets
// the unit each tick. The default archer / ranged-unit shot. variant overrides
// the on-the-wire variant string when non-empty (used to mark double-shot's
// second arrow so the client can render a combined damage number).
func (s *GameState) fireHomingProjectileLocked(attacker, target *Unit, damage int, variant string) {
	// A unit may declare a projectile asset (UnitDef.projectile → ProjectileID,
	// e.g. the Acolyte's "fire_bolt"). When set and resolvable, the shot
	// uses that def's speed and carries its follow/impact effects, and the
	// wire Variant defaults to the projectile id so the client renders that
	// sprite. Unset (the archer / default case) is unchanged: defaultProjectile
	// Speed, no follow/impact effect, Variant = UnitType. The explicit
	// `variant` arg (double-shot marker) still wins when non-empty.
	speed := defaultProjectileSpeed
	var followEffect, impactEffect string
	if attacker.ProjectileID != "" {
		if def, ok := getProjectileDef(attacker.ProjectileID); ok {
			speed = def.Speed
			followEffect = followEffectForProjectileDef(def)
			impactEffect = impactEffectForProjectileDef(def)
			if variant == "" {
				variant = attacker.ProjectileID
			}
		}
	}

	dx := target.X - attacker.X
	dy := target.Y - attacker.Y
	travelTime := math.Sqrt(dx*dx+dy*dy) / speed
	if travelTime < minProjectileFlightSeconds {
		travelTime = minProjectileFlightSeconds
	}

	id := fmt.Sprintf("proj_%d", s.nextProjectileID)
	s.nextProjectileID++

	if variant == "" {
		variant = attacker.UnitType
	}
	s.Projectiles = append(s.Projectiles, &Projectile{
		ID:               id,
		OwnerUnitID:      attacker.ID,
		OwnerPlayerID:    attacker.OwnerID,
		TargetUnitID:     target.ID,
		OriginX:          attacker.X,
		OriginY:          attacker.Y,
		TargetX:          target.X,
		TargetY:          target.Y,
		TotalSeconds:     travelTime,
		RemainingSeconds: travelTime,
		Damage:           damage,
		Variant:          variant,
		FollowEffect:     followEffect,
		ImpactEffect:     impactEffect,
		DamageType:       attacker.AttackDamageType,
		Scale:            attacker.ProjectileScale,
	})
}

// fireProcProjectileLocked spawns a homing projectile for a proc effect fired
// from src. It carries the effect's own Damage/DamageType (not a unit's
// attack type) and sets SkipOnHitEffects so landing applies damage directly
// without re-entering the on-hit hub. Non-unit sources (src.OwnerUnitID == 0)
// launch from src.OriginX/Y with no kill credit. Must be called under s.mu.
func (s *GameState) fireProcProjectileLocked(src ProcSource, target *Unit, p ProcEffectParams) {
	speed := defaultProjectileSpeed
	var followEffect, impactEffect string
	if def, ok := getProjectileDef(p.ProjectileID); ok {
		speed = def.Speed
		followEffect = followEffectForProjectileDef(def)
		impactEffect = impactEffectForProjectileDef(def)
	}

	dx := target.X - src.OriginX
	dy := target.Y - src.OriginY
	travelTime := math.Sqrt(dx*dx+dy*dy) / speed
	if travelTime < minProjectileFlightSeconds {
		travelTime = minProjectileFlightSeconds
	}

	id := fmt.Sprintf("proj_%d", s.nextProjectileID)
	s.nextProjectileID++

	// Params-authored scale wins when set; otherwise inherit the firing
	// unit's scale (the prior behavior — resolved at point of use, within
	// this tick). Both are "0 ⇒ client default 1×". The variant falls back to
	// the firing unit's type only for hand-built params with no ProjectileID;
	// catalog-loaded effects always name one (validated at load).
	variant := p.ProjectileID
	scale := p.ProjectileScale
	if scale <= 0 || variant == "" {
		if owner := s.getUnitByIDLocked(src.OwnerUnitID); owner != nil {
			if scale <= 0 {
				scale = owner.ProjectileScale
			}
			if variant == "" {
				variant = owner.UnitType
			}
		}
	}
	s.Projectiles = append(s.Projectiles, &Projectile{
		ID:                  id,
		OwnerUnitID:         src.OwnerUnitID,
		OwnerPlayerID:       src.OwnerPlayerID,
		TargetUnitID:        target.ID,
		OriginX:             src.OriginX,
		OriginY:             src.OriginY,
		TargetX:             target.X,
		TargetY:             target.Y,
		TotalSeconds:        travelTime,
		RemainingSeconds:    travelTime,
		Damage:              p.Damage,
		Variant:             variant,
		FollowEffect:        followEffect,
		ImpactEffect:        impactEffect,
		DamageType:          p.DamageType,
		Scale:               scale,
		SkipOnHitEffects:    true,
		SlowMultiplier:      p.SlowMultiplier,
		SlowDurationSeconds: p.SlowDurationSeconds,
		BurnDamagePerSecond: p.BurnDamagePerSecond,
		BurnDurationSeconds: p.BurnDurationSeconds,
	})
}

// fireAbilityProjectileLocked spawns a homing bolt that delivers an offensive
// ability's damage on impact instead of instantly. It carries the ability's own
// DamageAmount + DamageType and the caster's kill credit (OwnerUnitID), uses the
// ability's projectile def for speed/follow/impact visuals, and sets
// SkipOnHitEffects so the spell's damage is self-contained — landing it applies
// the typed damage directly (Kind "ability") without re-entering the attack
// on-hit hub (no accidental item-proc / elemental re-trigger). Mirrors
// fireProcProjectileLocked but sourced from a live caster and an AbilityDef.
// Caller holds s.mu write lock.
func (s *GameState) fireAbilityProjectileLocked(caster, target *Unit, def AbilityDef, eff EffectiveSpell) {
	speed := defaultProjectileSpeed
	var followEffect, impactEffect string
	if pdef, ok := getProjectileDef(def.Projectile); ok {
		speed = pdef.Speed
		followEffect = followEffectForProjectileDef(pdef)
		impactEffect = impactEffectForProjectileDef(pdef)
	}
	// A modifier-supplied projectile speed overrides the def's speed (0 ⇒ keep
	// the projectile def's own speed).
	if eff.ProjectileSpeed > 0 {
		speed = eff.ProjectileSpeed
	}

	dx := target.X - caster.X
	dy := target.Y - caster.Y
	travelTime := math.Sqrt(dx*dx+dy*dy) / speed
	if travelTime < minProjectileFlightSeconds {
		travelTime = minProjectileFlightSeconds
	}

	id := fmt.Sprintf("proj_%d", s.nextProjectileID)
	s.nextProjectileID++

	// Render scale is owned by the ABILITY, not the caster: a spell's visuals
	// are authored per-ability (e.g. arcane_missiles at 0.5 for smaller bolts).
	// The caster unit's ProjectileScale governs only its BASIC ATTACK, never its
	// abilities. 0/absent ⇒ the client's default 1×.
	scale := def.ProjectileScale

	s.Projectiles = append(s.Projectiles, &Projectile{
		ID:                  id,
		OwnerUnitID:         caster.ID,
		OwnerPlayerID:       caster.OwnerID,
		TargetUnitID:        target.ID,
		OriginX:             caster.X,
		OriginY:             caster.Y,
		TargetX:             target.X,
		TargetY:             target.Y,
		TotalSeconds:        travelTime,
		RemainingSeconds:    travelTime,
		Damage:              eff.Damage,     // effective (modifier-folded) damage
		AbilitySplashRadius: eff.Radius,     // fireball splash; 0 ⇒ single-target impact
		Variant:             def.Projectile, // client renders the projectile sprite by id
		FollowEffect:        followEffect,
		ImpactEffect:        impactEffect,
		DamageType:          def.DamageType.OrPhysical(),
		Scale:               scale,
		SkipOnHitEffects:    true,
		SourceKind:          "ability",
		SourceAbilityID:     def.ID,
		MinorDamage:         def.MinorDamage,
	})
}

// fireProjectileWithImpactActionsLocked spawns a homing ("to_target"
// travelMode) bolt that carries a COMPOSED on_projectile_impact trigger
// instead of baked damage — launch_projectile's redesigned non-chain shape
// (arcane_bolt/fireball's migrated shape). Mirrors fireAbilityProjectileLocked's
// spawn geometry (projectile-def speed/follow/impact-effect lookup, travel-
// time calc) exactly, but never bakes Damage/AbilitySplashRadius: this bolt's
// entire impact behavior is impactActions, run through the shared executor by
// landProjectileLocked -> fireProjectileImpactLocked once it lands, not this
// file's baked SkipOnHitEffects path. opsBudget is the shared cross-tick
// op-budget pointer this bolt's impact will decrement (see
// ability_exec_projectile.go's CROSS-TICK OP BUDGET section). damageMultiplier
// carries the launching ctx's effectiveDamageMultiplier() forward to impact
// time (see Projectile.ImpactDamageMultiplier's doc comment) — a caller-
// customized cast (Unstable Magic's reduced-effectiveness free proc) would
// otherwise deal full, un-scaled damage once the bolt lands on a later tick.
//
// DamageType is deliberately left unset (OrPhysical() at any point that
// reads it): it was informational/rendering-only on the legacy baked path,
// and the actual damage-popup color now comes from the impact's own
// deal_damage config, resolved independently at landing time — see
// fireProjectileImpactLocked.
//
// originPos is this bolt's SPAWN point — the caster's own position for every
// ability compiled/authored before launch_projectile's spawnOrigin field
// existed (resolveOriginLocked's default-case fallback), or a different
// resolved world position when the launching action authored a non-caster
// SpawnOrigin (e.g. a split bolt spawning at the enemy a preceding bolt just
// hit, rather than back at the original caster) — see
// launchProjectileConfig.SpawnOrigin's doc comment (ability_compile.go).
// caster/target identity (OwnerUnitID/OwnerPlayerID/TargetUnitID — kill
// credit and homing) are UNCHANGED by this: only the bolt's spawn geometry
// moves, never who cast it or who it's flying at.
//
// Caller holds s.mu.
func (s *GameState) fireProjectileWithImpactActionsLocked(caster, target *Unit, originPos protocol.Vec2, originUnitID int, projectileID string, scale float64, abilityID string, impactActions []AbilityActionDef, opsBudget *int, damageMultiplier float64) {
	speed := defaultProjectileSpeed
	var followEffect, impactEffect string
	if pdef, ok := getProjectileDef(projectileID); ok {
		speed = pdef.Speed
		followEffect = followEffectForProjectileDef(pdef)
		impactEffect = impactEffectForProjectileDef(pdef)
	}

	dx := target.X - originPos.X
	dy := target.Y - originPos.Y
	travelTime := math.Sqrt(dx*dx+dy*dy) / speed
	if travelTime < minProjectileFlightSeconds {
		travelTime = minProjectileFlightSeconds
	}

	id := fmt.Sprintf("proj_%d", s.nextProjectileID)
	s.nextProjectileID++

	s.Projectiles = append(s.Projectiles, &Projectile{
		ID:                     id,
		OwnerUnitID:            caster.ID,
		OwnerPlayerID:          caster.OwnerID,
		TargetUnitID:           target.ID,
		OriginUnitID:           originUnitID,
		OriginX:                originPos.X,
		OriginY:                originPos.Y,
		TargetX:                target.X,
		TargetY:                target.Y,
		TotalSeconds:           travelTime,
		RemainingSeconds:       travelTime,
		Variant:                projectileID,
		FollowEffect:           followEffect,
		ImpactEffect:           impactEffect,
		Scale:                  scale,
		SkipOnHitEffects:       true,
		SourceKind:             "ability",
		SourceAbilityID:        abilityID,
		ImpactActions:          impactActions,
		ImpactOpsBudget:        opsBudget,
		ImpactDamageMultiplier: damageMultiplier,
	})
}

// directionalImpactHitRadius is the point-hit-detection radius for a
// "direction" travelMode projectile: each tick, the FIRST hostile whose
// position is within this radius of the bolt's current point on its line
// counts as struck (see launchDirectionalProjectileLocked's IMPACT SEMANTICS
// doc). Matches firePierceProjectileLocked's default corridor half-width
// (28px) so a directional bolt's "hit window" reads the same size as a
// pierce arrow's corridor.
const directionalImpactHitRadius = 28.0

// directionalAimPointLocked resolves the world position a "direction"
// travelMode bolt aims its straight-line flight at: the FIRST live unit in
// targets (this action's own resolved target-set — see Execute's doc comment,
// ability_exec_projectile.go: its declared "source" query, an
// Input["targets"] ref, or a preceding select_targets action's output,
// exactly the same resolution every other action's `targets` parameter
// already gets) at LAUNCH time, else ctx.CastPoint (nothing resolved — a
// point-cast with no preceding selection, "fly toward the clicked ground
// point"). ctx.CastPoint is only ever populated by a POINT cast
// (resolveAbilityProgramCastLocked's callers — see ability_cast.go).
//
// Deliberately reads `targets`, NOT ctx.InitialTarget directly: the whole
// point of this action accepting a resolved target-set (Fix 2 of the
// targeting-shape correction) is that a PRECEDING select_targets action can
// narrow/redirect who a direction-mode bolt aims at — falling back to the
// cast's raw InitialTarget here would silently override that narrowing with
// a second, competing source of truth. When this action's own query is
// {Source: source_initial_target} (the same shape compileProjectileActions
// gives "to_target" mode), targets already resolves to exactly
// [ctx.InitialTarget], so the common single-target-cast case behaves
// identically either way — only the composed case (select_targets feeding a
// point-cast bolt) actually depends on reading `targets`.
//
// Caller holds s.mu.
func (s *GameState) directionalAimPointLocked(ctx *RuntimeAbilityContext, targets []int) (x, y float64) {
	if len(targets) > 0 {
		if t := s.getUnitByIDLocked(targets[0]); t != nil {
			return t.X, t.Y
		}
	}
	return ctx.CastPoint.X, ctx.CastPoint.Y
}

// launchDirectionalProjectileLocked spawns a NON-HOMING bolt that flies in a
// straight line from the caster toward directionalAimPointLocked's resolved
// aim point and keeps going for cfg.Distance world px (0/absent derives the
// distance from the caster-to-aim-point distance, mirroring
// spawnArcaneOrbLocked's identical fallback and its degenerate-geometry
// guard — dirX,dirY defaults to +X when caster and aim point coincide).
// Unlike "to_target" mode, the aim point is fixed at LAUNCH time and never
// re-targets a live unit, exactly like arcane_orb's straight flight.
//
// IMPACT SEMANTICS (a deliberate design choice — documented per the task
// that introduced "direction" travelMode; neither pierce nor arcane_orb ever
// fire an "impact" event at all, so there is no existing precedent to
// inherit): the bolt fires on_projectile_impact for the FIRST hostile unit
// its path crosses (nearest tick-of-detection wins; ties within one tick
// broken by ascending unit ID for determinism), exactly ONCE, then despawns
// immediately — it does NOT pierce through multiple victims like a Marksman
// pierce arrow. If it reaches the end of its Distance with no hostile
// crossed, it STILL fires on_projectile_impact once, at the flight endpoint,
// with no hit unit (CurrentEventUnitID 0) — this is what lets an author's
// impact trigger (e.g. a splash select_targets{origin: impact_position})
// resolve even on a whiff, mirroring how an instant point-AoE cast (shatter)
// still plays its burst VFX on a miss. A multi-hit "pierce with a composed
// impact per victim" mode is explicitly NOT implemented — nothing in the
// shipped catalog needs it (arcane_bolt/fireball are both "to_target").
//
// targets is this action's own resolved target-set (Execute's parameter,
// ability_exec_projectile.go), passed through unchanged for
// directionalAimPointLocked to read — see that function's doc comment for
// why it takes priority over ctx.InitialTarget.
//
// c.SpawnOrigin resolves (via s.resolveOriginLocked) the world position this
// bolt spawns FROM and flies its straight line relative to — the caster's
// own position for the unset/"caster" default (byte-identical with every
// ability compiled/authored before this field existed), or a different
// resolved position for a composed split bolt — see
// launchProjectileConfig.SpawnOrigin's doc comment (ability_compile.go). The
// AIM POINT (directionalAimPointLocked) is unaffected: spawn origin and aim
// point are orthogonal — where the bolt starts vs. what it's flying toward.
//
// Caller holds s.mu.
func (s *GameState) launchDirectionalProjectileLocked(caster *Unit, ctx *RuntimeAbilityContext, c launchProjectileConfig, impactActions []AbilityActionDef, opsBudget *int, targets []int) {
	originPos := s.resolveOriginLocked(ctx, c.SpawnOrigin, c.SpawnOriginRef)
	originUnitID := s.originUnitForSpawnLocked(ctx, c.SpawnOrigin, c.SpawnOriginRef)
	aimX, aimY := s.directionalAimPointLocked(ctx, targets)
	dx := aimX - originPos.X
	dy := aimY - originPos.Y
	dist := math.Sqrt(dx*dx + dy*dy)
	dirX, dirY := 1.0, 0.0
	if dist > 0 {
		dirX, dirY = dx/dist, dy/dist
	}
	distance := c.Distance.Resolve(caster)
	if distance <= 0 {
		distance = dist
	}
	if distance <= 0 {
		// Degenerate: no authored Distance and the aim point coincides with
		// the spawn origin (e.g. a self-cast with no point/target). Nothing to
		// fly — fail safe rather than spawn a zero-length bolt.
		ctx.trace("action_skipped", ctx.currentActionPath, map[string]any{"reason": "zero_distance"})
		return
	}

	speed := defaultProjectileSpeed
	var followEffect, impactEffect string
	if pdef, ok := getProjectileDef(c.Projectile); ok {
		speed = pdef.Speed
		followEffect = followEffectForProjectileDef(pdef)
		impactEffect = impactEffectForProjectileDef(pdef)
	}
	travelTime := distance / speed
	if travelTime < minProjectileFlightSeconds {
		travelTime = minProjectileFlightSeconds
	}

	id := fmt.Sprintf("proj_%d", s.nextProjectileID)
	s.nextProjectileID++

	s.Projectiles = append(s.Projectiles, &Projectile{
		ID:                     id,
		OwnerUnitID:            caster.ID,
		OwnerPlayerID:          caster.OwnerID,
		TargetUnitID:           0, // no homing target — see DirectionalImpact
		OriginUnitID:           originUnitID,
		OriginX:                originPos.X,
		OriginY:                originPos.Y,
		TargetX:                originPos.X + dirX*distance,
		TargetY:                originPos.Y + dirY*distance,
		TotalSeconds:           travelTime,
		RemainingSeconds:       travelTime,
		Variant:                c.Projectile,
		FollowEffect:           followEffect,
		ImpactEffect:           impactEffect,
		Scale:                  c.ProjectileScale,
		SkipOnHitEffects:       true,
		SourceKind:             "ability",
		SourceAbilityID:        ctx.AbilityID,
		ImpactActions:          impactActions,
		ImpactOpsBudget:        opsBudget,
		ImpactDamageMultiplier: ctx.effectiveDamageMultiplier(),
		DirectionalImpact:      true,
		PierceLength:           distance,
		PierceDirX:             dirX,
		PierceDirY:             dirY,
	})
	ctx.trace("projectile_launched", ctx.currentActionPath, map[string]any{"travelMode": travelModeDirection, "distance": distance})
}

// tickDirectionalImpactProjectileLocked advances a "direction" travelMode
// projectile by dt along its fixed line and looks for the first hostile unit
// within directionalImpactHitRadius of its CURRENT position (ties broken by
// ascending unit ID). Returns the hit unit (nil if none yet), the bolt's
// current world position, and whether its flight is OVER (a hit was found,
// or it reached the end of its Distance) — the caller fires the impact and
// drops the projectile when over is true, otherwise keeps it for the next
// tick. Caller holds s.mu.
func (s *GameState) tickDirectionalImpactProjectileLocked(proj *Projectile, dt float64) (hit *Unit, curX, curY float64, over bool) {
	if proj.PierceLength <= 0 || proj.TotalSeconds <= 0 {
		return nil, proj.OriginX, proj.OriginY, true // malformed — end immediately
	}
	proj.RemainingSeconds -= dt
	remaining := math.Max(0, proj.RemainingSeconds)
	along := proj.PierceLength * (1.0 - remaining/proj.TotalSeconds)
	if along > proj.PierceLength {
		along = proj.PierceLength
	}
	curX = proj.OriginX + proj.PierceDirX*along
	curY = proj.OriginY + proj.PierceDirY*along

	attacker := s.getUnitByIDLocked(proj.OwnerUnitID)
	radSq := directionalImpactHitRadius * directionalImpactHitRadius
	var best *Unit
	for _, u := range s.Units {
		if u == nil || u.HP <= 0 || !u.Visible || u.ID == proj.OwnerUnitID {
			continue
		}
		if attacker != nil {
			if !s.playersAreHostileLocked(u.OwnerID, attacker.OwnerID) {
				continue
			}
		} else if s.playersAreFriendlyLocked(u.OwnerID, proj.OwnerPlayerID) {
			continue // attacker gone: skip allies to avoid friendly fire
		}
		ddx := u.X - curX
		ddy := u.Y - curY
		if ddx*ddx+ddy*ddy > radSq {
			continue
		}
		if best == nil || u.ID < best.ID {
			best = u
		}
	}
	if best != nil {
		return best, curX, curY, true
	}
	if proj.RemainingSeconds <= 0 {
		return nil, curX, curY, true
	}
	return nil, curX, curY, false
}

// fireProjectileImpactLocked runs proj's compiled on_projectile_impact
// actions through the shared executor instead of the legacy baked-damage
// path — see landProjectileLocked's branch and Projectile.ImpactActions'
// doc comment. hitUnitID is 0 for a "direction" bolt's end-of-flight-no-hit
// case (see launchDirectionalProjectileLocked); impactX/Y is the world
// position the bolt actually stopped at.
//
// Builds a fresh RuntimeAbilityContext from the projectile's own carried
// ids (CasterID = OwnerUnitID, AbilityID = SourceAbilityID, InitialTarget =
// TargetUnitID — no new fields duplicate these), binds CurrentEventUnitID to
// the hit unit (mirrors fireAbilityZoneOccupancyEventLocked's
// CurrentEventUnitID convention for "the unit this event centers on" — the
// SrcCurrentEvent case in candidatePoolIDsLocked, ability_exec_targeting.go).
// abilityDef/program are re-resolved via getAbilityDef — the SAME
// overlay-first resolver every other executor entry point uses (matching
// fireScheduledMarkerLocked) — so deal_damage folds the caster's spell
// modifiers exactly once, at parity with the direct-cast path (see
// ability_exec_projectile.go's DOUBLE-FOLD note).
//
// sharedOpsRemaining is proj.ImpactOpsBudget: the SAME shared counter this
// bolt was seeded with at launch (see the CROSS-TICK OP BUDGET section) —
// decremented here exactly like any other executeActionLocked call, so an
// impact that itself relaunches a projectile continues to draw down the ONE
// shared total instead of resetting.
//
// Does not append to any deadUnitIDs-style out-param: a lethal deal_damage
// already routed the kill through applyUnitDamageWithSourceLocked's shared
// pending-death drain (keyed on Kind=="ability", which this path's
// deal_damage sets — see DamageSource.SourceAbilityID's doc), the same
// carve-out the legacy SkipOnHitEffects+SourceKind=="ability" branch in
// landProjectileLocked already relies on.
//
// Caller holds s.mu.
func (s *GameState) fireProjectileImpactLocked(proj *Projectile, hitUnitID int, impactX, impactY float64) {
	def, ok := getAbilityDef(proj.SourceAbilityID)
	ctx := &RuntimeAbilityContext{
		CasterID:           proj.OwnerUnitID,
		AbilityID:          proj.SourceAbilityID,
		InitialTarget:      proj.TargetUnitID,
		ImpactPosition:     protocol.Vec2{X: impactX, Y: impactY},
		EventPosition:      protocol.Vec2{X: impactX, Y: impactY},
		CurrentEventUnitID: hitUnitID,
		Named:              map[string]ContextValue{},
		Trace:              s.previewTrace,
		now:                s.previewClock,
		sharedOpsRemaining: proj.ImpactOpsBudget,
		// Carry the launching ctx's caller-customized effectiveness forward —
		// see Projectile.ImpactDamageMultiplier's doc comment. Zero (the
		// field's default for every ordinary bolt) is treated as 1.0 by
		// effectiveDamageMultiplier(), matching every non-customized cast.
		damageEffectivenessMultiplier: proj.ImpactDamageMultiplier,
	}
	if ok {
		ctx.program = def.Program
		ctx.abilityDef = &def
	}
	path := "on_projectile_impact"
	for i := range proj.ImpactActions {
		if ctx.opsExhausted() {
			break
		}
		s.executeActionLocked(ctx, &proj.ImpactActions[i], path)
	}
}

// fireProcBeamLocked handles a proc effect whose emitter def is
// EmitterKindBeam (e.g. lightning_chain's "lightning_bolt"). It spawns the
// momentary beam flash NOW (frozen endpoints let it render even if the target
// later dies) but DEFERS the damage by beamProcDamageDelaySeconds — a beam is
// otherwise instantaneous, so applying damage this tick would merge its
// number into the triggering hit's number. tickBeamsLocked lands the damage a
// beat later, bypassing the on-hit hub, so a proc can't trigger another proc.
//
// Non-unit sources: the primary flash leaves src.OriginX/Y with no caster
// unit; hostility for chain hops keys off src.OwnerPlayerID.
//
// Caller holds s.mu write lock.
func (s *GameState) fireProcBeamLocked(src ProcSource, target *Unit, p ProcEffectParams, def ProjectileDef) {
	variant := p.ProjectileID
	if variant == "" {
		variant = def.ID
	}
	impact := impactEffectForProjectileDef(def)

	// Primary hit: source → target. Damage is deferred (see the helper) so it
	// pops as its own number instead of merging into the triggering attack.
	primary := s.spawnMomentaryDamageBeamLocked(src, src.OwnerUnitID, src.OriginX, src.OriginY, target, variant, p.Damage, p.DamageType, impact, def.DurationMs, beamProcDamageDelaySeconds)
	primary.SlowMultiplier = p.SlowMultiplier
	primary.SlowDurationSeconds = p.SlowDurationSeconds
	primary.BurnDamagePerSecond = p.BurnDamagePerSecond
	primary.BurnDurationSeconds = p.BurnDurationSeconds

	// Optional chain: the bolt arcs to up to BounceCount further enemies.
	// Each hop leaps off the PREVIOUS victim to the nearest not-yet-hit
	// hostile within BounceRange, losing BounceDamageFalloff damage per hop
	// (25 → 20 → 15 with count=2, falloff=5). Kill credit always stays with
	// the source. Reuses the generic bounce picker shared with chain_siphon.
	if p.BounceCount <= 0 || p.BounceRange <= 0 {
		return
	}
	rangeSq := p.BounceRange * p.BounceRange
	// Exclude the primary target and the source unit from every hop so the
	// chain can't oscillate back onto an already-hit unit or the wielder.
	// A non-unit source (OwnerUnitID 0) matches no unit, so nothing extra is
	// excluded for it.
	excluded := make(map[int]struct{}, p.BounceCount+2)
	excluded[target.ID] = struct{}{}
	if src.OwnerUnitID != 0 {
		excluded[src.OwnerUnitID] = struct{}{}
	}
	cursor := target
	for hop := 1; hop <= p.BounceCount; hop++ {
		next := s.nearestChainBounceTargetLocked(src.OwnerPlayerID, cursor, rangeSq, excluded)
		if next == nil {
			break // chain fizzles: nothing eligible within range of the last victim
		}
		dmg := p.Damage - p.BounceDamageFalloff*hop
		if dmg <= 0 {
			break // fully attenuated — stop arcing
		}
		// Beam leaves the previous victim (cursor) but the hit still credits
		// the original source. The chill/burn rides each hop too.
		bounce := s.spawnMomentaryDamageBeamLocked(src, cursor.ID, cursor.X, cursor.Y, next, variant, dmg, p.DamageType, impact, def.DurationMs, beamProcDamageDelaySeconds)
		bounce.SlowMultiplier = p.SlowMultiplier
		bounce.SlowDurationSeconds = p.SlowDurationSeconds
		bounce.BurnDamagePerSecond = p.BurnDamagePerSecond
		bounce.BurnDurationSeconds = p.BurnDurationSeconds
		excluded[next.ID] = struct{}{}
		cursor = next
	}
}

// firePierceProjectileLocked spawns a fixed-line piercing projectile that
// travels from the attacker outward in the direction of `target`, all the way
// to attacker.AttackRange. The projectile damages enemies as it physically
// passes through their position (per-tick along-line check in
// tickProjectilesLocked). Primary target (the one targeted at fire time)
// takes full damage; other enemies caught in the corridor take secondary
// damage scaled by the pierce perk's secondaryDamageMultiplier.
// arcaneOrbPullRefreshSeconds is the per-tick pull duration the orb re-applies
// to hostiles within its radius. Long enough to persist across a couple of
// ticks (so a dragged enemy keeps moving between refreshes) but short enough
// that the pull lapses quickly once the orb moves past and the enemy leaves the
// radius — producing the "dragged only while near the orb" moving-vortex feel.
const arcaneOrbPullRefreshSeconds = 0.2

// arcaneOrbDefaultSpeed is the fallback travel speed (world px/sec) when the
// ability declares no projectileSpeed. Deliberately slow — the orb is a
// drifting vortex, not a bolt.
const arcaneOrbDefaultSpeed = 150.0

// arcaneOrbDamageIntervalSeconds is the DoT cadence: every interval the orb
// deals (DamagePerSecond * interval) to hostiles in radius. A fixed cadence
// keeps the per-second total exact (no per-tick rounding drift) and reads as a
// pulsing vortex. 0.25s ⇒ 4 damage ticks/sec.
const arcaneOrbDamageIntervalSeconds = 0.25

// spawnArcaneOrbLocked launches an Arcane Orb from caster in the direction of
// target (unit-targeted, but the orb then travels the ground independently). It
// flies a straight line of length `distance` at `speed`, dealing no impact
// damage; tickArcaneOrbProjectileLocked drives the moving-vortex pull.
// Degenerate caster-on-target geometry falls back to firing along +X.
// tickInterval is the on_projectile_tick damage cadence (seconds) stamped
// onto the spawned Projectile — callers pass arcaneOrbDamageIntervalSeconds
// (the legacy point-cast resolver, ability_cast.go) or the compiled
// launchProjectileConfig.TickInterval (executeTickingVortexShimLocked,
// ability_exec_projectile.go), which is baked from that same constant, so
// both callers set an identical value today.
//
// tickActions/opsBudget are the AUTHORED, launch-time-frozen on_projectile_tick
// actions and their shared op budget (see executeTickingVortexShimLocked /
// freezeVortexTickActions, ability_exec_projectile.go, and
// Projectile.TickActions' doc comment) — non-nil ONLY on the composable
// executor leg. The legacy point-cast resolver (ability_cast.go) passes
// nil, nil, leaving the spawned Projectile to fall back to the frozen
// ArcaneOrbRadius/PullStrength/DamagePerSecond hardcoded math in
// fireProjectileTickLocked, unchanged from before this fix. Caller holds s.mu.
func (s *GameState) spawnArcaneOrbLocked(caster *Unit, targetX, targetY float64, def AbilityDef, eff EffectiveSpell, distance, tickInterval float64, tickActions []AbilityActionDef, opsBudget *int) {
	dx := targetX - caster.X
	dy := targetY - caster.Y
	dist := math.Sqrt(dx*dx + dy*dy)
	dirX, dirY := 1.0, 0.0
	if dist > 0 {
		dirX, dirY = dx/dist, dy/dist
	}
	if distance <= 0 {
		distance = dist
	}
	speed := eff.ProjectileSpeed
	if speed <= 0 {
		speed = arcaneOrbDefaultSpeed
	}
	travelTime := distance / speed
	if travelTime < minProjectileFlightSeconds {
		travelTime = minProjectileFlightSeconds
	}
	id := fmt.Sprintf("proj_%d", s.nextProjectileID)
	s.nextProjectileID++
	s.Projectiles = append(s.Projectiles, &Projectile{
		ID:            id,
		OwnerUnitID:   caster.ID,
		OwnerPlayerID: caster.OwnerID,
		// No homing target — the orb flies to the fixed endpoint.
		TargetUnitID:     0,
		OriginX:          caster.X,
		OriginY:          caster.Y,
		TargetX:          caster.X + dirX*distance,
		TargetY:          caster.Y + dirY*distance,
		TotalSeconds:     travelTime,
		RemainingSeconds: travelTime,
		Variant:          def.Projectile, // client renders the orb sprite by id
		// Ability-owned render scale (the caster's ProjectileScale is for its
		// basic attack, not its spells). 0/absent ⇒ the client's default 1×.
		Scale: def.ProjectileScale,
		// The orb never impacts (landProjectileLocked is never called for it —
		// see tickArcaneOrbProjectileLocked's caller below); this is read back
		// by that function's applyAbilitySplashDamageLocked DoT-tick call for
		// on_unit_death attribution.
		SourceAbilityID:          def.ID,
		ArcaneOrb:                true,
		ArcaneOrbRadius:          eff.Radius,
		ArcaneOrbPullStrength:    eff.PullStrength,
		ArcaneOrbDamagePerSecond: eff.DamagePerSecond,
		ArcaneOrbDamageType:      def.DamageType.OrPhysical(),
		TickInterval:             tickInterval,
		TickActions:              tickActions,
		TickOpsBudget:            opsBudget,
		// Straight-line path fields (shared with pierce flight math).
		PierceLength: distance,
		PierceDirX:   dirX,
		PierceDirY:   dirY,
	})
}

// tickArcaneOrbProjectileLocked advances the orb along its straight path and,
// from its CURRENT position, fires its composed on_projectile_tick actions
// (select_targets -> apply_force every call, deal_damage on a fixed cadence —
// see fireProjectileTickLocked). Returns true while the orb is still
// travelling; false when it reaches the end of its path (caller drops it —
// no impact, ever, for a ticking vortex). Caller holds s.mu write lock.
func (s *GameState) tickArcaneOrbProjectileLocked(proj *Projectile, dt float64) bool {
	if proj.PierceLength <= 0 || proj.TotalSeconds <= 0 {
		return false
	}
	proj.RemainingSeconds = math.Max(0, proj.RemainingSeconds-dt)
	along := proj.PierceLength * (1.0 - proj.RemainingSeconds/proj.TotalSeconds)
	curX := proj.OriginX + proj.PierceDirX*along
	curY := proj.OriginY + proj.PierceDirY*along
	// A dead/gone owner means no pull/damage this tick (hostility is
	// owner-relative); the orb keeps drifting so its visual finishes cleanly.
	if owner := s.getUnitByIDLocked(proj.OwnerUnitID); owner != nil {
		s.fireProjectileTickLocked(proj, curX, curY, dt)
	}
	return proj.RemainingSeconds > 0
}

// fireProjectileTickLocked drives one simulation tick of a ticking
// ("direction" travelMode + TickInterval>0) projectile's on_projectile_tick
// behavior. Dispatches on whether this orb carries AUTHORED tick actions
// (Projectile.TickActions, populated only by the composable executor leg —
// see executeTickingVortexShimLocked, ability_exec_projectile.go):
//
//   - TickActions non-empty (composable executor leg): runs the AUTHORED
//     program via fireComposedProjectileTickLocked below — the genuine fix.
//     Before this, this function silently DISCARDED the authored
//     on_projectile_tick trigger and re-synthesized its own hardcoded
//     select_targets/apply_force/deal_damage from frozen scalar fields,
//     which is why an authored apply_force{mode:"push"} was unreachable: the
//     hardcoded copy never carried Mode at all.
//   - TickActions empty (the legacy, pre-migration SchemaVersion<2 cast leg,
//     and any hand-built Projectile a test constructs directly — e.g.
//     TestArcaneOrb_DamageOverTimeRate): falls back to the ORIGINAL hardcoded
//     synthesis, unchanged, so that leg's behavior (and every test exercising
//     it) is untouched by this fix.
//
// Caller holds s.mu.
func (s *GameState) fireProjectileTickLocked(proj *Projectile, curX, curY float64, dt float64) {
	if len(proj.TickActions) > 0 {
		s.fireComposedProjectileTickLocked(proj, curX, curY, dt)
		return
	}
	s.fireLegacyProjectileTickLocked(proj, curX, curY, dt)
}

// fireComposedProjectileTickLocked runs proj's AUTHORED on_projectile_tick
// actions (Projectile.TickActions) through the shared executor —
// select_targets -> apply_force EVERY call (every simulation tick dt, not
// gated by any interval, so the pull center never goes stale — see the
// DELIBERATE DIVERGENCE note below), and deal_damage throttled to its own
// Timing.TickInterval cadence (AbilityActionDef.Timing — arcane_orb's "dmg"
// action carries this; see compileProjectileTickTrigger, ability_compile.go).
//
// DELIBERATE DIVERGENCE FROM AbilityZone's on_zone_tick PRECEDENT: a zone
// fires its WHOLE trigger only once per TickInterval (tickAbilityZonesLocked,
// ability_zone.go). Doing the same here would make apply_force's pull center
// stale by up to (orb speed * TickInterval) world-px between refreshes —
// this orb travels at ~150px/s with a 0.25s TickInterval, so a once-per-
// TickInterval pull would aim up to ~37px behind the orb's actual live
// position, measurably changing the dragged unit's trajectory. The golden
// equivalence test (TestAbilityCompileGolden_ArcaneOrb) asserts the dragged
// unit's displacement matches to within 0.01px — so apply_force/
// select_targets run every dt here (parity-exact), and only deal_damage is
// gated, per-ACTION, to its own fixed cadence via proj.TickActionTimers.
//
// ROUNDING DECISION (the DOUBLE-FOLD hazard, worked through): deal_damage's
// registered Execute (ability_program_registry.go) folds its config's Amount
// through the caster's spell modifiers EXACTLY ONCE per call, whenever
// ctx.abilityDef is set — the same seam every other composable action's
// damage goes through (impact bolts, zone ticks). ctx.abilityDef IS set here
// (unlike before this fix, where it was deliberately left nil to avoid
// re-folding an already-frozen amount): the authored "dmg" action's Amount is
// the per-tick CHUNK baked at compile time from the RAW, unmodified
// DamagePerSecond (round(DamagePerSecond*TickInterval) — see
// compileProjectileTickTrigger), so folding it per-firing computes
// round(fold(round(rawDPS*interval))*mod) — NOT byte-identical in the general
// case to legacy's single frozen-then-rounded round(rawDPS*mod*interval),
// since fold-then-round and round-then-fold only commute under a PURE
// multiplicative modifier (no additive component). For arcane_orb's shipped
// numbers (DPS 16, interval 0.25 ⇒ 4/tick) under the golden test's +50%
// multiply modifier: legacy round(16*1.5*0.25)=round(6)=6; composed
// round(fold(4)*1)... — effectiveAbilityDamageLocked computes
// round(applySpellModField(mods,...,4)) = round(4*1.5) = round(6) = 6. Equal.
// This is a genuine, intentional trade: the alternative (freezing the
// per-tick amount at launch and never re-folding, mirroring how radius/
// pullStrength are handled just above) would guarantee byte-identical
// arithmetic for ANY modifier shape, but would mean editing the authored
// deal_damage amount changes the FROZEN base while an "amount" schema field
// generically means "the literal chunk this action deals," folded like every
// other deal_damage action in the codebase — special-casing this one action's
// amount as a pre-frozen constant would itself be the kind of inconsistent,
// hard-to-discover behavior this whole fix exists to remove. Chosen: genuine,
// ordinary per-firing fold (matches every other deal_damage call site);
// TestAbilityCompileGolden_ArcaneOrb (both sub-tests) stays green because the
// shipped numbers don't hit the divergent case — see
// TestArcaneOrb_ComposedDamageFoldMatchesLegacy_AtShippedMagnitudes for a
// dedicated proof of the exact arithmetic above, and a documented note on
// what WOULD diverge (an additive modifier, or a non-multiple-of-4 chunk)
// were arcane_orb's magnitudes ever to change.
//
// select_targets/apply_force still fold radius/pullStrength exactly once, at
// LAUNCH (frozen — see freezeVortexTickActions), since apply_force has no
// per-call fold seam of its own to reuse.
//
// Caller holds s.mu.
func (s *GameState) fireComposedProjectileTickLocked(proj *Projectile, curX, curY float64, dt float64) {
	def, ok := getAbilityDef(proj.SourceAbilityID)
	ctx := &RuntimeAbilityContext{
		CasterID:           proj.OwnerUnitID,
		AbilityID:          proj.SourceAbilityID,
		OwnerUnitID:        proj.OwnerUnitID,
		ProjectilePosition: protocol.Vec2{X: curX, Y: curY},
		EventPosition:      protocol.Vec2{X: curX, Y: curY},
		Named:              map[string]ContextValue{},
		Trace:              s.previewTrace,
		now:                s.previewClock,
		sharedOpsRemaining: proj.TickOpsBudget,
	}
	if ok {
		ctx.program = def.Program
		ctx.abilityDef = &def
	}
	if proj.TickActionTimers == nil {
		proj.TickActionTimers = map[string]float64{}
	}

	const path = "on_tick"
	for i := range proj.TickActions {
		if ctx.opsExhausted() {
			break
		}
		a := &proj.TickActions[i]
		if a.Timing == nil || a.Timing.TickInterval <= 0 {
			s.executeActionLocked(ctx, a, path)
			continue
		}
		// Throttled action (arcane_orb's "dmg"): loop in case a large dt spans
		// multiple due intervals, matching the pre-fix loop's per-interval
		// firing discipline. Each due firing runs against THIS SAME ctx, so it
		// reads the Named binding this call's (unthrottled, always-runs-first)
		// select_targets action already populated — never a stale set from an
		// earlier tick.
		proj.TickActionTimers[a.ID] += dt
		for proj.TickActionTimers[a.ID] >= a.Timing.TickInterval {
			proj.TickActionTimers[a.ID] -= a.Timing.TickInterval
			if ctx.opsExhausted() {
				break
			}
			s.executeActionLocked(ctx, a, path)
		}
	}
}

// fireLegacyProjectileTickLocked is the ORIGINAL (pre-genuine-composition-fix)
// hardcoded on_projectile_tick synthesis: select_targets(all_in_scene,
// origin: projectile_position, radius, relations:[enemy]) -> apply_force
// EVERY call, then -> deal_damage on a fixed cadence, built fresh each call
// from the projectile's own frozen ArcaneOrbRadius/PullStrength/
// DamagePerSecond/DamageType fields. Used ONLY when Projectile.TickActions is
// empty: the legacy (pre-migration, SchemaVersion<2) cast leg
// (resolveAbilityCastAtPointLocked -> spawnArcaneOrbLocked directly, never
// through the executor) and any hand-built Projectile a test constructs by
// hand (e.g. TestArcaneOrb_DamageOverTimeRate) — see fireProjectileTickLocked's
// dispatch doc comment. ctx.abilityDef is deliberately left nil here:
// ArcaneOrbDamagePerSecond/Radius/PullStrength were already folded ONCE, at
// launch (the legacy resolveAbilityCastAtPointLocked leg's own
// effectiveSpellLocked call), and are frozen for the whole flight — routing
// this firing's deal_damage through ctx.abilityDef would fold them a SECOND
// time via deal_damage's own automatic ctx.abilityDef-gated scaling
// (ability_program_registry.go), silently double-applying every modifier.
// This mirrors zone-tick's own "ctx.abilityDef==nil so burn stays raw" note
// on that same seam. Caller holds s.mu.
func (s *GameState) fireLegacyProjectileTickLocked(proj *Projectile, curX, curY float64, dt float64) {
	ctx := &RuntimeAbilityContext{
		CasterID:           proj.OwnerUnitID,
		AbilityID:          proj.SourceAbilityID,
		OwnerUnitID:        proj.OwnerUnitID,
		ProjectilePosition: protocol.Vec2{X: curX, Y: curY},
		EventPosition:      protocol.Vec2{X: curX, Y: curY},
		Named:              map[string]ContextValue{},
		Trace:              s.previewTrace,
		now:                s.previewClock,
	}

	actions := []AbilityActionDef{
		{
			ID:   "sel",
			Type: ActionSelectTargets,
			Target: &TargetQueryDef{
				Source:    SrcAllInScene,
				Origin:    OriginProjectilePos,
				Radius:    proj.ArcaneOrbRadius,
				Relations: []TargetRelation{RelEnemy},
			},
			Outputs: map[string]string{"targets": "vortexHits"},
		},
		{
			ID:     "force",
			Type:   ActionApplyForce,
			Input:  map[string]ContextRef{"targets": {Key: "vortexHits"}},
			Config: marshalConfig(applyForceConfig{Strength: proj.ArcaneOrbPullStrength, Duration: arcaneOrbPullRefreshSeconds, Origin: OriginProjectilePos}),
		},
	}

	// Damage-over-time on a fixed cadence so the per-second total is exactly
	// ArcaneOrbDamagePerSecond regardless of tick rate. Loop in case a large
	// dt spans multiple intervals; append one deal_damage action per due
	// interval so each fires as its own executor action (and its own trace
	// event), matching the pre-composable loop's per-interval damage calls.
	// interval falls back to the package constant for a hand-built Projectile
	// that never set TickInterval (e.g. a test constructing one directly).
	if proj.ArcaneOrbDamagePerSecond > 0 {
		interval := proj.TickInterval
		if interval <= 0 {
			interval = arcaneOrbDamageIntervalSeconds
		}
		proj.ArcaneOrbDamageTickTimer += dt
		for proj.ArcaneOrbDamageTickTimer >= interval {
			proj.ArcaneOrbDamageTickTimer -= interval
			dmg := int(math.Round(proj.ArcaneOrbDamagePerSecond * interval))
			if dmg > 0 {
				actions = append(actions, AbilityActionDef{
					ID:     "dmg",
					Type:   ActionDealDamage,
					Input:  map[string]ContextRef{"targets": {Key: "vortexHits"}},
					Config: marshalConfig(dealDamageConfig{Amount: dmg, Type: proj.ArcaneOrbDamageType}),
				})
			}
		}
	}

	s.runProgramTriggersLocked(ctx, []AbilityTriggerDef{{ID: "tick", Type: TriggerOnTick, Actions: actions}}, TriggerOnTick)
}

func (s *GameState) firePierceProjectileLocked(attacker, target *Unit, damage int) {
	def := perkDefByID("pierce")
	corridorWidth := 28.0
	maxSecondaries := 8
	secondaryMult := 0.5
	if def != nil {
		if v := def.Config["corridorWidth"]; v > 0 {
			corridorWidth = v
		}
		if v := int(def.Config["maxSecondaryTargets"]); v > 0 {
			maxSecondaries = v
		}
		if v := def.Config["secondaryDamageMultiplier"]; v > 0 {
			secondaryMult = v
		}
	}

	dx := target.X - attacker.X
	dy := target.Y - attacker.Y
	dist := math.Sqrt(dx*dx + dy*dy)
	if dist <= 0 {
		// Degenerate case (attacker on top of target) — fall back to homing.
		s.fireHomingProjectileLocked(attacker, target, damage, "")
		return
	}
	dirX, dirY := dx/dist, dy/dist
	// Length of the pierce path: full attack range so the shot doesn't stop
	// at the primary target. Eagle Spirit / Bullseye automatically extend it.
	length := attacker.AttackRange
	if length <= 0 {
		length = dist
	}
	endX := attacker.X + dirX*length
	endY := attacker.Y + dirY*length
	// Pierce shot is rendered as a fast green wind streak rather than a
	// physical arrow — fixed flight time of pierceFlightSeconds regardless
	// of distance. The implied speed (length / pierceFlightSeconds) is high
	// enough at long range that the visual reads as an instant zip across
	// the corridor, which matches the "blade of wind" feel.
	travelTime := pierceFlightSeconds
	if travelTime < minProjectileFlightSeconds {
		travelTime = minProjectileFlightSeconds
	}

	id := fmt.Sprintf("proj_%d", s.nextProjectileID)
	s.nextProjectileID++

	s.Projectiles = append(s.Projectiles, &Projectile{
		ID:            id,
		OwnerUnitID:   attacker.ID,
		OwnerPlayerID: attacker.OwnerID,
		TargetUnitID:  target.ID,
		OriginX:       attacker.X,
		OriginY:       attacker.Y,
		// TargetX/Y on a pierce projectile is the END of the line, not the
		// primary target's position. The client renders straight-line flight
		// from origin to endpoint with no homing.
		TargetX:          endX,
		TargetY:          endY,
		TotalSeconds:     travelTime,
		RemainingSeconds: travelTime,
		Damage:           damage,
		// Distinct variant string so the client renderer can dispatch a
		// custom green-wind visual instead of the default unit-type arrow.
		Variant:             "wind_pierce",
		Pierce:              true,
		PierceMaxHits:       maxSecondaries + 1, // +1 so the primary doesn't consume the secondary cap
		PierceSecondaryMult: secondaryMult,
		PierceCorridorWidth: corridorWidth,
		PierceLength:        length,
		PierceDirX:          dirX,
		PierceDirY:          dirY,
		// Carried for invariant consistency (every fired projectile records
		// its firer's scale). The pierce visual is procedural "wind_pierce",
		// not a sprite, so this has no effect today — it future-proofs a
		// sprite-backed pierce variant.
		Scale: attacker.ProjectileScale,
	})
}

// tickProjectilesLocked advances in-flight projectiles and lands the ones that
// hit zero this tick. Must run after tickUnitCombatLocked so shots fired this
// tick wait a full dt before decaying.
//
// Pierce arrows take a different path: instead of homing on a target and
// landing once, they fly in a fixed straight line and damage enemies along
// the way as the arrow passes through their position.
func (s *GameState) tickProjectilesLocked(dt float64) {
	if len(s.Projectiles) == 0 {
		return
	}

	var deadUnitIDs []int
	// Landing a projectile can spawn NEW projectiles by appending to
	// s.Projectiles — equipment on-hit proc bolts fire from
	// landProjectileLocked → resolveAttackHitLocked → rollEquipmentProcsLocked.
	// Snapshot this tick's set into `active` and reset s.Projectiles so those
	// mid-loop appends accumulate cleanly on their own; we merge them back with
	// this tick's survivors after the loop. Without this the closing
	// reassignment discarded any bolt spawned while landing another projectile,
	// which silently ate every ranged attacker's proc shot.
	active := s.Projectiles
	s.Projectiles = nil
	kept := active[:0]

	for _, proj := range active {
		// Pierce arrows traverse independently of any single target.
		if proj.Pierce {
			if survived := s.tickPierceProjectileLocked(proj, dt, &deadUnitIDs); survived {
				kept = append(kept, proj)
			}
			continue
		}
		// Arcane Orb: a slow straight-line vortex — no impact, pulls as it flies.
		if proj.ArcaneOrb {
			if survived := s.tickArcaneOrbProjectileLocked(proj, dt); survived {
				kept = append(kept, proj)
			}
			continue
		}
		// launch_projectile "direction" travelMode: flies a fixed straight
		// line and fires its composed on_projectile_impact on the first
		// hostile crossed, or at end of flight if none — see
		// launchDirectionalProjectileLocked's IMPACT SEMANTICS doc.
		if proj.DirectionalImpact {
			hit, cx, cy, over := s.tickDirectionalImpactProjectileLocked(proj, dt)
			if !over {
				kept = append(kept, proj)
				continue
			}
			hitID := 0
			if hit != nil {
				hitID = hit.ID
				s.playProjectileImpactLocked(proj, hit)
			} else if proj.ImpactEffect != "" {
				s.playEffectAtPointLocked(proj.ImpactEffect, cx, cy, 1.0)
			}
			s.fireProjectileImpactLocked(proj, hitID, cx, cy)
			continue
		}

		target := s.getUnitByIDLocked(proj.TargetUnitID)
		// Drop silently if the target is gone — no retarget, no wasted hit.
		if target == nil || target.HP <= 0 || !target.Visible {
			continue
		}

		proj.TargetX = target.X
		proj.TargetY = target.Y

		proj.RemainingSeconds -= dt
		if proj.RemainingSeconds > 0 {
			kept = append(kept, proj)
			continue
		}

		s.landProjectileLocked(proj, target, &deadUnitIDs)
	}
	// kept = survivors of this tick (compacted into active's array); s.Projectiles
	// = only the bolts spawned during landings above. Merge so both persist.
	s.Projectiles = append(kept, s.Projectiles...)

	for _, id := range deadUnitIDs {
		s.removeUnitLocked(id)
	}
}

// tickPierceProjectileLocked advances a pierce projectile by dt seconds and
// damages any not-yet-hit enemy whose position the arrow physically crossed
// during this tick. Returns true if the arrow is still in flight (caller
// keeps it in s.Projectiles), false when it has reached its endpoint or hit
// its max-hits cap (caller drops it).
//
// "Crossed during this tick" is defined as: the enemy's projection onto the
// flight line has an along-line distance t such that prevAlong < t <=
// currentAlong AND the perpendicular distance to the line is within the
// corridor half-width. This means an enemy walking into the path mid-flight
// can still get hit if the arrow happens to be passing through their
// position at that exact tick.
func (s *GameState) tickPierceProjectileLocked(proj *Projectile, dt float64, deadUnitIDs *[]int) bool {
	if proj.PierceLength <= 0 || proj.TotalSeconds <= 0 {
		return false
	}

	prevRemaining := proj.RemainingSeconds
	proj.RemainingSeconds = math.Max(0, proj.RemainingSeconds-dt)

	prevAlong := proj.PierceLength * (1.0 - prevRemaining/proj.TotalSeconds)
	currAlong := proj.PierceLength * (1.0 - proj.RemainingSeconds/proj.TotalSeconds)
	if currAlong <= prevAlong {
		// Sub-precision tick — nothing to do.
		return proj.RemainingSeconds > 0
	}

	attacker := s.getUnitByIDLocked(proj.OwnerUnitID)
	corridorHalf := proj.PierceCorridorWidth * 0.5

	// Primary target gets full damage; everyone else takes the secondary
	// multiplier. Pre-resolve the secondary damage cap so we don't recompute
	// in the inner loop.
	primaryID := proj.TargetUnitID

	hitOne := func(target *Unit, isPrimary bool) {
		if target == nil || target.HP <= 0 || !target.Visible {
			return
		}
		// De-dupe by ID so the same victim never takes two pierce hits from
		// one arrow.
		for _, id := range proj.PierceHits {
			if id == target.ID {
				return
			}
		}
		proj.PierceHits = append(proj.PierceHits, target.ID)

		// Evasion: each pierce victim rolls independently. Appended to
		// PierceHits above regardless, so an evaded victim is spent — the
		// arrow doesn't retry them next tick.
		if hitRoll, avoidedBy := s.attackHitsLocked(evasionForUnit(target)); !hitRoll {
			s.recordEvadeEventLocked(target, avoidedBy)
			return
		}

		damage := proj.Damage
		if !isPrimary {
			damage = maxInt(0, int(math.Round(float64(proj.Damage)*proj.PierceSecondaryMult)))
		}
		if damage <= 0 {
			return
		}
		// If the attacker is gone, apply raw damage so the arrow that's
		// already in flight still hits — same fall-back semantic as
		// landProjectileLocked. No crit roll here; the attacker isn't
		// available to read crit-bonus perks off of.
		if attacker == nil {
			// Attacker died mid-flight — apply via the canonical pipeline
			// directly. DamageType rides on the projectile (snapshot of the
			// attacker's AttackDamageType at fire time) so the popup colors
			// correctly even after the firing unit is gone.
			// Pierce is a marksman perk that reshapes the ARROW (a fixed
			// straight-line corridor instead of a homing single-target bolt),
			// not a bonus/secondary hit — every victim along the corridor,
			// including the primary, is this attack's own damage. Still
			// DamageCategoryBasicAttack, matching resolveAttackHitLocked's
			// treatment below (the normal, attacker-alive path for the same
			// arrow).
			s.applyUnitDamageWithSourceLocked(target, damage, DamageSource{AttackerUnitID: proj.OwnerUnitID, Kind: "pierce", Category: DamageCategoryBasicAttack, DamageType: proj.DamageType})
			if target.HP <= 0 {
				target.HP = 0
				*deadUnitIDs = append(*deadUnitIDs, target.ID)
			}
			return
		}
		// Per-victim crit roll — each enemy along the pierce line rolls
		// independently so a high-crit Marksman lands red-circle visuals on
		// individual victims rather than all-or-nothing across the corridor.
		// Hunter's Mark on the victim feeds in via the standard crit-chance
		// path (huntersMarkCritBonusLocked).
		isVictimCrit := false
		if rolled := s.rollCritDamage(attacker, target); rolled > 1.0 {
			damage = int(math.Round(float64(damage) * rolled))
			isVictimCrit = true
		}
		// Route through resolveAttackHitLocked so on-hit perks (Hunter's
		// Mark, explosive_tips) trigger on every pierce victim. The
		// recursion guards inside those perks prevent feedback loops.
		s.resolveAttackHitLocked(attacker, target, damage, deadUnitIDs)
		if isVictimCrit {
			s.recordCritHitLocked(target, damage)
		}
	}

	// Walk all hostiles, collect those that fall inside this tick's swept
	// segment, then apply hits in along-line order so a player watching the
	// arrow sees damage land in flight order even when two enemies overlap.
	type hitCandidate struct {
		unit *Unit
		t    float64
	}
	var candidates []hitCandidate
	for _, candidate := range s.Units {
		if candidate == nil || candidate.HP <= 0 || !candidate.Visible {
			continue
		}
		if candidate.ID == proj.OwnerUnitID {
			continue
		}
		if attacker != nil {
			if !s.playersAreHostileLocked(candidate.OwnerID, attacker.OwnerID) {
				continue
			}
		} else if s.playersAreFriendlyLocked(candidate.OwnerID, proj.OwnerPlayerID) {
			// Attacker died; skip allied (same-team) units to avoid friendly fire.
			continue
		}
		ox := candidate.X - proj.OriginX
		oy := candidate.Y - proj.OriginY
		t := ox*proj.PierceDirX + oy*proj.PierceDirY
		if t <= prevAlong || t > currAlong {
			continue
		}
		perp := ox*(-proj.PierceDirY) + oy*proj.PierceDirX
		if math.Abs(perp) > corridorHalf {
			continue
		}
		// Skip if already on the dedupe list.
		alreadyHit := false
		for _, id := range proj.PierceHits {
			if id == candidate.ID {
				alreadyHit = true
				break
			}
		}
		if alreadyHit {
			continue
		}
		candidates = append(candidates, hitCandidate{unit: candidate, t: t})
	}
	for i := 1; i < len(candidates); i++ {
		for j := i; j > 0 && candidates[j].t < candidates[j-1].t; j-- {
			candidates[j], candidates[j-1] = candidates[j-1], candidates[j]
		}
	}
	for _, c := range candidates {
		hitOne(c.unit, c.unit.ID == primaryID)
		if proj.PierceMaxHits > 0 && len(proj.PierceHits) >= proj.PierceMaxHits {
			return false
		}
	}

	// Drop the arrow once it has finished its flight or exhausted its hits.
	if proj.RemainingSeconds <= 0 {
		return false
	}
	if proj.PierceMaxHits > 0 && len(proj.PierceHits) >= proj.PierceMaxHits {
		return false
	}
	return true
}

func (s *GameState) landProjectileLocked(proj *Projectile, target *Unit, deadUnitIDs *[]int) {
	// The projectile has reached its target — play its impact effect on the
	// unit it hit (e.g. fire_bolt → "fizzle"). Done up-front so it fires
	// regardless of whether the attacker survived the flight. No-op when the
	// projectile carries no impact effect.
	s.playProjectileImpactLocked(proj, target)

	// Composable launch_projectile impact (arcane_bolt/fireball's migrated
	// shape): skip the baked-damage path entirely — this bolt's Damage/
	// AbilitySplashRadius are unused (0); its ENTIRE impact behavior is
	// ImpactActions, run through the shared executor. See
	// Projectile.ImpactActions' doc comment and fireProjectileImpactLocked.
	if proj.ImpactActions != nil {
		s.fireProjectileImpactLocked(proj, target.ID, target.X, target.Y)
		return
	}

	if proj.SkipOnHitEffects {
		// Equipment-proc / ability bolt: apply its typed damage directly.
		// Bypasses the on-hit hub so it cannot trigger another proc or
		// elemental instance. Kind defaults to "item-proc" (the original proc
		// bolt); ability bolts override it via SourceKind (e.g. "ability").
		kind := proj.SourceKind
		if kind == "" {
			kind = "item-proc"
		}
		// Category mirrors kind's own SourceKind branch: an ability bolt
		// (fireAbilityProjectileLocked, SourceKind "ability") is
		// DamageCategoryAbility; a plain equipment/item proc bolt
		// (SourceKind == "") is DamageCategoryItem. SourceKind only ever
		// takes these two values (see its doc comment above).
		category := DamageCategoryItem
		if proj.SourceKind == "ability" {
			category = DamageCategoryAbility
		}
		landed := s.applyUnitDamageWithSourceLocked(target, proj.Damage, DamageSource{
			AttackerUnitID: proj.OwnerUnitID,
			Kind:           kind,
			Category:       category,
			DamageType:     proj.DamageType,
			// Minor bolts (Arcane Missiles) suppress the main color hint and
			// render as a small side-falling popup instead of the big number.
			SuppressTypeHint: proj.MinorDamage,
			// "" for a plain item-proc bolt (proj.SourceKind != "ability");
			// carries the launching ability's id for fireball/arcane_bolt/
			// arcane_missiles bolts (stamped at spawn — fireAbilityProjectileLocked,
			// SourceKind "ability"). See DamageSource.SourceAbilityID's doc.
			SourceAbilityID: proj.SourceAbilityID,
		})
		if proj.MinorDamage {
			s.recordMinorDamageHitLocked(target, landed, damageTypeColorVariant(proj.DamageType))
		}
		// Ability AoE (fireball): deal the same damage to every other hostile
		// within the effective radius of the impact point. Reuses the shared
		// authoritative damage entry point (no parallel death path); inert
		// when AbilitySplashRadius == 0 (single-target bolts, e.g. arcane_bolt).
		if proj.AbilitySplashRadius > 0 {
			s.applyAbilitySplashDamageLocked(proj.OwnerUnitID, proj.OwnerPlayerID, target.X, target.Y, proj.AbilitySplashRadius, proj.Damage, proj.DamageType, target.ID, proj.SourceAbilityID)
		}
		// On-hit slow: routed to the cold (chill) or physical track by the
		// bolt's damage type. No-op when the proc carries no slow (zero fields).
		s.applyProcSlowLocked(target.ID, proj.SlowMultiplier, proj.SlowDurationSeconds, proj.DamageType)
		// On-hit burn: ignite the target with a fire DoT. No-op when the proc
		// carries no burn. Credited to the firing unit (proj.OwnerUnitID).
		s.applyProcBurnLocked(target.ID, proj.BurnDamagePerSecond, proj.BurnDurationSeconds, proj.OwnerUnitID)
		// Arch Mage Gold perks trigger on an Arcane Missile hit. Gated to
		// charge-fire passive bolts (Arcane Missiles) so ordinary ability bolts
		// (fireball, arcane_bolt) don't fire the hooks; the per-perk reactions
		// (mana feedback, item on-hit procs, Unstable Magic) each self-gate on
		// the caster owning the perk. Fires whether or not this hit is lethal —
		// a killing missile is still a hit.
		if proj.SourceKind == "ability" && proj.SourceAbilityID != "" {
			if adef, ok := getAbilityDef(proj.SourceAbilityID); ok && adef.IsChargeFirePassive() {
				if caster := s.getUnitByIDLocked(proj.OwnerUnitID); caster != nil {
					s.onArcaneMissileHitLocked(caster, target)
				}
			}
		}
		if target.HP <= 0 {
			target.HP = 0
			// Ability bolts defer removal + kill-XP to the attributed pending-death
			// drain that applyUnitDamageWithSourceLocked already enqueued (matching
			// the instant-cast path) — the raw removeUnitLocked below would strip
			// the unit before the drain can award XP to the caster. Proc bolts keep
			// their legacy immediate removal.
			if proj.SourceKind != "ability" {
				*deadUnitIDs = append(*deadUnitIDs, target.ID)
			}
		}
		return
	}

	// Evasion: a basic-attack projectile can be dodged/blocked at LANDING —
	// full whiff. Proc bolts took the SkipOnHitEffects return above and are
	// never evaded (effects always land).
	if hit, avoidedBy := s.attackHitsLocked(evasionForUnit(target)); !hit {
		s.recordEvadeEventLocked(target, avoidedBy)
		return
	}

	attacker := s.getUnitByIDLocked(proj.OwnerUnitID)
	if attacker == nil {
		// Attacker died between fire and land — damage still lands (the arrow
		// was already in flight), but attacker-side perks are skipped.
		// Use the owner unit ID from the projectile for attribution so the drain
		// can attempt XP bookkeeping (it will no-op if the attacker is gone).
		// DamageType carried on the projectile (snapshot of attacker's
		// AttackDamageType at fire time) so the popup colors correctly
		// whether or not the firing unit is still alive when it lands.
		// Reached only when proj.SkipOnHitEffects is false — every ability/
		// item-proc bolt sets that flag and returns above, so this path is
		// exclusively a normal ranged basic-attack arrow whose firer died
		// mid-flight.
		s.applyUnitDamageWithSourceLocked(target, proj.Damage, DamageSource{AttackerUnitID: proj.OwnerUnitID, Kind: "projectile", Category: DamageCategoryBasicAttack, DamageType: proj.DamageType})
		if target.HP <= 0 {
			target.HP = 0
			*deadUnitIDs = append(*deadUnitIDs, target.ID)
		}
		return
	}
	s.resolveAttackHitLocked(attacker, target, proj.Damage, deadUnitIDs)
	// Queue the crit visual after the hit lands. The damage parameter passed
	// here is the post-armor amount the projectile carried; mark amplification
	// can grow the actual HP-drop on the client beyond this value, which
	// causes a small mismatch in (UnitID, Damage) matching. The client falls
	// back to "any crit on this target this tick" matching when amounts
	// differ, so the visual still lands on the right number.
	if proj.IsCrit && target != nil {
		s.recordCritHitLocked(target, proj.Damage)
	}
}

// cullProjectilesLocked drops any in-flight projectiles matching shouldDrop.
// Used by player/unit removal paths to clear stale references.
func (s *GameState) cullProjectilesLocked(shouldDrop func(*Projectile) bool) {
	if len(s.Projectiles) == 0 {
		return
	}
	kept := s.Projectiles[:0]
	for _, proj := range s.Projectiles {
		if shouldDrop(proj) {
			continue
		}
		kept = append(kept, proj)
	}
	s.Projectiles = kept
}
