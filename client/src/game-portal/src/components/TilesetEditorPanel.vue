<template>
  <EditorShell class="tileset-editor" theme="forge">
    <template #sidebar>
      <div class="tileset-editor__sidebar-col">
        <div class="tileset-editor__view-toggle" role="group" aria-label="Library view">
          <button
            type="button"
            class="tileset-editor__toggle-btn"
            :class="{ 'tileset-editor__toggle-btn--active': view === 'tilesets' }"
            @click="view = 'tilesets'"
          >Tilesets</button>
          <button
            type="button"
            class="tileset-editor__toggle-btn"
            :class="{ 'tileset-editor__toggle-btn--active': view === 'tiles' }"
            @click="view = 'tiles'"
          >Tiles</button>
        </div>

        <EditorSidebar
          v-if="view === 'tilesets'"
          title="Tilesets"
          new-label="New Tileset"
          :groups="sidebarGroups"
          :selected-id="selectedId"
          :search="search"
          search-placeholder="Search tilesets…"
          :empty-text="loading ? 'Loading…' : 'No tilesets match.'"
          @update:search="search = $event"
          @select="selectTileset"
          @new="newTileset"
          @duplicate="duplicateTileset"
        />

        <div v-else class="tileset-editor__tiles-side">
          <span class="tileset-editor__tiles-side-title">Tile Library</span>
          <p class="tileset-editor__hint">
            Cut tiles from a tileset image, then build a new tileset from them.
          </p>
        </div>
      </div>
    </template>

    <template #main>
      <template v-if="view === 'tilesets'">
        <template v-if="form">
          <EditorHeader
            :title="form.name || 'New Tileset'"
            :badge="form.isNew ? 'new' : undefined"
            badge-color="#e7c88a"
            :breadcrumb="`${form.cols} × ${form.rows} tiles`"
            :id="form.id"
            :id-editable="form.isNew"
            id-input-id="te-id"
            :saving="saving"
            :save-disabled="saving || !canSave"
            :saved-label="savedLabel"
            :error="saveError"
            :remove-label="form.isNew ? '' : 'Delete'"
            @update:id="onIdInput"
            @save="save"
            @remove="removeTileset"
          />

          <GameScrollArea class="tileset-editor__scroll">
            <div class="tileset-editor__body">
              <SectionCard title="Details" :index="1">
                <EditorField label="Name" for-id="te-name">
                  <input id="te-name" v-model.trim="form.name" type="text" placeholder="e.g. Grassland" />
                </EditorField>
              </SectionCard>

              <SectionCard title="Image" :index="2">
                <p v-if="form.isNew" class="tileset-editor__hint">
                  Save the tileset first, then upload an image.
                </p>
                <EditorField label="Terrain PNG" for-id="te-image" hint="(replaces the current image)">
                  <input
                    id="te-image"
                    type="file"
                    accept="image/png"
                    :disabled="form.isNew || imageUploading"
                    @change="onImageFileChange"
                  />
                </EditorField>
                <p v-if="imageUploading" class="tileset-editor__hint">Uploading…</p>
                <p v-if="imageError" class="tileset-editor__error">{{ imageError }}</p>
              </SectionCard>

              <SectionCard title="Slice" :index="3">
                <div class="tileset-editor__grid2">
                  <EditorField label="Columns" for-id="te-cols">
                    <input id="te-cols" v-model.number="form.cols" type="number" min="1" step="1" />
                  </EditorField>
                  <EditorField label="Rows" for-id="te-rows">
                    <input id="te-rows" v-model.number="form.rows" type="number" min="1" step="1" />
                  </EditorField>
                  <EditorField label="Offset X" for-id="te-offset-x">
                    <input id="te-offset-x" v-model.number="form.offsetX" type="number" min="0" step="1" />
                  </EditorField>
                  <EditorField label="Offset Y" for-id="te-offset-y">
                    <input id="te-offset-y" v-model.number="form.offsetY" type="number" min="0" step="1" />
                  </EditorField>
                  <EditorField label="Tile Width" for-id="te-tile-width">
                    <input id="te-tile-width" v-model.number="form.tileWidth" type="number" min="1" step="1" />
                  </EditorField>
                  <EditorField label="Tile Height" for-id="te-tile-height">
                    <input id="te-tile-height" v-model.number="form.tileHeight" type="number" min="1" step="1" />
                  </EditorField>
                  <EditorField label="Spacing X" for-id="te-spacing-x">
                    <input id="te-spacing-x" v-model.number="form.spacingX" type="number" min="0" step="1" />
                  </EditorField>
                  <EditorField label="Spacing Y" for-id="te-spacing-y">
                    <input id="te-spacing-y" v-model.number="form.spacingY" type="number" min="0" step="1" />
                  </EditorField>
                </div>
              </SectionCard>

              <SectionCard title="Preview" :index="4">
                <canvas
                  v-if="imageSrc"
                  ref="overlayCanvas"
                  class="tileset-editor__canvas"
                  @click="onCanvasClick"
                  @pointerdown="onCanvasPointerDown"
                  @pointermove="onCanvasPointerMove"
                  @pointerup="onCanvasPointerUp"
                />
                <p v-else class="tileset-editor__placeholder">Upload an image to see the grid overlay.</p>

                <div v-if="imageSrc" class="tileset-editor__cut">
                  <div class="tileset-editor__view-toggle" role="group" aria-label="Cut mode">
                    <button
                      type="button"
                      class="tileset-editor__toggle-btn"
                      :class="{ 'tileset-editor__toggle-btn--active': cutMode === 'snap' }"
                      @click="cutMode = 'snap'"
                    >Snap cell</button>
                    <button
                      type="button"
                      class="tileset-editor__toggle-btn"
                      :class="{ 'tileset-editor__toggle-btn--active': cutMode === 'crop' }"
                      @click="cutMode = 'crop'"
                    >Free crop</button>
                  </div>

                  <p class="tileset-editor__hint">{{ selectionHint }}</p>

                  <EditorField label="Tile ID" for-id="te-new-tile-id">
                    <input
                      id="te-new-tile-id"
                      :value="newTileId"
                      type="text"
                      placeholder="e.g. grass-01"
                      @input="onNewTileIdInput"
                    />
                  </EditorField>

                  <UiButton variant="active" :disabled="!canSaveTile" @click="saveSelectedTile">
                    {{ savingTile ? 'Saving…' : 'Save as tile' }}
                  </UiButton>
                  <p v-if="tileSaveError" class="tileset-editor__error">{{ tileSaveError }}</p>
                  <p v-else-if="tileSavedLabel" class="tileset-editor__hint">Saved.</p>
                </div>
              </SectionCard>
            </div>
          </GameScrollArea>
        </template>

        <section v-else class="tileset-editor__welcome">
          <h2>Tilesets</h2>
          <p>Select a tileset to edit, or create a new one. A tileset slices a terrain PNG into a grid of tiles.</p>
          <UiButton variant="active" @click="newTileset">New Tileset</UiButton>
        </section>
      </template>

      <section v-else class="tileset-editor__tiles-view">
        <template v-if="!building">
          <div class="tileset-editor__tiles-head">
            <h2>Tile Library</h2>
            <UiButton variant="active" @click="startBuild">Build tileset ▸</UiButton>
          </div>

          <GameScrollArea class="tileset-editor__scroll">
            <p v-if="tilesLoading" class="tileset-editor__hint">Loading…</p>
            <p v-else-if="tilesError" class="tileset-editor__error">{{ tilesError }}</p>
            <p v-else-if="tiles.length === 0" class="tileset-editor__hint">
              No tiles yet — cut one from a tileset image in the Tilesets view.
            </p>
            <div v-else class="tileset-editor__tile-grid">
              <div v-for="t in tiles" :key="t.id" class="tileset-editor__tile-card">
                <div class="tileset-editor__tile-thumb-wrap">
                  <img :src="tileImageUrl(t.id)" :alt="t.id" class="tileset-editor__tile-thumb" />
                </div>
                <span class="tileset-editor__tile-id" :title="t.id">{{ t.id }}</span>
                <span class="tileset-editor__tile-dims">{{ t.width }}×{{ t.height }}</span>
                <button type="button" class="tileset-editor__tile-delete" @click="removeTile(t.id)">Delete</button>
              </div>
            </div>
          </GameScrollArea>
        </template>

        <template v-else>
          <div class="tileset-editor__tiles-head">
            <div class="tileset-editor__tiles-head-title">
              <button type="button" class="tileset-editor__back-btn" @click="building = false">◂ Back</button>
              <h2>Build Tileset</h2>
            </div>
          </div>

          <GameScrollArea class="tileset-editor__scroll">
            <div class="tileset-editor__build">
              <SectionCard title="Details" :index="1">
                <EditorField label="Name" for-id="tb-name">
                  <input id="tb-name" v-model.trim="buildName" type="text" placeholder="e.g. Grassland Set" />
                </EditorField>
                <EditorField label="Id" hint="(from name)">
                  <input :value="buildId" type="text" readonly />
                </EditorField>
                <div class="tileset-editor__grid2">
                  <EditorField label="Columns" for-id="tb-cols">
                    <input id="tb-cols" v-model.number="buildCols" type="number" min="1" max="16" step="1" />
                  </EditorField>
                  <EditorField label="Rows" for-id="tb-rows">
                    <input id="tb-rows" v-model.number="buildRows" type="number" min="1" max="16" step="1" />
                  </EditorField>
                </div>
              </SectionCard>

              <SectionCard title="Layout" :index="2">
                <p class="tileset-editor__hint">
                  Drag tiles onto cells, or click a tile then click a cell. Click a filled cell to clear it.
                </p>

                <div class="tileset-editor__build-cols">
                  <div class="tileset-editor__palette">
                    <p v-if="tilesLoading" class="tileset-editor__hint">Loading…</p>
                    <p v-else-if="tiles.length === 0" class="tileset-editor__hint">No tiles in the library yet.</p>
                    <div
                      v-for="t in tiles"
                      :key="t.id"
                      class="tileset-editor__palette-item"
                      :class="{ 'tileset-editor__palette-item--picked': pickedTileId === t.id }"
                      draggable="true"
                      @dragstart="onPaletteDragStart($event, t.id)"
                      @click="pickedTileId = pickedTileId === t.id ? null : t.id"
                    >
                      <div class="tileset-editor__palette-thumb-wrap">
                        <img :src="tileImageUrl(t.id)" :alt="t.id" class="tileset-editor__palette-thumb" />
                      </div>
                      <span class="tileset-editor__palette-id" :title="t.id">{{ t.id }}</span>
                    </div>
                  </div>

                  <div
                    class="tileset-editor__build-grid"
                    :style="{
                      gridTemplateColumns: `repeat(${buildCols}, minmax(0, 1fr))`,
                      gridTemplateRows: `repeat(${buildRows}, minmax(0, 1fr))`,
                    }"
                  >
                    <div
                      v-for="cell in gridCells"
                      :key="cell.key"
                      class="tileset-editor__build-cell"
                      @dragover.prevent
                      @drop.prevent="onCellDrop($event, cell.key)"
                      @click="onCellClick(cell.key)"
                    >
                      <img
                        v-if="cell.tile"
                        :src="tileImageUrl(cell.tile.id)"
                        :alt="cell.tile.id"
                        class="tileset-editor__build-cell-img"
                      />
                      <span v-else class="tileset-editor__build-cell-empty" />
                    </div>
                  </div>
                </div>
              </SectionCard>

              <SectionCard title="Create" :index="3">
                <p class="tileset-editor__hint">{{ placementSummary }}</p>
                <UiButton variant="active" :disabled="!canCreateBuild" @click="createFromTiles">
                  {{ buildSaving ? 'Creating…' : 'Create tileset' }}
                </UiButton>
                <p v-if="buildError" class="tileset-editor__error">{{ buildError }}</p>
              </SectionCard>
            </div>
          </GameScrollArea>
        </template>
      </section>
    </template>
  </EditorShell>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import EditorShell from '@/components/editor/EditorShell.vue'
import EditorSidebar, { type SidebarGroup } from '@/components/editor/EditorSidebar.vue'
import EditorHeader from '@/components/editor/EditorHeader.vue'
import SectionCard from '@/components/editor/SectionCard.vue'
import EditorField from '@/components/editor/EditorField.vue'
import UiButton from '@/components/ui/UiButton.vue'
import GameScrollArea from '@/components/ui/GameScrollArea.vue'
import { fetchTilesetDefs } from '@/game/maps/catalog'
import { saveTileset, deleteTileset, uploadTilesetImage, TilesetSaveError } from '@/services/tilesetEditorApi'
import { listTiles, saveTile, deleteTile, tileImageUrl, TileSaveError, type TileAsset } from '@/services/tileLibraryApi'
import { tilesetImageUrl } from '@/game/rendering/terrainTileset'
import type { TilesetDef } from '@/game/network/protocol'

type TilesetForm = TilesetDef & { isNew: boolean }

// Which top-level surface the sidebar toggle + main panel show — the whole
// pre-existing tileset editor is the 'tilesets' view; 'tiles' is the new
// per-tile library gallery.
const view = ref<'tilesets' | 'tiles'>('tilesets')

const tilesets = ref<TilesetDef[]>([])
const loading = ref(false)
const saving = ref(false)
const saveError = ref('')
const savedLabel = ref('')
const search = ref('')

const selectedId = ref('')
const form = ref<TilesetForm | null>(null)

const imageUploading = ref(false)
const imageError = ref('')
// Bumped on every successful upload so the preview cache-busts even if the
// server ever reuses the same image key for a re-upload.
const uploadBust = ref(0)
const overlayCanvas = ref<HTMLCanvasElement | null>(null)

// ── Tile library (gallery view) ─────────────────────────────────────────────
const tiles = ref<TileAsset[]>([])
const tilesLoading = ref(false)
const tilesError = ref('')

async function reloadTiles(): Promise<void> {
  tilesLoading.value = true
  tilesError.value = ''
  try {
    tiles.value = await listTiles()
  } catch (err) {
    tilesError.value = err instanceof Error ? err.message : 'Failed to load tiles.'
  } finally {
    tilesLoading.value = false
  }
}
void reloadTiles()

// Refresh whenever the author switches into the gallery, so tiles cut while
// in the Tilesets view show up without a manual reload.
watch(view, (v) => {
  if (v === 'tiles') void reloadTiles()
})

async function removeTile(id: string): Promise<void> {
  if (!window.confirm(`Delete tile "${id}"? This cannot be undone.`)) return
  try {
    await deleteTile(id)
    await reloadTiles()
  } catch (err) {
    tilesError.value = err instanceof TileSaveError ? err.message : (err instanceof Error ? err.message : 'Delete failed.')
  }
}

// ── Build-tileset-from-tiles (freeform builder) ─────────────────────────────
const building = ref(false)
const buildName = ref('')
const buildCols = ref(4)
const buildRows = ref(4)
// Key is `"row,col"` -> tileId. Freeform: gaps are allowed, one tile per cell.
const placements = ref<Record<string, string>>({})
const pickedTileId = ref<string | null>(null)
const buildSaving = ref(false)
const buildError = ref('')

function startBuild(): void {
  if (tiles.value.length === 0) void reloadTiles()
  building.value = true
}

const buildId = computed(() => slugify(buildName.value))

const tileById = computed(() => {
  const m = new Map<string, TileAsset>()
  for (const t of tiles.value) m.set(t.id, t)
  return m
})

// Cells for the current rows × cols. Resizing does not clear `placements` —
// out-of-range keys simply aren't rendered here (and are skipped at stitch
// time too), so shrinking then growing back restores prior placements.
const gridCells = computed(() => {
  const cells: { key: string; tile: TileAsset | null }[] = []
  for (let r = 0; r < buildRows.value; r++) {
    for (let c = 0; c < buildCols.value; c++) {
      const key = `${r},${c}`
      const tileId = placements.value[key]
      cells.push({ key, tile: tileId ? (tileById.value.get(tileId) ?? null) : null })
    }
  }
  return cells
})

// Placements that are both within the current bounds and reference a tile
// that still exists in the library.
const placedList = computed(() => {
  const result: { key: string; tile: TileAsset }[] = []
  for (const [key, id] of Object.entries(placements.value)) {
    const [r, c] = key.split(',').map(Number)
    if (r >= buildRows.value || c >= buildCols.value) continue
    const t = tileById.value.get(id)
    if (t) result.push({ key, tile: t })
  }
  return result
})

const buildCellSize = computed(() => {
  let w = 0
  let h = 0
  for (const p of placedList.value) {
    w = Math.max(w, p.tile.width)
    h = Math.max(h, p.tile.height)
  }
  return { w: w || 32, h: h || 32 }
})

const placementSummary = computed(() => {
  const n = placedList.value.length
  const { w, h } = buildCellSize.value
  return `${n} tile${n === 1 ? '' : 's'} placed, sheet ${buildCols.value * w}×${buildRows.value * h}`
})

const canCreateBuild = computed(
  () =>
    buildId.value.length > 0 &&
    buildName.value.trim().length > 0 &&
    placedList.value.length > 0 &&
    !buildSaving.value,
)

function onPaletteDragStart(e: DragEvent, id: string): void {
  e.dataTransfer?.setData('text/tile-id', id)
}

function setPlacement(key: string, tileId: string): void {
  placements.value = { ...placements.value, [key]: tileId }
}

function clearPlacement(key: string): void {
  const next = { ...placements.value }
  delete next[key]
  placements.value = next
}

function onCellDrop(e: DragEvent, key: string): void {
  const id = e.dataTransfer?.getData('text/tile-id') || pickedTileId.value
  if (!id) return
  setPlacement(key, id)
}

function onCellClick(key: string): void {
  if (pickedTileId.value) {
    setPlacement(key, pickedTileId.value)
    return
  }
  if (placements.value[key]) clearPlacement(key)
}

async function createFromTiles(): Promise<void> {
  if (!canCreateBuild.value) return
  buildSaving.value = true
  buildError.value = ''
  try {
    const placed = placedList.value
    const { w: cellW, h: cellH } = buildCellSize.value

    // Preload every distinct placed tile's PNG once. Same-origin via the
    // vite proxy, so the canvas stays untainted for toBlob() below.
    const uniqueIds = [...new Set(placed.map((p) => p.tile.id))]
    const images = new Map<string, HTMLImageElement>()
    await Promise.all(
      uniqueIds.map(
        (id) =>
          new Promise<void>((resolve, reject) => {
            const img = new Image()
            img.onload = () => {
              images.set(id, img)
              resolve()
            }
            img.onerror = () => reject(new Error(`Failed to load tile image "${id}".`))
            img.src = tileImageUrl(id)
          }),
      ),
    )

    const canvas = document.createElement('canvas')
    canvas.width = buildCols.value * cellW
    canvas.height = buildRows.value * cellH
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('Canvas unavailable.')
    ctx.imageSmoothingEnabled = false

    for (const [key, id] of Object.entries(placements.value)) {
      const [r, c] = key.split(',').map(Number)
      if (r >= buildRows.value || c >= buildCols.value) continue
      const img = images.get(id)
      if (!img) continue
      const dx = Math.round(c * cellW + (cellW - img.naturalWidth) / 2)
      const dy = Math.round(r * cellH + (cellH - img.naturalHeight) / 2)
      ctx.drawImage(img, dx, dy)
    }

    const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, 'image/png'))
    if (!blob) throw new Error('Failed to render the stitched tileset image.')
    const file = new File([blob], `${buildId.value}.png`, { type: 'image/png' })

    // Upload first — saveTileset requires def.image to be non-empty, and
    // uploadTilesetImage only needs the id + PNG, not a pre-existing def.
    const { image } = await uploadTilesetImage(buildId.value, file)
    await saveTileset({
      id: buildId.value,
      name: buildName.value,
      image,
      cols: buildCols.value,
      rows: buildRows.value,
      offsetX: 0,
      offsetY: 0,
      tileWidth: cellW,
      tileHeight: cellH,
      spacingX: 0,
      spacingY: 0,
    })

    await reload()
    building.value = false
    view.value = 'tilesets'
    selectTileset(buildId.value)
    buildName.value = ''
    placements.value = {}
    pickedTileId.value = null
  } catch (err) {
    buildError.value =
      err instanceof TilesetSaveError ? err.message : err instanceof Error ? err.message : 'Build failed.'
  } finally {
    buildSaving.value = false
  }
}

function clone<T>(v: T): T {
  return JSON.parse(JSON.stringify(v)) as T
}

function slugify(name: string): string {
  const base = name.toLowerCase().trim().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')
  return base || 'tileset'
}

const matches = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return tilesets.value
  return tilesets.value.filter((t) => t.name.toLowerCase().includes(q) || t.id.toLowerCase().includes(q))
})

const sidebarGroups = computed<SidebarGroup[]>(() => {
  if (matches.value.length === 0) return []
  return [
    {
      label: 'Tilesets',
      entries: [...matches.value]
        .sort((a, b) => a.name.localeCompare(b.name))
        .map((t) => ({ id: t.id, name: t.name })),
    },
  ]
})

// Client-side gate; the server re-validates. Every numeric field is an
// integer, cols/rows/tileWidth/tileHeight are at least 1, offsets/spacing
// are non-negative.
const canSave = computed(() => {
  if (!form.value) return false
  const f = form.value
  if (!f.id.trim() || !f.name.trim()) return false
  const positiveInt = (n: number) => Number.isInteger(n) && n >= 1
  const nonNegativeInt = (n: number) => Number.isInteger(n) && n >= 0
  return (
    positiveInt(f.cols) &&
    positiveInt(f.rows) &&
    positiveInt(f.tileWidth) &&
    positiveInt(f.tileHeight) &&
    nonNegativeInt(f.offsetX) &&
    nonNegativeInt(f.offsetY) &&
    nonNegativeInt(f.spacingX) &&
    nonNegativeInt(f.spacingY)
  )
})

async function reload(): Promise<void> {
  loading.value = true
  try {
    tilesets.value = await fetchTilesetDefs()
  } catch (err) {
    saveError.value = err instanceof Error ? err.message : 'Failed to load tilesets.'
  } finally {
    loading.value = false
  }
}
void reload()

function selectTileset(id: string): void {
  const t = tilesets.value.find((x) => x.id === id)
  if (!t) return
  selectedId.value = id
  saveError.value = ''
  savedLabel.value = ''
  imageError.value = ''
  form.value = { ...clone(t), isNew: false }
}

function newTileset(): void {
  selectedId.value = ''
  saveError.value = ''
  savedLabel.value = ''
  imageError.value = ''
  form.value = {
    id: '',
    name: '',
    image: '',
    cols: 4,
    rows: 4,
    offsetX: 0,
    offsetY: 0,
    tileWidth: 32,
    tileHeight: 32,
    spacingX: 0,
    spacingY: 0,
    isNew: true,
  }
}

// Duplicate copies the slice geometry and the uploaded image key — the copy
// shares the same PNG until the author uploads a different one.
function duplicateTileset(id: string): void {
  const t = tilesets.value.find((x) => x.id === id)
  if (!t) return
  selectedId.value = ''
  saveError.value = ''
  savedLabel.value = ''
  imageError.value = ''
  const name = `${t.name} Copy`
  form.value = {
    ...clone(t),
    id: slugify(name),
    name,
    isNew: true,
  }
}

// While the tileset is new, the id follows the name (read-only field); once
// saved, the id is locked and manual edits win.
watch(
  () => form.value?.name,
  (name) => {
    if (form.value?.isNew && name != null) form.value.id = slugify(name)
  },
)

function onIdInput(value: string): void {
  if (form.value?.isNew) form.value.id = value
}

async function onImageFileChange(e: Event): Promise<void> {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  // Reset so re-selecting the same file re-fires @change.
  input.value = ''
  if (!file || !form.value || form.value.isNew) return
  imageError.value = ''
  imageUploading.value = true
  try {
    const res = await uploadTilesetImage(form.value.id, file)
    form.value.image = res.image
    uploadBust.value++
  } catch (err) {
    imageError.value = err instanceof TilesetSaveError ? err.message : (err instanceof Error ? err.message : 'Upload failed.')
  } finally {
    imageUploading.value = false
  }
}

const imageSrc = computed(() => {
  if (!form.value?.image) return ''
  return `${tilesetImageUrl(form.value as TilesetDef)}?t=${form.value.image}-${uploadBust.value}`
})

const PREVIEW_MAX_WIDTH = 480
const GRID_LINE_COLOR = '#7CFC00'
const SELECTION_COLOR = '#facc15'

// ── Create-Tile (cut a tile out of the preview) ─────────────────────────────
// Holds the loaded native-resolution bitmap so extraction always samples the
// source PNG's real pixels, independent of the on-screen preview scale.
const sourceImage = ref<HTMLImageElement | null>(null)
const cutMode = ref<'snap' | 'crop'>('snap')
const selectedCell = ref<{ col: number; row: number } | null>(null)
// Stored in SOURCE-image pixel coordinates (not canvas/CSS pixels).
const cropRect = ref<{ x: number; y: number; w: number; h: number } | null>(null)
const newTileId = ref('')
// Tracks whether the author has hand-edited the Tile ID. While false we keep
// the field auto-filled from the current selection so that selecting a tile is
// enough to enable "Save as tile" — the empty-id gate was the reason the button
// looked permanently un-clickable.
const tileIdEdited = ref(false)
const savingTile = ref(false)
const tileSaveError = ref('')
const tileSavedLabel = ref('')

// A sensible default id for the current selection, derived from the tileset id
// plus the cell/crop, so the Save button is usable without typing.
function suggestedTileId(): string {
  const f = form.value
  if (!f) return ''
  const base = f.id || slugify(f.name) || 'tile'
  if (cutMode.value === 'snap' && selectedCell.value) {
    return slugify(`${base}-c${selectedCell.value.col}-r${selectedCell.value.row}`)
  }
  if (cutMode.value === 'crop' && cropRect.value) {
    return slugify(`${base}-crop-${Math.round(cropRect.value.x)}-${Math.round(cropRect.value.y)}`)
  }
  return ''
}

// When a selection appears (and the author hasn't taken over the field), fill
// in the suggested id so the selection alone satisfies the save gate.
watch(
  () => [cutMode.value, selectedCell.value, cropRect.value] as const,
  () => {
    if (tileIdEdited.value) return
    const suggestion = suggestedTileId()
    if (suggestion) newTileId.value = suggestion
  },
)
// In-progress crop drag, source-pixel coordinates. Not reactive UI state —
// only cropRect (the normalized result) needs to trigger a repaint.
let cropDragOrigin: { x: number; y: number } | null = null

function drawOverlay(): void {
  const canvas = overlayCanvas.value
  const f = form.value
  const src = imageSrc.value
  if (!canvas || !f || !src) return
  const img = new Image()
  img.onload = () => {
    // The selection or image may have changed while this image was loading —
    // bail rather than paint a stale overlay over a since-replaced canvas.
    if (overlayCanvas.value !== canvas || imageSrc.value !== src) return
    sourceImage.value = img
    const scale = img.naturalWidth > 0 ? PREVIEW_MAX_WIDTH / img.naturalWidth : 1
    canvas.width = Math.max(1, Math.round(img.naturalWidth * scale))
    canvas.height = Math.max(1, Math.round(img.naturalHeight * scale))
    const ctx = canvas.getContext('2d')
    if (!ctx) return
    ctx.imageSmoothingEnabled = false
    ctx.clearRect(0, 0, canvas.width, canvas.height)
    ctx.drawImage(img, 0, 0, canvas.width, canvas.height)

    ctx.strokeStyle = GRID_LINE_COLOR
    ctx.lineWidth = 1
    ctx.setLineDash([])
    for (let col = 0; col < f.cols; col++) {
      for (let row = 0; row < f.rows; row++) {
        const x = (f.offsetX + col * (f.tileWidth + f.spacingX)) * scale
        const y = (f.offsetY + row * (f.tileHeight + f.spacingY)) * scale
        ctx.strokeRect(x + 0.5, y + 0.5, f.tileWidth * scale, f.tileHeight * scale)
      }
    }

    if (cutMode.value === 'snap' && selectedCell.value) {
      const { col, row } = selectedCell.value
      const x = (f.offsetX + col * (f.tileWidth + f.spacingX)) * scale
      const y = (f.offsetY + row * (f.tileHeight + f.spacingY)) * scale
      ctx.strokeStyle = SELECTION_COLOR
      ctx.lineWidth = 2
      ctx.setLineDash([])
      ctx.strokeRect(x + 1, y + 1, f.tileWidth * scale - 2, f.tileHeight * scale - 2)
    }

    if (cutMode.value === 'crop' && cropRect.value) {
      const r = cropRect.value
      ctx.strokeStyle = SELECTION_COLOR
      ctx.lineWidth = 2
      ctx.setLineDash([5, 4])
      ctx.strokeRect(r.x * scale + 1, r.y * scale + 1, r.w * scale - 2, r.h * scale - 2)
      ctx.setLineDash([])
    }
  }
  img.src = src
}

// Redraw whenever any slice field, the image, or the active selection changes.
watch(
  () =>
    form.value
      ? ([
          form.value.cols,
          form.value.rows,
          form.value.offsetX,
          form.value.offsetY,
          form.value.tileWidth,
          form.value.tileHeight,
          form.value.spacingX,
          form.value.spacingY,
          imageSrc.value,
          cutMode.value,
          selectedCell.value,
          cropRect.value,
        ] as const)
      : null,
  async () => {
    await nextTick()
    drawOverlay()
  },
  { immediate: true },
)

// Switching modes drops whatever was selected in the other mode — the two
// selections are mutually exclusive so there's never a stale one to extract.
watch(cutMode, () => {
  selectedCell.value = null
  cropRect.value = null
  // Let the next selection re-suggest an id for the new mode.
  tileIdEdited.value = false
})

// Converts a pointer/mouse event to SOURCE-image pixel coordinates. The
// canvas bitmap is a uniform scale of the source image regardless of CSS
// display size, so this ratio is exact.
function toSourcePoint(e: MouseEvent): { x: number; y: number } | null {
  const canvas = overlayCanvas.value
  const img = sourceImage.value
  if (!canvas || !img) return null
  const rect = canvas.getBoundingClientRect()
  if (rect.width <= 0 || rect.height <= 0) return null
  return {
    x: (e.clientX - rect.left) * (img.naturalWidth / rect.width),
    y: (e.clientY - rect.top) * (img.naturalHeight / rect.height),
  }
}

function onCanvasClick(e: MouseEvent): void {
  if (cutMode.value !== 'snap') return
  const f = form.value
  const pt = toSourcePoint(e)
  if (!f || !pt) return
  const strideX = f.tileWidth + f.spacingX
  const strideY = f.tileHeight + f.spacingY
  if (strideX <= 0 || strideY <= 0) return
  const relX = pt.x - f.offsetX
  const relY = pt.y - f.offsetY
  if (relX < 0 || relY < 0) return
  const col = Math.floor(relX / strideX)
  const row = Math.floor(relY / strideY)
  if (col < 0 || col >= f.cols || row < 0 || row >= f.rows) return
  // Ignore clicks landing in the spacing gutter between tiles.
  const withinX = relX - col * strideX
  const withinY = relY - row * strideY
  if (withinX > f.tileWidth || withinY > f.tileHeight) return
  selectedCell.value = { col, row }
  cropRect.value = null
}

function normalizeCropRect(
  a: { x: number; y: number },
  b: { x: number; y: number },
  maxW: number,
  maxH: number,
): { x: number; y: number; w: number; h: number } | null {
  const x0 = Math.max(0, Math.min(a.x, b.x))
  const y0 = Math.max(0, Math.min(a.y, b.y))
  const x1 = Math.min(maxW, Math.max(a.x, b.x))
  const y1 = Math.min(maxH, Math.max(a.y, b.y))
  const w = x1 - x0
  const h = y1 - y0
  if (w <= 0 || h <= 0) return null
  return { x: x0, y: y0, w, h }
}

function onCanvasPointerDown(e: PointerEvent): void {
  if (cutMode.value !== 'crop') return
  const pt = toSourcePoint(e)
  if (!pt) return
  ;(e.currentTarget as HTMLElement | null)?.setPointerCapture?.(e.pointerId)
  cropDragOrigin = pt
  cropRect.value = null
}

function onCanvasPointerMove(e: PointerEvent): void {
  if (cutMode.value !== 'crop' || !cropDragOrigin) return
  const pt = toSourcePoint(e)
  const img = sourceImage.value
  if (!pt || !img) return
  cropRect.value = normalizeCropRect(cropDragOrigin, pt, img.naturalWidth, img.naturalHeight)
}

function onCanvasPointerUp(e: PointerEvent): void {
  if (cutMode.value !== 'crop' || !cropDragOrigin) return
  const pt = toSourcePoint(e)
  const img = sourceImage.value
  const origin = cropDragOrigin
  cropDragOrigin = null
  if (!pt || !img) return
  const rect = normalizeCropRect(origin, pt, img.naturalWidth, img.naturalHeight)
  // Ignore sub-2px drags — almost certainly an accidental click, not an
  // intentional crop.
  cropRect.value = rect && rect.w >= 2 && rect.h >= 2 ? rect : null
}

// The source-pixel rect for whichever selection is active, or null if
// nothing is selected yet.
function selectionSourceRect(): { x: number; y: number; w: number; h: number } | null {
  const f = form.value
  if (cutMode.value === 'snap') {
    if (!f || !selectedCell.value) return null
    const { col, row } = selectedCell.value
    return {
      x: f.offsetX + col * (f.tileWidth + f.spacingX),
      y: f.offsetY + row * (f.tileHeight + f.spacingY),
      w: f.tileWidth,
      h: f.tileHeight,
    }
  }
  if (!cropRect.value) return null
  return {
    x: Math.round(cropRect.value.x),
    y: Math.round(cropRect.value.y),
    w: Math.round(cropRect.value.w),
    h: Math.round(cropRect.value.h),
  }
}

const selectionHint = computed(() => {
  if (cutMode.value === 'snap') {
    if (!selectedCell.value || !form.value) return 'Click a tile to select.'
    return `Tile ${selectedCell.value.col},${selectedCell.value.row} (${form.value.tileWidth}×${form.value.tileHeight})`
  }
  if (!cropRect.value) return 'Drag to select a crop region.'
  return `Crop ${Math.round(cropRect.value.w)}×${Math.round(cropRect.value.h)}`
})

const canSaveTile = computed(
  () => selectionSourceRect() != null && newTileId.value.trim().length > 0 && !savingTile.value,
)

function onNewTileIdInput(e: Event): void {
  tileIdEdited.value = true
  newTileId.value = slugify((e.target as HTMLInputElement).value)
}

async function saveSelectedTile(): Promise<void> {
  const img = sourceImage.value
  const rect = selectionSourceRect()
  const id = newTileId.value.trim()
  if (!img || !rect || !id || savingTile.value) return
  savingTile.value = true
  tileSaveError.value = ''
  tileSavedLabel.value = ''
  try {
    const c = document.createElement('canvas')
    c.width = rect.w
    c.height = rect.h
    const cx = c.getContext('2d')
    if (!cx) throw new Error('Canvas unavailable.')
    cx.imageSmoothingEnabled = false
    cx.drawImage(img, rect.x, rect.y, rect.w, rect.h, 0, 0, rect.w, rect.h)
    const blob = await new Promise<Blob | null>((resolve) => c.toBlob(resolve, 'image/png'))
    if (!blob) throw new Error('Failed to extract tile image.')
    await saveTile(id, blob)
    tileSavedLabel.value = 'saved'
    // Re-enable auto-suggestion so the next tile gets a fresh default id.
    tileIdEdited.value = false
    await reloadTiles()
  } catch (err) {
    tileSaveError.value = err instanceof TileSaveError ? err.message : (err instanceof Error ? err.message : 'Save failed.')
  } finally {
    savingTile.value = false
  }
}

// ── Save / delete ───────────────────────────────────────────────────────────
async function save(): Promise<void> {
  if (!form.value || !canSave.value || saving.value) return
  saving.value = true
  saveError.value = ''
  const f = form.value
  const def: TilesetDef = {
    id: f.id,
    name: f.name,
    image: f.image,
    cols: f.cols,
    rows: f.rows,
    offsetX: f.offsetX,
    offsetY: f.offsetY,
    tileWidth: f.tileWidth,
    tileHeight: f.tileHeight,
    spacingX: f.spacingX,
    spacingY: f.spacingY,
  }
  try {
    await saveTileset(def)
    f.isNew = false
    selectedId.value = f.id
    savedLabel.value = 'just now'
    await reload()
  } catch (err) {
    saveError.value = err instanceof TilesetSaveError ? err.message : (err instanceof Error ? err.message : 'Save failed.')
  } finally {
    saving.value = false
  }
}

async function removeTileset(): Promise<void> {
  if (!form.value || form.value.isNew) return
  const f = form.value
  if (!window.confirm(`Delete tileset "${f.name}"? This cannot be undone.`)) return
  saving.value = true
  saveError.value = ''
  try {
    await deleteTileset(f.id)
    form.value = null
    selectedId.value = ''
    await reload()
  } catch (err) {
    saveError.value = err instanceof TilesetSaveError ? err.message : (err instanceof Error ? err.message : 'Delete failed.')
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.tileset-editor {
  width: 100%;
  height: 100%;
}

.tileset-editor__scroll {
  flex: 1 1 auto;
  min-height: 0;
}

.tileset-editor__body {
  display: flex;
  flex-direction: column;
  gap: var(--ed-gap);
  padding-right: 4px;
}

.tileset-editor__grid2 {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px 12px;
}

.tileset-editor__hint {
  margin: 0;
  font-size: 0.76rem;
  color: var(--ed-text-dim);
  line-height: 1.4;
}

.tileset-editor__error {
  margin: 0;
  font-size: 0.76rem;
  color: var(--ed-danger);
}

.tileset-editor__canvas {
  display: block;
  /* Centered preview showing the whole sheet at a glance. The canvas bitmap
     stays higher-res (PREVIEW_MAX_WIDTH) for a crisp grid overlay; this just
     constrains the on-screen size, preserving aspect via height:auto. */
  margin: 0 auto;
  width: auto;
  height: auto;
  max-width: 420px;
  max-height: 420px;
  object-fit: contain;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
  background: rgba(0, 0, 0, 0.22);
  /* Crop-mode drags use pointer events on the canvas itself; without this the
     browser can hijack the gesture as a page scroll/pan on touch input. */
  touch-action: none;
}

.tileset-editor__placeholder {
  margin: 0;
  font-size: 0.8rem;
  color: var(--ed-text-dim);
  padding: 6px 2px;
}

.tileset-editor__welcome {
  margin: auto;
  text-align: center;
  max-width: 380px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  align-items: center;
  color: var(--ed-text-dim);
}

.tileset-editor__welcome h2 {
  font-family: var(--font-title);
  color: var(--ed-brass);
  margin: 0;
}

/* ── Sidebar view toggle ─────────────────────────────────────────────────── */
.tileset-editor__sidebar-col {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.tileset-editor__sidebar-col > :last-child {
  flex: 1 1 auto;
  min-height: 0;
}

.tileset-editor__view-toggle {
  display: flex;
  gap: 2px;
  padding: 3px;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
  background: rgba(0, 0, 0, 0.22);
  flex: 0 0 auto;
}

.tileset-editor__toggle-btn {
  flex: 1 1 0;
  padding: 6px 8px;
  font-family: var(--font-body);
  font-size: 0.72rem;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--ed-text-dim);
  background: none;
  border: 0;
  border-radius: 4px;
}

.tileset-editor__toggle-btn:hover {
  color: var(--ed-text);
}

.tileset-editor__toggle-btn--active {
  color: #1a1408;
  background: var(--ed-brass);
}

.tileset-editor__tiles-side {
  padding: 10px 4px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.tileset-editor__tiles-side-title {
  font-family: var(--font-title);
  font-size: 0.86rem;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--ed-brass);
}

/* ── Tiles library gallery ───────────────────────────────────────────────── */
.tileset-editor__tiles-view {
  display: flex;
  flex-direction: column;
  gap: var(--ed-gap);
  min-height: 0;
  flex: 1 1 auto;
}

.tileset-editor__tiles-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.tileset-editor__tiles-head h2 {
  font-family: var(--font-title);
  color: var(--ed-brass);
  margin: 0;
}

.tileset-editor__tile-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(96px, 1fr));
  gap: 10px;
  padding-right: 4px;
}

.tileset-editor__tile-card {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 6px;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
  background: rgba(0, 0, 0, 0.18);
}

.tileset-editor__tile-thumb-wrap {
  aspect-ratio: 1 / 1;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
  overflow: hidden;
  background-color: #201c14;
  background-image:
    linear-gradient(45deg, rgba(0, 0, 0, 0.25) 25%, transparent 25%),
    linear-gradient(-45deg, rgba(0, 0, 0, 0.25) 25%, transparent 25%),
    linear-gradient(45deg, transparent 75%, rgba(0, 0, 0, 0.25) 75%),
    linear-gradient(-45deg, transparent 75%, rgba(0, 0, 0, 0.25) 75%);
  background-size: 12px 12px;
  background-position: 0 0, 0 6px, 6px -6px, -6px 0;
}

.tileset-editor__tile-thumb {
  max-width: 100%;
  max-height: 100%;
  image-rendering: pixelated;
}

.tileset-editor__tile-id {
  font-size: 0.74rem;
  color: var(--ed-text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.tileset-editor__tile-dims {
  font-size: 0.66rem;
  color: var(--ed-text-dim);
}

.tileset-editor__tile-delete {
  align-self: flex-start;
  padding: 2px 6px;
  font-size: 0.68rem;
  color: var(--ed-danger);
  background: none;
  border: 1px solid var(--ed-line);
  border-radius: 4px;
}

.tileset-editor__tile-delete:hover {
  border-color: var(--ed-danger);
}

/* ── Create-Tile (cut) panel below the Preview canvas ────────────────────── */
.tileset-editor__cut {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 4px;
  padding-top: 10px;
  border-top: 1px solid var(--ed-line);
}

/* ── Builder (Build tileset from tiles) ──────────────────────────────────── */
.tileset-editor__back-btn {
  padding: 6px 10px;
  font-family: var(--font-body);
  font-size: 0.72rem;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--ed-text-dim);
  background: none;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}

.tileset-editor__back-btn:hover {
  color: var(--ed-text);
  border-color: var(--ed-brass);
}

.tileset-editor__tiles-head-title {
  display: flex;
  align-items: center;
  gap: 12px;
}

.tileset-editor__build {
  display: flex;
  flex-direction: column;
  gap: var(--ed-gap);
  padding-right: 4px;
}

.tileset-editor__build-cols {
  display: grid;
  grid-template-columns: minmax(160px, 220px) 1fr;
  gap: 14px;
  align-items: start;
}

.tileset-editor__palette {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 420px;
  overflow-y: auto;
  padding-right: 4px;
}

.tileset-editor__palette-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 5px;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
  background: rgba(0, 0, 0, 0.18);
}

.tileset-editor__palette-item:hover {
  border-color: var(--ed-brass);
}

.tileset-editor__palette-item--picked {
  border-color: var(--ed-brass);
  background: rgba(231, 200, 138, 0.14);
}

.tileset-editor__palette-thumb-wrap {
  flex: 0 0 auto;
  width: 32px;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
  overflow: hidden;
  background-color: #201c14;
}

.tileset-editor__palette-thumb {
  max-width: 100%;
  max-height: 100%;
  image-rendering: pixelated;
}

.tileset-editor__palette-id {
  font-size: 0.72rem;
  color: var(--ed-text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.tileset-editor__build-grid {
  display: grid;
  gap: 2px;
  padding: 8px;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
  background: rgba(0, 0, 0, 0.22);
}

.tileset-editor__build-cell {
  position: relative;
  aspect-ratio: 1 / 1;
  min-width: 28px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px dashed var(--ed-line);
  border-radius: 3px;
  background-color: #201c14;
  overflow: hidden;
}

.tileset-editor__build-cell:hover {
  border-color: var(--ed-brass);
}

.tileset-editor__build-cell-img {
  max-width: 100%;
  max-height: 100%;
  image-rendering: pixelated;
}

.tileset-editor__build-cell-empty {
  display: block;
  width: 100%;
  height: 100%;
}
</style>
