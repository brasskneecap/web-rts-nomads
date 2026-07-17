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
	TriggerOnZoneTick         TriggerType = "on_zone_tick"
	TriggerOnZoneEnter        TriggerType = "on_zone_enter"
	TriggerOnZoneExit         TriggerType = "on_zone_exit"
	TriggerOnStatusTick       TriggerType = "on_status_tick"
	TriggerOnStatusExpire     TriggerType = "on_status_expire"
	TriggerOnTargetHit        TriggerType = "on_target_hit"
	TriggerOnDamageDealt      TriggerType = "on_damage_dealt"
	TriggerOnUnitDeath        TriggerType = "on_unit_death"
	TriggerOnActionComplete   TriggerType = "on_action_complete"
	TriggerOnChargeFull       TriggerType = "on_charge_full"
	TriggerCustom             TriggerType = "custom"
)

// ActionType identifies the behavior an AbilityActionDef performs when its
// trigger fires.
type ActionType string

const (
	ActionSelectTargets    ActionType = "select_targets"
	ActionStoreTargets     ActionType = "store_targets"
	ActionFilterTargets    ActionType = "filter_targets"
	ActionDealDamage       ActionType = "deal_damage"
	ActionRestoreHealth    ActionType = "restore_health"
	ActionApplyStatus      ActionType = "apply_status"
	ActionRemoveStatus     ActionType = "remove_status"
	ActionCreateZone       ActionType = "create_zone"
	ActionLaunchProjectile ActionType = "launch_projectile"
	// ActionLaunchVortex spawns a traveling, non-impacting pull+DoT vortex
	// (arcane_orb's moving orb). Kept distinct from ActionLaunchProjectile
	// rather than folding vortex fields into that action's config: a
	// launch_projectile action's identity is "home in on a target and deal
	// impact damage" (optionally chaining) — the orb does neither (no target
	// lock, no impact hit, damage instead ticks on a fixed cadence to
	// whatever is in its radius as it travels) — see ability_exec_vortex.go's
	// file doc comment for the full design rationale.
	ActionLaunchVortex ActionType = "launch_vortex"
	// ActionChargeFireVolley enqueues the staggered volley of a charge-fire
	// passive's on_charge_full trigger (arcane_missiles). Kept distinct from
	// every other action rather than folded into an existing one: its identity
	// is "given a unit that just crossed its charge threshold, queue N
	// staggered pending bolts" — it has no target lock at queue time (targets
	// are re-picked per bolt at LAUNCH time, well after this action returns),
	// no direct damage application, and its own trigger type (on_charge_full)
	// is unlike every cast-driven/zone-tick-driven action — see
	// spell_charge.go's file doc comment for the full design rationale.
	ActionChargeFireVolley ActionType = "charge_fire_volley"
	// ActionChannelBeam starts a channeled beam (siphon_life's per-tick
	// drain/heal loop). Kept distinct from every other action rather than
	// folded into an existing one: its identity is "given a validated
	// caster+target, hand off to the multi-tick channel lifecycle
	// (ability_channel.go) instead of resolving once" — unlike deal_damage/
	// restore_health it has no single amount to apply, and unlike
	// launch_projectile/launch_vortex it persists across many future ticks
	// driven entirely by Unit.Channel* state, not by this action itself —
	// see ability_exec_channel.go's file doc comment for the full design
	// rationale.
	ActionChannelBeam       ActionType = "channel_beam"
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
	ActionCustom            ActionType = "custom"
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
	// Children are follow-up / nested triggers (e.g. on_action_complete).
	Children []AbilityTriggerDef `json:"children,omitempty"`
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
	RequireLineOfSight   bool             `json:"requireLineOfSight,omitempty"`
	AliveState           string           `json:"aliveState,omitempty"`
}

// TargetFilter is a placeholder for a richer unit/object filter (defined further
// in a later task). A key + optional value covers the current authoring needs.
type TargetFilter struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value,omitempty"`
}
