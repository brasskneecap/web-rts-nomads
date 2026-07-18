// executionTimeline: pure transform from an AbilityProgram + its preview
// execution trace into a Gantt-style lane model for PreviewExecutionTimeline.vue.
//
// The trace is a FLAT, point-in-time list of AbilityExecutionTraceEvents; the
// timeline needs one LANE per program node (trigger/action), in flow order and
// indented by depth, each with a start/end (for a duration bar) or discrete
// marker times (for instantaneous fires and repeating ticks). This module does
// that derivation; the component only lays the result out.
//
// Timing is derived, not stored on the wire, so the rules below encode real
// trace behavior (verified against a live Fireball run):
//   - Trace paths use the ID grammar ("cast", "cast.actions[proj]", …) resolved
//     via refFromPath — the SAME resolver the event log / flow selection use, so
//     a lane and a trace event agree on "which node" without a parallel matcher.
//   - An IMPACT/zone-tick trigger emits no trigger_fired of its own; its fire
//     time is only visible through its child actions' events. So a trigger's
//     start is the MIN time across its whole subtree, not just its own events.
//   - launch_projectile/beam "travel": the action fires at launch but its effect
//     lands when its nested impact/tick trigger's actions fire — so its bar spans
//     [launch, max subtree time]. create_zone spans [created, created+duration]
//     (duration from the zone_created payload). Everything else is instantaneous
//     (a marker at its own event time).

import type {
  AbilityActionDef,
  AbilityProgram,
  AbilityTriggerDef,
} from '@/game/abilities/program/abilityProgram'
import type { AbilityExecutionTraceEvent } from '@/game/abilities/program/programPreview'
import { humanizeActionType } from './summarizeAction'
import { nestedTriggersFor, type NodePath } from './programTree'
import { refFromPath } from './refFromPath'

export type LaneKind = 'trigger' | 'action'

// LaneCategory drives both the lane's color and the legend. Kept coarse (one
// bucket per legend swatch) rather than one-per-action-type so the palette
// stays readable — the exact action type is still in `label`.
export type LaneCategory =
  | 'trigger'
  | 'action'
  | 'damage'
  | 'heal'
  | 'targets'
  | 'zone'
  | 'status'
  | 'presentation'

export interface TimelineLane {
  /** Stable key (serialized nodePath) for v-for and selection matching. */
  key: string
  nodePath: NodePath
  kind: LaneKind
  /** Raw trigger.type / action.type (for icons / debugging). */
  nodeType: string
  label: string
  depth: number
  category: LaneCategory
  /** Did any trace event land on this node or its subtree? */
  fired: boolean
  /** Bar start in seconds, or null when the node never fired. */
  startT: number | null
  /** Bar end in seconds. > startT ⇒ render a bar; == startT ⇒ marker-only. */
  endT: number | null
  /** Discrete diamond times: instantaneous fires and repeating ticks. */
  markers: number[]
}

export interface ExecutionTimeline {
  lanes: TimelineLane[]
  /** Axis length in seconds (fit to content). */
  axisDuration: number
}

// Action types whose bar spans from launch to the earliest event of their
// nested trigger's subtree (projectile travel, beam duration). create_zone is
// handled separately (its length is an authored duration, not a subtree span).
const TRAVEL_SPAWNERS: ReadonlySet<string> = new Set(['launch_projectile', 'beam'])

function serializeKey(path: NodePath): string {
  return path.map((s) => `${s.kind}:${s.id}`).join('/')
}

function laneCategory(kind: LaneKind, type: string): LaneCategory {
  if (kind === 'trigger') return 'trigger'
  switch (type) {
    case 'deal_damage':
      return 'damage'
    case 'heal':
    case 'restore_health':
      return 'heal'
    case 'select_targets':
    case 'filter_targets':
    case 'store_targets':
      return 'targets'
    case 'create_zone':
      return 'zone'
    case 'apply_status':
      return 'status'
    case 'play_presentation':
    case 'play_sound':
    case 'change_render_layer':
    case 'camera_shake':
    case 'spawn_effect':
      return 'presentation'
    default:
      return 'action'
  }
}

interface WalkedNode {
  nodePath: NodePath
  kind: LaneKind
  nodeType: string
  depth: number
}

// walkProgram flattens the program into a pre-order list of trigger/action
// nodes (root triggers → their actions → each action's nested triggers, at any
// depth), mirroring the flow view's own nesting via nestedTriggersFor. Pre-order
// guarantees a node's descendants are a CONTIGUOUS run immediately after it with
// greater depth — used below to bracket a node's subtree without extra lookups.
//
// Presentation triggers (e.g. meteor's on_animation_marker "impact" trigger,
// which fires the whole land → damage → create-zone → burn-tick flow) fire at
// runtime just like root triggers, so they MUST be walked too — otherwise their
// lanes (and their late zone/tick events, seconds after the cast) never match a
// lane, the axis collapses to just the cast, and the timeline shows almost
// nothing. Their nodePath is prefixed with the presentation segment so it lines
// up with what refFromPath resolves a `marker[...]` trace path to.
function walkProgram(prog: AbilityProgram): WalkedNode[] {
  const out: WalkedNode[] = []
  const visitTrigger = (t: AbilityTriggerDef, path: NodePath, depth: number) => {
    out.push({ nodePath: path, kind: 'trigger', nodeType: t.type, depth })
    for (const a of t.actions) {
      visitAction(a, [...path, { kind: 'action', id: a.id }], depth + 1)
    }
  }
  const visitAction = (a: AbilityActionDef, path: NodePath, depth: number) => {
    out.push({ nodePath: path, kind: 'action', nodeType: a.type, depth })
    for (const nested of nestedTriggersFor(a)) {
      visitTrigger(nested, [...path, { kind: 'trigger', id: nested.id }], depth + 1)
    }
  }
  for (const t of prog.triggers) visitTrigger(t, [{ kind: 'trigger', id: t.id }], 0)
  for (const p of prog.presentations ?? []) {
    for (const t of p.triggers ?? []) {
      visitTrigger(t, [{ kind: 'presentation', id: p.id }, { kind: 'trigger', id: t.id }], 0)
    }
  }
  return out
}

function readNumber(v: unknown): number | undefined {
  return typeof v === 'number' && Number.isFinite(v) ? v : undefined
}

function niceCeil(v: number): number {
  if (v <= 0) return 1
  // Round up to a tidy value so the axis ends on a clean ruler tick.
  const step = v <= 2 ? 0.25 : v <= 5 ? 0.5 : 1
  return Math.ceil(v / step) * step
}

export function buildExecutionTimeline(
  program: AbilityProgram | null | undefined,
  trace: AbilityExecutionTraceEvent[],
  runDuration: number,
): ExecutionTimeline {
  if (!program) return { lanes: [], axisDuration: Math.max(1, runDuration || 1) }

  const walked = walkProgram(program)
  const keyOf = walked.map((n) => serializeKey(n.nodePath))

  // Fallback lookups for trace paths refFromPath can't resolve. A projectile-
  // impact (and marker/zone) context restarts the trace path at the trigger's
  // TYPE, not its id (e.g. "on_projectile_impact.actions[sel]" when the trigger
  // id is "impact") — refFromPath's id-guard rejects that. Action ids are
  // globally unique (collectAllIds), so the leaf action id alone identifies the
  // lane unambiguously; a bare trigger root falls back to id-or-type.
  const keyByActionId = new Map<string, string>()
  const keyByTriggerId = new Map<string, string>()
  const keyByTriggerType = new Map<string, string>()
  walked.forEach((n, i) => {
    const leaf = n.nodePath[n.nodePath.length - 1]
    if (n.kind === 'action') keyByActionId.set(leaf.id, keyOf[i])
    else {
      keyByTriggerId.set(leaf.id, keyOf[i])
      if (!keyByTriggerType.has(n.nodeType)) keyByTriggerType.set(n.nodeType, keyOf[i])
    }
  })

  const LEAF_ACTION = /\.actions\[([A-Za-z0-9_]+)\]$/
  const keyForEventPath = (path: string): string | null => {
    const ref = refFromPath(program, path)
    if (ref && ref.kind !== 'ability') return serializeKey(ref.path)
    const am = LEAF_ACTION.exec(path)
    if (am) return keyByActionId.get(am[1]) ?? null
    return keyByTriggerId.get(path) ?? keyByTriggerType.get(path) ?? null
  }

  // Match every trace event to a node key, collecting per-node event times and,
  // for zone_created, the authored duration.
  const ownTimes = new Map<string, number[]>()
  const zoneDurationByKey = new Map<string, number>()
  for (const e of trace) {
    if (!e.path) continue
    const key = keyForEventPath(e.path)
    if (!key) continue
    const list = ownTimes.get(key)
    if (list) list.push(e.t)
    else ownTimes.set(key, [e.t])
    if (e.type === 'zone_created') {
      const d = readNumber(e.payload?.duration)
      if (d !== undefined) zoneDurationByKey.set(key, d)
    }
  }

  // Subtree times: pre-order means descendants of node i are the contiguous run
  // [i+1 .. while depth > node.depth). Union each node's own times with all of
  // that run's own times.
  const lanes: TimelineLane[] = []
  let contentEnd = 0
  for (let i = 0; i < walked.length; i++) {
    const node = walked[i]
    const key = keyOf[i]
    const own = ownTimes.get(key) ?? []
    const subtree: number[] = [...own]
    for (let j = i + 1; j < walked.length && walked[j].depth > node.depth; j++) {
      const childTimes = ownTimes.get(keyOf[j])
      if (childTimes) subtree.push(...childTimes)
    }

    const fired = subtree.length > 0
    const startT = fired ? Math.min(...subtree) : null

    let endT = startT
    if (fired && startT !== null && node.kind === 'action') {
      const zoneDur = zoneDurationByKey.get(key)
      if (node.nodeType === 'create_zone' && zoneDur !== undefined) {
        endT = startT + zoneDur
      } else if (TRAVEL_SPAWNERS.has(node.nodeType)) {
        endT = Math.max(...subtree)
      }
    }

    // Markers: this node's OWN distinct fire times (instantaneous actions get
    // one; a repeating tick trigger gets several). A trigger with no own events
    // but a fired subtree (impact/zone-tick triggers emit no trigger_fired) gets
    // a single marker at its inferred start.
    const uniqueOwn = [...new Set(own)].sort((a, b) => a - b)
    const markers = uniqueOwn.length > 0 ? uniqueOwn : node.kind === 'trigger' && startT !== null ? [startT] : []

    lanes.push({
      key,
      nodePath: node.nodePath,
      kind: node.kind,
      nodeType: node.nodeType,
      label: humanizeActionType(node.nodeType),
      depth: node.depth,
      category: laneCategory(node.kind, node.nodeType),
      fired,
      startT,
      endT,
      markers,
    })

    if (endT !== null) contentEnd = Math.max(contentEnd, endT)
    for (const m of markers) contentEnd = Math.max(contentEnd, m)
  }

  const axisDuration = contentEnd > 0 ? niceCeil(contentEnd * 1.08) : Math.max(1, runDuration || 1)
  return { lanes, axisDuration }
}
