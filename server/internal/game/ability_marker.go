package game

import "webrts/server/pkg/protocol"

// ═════════════════════════════════════════════════════════════════════════════
// ANIMATION-MARKER SCHEDULER (Phase 6b, Task 2)
//
// play_presentation (ability_exec_presentation.go) resolves an at-point
// presentation's on_animation_marker triggers synchronously today (Phase 3's
// TestMeteorV2Program_ImpactAndZone_EndToEnd still fires meteor's "impact"
// trigger by hand through runProgramTriggersLocked to prove the gameplay
// pipeline). This file lets play_presentation instead ENQUEUE those triggers
// to fire on a LATER tick, once Timing.DelaySeconds has elapsed on the
// server's simulation clock — matching a real animation, where "impact"
// actually lands partway through playback rather than instantly at cast
// time.
//
// Per AI_RULES' target-by-ID discipline: scheduledMarker stores only ids and
// plain positions/def-data across ticks, never a *Unit. Immutable action defs
// ([]AbilityActionDef) are fine to carry across ticks (same as AbilityZone's
// Triggers field) — they are catalog/program data, not entities.
//
// Determinism: fireAtSimTime is computed from s.simTime (a plain
// dt-accumulator advanced once per Update, see state.go), never wall-clock.
// tickAbilityMarkersLocked processes s.pendingMarkers in slice (enqueue)
// order every tick, so firing order is fully determined by the sequence of
// scheduleMarkerTriggersLocked calls and simTime — no map iteration, no
// randomness.
// ═════════════════════════════════════════════════════════════════════════════

// scheduledMarker is one on_animation_marker trigger enqueued by
// scheduleMarkerTriggersLocked, waiting for fireAtSimTime. Every field is an
// id, plain value, or immutable def data — never a *Unit/*BuildingTile — so
// it is safe to hold across tick boundaries.
type scheduledMarker struct {
	fireAtSimTime float64
	casterID      int
	abilityID     string
	marker        string
	castPoint     protocol.Vec2
	impactPos     protocol.Vec2
	zoneCenter    protocol.Vec2
	eventPos      protocol.Vec2
	ownerUnitID   int
	initialTarget int
	actions       []AbilityActionDef
}

// scheduleMarkerTriggersLocked enqueues every on_animation_marker trigger on
// pres to fire at s.simTime + Timing.DelaySeconds — never synchronously from
// within this call. It is NOT true in general that a zero-delay (or no
// Timing) marker fires on the "next tick": it fires no earlier than the
// first tickAbilityMarkersLocked pass whose s.simTime has reached
// fireAtSimTime, and exactly which tick that is depends on where in the tick
// scheduling happened relative to Update's `s.simTime += dt` line (state.go).
// Autocast (ability_autocast.go, called from tickCombatAILocked) resolves a
// cast — and so calls this — BEFORE that increment runs later in the SAME
// Update call, so a zero-delay marker scheduled that way is picked up by
// that same call's tickAbilityMarkersLocked pass (fires the SAME tick it was
// scheduled). Scheduling from outside Update (as these tests do, and as a
// preview/editor path would) instead waits for the next Update's increment
// to cross fireAtSimTime, i.e. the next tick.
//
// Positions/ids are captured from ctx at enqueue time (the cast/impact
// moment), NOT re-resolved from a live unit at fire time — this matches
// meteor's existing "impact point is fixed at cast resolution" semantics.
// initialTarget is carried as an id and re-validated indirectly: at fire
// time the rebuilt RuntimeAbilityContext's InitialTarget is just an id, and
// deal_damage/etc. re-resolve + nil/HP-check it via getUnitByIDLocked like
// any other target, exactly as AI_RULES requires.
//
// Caller holds s.mu.
func (s *GameState) scheduleMarkerTriggersLocked(ctx *RuntimeAbilityContext, pres PresentationInstanceDef) {
	for i := range pres.Triggers {
		trig := &pres.Triggers[i]
		if trig.Type != TriggerOnAnimationMarker {
			continue
		}
		var delay float64
		var marker string
		if trig.Timing != nil {
			delay = trig.Timing.DelaySeconds
			marker = trig.Timing.Marker
		}
		s.pendingMarkers = append(s.pendingMarkers, scheduledMarker{
			fireAtSimTime: s.simTime + delay,
			casterID:      ctx.CasterID,
			abilityID:     ctx.AbilityID,
			marker:        marker,
			castPoint:     ctx.CastPoint,
			impactPos:     ctx.ImpactPosition,
			zoneCenter:    ctx.ZoneCenter,
			eventPos:      ctx.EventPosition,
			ownerUnitID:   ctx.OwnerUnitID,
			initialTarget: ctx.InitialTarget,
			// TODO(phase-3b): carry trig.Conditions and evaluate at fire time
			// once triggerConditionsPassLocked is implemented — marker
			// triggers currently bypass conditions.
			actions: trig.Actions,
		})
	}
}

// tickAbilityMarkersLocked fires every pending marker whose fireAtSimTime has
// arrived, in enqueue (slice) order, rebuilding a fresh RuntimeAbilityContext
// per marker. With no markers pending (true for every match today — see
// TestMarkerScheduler_ProductionNoOp) this is a single length check and
// allocates nothing, matching the tick loop's hot-path allocation discipline.
//
// remaining is a fresh, decoupled backing array (not s.pendingMarkers[:0])
// specifically because firing a marker's actions can itself invoke
// play_presentation -> scheduleMarkerTriggersLocked again (a marker action
// chaining into another marker-triggered presentation), which appends onto
// s.pendingMarkers WHILE this loop is still ranging over the pre-tick
// snapshot. Reusing s.pendingMarkers's own backing array for remaining would
// risk that re-entrant append being silently discarded when remaining is
// written back at the end; a decoupled slice guarantees nothing scheduled
// during firing is ever lost, at the cost of one allocation on ticks where at
// least one marker is pending (never in production today).
//
// Caller holds s.mu.
func (s *GameState) tickAbilityMarkersLocked() {
	n := len(s.pendingMarkers)
	if n == 0 {
		return
	}
	remaining := make([]scheduledMarker, 0, n)
	for i := 0; i < n; i++ {
		// Re-read by index each iteration (not a cached slice header) so a
		// re-entrant scheduleMarkerTriggersLocked call during firing (which
		// may grow/reallocate s.pendingMarkers) can never leave this loop
		// looking at a stale backing array for the entries it hasn't visited
		// yet.
		m := s.pendingMarkers[i]
		if m.fireAtSimTime > s.simTime {
			remaining = append(remaining, m)
			continue
		}
		s.fireScheduledMarkerLocked(&m)
	}
	// Preserve anything newly enqueued during firing (appended past index
	// n-1 of the pre-tick snapshot).
	if len(s.pendingMarkers) > n {
		remaining = append(remaining, s.pendingMarkers[n:]...)
	}
	s.pendingMarkers = remaining
}

// fireScheduledMarkerLocked re-resolves m's ability by id (the SAME
// overlay-first resolver every other executor entry point uses —
// getAbilityDef, ability_defs.go — so a preview-registered ability is still
// found here even though this fires on a later tick than the one that
// scheduled it), rebuilds a fresh RuntimeAbilityContext from m's stored
// ids/positions, and runs m's actions in order. Silently drops (no panic, no
// effect) when the ability is gone or has no Program by fire time — the
// caster/ability may have been removed between scheduling and firing, which
// must never crash the tick loop.
//
// Caller holds s.mu.
func (s *GameState) fireScheduledMarkerLocked(m *scheduledMarker) {
	def, ok := getAbilityDef(m.abilityID)
	if !ok || def.Program == nil {
		return
	}
	ctx := &RuntimeAbilityContext{
		CasterID:       m.casterID,
		AbilityID:      m.abilityID,
		InitialTarget:  m.initialTarget,
		CastPoint:      m.castPoint,
		ImpactPosition: m.impactPos,
		ZoneCenter:     m.zoneCenter,
		EventPosition:  m.eventPos,
		OwnerUnitID:    m.ownerUnitID,
		Named:          map[string]ContextValue{},
		Trace:          s.previewTrace,
		now:            s.previewClock,
		program:        def.Program,
		abilityDef:     &def,
	}
	path := "marker[" + m.marker + "]"
	for i := range m.actions {
		if ctx.opsUsed >= maxExecutionOps {
			break
		}
		s.executeActionLocked(ctx, &m.actions[i], path)
	}
}
