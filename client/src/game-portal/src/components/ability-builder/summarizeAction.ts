// summarizeAction: a pure, best-effort one-line human summary of an
// AbilityActionDef, for the Flow view's compact action cards. Never throws —
// `action.config` is an opaque bag (see AbilityActionDef's doc comment in
// abilityProgram.ts) that may be undefined or missing any given key, so every
// field read here is guarded.

import type { AbilityActionDef, AbilityConditionDef } from '@/game/abilities/program/abilityProgram'
import type { ActionSchemaBundle } from '@/game/abilities/program/programSchema'
// PERK_DEF_MAP is populated by GameClient once a match session has loaded the
// perk catalog (see perkDefs.ts). The ability editor doesn't fetch the perk
// catalog itself, so this is a best-effort lookup: when it's empty (no match
// has loaded in this tab yet) perkLabel below falls back to a humanized id
// rather than this module adding a fetch of its own just for one summary line.
import { PERK_DEF_MAP } from '@/game/maps/perkDefs'

// Display-name overrides for action types whose plain title-cased id doesn't
// read the way we want in the UI. The wire type is UNCHANGED (store_targets
// stays store_targets); only the label differs. "Save Targets" pairs with the
// "Saved Value" picker that reads the saved set back.
const ACTION_TYPE_LABELS: Record<string, string> = {
  store_targets: 'Save Targets',
  // Named in full, matching the action type. The old "Apply Duration" shorthand
  // became ambiguous once ability stats could scope a duration bonus per action:
  // an author reading "Status Duration +2s" in the unit editor has to be able to
  // find the action it targets, and the shorthand no longer named its own stat.
  apply_status_duration: 'Apply Status Duration',
  // Trigger moments, named the way a container reads: On Duration Tick (the
  // generic on_tick), On Expire (on_status_expire). On Apply (on_action_complete)
  // is NOT overridden — it's a GENERIC trigger reused as any action's child, so
  // a global "On Apply" would mislabel it elsewhere.
  on_tick: 'On Duration Tick',
  on_status_expire: 'On Expire',
}

// humanizeActionType turns a snake_case action type id into a Title Case
// label, e.g. "deal_damage" -> "Deal Damage" (with a few overrides above).
// Unknown/empty types fall back to the raw string so the card never renders
// blank. Also reused for trigger types (FlowTriggerCard / AbilityFlow) — hence
// the on_tick / on_status_expire trigger-type overrides above.
export function humanizeActionType(type: string): string {
  if (!type) return ''
  if (ACTION_TYPE_LABELS[type]) return ACTION_TYPE_LABELS[type]
  return type
    .split('_')
    .filter(Boolean)
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(' ')
}

function asString(v: unknown): string | undefined {
  return typeof v === 'string' && v.length > 0 ? v : undefined
}

function asNumber(v: unknown): number | undefined {
  return typeof v === 'number' && Number.isFinite(v) ? v : undefined
}

// detailFor computes the best-effort "— detail" suffix for known action
// types. Returns '' when there's nothing worth showing (unknown type, or a
// known type with no usable config/target fields yet).
function detailFor(action: AbilityActionDef): string {
  const config = action.config ?? {}
  switch (action.type) {
    case 'deal_damage': {
      const amount = asNumber(config.amount)
      const type = asString(config.type)
      const parts = [amount, type].filter(Boolean)
      return parts.length ? parts.join(' ') : ''
    }
    case 'restore_health': {
      const amount = asNumber(config.amount)
      return amount == null ? '' : String(amount)
    }
    case 'select_targets': {
      const relations = action.target?.relations
      const radius = action.target?.radius
      const relPart = Array.isArray(relations) && relations.length ? relations.join('/') : undefined
      const radiusPart = typeof radius === 'number' ? `within ${radius}` : undefined
      return [relPart, radiusPart].filter(Boolean).join(' ')
    }
    case 'store_targets': {
      // The saved-selection name (+ a "merge" note when it accumulates).
      const as = asString(config.as)
      if (as == null) return ''
      return config.merge === true ? `${as} (merge)` : as
    }
    case 'create_zone': {
      const name = asString(config.name)
      return name ?? ''
    }
    case 'summon_unit': {
      const count = asNumber(config.count)
      const unitType = asString(config.unitType)
      if (unitType == null && count == null) return ''
      return `${count ?? '1'}× ${unitType ?? 'unit'}`
    }
    case 'apply_status': {
      const status = asString(config.status)
      return status ?? ''
    }
    case 'conditional': {
      const conditions = Array.isArray(config.conditions) ? (config.conditions as AbilityConditionDef[]) : []
      return conditions.map(summarizeCondition).join(' & ')
    }
    default:
      return ''
  }
}

// CONDITION_OP_SYMBOLS covers the scalar-comparison operators evaluated by
// evaluateOneConditionLocked's "eq"/"ne"/... case (ability_exec_flow.go) —
// everything except the perk operators and has/not, which read better as
// words than symbols (see summarizeCondition below).
const CONDITION_OP_SYMBOLS: Record<string, string> = {
  eq: '==',
  ne: '!=',
  lt: '<',
  lte: '<=',
  gt: '>',
  gte: '>=',
}

// perkLabel resolves a perk id to its catalog display name via PERK_DEF_MAP
// (see this module's import comment for why that map may be empty here), or
// a title-cased fallback of the id when it isn't found — never a blank/raw
// id, so the card always reads as prose.
function perkLabel(perkId: string): string {
  return PERK_DEF_MAP.get(perkId)?.displayName ?? humanizeActionType(perkId)
}

// summarizeCondition renders one AbilityConditionDef as a short human phrase.
// Mirrors evaluateOneConditionLocked's operator set (ability_exec_flow.go):
// the perk operators name the perk directly (the whole point of has_perk/
// not_perk — see that function's doc comment), has/not read a named-context
// key's presence, and the scalar comparisons read as "<key> <symbol> <right>"
// (e.g. "selected_count >= 2"). An operator this function doesn't recognize
// still renders as its raw op string rather than vanishing silently.
function summarizeCondition(c: AbilityConditionDef): string {
  const right = c.right
  switch (c.op) {
    case 'has_perk':
      return typeof right === 'string' && right ? `has perk: ${perkLabel(right)}` : 'has perk'
    case 'not_perk':
      return typeof right === 'string' && right ? `missing perk: ${perkLabel(right)}` : 'missing perk'
    case 'has':
      return `${c.left?.key ?? '?'} is set`
    case 'not':
      return `${c.left?.key ?? '?'} is unset`
    default: {
      const symbol = CONDITION_OP_SYMBOLS[c.op]
      if (!symbol) return c.op
      const rightStr = typeof right === 'string' ? right : right === undefined ? '?' : JSON.stringify(right)
      return `${c.left?.key ?? '?'} ${symbol} ${rightStr}`
    }
  }
}

// summarizeAction renders `<Label>` or `<Label> — <detail>` for known action
// types with a usable detail, and just `<Label>` otherwise. `schema` is
// accepted (and reserved) for future field-aware summaries but not currently
// consulted — the switch above is a fixed, hand-picked set of the most
// common action types per the Flow view spec.
export function summarizeAction(
  action: AbilityActionDef,
  _schema: ActionSchemaBundle | null,
): string {
  const label = humanizeActionType(action.type)
  const detail = detailFor(action)
  return detail ? `${label} — ${detail}` : label
}
