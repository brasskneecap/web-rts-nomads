// TypeScript mirror of the Go composable ability program model
// (server/internal/game/ability_program.go). Kept as a pure data model —
// nothing executes it client-side; the client renders server-driven state.
//
// Forward-compat mirrors the Go side's Remainder mechanism: parseProgram /
// serializeProgram round-trip unknown top-level keys through `__remainder`
// so a client running against an older schema doesn't drop fields a newer
// server (or another client) authored. `AbilityActionDef.config` is treated
// as a fully opaque bag for the same reason — see its doc comment below.

// AbilityEntryType mirrors Go's AbilityEntryType enum.
export type AbilityEntryType =
  | 'self'
  | 'unit'
  | 'ground_point'
  | 'direction'
  | 'no_target'
  | 'passive'

// TargetRelation mirrors Go's TargetRelation enum.
export type TargetRelation = 'self' | 'ally' | 'enemy' | 'neutral'

// TriggerType mirrors Go's TriggerType enum. The `| (string & {})` escape
// hatch keeps the type open so an unrecognized value from a newer server
// doesn't fail to type-check — it just isn't one of the known literals.
export type TriggerType =
  | 'on_cast_start'
  | 'on_cast_complete'
  | 'on_animation_marker'
  | 'on_projectile_impact'
  | 'on_zone_tick'
  | 'on_zone_enter'
  | 'on_zone_exit'
  | 'on_status_tick'
  | 'on_status_expire'
  | 'on_target_hit'
  | 'on_damage_dealt'
  | 'on_unit_death'
  | 'on_action_complete'
  | 'on_charge_full'
  | 'custom'
  | (string & {})

// ActionType mirrors Go's ActionType enum. Same forward-compat escape hatch
// as TriggerType.
export type ActionType =
  | 'select_targets'
  | 'store_targets'
  | 'filter_targets'
  | 'deal_damage'
  | 'restore_health'
  | 'apply_status'
  | 'remove_status'
  | 'create_zone'
  | 'launch_projectile'
  | 'summon_unit'
  | 'move_unit'
  | 'apply_force'
  | 'modify_resource'
  | 'trigger_event'
  | 'play_presentation'
  | 'play_sound'
  | 'change_render_layer'
  | 'camera_shake'
  | 'wait'
  | 'conditional'
  | 'repeat'
  | 'custom'
  | (string & {})

// TargetSource mirrors Go's TargetSource enum.
export type TargetSource =
  | 'caster'
  | 'initial_target'
  | 'previous_action_targets'
  | 'current_event'
  | 'named_context'
  | 'source_object'
  | 'all_in_scene'
  | (string & {})

// TargetOrigin mirrors Go's TargetOrigin enum.
export type TargetOrigin =
  | 'caster'
  | 'initial_target'
  | 'initial_target_position'
  | 'cast_point'
  | 'impact_position'
  | 'current_event_position'
  | 'projectile_position'
  | 'zone_center'
  | 'status_owner'
  | 'summoned_unit'
  | 'named_context_value'
  | (string & {})

// TargetOrdering mirrors Go's TargetOrdering enum.
export type TargetOrdering =
  | 'closest'
  | 'farthest'
  | 'lowest_health'
  | 'lowest_health_percentage'
  | 'highest_health'
  | 'random'
  | 'unit_id'
  | (string & {})

// ZoneAnchor mirrors Go's ZoneAnchor enum.
export type ZoneAnchor = 'ground' | 'unit' | 'object' | (string & {})

// ConditionType mirrors Go's ConditionType — concrete values are introduced
// in a later task server-side, so this stays a free string for now.
export type ConditionType = string

// ContextRef is a named lookup key into the runtime execution context.
export interface ContextRef {
  key: string
}

// AbilityEntryDef describes how an ability is initiated and what it can
// legally target at cast time. `range` is polymorphic — a world-pixel
// number or the sentinel string — matching the `castRange` precedent in
// abilityEditorForm.ts.
export interface AbilityEntryDef {
  type: AbilityEntryType
  relations?: TargetRelation[]
  range: number | 'match_attack_range'
}

// TriggerTiming refines when a trigger fires relative to its event.
export interface TriggerTiming {
  marker?: string
  frame?: number
  tickInterval?: number
  delaySeconds?: number
}

// TargetFilter is a placeholder for a richer unit/object filter (defined
// further in a later task). A key + optional value covers current authoring
// needs; value is opaque JSON.
export interface TargetFilter {
  key: string
  value?: unknown
}

// TargetQueryDef describes how an action gathers, filters, orders, and
// limits its candidate targets.
export interface TargetQueryDef {
  source: TargetSource
  origin?: TargetOrigin
  originRef?: ContextRef
  relations?: TargetRelation[]
  filters?: TargetFilter[]
  radius?: number
  minCount?: number
  maxCount?: number
  ordering?: TargetOrdering
  includeInitialTarget?: boolean
  excludeSource?: boolean
  requireLineOfSight?: boolean
  aliveState?: string
}

// AbilityConditionDef is a single boolean check evaluated against the
// runtime execution context.
export interface AbilityConditionDef {
  type: ConditionType
  left: ContextRef
  op: string
  right?: unknown
}

// AbilityActionDef is a single step run by a trigger.
export interface AbilityActionDef {
  id: string
  type: ActionType
  displayName?: string
  // disabled turns the action off. The authoring default is enabled: an
  // omitted or false `disabled` means the action runs; only `disabled: true`
  // turns it off. Mirrors Go's AbilityActionDef.Disabled (json "disabled,omitempty").
  disabled?: boolean
  conditions?: AbilityConditionDef[]
  target?: TargetQueryDef
  input?: Record<string, ContextRef>
  outputs?: Record<string, string>
  // config: action-specific config decoded by the action registry in a
  // later task. Kept as an OPAQUE bag — NEVER destructure it. Readers pull
  // individual fields out; nothing re-marshals it, so unknown sub-keys
  // survive a parse -> serialize round-trip untouched (mirrors Go's
  // json.RawMessage Config field).
  config?: Record<string, unknown>
  // children: follow-up / nested triggers (e.g. on_action_complete).
  children?: AbilityTriggerDef[]
}

// AbilityTriggerDef binds a TriggerType to the actions it runs, optionally
// gated by conditions and timing.
export interface AbilityTriggerDef {
  id: string
  name?: string
  type: TriggerType
  source?: ContextRef
  timing?: TriggerTiming
  conditions?: AbilityConditionDef[]
  actions: AbilityActionDef[]
}

// ZoneDef describes a persistent area-of-effect spawned by an action.
export interface ZoneDef {
  name?: string
  position: ContextRef
  anchor: ZoneAnchor
  followsAnchor?: boolean
  radius: number
  duration: number
  tickInterval?: number
  owner: ContextRef
  presentation?: string
  triggers?: AbilityTriggerDef[]
}

// StatusDef describes a buff/debuff applied to a unit by an action.
export interface StatusDef {
  name?: string
  target: ContextRef
  duration: number
  tickInterval?: number
  stacking?: string
  maxStacks?: number
  source: ContextRef
  presentation?: string
  triggers?: AbilityTriggerDef[]
}

// ProjectileSpawnDef describes a projectile launched by an action.
export interface ProjectileSpawnDef {
  source: ContextRef
  destination: ContextRef
  projectile?: string
  speed?: number
  piercing?: boolean
  presentation?: string
  triggers?: AbilityTriggerDef[]
}

// PresentationInstanceDef describes a single visual/audio effect instance
// attached to a position or object.
export interface PresentationInstanceDef {
  id: string
  asset: string
  position: ContextRef
  attach?: ContextRef
  scale?: number
  renderLayer?: string
  animation?: string
  triggers?: AbilityTriggerDef[]
}

// AbilityProgram is the composable trigger/action definition of an ability.
// It is a pure data model in this phase: nothing executes it yet.
export interface AbilityProgram {
  entry: AbilityEntryDef
  triggers: AbilityTriggerDef[]
  namedTriggers?: Record<string, AbilityTriggerDef>
  presentations?: PresentationInstanceDef[]
  // __remainder holds unknown top-level program keys for round-trip safety.
  // Populated by parseProgram, re-merged (without shadowing known fields)
  // by serializeProgram. Mirrors Go's AbilityProgram.Remainder.
  __remainder?: Record<string, unknown>
}

// programKnownKeys are the JSON keys mapped to explicit AbilityProgram
// fields; anything else in the object is captured into __remainder for
// round-trip safety. Mirrors Go's programKnownKeys.
const PROGRAM_KNOWN_KEYS = ['entry', 'triggers', 'namedTriggers', 'presentations'] as const

// parseProgram decodes a raw (already JSON.parsed) value into an
// AbilityProgram, capturing any object keys not mapped to a known field
// into __remainder so a newer schema round-trips through this version
// untouched. triggers/actions/config are copied verbatim — no sub-key
// stripping — so unknown nested keys survive automatically.
export function parseProgram(raw: unknown): AbilityProgram {
  const src = (raw ?? {}) as Record<string, unknown>
  const remainder: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(src)) {
    if (!(PROGRAM_KNOWN_KEYS as readonly string[]).includes(k)) remainder[k] = v
  }
  const prog: AbilityProgram = {
    entry: src.entry as AbilityEntryDef,
    triggers: (src.triggers as AbilityTriggerDef[]) ?? [],
  }
  if (src.namedTriggers !== undefined) {
    prog.namedTriggers = src.namedTriggers as Record<string, AbilityTriggerDef>
  }
  if (src.presentations !== undefined) {
    prog.presentations = src.presentations as PresentationInstanceDef[]
  }
  if (Object.keys(remainder).length > 0) prog.__remainder = remainder
  return prog
}

// serializeProgram encodes an AbilityProgram back into a plain object,
// re-merging any keys captured in __remainder without letting a remainder
// key shadow a known field. Omits __remainder itself from the output.
// Mirrors Go's AbilityProgram.MarshalJSON.
export function serializeProgram(prog: AbilityProgram): Record<string, unknown> {
  const out: Record<string, unknown> = {
    entry: prog.entry,
    triggers: prog.triggers,
  }
  if (prog.namedTriggers !== undefined) out.namedTriggers = prog.namedTriggers
  if (prog.presentations !== undefined) out.presentations = prog.presentations
  if (prog.__remainder) {
    for (const [k, v] of Object.entries(prog.__remainder)) {
      if (!(k in out)) out[k] = v // never let remainder shadow a known field
    }
  }
  return out
}
