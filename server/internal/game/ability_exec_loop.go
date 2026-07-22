package game

import (
	"encoding/json"
	"math"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// The `loop` action — a WRAPPER that runs its body once per iteration.
//
// A loop lives in a trigger's action list (e.g. inside on_cast_complete) and
// provides two things to the body it contains: VARIABLES (a..z, each
// start + step*iteration) and an ITERATION count. Body number fields reference
// a variable by its bare letter or hold a literal (resolveConfigVars).
//
// TIMING — each iteration completes fully before the next begins. A `wait`
// action anywhere in the body defines the gap between iterations: the loop runs
// one iteration to completion, then schedules the next after the body's total
// wait time (the pendingLoop scheduler, modeled on the animation-marker
// scheduler in ability_marker.go). A body with NO wait runs every iteration in
// the same tick. This is how chain_lightning stays sequential-over-time without
// any self-referencing named trigger: damage → pick next → arc a beam → wait.
//
// Per AI_RULES' target-by-ID discipline: a scheduled iteration carries only
// ids, plain positions, immutable action defs, and a snapshot of the named
// context (the visited set / cursor) — never a *Unit. Determinism: fireAtSimTime
// is s.simTime + delay (a dt-accumulator, never wall-clock), and
// tickPendingLoopsLocked processes the queue in slice order.
// ─────────────────────────────────────────────────────────────────────────────

// maxLoopVars caps a loop's variable count at the 26 single-letter names a..z.
const maxLoopVars = 26

// LoopVar.StepMode values. "" is treated as loopStepNumber (additive).
const (
	loopStepNumber  = "number"
	loopStepPercent = "percent"
)

// loopVarValue computes variable v's value at iteration k — additive by default,
// multiplicative (compounding Step% per iteration) when StepMode is "percent" —
// rounded to the nearest integer (see LoopVar's doc comment).
func loopVarValue(v LoopVar, iteration int) float64 {
	var value float64
	if v.StepMode == loopStepPercent {
		value = v.Start * math.Pow(1+v.Step/100, float64(iteration))
	} else {
		value = v.Start + v.Step*float64(iteration)
	}
	return math.Round(value)
}

// maxLoopIterations bounds a loop's iteration count so a malformed/hand-authored
// program can't spin the synchronous (no-wait) path or the scheduler unbounded.
const maxLoopIterations = 256

// runTriggerActionsLocked runs a trigger's actions in order — the shared choke
// point runProgramTriggersLocked and trigger_event route through.
func (s *GameState) runTriggerActionsLocked(ctx *RuntimeAbilityContext, trg *AbilityTriggerDef, path string) {
	for ai := range trg.Actions {
		if ctx.opsExhausted() {
			break
		}
		s.executeActionLocked(ctx, &trg.Actions[ai], path)
	}
}

// loopConfig is the decoded config of a `loop` action. Body is the wrapped
// actions run each iteration; Vars are bound before the body runs.
type loopConfig struct {
	Iterations int                `json:"iterations"`
	Vars       []LoopVar          `json:"vars,omitempty"`
	Body       []AbilityActionDef `json:"body,omitempty"`
	// StepFirst applies each variable's step to the FIRST iteration too. Off
	// (default): iteration 0 = Start (unstepped), then step accrues (0,1,2,…).
	// On: iteration 0 is already stepped once (1,2,3,…) — i.e. the step is
	// applied to the first loop as well.
	StepFirst bool `json:"stepFirst,omitempty"`
}

func (loopConfig) actionConfig() {}

// runLoopBodyLocked binds iteration k's variables and runs the body once. It
// does NOT schedule anything — the caller (Execute or fireLoopIterationLocked)
// decides whether the next iteration runs now (no wait) or later (scheduled).
func (s *GameState) runLoopBodyLocked(ctx *RuntimeAbilityContext, cfg loopConfig, iteration int, path string) {
	if ctx.Named == nil {
		ctx.Named = map[string]ContextValue{}
	}
	// StepFirst shifts the stepping so the first iteration is already stepped
	// once (see loopConfig.StepFirst).
	stepIter := iteration
	if cfg.StepFirst {
		stepIter = iteration + 1
	}
	for _, v := range cfg.Vars {
		if v.Name == "" {
			continue
		}
		ctx.Named[v.Name] = ContextValue{Kind: ctxScalar, Scalar: loopVarValue(v, stepIter)}
	}
	ctx.trace("loop_iteration", path, map[string]any{"iteration": iteration})
	for bi := range cfg.Body {
		if ctx.opsExhausted() {
			break
		}
		s.executeActionLocked(ctx, &cfg.Body[bi], path)
	}
}

// unbindLoopVars removes a loop's variables from ctx.Named so they don't leak
// into sibling actions that run after the loop in the same trigger.
func unbindLoopVars(ctx *RuntimeAbilityContext, vars []LoopVar) {
	for _, v := range vars {
		delete(ctx.Named, v.Name)
	}
}

// loopBodyWaitSeconds sums the durations of every `wait` action in the body —
// the gap the loop leaves between iterations. 0 (no wait) ⇒ iterations run in
// one tick.
func loopBodyWaitSeconds(body []AbilityActionDef) float64 {
	total := 0.0
	for i := range body {
		if body[i].Type != ActionWait {
			continue
		}
		var wc waitConfig
		decodeActionConfig(body[i].Config, &wc)
		total += wc.Seconds
	}
	return total
}

func clampLoopIterations(n int) int {
	if n < 0 {
		return 0
	}
	if n > maxLoopIterations {
		return maxLoopIterations
	}
	return n
}

// ── scheduler ──────────────────────────────────────────────────────────────

// pendingLoopIteration is one loop iteration enqueued to run at fireAtSimTime.
// Every field is an id, plain value, immutable def data, or a context snapshot
// — safe to hold across ticks.
type pendingLoopIteration struct {
	fireAtSimTime float64
	casterID      int
	abilityID     string
	iteration     int
	cfg           loopConfig
	path          string

	initialTarget      int
	castPoint          protocol.Vec2
	impactPos          protocol.Vec2
	eventPos           protocol.Vec2
	zoneCenter         protocol.Vec2
	projectilePos      protocol.Vec2
	ownerUnitID        int
	currentEventUnitID int
	carriedNamed       map[string]ContextValue
	selected           []int
}

// scheduleLoopIterationLocked enqueues iteration `iteration` to run `delay`
// seconds from now, snapshotting the context that must carry forward (the
// visited set / cursor in Named, the current selection, the cast positions).
func (s *GameState) scheduleLoopIterationLocked(ctx *RuntimeAbilityContext, cfg loopConfig, iteration int, delay float64, path string) {
	s.pendingLoops = append(s.pendingLoops, pendingLoopIteration{
		fireAtSimTime:      s.simTime + delay,
		casterID:           ctx.CasterID,
		abilityID:          ctx.AbilityID,
		iteration:          iteration,
		cfg:                cfg,
		path:               path,
		initialTarget:      ctx.InitialTarget,
		castPoint:          ctx.CastPoint,
		impactPos:          ctx.ImpactPosition,
		eventPos:           ctx.EventPosition,
		zoneCenter:         ctx.ZoneCenter,
		projectilePos:      ctx.ProjectilePosition,
		ownerUnitID:        ctx.OwnerUnitID,
		currentEventUnitID: ctx.CurrentEventUnitID,
		carriedNamed:       cloneNamedContext(ctx.Named),
		selected:           append([]int(nil), ctx.Selected...),
	})
}

// tickPendingLoopsLocked fires every loop iteration whose fireAtSimTime has
// arrived, in enqueue order, rebuilding a fresh context per iteration. Mirrors
// tickAbilityMarkersLocked's re-entrancy discipline (a decoupled `remaining`
// slice so an iteration that schedules the next isn't lost). No-op / zero-alloc
// when nothing is pending.
func (s *GameState) tickPendingLoopsLocked() {
	n := len(s.pendingLoops)
	if n == 0 {
		return
	}
	remaining := make([]pendingLoopIteration, 0, n)
	for i := 0; i < n; i++ {
		p := s.pendingLoops[i]
		if p.fireAtSimTime > s.simTime {
			remaining = append(remaining, p)
			continue
		}
		s.fireLoopIterationLocked(&p)
	}
	if len(s.pendingLoops) > n {
		remaining = append(remaining, s.pendingLoops[n:]...)
	}
	s.pendingLoops = remaining
}

// fireLoopIterationLocked rebuilds a context from p's snapshot, runs iteration
// p.iteration's body, then schedules the following iteration after the body's
// wait. Silently drops if the ability is gone by fire time.
func (s *GameState) fireLoopIterationLocked(p *pendingLoopIteration) {
	def, ok := getAbilityDef(p.abilityID)
	if !ok || def.Program == nil {
		return
	}
	named := cloneNamedContext(p.carriedNamed)
	if named == nil {
		named = map[string]ContextValue{}
	}
	ctx := &RuntimeAbilityContext{
		CasterID:           p.casterID,
		AbilityID:          p.abilityID,
		InitialTarget:      p.initialTarget,
		CastPoint:          p.castPoint,
		ImpactPosition:     p.impactPos,
		EventPosition:      p.eventPos,
		ZoneCenter:         p.zoneCenter,
		ProjectilePosition: p.projectilePos,
		OwnerUnitID:        p.ownerUnitID,
		CurrentEventUnitID: p.currentEventUnitID,
		Selected:           append([]int(nil), p.selected...),
		Named:              named,
		Trace:              s.previewTrace,
		now:                s.previewClock,
		program:            def.Program,
		abilityDef:         &def,
	}
	s.runLoopBodyLocked(ctx, p.cfg, p.iteration, p.path)
	if p.iteration+1 < clampLoopIterations(p.cfg.Iterations) {
		s.scheduleLoopIterationLocked(ctx, p.cfg, p.iteration+1, loopBodyWaitSeconds(p.cfg.Body), p.path)
	}
}

func init() {
	registerAction(ActionDescriptor{
		Type: ActionLoop,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c loopConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(loopConfig)
			var out []ValidationIssue
			if c.Iterations < 0 {
				out = append(out, ValidationIssue{Code: "invalid_property", Message: "loop iterations must be >= 0", Severity: "error"})
			}
			if len(c.Vars) > maxLoopVars {
				out = append(out, ValidationIssue{Code: "invalid_property", Message: "a loop may declare at most 26 variables", Severity: "error"})
			}
			seen := map[string]bool{}
			for _, v := range c.Vars {
				if len(v.Name) != 1 || v.Name[0] < 'a' || v.Name[0] > 'z' {
					out = append(out, ValidationIssue{Code: "invalid_property", Message: "loop variable name must be a single letter a-z, got \"" + v.Name + "\"", Severity: "error"})
					continue
				}
				if seen[v.Name] {
					out = append(out, ValidationIssue{Code: "invalid_property", Message: "duplicate loop variable \"" + v.Name + "\"", Severity: "error"})
				}
				if v.StepMode != "" && v.StepMode != loopStepNumber && v.StepMode != loopStepPercent {
					out = append(out, ValidationIssue{Code: "invalid_property", Message: "loop variable stepMode must be number or percent, got \"" + v.StepMode + "\"", Severity: "error"})
				}
				seen[v.Name] = true
			}
			return out
		},
		// vars + body are edited via the loop's own wrapper UI, not flat fields;
		// only iterations is a plain inspector control.
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "iterations", Label: "Iterations", Control: "number", Kind: abilityStatKindCount, Section: "Properties"},
			{Key: "stepFirst", Label: "Step On First Iteration", Control: "boolean", Section: "Advanced"},
		}},
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(loopConfig)
			iters := clampLoopIterations(c.Iterations)
			if iters == 0 {
				return targets
			}
			path := ctx.currentActionPath
			delay := loopBodyWaitSeconds(c.Body)
			if delay <= 0 {
				// No wait: run every iteration this tick (each completes before
				// the next — a plain synchronous loop).
				for k := 0; k < iters; k++ {
					if ctx.opsExhausted() {
						break
					}
					s.runLoopBodyLocked(ctx, c, k, path)
				}
				unbindLoopVars(ctx, c.Vars)
			} else {
				// Timed: EVERY iteration is scheduled, the first `delay` seconds
				// from now — so the wait spaces iterations AFTER whatever action
				// preceded the loop (e.g. chain_lightning's initial caster→target
				// bolt), giving a gap before the first bounce. Nothing runs
				// synchronously here.
				s.scheduleLoopIterationLocked(ctx, c, 0, delay, path)
			}
			return targets
		},
	})
}

// ── value references (a..z) ─────────────────────────────────────────────────

// loopVarInScope reports whether name is a single-letter loop variable (a..z)
// currently bound as a scalar in ctx — i.e. a live variable reference target.
func (ctx *RuntimeAbilityContext) loopVarInScope(name string) bool {
	if len(name) != 1 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	v, ok := ctx.Named[name]
	return ok && v.Kind == ctxScalar
}

// hasLoopVarsInScope is the cheap fast-path guard for resolveConfigVars: true
// only when at least one single-letter scalar is bound, so action executions
// outside a loop skip the config walk entirely.
func (ctx *RuntimeAbilityContext) hasLoopVarsInScope() bool {
	for k, v := range ctx.Named {
		if v.Kind == ctxScalar && len(k) == 1 && k[0] >= 'a' && k[0] <= 'z' {
			return true
		}
	}
	return false
}

// resolveConfigVars replaces loop-variable and ability-PARAMETER references in
// an action's raw config with their current numeric values. A JSON string that
// is a single lowercase letter bound as a loop variable becomes that number; a
// string of the form "$name" naming one of the cast's resolved parameters
// becomes that parameter's effective value. Every other value is untouched.
//
// Returns the input unchanged when neither loop variables nor parameters are in
// scope (the zero-cost common path) or the config isn't decodable — so an
// ability that declares no params pays a single length check per action.
func (ctx *RuntimeAbilityContext) resolveConfigVars(config json.RawMessage) json.RawMessage {
	if len(config) == 0 || !ctx.hasLoopVarsInScope() {
		return config
	}
	var v any
	if err := json.Unmarshal(config, &v); err != nil {
		return config
	}
	out, err := json.Marshal(ctx.substituteLoopVars(v))
	if err != nil {
		return config
	}
	return out
}

// substituteLoopVars walks a decoded JSON value and replaces any string that is
// an in-scope loop variable with its numeric value, recursing through objects
// and arrays.
func (ctx *RuntimeAbilityContext) substituteLoopVars(v any) any {
	switch t := v.(type) {
	case string:
		if ctx.loopVarInScope(t) {
			return ctx.Named[t].Scalar
		}
		return t
	case map[string]any:
		for k, val := range t {
			t[k] = ctx.substituteLoopVars(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = ctx.substituteLoopVars(val)
		}
		return t
	default:
		return v
	}
}

// substituteLoopVarPlaceholders is the STATIC-VALIDATION counterpart to
// resolveConfigVars: it replaces any string in a raw config that names one of
// the given in-scope loop variables with a positive placeholder (1), so a body
// field authored as a variable reference decodes to a valid typed config for
// structural validation. The placeholder is 1, not 0, so a field a variable
// feeds that requires a positive value (e.g. deal_damage amount > 0) still
// validates. Returns config unchanged when no vars are in scope or it isn't
// decodable JSON.
func substituteLoopVarPlaceholders(config json.RawMessage, vars map[string]bool) json.RawMessage {
	if len(config) == 0 || len(vars) == 0 {
		return config
	}
	var v any
	if err := json.Unmarshal(config, &v); err != nil {
		return config
	}
	out, err := json.Marshal(replaceLoopVarStrings(v, vars))
	if err != nil {
		return config
	}
	return out
}

// replaceLoopVarStrings walks a decoded JSON value and replaces any string that
// names an in-scope loop variable with the number 1, recursing through objects
// and arrays.
func replaceLoopVarStrings(v any, vars map[string]bool) any {
	switch t := v.(type) {
	case string:
		if vars[t] {
			return 1
		}
		return t
	case map[string]any:
		for k, val := range t {
			t[k] = replaceLoopVarStrings(val, vars)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = replaceLoopVarStrings(val, vars)
		}
		return t
	default:
		return v
	}
}
