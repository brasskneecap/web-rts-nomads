package game

import "encoding/json"

// AbilityEntryType describes how an ability is initiated: what the player
// (or AI) targets to begin casting it.
type AbilityEntryType string

const (
	EntrySelf        AbilityEntryType = "self"
	EntryUnit        AbilityEntryType = "unit"
	EntryGroundPoint AbilityEntryType = "ground_point"
	EntryDirection   AbilityEntryType = "direction"
	EntryNoTarget    AbilityEntryType = "no_target"
	EntryPassive     AbilityEntryType = "passive"
)

// TargetRelation filters candidate targets by their relationship to the
// caster.
type TargetRelation string

const (
	RelSelf    TargetRelation = "self"
	RelAlly    TargetRelation = "ally"
	RelEnemy   TargetRelation = "enemy"
	RelNeutral TargetRelation = "neutral"
)

// TriggerType identifies the event that fires an AbilityTriggerDef.
type TriggerType string

const (
	TriggerOnCastStart        TriggerType = "on_cast_start"
	TriggerOnCastComplete     TriggerType = "on_cast_complete"
	TriggerOnAnimationMarker  TriggerType = "on_animation_marker"
	TriggerOnProjectileImpact TriggerType = "on_projectile_impact"
	// TriggerOnProjectileTick fires repeatedly while a "direction" travelMode
	// launch_projectile bolt is in flight (arcane_orb's migrated shape — see
	// launchProjectileConfig's TickInterval doc comment, ability_compile.go,
	// and tickArcaneOrbProjectileLocked, projectile.go, for the firing
	// cadence). Distinct from on_projectile_impact, which fires at most once,
	// at the END of a bolt's flight (or on the first hostile it crosses, for
	// an impact-shaped "direction" bolt) — a ticking bolt never fires impact
	// at all.
	TriggerOnProjectileTick TriggerType = "on_projectile_tick"
	// TriggerOnBeamImpact fires once, a beat after a launch_beam action spawns
	// a momentary beam — the beam analogue of TriggerOnProjectileImpact. See
	// launchBeamConfig's doc comment (ability_exec_beam.go) and
	// fireBeamImpactLocked (beam.go) for the exact firing mechanics.
	TriggerOnBeamImpact TriggerType = "on_beam_impact"
	// TriggerOnBeamTick is reserved for a future channeling/ticking beam shape
	// (Task 3 of the composable-beam plan); not fired by anything in this
	// task. Declared now alongside TriggerOnBeamImpact so both beam trigger
	// types exist together rather than trickling in one at a time.
	TriggerOnBeamTick       TriggerType = "on_beam_tick"
	TriggerOnZoneTick       TriggerType = "on_zone_tick"
	TriggerOnZoneEnter      TriggerType = "on_zone_enter"
	TriggerOnZoneExit       TriggerType = "on_zone_exit"
	TriggerOnStatusTick     TriggerType = "on_status_tick"
	TriggerOnStatusExpire   TriggerType = "on_status_expire"
	TriggerOnDamageDealt    TriggerType = "on_damage_dealt"
	TriggerOnUnitDeath      TriggerType = "on_unit_death"
	TriggerOnActionComplete TriggerType = "on_action_complete"
	TriggerOnChargeFull     TriggerType = "on_charge_full"
	TriggerCustom           TriggerType = "custom"
)

// ActionType identifies the behavior an AbilityActionDef performs when its
// trigger fires.
type ActionType string

const (
	ActionSelectTargets ActionType = "select_targets"
	ActionStoreTargets  ActionType = "store_targets"
	ActionFilterTargets ActionType = "filter_targets"
	ActionDealDamage    ActionType = "deal_damage"
	ActionRestoreHealth ActionType = "restore_health"
	ActionApplyStatus   ActionType = "apply_status"
	ActionRemoveStatus  ActionType = "remove_status"
	ActionCreateZone    ActionType = "create_zone"
	// ActionLaunchProjectile also covers arcane_orb's moving pull+DoT vortex
	// shape (TravelMode "direction" + TickInterval > 0 — see
	// launchProjectileConfig's doc comment, ability_compile.go): a formerly
	// separate "launch_vortex" action type was retired in favor of this one,
	// since the vortex IS a "direction" travelMode projectile (no target
	// lock, no impact hit — it just never fires on_projectile_impact and
	// instead fires on_projectile_tick repeatedly while airborne).
	ActionLaunchProjectile ActionType = "launch_projectile"
	// ActionBeam spawns a beam between the caster (or a spawn origin) and a
	// resolved target. Its `channeled` config toggle selects one of two shapes:
	//   - Momentary (channeled=false): an instantaneous Beam visual that, a beat
	//     later (impactDelaySeconds), runs a nested on_beam_impact trigger —
	//     mirroring launch_projectile's on_projectile_impact seam. chain_lightning.
	//   - Channeled (channeled=true): hands off to the multi-tick channel
	//     lifecycle (ability_channel.go, startChannelLocked), persisting across
	//     many future ticks driven by Unit.Channel* state and running a nested
	//     on_beam_tick trigger each tick. siphon_life.
	// A channeled beam is ONLY valid as the channel-start action of a root
	// on_cast_complete trigger (channels can only START from the cast-begin
	// gating path — see ability_channel.go's "THE ORDERING DECISION"); the
	// validator rejects it anywhere else. See beamConfig (ability_exec_beam.go).
	ActionBeam ActionType = "beam"
	// ActionChargeFireVolley enqueues the staggered volley of a charge-fire
	// passive's on_charge_full trigger (arcane_missiles). Kept distinct from
	// every other action rather than folded into an existing one: its identity
	// is "given a unit that just crossed its charge threshold, queue N
	// staggered pending bolts" — it has no target lock at queue time (targets
	// are re-picked per bolt at LAUNCH time, well after this action returns),
	// no direct damage application, and its own trigger type (on_charge_full)
	// is unlike every cast-driven/zone-tick-driven action — see
	// spell_charge.go's file doc comment for the full design rationale.
	ActionChargeFireVolley  ActionType = "charge_fire_volley"
	ActionSummonUnit        ActionType = "summon_unit"
	ActionMoveUnit          ActionType = "move_unit"
	ActionApplyForce        ActionType = "apply_force"
	ActionModifyResource    ActionType = "modify_resource"
	ActionTriggerEvent      ActionType = "trigger_event"
	ActionPlayPresentation  ActionType = "play_presentation"
	ActionPlaySound         ActionType = "play_sound"
	ActionChangeRenderLayer ActionType = "change_render_layer"
	ActionCameraShake       ActionType = "camera_shake"
	ActionWait              ActionType = "wait"
	ActionConditional       ActionType = "conditional"
	ActionRepeat            ActionType = "repeat"
	// ActionSetContext writes a scalar (ctxScalar) into Named[key] — set to a
	// literal or add a delta to the current value. A general counter/tally
	// primitive; scalar conditions and value-references read it.
	ActionSetContext ActionType = "set_context"
	// ActionLoop is a WRAPPER: it runs its body (loopConfig.Body) once per
	// iteration, binding its Vars (a..z = start + step*iteration) so body number
	// fields can reference them by letter. Iterations are spaced over time by a
	// wait in the body — the loop runs one iteration to completion, then
	// schedules the next after the body's total wait (see runLoopIterationLocked
	// / the pendingLoop scheduler, ability_exec_loop.go). A body with no wait
	// runs all iterations in one tick. This is chain_lightning's shape: a loop
	// inside on_cast_complete whose body damages, picks the next target, arcs a
	// beam, and waits.
	ActionLoop   ActionType = "loop"
	ActionCustom ActionType = "custom"
)

// TargetSource identifies where a TargetQueryDef begins gathering candidates
// from.
type TargetSource string

const (
	SrcCaster            TargetSource = "caster"
	SrcInitialTarget     TargetSource = "initial_target"
	SrcPrevActionTargets TargetSource = "previous_action_targets"
	SrcCurrentEvent      TargetSource = "current_event"
	SrcNamedContext      TargetSource = "named_context"
	SrcSourceObject      TargetSource = "source_object"
	SrcAllInScene        TargetSource = "all_in_scene"
)

// TargetOrigin identifies the spatial anchor a TargetQueryDef searches
// around.
type TargetOrigin string

const (
	OriginCaster            TargetOrigin = "caster"
	OriginInitialTarget     TargetOrigin = "initial_target"
	OriginInitialTargetPos  TargetOrigin = "initial_target_position"
	OriginCastPoint         TargetOrigin = "cast_point"
	OriginImpactPosition    TargetOrigin = "impact_position"
	OriginCurrentEventPos   TargetOrigin = "current_event_position"
	OriginProjectilePos     TargetOrigin = "projectile_position"
	OriginZoneCenter        TargetOrigin = "zone_center"
	OriginStatusOwner       TargetOrigin = "status_owner"
	OriginSummonedUnit      TargetOrigin = "summoned_unit"
	OriginNamedContextValue TargetOrigin = "named_context_value"
	// OriginTargetsCenter is the CENTROID of the units an action is aimed at
	// (its resolved target set), not any single unit's point. It needs that
	// target list, so it's resolved in the launch_projectile / beam Execute
	// (see targetsCenterLocked), NOT in resolveOriginLocked — a query radius
	// origin has no meaningful "targets" to average, so it's offered only as a
	// spawn origin. Used for a bolt/beam that emanates from the middle of the
	// group it hits (e.g. Frost Bolt's secondary bolts).
	OriginTargetsCenter TargetOrigin = "targets_center"
)

// TargetOrdering determines the sort applied to candidates before a
// TargetQueryDef's MinCount/MaxCount are applied.
type TargetOrdering string

const (
	OrderClosest         TargetOrdering = "closest"
	OrderFarthest        TargetOrdering = "farthest"
	OrderLowestHealth    TargetOrdering = "lowest_health"
	OrderLowestHealthPct TargetOrdering = "lowest_health_percentage"
	OrderHighestHealth   TargetOrdering = "highest_health"
	OrderRandom          TargetOrdering = "random"
	OrderUnitID          TargetOrdering = "unit_id"
)

// AbilityProgram is the composable trigger/action definition of an ability.
// It is a pure data model in this phase: nothing executes it yet.
type AbilityProgram struct {
	Entry         AbilityEntryDef              `json:"entry"`
	Triggers      []AbilityTriggerDef          `json:"triggers"`
	NamedTriggers map[string]AbilityTriggerDef `json:"namedTriggers,omitempty"`
	Presentations []PresentationInstanceDef    `json:"presentations,omitempty"`
	// Remainder holds unknown program-level keys for round-trip safety.
	// Populated/emitted by the custom (Un)marshalers below.
	Remainder map[string]json.RawMessage `json:"-"`
}

// programAlias avoids infinite recursion in the custom (Un)marshalers below.
type programAlias AbilityProgram

// programKnownKeys are the JSON keys mapped to explicit AbilityProgram fields;
// anything else in the object is captured into Remainder for round-trip safety.
var programKnownKeys = []string{"entry", "triggers", "namedTriggers", "presentations"}

// UnmarshalJSON decodes an AbilityProgram, capturing any JSON object keys not
// mapped to a known field into Remainder so a newer schema round-trips
// through this version untouched.
func (p *AbilityProgram) UnmarshalJSON(b []byte) error {
	var base programAlias
	if err := json.Unmarshal(b, &base); err != nil {
		return err
	}
	*p = AbilityProgram(base)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	for _, known := range programKnownKeys {
		delete(raw, known)
	}
	if len(raw) > 0 {
		p.Remainder = raw
	}
	return nil
}

// MarshalJSON encodes an AbilityProgram, re-merging any keys captured in
// Remainder by UnmarshalJSON so unknown program-level keys survive a
// decode->encode round-trip. Remainder never shadows a known field.
func (p AbilityProgram) MarshalJSON() ([]byte, error) {
	out, err := json.Marshal(programAlias(p))
	if err != nil {
		return nil, err
	}
	if len(p.Remainder) == 0 {
		return out, nil
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(out, &merged); err != nil {
		return nil, err
	}
	for k, v := range p.Remainder {
		if _, exists := merged[k]; !exists { // never let Remainder shadow a real field
			merged[k] = v
		}
	}
	return json.Marshal(merged)
}

// AbilityEntryDef describes how an ability is initiated and what it can
// legally target at cast time.
type AbilityEntryDef struct {
	Type      AbilityEntryType `json:"type"`
	Relations []TargetRelation `json:"relations,omitempty"`
	Range     CastRange        `json:"range"`
}

// AbilityTriggerDef binds a TriggerType to the actions it runs, optionally
// gated by conditions and timing.
type AbilityTriggerDef struct {
	ID         string                `json:"id"`
	Name       string                `json:"name,omitempty"`
	Type       TriggerType           `json:"type"`
	Source     *ContextRef           `json:"source,omitempty"`
	Timing     *TriggerTiming        `json:"timing,omitempty"`
	Conditions []AbilityConditionDef `json:"conditions,omitempty"`
	Actions    []AbilityActionDef    `json:"actions"`
}

// LoopVar is one loop variable. At iteration k (0-based) it holds:
//   - StepMode "" / "number" (default): Start + Step*k   — additive.
//   - StepMode "percent": Start * (1 + Step/100)^k        — multiplicative,
//     compounding by Step% each iteration (Step -10 ⇒ ×0.9 per iteration).
// The value is rounded to the nearest integer (loop vars are discrete — damage,
// counts — and feed integer config fields). Name is a single lowercase letter
// "a".."z"; body number fields reference it by that letter. maxLoopVars caps the
// count at 26. Used by the `loop` action's config (loopConfig, ability_exec_loop.go).
type LoopVar struct {
	Name     string  `json:"name"`
	Start    float64 `json:"start"`
	Step     float64 `json:"step"`
	StepMode string  `json:"stepMode,omitempty"`
}

// TriggerTiming refines when a trigger fires relative to its event.
type TriggerTiming struct {
	Marker       string  `json:"marker,omitempty"`
	Frame        *int    `json:"frame,omitempty"`
	TickInterval float64 `json:"tickInterval,omitempty"`
	DelaySeconds float64 `json:"delaySeconds,omitempty"`
}

// AbilityActionDef is a single step run by a trigger.
type AbilityActionDef struct {
	ID          string     `json:"id"`
	Type        ActionType `json:"type"`
	DisplayName string     `json:"displayName,omitempty"`
	// Disabled turns an action off. The authoring default is enabled: an
	// omitted or false "disabled" key means the action runs; only an explicit
	// "disabled": true turns it off. (Inverted from a former Enabled bool,
	// which decoded absent->false and silently disabled hand-authored actions.)
	Disabled   bool                  `json:"disabled,omitempty"`
	Conditions []AbilityConditionDef `json:"conditions,omitempty"`
	Target     *TargetQueryDef       `json:"target,omitempty"`
	Input      map[string]ContextRef `json:"input,omitempty"`
	Outputs    map[string]string     `json:"outputs,omitempty"`
	// Config is action-specific config decoded by the action registry in a later
	// task. Kept as raw JSON so unknown sub-keys survive a round-trip untouched —
	// decoders READ fields from it; they must never re-marshal it back.
	Config json.RawMessage `json:"config,omitempty"`
	// Timing throttles THIS action to fire at most once every
	// Timing.TickInterval seconds, even while its enclosing trigger fires on
	// every simulation tick — e.g. arcane_orb's on_projectile_tick trigger
	// fires every dt (so select_targets/apply_force track the orb's live
	// position for pull accuracy), but its deal_damage action carries
	// Timing.TickInterval so the DoT still lands on a fixed cadence rather
	// than once per simulation tick (see fireProjectileTickLocked,
	// projectile.go, for the accumulator that drives this). nil/absent (every
	// action authored before this field existed) means "runs every time its
	// trigger fires" — the pre-existing, unthrottled behavior. Distinct from
	// AbilityTriggerDef.Timing (TriggerTiming), which times/delays the WHOLE
	// TRIGGER (e.g. on_animation_marker's Marker/DelaySeconds) — this throttles
	// one action inside a trigger that already fires unconditionally.
	Timing *ActionTiming `json:"timing,omitempty"`
	// Children are follow-up / nested triggers (e.g. on_action_complete).
	Children []AbilityTriggerDef `json:"children,omitempty"`
}

// ActionTiming is the per-action throttle attached via AbilityActionDef.Timing.
// A single-field struct today (mirrors TriggerTiming's shape) so a future
// per-action delay/frame-gate can be added the same way TriggerTiming grew
// its own fields, without another migration of every existing caller.
type ActionTiming struct {
	// TickInterval, when > 0, gates the action to run at most once every this
	// many seconds — see AbilityActionDef.Timing's doc comment. The
	// accumulator that tracks elapsed time toward the next due firing lives on
	// the runtime object driving the trigger (e.g. Projectile.TickActionTimers,
	// keyed by this action's ID), never on this struct — this is pure,
	// stateless authored data.
	TickInterval float64 `json:"tickInterval,omitempty"`
}

// IsEnabled reports whether the action runs. It is the inverse of Disabled so
// the authoring default (an omitted "disabled" key) is enabled.
func (a AbilityActionDef) IsEnabled() bool { return !a.Disabled }

// ContextRef is a named lookup key into the runtime execution context (e.g.
// a stored target set, an event field). Resolution is implemented in a
// later task.
type ContextRef struct {
	Key string `json:"key"`
}

// AbilityConditionDef is a single boolean check evaluated against the
// runtime execution context.
type AbilityConditionDef struct {
	Type  ConditionType   `json:"type"`
	Left  ContextRef      `json:"left"`
	Op    string          `json:"op"`
	Right json.RawMessage `json:"right,omitempty"`
}

// ConditionType identifies the kind of check an AbilityConditionDef
// performs. Concrete values are introduced in a later task.
type ConditionType string

// ZoneAnchor identifies what a ZoneDef's position is relative to.
type ZoneAnchor string

const (
	ZoneAnchorGround ZoneAnchor = "ground"
	ZoneAnchorUnit   ZoneAnchor = "unit"
	ZoneAnchorObject ZoneAnchor = "object"
)

// ZoneDef describes a persistent area-of-effect spawned by an action.
type ZoneDef struct {
	Name          string              `json:"name,omitempty"`
	PositionRef   ContextRef          `json:"position"`
	Anchor        ZoneAnchor          `json:"anchor"`
	FollowsAnchor bool                `json:"followsAnchor,omitempty"`
	Radius        float64             `json:"radius"`
	Duration      float64             `json:"duration"`
	TickInterval  float64             `json:"tickInterval,omitempty"`
	OwnerRef      ContextRef          `json:"owner"`
	Presentation  string              `json:"presentation,omitempty"`
	Triggers      []AbilityTriggerDef `json:"triggers,omitempty"`
}

// StatusDef describes a buff/debuff applied to a unit by an action.
type StatusDef struct {
	Name         string              `json:"name,omitempty"`
	TargetRef    ContextRef          `json:"target"`
	Duration     float64             `json:"duration"`
	TickInterval float64             `json:"tickInterval,omitempty"`
	Stacking     string              `json:"stacking,omitempty"`
	MaxStacks    int                 `json:"maxStacks,omitempty"`
	SourceRef    ContextRef          `json:"source"`
	Presentation string              `json:"presentation,omitempty"`
	Triggers     []AbilityTriggerDef `json:"triggers,omitempty"`
}

// ProjectileSpawnDef describes a projectile launched by an action.
type ProjectileSpawnDef struct {
	SourceRef    ContextRef          `json:"source"`
	DestRef      ContextRef          `json:"destination"`
	ProjectileID string              `json:"projectile,omitempty"`
	Speed        float64             `json:"speed,omitempty"`
	Piercing     bool                `json:"piercing,omitempty"`
	Presentation string              `json:"presentation,omitempty"`
	Triggers     []AbilityTriggerDef `json:"triggers,omitempty"`
}

// PresentationInstanceDef describes a single visual/audio effect instance
// attached to a position or object.
type PresentationInstanceDef struct {
	ID          string              `json:"id"`
	Asset       string              `json:"asset"`
	PositionRef ContextRef          `json:"position"`
	AttachRef   *ContextRef         `json:"attach,omitempty"`
	Scale       float64             `json:"scale,omitempty"`
	RenderLayer string              `json:"renderLayer,omitempty"`
	Animation   string              `json:"animation,omitempty"`
	Triggers    []AbilityTriggerDef `json:"triggers,omitempty"`
}

// TargetQueryDef describes how an action gathers, filters, orders, and
// limits its candidate targets.
type TargetQueryDef struct {
	Source               TargetSource     `json:"source"`
	Origin               TargetOrigin     `json:"origin,omitempty"`
	OriginRef            *ContextRef      `json:"originRef,omitempty"`
	Relations            []TargetRelation `json:"relations,omitempty"`
	Filters              []TargetFilter   `json:"filters,omitempty"`
	Radius               float64          `json:"radius,omitempty"`
	MinCount             int              `json:"minCount,omitempty"`
	MaxCount             int              `json:"maxCount,omitempty"`
	Ordering             TargetOrdering   `json:"ordering,omitempty"`
	IncludeInitialTarget bool             `json:"includeInitialTarget,omitempty"`
	ExcludeSource        bool             `json:"excludeSource,omitempty"`
	// ExcludeCurrentEvent drops the "current_event" unit (ctx.CurrentEventUnitID
	// — the unit a trigger's event centers on, e.g. the enemy a projectile just
	// hit) from this query's results, the same way ExcludeSource drops the
	// caster. Deliberately a SEPARATE bool rather than folding into
	// ExcludeSource or widening ExcludeSource's meaning: ExcludeSource is
	// already on the wire, in the TS mirror, in the editor UI, and in shipped
	// authored programs, all with the fixed meaning "drop the caster" —
	// changing what it excludes would be a silent migration for every existing
	// consumer. This is needed for a "hit an enemy, then splash to OTHER
	// nearby enemies" query (all_in_scene, origin: current_event_position):
	// without it, the current-event unit is its own enemy at distance 0 from
	// itself and is always included. Zero value (false, the default for every
	// query authored before this field existed) is a no-op — current_event is
	// included exactly like before. Only meaningful when the executing
	// trigger actually bound a current-event unit (ctx.CurrentEventUnitID !=
	// 0, e.g. on_projectile_impact/on_zone_enter/on_zone_exit); a query run
	// from a context with no bound current-event unit is unaffected.
	ExcludeCurrentEvent bool `json:"excludeCurrentEvent,omitempty"`
	// ExcludeRef drops every unit whose ID is present in the named ctxUnitSet
	// ExcludeRef.Key — e.g. a chain that must not re-hit already-struck
	// victims. No-op when the key is absent or not a unit-set.
	ExcludeRef         *ContextRef `json:"excludeRef,omitempty"`
	RequireLineOfSight bool        `json:"requireLineOfSight,omitempty"`
	AliveState         string      `json:"aliveState,omitempty"`
}

// TargetFilter is a placeholder for a richer unit/object filter (defined further
// in a later task). A key + optional value covers the current authoring needs.
type TargetFilter struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value,omitempty"`
}
