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

	// lasting_flames (fire_pit): when LastingFlamesBurnDuration > 0 on a
	// fire_pit trap, the trap stops dealing direct DoT and instead applies a
	// burn debuff to every enemy in the zone. The burn's DPS is the fire
	// pit's own effective DamagePerSecond (EffectMultiplier-scaled) — we
	// don't snapshot a separate DPS here because the design ties the burn
	// rate to the fire pit's tuning directly. Duration is refreshed while
	// the victim is in the zone, so leaving starts a fresh countdown.
	LastingFlamesBurnDuration float64

	// ── Gold trap-specific upgrades (snapshot at plant time) ──────────────────
	// All fields default to zero and are only populated when the owner owns the
	// corresponding Gold perk gated on the Bronze trap type. Zero means "no
	// upgrade active".

	// ascendant_infusion → Electrified Caltrops (caltrops).
	// Adds bonus damage on each DoT integer tick and grants a chance to
	// micro-stun the victim. Per-target stun cooldown lives on the victim's
	// PerkState to avoid stun-lock.
	InfusionElectrifiedBonusDamage     int
	InfusionElectrifiedStunChance      float64
	InfusionElectrifiedStunDuration    float64
	InfusionElectrifiedStunCooldownSec float64

	// ascendant_infusion → Reactive Flames (fire_pit).
	// Each fire_pit DoT integer tick and each lasting_flames burn integer tick
	// triggers a small raw-damage AoE around the victim. Secondary explosions
	// do NOT themselves trigger further explosions or apply burns.
	InfusionReactiveFlamesRadius float64
	InfusionReactiveFlamesDamage int

	// ascendant_infusion → Scatter Bomb (explosive_trap).
	// On detonation, spawns InfusionScatterBombCount mini explosive_traps
	// around the explosion at InfusionScatterBombSpawnRadius. Mini traps
	// inherit base/silver damage but do NOT carry any gold modifiers
	// (IsScatterBombChild=true blocks recursion).
	InfusionScatterBombCount        int
	InfusionScatterBombSpawnRadius  float64
	InfusionScatterBombChildSeconds float64

	// ascendant_infusion → Shared Pain (marker_trap).
	// Fraction of incoming damage redistributed to other marked enemies when
	// a marked enemy takes damage. Plumbed onto victims via MarkedRemaining
	// stamping — the trap snapshots the fraction so the value still applies
	// after the owner dies or the perk is tuned live.
	InfusionSharedPainFraction float64

	// overload_protocol → Spike Surge (caltrops).
	// On trap expiry: burst damage + strong slow applied to all enemies still
	// inside the caltrops zone. Fired once in tickTrapsLocked just before cull.
	OverloadSpikeSurgeBurstDamage   int
	OverloadSpikeSurgeSlowMult      float64
	OverloadSpikeSurgeSlowDuration  float64

	// overload_protocol → Flame Collapse (fire_pit).
	// On trap expiry: AoE explosion at the center with a re-applied burn to
	// all affected enemies. The burn uses the shared Burn* plumbing (same as
	// lasting_flames) so it integrates with existing debuff decay + DoT ticks.
	OverloadFlameCollapseRadius      float64
	OverloadFlameCollapseDamage      int
	OverloadFlameCollapseBurnDPS     float64
	OverloadFlameCollapseBurnSeconds float64

	// overload_protocol → Cataclysm Blast (explosive_trap).
	// Larger-radius initial blast (Radius is pre-multiplied at plant time) and
	// a delayed secondary explosion wired through the existing aftershock
	// plumbing. If explosive_chain is also owned, the longer of the two delays
	// wins (the effects stack additively on the final blast, not as two
	// separate blasts — the aftershock slot is single).
	OverloadCataclysmDelaySeconds float64
	// Client-side sprite inflate for this trap while overload_protocol is
	// active. Snapshotted at plant time from gold.json's cataclysmSpriteScale
	// so tuning lives next to the other Cataclysm knobs. 0 = no inflate.
	OverloadCataclysmSpriteScale float64

	// overload_protocol → Final Exposure (marker_trap).
	// When the MARK expires (not the trap), marked enemies take burst damage
	// with an optional small AoE. Plumbed onto victims at mark-stamp time via
	// PerkState.FinalExposure* fields; fired on mark-expiry in state.go.
	OverloadFinalExposureDamage    int
	OverloadFinalExposureAoeRadius float64

	// IsScatterBombChild is true for the mini explosive_traps spawned by
	// Scatter Bomb. Blocks recursion: children cannot themselves spawn more
	// children, and do NOT apply any gold-tier effects.
	IsScatterBombChild bool

	// IsBonusDeployment is true for each extra trap planted by Increased
	// Deployment. Exclusively a debug/telemetry marker — no runtime branching
	// keys off this field. Recursion is blocked at the plant site instead.
	IsBonusDeployment bool
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
				// overload_protocol expiry effects fire exactly once, just
				// before the trap is dropped. Skipped for PendingCull traps
				// (explosive_trap detonation path) because those already had
				// their final effect via detonateExplosiveTrapLocked.
				s.fireTrapExpiryEffectsLocked(trap)
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
				if unit == nil || !playersAreHostile(unit.OwnerID, trap.OwnerPlayerID) {
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
					// ascendant_infusion → Electrified Caltrops: bonus damage on
					// each integer tick, and a stun roll gated by a per-victim
					// cooldown (ElectrifiedStunCooldownRemaining) to prevent
					// stun-lock. Bonus damage applies even when no stun fires.
					if trap.InfusionElectrifiedBonusDamage > 0 {
						dmg += trap.InfusionElectrifiedBonusDamage
					}
					if trap.InfusionElectrifiedStunChance > 0 &&
						unit.PerkState.ElectrifiedStunCooldownRemaining <= 0 &&
						s.rngPerks.Float64() < trap.InfusionElectrifiedStunChance {
						s.ApplyStunLocked(unit.ID, trap.InfusionElectrifiedStunDuration)
						unit.PerkState.ElectrifiedStunCooldownRemaining = trap.InfusionElectrifiedStunCooldownSec
					}
					s.applyUnitDamageWithSourceLocked(unit, dmg, DamageSource{AttackerTrapID: trap.ID, Kind: "trap_dot"})
					if ownerUnit != nil {
						s.recordDamageDealtLocked(ownerUnit, unit, dmg)
					}
					// Debug: attribute to the trap even when the owner unit is
					// dead — trap damage continues after the trapper dies.
					s.trackBattleDamageLocked(battleSourceFromTrap(trap), unit, dmg)
					if unit.HP <= 0 {
						if ownerUnit != nil {
							s.awardKillXPLocked(ownerUnit)
							s.payoutDamageDealtXPLocked(unit)
						}
						s.trackBattleKillLocked(battleSourceFromTrap(trap), unit)
						deadUnitIDs = append(deadUnitIDs, unit.ID)
					}
				}
			}

		case "fire_pit":
			for _, unit := range s.Units {
				if unit == nil || !playersAreHostile(unit.OwnerID, trap.OwnerPlayerID) {
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
				// lasting_flames (silver): the fire_pit applies its damage as a
				// BURN DEBUFF instead of dealing direct DoT in the zone. The
				// burn duration is refreshed every tick while the victim is in
				// the zone, so leaving the zone starts the post-exit countdown
				// with the full duration. The burn DPS is the fire_pit's own
				// EffectMultiplier-scaled DamagePerSecond — amplified_effects
				// scales the burn the same way it would have scaled direct DoT.
				//
				// This unifies damage delivery across in-zone and out-of-zone
				// ticks, which composes cleanly with Gold perks:
				//   - Reactive Flames triggers on every burn tick (inside AND
				//     outside the pit), doubling its reach vs. the old design.
				//   - Flame Collapse also applies a burn, so its damage uses
				//     the same accumulator and timer — stacking works naturally.
				if trap.LastingFlamesBurnDuration > 0 {
					// Apply/refresh a burn stack keyed by this trap's owner.
					// Multiple trappers each get their own stack (up to
					// maxDebuffStacks); same-source re-applications refresh
					// the one stack. Duration resets every tick while in-zone
					// so leaving starts a full post-exit countdown.
					//
					// NOTE: applyBurnStack uses refresh-longer on duration, so
					// "reset every tick" is achieved by passing the configured
					// burnDuration unchanged — it's always the target value and
					// max(currentRemaining, burnDuration) == burnDuration while
					// the victim sits in the pit.
					// Keyed by trap.ID so two fire_pit traps from the SAME
					// trapper (e.g. increased_deployment's bonus traps
					// overlapping the primary) each land their own burn
					// stack on an enemy standing in both.
					unit.PerkState.applyBurnStack(
						trap.ID,
						trap.OwnerUnitID,
						trap.DamagePerSecond,
						trap.LastingFlamesBurnDuration,
						trap.InfusionReactiveFlamesRadius,
						trap.InfusionReactiveFlamesDamage,
					)
					// Damage is delivered by the burn tick in
					// tickTrapperSilverDebuffsLocked — SKIP inline DoT here.
					continue
				}
				// Base fire_pit (no lasting_flames): direct DoT applied in-zone.
				// Accumulate fractional damage across ticks so production tick
				// rates (dt=0.05) don't round every tick to zero.
				unit.PerkState.TrapDoTAccumulator += trap.DamagePerSecond * dt
				if unit.PerkState.TrapDoTAccumulator >= 1.0 {
					dmg := int(unit.PerkState.TrapDoTAccumulator)
					unit.PerkState.TrapDoTAccumulator -= float64(dmg)
					s.applyUnitDamageWithSourceLocked(unit, dmg, DamageSource{AttackerTrapID: trap.ID, Kind: "trap_dot"})
					if ownerUnit != nil {
						s.recordDamageDealtLocked(ownerUnit, unit, dmg)
					}
					// Debug: fire_pit DoT attribution, survives dead owner.
					s.trackBattleDamageLocked(battleSourceFromTrap(trap), unit, dmg)
					// ascendant_infusion → Reactive Flames: each integer DoT
					// tick triggers a small raw-damage AoE around the victim.
					// The explosion is untagged on burns/explosions so it does
					// not recursively trigger more Reactive Flames or apply
					// burns (see fireReactiveFlamesLocked).
					if trap.InfusionReactiveFlamesDamage > 0 && unit.HP > 0 {
						deadUnitIDs = s.fireReactiveFlamesLocked(
							unit.X, unit.Y,
							trap.InfusionReactiveFlamesRadius,
							trap.InfusionReactiveFlamesDamage,
							trap.OwnerPlayerID, ownerUnit, trap.ID, deadUnitIDs,
						)
					}
					if unit.HP <= 0 {
						if ownerUnit != nil {
							s.awardKillXPLocked(ownerUnit)
							s.payoutDamageDealtXPLocked(unit)
						}
						s.trackBattleKillLocked(battleSourceFromTrap(trap), unit)
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
				if unit == nil || !playersAreHostile(unit.OwnerID, trap.OwnerPlayerID) {
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
				if unit == nil || !playersAreHostile(unit.OwnerID, trap.OwnerPlayerID) {
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
				// Apply a mark stack keyed by this trap's ID (not the owner
				// unit) so two marker_traps from the same trapper — e.g.
				// the primary + increased_deployment bonus — each land
				// their own stack when their zones overlap. Same-trap
				// re-ticks refresh that stack in place.
				unit.PerkState.applyMarkStack(trap.ID, trap.OwnerUnitID, trap.MarkMultiplier, trap.MarkDuration)
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
				// ascendant_infusion → Shared Pain: stamp the victim with the
				// redistribution fraction while they remain marked. Cleared in
				// state.go when MarkedRemaining decays to 0. Refresh-stronger
				// so a stronger overlapping source wins.
				if trap.InfusionSharedPainFraction > unit.PerkState.SharedPainFraction {
					unit.PerkState.SharedPainFraction = trap.InfusionSharedPainFraction
				}
				// overload_protocol → Final Exposure: arm the mark-expiry burst
				// on the victim. Refresh-stronger per dimension, and always
				// track the latest non-zero owner so XP credit can be awarded.
				if trap.OverloadFinalExposureDamage > 0 {
					if trap.OverloadFinalExposureDamage > unit.PerkState.FinalExposureDamage {
						unit.PerkState.FinalExposureDamage = trap.OverloadFinalExposureDamage
					}
					if trap.OverloadFinalExposureAoeRadius > unit.PerkState.FinalExposureAoeRadius {
						unit.PerkState.FinalExposureAoeRadius = trap.OverloadFinalExposureAoeRadius
					}
					unit.PerkState.FinalExposureOwnerUnitID = trap.OwnerUnitID
					unit.PerkState.FinalExposureTrapID = trap.ID
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
// continue to tick even when no trap entities exist (detached burn DoT).
//
// MUST be called AFTER tickTrapEffectsLocked so the scratch flags set by this
// tick's trap effects are current:
//   - BarbedFieldInZoneThisTick   (caltrops onStay)
//
// Runs two passes per unit:
//  1. barbed_field: if the victim was hit by any barbed caltrops this tick,
//     advance the accumulator by dt. Otherwise reset — the ramp drops the
//     moment they step out of the zone.
//  2. burn DoT: decay BurnRemaining, bank fractional damage, apply when ≥1.
//     Credits the original trap owner when alive; purely applies damage when
//     the owner has died (no XP, same pattern as CC primitives). While the
//     victim stands in a lasting_flames fire_pit, the fire_pit branch of
//     tickTrapEffectsLocked refreshes BurnRemaining to the full duration
//     every tick, so the countdown here only makes progress once the victim
//     leaves the zone.
//
// Dead units are collected and culled at the end of the pass, mirroring
// tickTrapEffectsLocked. New trap-specific debuffs plug in here alongside
// the existing two sections.
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

		// ── burn DoT tick ───────────────────────────────────────────────────
		// Iterate each stack independently: every stack decays its own
		// Remaining, banks its own fractional damage, credits its own owner
		// for XP/telemetry, and (if armed) fires its own Reactive Flames AoE.
		// Stacks that hit Remaining == 0 this tick are dropped in-place.
		if len(unit.PerkState.BurnStacks) == 0 || unit.HP <= 0 || !unit.Visible {
			continue
		}
		kept := unit.PerkState.BurnStacks[:0]
		for _, stack := range unit.PerkState.BurnStacks {
			stack.Remaining = math.Max(0, stack.Remaining-dt)
			stack.Accumulator += stack.DPS * dt
			if stack.Accumulator >= 1.0 {
				dmg := int(stack.Accumulator)
				stack.Accumulator -= float64(dmg)

				owner := s.unitsByID[stack.OwnerUnitID]
				if owner != nil && owner.HP <= 0 {
					owner = nil
				}
				// Reactive Flames friendly-fire filter: prefer the live
				// owner's PlayerID; fall back to the victim's own OwnerID
				// (victim is hostile to the trapper's team, so any
				// non-same-owner unit is a valid AoE target).
				ownerPlayerID := unit.OwnerID
				if owner != nil {
					ownerPlayerID = owner.OwnerID
				}
				s.applyUnitDamageWithSourceLocked(unit, dmg, DamageSource{AttackerTrapID: stack.SourceID, Kind: "trap_silver_stack"})
				if owner != nil {
					s.recordDamageDealtLocked(owner, unit, dmg)
				}
				// Debug: burn damage attributes to fire_pit for this owner.
				// Requires a live owner so we can read their PlayerID —
				// minor attribution gap when the trapper dies mid-burn.
				if owner != nil {
					s.trackBattleDamageLocked(
						BattleSource{PlayerID: owner.OwnerID, Kind: "trap", Subtype: "fire_pit"},
						unit, dmg,
					)
				}
				// ascendant_infusion → Reactive Flames: each stack fires
				// its own AoE on each integer tick. Two stacks with
				// Infusion → two explosions per tick (independent
				// sources, independent effects).
				if stack.ReactiveDamage > 0 && unit.HP > 0 {
					deadUnitIDs = s.fireReactiveFlamesLocked(
						unit.X, unit.Y,
						stack.ReactiveRadius,
						stack.ReactiveDamage,
						ownerPlayerID, owner, stack.SourceID, deadUnitIDs,
					)
				}
				if unit.HP <= 0 {
					if owner != nil {
						s.awardKillXPLocked(owner)
						s.payoutDamageDealtXPLocked(unit)
						s.trackBattleKillLocked(
							BattleSource{PlayerID: owner.OwnerID, Kind: "trap", Subtype: "fire_pit"},
							unit,
						)
					}
					deadUnitIDs = append(deadUnitIDs, unit.ID)
					// Victim is dead — no point ticking the remaining
					// stacks against a corpse.
					break
				}
			}
			if stack.Remaining > 0 {
				kept = append(kept, stack)
			}
		}
		unit.PerkState.BurnStacks = kept
	}

	for _, id := range deadUnitIDs {
		s.removeUnitLocked(id)
	}
}

// plantTrapLocked constructs a new Trap at the unit's current position from the
// perk's config snapshot and appends it to s.Traps. When the unit owns
// increased_deployment (gold), additional bonus traps are planted at
// perpendicular offsets using the same snapshotted stats — count is driven by
// that perk's bonusTrapCount config key. The bonus plants are NOT recursive —
// only the outer plant call reads increased_deployment.
//
// Must be called under s.mu write lock.
func (s *GameState) plantTrapLocked(unit *Unit, def *PerkDef) {
	if unit == nil || def == nil {
		return
	}
	s.plantOneTrapLocked(unit, def, 0)
	if !containsString(unit.PerkIDs, "increased_deployment") {
		return
	}
	bonusCount := 1
	if bd := perkDefByID("increased_deployment"); bd != nil {
		if v, ok := bd.ConfigForRank(unit.Rank)["bonusTrapCount"]; ok && v > 0 {
			bonusCount = int(v)
		}
	}
	for i := 1; i <= bonusCount; i++ {
		s.plantOneTrapLocked(unit, def, i)
	}
}

// plantOneTrapLocked is the single-trap plant primitive used by plantTrapLocked
// for both the primary trap and each increased_deployment bonus trap.
// bonusIndex == 0 means the primary trap (centered on the throw target);
// bonusIndex >= 1 marks a bonus trap and determines its offset direction and
// magnitude so multiple bonuses fan out to alternating sides of the primary.
//
// Must be called under s.mu write lock.
func (s *GameState) plantOneTrapLocked(unit *Unit, def *PerkDef, bonusIndex int) {
	isBonus := bonusIndex > 0
	id := s.nextTrapID
	s.nextTrapID++

	trapType := def.ID // perk ID == trap type string ("caltrops", "fire_pit", etc.)

	// Resolve effective modifiers from this unit's perks. Identity (all 1.0)
	// if the unit owns no Silver/Gold trap-modifying perks. Trap-specific
	// upgrades (barbed_field, exposed_weakness, lasting_flames, explosive_chain,
	// ascendant_infusion, overload_protocol) are resolved per-branch below via
	// trapSpecificModifiersForUnitLocked.
	mods := s.trapModifiersForUnitLocked(unit)

	// Rank-scoped tuning: bronze trap perks persist through silver/gold, so
	// base values in the JSON are overridden by the matching rank block when
	// present. See PerkDef.ConfigForRank.
	cfg := def.ConfigForRank(unit.Rank)

	// Position is assigned AFTER the switch so the trap's final Radius is
	// available for the "edge-touching-archer" offset in trapPlacementOffsetLocked.
	trap := &Trap{
		ID:                trapIDString(id),
		OwnerUnitID:       unit.ID,
		OwnerPlayerID:     unit.OwnerID,
		RemainingSeconds:  cfg["durationSeconds"] * mods.DurationMultiplier,
		TrapType:          trapType,
		IsBonusDeployment: isBonus,
	}

	// Trap-specific Silver/Gold upgrades are resolved once here. The resolver
	// is silent (zero values) for any perk the unit doesn't own or that doesn't
	// match this trap type — so blindly snapshotting the fields below is safe
	// even when no upgrade is active.
	specific := s.trapSpecificModifiersForUnitLocked(unit, trapType)

	switch trapType {
	case "caltrops":
		trap.Radius = cfg["radius"] * mods.RadiusMultiplier
		trap.DamagePerSecond = cfg["damagePerSecond"] * mods.EffectMultiplier
		trap.SlowMultiplier = amplifySlow(cfg["slowMultiplier"], mods.EffectMultiplier)
		// barbed_field: ramp values scale with EffectMultiplier so amplified_effects
		// stacks the way the player expects (more ramp, harder cap).
		trap.BarbedFieldRampPerSec = specific.BarbedFieldRampPerSec * mods.EffectMultiplier
		trap.BarbedFieldMaxBonusDPS = specific.BarbedFieldMaxBonusDPS * mods.EffectMultiplier
		// ascendant_infusion → Electrified Caltrops. Bonus damage scales with
		// EffectMultiplier; stun odds/duration/cooldown are carried as-is so
		// tuning remains predictable (tuning chance with effectMult would be
		// surprising).
		trap.InfusionElectrifiedBonusDamage = int(float64(specific.InfusionElectrifiedBonusDamage)*mods.EffectMultiplier + 0.5)
		trap.InfusionElectrifiedStunChance = specific.InfusionElectrifiedStunChance
		trap.InfusionElectrifiedStunDuration = specific.InfusionElectrifiedStunDuration
		trap.InfusionElectrifiedStunCooldownSec = specific.InfusionElectrifiedStunCooldownSec
		// overload_protocol → Spike Surge (expiry burst + strong slow).
		trap.OverloadSpikeSurgeBurstDamage = int(float64(specific.OverloadSpikeSurgeBurstDamage)*mods.EffectMultiplier + 0.5)
		trap.OverloadSpikeSurgeSlowMult = amplifySlow(specific.OverloadSpikeSurgeSlowMult, mods.EffectMultiplier)
		trap.OverloadSpikeSurgeSlowDuration = specific.OverloadSpikeSurgeSlowDuration * mods.DurationMultiplier

	case "fire_pit":
		trap.Radius = cfg["radius"] * mods.RadiusMultiplier
		trap.DamagePerSecond = cfg["damagePerSecond"] * mods.EffectMultiplier
		// lasting_flames: set the burn duration so fire_pit switches into
		// "damage-as-debuff" mode. The burn's DPS derives from the pit's own
		// DamagePerSecond at tick time, so amplified_effects continues to
		// scale the burn strength without a separate path. Duration gets
		// DurationMultiplier so extended_setup extends the debuff window too.
		trap.LastingFlamesBurnDuration = specific.LastingFlamesBurnDuration * mods.DurationMultiplier
		// ascendant_infusion → Reactive Flames. Radius scales with
		// RadiusMultiplier so wider_nets enlarges the secondary explosion;
		// damage scales with EffectMultiplier.
		trap.InfusionReactiveFlamesRadius = specific.InfusionReactiveFlamesRadius * mods.RadiusMultiplier
		trap.InfusionReactiveFlamesDamage = int(float64(specific.InfusionReactiveFlamesDamage)*mods.EffectMultiplier + 0.5)
		// overload_protocol → Flame Collapse (expiry AoE + reapplied burn).
		if specific.OverloadFlameCollapseRadiusMult > 0 {
			trap.OverloadFlameCollapseRadius = trap.Radius * specific.OverloadFlameCollapseRadiusMult
			trap.OverloadFlameCollapseDamage = int(float64(specific.OverloadFlameCollapseDamage)*mods.EffectMultiplier + 0.5)
			trap.OverloadFlameCollapseBurnDPS = specific.OverloadFlameCollapseBurnDPS * mods.EffectMultiplier
			trap.OverloadFlameCollapseBurnSeconds = specific.OverloadFlameCollapseBurnSeconds * mods.DurationMultiplier
		}

	case "explosive_trap":
		// explosionRadius (AoE) → Radius; triggerRadius (inner) → TriggerRadius.
		// Cataclysm Blast multiplies Radius (the explosion zone), NOT TriggerRadius
		// (the trigger zone stays tuned independently so detonation timing is
		// preserved). Default multiplier = 1 when the perk is absent.
		cataclysmMult := 1.0
		if specific.OverloadCataclysmRadiusMult > 0 {
			cataclysmMult = specific.OverloadCataclysmRadiusMult
		}
		trap.Radius = cfg["explosionRadius"] * mods.RadiusMultiplier * cataclysmMult
		trap.TriggerRadius = cfg["triggerRadius"] * mods.RadiusMultiplier
		base := int(cfg["burstDamage"])
		trap.BurstDamage = int(float64(base)*mods.EffectMultiplier + 0.5)
		// Aftershock slot is single — if explosive_chain AND overload_protocol
		// are both owned, use the longer delay so both perks contribute (the
		// aftershock blast itself is already a large-radius explosion on the
		// same trap, so the stronger upgrade wins naturally).
		trap.AftershockDelaySeconds = math.Max(specific.AftershockDelaySeconds, specific.OverloadCataclysmDelaySeconds)
		trap.OverloadCataclysmDelaySeconds = specific.OverloadCataclysmDelaySeconds
		trap.OverloadCataclysmSpriteScale = specific.OverloadCataclysmSpriteScale
		// ascendant_infusion → Scatter Bomb. Snapshot child count, spawn
		// radius, and child lifetime — mini traps inherit base/silver damage
		// from this trap but NOT gold (IsScatterBombChild prevents recursion).
		trap.InfusionScatterBombCount = specific.InfusionScatterBombCount
		trap.InfusionScatterBombSpawnRadius = specific.InfusionScatterBombSpawnRadius * mods.RadiusMultiplier
		trap.InfusionScatterBombChildSeconds = specific.InfusionScatterBombChildSeconds * mods.DurationMultiplier

	case "marker_trap":
		trap.Radius = cfg["radius"] * mods.RadiusMultiplier
		// MarkMultiplier and MarkDuration both scale with effect strength.
		// DurationMultiplier is about trap-entity lifetime, not the post-effect
		// debuff — EffectMultiplier governs both the strength and the window of
		// the mark applied to enemies.
		trap.MarkMultiplier = cfg["markMultiplier"] * mods.EffectMultiplier
		trap.MarkDuration = cfg["markDuration"] * mods.EffectMultiplier
		// exposed_weakness: damage-reduction strength scales with EffectMultiplier
		// so amplified_effects makes the debuff harsher. Duration aligns with
		// MarkDuration (stamped together in tickTrapEffectsLocked).
		trap.ExposedWeakenedMultiplier = specific.ExposedWeakenedMultiplier * mods.EffectMultiplier
		// ascendant_infusion → Shared Pain (fraction is not scaled — it's a
		// direct percentage of incoming damage).
		trap.InfusionSharedPainFraction = specific.InfusionSharedPainFraction
		// overload_protocol → Final Exposure (burst + optional AoE on mark expiry).
		trap.OverloadFinalExposureDamage = int(float64(specific.OverloadFinalExposureDamage)*mods.EffectMultiplier + 0.5)
		trap.OverloadFinalExposureAoeRadius = specific.OverloadFinalExposureAoeRadius * mods.RadiusMultiplier
	}

	// Position the trap toward the nearest enemy at the archer's attack
	// range (see trapPlacementOffsetLocked for the "throw at target" semantics).
	// Falls back to the unit's exact position when no enemy can be found so
	// isolated-unit test scenarios stay deterministic.
	offsetX, offsetY := s.trapPlacementOffsetLocked(unit, trap.Radius)
	if isBonus {
		// Increased Deployment bonus: nudge the primary offset sideways by
		// bonusOffsetDistance so the bonus trap lands NEXT TO the primary
		// (which is now on the enemy) rather than off to the side of the
		// archer. Fall back to a purely horizontal offset when the primary
		// offset is (0, 0) — i.e. no enemy in range — so the bonus is still
		// visibly separated for isolation tests.
		//
		// For multiple bonuses, alternate perpendicular direction by index
		// (odd → +perp, even → -perp) and scale the stride so pairs beyond
		// the first land further out, preventing overlap.
		bonusDist := 50.0
		if bd := perkDefByID("increased_deployment"); bd != nil {
			if v, ok := bd.ConfigForRank(unit.Rank)["bonusOffsetDistance"]; ok {
				bonusDist = v
			}
		}
		sign := 1.0
		if bonusIndex%2 == 0 {
			sign = -1.0
		}
		stride := float64((bonusIndex + 1) / 2)
		signedDist := bonusDist * sign * stride
		if offsetX == 0 && offsetY == 0 {
			offsetX, offsetY = signedDist, 0
		} else {
			mag := math.Sqrt(offsetX*offsetX + offsetY*offsetY)
			// Perpendicular unit vector × signedDist, added to the primary
			// offset so each bonus trap is a short hop to the side of the
			// primary target rather than replacing the throw direction.
			perpX := -offsetY / mag * signedDist
			perpY := offsetX / mag * signedDist
			offsetX += perpX
			offsetY += perpY
		}
	}
	trap.X = unit.X + offsetX
	trap.Y = unit.Y + offsetY

	s.Traps = append(s.Traps, trap)
}

// trapPlacementThrowBuffer is how far past the unit's own AttackRange a
// candidate target may sit and still be considered "throwable at". Beyond
// reach+buffer, the helper returns (0, 0) so the trap drops at the unit's
// feet instead of being flung across the map toward a distant enemy.
const trapPlacementThrowBuffer = 150.0

// trapPlacementOffsetLocked returns the (dx, dy) offset to add to the unit's
// position so the trap is "thrown" toward the archer's current target and
// lands on the enemy rather than between the archer and the enemy. Direction
// is chosen from (in priority order):
//
//  1. The unit's current AttackTargetID — definitionally the enemy being
//     engaged this combat tick and usually populated whenever LastCombatSeconds > 0.
//  2. The nearest visible hostile unit — O(N) fallback for the rare case the
//     attack target has died or been cleared the same tick placement fires.
//
// Regardless of which path resolves a target, the target must sit within
// reach+trapPlacementThrowBuffer of the unit. Anything farther is treated as
// "no enemy in range" and the helper returns (0, 0) so the trap drops at the
// trapper's feet — prevents idle/out-of-combat trappers from flinging traps
// toward distant enemies visible across the map.
//
// Throw distance = min(distance to target, unit.AttackRange). This means:
//   - A target within the archer's firing range gets a trap dropped ON IT.
//   - A target who has just stepped out of range (but still within buffer)
//     gets a trap at the archer's max reach along the target direction.
//   - A target inside the archer's minimum reach still gets the trap at its
//     feet (dist < range → we place exactly on the target).
//
// `radius` is used only as the fallback throw distance when AttackRange is
// zero or negative (e.g. test fixtures / future melee trappers) — the old
// near-edge-touching behavior. Returns (0, 0) when no enemy is found or
// when the nearest enemy sits outside reach+trapPlacementThrowBuffer.
//
// Must be called under s.mu (read or write) lock.
func (s *GameState) trapPlacementOffsetLocked(unit *Unit, radius float64) (float64, float64) {
	if unit == nil {
		return 0, 0
	}

	var targetX, targetY float64
	haveTarget := false

	if unit.AttackTargetID != 0 {
		if target := s.unitsByID[unit.AttackTargetID]; target != nil &&
			target.HP > 0 && target.Visible && playersAreHostile(target.OwnerID, unit.OwnerID) {
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
			if !playersAreHostile(candidate.OwnerID, unit.OwnerID) {
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

	// Throw distance: ride the attack range so the trap lands on the enemy.
	// Fall back to the trap's own radius when AttackRange is missing so we
	// keep the "near-edge-touching" behavior for any unit type that lacks a
	// ranged stat (today only archer trappers exist, but this keeps the
	// helper safe to reuse).
	reach := unit.AttackRange
	if reach <= 0 {
		reach = radius
	}

	// Distance gate: if the chosen target sits beyond reach + buffer, treat
	// this as "no enemy in range" and drop at the unit's feet.
	if dist > reach+trapPlacementThrowBuffer {
		return 0, 0
	}

	throwDist := math.Min(dist, reach)

	return dx / dist * throwDist, dy / dist * throwDist
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
		if unit == nil || !playersAreHostile(unit.OwnerID, trap.OwnerPlayerID) {
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
		s.applyUnitDamageWithSourceLocked(unit, trap.BurstDamage, DamageSource{AttackerTrapID: trap.ID, Kind: "explosive_burst"})
		if ownerUnit != nil {
			s.recordDamageDealtLocked(ownerUnit, unit, trap.BurstDamage)
		}
		// Debug: explosive_trap detonation attribution. Scatter Bomb children
		// carry TrapType="explosive_trap" too, so their damage rolls up under
		// the same bucket as the parent — consistent with "it's still an
		// explosive trap" from a balance perspective.
		s.trackBattleDamageLocked(battleSourceFromTrap(trap), unit, trap.BurstDamage)
		if unit.HP <= 0 {
			if ownerUnit != nil {
				s.awardKillXPLocked(ownerUnit)
				s.payoutDamageDealtXPLocked(unit)
			}
			s.trackBattleKillLocked(battleSourceFromTrap(trap), unit)
			deadUnitIDs = append(deadUnitIDs, unit.ID)
		}
	}
	// ascendant_infusion → Scatter Bomb: on detonation, spawn mini explosive
	// traps around the explosion. Only fires on the INITIAL blast (not on the
	// aftershock or on mini children themselves) to bound total trap growth.
	if !trap.IsScatterBombChild &&
		!trap.AftershockPending &&
		trap.InfusionScatterBombCount > 0 {
		s.spawnScatterBombChildrenLocked(trap)
	}
	return deadUnitIDs
}

// fireReactiveFlamesLocked is the secondary-explosion primitive used by
// ascendant_infusion → Reactive Flames. It deals raw burst damage to visible
// hostile units within `radius` of (cx, cy) — NO burn application, NO further
// Reactive Flames triggering. This is the tagging strategy for recursion
// prevention: by routing through this helper (rather than reusing the fire_pit
// DoT or the explosive_trap detonate path), no caller can accidentally chain.
//
// ownerPlayerID filters friendlies; ownerUnit (may be nil) is used for XP and
// damage-dealt accounting. Returns the updated deadUnitIDs slice.
//
// Must be called under s.mu write lock.
func (s *GameState) fireReactiveFlamesLocked(cx, cy, radius float64, damage int, ownerPlayerID string, ownerUnit *Unit, trapID string, deadUnitIDs []int) []int {
	if radius <= 0 || damage <= 0 {
		return deadUnitIDs
	}
	radSq := radius * radius
	for _, u := range s.Units {
		if u == nil || !playersAreHostile(u.OwnerID, ownerPlayerID) {
			continue
		}
		if u.HP <= 0 || !u.Visible {
			continue
		}
		dx := u.X - cx
		dy := u.Y - cy
		if dx*dx+dy*dy > radSq {
			continue
		}
		s.applyUnitDamageWithSourceLocked(u, damage, DamageSource{AttackerTrapID: trapID, Kind: "trap_silver_tick"})
		if ownerUnit != nil && ownerUnit.HP > 0 {
			s.recordDamageDealtLocked(ownerUnit, u, damage)
		}
		// Debug: Reactive Flames attributes under the fire_pit bucket —
		// consistent with how other Infusion effects roll up under their
		// Bronze trap type.
		if ownerPlayerID != "" {
			s.trackBattleDamageLocked(
				BattleSource{PlayerID: ownerPlayerID, Kind: "trap", Subtype: "fire_pit"},
				u, damage,
			)
		}
		if u.HP <= 0 {
			if ownerUnit != nil && ownerUnit.HP > 0 {
				s.awardKillXPLocked(ownerUnit)
				s.payoutDamageDealtXPLocked(u)
			}
			if ownerPlayerID != "" {
				s.trackBattleKillLocked(
					BattleSource{PlayerID: ownerPlayerID, Kind: "trap", Subtype: "fire_pit"},
					u,
				)
			}
			deadUnitIDs = append(deadUnitIDs, u.ID)
		}
	}
	return deadUnitIDs
}

// fireFinalExposureLocked executes overload_protocol → Final Exposure when a
// marker_trap-stamped mark expires on `victim`. Deals burst damage to the
// victim (direct) plus an AoE hit to nearby visible enemies of the same owner
// (so the effect still punishes an entire marked group).
//
// Called from state.go's mark-expiry branch. The victim's FinalExposure* fields
// are consumed (cleared to 0) by the caller after this returns.
//
// Must be called under s.mu write lock.
func (s *GameState) fireFinalExposureLocked(victim *Unit) {
	if victim == nil || victim.HP <= 0 {
		return
	}
	damage := victim.PerkState.FinalExposureDamage
	radius := victim.PerkState.FinalExposureAoeRadius
	owner := s.unitsByID[victim.PerkState.FinalExposureOwnerUnitID]
	if owner != nil && owner.HP <= 0 {
		owner = nil
	}

	// Debug: Final Exposure fires from a marker_trap Overload Protocol effect —
	// attribute to the owning player's marker_trap bucket. Requires a living
	// owner so we have the PlayerID.
	trapID := victim.PerkState.FinalExposureTrapID
	var finalExposureSrc BattleSource
	var finalExposureDmgSrc DamageSource
	if owner != nil {
		finalExposureSrc = BattleSource{PlayerID: owner.OwnerID, Kind: "trap", Subtype: "marker_trap"}
		finalExposureDmgSrc = DamageSource{AttackerTrapID: trapID, Kind: "final_exposure"}
	}

	var dead []int
	// Direct hit on the victim whose mark just expired.
	s.applyUnitDamageWithSourceLocked(victim, damage, finalExposureDmgSrc)
	if owner != nil {
		s.recordDamageDealtLocked(owner, victim, damage)
	}
	s.trackBattleDamageLocked(finalExposureSrc, victim, damage)
	if victim.HP <= 0 {
		if owner != nil {
			s.awardKillXPLocked(owner)
			s.payoutDamageDealtXPLocked(victim)
		}
		s.trackBattleKillLocked(finalExposureSrc, victim)
		dead = append(dead, victim.ID)
	}
	// Optional AoE to nearby enemies of the victim's owner (the trap's
	// original targets). Skip if radius is zero (AoE disabled by tuning).
	if radius > 0 {
		radSq := radius * radius
		for _, u := range s.Units {
			if u == nil || u.ID == victim.ID {
				continue
			}
			if u.OwnerID != victim.OwnerID {
				continue // same-team-as-victim only; other teams aren't this trapper's concern
			}
			if u.HP <= 0 || !u.Visible {
				continue
			}
			dx := u.X - victim.X
			dy := u.Y - victim.Y
			if dx*dx+dy*dy > radSq {
				continue
			}
			s.applyUnitDamageWithSourceLocked(u, damage, DamageSource{AttackerTrapID: trapID, Kind: "final_exposure_aoe"})
			if owner != nil {
				s.recordDamageDealtLocked(owner, u, damage)
			}
			s.trackBattleDamageLocked(finalExposureSrc, u, damage)
			if u.HP <= 0 {
				if owner != nil {
					s.awardKillXPLocked(owner)
					s.payoutDamageDealtXPLocked(u)
				}
				s.trackBattleKillLocked(finalExposureSrc, u)
				dead = append(dead, u.ID)
			}
		}
	}
	for _, id := range dead {
		s.removeUnitLocked(id)
	}
}

// perkShareDamageToMarkedLocked redistributes a fraction of the source unit's
// incoming damage to every other marked enemy that is participating in Shared
// Pain (ascendant_infusion → marker_trap). Called from applyUnitDamageLocked.
//
// RECURSION GUARD: each sub-target is flagged SharedPainActive=true before the
// damage call and cleared after. Any nested applyUnitDamageLocked that re-enters
// this function for that sub-target short-circuits on the SharedPainActive check.
// Pattern mirrors pain_share (Vanguard).
//
// Scope: shared damage goes to all units with MarkedRemaining > 0 AND
// SharedPainFraction > 0. This includes enemies of different players so long
// as they carry an infusion-armed mark, which is the intuitive semantics —
// "all marked enemies share the pain" regardless of which trapper marked them.
//
// Must be called under s.mu write lock.
func (s *GameState) perkShareDamageToMarkedLocked(source *Unit, rawDamage int, src DamageSource) {
	if source == nil || rawDamage <= 0 {
		return
	}
	if source.PerkState.SharedPainActive {
		return
	}
	frac := source.PerkState.SharedPainFraction
	if frac <= 0 || !source.PerkState.anyMarkActive() {
		return
	}
	shared := int(math.Round(float64(rawDamage) * frac))
	if shared <= 0 {
		return
	}
	// Kill credit for Shared Pain victims goes to the original attacker — the
	// unit being damaged is not the killer, it's the conduit. Preserve attacker
	// IDs from src and override Kind for telemetry.
	sharedSrc := DamageSource{
		AttackerUnitID:     src.AttackerUnitID,
		AttackerBuildingID: src.AttackerBuildingID,
		AttackerTrapID:     src.AttackerTrapID,
		Kind:               "shared_pain",
	}
	for _, u := range s.Units {
		if u == nil || u.ID == source.ID {
			continue
		}
		if u.HP <= 0 || !u.Visible {
			continue
		}
		if u.PerkState.SharedPainActive {
			continue // already in the chain this tick
		}
		if !u.PerkState.anyMarkActive() || u.PerkState.SharedPainFraction <= 0 {
			continue
		}
		u.PerkState.SharedPainActive = true
		s.applyUnitDamageWithSourceLocked(u, shared, sharedSrc)
		u.PerkState.SharedPainActive = false
	}
}

// fireTrapExpiryEffectsLocked runs the overload_protocol expiry effect for a
// trap that is about to be removed due to RemainingSeconds hitting 0. The
// effect is adaptive per trap type:
//
//   - caltrops  → Spike Surge: burst damage + strong slow to all enemies still
//                 inside the caltrops radius.
//   - fire_pit  → Flame Collapse: AoE explosion at the center + reapplied burn
//                 to all affected enemies (integrates with existing burn DoT).
//
// No effect for explosive_trap (Cataclysm Blast uses the aftershock plumbing
// which fires on detonation, not expiry) or marker_trap (Final Exposure fires
// on mark-expiry per victim, handled in state.go).
//
// Must be called under s.mu write lock.
func (s *GameState) fireTrapExpiryEffectsLocked(trap *Trap) {
	if trap == nil {
		return
	}
	ownerUnit := s.unitsByID[trap.OwnerUnitID]
	if ownerUnit != nil && ownerUnit.HP <= 0 {
		ownerUnit = nil
	}

	switch trap.TrapType {
	case "caltrops":
		if trap.OverloadSpikeSurgeBurstDamage <= 0 {
			return
		}
		var dead []int
		radSq := trap.Radius * trap.Radius
		for _, u := range s.Units {
			if u == nil || !playersAreHostile(u.OwnerID, trap.OwnerPlayerID) {
				continue
			}
			if u.HP <= 0 || !u.Visible {
				continue
			}
			dx := u.X - trap.X
			dy := u.Y - trap.Y
			if dx*dx+dy*dy > radSq {
				continue
			}
			s.applyUnitDamageWithSourceLocked(u, trap.OverloadSpikeSurgeBurstDamage, DamageSource{AttackerTrapID: trap.ID, Kind: "overload_spike_surge"})
			if ownerUnit != nil {
				s.recordDamageDealtLocked(ownerUnit, u, trap.OverloadSpikeSurgeBurstDamage)
			}
			// Debug: Spike Surge rolls up under caltrops (consistent with the
			// Bronze-trap bucket — Overload Protocol is a Gold upgrade to it).
			s.trackBattleDamageLocked(battleSourceFromTrap(trap), u, trap.OverloadSpikeSurgeBurstDamage)
			if trap.OverloadSpikeSurgeSlowMult > 0 && trap.OverloadSpikeSurgeSlowMult < 1.0 {
				s.ApplySlowLocked(u.ID, trap.OverloadSpikeSurgeSlowMult, trap.OverloadSpikeSurgeSlowDuration)
			}
			if u.HP <= 0 {
				if ownerUnit != nil {
					s.awardKillXPLocked(ownerUnit)
					s.payoutDamageDealtXPLocked(u)
				}
				s.trackBattleKillLocked(battleSourceFromTrap(trap), u)
				dead = append(dead, u.ID)
			}
		}
		for _, id := range dead {
			s.removeUnitLocked(id)
		}

	case "fire_pit":
		if trap.OverloadFlameCollapseRadius <= 0 || trap.OverloadFlameCollapseDamage <= 0 {
			return
		}
		var dead []int
		radSq := trap.OverloadFlameCollapseRadius * trap.OverloadFlameCollapseRadius
		for _, u := range s.Units {
			if u == nil || !playersAreHostile(u.OwnerID, trap.OwnerPlayerID) {
				continue
			}
			if u.HP <= 0 || !u.Visible {
				continue
			}
			dx := u.X - trap.X
			dy := u.Y - trap.Y
			if dx*dx+dy*dy > radSq {
				continue
			}
			s.applyUnitDamageWithSourceLocked(u, trap.OverloadFlameCollapseDamage, DamageSource{AttackerTrapID: trap.ID, Kind: "overload_flame_collapse"})
			if ownerUnit != nil {
				s.recordDamageDealtLocked(ownerUnit, u, trap.OverloadFlameCollapseDamage)
			}
			// Debug: Flame Collapse rolls up under fire_pit.
			s.trackBattleDamageLocked(battleSourceFromTrap(trap), u, trap.OverloadFlameCollapseDamage)
			// Reapply burn via the stack helper so Flame Collapse integrates
			// with the same per-source stacking rules as lasting_flames.
			// Keyed by trap.ID so two overlapping Flame Collapse blasts
			// (e.g. primary + increased_deployment bonus) land separate
			// stacks. Reactive Flames is NOT piggy-backed here — Flame
			// Collapse is an Overload effect, not an Infusion effect.
			if trap.OverloadFlameCollapseBurnDPS > 0 && trap.OverloadFlameCollapseBurnSeconds > 0 {
				u.PerkState.applyBurnStack(
					trap.ID,
					trap.OwnerUnitID,
					trap.OverloadFlameCollapseBurnDPS,
					trap.OverloadFlameCollapseBurnSeconds,
					0, 0,
				)
			}
			if u.HP <= 0 {
				if ownerUnit != nil {
					s.awardKillXPLocked(ownerUnit)
					s.payoutDamageDealtXPLocked(u)
				}
				s.trackBattleKillLocked(battleSourceFromTrap(trap), u)
				dead = append(dead, u.ID)
			}
		}
		for _, id := range dead {
			s.removeUnitLocked(id)
		}
	}
}

// spawnScatterBombChildrenLocked constructs N mini explosive_trap children at
// a ring around the parent trap's position. Children inherit base damage and
// the (silver) aftershock delay from the parent, but NOT any gold-tier fields
// — the IsScatterBombChild flag prevents further recursion.
//
// Called from detonateExplosiveTrapLocked on the initial blast only.
//
// Must be called under s.mu write lock.
func (s *GameState) spawnScatterBombChildrenLocked(parent *Trap) {
	count := parent.InfusionScatterBombCount
	if count <= 0 {
		return
	}
	spawnR := parent.InfusionScatterBombSpawnRadius
	if spawnR <= 0 {
		spawnR = parent.Radius
	}
	childDuration := parent.InfusionScatterBombChildSeconds
	if childDuration <= 0 {
		childDuration = 4
	}
	// Evenly space N children around the ring with a random phase offset so
	// multiple Scatter Bombs in the same match don't land in the same pattern.
	phase := s.rngPerks.Float64() * 2 * math.Pi
	for i := 0; i < count; i++ {
		angle := phase + 2*math.Pi*float64(i)/float64(count)
		childX := parent.X + math.Cos(angle)*spawnR
		childY := parent.Y + math.Sin(angle)*spawnR
		id := s.nextTrapID
		s.nextTrapID++
		child := &Trap{
			ID:                 trapIDString(id),
			OwnerUnitID:        parent.OwnerUnitID,
			OwnerPlayerID:      parent.OwnerPlayerID,
			X:                  childX,
			Y:                  childY,
			Radius:             parent.Radius,
			TriggerRadius:      parent.TriggerRadius,
			RemainingSeconds:   childDuration,
			TrapType:           "explosive_trap",
			BurstDamage:        parent.BurstDamage,
			AftershockDelaySeconds: parent.AftershockDelaySeconds,
			IsScatterBombChild: true,
		}
		s.Traps = append(s.Traps, child)
	}
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

// trapVisualVariant returns the visual-variant tag sent to the client for
// this trap, or "" when the default animation should be used. Variants let
// the client swap to a perk-specific animation (e.g. electrified caltrops
// under ascendant_infusion) without hard-coding trap-perk knowledge into
// the renderer — the server says which variant applies, the client maps
// that tag to a named animation in the object's sprites.json.
//
// Extend the switch as more trap/perk variants gain dedicated visuals.
func trapVisualVariant(trap *Trap) string {
	if trap == nil {
		return ""
	}
	switch trap.TrapType {
	case "caltrops":
		if trap.InfusionElectrifiedBonusDamage > 0 {
			return "electrified"
		}
	}
	return ""
}

// trapVisualScaleMultiplier returns an extra render-scale factor the client
// applies on top of the object's base sprite scale. Use for perks that
// visually inflate a trap beyond its normal footprint. Returns 0 when no
// multiplier applies (client treats 0/absent as 1×).
//
// Extend the switch as more perks need dedicated scale bumps.
func trapVisualScaleMultiplier(trap *Trap) float64 {
	if trap == nil {
		return 0
	}
	switch trap.TrapType {
	case "explosive_trap":
		// overload_protocol → Cataclysm. Value is authored in gold.json
		// (cataclysmSpriteScale) and snapshotted onto the trap at plant time.
		return trap.OverloadCataclysmSpriteScale
	}
	return 0
}

// tickTrapPlacementLocked is the per-tick auto-placement driver for Trapper
// perks. It decays TrapPlaceCooldownRemaining and plants a new trap whenever
// the cooldown reaches 0 on a living unit.
//
// Placement policy: traps drop as often as their cooldown allows. If the
// Trapper is actively attacking, placement still runs — the trap is
// prioritized over "just another shot", which keeps zone control flowing
// even during sustained engagements. Friendly units are never hit by
// traps (trap.go's damage paths all filter on OwnerID), so there's no
// risk of self-inflicted damage mid-fight.
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

	// Cooldown still running: wait.
	if unit.PerkState.TrapPlaceCooldownRemaining > 0 {
		return
	}

	// Plant the trap and reset the cooldown.
	s.plantTrapLocked(unit, def)
	mods := s.trapModifiersForUnitLocked(unit)
	cfg := def.ConfigForRank(unit.Rank)
	unit.PerkState.TrapPlaceCooldownRemaining = cfg["placeIntervalSeconds"] * mods.CooldownMultiplier
}
