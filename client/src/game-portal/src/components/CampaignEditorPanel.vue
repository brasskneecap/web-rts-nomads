<template>
  <EditorShell class="campaign-editor" theme="forge">
    <template #sidebar>
      <EditorSidebar
        title="Campaigns"
        new-label="New Campaign"
        :groups="sidebarGroups"
        :selected-id="selectedId"
        :search="search"
        search-placeholder="Search campaigns…"
        :empty-text="loading ? 'Loading…' : 'No campaigns match.'"
        @update:search="search = $event"
        @select="selectCampaign"
        @new="newCampaign"
        @duplicate="duplicateCampaign"
      />
    </template>

    <template #main>
      <template v-if="form">
        <EditorHeader
          :title="form.displayName || 'New Campaign'"
          :badge="form.isNew ? 'new' : undefined"
          badge-color="#e7c88a"
          :breadcrumb="`${levels.length} level${levels.length === 1 ? '' : 's'}`"
          :id="form.id"
          :id-editable="form.isNew"
          id-input-id="ce-id"
          :saving="saving"
          :save-disabled="saving || !canSave"
          :saved-label="savedLabel"
          :error="saveError"
          :remove-label="form.isNew ? '' : 'Delete'"
          @update:id="onIdInput"
          @save="save"
          @remove="removeCampaign"
        />

        <GameScrollArea class="campaign-editor__scroll">
          <div class="campaign-editor__body">
            <SectionCard title="Details" :index="1">
              <EditorField label="Display Name" for-id="ce-name">
                <input id="ce-name" v-model.trim="form.displayName" type="text" placeholder="e.g. Forest" />
              </EditorField>
              <EditorField label="Description" for-id="ce-desc">
                <textarea id="ce-desc" v-model.trim="form.description" rows="2" placeholder="One-line campaign blurb"></textarea>
              </EditorField>
              <div class="campaign-editor__pair">
                <EditorField label="Sort Order" hint="(tab order)" for-id="ce-sort">
                  <input id="ce-sort" v-model.number="form.sortOrder" type="number" />
                </EditorField>
                <EditorField label="Locked" hint="(greyed in the strip)">
                  <label class="ed-check">
                    <input type="checkbox" v-model="form.locked" />
                    <span>Coming soon / not playable</span>
                  </label>
                </EditorField>
              </div>
            </SectionCard>

            <SectionCard title="Levels" :index="2">
              <template #head-action>
                <span class="campaign-editor__count">{{ levels.length }}</span>
              </template>

              <p class="campaign-editor__hint">
                Each level is a map assigned to this campaign, in order. Adding a map here writes its
                campaign membership; removing it clears it. A map can belong to one campaign at a time.
              </p>

              <div v-if="levelsLoading" class="campaign-editor__loading">Loading levels…</div>

              <div v-else class="campaign-editor__levels">
                <div v-for="(lvl, idx) in levels" :key="lvl.key" class="campaign-level">
                  <div class="campaign-level__head">
                    <span class="campaign-level__num">{{ idx + 1 }}</span>
                    <span class="campaign-level__map" :title="lvl.mapId">{{ lvl.mapName }}</span>
                    <span class="campaign-level__spacer" />
                    <button type="button" class="campaign-level__btn" :disabled="idx === 0" title="Move up" @click="moveLevel(idx, -1)">↑</button>
                    <button type="button" class="campaign-level__btn" :disabled="idx === levels.length - 1" title="Move down" @click="moveLevel(idx, 1)">↓</button>
                    <button type="button" class="campaign-level__btn campaign-level__btn--danger" title="Remove from campaign" @click="removeLevel(idx)">✕</button>
                  </div>
                </div>

                <p v-if="levels.length === 0" class="campaign-editor__empty">No levels yet — add a map below.</p>

                <div class="campaign-editor__add">
                  <select v-model="addMapId">
                    <option value="">Add a map…</option>
                    <option
                      v-for="m in addableMaps"
                      :key="m.id"
                      :value="m.id"
                      :disabled="isInThisCampaign(m.id)"
                    >
                      {{ m.name }}{{ isInThisCampaign(m.id) ? ' — already added' : (m.campaignId && m.campaignId !== form.id ? ` — in ${m.campaignId}` : '') }}
                    </option>
                  </select>
                  <UiButton size="sm" variant="active" :disabled="!addMapId" @click="addSelectedMap">Add Level</UiButton>
                </div>
              </div>
            </SectionCard>
          </div>
        </GameScrollArea>
      </template>

      <section v-else class="campaign-editor__welcome">
        <h2>Campaigns</h2>
        <p>Select a campaign to edit, or create a new one. A campaign is an ordered set of maps.</p>
        <UiButton variant="active" @click="newCampaign">New Campaign</UiButton>
      </section>
    </template>
  </EditorShell>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import EditorShell from '@/components/editor/EditorShell.vue'
import EditorSidebar, { type SidebarGroup } from '@/components/editor/EditorSidebar.vue'
import EditorHeader from '@/components/editor/EditorHeader.vue'
import SectionCard from '@/components/editor/SectionCard.vue'
import EditorField from '@/components/editor/EditorField.vue'
import UiButton from '@/components/ui/UiButton.vue'
import GameScrollArea from '@/components/ui/GameScrollArea.vue'
import { fetchCampaignCatalog } from '@/services/campaignApi'
import { saveCampaignHeader, deleteCampaign, CampaignSaveError } from '@/services/campaignEditorApi'
import { fetchMapCatalog, fetchMapCatalogFile, saveMapCatalogFile, LevelConflictError } from '@/game/maps/catalog'
import type { Campaign } from '@/types/campaign'
import type { MapCatalogEntry, MapCampaignBlock } from '@/game/network/protocol'

type CampaignForm = {
  id: string
  displayName: string
  description: string
  sortOrder: number
  locked: boolean
  isNew: boolean
}

// One editable level. `block` is the map's campaign block (the authoring shape);
// campaignId/sortOrder are (re)stamped at save from the campaign + row order.
type LevelDraft = {
  key: number
  mapId: string
  mapName: string
  block: MapCampaignBlock
}

const campaigns = ref<Campaign[]>([])
const maps = ref<MapCatalogEntry[]>([])
const loading = ref(false)
const levelsLoading = ref(false)
const saving = ref(false)
const saveError = ref('')
const savedLabel = ref('')
const search = ref('')

const selectedId = ref('')
const form = ref<CampaignForm | null>(null)
const levels = ref<LevelDraft[]>([])
// Maps removed from the campaign this session — cleared on save unless re-added.
const removedMapIds = ref<Set<string>>(new Set())
const addMapId = ref('')

let levelKeySeq = 1

const mapName = (id: string) => maps.value.find((m) => m.id === id)?.name ?? id

const matches = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return campaigns.value
  return campaigns.value.filter(
    (c) => c.displayName.toLowerCase().includes(q) || c.id.toLowerCase().includes(q),
  )
})

const sidebarGroups = computed<SidebarGroup[]>(() => {
  if (matches.value.length === 0) return []
  return [
    {
      label: 'Campaigns',
      entries: [...matches.value]
        .sort((a, b) => (a.sortOrder ?? 0) - (b.sortOrder ?? 0) || a.displayName.localeCompare(b.displayName))
        .map((c) => ({ id: c.id, name: c.displayName })),
    },
  ]
})

const addableMaps = computed(() =>
  [...maps.value].sort((a, b) => a.name.localeCompare(b.name)),
)

function isInThisCampaign(mapId: string): boolean {
  return levels.value.some((l) => l.mapId === mapId)
}

// Client-side gate; the server re-validates. Every level needs a non-empty,
// unique id + a display name (the server rejects a blank level display name).
const canSave = computed(() => {
  if (!form.value) return false
  if (!form.value.id.trim() || !form.value.displayName.trim()) return false
  const ids = new Set<string>()
  for (const lvl of levels.value) {
    const id = lvl.block.levelId.trim()
    if (!id || !lvl.block.displayName.trim()) return false
    if (ids.has(id)) return false
    ids.add(id)
  }
  return true
})

function slugify(name: string): string {
  const base = name.toLowerCase().trim().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')
  return base || 'campaign'
}

async function reload(): Promise<void> {
  loading.value = true
  try {
    const [c, m] = await Promise.all([fetchCampaignCatalog(), fetchMapCatalog()])
    campaigns.value = c
    maps.value = m
  } catch (err) {
    saveError.value = err instanceof Error ? err.message : 'Failed to load campaigns.'
  } finally {
    loading.value = false
  }
}
void reload()

async function selectCampaign(id: string): Promise<void> {
  const camp = campaigns.value.find((c) => c.id === id)
  if (!camp) return
  selectedId.value = id
  saveError.value = ''
  savedLabel.value = ''
  removedMapIds.value = new Set()
  addMapId.value = ''
  form.value = {
    id: camp.id,
    displayName: camp.displayName,
    description: camp.description ?? '',
    sortOrder: camp.sortOrder ?? 0,
    locked: camp.locked ?? false,
    isNew: false,
  }
  // Load each level's authoring block from its map file (source of truth for
  // objectives + prereq), so what we edit is exactly what we save back.
  levels.value = []
  levelsLoading.value = true
  try {
    const drafts: LevelDraft[] = []
    for (const lvl of camp.levels) {
      let block: MapCampaignBlock
      try {
        const file = await fetchMapCatalogFile(lvl.mapId)
        block = file.map.campaign ?? deriveBlock(lvl.mapId, camp.id)
      } catch {
        block = deriveBlock(lvl.mapId, camp.id)
      }
      drafts.push({ key: levelKeySeq++, mapId: lvl.mapId, mapName: mapName(lvl.mapId), block: clone(block) })
    }
    levels.value = drafts
  } finally {
    levelsLoading.value = false
  }
}

function deriveBlock(mapId: string, campaignId: string): MapCampaignBlock {
  return {
    campaignId,
    levelId: mapId,
    displayName: mapName(mapId),
    prerequisiteLevelId: null,
    description: '',
    sortOrder: 0,
    objectives: [],
  }
}

function clone<T>(v: T): T {
  return JSON.parse(JSON.stringify(v)) as T
}

function newCampaign(): void {
  selectedId.value = ''
  saveError.value = ''
  savedLabel.value = ''
  removedMapIds.value = new Set()
  levels.value = []
  const nextSort = campaigns.value.reduce((max, c) => Math.max(max, c.sortOrder ?? 0), 0) + 1
  form.value = { id: '', displayName: '', description: '', sortOrder: nextSort, locked: false, isNew: true }
}

// Duplicate copies only the header — a campaign's levels ARE its maps, and a
// map can belong to one campaign at a time, so the copy starts with no levels.
function duplicateCampaign(id: string): void {
  const camp = campaigns.value.find((c) => c.id === id)
  if (!camp) return
  selectedId.value = ''
  removedMapIds.value = new Set()
  levels.value = []
  saveError.value = ''
  savedLabel.value = ''
  const name = `${camp.displayName} Copy`
  form.value = {
    id: slugify(name),
    displayName: name,
    description: camp.description ?? '',
    sortOrder: (camp.sortOrder ?? 0) + 1,
    locked: camp.locked ?? false,
    isNew: true,
  }
}

// While the campaign is new, the id follows the display name (read-only field);
// once saved, the id is locked and manual edits win.
watch(
  () => form.value?.displayName,
  (name) => {
    if (form.value?.isNew && name != null) form.value.id = slugify(name)
  },
)

function onIdInput(value: string): void {
  if (form.value?.isNew) form.value.id = value
}

function addSelectedMap(): void {
  if (!addMapId.value || !form.value) return
  const mapId = addMapId.value
  addMapId.value = ''
  if (isInThisCampaign(mapId)) return
  const inOther = maps.value.find((m) => m.id === mapId)?.campaignId
  if (inOther && inOther !== form.value.id) {
    if (!window.confirm(`"${mapName(mapId)}" is currently in campaign "${inOther}". Move it to this campaign?`)) {
      return
    }
  }
  removedMapIds.value.delete(mapId)
  const prev = levels.value.length ? levels.value[levels.value.length - 1].block.levelId : null
  levels.value.push({
    key: levelKeySeq++,
    mapId,
    mapName: mapName(mapId),
    block: {
      campaignId: form.value.id,
      levelId: mapId,
      displayName: mapName(mapId),
      prerequisiteLevelId: prev || null,
      description: '',
      sortOrder: levels.value.length,
      objectives: [],
    },
  })
}

function removeLevel(idx: number): void {
  const [lvl] = levels.value.splice(idx, 1)
  if (lvl) removedMapIds.value.add(lvl.mapId)
}

function moveLevel(idx: number, dir: -1 | 1): void {
  const target = idx + dir
  if (target < 0 || target >= levels.value.length) return
  const arr = levels.value
  ;[arr[idx], arr[target]] = [arr[target], arr[idx]]
}

// ── Save / delete ───────────────────────────────────────────────────────────
async function save(): Promise<void> {
  if (!form.value || !canSave.value || saving.value) return
  saving.value = true
  saveError.value = ''
  const f = form.value
  try {
    // Header FIRST: a map's campaign block is rejected if its campaignId has no
    // header, so a brand-new campaign must exist before its maps reference it.
    await saveCampaignHeader({
      id: f.id,
      displayName: f.displayName,
      description: f.description,
      sortOrder: f.sortOrder,
      locked: f.locked,
    })

    // Each level → write its map's campaign block. This editor owns only the
    // ASSEMBLY fields (campaignId, level id/name, prerequisite, order); the
    // map's OBJECTIVES and description are authored in the Map editor, so we
    // re-fetch the current file and preserve whatever it has on disk rather
    // than writing our (possibly stale) copy back — the two editors never
    // clobber each other's fields.
    for (let i = 0; i < levels.value.length; i++) {
      const lvl = levels.value[i]
      const file = await fetchMapCatalogFile(lvl.mapId)
      const existing = file.map.campaign
      file.map.campaign = {
        campaignId: f.id,
        levelId: lvl.block.levelId,
        displayName: lvl.block.displayName,
        prerequisiteLevelId: lvl.block.prerequisiteLevelId,
        sortOrder: i,
        description: existing?.description ?? lvl.block.description ?? '',
        objectives: existing?.objectives ?? lvl.block.objectives ?? [],
      }
      await saveMapCatalogFile(file)
    }

    // Removed maps → clear their campaign block (only if it still points at us).
    for (const mapId of removedMapIds.value) {
      if (levels.value.some((l) => l.mapId === mapId)) continue
      const file = await fetchMapCatalogFile(mapId)
      if (file.map.campaign?.campaignId === f.id) {
        file.map.campaign = undefined
        await saveMapCatalogFile(file)
      }
    }

    f.isNew = false
    selectedId.value = f.id
    removedMapIds.value = new Set()
    savedLabel.value = 'just now'
    await reload()
  } catch (err) {
    if (err instanceof LevelConflictError) {
      saveError.value = `Level "${err.conflict.levelId}" is already used by map "${err.conflict.ownerMapName}" — change that level id.`
    } else if (err instanceof CampaignSaveError) {
      saveError.value = err.message
    } else {
      saveError.value = err instanceof Error ? err.message : 'Save failed.'
    }
  } finally {
    saving.value = false
  }
}

async function removeCampaign(): Promise<void> {
  if (!form.value || form.value.isNew) return
  const f = form.value
  if (levels.value.length > 0) {
    saveError.value = 'Remove all levels (maps) from this campaign before deleting it.'
    return
  }
  if (!window.confirm(`Delete campaign "${f.displayName}"? This cannot be undone.`)) return
  saving.value = true
  saveError.value = ''
  try {
    await deleteCampaign(f.id)
    form.value = null
    selectedId.value = ''
    await reload()
  } catch (err) {
    saveError.value = err instanceof CampaignSaveError ? err.message : (err instanceof Error ? err.message : 'Delete failed.')
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.campaign-editor {
  width: 100%;
  height: 100%;
}

.campaign-editor__scroll {
  flex: 1 1 auto;
  min-height: 0;
}

.campaign-editor__body {
  display: flex;
  flex-direction: column;
  gap: var(--ed-gap);
  padding-right: 4px;
}

.campaign-editor__pair {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}

.campaign-editor__hint {
  margin: 0 0 4px;
  font-size: 0.76rem;
  color: var(--ed-text-dim);
  line-height: 1.4;
}

.campaign-editor__count {
  font-size: 0.8rem;
  color: var(--ed-text-dim);
}

.campaign-editor__loading,
.campaign-editor__empty {
  font-size: 0.8rem;
  color: var(--ed-text-dim);
  padding: 6px 2px;
}

.campaign-editor__levels {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.campaign-level {
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
  background: rgba(0, 0, 0, 0.22);
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.campaign-level__head {
  display: flex;
  align-items: center;
  gap: 8px;
}

.campaign-level__num {
  flex: 0 0 auto;
  width: 22px;
  height: 22px;
  display: grid;
  place-items: center;
  font-size: 0.74rem;
  font-weight: 700;
  color: #0b1220;
  background: var(--ed-brass);
  border-radius: 50%;
}

.campaign-level__map {
  font-weight: 700;
  color: var(--ed-text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.campaign-level__spacer {
  flex: 1 1 auto;
}

.campaign-level__btn {
  flex: 0 0 auto;
  padding: 2px 7px;
  font-size: 0.8rem;
  color: var(--ed-text-dim);
  background: rgba(148, 163, 184, 0.1);
  border: 1px solid var(--ed-line);
  border-radius: 6px;
}

.campaign-level__btn:hover:not(:disabled) {
  color: var(--ed-brass);
  border-color: var(--ed-line-strong);
}

.campaign-level__btn:disabled {
  opacity: 0.35;
}

.campaign-level__btn--danger:hover {
  color: var(--ed-danger);
}

.campaign-level__fields {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px 12px;
}

.campaign-editor__add {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-top: 4px;
}

.campaign-editor__add select {
  flex: 1 1 auto;
}

.campaign-editor__welcome {
  margin: auto;
  text-align: center;
  max-width: 380px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  align-items: center;
  color: var(--ed-text-dim);
}

.campaign-editor__welcome h2 {
  font-family: var(--font-title);
  color: var(--ed-brass);
  margin: 0;
}

/* Objectives — compact per-level editor. */
.campaign-objectives {
  display: flex;
  flex-direction: column;
  gap: 6px;
  border-top: 1px solid var(--ed-line);
  padding-top: 8px;
}

.campaign-objectives__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--ed-text-dim);
}

.campaign-objectives__header button {
  font-size: 0.72rem;
  padding: 3px 8px;
  color: var(--ed-brass);
  background: rgba(212, 168, 71, 0.08);
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}

.campaign-objectives__empty {
  margin: 0;
  font-size: 0.72rem;
  color: var(--ed-text-dim);
  opacity: 0.8;
}

.campaign-objective {
  display: flex;
  flex-direction: column;
  gap: 5px;
  padding: 8px;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
  background: rgba(0, 0, 0, 0.18);
}

.campaign-objective__row {
  display: flex;
  gap: 6px;
  align-items: center;
}

.campaign-objective__id {
  flex: 1 1 auto;
}

.campaign-objective__remove {
  flex: 0 0 auto;
  padding: 2px 8px;
  color: var(--ed-danger);
  background: none;
  border: 1px solid var(--ed-line);
  border-radius: 6px;
}

.campaign-objective__meta {
  display: flex;
  gap: 10px;
  align-items: center;
  flex-wrap: wrap;
}

.campaign-objective__required,
.campaign-objective__reward {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 0.72rem;
  color: var(--ed-text-dim);
}

.campaign-objective__reward input {
  width: 64px;
}

.campaign-objective__config {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}

.campaign-objective__config label {
  display: flex;
  flex-direction: column;
  gap: 3px;
  font-size: 0.7rem;
  color: var(--ed-text-dim);
}
</style>
