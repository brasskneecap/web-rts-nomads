package game

import "strconv"

// ═════════════════════════════════════════════════════════════════════════════
// GROUND HAZARD SYSTEM
//
// A GroundHazard is a placeable, server-only zone with a two-phase life:
//   1. FALL / DELAY: for ImpactDelayRemaining seconds the hazard does nothing
//      (the projectile is visually "falling"; the client shows this via the
//      meteor effect's early frames).
//   2. IMPACT: when the delay elapses, a one-time AoE hit lands at the center.
//   3. BURN: for BurnRemaining seconds after impact, a periodic AoE (every
//      BurnTickInterval) damages hostile units currently within BurnRadius.
//      Re-evaluated each tick — a true ground zone, not a stick-on debuff.
//
// EXTENSION POINT: this is the generic delayed-AoE + lingering-DoT primitive.
// Future "sky-drop"/hazard spells reuse it via the AbilityDef impactDelay/burn
// fields + spawnGroundHazardLocked — no new per-spell code. Kind is carried for
// future per-variant branching/telemetry but is not switched on today.
//
// Server-only by design: never serialized. Its only player-visible output is
// (a) the client meteor EFFECT (queued separately at cast time) and (b) damage,
// which rides the authoritative pipeline and already reaches the client as HP
// deltas + damage popups. MP joiners run their sim on the host.
//
// Damage is delivered via applyAbilitySplashDamageLocked, which owns mitigation,
// threat, and the attributed death/XP drain — so this file never touches the
// death queue (drainPendingDeathsLocked, later in Update, cleans up kills).
// ═════════════════════════════════════════════════════════════════════════════

// GroundHazard is a delayed-impact + lingering-burn ground zone. All fields are
// snapshotted at spawn time so live catalog tuning cannot retroactively change
// active hazards (same discipline as Trap).
type GroundHazard struct {
	ID            string
	Kind          string // ability id that spawned it ("meteor"); reserved for future branching
	OwnerUnitID   int
	OwnerPlayerID string
	X, Y          float64

	// Impact (fall) phase.
	ImpactDelayRemaining float64 // counts down; at <= 0 the one-time impact fires
	Impacted             bool
	ImpactRadius         float64
	ImpactDamage         int
	DamageType           DamageType

	// Burn (lingering) phase. Active while BurnRemaining > 0 after impact.
	BurnRemaining     float64
	BurnRadius        float64
	BurnDamagePerTick int
	BurnTickInterval  float64
	burnTickTimer     float64 // counts down to the next burn tick (runtime-only)

	// Lingering-burn VFX (visual only). BurnEffectID names a looping ground
	// effect queued once the fall/impact animation finishes (see
	// BurnVFXDelayRemaining), lasting the rest of the burn window so it "sits" on
	// the ground while the burn ticks. EffectScale matches the ability's
	// effectScale so the crater lines up with the meteor's own impact frames.
	// Empty BurnEffectID ⇒ no lingering VFX (the burn still deals damage).
	BurnEffectID string
	EffectScale  float64
	// BurnVFXDelayRemaining delays the lingering crater VFX until the primary
	// fall/impact effect (EffectAtPoint) has finished playing, so the crater does
	// not visibly overlap the meteor's own impact frames. Auto-derived at spawn
	// as (fall effect duration − impact delay); counts down only after impact.
	// burnVFXQueued guards the one-shot spawn (runtime-only).
	BurnVFXDelayRemaining float64
	burnVFXQueued         bool
}

func groundHazardIDString(id int) string {
	return "hazard-" + strconv.Itoa(id)
}

// spawnGroundHazardLocked constructs a GroundHazard from an ability def + its
// effective values and appends it to s.GroundHazards. Impact damage/radius come
// from the effective spell (so a damage modifier is honored); burn knobs and the
// fall delay come from the raw def (not modifier-eligible today).
//
// Caller holds s.mu.
func (s *GameState) spawnGroundHazardLocked(caster *Unit, def AbilityDef, eff EffectiveSpell, x, y float64) {
	if caster == nil {
		return
	}
	id := s.nextGroundHazardID
	s.nextGroundHazardID++
	// Delay the lingering-burn VFX until the fall/impact animation (EffectAtPoint)
	// finishes, so the crater doesn't double up with the meteor's own impact
	// frames. Auto-derived from that effect's own duration minus the fall delay —
	// no separate tuning knob to keep in sync. 0 when there's no fall effect or
	// it ends by impact (crater then appears immediately at impact).
	burnVFXDelay := 0.0
	if def.BurnEffectAtPoint != "" && def.EffectAtPoint != "" {
		if fx, ok := getEffectDef(def.EffectAtPoint); ok {
			if burnVFXDelay = fx.Duration - def.ImpactDelaySeconds; burnVFXDelay < 0 {
				burnVFXDelay = 0
			}
		}
	}
	s.GroundHazards = append(s.GroundHazards, &GroundHazard{
		ID:                    groundHazardIDString(id),
		Kind:                  def.ID,
		OwnerUnitID:           caster.ID,
		OwnerPlayerID:         caster.OwnerID,
		X:                     x,
		Y:                     y,
		ImpactDelayRemaining:  def.ImpactDelaySeconds,
		ImpactRadius:          eff.Radius,
		ImpactDamage:          eff.Damage,
		DamageType:            def.DamageType.OrPhysical(),
		BurnRemaining:         def.BurnDurationSeconds,
		BurnRadius:            def.BurnRadius,
		BurnDamagePerTick:     def.BurnDamagePerTick,
		BurnTickInterval:      def.BurnTickIntervalSeconds,
		BurnEffectID:          def.BurnEffectAtPoint,
		EffectScale:           def.EffectScale,
		BurnVFXDelayRemaining: burnVFXDelay,
	})
}

// tickGroundHazardsLocked advances every hazard by dt: counts down the fall
// delay, fires the one-time impact, then ticks the burn. Culls hazards whose
// burn has ended or whose owning player has left the match. Filter-into-front-
// of-slice (like tickTrapsLocked) to avoid steady-state allocation.
//
// Must run under s.mu, AFTER combat/trap ticks and BEFORE drainPendingDeaths.
func (s *GameState) tickGroundHazardsLocked(dt float64) {
	if len(s.GroundHazards) == 0 {
		return
	}
	kept := s.GroundHazards[:0]
	for _, h := range s.GroundHazards {
		// Drop if the owning player has left the match (mirrors tickTrapsLocked).
		if _, ok := s.Players[h.OwnerPlayerID]; !ok {
			continue
		}

		if !h.Impacted {
			h.ImpactDelayRemaining -= dt
			if h.ImpactDelayRemaining > 0 {
				kept = append(kept, h)
				continue // still falling
			}
			s.applyHazardImpactLocked(h)
			h.Impacted = true
			h.burnTickTimer = 0 // fire the first burn tick promptly below
		}

		// Burn phase.
		if h.BurnRemaining > 0 {
			// Queue the lingering crater VFX ONCE, after the fall/impact animation
			// has finished (BurnVFXDelayRemaining), so it doesn't double up with
			// the meteor's own crater frames. Duration = the burn time still
			// remaining when it appears, so it fades out with the burn. The effect
			// loops + renders below units (client manifest: loop + impactFrame:1),
			// so it reads as a crater smoldering on the ground. No-op when no
			// effect id is configured.
			if h.BurnEffectID != "" && !h.burnVFXQueued {
				h.BurnVFXDelayRemaining -= dt
				if h.BurnVFXDelayRemaining <= 0 {
					s.playEffectAtPointForDurationLocked(h.BurnEffectID, h.X, h.Y, h.EffectScale, h.BurnRemaining)
					h.burnVFXQueued = true
				}
			}
			h.burnTickTimer -= dt
			if h.burnTickTimer <= 0 {
				h.burnTickTimer += h.BurnTickInterval
				s.applyHazardBurnTickLocked(h)
			}
			h.BurnRemaining -= dt
			if h.BurnRemaining > 0 {
				kept = append(kept, h)
			}
		}
		// else: no burn configured (pure delayed AoE) — drop after impact.
	}
	s.GroundHazards = kept
}

// applyHazardImpactLocked deals the one-time impact AoE. Caller holds s.mu.
func (s *GameState) applyHazardImpactLocked(h *GroundHazard) {
	if h.ImpactDamage <= 0 || h.ImpactRadius <= 0 {
		return
	}
	s.applyAbilitySplashDamageLocked(h.OwnerUnitID, h.OwnerPlayerID, h.X, h.Y, h.ImpactRadius, h.ImpactDamage, h.DamageType, 0)
}

// applyHazardBurnTickLocked deals one burn tick to all hostile units currently
// within BurnRadius (re-evaluated each tick). Caller holds s.mu.
func (s *GameState) applyHazardBurnTickLocked(h *GroundHazard) {
	if h.BurnDamagePerTick <= 0 || h.BurnRadius <= 0 {
		return
	}
	s.applyAbilitySplashDamageLocked(h.OwnerUnitID, h.OwnerPlayerID, h.X, h.Y, h.BurnRadius, h.BurnDamagePerTick, h.DamageType, 0)
}
