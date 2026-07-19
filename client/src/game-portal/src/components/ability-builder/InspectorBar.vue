<template>
  <div class="ib-bar" data-test="inspector-bar">
    <span class="ib-bar__label">Inspector</span>

    <div class="ib-bar__scroll">
      <!-- Empty state: nothing trigger/action-shaped is selected (either the
           ability node, or — belt-and-braces — no NodeRef at all). Identity/
           Entry/Cast Setup live in the Identity tab now, so this bar has
           nothing of its own to show; a blank strip would read as broken. -->
      <p v-if="isEmpty" class="ib-hint" data-test="inspector-bar-empty">
        Select a trigger or action in the flow to edit its fields here.
      </p>

      <template v-else>
        <!-- Validation issues for whatever is selected, pinned to the front
             of the row so they're visible without scrolling past the fields
             first. -->
        <div v-if="selectedIssues.length" class="ib-issues" data-test="inspector-bar-issues">
          <p
            v-for="(iss, idx) in selectedIssues"
            :key="idx"
            class="ib-issue"
            :class="iss.severity === 'error' ? 'ib-issue--error' : 'ib-issue--warning'"
          >{{ iss.message }}</p>
        </div>

        <!-- ── Trigger ──────────────────────────────────────────────────── -->
        <template v-if="selected.kind === 'trigger'">
          <SectionCard v-if="selectedTrigger" title="Trigger" class="ib-card">
            <SchemaField
              :field="{ key: 'type', label: 'Trigger Type', control: 'enum', options: triggerTypeOptions }"
              :model-value="selectedTrigger.type"
              :enums="enumsValue"
              :catalogs="builder.catalogs.value"
              @update:model-value="(v) => commitTriggerField({ type: v as AbilityTriggerDef['type'] })"
            />
            <SchemaField
              :field="{ key: 'name', label: 'Name', control: 'text' }"
              :model-value="selectedTrigger.name ?? ''"
              :enums="enumsValue"
              :catalogs="builder.catalogs.value"
              @update:model-value="(v) => commitTriggerField({ name: v as string })"
            />
            <!-- Timing shape follows the trigger type: on_animation_marker
                 wants a marker name, on_zone_tick/on_status_tick want a tick
                 interval, everything else falls back to the generic `frame`
                 field. -->
            <SchemaField
              v-if="timingKind === 'marker'"
              :field="{ key: 'marker', label: 'Marker', control: 'text' }"
              :model-value="selectedTrigger.timing?.marker ?? ''"
              :enums="enumsValue"
              :catalogs="builder.catalogs.value"
              @update:model-value="(v) => commitTriggerTiming({ marker: v as string })"
            />
            <SchemaField
              v-else-if="timingKind === 'tickInterval'"
              :field="{ key: 'tickInterval', label: 'Tick Interval (ms)', control: 'number' }"
              :model-value="selectedTrigger.timing?.tickInterval ?? 0"
              :enums="enumsValue"
              :catalogs="builder.catalogs.value"
              @update:model-value="(v) => commitTriggerTiming({ tickInterval: v as number })"
            />
            <SchemaField
              v-else
              :field="{ key: 'frame', label: 'Frame', control: 'number' }"
              :model-value="selectedTrigger.timing?.frame ?? 0"
              :enums="enumsValue"
              :catalogs="builder.catalogs.value"
              @update:model-value="(v) => commitTriggerTiming({ frame: v as number })"
            />
          </SectionCard>
          <p v-else class="ib-hint">This trigger no longer exists — select another node.</p>
        </template>

        <!-- ── Action (schema-driven) ──────────────────────────────────── -->
        <template v-else-if="selected.kind === 'action'">
          <template v-if="selectedAction">
            <SectionCard title="Action" class="ib-card">
              <p class="ib-note">{{ selectedAction.type }} <span class="ib-dim">(id: {{ selectedAction.id }})</span></p>
              <p v-if="actionSchema && !actionSchema.runnable" class="ib-display-only">
                This action isn't executed by the runtime yet — display-only.
              </p>
              <p v-if="fieldSections.length === 0" class="ib-hint">No configurable fields for this action type.</p>
              <p v-if="unreadSavedNames.length" class="ib-warning" data-test="ib-unread-save">
                Nothing reads {{ unreadSavedNames.length > 1 ? 'these saved names' : 'the saved name' }}
                <strong>{{ unreadSavedNames.map((n) => `"${n}"`).join(', ') }}</strong> back — the save has
                no effect. Reference it via Saved Value or Exclude Saved Set on a later query, or remove it.
              </p>
            </SectionCard>

            <SectionCard v-for="[section, fields] in fieldSections" :key="section" :title="section" class="ib-card">
              <template v-for="f in fields" :key="f.key">
                <!-- A `target_query`-control field (e.g. select_targets'
                     `target`) binds to action.target, NOT
                     action.config[f.key] — the server registry documents
                     that exact case (config is empty; the TargetQueryDef
                     lives on the action itself). -->
                <SchemaField
                  v-if="f.control === 'target_query'"
                  :field="f"
                  :model-value="selectedAction.target"
                  :enums="enumsValue"
                  :catalogs="builder.catalogs.value"
                  :saved-names="savedNames"
                  @update:model-value="commitActionTarget"
                />
                <SchemaField
                  v-else
                  :field="f"
                  :model-value="selectedAction.config?.[f.key]"
                  :enums="enumsValue"
                  :catalogs="builder.catalogs.value"
                  :loop-vars="loopScope.vars"
                  :variable-capable="loopScope.inLoop"
                  @update:model-value="(v) => commitActionConfig(f.key, v)"
                />
              </template>
            </SectionCard>

            <!-- Save result: a producing action (select/filter) can name its
                 own resulting set inline via `outputs.targets`, so a later
                 query can read it back — the same effect as a separate Save
                 Targets action, without the extra step. Reuses SchemaField's
                 text control (commit-on-change), routed to outputs not config. -->
            <SectionCard v-if="canSaveResult" title="Output" class="ib-card">
              <SchemaField
                :field="{ key: 'saveResultAs', label: 'Save result as', control: 'text' }"
                :model-value="outputTargetsName"
                :enums="enumsValue"
                :catalogs="builder.catalogs.value"
                @update:model-value="(v) => commitActionOutput(v as string)"
              />
              <p class="ib-hint">
                Names this action's targets so a later query can start from them (Saved Value) or skip
                them (Exclude Saved Set).
              </p>
            </SectionCard>
          </template>
          <p v-else class="ib-hint">This action no longer exists — select another node.</p>
        </template>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
// InspectorBar: the bottom strip that replaces the trigger/action sections
// of the old rail ItemInspector (see docs/superpowers/plans/
// 2026-07-16-ability-builder-ui-corrections.md Task 3). Identity/Entry/Cast
// Setup moved to IdentityTab.vue instead — this ONLY handles whatever trigger
// or action is selected in the flow. It lives in its own column between the
// flow and the preview, with its field-group cards (`.ib-card`) stacked
// vertically and the stack scrolling once it outgrows the column height.
//
// The action section stays fully schema-driven via schemaForAction — adding
// a new action config field server-side needs no client change here.
import { computed } from 'vue'
import type { AbilityActionDef, AbilityTriggerDef, TargetQueryDef, TriggerType } from '@/game/abilities/program/abilityProgram'
import { fieldVisible, schemaForAction, type SchemaField as SchemaFieldDescriptor } from '@/game/abilities/program/programSchema'
import { issuesForPath, type ValidationIssue } from '@/game/abilities/program/programValidation'
import SectionCard from '@/components/editor/SectionCard.vue'
import { useAbilityBuilderContext } from './AbilityBuilderContext'
import {
  collectReadContextNames,
  collectSavedContextNames,
  indexPathFor,
  loopScopeFor,
  namesSavedByAction,
  resolveNode,
} from './programTree'
import SchemaField from './SchemaField.vue'

const builder = useAbilityBuilderContext()

const selected = computed(() => builder.selected.value)
const enumsValue = computed(() => builder.schema.value?.enums ?? {})

// Nothing trigger/action-shaped selected -> empty state (ability node is
// covered by the Identity tab, not this bar).
const isEmpty = computed(() => selected.value.kind === 'ability')

// ── Selected node lookups ──────────────────────────────────────────────────
// Both resolve the CURRENT selection's NodePath against the live program via
// resolveNode (any depth — a root trigger/action or one nested arbitrarily
// deep), rather than a flat triggerId/actionId lookup.
const selectedTrigger = computed<AbilityTriggerDef | undefined>(() => {
  const sel = selected.value
  if (sel.kind !== 'trigger') return undefined
  const resolved = resolveNode(builder.program.value, sel.path)
  return resolved?.kind === 'trigger' ? resolved.node : undefined
})

const selectedAction = computed<AbilityActionDef | undefined>(() => {
  const sel = selected.value
  if (sel.kind !== 'action') return undefined
  const resolved = resolveNode(builder.program.value, sel.path)
  return resolved?.kind === 'action' ? resolved.node : undefined
})

// ── Validation path + issues for the current selection ─────────────────────
// indexPathFor derives the validator-grammar path (`triggers[i]` /
// `triggers[i].actions[j]` / `...config.triggers[k].actions[m]` / etc, at
// any depth) from the selection's NodePath against the LIVE program (ids are
// stable; indices aren't, so this is recomputed every time, never cached) —
// this is the whole reason indexPathFor exists (see programTree.ts), rather
// than hand-deriving `triggers[i].actions[j]` here. Ability-level issues
// don't go through here at all — IdentityTab filters those out of
// `builder.issues` independently, so there's no shared "selectedPath" logic
// to factor out between the two components (this bar never addresses the
// ability node).
const selectedPath = computed<string>(() => {
  const sel = selected.value
  if (sel.kind === 'ability') return ''
  return indexPathFor(builder.program.value, sel.path) ?? ''
})

const selectedIssues = computed<ValidationIssue[]>(() => {
  if (!selectedPath.value) return []
  return issuesForPath(builder.issues.value, selectedPath.value)
})

// ── Trigger: type options + timing-field-per-type ───────────────────────────
// Fallback for when the server's schema hasn't loaded yet. These are all
// triggers that ACTUALLY FIRE at runtime — the server's TriggerType enum still
// carries aspirational values with no producer (on_projectile_impact,
// on_damage_dealt, ...), so a curated fallback should not widen the offer
// beyond what works. `on_target_hit` was removed from the enum entirely: it
// had no definition that distinguished it from on_damage_dealt.
const CURATED_TRIGGER_TYPES: TriggerType[] = [
  'on_cast_start',
  'on_cast_complete',
  'on_animation_marker',
  'on_zone_tick',
  'on_zone_enter',
  'on_zone_exit',
]
const triggerTypeOptions = computed<string[]>(() => {
  const fromSchema = builder.schema.value?.enums.triggerTypes
  return fromSchema && fromSchema.length > 0 ? fromSchema : CURATED_TRIGGER_TYPES
})

type TimingKind = 'marker' | 'tickInterval' | 'frame'
const timingKind = computed<TimingKind>(() => {
  const t = selectedTrigger.value?.type
  if (t === 'on_animation_marker') return 'marker'
  if (t === 'on_zone_tick' || t === 'on_status_tick') return 'tickInterval'
  return 'frame'
})

function commitTriggerField(patch: Partial<AbilityTriggerDef>) {
  if (!selectedTrigger.value || selected.value.kind !== 'trigger') return
  builder.updateTrigger(selected.value.path, patch)
}

function commitTriggerTiming(patch: Partial<NonNullable<AbilityTriggerDef['timing']>>) {
  if (!selectedTrigger.value || selected.value.kind !== 'trigger') return
  builder.updateTrigger(selected.value.path, { timing: { ...selectedTrigger.value.timing, ...patch } })
}

// ── Action: schema-driven fields + targeting ────────────────────────────────
const actionSchema = computed(() => {
  const action = selectedAction.value
  if (!action || !builder.schema.value) return undefined
  return schemaForAction(builder.schema.value, action.type)
})

// loopScope: whether the selected action sits inside a loop, and which loop
// variables are in scope for its number fields (see loopScopeFor). Drives the
// literal-or-variable selector SchemaField shows for `number` controls.
const loopScope = computed<{ inLoop: boolean; vars: string[] }>(() => {
  const sel = selected.value
  if (sel.kind !== 'action') return { inLoop: false, vars: [] }
  return loopScopeFor(builder.program.value, sel.path)
})

// savedNames: every named-context key this ability saves to (outputs +
// store_targets), offered to the target-query "Saved Value" picker so an
// author references real saved selections instead of a fixed guess-list. The
// whole program is scanned (not just earlier actions) — scope-to-position
// precision is a later refinement (F2).
const savedNames = computed<string[]>(() => collectSavedContextNames(builder.program.value))

// Dead-save warning (G): names the SELECTED action saves that NOTHING reads
// back by name — a saved output that has no effect (the review's Frost Bolt
// "hit" case). Advisory only; never blocks. Reads are scanned program-wide, so
// a name consumed by any sibling/nested query counts.
const unreadSavedNames = computed<string[]>(() => {
  const action = selectedAction.value
  if (!action) return []
  const read = new Set(collectReadContextNames(builder.program.value))
  return namesSavedByAction(action).filter((name) => !read.has(name))
})

// Fields grouped by their declared `section` (falling back to "Properties"),
// in first-seen order — a Map preserves insertion order, so this needs no
// separate sort step. Each group renders as its own `.ib-card` in the stack.
//
// Fields gated by `showWhen` are filtered out BEFORE grouping, evaluated
// against the selected action's OWN config (fieldVisible/programSchema.ts —
// a pure mirror of the Go registry's FieldConditionMatches). A section that
// ends up with no visible fields at all (e.g. launch_projectile's
// "Targeting" once travelMode is "direction" hides `target`... though
// distance's own Properties-section placement means that specific example
// doesn't empty a whole section, this still generalizes correctly) simply
// never gets a Map entry, so it renders no card — no separate "is this
// section empty" check needed.
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
  builder.updateActionConfig(selected.value.path, { [key]: value })
}

function commitActionTarget(value: unknown) {
  if (!selectedAction.value || selected.value.kind !== 'action') return
  builder.updateAction(selected.value.path, { target: value as TargetQueryDef })
}

// ── Save result (outputs.targets) ────────────────────────────────────────────
// Action types whose Execute returns a meaningful target SET worth naming
// inline — the selection producers. Other actions' returns aren't a
// "selection" you'd read back, so they don't get the field (it would just be
// noise). store_targets is excluded: it already has its own "Save As".
const SAVE_RESULT_ACTION_TYPES = new Set(['select_targets', 'filter_targets'])

const canSaveResult = computed(
  () => !!selectedAction.value && SAVE_RESULT_ACTION_TYPES.has(selectedAction.value.type),
)

// The conventional single output slot is `targets` (mirrors compiled abilities
// and bindActionOutputsLocked). The field edits that slot; any other keys the
// action carries are preserved.
const outputTargetsName = computed(() => selectedAction.value?.outputs?.targets ?? '')

function commitActionOutput(name: string) {
  if (!selectedAction.value || selected.value.kind !== 'action') return
  const next = { ...(selectedAction.value.outputs ?? {}) }
  const trimmed = name.trim()
  if (trimmed) next.targets = trimmed
  else delete next.targets
  builder.updateAction(selected.value.path, {
    outputs: Object.keys(next).length ? next : undefined,
  })
}
</script>

<style scoped>
/* The strip is the shortest region that can still do its job: the flow above
/* Its own column between the flow and the preview, so it fills the available
   height rather than capping itself — the column's width is what bounds it
   now, not a max-height. */
.ib-bar {
  display: flex;
  flex-direction: column;
  gap: 6px;
  width: 100%;
  height: 100%;
  min-height: 0;
  padding: 8px 12px 10px;
  box-sizing: border-box;
}

.ib-bar__label {
  flex: 0 0 auto;
  font-family: var(--font-title);
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--ed-brass);
}

/* Sections stack vertically, one per row, and this scrolls when they run past
   the column's height. */
.ib-bar__scroll {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: var(--ed-gap);
}

.ib-hint {
  margin: 0;
  padding: 4px 2px;
  font-size: 0.78rem;
  color: var(--ed-text-dim);
}

/* Full column width — the sections read as a vertical stack, not a row. */
.ib-card {
  flex: 0 0 auto;
  width: 100%;
}

.ib-issues {
  flex: 1 1 100%;
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 8px 10px;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
  background: rgba(15, 23, 42, 0.25);
}

.ib-issue {
  margin: 0;
  font-size: 0.76rem;
}

.ib-issue--error {
  color: var(--ed-danger);
}

.ib-issue--warning {
  color: #e0b258;
}

.ib-note {
  margin: 0;
  font-size: 0.8rem;
  color: var(--ed-text);
  line-height: 1.5;
}

.ib-dim {
  color: var(--ed-text-dim);
  font-weight: 400;
}

.ib-display-only {
  margin: 0;
  font-size: 0.76rem;
  color: #e0b258;
}

/* Advisory (non-blocking) — a saved name nothing reads back. Same amber as the
   warning issues, with a faint rule so it reads as a nudge, not a hard error. */
.ib-warning {
  margin: 6px 0 0;
  padding: 6px 10px;
  border-left: 2px solid #e0b258;
  background: rgba(224, 178, 88, 0.08);
  border-radius: 0 4px 4px 0;
  font-size: 0.76rem;
  line-height: 1.5;
  color: #e0b258;
}
</style>
