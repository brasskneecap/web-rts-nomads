// Pure, immutable operations over an AbilityProgram tree. Every function
// here returns a NEW program (never mutates its input) so
// useAbilityBuilder.ts can snapshot the "before" program for undo/redo
// simply by holding a reference to it — no defensive cloning needed at the
// call site.

import type {
  AbilityActionDef,
  AbilityProgram,
  AbilityTriggerDef,
  ActionType,
  LoopVar,
  PresentationInstanceDef,
  TriggerType,
} from '@/game/abilities/program/abilityProgram'

// --- Depth-aware path model -------------------------------------------
//
// A flat `{triggerId, actionId}` ref can only address a ROOT trigger and its
// direct actions — it has no way to reach into a presentation's triggers, or
// into an action's nested triggers (`action.children` / `create_zone`'s
// `config.triggers`), or any further depth below those. This section
// replaces that with a typed id-CHAIN so any depth is addressable.
//
// Why typed segments, not a bare `string[]`: segment 0 is ambiguous
// otherwise — it could be a root trigger id OR a presentation id, and
// nothing guarantees those two live in disjoint id spaces. Tagging every
// segment with its container kind removes the ambiguity and lets every
// resolver step dispatch on "what container am I inside right now" instead
// of guessing from string shape.
//
// Why ids, not the validator's index grammar (`triggers[0].actions[1]`):
// indices shift under add/remove; ids are stable identity across edits.
// Index paths are DERIVED from an id-path on demand (see `indexPathFor`)
// purely for looking up validation issues by path — they are never the
// thing stored as "what's selected." This mirrors the id-vs-index rationale
// already documented at ItemInspector.vue:298.
export type NodeSeg =
  | { kind: 'presentation'; id: string }
  | { kind: 'trigger'; id: string }
  | { kind: 'action'; id: string }

export type NodePath = NodeSeg[]

// NodeRef identifies whatever is currently selected in the editor tree, at
// any depth. This USED to be a flat, root-only `{triggerId, actionId}` shape
// (and, mid-migration, a separately-named `NestedNodeRef` sat alongside it
// so old and new call sites could both typecheck at once — see
// docs/superpowers/plans/2026-07-16-composable-abilities-phase7-nested-authoring.md,
// Task 5). Every consumer now constructs/compares this depth-aware shape, so
// the two names have been folded back into one.
export type NodeRef =
  | { kind: 'ability' }
  | { kind: 'trigger'; path: NodePath } // last segment is a trigger
  | { kind: 'action'; path: NodePath } // last segment is an action

// pathsEqual compares two NodePaths by VALUE (segment kind + id, in order),
// not by reference — used by isSelected checks (FlowTriggerCard/
// FlowActionCard) instead of JSON.stringify-ing both sides at every call
// site. Different length or any differing segment -> false.
export function pathsEqual(a: NodePath, b: NodePath): boolean {
  if (a.length !== b.length) return false
  return a.every((seg, i) => seg.kind === b[i].kind && seg.id === b[i].id)
}

// emptyProgram returns a minimal, structurally valid AbilityProgram to seed
// a brand-new ability. `range` is required on AbilityEntryDef, so a fresh
// no_target entry gets the harmless default 0 (no_target entries don't use
// range at cast time; the editor's entry-type UI can set a real value once
// the author picks something more specific).
export function emptyProgram(): AbilityProgram {
  return { entry: { type: 'no_target', range: 0 }, triggers: [] }
}

// walkTrigger / collectAllIds gather every id string used anywhere in the
// program (root triggers, their actions, nested action.children triggers,
// namedTriggers, and presentations + their triggers) so new ids can never
// collide with an existing one, however deep it lives.
function walkTrigger(t: AbilityTriggerDef, ids: string[]): void {
  ids.push(t.id)
  for (const a of t.actions) walkAction(a, ids)
}

function walkAction(a: AbilityActionDef, ids: string[]): void {
  ids.push(a.id)
  if (a.children) {
    for (const child of a.children) walkTrigger(child, ids)
  }
  // A loop action's config.body is a nested ACTION list — scan it so a newly
  // minted id can never collide with one living inside a loop body.
  for (const body of loopBodyOf(a)) walkAction(body, ids)
  // config.triggers (create_zone's nested-trigger slot) is addressable now
  // that resolveNode/updateNodeAt reach into it — a new id minted without
  // scanning this slot could collide with an id already living inside it,
  // which the Go validator's duplicate-id check (now recursing into
  // config.triggers too, see ability_program_validate.go) would reject at
  // save time.
  for (const nested of configTriggersOf(a)) walkTrigger(nested, ids)
}

function collectAllIds(prog: AbilityProgram): string[] {
  const ids: string[] = []
  for (const t of prog.triggers) walkTrigger(t, ids)
  if (prog.namedTriggers) {
    for (const t of Object.values(prog.namedTriggers)) walkTrigger(t, ids)
  }
  if (prog.presentations) {
    for (const p of prog.presentations) {
      ids.push(p.id)
      if (p.triggers) {
        for (const t of p.triggers) walkTrigger(t, ids)
      }
    }
  }
  return ids
}

// collectSavedContextNames returns every named-context key an ability SAVES to
// — the destination names in any action's `outputs` map, plus every
// `store_targets` action's `config.as`. These are exactly the keys a later
// query can read back via source="named_context" / origin="named_context_value"
// (the "Saved Value" picker), so the editor can offer the names actually
// defined in THIS ability instead of a fixed guess-list. Walks every nesting
// slot (children, config.triggers, loop bodies, namedTriggers, presentations),
// mirroring collectAllIds. De-duplicated and sorted for a stable menu order.
export function collectSavedContextNames(prog: AbilityProgram): string[] {
  const names = new Set<string>()
  function addAction(a: AbilityActionDef): void {
    if (a.outputs) {
      for (const dest of Object.values(a.outputs)) {
        if (typeof dest === 'string' && dest) names.add(dest)
      }
    }
    if (a.type === 'store_targets') {
      const as = a.config?.as
      if (typeof as === 'string' && as) names.add(as)
    }
    if (a.children) for (const child of a.children) addTrigger(child)
    for (const body of loopBodyOf(a)) addAction(body)
    for (const nested of configTriggersOf(a)) addTrigger(nested)
  }
  function addTrigger(t: AbilityTriggerDef): void {
    for (const a of t.actions) addAction(a)
  }
  for (const t of prog.triggers) addTrigger(t)
  if (prog.namedTriggers) for (const t of Object.values(prog.namedTriggers)) addTrigger(t)
  if (prog.presentations) {
    for (const p of prog.presentations) {
      if (p.triggers) for (const t of p.triggers) addTrigger(t)
    }
  }
  return [...names].sort()
}

// collectReadContextNames returns every named-context key an ability READS
// back BY NAME — a target query's originRef.key (source "named_context" /
// origin "named_context_value") and excludeRef.key, plus any action Input
// ContextRef. It is the counterpart to collectSavedContextNames: a saved name
// present here is consumed somewhere; one absent is dead weight (nothing reads
// it). Same full-tree traversal. The implicit "previous_action_targets" /
// "selected" / "initial_target" refs are NOT names an author saved, so they're
// irrelevant to that dead-weight check and simply never appear as saved names.
export function collectReadContextNames(prog: AbilityProgram): string[] {
  const names = new Set<string>()
  function addAction(a: AbilityActionDef): void {
    const q = a.target
    if (q?.originRef?.key) names.add(q.originRef.key)
    if (q?.excludeRef?.key) names.add(q.excludeRef.key)
    if (a.input) {
      for (const ref of Object.values(a.input)) {
        if (ref && typeof ref.key === 'string' && ref.key) names.add(ref.key)
      }
    }
    if (a.children) for (const child of a.children) addTrigger(child)
    for (const body of loopBodyOf(a)) addAction(body)
    for (const nested of configTriggersOf(a)) addTrigger(nested)
  }
  function addTrigger(t: AbilityTriggerDef): void {
    for (const a of t.actions) addAction(a)
  }
  for (const t of prog.triggers) addTrigger(t)
  if (prog.namedTriggers) for (const t of Object.values(prog.namedTriggers)) addTrigger(t)
  if (prog.presentations) {
    for (const p of prog.presentations) {
      if (p.triggers) for (const t of p.triggers) addTrigger(t)
    }
  }
  return [...names].sort()
}

// namesSavedByAction returns the named-context keys a single action saves to —
// its `outputs` destinations plus, for a store_targets action, its config.as.
// (The per-action slice of collectSavedContextNames, for the inspector's
// dead-save warning.)
export function namesSavedByAction(a: AbilityActionDef): string[] {
  const names = new Set<string>()
  if (a.outputs) for (const dest of Object.values(a.outputs)) if (dest) names.add(dest)
  if (a.type === 'store_targets' && typeof a.config?.as === 'string' && a.config.as) names.add(a.config.as)
  return [...names]
}

// nextUniqueId returns `<prefix><n>` where n is one more than the highest
// numeric suffix currently used by any existing `<prefix>NNN`-shaped id in
// the program (or 1 if none exist).
function nextUniqueId(prog: AbilityProgram, prefix: string): string {
  const ids = collectAllIds(prog)
  let max = 0
  for (const id of ids) {
    if (!id.startsWith(prefix)) continue
    const rest = id.slice(prefix.length)
    if (/^\d+$/.test(rest)) max = Math.max(max, Number(rest))
  }
  return `${prefix}${max + 1}`
}

export function findTrigger(prog: AbilityProgram, triggerId: string): AbilityTriggerDef | undefined {
  return prog.triggers.find((t) => t.id === triggerId)
}

export function findAction(
  prog: AbilityProgram,
  triggerId: string,
  actionId: string,
): AbilityActionDef | undefined {
  return findTrigger(prog, triggerId)?.actions.find((a) => a.id === actionId)
}

// --- Depth-aware resolution ---------------------------------------------
//
// CONFIG_TRIGGER_ACTION_TYPES: the action types whose Go descriptor decodes a
// `triggers` field out of its own config, meaning a nested trigger belongs in
// `config.triggers` rather than `children`. Mirrors the Go side exactly (see
// walkAction's switch in ability_program_validate.go):
//   create_zone       -> createZoneConfig.Triggers      (on_zone_tick/enter/exit)
//   apply_status      -> applyStatusConfig.Triggers     (on_status_tick/expire;
//                        non-empty is ALSO the authored-vs-legacy discriminator)
//   launch_projectile -> launchProjectileConfig.Triggers (on_projectile_impact)
//   beam              -> beamConfig.Triggers             (on_beam_impact | on_beam_tick)
// Reading is deliberately action-type-agnostic (configTriggersOf below) — only
// the ADD path needs this list, to pick the right slot. Keep it in step with
// the Go descriptors; a missing entry silently misfiles an authored trigger
// into `children`, where it fires as on_action_complete and the Go decoder
// never sees it.
const CONFIG_TRIGGER_ACTION_TYPES: ReadonlySet<string> = new Set([
  'create_zone',
  'apply_status',
  'launch_projectile',
  'beam',
])

// configTriggersOf reads action.config.triggers defensively: `config` is an
// OPAQUE bag (decoded per-action-type by a later task's registry — see
// AbilityActionDef.config's doc comment), so this never destructures or
// re-marshals it. It only ever reads the one sub-key it needs, and only
// trusts it once Array.isArray confirms the shape, so an unrelated
// `config.triggers` left over from a different action type (or just
// malformed authoring) can never be walked as if it were real triggers.
function configTriggersOf(action: AbilityActionDef): AbilityTriggerDef[] {
  const raw = action.config?.triggers
  return Array.isArray(raw) ? (raw as AbilityTriggerDef[]) : []
}

// loopBodyOf reads a `loop` action's config.body — the nested ACTION list it
// runs each iteration — defensively (opaque config, never destructured). This
// is the one place an action owns a list of ACTIONS (not triggers), so the
// path model can reach into it: an `action` segment following a loop action
// resolves against this list (see walkPath / rebuildActionList).
export function loopBodyOf(action: AbilityActionDef): AbilityActionDef[] {
  const raw = action.config?.body
  return Array.isArray(raw) ? (raw as AbilityActionDef[]) : []
}

// loopScopeFor reports whether the node at `path` sits inside a loop, and which
// loop variables are in scope for it — the union of every ANCESTOR loop
// action's declared variables. A loop's OWN fields (e.g. iterations) are NOT in
// its own variable scope, so only proper ancestors (prefixes shorter than the
// full path) are walked. Used by the inspector so a body field can offer a
// literal-or-variable choice. Nested loops accumulate (both loops' vars).
export function loopScopeFor(prog: AbilityProgram, path: NodePath): { inLoop: boolean; vars: string[] } {
  let inLoop = false
  const vars: string[] = []
  for (let i = 1; i < path.length; i++) {
    if (path[i - 1].kind !== 'action') continue
    const node = resolveNode(prog, path.slice(0, i))
    if (node?.kind === 'action' && node.node.type === 'loop') {
      inLoop = true
      const raw = node.node.config?.vars
      if (Array.isArray(raw)) {
        for (const v of raw as LoopVar[]) if (typeof v?.name === 'string') vars.push(v.name)
      }
    }
  }
  return { inLoop, vars }
}

// nestedTriggersFor returns every trigger nested directly under an action,
// from BOTH real nesting slots: the typed `children` field and
// `create_zone`'s `config.triggers`. Nothing forbids an action from having
// both populated at once (structurally legal), so this returns the UNION —
// deliberately not "children, else config" — because a first-match read
// would silently hide the config-carried triggers whenever children is
// non-empty (the bug this function replaces; see FlowTriggerCard.vue).
export function nestedTriggersFor(action: AbilityActionDef): AbilityTriggerDef[] {
  return [...(action.children ?? []), ...configTriggersOf(action)]
}

// slotOfNestedTrigger reports which of the two nesting slots a given nested
// trigger id currently lives in, so a write-back (add/remove/move) can
// target the right slot instead of guessing. Returns undefined if id isn't
// found in either slot on this action.
export function slotOfNestedTrigger(
  action: AbilityActionDef,
  id: string,
): 'children' | 'config' | undefined {
  if (action.children?.some((t) => t.id === id)) return 'children'
  if (configTriggersOf(action).some((t) => t.id === id)) return 'config'
  return undefined
}

// ResolvedNode is what a NodePath resolves to: the trigger or action object
// living at the path's final segment. Presentation nodes are deliberately
// NOT a resolvable leaf here — per the phase-7 plan, presentation nodes stay
// read-only headers (their asset/scale are edited via the owning
// `play_presentation` action's config, not a second editor), so a path
// ending in a `presentation` segment is a dead end, same as a path pointing
// at nothing at all.
export type ResolvedNode =
  | { kind: 'trigger'; node: AbilityTriggerDef }
  | { kind: 'action'; node: AbilityActionDef }

// WalkStep is the internal per-segment result of walking a NodePath. It
// carries enough state to resume the walk from wherever it left off
// (`presentation` steps carry the presentation's own array index because
// the validator grammar has no standalone `presentations[p]` fragment — it
// only ever appears fused with the trigger that follows it, e.g.
// `presentations[0].triggers[1]`) and, for trigger/action steps, the fully
// composed validator-grammar fragment so far (`indexFrag`), which
// `indexPathFor` returns directly off the last step.
type WalkStep =
  | { kind: 'presentation'; node: PresentationInstanceDef; presIndex: number }
  | { kind: 'trigger'; node: AbilityTriggerDef; indexFrag: string }
  | { kind: 'action'; node: AbilityActionDef; indexFrag: string }

// walkPath resolves every segment of `path` in turn, threading the
// container to search through each step (root -> presentation/trigger ->
// trigger's actions -> action's nested-trigger slots -> ...). Returns
// undefined — NEVER throws — as soon as any segment can't be found or is
// structurally impossible in its container (e.g. an `action` segment at the
// root, or two `trigger` segments in a row with no action between them).
// This is the single spine both `resolveNode` and `indexPathFor` walk, so
// the two can never disagree about what a path addresses.
function walkPath(prog: AbilityProgram, path: NodePath): WalkStep[] | undefined {
  const steps: WalkStep[] = []
  for (const seg of path) {
    const prev = steps[steps.length - 1]

    if (!prev) {
      if (seg.kind === 'trigger') {
        const idx = prog.triggers.findIndex((t) => t.id === seg.id)
        if (idx === -1) return undefined
        steps.push({ kind: 'trigger', node: prog.triggers[idx], indexFrag: `triggers[${idx}]` })
      } else if (seg.kind === 'presentation') {
        const idx = (prog.presentations ?? []).findIndex((p) => p.id === seg.id)
        if (idx === -1) return undefined
        steps.push({ kind: 'presentation', node: prog.presentations![idx], presIndex: idx })
      } else {
        return undefined // no root-level actions
      }
      continue
    }

    if (prev.kind === 'presentation') {
      if (seg.kind !== 'trigger') return undefined
      const idx = (prev.node.triggers ?? []).findIndex((t) => t.id === seg.id)
      if (idx === -1) return undefined
      steps.push({
        kind: 'trigger',
        node: prev.node.triggers![idx],
        indexFrag: `presentations[${prev.presIndex}].triggers[${idx}]`,
      })
      continue
    }

    if (prev.kind === 'trigger') {
      if (seg.kind !== 'action') return undefined
      const idx = prev.node.actions.findIndex((a) => a.id === seg.id)
      if (idx === -1) return undefined
      steps.push({
        kind: 'action',
        node: prev.node.actions[idx],
        indexFrag: `${prev.indexFrag}.actions[${idx}]`,
      })
      continue
    }

    // prev.kind === 'action': the next step is either a nested TRIGGER (its
    // children / config.triggers slots) or, for a loop action, a nested ACTION
    // in its config.body.
    if (seg.kind === 'action') {
      const body = loopBodyOf(prev.node)
      const idx = body.findIndex((a) => a.id === seg.id)
      if (idx === -1) return undefined
      steps.push({
        kind: 'action',
        node: body[idx],
        indexFrag: `${prev.indexFrag}.body[${idx}]`,
      })
      continue
    }
    if (seg.kind !== 'trigger') return undefined
    const slot = slotOfNestedTrigger(prev.node, seg.id)
    if (!slot) return undefined
    if (slot === 'children') {
      const idx = prev.node.children!.findIndex((t) => t.id === seg.id)
      steps.push({
        kind: 'trigger',
        node: prev.node.children![idx],
        indexFrag: `${prev.indexFrag}.children[${idx}]`,
      })
    } else {
      const cfgTriggers = configTriggersOf(prev.node)
      const idx = cfgTriggers.findIndex((t) => t.id === seg.id)
      steps.push({
        kind: 'trigger',
        node: cfgTriggers[idx],
        indexFrag: `${prev.indexFrag}.config.triggers[${idx}]`,
      })
    }
  }
  return steps
}

// resolveNode walks `path` against `prog` and returns the trigger/action
// object living at its final segment, or undefined if any part of the path
// is unresolvable (missing id, wrong container, empty path, or a path that
// dead-ends on a presentation segment). Never throws.
export function resolveNode(prog: AbilityProgram, path: NodePath): ResolvedNode | undefined {
  const steps = walkPath(prog, path)
  if (!steps || steps.length === 0) return undefined
  const last = steps[steps.length - 1]
  if (last.kind === 'presentation') return undefined
  return last.kind === 'trigger' ? { kind: 'trigger', node: last.node } : { kind: 'action', node: last.node }
}

// indexPathFor derives the server validator's index-based path grammar for
// `path` (see ability_program_validate.go), so a NodePath can be used to
// look up `issuesForPath`. Grammar: `triggers[i]` ·
// `presentations[p].triggers[i]` · `${parent}.actions[j]` ·
// `${parent}.children[k]` · `${parent}.config.triggers[k]`. Returns
// undefined for anything walkPath can't resolve, or a path that dead-ends on
// a bare presentation segment (no such fragment exists in the grammar).
export function indexPathFor(prog: AbilityProgram, path: NodePath): string | undefined {
  const steps = walkPath(prog, path)
  if (!steps || steps.length === 0) return undefined
  const last = steps[steps.length - 1]
  return last.kind === 'presentation' ? undefined : last.indexFrag
}

// findNodePathById searches the whole program (root triggers + presentation
// triggers, recursing through every action's nested-trigger slots at any
// depth) for the first trigger or action whose id matches, and returns the
// full NodePath to it. Used by refFromPath (Task 4) to translate an
// execution-trace id — which carries no ancestry of its own (see the phase-7
// plan's Ground truth #4) — into an addressable path. Returns undefined if
// nothing matches.
export function findNodePathById(prog: AbilityProgram, id: string): NodePath | undefined {
  for (const t of prog.triggers) {
    const found = findIdInTrigger(t, id, [{ kind: 'trigger', id: t.id }])
    if (found) return found
  }
  for (const p of prog.presentations ?? []) {
    for (const t of p.triggers ?? []) {
      const found = findIdInTrigger(t, id, [
        { kind: 'presentation', id: p.id },
        { kind: 'trigger', id: t.id },
      ])
      if (found) return found
    }
  }
  return undefined
}

function findIdInTrigger(t: AbilityTriggerDef, id: string, path: NodePath): NodePath | undefined {
  if (t.id === id) return path
  for (const a of t.actions) {
    const found = findIdInAction(a, id, [...path, { kind: 'action', id: a.id }])
    if (found) return found
  }
  return undefined
}

function findIdInAction(a: AbilityActionDef, id: string, path: NodePath): NodePath | undefined {
  if (a.id === id) return path
  for (const nested of nestedTriggersFor(a)) {
    const found = findIdInTrigger(nested, id, [...path, { kind: 'trigger', id: nested.id }])
    if (found) return found
  }
  // A loop action owns a nested ACTION list (config.body) — recurse into it so
  // a body action (or anything nested under it) is addressable.
  for (const body of loopBodyOf(a)) {
    const found = findIdInAction(body, id, [...path, { kind: 'action', id: body.id }])
    if (found) return found
  }
  return undefined
}

// --- Generic immutable spine rebuild ------------------------------------
//
// updateNodeAt is the ONE traversal every mutation op below is expressed
// through (per the phase-7 plan's Task 3). It walks `path` the same way
// `walkPath` does (root -> presentation/trigger -> trigger's actions ->
// action's nested-trigger slots -> ...), but instead of just reading it
// rebuilds a NEW container object at every step, so the result is a fresh
// object along the whole spine while every untouched sibling — at ANY
// depth, in either nesting slot — is passed through by object reference
// (`Array.prototype.map`/`.slice()` only ever replace the one matching
// element).
//
// A path that doesn't resolve (missing id, wrong container, or a
// structurally impossible shape) is a NO-OP: `prog` itself is returned
// unchanged, never a throw — this mirrors resolveNode/indexPathFor's
// contract so every op built on top of updateNodeAt stays no-throw too.
//
// Two overloads (rather than one loosely-typed signature) let call sites
// pass a trigger-mutator or an action-mutator and get the matching return
// type back untyped-cast-free; the implementation signature underneath
// accepts the union because it can't statically know which `path` denotes
// at compile time (that's `path`'s runtime shape, not its static type) —
// see the WATCH OUT note on `resolveNode`: passing a path whose last
// segment doesn't match `fn`'s expected node kind is a caller bug, not
// something this function can catch by itself. Every op below guards
// against that by checking `resolveNode(prog, path)`'s `kind` before ever
// calling updateNodeAt, so a mismatched path degrades to a safe no-op
// instead of reaching in here with the wrong node type.
export function updateNodeAt(
  prog: AbilityProgram,
  path: NodePath,
  fn: (node: AbilityTriggerDef) => AbilityTriggerDef,
): AbilityProgram
export function updateNodeAt(
  prog: AbilityProgram,
  path: NodePath,
  fn: (node: AbilityActionDef) => AbilityActionDef,
): AbilityProgram
export function updateNodeAt(prog: AbilityProgram, path: NodePath, fn: NodeMutator): AbilityProgram {
  if (path.length === 0) return prog
  const [head, ...rest] = path

  if (head.kind === 'trigger') {
    const triggers = rebuildTriggerList(prog.triggers, head.id, rest, fn)
    return triggers ? { ...prog, triggers } : prog
  }
  if (head.kind === 'presentation') {
    const presentations = rebuildPresentationList(prog.presentations ?? [], head.id, rest, fn)
    return presentations ? { ...prog, presentations } : prog
  }
  return prog // no root-level actions, same as walkPath
}

// NodeMutator is a UNION OF FUNCTION TYPES (not a function-of-a-union) so
// that each of updateNodeAt's two public overloads — whose `fn` types differ
// only in which single node kind they accept — is indiviually a member of
// this union and therefore assignable to it. A function typed to accept the
// union `AbilityTriggerDef | AbilityActionDef` would NOT work here: TS
// checks function-parameter assignability contravariantly, and neither
// overload's narrower, single-kind `fn` is assignable to a parameter
// requiring it accept BOTH kinds. The helpers below cast `fn` to whichever
// single-kind shape they're about to call it with — safe because each call
// site only ever invokes `fn` with the node kind that reaching it implies
// (rebuildTriggerList only ever has a trigger in hand; rebuildActionList
// only ever has an action).
type NodeMutator = ((node: AbilityTriggerDef) => AbilityTriggerDef) | ((node: AbilityActionDef) => AbilityActionDef)

// rebuildPresentationList locates `id` in `presentations`, descends into its
// `triggers` array for `rest` (a presentation is never itself a target — the
// next segment must be a trigger, matching walkPath's dead-end rule), and
// returns a new presentations array with that one entry rebuilt, or
// undefined if anything along the way didn't resolve (propagates up as a
// no-op, never throws).
function rebuildPresentationList(
  presentations: PresentationInstanceDef[],
  id: string,
  rest: NodePath,
  fn: NodeMutator,
): PresentationInstanceDef[] | undefined {
  const idx = presentations.findIndex((p) => p.id === id)
  if (idx === -1) return undefined
  if (rest.length === 0 || rest[0].kind !== 'trigger') return undefined
  const triggers = rebuildTriggerList(presentations[idx].triggers ?? [], rest[0].id, rest.slice(1), fn)
  if (!triggers) return undefined
  const updated = presentations.slice()
  updated[idx] = { ...presentations[idx], triggers }
  return updated
}

// rebuildTriggerList locates `id` in `triggers` (this list is whichever
// triggers-array the caller is currently inside — root, a presentation's
// triggers, an action's children, or an action's config.triggers; the
// function doesn't need to know which). If `rest` is empty, `id` IS the
// target: replace it with `fn(node)`. Otherwise descend into its `actions`
// (the only legal next step from a trigger).
function rebuildTriggerList(
  triggers: AbilityTriggerDef[],
  id: string,
  rest: NodePath,
  fn: NodeMutator,
): AbilityTriggerDef[] | undefined {
  const idx = triggers.findIndex((t) => t.id === id)
  if (idx === -1) return undefined
  const updated = triggers.slice()
  if (rest.length === 0) {
    updated[idx] = (fn as (node: AbilityTriggerDef) => AbilityTriggerDef)(triggers[idx])
    return updated
  }
  if (rest[0].kind !== 'action') return undefined
  const actions = rebuildActionList(triggers[idx].actions, rest[0].id, rest.slice(1), fn)
  if (!actions) return undefined
  updated[idx] = { ...triggers[idx], actions }
  return updated
}

// rebuildActionList mirrors rebuildTriggerList one level down: `rest` empty
// means `id` is the target action; otherwise the only legal next step is
// into one of the action's two nested-trigger slots (`children` or
// `create_zone`'s `config.triggers`), chosen via slotOfNestedTrigger exactly
// like walkPath does.
//
// The config.triggers branch is where the OPAQUE-bag rule actually bites:
// `config` may carry keys this codebase doesn't know about (a different
// action type's config, or a newer server schema's field) — spreading it
// and replacing only `triggers` is what lets those keys round-trip
// untouched instead of being silently dropped by a full re-marshal.
function rebuildActionList(
  actions: AbilityActionDef[],
  id: string,
  rest: NodePath,
  fn: NodeMutator,
): AbilityActionDef[] | undefined {
  const idx = actions.findIndex((a) => a.id === id)
  if (idx === -1) return undefined
  const updated = actions.slice()
  if (rest.length === 0) {
    updated[idx] = (fn as (node: AbilityActionDef) => AbilityActionDef)(actions[idx])
    return updated
  }
  const action = actions[idx]

  // A loop action's nested ACTION list (config.body): descend when the next
  // step is an `action` segment. Spread the opaque config, replacing only
  // `body`, so unknown keys round-trip untouched (same discipline as the
  // config.triggers branch below).
  if (rest[0].kind === 'action') {
    const body = rebuildActionList(loopBodyOf(action), rest[0].id, rest.slice(1), fn)
    if (!body) return undefined
    updated[idx] = { ...action, config: { ...action.config, body } }
    return updated
  }

  if (rest[0].kind !== 'trigger') return undefined
  const slot = slotOfNestedTrigger(action, rest[0].id)
  if (!slot) return undefined

  if (slot === 'children') {
    const children = rebuildTriggerList(action.children!, rest[0].id, rest.slice(1), fn)
    if (!children) return undefined
    updated[idx] = { ...action, children }
    return updated
  }

  // slot === 'config': rebuild config.triggers without disturbing any other
  // key already living in the opaque config bag.
  const cfgTriggers = rebuildTriggerList(configTriggersOf(action), rest[0].id, rest.slice(1), fn)
  if (!cfgTriggers) return undefined
  updated[idx] = { ...action, config: { ...action.config, triggers: cfgTriggers } }
  return updated
}

// --- Duplicate-subtree re-id (fixes the latent bug flagged in the phase-7
// plan) --------------------------------------------------------------------
//
// The OLD duplicateAction structuredClone'd an action and re-id'd ONLY its
// top level. Duplicating a create_zone action clones its nested
// children/config.triggers ids VERBATIM, so the clone and the original end
// up sharing ids several levels down — a save-blocking duplicate_id error
// from the Go validator (which now recurses into config.triggers too). The
// fix: re-id EVERY trigger and action in the cloned subtree, in both
// nesting slots, at any depth.
//
// makeIdMinter seeds a running "used ids" set from collectAllIds(prog) and
// returns a mint(prefix) function that remembers every id it has already
// handed out. This is deliberately NOT just repeated nextUniqueId(prog, ...)
// calls: nextUniqueId recomputes "the next free id" from `prog`, which never
// changes mid-clone, so two sequential calls for the same prefix would both
// return the same id. Minting against a running local set instead lets a
// single duplicate of a multi-node subtree hand out N distinct ids per
// prefix in one pass.
function makeIdMinter(prog: AbilityProgram): (prefix: string) => string {
  const used = new Set(collectAllIds(prog))
  const highest = new Map<string, number>()
  return (prefix: string): string => {
    if (!highest.has(prefix)) {
      let max = 0
      for (const existing of used) {
        if (!existing.startsWith(prefix)) continue
        const rest = existing.slice(prefix.length)
        if (/^\d+$/.test(rest)) max = Math.max(max, Number(rest))
      }
      highest.set(prefix, max)
    }
    const n = highest.get(prefix)! + 1
    highest.set(prefix, n)
    const id = `${prefix}${n}`
    used.add(id)
    return id
  }
}

// reidAction / reidTrigger mutate a freshly structuredClone'd subtree IN
// PLACE. This is safe (and the only place in this file that mutates rather
// than copies) because the clone is brand-new memory nothing else holds a
// reference to yet — mutating it can never violate the "never mutate the
// input program" contract every exported op honors.
function reidAction(a: AbilityActionDef, mint: (prefix: string) => string): AbilityActionDef {
  a.id = mint('a')
  if (a.children) {
    a.children = a.children.map((t) => reidTrigger(t, mint))
  }
  const cfgTriggers = configTriggersOf(a)
  if (cfgTriggers.length > 0) {
    a.config = { ...a.config, triggers: cfgTriggers.map((t) => reidTrigger(t, mint)) }
  }
  const body = loopBodyOf(a)
  if (body.length > 0) {
    a.config = { ...a.config, body: body.map((b) => reidAction(b, mint)) }
  }
  return a
}

function reidTrigger(t: AbilityTriggerDef, mint: (prefix: string) => string): AbilityTriggerDef {
  t.id = mint('t')
  t.actions = t.actions.map((a) => reidAction(a, mint))
  return t
}

// cloneActionWithFreshIds deep-clones `action` (independent of `prog` —
// mutating it below never touches the original tree) and mints a brand new
// id for it and for every trigger/action nested under it, at any depth, in
// either nesting slot.
function cloneActionWithFreshIds(action: AbilityActionDef, prog: AbilityProgram): AbilityActionDef {
  const mint = makeIdMinter(prog)
  return reidAction(structuredClone(action), mint)
}

// --- Mutation ops --------------------------------------------------------
//
// Every op takes a NodePath (any depth) rather than flat triggerId/actionId
// strings — that flat, root-only shape (and a parallel set of overloads
// accepting it) existed only mid-migration, while useAbilityBuilder.ts and
// the Vue consumers still constructed it; both have since moved to NodePath
// (see docs/superpowers/plans/2026-07-16-composable-abilities-phase7-nested-authoring.md,
// Task 5), so the flat overloads have been removed. A root-only call now
// passes a single-segment path (e.g. `[{kind:'trigger', id:'t1'}]`) instead
// of the bare string `'t1'`.
//
// Every body validates the target (and, where relevant, its parent) via
// resolveNode before mutating. This is deliberate, not redundant: per the
// WATCH OUT note on resolveNode, a path whose last segment doesn't name the
// kind of node an op expects (e.g. an action-path handed to an op that wants
// a trigger) would otherwise reach updateNodeAt and invoke `fn` on the wrong
// node shape. resolveNode's full walk validates the ENTIRE path's structure
// in one pass, so checking its `kind` up front turns a caller's malformed
// path into the same safe no-op every other unresolvable-path case already
// gets, instead of a crash.

// addTrigger keeps two overloads — NOT the flat/path duality described
// above, but a genuinely different pair of operations: a bare `type` adds a
// new ROOT trigger (nothing to address; there's no existing node to name),
// while `parentActionPath` nests the new trigger under an action's
// nested-trigger slot (`children`, or the `config.triggers` of an action in
// CONFIG_TRIGGER_ACTION_TYPES — see the ADD-TRIGGER slot rule below).
export function addTrigger(prog: AbilityProgram, type: TriggerType): AbilityProgram
export function addTrigger(prog: AbilityProgram, parentActionPath: NodePath, type: TriggerType): AbilityProgram
export function addTrigger(
  prog: AbilityProgram,
  arg2: TriggerType | NodePath,
  arg3?: TriggerType,
): AbilityProgram {
  const [parentPath, type]: [NodePath, TriggerType] = typeof arg2 === 'string' ? [[], arg2] : [arg2, arg3!]
  const id = nextUniqueId(prog, 't')
  const newTrigger: AbilityTriggerDef = { id, type, actions: [] }

  if (parentPath.length === 0) {
    // Root-level add: the legacy behavior, and also what an explicit empty
    // NodePath means under the new API.
    return { ...prog, triggers: [...prog.triggers, newTrigger] }
  }

  const parentSeg = parentPath[parentPath.length - 1]

  if (parentSeg.kind === 'presentation') {
    if (!(prog.presentations ?? []).some((p) => p.id === parentSeg.id)) return prog
    const presentations = (prog.presentations ?? []).map((p) =>
      p.id === parentSeg.id ? { ...p, triggers: [...(p.triggers ?? []), newTrigger] } : p,
    )
    return { ...prog, presentations }
  }

  if (parentSeg.kind !== 'action') return prog
  const resolvedParent = resolveNode(prog, parentPath)
  if (!resolvedParent || resolvedParent.kind !== 'action') return prog

  return updateNodeAt(prog, parentPath, (action: AbilityActionDef): AbilityActionDef => {
    // ADD-TRIGGER slot rule. Actions in CONFIG_TRIGGER_ACTION_TYPES nest new
    // triggers into `config.triggers`; every other action type nests into
    // `children`.
    //
    // The slot is NOT cosmetic — it decides WHEN the trigger fires, and the
    // wrong one fails silently:
    //   - `children` fires as on_action_complete, i.e. immediately after the
    //     action's own Execute returns.
    //   - `config.triggers` is decoded by the action's own descriptor and
    //     fired by the object it creates — a zone on its tick, a projectile on
    //     its impact (possibly seconds later, in a different tick).
    // Put a launch_projectile's on_projectile_impact trigger into `children`
    // and it fires the instant the bolt is LAUNCHED, not when it lands — and
    // the Go decoder never sees it at all, so it silently does nothing on
    // impact. Keep this list in step with the Go descriptors that decode a
    // `Triggers` field out of their config.
    if (CONFIG_TRIGGER_ACTION_TYPES.has(action.type)) {
      return { ...action, config: { ...action.config, triggers: [...configTriggersOf(action), newTrigger] } }
    }
    return { ...action, children: [...(action.children ?? []), newTrigger] }
  })
}

export function removeTrigger(prog: AbilityProgram, path: NodePath): AbilityProgram {
  const resolved = resolveNode(prog, path)
  if (!resolved || resolved.kind !== 'trigger') return prog

  const targetId = path[path.length - 1].id
  const parentPath = path.slice(0, -1)

  if (parentPath.length === 0) {
    return { ...prog, triggers: prog.triggers.filter((t) => t.id !== targetId) }
  }

  const parentSeg = parentPath[parentPath.length - 1]
  if (parentSeg.kind === 'presentation') {
    const presentations = (prog.presentations ?? []).map((p) =>
      p.id === parentSeg.id ? { ...p, triggers: (p.triggers ?? []).filter((t) => t.id !== targetId) } : p,
    )
    return { ...prog, presentations }
  }

  // parentSeg.kind === 'action': remove targetId from whichever nested slot
  // it currently lives in.
  return updateNodeAt(prog, parentPath, (action: AbilityActionDef): AbilityActionDef => {
    const slot = slotOfNestedTrigger(action, targetId)
    if (slot === 'children') {
      return { ...action, children: action.children!.filter((t) => t.id !== targetId) }
    }
    if (slot === 'config') {
      return {
        ...action,
        config: { ...action.config, triggers: configTriggersOf(action).filter((t) => t.id !== targetId) },
      }
    }
    return action
  })
}

// updateActionListAt rebuilds the ACTION LIST owned by whatever container
// parentPath resolves to — a trigger's `actions` (root/presentation/nested
// trigger), or a `loop` action's `config.body`. This is the single seam the
// list-mutating ops (add/remove/move/duplicate) funnel through so they work
// identically whether an action sits directly under a trigger or inside a loop.
function updateActionListAt(
  prog: AbilityProgram,
  parentPath: NodePath,
  fn: (actions: AbilityActionDef[]) => AbilityActionDef[],
): AbilityProgram {
  const parentSeg = parentPath[parentPath.length - 1]
  if (parentSeg?.kind === 'action') {
    // Loop body: rebuild config.body, preserving every other opaque config key.
    return updateNodeAt(prog, parentPath, (a: AbilityActionDef): AbilityActionDef => ({
      ...a,
      config: { ...a.config, body: fn(loopBodyOf(a)) },
    }))
  }
  return updateNodeAt(prog, parentPath, (t: AbilityTriggerDef): AbilityTriggerDef => ({
    ...t,
    actions: fn(t.actions),
  }))
}

// addAction appends a new action to a CONTAINER: a trigger (its `actions`) or a
// loop action (its `config.body`). containerPath must resolve to one of those.
export function addAction(prog: AbilityProgram, containerPath: NodePath, actionType: ActionType): AbilityProgram {
  const resolved = resolveNode(prog, containerPath)
  if (!resolved) return prog
  const id = nextUniqueId(prog, 'a')
  const newAction: AbilityActionDef = { id, type: actionType, disabled: false }

  if (resolved.kind === 'trigger') {
    return updateNodeAt(prog, containerPath, (t: AbilityTriggerDef): AbilityTriggerDef => ({
      ...t,
      actions: [...t.actions, newAction],
    }))
  }
  if (resolved.kind === 'action' && resolved.node.type === 'loop') {
    return updateNodeAt(prog, containerPath, (a: AbilityActionDef): AbilityActionDef => ({
      ...a,
      config: { ...a.config, body: [...loopBodyOf(a), newAction] },
    }))
  }
  return prog
}

export function removeAction(prog: AbilityProgram, path: NodePath): AbilityProgram {
  const resolved = resolveNode(prog, path)
  if (!resolved || resolved.kind !== 'action') return prog

  const targetId = path[path.length - 1].id
  const parentPath = path.slice(0, -1)
  return updateActionListAt(prog, parentPath, (actions) => actions.filter((a) => a.id !== targetId))
}

export function moveAction(prog: AbilityProgram, path: NodePath, dir: 'up' | 'down'): AbilityProgram {
  const resolved = resolveNode(prog, path)
  if (!resolved || resolved.kind !== 'action') return prog

  const targetId = path[path.length - 1].id
  const parentPath = path.slice(0, -1)
  return updateActionListAt(prog, parentPath, (actions) => {
    const idx = actions.findIndex((a) => a.id === targetId)
    if (idx === -1) return actions
    const swapIdx = dir === 'up' ? idx - 1 : idx + 1
    if (swapIdx < 0 || swapIdx >= actions.length) return actions
    const next = actions.slice()
    const tmp = next[idx]
    next[idx] = next[swapIdx]
    next[swapIdx] = tmp
    return next
  })
}

export function duplicateAction(prog: AbilityProgram, path: NodePath): AbilityProgram {
  const resolved = resolveNode(prog, path)
  if (!resolved || resolved.kind !== 'action') return prog

  const targetId = path[path.length - 1].id
  const parentPath = path.slice(0, -1)
  const copy = cloneActionWithFreshIds(resolved.node, prog)
  return updateActionListAt(prog, parentPath, (actions) => {
    const idx = actions.findIndex((a) => a.id === targetId)
    if (idx === -1) return actions
    const next = actions.slice()
    next.splice(idx + 1, 0, copy)
    return next
  })
}

export function setActionDisabled(prog: AbilityProgram, path: NodePath, disabled: boolean): AbilityProgram {
  const resolved = resolveNode(prog, path)
  if (!resolved || resolved.kind !== 'action') return prog

  return updateNodeAt(prog, path, (a: AbilityActionDef): AbilityActionDef => ({ ...a, disabled }))
}

export function updateAction(
  prog: AbilityProgram,
  path: NodePath,
  patch: Partial<AbilityActionDef>,
): AbilityProgram {
  const resolved = resolveNode(prog, path)
  if (!resolved || resolved.kind !== 'action') return prog

  return updateNodeAt(prog, path, (a: AbilityActionDef): AbilityActionDef => ({ ...a, ...patch }))
}

export function updateTrigger(
  prog: AbilityProgram,
  path: NodePath,
  patch: Partial<AbilityTriggerDef>,
): AbilityProgram {
  const resolved = resolveNode(prog, path)
  if (!resolved || resolved.kind !== 'trigger') return prog

  return updateNodeAt(prog, path, (t: AbilityTriggerDef): AbilityTriggerDef => ({ ...t, ...patch }))
}
