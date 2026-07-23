// TypeScript mirror of the Go action-schema catalog served by
// GET /catalog/action-schema (server/internal/game — Phase 5a T1-T4). Drives
// the Phase 5b editor's field rendering: each action type declares the
// SchemaFields it needs and whether it is currently runnable server-side.

// ControlType mirrors the Go schema field "control" enum. The
// `| (string & {})` escape hatch keeps the type open so an unrecognized
// control from a newer server doesn't fail to type-check.
export type ControlType =
  | 'number'
  | 'text'
  | 'boolean'
  | 'enum'
  | 'multiselect'
  | 'asset'
  | 'sentinel_number'
  | 'duration'
  | 'percentage'
  | 'target_query'
  | 'context_ref'
  | 'animation_marker'
  | 'animation'
  | 'nested_triggers'
  | (string & {})

// FieldCondition mirrors the Go `FieldCondition` (server/internal/game/
// ability_program_registry.go): "show this field only when the action's own
// config[key] <op> value". `op` is left open the same way ControlType is —
// the server's own vocabulary is eq|ne|lt|lte|gt|gte, but an unrecognized op
// must fail closed (see fieldConditionMatches), never throw or type-fail.
export interface FieldCondition {
  key: string
  op: 'eq' | 'ne' | 'lt' | 'lte' | 'gt' | 'gte' | (string & {})
  value?: unknown
}

// SchemaField describes a single editable property of an action config.
//
// `targetQueryFields` is ONLY meaningful on a field whose `control` is
// "target_query" — it names, in display order, which TargetQueryDef
// sub-fields THIS action's targeting shape actually uses (of the 10 the
// server currently enforces). A `target_query` field with no
// `targetQueryFields` (schema not yet updated, or a forward-compat gap)
// falls back to the full set in TargetQueryEditor rather than rendering
// nothing.
//
// `showWhen` is a conditional-visibility gate evaluated against the SAME
// action's own config via `fieldConditionMatches` below. `undefined` means
// "always shown" — every field declared before this mechanism existed keeps
// that behavior for free.
export interface SchemaField {
  key: string
  label: string
  control: ControlType
  options?: string[]
  section?: string
  targetQueryFields?: string[]
  showWhen?: FieldCondition
}

// fieldConditionMatches is the client-side mirror of the Go reference
// implementation `FieldConditionMatches` (ability_program_registry.go) — it
// must match that function's semantics EXACTLY, since the server is the
// authoritative source of truth for what an author's authored config
// actually gates:
//
//   - A `key` absent from `config` resolves to the ZERO VALUE OF `value`'s
//     OWN TYPE (0 / '' / false), not "unevaluable". This is what makes an
//     untouched `omitempty` field (e.g. launch_projectile's `chainCount`
//     before it's ever set, or `travelMode` before it's ever authored away
//     from its "to_target" default) gate identically to one explicitly
//     authored as that zero value.
//   - `eq`/`ne` are generic JSON-scalar equality (number/string/boolean/
//     null) — needed for both a string gate (travelMode === "direction")
//     and a numeric one (chainCount > 0) through the same mechanism.
//   - `lt`/`lte`/`gt`/`gte` are numeric-only.
//   - A non-scalar `value` or `config[key]` (object/array), a type
//     mismatch, or an unrecognized `op` all conservatively resolve to
//     `false` (hide the field) — this function must NEVER throw, no matter
//     what a newer/older server sends.
export function fieldConditionMatches(cond: FieldCondition, config: Record<string, unknown>): boolean {
  const want = cond.value
  let got: unknown = config[cond.key]
  if (!(cond.key in config)) {
    if (typeof want === 'string') got = ''
    else if (typeof want === 'boolean') got = false
    else got = 0
  }

  switch (cond.op) {
    case 'eq': {
      const { comparable, equal } = scalarEqual(got, want)
      return comparable && equal
    }
    case 'ne': {
      const { comparable, equal } = scalarEqual(got, want)
      return comparable && !equal
    }
    case 'lt':
    case 'lte':
    case 'gt':
    case 'gte': {
      if (typeof got !== 'number' || typeof want !== 'number') return false
      switch (cond.op) {
        case 'lt':
          return got < want
        case 'lte':
          return got <= want
        case 'gt':
          return got > want
        default: // 'gte'
          return got >= want
      }
    }
    default:
      return false
  }
}

// isJsonScalar / scalarEqual: guards against ever running `===` on an
// object/array decoded from a malformed or unexpected `showWhen.value` /
// `config[key]` — those aren't meaningfully comparable here, so they report
// `comparable: false` rather than risking a wrong-but-truthy result (they
// can't throw the way a bare `==` on incomparable Go interface values can,
// but the "conservatively not comparable" contract is the same).
function isJsonScalar(v: unknown): v is null | boolean | number | string {
  return v === null || typeof v === 'boolean' || typeof v === 'number' || typeof v === 'string'
}

function scalarEqual(a: unknown, b: unknown): { equal: boolean; comparable: boolean } {
  if (!isJsonScalar(a) || !isJsonScalar(b)) return { equal: false, comparable: false }
  return { equal: a === b, comparable: true }
}

// fieldVisible reports whether `field` should be shown in the inspector for
// the given action config. `undefined` `showWhen` -> always visible.
export function fieldVisible(field: SchemaField, config: Record<string, unknown> | undefined): boolean {
  if (!field.showWhen) return true
  return fieldConditionMatches(field.showWhen, config ?? {})
}

// ActionSchema describes one action type: its editable fields and whether
// the action registry actually executes it (vs. being defined but not yet
// runnable server-side).
export interface ActionSchema {
  type: string
  fields: SchemaField[]
  runnable: boolean
}

// ProgramEnums holds the named enum value lists the server exposes
// (entryTypes, relations, triggerTypes, actionTypes, targetSources,
// targetOrigins, targetOrderings, zoneAnchors, conditionOps).
export type ProgramEnums = Record<string, string[]>

// ActionSchemaBundle is the parsed response of GET /catalog/action-schema.
export interface ActionSchemaBundle {
  actions: ActionSchema[]
  enums: ProgramEnums
}

// parseActionSchemaResponse defensively shapes the raw JSON body into an
// ActionSchemaBundle: missing `fields` become an empty array, missing/falsy
// `runnable` becomes false, and a missing `enums` becomes an empty object.
export function parseActionSchemaResponse(raw: unknown): ActionSchemaBundle {
  const src = (raw ?? {}) as { actions?: unknown; enums?: unknown }
  const rawActions = Array.isArray(src.actions) ? src.actions : []
  const actions: ActionSchema[] = rawActions.map((entry) => {
    const e = (entry ?? {}) as { type?: unknown; fields?: unknown; runnable?: unknown }
    return {
      type: String(e.type ?? ''),
      fields: (e.fields as SchemaField[] | undefined) ?? [],
      runnable: !!e.runnable,
    }
  })
  const enums = (src.enums as ProgramEnums | undefined) ?? {}
  return { actions, enums }
}

// schemaForAction finds the ActionSchema for a given action type, or
// undefined if the server doesn't (yet) describe that type.
export function schemaForAction(bundle: ActionSchemaBundle, type: string): ActionSchema | undefined {
  return bundle.actions.find((a) => a.type === type)
}
