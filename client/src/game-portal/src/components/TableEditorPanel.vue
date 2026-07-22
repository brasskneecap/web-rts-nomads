<template>
  <EditorShell class="table-editor">
    <template #sidebar>
      <EditorSidebar
        title="Tables"
        new-label="Add New Table"
        :groups="sidebarGroups"
        :selected-id="selectedId"
        :search="search"
        search-placeholder="Search tables…"
        empty-text="No tables match."
        @update:search="search = $event"
        @select="selectTable"
        @new="newTable"
        @duplicate="duplicateTable"
      />
    </template>

    <template #main>
      <template v-if="form">
        <EditorHeader
          :title="form.name || 'New Table'"
          :file-path="filePath"
          :id="form.id"
          :id-editable="form.isNew"
          id-input-id="te-id"
          :saving="saving"
          :save-disabled="saving || !canSave"
          :saved-label="savedLabel"
          :error="saveError"
          remove-label="Delete"
          @update:id="onIdInput"
          @save="save"
          @remove="remove"
        />

        <GameScrollArea class="table-editor__scroll">
          <div class="table-editor__grid">
            <SectionCard title="Identity" :index="1" class="table-editor__identity">
              <EditorField label="Name" for-id="te-name">
                <input id="te-name" v-model.trim="form.name" type="text" />
              </EditorField>
              <p class="table-editor__note">
                A table is rolled when a camp is cleared (or to stock a shop). Each row owns a
                slice of the die and does one thing: roll a <strong>list</strong>, grant
                <strong>resources</strong>, or drop <strong>nothing</strong>. Every roll must land
                on a row — a “nothing” row is how you author a no-drop chance.
              </p>
            </SectionCard>

            <SectionCard title="Rows" :index="2" class="table-editor__rows-card">
              <!-- Max Roll is the die these rows must tile, so it lives with the
                   rows it constrains rather than off in Identity. -->
              <EditorField label="Max Roll" hint="(the die — rows must cover 1..this)" for-id="te-maxroll">
                <input id="te-maxroll" v-model.number="form.maxRoll" type="number" min="1" />
              </EditorField>
              <RepeatableList
                :rows="form.rows.length"
                add-label="Add Row"
                empty-text="No rows yet."
                @add="addRow"
              >
                <div v-for="(row, idx) in form.rows" :key="idx" class="table-editor__row">
                  <div class="table-editor__range">
                    <input
                      v-model.number="row.min" type="number" min="1"
                      :aria-label="`Row ${idx + 1} min`" class="table-editor__num"
                    />
                    <span>–</span>
                    <input
                      v-model.number="row.max" type="number" min="1"
                      :aria-label="`Row ${idx + 1} max`" class="table-editor__num"
                    />
                  </div>

                  <select
                    :value="rowKind(row)"
                    :aria-label="`Row ${idx + 1} outcome`"
                    @change="setRowKind(row, ($event.target as HTMLSelectElement).value as RowKind)"
                  >
                    <option value="list">Roll a list</option>
                    <option value="resources">Grant resources</option>
                    <option value="nothing">Nothing</option>
                  </select>

                  <!-- List outcome — type-to-filter select (there can be many
                       lists), storing the id while showing the name. -->
                  <FilterableSelect
                    v-if="rowKind(row) === 'list'"
                    :model-value="row.list ?? ''"
                    :options="listSelectOptions"
                    placeholder="Select a list…"
                    :aria-label="`Row ${idx + 1} list`"
                    class="table-editor__list-select"
                    @update:model-value="row.list = $event"
                  />

                  <!-- Resource outcome -->
                  <div v-else-if="rowKind(row) === 'resources'" class="table-editor__resources">
                    <label>Gold
                      <input
                        type="number" min="0" class="table-editor__num"
                        :value="row.resources?.['gold'] ?? 0"
                        @input="setResource(row, 'gold', $event)"
                      />
                    </label>
                    <label>Wood
                      <input
                        type="number" min="0" class="table-editor__num"
                        :value="row.resources?.['wood'] ?? 0"
                        @input="setResource(row, 'wood', $event)"
                      />
                    </label>
                  </div>

                  <span v-else class="table-editor__nothing">drops nothing</span>

                  <button type="button" class="table-editor__row-del" @click="form.rows.splice(idx, 1)">✕</button>
                </div>
              </RepeatableList>
            </SectionCard>

            <SectionCard title="Validation" class="table-editor__validation">
              <ValidationChecklist :checks="checks" />
              <span v-if="statusNote" class="table-editor__status-note">{{ statusNote }}</span>
            </SectionCard>
          </div>
        </GameScrollArea>
      </template>

      <div v-else class="table-editor__empty">
        <p v-if="loadError" role="alert">{{ loadError }}</p>
        <p v-else>Select a table or create a new one.</p>
      </div>
    </template>
  </EditorShell>
</template>

<script setup lang="ts">
import { confirmDelete } from '@/components/editor/confirmDelete'
import { computed, onMounted, ref, watch } from 'vue'
import EditorShell from '@/components/editor/EditorShell.vue'
import EditorSidebar from '@/components/editor/EditorSidebar.vue'
import type { SidebarGroup } from '@/components/editor/EditorSidebar.vue'
import EditorHeader from '@/components/editor/EditorHeader.vue'
import EditorField from '@/components/editor/EditorField.vue'
import SectionCard from '@/components/editor/SectionCard.vue'
import RepeatableList from '@/components/editor/RepeatableList.vue'
import FilterableSelect from '@/components/editor/FilterableSelect.vue'
import ValidationChecklist from '@/components/editor/ValidationChecklist.vue'
import type { ValidationCheck } from '@/components/editor/ValidationChecklist.vue'
import GameScrollArea from '@/components/ui/GameScrollArea.vue'
import { fetchLists, fetchTables } from '@/game/maps/catalog'
import type { ListDef } from '@/game/maps/listDefs'
import type { TableDef, TableRow } from '@/game/maps/tableDefs'
import { rowOutcome } from '@/game/maps/tableDefs'
import { EditorValidationError } from '@/game/items/itemEditorApi'
import { deleteEditorTable, saveEditorTable } from '@/game/items/tableEditorApi'
import { analyzeCoverage } from '@/game/items/rollCoverage'

type TableForm = { id: string; isNew: boolean; name: string; maxRoll: number; rows: TableRow[] }
type RowKind = 'list' | 'resources' | 'nothing'

const tables = ref<TableDef[]>([])
const lists = ref<ListDef[]>([])
const selectedId = ref('')
const search = ref('')
const form = ref<TableForm | null>(null)
const saving = ref(false)
const saveError = ref('')
const statusNote = ref('')
const loadError = ref('')
const savedLabel = ref('')

const listOptions = computed(() =>
  [...lists.value].sort((a, b) => (a.name || a.id).localeCompare(b.name || b.id)),
)

// Shape the sorted lists for FilterableSelect: it stores the id, shows the name.
const listSelectOptions = computed(() =>
  listOptions.value.map((l) => ({ id: l.id, label: l.name || l.id })),
)

const sidebarGroups = computed<SidebarGroup[]>(() => {
  const term = search.value.trim().toLowerCase()
  const entries = tables.value
    .filter((t) => !term || t.name.toLowerCase().includes(term) || t.id.includes(term))
    .sort((a, b) => (a.name || a.id).localeCompare(b.name || b.id))
    .map((t) => ({ id: t.id, name: t.name || t.id }))
  return entries.length > 0 ? [{ label: 'Tables', entries }] : []
})

const filePath = computed(() =>
  form.value?.id ? `catalog/tables/${form.value.id}.json` : 'catalog/tables/',
)

// ─── Row outcome helpers ─────────────────────────────────────────────────────

function rowKind(row: TableRow): RowKind {
  const o = rowOutcome(row)
  return o === 'none' ? 'nothing' : o
}

function setRowKind(row: TableRow, kind: RowKind) {
  delete row.list
  delete row.resources
  delete row.nothing
  if (kind === 'list') row.list = ''
  else if (kind === 'resources') row.resources = { gold: 50 }
  else row.nothing = true
}

function setResource(row: TableRow, key: string, ev: Event) {
  const v = Math.max(0, Math.floor(+(ev.target as HTMLInputElement).value || 0))
  row.resources = { ...(row.resources ?? {}) }
  if (v === 0) delete row.resources[key]
  else row.resources[key] = v
}

function addRow() {
  if (!form.value) return
  // Start the new row where the last one left off, so filling the die is a
  // matter of setting the max rather than doing arithmetic on both ends.
  const last = form.value.rows[form.value.rows.length - 1]
  const min = last ? last.max + 1 : 1
  form.value.rows.push({ min, max: Math.max(min, form.value.maxRoll), nothing: true })
}

// ─── Coverage + validation ───────────────────────────────────────────────────

const coverage = computed(() => {
  if (!form.value) return { bands: [], errors: [], complete: false }
  const ranges = form.value.rows.map((r) => ({
    min: r.min, max: r.max, label: rowLabel(r),
  }))
  return analyzeCoverage(form.value.maxRoll, ranges)
})

function rowLabel(r: TableRow): string {
  const k = rowKind(r)
  if (k === 'list') return r.list ? `list: ${r.list}` : 'list: (none)'
  if (k === 'resources') {
    const parts = Object.entries(r.resources ?? {}).map(([res, n]) => `${n} ${res}`)
    return parts.length ? parts.join(', ') : 'resources: (none)'
  }
  return 'nothing'
}

const checks = computed<ValidationCheck[]>(() => {
  if (!form.value) return []
  const c: ValidationCheck[] = []
  const known = new Set(tables.value.map((t) => t.id))
  const collides = form.value.isNew && known.has(form.value.id)
  c.push(
    !form.value.id
      ? { ok: false, message: 'ID is required.' }
      : !/^[a-z0-9_]+$/.test(form.value.id)
        ? { ok: false, message: 'ID must be lowercase letters, digits and underscores only.' }
        : collides
          ? { ok: false, message: `A table with the ID "${form.value.id}" already exists.` }
          : { ok: true, message: 'ID is valid.' },
  )
  c.push(form.value.name ? { ok: true, message: 'Name is set.' } : { ok: false, message: 'Name is required.' })

  // Each row must have a resolved outcome.
  const badRows = form.value.rows.filter((r) => {
    const k = rowKind(r)
    if (k === 'list') return !r.list
    if (k === 'resources') return Object.keys(r.resources ?? {}).length === 0
    return false
  }).length
  c.push(badRows > 0
    ? { ok: false, message: `${badRows} row${badRows > 1 ? 's have' : ' has'} no outcome chosen.` }
    : { ok: true, message: 'Every row has an outcome.' })

  // Coverage: the die must be tiled. Surface the specific holes/overlaps.
  const cov = coverage.value
  if (cov.errors.length > 0) c.push({ ok: false, message: cov.errors[0] })
  else c.push({ ok: true, message: 'The die is fully covered.' })

  return c
})

const canSave = computed(() => checks.value.every((c) => c.ok))

// ─── Id ← Name ──────────────────────────────────────────────────────────────

const idManuallyEdited = ref(false)
function onIdInput(raw: string) {
  if (!form.value) return
  form.value.id = raw.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+/, '')
  idManuallyEdited.value = true
}
watch(() => form.value?.name, (name) => {
  if (form.value?.isNew && !idManuallyEdited.value) {
    form.value.id = (name ?? '').toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '')
  }
})

// ─── Catalog + CRUD ─────────────────────────────────────────────────────────

async function reload() {
  const [tableDefs, listDefs] = await Promise.all([fetchTables(), fetchLists()])
  tables.value = tableDefs
  lists.value = listDefs
}

onMounted(async () => {
  try {
    await reload()
  } catch (err) {
    loadError.value = err instanceof Error ? err.message : String(err)
  }
})

function resetStatus() {
  saveError.value = ''
  statusNote.value = ''
  savedLabel.value = ''
}

// Deep-clone a table's rows so editing the form never mutates the cached def.
function formFromDef(def: TableDef, isNew: boolean, id = def.id, name = def.name): TableForm {
  return { id, isNew, name, maxRoll: def.maxRoll, rows: def.rows.map((r) => ({ ...r, resources: r.resources ? { ...r.resources } : undefined })) }
}

function selectTable(id: string) {
  const def = tables.value.find((t) => t.id === id)
  if (!def) return
  selectedId.value = id
  resetStatus()
  idManuallyEdited.value = false
  form.value = formFromDef(def, false)
}

function newTable() {
  selectedId.value = ''
  resetStatus()
  idManuallyEdited.value = false
  form.value = { id: '', isNew: true, name: '', maxRoll: 100, rows: [{ min: 1, max: 100, nothing: true }] }
}

function duplicateTable(id: string) {
  const def = tables.value.find((t) => t.id === id)
  if (!def) return
  selectedId.value = ''
  resetStatus()
  idManuallyEdited.value = false
  form.value = formFromDef(def, true, '', `${def.name} Copy`)
  form.value.id = form.value.name.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '')
}

async function save() {
  if (!form.value || !canSave.value) return
  saving.value = true
  resetStatus()
  try {
    // Strip each row to its single chosen outcome, so a stale field from a
    // toggled-away kind never rides along to the server.
    const rows: TableRow[] = form.value.rows.map((r) => {
      const base = { min: r.min, max: r.max }
      const k = rowKind(r)
      if (k === 'list') return { ...base, list: r.list }
      if (k === 'resources') return { ...base, resources: r.resources }
      return { ...base, nothing: true }
    })
    const def: TableDef = { id: form.value.id, name: form.value.name, maxRoll: form.value.maxRoll, rows }
    await saveEditorTable(def)
    await reload()
    savedLabel.value = 'Saved'
    selectedId.value = def.id
    form.value = { ...form.value, isNew: false }
  } catch (err) {
    saveError.value = err instanceof EditorValidationError
      ? err.serverMessage
      : err instanceof Error ? err.message : String(err)
  } finally {
    saving.value = false
  }
}

async function remove() {
  if (!form.value || form.value.isNew) return
  const id = form.value.id
  if (!(await confirmDelete('table', id, undefined, 'If it ships with the game it will reset to its built-in default; a custom one is removed.'))) return
  resetStatus()
  try {
    await deleteEditorTable(id)
    await reload()
    const stillThere = tables.value.some((t) => t.id === id)
    statusNote.value = stillThere
      ? 'Your edits were removed — this table ships with the game, so its default is back.'
      : 'Table deleted.'
    if (stillThere) selectTable(id)
    else { form.value = null; selectedId.value = '' }
  } catch (err) {
    saveError.value = err instanceof Error ? err.message : String(err)
  }
}
</script>

<style scoped>
.table-editor__scroll { flex: 1; min-height: 0; }

.table-editor__grid {
  display: grid;
  /* Identity and Rows share the top row. Rows takes the wider right column and
     spans the full height so a long row list has room; Identity and Validation
     stack in the narrower left column. */
  grid-template-columns: minmax(260px, 0.8fr) minmax(360px, 1.6fr);
  grid-template-areas:
    "identity   rows"
    "validation rows";
  gap: var(--ed-gap);
  align-items: start;
}

.table-editor__identity { grid-area: identity; }
.table-editor__rows-card { grid-area: rows; }
.table-editor__validation { grid-area: validation; }

.table-editor__row {
  display: flex;
  gap: 8px;
  align-items: center;
  /* One line: range + outcome kind + the outcome control all inline. Rows get
     the wider right column, so there is room. */
  flex-wrap: nowrap;
}

/* The outcome-kind select and the list select share the leftover width. basis 0
   (not auto) so they never inherit editor-controls.css's width:100%. */
.table-editor__row select { flex: 1 1 0; min-width: 0; }
/* The filterable list select is a component (root .fsel); size it the same way
   as the plain selects so the row still lays out on one line. */
.table-editor__list-select { flex: 1 1 0; min-width: 0; }

.table-editor__range { display: flex; align-items: center; gap: 4px; flex: 0 0 auto; }
/* Fixed 64px. flex-basis 64px overrides the shared input[type=number] width:100%
   that would otherwise blow these up to fill the row. */
.table-editor__num { flex: 0 0 64px; width: 64px; }
/* Bordered "X box", mirroring the item editor's Proc Effects remove button:
   a dim outlined box that turns danger-red on hover. */
.table-editor__row-del {
  flex: 0 0 auto;
  padding: 4px 7px;
  font-size: 0.78rem;
  line-height: 1;
  color: var(--ed-text-dim);
  background: none;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}
.table-editor__row-del:hover {
  color: var(--ed-danger);
  border-color: rgba(240, 132, 108, 0.4);
}

.table-editor__resources { display: flex; gap: 10px; flex: 1 1 auto; min-width: 0; }
.table-editor__resources label { display: flex; align-items: center; gap: 4px; font-size: 0.82rem; }

.table-editor__nothing { color: var(--ed-text-dim, #9a8f7d); font-style: italic; font-size: 0.85rem; }

.table-editor__note {
  margin: 10px 0 0;
  color: var(--ed-text-dim, #9a8f7d);
  font-size: 0.85rem;
  line-height: 1.5;
}

.table-editor__status-note {
  display: block;
  margin-top: 8px;
  color: var(--ed-text-dim, #9a8f7d);
  font-size: 0.85rem;
}

.table-editor__empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--ed-text-dim, #9a8f7d);
}
</style>
