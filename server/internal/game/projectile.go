package game

import (
	"fmt"
	"math"
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
	Pierce              bool
	// PierceMaxHits caps how many distinct enemies the arrow can damage
	// (primary + secondaries) before despawning. Prevents runaway DPS through
	// a packed line of enemies.
	PierceMaxHits       int
	// PierceSecondaryMult scales damage on enemies other than the original
	// targeted unit. The original target takes full Damage.
	PierceSecondaryMult float64
	// PierceCorridorWidth is the perpendicular-distance window (in world px)
	// that counts as "in the line of fire" for hit detection.
	PierceCorridorWidth float64
	// PierceLength is the total length of the line in world px.
	PierceLength        float64
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
	// base-stat splash, which also bypasses resolveAttackHitLocked).
	SkipOnHitEffects bool
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

// fireOnHitProcProjectileLocked spawns a homing projectile for an equipment
// on-hit proc. Unlike fireHomingProjectileLocked it does not derive its damage
// type from the attacker — it carries the proc's own Damage/DamageType — and it
// sets SkipOnHitEffects so landing applies damage directly without re-entering
// the on-hit hub. Must be called under s.mu.
func (s *GameState) fireOnHitProcProjectileLocked(attacker, target *Unit, proc EquipmentProc) {
	speed := defaultProjectileSpeed
	var followEffect, impactEffect string
	if def, ok := getProjectileDef(proc.ProjectileID); ok {
		speed = def.Speed
		followEffect = followEffectForProjectileDef(def)
		impactEffect = impactEffectForProjectileDef(def)
	}

	dx := target.X - attacker.X
	dy := target.Y - attacker.Y
	travelTime := math.Sqrt(dx*dx+dy*dy) / speed
	if travelTime < minProjectileFlightSeconds {
		travelTime = minProjectileFlightSeconds
	}

	id := fmt.Sprintf("proj_%d", s.nextProjectileID)
	s.nextProjectileID++

	variant := proc.ProjectileID
	if variant == "" {
		variant = attacker.UnitType
	}
	// Proc-authored scale wins when set; otherwise inherit the firing unit's
	// scale (the prior behavior). Both are "0 ⇒ client default 1×".
	scale := attacker.ProjectileScale
	if proc.ProjectileScale > 0 {
		scale = proc.ProjectileScale
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
		Damage:           proc.Damage,
		Variant:          variant,
		FollowEffect:     followEffect,
		ImpactEffect:     impactEffect,
		DamageType:       proc.DamageType,
		Scale:            scale,
		SkipOnHitEffects: true,
	})
}

// fireOnHitProcBeamLocked handles an equipment on-hit proc whose emitter def is
// EmitterKindBeam (e.g. the lightning_sword's "lightning_bolt"). It spawns the
// momentary beam flash NOW (frozen endpoints let it render even if the target
// later dies) but DEFERS the proc's damage by beamProcDamageDelaySeconds: a beam
// is otherwise instantaneous, so applying damage this tick would merge its
// number into the triggering hit's number. tickBeamsLocked lands the damage a
// beat later — bypassing the on-hit hub, so a proc can't trigger another proc —
// which pops it as its own floating number, the same separation the projectile
// version got by traveling.
//
// Caller holds s.mu write lock.
func (s *GameState) fireOnHitProcBeamLocked(attacker, target *Unit, proc EquipmentProc, def ProjectileDef) {
	variant := proc.ProjectileID
	if variant == "" {
		variant = def.ID
	}
	impact := impactEffectForProjectileDef(def)

	// Primary hit: attacker → target. Damage is deferred (see the helper) so it
	// pops as its own number instead of merging into the triggering attack.
	s.spawnMomentaryDamageBeamLocked(attacker, attacker, target, variant, proc.Damage, proc.DamageType, impact, def.DurationMs, beamProcDamageDelaySeconds)

	// Optional chain: the bolt arcs to up to BounceCount further enemies. Each
	// hop leaps off the PREVIOUS victim to the nearest not-yet-hit hostile
	// within BounceRange, losing BounceDamageFalloff damage per hop (25 → 20 →
	// 15 with count=2, falloff=5). Kill credit always stays with `attacker`.
	// Reuses the generic bounce picker shared with chain_siphon.
	if proc.BounceCount <= 0 || proc.BounceRange <= 0 {
		return
	}
	rangeSq := proc.BounceRange * proc.BounceRange
	// Exclude the primary target and the attacker from every hop so the chain
	// can't oscillate back onto an already-hit unit or the wielder.
	excluded := make(map[int]struct{}, proc.BounceCount+2)
	excluded[target.ID] = struct{}{}
	excluded[attacker.ID] = struct{}{}
	cursor := target
	for hop := 1; hop <= proc.BounceCount; hop++ {
		next := s.nearestChainBounceTargetLocked(attacker, cursor, rangeSq, excluded)
		if next == nil {
			break // chain fizzles: nothing eligible within range of the last victim
		}
		dmg := proc.Damage - proc.BounceDamageFalloff*hop
		if dmg <= 0 {
			break // fully attenuated — stop arcing
		}
		// Beam leaves the previous victim (cursor) but the hit still credits
		// the original attacker.
		s.spawnMomentaryDamageBeamLocked(attacker, cursor, next, variant, dmg, proc.DamageType, impact, def.DurationMs, beamProcDamageDelaySeconds)
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
		ID:                  id,
		OwnerUnitID:         attacker.ID,
		OwnerPlayerID:       attacker.OwnerID,
		TargetUnitID:        target.ID,
		OriginX:             attacker.X,
		OriginY:             attacker.Y,
		// TargetX/Y on a pierce projectile is the END of the line, not the
		// primary target's position. The client renders straight-line flight
		// from origin to endpoint with no homing.
		TargetX:             endX,
		TargetY:             endY,
		TotalSeconds:        travelTime,
		RemainingSeconds:    travelTime,
		Damage:              damage,
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
			s.applyUnitDamageWithSourceLocked(target, damage, DamageSource{AttackerUnitID: proj.OwnerUnitID, Kind: "pierce", DamageType: proj.DamageType})
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

	if proj.SkipOnHitEffects {
		// Equipment-proc bolt: apply its typed damage directly. Bypasses the
		// on-hit hub so it cannot trigger another proc or elemental instance.
		s.applyUnitDamageWithSourceLocked(target, proj.Damage, DamageSource{
			AttackerUnitID: proj.OwnerUnitID,
			Kind:           "item-proc",
			DamageType:     proj.DamageType,
		})
		if target.HP <= 0 {
			target.HP = 0
			*deadUnitIDs = append(*deadUnitIDs, target.ID)
		}
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
		s.applyUnitDamageWithSourceLocked(target, proj.Damage, DamageSource{AttackerUnitID: proj.OwnerUnitID, Kind: "projectile", DamageType: proj.DamageType})
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
