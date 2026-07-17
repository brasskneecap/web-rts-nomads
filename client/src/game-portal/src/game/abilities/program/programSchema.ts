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
  | 'nested_triggers'
  | (string & {})

// SchemaField describes a single editable property of an action config.
export interface SchemaField {
  key: string
  label: string
  control: ControlType
  options?: string[]
  section?: string
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
