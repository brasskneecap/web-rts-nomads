<template>
  <div class="unit-editor">
    <aside class="unit-editor__list">
      <button type="button" class="unit-editor__new" :disabled="busy" @click="newUnit">+ New Unit</button>
      <p v-if="loadError" class="unit-editor__error">{{ loadError }}</p>
      <ul>
        <li v-for="u in units" :key="u.type">
          <button
            type="button"
            :class="{ 'is-selected': u.type === selectedType }"
            @click="selectUnit(u)"
          >
            {{ u.type }}
          </button>
        </li>
      </ul>
    </aside>

    <section class="unit-editor__form">
      <!-- Identity -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('identity') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('identity')">Identity</button>
        <div v-if="openSections.has('identity')" class="unit-editor__section-body">
          <label>Type <input v-model="form.type" :disabled="selectedType !== null" /></label>
          <label>Name <input v-model="form.name" /></label>
          <label>
            Faction
            <select v-model="form.faction">
              <option v-for="f in FACTIONS" :key="f" :value="f">{{ f }}</option>
            </select>
          </label>
          <label>Archetype <input v-model="form.archetype" /></label>
          <label>Train Label <input v-model="form.trainLabel" /></label>
        </div>
      </section>

      <!-- Stats -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('stats') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('stats')">Stats</button>
        <div v-if="openSections.has('stats')" class="unit-editor__section-body">
          <label>HP <input type="number" v-model.number="form.hp" /></label>
          <label>Armor <input type="number" v-model.number="form.armor" /></label>
          <label>Damage <input type="number" v-model.number="form.damage" /></label>
          <label>Attack Range <input type="number" v-model.number="form.attackRange" /></label>
          <label>Attack Speed <input type="number" v-model.number="form.attackSpeed" /></label>
          <label>Splash Radius <input type="number" v-model.number="form.splashRadius" /></label>
          <label>Move Speed <input type="number" v-model.number="form.moveSpeed" /></label>
        </div>
      </section>

      <!-- Cost -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('cost') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('cost')">Cost</button>
        <div v-if="openSections.has('cost')" class="unit-editor__section-body">
          <div class="unit-editor__map">
            <span class="unit-editor__map-label">Resource Cost</span>
            <div v-for="(row, idx) in resourceCostRows" :key="idx" class="unit-editor__map-row">
              <input v-model="row.key" placeholder="resource key" />
              <input type="number" v-model.number="row.value" />
              <button type="button" @click="removeResourceCostRow(idx)">Remove</button>
            </div>
            <button type="button" @click="addResourceCostRow">+ Add resource cost</button>
          </div>
          <label>Meat Cost <input type="number" v-model.number="form.meatCost" /></label>
          <label>Spawn Seconds <input type="number" v-model.number="form.spawnSeconds" /></label>
        </div>
      </section>

      <!-- Combat -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('combat') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('combat')">Combat</button>
        <div v-if="openSections.has('combat')" class="unit-editor__section-body">
          <label>Combat Profile <input v-model="form.combatProfile" /></label>
          <label>Attack Type <input v-model="form.attackType" /></label>
          <label>Damage Type <input v-model="form.damageType" /></label>
          <label>
            Targetable Types (comma-separated)
            <input
              :value="(form.targetableTypes ?? []).join(',')"
              @input="updateStringList('targetableTypes', ($event.target as HTMLInputElement).value)"
            />
          </label>
          <label>Projectile <input v-model="form.projectile" /></label>
          <label>Projectile Scale <input type="number" v-model.number="form.projectileScale" /></label>
        </div>
      </section>

      <!-- Resources -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('resources') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('resources')">Resources</button>
        <div v-if="openSections.has('resources')" class="unit-editor__section-body">
          <label>Gold Gather Amount <input type="number" v-model.number="form.goldGatherAmount" /></label>
          <label>Wood Gather Amount <input type="number" v-model.number="form.woodGatherAmount" /></label>
        </div>
      </section>

      <!-- Mana -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('mana') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('mana')">Mana</button>
        <div v-if="openSections.has('mana')" class="unit-editor__section-body">
          <label>Max Mana <input type="number" v-model.number="form.maxMana" /></label>
          <label>Mana Regen Rate <input type="number" v-model.number="form.manaRegenRate" /></label>
          <div class="unit-editor__channel-loop">
            <span class="unit-editor__map-label">Channel Loop <span class="unit-editor__hint">(leave both blank to unset)</span></span>
            <label>
              Start
              <input
                type="number"
                :value="channelLoopStart ?? ''"
                @input="channelLoopStart = ($event.target as HTMLInputElement).value === '' ? undefined : Number(($event.target as HTMLInputElement).value)"
              />
            </label>
            <label>
              End
              <input
                type="number"
                :value="channelLoopEnd ?? ''"
                @input="channelLoopEnd = ($event.target as HTMLInputElement).value === '' ? undefined : Number(($event.target as HTMLInputElement).value)"
              />
            </label>
          </div>
        </div>
      </section>

      <!-- Vision -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('vision') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('vision')">Vision</button>
        <div v-if="openSections.has('vision')" class="unit-editor__section-body">
          <label>Vision Range <input type="number" v-model.number="form.visionRange" /></label>
          <label class="unit-editor__checkbox-label">
            <input type="checkbox" v-model="form.flyer" /> Flyer
          </label>
        </div>
      </section>

      <!-- Abilities -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('abilities') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('abilities')">Abilities</button>
        <div v-if="openSections.has('abilities')" class="unit-editor__section-body">
          <label>
            Abilities (comma-separated)
            <input
              :value="(form.abilities ?? []).join(',')"
              @input="updateStringList('abilities', ($event.target as HTMLInputElement).value)"
            />
          </label>
          <label>
            Capabilities (comma-separated)
            <input
              :value="(form.capabilities ?? []).join(',')"
              @input="updateStringList('capabilities', ($event.target as HTMLInputElement).value)"
            />
          </label>
        </div>
      </section>

      <!-- Gating -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('gating') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('gating')">Gating</button>
        <div v-if="openSections.has('gating')" class="unit-editor__section-body">
          <label>
            Requires Buildings (comma-separated)
            <input
              :value="(form.requiresBuildings ?? []).join(',')"
              @input="updateStringList('requiresBuildings', ($event.target as HTMLInputElement).value)"
            />
          </label>
          <div class="unit-editor__map">
            <span class="unit-editor__map-label">Path Chances</span>
            <div v-for="(row, idx) in pathChancesRows" :key="idx" class="unit-editor__map-row">
              <input v-model="row.key" placeholder="path key" />
              <input type="number" v-model.number="row.value" />
              <button type="button" @click="removePathChanceRow(idx)">Remove</button>
            </div>
            <button type="button" @click="addPathChanceRow">+ Add path chance</button>
          </div>
        </div>
      </section>

      <!-- Rewards -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('rewards') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('rewards')">Rewards</button>
        <div v-if="openSections.has('rewards')" class="unit-editor__section-body">
          <label>Dominion Point Drop Chance <input type="number" v-model.number="form.dominionPointDropChance" /></label>
          <label>Dominion Point Amount <input type="number" v-model.number="form.dominionPointAmount" /></label>
          <label>Spawn Exp <input type="number" v-model.number="form.spawnExp" /></label>
          <label>Experience <input type="number" v-model.number="form.experience" /></label>
        </div>
      </section>

      <!-- Flags -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('flags') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('flags')">Flags</button>
        <div v-if="openSections.has('flags')" class="unit-editor__section-body">
          <label class="unit-editor__checkbox-label">
            <input type="checkbox" v-model="form.nonCombat" /> Non-combat
          </label>
        </div>
      </section>

      <p v-if="saveError" class="unit-editor__error">{{ saveError }}</p>
      <div class="unit-editor__actions">
        <button type="button" :disabled="busy || !form.type || !form.faction" @click="save">Save</button>
        <button type="button" :disabled="busy || selectedType === null" @click="removeUnit">Delete</button>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import {
  createBlankForm, formFromDef, saveRequestFromForm,
  type AuthoredUnitDef, type UnitEditorForm,
} from '@/game/units/unitEditorForm'
import {
  fetchAuthoredUnitDefs, saveEditorUnit, deleteEditorUnit, EditorValidationError,
} from '@/game/units/unitEditorApi'

const units = ref<AuthoredUnitDef[]>([])
const form = ref<UnitEditorForm>(createBlankForm())
const selectedType = ref<string | null>(null)
const saveError = ref('')
const loadError = ref('')
const busy = ref(false)

const FACTIONS = ['human', 'raider', 'wildborne', 'witherborne'] as const

// Collapsible sections — Identity starts open so a freshly loaded panel isn't
// entirely collapsed; every other section starts closed like ItemEditorPanel's
// accordion, but as a Set so multiple sections can be open at once.
const openSections = reactive(new Set<string>(['identity']))
function toggleSection(key: string) {
  if (openSections.has(key)) openSections.delete(key)
  else openSections.add(key)
}

// resourceCost / pathChances are string->number maps on the form. Rows are
// kept as local {key,value} arrays (so add/remove/rename is simple array
// editing) and mirrored back into form.<field> via watch — saveRequestFromForm
// reads form.<field> directly, so the mirrored map is what actually saves.
interface MapRow { key: string; value: number }
const resourceCostRows = ref<MapRow[]>([])
const pathChancesRows = ref<MapRow[]>([])

function rowsFromMap(map?: Record<string, number>): MapRow[] {
  return Object.entries(map ?? {}).map(([key, value]) => ({ key, value }))
}
function mapFromRows(rows: MapRow[]): Record<string, number> {
  const out: Record<string, number> = {}
  for (const row of rows) if (row.key) out[row.key] = row.value
  return out
}
function addResourceCostRow() { resourceCostRows.value.push({ key: '', value: 0 }) }
function removeResourceCostRow(idx: number) { resourceCostRows.value.splice(idx, 1) }
function addPathChanceRow() { pathChancesRows.value.push({ key: '', value: 0 }) }
function removePathChanceRow(idx: number) { pathChancesRows.value.splice(idx, 1) }

watch(resourceCostRows, (rows) => { form.value.resourceCost = mapFromRows(rows) }, { deep: true })
watch(pathChancesRows, (rows) => { form.value.pathChances = mapFromRows(rows) }, { deep: true })

// channelLoop is a nested {start,end} object that must stay entirely
// undefined when both fields are unset. Tracked as two local optional refs
// and merged back into form.channelLoop via watch.
const channelLoopStart = ref<number | undefined>(undefined)
const channelLoopEnd = ref<number | undefined>(undefined)
watch([channelLoopStart, channelLoopEnd], ([s, e]) => {
  form.value.channelLoop = (s === undefined && e === undefined) ? undefined : { start: s ?? 0, end: e ?? 0 }
})

// Generic comma-separated string[] binding, shared by abilities/capabilities/
// requiresBuildings/targetableTypes.
type StringListField = 'abilities' | 'capabilities' | 'requiresBuildings' | 'targetableTypes'
function updateStringList(field: StringListField, raw: string) {
  form.value[field] = raw.split(',').map((s) => s.trim()).filter(Boolean)
}

async function reload() {
  try {
    units.value = await fetchAuthoredUnitDefs()
    loadError.value = ''
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e)
  }
}

function selectUnit(def: AuthoredUnitDef) {
  form.value = formFromDef(def)
  selectedType.value = def.type
  saveError.value = ''
  resourceCostRows.value = rowsFromMap(def.resourceCost)
  pathChancesRows.value = rowsFromMap(def.pathChances)
  channelLoopStart.value = def.channelLoop?.start
  channelLoopEnd.value = def.channelLoop?.end
}

function newUnit() {
  form.value = createBlankForm()
  selectedType.value = null
  saveError.value = ''
  resourceCostRows.value = []
  pathChancesRows.value = []
  channelLoopStart.value = undefined
  channelLoopEnd.value = undefined
}

async function save() {
  saveError.value = ''
  busy.value = true
  try {
    await saveEditorUnit(saveRequestFromForm(form.value))
    await reload()
    selectedType.value = form.value.type
  } catch (e) {
    saveError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function removeUnit() {
  if (!selectedType.value) return
  busy.value = true
  try {
    await deleteEditorUnit(selectedType.value)
    await reload()
    newUnit()
  } finally {
    busy.value = false
  }
}

onMounted(reload)
</script>

<style scoped>
.unit-editor {
  display: flex;
  width: 100%;
  height: 100%;
  min-height: 0;
  min-width: 0;
  gap: 12px;
  padding: 16px;
  box-sizing: border-box;
}

.unit-editor__list {
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

.unit-editor__list ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.unit-editor__list button {
  width: 100%;
  border: 1px solid transparent;
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.6);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
  text-align: left;
}

.unit-editor__list button.is-selected {
  border-color: rgba(215, 187, 132, 0.6);
  background: rgba(30, 41, 59, 0.9);
}

.unit-editor__new {
  font-weight: 700;
}

.unit-editor__form {
  flex: 1;
  min-width: 0;
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

.unit-editor__section {
  border: 1px solid rgba(148, 163, 184, 0.18);
  border-radius: 12px;
  background: rgba(8, 14, 24, 0.55);
  overflow: clip;
  flex: 0 0 auto;
}

.unit-editor__section--open {
  background: rgba(8, 14, 24, 0.72);
}

.unit-editor__section-summary {
  width: 100%;
  border: 0;
  padding: 10px 12px;
  font-size: 0.78rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #f8fafc;
  text-align: left;
  background: linear-gradient(180deg, rgba(25, 35, 52, 0.92), rgba(14, 22, 36, 0.94));
}

.unit-editor__section-summary::after {
  content: '+';
  float: right;
  color: #d7bb84;
}

.unit-editor__section--open .unit-editor__section-summary::after {
  content: '-';
}

.unit-editor__section-body {
  display: grid;
  gap: 8px;
  padding: 10px;
}

.unit-editor__section-body label {
  display: grid;
  gap: 4px;
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.75rem;
}

.unit-editor__section-body input,
.unit-editor__section-body select {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
}

.unit-editor__checkbox-label {
  flex-direction: row !important;
  align-items: center;
  gap: 6px !important;
}

.unit-editor__map {
  display: grid;
  gap: 6px;
}

.unit-editor__map-label {
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.75rem;
  font-weight: 700;
}

.unit-editor__hint {
  font-weight: 400;
  opacity: 0.65;
  text-transform: none;
  letter-spacing: 0;
}

.unit-editor__map-row {
  display: flex;
  gap: 6px;
  align-items: center;
}

.unit-editor__map-row input {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
  flex: 1;
}

.unit-editor__channel-loop {
  display: grid;
  gap: 6px;
}

.unit-editor__actions {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: auto;
  padding-top: 8px;
}

.unit-editor__error {
  color: #fca5a5;
  font-size: 0.78rem;
}
</style>
