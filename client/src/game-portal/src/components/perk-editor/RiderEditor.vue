<template>
  <div class="rider-editor" data-test="rider-editor">
    <div class="rider-editor__head">
      <label class="rider-editor__field">
        Target Ability
        <input v-model="targetProxy" :list="targetListId" placeholder="ability id" aria-label="Target ability" />
      </label>
      <label class="rider-editor__field">
        Trigger
        <select v-model="triggerProxy" aria-label="Trigger">
          <option value="">(choose)</option>
          <option v-for="t in triggerTypeOptions" :key="t" :value="t">{{ humanizeActionType(t) }}</option>
        </select>
      </label>
    </div>

    <div class="rider-editor__actions">
      <p v-if="actions.length === 0" class="rider-editor__hint-line">No actions yet.</p>
      <FlowActionCard
        v-for="(a, i) in actions"
        :key="a.id"
        :action="a"
        :index="i"
        :count="actions.length"
        :path="actionPath(a.id)"
      />
      <div class="rider-editor__add-action">
        <select v-model="newActionType" aria-label="New action type">
          <option v-for="t in actionTypeOptions" :key="t" :value="t">{{ humanizeActionType(t) }}</option>
        </select>
        <button type="button" class="rider-editor__row-add" data-test="rider-add-action" @click="onAddAction">
          + Action
        </button>
      </div>
    </div>

    <!-- Inline inspector for whichever action is currently selected (via
         FlowActionCard's onSelect -> localBuilder.select). This is the
         scoped-down equivalent of InspectorBar.vue's "Action" branch — same
         schemaForAction/fieldVisible grouping, same SchemaField per field —
         trimmed to what a rider fragment needs (no trigger branch, since a
         rider has no trigger CARD of its own; no output/unread-save warning,
         which only make sense across the target ability's full action tree). -->
    <div v-if="selectedAction" class="rider-editor__inspector" data-test="rider-inspector">
      <p class="rider-editor__inspector-head">
        {{ selectedAction.type }} <span class="rider-editor__hint">(id: {{ selectedAction.id }})</span>
      </p>
      <p v-if="fieldSections.length === 0" class="rider-editor__hint-line">No configurable fields for this action type.</p>
      <div v-for="[section, fields] in fieldSections" :key="section" class="rider-editor__field-section">
        <h4 class="rider-editor__field-section-title">{{ section }}</h4>
        <template v-for="f in fields" :key="f.key">
          <SchemaField
            v-if="f.control === 'target_query'"
            :field="f"
            :model-value="selectedAction.target"
            :enums="enumsValue"
            :catalogs="catalogs"
            @update:model-value="commitActionTarget"
          />
          <SchemaField
            v-else
            :field="f"
            :model-value="selectedAction.config?.[f.key]"
            :enums="enumsValue"
            :catalogs="catalogs"
            @update:model-value="(v) => commitActionConfig(f.key, v)"
          />
        </template>
      </div>
    </div>

    <datalist :id="targetListId">
      <option v-for="id in abilityIds" :key="id" :value="id" />
    </datalist>
  </div>
</template>

<script lang="ts">
// MODULE-scoped (not inside <script setup>, which re-executes its top-level
// statements once PER COMPONENT INSTANCE): a `let` declared inside
// <script setup> resets to its initializer on every mount, so every
// RiderEditor instance would compute the same "0" suffix and collide on the
// same datalist id. This plain <script> block's top level runs once per
// MODULE, so the counter is genuinely shared/incrementing across instances —
// mirrors the two-block idiom for a component that needs one script-setup
// per instance plus one counter shared across instances.
let uidCounter = 0
</script>

<script setup lang="ts">
// RiderEditor authors ONE AbilityRider — a perk-grafted action fragment that
// runs when its `target` ability fires its `trigger` — using the SAME
// widgets the composable ability builder uses (FlowActionCard, SchemaField),
// so a rider's actions are edited in the identical vocabulary an author
// already knows from the Ability editor.
//
// A rider's `actions` is a FLAT AbilityActionDef[] — one trigger's worth of
// steps, not a full AbilityProgram. FlowActionCard (and the mutation ops it
// calls through its injected builder) are written against the real
// programTree.ts, which operates on a full AbilityProgram. Rather than
// reimplementing FlowActionCard/SchemaField's rendering against a bespoke
// flat-array model, this component wraps `actions` in a SYNTHETIC one-trigger
// AbilityProgram (see `buildProgram`/SYN_TRIGGER_ID below) purely as an
// address space for NodePath, and drives every mutation through the REAL,
// already-tested programTree.ts ops (tree.addAction/removeAction/moveAction/
// duplicateAction/setActionDisabled/updateAction) — the same ops
// useAbilityBuilder.ts's applyProgramMutation funnels through. The synthetic
// wrapper never leaves this component: every mutation immediately unwraps
// `program.value.triggers[0].actions` back out to a plain AbilityRider and
// emits it.
//
// FlowActionCard (and, transitively, SchemaField for target_query fields)
// reads its data through an INJECTED AbilityBuilder (see
// AbilityBuilderContext.ts) rather than props — useAbilityBuilder()'s full
// composable additionally owns the loaded ability list, undo/redo, debounced
// server validation, and save/convert/delete, none of which apply to editing
// one rider fragment in place. `localBuilder` below implements only the
// slice of that API FlowActionCard actually reads/calls (mirrors
// FlowActionCard.test.ts's own `makeBuilderStub` — see that file), and is
// cast to the full AbilityBuilder type so the injected context typechecks at
// the provide() call site; nothing this component renders touches the
// untouched fields (abilities, undo/redo, save, ...).
import { computed, provide, ref, shallowRef, watch } from 'vue'
import type {
  AbilityActionDef,
  AbilityProgram,
  ActionType,
  TargetQueryDef,
  TriggerType,
} from '@/game/abilities/program/abilityProgram'
import { fieldVisible, schemaForAction, type ActionSchemaBundle, type SchemaField as SchemaFieldDescriptor } from '@/game/abilities/program/programSchema'
import type { ValidationIssue } from '@/game/abilities/program/programValidation'
import type { AbilityRider } from '@/game/perks/perkEditorForm'
import { AbilityBuilderKey, type AbilityBuilder } from '@/components/ability-builder/AbilityBuilderContext'
import type { AbilityBuilderCatalogs } from '@/components/ability-builder/useAbilityBuilder'
import * as tree from '@/components/ability-builder/programTree'
import type { NodePath, NodeRef } from '@/components/ability-builder/programTree'
import { humanizeActionType } from '@/components/ability-builder/summarizeAction'
import FlowActionCard from '@/components/ability-builder/FlowActionCard.vue'
import SchemaField from '@/components/ability-builder/SchemaField.vue'

const props = defineProps<{
  modelValue: AbilityRider
  /** Target-ability id datalist source — fetched once by the parent
   *  (PerkEditorPanel) and shared across every RiderEditor instance, rather
   *  than each rider re-fetching the same ability list. */
  abilityIds: string[]
  /** The composable ability builder's action schema bundle (fields per
   *  action type + shared enums), fetched once by the parent and shared —
   *  same reasoning as `abilityIds`. */
  schema: ActionSchemaBundle | null
  /** Display catalogs (effects, projectiles, damage types, ...) SchemaField
   *  needs to resolve enum/asset controls — same source useAbilityBuilder()
   *  loads for the real Ability editor. */
  catalogs: AbilityBuilderCatalogs
}>()

const emit = defineEmits<{ 'update:modelValue': [value: AbilityRider] }>()

const targetListId = `rider-editor-ability-ids-${uidCounter++}`

const targetProxy = computed<string>({
  get: () => props.modelValue.target,
  set: (v) => emit('update:modelValue', { ...props.modelValue, target: v }),
})

const triggerProxy = computed<string>({
  get: () => props.modelValue.trigger,
  set: (v) => emit('update:modelValue', { ...props.modelValue, trigger: v }),
})

// ── Trigger type options ────────────────────────────────────────────────
// Sourced from the server's ProgramEnums (schema.enums.triggerTypes) — the
// SAME enum the ability builder's own Flow/Inspector views use. The curated
// fallback (schema not yet loaded) is deliberately WIDER than AbilityFlow's
// or InspectorBar's own fallbacks: a rider's whole purpose is grafting onto
// a NESTED trigger slot (e.g. shared_suffering's target siphon_life fires
// `on_beam_tick`, which only exists inside the beam action's own config —
// see the module comment on this component's limitation note below), so a
// root-trigger-only curated list would silently exclude the one type most
// riders actually need.
const CURATED_TRIGGER_TYPES: TriggerType[] = [
  'on_cast_start', 'on_cast_complete', 'on_animation_marker',
  'on_projectile_impact', 'on_projectile_tick', 'on_beam_impact', 'on_beam_tick',
  'on_zone_tick', 'on_zone_enter', 'on_zone_exit',
  'on_status_tick', 'on_status_expire', 'on_damage_dealt', 'on_unit_death',
  'on_action_complete', 'on_charge_full',
]
const triggerTypeOptions = computed<string[]>(() => {
  const fromSchema = props.schema?.enums.triggerTypes
  return fromSchema && fromSchema.length > 0 ? fromSchema : CURATED_TRIGGER_TYPES
})

// KNOWN LIMITATION: this list is every trigger type the enum knows about, not
// scoped to which triggers the currently-picked target ability actually
// fires. Filtering that precisely would mean walking the target ability's
// own program looking for a matching trigger at ANY nesting depth (including
// inside actions' opaque config, e.g. beam's config.triggers) — worth doing
// later, not required for authoring a rider correctly today (the author is
// expected to know which trigger their target ability fires, same as they
// would reading its Flow view).

// ── Action type options (for "+ Action") ────────────────────────────────
const CURATED_ACTION_TYPES: ActionType[] = [
  'select_targets', 'store_targets', 'filter_targets', 'deal_damage', 'restore_health',
  'apply_status', 'remove_status', 'create_zone', 'launch_projectile', 'beam',
  'summon_unit', 'move_unit', 'apply_force', 'modify_resource', 'trigger_event',
  'play_presentation', 'play_sound', 'change_render_layer', 'camera_shake', 'wait',
  'conditional', 'repeat', 'loop',
]
const actionTypeOptions = computed<string[]>(() => {
  const fromSchema = props.schema?.enums.actionTypes
  return fromSchema && fromSchema.length > 0 ? fromSchema : CURATED_ACTION_TYPES
})

const newActionType = ref<string>(actionTypeOptions.value[0] ?? 'deal_damage')
watch(actionTypeOptions, (opts) => {
  if (!opts.includes(newActionType.value)) newActionType.value = opts[0] ?? 'deal_damage'
})

// ── Synthetic single-trigger program: an address space for NodePath ────────
const SYN_TRIGGER_ID = 'rider'

function buildProgram(actionsList: AbilityActionDef[]): AbilityProgram {
  return {
    entry: { type: 'no_target', range: 0 },
    triggers: [{ id: SYN_TRIGGER_ID, type: 'custom', actions: actionsList }],
  }
}

const program = shallowRef<AbilityProgram>(buildProgram(props.modelValue.actions ?? []))

// Re-sync from an EXTERNAL swap (a different rider row, or a different perk
// entirely, was selected) — never from our OWN round-tripped emit, which
// always carries the exact `actions` array program.value just produced (same
// reference), so this guard also doubles as a no-op short-circuit for that
// case.
watch(
  () => props.modelValue.actions,
  (actionsList) => {
    if (actionsList !== program.value.triggers[0]?.actions) {
      program.value = buildProgram(actionsList ?? [])
    }
  },
)

function actionPath(id: string): NodePath {
  return [{ kind: 'trigger', id: SYN_TRIGGER_ID }, { kind: 'action', id }]
}

const actions = computed<AbilityActionDef[]>(() => program.value.triggers[0]?.actions ?? [])

function applyMutation(fn: (p: AbilityProgram) => AbilityProgram) {
  const next = fn(program.value)
  program.value = next
  emit('update:modelValue', { ...props.modelValue, actions: next.triggers[0]?.actions ?? [] })
}

// ── Selection + inline inspector (the scoped InspectorBar equivalent) ─────
const selected = shallowRef<NodeRef>({ kind: 'ability' })
// No server-side validation for a rider fragment in isolation (there is no
// standalone "validate this fragment" endpoint) — issues stays empty, so
// FlowActionCard's badge simply never renders here. Round-trip correctness
// is still checked at Save time, same as any other perk field.
const issues = ref<ValidationIssue[]>([])

const localBuilder = {
  schema: computed(() => props.schema),
  program,
  selected,
  issues,
  select: (r: NodeRef) => { selected.value = r },
  moveAction: (path: NodePath, dir: 'up' | 'down') => applyMutation((p) => tree.moveAction(p, path, dir)),
  removeAction: (path: NodePath) => {
    applyMutation((p) => tree.removeAction(p, path))
    if (selected.value.kind === 'action' && tree.pathsEqual(selected.value.path, path)) {
      selected.value = { kind: 'ability' }
    }
  },
  duplicateAction: (path: NodePath) => applyMutation((p) => tree.duplicateAction(p, path)),
  toggleActionDisabled: (path: NodePath) => {
    const resolved = tree.resolveNode(program.value, path)
    const current = resolved?.kind === 'action' ? resolved.node : undefined
    applyMutation((p) => tree.setActionDisabled(p, path, !(current?.disabled ?? false)))
  },
  updateAction: (path: NodePath, patch: Partial<AbilityActionDef>) => applyMutation((p) => tree.updateAction(p, path, patch)),
  updateActionConfig: (path: NodePath, configPatch: Record<string, unknown>) => {
    const resolved = tree.resolveNode(program.value, path)
    const action = resolved?.kind === 'action' ? resolved.node : undefined
    const merged = { ...(action?.config ?? {}), ...configPatch }
    applyMutation((p) => tree.updateAction(p, path, { config: merged }))
  },
  addAction: (containerPath: NodePath, actionType: ActionType) => {
    applyMutation((p) => tree.addAction(p, containerPath, actionType))
    const newId = actions.value.at(-1)?.id
    if (newId) selected.value = { kind: 'action', path: actionPath(newId) }
  },
}

// See this component's module doc comment: localBuilder implements only the
// slice of AbilityBuilder that FlowActionCard reads/calls, not the full
// ability-authoring session. The cast is what lets that narrower object
// satisfy the injected context's full type.
provide(AbilityBuilderKey, localBuilder as unknown as AbilityBuilder)

function onAddAction() {
  if (!newActionType.value) return
  localBuilder.addAction([{ kind: 'trigger', id: SYN_TRIGGER_ID }], newActionType.value as ActionType)
}

// ── Inline inspector for the selected action ────────────────────────────
const enumsValue = computed(() => props.schema?.enums ?? {})

const selectedAction = computed<AbilityActionDef | undefined>(() => {
  const sel = selected.value
  if (sel.kind !== 'action') return undefined
  const resolved = tree.resolveNode(program.value, sel.path)
  return resolved?.kind === 'action' ? resolved.node : undefined
})

const actionSchema = computed(() => {
  const action = selectedAction.value
  if (!action || !props.schema) return undefined
  return schemaForAction(props.schema, action.type)
})

const fieldSections = computed<[string, SchemaFieldDescriptor[]][]>(() => {
  const fields = actionSchema.value?.fields ?? []
  const config = selectedAction.value?.config ?? {}
  const groups = new Map<string, SchemaFieldDescriptor[]>()
  for (const f of fields) {
    if (!fieldVisible(f, config)) continue
    const section = f.section || 'Properties'
    const list = groups.get(section) ?? []
    list.push(f)
    groups.set(section, list)
  }
  return [...groups.entries()]
})

function commitActionConfig(key: string, value: unknown) {
  if (!selectedAction.value || selected.value.kind !== 'action') return
  localBuilder.updateActionConfig(selected.value.path, { [key]: value })
}

function commitActionTarget(value: unknown) {
  if (!selectedAction.value || selected.value.kind !== 'action') return
  localBuilder.updateAction(selected.value.path, { target: value as TargetQueryDef })
}
</script>

<style scoped>
.rider-editor {
  display: flex;
  flex-direction: column;
  gap: 8px;
  flex: 1 1 auto;
  min-width: 0;
  padding: 8px;
  border: 1px solid rgba(148, 163, 184, 0.16);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.35);
}

.rider-editor__head {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.rider-editor__field {
  display: grid;
  gap: 4px;
  flex: 1 1 160px;
  min-width: 140px;
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.75rem;
}

.rider-editor__field input,
.rider-editor__field select {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
  font-family: inherit;
}

.rider-editor__actions {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.rider-editor__add-action {
  display: flex;
  align-items: center;
  gap: 8px;
}

.rider-editor__add-action select {
  flex: 1 1 auto;
  min-width: 0;
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 6px 8px;
  font-size: 0.76rem;
}

.rider-editor__row-add {
  flex: 0 0 auto;
  border: 1px solid rgba(215, 187, 132, 0.5);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.6);
  color: #d7bb84;
  padding: 6px 10px;
  font-size: 0.76rem;
  font-weight: 700;
}

.rider-editor__hint-line {
  margin: 0;
  color: rgba(226, 232, 240, 0.55);
  font-size: 0.72rem;
  font-style: italic;
}

.rider-editor__inspector {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 8px;
  border-top: 1px dashed rgba(148, 163, 184, 0.25);
}

.rider-editor__inspector-head {
  margin: 0;
  font-size: 0.8rem;
  font-weight: 700;
  color: #d7bb84;
}

.rider-editor__hint {
  font-weight: 400;
  opacity: 0.65;
}

.rider-editor__field-section {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 6px 8px;
  border: 1px solid rgba(148, 163, 184, 0.14);
  border-radius: 8px;
  background: rgba(8, 14, 24, 0.4);
}

.rider-editor__field-section-title {
  margin: 0;
  font-size: 0.68rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: rgba(226, 232, 240, 0.6);
}
</style>
