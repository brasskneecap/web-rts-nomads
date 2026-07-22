<template>
  <RepeatableList
    :rows="rows.length"
    add-label="Add Ability Stat"
    :empty-text="emptyText"
    @add="addRow"
  >
    <div v-for="(row, idx) in rows" :key="idx" class="abilstat-row" data-test="ability-stat-row">
      <FilterableSelect
        :model-value="row.id"
        :options="options"
        placeholder="Select stat…"
        :aria-label="`Ability stat ${idx + 1}`"
        @update:model-value="onPick(idx, $event)"
      />
      <label class="abilstat-num">
        <span>Flat</span>
        <input
          v-model.number="row.flat"
          type="number"
          step="0.5"
          :aria-label="`${row.id || 'stat'} flat bonus`"
          data-test="ability-stat-flat"
        />
      </label>
      <!-- A whole quantity takes no percentage: +15% of 3 bounces rounds
           straight back to 3. The server REJECTS an authored pct on these
           (validateAbilityStats), so offering the input would be offering a
           save error. flatOnly comes from the server so the rule lives in
           one place. -->
      <label v-if="!isFlatOnly(row.id)" class="abilstat-num">
        <span>%</span>
        <input
          v-model.number="row.pct"
          type="number"
          step="5"
          :aria-label="`${row.id || 'stat'} percentage bonus`"
          data-test="ability-stat-pct"
        />
      </label>
      <span v-else class="abilstat-flatonly" data-test="ability-stat-flatonly">whole numbers only</span>
      <button type="button" class="abilstat-del" title="Remove" @click="removeRow(idx)">✕</button>
    </div>
  </RepeatableList>
</template>

<script setup lang="ts">
// AbilityStatsEditor: the BROAD "+2s duration / +15% radius" rows a unit type or
// an item contributes to every ability its owner casts (server: ability_stats.go).
//
// Shared by the unit and item editors deliberately — the authored shape is the
// same map in both, so one component means the two panels cannot drift.
//
// The offered rows come from the SERVER (/catalog/ability-stats), not a local
// list: scoped ids like "create_zone.duration" are derived from the action
// registry, so a hand-maintained mirror would go stale the moment an action
// gained or lost a kinded field.
//
// Percentages are authored as WHOLE numbers here (15 means +15%) and stored as
// the fraction the server folds (0.15) — the conversion lives at this boundary
// so neither the wire format nor the designer has to think about the other.
import { computed, ref, watch } from 'vue'
import RepeatableList from './editor/RepeatableList.vue'
import FilterableSelect, { type FilterableOption } from './editor/FilterableSelect.vue'

/** One row offered by the server. `flatOnly` means "no percentage column". */
export interface AbilityStatDef {
  id: string
  label: string
  kind: string
  action?: string
  flatOnly?: boolean
}

/** The authored value for one stat id. Both default to 0/absent. */
export interface AbilityStatMod {
  flat?: number
  pct?: number
}

const props = defineProps<{
  modelValue?: Record<string, AbilityStatMod>
  defs: AbilityStatDef[]
  /** Shown when nothing is authored — worded per host panel. */
  emptyText?: string
}>()

const emit = defineEmits<{ 'update:modelValue': [Record<string, AbilityStatMod>] }>()

interface Row {
  id: string
  flat: number
  /** Whole percent as the designer types it (15), not the stored fraction. */
  pct: number
}

const rows = ref<Row[]>([])

const emptyText = computed(
  () => props.emptyText ?? 'No ability stats — this does not change any ability.',
)

const flatOnlyIDs = computed(() => new Set(props.defs.filter((d) => d.flatOnly).map((d) => d.id)))
function isFlatOnly(id: string): boolean {
  return flatOnlyIDs.value.has(id)
}

// An already-authored id is kept as an option even if the server no longer
// offers it, so an existing value never silently vanishes from the picker (same
// rule the unit editor's Base Stats options follow).
const options = computed<FilterableOption[]>(() => {
  const opts = new Map<string, string>()
  for (const d of props.defs) opts.set(d.id, d.label)
  for (const row of rows.value) if (row.id && !opts.has(row.id)) opts.set(row.id, row.id)
  return [...opts].map(([id, label]) => ({ id, label }))
})

// Seed rows from the incoming map. Guarded against the echo of our own emit:
// re-seeding on every parent update would clobber a half-typed row (an id
// picked but no value yet), which the map round-trip cannot represent.
let selfEmitted = false
watch(
  () => props.modelValue,
  (v) => {
    if (selfEmitted) {
      selfEmitted = false
      return
    }
    rows.value = Object.entries(v ?? {}).map(([id, m]) => ({
      id,
      flat: m.flat ?? 0,
      pct: (m.pct ?? 0) * 100,
    }))
  },
  { immediate: true, deep: true },
)

watch(
  rows,
  (list) => {
    const out: Record<string, AbilityStatMod> = {}
    for (const row of list) {
      if (!row.id) continue // a row whose stat isn't chosen yet contributes nothing
      const mod: AbilityStatMod = {}
      if (row.flat) mod.flat = row.flat
      // A flat-only stat drops any percentage outright rather than sending one
      // the server will reject — switching a row's stat to a count after typing
      // a percentage must not make the whole def unsaveable.
      if (row.pct && !isFlatOnly(row.id)) mod.pct = row.pct / 100
      out[row.id] = mod
    }
    selfEmitted = true
    emit('update:modelValue', out)
  },
  { deep: true },
)

function addRow() {
  rows.value.push({ id: '', flat: 0, pct: 0 })
}
function removeRow(idx: number) {
  rows.value.splice(idx, 1)
}
function onPick(idx: number, id: string) {
  const row = rows.value[idx]
  if (!row) return
  row.id = id
  if (isFlatOnly(id)) row.pct = 0
}
</script>

<style scoped>
.abilstat-row {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.abilstat-num {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  font-size: 0.85em;
  opacity: 0.85;
}

.abilstat-num input {
  width: 5rem;
}

.abilstat-flatonly {
  font-size: 0.8em;
  opacity: 0.6;
  font-style: italic;
}

.abilstat-del {
  margin-left: auto;
}
</style>
