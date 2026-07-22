<template>
  <div class="abilstat">
    <p v-if="rows.length === 0" class="abilstat-empty">{{ emptyText }}</p>

    <!-- Laid out exactly like the per-rank unit stats: the stat NAME is a plain
         label above the fields it names, and the ✕ sits to the right of those
         fields. The stat is chosen from the "Add a stat…" picker below rather
         than from a per-row dropdown, so a row always names a real stat — there
         is no half-made row to lose. -->
    <div v-if="rows.length > 0" class="abilstat-grid">
      <div v-for="(row, idx) in rows" :key="row.id" class="abilstat-row" data-test="ability-stat-row">
        <span class="abilstat-label" :data-test="`ability-stat-name-${row.id}`">{{ labelFor(row.id) }}</span>

        <div class="abilstat-fields">
          <label class="abilstat-num">
            <span>Flat</span>
            <input
              :value="row.flat ?? ''"
              type="number"
              step="0.5"
              :min="floorOf(row.id).flat"
              :placeholder="placeholderFor(row.id, 'flat')"
              :aria-label="`${row.id} flat bonus`"
              data-test="ability-stat-flat"
              @input="onNumInput(idx, 'flat', $event)"
              @change="commitRow(idx)"
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
              :value="row.pct ?? ''"
              type="number"
              step="5"
              :min="floorOf(row.id).pct"
              :placeholder="placeholderFor(row.id, 'pct')"
              :aria-label="`${row.id} percentage bonus`"
              data-test="ability-stat-pct"
              @input="onNumInput(idx, 'pct', $event)"
              @change="commitRow(idx)"
            />
          </label>
          <span v-else class="abilstat-flatonly" data-test="ability-stat-flatonly">whole numbers only</span>

          <!-- Removable only when nothing BELOW holds this stat. A row inherited
               from the unit or a lower rank is not this level's to take away. -->
          <button
            v-if="!isInherited(row.id)"
            type="button"
            class="abilstat-del"
            title="Remove"
            :data-test="`ability-stat-remove-${row.id}`"
            @click="removeRow(idx)"
          >✕</button>
        </div>
      </div>
    </div>

    <div v-if="addableStats.length > 0" class="abilstat-add">
      <select :value="''" aria-label="Add an ability stat" data-test="ability-stat-add" @change="onAddStat">
        <option value="">Add a stat…</option>
        <option v-for="opt in addableStats" :key="opt.id" :value="opt.id">{{ opt.label }}</option>
      </select>
    </div>
  </div>
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
  /**
   * Values carried from BELOW this level, accumulated (see the unit editor's
   * accumulatedAbilityStatsBelow). Rows appear for these even when this level
   * authors nothing, each floored at the inherited value so a promotion can
   * never weaken a unit. Omitted by the unit and item editors, which have
   * nothing below them — those keep the plain add/remove behaviour.
   */
  inherited?: Record<string, AbilityStatMod>
  /** Names the source in the inherited note, e.g. "bronze". */
  inheritedFrom?: string
}>()

const emit = defineEmits<{ 'update:modelValue': [Record<string, AbilityStatMod>] }>()

interface Row {
  id: string
  /** UNDEFINED means "not set at this level" — the field renders blank so the
   *  inherited placeholder shows through. A 0 would hide it and read as an
   *  authored zero. */
  flat?: number
  /** Whole percent as the designer types it (15), not the stored fraction. */
  pct?: number
}

const rows = ref<Row[]>([])

const emptyText = computed(
  () => props.emptyText ?? 'No ability stats — this does not change any ability.',
)

const flatOnlyIDs = computed(() => new Set(props.defs.filter((d) => d.flatOnly).map((d) => d.id)))
function isFlatOnly(id: string): boolean {
  return flatOnlyIDs.value.has(id)
}

const inherited = computed<Record<string, AbilityStatMod>>(() => props.inherited ?? {})

/** True when the value comes from BELOW this level rather than being set here. */
function isInherited(id: string): boolean {
  return id !== '' && inherited.value[id] !== undefined
}

// Falls back to the raw id so a stat the server no longer offers still renders
// as its own row rather than vanishing and taking its value with it.
function labelFor(id: string): string {
  return props.defs.find((d) => d.id === id)?.label ?? id
}

// The floor is whatever is carried from below — in the editor's whole-percent
// units, matching what the inputs display.
function floorOf(id: string): { flat: number | undefined; pct: number | undefined } {
  const inh = inherited.value[id]
  if (!inh) return { flat: undefined, pct: undefined }
  return { flat: inh.flat, pct: inh.pct === undefined ? undefined : inh.pct * 100 }
}

// The inherited value shows as a FADED placeholder in the field itself rather
// than as a worded note — the same treatment the per-rank unit stats use, and
// far less text per row now that rows sit side by side.
function placeholderFor(id: string, which: 'flat' | 'pct'): string {
  const floor = floorOf(id)
  const v = which === 'flat' ? floor.flat : floor.pct
  return v === undefined ? '' : String(v)
}

// Clamp on COMMIT, not per keystroke — typing "1" on the way to "15" under a
// floor of 10 would otherwise snap before the second digit arrived.
function commitRow(idx: number) {
  const row = rows.value[idx]
  if (!row || !row.id) return
  const floor = floorOf(row.id)
  // Only a value the author actually typed is clamped — an untouched (blank)
  // field stays blank so it keeps showing the inherited placeholder.
  if (floor.flat !== undefined && row.flat !== undefined && row.flat < floor.flat) row.flat = floor.flat
  if (floor.pct !== undefined && row.pct !== undefined && row.pct < floor.pct) row.pct = floor.pct
}

function onNumInput(idx: number, field: 'flat' | 'pct', e: Event) {
  const row = rows.value[idx]
  if (!row) return
  const raw = (e.target as HTMLInputElement).value
  row[field] = raw === '' ? undefined : Number(raw)
}

// Stats not already on a row — what the picker can still add.
const addableStats = computed(() => {
  const present = new Set(rows.value.map((r) => r.id))
  return props.defs
    .filter((d) => !present.has(d.id))
    .map((d) => ({ id: d.id, label: d.label }))
    .sort((a, b) => a.label.localeCompare(b.label))
})

// Seed rows from the incoming map — but only when its CONTENT actually changes.
//
// Watching the object (even deeply) re-seeded on every parent re-render, because
// the parent rebuilds this map each time. That silently destroyed any row the
// map cannot represent: a freshly-added blank row (no stat picked yet) vanished
// a tick after "Add" was clicked, so a rank inheriting a stat could not add a
// second one at all. A content signature only fires on a real change.
const modelSignature = computed(() => signatureOf(props.modelValue ?? {}))

function signatureOf(map: Record<string, AbilityStatMod>): string {
  return Object.entries(map)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([id, m]) => `${id}:${m.flat ?? ''}:${m.pct ?? ''}`)
    .join('|')
}

// rowsSignature is what THIS component last emitted. When the incoming map
// matches it the change is our own echo, so there is nothing to re-seed.
// Starts null — an empty map's signature is '' and would otherwise match on the
// FIRST run, skipping the initial seed entirely (inherited rows never appeared).
const rowsSignature = ref<string | null>(null)

watch(
  modelSignature,
  (sig) => {
    if (sig === rowsSignature.value) return
    rows.value = seedRows(props.modelValue ?? {})
    rowsSignature.value = sig
  },
  { immediate: true },
)

// Re-seed when what's carried from below changes — a stat introduced at bronze
// has to appear at silver and gold immediately, and disappear from them when
// bronze takes it back.
//
// Watches a SIGNATURE, not the object. The parent rebuilds this map on every
// render, so watching identity (even deeply) re-seeded on every keystroke's
// round-trip and wiped the value being typed.
const inheritedSignature = computed(() =>
  Object.entries(inherited.value)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([id, m]) => `${id}:${m.flat ?? ''}:${m.pct ?? ''}`)
    .join('|'),
)

watch(inheritedSignature, () => {
  rows.value = seedRows(props.modelValue ?? {})
  rowsSignature.value = signatureOf(props.modelValue ?? {})
})

// Rows are the UNION of what this level authored and what it inherits, so an
// inherited stat is visible (and floored) even before this level touches it.
function seedRows(authored: Record<string, AbilityStatMod>): Row[] {
  const ids = [...new Set([...Object.keys(inherited.value), ...Object.keys(authored)])].sort()
  return ids.map((id) => {
    const mine = authored[id]
    // Left at 0 (rendering blank against the placeholder) when this level has
    // authored nothing — the inherited number shows faded instead of being
    // copied in, so "not set here" reads differently from "set to the same".
    return {
      id,
      flat: mine?.flat,
      pct: mine?.pct !== undefined ? mine.pct * 100 : undefined,
    }
  })
}

watch(
  rows,
  (list) => {
    const out: Record<string, AbilityStatMod> = {}
    for (const row of list) {
      const mod: AbilityStatMod = {}
      if (row.flat) mod.flat = row.flat
      // A flat-only stat drops any percentage outright rather than sending one
      // the server will reject — switching a row's stat to a count after typing
      // a percentage must not make the whole def unsaveable.
      if (row.pct && !isFlatOnly(row.id)) mod.pct = row.pct / 100
      // An INHERITED row the author has not changed is not authored HERE.
      // Emitting it would make this level silently own a value it merely
      // displays: the row would gain a remove button, taking the stat back at
      // the rank that really set it would leave orphaned copies behind, and an
      // empty {} would OVERRIDE the real value when higher ranks accumulate
      // what is below them.
      //
      // "Not changed" covers both shapes: a value equal to the inherited one,
      // and an untouched (blank) field.
      const untouched = mod.flat === undefined && mod.pct === undefined
      if (isInherited(row.id) && (untouched || matchesInherited(row.id, mod))) continue
      out[row.id] = mod
    }
    rowsSignature.value = signatureOf(out)
    emit('update:modelValue', out)
  },
  { deep: true },
)

// True when a row's value is exactly what it inherits — i.e. untouched.
function matchesInherited(id: string, mod: AbilityStatMod): boolean {
  const inh = inherited.value[id]
  if (!inh) return false
  return (mod.flat ?? 0) === (inh.flat ?? 0) && (mod.pct ?? 0) === (inh.pct ?? 0)
}

// Adding seeds an EMPTY row (no flat/pct) so the field shows blank against any
// inherited placeholder. The row exists the moment a stat is chosen, so there is
// never a half-made row for a re-seed to destroy — the failure mode the old
// pick-from-the-row flow had.
function onAddStat(e: Event) {
  const select = e.target as HTMLSelectElement
  const id = select.value
  select.value = ''
  if (!id) return
  rows.value = [...rows.value, { id }].sort((a, b) => a.id.localeCompare(b.id))
}
function removeRow(idx: number) {
  rows.value.splice(idx, 1)
}

</script>

<style scoped>
.abilstat {
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
  min-width: 0;
}

.abilstat-empty {
  margin: 0;
  font-size: 0.78rem;
  color: var(--ed-text-dim);
}

/* Rows flow left-to-right and wrap, matching the per-rank unit-stat grid. */
.abilstat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(15rem, 1fr));
  gap: 0.6rem 1rem;
  align-items: start;
}

.abilstat-row {
  display: flex;
  flex-direction: column;
  gap: 0.2rem;
  min-width: 0;
  font-size: 0.9em;
}

.abilstat-label {
  opacity: 0.85;
}

.abilstat-fields {
  display: flex;
  align-items: center;
  gap: 0.4rem;
}

.abilstat-num {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  font-size: 0.85em;
  opacity: 0.85;
}

.abilstat-num input {
  width: 4.5rem;
  min-width: 0;
}

/* A blank field showing an inherited placeholder reads as "not set here". */
.abilstat-num input::placeholder {
  opacity: 0.45;
  font-style: italic;
}

.abilstat-flatonly {
  font-size: 0.78em;
  opacity: 0.6;
  font-style: italic;
}

/* IDENTICAL to unit-editor__row-del / rank-panel__row-del — the bordered ✕ every
   removable row in these editors uses. */
.abilstat-del {
  flex: 0 0 auto;
  padding: 4px 8px;
  font-size: 0.76rem;
  color: var(--ed-text-dim);
  background: none;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}

.abilstat-del:hover {
  color: var(--ed-danger);
  border-color: rgba(240, 132, 108, 0.4);
}
</style>
