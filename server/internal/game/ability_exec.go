package game

import "webrts/server/pkg/protocol"

// ctxValueKind tags what a ContextValue holds.
type ctxValueKind int

const (
	ctxNone ctxValueKind = iota
	ctxUnitID
	ctxUnitSet
	ctxPosition
	// ctxScalar holds a plain number (Scalar field) — a loop/hop counter, a
	// running tally, etc. Set/incremented by the set_context action, read by
	// scalar-comparison conditions and by deal_damage's flatOffsetRef. Rides
	// CarriedNamed across beam/projectile hops like every other Named entry, so
	// a chain's bounce counter survives from one impact to the next.
	ctxScalar
)

// ContextValue is one typed runtime value bound under a name (an output binding)
// or read from a ContextRef during execution. Entity refs are unit IDs, resolved
// to *Unit at point of use (getUnitByIDLocked) — never stored as pointers.
type ContextValue struct {
	Kind     ctxValueKind
	UnitID   int
	UnitIDs  []int
	Position protocol.Vec2
	Scalar   float64
}

// RuntimeAbilityContext is the typed context one program execution runs under.
// All entity references are unit IDs. Trace is nil in production (zero overhead)
// and non-nil for the preview / tests.
type RuntimeAbilityContext struct {
	CasterID       int
	AbilityID      string
	CastID         int64
	InitialTarget  int // 0 = none
	CastPoint      protocol.Vec2
	EventPosition  protocol.Vec2
	ImpactPosition protocol.Vec2
	ZoneCenter     protocol.Vec2
	// ProjectilePosition is a ticking ("direction" travelMode + TickInterval>0)
	// projectile's CURRENT world position, bound fresh every firing by
	// tickArcaneOrbProjectileLocked (projectile.go). Resolved by
	// resolveOriginLocked's OriginProjectilePos case (ability_exec_targeting.go)
	// — the seam BLOCKER 2 of the arcane_orb composable migration made real.
	// Zero value {0,0} for every non-projectile-tick context (zone ticks,
	// ordinary casts, ...), which is harmless since nothing else references
	// this origin today.
	ProjectilePosition protocol.Vec2
	OwnerUnitID        int // owner of the current zone/status/projectile, if any
	// CurrentZoneID is the id of the AbilityZone whose trigger is running, bound
	// by the zone fire sites (ability_zone.go). Empty outside a zone-driven
	// execution. Used for tracing and as the "am I inside a zone" test.
	CurrentZoneID string
	// currentZone is that same zone as a WITHIN-TICK working value, so
	// consume_zone can mark it without looking it up. A lookup is not possible:
	// tickAbilityZonesLocked detaches s.AbilityZones (sets it nil) while it
	// iterates a local copy, so the zone is unreachable from GameState for the
	// duration of its own trigger. Bound and read entirely inside one
	// synchronous tick and never persisted — the same within-tick allowance
	// AI_RULES grants for *Unit working values, and the identical idiom
	// CurrentStatus already uses.
	currentZone *AbilityZone
	// CurrentStatus is the AbilityStatus an enclosing apply_status_duration
	// action just spawned (or refreshed — see spawnAbilityStatusLocked's
	// reference-semantics note below), exposed to that action's own
	// config.triggers so nested change_stat/apply_mark actions
	// (ability_status_duration.go) can bind their effect to it without
	// knowing anything about duration/stacking themselves. Mirrors ZoneCenter/
	// CurrentEventUnitID's "expose the current container object to its own
	// nested actions" idiom, except this ONE is a live pointer rather than an
	// id/value — safe because it is bound and cleared entirely within a
	// single synchronous apply_status_duration Execute call (never persisted
	// past it, never read by anything that outlives this tick — see
	// AI_RULES's "within-tick *Unit parameters are fine" allowance, which
	// applies identically here).
	//
	// REFRESH/STACK-CAP SEMANTICS FOR FREE: apply_status_duration's Execute
	// always builds a fresh *AbilityStatus and binds CurrentStatus to IT
	// (never to whatever spawnAbilityStatusLocked's refresh path found and
	// extended instead), then calls spawnAbilityStatusLocked. On a
	// "refresh" that collapses onto an already-live status, or a "stack"
	// application dropped for exceeding MaxStacks, the fresh object is
	// simply never appended to s.AbilityStatuses — so change_stat/apply_mark
	// still run (nothing here skips them) but their writes land on an
	// orphaned struct nothing ever reads, which is exactly the desired
	// "don't double the stat modifiers/icon a still-live refreshed status
	// already carries from its original application" outcome, with zero
	// special-case code.
	//
	// Nil outside an apply_status_duration's own config.triggers (every
	// other ctx in the codebase) — change_stat/apply_mark's Execute treats a
	// nil CurrentStatus as a no-op trace (validation already rejects
	// authoring them anywhere reachable with a nil binding, but Execute
	// stays defensive regardless).
	CurrentStatus *AbilityStatus
	// CurrentEventUnitID is the unit ID bound to the "current_event" target
	// source (SrcCurrentEvent, candidatePoolIDsLocked in
	// ability_exec_targeting.go) for a trigger whose event centers on one
	// specific unit — today only on_zone_enter/on_zone_exit
	// (fireAbilityZoneOccupancyEventLocked, ability_zone.go) set this; 0 means
	// no unit is bound (SrcCurrentEvent then yields an empty candidate pool).
	// Resolved to *Unit at point of use like every other ID field on this
	// struct, never stored as a pointer.
	CurrentEventUnitID int
	Selected           []int // most recent select_targets output (previous_action_targets)
	Named              map[string]ContextValue
	Trace              *AbilityExecutionTrace
	// now is the sim time stamped onto trace events. Set from
	// GameState.previewClock at ctx build time by top-level executor entry
	// points (resolveAbilityProgramCastLocked, fireAbilityZoneTickLocked,
	// ...); 0 in production, where it is never read (Trace is nil so
	// record() no-ops regardless).
	now   float64
	depth int // recursion guard for trigger_event / named triggers (Phase 3 Task 6)
	// program is the AbilityProgram trigger_event resolves NamedTriggers
	// against. Nil in most Phase 3 contexts (zone tick, etc.) — trigger_event
	// traces "no_program" and no-ops when nil. Set by the (later-phase) cast
	// entry point wherever top-level execution begins; tests set it directly
	// since this field is package-internal.
	program *AbilityProgram
	// abilityDef is set at top-level cast resolution so deal_damage applies
	// the caster's spell-modifiers for this ability's school/tags; nil ⇒ raw
	// amount (e.g. zone-tick/DoT, which legacy also applies raw).
	abilityDef *AbilityDef
	// damageEffectivenessMultiplier scales deal_damage's resolved amount for a
	// program run whose caller supplied a customized EffectiveSpell (today:
	// unstable_magic's reduced-effectiveness free proc — see
	// resolveAbilityProgramCastLocked and EffectiveSpell.DamageEffectivenessMultiplier).
	// The Go zero value (every context NOT built via
	// resolveAbilityProgramCastLocked — zone ticks, hand-built test contexts)
	// is treated as 1.0 (no extra scaling) by effectiveDamageMultiplier(),
	// never read directly. restore_health deliberately does NOT consult this:
	// legacy's own effectiveness scaling never touches heal amounts either —
	// resolveAbilityCastOnTargetLocked reads def.HealAmount directly, not eff.
	damageEffectivenessMultiplier float64
	// lastAppliedDamage is the total damage the most recent deal_damage action
	// applied this run (reset to 0 at the start of every deal_damage Execute,
	// then set to the sum of `amount` across every unit it actually hit — see
	// that action's Execute, ability_program_registry.go). Read by the
	// siphon_life channel loop (tickUnitChannelLocked, ability_channel.go) so
	// the per-tick heal amount and every Siphoner perk hook drive off the
	// SAME single authority a converted (SchemaVersion>=2) def's on_beam_tick
	// deal_damage actually applied, instead of recomputing the fold
	// separately and risking it drift from what deal_damage really did. A
	// general, additive field — no other caller reads it, so nothing else's
	// behavior changes.
	lastAppliedDamage int
	// opsUsed counts total executeActionLocked invocations across the whole
	// program run (not just the current recursion stack). This bounds TOTAL
	// WORK, which ctx.depth/maxTriggerDepth alone does not: a bounded-depth
	// recursion nested inside a repeat/multiplier (e.g.
	// repeat(64){ trigger_event(self) }) can still fan out exponentially
	// (64^depth) while never exceeding maxTriggerDepth. See maxExecutionOps.
	opsUsed int
	// currentActionPath is the path of the action currently executing, set by
	// executeActionLocked so an action's Execute can stamp leaf trace events
	// (damage/heal/etc.) with its own path; empty outside an action's
	// execution. executeActionLocked saves/restores the previous value around
	// desc.Execute so nested/sibling actions run via conditional/repeat/
	// trigger_event each see their own path and the parent's is restored on
	// return.
	currentActionPath string
	// currentActionID is the AUTHORED id of the action currently executing
	// ("deliver", "burn", ...), saved/restored alongside currentActionPath.
	// Unlike currentActionPath — which is a positional flow path that shifts
	// whenever an action is reordered — an id is stable identity, so it is the
	// right key for anything the EDITOR names a specific node by. Today that is
	// exactly one thing: previewConditionalOverrides (see state.go), which the
	// conditional action reads to force a branch during a preview run.
	currentActionID string
	// currentActionHasAttachInput records whether the action currently
	// executing declared Input["attach"] (a unit-set context ref). Set by
	// executeActionLocked alongside currentActionPath (save/restore around
	// desc.Execute), so play_presentation's Execute
	// (ability_exec_presentation.go) can distinguish its on-target shape
	// (attach a per-unit effect to each resolved target) from its at-point
	// shape (play one effect at a world position) without widening the
	// shared ActionDescriptor.Execute signature just for this one action
	// type. targets is already resolved from Input["attach"] the same way
	// Input["targets"] is resolved for deal_damage/restore_health — see
	// resolveActionTargetsLocked below.
	currentActionHasAttachInput bool
	// sharedOpsRemaining, when non-nil, is a budget pointer SHARED across every
	// RuntimeAbilityContext in one projectile-impact lineage (a cast that
	// launches a projectile, whose impact may itself launch another
	// projectile, and so on) — see ability_exec_projectile.go's CROSS-TICK OP
	// BUDGET doc section. When set, opsExhausted/consumeOp gate and decrement
	// THIS shared counter instead of the per-ctx opsUsed/maxExecutionOps pair,
	// so total work across the WHOLE lineage — however many projectiles it
	// branches into, however many ticks it spans — is bounded by ONE number
	// instead of resetting to a fresh maxExecutionOps every time a later tick
	// builds a fresh ctx for the next hop. nil (the default) for every
	// non-projectile ctx (zone ticks, statuses, markers, ordinary casts),
	// which keeps the original per-run behavior unchanged.
	sharedOpsRemaining *int
}

// opsExhausted reports whether this ctx's op budget has been fully spent.
// See sharedOpsRemaining's doc comment for the two modes this checks.
func (ctx *RuntimeAbilityContext) opsExhausted() bool {
	if ctx.sharedOpsRemaining != nil {
		return *ctx.sharedOpsRemaining <= 0
	}
	return ctx.opsUsed >= maxExecutionOps
}

// consumeOp records one unit of executor work. It always increments opsUsed
// (so every existing per-ctx diagnostic/test — e.g.
// TestExecutor_OpBudget_BoundsExponentialFanout — keeps reading a real,
// monotonically increasing count) and, when this ctx participates in a
// shared projectile-impact budget, ALSO decrements the shared counter — see
// sharedOpsRemaining's doc comment.
func (ctx *RuntimeAbilityContext) consumeOp() {
	ctx.opsUsed++
	if ctx.sharedOpsRemaining != nil {
		*ctx.sharedOpsRemaining--
	}
}

// maxTriggerDepth bounds trigger_event -> named-trigger recursion (Phase 3
// Task 6). ctx.depth is incremented before running a named trigger's actions
// and decremented after, so a self- or mutually-recursive named-trigger
// graph is refused once depth reaches maxTriggerDepth instead of stack-
// blowing / infinite-looping the synchronous executor. See
// triggerEventConfig's Execute in ability_exec_flow.go.
const maxTriggerDepth = 16

// maxExecutionOps is a shared TOTAL-WORK budget across one program run,
// independent of ctx.depth. maxTriggerDepth alone only bounds recursion
// STACK DEPTH — it does not stop a bounded-depth multiplier fan-out like
// repeat(64){ trigger_event(self) }, which is 64^maxTriggerDepth
// executeActionLocked calls without ever exceeding the depth guard. Once the
// executor is wired into the live cast path (Phase 4+) an unbounded op count
// would hang the tick loop under s.mu, so every action counts against this
// budget and every action-iterating loop (runProgramTriggersLocked, repeat,
// trigger_event, conditional) breaks early once it's exhausted. Real
// programs run well under 1000 ops; this only trips on adversarial /
// exponential fan-out.
const maxExecutionOps = 100000

// AbilityExecutionTrace is the ordered event log the executor emits when non-nil.
// It is the single source for the preview timeline + event log (later phase).
type AbilityExecutionTrace struct{ Events []AbilityExecutionTraceEvent }

// AbilityExecutionTraceEvent is one recorded step. Path maps the event back to a
// flow card (e.g. "triggers[0].actions[1]"). Time is a simulation-relative time,
// stamped from RuntimeAbilityContext.now; production leaves it 0 (no clock
// threaded in and Trace is nil regardless), the preview harness sets real times
// (Phase 6a).
type AbilityExecutionTraceEvent struct {
	Time    float64        `json:"t"`
	Type    string         `json:"type"`
	Path    string         `json:"path,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
}

// record appends an event. Nil-receiver-safe: a nil trace (production) is a no-op.
func (tr *AbilityExecutionTrace) record(t float64, typ, path string, payload map[string]any) {
	if tr == nil {
		return
	}
	tr.Events = append(tr.Events, AbilityExecutionTraceEvent{Time: t, Type: typ, Path: path, Payload: payload})
}

// trace is a convenience so executor code can emit an event without a nil
// check. Stamps ctx.now, which is 0 in production (no clock threaded in) and
// the preview harness's accumulated sim time during a preview run (Phase 6a).
func (ctx *RuntimeAbilityContext) trace(typ, path string, payload map[string]any) {
	ctx.Trace.record(ctx.now, typ, path, payload)
}

// effectiveDamageMultiplier returns ctx.damageEffectivenessMultiplier,
// treating the zero value as 1.0 (no extra scaling) — see that field's doc
// comment.
func (ctx *RuntimeAbilityContext) effectiveDamageMultiplier() float64 {
	if ctx.damageEffectivenessMultiplier == 0 {
		return 1.0
	}
	return ctx.damageEffectivenessMultiplier
}

// ─────────────────────────────────────────────────────────────────────────────
// Executor loop (Phase 3, Task 3)
//
// runProgramTriggersLocked / executeActionLocked / resolveActionTargetsLocked /
// bindActionOutputsLocked are the executor entry points. They are NOT wired
// into the live cast path in Phase 3 — nothing in the tick loop or
// resolveAbilityCastLocked calls runProgramTriggersLocked yet. Only tests
// (and later-phase callers) invoke it. Zero live behavior change.
// ─────────────────────────────────────────────────────────────────────────────

// runProgramTriggersLocked fires every trigger of type ttype (conditions
// permitting) in order, executing each trigger's enabled actions. Caller holds
// s.mu. This is the executor entry point; it is NOT wired into the live cast
// path in Phase 3 (tests + later-phase callers only).
func (s *GameState) runProgramTriggersLocked(ctx *RuntimeAbilityContext, triggers []AbilityTriggerDef, ttype TriggerType) {
	for ti := range triggers {
		trg := &triggers[ti]
		if trg.Type != ttype {
			continue
		}
		if !s.triggerConditionsPassLocked(ctx, trg) {
			ctx.trace("condition_failed", trg.ID, nil)
			continue
		}
		ctx.trace("trigger_fired", trg.ID, map[string]any{"type": string(trg.Type)})
		s.runTriggerActionsLocked(ctx, trg, trg.ID)
	}
}

// executeActionLocked resolves an action's target set and dispatches to its
// registered Execute. Disabled actions and descriptorless / deferred actions
// (nil Execute) are skipped with a trace event. Caller holds s.mu.
//
// The op-budget check runs BEFORE any other work (including the disabled/
// descriptor checks) so a program that has exhausted maxExecutionOps cannot
// do any further work through this entry point, no matter how it's shaped.
func (s *GameState) executeActionLocked(ctx *RuntimeAbilityContext, a *AbilityActionDef, path string) {
	apath := path + ".actions[" + a.ID + "]"
	if ctx.opsExhausted() {
		ctx.trace("op_budget_exceeded", apath, map[string]any{"limit": maxExecutionOps})
		return
	}
	ctx.consumeOp()
	if !a.IsEnabled() {
		return
	}
	desc, ok := lookupActionDescriptor(a.Type)
	if !ok || desc.Execute == nil {
		ctx.trace("action_skipped", apath, map[string]any{"type": string(a.Type)})
		return
	}
	// Resolve loop-variable (a..z) and ability-parameter ("$name") references in
	// the raw config to their current values before decode — a no-op with zero
	// cost when neither is in scope (see resolveConfigVars, ability_exec_loop.go).
	// Fold the caster's BROAD ability stats ("+2s duration", "+15% radius") into
	// this action's kinded config fields. Runs after the substitution above so it
	// operates on concrete numbers, and before decode so every action type gets it
	// without touching its Execute — see ability_stats.go for why this seam, and
	// not a per-action read site, is the right one.
	// PRECISE modifiers fold first ({ability, action, field} — perks that know the
	// program), then the BROAD ability stats fold on that result ({kind} — units
	// and items, which cannot name an ability). Order matters and is deliberate:
	// a unit's "+15% radius" amplifies a perk's contribution rather than the other
	// way round. See ability_field_mods.go's FOLD ORDER note.
	caster := s.getUnitByIDLocked(ctx.CasterID)
	rawConfig := ctx.resolveConfigVars(a.Config)
	rawConfig = s.applyAbilityFieldModsToConfigLocked(caster, ctx.AbilityID, a.ID, rawConfig)
	rawConfig = s.applyAbilityStatsToConfigLocked(caster, a.Type, rawConfig)
	cfg, err := desc.Decode(rawConfig)
	if err != nil {
		ctx.trace("validation_error", apath, map[string]any{"error": err.Error()})
		return
	}
	targets := s.resolveActionTargetsLocked(ctx, a)
	ctx.trace("action_started", apath, map[string]any{"type": string(a.Type), "targets": len(targets)})
	// Stamp this action's path onto ctx for the duration of its Execute (and
	// any nested on_action_complete children run below) so leaf trace events
	// emitted from inside Execute (damage_applied, healing_applied, ...) can
	// be attributed back to this action's flow node. Restore the caller's
	// value on return (defer, panic-safe) so sibling/parent actions are
	// unaffected — nested executeActionLocked calls (conditional/repeat/
	// trigger_event/children) each save+restore their own, one level at a
	// time.
	prevActionPath := ctx.currentActionPath
	ctx.currentActionPath = apath
	prevActionID := ctx.currentActionID
	ctx.currentActionID = a.ID
	_, hasAttachInput := a.Input["attach"]
	prevHasAttachInput := ctx.currentActionHasAttachInput
	ctx.currentActionHasAttachInput = hasAttachInput
	defer func() {
		ctx.currentActionPath = prevActionPath
		ctx.currentActionID = prevActionID
		ctx.currentActionHasAttachInput = prevHasAttachInput
	}()
	out := desc.Execute(s, ctx, cfg, targets)
	s.bindActionOutputsLocked(ctx, a, out)
	ctx.trace("action_completed", apath, nil)
	// Inline follow-up triggers on this action (on_action_complete).
	s.runProgramTriggersLocked(ctx, a.Children, TriggerOnActionComplete)
}

// resolveActionTargetsLocked prepares an action's target-set: its own Target
// query if present; else an Input "targets" ContextRef; else an Input
// "attach" ContextRef (play_presentation's on-target shape — see
// ability_exec_presentation.go); else the most-recent selection
// (previous_action_targets).
func (s *GameState) resolveActionTargetsLocked(ctx *RuntimeAbilityContext, a *AbilityActionDef) []int {
	if a.Target != nil {
		// A target query's radius IS the area of effect for most AoE abilities
		// (fireball's 100, meteor's 230, shatter's 110 — none of which is a
		// create_zone radius), so radius modifiers have to reach it or the whole
		// stat would only affect trap zones. Folded on a COPY: the action def is
		// shared, immutable catalog data.
		q := *a.Target
		q.Radius = s.foldTargetQueryRadiusLocked(ctx, a, q)
		return s.resolveTargetQueryLocked(ctx, q)
	}
	if ref, ok := a.Input["targets"]; ok {
		return ctx.resolveTargetRef(ref)
	}
	if ref, ok := a.Input["attach"]; ok {
		return ctx.resolveTargetRef(ref)
	}
	return append([]int(nil), ctx.Selected...)
}

// bindActionOutputsLocked stores an action's returned ids under its Outputs
// bindings and updates ctx.Selected (previous_action_targets).
func (s *GameState) bindActionOutputsLocked(ctx *RuntimeAbilityContext, a *AbilityActionDef, out []int) {
	if out != nil {
		ctx.Selected = out
	}
	for _, key := range a.Outputs { // map value is the destination context key
		ctx.Named[key] = ContextValue{Kind: ctxUnitSet, UnitIDs: append([]int(nil), out...)}
	}
}

// triggerConditionsPassLocked evaluates a trigger's conditions. Phase 3 authors
// no conditions yet, so an empty list always passes; non-empty is a
// TODO(phase-3b) and treated as passing for now (documented).
func (s *GameState) triggerConditionsPassLocked(ctx *RuntimeAbilityContext, trg *AbilityTriggerDef) bool {
	// TODO(phase-3b): evaluate trg.Conditions.
	return true
}

// resolveTargetRef reads a ContextRef into a set of unit ids: a Named binding,
// or the special keys "selected"/"previous_action_targets" (ctx.Selected) and
// "initial_target" (ctx.InitialTarget). Unknown ⇒ empty.
func (ctx *RuntimeAbilityContext) resolveTargetRef(ref ContextRef) []int {
	switch ref.Key {
	case "selected", "previous_action_targets":
		return append([]int(nil), ctx.Selected...)
	case "initial_target":
		if ctx.InitialTarget != 0 {
			return []int{ctx.InitialTarget}
		}
		return nil
	}
	if v, ok := ctx.Named[ref.Key]; ok {
		if len(v.UnitIDs) > 0 {
			return append([]int(nil), v.UnitIDs...)
		}
		if v.UnitID != 0 {
			return []int{v.UnitID}
		}
	}
	return nil
}

// resolveScalarRef reads a ContextRef into a number: the Named binding at
// ref.Key when it holds a ctxScalar. Returns (0, false) when the key is absent
// or bound to a non-scalar value, so callers can distinguish "counter not set
// yet" from "counter is 0".
func (ctx *RuntimeAbilityContext) resolveScalarRef(ref ContextRef) (float64, bool) {
	if v, ok := ctx.Named[ref.Key]; ok && v.Kind == ctxScalar {
		return v.Scalar, true
	}
	return 0, false
}
