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
            <!-- Timing shape follows the trigger type: on_animation_marker wants
                 a marker name; on_tick has NO timing field (its cadence lives on
                 the enclosing container — see the hint); everything else falls
                 back to the generic `frame` field. -->
            <SchemaField
              v-if="timingKind === 'marker'"
              :field="{ key: 'marker', label: 'Marker', control: 'text' }"
              :model-value="selectedTrigger.timing?.marker ?? ''"
              :enums="enumsValue"
              :catalogs="builder.catalogs.value"
              @update:model-value="(v) => commitTriggerTiming({ marker: v as string })"
            />
            <p v-else-if="timingKind === 'none'" class="ib-hint" data-test="tick-cadence-hint">
              Fires every tick — set the interval on the container (its Tick Interval).
            </p>
            <SchemaField
              v-else
              :field="{ key: 'frame', label: 'Frame', control: 'number' }"
              :model-value="selectedTrigger.timing?.frame ?? 0"
              :enums="enumsValue"
              :catalogs="builder.catalogs.value"
              @update:model-value="(v) => commitTriggerTiming({ frame: v as number })"
            />

            <!-- Damage Scope: on_damage_dealt-only (see isDamageDealtTrigger).
                 Hidden for every other trigger type — the server validator
                 rejects damageScope there outright, so this mirrors that by
                 never offering what it refuses. -->
            <template v-if="isDamageDealtTrigger">
              <p class="ib-subhead">Damage Scope</p>
              <p class="ib-hint">Empty = fires on any damage this unit deals.</p>
              <div class="ib-damage-scope__categories" data-test="damage-scope-categories">
                <label v-for="cat in damageCategoryOptions" :key="cat" class="ed-check">
                  <input
                    type="checkbox"
                    :checked="damageScopeCategories.has(cat)"
                    @change="toggleDamageCategory(cat, ($event.target as HTMLInputElement).checked)"
                  />
                  {{ humanizeDamageCategory(cat) }}
                </label>
              </div>
              <EditorField label="Specific Ability" hint="(blank = any)" :for-id="abilityIdFieldId">
                <input
                  :id="abilityIdFieldId"
                  type="text"
                  :list="abilityIdListId"
                  placeholder="ability id"
                  :value="damageScopeAbilityIdText"
                  @input="onDamageScopeAbilityIdInput"
                  @change="commitDamageScopeAbilityId"
                />
              </EditorField>
              <datalist :id="abilityIdListId">
                <option v-for="id in abilityIdOptions" :key="id" :value="id" />
              </datalist>
              <p v-if="damageScopeContradiction" class="ib-warning" data-test="damage-scope-contradiction">
                {{ damageScopeContradiction }}
              </p>
            </template>
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
              <p v-if="fieldSections.length === 0 && !isConditionalAction" class="ib-hint">No configurable fields for this action type.</p>
              <p v-if="unreadSavedNames.length" class="ib-warning" data-test="ib-unread-save">
                Nothing reads {{ unreadSavedNames.length > 1 ? 'these saved names' : 'the saved name' }}
                <strong>{{ unreadSavedNames.map((n) => `"${n}"`).join(', ') }}</strong> back — the save has
                no effect. Reference it via Saved Value or Exclude Saved Set on a later query, or remove it.
              </p>
            </SectionCard>

            <!-- Conditional's GATE. Edited with the same labeled-select idiom the
                 change_stat "Stat" field uses above — has_perk is a selectable
                 op and the perk it names comes from the catalog, changeable. -->
            <SectionCard v-if="isConditionalAction" title="Condition" class="ib-card" data-test="condition-editor">
              <p class="ib-hint">Runs THEN when these hold (all must match); otherwise ELSE.</p>
              <p v-if="!conditionRows.length" class="ib-hint" data-test="condition-empty">Always runs.</p>
              <div v-for="(c, i) in conditionRows" :key="i" class="ib-condition" data-test="condition-row">
                <EditorField label="When" :for-id="`ib-cond-op-${i}`">
                  <select
                    :id="`ib-cond-op-${i}`"
                    data-test="condition-op"
                    aria-label="Condition operator"
                    :value="c.op"
                    @change="(e) => setConditionOp(i, (e.target as HTMLSelectElement).value)"
                  >
                    <option v-for="op in CONDITION_OPS" :key="op" :value="op">{{ CONDITION_OP_LABELS[op] }}</option>
                  </select>
                </EditorField>

                <EditorField v-if="isPerkOp(c.op)" label="Perk" :for-id="`ib-cond-perk-${i}`">
                  <select
                    :id="`ib-cond-perk-${i}`"
                    data-test="condition-perk"
                    aria-label="Condition perk"
                    :value="typeof c.right === 'string' ? c.right : ''"
                    @change="(e) => setConditionPerk(i, (e.target as HTMLSelectElement).value)"
                  >
                    <option v-if="!perkOptionsList.length" value="">No perks loaded</option>
                    <option v-for="p in perkOptionsList" :key="p.id" :value="p.id">{{ p.label }}</option>
                  </select>
                </EditorField>

                <EditorField v-else-if="isPresenceOp(c.op)" label="Context key" :for-id="`ib-cond-key-${i}`">
                  <input
                    :id="`ib-cond-key-${i}`"
                    data-test="condition-left"
                    type="text"
                    :value="c.left?.key ?? ''"
                    @change="(e) => setConditionLeftKey(i, (e.target as HTMLInputElement).value)"
                  />
                </EditorField>

                <template v-else>
                  <EditorField label="Context key" :for-id="`ib-cond-key-${i}`">
                    <input
                      :id="`ib-cond-key-${i}`"
                      data-test="condition-left"
                      type="text"
                      :value="c.left?.key ?? 'selected_count'"
                      @change="(e) => setConditionLeftKey(i, (e.target as HTMLInputElement).value)"
                    />
                  </EditorField>
                  <EditorField label="Value" :for-id="`ib-cond-val-${i}`">
                    <input
                      :id="`ib-cond-val-${i}`"
                      data-test="condition-right"
                      type="number"
                      :value="typeof c.right === 'number' ? c.right : 0"
                      @change="(e) => setConditionRight(i, e)"
                    />
                  </EditorField>
                </template>

                <button
                  type="button"
                  class="ib-condition__remove"
                  data-test="condition-remove"
                  @click="removeCondition(i)"
                >
                  Remove condition
                </button>
              </div>
              <button type="button" class="ib-condition__add" data-test="condition-add" @click="addCondition">
                + Condition
              </button>
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
                <!-- apply_mark's "icon" field: a VISUAL picker (the actual
                     overhead art, not an id string), which also derives and
                     commits iconKind from the chosen id's prefix in the same
                     write — see commitApplyMarkIcon's doc comment. The
                     server's separate `iconKind` schema field is filtered
                     out of fieldSections below for this action type, since
                     nothing here still needs a manual control for it. -->
                <EditorField
                  v-else-if="isApplyMarkAction && f.key === 'icon'"
                  :label="f.label"
                >
                  <OverheadIconPicker
                    :options="enumsValue.icon ?? []"
                    :model-value="typeof selectedAction.config?.icon === 'string' ? selectedAction.config.icon : ''"
                    :aria-label="f.label"
                    @update:model-value="commitApplyMarkIcon"
                  />
                </EditorField>
                <!-- change_stat's "stat" field: a CUSTOM control, not the
                     generic schema-driven enum. The server's schema Options
                     for this field is ListStatIDs() — every registered stat,
                     including aura-only ones (armorPercent,
                     projectileDamageReduction) — because change_stat's own
                     Validate rejects those at save time rather than the
                     schema hiding them. Mirrors selfStatDefs()'s exclusion
                     (the same rule a perk's top-level Unit Stat Modifiers
                     dropdown and the old apply_status "Change Status" editor
                     both used) so the dropdown never offers a stat this
                     action would fail validation for. -->
                <EditorField
                  v-else-if="isChangeStatAction && f.key === 'stat'"
                  :label="f.label"
                  :for-id="changeStatStatFieldId"
                >
                  <select
                    :id="changeStatStatFieldId"
                    :value="selectedAction.config?.stat ?? ''"
                    aria-label="Stat"
                    @change="(e) => commitActionConfig('stat', (e.target as HTMLSelectElement).value)"
                  >
                    <option v-for="d in selfStatDefsList" :key="d.id" :value="d.id">{{ d.label }}</option>
                  </select>
                </EditorField>
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
import { computed, ref, watch } from 'vue'
import type { AbilityActionDef, AbilityConditionDef, AbilityTriggerDef, TargetQueryDef, TriggerType } from '@/game/abilities/program/abilityProgram'
import { fieldVisible, schemaForAction, type SchemaField as SchemaFieldDescriptor } from '@/game/abilities/program/programSchema'
import { issuesForPath, type ValidationIssue } from '@/game/abilities/program/programValidation'
import { selfStatDefs } from '@/game/stats/statRegistry'
import EditorField from '@/components/editor/EditorField.vue'
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
import { humanizeActionType } from './summarizeAction'
import { iconKindForId } from '@/game/maps/actionIconDefs'
import OverheadIconPicker from './OverheadIconPicker.vue'
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
  'on_tick',
  'on_zone_enter',
  'on_zone_exit',
]
const triggerTypeOptions = computed<string[]>(() => {
  const fromSchema = builder.schema.value?.enums.triggerTypes
  return fromSchema && fromSchema.length > 0 ? fromSchema : CURATED_TRIGGER_TYPES
})

type TimingKind = 'marker' | 'none' | 'frame'
const timingKind = computed<TimingKind>(() => {
  const t = selectedTrigger.value?.type
  if (t === 'on_animation_marker') return 'marker'
  // on_tick carries NO trigger-level interval — the tick cadence is authored on
  // the enclosing container (Apply Status Duration / create_zone / …), so show no
  // timing field here (a second "Tick Interval" would just be confusing).
  if (t === 'on_tick') return 'none'
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

// ── Trigger: on_damage_dealt scope (damageScope) ─────────────────────────
// Follows the SAME "trigger-type-specific optional field" precedent as
// Timing above (timingKind) — DamageScope is only ever meaningful on an
// on_damage_dealt trigger (see AbilityTriggerDef.damageScope's doc comment),
// so this whole block is gated on isDamageDealtTrigger in the template and
// never renders for any other trigger type — mirroring the server validator,
// which rejects damageScope there outright (invalid_damage_scope_placement).

// Fallback for when the schema hasn't loaded yet, mirroring
// triggerTypeOptions' own fallback above. Matches the server's
// ProgramEnums()["damageCategories"] (ability_program_enums.go), itself
// sourced from allDamageCategories (damage_pipeline.go).
const CURATED_DAMAGE_CATEGORIES = ['basic_attack', 'ability', 'trap', 'building', 'perk', 'item']

const damageCategoryOptions = computed<string[]>(() => {
  const fromSchema = builder.schema.value?.enums.damageCategories
  return fromSchema && fromSchema.length > 0 ? fromSchema : CURATED_DAMAGE_CATEGORIES
})

const isDamageDealtTrigger = computed(() => selectedTrigger.value?.type === 'on_damage_dealt')

const damageScopeCategories = computed<Set<string>>(
  () => new Set(selectedTrigger.value?.damageScope?.categories ?? []),
)

// humanizeDamageCategory reuses the same snake_case -> Title Case rule as
// action/trigger types ("basic_attack" -> "Basic Attack") — the humanization
// is generic, not action-specific (see FlowTriggerCard.vue's identical reuse
// for trigger types).
const humanizeDamageCategory = humanizeActionType

// abilityIdOptions: every authored ability's id, offered as the "Specific
// Ability" datalist — sourced from the SAME loaded ability list
// RiderEditor.vue's own ability-id datalist is built from
// (fetchAuthoredAbilityDefs, surfaced here via useAbilityBuilder's
// `abilities`), so no separate fetch is needed.
const abilityIdOptions = computed<string[]>(() => builder.abilities.value.map((a) => a.id))

const abilityIdListId = 'ib-damage-scope-ability-ids'
const abilityIdFieldId = 'ib-damage-scope-ability-id'

// Local editable copy of damageScope.abilityId, committed on blur/Enter (the
// same commit-on-change discipline SchemaField's text controls use — see its
// doc comment: committing per keystroke would flood the undo stack with one
// entry per character typed). Re-synced whenever the SELECTED trigger's own
// scope changes externally (switching triggers, undo/redo).
const damageScopeAbilityIdText = ref(selectedTrigger.value?.damageScope?.abilityId ?? '')
watch(
  () => selectedTrigger.value?.damageScope?.abilityId,
  (v) => {
    damageScopeAbilityIdText.value = v ?? ''
  },
)

function onDamageScopeAbilityIdInput(e: Event) {
  damageScopeAbilityIdText.value = (e.target as HTMLInputElement).value
}

// commitDamageScope merges `patch` onto the trigger's EXISTING damageScope
// and OMITS empty fields entirely: an empty categories selection is dropped
// (never stored as `[]`), a blank/whitespace abilityId is dropped, and the
// whole `damageScope` key is omitted once both end up empty — so an
// untouched on_damage_dealt trigger round-trips with no damageScope key at
// all, matching this editor's omit-when-default convention (see
// commitActionOutput's outputs.targets, above).
function commitDamageScope(patch: { categories?: string[]; abilityId?: string }) {
  if (!selectedTrigger.value || selected.value.kind !== 'trigger') return
  const merged = { ...selectedTrigger.value.damageScope, ...patch }
  const categories = merged.categories && merged.categories.length > 0 ? merged.categories : undefined
  const abilityId = merged.abilityId?.trim() ? merged.abilityId.trim() : undefined
  const next = categories || abilityId ? { ...(categories ? { categories } : {}), ...(abilityId ? { abilityId } : {}) } : undefined
  builder.updateTrigger(selected.value.path, { damageScope: next })
}

function toggleDamageCategory(cat: string, checked: boolean) {
  const next = new Set(damageScopeCategories.value)
  if (checked) next.add(cat)
  else next.delete(cat)
  commitDamageScope({ categories: [...next] })
}

function commitDamageScopeAbilityId() {
  commitDamageScope({ abilityId: damageScopeAbilityIdText.value })
}

// damageScopeContradiction: an inline, NON-BLOCKING note mirroring the
// server's "contradictory_damage_scope" validation rule
// (ability_program_validate.go) — an abilityId set alongside a non-empty
// Categories list that excludes "ability" describes a damage instance that
// can never occur (ability-attributed damage always carries category
// "ability"). This deliberately does NOT auto-correct the author's Categories
// selection — silently rewriting a field the author didn't touch would be
// more surprising than a note; the server remains the final validator at
// Save.
const damageScopeContradiction = computed<string>(() => {
  const scope = selectedTrigger.value?.damageScope
  if (!scope?.abilityId || !scope.categories || scope.categories.length === 0) return ''
  if (scope.categories.includes('ability')) return ''
  return 'A specific ability always deals damage in category "Ability" — add "Ability" to Categories (or clear Categories) so this scope can actually fire.'
})

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
    // apply_mark's iconKind is now fully derived from the chosen icon's
    // prefix (see commitApplyMarkIcon) — the server still publishes it as a
    // schema field (it's still the stored/validated key), but nothing here
    // needs a manual control for it any more, so it's dropped from the
    // rendered fields entirely rather than kept as a dead/hidden control.
    if (isApplyMarkAction.value && f.key === 'iconKind') continue
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

// commitApplyMarkIcon: apply_mark's icon/iconKind pairing (see the template's
// doc comment above the OverheadIconPicker that calls this). iconKind is now
// DERIVED from the chosen icon id's "buff-"/"debuff-" prefix via
// iconKindForId, rather than authored separately — the id encodes the
// channel already (there's no such thing as a "debuff-*" id used as a buff),
// so asking the author to also pick iconKind was just a second place the two
// could go stale relative to each other. Clearing the icon (empty string / no
// selection) drops iconKind in the SAME commit for the same reason. If a
// future id carries neither prefix, iconKindForId returns undefined and this
// falls back to whatever iconKind was already stored — never silently wiping
// a previously-authored value it can't re-derive. This used to be
// apply_status's own "icon" pairing — that action no longer carries
// icon/iconKind at all (see applyStatusConfig's doc comment,
// ability_exec_actions.go): overhead marks are authored via a dedicated
// apply_mark action nested inside an apply_status_duration instead.
function commitApplyMarkIcon(value: unknown) {
  if (!selectedAction.value || selected.value.kind !== 'action') return
  const icon = typeof value === 'string' ? value : ''
  if (!icon) {
    builder.updateActionConfig(selected.value.path, { icon: undefined, iconKind: undefined })
    return
  }
  const iconKind = iconKindForId(icon) ?? selectedAction.value.config?.iconKind
  builder.updateActionConfig(selected.value.path, { icon, iconKind })
}

function commitActionTarget(value: unknown) {
  if (!selectedAction.value || selected.value.kind !== 'action') return
  builder.updateAction(selected.value.path, { target: value as TargetQueryDef })
}

// ── change_stat: custom "Stat" control ───────────────────────────────────────
// change_stat is ONE stat modifier (stat/op/value/stage), valid only nested
// inside an apply_status_duration's config.triggers (server-enforced — see
// ability_status_duration.go's Validate). This used to be apply_status's
// "Change Status" row-list editor (a list of PerkStatModifier-shaped rows);
// now that the server model is "one change_stat action per stat change", each
// action's inspector authors exactly one modifier — no row list needed.
//
// The "stat" field itself needs a CUSTOM control rather than the generic
// schema-driven enum: the server's schema Options for it is ListStatIDs()
// (every registered stat, including aura-only ones — change_stat's own
// Validate rejects those at save time instead of the schema pre-filtering
// them). selfStatDefs() is the single source of truth for "which stats have
// a fold site outside an aura" (same list a perk's top-level Unit Stat
// Modifiers section and PerkAura's contributions section both draw from), so
// this dropdown reuses it directly rather than re-deriving the exclusion.
const isChangeStatAction = computed(() => selectedAction.value?.type === 'change_stat')
const isApplyMarkAction = computed(() => selectedAction.value?.type === 'apply_mark')
const selfStatDefsList = selfStatDefs()
const changeStatStatFieldId = 'ib-changestat-stat'

// ── conditional: condition editor ─────────────────────────────────────────────
// The GATE on a conditional's branches, edited here in the action panel with the
// same labeled-select idiom change_stat's "Stat" field uses above — not a
// bespoke widget elsewhere. Before this the has_perk op was baked into JSON by
// hand and the perk it named couldn't be chosen or changed; now the op is a
// dropdown and the perk comes from the catalog (the same builder.catalogs.perks
// list the preview's grant control draws from) and is changeable. Ops mirror
// exactly what evaluateOneConditionLocked runs (ability_exec_flow.go): perk ops
// name a perk directly, presence ops read a named-context key, scalar ops
// compare a key (usually selected_count) against a number.
const PERK_OPS = ['has_perk', 'not_perk']
const PRESENCE_OPS = ['has', 'not']
const SCALAR_OPS = ['eq', 'ne', 'lt', 'lte', 'gt', 'gte']
const CONDITION_OPS = [...PERK_OPS, ...PRESENCE_OPS, ...SCALAR_OPS]
const CONDITION_OP_LABELS: Record<string, string> = {
  has_perk: 'has perk',
  not_perk: 'missing perk',
  has: 'is set',
  not: 'is unset',
  eq: '==',
  ne: '!=',
  lt: '<',
  lte: '<=',
  gt: '>',
  gte: '>=',
}
function isPerkOp(op: string): boolean {
  return PERK_OPS.includes(op)
}
function isPresenceOp(op: string): boolean {
  return PRESENCE_OPS.includes(op)
}

const isConditionalAction = computed(() => selectedAction.value?.type === 'conditional')
const conditionRows = computed<AbilityConditionDef[]>(() => {
  const raw = (selectedAction.value?.config as { conditions?: unknown } | undefined)?.conditions
  return Array.isArray(raw) ? (raw as AbilityConditionDef[]) : []
})
const perkOptionsList = computed(() => builder.catalogs.value.perks)

function commitConditions(next: AbilityConditionDef[]) {
  if (!selectedAction.value || selected.value.kind !== 'action') return
  builder.updateActionConfig(selected.value.path, { conditions: next })
}

// conditionForOp builds a clean condition for a chosen op, carrying whatever of
// the previous one still applies: a perk id survives a has_perk↔not_perk switch,
// a context key survives any switch, a scalar threshold defaults to
// selected_count. Switching families drops the now-meaningless field.
function conditionForOp(op: string, prev?: AbilityConditionDef): AbilityConditionDef {
  if (isPerkOp(op)) {
    const right = typeof prev?.right === 'string' && prev.right ? prev.right : (perkOptionsList.value[0]?.id ?? '')
    return { op, right } as AbilityConditionDef
  }
  if (isPresenceOp(op)) {
    return { op, left: { key: prev?.left?.key ?? '' } } as AbilityConditionDef
  }
  const right = typeof prev?.right === 'number' ? prev.right : 0
  return { op, left: { key: prev?.left?.key ?? 'selected_count' }, right } as AbilityConditionDef
}

function setConditionOp(i: number, op: string) {
  commitConditions(conditionRows.value.map((c, idx) => (idx === i ? conditionForOp(op, c) : c)))
}
function setConditionPerk(i: number, perkId: string) {
  commitConditions(conditionRows.value.map((c, idx) => (idx === i ? { ...c, right: perkId } : c)))
}
function setConditionLeftKey(i: number, key: string) {
  commitConditions(conditionRows.value.map((c, idx) => (idx === i ? { ...c, left: { key } } : c)))
}
function setConditionRight(i: number, e: Event) {
  const n = Number((e.target as HTMLInputElement).value)
  if (!Number.isFinite(n)) return
  commitConditions(conditionRows.value.map((c, idx) => (idx === i ? { ...c, right: Math.round(n) } : c)))
}
function addCondition() {
  commitConditions([...conditionRows.value, conditionForOp('has_perk')])
}
function removeCondition(i: number) {
  commitConditions(conditionRows.value.filter((_, idx) => idx !== i))
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

/* One condition of a conditional's gate: its fields stacked, with a hairline
   rule + remove control, so several ANDed conditions read as distinct rows. */
.ib-condition {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 8px 0;
  border-top: 1px dashed var(--ed-line);
}
.ib-condition:first-of-type {
  border-top: 0;
}

.ib-condition__remove {
  align-self: flex-start;
  padding: 2px 6px;
  background: none;
  border: 0;
  font-size: 0.72rem;
  color: var(--ed-text-dim);
}
.ib-condition__remove:hover {
  color: var(--ed-danger);
}

.ib-condition__add {
  align-self: flex-start;
  margin-top: 6px;
  padding: 3px 10px;
  background: rgba(148, 163, 184, 0.14);
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
  font-size: 0.75rem;
  color: var(--ed-text);
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

/* Damage Scope's own mini-heading, distinguishing it from the Trigger card's
   Type/Name/Timing fields above it without a whole separate SectionCard. */
.ib-subhead {
  margin: 6px 0 0;
  font-family: var(--font-body);
  font-size: 0.72rem;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--ed-text-dim);
}

/* Damage category checkboxes, same wrapping-row treatment as SchemaField's
   own .sf-checkgroup (not shared across component boundaries — scoped
   styles don't cross them — so this is its own copy of the same rule). */
.ib-damage-scope__categories {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 14px;
}

</style>
