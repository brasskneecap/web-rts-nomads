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

	// Triggered is set true for EXACTLY ONE TICK when the trap detonates
	// (initial blast or aftershock). Serialized into TrapSnapshot.triggered so
	// the client renders a one-frame radial burst. Reset at the start of every
	// tickTrapEffectsLocked pass so it never persists across ticks.
	Triggered bool

	// PendingCull marks the trap for removal on the NEXT tickTrapsLocked pass.
	// Set when the trap's final blast fires (non-aftershock trap's only blast,
	// or aftershock trap's second blast). The one-tick delay between setting
	// this and actual removal ensures the Triggered=true flag is serialized
	// into the end-of-tick snapshot before the trap disappears.
	PendingCull bool

	// Per-type config (snapshot at plant time)
	DamagePerSecond float64 // caltrops, fire_pit
	SlowMultiplier  float64 // caltrops
	BurstDamage     int     // explosive_trap
	MarkMultiplier  float64 // marker_trap: bonus damage multiplier (e.g. 0.20 = +20%)
	MarkDuration    float64 // marker_trap: seconds applied per tick

	// Aftershock (explosive_trap + explosive_chain perk):
	// AftershockDelaySeconds is the snapshotted delay before a second blast.
	// Zero means no aftershock. Non-zero means the trap will re-blast that many
	// seconds after the initial trigger.
	AftershockDelaySeconds float64
	// AftershockPending is set true between the first blast and the aftershock.
	// While true, the trap is NOT culled (even if RemainingSeconds would expire)
	// and its Triggered flag is still false.
	AftershockPending bool
	// AftershockRemaining counts down from AftershockDelaySeconds once the
	// initial blast fires. When it reaches 0, the second blast fires and
	// Triggered is set to true.
	AftershockRemaining float64

	// ── Silver trap-specific upgrades (snapshot at plant time) ────────────────
	// All fields default to zero and are only populated when the owner owns the
	// corresponding Silver perk gated on the Bronze trap type. Zero means "no
	// upgrade active" — the trap behaves exactly like the Bronze baseline.

	// barbed_field (caltrops): ramping bonus DPS per second-in-zone.
	BarbedFieldRampPerSec    float64
	BarbedFieldMaxBonusDPS   float64

	// exposed_weakness (marker_trap): fraction of outgoing-damage reduction
	// stamped on marked victims alongside the usual mark. Composes via the
	// shared Weakened* plumbing (see perkOutgoingDamageDebuffMultiplierLocked).
	ExposedWeakenedMultiplier float64

	// lasting_flames (fire_pit): burn DoT params to apply when the victim
	// LEAVES this trap's radius. Plumbed through UnitPerkState.FirePitArmedBurn*
	// while the victim is in zone; transferred to Burn* on exit.
	LastingFlamesBurnDPS      float64
	LastingFlamesBurnDuration float64
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
		// Two-phase cull: on the blast tick tickTrapEffectsLocked sets both
		// Triggered=true AND PendingCull=true in the same Update call. Because
		// tickTrapsLocked runs AFTER tickTrapEffectsLocked (and before the
		// end-of-tick Snapshot), we must NOT cull while Triggered is still true —
		// the snapshot needs to see triggered=true to deliver the VFX frame to the
		// client.
		//
		// Next tick, tickTrapEffectsLocked resets Triggered=false at its top, so
		// the condition below fires and the trap is finally removed. The client
		// sees: blast-tick snapshot (triggered=true, trap present) → next-tick
		// snapshot (trap gone).
		if trap.PendingCull && !trap.Triggered {
			continue
		}
		if !trap.AftershockPending {
			// Only decay lifetime while waiting for the initial trigger.
			// AftershockPending traps hold their position until the scheduled blast
			// fires — decaying lifetime here would race against the 2s aftershock window.
			trap.RemainingSeconds -= dt
			if trap.RemainingSeconds <= 1e-9 {
				continue
			}
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

	// Reset per-tick transient VFX flags. Triggered is a one-tick signal that
	// must be cleared here so a trap that blasted last tick doesn't serialize
	// triggered=true in THIS tick's snapshot.
	for _, trap := range s.Traps {
		trap.Triggered = false
	}

	var deadUnitIDs []int

	for _, trap := range s.Traps {
		if trap.PendingCull {
			continue // blasted last tick; PendingCull awaits removal in tickTrapsLocked
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
				// barbed_field (silver): add ramping bonus DPS scaled by the
				// victim's accumulated in-zone time. The accumulator is advanced
				// once per tick in tickTrapperSilverDebuffsLocked regardless of
				// how many overlapping barbed zones hit; here we only read.
				dps := trap.DamagePerSecond
				if trap.BarbedFieldRampPerSec > 0 {
					bonus := unit.PerkState.BarbedFieldStaySeconds * trap.BarbedFieldRampPerSec
					if trap.BarbedFieldMaxBonusDPS > 0 && bonus > trap.BarbedFieldMaxBonusDPS {
						bonus = trap.BarbedFieldMaxBonusDPS
					}
					dps += bonus
					unit.PerkState.BarbedFieldInZoneThisTick = true
				}
				// DoT — accumulate fractional damage across ticks so production
				// tick rates (dt=0.05) don't round every tick to zero.
				unit.PerkState.TrapDoTAccumulator += dps * dt
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
				// lasting_flames (silver): arm a burn debuff that will stamp on
				// the victim the moment they exit. Refresh-stronger per dimension
				// across overlapping lasting fire pits. The exit detection and
				// actual stamp happen in tickTrapperSilverDebuffsLocked; here we
				// just note "this victim is standing in a lasting pit" and track
				// the strongest params to pre-arm.
				if trap.LastingFlamesBurnDPS > 0 {
					unit.PerkState.FirePitInLastingZoneThisTick = true
					if trap.LastingFlamesBurnDPS > unit.PerkState.FirePitArmedBurnDPS {
						unit.PerkState.FirePitArmedBurnDPS = trap.LastingFlamesBurnDPS
					}
					if trap.LastingFlamesBurnDuration > unit.PerkState.FirePitArmedBurnDuration {
						unit.PerkState.FirePitArmedBurnDuration = trap.LastingFlamesBurnDuration
					}
					// Track the last-seen trap owner so burn damage that lands
					// after exit can be credited for XP. Using last-seen is fine
					// because overlapping fire pits usually share an owner and
					// XP credit is not load-bearing for balance.
					unit.PerkState.FirePitArmedBurnOwnerID = trap.OwnerUnitID
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
			if trap.AftershockPending {
				// Countdown to the aftershock blast.
				trap.AftershockRemaining -= dt
				if trap.AftershockRemaining <= 0 {
					// Aftershock = final blast. Fire unconditionally at original position.
					deadUnitIDs = s.detonateExplosiveTrapLocked(trap, ownerUnit, deadUnitIDs)
					trap.AftershockPending = false
					trap.PendingCull = true // cull after this tick's Snapshot
				}
				break
			}
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
			// Phase 2: first blast.
			deadUnitIDs = s.detonateExplosiveTrapLocked(trap, ownerUnit, deadUnitIDs)
			if trap.AftershockDelaySeconds > 0 {
				// Initial blast only; aftershock scheduled. Do NOT set PendingCull.
				trap.AftershockPending = true
				trap.AftershockRemaining = trap.AftershockDelaySeconds
			} else {
				// Initial blast is also the final blast.
				trap.PendingCull = true
			}

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
				// exposed_weakness (silver): piggyback a Weakened debuff on top
				// of the mark so marked enemies also deal less damage. Reuses
				// the Vanguard-era Weakened* plumbing — the outgoing-damage
				// debuff is already wired into perkOutgoingDamageDebuffMultiplierLocked.
				// Refresh-stronger/refresh-longer per dimension, same as mark.
				if trap.ExposedWeakenedMultiplier > 0 {
					if trap.MarkDuration > unit.PerkState.WeakenedRemaining {
						unit.PerkState.WeakenedRemaining = trap.MarkDuration
					}
					if trap.ExposedWeakenedMultiplier > unit.PerkState.WeakenedMultiplier {
						unit.PerkState.WeakenedMultiplier = trap.ExposedWeakenedMultiplier
					}
				}
			}
		}
	}

	// Cull units that died from trap effects this tick.
	for _, id := range deadUnitIDs {
		s.removeUnitLocked(id)
	}
}

// tickTrapperSilverDebuffsLocked advances all Silver-trapper-specific per-unit
// debuff state that cannot live inside tickTrapEffectsLocked because it must
// continue to tick even when no trap entities exist (detached burn DoT) or
// must observe a tick-boundary transition (fire-pit exit detection).
//
// MUST be called AFTER tickTrapEffectsLocked so the scratch flags set by this
// tick's trap effects are current:
//   - BarbedFieldInZoneThisTick   (caltrops onStay)
//   - FirePitInLastingZoneThisTick (fire_pit onStay)
//
// Runs three passes per unit:
//  1. barbed_field: if the victim was hit by any barbed caltrops this tick,
//     advance the accumulator by dt. Otherwise reset — the ramp drops the
//     moment they step out of the zone.
//  2. lasting_flames: detect the edge transition from "in lasting zone" to
//     "not in lasting zone" and stamp the armed burn onto the victim. Then
//     shift the prev-tick snapshot forward.
//  3. burn DoT: decay BurnRemaining, bank fractional damage, apply when ≥1.
//     Credits the original trap owner when alive; purely applies damage when
//     the owner has died (no XP, same pattern as CC primitives).
//
// Dead units are collected and culled at the end of the pass, mirroring
// tickTrapEffectsLocked. New trap-specific debuffs plug in here alongside
// the existing three sections.
//
// Must be called under s.mu write lock.
func (s *GameState) tickTrapperSilverDebuffsLocked(dt float64) {
	var deadUnitIDs []int

	for _, unit := range s.Units {
		if unit == nil {
			continue
		}

		// ── barbed_field: accumulate or reset the in-zone timer ─────────────
		if unit.PerkState.BarbedFieldInZoneThisTick {
			unit.PerkState.BarbedFieldStaySeconds += dt
		} else {
			unit.PerkState.BarbedFieldStaySeconds = 0
		}
		unit.PerkState.BarbedFieldInZoneThisTick = false

		// ── lasting_flames: detect exit and stamp the armed burn debuff ─────
		// Transition prev=true, curr=false means the victim was in a lasting
		// fire pit last tick and has moved out this tick. Stamp the strongest
		// armed params we saw last tick onto Burn*.
		exited := unit.PerkState.FirePitInLastingZonePrev && !unit.PerkState.FirePitInLastingZoneThisTick
		if exited {
			if unit.PerkState.FirePitArmedBurnDuration > unit.PerkState.BurnRemaining {
				unit.PerkState.BurnRemaining = unit.PerkState.FirePitArmedBurnDuration
			}
			if unit.PerkState.FirePitArmedBurnDPS > unit.PerkState.BurnDamagePerSecond {
				unit.PerkState.BurnDamagePerSecond = unit.PerkState.FirePitArmedBurnDPS
			}
			// Credit the last-seen fire-pit owner for burn damage. If later
			// replaced by a stronger burn, the new owner overwrites; tracking
			// per-burn ownership for each re-arm would be more accurate but
			// the accounting doesn't justify the complexity.
			unit.PerkState.BurnOwnerUnitID = unit.PerkState.FirePitArmedBurnOwnerID
		}
		// Shift the prev-tick flag so next tick can observe a new transition.
		unit.PerkState.FirePitInLastingZonePrev = unit.PerkState.FirePitInLastingZoneThisTick
		unit.PerkState.FirePitInLastingZoneThisTick = false
		// Clear armed params when fully out of any lasting zone so the victim
		// cannot carry stale stronger-burn values across a re-entry later.
		if !unit.PerkState.FirePitInLastingZonePrev {
			unit.PerkState.FirePitArmedBurnDPS = 0
			unit.PerkState.FirePitArmedBurnDuration = 0
			unit.PerkState.FirePitArmedBurnOwnerID = 0
		}

		// ── burn DoT tick ───────────────────────────────────────────────────
		if unit.PerkState.BurnRemaining <= 0 || unit.HP <= 0 || !unit.Visible {
			continue
		}
		unit.PerkState.BurnRemaining = math.Max(0, unit.PerkState.BurnRemaining-dt)
		unit.PerkState.BurnDoTAccumulator += unit.PerkState.BurnDamagePerSecond * dt
		if unit.PerkState.BurnDoTAccumulator >= 1.0 {
			dmg := int(unit.PerkState.BurnDoTAccumulator)
			unit.PerkState.BurnDoTAccumulator -= float64(dmg)

			owner := s.unitsByID[unit.PerkState.BurnOwnerUnitID]
			if owner != nil && owner.HP <= 0 {
				owner = nil
			}
			s.applyUnitDamageLocked(unit, dmg)
			if owner != nil {
				s.recordDamageDealtLocked(owner, unit, dmg)
			}
			if unit.HP <= 0 {
				if owner != nil {
					s.awardKillXPLocked(owner)
					s.payoutDamageDealtXPLocked(unit)
				}
				deadUnitIDs = append(deadUnitIDs, unit.ID)
			}
		}
		if unit.PerkState.BurnRemaining == 0 {
			unit.PerkState.BurnDamagePerSecond = 0
			unit.PerkState.BurnOwnerUnitID = 0
			unit.PerkState.BurnDoTAccumulator = 0
		}
	}

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

	// Resolve effective modifiers from this unit's perks. Identity (all 1.0)
	// if the unit owns no Silver/Gold trap-modifying perks. Trap-specific
	// upgrades (barbed_field, exposed_weakness, lasting_flames, explosive_chain)
	// are resolved per-branch below via trapSpecificModifiersForUnitLocked.
	mods := s.trapModifiersForUnitLocked(unit)

	// Position is assigned AFTER the switch so the trap's final Radius is
	// available for the "edge-touching-archer" offset in trapPlacementOffsetLocked.
	trap := &Trap{
		ID:               trapIDString(id),
		OwnerUnitID:      unit.ID,
		OwnerPlayerID:    unit.OwnerID,
		RemainingSeconds: def.Config["durationSeconds"] * mods.DurationMultiplier,
		TrapType:         trapType,
	}

	// Trap-specific Silver/Gold upgrades are resolved once here. The resolver
	// is silent (zero values) for any perk the unit doesn't own or that doesn't
	// match this trap type — so blindly snapshotting the fields below is safe
	// even when no upgrade is active.
	specific := s.trapSpecificModifiersForUnitLocked(unit, trapType)

	switch trapType {
	case "caltrops":
		trap.Radius = def.Config["radius"] * mods.RadiusMultiplier
		trap.DamagePerSecond = def.Config["damagePerSecond"] * mods.EffectMultiplier
		trap.SlowMultiplier = amplifySlow(def.Config["slowMultiplier"], mods.EffectMultiplier)
		// barbed_field: ramp values scale with EffectMultiplier so amplified_effects
		// stacks the way the player expects (more ramp, harder cap).
		trap.BarbedFieldRampPerSec = specific.BarbedFieldRampPerSec * mods.EffectMultiplier
		trap.BarbedFieldMaxBonusDPS = specific.BarbedFieldMaxBonusDPS * mods.EffectMultiplier

	case "fire_pit":
		trap.Radius = def.Config["radius"] * mods.RadiusMultiplier
		trap.DamagePerSecond = def.Config["damagePerSecond"] * mods.EffectMultiplier
		// lasting_flames: burn DPS scales with EffectMultiplier; duration is a
		// debuff window (not the trap lifetime) so we scale it too, matching
		// the mark-duration rule for marker_trap above.
		trap.LastingFlamesBurnDPS = specific.LastingFlamesBurnDPS * mods.EffectMultiplier
		trap.LastingFlamesBurnDuration = specific.LastingFlamesBurnDuration * mods.EffectMultiplier

	case "explosive_trap":
		// explosionRadius (AoE) → Radius; triggerRadius (inner) → TriggerRadius.
		trap.Radius = def.Config["explosionRadius"] * mods.RadiusMultiplier
		trap.TriggerRadius = def.Config["triggerRadius"] * mods.RadiusMultiplier
		base := int(def.Config["burstDamage"])
		trap.BurstDamage = int(float64(base)*mods.EffectMultiplier + 0.5)
		// Trap-specific upgrade: explosive_chain schedules an aftershock blast.
		trap.AftershockDelaySeconds = specific.AftershockDelaySeconds

	case "marker_trap":
		trap.Radius = def.Config["radius"] * mods.RadiusMultiplier
		// MarkMultiplier and MarkDuration both scale with effect strength.
		// DurationMultiplier is about trap-entity lifetime, not the post-effect
		// debuff — EffectMultiplier governs both the strength and the window of
		// the mark applied to enemies.
		trap.MarkMultiplier = def.Config["markMultiplier"] * mods.EffectMultiplier
		trap.MarkDuration = def.Config["markDuration"] * mods.EffectMultiplier
		// exposed_weakness: damage-reduction strength scales with EffectMultiplier
		// so amplified_effects makes the debuff harsher. Duration aligns with
		// MarkDuration (stamped together in tickTrapEffectsLocked).
		trap.ExposedWeakenedMultiplier = specific.ExposedWeakenedMultiplier * mods.EffectMultiplier
	}

	// Position the trap "in front of" the unit, toward the nearest enemy, with
	// the near edge of the trap's circle touching the archer. Falls back to the
	// unit's exact position when no enemy can be found (keeps test scenarios
	// and fully-isolated placements deterministic).
	offsetX, offsetY := s.trapPlacementOffsetLocked(unit, trap.Radius)
	trap.X = unit.X + offsetX
	trap.Y = unit.Y + offsetY

	s.Traps = append(s.Traps, trap)
}

// trapPlacementOffsetLocked returns the (dx, dy) offset to add to the unit's
// position so a circular trap of the given radius is planted "in front of" the
// unit with the near edge of its circle touching the archer. Direction is
// chosen from (in priority order):
//
//  1. The unit's current AttackTargetID — definitionally the enemy being
//     engaged this combat tick and usually populated whenever LastCombatSeconds > 0.
//  2. The nearest visible hostile unit — O(N) fallback for the rare case the
//     attack target has died or been cleared the same tick placement fires.
//
// Returns (0, 0) when no enemy is found (plant at feet — preserves legacy
// behavior for tests that spawn a lone archer with no adversaries).
//
// Must be called under s.mu (read or write) lock.
func (s *GameState) trapPlacementOffsetLocked(unit *Unit, radius float64) (float64, float64) {
	if unit == nil || radius <= 0 {
		return 0, 0
	}

	var targetX, targetY float64
	haveTarget := false

	if unit.AttackTargetID != 0 {
		if target := s.unitsByID[unit.AttackTargetID]; target != nil &&
			target.HP > 0 && target.Visible && target.OwnerID != unit.OwnerID {
			targetX, targetY = target.X, target.Y
			haveTarget = true
		}
	}

	if !haveTarget {
		bestDistSq := math.Inf(1)
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
			distSq := dx*dx + dy*dy
			if distSq < bestDistSq {
				bestDistSq = distSq
				targetX, targetY = candidate.X, candidate.Y
				haveTarget = true
			}
		}
	}

	if !haveTarget {
		return 0, 0
	}

	dx := targetX - unit.X
	dy := targetY - unit.Y
	dist := math.Sqrt(dx*dx + dy*dy)
	if dist < 1e-6 {
		return 0, 0 // directly stacked — offsetting would divide by zero
	}

	return dx / dist * radius, dy / dist * radius
}

// detonateExplosiveTrapLocked applies the burst-damage phase of an explosive_trap
// blast (initial or aftershock). It deals BurstDamage to all visible enemy units
// within Radius, credits ownerUnit for kills and damage dealt, and appends newly
// killed unit IDs to deadUnitIDs. Returns the updated deadUnitIDs slice.
//
// Called from the phase-2 path (initial trigger) and from the aftershock path in
// tickTrapEffectsLocked so both blasts share identical logic without duplication.
//
// Must be called under s.mu write lock.
func (s *GameState) detonateExplosiveTrapLocked(trap *Trap, ownerUnit *Unit, deadUnitIDs []int) []int {
	// Signal the one-tick VFX flash. This is the single write site for both
	// the initial blast and the aftershock blast so both detonations surface
	// triggered=true in the tick's Snapshot.
	trap.Triggered = true
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
	return deadUnitIDs
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
	mods := s.trapModifiersForUnitLocked(unit)
	unit.PerkState.TrapPlaceCooldownRemaining = def.Config["placeIntervalSeconds"] * mods.CooldownMultiplier
}
