package game

import (
	"log/slog"
	"strconv"

	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// ABILITY STATUS SYSTEM
//
// AbilityStatus is the first-class runtime object behind an AUTHORED
// apply_status action — one whose config carries Triggers (on_status_tick /
// on_status_expire), as opposed to the three LEGACY CC primitives
// (slow/stun/burn) that apply_status's Execute keeps routing to their
// existing seams (applyProcSlowLocked / ApplyStunLocked / applyAbilityBurnLocked)
// completely unchanged — see applyStatusConfig's doc comment
// (ability_exec_actions.go) for the discriminator and the parity argument.
//
// Modeled directly on AbilityZone (ability_zone.go), which is the structural
// precedent this file follows field-for-field: {ID, AbilityID, CasterID,
// TargetUnitID, Remaining, TickInterval, tickTimer, Triggers}, held on
// GameState.AbilityStatuses, ticked by tickAbilityStatusesLocked, firing
// through the SAME executor (runProgramTriggersLocked) every other trigger
// uses — this file never hand-rolls damage/healing/etc, it only supplies the
// per-tick RuntimeAbilityContext (CurrentEventUnitID = the afflicted unit)
// that lets a nested select_targets{source:"current_event"} + deal_damage/
// etc. resolve exactly like any other program.
//
// Target stored by ID (AI_RULES): TargetUnitID, never *Unit. Re-resolved via
// getUnitByIDLocked every tick and validated (nil / HP<=0) before use, exactly
// like the channel state's target-validation discipline.
//
// EXPIRY SEMANTICS (the design call this file makes): on_status_expire fires
// EXACTLY ONCE per AbilityStatus — either on natural Remaining timeout, or the
// instant its target becomes invalid (removed or HP<=0), whichever comes
// first. This mirrors tickAbilityZonesLocked's occupancy-based on_zone_exit-
// on-death precedent (a unit that dies while inside a zone still gets a
// paired exit) rather than leaving on_status_expire an "only fires if you
// outlast your target" event: an author reacting to on_status_tick almost
// always wants a matching on_status_expire cleanup (remove a visual marker,
// restore a stat, etc.) regardless of WHY the status ended, and an unpaired
// tick/expire is exactly the trap on_cast_start's own doc comment warns
// against. See tickAbilityStatusesLocked for where each of the three
// termination paths (found-dead-at-tick-start, dies-mid-tick, natural
// timeout) fires it.
// ═════════════════════════════════════════════════════════════════════════════

// statusTickEpsilon absorbs float64 accumulation error the same way
// zoneTickEpsilon does for AbilityZone (see that constant's doc comment) —
// both the per-tick cadence (tickTimer) and the expiry countdown (Remaining)
// compare against it instead of a bare "<= 0".
const statusTickEpsilon = 1e-9

// AbilityStatus is the composable, tick-driven buff/debuff object spawned by
// an authored apply_status action. Server-only, never serialized.
type AbilityStatus struct {
	ID        string
	AbilityID string
	// Name disambiguates multiple distinctly-named statuses authored by the
	// SAME ability (StatusDef.Name — ability_program.go). Empty means the
	// dedup/stacking key is AbilityID alone (see statusStackKey).
	Name         string
	CasterID     int
	TargetUnitID int
	Remaining    float64
	TickInterval float64
	tickTimer    float64             // counts down to the next on_status_tick fire (runtime-only)
	Triggers     []AbilityTriggerDef // compiled on_status_tick / on_status_expire trigger(s)
	// Stacking controls what happens when a NEW application targets a unit
	// that already carries a status sharing this one's stack key (see
	// statusStackKey / spawnAbilityStatusLocked):
	//   ""/"refresh" (default): keep the single existing instance and extend
	//     its Remaining to the longer of the two durations — mirrors
	//     ApplyStunLocked/applySlowToTrack's existing refresh-longer
	//     convention for the legacy CC primitives exactly, just generalized
	//     to an authored status object.
	//   "stack": keep the existing instance(s) AND add this one as a fully
	//     independent instance (its own Remaining/tickTimer), up to MaxStacks
	//     total sharing the key. Each instance ticks on its own cadence, so N
	//     stacks fire on_status_tick N times per interval rather than
	//     requiring a numeric "stack count" bound into the context (see
	//     ContextValue's kinds in ability_exec.go, which this subsystem
	//     deliberately does not extend — reported in the phase's summary).
	Stacking  string
	MaxStacks int
}

func abilityStatusIDString(id int) string {
	return "status-" + strconv.Itoa(id)
}

// statusStackKey identifies the dedup/stacking group a status belongs to on
// one target: same AbilityID + Name. Caster-agnostic, deliberately mirroring
// the existing legacy CC primitives' own caster-agnostic refresh convention
// (any source's stun refreshes the single global StunnedRemaining,
// regardless of who cast it) rather than keying by caster too.
func statusStackKey(abilityID, name string) string {
	if name == "" {
		return abilityID
	}
	return abilityID + "::" + name
}

// spawnAbilityStatusLocked applies st's stacking policy (statusStackKey)
// against any existing status already on st.TargetUnitID sharing that key,
// then — unless the application was absorbed by a refresh — assigns st's id,
// arms its tick cadence, and appends it to s.AbilityStatuses.
//
// Cadence: unlike spawnAbilityZoneLocked (which arms tickTimer=0 for an
// IMMEDIATE first fire, deliberately mirroring GroundHazard's impact-then-
// burn pacing), a status's first on_status_tick fires after one full
// TickInterval (tickTimer = TickInterval, not 0): the apply_status action's
// own trigger has already run whatever on-apply effects the author wanted in
// the SAME frame the status lands, so an immediate tick would double-apply on
// that same frame. This is a deliberate difference from AbilityZone, not an
// oversight.
//
// No-op if st is nil or carries no target (TargetUnitID == 0) — every caller
// today is apply_status's Execute, which already skips dead/missing targets
// before calling this, but this guard keeps the function safe standalone.
//
// Caller holds s.mu.
func (s *GameState) spawnAbilityStatusLocked(st *AbilityStatus) {
	if st == nil || st.TargetUnitID == 0 {
		return
	}
	key := statusStackKey(st.AbilityID, st.Name)

	if st.Stacking != "stack" {
		// refresh (default): find an existing status sharing this key on the
		// same target and extend it (refresh-longer) instead of spawning a
		// second instance.
		for _, existing := range s.AbilityStatuses {
			if existing.TargetUnitID != st.TargetUnitID {
				continue
			}
			if statusStackKey(existing.AbilityID, existing.Name) != key {
				continue
			}
			if st.Remaining > existing.Remaining {
				existing.Remaining = st.Remaining
			}
			return
		}
	} else {
		// stack: cap the number of instances sharing this key on this target,
		// mirroring UnitPerkState.applyBurnStack's per-group cap style.
		count := 0
		for _, existing := range s.AbilityStatuses {
			if existing.TargetUnitID == st.TargetUnitID && statusStackKey(existing.AbilityID, existing.Name) == key {
				count++
			}
		}
		maxStacks := st.MaxStacks
		if maxStacks <= 0 {
			maxStacks = 1
		}
		if count >= maxStacks {
			slog.Debug("ability status stack cap reached; application dropped",
				"abilityId", st.AbilityID, "name", st.Name, "target", st.TargetUnitID, "maxStacks", maxStacks)
			return
		}
	}

	if st.TickInterval <= 0 {
		slog.Warn("ability status spawned with non-positive tickInterval; it will never tick",
			"abilityId", st.AbilityID, "casterId", st.CasterID)
	} else {
		st.tickTimer = st.TickInterval
	}
	st.ID = abilityStatusIDString(s.nextAbilityStatusID)
	s.nextAbilityStatusID++
	s.AbilityStatuses = append(s.AbilityStatuses, st)
}

// tickAbilityStatusesLocked advances every AbilityStatus by dt.
//
// For each status, in s.AbilityStatuses' existing order (append order —
// program-execution order — never map-iteration order; no sort is needed
// here, unlike zone occupancy, because a status's target is already a stored
// ID with no per-tick discovery step whose ordering could vary):
//
//  1. Re-resolve TargetUnitID and validate it (nil or HP<=0). If invalid
//     already, fire on_status_expire now and drop the status — this is what
//     makes "the target died earlier this very tick, before statuses ticked"
//     terminate the status on the SAME tick rather than one tick late (see
//     the placement note below).
//  2. Otherwise fire as many due on_status_tick triggers as TickInterval
//     cadence demands (epsilon-robust, matching tickAbilityZonesLocked's
//     loop), re-validating the target after each fire since the tick's own
//     actions (e.g. deal_damage) can kill it mid-loop. If it does,
//     on_status_expire fires immediately and the status is dropped — exactly
//     once, never also via the natural-timeout path below.
//  3. If the target survived every due tick this call, count Remaining down;
//     on natural timeout fire on_status_expire and drop; otherwise keep the
//     status for the next call.
//
// Re-entrancy: snapshot-and-reset s.AbilityStatuses (active/kept), NOT the
// `kept := s.AbilityStatuses[:0]` in-place-compaction idiom — an
// on_status_tick or on_status_expire trigger's actions run through the SAME
// executor as any other trigger, so either can contain a nested
// apply_status(authored) action, which appends to s.AbilityStatuses via
// spawnAbilityStatusLocked. Appending to the field while this loop still
// holds its original slice header would corrupt or lose the new status
// exactly like the hazard tickAbilityZonesLocked's own doc comment describes
// (and fixes the identical way) for a nested create_zone.
//
// PLACEMENT: called from Update() immediately after tickAbilityZonesLocked
// and BEFORE drainPendingDeathsLocked — the same slot zones occupy, for the
// same reason: a status's target can die from this tick's combat/trap/
// projectile/zone damage (queued in s.pendingDeaths but NOT YET removed from
// s.unitsByID — drainPendingDeathsLocked runs after), so ticking statuses
// here lets step 1 above observe HP<=0 and fire the expire THIS tick instead
// of the target vanishing out from under a status that never got a chance to
// react. Running after zones specifically also means a status spawned by a
// zone's own on_zone_tick this same Update call is already ticking machinery
// (armed, in s.AbilityStatuses) by the time this function's caller returns —
// consistent with how zones themselves can spawn other zones mid-tick.
//
// With no statuses spawned (s.AbilityStatuses nil/empty — true for every
// existing test and every match until an authored status ships) this is a
// no-op.
//
// Must run under s.mu.
func (s *GameState) tickAbilityStatusesLocked(dt float64) {
	if len(s.AbilityStatuses) == 0 {
		return
	}
	active := s.AbilityStatuses
	s.AbilityStatuses = nil
	kept := active[:0]

	for _, st := range active {
		target := s.getUnitByIDLocked(st.TargetUnitID)
		if target == nil || target.HP <= 0 {
			s.fireAbilityStatusExpireLocked(st)
			continue
		}

		diedMidTick := false
		if st.TickInterval > 0 {
			st.tickTimer -= dt
			for st.tickTimer <= statusTickEpsilon {
				st.tickTimer += st.TickInterval
				s.fireAbilityStatusTickLocked(st)
				if u := s.getUnitByIDLocked(st.TargetUnitID); u == nil || u.HP <= 0 {
					diedMidTick = true
					break
				}
			}
		}
		if diedMidTick {
			s.fireAbilityStatusExpireLocked(st)
			continue
		}

		st.Remaining -= dt
		if st.Remaining > statusTickEpsilon {
			kept = append(kept, st)
			continue
		}
		s.fireAbilityStatusExpireLocked(st)
	}
	s.AbilityStatuses = append(kept, s.AbilityStatuses...)
}

// buildStatusEventContextLocked builds the shared RuntimeAbilityContext shape
// for one on_status_tick/on_status_expire fire: CurrentEventUnitID binds the
// afflicted unit (so select_targets{source:"current_event"} resolves to
// exactly it, mirroring fireAbilityZoneOccupancyEventLocked's identical
// binding) and EventPosition anchors on its current world position, falling
// back to the zero position if the target can no longer be resolved (only
// reachable from the expire path — the tick path always calls this with an
// already-validated live target). Caller holds s.mu.
func (s *GameState) buildStatusEventContextLocked(st *AbilityStatus) *RuntimeAbilityContext {
	var pos protocol.Vec2
	if u := s.getUnitByIDLocked(st.TargetUnitID); u != nil {
		pos = protocol.Vec2{X: u.X, Y: u.Y}
	}
	return &RuntimeAbilityContext{
		CasterID:           st.CasterID,
		AbilityID:          st.AbilityID,
		OwnerUnitID:        st.CasterID,
		EventPosition:      pos,
		CurrentEventUnitID: st.TargetUnitID,
		Named:              map[string]ContextValue{},
		Trace:              s.previewTrace,
		now:                s.previewClock,
	}
}

// fireAbilityStatusTickLocked runs st's compiled on_status_tick trigger(s)
// through the shared executor. Caller holds s.mu.
func (s *GameState) fireAbilityStatusTickLocked(st *AbilityStatus) {
	ctx := s.buildStatusEventContextLocked(st)
	s.runProgramTriggersLocked(ctx, st.Triggers, TriggerOnStatusTick)
}

// fireAbilityStatusExpireLocked runs st's compiled on_status_expire
// trigger(s) through the shared executor. Fired exactly once per
// AbilityStatus by tickAbilityStatusesLocked — see this file's EXPIRY
// SEMANTICS doc comment. Caller holds s.mu.
func (s *GameState) fireAbilityStatusExpireLocked(st *AbilityStatus) {
	ctx := s.buildStatusEventContextLocked(st)
	s.runProgramTriggersLocked(ctx, st.Triggers, TriggerOnStatusExpire)
}
