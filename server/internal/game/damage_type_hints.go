package game

import "webrts/server/pkg/protocol"

// ═════════════════════════════════════════════════════════════════════════════
// DAMAGE TYPE HINTS — per-tick transient list of color hints for the regular
// (floating-up) damage popups
//
// Sibling to minor_damage_events.go. Where minor events drive a SEPARATE
// side-falling smaller popup (splash, DoT, echo damage), damage type hints
// just COLOR the existing major popup the client derives from HP-diff.
//
// Pipeline:
//   1. applyUnitDamageWithSourceLocked auto-emits a hint at the HP-loss point
//      whenever the DamageSource carries a typed damage type recognised by
//      damageTypeColorVariant (shadow / fire / holy / lightning / cold / arcane today).
//   2. The hint travels to the client on the snapshot.
//   3. Client matches (UnitID, Damage) and tags the corresponding HP-diff
//      popup with the variant. Renderer paints the major popup the variant's
//      color instead of the default white/red.
//
// Hints are visual-only — gameplay logic never reads them. They are SAFE to
// silently drop / fail to match: the popup just falls back to the default
// color, no mechanical effect.
//
// EXTENSION POINT: to color a new damage type, add a case to
// damageTypeColorVariant; the rest of the pipe picks it up automatically as
// long as the call site passes a DamageSource with that type.
// ═════════════════════════════════════════════════════════════════════════════

// damageTypeHint is one server-side record of "this HP loss this tick has
// damage type X". Variant maps to a renderer color on the client (e.g.
// "shadow" → dark purple). Drained at end-of-tick by
// resetDamageTypeHintsThisTickLocked.
type damageTypeHint struct {
	UnitID  int
	Damage  int
	Variant string
}

// damageTypeColorVariant returns the renderer variant string for a damage
// type, or "" when the type has no dedicated color (in which case the popup
// keeps its default white/red). Centralised so adding a new colored damage
// school touches ONE place — every site that calls
// applyUnitDamageWithSourceLocked with that type automatically benefits.
//
// Variants must match the client's per-variant color switch in
// CanvasRenderer.ts (drawFloatingDamageNumbers, the "kind === minor" case
// shares the same palette).
func damageTypeColorVariant(dt DamageType) string {
	switch dt {
	case DamageShadow:
		return "shadow"
	case DamageFire:
		return "fire"
	case DamageHoly:
		return "holy"
	case DamageLightning:
		return "electric"
	case DamageCold:
		return "cold"
	case DamageArcane:
		return "arcane"
	// ── add cases here as new damage schools get a dedicated color ──
	}
	return ""
}

// recordDamageTypeHintLocked queues a color hint for the floating-up popup
// the client will derive from the unit's HP-diff this tick. No-op for
// damage <= 0 or empty variant (callers don't need to gate themselves).
//
// Called automatically from applyUnitDamageWithSourceLocked at the HP-loss
// point — perks / abilities should NOT call it manually; just pass the
// right DamageType in DamageSource and the hint emits itself.
//
// Caller holds s.mu write lock.
func (s *GameState) recordDamageTypeHintLocked(target *Unit, damage int, variant string) {
	if target == nil || damage <= 0 || variant == "" {
		return
	}
	s.damageTypeHintsThisTick = append(s.damageTypeHintsThisTick, damageTypeHint{
		UnitID:  target.ID,
		Damage:  damage,
		Variant: variant,
	})
}

// snapshotDamageTypeHintsLocked converts the per-tick queue into wire format.
// Returns nil when empty so the JSON field omits cleanly.
//
// Caller holds s.mu (read or write).
func (s *GameState) snapshotDamageTypeHintsLocked() []protocol.DamageTypeHintSnapshot {
	if len(s.damageTypeHintsThisTick) == 0 {
		return nil
	}
	out := make([]protocol.DamageTypeHintSnapshot, 0, len(s.damageTypeHintsThisTick))
	for _, e := range s.damageTypeHintsThisTick {
		out = append(out, protocol.DamageTypeHintSnapshot{
			UnitID:  e.UnitID,
			Damage:  e.Damage,
			Variant: e.Variant,
		})
	}
	return out
}

// resetDamageTypeHintsThisTickLocked drops the per-tick queue. Called from
// the same place minor events get reset, so both transient channels lifecycle
// in lockstep.
func (s *GameState) resetDamageTypeHintsThisTickLocked() {
	s.damageTypeHintsThisTick = s.damageTypeHintsThisTick[:0]
}
