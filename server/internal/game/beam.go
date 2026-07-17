package game

import (
	"fmt"

	"webrts/server/pkg/protocol"
)

// Beam is a beam visual entity the client renders as a line between two points.
// There are two flavors, distinguished by Momentary:
//
//   - Channeled (Momentary == false): exists for the duration of a unit's
//     channel and carries NO simulation state — all damage, mana, and stop
//     logic is driven by the Unit's Channel* fields. Its endpoints are the
//     LIVE caster/target positions (resolved by the client each frame), so it
//     is removed when the channel stops or when either participant leaves the
//     game (removeBeamForUnitLocked / removeBeamForTargetLocked).
//
//   - Momentary (Momentary == true): a one-shot "zap" fired by an emitter of
//     EmitterKindBeam (e.g. an item on-hit proc). Its damage is applied at
//     spawn time by the firing site; the Beam is purely a short-lived visual
//     that decays over RemainingSeconds. Its endpoints are FROZEN at fire time
//     (OriginX/Y → TargetX/Y) so the flash still renders even if the target
//     dies from the same hit or moves during the flash — it is NOT removed by
//     participant-removal paths.
//
// ID-not-pointer rule: CasterUnitID and TargetUnitID are integer IDs.
type Beam struct {
	// ID is the stable wire identifier for this beam (e.g. "beam-0").
	ID string
	// CasterUnitID is the ID of the unit channeling the ability (or the VISUAL
	// origin of a momentary proc beam — the attacker for the primary hit, or
	// the previous victim on a bounce hop). Drives the client's origin-lift
	// sprite lookup; not used for damage credit (see AttackerUnitID).
	CasterUnitID int
	// TargetUnitID is the ID of the enemy unit being drained (or hit, for a
	// momentary proc beam).
	TargetUnitID int
	// OwnerPlayerID is the player who owns the caster (for FOW filtering).
	OwnerPlayerID string
	// AbilityID is the ability driving this beam (e.g. "siphon_life"). Empty
	// for momentary proc beams (they aren't tied to an ability).
	AbilityID string
	// Variant is the client-side renderer variant (e.g. "siphon_life",
	// "lightning_bolt") — for momentary beams this is the emitter def id, which
	// selects assets/beams/<variant>/.
	Variant string

	// ── Momentary (one-shot proc zap) fields — all zero for channel beams ────
	// Momentary marks a self-contained, short-lived beam flash whose endpoints
	// are frozen and whose lifetime is RemainingSeconds. Channel beams leave
	// this false and use live participant positions instead.
	Momentary bool
	// RemainingSeconds counts a momentary beam down to removal (see
	// tickBeamsLocked). Unused (0) for channel beams.
	RemainingSeconds float64
	// OriginX/Y and TargetX/Y are the frozen world endpoints of a momentary
	// beam, snapshot from the attacker/target positions at fire time. Unused
	// for channel beams, whose endpoints are the live unit positions.
	OriginX, OriginY float64
	TargetX, TargetY float64

	// ── Deferred proc damage — momentary beams only ─────────────────────────
	// A beam is instantaneous, so its damage would otherwise land on the SAME
	// tick as the triggering hit and merge into that hit's floating number. To
	// read as its own number, the damage is deferred by DamageDelayRemaining:
	// tickBeamsLocked applies PendingDamage to TargetUnitID once the delay
	// elapses (a beat after the flash appears), then zeroes PendingDamage so it
	// lands exactly once. CasterUnitID is the attacker for attribution.
	PendingDamage        int
	DamageType           DamageType
	DamageDelayRemaining float64
	// ImpactEffect is the effect id played on the target when the deferred
	// damage lands (e.g. "fizzle"), mirroring a projectile's on-land impact.
	ImpactEffect string
	// AttackerUnitID credits the deferred damage's kill/XP. Distinct from
	// CasterUnitID because a bounce hop's beam VISUALLY leaves the previous
	// victim, but the kill must still credit the original wielder. Defaults to
	// CasterUnitID for the primary hit (attacker == visual origin).
	AttackerUnitID int
	// SourceAbilityID carries ProcSource.SourceAbilityID through to the
	// deferred damage's DamageSource (applyBeamPendingDamageLocked) so a
	// chain-bounce kill fired from an authored ability (chain_lightning-shape,
	// via fireAbilityChainLocked) attributes to that ability for on_unit_death
	// purposes. "" for every non-ability proc beam (equipment/item/perk),
	// matching ProcSource.SourceAbilityID's zero-value contract. Distinct from
	// AbilityID above, which is the CHANNEL-beam field (siphon_life) and is
	// never set on a momentary beam.
	SourceAbilityID string
	// SlowMultiplier / SlowDurationSeconds: an on-hit chill carried from the
	// proc config, applied to TargetUnitID when the deferred damage lands (via
	// ApplySlowLocked). Zero ⇒ no slow.
	SlowMultiplier      float64
	SlowDurationSeconds float64
	// BurnDamagePerSecond / BurnDurationSeconds: an on-hit burn carried from the
	// proc config, igniting TargetUnitID when the deferred damage lands. Credit
	// goes to AttackerUnitID (the original wielder). Zero ⇒ no burn.
	BurnDamagePerSecond float64
	BurnDurationSeconds float64
}

// spawnBeamLocked creates a new Beam entity, appends it to s.Beams, and
// returns it. Called by beginAbilityChannelLocked when a channel starts.
//
// Caller holds s.mu write lock.
func (s *GameState) spawnBeamLocked(caster *Unit, target *Unit, abilityID, variant string) *Beam {
	b := &Beam{
		ID:            fmt.Sprintf("beam-%d", s.nextBeamID),
		CasterUnitID:  caster.ID,
		TargetUnitID:  target.ID,
		OwnerPlayerID: caster.OwnerID,
		AbilityID:     abilityID,
		Variant:       variant,
	}
	s.nextBeamID++
	s.Beams = append(s.Beams, b)
	return b
}

// spawnMomentaryBeamLocked creates a self-contained one-shot beam flash from
// the attacker to the target, used by EmitterKindBeam procs. Endpoints are
// frozen at the current unit positions and the beam decays over durationMs.
// It carries NO damage — the caller applies damage separately (a beam is
// instantaneous, so damage lands at fire time, not on the visual's removal).
//
// Caller holds s.mu write lock.
func (s *GameState) spawnMomentaryBeamLocked(attacker, target *Unit, variant string, durationMs int) *Beam {
	if durationMs <= 0 {
		durationMs = defaultBeamDurationMs
	}
	b := &Beam{
		ID:               fmt.Sprintf("beam-%d", s.nextBeamID),
		CasterUnitID:     attacker.ID,
		AttackerUnitID:   attacker.ID,
		TargetUnitID:     target.ID,
		OwnerPlayerID:    attacker.OwnerID,
		Variant:          variant,
		Momentary:        true,
		RemainingSeconds: float64(durationMs) / 1000.0,
		OriginX:          attacker.X,
		OriginY:          attacker.Y,
		TargetX:          target.X,
		TargetY:          target.Y,
	}
	s.nextBeamID++
	s.Beams = append(s.Beams, b)
	return b
}

// spawnMomentaryDamageBeamLocked spawns a one-shot beam flash from a frozen
// origin point to `to` and schedules `damage` (typed) to land on `to` after
// `delaySec`, credited to src.OwnerUnitID. fromUnitID is the VISUAL origin
// unit (drives the client's origin-lift sprite lookup; 0 when the beam leaves
// a non-unit source) and fromX/Y freeze the beam's start — the visual origin
// and the kill credit differ on a bounce hop, where the beam leaps off a
// victim but the original source still gets the kill.
//
// Caller holds s.mu write lock.
func (s *GameState) spawnMomentaryDamageBeamLocked(src ProcSource, fromUnitID int, fromX, fromY float64, to *Unit, variant string, damage int, dmgType DamageType, impactEffect string, durationMs int, delaySec float64) *Beam {
	if durationMs <= 0 {
		durationMs = defaultBeamDurationMs
	}
	b := &Beam{
		ID:                   fmt.Sprintf("beam-%d", s.nextBeamID),
		CasterUnitID:         fromUnitID,
		AttackerUnitID:       src.OwnerUnitID,
		TargetUnitID:         to.ID,
		OwnerPlayerID:        src.OwnerPlayerID,
		Variant:              variant,
		Momentary:            true,
		RemainingSeconds:     float64(durationMs) / 1000.0,
		OriginX:              fromX,
		OriginY:              fromY,
		TargetX:              to.X,
		TargetY:              to.Y,
		PendingDamage:        damage,
		DamageType:           dmgType,
		DamageDelayRemaining: delaySec,
		ImpactEffect:         impactEffect,
		SourceAbilityID:      src.SourceAbilityID,
	}
	s.nextBeamID++
	s.Beams = append(s.Beams, b)
	return b
}

// tickBeamsLocked advances momentary beams: it lands their deferred proc damage
// once the delay elapses and removes the flashes that have expired. Channel
// beams (Momentary == false) are untouched — their lifetime is owned by the
// channel state machine, not a timer. No RNG, no cross-tick pointers: keeps
// simulation determinism.
//
// Caller holds s.mu write lock.
func (s *GameState) tickBeamsLocked(dt float64) {
	if len(s.Beams) == 0 {
		return
	}
	var deadUnitIDs []int
	kept := s.Beams[:0]
	for _, b := range s.Beams {
		if b.Momentary {
			// Deferred proc damage lands a beat AFTER the triggering hit so it
			// reads as its own damage number. Apply exactly once when the delay
			// elapses (applyBeamPendingDamageLocked zeroes PendingDamage).
			if b.PendingDamage > 0 {
				b.DamageDelayRemaining -= dt
				if b.DamageDelayRemaining <= 0 {
					s.applyBeamPendingDamageLocked(b, &deadUnitIDs)
				}
			}
			b.RemainingSeconds -= dt
			if b.RemainingSeconds <= 0 {
				// Safety net: if the flash somehow expired before the delay
				// elapsed (delay >= duration), still land the damage so a
				// rolled proc is never silently dropped.
				if b.PendingDamage > 0 {
					s.applyBeamPendingDamageLocked(b, &deadUnitIDs)
				}
				continue // flash finished — drop
			}
		}
		kept = append(kept, b)
	}
	s.Beams = kept
	// Remove anything the deferred damage just killed, mirroring
	// tickProjectilesLocked. Momentary beams are skipped by the removal paths,
	// so a beam that just killed its own target keeps flashing.
	for _, id := range deadUnitIDs {
		s.removeUnitLocked(id)
	}
}

// applyBeamPendingDamageLocked lands a momentary beam's deferred proc damage on
// its target, then clears PendingDamage so it can never apply twice. Bypasses
// the on-hit hub (direct HP pipeline, like a SkipOnHitEffects proc bolt) so a
// proc can't trigger another proc. If the target is already gone/dead the zap
// fizzles harmlessly — same "lost the target" semantics a projectile has.
//
// Caller holds s.mu write lock.
func (s *GameState) applyBeamPendingDamageLocked(b *Beam, deadUnitIDs *[]int) {
	damage := b.PendingDamage
	b.PendingDamage = 0 // land exactly once, even across the safety-net path
	if damage <= 0 {
		return
	}
	target := s.getUnitByIDLocked(b.TargetUnitID)
	if target == nil || target.HP <= 0 || !target.Visible {
		return
	}
	s.applyUnitDamageWithSourceLocked(target, damage, DamageSource{
		AttackerUnitID:  b.AttackerUnitID,
		Kind:            "item-proc",
		DamageType:      b.DamageType,
		SourceAbilityID: b.SourceAbilityID,
	})
	// On-hit slow: routed to the cold (chill) or physical track by the beam's
	// damage type. No-op on zero / out-of-range values.
	s.applyProcSlowLocked(target.ID, b.SlowMultiplier, b.SlowDurationSeconds, b.DamageType)
	// On-hit burn: ignite the target with a fire DoT, credited to the original
	// wielder (AttackerUnitID). No-op when the proc carries no burn.
	s.applyProcBurnLocked(target.ID, b.BurnDamagePerSecond, b.BurnDurationSeconds, b.AttackerUnitID)
	if b.ImpactEffect != "" {
		s.playEffectOnUnitLocked(target, b.ImpactEffect)
	}
	if target.HP <= 0 {
		target.HP = 0
		// Ability-attributed beam kills (chain_lightning-shape, primary or
		// bounce hop) defer removal + kill-XP/on_unit_death to the
		// attributed pending-death drain that applyUnitDamageWithSourceLocked
		// already enqueued — mirrors landProjectileLocked's
		// proj.SourceKind=="ability" carve-out (projectile.go): appending to
		// deadUnitIDs here would strip the unit via tickBeamsLocked's
		// immediate removeUnitLocked BEFORE drainPendingDeathsLocked runs
		// later this same tick, which is exactly the "already removed by the
		// primary call site" skip path — silently discarding XP/kill
		// bookkeeping AND making an authored ability's on_unit_death
		// unreachable for every beam-delivered kill. Non-ability proc beams
		// (equipment/item procs — b.SourceAbilityID == "") keep their legacy
		// immediate removal; they never routed through the drain's XP path
		// to begin with, so this is unchanged for them.
		if b.SourceAbilityID == "" {
			*deadUnitIDs = append(*deadUnitIDs, target.ID)
		}
	}
}

// removeBeamForUnitLocked drops any CHANNEL beam whose CasterUnitID == unitID.
// Called from stopUnitChannelLocked and clearUnitChannelLocked, and also
// from removeUnitLocked so a dying caster's beam doesn't linger. Momentary
// beams are skipped: they carry frozen endpoints and must complete their brief
// flash even if the caster is removed the same tick.
//
// Caller holds s.mu write lock.
func (s *GameState) removeBeamForUnitLocked(unitID int) {
	if len(s.Beams) == 0 {
		return
	}
	kept := s.Beams[:0]
	for _, b := range s.Beams {
		if !b.Momentary && b.CasterUnitID == unitID {
			continue // drop
		}
		kept = append(kept, b)
	}
	s.Beams = kept
}

// removeBeamByIDLocked drops the beam with the given stable wire ID. No-op
// when no beam matches (the caller may be cleaning up state that was already
// removed via a different path — e.g. removeBeamForTargetLocked firing on a
// dead chain victim before chain_siphon's per-tick sync runs).
//
// Caller holds s.mu write lock.
func (s *GameState) removeBeamByIDLocked(id string) {
	if len(s.Beams) == 0 || id == "" {
		return
	}
	kept := s.Beams[:0]
	for _, b := range s.Beams {
		if b.ID == id {
			continue // drop
		}
		kept = append(kept, b)
	}
	s.Beams = kept
}

// removeBeamForTargetLocked drops any CHANNEL beam whose TargetUnitID ==
// targetID. Called from removeUnitLocked so a beam whose target died is
// dropped immediately. The channel tick also catches this on the next tick,
// but removing the beam here keeps the visual state clean during the same tick
// the target dies. Momentary beams are skipped: a proc zap that KILLS its
// target must still flash, so it lives on its own timer regardless of the
// target's removal.
//
// Caller holds s.mu write lock.
func (s *GameState) removeBeamForTargetLocked(targetID int) {
	if len(s.Beams) == 0 {
		return
	}
	kept := s.Beams[:0]
	for _, b := range s.Beams {
		if !b.Momentary && b.TargetUnitID == targetID {
			continue // drop
		}
		kept = append(kept, b)
	}
	s.Beams = kept
}

// beamSnapshotsLocked builds the wire-format beam slice for a snapshot.
// When fow is nil (spectator / unfiltered Snapshot()), all beams are
// included. When fow is non-nil (SnapshotForPlayer), a beam is included only
// when the caster OR the target is visible to the viewer — matching the
// pattern projectiles use.
//
// Caller holds s.mu (read lock is sufficient).
func (s *GameState) beamSnapshotsLocked(fow *PlayerFOW) []protocol.BeamSnapshot {
	if len(s.Beams) == 0 {
		return nil
	}
	var out []protocol.BeamSnapshot
	for _, b := range s.Beams {
		if fow != nil {
			// FOW filter: include the beam when either endpoint is visible.
			// Momentary beams carry frozen coords (their participants may have
			// died) so they filter on those; channel beams resolve the live
			// caster/target positions.
			var visible bool
			if b.Momentary {
				visible = fow.isClearAtWorld(b.OriginX, b.OriginY, s.MapConfig.CellSize) ||
					fow.isClearAtWorld(b.TargetX, b.TargetY, s.MapConfig.CellSize)
			} else {
				if caster := s.getUnitByIDLocked(b.CasterUnitID); caster != nil {
					visible = fow.isClearAtWorld(caster.X, caster.Y, s.MapConfig.CellSize)
				}
				if !visible {
					if target := s.getUnitByIDLocked(b.TargetUnitID); target != nil {
						visible = fow.isClearAtWorld(target.X, target.Y, s.MapConfig.CellSize)
					}
				}
			}
			if !visible {
				continue
			}
		}
		snap := protocol.BeamSnapshot{
			ID:           b.ID,
			CasterUnitId: b.CasterUnitID,
			TargetUnitId: b.TargetUnitID,
			OwnerId:      b.OwnerPlayerID,
			AbilityId:    b.AbilityID,
			Variant:      b.Variant,
		}
		// Momentary beams send their frozen endpoints so the client renders the
		// flash from coords instead of live unit positions (which may be gone).
		if b.Momentary {
			snap.Momentary = true
			snap.OriginX = b.OriginX
			snap.OriginY = b.OriginY
			snap.TargetX = b.TargetX
			snap.TargetY = b.TargetY
		}
		out = append(out, snap)
	}
	return out
}
