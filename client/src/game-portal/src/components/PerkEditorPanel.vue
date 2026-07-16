<template>
  <div class="perk-editor">
    <aside class="perk-editor__list">
      <button type="button" class="perk-editor__new" :disabled="busy" @click="newPerk">+ New Perk</button>
      <p v-if="loadError" class="perk-editor__error">{{ loadError }}</p>
      <ul>
        <li v-for="p in perks" :key="p.id">
          <button
            type="button"
            data-test="perk-row"
            :class="{ 'is-selected': p.id === selectedId }"
            @click="selectPerk(p)"
          >
            {{ p.id }} <span v-if="p.displayName">— {{ p.displayName }}</span>
            <span v-if="!p.wired" class="perk-editor__badge perk-editor__badge--inert">inert</span>
          </button>
        </li>
      </ul>
    </aside>

    <section class="perk-editor__form">
      <!-- Identity -->
      <section class="perk-editor__section">
        <h3 class="perk-editor__section-title">Identity</h3>
        <label>Id <input v-model="form.id" :disabled="selectedId !== null" /></label>
        <label>Display Name <input v-model="form.displayName" /></label>
        <label>Description <textarea v-model="form.description" rows="2" /></label>
        <label>Icon <input v-model="form.icon" /></label>
        <label class="perk-editor__checkbox-label">
          <input type="checkbox" :checked="!!form.wired" disabled />
          Wired <span class="perk-editor__hint">(server-derived, read-only)</span>
        </label>
      </section>

      <!-- Eligibility -->
      <section class="perk-editor__section">
        <h3 class="perk-editor__section-title">Eligibility</h3>
        <label>
          Unit Type
          <input v-model="form.unitType" list="perk-editor-unit-types" placeholder="(any)" />
        </label>
        <label>Path <input v-model="form.path" placeholder="(any)" /></label>
        <label>
          Rank
          <select v-model="form.rank">
            <option value="">(any)</option>
            <option value="bronze">bronze</option>
            <option value="silver">silver</option>
            <option value="gold">gold</option>
          </select>
        </label>
        <label>Requires Perk <input v-model="form.requiresPerk" list="perk-editor-perk-ids" placeholder="(none)" /></label>
      </section>

      <!-- Tooltip -->
      <section class="perk-editor__section">
        <h3 class="perk-editor__section-title">Tooltip</h3>
        <label>Tooltip Template <textarea v-model="form.tooltipTemplate" rows="3" /></label>
      </section>

      <!-- Config -->
      <section class="perk-editor__section">
        <h3 class="perk-editor__section-title">Config</h3>
        <p v-if="configRows.length === 0" class="perk-editor__hint-line">No config values.</p>
        <div v-for="(row, idx) in configRows" :key="idx" class="perk-editor__map-row">
          <input v-model="row.key" placeholder="key" :aria-label="`Config ${idx + 1} key`" />
          <input v-model.number="row.value" type="number" :aria-label="`Config ${idx + 1} value`" />
          <button type="button" class="perk-editor__row-del" title="Remove" @click="removeConfigRow(idx)">✕</button>
        </div>
        <button type="button" class="perk-editor__row-add" @click="addConfigRow">+ Add Config Value</button>
      </section>

      <!-- Config By Rank -->
      <section class="perk-editor__section">
        <h3 class="perk-editor__section-title">Config By Rank</h3>
        <p class="perk-editor__hint-line">
          JSON object of rank → (key → number). Leave blank for none.
        </p>
        <textarea
          class="perk-editor__json"
          rows="5"
          :value="configByRankText"
          @input="onConfigByRankInput(($event.target as HTMLTextAreaElement).value)"
        />
        <p v-if="configByRankError" class="perk-editor__error">{{ configByRankError }}</p>
      </section>

      <!-- Effect -->
      <section class="perk-editor__section">
        <h3 class="perk-editor__section-title">Effect</h3>
        <label>Name <input v-model="effectDraft.name" /></label>
        <label>
          Target
          <select v-model="effectDraft.target">
            <option value="">(none)</option>
            <option value="self">self</option>
            <option value="enemies">enemies</option>
          </select>
        </label>
        <label>Size Scale <input type="number" v-model.number="effectDraft.sizeScale" /></label>
        <label>Duration Seconds <input type="number" v-model.number="effectDraft.durationSeconds" /></label>
        <label>Variant <input v-model="effectDraft.variant" /></label>
      </section>

      <!-- Grants Abilities -->
      <section class="perk-editor__section">
        <h3 class="perk-editor__section-title">Grants Abilities</h3>
        <label>
          Ability Ids (comma-separated)
          <input
            :value="(form.grantsAbilities ?? []).join(',')"
            @input="updateGrantsAbilities(($event.target as HTMLInputElement).value)"
          />
        </label>
      </section>

      <p v-if="saveError" class="perk-editor__error">{{ saveError }}</p>
      <p v-if="statusMessage" class="perk-editor__status">{{ statusMessage }}</p>
      <div class="perk-editor__actions">
        <button type="button" :disabled="busy || !form.id" @click="save">Save</button>
        <button type="button" :disabled="busy || selectedId === null" @click="removePerk">Delete / Reset</button>
      </div>
    </section>

    <datalist id="perk-editor-unit-types">
      <option v-for="u in unitTypeIds" :key="u" :value="u" />
    </datalist>
    <datalist id="perk-editor-perk-ids">
      <option v-for="p in perks" :key="p.id" :value="p.id" />
    </datalist>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import {
  createBlankForm, formFromDef, saveRequestFromForm,
  type AuthoredPerkDef, type PerkEditorForm,
} from '@/game/perks/perkEditorForm'
import {
  fetchAuthoredPerkDefs, saveEditorPerk, deleteEditorPerk, EditorValidationError,
} from '@/game/perks/perkEditorApi'
import { fetchAuthoredUnitDefs } from '@/game/units/unitEditorApi'

const perks = ref<AuthoredPerkDef[]>([])
const form = ref<PerkEditorForm>(createBlankForm())
const selectedId = ref<string | null>(null)
const saveError = ref('')
const loadError = ref('')
const statusMessage = ref('')
const busy = ref(false)
const unitTypeIds = ref<string[]>([])

// Config: add/remove key→number rows, kept in sync with form.value.config via
// a deep watch — mirrors UnitTypeEditorPanel's resourceCostRows/mapFromRows idiom.
interface MapRow { key: string; value: number }
const configRows = ref<MapRow[]>([])

function rowsFromMap(map?: Record<string, number>): MapRow[] {
  return Object.entries(map ?? {}).map(([key, value]) => ({ key, value }))
}
function mapFromRows(rows: MapRow[]): Record<string, number> {
  const out: Record<string, number> = {}
  for (const row of rows) if (row.key) out[row.key] = row.value
  return out
}
function addConfigRow() { configRows.value.push({ key: '', value: 0 }) }
function removeConfigRow(idx: number) { configRows.value.splice(idx, 1) }
watch(configRows, (rows) => { form.value.config = mapFromRows(rows) }, { deep: true })

// configByRank: a pragmatic JSON-textarea control (rank → key → number is a
// nested shape not worth a bespoke rows-of-rows UI for). Invalid JSON is held
// in the textarea and flagged inline without touching form.value.configByRank,
// so a typo never silently drops previously-valid data.
const configByRankText = ref('')
const configByRankError = ref('')

function syncConfigByRankText(def?: Record<string, Record<string, number>>) {
  configByRankText.value = def && Object.keys(def).length ? JSON.stringify(def, null, 2) : ''
  configByRankError.value = ''
}

function onConfigByRankInput(value: string) {
  configByRankText.value = value
  const trimmed = value.trim()
  if (!trimmed) {
    form.value.configByRank = undefined
    configByRankError.value = ''
    return
  }
  try {
    form.value.configByRank = JSON.parse(trimmed)
    configByRankError.value = ''
  } catch {
    configByRankError.value = 'Invalid JSON — not saved until fixed.'
  }
}

// Effect: only ever sent as a full object when a name is set (blank name =
// no effect), per saveRequestFromForm's undefined-field-drop contract.
interface EffectDraft { name: string; target: string; sizeScale?: number; durationSeconds?: number; variant: string }
const effectDraft = reactive<EffectDraft>({ name: '', target: '', sizeScale: undefined, durationSeconds: undefined, variant: '' })

function syncEffectDraft(effect?: AuthoredPerkDef['effect']) {
  effectDraft.name = effect?.name ?? ''
  effectDraft.target = effect?.target ?? ''
  effectDraft.sizeScale = effect?.sizeScale
  effectDraft.durationSeconds = effect?.durationSeconds
  effectDraft.variant = effect?.variant ?? ''
}

watch(effectDraft, (draft) => {
  if (!draft.name.trim()) {
    form.value.effect = undefined
    return
  }
  form.value.effect = {
    name: draft.name,
    ...(draft.target ? { target: draft.target } : {}),
    ...(typeof draft.sizeScale === 'number' && !Number.isNaN(draft.sizeScale) ? { sizeScale: draft.sizeScale } : {}),
    ...(typeof draft.durationSeconds === 'number' && !Number.isNaN(draft.durationSeconds) ? { durationSeconds: draft.durationSeconds } : {}),
    ...(draft.variant ? { variant: draft.variant } : {}),
  }
}, { deep: true })

function updateGrantsAbilities(raw: string) {
  const list = raw.split(',').map((s) => s.trim()).filter(Boolean)
  form.value.grantsAbilities = list.length ? list : undefined
}

async function reload() {
  try {
    perks.value = await fetchAuthoredPerkDefs()
    loadError.value = ''
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e)
  }
}

async function loadCatalogs() {
  try {
    const units = await fetchAuthoredUnitDefs()
    unitTypeIds.value = units.map((u) => u.type)
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e)
  }
}

function selectPerk(def: AuthoredPerkDef) {
  form.value = formFromDef(def)
  selectedId.value = def.id
  configRows.value = rowsFromMap(def.config)
  syncConfigByRankText(def.configByRank)
  syncEffectDraft(def.effect)
  saveError.value = ''
  statusMessage.value = ''
}

function newPerk() {
  form.value = createBlankForm()
  selectedId.value = null
  configRows.value = []
  syncConfigByRankText(undefined)
  syncEffectDraft(undefined)
  saveError.value = ''
  statusMessage.value = ''
}

async function save() {
  saveError.value = ''
  statusMessage.value = ''
  busy.value = true
  try {
    await saveEditorPerk(saveRequestFromForm(form.value))
    await reload()
    selectedId.value = form.value.id
    statusMessage.value = 'Saved.'
  } catch (e) {
    saveError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function removePerk() {
  if (!selectedId.value) return
  saveError.value = ''
  statusMessage.value = ''
  busy.value = true
  try {
    const status = await deleteEditorPerk(selectedId.value)
    await reload()
    newPerk()
    statusMessage.value = status === 'deleted' ? 'Deleted.' : 'Reset to default.'
  } catch (e) {
    saveError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

onMounted(() => {
  reload()
  loadCatalogs()
})
</script>

<style scoped>
.perk-editor {
  display: flex;
  width: 100%;
  height: 100%;
  min-height: 0;
  min-width: 0;
  gap: 12px;
  padding: 16px;
  box-sizing: border-box;
}

.perk-editor__list {
  flex: 0 0 220px;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 8px;
  background: rgba(3, 8, 14, 0.86);
  border: 1px solid rgba(148, 163, 184, 0.22);
  border-radius: 16px;
  padding: 12px;
}

.perk-editor__list ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.perk-editor__list button {
  width: 100%;
  border: 1px solid transparent;
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.6);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
  text-align: left;
}

.perk-editor__list button.is-selected {
  border-color: rgba(215, 187, 132, 0.6);
  background: rgba(30, 41, 59, 0.9);
}

.perk-editor__new {
  font-weight: 700;
}

.perk-editor__badge {
  margin-left: 6px;
  border-radius: 999px;
  padding: 1px 7px;
  font-size: 0.64rem;
  font-weight: 700;
  white-space: nowrap;
}

.perk-editor__badge--inert {
  background: rgba(248, 113, 113, 0.18);
  color: #fca5a5;
  border: 1px solid rgba(248, 113, 113, 0.55);
}

.perk-editor__form {
  flex: 1;
  min-width: 0;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 12px;
  background: rgba(3, 8, 14, 0.86);
  border: 1px solid rgba(148, 163, 184, 0.22);
  border-radius: 16px;
  padding: 12px;
}

.perk-editor__section {
  border: 1px solid rgba(148, 163, 184, 0.18);
  border-radius: 12px;
  background: rgba(8, 14, 24, 0.55);
  padding: 10px;
  display: grid;
  gap: 8px;
}

.perk-editor__section-title {
  margin: 0;
  font-size: 0.78rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #d7bb84;
}

.perk-editor__section label {
  display: grid;
  gap: 4px;
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.75rem;
}

.perk-editor__section input,
.perk-editor__section select,
.perk-editor__section textarea {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
  font-family: inherit;
}

.perk-editor__checkbox-label {
  flex-direction: row !important;
  align-items: center;
  gap: 6px !important;
}

.perk-editor__hint {
  font-weight: 400;
  opacity: 0.65;
}

.perk-editor__hint-line {
  margin: 0;
  color: rgba(226, 232, 240, 0.55);
  font-size: 0.72rem;
  font-style: italic;
}

.perk-editor__map-row {
  display: flex;
  align-items: center;
  gap: 6px;
}

.perk-editor__map-row input {
  flex: 1 1 auto;
  min-width: 0;
}

.perk-editor__row-del {
  flex: 0 0 auto;
  border: 1px solid rgba(148, 163, 184, 0.25);
  border-radius: 6px;
  background: rgba(15, 23, 42, 0.6);
  color: #f8fafc;
  padding: 4px 8px;
  font-size: 0.72rem;
}

.perk-editor__row-add {
  align-self: flex-start;
  border: 1px solid rgba(215, 187, 132, 0.5);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.6);
  color: #d7bb84;
  padding: 6px 10px;
  font-size: 0.76rem;
  font-weight: 700;
}

.perk-editor__json {
  resize: vertical;
  font-family: monospace;
}

.perk-editor__actions {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: auto;
  padding-top: 8px;
}

.perk-editor__error {
  color: #fca5a5;
  font-size: 0.78rem;
}

.perk-editor__status {
  color: #86efac;
  font-size: 0.78rem;
}
</style>
