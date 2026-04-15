<template>
  <div class="spe">

    <!-- Mode tabs -->
    <div class="spe__tabs">
      <button class="spe__tab" :class="{ 'spe__tab--active': mode === 'building' }" @click="setMode('building')">
        Building
      </button>
      <button class="spe__tab" :class="{ 'spe__tab--active': mode === 'unit' }" @click="setMode('unit')">
        Unit
      </button>
    </div>

    <div class="spe__body">

      <!-- ── Left sidebar: def form ── -->
      <div class="spe__sidebar">

        <template v-if="mode === 'building'">
          <div class="spe__section-title">Definition</div>

          <div class="spe__field">
            <label>Type ID</label>
            <input v-model="bType" />
          </div>
          <div class="spe__field-row">
            <div class="spe__field">
              <label>Width (cells)</label>
              <input type="number" v-model.number="bWidth" min="1" max="6" />
            </div>
            <div class="spe__field">
              <label>Height (cells)</label>
              <input type="number" v-model.number="bHeight" min="1" max="6" />
            </div>
          </div>
          <div class="spe__field-row">
            <div class="spe__field">
              <label>Max HP</label>
              <input type="number" v-model.number="bMaxHp" min="1" />
            </div>
            <div class="spe__field">
              <label>Build Seconds</label>
              <input type="number" v-model.number="bBuildSeconds" min="0" />
            </div>
          </div>
          <div class="spe__field-row">
            <div class="spe__field">
              <label>Gold Cost</label>
              <input type="number" v-model.number="bGold" min="0" />
            </div>
            <div class="spe__field">
              <label>Wood Cost</label>
              <input type="number" v-model.number="bWood" min="0" />
            </div>
          </div>
          <div class="spe__field">
            <label>Label</label>
            <input v-model="bLabel" />
          </div>
          <div class="spe__field-row">
            <div class="spe__field">
              <label>Hotkey</label>
              <input v-model="bHotkey" maxlength="1" style="width: 48px" />
            </div>
            <div class="spe__field">
              <label>Minimap Color</label>
              <input type="color" v-model="bColor" class="spe__color-input" />
            </div>
          </div>
          <div class="spe__field">
            <label>Inset <span class="spe__hint">(cell units — for HP bar / overlay anchor)</span></label>
            <input type="number" v-model.number="bInset" min="0" max="0.49" step="0.01" />
          </div>

          <div class="spe__section-title" style="margin-top: 12px">Capabilities</div>
          <div class="spe__checklist">
            <label v-for="cap in ALL_BUILDING_CAPS" :key="cap">
              <input type="checkbox" :value="cap" v-model="bCapabilities" />
              <span>{{ cap }}</span>
            </label>
          </div>

          <div class="spe__section-title" style="margin-top: 12px">Spawn Unit Types</div>
          <div class="spe__checklist">
            <label v-for="ut in ALL_UNIT_TYPES" :key="ut">
              <input type="checkbox" :value="ut" v-model="bSpawnUnitTypes" />
              <span>{{ ut }}</span>
            </label>
          </div>
        </template>

        <template v-else>
          <div class="spe__section-title">Definition</div>

          <div class="spe__field">
            <label>Type ID</label>
            <input v-model="uType" />
          </div>
          <div class="spe__field">
            <label>Name</label>
            <input v-model="uName" />
          </div>
          <div class="spe__field-row">
            <div class="spe__field">
              <label>HP</label>
              <input type="number" v-model.number="uHp" min="1" />
            </div>
            <div class="spe__field">
              <label>Damage</label>
              <input type="number" v-model.number="uDamage" min="0" />
            </div>
          </div>
          <div class="spe__field-row">
            <div class="spe__field">
              <label>Attack Range</label>
              <input type="number" v-model.number="uAttackRange" min="0" />
            </div>
            <div class="spe__field">
              <label>Attack Speed</label>
              <input type="number" v-model.number="uAttackSpeed" step="0.1" min="0.1" />
            </div>
          </div>
          <div class="spe__field-row">
            <div class="spe__field">
              <label>Gold Cost</label>
              <input type="number" v-model.number="uGold" min="0" />
            </div>
            <div class="spe__field">
              <label>Wood Cost</label>
              <input type="number" v-model.number="uWood" min="0" />
            </div>
          </div>
          <div class="spe__field-row">
            <div class="spe__field">
              <label>Meat Cost</label>
              <input type="number" v-model.number="uMeatCost" min="0" />
            </div>
            <div class="spe__field">
              <label>Spawn Seconds</label>
              <input type="number" v-model.number="uSpawnSeconds" step="0.5" min="0" />
            </div>
          </div>
          <div class="spe__field">
            <label>Train Label</label>
            <input v-model="uTrainLabel" />
          </div>

          <div class="spe__section-title" style="margin-top: 12px">Capabilities</div>
          <div class="spe__checklist">
            <label v-for="cap in ALL_UNIT_CAPS" :key="cap">
              <input type="checkbox" :value="cap" v-model="uCapabilities" />
              <span>{{ cap }}</span>
            </label>
          </div>
        </template>

      </div>

      <!-- ── Center: canvas ── -->
      <div class="spe__main">

        <!-- Toolbar -->
        <div class="spe__toolbar">
          <template v-if="mode === 'building'">
            <span class="spe__toolbar-label">Paint Color</span>
            <button
              class="spe__player-btn"
              :class="{ 'spe__player-btn--active': paintMode === 'player' }"
              @click="paintMode = 'player'"
              title="Use owner/player color at runtime"
            >Player</button>
            <input
              type="color"
              v-model="paintCustomColor"
              class="spe__color-input"
              title="Custom color"
              @input="paintMode = 'custom'"
            />
            <div
              class="spe__active-color"
              :style="{ background: paintMode === 'player' ? '#3b82f6' : paintCustomColor }"
              :title="paintMode === 'player' ? 'player color' : paintCustomColor"
            />
            <template v-if="colorHistory.length > 0">
              <div class="spe__toolbar-divider" />
              <span class="spe__toolbar-label">Recent</span>
              <div
                v-for="c in colorHistory"
                :key="c"
                class="spe__history-swatch"
                :class="{ 'spe__history-swatch--active': paintMode === 'custom' && paintCustomColor === c }"
                :style="{ background: c }"
                :title="c"
                @click="selectHistoryColor(c)"
              />
            </template>
            <span class="spe__toolbar-hint">Left-click paint · Right-click erase (transparent)</span>
            <div class="spe__toolbar-divider" />
            <span class="spe__toolbar-label">Zoom</span>
            <button class="spe__btn spe__btn--sm" @click="adjustCanvasZoom(-0.25)">-</button>
            <input
              v-model.number="canvasZoom"
              class="spe__zoom-slider"
              type="range"
              min="0.5"
              max="4"
              step="0.25"
            />
            <button class="spe__btn spe__btn--sm" @click="adjustCanvasZoom(0.25)">+</button>
            <button class="spe__btn spe__btn--sm spe__btn--ghost" @click="resetCanvasZoom">100%</button>
            <span class="spe__toolbar-value">{{ canvasZoomPercent }}%</span>
          </template>
          <template v-else>
            <span class="spe__toolbar-label">Add Layer</span>
            <button class="spe__btn spe__btn--sm" @click="openAddShape('circle')">+ Circle</button>
            <button class="spe__btn spe__btn--sm" @click="openAddShape('poly')">+ Poly</button>
          </template>
        </div>

        <!-- Canvas -->
        <div class="spe__canvas-wrap">
          <canvas
            ref="drawCanvas"
            class="spe__canvas"
            :width="canvasWidth"
            :height="canvasHeight"
            :style="canvasStyle"
            @mousedown="onMouseDown"
            @mousemove="onMouseMove"
            @mouseup="onMouseUp"
            @mouseleave="onMouseLeave"
            @contextmenu.prevent
          />
        </div>

        <!-- Add-shape form (unit mode) -->
        <div v-if="showAddShape" class="spe__add-shape">
          <div class="spe__section-title">
            Add {{ pendingKind === 'circle' ? 'Circle' : 'Polygon' }}
          </div>

          <template v-if="pendingKind === 'circle'">
            <div class="spe__field-row">
              <div class="spe__field">
                <label>cx <span class="spe__hint">(px)</span></label>
                <input type="number" v-model.number="pendingCx" step="1" />
              </div>
              <div class="spe__field">
                <label>cy <span class="spe__hint">(px)</span></label>
                <input type="number" v-model.number="pendingCy" step="1" />
              </div>
              <div class="spe__field">
                <label>r <span class="spe__hint">(px)</span></label>
                <input type="number" v-model.number="pendingR" min="1" step="1" />
              </div>
            </div>
          </template>
          <template v-else>
            <div class="spe__field">
              <label>Points <span class="spe__hint">[x,y] pairs, comma-separated</span></label>
              <textarea v-model="pendingPoints" rows="2" class="spe__textarea" placeholder="[-9,0],[9,0],[0,10]" />
            </div>
          </template>

          <div class="spe__field-row" style="margin-top: 6px; align-items: center">
            <button
              class="spe__player-btn"
              :class="{ 'spe__player-btn--active': pendingColorMode === 'player' }"
              @click="pendingColorMode = 'player'"
            >Player</button>
            <input
              type="color"
              v-model="pendingCustomColor"
              class="spe__color-input"
              @input="pendingColorMode = 'custom'"
            />
            <div class="spe__spacer" />
            <button class="spe__btn" @click="commitShape">Add</button>
            <button class="spe__btn spe__btn--ghost" @click="showAddShape = false">Cancel</button>
          </div>

          <div v-if="addShapeError" class="spe__error">{{ addShapeError }}</div>
        </div>

      </div>

      <!-- ── Right: layer list + export ── -->
      <div class="spe__layers-panel">

        <template v-if="mode === 'building'">
          <div class="spe__section-title">Colors Used</div>
          <div class="spe__layer-list">
            <div
              v-for="group in triColorGroups"
              :key="group.color"
              class="spe__layer-item"
            >
              <div
                class="spe__layer-swatch"
                :style="{
                  background: group.color === 'player' ? '#3b82f6' : group.color,
                  border: group.color === 'player' ? '2px dashed #93c5fd' : '2px solid transparent'
                }"
              />
              <span class="spe__layer-label">{{ group.count }} tri · {{ group.color === 'player' ? 'player' : group.color }}</span>
              <button class="spe__icon-btn spe__icon-btn--del" @click="clearTriColor(group.color)" title="Clear this color">×</button>
            </div>
            <div v-if="triColorGroups.length === 0" class="spe__layer-empty">
              No paint yet. Unpainted areas show terrain.
            </div>
          </div>
          <button
            v-if="triColorGroups.length > 0"
            class="spe__btn spe__btn--ghost spe__btn--full"
            style="margin-top: 4px"
            @click="clearAllTri"
          >Clear All</button>
        </template>

        <template v-else>
          <div class="spe__section-title">Layers</div>
          <div class="spe__layer-list">
            <div
              v-for="(layer, i) in displayLayers"
              :key="i"
              class="spe__layer-item"
              :class="{ 'spe__layer-item--selected': selectedLayerIdx === i }"
              @click="selectedLayerIdx = i"
            >
              <div
                class="spe__layer-swatch"
                :style="{
                  background: layer.color === 'player' ? '#3b82f6' : layer.color,
                  border: layer.color === 'player' ? '2px dashed #93c5fd' : '2px solid transparent'
                }"
              />
              <span class="spe__layer-label">{{ layerLabel(layer) }}</span>
              <div class="spe__layer-actions">
                <button class="spe__icon-btn" :disabled="i === 0" @click.stop="moveLayer(i, -1)">↑</button>
                <button class="spe__icon-btn" :disabled="i === displayLayers.length - 1" @click.stop="moveLayer(i, 1)">↓</button>
                <button class="spe__icon-btn spe__icon-btn--del" @click.stop="deleteLayer(i)">×</button>
              </div>
            </div>
            <div v-if="displayLayers.length === 0" class="spe__layer-empty">
              No layers yet. Use + Circle or + Poly.
            </div>
          </div>
        </template>

        <div class="spe__section-title" style="margin-top: 16px">Load</div>
        <select class="spe__catalog-select" @change="onCatalogSelect">
          <option value="">— select existing {{ mode }} —</option>
          <template v-if="mode === 'building'">
            <option v-for="def in catalogBuildings" :key="def.type" :value="def.type">
              {{ def.label || def.type }}
            </option>
          </template>
          <template v-else>
            <option v-for="def in catalogUnits" :key="def.type" :value="def.type">
              {{ def.name || def.type }}
            </option>
          </template>
        </select>
        <button class="spe__btn spe__btn--full spe__btn--ghost" @click="toggleLoadPanel">
          {{ showLoadPanel ? 'Cancel paste' : 'Paste JSON...' }}
        </button>
        <template v-if="showLoadPanel">
          <textarea
            v-model="loadJsonText"
            class="spe__load-textarea"
            placeholder="Paste building or unit JSON here..."
            spellcheck="false"
          />
          <button class="spe__btn spe__btn--full" @click="loadFromJson">Apply</button>
          <div v-if="loadError" class="spe__error">{{ loadError }}</div>
        </template>

        <div class="spe__section-title" style="margin-top: 16px">Export JSON</div>
        <button class="spe__btn spe__btn--full" @click="copyExport">{{ copyLabel }}</button>
        <pre class="spe__export-pre">{{ exportJson }}</pre>

      </div>

    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick, onMounted } from 'vue'
import { fetchBuildingDefs, fetchUnitDefs } from '../game/maps/catalog'
import type { BuildingDef } from '../game/maps/buildingDefs'
import type { UnitDef } from '../game/maps/unitDefs'

// ─── Constants ────────────────────────────────────────────────────────────────

const CELL_PX   = 100   // pixels per cell in the editor canvas
const SUB       = 6     // subdivisions per cell (6×6 triangle grid)
const SUB_PX    = CELL_PX / SUB  // pixels per sub-cell
const UNIT_CANVAS = 240
const UNIT_CENTER = UNIT_CANVAS / 2

const ALL_BUILDING_CAPS = ['unit-spawner', 'occupiable', 'deposit-point', 'resource-source', 'enemy-spawner']
const ALL_UNIT_TYPES    = ['worker', 'soldier']
const ALL_UNIT_CAPS     = ['move', 'attack', 'gather', 'build']

// ─── Types ────────────────────────────────────────────────────────────────────

type TriId     = { cx: number; cy: number; sc: number; sr: number; h: 0 | 1 }
type UnitCircle = { kind: 'circle'; cx: number; cy: number; r: number; color: string }
type UnitPoly   = { kind: 'poly'; points: [number, number][]; color: string }
type UnitLayer  = UnitCircle | UnitPoly

// ─── Mode ─────────────────────────────────────────────────────────────────────

const mode = ref<'building' | 'unit'>('building')

function setMode(m: 'building' | 'unit') {
  mode.value = m
  nextTick(() => renderCanvas())
}

// ─── Building def state ───────────────────────────────────────────────────────

const bType         = ref('new-building')
const bWidth        = ref(2)
const bHeight       = ref(2)
const bMaxHp        = ref(500)
const bBuildSeconds = ref(15)
const bGold         = ref(200)
const bWood         = ref(0)
const bLabel        = ref('')
const bHotkey       = ref('')
const bColor        = ref('#1e40af')
const bInset        = ref(0.18)
const bCapabilities    = ref<string[]>([])
const bSpawnUnitTypes  = ref<string[]>([])

// Triangle fill grid: key = "cx,cy,sc,sr,h", value = color string ('player' or hex)
const bTriangles = ref<Record<string, string>>({})

// ─── Unit def state ───────────────────────────────────────────────────────────

const uType         = ref('new-unit')
const uName         = ref('New Unit')
const uHp           = ref(100)
const uDamage       = ref(5)
const uAttackRange  = ref(60)
const uAttackSpeed  = ref(1.0)
const uGold         = ref(100)
const uWood         = ref(0)
const uMeatCost     = ref(1)
const uSpawnSeconds = ref(5)
const uTrainLabel   = ref('')
const uCapabilities = ref<string[]>(['move', 'attack'])
const uLayers       = ref<UnitLayer[]>([])

// ─── Paint state (building mode) ─────────────────────────────────────────────

const paintMode        = ref<'player' | 'custom'>('player')
const paintCustomColor = ref('#94a3b8')
const paintColor       = computed(() => paintMode.value === 'player' ? 'player' : paintCustomColor.value)

const colorHistory = ref<string[]>([])

function pushColorHistory(color: string) {
  if (color === 'player') return
  const hist = colorHistory.value.filter(c => c !== color)
  hist.unshift(color)
  colorHistory.value = hist.slice(0, 5)
}

function selectHistoryColor(color: string) {
  paintCustomColor.value = color
  paintMode.value = 'custom'
}

// ─── Interaction state (building mode) ───────────────────────────────────────

const isPainting  = ref(false)
const isErasing   = ref(false)
const hoveredTri  = ref<TriId | null>(null)

// ─── Add-shape form (unit mode) ───────────────────────────────────────────────

const showAddShape      = ref(false)
const pendingKind       = ref<'circle' | 'poly'>('circle')
const pendingCx         = ref(0)
const pendingCy         = ref(0)
const pendingR          = ref(10)
const pendingPoints     = ref('[-9,0],[9,0],[0,10]')
const pendingColorMode  = ref<'player' | 'custom'>('player')
const pendingCustomColor = ref('#94a3b8')
const addShapeError     = ref('')

function openAddShape(kind: 'circle' | 'poly') {
  pendingKind.value   = kind
  addShapeError.value = ''
  showAddShape.value  = true
}

function commitShape() {
  addShapeError.value = ''
  const color = pendingColorMode.value === 'player' ? 'player' : pendingCustomColor.value

  if (pendingKind.value === 'circle') {
    uLayers.value.push({ kind: 'circle', cx: pendingCx.value, cy: pendingCy.value, r: pendingR.value, color })
    showAddShape.value = false
    return
  }

  try {
    const points = JSON.parse(`[${pendingPoints.value}]`) as [number, number][]
    if (!Array.isArray(points) || points.length < 2) throw new Error()
    uLayers.value.push({ kind: 'poly', points, color })
    showAddShape.value = false
  } catch {
    addShapeError.value = 'Invalid points — use [x,y] pairs separated by commas, e.g. [-9,0],[9,0],[0,10]'
  }
}

// ─── Layer panel (unit mode) ──────────────────────────────────────────────────

const selectedLayerIdx = ref<number | null>(null)

const displayLayers = computed<UnitLayer[]>(() => uLayers.value)

function layerLabel(layer: UnitLayer): string {
  if (layer.kind === 'circle') return `Circle r=${layer.r} @ (${layer.cx}, ${layer.cy})`
  return `Poly (${layer.points.length} pts)`
}

function deleteLayer(i: number) {
  uLayers.value.splice(i, 1)
  if (selectedLayerIdx.value === i) selectedLayerIdx.value = null
}

function moveLayer(i: number, dir: -1 | 1) {
  const arr = uLayers.value
  const j = i + dir
  if (j < 0 || j >= arr.length) return
  ;[arr[i], arr[j]] = [arr[j], arr[i]]
}

// ─── Triangle color groups (building mode right panel) ────────────────────────

const triColorGroups = computed(() => {
  const counts: Record<string, number> = {}
  for (const color of Object.values(bTriangles.value)) {
    counts[color] = (counts[color] ?? 0) + 1
  }
  return Object.entries(counts)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([color, count]) => ({ color, count }))
})

function clearTriColor(color: string) {
  const next: Record<string, string> = {}
  for (const [k, v] of Object.entries(bTriangles.value)) {
    if (v !== color) next[k] = v
  }
  bTriangles.value = next
  renderCanvas()
}

function clearAllTri() {
  bTriangles.value = {}
  renderCanvas()
}

// ─── Canvas ───────────────────────────────────────────────────────────────────

const drawCanvas = ref<HTMLCanvasElement | null>(null)
const canvasZoom = ref(2)

const canvasWidth  = computed(() => mode.value === 'building' ? bWidth.value  * CELL_PX : UNIT_CANVAS)
const canvasHeight = computed(() => mode.value === 'building' ? bHeight.value * CELL_PX : UNIT_CANVAS)
const canvasZoomPercent = computed(() => Math.round(canvasZoom.value * 100))
const canvasStyle = computed(() => ({
  width: `${Math.round(canvasWidth.value * canvasZoom.value)}px`,
  height: `${Math.round(canvasHeight.value * canvasZoom.value)}px`,
}))

function clampCanvasZoom(value: number): number {
  return Math.min(4, Math.max(0.5, Number(value.toFixed(2))))
}

function adjustCanvasZoom(delta: number) {
  canvasZoom.value = clampCanvasZoom(canvasZoom.value + delta)
}

function resetCanvasZoom() {
  canvasZoom.value = 1
}

// Returns raw canvas pixel coordinates
function canvasPx(e: MouseEvent): { px: number; py: number } {
  const rect = drawCanvas.value!.getBoundingClientRect()
  const scaleX = canvasWidth.value  / rect.width
  const scaleY = canvasHeight.value / rect.height
  return {
    px: (e.clientX - rect.left) * scaleX,
    py: (e.clientY - rect.top)  * scaleY,
  }
}

// Returns true when this sub-cell uses a backslash \, false for forward slash /
// Alternates in a checkerboard based on sub-cell position.
function isBackslash(sc: number, sr: number): boolean {
  return (sc + sr) % 2 === 1
}

// Maps pixel coords to the triangle under the cursor
function triAtPos(px: number, py: number): TriId | null {
  const stx = Math.floor(px / SUB_PX)
  const sty = Math.floor(py / SUB_PX)
  if (stx < 0 || stx >= bWidth.value * SUB || sty < 0 || sty >= bHeight.value * SUB) return null
  const cx = Math.floor(stx / SUB)
  const cy = Math.floor(sty / SUB)
  const sc = stx % SUB
  const sr = sty % SUB
  const lx = px - stx * SUB_PX
  const ly = py - sty * SUB_PX
  // \ diagonal: h=0 is upper-right (lx > ly), h=1 is lower-left
  // / diagonal: h=0 is upper-left  (lx + ly < s), h=1 is lower-right
  const h: 0 | 1 = isBackslash(sc, sr) ? (lx > ly ? 0 : 1) : (lx + ly < SUB_PX ? 0 : 1)
  return { cx, cy, sc, sr, h }
}

function triKey({ cx, cy, sc, sr, h }: TriId): string {
  return `${cx},${cy},${sc},${sr},${h}`
}

function applyPaint(e: MouseEvent) {
  const { px, py } = canvasPx(e)
  const tri = triAtPos(px, py)
  if (!tri) return
  const key = triKey(tri)
  if (isErasing.value) {
    const next = { ...bTriangles.value }
    delete next[key]
    bTriangles.value = next
  } else {
    bTriangles.value = { ...bTriangles.value, [key]: paintColor.value }
  }
  renderCanvas()
}

function onMouseDown(e: MouseEvent) {
  if (mode.value !== 'building') return
  e.preventDefault()
  if (e.button === 2) isErasing.value = true
  else {
    isPainting.value = true
    pushColorHistory(paintColor.value)
  }
  applyPaint(e)
}

function onMouseMove(e: MouseEvent) {
  if (mode.value !== 'building') return
  const { px, py } = canvasPx(e)
  hoveredTri.value = triAtPos(px, py)
  if (isPainting.value || isErasing.value) {
    applyPaint(e)
  } else {
    renderCanvas()
  }
}

function onMouseUp() {
  isPainting.value = false
  isErasing.value  = false
}

function onMouseLeave() {
  hoveredTri.value = null
  isPainting.value = false
  isErasing.value  = false
  renderCanvas()
}

// Fills one triangle in a sub-cell.
// For / cells: h=0 = upper-left, h=1 = lower-right
// For \ cells: h=0 = upper-right, h=1 = lower-left
function fillTriangle(ctx: CanvasRenderingContext2D, tlX: number, tlY: number, s: number, sc: number, sr: number, h: 0 | 1, color: string) {
  ctx.fillStyle = color
  ctx.beginPath()
  if (!isBackslash(sc, sr)) {
    if (h === 0) { ctx.moveTo(tlX,     tlY); ctx.lineTo(tlX + s, tlY); ctx.lineTo(tlX,     tlY + s) }
    else         { ctx.moveTo(tlX + s, tlY); ctx.lineTo(tlX + s, tlY + s); ctx.lineTo(tlX, tlY + s) }
  } else {
    if (h === 0) { ctx.moveTo(tlX,     tlY); ctx.lineTo(tlX + s, tlY); ctx.lineTo(tlX + s, tlY + s) }
    else         { ctx.moveTo(tlX,     tlY); ctx.lineTo(tlX,     tlY + s); ctx.lineTo(tlX + s, tlY + s) }
  }
  ctx.closePath()
  ctx.fill()
  ctx.strokeStyle = color
  ctx.lineWidth = 1
  ctx.stroke()
}

function renderCanvas() {
  const canvas = drawCanvas.value
  if (!canvas) return
  const ctx = canvas.getContext('2d')
  if (!ctx) return

  const W = canvasWidth.value
  const H = canvasHeight.value

  ctx.clearRect(0, 0, W, H)

  if (mode.value === 'building') {

    // 1. Draw filled triangles first
    for (const [key, color] of Object.entries(bTriangles.value)) {
      const [kCx, kCy, kSc, kSr, kH] = key.split(',').map(Number)
      const tlX = kCx * CELL_PX + kSc * SUB_PX
      const tlY = kCy * CELL_PX + kSr * SUB_PX
      fillTriangle(ctx, tlX, tlY, SUB_PX, kSc, kSr, kH as 0 | 1, color === 'player' ? '#3b82f6' : color)
    }

    // 2. Hover preview
    if (hoveredTri.value && !isPainting.value && !isErasing.value) {
      const { cx, cy, sc, sr, h } = hoveredTri.value
      const tlX = cx * CELL_PX + sc * SUB_PX
      const tlY = cy * CELL_PX + sr * SUB_PX
      ctx.save()
      ctx.globalAlpha = 0.4
      fillTriangle(ctx, tlX, tlY, SUB_PX, sc, sr, h, paintColor.value === 'player' ? '#3b82f6' : paintColor.value)
      ctx.restore()
    }

    // 3. Guides drawn on top — but only for sub-cells that still have an empty half,
    //    so fully-painted sub-cells never have a guide line drawn through them.
    ctx.strokeStyle = '#1e293b'
    ctx.lineWidth = 0.5
    for (let stx = 0; stx < bWidth.value * SUB; stx++) {
      for (let sty = 0; sty < bHeight.value * SUB; sty++) {
        const cx = Math.floor(stx / SUB)
        const cy = Math.floor(sty / SUB)
        const sc = stx % SUB
        const sr = sty % SUB
        const h0 = bTriangles.value[`${cx},${cy},${sc},${sr},0`]
        const h1 = bTriangles.value[`${cx},${cy},${sc},${sr},1`]
        if (h0 !== undefined && h1 !== undefined) continue  // both filled — no guide needed
        const tlX = stx * SUB_PX
        const tlY = sty * SUB_PX
        ctx.beginPath()
        if (isBackslash(sc, sr)) {
          ctx.moveTo(tlX,          tlY)
          ctx.lineTo(tlX + SUB_PX, tlY + SUB_PX)
        } else {
          ctx.moveTo(tlX,          tlY + SUB_PX)
          ctx.lineTo(tlX + SUB_PX, tlY)
        }
        ctx.stroke()
      }
    }
    for (let i = 0; i <= bWidth.value * SUB; i++) {
      if (i % SUB === 0) continue
      ctx.beginPath(); ctx.moveTo(i * SUB_PX, 0); ctx.lineTo(i * SUB_PX, H); ctx.stroke()
    }
    for (let j = 0; j <= bHeight.value * SUB; j++) {
      if (j % SUB === 0) continue
      ctx.beginPath(); ctx.moveTo(0, j * SUB_PX); ctx.lineTo(W, j * SUB_PX); ctx.stroke()
    }

    // 4. Major cell boundary lines
    ctx.strokeStyle = '#334155'
    ctx.lineWidth = 1.5
    for (let i = 0; i <= bWidth.value; i++) {
      ctx.beginPath(); ctx.moveTo(i * CELL_PX, 0); ctx.lineTo(i * CELL_PX, H); ctx.stroke()
    }
    for (let j = 0; j <= bHeight.value; j++) {
      ctx.beginPath(); ctx.moveTo(0, j * CELL_PX); ctx.lineTo(W, j * CELL_PX); ctx.stroke()
    }

    // 6. Inset guide (dashed — shows where HP bar / overlay will anchor)
    const insetPx = bInset.value * CELL_PX
    ctx.save()
    ctx.strokeStyle = 'rgba(148,163,184,0.2)'
    ctx.lineWidth   = 1
    ctx.setLineDash([4, 4])
    ctx.strokeRect(insetPx, insetPx, W - insetPx * 2, H - insetPx * 2)
    ctx.restore()

  } else {
    // Unit canvas
    const cx = UNIT_CENTER
    const cy = UNIT_CENTER

    ctx.strokeStyle = '#1e293b'
    ctx.lineWidth   = 1
    ctx.beginPath(); ctx.moveTo(cx, 0);  ctx.lineTo(cx, H); ctx.stroke()
    ctx.beginPath(); ctx.moveTo(0,  cy); ctx.lineTo(W, cy); ctx.stroke()

    for (const layer of uLayers.value) {
      ctx.fillStyle = layer.color === 'player' ? '#3b82f6' : layer.color
      if (layer.kind === 'circle') {
        ctx.beginPath()
        ctx.arc(cx + layer.cx, cy + layer.cy, layer.r, 0, Math.PI * 2)
        ctx.fill()
      } else if (layer.kind === 'poly' && layer.points.length >= 2) {
        ctx.beginPath()
        ctx.moveTo(cx + layer.points[0][0], cy + layer.points[0][1])
        for (let i = 1; i < layer.points.length; i++) {
          ctx.lineTo(cx + layer.points[i][0], cy + layer.points[i][1])
        }
        ctx.closePath()
        ctx.fill()
      }
    }
  }
}

watch(
  [mode, bTriangles, uLayers, bWidth, bHeight, bInset],
  () => nextTick(() => renderCanvas()),
  { deep: true },
)

watch(canvasZoom, value => {
  const clamped = clampCanvasZoom(value)
  if (clamped !== value) {
    canvasZoom.value = clamped
  }
})

onMounted(() => {
  renderCanvas()
  fetchBuildingDefs().then(defs => { catalogBuildings.value = defs }).catch(() => {})
  fetchUnitDefs().then(defs => { catalogUnits.value = defs }).catch(() => {})
})

// ─── Load / Catalog ───────────────────────────────────────────────────────────

const catalogBuildings = ref<BuildingDef[]>([])
const catalogUnits     = ref<UnitDef[]>([])

const showLoadPanel = ref(false)
const loadJsonText  = ref('')
const loadError     = ref('')

function toggleLoadPanel() {
  showLoadPanel.value = !showLoadPanel.value
  loadError.value     = ''
}

/**
 * Converts exported render layers back into the raw bTriangles map.
 * Rect layers are expanded to fill every sub-cell they cover (both h=0 and h=1).
 * Tri layers map directly to a single triangle entry.
 */
function layersToTriangles(layers: any[], cellW: number, cellH: number): Record<string, string> {
  const tris: Record<string, string> = {}
  const maxStx = cellW * SUB
  const maxSty = cellH * SUB

  for (const layer of layers) {
    if (!layer.color) continue
    const color = layer.color as string

    if (layer.kind === 'tri') {
      tris[`${layer.cx},${layer.cy},${layer.sc},${layer.sr},${layer.h}`] = color
    } else {
      // rect — no kind or kind: 'rect'
      const stxStart = Math.round(layer.x * SUB)
      const styStart = Math.round(layer.y * SUB)
      const stxEnd   = Math.round((layer.x + layer.w) * SUB)
      const styEnd   = Math.round((layer.y + layer.h) * SUB)
      for (let sty = styStart; sty < Math.min(styEnd, maxSty); sty++) {
        for (let stx = stxStart; stx < Math.min(stxEnd, maxStx); stx++) {
          const cx = Math.floor(stx / SUB), sc = stx % SUB
          const cy = Math.floor(sty / SUB), sr = sty % SUB
          tris[`${cx},${cy},${sc},${sr},0`] = color
          tris[`${cx},${cy},${sc},${sr},1`] = color
        }
      }
    }
  }
  return tris
}

function applyBuildingDef(data: BuildingDef) {
  bType.value           = data.type
  bWidth.value          = data.width
  bHeight.value         = data.height
  bMaxHp.value          = data.maxHp
  bBuildSeconds.value   = data.buildSeconds
  bGold.value           = data.resourceCost?.gold ?? 0
  bWood.value           = data.resourceCost?.wood ?? 0
  bLabel.value          = data.label        ?? ''
  bHotkey.value         = data.hotkey       ?? ''
  bColor.value          = data.color        ?? '#1e40af'
  bInset.value          = data.render?.inset ?? 0.18
  bCapabilities.value   = [...(data.capabilities   ?? [])]
  bSpawnUnitTypes.value = [...(data.spawnUnitTypes  ?? [])]
  bTriangles.value      = data.render?.layers
    ? layersToTriangles(data.render.layers as any[], data.width, data.height)
    : {}
  mode.value = 'building'

  const usedColors = [...new Set(Object.values(bTriangles.value).filter(c => c !== 'player'))]
  for (const c of usedColors.reverse()) pushColorHistory(c)

  nextTick(() => renderCanvas())
}

function applyUnitDef(data: UnitDef) {
  uType.value         = data.type
  uName.value         = data.name
  uHp.value           = data.hp
  uDamage.value       = data.damage
  uAttackRange.value  = data.attackRange
  uAttackSpeed.value  = data.attackSpeed
  uGold.value         = data.resourceCost?.gold ?? 0
  uWood.value         = data.resourceCost?.wood ?? 0
  uMeatCost.value     = data.meatCost
  uSpawnSeconds.value = data.spawnSeconds
  uTrainLabel.value   = data.trainLabel   ?? ''
  uCapabilities.value = [...(data.capabilities ?? [])]
  uLayers.value       = [...(data.render?.layers ?? [])] as typeof uLayers.value
  mode.value = 'unit'

  const usedColors = [...new Set(uLayers.value.map(l => l.color).filter(c => c !== 'player'))]
  for (const c of usedColors.reverse()) pushColorHistory(c)

  nextTick(() => renderCanvas())
}

function onCatalogSelect(e: Event) {
  const select = e.target as HTMLSelectElement
  const val = select.value
  if (!val) return

  if (mode.value === 'building') {
    const def = catalogBuildings.value.find(d => d.type === val)
    if (def) applyBuildingDef(def)
  } else {
    const def = catalogUnits.value.find(d => d.type === val)
    if (def) applyUnitDef(def)
  }

  // Reset to placeholder so the select doesn't stay on a value
  select.value = ''
}

function loadFromJson() {
  loadError.value = ''
  let data: any
  try {
    data = JSON.parse(loadJsonText.value)
  } catch {
    loadError.value = 'Invalid JSON — could not parse.'
    return
  }

  if ('width' in data && 'height' in data) {
    applyBuildingDef(data as BuildingDef)
  } else if ('hp' in data || 'damage' in data) {
    applyUnitDef(data as UnitDef)
  } else {
    loadError.value = 'Unrecognised format — expected a building (width/height) or unit (hp/damage) definition.'
    return
  }

  showLoadPanel.value = false
  loadJsonText.value  = ''
}

// ─── Export ───────────────────────────────────────────────────────────────────

type RectLayer = { kind: 'rect'; x: number; y: number; w: number; h: number; color: string }
type TriLayer  = { kind: 'tri';  cx: number; cy: number; sc: number; sr: number; h: 0 | 1; color: string }
type ExportLayer = RectLayer | TriLayer

/**
 * Converts the raw triangle map into an optimized list of rect + tri layers.
 *
 * Strategy per color:
 *   1. A sub-cell whose both halves (h=0 and h=1) share the same color becomes a "full square".
 *   2. Full squares are merged into the largest possible rectangles using a greedy
 *      row-major scan: expand right first, then expand down while the full width holds.
 *   3. Any triangle whose partner is absent or a different color is emitted as-is.
 */
function buildOptimizedLayers(
  triangles: Record<string, string>,
  totalCellW: number,
  totalCellH: number,
): ExportLayer[] {
  const SUB_W = totalCellW * SUB
  const SUB_H = totalCellH * SUB

  // --- 1. Bucket triangles by color and identify full squares ----------------
  // squaresByColor: color → Set of "stx,sty" strings
  // looseTrisByColor: color → array of tri descriptors
  const squaresByColor = new Map<string, Set<string>>()
  const looseTrisByColor = new Map<string, TriLayer[]>()

  const seenSquares = new Set<string>() // "stx,sty" regardless of color

  for (const [key, color] of Object.entries(triangles)) {
    const [cx, cy, sc, sr, h] = key.split(',').map(Number)
    const stx = cx * SUB + sc
    const sty = cy * SUB + sr
    const cellKey = `${stx},${sty}`

    // Only process h=0 to avoid double-counting squares
    if (h !== 0) continue

    const partnerKey = `${cx},${cy},${sc},${sr},1`
    if (triangles[partnerKey] === color) {
      // Both halves same color → full square
      if (!squaresByColor.has(color)) squaresByColor.set(color, new Set())
      squaresByColor.get(color)!.add(cellKey)
      seenSquares.add(cellKey)
    }
  }

  // Collect loose triangles (those not part of a same-color full square)
  for (const [key, color] of Object.entries(triangles)) {
    const [cx, cy, sc, sr, h] = key.split(',').map(Number)
    const stx = cx * SUB + sc
    const sty = cy * SUB + sr
    const cellKey = `${stx},${sty}`
    const isInSquare = squaresByColor.get(color)?.has(cellKey) ?? false
    if (!isInSquare) {
      if (!looseTrisByColor.has(color)) looseTrisByColor.set(color, [])
      looseTrisByColor.get(color)!.push({ kind: 'tri', cx, cy, sc, sr, h: h as 0 | 1, color })
    }
  }

  // --- 2. Greedy rectangle merge per color -----------------------------------
  const layers: ExportLayer[] = []

  for (const [color, squareSet] of squaresByColor) {
    // Build a boolean grid
    const grid: boolean[][] = Array.from({ length: SUB_H }, () => new Array(SUB_W).fill(false))
    for (const key of squareSet) {
      const [stx, sty] = key.split(',').map(Number)
      grid[sty][stx] = true
    }

    // Scan row-major; for each filled cell find the maximal width then max height
    for (let sty = 0; sty < SUB_H; sty++) {
      for (let stx = 0; stx < SUB_W; stx++) {
        if (!grid[sty][stx]) continue

        // Expand right
        let rw = 0
        while (stx + rw < SUB_W && grid[sty][stx + rw]) rw++

        // Expand down while the full width is still filled
        let rh = 1
        outer: while (sty + rh < SUB_H) {
          for (let dx = 0; dx < rw; dx++) {
            if (!grid[sty + rh][stx + dx]) break outer
          }
          rh++
        }

        // Emit rect in cell-unit coordinates (SUB sub-cells = 1 cell)
        layers.push({
          kind: 'rect',
          x: stx / SUB,
          y: sty / SUB,
          w: rw  / SUB,
          h: rh  / SUB,
          color,
        })

        // Clear consumed cells
        for (let dy = 0; dy < rh; dy++)
          for (let dx = 0; dx < rw; dx++)
            grid[sty + dy][stx + dx] = false
      }
    }
  }

  // --- 3. Append loose triangles sorted for deterministic output -------------
  for (const tris of looseTrisByColor.values()) {
    tris.sort((a, b) => `${a.cx},${a.cy},${a.sc},${a.sr},${a.h}`.localeCompare(`${b.cx},${b.cy},${b.sc},${b.sr},${b.h}`))
    layers.push(...tris)
  }

  return layers
}

const exportJson = computed(() => {
  if (mode.value === 'building') {
    const resourceCost: Record<string, number> = {}
    if (bGold.value > 0) resourceCost.gold = bGold.value
    if (bWood.value > 0) resourceCost.wood = bWood.value

    const layers = buildOptimizedLayers(bTriangles.value, bWidth.value, bHeight.value)

    return JSON.stringify({
      type: bType.value,
      width: bWidth.value,
      height: bHeight.value,
      maxHp: bMaxHp.value,
      buildSeconds: bBuildSeconds.value,
      resourceCost,
      capabilities: bCapabilities.value,
      spawnUnitTypes: bSpawnUnitTypes.value,
      metadata: {},
      color: bColor.value,
      label: bLabel.value,
      hotkey: bHotkey.value,
      render: {
        inset: bInset.value,
        layers,
      },
    }, null, 2)
  }

  const resourceCost: Record<string, number> = {}
  if (uGold.value > 0) resourceCost.gold = uGold.value
  if (uWood.value > 0) resourceCost.wood = uWood.value

  return JSON.stringify({
    type: uType.value,
    name: uName.value,
    hp: uHp.value,
    damage: uDamage.value,
    attackRange: uAttackRange.value,
    attackSpeed: uAttackSpeed.value,
    resourceCost,
    meatCost: uMeatCost.value,
    spawnSeconds: uSpawnSeconds.value,
    capabilities: uCapabilities.value,
    trainLabel: uTrainLabel.value,
    render: {
      layers: uLayers.value,
    },
  }, null, 2)
})

const copyLabel = ref('Copy JSON')

async function copyExport() {
  await navigator.clipboard.writeText(exportJson.value)
  copyLabel.value = 'Copied!'
  setTimeout(() => { copyLabel.value = 'Copy JSON' }, 2000)
}
</script>

<style scoped>
.spe {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  background: #0f172a;
  color: #f1f5f9;
  font-family: monospace;
  font-size: 13px;
  overflow: hidden;
}

/* ── Tabs ── */
.spe__tabs {
  display: flex;
  gap: 2px;
  padding: 8px 12px 0;
  border-bottom: 1px solid #1e293b;
  flex-shrink: 0;
}
.spe__tab {
  padding: 6px 20px;
  background: #1e293b;
  border: 1px solid #334155;
  border-bottom: none;
  border-radius: 4px 4px 0 0;
  color: #94a3b8;
  cursor: pointer;
  font-family: monospace;
  font-size: 13px;
  transition: background 0.1s, color 0.1s;
}
.spe__tab:hover { background: #253344; color: #e2e8f0; }
.spe__tab--active { background: #0f172a; color: #f1f5f9; border-color: #334155; }

/* ── Body layout ── */
.spe__body {
  display: flex;
  flex: 1;
  overflow: hidden;
}

/* ── Sidebar ── */
.spe__sidebar {
  width: 240px;
  min-width: 240px;
  padding: 14px 12px;
  background: #1e293b;
  border-right: 1px solid #334155;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.spe__section-title {
  font-size: 0.7rem;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: #64748b;
  margin-bottom: 2px;
}

.spe__field {
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.spe__field label {
  font-size: 11px;
  color: #94a3b8;
}
.spe__field input,
.spe__field textarea {
  background: #0f172a;
  border: 1px solid #334155;
  border-radius: 3px;
  color: #f1f5f9;
  padding: 4px 7px;
  font-family: monospace;
  font-size: 12px;
  width: 100%;
  box-sizing: border-box;
}
.spe__field input:focus,
.spe__field textarea:focus {
  outline: none;
  border-color: #3b82f6;
}
.spe__field input[type="color"] {
  padding: 1px 3px;
  height: 28px;
  cursor: pointer;
}

.spe__field-row {
  display: flex;
  gap: 8px;
}
.spe__field-row .spe__field { flex: 1; }

.spe__hint { color: #475569; font-size: 10px; }

.spe__checklist {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.spe__checklist label {
  display: flex;
  align-items: center;
  gap: 6px;
  color: #cbd5e1;
  cursor: pointer;
  font-size: 12px;
}
.spe__checklist input[type="checkbox"] { accent-color: #3b82f6; }

/* ── Main (canvas area) ── */
.spe__main {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.spe__toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  padding: 8px 14px;
  background: #1e293b;
  border-bottom: 1px solid #334155;
  flex-shrink: 0;
}
.spe__toolbar-label {
  font-size: 11px;
  color: #64748b;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.spe__toolbar-hint {
  font-size: 10px;
  color: #475569;
  margin-left: 4px;
}
.spe__toolbar-divider {
  width: 1px;
  height: 20px;
  background: #334155;
}
.spe__toolbar-value {
  min-width: 44px;
  font-size: 11px;
  color: #94a3b8;
  font-family: monospace;
}
.spe__zoom-slider {
  width: 120px;
  accent-color: #3b82f6;
}

.spe__player-btn {
  padding: 3px 10px;
  background: #1e3a5f;
  border: 1px solid #3b82f6;
  border-radius: 3px;
  color: #93c5fd;
  cursor: pointer;
  font-family: monospace;
  font-size: 12px;
}
.spe__player-btn--active {
  background: #1d4ed8;
  color: #fff;
}
.spe__player-btn:hover { background: #1e40af; }

.spe__color-input {
  width: 34px;
  height: 26px;
  padding: 1px 2px;
  border: 1px solid #334155;
  border-radius: 3px;
  background: #0f172a;
  cursor: pointer;
}

.spe__active-color {
  width: 20px;
  height: 20px;
  border-radius: 3px;
  border: 1px solid #334155;
  flex-shrink: 0;
}

.spe__history-swatch {
  width: 20px;
  height: 20px;
  border-radius: 3px;
  border: 1px solid #334155;
  flex-shrink: 0;
  cursor: pointer;
  transition: border-color 0.1s, transform 0.1s;
}
.spe__history-swatch:hover { border-color: #94a3b8; transform: scale(1.15); }
.spe__history-swatch--active { border-color: #f1f5f9; box-shadow: 0 0 0 1px #f1f5f9; }

.spe__canvas-wrap {
  flex: 1;
  overflow: auto;
  display: flex;
  align-items: flex-start;
  justify-content: flex-start;
  padding: 20px;
}

.spe__canvas {
  cursor: crosshair;
  border: 1px solid #334155;
  display: block;
  image-rendering: pixelated;
}

/* ── Add-shape form ── */
.spe__add-shape {
  padding: 12px 14px;
  background: #162032;
  border-top: 1px solid #334155;
  display: flex;
  flex-direction: column;
  gap: 8px;
  flex-shrink: 0;
}
.spe__textarea {
  background: #0f172a;
  border: 1px solid #334155;
  border-radius: 3px;
  color: #f1f5f9;
  padding: 5px 7px;
  font-family: monospace;
  font-size: 12px;
  width: 100%;
  box-sizing: border-box;
  resize: vertical;
}
.spe__textarea:focus { outline: none; border-color: #3b82f6; }
.spe__spacer { flex: 1; }
.spe__error { color: #f87171; font-size: 11px; }

/* ── Buttons ── */
.spe__btn {
  padding: 5px 12px;
  background: #1e40af;
  border: 1px solid #3b82f6;
  border-radius: 3px;
  color: #bfdbfe;
  cursor: pointer;
  font-family: monospace;
  font-size: 12px;
}
.spe__btn:hover { background: #1d4ed8; }
.spe__btn--sm { padding: 3px 9px; }
.spe__btn--full { width: 100%; }
.spe__btn--ghost {
  background: transparent;
  border-color: #334155;
  color: #64748b;
}
.spe__btn--ghost:hover { border-color: #475569; color: #94a3b8; }

/* ── Layers panel ── */
.spe__layers-panel {
  width: 240px;
  min-width: 240px;
  padding: 14px 12px;
  background: #1e293b;
  border-left: 1px solid #334155;
  display: flex;
  flex-direction: column;
  gap: 6px;
  overflow-y: auto;
}

.spe__layer-list {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-height: 60px;
}

.spe__layer-item {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 5px 7px;
  border-radius: 4px;
  border: 1px solid transparent;
  cursor: pointer;
  background: #0f172a;
}
.spe__layer-item:hover { border-color: #334155; }
.spe__layer-item--selected { border-color: #3b82f6; background: #162032; }

.spe__layer-swatch {
  width: 16px;
  height: 16px;
  border-radius: 2px;
  flex-shrink: 0;
}
.spe__layer-label {
  flex: 1;
  font-size: 11px;
  color: #94a3b8;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.spe__layer-actions {
  display: flex;
  gap: 2px;
  flex-shrink: 0;
}
.spe__layer-empty {
  font-size: 11px;
  color: #475569;
  font-style: italic;
  padding: 6px 4px;
}

.spe__icon-btn {
  width: 22px;
  height: 22px;
  padding: 0;
  background: transparent;
  border: 1px solid #334155;
  border-radius: 3px;
  color: #64748b;
  cursor: pointer;
  font-size: 12px;
  line-height: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}
.spe__icon-btn:hover:not(:disabled) { border-color: #475569; color: #94a3b8; }
.spe__icon-btn:disabled { opacity: 0.3; cursor: default; }
.spe__icon-btn--del:hover:not(:disabled) { border-color: #ef4444; color: #ef4444; }

/* ── Export ── */
.spe__catalog-select {
  width: 100%;
  background: #0f172a;
  border: 1px solid #334155;
  border-radius: 3px;
  padding: 5px 8px;
  color: #f1f5f9;
  font-family: monospace;
  font-size: 12px;
  cursor: pointer;
}
.spe__catalog-select:focus { outline: none; border-color: #3b82f6; }

.spe__load-textarea {
  width: 100%;
  min-height: 120px;
  background: #0f172a;
  border: 1px solid #334155;
  border-radius: 3px;
  padding: 8px;
  font-family: monospace;
  font-size: 10px;
  color: #94a3b8;
  resize: vertical;
  box-sizing: border-box;
}
.spe__load-textarea:focus {
  outline: none;
  border-color: #3b82f6;
}

.spe__export-pre {
  background: #0f172a;
  border: 1px solid #334155;
  border-radius: 3px;
  padding: 8px;
  font-size: 10px;
  color: #64748b;
  overflow: auto;
  max-height: 320px;
  white-space: pre;
  margin: 0;
  flex-shrink: 0;
}
</style>
