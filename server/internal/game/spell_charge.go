package game

import "sort"

// spell_charge.go implements the Arcane Charge → Arcane Missiles passive loop
// (arch-mage-spell-system). The Arch Mage is rewarded for actively spending
// mana: every mana point spent generates Arcane Charge, and once enough charge
// accrues the unit automatically fires a volley of Arcane Missiles at random
// in-range enemies, then resets its charge.
//
// The passive is entirely data-driven by a charge-fire AbilityDef (type
// "passive", ChargeRequired > 0) the unit knows — see arcane_missiles.json.
// Determinism: charge accrual is pure arithmetic; missile targeting draws from
// the seeded rngCombat stream over a sorted candidate list.

// unitChargeFirePassiveLocked returns the charge-fire passive AbilityDef the
// unit knows (the first one, in ability order), if any. Caller holds s.mu.
func (s *GameState) unitChargeFirePassiveLocked(unit *Unit) (AbilityDef, bool) {
	if unit == nil {
		return AbilityDef{}, false
	}
	for _, id := range unit.Abilities {
		if def, ok := getAbilityDef(id); ok && def.IsChargeFirePassive() {
			return def, true
		}
	}
	return AbilityDef{}, false
}

// accrueArcaneChargeLocked adds charge for `manaSpent` mana points, but only for
// a unit that owns a charge-fire passive. Called from spendUnitManaLocked so
// EVERY mana spend (spell casts, channel ticks) feeds the loop. Caller holds
// s.mu.
func (s *GameState) accrueArcaneChargeLocked(unit *Unit, manaSpent int) {
	if manaSpent <= 0 {
		return
	}
	def, ok := s.unitChargeFirePassiveLocked(unit)
	if !ok {
		return
	}
	unit.ArcaneCharge += float64(manaSpent) * def.ManaToChargeRatio
}

// arcaneMissileStaggerSeconds is the delay between successive bolts of one
// Arcane Missiles volley — they fire rat-a-tat rather than all at once for a
// better feel.
const arcaneMissileStaggerSeconds = 0.06

// pendingArcaneMissile is one queued bolt of a volley awaiting its staggered
// launch. The target is (re)picked at LAUNCH time so a bolt chases a live enemy
// rather than a stale one. IDs, never pointers (targeting invariant).
type pendingArcaneMissile struct {
	OwnerUnitID    int
	AbilityID      string
	DelayRemaining float64
}

// tickArcaneMissilesLocked drives the Arcane Missiles loop for the tick: it
// queues a fresh volley for any charged unit, then launches whichever queued
// bolts have finished their stagger. Charge is only consumed when there is at
// least one in-range enemy at trigger time — a full-but-idle Arch Mage banks
// its charge rather than wasting a volley. Iterates s.Units in slice order
// (deterministic). Caller holds s.mu write lock.
func (s *GameState) tickArcaneMissilesLocked(dt float64) {
	for _, unit := range s.Units {
		if unit == nil || unit.HP <= 0 {
			continue
		}
		def, ok := s.unitChargeFirePassiveLocked(unit)
		if !ok || unit.ArcaneCharge < def.ChargeRequired {
			continue
		}
		if s.queueArcaneMissileVolleyLocked(unit, def) {
			unit.ArcaneCharge -= def.ChargeRequired // carry any overflow
		}
	}
	s.launchDueArcaneMissilesLocked(dt)
}

// queueArcaneMissileVolleyLocked enqueues one volley of MissileCount bolts with
// staggered launch delays (0, 60ms, 120ms, …). Returns false without queuing
// when no enemy is in range at trigger time (caller then banks the charge).
// Caller holds s.mu write lock.
func (s *GameState) queueArcaneMissileVolleyLocked(unit *Unit, def AbilityDef) bool {
	if len(s.hostilesInRangeSortedLocked(unit, unit.AttackRange)) == 0 {
		return false
	}
	count := def.MissileCount
	if count < 1 {
		count = 1
	}
	// Per-missile stagger: authored in ms on the ability, default when unset.
	stagger := arcaneMissileStaggerSeconds
	if def.MissileDelayMs > 0 {
		stagger = def.MissileDelayMs / 1000.0
	}
	for i := 0; i < count; i++ {
		s.pendingArcaneMissiles = append(s.pendingArcaneMissiles, pendingArcaneMissile{
			OwnerUnitID:    unit.ID,
			AbilityID:      def.ID,
			DelayRemaining: float64(i) * stagger,
		})
	}
	return true
}

// launchDueArcaneMissilesLocked decrements every queued bolt's stagger by dt and
// launches those that have come due (FIFO for determinism). Caller holds s.mu.
func (s *GameState) launchDueArcaneMissilesLocked(dt float64) {
	if len(s.pendingArcaneMissiles) == 0 {
		return
	}
	kept := s.pendingArcaneMissiles[:0]
	for _, pm := range s.pendingArcaneMissiles {
		pm.DelayRemaining -= dt
		if pm.DelayRemaining > 0 {
			kept = append(kept, pm)
			continue
		}
		s.launchArcaneMissileLocked(pm.OwnerUnitID, pm.AbilityID)
	}
	s.pendingArcaneMissiles = kept
}

// launchArcaneMissileLocked fires a single bolt at a random in-range enemy
// (duplicates allowed), reusing the ability-projectile spawn path so
// mitigation / death / threat / attribution / minor-damage rendering are all
// inherited. A dead owner or an empty target list simply fizzles the bolt.
// Caller holds s.mu write lock.
func (s *GameState) launchArcaneMissileLocked(ownerID int, abilityID string) {
	owner := s.getUnitByIDLocked(ownerID)
	if owner == nil || owner.HP <= 0 {
		return
	}
	def, ok := getAbilityDef(abilityID)
	if !ok {
		return
	}
	candidates := s.hostilesInRangeSortedLocked(owner, owner.AttackRange)
	if len(candidates) == 0 {
		return
	}
	target := candidates[s.rngCombat.Intn(len(candidates))]
	// Carry the ability's projectileSpeed so missiles can be slowed via the
	// ability file (0 ⇒ the projectile def's own speed).
	s.fireAbilityProjectileLocked(owner, target, def, EffectiveSpell{
		Damage:          def.DamagePerMissile,
		ProjectileSpeed: def.ProjectileSpeed,
	})
}

// hostilesInRangeSortedLocked returns the visible, living units hostile to
// `unit` within `radius` world-px of it, sorted by unit ID for deterministic
// random selection. Caller holds s.mu.
func (s *GameState) hostilesInRangeSortedLocked(unit *Unit, radius float64) []*Unit {
	if unit == nil || radius <= 0 {
		return nil
	}
	radSq := radius * radius
	out := make([]*Unit, 0, 8)
	for _, u := range s.Units {
		if u == nil || u.ID == unit.ID || u.HP <= 0 || !u.Visible {
			continue
		}
		if !s.playersAreHostileLocked(u.OwnerID, unit.OwnerID) {
			continue
		}
		dx := u.X - unit.X
		dy := u.Y - unit.Y
		if dx*dx+dy*dy > radSq {
			continue
		}
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
