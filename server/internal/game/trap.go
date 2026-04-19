package game

import "math"

// ═════════════════════════════════════════════════════════════════════════════
// TRAP SYSTEM
//
// Traps are placeable, persistent entities created by Archer Trapper perk
// holders. They apply zone effects (slow, DoT, AoE burst, mark) to enemy units
// that enter their radius. Traps are team-sided: they never affect the owner's
// allies.
//
// Architecture mirrors the Banner system:
//   - Traps live on GameState.Traps ([]*Trap)
//   - tickTrapEffectsLocked applies zone effects each tick (before tickTrapsLocked)
//   - tickTrapsLocked decays lifetimes and culls expired/triggered traps
//   - plantTrapLocked constructs a new Trap from a perk's Config snapshot
//   - tickTrapPlacementLocked gates and drives the placement timer
//
// Auto-placement gate: the Trapper perk places a trap at the unit's feet during
// combat. "In combat" is tracked via UnitPerkState.LastCombatSeconds, a
// tail-window set to 1.5s whenever the Archer fires an attack and decayed each
// tick. Trap placement requires LastCombatSeconds > 0.
//
// CALL SITES (wired in state.go Update):
//   tickTrapEffectsLocked(dt)   — runs BEFORE tickBannersLocked
//   tickTrapsLocked(dt)         — runs AFTER tickBannersLocked
//   tickTrapPlacementLocked     — called from tickUnitPerkStateLocked
// ═════════════════════════════════════════════════════════════════════════════

// Trap is a placeable hazard entity created by Archer Trapper perks.
// All config fields are snapshotted from the perk's Config map at plant time
// so that live catalog tuning does not retroactively change active traps.
type Trap struct {
	// Identity
	ID            string
	OwnerUnitID   int
	OwnerPlayerID string

	// Position and geometry
	X, Y          float64
	Radius        float64 // zone effect radius (caltrops/fire_pit/marker_trap: trigger AND effect zone)
	TriggerRadius float64 // for explosive_trap: the smaller inner radius that triggers the blast

	// Lifetime
	RemainingSeconds float64
	TrapType         string
	Triggered        bool // set to true when explosive_trap fires; causes cull next tick

	// Per-type config (snapshot at plant time)
	DamagePerSecond float64 // caltrops, fire_pit
	SlowMultiplier  float64 // caltrops
	BurstDamage     int     // explosive_trap
	MarkMultiplier  float64 // marker_trap: bonus damage multiplier (e.g. 0.20 = +20%)
	MarkDuration    float64 // marker_trap: seconds applied per tick
}

// tickTrapsLocked advances all active trap lifetimes by dt seconds, removing
// expired traps and traps that have already triggered.
//
// Uses the filter-into-front-of-slice pattern to avoid allocations in the
// steady state. Traps whose owner player has left the match are also dropped.
//
// Must be called under s.mu write lock.
func (s *GameState) tickTrapsLocked(dt float64) {
	if len(s.Traps) == 0 {
		return
	}
	kept := s.Traps[:0]
	for _, trap := range s.Traps {
		// Drop traps that have triggered (explosive_trap detonated this tick).
		if trap.Triggered {
			continue
		}
		trap.RemainingSeconds -= dt
		if trap.RemainingSeconds <= 1e-9 {
			continue
		}
		// Drop if owner's player has left the match.
		if _, ok := s.Players[trap.OwnerPlayerID]; !ok {
			continue
		}
		kept = append(kept, trap)
	}
	s.Traps = kept
}

// tickTrapEffectsLocked applies zone effects for all active, non-triggered
// traps. Effect resolution always runs BEFORE tickTrapsLocked so that a trap
// expiring this tick still applies its effect one last time.
//
// Must be called under s.mu write lock.
func (s *GameState) tickTrapEffectsLocked(dt float64) {
	if len(s.Traps) == 0 {
		return
	}

	var deadUnitIDs []int

	for _, trap := range s.Traps {
		if trap.Triggered {
			continue
		}

		// Look up the owner unit for XP credit (nil when dead — handled below).
		ownerUnit := s.unitsByID[trap.OwnerUnitID]
		if ownerUnit != nil && ownerUnit.HP <= 0 {
			ownerUnit = nil
		}

		switch trap.TrapType {

		case "caltrops":
			for _, unit := range s.Units {
				if unit == nil || unit.OwnerID == trap.OwnerPlayerID {
					continue // skip allies
				}
				if unit.HP <= 0 || !unit.Visible {
					continue
				}
				dx := unit.X - trap.X
				dy := unit.Y - trap.Y
				if dx*dx+dy*dy > trap.Radius*trap.Radius {
					continue
				}
				// Apply slow with a 1s refresh window so it expires ~1s after leaving.
				s.ApplySlowLocked(unit.ID, trap.SlowMultiplier, 1.0)
				// DoT — accumulate fractional damage across ticks so production
				// tick rates (dt=0.05) don't round every tick to zero.
				unit.PerkState.TrapDoTAccumulator += trap.DamagePerSecond * dt
				if unit.PerkState.TrapDoTAccumulator >= 1.0 {
					dmg := int(unit.PerkState.TrapDoTAccumulator)
					unit.PerkState.TrapDoTAccumulator -= float64(dmg)
					s.applyUnitDamageLocked(unit, dmg)
					if ownerUnit != nil {
						s.recordDamageDealtLocked(ownerUnit, unit, dmg)
					}
					if unit.HP <= 0 {
						if ownerUnit != nil {
							s.awardKillXPLocked(ownerUnit)
							s.payoutDamageDealtXPLocked(unit)
						}
						deadUnitIDs = append(deadUnitIDs, unit.ID)
					}
				}
			}

		case "fire_pit":
			for _, unit := range s.Units {
				if unit == nil || unit.OwnerID == trap.OwnerPlayerID {
					continue
				}
				if unit.HP <= 0 || !unit.Visible {
					continue
				}
				dx := unit.X - trap.X
				dy := unit.Y - trap.Y
				if dx*dx+dy*dy > trap.Radius*trap.Radius {
					continue
				}
				// DoT — accumulate fractional damage across ticks so production
				// tick rates (dt=0.05) don't round every tick to zero.
				unit.PerkState.TrapDoTAccumulator += trap.DamagePerSecond * dt
				if unit.PerkState.TrapDoTAccumulator >= 1.0 {
					dmg := int(unit.PerkState.TrapDoTAccumulator)
					unit.PerkState.TrapDoTAccumulator -= float64(dmg)
					s.applyUnitDamageLocked(unit, dmg)
					if ownerUnit != nil {
						s.recordDamageDealtLocked(ownerUnit, unit, dmg)
					}
					if unit.HP <= 0 {
						if ownerUnit != nil {
							s.awardKillXPLocked(ownerUnit)
							s.payoutDamageDealtXPLocked(unit)
						}
						deadUnitIDs = append(deadUnitIDs, unit.ID)
					}
				}
			}

		case "explosive_trap":
			// Phase 1: detect the first enemy in TriggerRadius.
			triggerRadSq := trap.TriggerRadius * trap.TriggerRadius
			triggered := false
			for _, unit := range s.Units {
				if unit == nil || unit.OwnerID == trap.OwnerPlayerID {
					continue
				}
				if unit.HP <= 0 || !unit.Visible {
					continue
				}
				dx := unit.X - trap.X
				dy := unit.Y - trap.Y
				if dx*dx+dy*dy <= triggerRadSq {
					triggered = true
					break
				}
			}
			if !triggered {
				break
			}
			// Phase 2: deal burst damage to ALL enemies within explosion radius.
			// Allies are filtered at the top of this loop — the explosion never
			// harms the owner's team. This invariant is tested explicitly.
			explosionRadSq := trap.Radius * trap.Radius
			for _, unit := range s.Units {
				if unit == nil || unit.OwnerID == trap.OwnerPlayerID {
					continue // no friendly fire
				}
				if unit.HP <= 0 || !unit.Visible {
					continue
				}
				dx := unit.X - trap.X
				dy := unit.Y - trap.Y
				if dx*dx+dy*dy > explosionRadSq {
					continue
				}
				s.applyUnitDamageLocked(unit, trap.BurstDamage)
				if ownerUnit != nil {
					s.recordDamageDealtLocked(ownerUnit, unit, trap.BurstDamage)
				}
				if unit.HP <= 0 {
					if ownerUnit != nil {
						s.awardKillXPLocked(ownerUnit)
						s.payoutDamageDealtXPLocked(unit)
					}
					deadUnitIDs = append(deadUnitIDs, unit.ID)
				}
			}
			trap.Triggered = true
			// tickTrapsLocked culls this trap next tick.

		case "marker_trap":
			for _, unit := range s.Units {
				if unit == nil || unit.OwnerID == trap.OwnerPlayerID {
					continue
				}
				if unit.HP <= 0 || !unit.Visible {
					continue
				}
				dx := unit.X - trap.X
				dy := unit.Y - trap.Y
				if dx*dx+dy*dy > trap.Radius*trap.Radius {
					continue
				}
				// Refresh-stronger / refresh-longer: use max per dimension so the
				// strongest mark wins and the mark cannot be shortened by a weaker
				// overlapping source. Consistent with ApplySlowLocked semantics.
				if trap.MarkDuration > unit.PerkState.MarkedRemaining {
					unit.PerkState.MarkedRemaining = trap.MarkDuration
				}
				if trap.MarkMultiplier > unit.PerkState.MarkedMultiplier {
					unit.PerkState.MarkedMultiplier = trap.MarkMultiplier
				}
			}
		}
	}

	// Cull units that died from trap effects this tick.
	for _, id := range deadUnitIDs {
		s.removeUnitLocked(id)
	}
}

// plantTrapLocked constructs a new Trap at the unit's current position from the
// perk's config snapshot and appends it to s.Traps. nextTrapID is advanced.
//
// Must be called under s.mu write lock.
func (s *GameState) plantTrapLocked(unit *Unit, def *PerkDef) {
	if unit == nil || def == nil {
		return
	}

	id := s.nextTrapID
	s.nextTrapID++

	trapType := def.ID // perk ID == trap type string ("caltrops", "fire_pit", etc.)

	trap := &Trap{
		ID:               trapIDString(id),
		OwnerUnitID:      unit.ID,
		OwnerPlayerID:    unit.OwnerID,
		X:                unit.X,
		Y:                unit.Y,
		RemainingSeconds: def.Config["durationSeconds"],
		TrapType:         trapType,
	}

	switch trapType {
	case "caltrops":
		trap.Radius = def.Config["radius"]
		trap.DamagePerSecond = def.Config["damagePerSecond"]
		trap.SlowMultiplier = def.Config["slowMultiplier"]

	case "fire_pit":
		trap.Radius = def.Config["radius"]
		trap.DamagePerSecond = def.Config["damagePerSecond"]

	case "explosive_trap":
		// explosionRadius (AoE) → Radius; triggerRadius (inner) → TriggerRadius.
		trap.Radius = def.Config["explosionRadius"]
		trap.TriggerRadius = def.Config["triggerRadius"]
		trap.BurstDamage = int(def.Config["burstDamage"])

	case "marker_trap":
		trap.Radius = def.Config["radius"]
		trap.MarkMultiplier = def.Config["markMultiplier"]
		trap.MarkDuration = def.Config["markDuration"]
	}

	s.Traps = append(s.Traps, trap)
}

// trapIDString formats a trap sequence number as a human-readable trap ID.
// Mirrors the pattern used for banners (integer ID on the struct).
func trapIDString(id int) string {
	// Inline simple int-to-string to avoid importing fmt in hot path.
	// For any realistic match this loop terminates in < 10 iterations.
	if id == 0 {
		return "trap-0"
	}
	digits := [20]byte{}
	pos := len(digits)
	n := id
	for n > 0 {
		pos--
		digits[pos] = byte('0' + n%10)
		n /= 10
	}
	return "trap-" + string(digits[pos:])
}

// tickTrapPlacementLocked is the per-tick auto-placement driver for Trapper
// perks. It decays TrapPlaceCooldownRemaining (and LastCombatSeconds is decayed
// in state.go's per-unit loop alongside other cross-unit debuffs), gates
// placement on the unit being alive and in combat, and plants a new trap when
// the cooldown expires.
//
// Called from tickUnitPerkStateLocked for each trap perk case.
// Must be called under s.mu write lock.
func (s *GameState) tickTrapPlacementLocked(unit *Unit, def *PerkDef, dt float64) {
	if unit == nil || def == nil {
		return
	}

	// Decay placement cooldown.
	if unit.PerkState.TrapPlaceCooldownRemaining > 0 {
		unit.PerkState.TrapPlaceCooldownRemaining = math.Max(0, unit.PerkState.TrapPlaceCooldownRemaining-dt)
	}

	// Dead unit: no placement.
	if unit.HP <= 0 {
		return
	}

	// Out-of-combat gate: no placement unless archer has recently attacked.
	if unit.PerkState.LastCombatSeconds <= 0 {
		return
	}

	// Cooldown still running: wait.
	if unit.PerkState.TrapPlaceCooldownRemaining > 0 {
		return
	}

	// Plant the trap and reset the cooldown.
	s.plantTrapLocked(unit, def)
	unit.PerkState.TrapPlaceCooldownRemaining = def.Config["placeIntervalSeconds"]
}
