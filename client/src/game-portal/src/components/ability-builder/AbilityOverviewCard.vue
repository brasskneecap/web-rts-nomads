<template>
  <div class="ab-overview" data-test="ability-overview-card">
    <div class="ab-overview__top">
      <!-- Click the icon to open the picker (effects / projectiles / upload).
           Rendered by AbilityIconCanvas — the SAME code the in-game action bar
           uses — so this preview is exactly the action icon. -->
      <button
        type="button"
        class="ab-overview__icon-btn"
        data-test="ability-overview-icon"
        aria-label="Change icon"
        title="Change icon"
        @click="pickerOpen = true"
      >
        <AbilityIconCanvas :icon="form.icon" :ability-id="form.id" :size="64" />
      </button>

      <button
        type="button"
        class="ab-overview__identity"
        data-test="overview-open-settings"
        aria-label="Open ability settings"
        @click="onOpenSettings"
      >
        <div class="ab-overview__name-block">
          <span class="ab-overview__name">{{ displayName }}</span>
          <span class="ab-overview__id">{{ form.id || '(unsaved)' }}</span>
        </div>

        <div class="ab-overview__badges">
          <span v-if="form.category" class="ab-overview__badge">{{ form.category }}</span>
          <span v-if="typeLabel" class="ab-overview__badge">{{ typeLabel }}</span>
        </div>
      </button>
    </div>

    <AbilityIconPicker
      v-if="pickerOpen"
      :model-icon="form.icon"
      :ability-id="form.id"
      @update:icon="onIconChosen"
      @close="pickerOpen = false"
    />

    <div v-if="statRows.length" class="ab-overview__stats">
      <span v-for="row in statRows" :key="row.label" class="ab-overview__stat">
        <span class="ab-overview__stat-label">{{ row.label }}</span>
        <span class="ab-overview__stat-value">{{ row.value }}</span>
      </span>
    </div>

    <p v-if="entrySummaryText" class="ab-overview__entry">{{ entrySummaryText }}</p>

    <div v-if="tags.length" class="ab-overview__tags">
      <span v-for="t in tags" :key="t" class="ab-overview__tag">{{ t }}</span>
    </div>

    <p v-if="descriptionText" class="ab-overview__desc">
      {{ descriptionText }}<span v-if="isOverride" class="ab-overview__desc-hint"> (override)</span>
    </p>

    <p v-if="!builder.runnable.value" class="ab-overview__display-only" data-test="overview-display-only-banner">
      This ability uses mechanics the composable runtime doesn't execute yet — it's shown for authoring but won't
      run in-game until a later phase.
    </p>
  </div>
</template>

<script setup lang="ts">
// AbilityOverviewCard: a compact, mostly-read-only summary of the selected
// ability, sitting above the tabs (visible on both Identity and Build, since
// it's a summary either workflow benefits from, and its button is a
// shortcut into Identity from Build). It navigates (clicking the identity
// row selects the ability node AND emits open-identity so
// AbilityBuilderPanel switches to the Identity tab) but does not itself
// edit anything.
//
// open-identity is an explicit emit, not something the panel infers by
// watching `builder.selected` — selected is ALREADY {kind:'ability'} most of
// the time (it's the default/reset selection), so clicking this button while
// selected is already 'ability' would be a same-value write a watcher never
// fires for. The click is a distinct user action every time; it needs its
// own event.
import { computed, ref } from 'vue'
import { useAbilityBuilderContext } from './AbilityBuilderContext'
import { summarizeEntry } from './summarizeEntry'
import AbilityIconCanvas from './AbilityIconCanvas.vue'
import AbilityIconPicker from './AbilityIconPicker.vue'

const emit = defineEmits<{
  (e: 'open-identity'): void
}>()

const builder = useAbilityBuilderContext()

function onOpenSettings() {
  builder.select({ kind: 'ability' })
  emit('open-identity')
}

const form = computed(() => builder.form.value)

// ── icon picker ─────────────────────────────────────────────────────────────
const pickerOpen = ref(false)
function onIconChosen(icon: string) {
  builder.updateForm({ icon })
  pickerOpen.value = false
}

const displayName = computed(() => form.value.displayName || form.value.id || 'New ability')

const typeLabel = computed(() => {
  if (form.value.type === 'spell') return 'Spell'
  if (form.value.type === 'passive') return 'Passive'
  return ''
})

// statRows: only the cost/timing fields that are actually set — an unset
// field means "not authored yet", not "authored as zero", so both are
// treated the same (hidden) except where the field IS a number (0 is a
// legitimate authored value, e.g. a free spell, so it still renders).
interface StatRow { label: string; value: string }
const statRows = computed<StatRow[]>(() => {
  const rows: StatRow[] = []
  if (typeof form.value.manaCost === 'number') rows.push({ label: 'Mana', value: String(form.value.manaCost) })
  if (typeof form.value.cooldown === 'number') rows.push({ label: 'Cooldown', value: `${form.value.cooldown}s` })
  if (typeof form.value.castTime === 'number') rows.push({ label: 'Cast Time', value: `${form.value.castTime}s` })
  return rows
})

const entrySummaryText = computed(() => summarizeEntry(builder.program.value.entry))

const tags = computed(() => form.value.tags ?? [])

const isOverride = computed(() => !!form.value.description?.trim())
const descriptionText = computed(() => (isOverride.value ? form.value.description! : form.value.generatedDescription ?? ''))
</script>

<style scoped>
.ab-overview {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
  background: rgba(15, 23, 42, 0.25);
}

.ab-overview__top {
  display: flex;
  align-items: center;
  gap: 10px;
}

.ab-overview__icon-btn {
  flex: 0 0 auto;
  width: 46px;
  height: 46px;
  padding: 2px;
  border: 1px solid var(--ed-line);
  border-radius: 6px;
  background: rgba(15, 23, 42, 0.4);
}

.ab-overview__icon-btn:hover {
  border-color: var(--ed-brass);
}

.ab-overview__identity {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: 1 1 auto;
  min-width: 0;
  padding: 0;
  border: none;
  background: none;
  text-align: left;
  color: inherit;
  font: inherit;
}

.ab-overview__identity:focus-visible {
  outline: 2px solid rgba(247, 216, 142, 0.9);
  outline-offset: 3px;
  border-radius: 4px;
}

.ab-overview__name-block {
  display: flex;
  flex-direction: column;
  min-width: 0;
  flex: 1 1 auto;
}

.ab-overview__name {
  font-family: var(--font-title);
  font-size: 0.94rem;
  font-weight: 700;
  color: var(--ed-brass);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.ab-overview__id {
  font-size: 0.7rem;
  color: var(--ed-text-dim);
}

.ab-overview__badges {
  flex: 0 0 auto;
  display: flex;
  gap: 6px;
}

.ab-overview__badge {
  padding: 2px 8px;
  font-size: 0.62rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--ed-brass);
  border: 1px solid var(--ed-line-strong);
  border-radius: 999px;
}

.ab-overview__stats {
  display: flex;
  flex-wrap: wrap;
  gap: 14px;
}

.ab-overview__stat {
  display: flex;
  align-items: baseline;
  gap: 4px;
  font-size: 0.78rem;
}

.ab-overview__stat-label {
  color: var(--ed-text-dim);
}

.ab-overview__stat-value {
  color: var(--ed-text);
  font-weight: 600;
}

.ab-overview__entry {
  margin: 0;
  font-size: 0.78rem;
  color: var(--ed-text-dim);
}

.ab-overview__tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.ab-overview__tag {
  padding: 1px 7px;
  font-size: 0.66rem;
  color: var(--ed-text-dim);
  border: 1px solid var(--ed-line);
  border-radius: 999px;
}

.ab-overview__desc {
  margin: 0;
  font-size: 0.78rem;
  color: var(--ed-text-dim);
  line-height: 1.5;
}

.ab-overview__desc-hint {
  color: var(--ed-brass-dim);
  font-style: italic;
}

.ab-overview__display-only {
  margin: 0;
  padding: 6px 8px;
  font-size: 0.76rem;
  color: #e0b258;
  background: rgba(224, 178, 88, 0.1);
  border: 1px solid rgba(224, 178, 88, 0.3);
  border-radius: var(--ed-radius);
}
</style>
