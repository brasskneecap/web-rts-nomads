<template>
  <div class="kingdom">
    <div class="kingdom__back">
      <ExitButton aria-label="Back to War Room" @click="onBack" />
    </div>

    <!-- Dev-only authoring toolbar (pinned to the viewport, not the cropped
         scene). Toggle Edit mode, then drag the icon or click box independently,
         wheel or +/- to size the icon, handles to size the box, arrows to nudge.
         Mirrors the War Room tool. -->
    <div v-if="NODE_EDITOR_ENABLED" class="kingdom__edit-bar">
      <button
        type="button"
        class="kingdom__edit-toggle"
        :class="{ 'is-on': editing }"
        @click="editing = !editing"
      >
        {{ editing ? '✓ Done' : '✎ Edit nodes' }}
      </button>
      <template v-if="editing">
        <span class="kingdom__edit-hint">
          drag icon / drag box = move each · handles = box size · wheel or +/− = icon size · arrows = nudge last
        </span>
        <button type="button" class="kingdom__edit-copy" @click="copyConfig">
          Copy config
        </button>
      </template>
    </div>

    <div class="kingdom__stage">
      <div
        ref="sceneRef"
        class="kingdom__scene"
        :style="{ backgroundImage: `url(${kingdomBgUrl})` }"
      >
        <div class="kingdom__hotspots">
          <template v-for="n in nodes" :key="n.id">
            <!-- Transparent hit box: the clickable area, sized (hitW × hitH)
                 independently of the badge. -->
            <button
              type="button"
              class="kingdom__hit"
              :class="{
                'kingdom__hit--editing': editing,
                'kingdom__hit--active': editId === n.id,
              }"
              :style="hitStyle(n)"
              :aria-label="n.label"
              @click="onSelect(n)"
              @pointerdown="onPointerDown($event, n)"
              @wheel="onWheel($event, n)"
            >
              <template v-if="editing">
                <span
                  v-for="hd in RESIZE_HANDLES"
                  :key="hd.name"
                  class="kingdom__handle"
                  :class="`kingdom__handle--${hd.name}`"
                  @pointerdown="onHandleDown($event, n, hd.dx, hd.dy)"
                ></span>
              </template>
            </button>

            <!-- Visual layer: badge medallion sitting on a dark plate with the
                 building name + a one-line hint. Non-interactive except in edit
                 mode, where it becomes draggable to move the icon on its own. -->
            <div
              class="kingdom__node"
              :class="{ 'kingdom__node--editing': editing, 'kingdom__node--active': editing && editId === n.id && editKind === 'icon' }"
              :style="nodeStyle(n)"
              @pointerdown="onIconDown($event, n)"
              @wheel="onWheel($event, n)"
            >
              <img class="kingdom__badge" :src="n.icon" :alt="n.label" draggable="false" />
              <div class="kingdom__plate">
                <span class="kingdom__label">{{ n.label }}</span>
                <span class="kingdom__desc">{{ n.desc }}</span>
              </div>

              <span v-if="editing" class="kingdom__coords">
                icon {{ n.x }},{{ n.y }} · {{ n.size }} — hit {{ n.hitX }},{{ n.hitY }} · {{ n.hitW }}×{{ n.hitH }}
              </span>
            </div>
          </template>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import ExitButton from '@/components/ui/ExitButton.vue'
import kingdomBgUrl from '@/assets/background-images/castle-view_tier1/full-town-view_tier1.png'
import kingdomNodeUrl from '@/assets/background-images/nodes/kingdom-node.png'
import barracksNodeUrl from '@/assets/background-images/nodes/barracks-node.png'
import chapelNodeUrl from '@/assets/background-images/nodes/chapel-node.png'
import farmNodeUrl from '@/assets/background-images/nodes/farm-node.png'
import marketplaceNodeUrl from '@/assets/background-images/nodes/marketplace-node.png'
import blacksmithNodeUrl from '@/assets/background-images/nodes/blacksmith-node.png'

const router = useRouter()

/* ----------------------------------------------------------------
 * Nodes: visible badge medallions (nodes/*.png) placed over the buildings in
 * full-town-view_tier1.png (2064x1152). Each node is TWO independently
 * positioned pieces, both in % of the scene (mirrors WarRoom.vue):
 *   • the badge icon — centered at (x, y), width `size`, over a dark plate
 *     holding the building name + a one-line hint;
 *   • the clickable area — a separate box centered at (hitX, hitY), size
 *     hitW × hitH, so the badge can float above a building while the click
 *     target covers the building itself.
 *
 * To re-align a node: use the dev toolbar (top-right) to enter Edit mode, drag
 * the icon or the click box independently, then "Copy config" and paste the
 * numbers back into DEFAULT_NODES.
 * ---------------------------------------------------------------- */
interface Node {
  id: string
  label: string
  desc: string
  x: number // badge (icon) center X, % of scene
  y: number // badge (icon) center Y, % of scene
  size: number // badge (icon) width, % of scene
  hitX: number // clickable area center X, % of scene
  hitY: number // clickable area center Y, % of scene
  hitW: number // clickable area width, % of scene
  hitH: number // clickable area height, % of scene
  icon: string
  route: string
}

const DEFAULT_NODES: Node[] = [
  { id: 'townHall', label: 'War Room', desc: 'Plan your next Conquest', x: 48.7, y: 17.5, size: 4, hitX: 53.2, hitY: 16.3, hitW: 32.3, hitH: 23.1, icon: kingdomNodeUrl, route: '/war-room' },
  { id: 'barracks', label: 'Barracks', desc: 'Improve Soldiers and Archers', x: 21.8, y: 34.7, size: 4, hitX: 23.7, hitY: 40.9, hitW: 25.4, hitH: 23.5, icon: barracksNodeUrl, route: '/kingdom/barracks' },
  { id: 'chapel', label: 'Chapel', desc: 'Improve Acolytes and Adepts', x: 77.4, y: 26.1, size: 4, hitX: 80.3, hitY: 36, hitW: 22, hitH: 34, icon: chapelNodeUrl, route: '/kingdom/chapel' },
  { id: 'farm', label: 'Farm', desc: 'Improve Workers and Base', x: 17.7, y: 64.3, size: 4, hitX: 22, hitY: 69.4, hitW: 17.8, hitH: 25.8, icon: farmNodeUrl, route: '/kingdom/farm' },
  { id: 'marketplace', label: 'Marketplace', desc: 'Improve Shops and Consumables', x: 47.9, y: 46.5, size: 4, hitX: 48.5, hitY: 52.2, hitW: 22.2, hitH: 26, icon: marketplaceNodeUrl, route: '/kingdom/marketplace' },
  { id: 'blacksmith', label: 'Blacksmith', desc: 'Improve Upgrades and Equipment', x: 73.1, y: 65.5, size: 4, hitX: 80.2, hitY: 71, hitW: 27.2, hitH: 29.6, icon: blacksmithNodeUrl, route: '/kingdom/blacksmith' },
]

// Reactive working copy so the dev positioning tool can mutate coordinates.
const nodes = reactive(DEFAULT_NODES.map((n) => ({ ...n })))

// The badge (visual icon). Centered on x/y; `size` drives width, height auto.
function nodeStyle(n: Node) {
  return { left: `${n.x}%`, top: `${n.y}%`, width: `${n.size}%` }
}

// The clickable hit area — its own center (hitX/hitY) and size (hitW/hitH).
function hitStyle(n: Node) {
  return { left: `${n.hitX}%`, top: `${n.hitY}%`, width: `${n.hitW}%`, height: `${n.hitH}%` }
}

function onSelect(n: Node) {
  if (editing.value) return // dragging must not also navigate
  router.push(n.route)
}

function onBack() {
  router.push('/war-room')
}

/* ----------------------------------------------------------------
 * Dev-only node positioning tool ("micro-adjust") — mirrors WarRoom.vue.
 * Flip NODE_EDITOR_ENABLED to false to bake in the current DEFAULT_NODES and
 * remove the authoring surface.
 * ---------------------------------------------------------------- */
const NODE_EDITOR_ENABLED = false

const editing = ref(false)
const editId = ref<string | null>(null) // currently selected node
const editKind = ref<'icon' | 'hit'>('icon') // which piece arrow keys / highlight target
const sceneRef = ref<HTMLElement | null>(null)

// The eight hit-area resize handles. dx/dy pick which axes a handle affects
// (-1/1 = that edge, 0 = axis untouched); corners drive both.
const RESIZE_HANDLES = [
  { name: 'n', dx: 0, dy: -1 },
  { name: 's', dx: 0, dy: 1 },
  { name: 'e', dx: 1, dy: 0 },
  { name: 'w', dx: -1, dy: 0 },
  { name: 'ne', dx: 1, dy: -1 },
  { name: 'nw', dx: -1, dy: -1 },
  { name: 'se', dx: 1, dy: 1 },
  { name: 'sw', dx: -1, dy: 1 },
] as const

type DragMode =
  | { kind: 'move-icon' }
  | { kind: 'move-hit' }
  | { kind: 'resize-hit'; dx: number; dy: number }
let dragMode: DragMode | null = null
let dragTarget: Node | null = null
let dragMoved = false

const round1 = (v: number) => Math.round(v * 10) / 10
const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v))

function onPointerMove(e: PointerEvent) {
  if (!dragTarget || !dragMode || !sceneRef.value) return
  dragMoved = true
  const rect = sceneRef.value.getBoundingClientRect()
  const px = ((e.clientX - rect.left) / rect.width) * 100
  const py = ((e.clientY - rect.top) / rect.height) * 100
  if (dragMode.kind === 'move-icon') {
    dragTarget.x = round1(clamp(px, 0, 100))
    dragTarget.y = round1(clamp(py, 0, 100))
  } else if (dragMode.kind === 'move-hit') {
    dragTarget.hitX = round1(clamp(px, 0, 100))
    dragTarget.hitY = round1(clamp(py, 0, 100))
  } else {
    if (dragMode.dx !== 0) dragTarget.hitW = round1(clamp(2 * Math.abs(px - dragTarget.hitX), 1, 100))
    if (dragMode.dy !== 0) dragTarget.hitH = round1(clamp(2 * Math.abs(py - dragTarget.hitY), 1, 100))
  }
}

function endDrag() {
  if (dragTarget && dragMoved) logConfig()
  dragTarget = null
  dragMode = null
  window.removeEventListener('pointermove', onPointerMove)
  window.removeEventListener('pointerup', endDrag)
}

function beginDrag(e: PointerEvent, n: Node, mode: DragMode) {
  if (!editing.value) return
  e.preventDefault()
  e.stopPropagation()
  editId.value = n.id
  editKind.value = mode.kind === 'move-icon' ? 'icon' : 'hit'
  dragTarget = n
  dragMode = mode
  dragMoved = false
  window.addEventListener('pointermove', onPointerMove)
  window.addEventListener('pointerup', endDrag)
}

function onIconDown(e: PointerEvent, n: Node) {
  beginDrag(e, n, { kind: 'move-icon' })
}

function onPointerDown(e: PointerEvent, n: Node) {
  beginDrag(e, n, { kind: 'move-hit' })
}

function onHandleDown(e: PointerEvent, n: Node, dx: number, dy: number) {
  beginDrag(e, n, { kind: 'resize-hit', dx, dy })
}

function onWheel(e: WheelEvent, n: Node) {
  if (!editing.value) return
  e.preventDefault()
  editId.value = n.id
  editKind.value = 'icon'
  const step = (e.shiftKey ? 0.1 : 0.5) * (e.deltaY < 0 ? 1 : -1)
  n.size = round1(clamp(n.size + step, 1, 60))
  logConfig()
}

function onKeyDown(e: KeyboardEvent) {
  if (!editing.value || !editId.value) return
  const target = nodes.find((n) => n.id === editId.value)
  if (!target) return

  if (e.key === '+' || e.key === '=' || e.key === '-' || e.key === '_') {
    e.preventDefault()
    const grow = e.key === '+' || e.key === '='
    const step = (e.shiftKey ? 0.1 : 0.5) * (grow ? 1 : -1)
    target.size = round1(clamp(target.size + step, 1, 60))
    logConfig()
    return
  }

  const step = e.shiftKey ? 1 : 0.1
  const dx = e.key === 'ArrowLeft' ? -step : e.key === 'ArrowRight' ? step : 0
  const dy = e.key === 'ArrowUp' ? -step : e.key === 'ArrowDown' ? step : 0
  if (dx === 0 && dy === 0) return
  e.preventDefault()
  if (editKind.value === 'icon') {
    target.x = round1(clamp(target.x + dx, 0, 100))
    target.y = round1(clamp(target.y + dy, 0, 100))
  } else {
    target.hitX = round1(clamp(target.hitX + dx, 0, 100))
    target.hitY = round1(clamp(target.hitY + dy, 0, 100))
  }
  logConfig()
}

// Map each node back to its imported icon var name for the copied config line.
const ICON_VAR: Record<string, string> = {
  townHall: 'kingdomNodeUrl',
  barracks: 'barracksNodeUrl',
  chapel: 'chapelNodeUrl',
  farm: 'farmNodeUrl',
  marketplace: 'marketplaceNodeUrl',
  blacksmith: 'blacksmithNodeUrl',
}

function configText(): string {
  const lines = nodes.map((n) => {
    const icon = ICON_VAR[n.id] ?? 'kingdomNodeUrl'
    return `  { id: '${n.id}', label: '${n.label}', desc: '${n.desc}', x: ${n.x}, y: ${n.y}, size: ${n.size}, hitX: ${n.hitX}, hitY: ${n.hitY}, hitW: ${n.hitW}, hitH: ${n.hitH}, icon: ${icon}, route: '${n.route}' },`
  })
  return `const DEFAULT_NODES: Node[] = [\n${lines.join('\n')}\n]`
}

function logConfig() {
  // eslint-disable-next-line no-console
  console.log(`[Kingdom nodes]\n${configText()}`)
}

function copyConfig() {
  navigator.clipboard?.writeText(configText())
  logConfig()
}

onMounted(() => {
  if (NODE_EDITOR_ENABLED) window.addEventListener('keydown', onKeyDown)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeyDown)
  window.removeEventListener('pointermove', onPointerMove)
  window.removeEventListener('pointerup', endDrag)
})
</script>

<style scoped>
.kingdom {
  position: relative;
  z-index: 1;
  width: 100%;
  height: 100%;
  overflow: hidden;
  background-color: #05080d;
}

.kingdom__back {
  position: absolute;
  top: 50px;
  left: 50px;
  z-index: 2;
}

/* Larger exit icon (2x the base) pinned to the top-left, matching meta views. */
.kingdom__back :deep(.exit-button) {
  width: 112px;
  height: 112px;
}

.kingdom__stage {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}

/*
 * Cover-style sizing: the scene preserves the background's aspect ratio
 * and grows until it covers the viewport on both axes — no letterbox bars.
 * Any overflow is clipped by the stage. Nodes are positioned by percentage
 * relative to the scene, so they stay locked to the artwork at any window
 * aspect ratio.
 */
.kingdom__scene {
  position: relative;
  aspect-ratio: 2064 / 1152;
  min-width: 100%;
  min-height: 100%;
  background-size: 100% 100%;
  background-position: center;
  background-repeat: no-repeat;
  image-rendering: pixelated;
}

.kingdom__hotspots {
  position: absolute;
  inset: 0;
}

/*
 * Each node is two independently positioned layers:
 *   .kingdom__hit  — a transparent button (the clickable area), centered at
 *                    hitX/hitY, sized hitW × hitH.
 *   .kingdom__node — the visual badge + plate, centered at x/y, sized by
 *                    `size`. pointer-events:none so clicks fall through to the
 *                    hit box (except in edit mode, where it becomes draggable).
 * Hover/focus of the hit box lights the adjacent badge (DOM siblings, hit first).
 */
.kingdom__hit {
  position: absolute;
  transform: translate(-50%, -50%);
  padding: 0;
  border: 0;
  background-color: transparent;
}

.kingdom__hit:focus-visible {
  outline: none;
}

.kingdom__node {
  position: absolute;
  transform: translate(-50%, -50%);
  display: flex;
  flex-direction: column;
  align-items: center;
  pointer-events: none;
}

.kingdom__node--editing {
  pointer-events: auto;
}

/* The badge image, sitting on top of the plate below it. */
.kingdom__badge {
  position: relative;
  z-index: 1;
  width: 100%;
  height: auto;
  display: block;
  margin-bottom: -6px;
  filter: drop-shadow(0 2px 4px rgba(0, 0, 0, 0.6))
    drop-shadow(0 0 6px rgba(212, 168, 71, 0.35));
  transition:
    transform 160ms cubic-bezier(0.34, 1.56, 0.64, 1),
    filter 160ms ease;
}

.kingdom__hit:hover + .kingdom__node .kingdom__badge,
.kingdom__hit:focus-visible + .kingdom__node .kingdom__badge {
  transform: translateY(-3px) scale(1.06);
  filter: drop-shadow(0 4px 8px rgba(0, 0, 0, 0.7))
    drop-shadow(0 0 14px rgba(255, 220, 140, 0.85));
  animation: node-pulse 1.8s ease-in-out infinite;
}

@keyframes node-pulse {
  0%,
  100% {
    filter: drop-shadow(0 4px 8px rgba(0, 0, 0, 0.7))
      drop-shadow(0 0 12px rgba(255, 220, 140, 0.6));
  }
  50% {
    filter: drop-shadow(0 4px 8px rgba(0, 0, 0, 0.7))
      drop-shadow(0 0 20px rgba(255, 220, 140, 0.95));
  }
}

/*
 * Plate — the dark backing box holding the title + hint. Semi-transparent so
 * the art shows through, with a faint gold hairline and a soft outer shadow so
 * its edges read shadowy rather than as a hard box.
 */
.kingdom__plate {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 12px 16px 9px;
  border-radius: 6px;
  background: radial-gradient(
    ellipse at center,
    rgba(8, 7, 5, 0.62) 0%,
    rgba(8, 7, 5, 0.55) 62%,
    rgba(8, 7, 5, 0.3) 100%
  );
  box-shadow:
    0 3px 12px rgba(0, 0, 0, 0.6),
    inset 0 0 0 1px rgba(196, 164, 96, 0.22);
}

/* Title — the building name. */
.kingdom__label {
  font-family: var(--font-title);
  font-size: clamp(11px, 1vw, 17px);
  font-weight: 700;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  white-space: nowrap;
  color: #f4d27a;
  text-shadow: 0 1px 3px rgba(0, 0, 0, 0.9);
  pointer-events: none;
  transition:
    color 140ms ease,
    text-shadow 140ms ease,
    transform 140ms ease;
}

/* Hint — one line describing what the building does. */
.kingdom__desc {
  margin-top: 3px;
  font-family: var(--font-body, var(--font-title));
  font-size: clamp(8px, 0.68vw, 11px);
  font-weight: 500;
  line-height: 1.2;
  white-space: nowrap;
  text-align: center;
  color: #d8c8a2;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.9);
  pointer-events: none;
}

.kingdom__hit:hover + .kingdom__node .kingdom__label,
.kingdom__hit:focus-visible + .kingdom__node .kingdom__label {
  color: #ffe9a8;
  text-shadow:
    0 1px 3px rgba(0, 0, 0, 0.9),
    0 0 12px rgba(255, 220, 140, 0.85);
}

/* --- Dev-only positioning tool --- */
.kingdom__hit--editing {
  outline: 2px dashed rgba(106, 178, 255, 0.7);
  outline-offset: -1px;
  background-color: rgba(106, 178, 255, 0.08);
}

.kingdom__hit--active {
  outline: 2px solid rgba(106, 178, 255, 0.95);
  background-color: rgba(106, 178, 255, 0.14);
}

.kingdom__node--editing .kingdom__badge {
  outline: 1px dashed rgba(255, 220, 140, 0.7);
  outline-offset: 2px;
}

.kingdom__node--active .kingdom__badge {
  outline: 2px solid rgba(255, 220, 140, 0.95);
  outline-offset: 2px;
}

.kingdom__handle {
  position: absolute;
  width: 12px;
  height: 12px;
  background: #6ab2ff;
  border: 1px solid rgba(0, 0, 0, 0.7);
  border-radius: 2px;
  pointer-events: auto;
  z-index: 2;
}

.kingdom__handle--n { top: 0; left: 50%; transform: translate(-50%, -50%); }
.kingdom__handle--s { top: 100%; left: 50%; transform: translate(-50%, -50%); }
.kingdom__handle--e { top: 50%; left: 100%; transform: translate(-50%, -50%); }
.kingdom__handle--w { top: 50%; left: 0; transform: translate(-50%, -50%); }
.kingdom__handle--ne { top: 0; left: 100%; transform: translate(-50%, -50%); }
.kingdom__handle--nw { top: 0; left: 0; transform: translate(-50%, -50%); }
.kingdom__handle--se { top: 100%; left: 100%; transform: translate(-50%, -50%); }
.kingdom__handle--sw { top: 100%; left: 0; transform: translate(-50%, -50%); }

.kingdom__coords {
  margin-top: 6px;
  padding: 1px 5px;
  font-family: var(--font-mono, monospace);
  font-size: 11px;
  white-space: nowrap;
  color: #ffe9a8;
  background: rgba(0, 0, 0, 0.7);
  border-radius: 3px;
  pointer-events: none;
}

/* Authoring toolbar — top-right, dev builds only (top so it clears the
   bottom-heavy town art / the back button at top-left). */
.kingdom__edit-bar {
  position: absolute;
  right: 16px;
  top: 16px;
  z-index: 4;
  display: flex;
  gap: 10px;
  align-items: center;
}

.kingdom__edit-hint {
  padding: 6px 12px;
  font-family: var(--font-mono, monospace);
  font-size: 12px;
  letter-spacing: 0.04em;
  color: #ffe9a8;
  background: rgba(0, 0, 0, 0.75);
  border: 1px solid rgba(255, 220, 140, 0.4);
  border-radius: 4px;
}

.kingdom__edit-toggle,
.kingdom__edit-copy {
  padding: 6px 12px;
  font-family: var(--font-mono, monospace);
  font-size: 12px;
  color: #05080d;
  background: #f4d27a;
  border: 1px solid #f4d27a;
  border-radius: 4px;
}

.kingdom__edit-toggle {
  color: #ffe9a8;
  background: rgba(0, 0, 0, 0.75);
  border-color: rgba(255, 220, 140, 0.4);
}

.kingdom__edit-toggle.is-on {
  color: #05080d;
  background: #f4d27a;
  border-color: #f4d27a;
}
</style>
