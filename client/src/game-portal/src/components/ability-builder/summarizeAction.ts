// summarizeAction: a pure, best-effort one-line human summary of an
// AbilityActionDef, for the Flow view's compact action cards. Never throws —
// `action.config` is an opaque bag (see AbilityActionDef's doc comment in
// abilityProgram.ts) that may be undefined or missing any given key, so every
// field read here is guarded.

import type { AbilityActionDef } from '@/game/abilities/program/abilityProgram'
import type { ActionSchemaBundle } from '@/game/abilities/program/programSchema'

// humanizeActionType turns a snake_case action type id into a Title Case
// label, e.g. "deal_damage" -> "Deal Damage". Unknown/empty types fall back
// to the raw string so the card never renders blank.
export function humanizeActionType(type: string): string {
  if (!type) return ''
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
      const parts = [amount != null ? String(amount) : undefined, type].filter(Boolean)
      return parts.length ? parts.join(' ') : ''
    }
    case 'restore_health': {
      const amount = asNumber(config.amount)
      return amount != null ? String(amount) : ''
    }
    case 'select_targets': {
      const relations = action.target?.relations
      const radius = action.target?.radius
      const relPart = Array.isArray(relations) && relations.length ? relations.join('/') : undefined
      const radiusPart = typeof radius === 'number' ? `within ${radius}` : undefined
      return [relPart, radiusPart].filter(Boolean).join(' ')
    }
    case 'create_zone': {
      const name = asString(config.name)
      return name ?? ''
    }
    case 'summon_unit': {
      const count = asNumber(config.count)
      const unitType = asString(config.unitType)
      if (unitType == null && count == null) return ''
      return `${count ?? 1}× ${unitType ?? 'unit'}`
    }
    case 'apply_status': {
      const status = asString(config.status)
      return status ?? ''
    }
    default:
      return ''
  }
}

// summarizeAction renders `<Label>` or `<Label> — <detail>` for known action
// types with a usable detail, and just `<Label>` otherwise. `schema` is
// accepted (and reserved) for future field-aware summaries but not currently
// consulted — the switch above is a fixed, hand-picked set of the most
// common action types per the Flow view spec.
export function summarizeAction(action: AbilityActionDef, _schema: ActionSchemaBundle | null): string {
  const label = humanizeActionType(action.type)
  const detail = detailFor(action)
  return detail ? `${label} — ${detail}` : label
}
