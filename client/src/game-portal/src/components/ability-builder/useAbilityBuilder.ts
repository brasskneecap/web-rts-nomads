// useAbilityBuilder: the reactive state backbone for the composable ability
// editor. Owns the loaded ability list, the action schema + display
// catalogs, the currently-edited form/program pair, tree selection,
// debounced server validation, and undo/redo. No UI here — every
// ability-builder component consumes this composable rather than talking to
// the API or programTree.ts directly.

import { computed, markRaw, ref, shallowRef } from 'vue'
import type { AbilityEditorForm, AuthoredAbilityDef } from '@/game/abilities/abilityEditorForm'
import { createBlankForm, formFromDef, saveRequestFromForm } from '@/game/abilities/abilityEditorForm'
import type { AbilityProgram, ActionType, TriggerType } from '@/game/abilities/program/abilityProgram'
import { parseProgram, serializeProgram } from '@/game/abilities/program/abilityProgram'
import type { ActionSchemaBundle } from '@/game/abilities/program/programSchema'
import type { ValidationIssue } from '@/game/abilities/program/programValidation'
import { hasBlockingError } from '@/game/abilities/program/programValidation'
import {
  EditorValidationError,
  convertAbility,
  deleteEditorAbility,
  fetchAbilityCategories,
  fetchActionSchema,
  fetchAuthoredAbilityDefs,
  fetchAutoCastSelectors,
  fetchDamageTypes,
  fetchEffectIds,
  fetchProjectileIds,
  saveEditorAbility,
  validateAbilityProgram,
} from '@/game/abilities/abilityEditorApi'
import { fetchAuthoredUnitDefs } from '@/game/units/unitEditorApi'
import { fetchActionIcons } from '@/game/maps/catalog'
import { ACTION_ICON_MAP, initActionIcons } from '@/game/maps/actionIconDefs'
import * as tree from './programTree'
import type { NodePath, NodeRef } from './programTree'

export type { NodePath, NodeRef } from './programTree'

const VALIDATE_DEBOUNCE_MS = 300
const UNDO_STACK_CAP = 50

export interface AbilityBuilderCatalogs {
  effects: string[]
  projectiles: string[]
  damageTypes: string[]
  categories: string[]
  autoCastSelectors: string[]
  unitTypes: string[]
}

interface Snapshot {
  form: AbilityEditorForm
  program: AbilityProgram
  selected: NodeRef
}

function emptyCatalogs(): AbilityBuilderCatalogs {
  return { effects: [], projectiles: [], damageTypes: [], categories: [], autoCastSelectors: [], unitTypes: [] }
}

function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

export function useAbilityBuilder() {
  // ── Loaded data ────────────────────────────────────────────────────────
  // shallowRef: these are always replaced wholesale from a fetch response,
  // never mutated field-by-field. Using a deep `ref` here would make
  // `abilities` entries (and anything copied out of them, e.g. a
  // selected ability's `compiledProgram`) reactive Proxies — which then
  // leak into `program`/`form` and break snapshot()'s structuredClone().
  const abilities = shallowRef<AuthoredAbilityDef[]>([])
  const schema = shallowRef<ActionSchemaBundle | null>(null)
  const catalogs = shallowRef<AbilityBuilderCatalogs>(emptyCatalogs())
  const loadError = ref('')

  // ── Editing session ────────────────────────────────────────────────────
  const editing = ref(false)
  // form/program/selected are always REPLACED wholesale by the ops below
  // (never mutated in place — that's what makes undo/redo correct), so a
  // shallowRef is both correct and avoids deep-proxying the whole program
  // tree. It also keeps snapshot()'s structuredClone() operating on plain
  // objects instead of Vue reactive Proxies (which structuredClone can't
  // clone).
  const form = shallowRef<AbilityEditorForm>(createBlankForm())
  const program = shallowRef<AbilityProgram>(tree.emptyProgram())
  const isLegacy = ref(false)
  const runnable = ref(false)
  const selected = shallowRef<NodeRef>({ kind: 'ability' })
  const issues = ref<ValidationIssue[]>([])
  const dirty = ref(false)
  const busy = ref(false)
  const saveError = ref('')
  const savedLabel = ref('')
  const warnings = ref<string[]>([])
  // statusNote: transient feedback after remove() — "deleted" vs "reverted"
  // vs "reset" are three different outcomes the author needs to see, not
  // just a generic success/fail (see remove() below and the Items editor's
  // matching removeOrReset()).
  const statusNote = ref('')

  // ── Undo / redo ────────────────────────────────────────────────────────
  const undoStack = ref<Snapshot[]>([])
  const redoStack = ref<Snapshot[]>([])
  const canUndo = computed(() => undoStack.value.length > 0)
  const canRedo = computed(() => redoStack.value.length > 0)

  // snapshot() clones the current form/program/selected into a plain,
  // detached object. `markRaw` keeps that object out of Vue's reactivity
  // system once it lands in undoStack/redoStack (which ARE deep refs, for
  // canUndo/canRedo's `.length` tracking) — otherwise a later
  // structuredClone() on a popped, Vue-proxied snapshot would throw
  // ("could not be cloned").
  function snapshot(): Snapshot {
    return markRaw(structuredClone({ form: form.value, program: program.value, selected: selected.value }))
  }

  function restoreSnapshot(s: Snapshot) {
    const restored = structuredClone(s)
    form.value = restored.form
    program.value = restored.program
    selected.value = restored.selected
  }

  // pushHistory records the CURRENT state before a mutation is applied, so
  // undo() can restore it. Any new mutation invalidates the redo stack.
  function pushHistory() {
    undoStack.value.push(snapshot())
    if (undoStack.value.length > UNDO_STACK_CAP) undoStack.value.shift()
    redoStack.value = []
  }

  function undo() {
    const prev = undoStack.value.pop()
    if (!prev) return
    redoStack.value.push(snapshot())
    restoreSnapshot(prev)
    dirty.value = true
    scheduleValidate()
  }

  function redo() {
    const next = redoStack.value.pop()
    if (!next) return
    undoStack.value.push(snapshot())
    restoreSnapshot(next)
    dirty.value = true
    scheduleValidate()
  }

  // applyProgramMutation is the single funnel every programTree op goes
  // through: snapshot for undo, apply the pure op, mark dirty, re-validate.
  function applyProgramMutation(fn: (p: AbilityProgram) => AbilityProgram) {
    pushHistory()
    program.value = fn(program.value)
    dirty.value = true
    scheduleValidate()
  }

  // addTrigger keeps the same two-overload shape as tree.addTrigger: a bare
  // `type` adds a new ROOT trigger, while `parentActionPath` nests it under
  // an action's own nested-trigger slot (children, or create_zone's
  // config.triggers — programTree.addTrigger picks the slot, not the
  // caller). This overload was the one piece of Task 5's migration FlowTrigger
  // Card's nested "+ Trigger" affordance (phase-7 Task 6) actually needed —
  // every other consumer only ever added root triggers.
  function addTrigger(type: TriggerType): void
  function addTrigger(parentActionPath: NodePath, type: TriggerType): void
  function addTrigger(arg1: TriggerType | NodePath, arg2?: TriggerType) {
    if (typeof arg1 === 'string') {
      applyProgramMutation((p) => tree.addTrigger(p, arg1))
    } else {
      applyProgramMutation((p) => tree.addTrigger(p, arg1, arg2!))
    }
  }

  function removeTrigger(path: NodePath) {
    applyProgramMutation((p) => tree.removeTrigger(p, path))
  }

  // addAction appends actionType to the trigger at triggerPath's actions
  // (last position — see programTree.addAction) and returns the new action's
  // id so callers (e.g. AddActionDialog) can immediately focus it. It also
  // selects the new action itself, so the inspector rail follows the
  // freshly-added action without requiring the caller to do so.
  function addAction(triggerPath: NodePath, actionType: ActionType): string {
    applyProgramMutation((p) => tree.addAction(p, triggerPath, actionType))
    const resolved = tree.resolveNode(program.value, triggerPath)
    const newId = resolved?.kind === 'trigger' ? resolved.node.actions.at(-1)?.id : undefined
    if (newId) select({ kind: 'action', path: [...triggerPath, { kind: 'action', id: newId }] })
    return newId ?? ''
  }

  function removeAction(path: NodePath) {
    applyProgramMutation((p) => tree.removeAction(p, path))
  }

  function moveAction(path: NodePath, dir: 'up' | 'down') {
    applyProgramMutation((p) => tree.moveAction(p, path, dir))
  }

  function duplicateAction(path: NodePath) {
    applyProgramMutation((p) => tree.duplicateAction(p, path))
  }

  function toggleActionDisabled(path: NodePath) {
    const resolved = tree.resolveNode(program.value, path)
    const current = resolved?.kind === 'action' ? resolved.node : undefined
    const next = !(current?.disabled ?? false)
    applyProgramMutation((p) => tree.setActionDisabled(p, path, next))
  }

  function updateAction(path: NodePath, patch: Partial<AbilityProgram['triggers'][number]['actions'][number]>) {
    applyProgramMutation((p) => tree.updateAction(p, path, patch))
  }

  function updateTrigger(path: NodePath, patch: Partial<AbilityProgram['triggers'][number]>) {
    applyProgramMutation((p) => tree.updateTrigger(p, path, patch))
  }

  // updateActionConfig is a convenience for inspector-style components that
  // edit one config key at a time: it merges configPatch onto the action's
  // EXISTING config (rather than requiring the caller to read + spread it
  // themselves) and routes through updateAction, so it still gets the usual
  // snapshot/dirty/validate treatment via applyProgramMutation.
  function updateActionConfig(path: NodePath, configPatch: Record<string, unknown>) {
    const resolved = tree.resolveNode(program.value, path)
    const action = resolved?.kind === 'action' ? resolved.node : undefined
    const merged = { ...(action?.config ?? {}), ...configPatch }
    updateAction(path, { config: merged })
  }

  // updateProgram is the general immutable-rewrite escape hatch for editors
  // that operate on parts of the program the path-based tree ops don't address
  // — e.g. the LoopCard, which edits a chain's namedTriggers body (named
  // triggers aren't in the NodePath traversal). It routes through the same
  // applyProgramMutation funnel, so it still snapshots for undo, marks dirty,
  // and re-validates like every other mutation.
  function updateProgram(fn: (p: AbilityProgram) => AbilityProgram) {
    applyProgramMutation(fn)
  }

  function updateForm(patch: Partial<AuthoredAbilityDef>) {
    pushHistory()
    form.value = { ...form.value, ...patch }
    dirty.value = true
    scheduleValidate()
  }

  function select(ref: NodeRef) {
    selected.value = ref
  }

  // ── Validation (debounced, server-authoritative) ──────────────────────
  let validateTimer: ReturnType<typeof setTimeout> | null = null
  let validateToken = 0

  // buildCandidateDef mirrors the shape sent to save(): the modeled form
  // fields (remainder re-merged, display-only fields stripped) plus the
  // current in-progress program, always tagged schemaVersion 2.
  function buildCandidateDef(): AuthoredAbilityDef {
    const base = saveRequestFromForm(form.value)
    return {
      ...base,
      schemaVersion: 2,
      // serializeProgram's Record<string, unknown> is the wire shape; the
      // AuthoredAbilityDef.program field type is AbilityProgram, but this
      // object only ever travels to fetch()'s JSON.stringify, never back
      // through typed program logic, so the cast is safe.
      program: serializeProgram(program.value) as unknown as AbilityProgram,
    }
  }

  async function revalidate() {
    const token = ++validateToken
    try {
      const result = await validateAbilityProgram(buildCandidateDef())
      if (token === validateToken) issues.value = result
    } catch {
      if (token === validateToken) issues.value = []
    }
  }

  function scheduleValidate() {
    if (validateTimer) clearTimeout(validateTimer)
    validateTimer = setTimeout(() => {
      validateTimer = null
      void revalidate()
    }, VALIDATE_DEBOUNCE_MS)
  }

  // ── Loading ────────────────────────────────────────────────────────────
  async function reloadAbilities() {
    abilities.value = await fetchAuthoredAbilityDefs()
  }

  async function load() {
    loadError.value = ''
    try {
      const [abilityDefs, schemaBundle, effects, projectiles, damageTypes, categories, autoCastSelectors, units, actionIcons] =
        await Promise.all([
          fetchAuthoredAbilityDefs(),
          fetchActionSchema(),
          fetchEffectIds(),
          fetchProjectileIds(),
          fetchDamageTypes(),
          fetchAbilityCategories(),
          fetchAutoCastSelectors(),
          fetchAuthoredUnitDefs(),
          fetchActionIcons(),
        ])
      abilities.value = abilityDefs
      schema.value = schemaBundle
      // Populate the shared action-icon lookup so the preview scene can draw
      // overhead status icons (apply_mark) and any ACTION_ICON_MAP-based icon.
      // In a live match GameClient.start() fills this; the standalone editor
      // never runs that, so without this the preview silently skips every
      // overhead icon (ACTION_ICON_MAP is empty). Guard on size so we don't
      // clobber an already-populated map from a match played this session.
      if (ACTION_ICON_MAP.size === 0) {
        initActionIcons(actionIcons)
      }
      catalogs.value = {
        effects,
        projectiles,
        damageTypes,
        categories,
        autoCastSelectors,
        unitTypes: units.map((u) => u.type),
      }
    } catch (e) {
      loadError.value = errorMessage(e)
    }
  }

  // resetSession clears undo/redo + transient status for a freshly-opened
  // ability (whether an existing one or a new blank one).
  function resetSession() {
    undoStack.value = []
    redoStack.value = []
    dirty.value = false
    warnings.value = []
    saveError.value = ''
    savedLabel.value = ''
    statusNote.value = ''
  }

  function selectAbility(id: string) {
    const def = abilities.value.find((a) => a.id === id)
    if (!def) return
    form.value = formFromDef(def)
    const rawProgram = def.program ?? def.compiledProgram
    program.value = rawProgram ? parseProgram(rawProgram) : tree.emptyProgram()
    isLegacy.value = (def.schemaVersion ?? 0) < 2
    runnable.value = def.runnable ?? false
    selected.value = { kind: 'ability' }
    editing.value = true
    resetSession()
    void revalidate()
  }

  function newAbility() {
    form.value = createBlankForm()
    program.value = tree.emptyProgram()
    isLegacy.value = false
    runnable.value = false
    selected.value = { kind: 'ability' }
    editing.value = true
    resetSession()
    issues.value = []
  }

  // ── Save / convert / delete ───────────────────────────────────────────
  async function save() {
    saveError.value = ''
    // A legacy (schemaVersion < 2) ability must go through convert() first —
    // saving it directly here would silently persist schemaVersion:2 + the
    // server-computed compiledProgram without the user ever seeing convert()'s
    // migration warnings. The UI additionally disables Save while legacy;
    // this is defense-in-depth.
    if (isLegacy.value) {
      saveError.value = 'Convert this ability to composable before saving.'
      return
    }
    if (hasBlockingError(issues.value)) {
      saveError.value = 'Fix validation errors before saving.'
      return
    }
    busy.value = true
    try {
      const payload = buildCandidateDef()
      await saveEditorAbility(payload)
      const savedId = payload.id
      await reloadAbilities()
      if (abilities.value.some((a) => a.id === savedId)) {
        selectAbility(savedId)
      }
      dirty.value = false
      savedLabel.value = 'just now'
    } catch (e) {
      saveError.value = e instanceof EditorValidationError ? e.serverMessage : errorMessage(e)
    } finally {
      busy.value = false
    }
  }

  async function convert() {
    if (!isLegacy.value) return
    busy.value = true
    saveError.value = ''
    try {
      const { ability, warnings: convertWarnings, runnable: convertRunnable } = await convertAbility(form.value.id)
      form.value = formFromDef(ability)
      program.value = ability.program ? parseProgram(ability.program) : tree.emptyProgram()
      isLegacy.value = false
      runnable.value = convertRunnable
      // resetSession() clears undo/redo so the converted program becomes a
      // clean baseline — without this, undo() could jump straight back to
      // the pre-conversion legacy form with no way to redo forward past it.
      // resetSession() also clears warnings/dirty; restore both since the
      // user still has unsaved (converted-but-not-persisted) work and
      // convert()'s migration warnings are exactly what they need to see.
      resetSession()
      warnings.value = convertWarnings
      dirty.value = true
      scheduleValidate()
    } catch (e) {
      saveError.value = errorMessage(e)
    } finally {
      busy.value = false
    }
  }

  // remove() deletes an author-created ability, or undoes the last save on a
  // shipped one. The server decides which and says so in its status (see
  // DeleteEditorAbility): `deleted` genuinely removes it (close the editor —
  // there's nothing left to show); `reverted`/`reset` restore it to an
  // earlier state, so the ability still exists and must be reloaded back
  // into the editor rather than closed (closing would read as "gone").
  async function remove() {
    const id = form.value.id
    if (!id) return
    busy.value = true
    saveError.value = ''
    statusNote.value = ''
    try {
      const status = await deleteEditorAbility(id)
      await reloadAbilities()
      if (status === 'deleted') {
        editing.value = false
        statusNote.value = 'Ability deleted.'
        return
      }
      selectAbility(id) // reload the restored def back into the editor
      statusNote.value = status === 'reverted'
        ? 'Reverted to the state before your last save.'
        : 'Reset to the catalog default.'
    } catch (e) {
      saveError.value = e instanceof EditorValidationError ? e.serverMessage : errorMessage(e)
    } finally {
      busy.value = false
    }
  }

  return {
    // loaded data
    abilities,
    schema,
    catalogs,
    loadError,
    // editing session
    editing,
    form,
    program,
    isLegacy,
    runnable,
    selected,
    issues,
    dirty,
    busy,
    saveError,
    savedLabel,
    warnings,
    statusNote,
    // undo/redo
    canUndo,
    canRedo,
    undo,
    redo,
    // tree mutations
    addTrigger,
    removeTrigger,
    addAction,
    removeAction,
    moveAction,
    duplicateAction,
    toggleActionDisabled,
    updateAction,
    updateActionConfig,
    updateTrigger,
    updateProgram,
    updateForm,
    select,
    // validation
    revalidate,
    scheduleValidate,
    // lifecycle
    load,
    selectAbility,
    newAbility,
    save,
    convert,
    remove,
  }
}
