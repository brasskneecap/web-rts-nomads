// traceEventDisplay: shared, pure, best-effort presentation helpers for an
// AbilityExecutionTraceEvent — humanized labels, timeline marker color, a
// coarse filter category (for PreviewEventLog's tabs), and a one-line
// payload summary (for PreviewEventLog's rows). Consumed by both
// PreviewTimeline and PreviewEventLog so the two views agree on what a
// given event "means".
//
// `payload` is an opaque `Record<string, unknown> | undefined` on the wire
// (mirrors Go's `map[string]any`) — every read here is guarded; an
// unexpected/missing key degrades to an empty summary, never a throw.
//
// The event type set below is sourced from the server's actual ctx.trace(...)
// call sites (server/internal/game/ability_exec*.go, ability_program_registry.go,
// ability_zone.go) PLUS a couple of forward-looking names the ability-preview
// design doc calls out (cast_started/cast_completed/zone_tick) that aren't
// emitted by any call site yet — kept here so a later server addition needs
// no client change, and so `traceEventColor`/`traceEventCategory` degrade
// sensibly for anything not in this table (see their fallback rules).

import { humanizeActionType } from './summarizeAction'
import type { AbilityExecutionTraceEvent } from '@/game/abilities/program/programPreview'

export const humanizeTraceType = humanizeActionType

export type TraceColor = 'neutral' | 'blue' | 'red' | 'green' | 'amber' | 'muted' | 'danger'

const COLOR_BY_TYPE: Record<string, TraceColor> = {
  cast_started: 'neutral',
  cast_completed: 'neutral',
  trigger_fired: 'neutral',
  condition_failed: 'muted',
  conditional_taken: 'neutral',
  action_started: 'neutral',
  action_completed: 'neutral',
  targets_selected: 'blue',
  targets_stored: 'blue',
  targets_filtered: 'blue',
  damage_applied: 'red',
  healing_applied: 'green',
  zone_created: 'amber',
  zone_tick: 'amber',
  status_applied: 'amber',
  status_removed: 'amber',
  unit_summoned: 'amber',
  force_applied: 'amber',
  resource_modified: 'blue',
  wait: 'muted',
  repeat: 'neutral',
  repeat_capped: 'danger',
  named_trigger_invoked: 'neutral',
  no_program: 'muted',
  action_skipped: 'muted',
  unknown_named_trigger: 'danger',
  op_budget_exceeded: 'danger',
  recursion_guard: 'danger',
  validation_error: 'danger',
}

// traceEventColor never throws on an unrecognized type: anything ending in
// "_error" is treated as danger (matching validation_error's own naming),
// and everything else falls back to neutral.
export function traceEventColor(type: string): TraceColor {
  const known = COLOR_BY_TYPE[type]
  if (known) return known
  if (type.endsWith('_error')) return 'danger'
  return 'neutral'
}

export type TraceCategory = 'damage' | 'healing' | 'targets' | 'zones' | 'skipped' | 'errors'

const CATEGORY_BY_TYPE: Partial<Record<string, TraceCategory>> = {
  damage_applied: 'damage',
  healing_applied: 'healing',
  targets_selected: 'targets',
  targets_stored: 'targets',
  targets_filtered: 'targets',
  zone_created: 'zones',
  zone_tick: 'zones',
  action_skipped: 'skipped',
  validation_error: 'errors',
  op_budget_exceeded: 'errors',
  recursion_guard: 'errors',
  unknown_named_trigger: 'errors',
}

// traceEventCategory returns null for an event that doesn't belong to any
// of PreviewEventLog's non-"All" tabs (e.g. trigger_fired, wait, repeat) —
// those events still show up under "All", just not under a narrower filter.
export function traceEventCategory(type: string): TraceCategory | null {
  const known = CATEGORY_BY_TYPE[type]
  if (known) return known
  if (type.endsWith('_error')) return 'errors'
  return null
}

function display(v: unknown): string {
  if (v === undefined || v === null) return '?'
  return String(v)
}

// summarizeTraceEvent renders the best-effort "— detail" portion of a
// PreviewEventLog row from the event's payload. Unknown types, or known
// types missing their usual payload keys, degrade to '' (the row still
// shows the humanized type + timestamp on their own).
export function summarizeTraceEvent(e: AbilityExecutionTraceEvent): string {
  const p = e.payload ?? {}
  switch (e.type) {
    case 'damage_applied':
      return `unit ${display(p.unit)} ← ${display(p.amount)}${p.type ? ` ${p.type}` : ''}`
    case 'healing_applied':
      return `unit ${display(p.unit)} ← +${display(p.amount)}`
    case 'targets_selected':
      return `${display(p.count)} target(s)`
    case 'targets_stored':
      return `stored ${display(p.count)} as "${display(p.as)}"`
    case 'targets_filtered':
      return `${display(p.count)} target(s) remain`
    case 'zone_created': {
      const parts = [
        p.name != null ? String(p.name) : undefined,
        p.radius != null ? `r${p.radius}` : undefined,
        p.duration != null ? `${p.duration}s` : undefined,
      ].filter(Boolean)
      return parts.join(' · ')
    }
    case 'trap_placed': {
      // Same "entity created" shape as zone_created above: what · how big · how long.
      const parts = [
        p.trapType != null ? String(p.trapType) : undefined,
        p.radius != null ? `r${p.radius}` : undefined,
        p.duration != null ? `${p.duration}s` : undefined,
      ].filter(Boolean)
      return parts.join(' · ')
    }
    case 'status_applied':
      return `unit ${display(p.unit)} ← ${display(p.status)}`
    case 'status_removed':
      return `unit ${display(p.unit)} ✕ ${display(p.status)}`
    case 'unit_summoned':
      return `${display(p.count)}× ${display(p.unitType)}`
    case 'force_applied':
      return `unit ${display(p.unit)} strength ${display(p.strength)}`
    case 'resource_modified':
      return `${display(p.resource)} ${display(p.amount)}`
    case 'action_skipped':
      if (p.reason) return String(p.reason)
      if (p.type) return `type "${p.type}" not runnable`
      return ''
    case 'action_started':
      if (!p.type) return ''
      return `${humanizeTraceType(String(p.type))}${p.targets != null ? ` (${p.targets} target(s))` : ''}`
    case 'trigger_fired':
      return p.type != null ? String(p.type) : ''
    case 'validation_error':
      return p.error != null ? String(p.error) : ''
    case 'op_budget_exceeded':
      return p.limit != null ? `limit ${p.limit}` : ''
    case 'recursion_guard':
      return p.trigger != null ? `trigger "${p.trigger}" @ depth ${display(p.depth)}` : ''
    case 'wait':
      return p.seconds != null ? `${p.seconds}s` : ''
    case 'repeat':
      return p.count != null ? `${p.count}×` : ''
    case 'repeat_capped':
      return p.requested != null && p.cappedTo != null ? `${p.requested} → ${p.cappedTo}` : ''
    default:
      return ''
  }
}
