<template>
  <!-- The map-editor landing state. Mirrors the item/unit/ability editors: a
       searchable sidebar list on the left plus a "New" action, and a center
       surface that stays empty until the author picks a map or starts a new
       one. Nothing is loaded into the canvas until the author acts — the
       parent only switches to the editing view on `open`/`create`. -->
  <EditorShell class="map-landing" theme="forge">
    <template #sidebar>
      <EditorSidebar
        title="Maps"
        new-label="New Map"
        :groups="sidebarGroups"
        :selected-id="selectedId"
        :search="search"
        search-placeholder="Search maps…"
        :empty-text="loading ? 'Loading maps…' : 'No maps match.'"
        @update:search="search = $event"
        @select="onSelect"
        @new="startNew"
        @duplicate="startFromTemplate"
      />
    </template>

    <template #main>
      <!-- New-map form. Unmistakably a NEW map: its own heading, an editable
           name, and an explicit template choice so the author knows they're
           creating rather than editing whatever they last had open. -->
      <section v-if="mode === 'new'" class="map-landing__panel">
        <div class="map-landing__eyebrow">Create</div>
        <h2 class="map-landing__title">New Map</h2>
        <p class="map-landing__lede">
          Start from a blank grid, or copy an existing map as a starting point.
          This creates a brand-new map — the source map is never modified.
        </p>

        <div class="map-landing__form">
          <label class="map-landing__field">
            <span class="map-landing__label">Map Name</span>
            <input
              ref="nameInput"
              v-model.trim="newName"
              type="text"
              placeholder="e.g. Ashen Valley"
              @keydown.enter="submitNew"
            />
          </label>

          <label class="map-landing__field">
            <span class="map-landing__label">Start From</span>
            <select v-model="templateId">
              <option value="">Blank Map</option>
              <optgroup v-if="templateGroups.campaign.length" label="Campaign Maps">
                <option v-for="m in templateGroups.campaign" :key="m.id" :value="m.id">{{ m.name }}</option>
              </optgroup>
              <optgroup v-if="templateGroups.custom.length" label="Custom Maps">
                <option v-for="m in templateGroups.custom" :key="m.id" :value="m.id">{{ m.name }}</option>
              </optgroup>
            </select>
            <span class="map-landing__hint">
              {{ templateId
                ? 'Copies terrain, tiles, obstacles, buildings, units and zones from the chosen map.'
                : 'An empty grid at the size you pick below.' }}
            </span>
          </label>

          <label v-if="!templateId" class="map-landing__field">
            <span class="map-landing__label">Size</span>
            <select v-model="presetLabel">
              <option v-for="p in PRESETS" :key="p.label" :value="p.label">
                {{ p.label }} — {{ p.cols }} × {{ p.rows }}
              </option>
            </select>
          </label>
        </div>

        <div class="map-landing__actions">
          <UiButton variant="active" :disabled="!canCreate" @click="submitNew">
            {{ busy ? 'Creating…' : 'Create Map' }}
          </UiButton>
          <UiButton v-if="maps.length" variant="secondary" @click="cancelNew">Cancel</UiButton>
        </div>
      </section>

      <!-- Detail card for a selected existing map. -->
      <section v-else-if="mode === 'detail' && selectedMap" class="map-landing__panel">
        <div class="map-landing__eyebrow">
          {{ selectedMap.campaignId ? 'Campaign Map' : 'Custom Map' }}
        </div>
        <h2 class="map-landing__title">{{ selectedMap.name }}</h2>
        <p v-if="selectedMap.description" class="map-landing__lede">{{ selectedMap.description }}</p>

        <dl class="map-landing__meta">
          <div><dt>Grid</dt><dd>{{ selectedMap.gridCols }} × {{ selectedMap.gridRows }}</dd></div>
          <div><dt>Spawn Points</dt><dd>{{ selectedMap.spawnPointCount }}</dd></div>
          <div v-if="selectedMap.campaignId"><dt>Campaign</dt><dd>{{ selectedMap.campaignId }}</dd></div>
          <div v-if="selectedMap.version"><dt>Version</dt><dd>{{ selectedMap.version }}</dd></div>
        </dl>

        <div class="map-landing__actions">
          <UiButton variant="active" :disabled="busy" @click="emit('open', selectedMap.id)">
            {{ busy ? 'Opening…' : 'Open in Editor' }}
          </UiButton>
          <UiButton variant="secondary" @click="startFromTemplate(selectedMap.id)">Use as Template</UiButton>
        </div>
      </section>

      <!-- First-run empty state — matches the other editors' "select or create". -->
      <section v-else class="map-landing__panel map-landing__panel--empty">
        <div class="map-landing__empty-mark">◈</div>
        <h2 class="map-landing__title">Map Editor</h2>
        <p class="map-landing__lede">Select a map from the list to edit it, or create a new one to get started.</p>
        <div class="map-landing__actions">
          <UiButton variant="active" @click="startNew">New Map</UiButton>
        </div>
      </section>

      <div v-if="error" class="map-landing__error">
        <span>{{ error }} — couldn’t reach the map server. You can still create a new blank map below.</span>
        <UiButton size="sm" variant="secondary" :disabled="loading" @click="emit('retry')">
          {{ loading ? 'Retrying…' : 'Retry' }}
        </UiButton>
      </div>
    </template>
  </EditorShell>
</template>

<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import EditorShell from '@/components/editor/EditorShell.vue'
import EditorSidebar, { type SidebarGroup } from '@/components/editor/EditorSidebar.vue'
import UiButton from '@/components/ui/UiButton.vue'
import { MAP_EDITOR_PRESETS } from '@/game/maps/mapConfig'
import type { MapCatalogEntry } from '@/game/network/protocol'

const PRESETS = MAP_EDITOR_PRESETS

const props = defineProps<{
  maps: MapCatalogEntry[]
  /** Catalog list is still loading. */
  loading?: boolean
  /** An open/create round-trip is in flight. */
  busy?: boolean
  error?: string
}>()

const emit = defineEmits<{
  open: [string]
  create: [{ name: string; templateId: string | null; cols: number; rows: number }]
  retry: []
}>()

const search = ref('')
const selectedId = ref('')
const mode = ref<'empty' | 'detail' | 'new'>('empty')
const nameInput = ref<HTMLInputElement | null>(null)

// New-map form state.
const newName = ref('')
const templateId = ref('')
const presetLabel = ref<string>(PRESETS[1]?.label ?? PRESETS[0].label)

const matches = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return props.maps
  return props.maps.filter(
    (m) => m.name.toLowerCase().includes(q) || m.id.toLowerCase().includes(q),
  )
})

// Campaign maps and custom maps are grouped separately so the list reads the
// same way the lobby's map picker does.
const sidebarGroups = computed<SidebarGroup[]>(() => {
  const campaign = matches.value.filter((m) => m.campaignId)
  const custom = matches.value.filter((m) => !m.campaignId)
  const byName = (a: MapCatalogEntry, b: MapCatalogEntry) => a.name.localeCompare(b.name)
  const groups: SidebarGroup[] = []
  if (campaign.length) {
    groups.push({
      label: 'Campaign',
      entries: [...campaign].sort(byName).map((m) => ({ id: m.id, name: m.name })),
    })
  }
  if (custom.length) {
    groups.push({
      label: 'Custom',
      entries: [...custom].sort(byName).map((m) => ({ id: m.id, name: m.name })),
    })
  }
  return groups
})

// Same split, for the "Start From" template dropdown.
const templateGroups = computed(() => ({
  campaign: props.maps.filter((m) => m.campaignId),
  custom: props.maps.filter((m) => !m.campaignId),
}))

const selectedMap = computed(() => props.maps.find((m) => m.id === selectedId.value) ?? null)

const canCreate = computed(() => !props.busy && newName.value.trim().length > 0)

function onSelect(id: string) {
  selectedId.value = id
  mode.value = 'detail'
}

function startNew() {
  selectedId.value = ''
  templateId.value = ''
  newName.value = ''
  mode.value = 'new'
  void nextTick(() => nameInput.value?.focus())
}

// "Use as template" from a row's duplicate action or the detail card. Seeds the
// new-map form with that map as the source and a "<name> Copy" default name.
function startFromTemplate(id: string) {
  const src = props.maps.find((m) => m.id === id)
  templateId.value = id
  newName.value = src ? `${src.name} Copy` : ''
  mode.value = 'new'
  void nextTick(() => nameInput.value?.focus())
}

function cancelNew() {
  mode.value = selectedMap.value ? 'detail' : 'empty'
}

function submitNew() {
  if (!canCreate.value) return
  const preset = PRESETS.find((p) => p.label === presetLabel.value) ?? PRESETS[0]
  emit('create', {
    name: newName.value.trim(),
    templateId: templateId.value || null,
    cols: preset.cols,
    rows: preset.rows,
  })
}
</script>

<style scoped>
.map-landing {
  width: 100%;
  height: 100%;
}

.map-landing__panel {
  max-width: 640px;
  padding: 22px 26px;
}

.map-landing__panel--empty {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
  padding-top: 12vh;
}

.map-landing__empty-mark {
  font-size: 2.4rem;
  color: var(--ed-brass-dim);
  line-height: 1;
}

.map-landing__eyebrow {
  font-size: 0.66rem;
  font-weight: 700;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--ed-brass-dim);
  margin-bottom: 4px;
}

.map-landing__title {
  font-family: var(--font-title);
  font-size: 1.35rem;
  color: var(--ed-brass);
  margin: 0 0 8px;
}

.map-landing__lede {
  font-size: 0.86rem;
  line-height: 1.5;
  color: var(--ed-text-dim);
  margin: 0 0 18px;
  max-width: 52ch;
}

.map-landing__form {
  display: flex;
  flex-direction: column;
  gap: 14px;
  margin-bottom: 20px;
}

.map-landing__field {
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.map-landing__label {
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--ed-text-dim);
}

.map-landing__hint {
  font-size: 0.74rem;
  color: var(--ed-text-dim);
  opacity: 0.85;
}

.map-landing__actions {
  display: flex;
  gap: 10px;
  align-items: center;
}

.map-landing__meta {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 10px 20px;
  margin: 0 0 20px;
}

.map-landing__meta div {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.map-landing__meta dt {
  font-size: 0.66rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--ed-text-dim);
}

.map-landing__meta dd {
  margin: 0;
  font-size: 0.95rem;
  color: var(--ed-text);
}

.map-landing__error {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  margin: 14px 26px;
  font-size: 0.8rem;
  color: var(--ed-danger);
}
</style>
