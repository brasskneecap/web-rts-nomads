<template>
  <div v-if="open" class="aad-overlay" data-test="add-action-overlay" @click.self="onClose">
    <div class="aad-panel" role="dialog" aria-modal="true" aria-label="Add Action">
      <header class="aad-panel__head">
        <span class="aad-panel__title">Add Action</span>
        <UiButton size="sm" variant="secondary" data-test="add-action-close" @click="onClose">Close</UiButton>
      </header>

      <input
        ref="searchInputEl"
        v-model="search"
        type="search"
        class="aad-panel__search"
        placeholder="Search actions…"
        aria-label="Search action types"
        data-test="add-action-search"
      />

      <div v-if="schemaCategories.length" class="aad-panel__chips" role="tablist" aria-label="Filter by category">
        <button
          type="button"
          class="aad-panel__chip"
          :class="{ 'aad-panel__chip--active': activeCategory === 'All' }"
          data-test="add-action-chip"
          data-category="All"
          @click="activeCategory = 'All'"
        >All</button>
        <button
          v-for="cat in schemaCategories"
          :key="cat"
          type="button"
          class="aad-panel__chip"
          :class="{ 'aad-panel__chip--active': activeCategory === cat }"
          data-test="add-action-chip"
          :data-category="cat"
          @click="activeCategory = cat"
        >{{ cat }}</button>
      </div>

      <p v-if="!builder.schema.value" class="aad-panel__loading">Loading actions…</p>

      <div v-else class="aad-panel__list">
        <button
          v-for="entry in entries"
          :key="entry.type"
          type="button"
          class="aad-panel__entry"
          data-test="add-action-entry"
          :data-type="entry.type"
          @click="onPick(entry.type)"
        >
          <span class="aad-panel__entry-label">{{ entry.label }}</span>
          <span
            v-if="!entry.runnable"
            class="aad-panel__badge"
            title="Not executed by the runtime yet"
          >display-only</span>
        </button>

        <p v-if="entries.length === 0" class="aad-panel__empty">No matching actions.</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
// AddActionDialog: opened by a FlowTriggerCard's "+ Action" button, with that
// trigger's id passed in explicitly as a prop (never read from
// builder.selected — see FlowTriggerCard.vue). Replaces the old permanent
// ActionPalette footer: search -> category pills -> one FLAT list of every
// action type the server describes (builder.schema.value.actions). Picking an
// entry appends it to the given trigger and closes.
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import UiButton from '@/components/ui/UiButton.vue'
import { useAbilityBuilderContext } from './AbilityBuilderContext'
import { humanizeActionType } from './summarizeAction'
import type { NodePath } from './programTree'

const props = defineProps<{
  open: boolean
  /** The trigger this dialog adds actions to, by NodePath — always provided
      by the caller (never read from builder.selected). */
  triggerPath: NodePath
}>()
const emit = defineEmits<{ close: [] }>()

const builder = useAbilityBuilderContext()

const search = ref('')
const activeCategory = ref<string>('All')
const searchInputEl = ref<HTMLInputElement | null>(null)

// CATEGORY_BY_TYPE is a small, hand-maintained lookup (design §9 categories)
// mapping each known action type to a dialog section. Any type the server
// describes that isn't in this table — a brand-new action type this table
// hasn't been updated for yet, or `custom` — falls into "Other" rather than
// being silently dropped from the list. Carried over verbatim from the
// retired ActionPalette.vue.
const CATEGORY_BY_TYPE: Record<string, string> = {
  select_targets: 'Targets',
  store_targets: 'Targets',
  filter_targets: 'Targets',
  deal_damage: 'Combat',
  restore_health: 'Combat',
  apply_status: 'Combat',
  apply_status_duration: 'Combat',
  change_stat: 'Combat',
  apply_mark: 'Combat',
  apply_color_overlay: 'Combat',
  remove_status: 'Combat',
  create_zone: 'World',
  summon_unit: 'World',
  launch_projectile: 'World',
  move_unit: 'World',
  apply_force: 'World',
  modify_resource: 'Resources',
  trigger_event: 'Flow',
  wait: 'Flow',
  conditional: 'Flow',
  repeat: 'Flow',
  play_presentation: 'Presentation',
  play_sound: 'Presentation',
  change_render_layer: 'Presentation',
  camera_shake: 'Presentation',
}

// Fixed display order. "Other" is always last and only appears when it has
// entries.
const CATEGORY_ORDER = ['Targets', 'Combat', 'World', 'Resources', 'Flow', 'Presentation', 'Other']

function categoryFor(type: string): string {
  return CATEGORY_BY_TYPE[type] ?? 'Other'
}

interface PaletteEntry {
  type: string
  label: string
  runnable: boolean
}

// schemaCategories: which categories actually have at least one entry in the
// full schema (independent of the current search text / chip selection) —
// this is what decides which filter chips render, so a category never shows
// an empty, dead-end chip.
const schemaCategories = computed<string[]>(() => {
  const actions = builder.schema.value?.actions ?? []
  const present = new Set(actions.map((a) => categoryFor(a.type)))
  return CATEGORY_ORDER.filter((cat) => present.has(cat))
})

// entries is ONE FLAT LIST, not grouped sections. The category is what the
// pills filter BY — repeating it as a section heading above the entries was
// redundant (a "Filter Targets" row under a "TARGETS" heading says the same
// thing twice). Category still drives the ORDER, so related actions stay
// adjacent, and CATEGORY_ORDER remains the single source for both.
//
// The active pill and the search text (matching the humanized label or the
// raw type, case-insensitive) compose as an AND.
const entries = computed<PaletteEntry[]>(() => {
  const actions = builder.schema.value?.actions ?? []
  const q = search.value.trim().toLowerCase()
  const out: { entry: PaletteEntry; order: number }[] = []
  for (const action of actions) {
    const cat = categoryFor(action.type)
    if (activeCategory.value !== 'All' && cat !== activeCategory.value) continue
    const label = humanizeActionType(action.type)
    if (q && !label.toLowerCase().includes(q) && !action.type.toLowerCase().includes(q)) continue
    out.push({ entry: { type: action.type, label, runnable: action.runnable }, order: CATEGORY_ORDER.indexOf(cat) })
  }
  // Category order first, then alphabetical within a category — a stable,
  // predictable list to scan.
  out.sort((a, b) => a.order - b.order || a.entry.label.localeCompare(b.entry.label))
  return out.map((o) => o.entry)
})

// onPick adds the action even when its schema marks it runnable: false —
// authors may build the full flow structure ahead of executor support (the
// compiler cares about structural completeness, not runtime coverage); the
// "display-only" marker above just tells them what to expect once it lands
// in the tree (FlowActionCard shows the same marker there).
function onPick(type: string) {
  // addAction selects the new action itself (see useAbilityBuilder), so the
  // bottom inspector focuses it immediately once this dialog closes.
  builder.addAction(props.triggerPath, type)
  emit('close')
}

function onClose() {
  emit('close')
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') onClose()
}

// Reset search/category and focus the search input every time the dialog
// (re)opens, and toggle the window-level Escape listener along with it —
// removing it while closed keeps a not-currently-shown dialog instance (one
// exists per FlowTriggerCard) from ever intercepting an Escape meant for
// something else. `immediate: true` matters here: FlowTriggerCard mounts its
// AddActionDialog instance up front with `open` starting false, but tests
// (and any future caller) may mount it already open, in which case the
// initial value would never fire a plain (non-immediate) watch.
watch(
  () => props.open,
  (isOpen) => {
    if (isOpen) {
      search.value = ''
      activeCategory.value = 'All'
      window.addEventListener('keydown', onKeydown)
      void nextTick(() => searchInputEl.value?.focus())
    } else {
      window.removeEventListener('keydown', onKeydown)
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
})
</script>

<style scoped>
.aad-overlay {
  position: fixed;
  inset: 0;
  background: rgba(3, 8, 14, 0.72);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 60;
}

.aad-panel {
  width: min(560px, 90vw);
  max-height: 80vh;
  overflow-y: auto;
  background: rgba(8, 14, 24, 0.96);
  border: 1px solid var(--ed-line-strong);
  border-radius: 12px;
  padding: 14px 16px 16px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.aad-panel__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--ed-line);
  font-family: var(--font-title);
  font-size: 0.9rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--ed-brass);
}

.aad-panel__search {
  width: 100%;
  box-sizing: border-box;
}

.aad-panel__chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.aad-panel__chip {
  padding: 3px 12px;
  font-family: var(--font-title);
  font-size: 0.68rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--ed-text-dim);
  background: rgba(15, 23, 42, 0.25);
  border: 1px solid var(--ed-line);
  border-radius: 999px;
}

.aad-panel__chip:hover {
  color: var(--ed-brass);
  border-color: var(--ed-brass);
}

.aad-panel__chip--active {
  color: #17120c;
  background: var(--ed-brass);
  border-color: var(--ed-brass);
}

.aad-panel__loading {
  margin: 0;
  font-size: 0.78rem;
  color: var(--ed-text-dim);
}

/* A plain vertical list. The pills above are the only rounded/floating
   affordance in here; the actions themselves read as rows to scan, which is
   why they're full-width with a hairline between rather than wrapped chips. */
.aad-panel__list {
  display: flex;
  flex-direction: column;
  overflow-y: auto;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}

.aad-panel__entry {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px 12px;
  font-family: var(--font-body);
  font-size: 0.82rem;
  text-align: left;
  color: var(--ed-text);
  background: none;
  border: 0;
  border-bottom: 1px solid var(--ed-line);
}

.aad-panel__entry:last-of-type {
  border-bottom: 0;
}

.aad-panel__entry:hover {
  color: var(--ed-brass);
  background: rgba(212, 168, 71, 0.1);
}

.aad-panel__entry-label {
  flex: 1 1 auto;
  min-width: 0;
}

.aad-panel__badge {
  flex: 0 0 auto;
  border-radius: 999px;
  padding: 1px 7px;
  font-size: 0.62rem;
  font-weight: 700;
  letter-spacing: 0.02em;
  white-space: nowrap;
  color: #e0b258;
  background: rgba(224, 178, 88, 0.14);
  border: 1px solid rgba(224, 178, 88, 0.4);
}

.aad-panel__empty {
  margin: 0;
  padding: 10px 12px;
  font-size: 0.78rem;
  color: var(--ed-text-dim);
}
</style>
