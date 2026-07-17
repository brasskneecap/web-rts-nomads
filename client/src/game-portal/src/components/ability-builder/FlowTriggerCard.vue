<template>
  <div class="flow-trigger" :class="{ 'flow-trigger--selected': isSelected }" data-test="flow-trigger-card">
    <div class="flow-trigger__head">
      <button
        type="button"
        class="flow-trigger__collapse"
        :aria-expanded="expanded"
        :title="expanded ? 'Collapse' : 'Expand'"
        @click.stop="expanded = !expanded"
      >{{ expanded ? '▾' : '▸' }}</button>

      <button type="button" class="flow-trigger__title" @click="onSelect">
        <span class="flow-trigger__type">{{ typeLabel }}</span>
        <span v-if="trigger.name" class="flow-trigger__name">{{ trigger.name }}</span>
        <span v-if="timingSummary" class="flow-trigger__timing">{{ timingSummary }}</span>
      </button>

      <span
        v-if="badge"
        class="flow-trigger__badge"
        :class="badge.severity === 'error' ? 'flow-trigger__badge--error' : 'flow-trigger__badge--warning'"
        :title="badge.title"
      >{{ badge.count }}</span>

      <button
        type="button"
        class="flow-trigger__remove"
        title="Remove trigger"
        @click.stop="builder.removeTrigger(path)"
      >✕</button>
    </div>

    <div v-if="expanded" class="flow-trigger__body">
      <p v-if="trigger.actions.length === 0" class="flow-trigger__empty">No actions yet.</p>

      <template v-for="(a, j) in trigger.actions" :key="a.id">
        <FlowActionCard
          :action="a"
          :index="j"
          :count="trigger.actions.length"
          :path="actionPath(a)"
        />

        <!-- Nested triggers under THIS action: real, recursive
             FlowTriggerCard instances — fully editable at any depth, not the
             dead read-only label this used to be. nestedTriggersFor is the
             UNION of `children` and create_zone's `config.triggers` (never
             first-match — an action with BOTH populated shows both). Each
             nested card gets its own NodePath (this action's path + one more
             `trigger` segment) and one more depth level, which drives the
             indent falloff below so a meteor-style 3-level tree stays
             legible in a ~1/3-width flow column. -->
        <div
          v-for="nested in nestedTriggersFor(a)"
          :key="nested.id"
          class="flow-trigger__nested"
          :style="{ marginLeft: `${nestedMarginPx}px` }"
        >
          <FlowTriggerCard
            :trigger="nested"
            :index="0"
            :path="[...actionPath(a), { kind: 'trigger', id: nested.id }]"
            :depth="depth + 1"
          />
        </div>

        <!-- "+ Trigger" nested-add, scoped to THIS action. The slot an added
             trigger lands in (children vs create_zone's config.triggers) is
             decided by builder.addTrigger/programTree, not here — this
             control only picks the TYPE, and hides the picker entirely when
             there's only one type that makes sense (see
             nestedTriggerTypeOptions), so it never presents a fake choice. -->
        <div class="flow-trigger__nested-add" :style="{ marginLeft: `${nestedMarginPx}px` }">
          <select
            v-if="nestedTriggerTypeOptions(a).length > 1"
            v-model="nestedTypeChoice[a.id]"
            :aria-label="`New nested trigger type for ${a.id}`"
          >
            <option v-for="t in nestedTriggerTypeOptions(a)" :key="t" :value="t">{{ humanizeTriggerType(t) }}</option>
          </select>
          <UiButton
            size="sm"
            variant="secondary"
            data-test="flow-trigger-add-nested-trigger"
            :data-action-id="a.id"
            @click="addNestedTrigger(a)"
          >+ Trigger</UiButton>
        </div>
      </template>

      <!-- Opens AddActionDialog with THIS trigger's id passed explicitly as a
           prop — not read from builder.selected. addAction() (called from
           the dialog) auto-selects the new action, so the bottom inspector
           follows it once the dialog closes. -->
      <UiButton size="sm" variant="secondary" data-test="flow-trigger-add-action" @click="addActionOpen = true">
        + Action
      </UiButton>
    </div>
  </div>

  <AddActionDialog :open="addActionOpen" :trigger-path="path" @close="addActionOpen = false" />
</template>

<script setup lang="ts">
// One root trigger's card in the Flow view: header (type/name/timing,
// collapse, selection, validation badge) + its action cards — and,
// recursively, a full peer FlowTriggerCard for any trigger nested under one
// of its actions (create_zone's config.triggers, or any action's own
// `children`). This makes nested triggers real, editable cards at any depth
// (selection, remove, "+ Action", further nesting) instead of the flat
// read-only label this file used to render — see
// docs/superpowers/plans/2026-07-16-composable-abilities-phase7-nested-authoring.md.
//
// FlowTriggerCard and FlowActionCard import each other (FlowActionCard needs
// FlowTriggerCard to render a presentation's own triggers as real cards
// too). This is a standard, supported ESM circular-import pattern: neither
// module reads the other's export until a render function actually runs
// (well after both modules have finished evaluating), so the cycle never
// observes a not-yet-initialized binding.
import { computed, reactive, ref } from 'vue'
import type { AbilityActionDef, AbilityTriggerDef, TriggerType } from '@/game/abilities/program/abilityProgram'
import { issuesForPath } from '@/game/abilities/program/programValidation'
import { useAbilityBuilderContext } from './AbilityBuilderContext'
import { humanizeActionType } from './summarizeAction'
import { indexPathFor, nestedTriggersFor, pathsEqual, type NodePath } from './programTree'
import FlowActionCard from './FlowActionCard.vue'
import AddActionDialog from './AddActionDialog.vue'
import UiButton from '@/components/ui/UiButton.vue'

const props = withDefaults(
  defineProps<{
    trigger: AbilityTriggerDef
    index: number
    /** This trigger's own NodePath — identifies it for selection/mutation ops
        and is extended by one `{kind:'action', id}` segment to build each of
        its child action cards' own path (and, for a nested trigger below one
        of those, one more `{kind:'trigger', id}` segment on top of that). */
    path: NodePath
    /** Nesting depth of THIS card: 0 for a root trigger, incremented by 1
        each time a FlowTriggerCard renders one of its own nested children.
        Purely a visual signal driving indent falloff (see nestedMarginPx) —
        identity and every mutation op always go through `path`, never
        `depth`. */
    depth?: number
  }>(),
  { depth: 0 },
)

const builder = useAbilityBuilderContext()
const expanded = ref(true)

// Local UI state for the "+ Action" dialog — deliberately NOT plumbed
// through AbilityBuilderPanel; each FlowTriggerCard owns its own dialog
// instance and open flag.
const addActionOpen = ref(false)

// nestedTypeChoice tracks the currently-picked "+ Trigger" type for each of
// THIS trigger's actions, keyed by action id (a trigger can have several
// actions, each with its own independent nested-add affordance). An action
// with no entry yet falls back to nestedTriggerTypeOptions(a)[0] — which is
// also what an unvisited <select> would already show natively.
const nestedTypeChoice = reactive<Record<string, TriggerType>>({})

// nestedMarginPx is the indent applied to both a nested trigger card's
// wrapper and its sibling "+ Trigger" control. The flow column is roughly
// 1/3 of the editor's width, so indentation can't keep growing linearly with
// depth — meteor's own 3 levels (impact -> zone -> burn) would eat most of
// the remaining width by the bottom. Each successive level's OWN indent
// shrinks toward a floor while the left-rule border (see .flow-trigger__nested
// CSS) still renders at every level, so the hierarchy stays legible by "rail
// count" rather than by raw accumulated pixels.
const nestedMarginPx = computed(() => Math.max(8, 18 - props.depth * 6))

// humanizeTriggerType reuses the same snake_case -> Title Case rule as
// action types (the humanization is generic, not action-specific).
const humanizeTriggerType = humanizeActionType

const typeLabel = computed(() => humanizeTriggerType(props.trigger.type))

const timingSummary = computed(() => {
  const t = props.trigger.timing
  if (!t) return ''
  const parts: string[] = []
  if (t.marker) parts.push(`marker: ${t.marker}`)
  if (t.frame != null) parts.push(`frame ${t.frame}`)
  if (t.tickInterval != null) parts.push(`every ${t.tickInterval}ms`)
  if (t.delaySeconds != null) parts.push(`+${t.delaySeconds}s`)
  return parts.join(' · ')
})

const isSelected = computed(() => {
  const sel = builder.selected.value
  return sel.kind === 'trigger' && pathsEqual(sel.path, props.path)
})

// badge derives this trigger's validator-grammar index path (`triggers[i]` /
// `...config.triggers[k]` / etc — see indexPathFor) from its NodePath on
// every read, rather than threading a separately hand-maintained string down
// from AbilityFlow — ids are stable identity, indices aren't, so this stays
// correct across add/remove without any parent recomputing anything for it.
// This holds identically at ANY nesting depth: indexPathFor walks the whole
// path, so a depth-3 crater-DoT trigger's badge is exactly as correct as a
// root trigger's — proving indexPathFor and the Go validator's path grammar
// agree end-to-end, not just at the root.
const badge = computed(() => {
  const indexPath = indexPathFor(builder.program.value, props.path)
  if (!indexPath) return null
  const issues = issuesForPath(builder.issues.value, indexPath)
  if (issues.length === 0) return null
  const severity = issues.some((i) => i.severity === 'error') ? 'error' : 'warning'
  return { count: issues.length, severity, title: issues.map((i) => i.message).join('\n') }
})

function onSelect() {
  builder.select({ kind: 'trigger', path: props.path })
}

// actionPath extends THIS trigger's own path by one `{kind:'action', id}`
// segment — the NodePath every one of `a`'s cards (its FlowActionCard, its
// nested FlowTriggerCards, and the nested-add control) is built from.
function actionPath(action: AbilityActionDef): NodePath {
  return [...props.path, { kind: 'action', id: action.id }]
}

// nestedTriggerTypeOptions surfaces only the trigger types that make sense
// to nest under a given action's own follow-up slot: create_zone's
// config.triggers fires on zone lifecycle events (on_zone_tick / _enter /
// _exit); every OTHER action's `children` slot only ever fires via
// on_action_complete (see ability_program.go's Children doc comment +
// TriggerOnActionComplete) — offering a type picker there would just be one
// option pretending to be a choice, so the template hides the <select>
// entirely when this returns a single-element array.
function nestedTriggerTypeOptions(action: AbilityActionDef): TriggerType[] {
  if (action.type === 'create_zone') return ['on_zone_tick', 'on_zone_enter', 'on_zone_exit']
  return ['on_action_complete']
}

function addNestedTrigger(action: AbilityActionDef) {
  const type = nestedTypeChoice[action.id] ?? nestedTriggerTypeOptions(action)[0]
  // builder.addTrigger's parent-path overload routes to the right SLOT
  // itself (create_zone -> config.triggers, everything else -> children) —
  // this call site only ever picks the TYPE.
  builder.addTrigger(actionPath(action), type)
}
</script>

<style scoped>
.flow-trigger {
  display: flex;
  flex-direction: column;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
  background: rgba(15, 23, 42, 0.3);
}

.flow-trigger--selected {
  border-color: var(--ed-brass);
  box-shadow: 0 0 0 1px rgba(212, 168, 71, 0.35);
}

.flow-trigger__head {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 8px;
  border-bottom: 1px solid var(--ed-line);
}

.flow-trigger__collapse {
  flex: 0 0 auto;
  padding: 2px 5px;
  font-size: 0.7rem;
  color: var(--ed-text-dim);
  background: none;
  border: 1px solid transparent;
  border-radius: 4px;
}

.flow-trigger__collapse:hover {
  color: var(--ed-brass);
  border-color: var(--ed-line);
}

.flow-trigger__title {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 2px 4px;
  background: none;
  border: 0;
  text-align: left;
}

.flow-trigger__type {
  font-family: var(--font-title);
  font-size: 0.8rem;
  font-weight: 700;
  letter-spacing: 0.04em;
  color: var(--ed-brass);
  white-space: nowrap;
}

.flow-trigger__name {
  font-family: var(--font-body);
  font-size: 0.78rem;
  color: var(--ed-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.flow-trigger__timing {
  font-size: 0.7rem;
  color: var(--ed-text-dim);
  white-space: nowrap;
}

.flow-trigger__badge {
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

.flow-trigger__badge--error {
  background: var(--ed-danger);
}

.flow-trigger__badge--warning {
  background: #e0b258;
}

.flow-trigger__remove {
  flex: 0 0 auto;
  padding: 2px 6px;
  font-size: 0.72rem;
  color: var(--ed-text-dim);
  background: none;
  border: 1px solid transparent;
  border-radius: 4px;
}

.flow-trigger__remove:hover {
  color: var(--ed-danger);
  border-color: rgba(240, 132, 108, 0.4);
}

.flow-trigger__body {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 8px;
}

.flow-trigger__empty {
  margin: 0;
  font-size: 0.78rem;
  color: var(--ed-text-dim);
  font-style: italic;
}

/* Wraps a recursively-rendered nested FlowTriggerCard. marginLeft is set
   inline (see nestedMarginPx) so it can shrink with depth; only the rule
   itself (rendered at every level, however far the indent has collapsed)
   is a plain CSS concern. */
.flow-trigger__nested {
  padding-left: 8px;
  border-left: 2px solid var(--ed-line);
}

/* The nested "+ Trigger" affordance sits at the same indent as the nested
   cards it adds alongside (see nestedMarginPx), so it visually reads as
   belonging to the same action rather than the trigger as a whole. */
.flow-trigger__nested-add {
  display: flex;
  align-items: center;
  gap: 6px;
}

.flow-trigger__nested-add select {
  flex: 0 1 auto;
  min-width: 0;
}
</style>
