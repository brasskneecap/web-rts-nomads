// refFromPath: best-effort translation of a validation/trace `path` string
// into a NodeRef the builder can select. Two distinct grammars are in play
// on the wire and both are resolved here, at ANY depth (phase-7 Task 4):
//
//   INDEX grammar (validateAbilityProgram's ValidationIssue.path, and the
//   flow view's own `triggers[i]` / `triggers[i].actions[j]` convention —
//   see FlowTriggerCard.vue / FlowActionCard.vue / InspectorBar.vue's
//   `selectedPath`). Fully qualified from the program root, unbounded depth
//   (ability_program_validate.go's `walkAction` recursion — see the
//   phase-7 plan's Ground truth #3):
//     "triggers[N]"
//     "triggers[N].actions[M]"
//     "triggers[N].actions[M].children[K]"                  -> nested trigger
//     "triggers[N].actions[M].children[K].actions[L]"        -> nested action
//     "triggers[N].actions[M].config.triggers[K]"            -> nested trigger
//     "triggers[N].actions[M].config.triggers[K].actions[L]" -> nested action
//     "presentations[P].triggers[N]" / ".actions[M]" / deeper still, same as
//       above once inside a presentation's trigger.
//   `namedTriggers[id]...` paths exist in this grammar too but namedTriggers
//   AUTHORING is out of scope for phase 7 (see plan's "Out of scope") — this
//   function doesn't special-case them; they simply don't match either root
//   regex below and fall through to null, same as any other unaddressable
//   shape.
//   The walk here is a small local state machine (root match, then a loop
//   consuming ".actions[M]" then optionally ".children[K]" or
//   ".config.triggers[K]", repeating) rather than one fixed-depth regex,
//   because the grammar's depth is unbounded — this mirrors
//   programTree.ts's `walkPath`, just driven by index tokens instead of ids.
//
//   ID grammar (RunAbilityPreview's execution trace —
//   AbilityExecutionTraceEvent.path — see server/internal/game/ability_exec.go
//   and ability_marker.go:180). CRITICAL: trace paths carry NO ANCESTRY — the
//   executor restarts the path string at every boundary (cast tick, a
//   scheduled animation marker, a zone tick are each their own namespace), so
//   this grammar can NEVER be walked top-down like the index grammar above.
//   It must be resolved by SEARCHING the program for a matching id
//   (`findNodePathById`, or a marker-string search for the `marker[...]`
//   root — see below) and returning whatever full NodePath that search
//   turns up. Roots, by entry point (phase-7 plan's Ground truth #4):
//     "<triggerId>"                       -> the trigger itself (any trigger
//                                             in the program, any depth —
//                                             e.g. meteor's zone-tick root
//                                             "burn", not just a root trigger)
//     "<triggerId>.actions[<actionId>]"   -> that action under that trigger
//     "marker[<markerString>].actions[<actionId>]" -> a trigger reached by
//       animation marker (ability_marker.go:180 sets the trace path root to
//       "marker[" + m.marker + "]"). `<markerString>` is the STRING from
//       that trigger's `timing.marker` — it is NOT a trigger id, and nothing
//       guarantees it differs from one (meteor's own real ability uses
//       "impact" for both, coincidentally — refFromPath.test.ts's fixture
//       deliberately uses a trigger id that DIFFERS from its marker string so
//       this branch's test can only pass if it genuinely searches
//       `timing.marker`, not if it were quietly matching on id instead).
//       Resolved by scanning every trigger in the program (root triggers AND
//       presentations' triggers, at any nesting depth) for one whose
//       `timing.marker` equals the string in brackets.
//     "marker[<markerString>]"            -> the marker trigger itself (the
//       bare form, symmetric with the bare "<triggerId>" root above).
//   Deliberately NOT addressable by this grammar, and returned as `null`
//   rather than mis-resolved (these roots are LITERAL strings that don't
//   name any searchable node — `conditional.then` / `repeat` nest ACTIONS
//   directly inside a config bag, not inside a trigger; `namedTrigger[id]`
//   is a real root in the trace grammar but namedTriggers authoring is out
//   of scope for phase 7, matching the index-grammar carve-out above):
//     "conditional.then" / "conditional.then.actions[...]"
//     "repeat" / "repeat.actions[...]"
//     "namedTrigger[<id>]" / "namedTrigger[<id>].actions[...]"
//
// Disambiguation: the index grammar is tried first whenever `path` starts
// with the literal "triggers[" or "presentations[" — both are unambiguous,
// unbounded-depth-safe prefixes that no id-grammar path can ever collide
// with (an authored trigger id would have to literally BE "triggers" or a
// presentation id "presentations" for that to even be possible, and ids
// share a namespace with everything else in programTree.ts's nextUniqueId,
// which never mints that shape). Everything else falls through to the id
// grammar.

import type { AbilityActionDef, AbilityProgram, AbilityTriggerDef } from '@/game/abilities/program/abilityProgram'
import { findNodePathById, loopBodyOf, nestedTriggersFor, type NodePath, type NodeRef } from './programTree'

// --- Index grammar (walk top-down; unbounded depth) ------------------------

const ROOT_PRESENTATION_TRIGGER = /^presentations\[(\d+)\]\.triggers\[(\d+)\]/
const ROOT_TRIGGER = /^triggers\[(\d+)\]/
const STEP_ACTION = /^\.actions\[(\d+)\]/
const STEP_CHILDREN = /^\.children\[(\d+)\]/
const STEP_CONFIG_TRIGGERS = /^\.config\.triggers\[(\d+)\]/
// A loop action's nested ACTION list — the one place ".body[K]" leads to
// another action rather than a trigger.
const STEP_BODY = /^\.body\[(\d+)\]/

// configTriggersOf is a local mirror of programTree.ts's (unexported)
// helper of the same name — `config` is an OPAQUE bag (see
// AbilityActionDef.config's doc comment in abilityProgram.ts), so this only
// ever reads the one sub-key it needs and only trusts it once Array.isArray
// confirms the shape. Duplicated rather than exported from programTree.ts
// because this module's scope (phase-7 Task 4) is limited to refFromPath.ts.
function configTriggersOf(action: AbilityActionDef): AbilityTriggerDef[] {
  const raw = action.config?.triggers
  return Array.isArray(raw) ? (raw as AbilityTriggerDef[]) : []
}

// IndexParseState is the running "where am I" cursor while consuming the
// index-grammar string left to right — mirrors programTree.ts's WalkStep,
// just built from index tokens instead of id segments.
type IndexParseState =
  | { kind: 'trigger'; node: AbilityTriggerDef; path: NodePath }
  | { kind: 'action'; node: AbilityActionDef; path: NodePath }

function refFromIndexPath(program: AbilityProgram, path: string): NodeRef | null {
  let rest = path
  let state: IndexParseState

  const presMatch = ROOT_PRESENTATION_TRIGGER.exec(rest)
  if (presMatch) {
    const pres = (program.presentations ?? [])[Number(presMatch[1])]
    const trigger = pres?.triggers?.[Number(presMatch[2])]
    if (!pres || !trigger) return null
    state = {
      kind: 'trigger',
      node: trigger,
      path: [
        { kind: 'presentation', id: pres.id },
        { kind: 'trigger', id: trigger.id },
      ],
    }
    rest = rest.slice(presMatch[0].length)
  } else {
    const trigMatch = ROOT_TRIGGER.exec(rest)
    // No match here also covers namedTriggers[...] roots (out of scope, see
    // module doc comment) and any other unrecognized root shape.
    if (!trigMatch) return null
    const trigger = program.triggers[Number(trigMatch[1])]
    if (!trigger) return null
    state = { kind: 'trigger', node: trigger, path: [{ kind: 'trigger', id: trigger.id }] }
    rest = rest.slice(trigMatch[0].length)
  }

  // Consume the rest of the string one segment at a time. A trigger's only
  // legal next segment is ".actions[M]"; an action's only legal next
  // segments are its two nested-trigger slots. This loop is what makes depth
  // unbounded without a fixed-depth regex per level.
  while (rest.length > 0) {
    if (state.kind === 'trigger') {
      const actionMatch = STEP_ACTION.exec(rest)
      if (!actionMatch) return null
      const action: AbilityActionDef | undefined = state.node.actions[Number(actionMatch[1])]
      if (!action) return null
      state = { kind: 'action', node: action, path: [...state.path, { kind: 'action', id: action.id }] }
      rest = rest.slice(actionMatch[0].length)
      continue
    }

    // state.kind === 'action': the only legal next step is into one of its
    // two nested-trigger slots (nestedTriggersFor's two source arrays,
    // indexed separately here because the index grammar addresses each slot
    // with its own array index).
    const childMatch = STEP_CHILDREN.exec(rest)
    if (childMatch) {
      const nested: AbilityTriggerDef | undefined = (state.node.children ?? [])[Number(childMatch[1])]
      if (!nested) return null
      state = { kind: 'trigger', node: nested, path: [...state.path, { kind: 'trigger', id: nested.id }] }
      rest = rest.slice(childMatch[0].length)
      continue
    }

    const cfgMatch = STEP_CONFIG_TRIGGERS.exec(rest)
    if (cfgMatch) {
      const nested: AbilityTriggerDef | undefined = configTriggersOf(state.node)[Number(cfgMatch[1])]
      if (!nested) return null
      state = { kind: 'trigger', node: nested, path: [...state.path, { kind: 'trigger', id: nested.id }] }
      rest = rest.slice(cfgMatch[0].length)
      continue
    }

    // A loop action's config.body: ".body[K]" resolves to a nested ACTION (not
    // a trigger), so state stays an action and can be followed by further steps.
    const bodyMatch = STEP_BODY.exec(rest)
    if (bodyMatch) {
      const nested: AbilityActionDef | undefined = loopBodyOf(state.node)[Number(bodyMatch[1])]
      if (!nested) return null
      state = { kind: 'action', node: nested, path: [...state.path, { kind: 'action', id: nested.id }] }
      rest = rest.slice(bodyMatch[0].length)
      continue
    }

    return null // unrecognized continuation (e.g. a trigger followed by
    // ".children[...]" directly, with no ".actions[...]" in between)
  }

  return state.kind === 'trigger' ? { kind: 'trigger', path: state.path } : { kind: 'action', path: state.path }
}

// --- Id grammar (search by id; NO ancestry in the wire string) -------------

// MARKER_ROOT captures the marker string in brackets and, optionally, a
// trailing action id. The marker string is opaque here — it's matched
// against triggers' `timing.marker`, never treated as an id.
const MARKER_ROOT = /^marker\[([^\]]+)\](?:\.actions\[([A-Za-z0-9_]+)\])?$/

// UNADDRESSABLE_ID_ROOT covers the id-grammar roots that are real trace
// roots server-side but name no searchable trigger/action node:
// `conditional.then` / `repeat` nest ACTIONS directly in a config bag (not a
// trigger), and `namedTrigger[id]` authoring is out of scope for phase 7
// (matching the index grammar's namedTriggers[...] carve-out above). Without
// this explicit check, "repeat" alone would incorrectly fall into the bare
// id-search branch below and (if a trigger ever happened to be authored with
// the literal id "repeat") silently resolve to the wrong node instead of
// honestly reporting "not addressable."
const UNADDRESSABLE_ID_ROOT = /^(?:repeat|conditional\.then|namedTrigger\[[^\]]+\])(?:\..*)?$/

const ID_TRIGGER_ONLY = /^([A-Za-z0-9_]+)$/
const ID_TRIGGER_ACTION = /^([A-Za-z0-9_]+)\.actions\[([A-Za-z0-9_]+)\]$/

// MarkerMatch is what searching the program for a trigger's `timing.marker`
// turns up: the trigger itself (so its actions can be looked up by id) and
// the full NodePath to reach it (built up DURING the search itself, since
// this is exactly the same "search from every root, descend through every
// nested-trigger slot" traversal findNodePathById does — just keyed on
// `timing.marker` instead of `id`).
interface MarkerMatch {
  path: NodePath
  trigger: AbilityTriggerDef
}

function searchTriggerForMarker(t: AbilityTriggerDef, marker: string, path: NodePath): MarkerMatch | undefined {
  if (t.timing?.marker === marker) return { path, trigger: t }
  for (const a of t.actions) {
    for (const nested of nestedTriggersFor(a)) {
      const found = searchTriggerForMarker(nested, marker, [
        ...path,
        { kind: 'action', id: a.id },
        { kind: 'trigger', id: nested.id },
      ])
      if (found) return found
    }
  }
  return undefined
}

function findMarkerMatch(program: AbilityProgram, marker: string): MarkerMatch | undefined {
  for (const t of program.triggers) {
    const found = searchTriggerForMarker(t, marker, [{ kind: 'trigger', id: t.id }])
    if (found) return found
  }
  for (const p of program.presentations ?? []) {
    for (const t of p.triggers ?? []) {
      const found = searchTriggerForMarker(t, marker, [
        { kind: 'presentation', id: p.id },
        { kind: 'trigger', id: t.id },
      ])
      if (found) return found
    }
  }
  return undefined
}

function refFromIdPath(program: AbilityProgram, path: string): NodeRef | null {
  const markerMatch = MARKER_ROOT.exec(path)
  if (markerMatch) {
    const found = findMarkerMatch(program, markerMatch[1])
    if (!found) return null
    if (markerMatch[2]) {
      const action = found.trigger.actions.find((a) => a.id === markerMatch[2])
      if (!action) return null
      return { kind: 'action', path: [...found.path, { kind: 'action', id: action.id }] }
    }
    return { kind: 'trigger', path: found.path }
  }

  if (UNADDRESSABLE_ID_ROOT.test(path)) return null

  const actionMatch = ID_TRIGGER_ACTION.exec(path)
  if (actionMatch) {
    const [, rootId, actionId] = actionMatch
    // Trace paths carry no ancestry, so this SEARCHES the whole program for
    // the action id (findNodePathById) rather than walking down from rootId
    // — see the module doc comment's Ground truth #4 callout. The parent
    // segment is then checked against rootId as a sanity guard: ids are a
    // single global namespace (programTree.ts's collectAllIds), so this
    // should always agree for well-formed trace data, and refuses to
    // mis-resolve rather than silently trusting either half alone if it
    // ever doesn't.
    const actionPath = findNodePathById(program, actionId)
    if (!actionPath) return null
    const parentSeg = actionPath[actionPath.length - 2]
    if (!parentSeg || parentSeg.kind !== 'trigger' || parentSeg.id !== rootId) return null
    return { kind: 'action', path: actionPath }
  }

  const triggerMatch = ID_TRIGGER_ONLY.exec(path)
  if (triggerMatch) {
    const triggerPath = findNodePathById(program, triggerMatch[1])
    if (!triggerPath) return null
    return { kind: 'trigger', path: triggerPath }
  }

  return null
}

export function refFromPath(program: AbilityProgram, path: string): NodeRef | null {
  if (!path) return null

  // Index grammar first — see the module doc comment's Disambiguation
  // section for why "triggers[" / "presentations[" are safe, unambiguous
  // prefixes to branch on.
  if (path.startsWith('triggers[') || path.startsWith('presentations[')) {
    return refFromIndexPath(program, path)
  }

  return refFromIdPath(program, path)
}
