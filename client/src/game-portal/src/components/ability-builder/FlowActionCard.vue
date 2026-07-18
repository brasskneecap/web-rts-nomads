<template>
  <div
    class="flow-action"
    :class="{ 'flow-action--selected': isSelected, 'flow-action--disabled': action.disabled }"
    data-test="flow-action-card"
  >
    <button type="button" class="flow-action__body" @click="onSelect">
      <span class="flow-action__summary">{{ summary }}</span>
      <span
        v-if="displayOnly"
        class="flow-action__chip"
        title="This action isn't executed by the runtime yet."
      >display-only</span>
      <span v-if="action.disabled" class="flow-action__disabled-marker">disabled</span>
      <span
        v-if="badge"
        class="flow-action__badge"
        :class="badge.severity === 'error' ? 'flow-action__badge--error' : 'flow-action__badge--warning'"
        :title="badge.title"
      >{{ badge.count }}</span>
    </button>

    <div class="flow-action__controls">
      <button
        type="button"
        title="Move up"
        :disabled="index === 0"
        @click.stop="builder.moveAction(path, 'up')"
      >▲</button>
      <button
        type="button"
        title="Move down"
        :disabled="index === count - 1"
        @click.stop="builder.moveAction(path, 'down')"
      >▼</button>
      <button
        type="button"
        title="Duplicate"
        @click.stop="builder.duplicateAction(path)"
      >⧉</button>
      <button
        type="button"
        :title="action.disabled ? 'Enable' : 'Disable'"
        @click.stop="builder.toggleActionDisabled(path)"
      >{{ action.disabled ? '⏵' : '⏸' }}</button>
      <button
        type="button"
        title="Delete"
        @click.stop="builder.removeAction(path)"
      >✕</button>
    </div>
  </div>

  <!-- Resolved presentation, shown subordinate to (indented/left-ruled
       under) the play_presentation action card above. `config` is an opaque
       bag (see AbilityActionDef.config's doc comment), so `presentationId`
       is read defensively, never destructured — and a missing/unresolved
       id renders nothing rather than an empty shell.

       The presentation NODE's own header (id + asset) stays read-only by
       design (per the phase-7 plan): asset/scale are already edited via the
       owning play_presentation action's config above, so a second editor
       for the same value would be redundant. Its TRIGGERS, however, are
       real recursive FlowTriggerCards — fully editable at any depth,
       rooted at a `{kind:'presentation', id}` path segment (see
       programTree's resolver) — that's the whole point of meteor's impact
       trigger being reachable at all. -->
  <!-- A `loop` action is a WRAPPER: its iteration count + variables (editable
       inline) sit at the top, and its body — the actions it runs each iteration
       — are REAL, recursive FlowActionCards nested beneath it (selectable,
       reorderable, editable in the inspector, add/remove) exactly like a
       trigger's actions, just addressed through the loop's config.body. -->
  <div v-if="loop" class="flow-action__loop" data-test="flow-action-loop">
    <div class="flow-action__loop-head">
      <label class="flow-action__loop-iter">
        <span>Loop ×</span>
        <input
          type="number"
          min="1"
          step="1"
          data-test="loop-iterations"
          :value="loop.iterations"
          @change="onIterations"
        />
      </label>
      <label class="flow-action__loop-stepfirst" title="Apply each variable's step to the first iteration too">
        <input type="checkbox" data-test="loop-step-first" :checked="loop.stepFirst" @change="onStepFirst" />
        step first
      </label>
      <button
        type="button"
        class="flow-action__loop-addvar"
        data-test="loop-add-var"
        :disabled="loop.vars.length >= 26"
        @click="onAddVar"
      >+ variable</button>
    </div>

    <div v-for="v in loop.vars" :key="v.name" class="flow-action__loop-var" :data-test="`loop-var-${v.name}`">
      <span class="flow-action__loop-var-name">{{ v.name }}</span>
      <label>start <input type="number" step="1" :data-test="`loop-var-${v.name}-start`" :value="v.start" @change="(e) => onVarField(v.name, 'start', e)" /></label>
      <label>step <input type="number" step="1" :data-test="`loop-var-${v.name}-step`" :value="v.step" @change="(e) => onVarField(v.name, 'step', e)" /></label>
      <select
        class="flow-action__loop-var-mode"
        :data-test="`loop-var-${v.name}-stepmode`"
        :aria-label="`${v.name} step mode`"
        :value="v.stepMode === 'percent' ? 'percent' : 'number'"
        @change="(e) => onVarStepMode(v.name, e)"
      >
        <option value="number">flat</option>
        <option value="percent">%</option>
      </select>
      <button type="button" :data-test="`loop-var-${v.name}-remove`" :aria-label="`Remove variable ${v.name}`" @click="onRemoveVar(v.name)">×</button>
    </div>

    <div class="flow-action__loop-body">
      <p v-if="!loop.body.length" class="flow-action__loop-empty">No steps yet.</p>
      <FlowActionCard
        v-for="(b, i) in loop.body"
        :key="b.id"
        :action="b"
        :index="i"
        :count="loop.body.length"
        :path="[...path, { kind: 'action', id: b.id }]"
      />
      <UiButton size="sm" variant="secondary" data-test="loop-add-action" @click="loopAddOpen = true">+ Action</UiButton>
    </div>

    <AddActionDialog :open="loopAddOpen" :trigger-path="path" @close="loopAddOpen = false" />
  </div>

  <div v-if="presentation" class="flow-action__presentation" data-test="flow-action-presentation">
    <div class="flow-action__presentation-head">
      <span class="flow-action__presentation-title">Presentation: {{ presentation.id }}</span>
      <span class="flow-action__presentation-asset">{{ presentation.asset }}</span>
    </div>
    <FlowTriggerCard
      v-for="pt in presentation.triggers ?? []"
      :key="pt.id"
      :trigger="pt"
      :index="0"
      :path="[{ kind: 'presentation', id: presentation.id }, { kind: 'trigger', id: pt.id }]"
      :depth="1"
    />
  </div>
</template>

<script setup lang="ts">
// A single action's card in the Flow view: a one-line summary + selection +
// per-card controls (move/duplicate/disable/delete), a "display-only" chip
// for actions the runtime doesn't execute yet, a validation badge, and (for
// play_presentation actions) the resolved presentation rendered inline
// beneath it — see the template comment above the presentation block.
//
// FlowActionCard imports FlowTriggerCard (to render a presentation's own
// triggers as real recursive cards) while FlowTriggerCard also imports
// FlowActionCard (for its own actions) — a supported ESM circular-import
// pattern; see the module comment at the top of FlowTriggerCard.vue for why
// this is safe.
import { computed, ref } from 'vue'
import type { AbilityActionDef } from '@/game/abilities/program/abilityProgram'
import { schemaForAction } from '@/game/abilities/program/programSchema'
import { issuesForPath } from '@/game/abilities/program/programValidation'
import { useAbilityBuilderContext } from './AbilityBuilderContext'
import { summarizeAction } from './summarizeAction'
import { indexPathFor, pathsEqual, type NodePath } from './programTree'
import { readLoop, withVarAdded, withVarField, withVarRemoved, withVarStepMode } from './loopEditor'
import FlowTriggerCard from './FlowTriggerCard.vue'
import AddActionDialog from './AddActionDialog.vue'
import UiButton from '@/components/ui/UiButton.vue'

const props = defineProps<{
  action: AbilityActionDef
  /** This action's index among its trigger's siblings — drives move bounds. */
  index: number
  /** Sibling count — move-down is disabled at index === count - 1. */
  count: number
  /** This action's own NodePath — identifies it for selection/mutation ops,
      at any depth (a root action or one nested under create_zone's
      config.triggers / an action's children). */
  path: NodePath
}>()

const builder = useAbilityBuilderContext()

const summary = computed(() => summarizeAction(props.action, builder.schema.value))

const isSelected = computed(() => {
  const sel = builder.selected.value
  return sel.kind === 'action' && pathsEqual(sel.path, props.path)
})

const displayOnly = computed(() => {
  const schema = builder.schema.value
  if (!schema) return false
  return schemaForAction(schema, props.action.type)?.runnable === false
})

// badge derives this action's validator-grammar index path from its
// NodePath on every read — see FlowTriggerCard's identical rationale.
const badge = computed(() => {
  const indexPath = indexPathFor(builder.program.value, props.path)
  if (!indexPath) return null
  const issues = issuesForPath(builder.issues.value, indexPath)
  if (issues.length === 0) return null
  const severity = issues.some((i) => i.severity === 'error') ? 'error' : 'warning'
  return { count: issues.length, severity, title: issues.map((i) => i.message).join('\n') }
})

function onSelect() {
  builder.select({ kind: 'action', path: props.path })
}

// presentation resolves this action's `config.presentationId` against the
// program's root `presentations` list — the same lookup the server does at
// ability_exec_presentation.go:112-126. Only meaningful for
// play_presentation actions; anything else (or an id that doesn't resolve)
// yields null so the template renders nothing extra.
const presentation = computed(() => {
  if (props.action.type !== 'play_presentation') return null
  const id = props.action.config?.presentationId
  if (typeof id !== 'string' || id.length === 0) return null
  const list = builder.program.value.presentations ?? []
  return list.find((p) => p.id === id) ?? null
})

// loop reads this action's config when it's a `loop` wrapper, so the template
// can render its iterations/variables (editable) and body actions.
const loop = computed(() => readLoop(props.action))

// Local open state for THIS loop's "+ Action" dialog (each loop owns its own).
const loopAddOpen = ref(false)

// intFromEvent reads an integer from a number input, or null for a blank/NaN
// entry (so a mid-edit empty field is ignored). `min` clamps when non-null.
function intFromEvent(e: Event, min: number | null): number | null {
  const raw = (e.target as HTMLInputElement).value
  if (raw.trim() === '') return null
  const n = Math.round(Number(raw))
  if (!Number.isFinite(n)) return null
  return min == null ? n : Math.max(min, n)
}

function onIterations(e: Event) {
  const n = intFromEvent(e, 1)
  if (n != null) builder.updateActionConfig(props.path, { iterations: n })
}

function onAddVar() {
  if (loop.value) builder.updateActionConfig(props.path, { vars: withVarAdded(loop.value.vars) })
}

function onRemoveVar(name: string) {
  if (loop.value) builder.updateActionConfig(props.path, { vars: withVarRemoved(loop.value.vars, name) })
}

function onVarField(name: string, field: 'start' | 'step', e: Event) {
  const n = intFromEvent(e, null) // start/step may be negative (e.g. falloff)
  if (loop.value && n != null) {
    builder.updateActionConfig(props.path, { vars: withVarField(loop.value.vars, name, field, n) })
  }
}

function onVarStepMode(name: string, e: Event) {
  const mode = (e.target as HTMLSelectElement).value === 'percent' ? 'percent' : 'number'
  if (loop.value) builder.updateActionConfig(props.path, { vars: withVarStepMode(loop.value.vars, name, mode) })
}

function onStepFirst(e: Event) {
  builder.updateActionConfig(props.path, { stepFirst: (e.target as HTMLInputElement).checked })
}
</script>

<style scoped>
.flow-action {
  display: flex;
  align-items: stretch;
  gap: 6px;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
  background: rgba(15, 23, 42, 0.2);
}

.flow-action--selected {
  border-color: var(--ed-brass);
  box-shadow: 0 0 0 1px rgba(212, 168, 71, 0.35);
  background: rgba(212, 168, 71, 0.08);
}

.flow-action--disabled {
  opacity: 0.55;
}

.flow-action__body {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  background: none;
  border: 0;
  text-align: left;
}

.flow-action__summary {
  flex: 1 1 auto;
  min-width: 0;
  font-family: var(--font-body);
  font-size: 0.8rem;
  color: var(--ed-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.flow-action--disabled .flow-action__summary {
  text-decoration: line-through;
  color: var(--ed-text-dim);
}

.flow-action__chip {
  flex: 0 0 auto;
  border-radius: 999px;
  padding: 1px 8px;
  font-size: 0.64rem;
  font-weight: 700;
  letter-spacing: 0.02em;
  white-space: nowrap;
  color: #e0b258;
  background: rgba(224, 178, 88, 0.14);
  border: 1px solid rgba(224, 178, 88, 0.4);
}

.flow-action__disabled-marker {
  flex: 0 0 auto;
  font-size: 0.64rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--ed-text-dim);
}

.flow-action__badge {
  flex: 0 0 auto;
  min-width: 16px;
  height: 16px;
  padding: 0 5px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 999px;
  font-size: 0.66rem;
  font-weight: 700;
  color: #17120c;
}

.flow-action__badge--error {
  background: var(--ed-danger);
}

.flow-action__badge--warning {
  background: #e0b258;
}

.flow-action__controls {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: 2px;
  padding: 4px 6px;
  border-left: 1px solid var(--ed-line);
}

.flow-action__controls button {
  padding: 2px 5px;
  font-size: 0.7rem;
  line-height: 1;
  color: var(--ed-text-dim);
  background: none;
  border: 1px solid transparent;
  border-radius: 4px;
}

.flow-action__controls button:hover:not(:disabled) {
  color: var(--ed-brass);
  border-color: var(--ed-line);
}

.flow-action__controls button:disabled {
  opacity: 0.35;
}

/* The loop wrapper: its header (iterations + variables) and the body steps it
   contains, indented + left-ruled beneath the loop action card so the whole
   loop reads as one block. */
.flow-action__loop {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-left: 18px;
  padding: 8px 10px;
  border-left: 2px solid var(--ed-brass-dim);
}

.flow-action__loop-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.flow-action__loop-iter {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 0.74rem;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--ed-brass);
}

.flow-action__loop-iter input {
  width: 4em;
}

.flow-action__loop-addvar {
  padding: 2px 8px;
  font-size: 0.68rem;
  color: var(--ed-text-dim);
  background: none;
  border: 1px solid var(--ed-line);
  border-radius: 4px;
}

.flow-action__loop-stepfirst {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 0.68rem;
  color: var(--ed-text-dim);
  white-space: nowrap;
}

.flow-action__loop-var {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 0.72rem;
  color: var(--ed-text-dim);
}

.flow-action__loop-var label {
  display: flex;
  align-items: center;
  gap: 4px;
}

.flow-action__loop-var input {
  width: 4em;
}

.flow-action__loop-var-mode {
  flex: 0 0 auto;
  width: auto;
}

.flow-action__loop-var-name {
  width: 1.4em;
  height: 1.4em;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
  background: var(--ed-field);
  color: var(--ed-brass);
  font-family: var(--font-mono, monospace);
  font-weight: 700;
}

.flow-action__loop-var button {
  margin-left: auto;
  width: 1.5em;
  height: 1.5em;
  color: var(--ed-text-dim);
  background: none;
  border: 1px solid var(--ed-line);
  border-radius: 4px;
}

.flow-action__loop-body {
  margin-top: 2px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.flow-action__loop-empty {
  margin: 0;
  font-size: 0.78rem;
  font-style: italic;
  color: var(--ed-text-dim);
}

/* Subordinate to the action card above — indented + left-ruled, matching
   FlowTriggerCard's .flow-trigger__nested treatment, so it reads as "part
   of this action" rather than a peer card. */
.flow-action__presentation {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-left: 18px;
  padding: 6px 10px;
  border-left: 2px solid var(--ed-line);
}

.flow-action__presentation-head {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.flow-action__presentation-title {
  font-size: 0.76rem;
  font-weight: 700;
  color: var(--ed-text-dim);
}

.flow-action__presentation-asset {
  font-size: 0.72rem;
  color: var(--ed-text-dim);
}
</style>
