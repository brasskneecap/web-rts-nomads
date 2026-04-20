<template>
  <div class="editor-shell">
    <div class="editor-controls">
      <div class="editor-title">Map Editor</div>
      <p class="editor-copy">
        Pan and zoom like the main game view. Turn on paint mode only when you
        want clicks to edit the map.
      </p>

      <section class="editor-section" :class="{ 'editor-section--open': openSection === 'setup' }">
        <button type="button" class="editor-section__summary" @click="toggleSection('setup')">
          Map Setup
        </button>
        <div v-if="openSection === 'setup'" class="editor-section__body">
          <div class="control-group">
            <label for="editor-map-id">Map ID</label>
            <input id="editor-map-id" v-model.trim="model.id" type="text" />
          </div>

          <div class="control-group">
            <label for="editor-map-name">Map Name</label>
            <input id="editor-map-name" v-model.trim="model.name" type="text" />
          </div>

          <div class="control-group">
            <label for="editor-map-description">Description</label>
            <textarea
              id="editor-map-description"
              v-model.trim="model.description"
              class="metadata-box"
              rows="3"
            ></textarea>
          </div>

          <div class="wave-config-block">
            <div class="wave-config-title">Wave Config</div>
            <div class="control-group">
              <label for="wave-total">Total Waves <span class="field-hint">(0 = disabled)</span></label>
              <input
                id="wave-total"
                :value="model.waveConfig?.totalWaves ?? 0"
                @input="setWaveConfig('totalWaves', +($event.target as HTMLInputElement).value)"
                type="number"
                min="0"
                max="999"
              />
            </div>
            <div class="control-group">
              <label for="wave-prep">Prep Duration <span class="field-hint">(sec, 0 = default 60)</span></label>
              <input
                id="wave-prep"
                :value="model.waveConfig?.prepDuration ?? 0"
                @input="setWaveConfig('prepDuration', +($event.target as HTMLInputElement).value)"
                type="number"
                min="0"
              />
            </div>
            <div class="control-group">
              <label for="wave-active">Wave Duration <span class="field-hint">(sec, 0 = default 120)</span></label>
              <input
                id="wave-active"
                :value="model.waveConfig?.waveDuration ?? 0"
                @input="setWaveConfig('waveDuration', +($event.target as HTMLInputElement).value)"
                type="number"
                min="0"
              />
            </div>
          </div>

          <div class="control-group load-map-group">
            <label for="editor-load-map">Load Existing Map</label>
            <select
              id="editor-load-map"
              v-model="selectedLoadMapId"
              :disabled="isLoadingMapCatalog || isLoadingSelectedMap || availableMaps.length === 0"
            >
              <option v-for="map in availableMaps" :key="map.id" :value="map.id">
                {{ map.name }}
              </option>
            </select>

            <div class="menu-text" v-if="mapLoadError">
              {{ mapLoadError }}
            </div>

            <button
              type="button"
              @click="loadSelectedMapIntoEditor"
              :disabled="!selectedLoadMapId || isLoadingMapCatalog || isLoadingSelectedMap"
            >
              {{ isLoadingSelectedMap ? 'Loading...' : 'Load Into Editor' }}
            </button>
          </div>

          <div class="control-group">
            <label for="editor-cols">Columns</label>
            <input id="editor-cols" v-model.number="draftCols" type="number" min="6" max="500" />
          </div>

          <div class="control-group">
            <label for="editor-rows">Rows</label>
            <input id="editor-rows" v-model.number="draftRows" type="number" min="6" max="500" />
          </div>

          <div class="preset-row">
            <button
              v-for="preset in MAP_EDITOR_PRESETS"
              :key="preset.label"
              type="button"
              @click="applyPreset(preset.cols, preset.rows)"
            >
              {{ preset.label }}
            </button>
          </div>

          <button type="button" class="apply-size" @click="applyGridSize">Apply Grid Size</button>

          <div class="summary-row">
            <span>{{ model.gridCols }} x {{ model.gridRows }}</span>
            <span>{{ model.terrain.length }} terrain</span>
            <span>{{ model.obstacles.length }} obstacles</span>
            <span>{{ model.buildings.length }} buildings</span>
          </div>
        </div>
      </section>

      <section class="editor-section" :class="{ 'editor-section--open': openSection === 'paint' }">
        <button type="button" class="editor-section__summary" @click="toggleSection('paint')">
          Paint
        </button>
        <div v-if="openSection === 'paint'" class="editor-section__body">
          <button
            type="button"
            class="paint-toggle"
            :class="{ 'paint-toggle--active': paintModeEnabled }"
            @click="paintModeEnabled = !paintModeEnabled"
          >
            {{ paintModeEnabled ? 'Painting Enabled' : 'Painting Disabled' }}
          </button>

          <div class="control-group">
            <label for="brush-mode">Brush</label>
            <select id="brush-mode" v-model="brushMode" :disabled="!paintModeEnabled">
              <option value="terrain">Terrain</option>
              <option value="tile">Tile</option>
              <option value="obstacle">Obstacle</option>
              <option value="building">Building</option>
              <option value="erase">Erase</option>
            </select>
          </div>

          <div v-if="brushMode !== 'building'" class="control-group">
            <label for="brush-size">Brush Size</label>
            <select id="brush-size" v-model.number="brushSize" :disabled="!paintModeEnabled">
              <option :value="1">1 × 1</option>
              <option :value="3">3 × 3</option>
              <option :value="5">5 × 5</option>
              <option :value="7">7 × 7</option>
            </select>
          </div>

          <div class="control-group">
            <label for="default-ground">Default Ground</label>
            <select id="default-ground" :value="defaultGroundName" @change="onDefaultGroundChange">
              <option value="grass">Grass</option>
              <option value="dirt">Dirt</option>
            </select>
          </div>

          <div v-if="brushMode === 'terrain'" class="control-group">
            <label for="terrain-type">Terrain Type</label>
            <select id="terrain-type" v-model="selectedTerrain" :disabled="!paintModeEnabled">
              <option value="grass">Grass</option>
              <option value="dirt">Dirt</option>
            </select>
          </div>

          <div v-if="brushMode === 'tile'" class="control-group">
            <label for="tile-sheet">Tile Sheet</label>
            <select id="tile-sheet" v-model="selectedTileSheet" :disabled="!paintModeEnabled">
              <option v-for="sheet in TILE_SHEET_NAMES" :key="sheet" :value="sheet">
                {{ sheet }}
              </option>
            </select>
            <div class="tile-picker-hint">
              {{ selectedTileCoord
                ? `Selected: (${selectedTileCoord.sx}, ${selectedTileCoord.sy}) — right-click a cell to erase`
                : 'Click a tile below to select it' }}
            </div>
            <canvas
              ref="tilePickerCanvas"
              class="tile-picker"
              @click="onTilePickerClick"
            />
          </div>

          <div v-if="brushMode === 'obstacle'" class="control-group">
            <label for="obstacle-type">Obstacle Type</label>
            <select id="obstacle-type" v-model="selectedObstacle" :disabled="!paintModeEnabled">
              <option value="rock">Rock</option>
              <option value="wall">Wall</option>
              <option value="tree">Tree</option>
            </select>
          </div>

          <div v-if="brushMode === 'building'" class="control-group">
            <label for="building-type">Building Type</label>
            <select id="building-type" v-model="selectedBuilding" :disabled="!paintModeEnabled">
              <option value="goldmine">Goldmine</option>
              <option value="townhall">Townhall</option>
              <option value="spawn-point">Spawn Point</option>
              <option value="enemy-spawnpoint">Enemy Spawnpoint</option>
            </select>
          </div>

          <div v-if="brushMode === 'building' && selectedBuilding === 'spawn-point'" class="control-group spawn-point-config">
            <label for="spawn-point-townhall">Townhall</label>
            <select
              id="spawn-point-townhall"
              v-model="selectedSpawnTownhallId"
              :disabled="!paintModeEnabled || townhallOptions.length === 0"
            >
              <option value="">Nearest / Unassigned</option>
              <option v-for="townhall in townhallOptions" :key="townhall.id" :value="townhall.id">
                {{ townhall.label }}
              </option>
            </select>
            <label for="spawn-point-fill-order">Fill Order</label>
            <input
              id="spawn-point-fill-order"
              v-model.number="spawnPointFillOrder"
              type="number"
              min="0"
              :disabled="!paintModeEnabled"
            />
            <label>Starting Units</label>
            <div
              v-for="entry in spawnPointLoadout"
              :key="entry.id"
              class="spawn-point-loadout-row"
            >
              <select v-model="entry.unitType" :disabled="!paintModeEnabled">
                <option v-for="unit in playerSpawnUnits" :key="unit.type" :value="unit.type">
                  {{ unit.label }}
                </option>
              </select>
              <input
                v-model.number="entry.count"
                type="number"
                min="1"
                max="20"
                :disabled="!paintModeEnabled"
              />
              <button
                type="button"
                class="spawn-point-row-button"
                @click="removeSpawnPointLoadoutEntry(entry.id)"
                :disabled="!paintModeEnabled || spawnPointLoadout.length <= 1"
              >
                Remove
              </button>
            </div>
            <button
              type="button"
              class="spawn-point-row-button"
              @click="addSpawnPointLoadoutEntry()"
              :disabled="!paintModeEnabled"
            >
              Add Unit Group
            </button>
          </div>

          <div v-if="brushMode === 'building' && selectedBuilding === 'enemy-spawnpoint'" class="control-group enemy-spawn-config">
            <label for="enemy-unit-type">Unit Type</label>
            <select
              id="enemy-unit-type"
              v-model="enemyUnitType"
              :disabled="!paintModeEnabled"
            >
              <option value="raider">Raider</option>
            </select>
            <label for="enemy-wave-mode">Spawn Timing</label>
            <select id="enemy-wave-mode" v-model="enemyWaveMode" :disabled="!paintModeEnabled">
              <option value="always">Always (legacy)</option>
              <option value="specific">Specific Wave</option>
              <option value="repeating">Every Wave From</option>
            </select>
            <template v-if="enemyWaveMode !== 'always'">
              <label for="enemy-wave-number">{{ enemyWaveMode === 'specific' ? 'Wave Number' : 'Starting Wave' }}</label>
              <input
                id="enemy-wave-number"
                v-model.number="enemyWaveNumber"
                type="number"
                min="1"
                max="999"
                :disabled="!paintModeEnabled"
              />
            </template>
            <label for="enemy-spawn-delay">Spawn Delay (sec)</label>
            <input
              id="enemy-spawn-delay"
              v-model.number="enemySpawnDelay"
              type="number"
              min="0"
              max="3600"
              :disabled="!paintModeEnabled"
            />
            <label for="enemy-spawn-interval">Spawn Interval (sec)</label>
            <input
              id="enemy-spawn-interval"
              v-model.number="enemySpawnInterval"
              type="number"
              min="1"
              max="3600"
              :disabled="!paintModeEnabled"
            />
            <label for="enemy-spawn-count">Spawn Count</label>
            <input
              id="enemy-spawn-count"
              v-model.number="enemySpawnCount"
              type="number"
              min="1"
              max="20"
              :disabled="!paintModeEnabled"
            />
          </div>

          <div class="hint-list">
            <div>`Wheel` zooms</div>
            <div>`Middle mouse` pans</div>
            <div>`Space + left drag` pans</div>
            <div>`Left click/drag` paints when enabled</div>
            <div>`Hold Control` temporarily erases</div>
            <div>`Erase` removes buildings too</div>
          </div>
        </div>
      </section>

      <section class="editor-section" :class="{ 'editor-section--open': openSection === 'export' }">
        <button type="button" class="editor-section__summary" @click="toggleSection('export')">
          Export
        </button>
        <div v-if="openSection === 'export'" class="editor-section__body">
          <div class="export-actions">
            <button type="button" @click="recenterCamera">Recenter</button>
            <button type="button" @click="clearMap">Clear Map</button>
            <button type="button" @click="copyExport">{{ copiedLabel }}</button>
          </div>

          <textarea
            class="export-box"
            :value="serializedMap"
            readonly
            spellcheck="false"
          ></textarea>
        </div>
      </section>
    </div>

    <div class="editor-preview">
      <div class="preview-header">
        <span>{{ paintModeEnabled ? 'Paint mode armed' : 'Navigation mode' }}</span>
        <span>{{ hoverLabel }}</span>
      </div>

      <div class="canvas-frame">
        <canvas ref="canvas" class="editor-canvas"></canvas>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { fetchMapCatalog, fetchMapCatalogFile, fetchUnitDefs } from '@/game/maps/catalog'
import type {
  BuildingType,
  JsonObject,
  MapCatalogEntry,
  MapCatalogFile,
  MapConfig,
  ObstacleType,
  TerrainType,
  TileSheet,
  UnitType,
} from '@/game/network/protocol'
import { Camera } from '@/game/rendering/Camera'
import {
  DEFAULT_GRASS_COLOR,
  MAP_EDITOR_PRESETS,
  createEditorMapConfig,
  getBuildingColor,
  getObstacleColor,
  getTerrainColor,
  resizeMapConfig,
  setBuildingTile,
  setObstacleTile,
  setTerrainTile,
  setTilePaint,
} from '@/game/maps/mapConfig'
import {
  DEFAULT_TILE_PRESETS,
  drawTerrainTile,
  GROUND_TILE_COORDS,
  TERRAIN_TILE_COORDS,
  TILE_SHEET_NAMES,
  getSheetImage,
  getSheetTileSize,
  isTerrainTilesetReady,
  onSheetReady,
} from '@/game/rendering/terrainTileset'
import { getBuildingSprite } from '@/game/rendering/buildingSprites'
import { BUILDING_DEF_MAP } from '@/game/maps/buildingDefs'

const model = defineModel<MapConfig>({ required: true })
type SpawnLoadoutEntry = { id: number; unitType: UnitType; count: number }

const canvas = ref<HTMLCanvasElement | null>(null)
const tilePickerCanvas = ref<HTMLCanvasElement | null>(null)

const TILE_PICKER_SCALE = 2
const brushMode = ref<'terrain' | 'tile' | 'obstacle' | 'building' | 'erase'>('terrain')
const brushSize = ref<1 | 3 | 5 | 7>(1)
const selectedTerrain = ref<TerrainType>('grass')
const selectedObstacle = ref<ObstacleType>('rock')
const selectedBuilding = ref<BuildingType>('goldmine')
const selectedTileSheet = ref<TileSheet>('floors')
const selectedTileCoord = ref<{ sx: number; sy: number } | null>(null)
const selectedSpawnTownhallId = ref('')
const playerSpawnUnits = ref<Array<{ type: UnitType; label: string }>>([
  { type: 'worker', label: 'Worker' },
  { type: 'soldier', label: 'Soldier' },
])
const spawnPointLoadout = ref<SpawnLoadoutEntry[]>([{ id: 1, unitType: 'worker', count: 3 }])
const spawnPointFillOrder = ref(0)
const enemySpawnDelay = ref(0)
const enemySpawnInterval = ref(10)
const enemySpawnCount = ref(1)
const enemyUnitType = ref('raider')
const enemyWaveMode = ref<'always' | 'specific' | 'repeating'>('always')
const enemyWaveNumber = ref(1)
const draftCols = ref(model.value.gridCols)
const draftRows = ref(model.value.gridRows)
const copiedLabel = ref('Copy Export')
const hoverLabel = ref('Hover a tile')
const paintModeEnabled = ref(false)
const openSection = ref<'setup' | 'paint' | 'export' | null>('paint')
const isControlHeld = ref(false)
const availableMaps = ref<MapCatalogEntry[]>([])
const selectedLoadMapId = ref('')
const isLoadingMapCatalog = ref(false)
const isLoadingSelectedMap = ref(false)
const mapLoadError = ref('')

const camera = new Camera()
let resizeObserver: ResizeObserver | null = null
let animationFrameId = 0
let isLeftMouseDown = false
let isMiddleMouseDown = false
let isSpaceHeld = false
let isSpacePanning = false
let isPainting = false
let lastMouseX = 0
let lastMouseY = 0
let lastPaintKey = ''
let nextSpawnLoadoutId = 2

watch(
  model,
  (nextMap) => {
    draftCols.value = nextMap.gridCols
    draftRows.value = nextMap.gridRows
    clampCamera()
  },
  { deep: true },
)

const exportedCatalogFile = computed<MapCatalogFile>(() => ({
  id: model.value.id,
  name: model.value.name,
  description: model.value.description,
  sortOrder: 1000,
  map: (({ id: _id, name: _name, description: _description, ...map }) => map)(model.value),
}))

const serializedMap = computed(() => JSON.stringify(exportedCatalogFile.value, null, 2))
const activeBrushMode = computed(() =>
  isControlHeld.value ? 'erase' : brushMode.value,
)
const townhallOptions = computed(() =>
  model.value.buildings
    .filter((building) => building.buildingType === 'townhall')
    .map((building) => ({
      id: building.id,
      label: `${building.id} (${building.x}, ${building.y})`,
    })),
)

const defaultGroundName = computed<'grass' | 'dirt'>(() => {
  const current = model.value.defaultTile
  if (!current) return 'grass'
  for (const name of ['grass', 'dirt'] as const) {
    const preset = DEFAULT_TILE_PRESETS[name]
    if (
      current.sheet === preset.sheet &&
      current.sx === preset.sx &&
      current.sy === preset.sy
    ) {
      return name
    }
  }
  return 'grass'
})

function onDefaultGroundChange(event: Event) {
  const value = (event.target as HTMLSelectElement).value as 'grass' | 'dirt'
  model.value = { ...model.value, defaultTile: { ...DEFAULT_TILE_PRESETS[value] } }
}

const eraseCursor = `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='24' height='24' viewBox='0 0 24 24'%3E%3Cg stroke='%23f8fafc' stroke-width='2.6' stroke-linecap='round'%3E%3Cpath d='M6 6 L18 18'/%3E%3Cpath d='M18 6 L6 18'/%3E%3C/g%3E%3Cg stroke='%230f172a' stroke-width='1.2' stroke-linecap='round'%3E%3Cpath d='M6 6 L18 18'/%3E%3Cpath d='M18 6 L6 18'/%3E%3C/g%3E%3C/svg%3E") 12 12, crosshair`

function getCanvasCursor() {
  if (isSpaceHeld) return 'grab'
  if (!paintModeEnabled.value) return 'default'
  if (isControlHeld.value) return eraseCursor
  return 'crosshair'
}

function toggleSection(section: 'setup' | 'paint' | 'export') {
  openSection.value = openSection.value === section ? null : section
}

function setWaveConfig(field: 'totalWaves' | 'prepDuration' | 'waveDuration', value: number) {
  const current = model.value.waveConfig ?? {}
  const updated = { ...current, [field]: value }
  // Drop waveConfig entirely if all fields are zero/absent — keeps the export clean
  const hasAny = (updated.totalWaves ?? 0) > 0 || (updated.prepDuration ?? 0) > 0 || (updated.waveDuration ?? 0) > 0
  model.value = { ...model.value, waveConfig: hasAny ? updated : undefined }
}

function applyGridSize() {
  model.value = resizeMapConfig(model.value, draftCols.value, draftRows.value)
  recenterCamera()
}

function applyPreset(cols: number, rows: number) {
  draftCols.value = cols
  draftRows.value = rows
  applyGridSize()
}

function clearMap() {
  model.value = createEditorMapConfig(model.value.gridCols, model.value.gridRows)
}

function addSpawnPointLoadoutEntry() {
  const defaultUnitType = playerSpawnUnits.value[0]?.type ?? 'worker'
  spawnPointLoadout.value = [
    ...spawnPointLoadout.value,
    { id: nextSpawnLoadoutId++, unitType: defaultUnitType, count: 1 },
  ]
}

function removeSpawnPointLoadoutEntry(entryId: number) {
  if (spawnPointLoadout.value.length <= 1) return
  spawnPointLoadout.value = spawnPointLoadout.value.filter((entry) => entry.id !== entryId)
}

async function loadAvailableMaps() {
  isLoadingMapCatalog.value = true
  mapLoadError.value = ''

  try {
    const maps = await fetchMapCatalog()
    availableMaps.value = maps
    if (!selectedLoadMapId.value) {
      selectedLoadMapId.value = maps[0]?.id ?? ''
    }
  } catch (error) {
    mapLoadError.value =
      error instanceof Error ? error.message : 'Failed to load saved maps.'
  } finally {
    isLoadingMapCatalog.value = false
  }
}

async function loadSelectedMapIntoEditor() {
  if (!selectedLoadMapId.value) return

  isLoadingSelectedMap.value = true
  mapLoadError.value = ''

  try {
    const catalogFile = await fetchMapCatalogFile(selectedLoadMapId.value)
    model.value = createEditorMapConfig(
      catalogFile.map.gridCols,
      catalogFile.map.gridRows,
      catalogFile.map,
    )
    draftCols.value = model.value.gridCols
    draftRows.value = model.value.gridRows
    recenterCamera()
  } catch (error) {
    mapLoadError.value =
      error instanceof Error ? error.message : 'Failed to load selected map.'
  } finally {
    isLoadingSelectedMap.value = false
  }
}

function recenterCamera() {
  const targetCanvas = canvas.value
  if (!targetCanvas) return

  resizeCanvas()
  camera.centerOn(
    model.value.width / 2,
    model.value.height / 2,
    targetCanvas.width,
    targetCanvas.height,
    model.value.width,
    model.value.height,
  )
}

async function copyExport() {
  try {
    await navigator.clipboard.writeText(serializedMap.value)
    copiedLabel.value = 'Copied'
    window.setTimeout(() => {
      copiedLabel.value = 'Copy Export'
    }, 1400)
  } catch {
    copiedLabel.value = 'Copy Failed'
  }
}

function resizeCanvas() {
  const targetCanvas = canvas.value
  if (!targetCanvas) return

  const width = targetCanvas.clientWidth
  const height = targetCanvas.clientHeight
  if (!width || !height) return

  targetCanvas.width = width
  targetCanvas.height = height
  clampCamera()
}

function clampCamera() {
  const targetCanvas = canvas.value
  if (!targetCanvas) return

  camera.clamp(
    targetCanvas.width,
    targetCanvas.height,
    model.value.width,
    model.value.height,
  )
}

function getPointerScreenPosition(event: MouseEvent | WheelEvent) {
  const rect = canvas.value?.getBoundingClientRect()
  if (!rect) {
    return { x: 0, y: 0 }
  }

  return {
    x: event.clientX - rect.left,
    y: event.clientY - rect.top,
  }
}

function updateHoverLabel(screenX: number, screenY: number) {
  const cell = getGridCellAtScreen(screenX, screenY)
  if (!cell) {
    hoverLabel.value = 'Outside map'
    return
  }

  const terrain = getTerrainAt(cell.x, cell.y) ?? 'empty'
  const obstacle = getObstacleAt(cell.x, cell.y) ?? 'none'
  const building = getBuildingAt(cell.x, cell.y)
  const buildingLabel = building ? building.buildingType : 'none'
  hoverLabel.value = `(${cell.x}, ${cell.y}) terrain: ${terrain}, obstacle: ${obstacle}, building: ${buildingLabel}`
}

function getGridCellAtScreen(screenX: number, screenY: number) {
  const world = camera.screenToWorld(screenX, screenY)
  const x = Math.floor(world.x / model.value.cellSize)
  const y = Math.floor(world.y / model.value.cellSize)

  if (x < 0 || y < 0 || x >= model.value.gridCols || y >= model.value.gridRows) {
    return null
  }

  return { x, y }
}

function getBrushCells(cx: number, cy: number, size: number): Array<{ x: number; y: number }> {
  const half = Math.floor(size / 2)
  const { gridCols, gridRows } = model.value
  const cells: Array<{ x: number; y: number }> = []
  for (let dy = -half; dy <= half; dy++) {
    for (let dx = -half; dx <= half; dx++) {
      const x = cx + dx
      const y = cy + dy
      if (x < 0 || y < 0 || x >= gridCols || y >= gridRows) continue
      cells.push({ x, y })
    }
  }
  return cells
}

function paintAtScreen(screenX: number, screenY: number) {
  const cell = getGridCellAtScreen(screenX, screenY)
  if (!cell) return

  const paintKey = `${cell.x}:${cell.y}:${activeBrushMode.value}:${brushSize.value}`
  if (paintKey === lastPaintKey) return
  lastPaintKey = paintKey

  // Buildings ignore brush size — placement is driven by the building footprint.
  if (activeBrushMode.value === 'building') {
    paintBuildingAt(cell.x, cell.y)
    return
  }

  const cells = getBrushCells(cell.x, cell.y, brushSize.value)

  if (activeBrushMode.value === 'erase') {
    let next = model.value
    for (const c of cells) {
      next = setTilePaint(
        setBuildingTile(
          setObstacleTile(setTerrainTile(next, c.x, c.y, null), c.x, c.y, null),
          c.x,
          c.y,
          null,
        ),
        c.x,
        c.y,
        null,
      )
    }
    model.value = next
    return
  }

  if (activeBrushMode.value === 'terrain') {
    let next = model.value
    for (const c of cells) {
      next = setTerrainTile(next, c.x, c.y, selectedTerrain.value)
    }
    model.value = next
    return
  }

  if (activeBrushMode.value === 'tile') {
    if (!selectedTileCoord.value) return
    let next = model.value
    for (const c of cells) {
      next = setTilePaint(next, c.x, c.y, {
        sheet: selectedTileSheet.value,
        sx: selectedTileCoord.value.sx,
        sy: selectedTileCoord.value.sy,
      })
    }
    model.value = next
    return
  }

  // Obstacle brush (default fall-through).
  let next = model.value
  for (const c of cells) {
    next = setObstacleTile(next, c.x, c.y, selectedObstacle.value)
  }
  model.value = next
}

function paintBuildingAt(cx: number, cy: number) {
  let metadata: JsonObject | undefined

  if (selectedBuilding.value === 'spawn-point') {
    metadata = {
      townhallId: selectedSpawnTownhallId.value || null,
      fillOrder: Math.round(spawnPointFillOrder.value || 0),
      spawnUnits: spawnPointLoadout.value.map((entry) => ({
        unitType: entry.unitType,
        count: Math.max(1, Math.round(entry.count || 1)),
      })),
    }
  } else if (selectedBuilding.value === 'enemy-spawnpoint') {
    metadata = {
      ...(enemyWaveMode.value === 'specific' ? { waveNumber: enemyWaveNumber.value } : {}),
      ...(enemyWaveMode.value === 'repeating' ? { startingWave: enemyWaveNumber.value } : {}),
      spawnDelaySeconds: enemySpawnDelay.value,
      spawnIntervalSeconds: enemySpawnInterval.value,
      spawnCount: enemySpawnCount.value,
      unitType: enemyUnitType.value,
    }
  }

  model.value = setBuildingTile(model.value, cx, cy, selectedBuilding.value, metadata)
}

function getTerrainAt(x: number, y: number) {
  return model.value.terrain.find((tile) => tile.x === x && tile.y === y)?.terrain
}

function getObstacleAt(x: number, y: number) {
  return model.value.obstacles.find((tile) => tile.x === x && tile.y === y)?.obstacle
}

function getBuildingAt(x: number, y: number) {
  return model.value.buildings.find(
    (building) =>
      x >= building.x &&
      x < building.x + building.width &&
      y >= building.y &&
      y < building.y + building.height,
  )
}

function onMouseDown(event: MouseEvent) {
  const targetCanvas = canvas.value
  if (!targetCanvas) return

  const screen = getPointerScreenPosition(event)
  lastMouseX = screen.x
  lastMouseY = screen.y
  updateHoverLabel(screen.x, screen.y)

  if (event.button === 1) {
    event.preventDefault()
    isMiddleMouseDown = true
    return
  }

  // Right-click in Tile brush mode: erase only painted tiles under the brush.
  if (event.button === 2 && paintModeEnabled.value && brushMode.value === 'tile') {
    event.preventDefault()
    const cell = getGridCellAtScreen(screen.x, screen.y)
    if (cell) {
      let next = model.value
      for (const c of getBrushCells(cell.x, cell.y, brushSize.value)) {
        next = setTilePaint(next, c.x, c.y, null)
      }
      model.value = next
    }
    return
  }

  if (event.button !== 0) return

  isLeftMouseDown = true

  if (isSpaceHeld) {
    isSpacePanning = true
    targetCanvas.style.cursor = 'grabbing'
    return
  }

  if (!paintModeEnabled.value) return

  isPainting = true
  lastPaintKey = ''
  paintAtScreen(screen.x, screen.y)
}

function onMouseMove(event: MouseEvent) {
  const targetCanvas = canvas.value
  if (!targetCanvas) return

  const screen = getPointerScreenPosition(event)
  updateHoverLabel(screen.x, screen.y)

  if (isMiddleMouseDown || (isLeftMouseDown && isSpacePanning)) {
    const dx = screen.x - lastMouseX
    const dy = screen.y - lastMouseY

    camera.pan(-dx / camera.zoom, -dy / camera.zoom)
    clampCamera()

    lastMouseX = screen.x
    lastMouseY = screen.y
    return
  }

  if (isPainting && paintModeEnabled.value) {
    paintAtScreen(screen.x, screen.y)
  }
}

function onMouseUp(event: MouseEvent) {
  if (event.button === 1) {
    isMiddleMouseDown = false
    return
  }

  if (event.button !== 0) return

  isLeftMouseDown = false
  isPainting = false
  lastPaintKey = ''

  if (isSpacePanning) {
    isSpacePanning = false
    if (canvas.value) {
      canvas.value.style.cursor = isSpaceHeld ? 'grab' : (paintModeEnabled.value ? 'crosshair' : 'default')
    }
  }
}

function onWheel(event: WheelEvent) {
  event.preventDefault()
  const screen = getPointerScreenPosition(event)
  camera.adjustZoom(event.deltaY, screen.x, screen.y)
  clampCamera()
  updateHoverLabel(screen.x, screen.y)
}

function onMouseLeave() {
  hoverLabel.value = 'Outside map'
}

function onKeyDown(event: KeyboardEvent) {
  if (event.key === 'Control') {
    isControlHeld.value = true
    if (canvas.value && !isSpacePanning) {
      canvas.value.style.cursor = getCanvasCursor()
    }
  }

  if (event.code !== 'Space') return
  isSpaceHeld = true
  if (canvas.value && !isSpacePanning) {
    canvas.value.style.cursor = getCanvasCursor()
  }
  event.preventDefault()
}

function onKeyUp(event: KeyboardEvent) {
  if (event.key === 'Control') {
    isControlHeld.value = false
    if (canvas.value && !isSpacePanning) {
      canvas.value.style.cursor = getCanvasCursor()
    }
  }

  if (event.code !== 'Space') return
  isSpaceHeld = false
  isSpacePanning = false
  if (canvas.value) {
    canvas.value.style.cursor = getCanvasCursor()
  }
  event.preventDefault()
}

function render() {
  const targetCanvas = canvas.value
  const ctx = targetCanvas?.getContext('2d')

  if (targetCanvas && ctx) {
    ctx.clearRect(0, 0, targetCanvas.width, targetCanvas.height)
    ctx.fillStyle = '#0a0a0a'
    ctx.fillRect(0, 0, targetCanvas.width, targetCanvas.height)

    ctx.save()
    ctx.scale(camera.zoom, camera.zoom)
    ctx.translate(-camera.x, -camera.y)

    drawMapBackground(ctx)
    drawGrid(ctx)
    drawMapBounds(ctx)

    ctx.restore()
  }

  animationFrameId = requestAnimationFrame(render)
}

function drawMapBackground(ctx: CanvasRenderingContext2D) {
  const cellSize = model.value.cellSize
  const { gridCols, gridRows } = model.value
  const tilesetReady = isTerrainTilesetReady()
  const groundCoord = model.value.defaultTile ?? GROUND_TILE_COORDS

  if (tilesetReady) {
    ctx.imageSmoothingEnabled = false
    for (let gy = 0; gy < gridRows; gy++) {
      for (let gx = 0; gx < gridCols; gx++) {
        drawTerrainTile(ctx, groundCoord, gx * cellSize, gy * cellSize, cellSize)
      }
    }
  } else {
    ctx.fillStyle = DEFAULT_GRASS_COLOR
    ctx.fillRect(0, 0, model.value.width, model.value.height)
  }

  if (tilesetReady && model.value.tiles) {
    for (const tile of model.value.tiles) {
      drawTerrainTile(
        ctx,
        { sheet: tile.sheet, sx: tile.sx, sy: tile.sy },
        tile.x * cellSize,
        tile.y * cellSize,
        cellSize,
      )
    }
  }

  for (const tile of model.value.terrain) {
    const coords = TERRAIN_TILE_COORDS[tile.terrain]
    if (tilesetReady && coords) {
      drawTerrainTile(ctx, coords, tile.x * cellSize, tile.y * cellSize, cellSize)
    } else {
      ctx.fillStyle = getTerrainColor(tile.terrain)
      ctx.fillRect(tile.x * cellSize, tile.y * cellSize, cellSize, cellSize)
    }
  }

  for (const tile of model.value.obstacles) {
    const worldX = tile.x * cellSize
    const worldY = tile.y * cellSize
    const inset = cellSize * 0.14

    ctx.fillStyle = getObstacleColor(tile.obstacle)
    ctx.fillRect(
      worldX + inset,
      worldY + inset,
      cellSize - inset * 2,
      cellSize - inset * 2,
    )

    ctx.strokeStyle = 'rgba(15, 23, 42, 0.75)'
    ctx.lineWidth = 2 / camera.zoom
    ctx.strokeRect(
      worldX + inset,
      worldY + inset,
      cellSize - inset * 2,
      cellSize - inset * 2,
    )
  }

  for (const building of model.value.buildings) {
    if (building.buildingType === 'enemy-spawnpoint') {
      drawEditorEnemySpawnpoint(ctx, building, cellSize)
      continue
    }

    const worldX = building.x * cellSize
    const worldY = building.y * cellSize
    const width = building.width * cellSize
    const height = building.height * cellSize
    const def = BUILDING_DEF_MAP.get(building.buildingType)
    const renderDef = def?.render
    const sprite = getBuildingSprite(building.buildingType)

    ctx.save()
    ctx.globalAlpha = building.visible ? 1 : 0.6

    if (sprite) {
      ctx.imageSmoothingEnabled = false
      ctx.drawImage(sprite, worldX, worldY, width, height)
    } else if (renderDef) {
      const fill = def?.color ?? getBuildingColor(building.buildingType, building.occupied)
      for (const layer of renderDef.layers) {
        ctx.fillStyle = layer.color === 'player' ? fill : layer.color
        if (!('kind' in layer) || layer.kind === 'rect') {
          ctx.fillRect(
            worldX + layer.x * cellSize,
            worldY + layer.y * cellSize,
            layer.w * cellSize,
            layer.h * cellSize,
          )
        } else if (layer.kind === 'tri') {
          const s = cellSize / 6
          const tlX = worldX + layer.cx * cellSize + layer.sc * s
          const tlY = worldY + layer.cy * cellSize + layer.sr * s
          const bslash = (layer.sc + layer.sr) % 2 === 1
          ctx.beginPath()
          if (!bslash) {
            if (layer.h === 0) { ctx.moveTo(tlX, tlY); ctx.lineTo(tlX + s, tlY); ctx.lineTo(tlX, tlY + s) }
            else { ctx.moveTo(tlX + s, tlY); ctx.lineTo(tlX + s, tlY + s); ctx.lineTo(tlX, tlY + s) }
          } else {
            if (layer.h === 0) { ctx.moveTo(tlX, tlY); ctx.lineTo(tlX + s, tlY); ctx.lineTo(tlX + s, tlY + s) }
            else { ctx.moveTo(tlX, tlY); ctx.lineTo(tlX, tlY + s); ctx.lineTo(tlX + s, tlY + s) }
          }
          ctx.closePath()
          ctx.fill()
        }
      }
    } else {
      const inset = cellSize * 0.18
      ctx.fillStyle = getBuildingColor(building.buildingType, building.occupied)
      ctx.fillRect(worldX + inset, worldY + inset, width - inset * 2, height - inset * 2)
    }

    if (!building.visible) {
      ctx.strokeStyle = 'rgba(226, 232, 240, 0.85)'
      ctx.lineWidth = 2 / camera.zoom
      ctx.setLineDash([10 / camera.zoom, 6 / camera.zoom])
      ctx.strokeRect(worldX, worldY, width, height)
    }

    ctx.restore()
  }
}

function drawEditorEnemySpawnpoint(
  ctx: CanvasRenderingContext2D,
  building: { x: number; y: number; width: number; height: number },
  cellSize: number,
) {
  const worldX = building.x * cellSize
  const worldY = building.y * cellSize
  const width = building.width * cellSize
  const height = building.height * cellSize

  ctx.save()
  ctx.fillStyle = 'rgba(153, 27, 27, 0.45)'
  ctx.fillRect(worldX, worldY, width, height)
  ctx.strokeStyle = '#fca5a5'
  ctx.lineWidth = 2 / camera.zoom
  ctx.setLineDash([8 / camera.zoom, 4 / camera.zoom])
  ctx.strokeRect(worldX, worldY, width, height)
  ctx.restore()
}

function drawGrid(ctx: CanvasRenderingContext2D) {
  const targetCanvas = canvas.value
  if (!targetCanvas) return

  const gridSize = model.value.cellSize
  const worldWidth = targetCanvas.width / camera.zoom
  const worldHeight = targetCanvas.height / camera.zoom
  const viewStartX = camera.x
  const viewEndX = camera.x + worldWidth
  const viewStartY = camera.y
  const viewEndY = camera.y + worldHeight

  const startX = Math.max(0, Math.floor(viewStartX / gridSize) * gridSize)
  const endX = Math.min(model.value.width, viewEndX + gridSize)
  const startY = Math.max(0, Math.floor(viewStartY / gridSize) * gridSize)
  const endY = Math.min(model.value.height, viewEndY + gridSize)

  ctx.strokeStyle = '#1f1f1f'
  ctx.lineWidth = 1 / camera.zoom

  for (let x = startX; x < endX; x += gridSize) {
    ctx.beginPath()
    ctx.moveTo(x, startY)
    ctx.lineTo(x, endY)
    ctx.stroke()
  }

  for (let y = startY; y < endY; y += gridSize) {
    ctx.beginPath()
    ctx.moveTo(startX, y)
    ctx.lineTo(endX, y)
    ctx.stroke()
  }
}

function renderTilePicker() {
  const canvasEl = tilePickerCanvas.value
  if (!canvasEl) return
  const img = getSheetImage(selectedTileSheet.value)
  if (!img) {
    onSheetReady(selectedTileSheet.value, renderTilePicker)
    return
  }

  const scale = TILE_PICKER_SCALE
  canvasEl.width = img.naturalWidth * scale
  canvasEl.height = img.naturalHeight * scale

  const ctx = canvasEl.getContext('2d')
  if (!ctx) return

  ctx.imageSmoothingEnabled = false
  ctx.clearRect(0, 0, canvasEl.width, canvasEl.height)
  ctx.drawImage(img, 0, 0, canvasEl.width, canvasEl.height)

  // Highlight the selected tile, if any.
  if (selectedTileCoord.value) {
    const { sx, sy } = selectedTileCoord.value
    const tileSize = getSheetTileSize(selectedTileSheet.value)
    ctx.strokeStyle = '#facc15'
    ctx.lineWidth = 2
    ctx.strokeRect(sx * scale, sy * scale, tileSize * scale, tileSize * scale)
  }
}

function onTilePickerClick(event: MouseEvent) {
  const canvasEl = tilePickerCanvas.value
  if (!canvasEl) return
  const rect = canvasEl.getBoundingClientRect()
  // CSS size may differ from canvas pixel size; convert through the ratio.
  const cssToPx = canvasEl.width / rect.width
  const px = (event.clientX - rect.left) * cssToPx
  const py = (event.clientY - rect.top) * cssToPx
  const scale = TILE_PICKER_SCALE
  const tileSize = getSheetTileSize(selectedTileSheet.value)
  const sx = Math.floor(px / (tileSize * scale)) * tileSize
  const sy = Math.floor(py / (tileSize * scale)) * tileSize
  selectedTileCoord.value = { sx, sy }
  renderTilePicker()
}

function drawMapBounds(ctx: CanvasRenderingContext2D) {
  ctx.strokeStyle = '#444'
  ctx.lineWidth = 2 / camera.zoom
  ctx.strokeRect(0, 0, model.value.width, model.value.height)
}

// Re-render the tile picker whenever it becomes visible or the sheet changes.
// Wait a tick so the v-if'd canvas is mounted before we try to draw.
watch(
  [brushMode, selectedTileSheet],
  async () => {
    if (brushMode.value !== 'tile') return
    await new Promise((resolve) => requestAnimationFrame(resolve))
    renderTilePicker()
  },
  { immediate: true },
)

// Clear the selected tile coord when switching sheets — coords aren't portable
// across sheets (different tile sizes, different content).
watch(selectedTileSheet, () => {
  selectedTileCoord.value = null
})

onMounted(() => {
  const targetCanvas = canvas.value
  if (!targetCanvas) return

  resizeCanvas()
  recenterCamera()
  targetCanvas.style.cursor = getCanvasCursor()
  void loadAvailableMaps()
  void fetchUnitDefs()
    .then((defs) => {
      playerSpawnUnits.value = defs
        .filter((def) => def.trainLabel)
        .map((def) => ({ type: def.type as UnitType, label: def.name }))
      const defaultUnitType = playerSpawnUnits.value[0]?.type ?? 'worker'
      spawnPointLoadout.value = spawnPointLoadout.value.map((entry) => ({
        ...entry,
        unitType: playerSpawnUnits.value.some((unit) => unit.type === entry.unitType)
          ? entry.unitType
          : defaultUnitType,
      }))
      if (spawnPointLoadout.value.length === 0) {
        spawnPointLoadout.value = [{ id: nextSpawnLoadoutId++, unitType: defaultUnitType, count: 3 }]
      }
    })
    .catch(() => {})

  targetCanvas.addEventListener('mousedown', onMouseDown)
  targetCanvas.addEventListener('mousemove', onMouseMove)
  targetCanvas.addEventListener('mouseleave', onMouseLeave)
  targetCanvas.addEventListener('wheel', onWheel, { passive: false })
  targetCanvas.addEventListener('contextmenu', (event) => event.preventDefault())

  window.addEventListener('mouseup', onMouseUp)
  window.addEventListener('keydown', onKeyDown)
  window.addEventListener('keyup', onKeyUp)
  window.addEventListener('resize', resizeCanvas)

  if (typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(() => {
      resizeCanvas()
    })
    resizeObserver.observe(targetCanvas)
  }

  render()
})

watch(paintModeEnabled, () => {
  if (!canvas.value || isSpacePanning) return
  canvas.value.style.cursor = getCanvasCursor()
})

watch(
  townhallOptions,
  (options) => {
    if (selectedSpawnTownhallId.value && options.some((option) => option.id === selectedSpawnTownhallId.value)) {
      return
    }
    selectedSpawnTownhallId.value = ''
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  if (canvas.value) {
    canvas.value.removeEventListener('mousedown', onMouseDown)
    canvas.value.removeEventListener('mousemove', onMouseMove)
    canvas.value.removeEventListener('mouseleave', onMouseLeave)
    canvas.value.removeEventListener('wheel', onWheel)
  }

  window.removeEventListener('mouseup', onMouseUp)
  window.removeEventListener('keydown', onKeyDown)
  window.removeEventListener('keyup', onKeyUp)
  window.removeEventListener('resize', resizeCanvas)

  resizeObserver?.disconnect()
  resizeObserver = null

  cancelAnimationFrame(animationFrameId)
})
</script>

<style scoped>
.editor-shell {
  display: grid;
  grid-template-columns: minmax(210px, 250px) minmax(0, 1fr);
  grid-template-rows: minmax(0, 1fr);
  gap: 12px;
  align-items: stretch;
  width: 100%;
  height: 100%;
  min-height: 0;
}

.editor-controls,
.editor-preview {
  background: rgba(3, 8, 14, 0.86);
  border: 1px solid rgba(148, 163, 184, 0.22);
  border-radius: 16px;
  box-shadow: 0 24px 60px rgba(0, 0, 0, 0.26);
  backdrop-filter: blur(14px);
}

.editor-controls {
  min-height: 0;
  max-height: 100%;
  overflow-y: auto;
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  scrollbar-width: thin;
  scrollbar-color: rgba(148, 163, 184, 0.35) transparent;
}

.editor-controls::-webkit-scrollbar {
  width: 8px;
}

.editor-controls::-webkit-scrollbar-thumb {
  background: rgba(148, 163, 184, 0.35);
  border-radius: 4px;
}

.editor-controls::-webkit-scrollbar-thumb:hover {
  background: rgba(148, 163, 184, 0.55);
}

.editor-controls::-webkit-scrollbar-track {
  background: transparent;
}

.editor-section {
  border: 1px solid rgba(148, 163, 184, 0.18);
  border-radius: 12px;
  background: rgba(8, 14, 24, 0.55);
  overflow: clip;
  flex: 0 0 auto;
}

.editor-section--open {
  background: rgba(8, 14, 24, 0.72);
}

.editor-section__summary {
  width: 100%;
  border: 0;
  cursor: pointer;
  padding: 10px 12px;
  font-size: 0.78rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #f8fafc;
  text-align: left;
  background: linear-gradient(180deg, rgba(25, 35, 52, 0.92), rgba(14, 22, 36, 0.94));
}

.editor-section__summary::after {
  content: '+';
  float: right;
  color: #d7bb84;
}

.editor-section--open .editor-section__summary::after {
  content: '-';
}

.editor-section__body {
  display: grid;
  gap: 8px;
  padding: 10px;
}

.editor-title {
  font-size: 0.9rem;
  font-weight: 700;
  color: #f8fafc;
}

.editor-copy {
  margin: 0;
  color: rgba(226, 232, 240, 0.82);
  font-size: 0.75rem;
  line-height: 1.2;
}

.control-group {
  display: grid;
  gap: 4px;
}

.tile-picker-hint {
  font-size: 0.7rem;
  color: rgba(226, 232, 240, 0.72);
  padding: 2px 0;
}

.tile-picker {
  display: block;
  max-width: 100%;
  image-rendering: pixelated;
  border: 1px solid rgba(148, 163, 184, 0.24);
  border-radius: 4px;
  cursor: crosshair;
  background: #0a0a0a;
}

.control-group label,
.preview-header,
.summary-row,
.hint-list {
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.75rem;
}

.control-group input,
.control-group select,
.control-group textarea,
.apply-size,
.preset-row button,
.export-actions button,
.paint-toggle {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
}

.paint-toggle {
  font-weight: 700;
}

.paint-toggle--active {
  background: rgba(22, 101, 52, 0.95);
  border-color: rgba(74, 222, 128, 0.45);
}

.preset-row,
.export-actions,
.summary-row {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.summary-row span {
  padding: 4px 8px;
  border-radius: 999px;
  background: rgba(30, 41, 59, 0.72);
}

.hint-list {
  display: grid;
  gap: 2px;
}

.export-box {
  min-height: 104px;
  resize: vertical;
  border-radius: 12px;
  border: 1px solid rgba(148, 163, 184, 0.2);
  background: rgba(2, 6, 23, 0.88);
  color: #dbeafe;
  padding: 8px;
  font-family: Consolas, 'Courier New', monospace;
  font-size: 0.72rem;
}

.metadata-box {
  min-height: 52px;
  resize: vertical;
}

.spawn-point-config,
.enemy-spawn-config {
  border: 1px solid rgba(239, 68, 68, 0.3);
  border-radius: 8px;
  padding: 8px;
}

.spawn-point-config {
  background: rgba(8, 145, 178, 0.18);
  border-color: rgba(56, 189, 248, 0.35);
}

.spawn-point-loadout-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 70px auto;
  gap: 6px;
  align-items: center;
}

.spawn-point-row-button {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.72rem;
}

.enemy-spawn-config {
  background: rgba(127, 29, 29, 0.18);
}

.wave-config-block {
  display: grid;
  gap: 8px;
  padding: 8px;
  border-radius: 8px;
  border: 1px solid rgba(215, 187, 132, 0.25);
  background: rgba(58, 35, 18, 0.35);
}

.wave-config-title {
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #d7bb84;
}

.field-hint {
  font-weight: 400;
  opacity: 0.65;
  text-transform: none;
  letter-spacing: 0;
}

.editor-preview {
  min-height: 0;
  min-width: 0;
  padding: 12px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.preview-header {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
  font-size: 0.78rem;
}

.canvas-frame {
  flex: 1 1 auto;
  min-height: 0;
  overflow: hidden;
  border-radius: 14px;
  border: 1px solid rgba(68, 68, 68, 0.9);
  background: #0a0a0a;
}

.editor-canvas {
  width: 100%;
  height: 100%;
  display: block;
  background: #0a0a0a;
}

@media (max-width: 1100px) {
  .editor-shell {
    grid-template-columns: minmax(200px, 230px) minmax(0, 1fr);
  }

  .canvas-frame {
    height: 60vh;
  }
}

@media (max-width: 820px) {
  .editor-shell {
    grid-template-columns: 1fr;
  }

  .editor-preview {
    min-height: 0;
  }
}
</style>
