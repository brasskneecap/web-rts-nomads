package game

// Unit animation slots.
//
// Animations are not a server-side enum: the server emits a freeform
// unit.Status string and the client (unitAnimation.ts pickAnimation) maps it
// to an animation slot, rendering what the server sends. The status values
// below are the ones that map to a *dedicated* animation slot; they are named
// here so the new "Casting" slot and its precedence guard reference one
// symbol instead of a scattered literal. (Existing combat/worker code still
// writes the literals directly — those sites are intentionally left as-is to
// avoid a churny rename of working code.)
//
// Slot inventory at the time this was added:
//   - "Idle"      → idle      (exists)
//   - "Moving"    → walking   (exists; many move-ish statuses map to walking)
//   - "Attacking" → attacking (exists; basic attack — set in state_combat.go)
//   - "Casting"   → casting   (ADDED by this part — spell ability cast)
//   - Death: there is NO death animation slot. Units are removed on death
//     (drainPendingDeathsLocked) and neither the server status set nor the
//     client UnitAnimationName union has a death entry. Out of scope here —
//     the spec only mandated adding "Casting".
const (
	unitStatusIdle      = "Idle"
	unitStatusAttacking = "Attacking"
	unitStatusCasting   = "Casting"
)

// beginUnitCastingLocked puts a unit into the casting animation state. It is
// the primitive the ability system's cast-initiation calls; the timed cast
// lifecycle (cast time countdown, mana deduction, interrupt-on-damage) is
// built on top of it in the ability/heal part.
//
// Casting "interrupts" whatever the unit was doing for animation purposes:
// the attack swing/windup is cleared and in-progress movement is stopped so
// the unit doesn't visually walk or swing while the cast plays. AttackTargetID
// is intentionally preserved — a player's attack order resumes after the cast
// ends; the combat tick guard merely suppresses attacking *during* the cast.
//
// Caller holds s.mu.
func (s *GameState) beginUnitCastingLocked(unit *Unit) {
	if unit == nil {
		return
	}
	unit.Casting = true
	unit.Status = unitStatusCasting
	// Clear conflicting attack state so "Casting" and "Attacking" never both
	// describe the same tick.
	unit.Attacking = false
	unit.AttackWindupRemaining = 0
	unit.ActionFacingDX = 0
	unit.ActionFacingDY = 0
	// Stop in-progress movement so the cast doesn't play over a walk cycle.
	unit.Moving = false
	unit.Path = nil
}

// endUnitCastingLocked clears the casting animation state. Status is dropped
// to Idle; the next combat / AI tick re-derives the unit's real status
// (resuming an attack on a preserved AttackTargetID, etc.).
//
// Caller holds s.mu.
func (s *GameState) endUnitCastingLocked(unit *Unit) {
	if unit == nil {
		return
	}
	unit.Casting = false
	if unit.Status == unitStatusCasting {
		unit.Status = unitStatusIdle
	}
}
