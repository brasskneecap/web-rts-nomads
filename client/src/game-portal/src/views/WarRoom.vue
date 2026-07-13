<template>
  <div class="war-room">
    <div class="war-room__back">
      <ExitButton destination="Main Menu" @click="onBack" />
    </div>

    <!-- Dev-only authoring toolbar. Pinned to the viewport (NOT inside the
         cover-fit scene, whose corners get cropped). Toggle edit mode, then
         drag a node to place it, wheel to resize, or select one and nudge with
         arrow keys (Shift = coarse) for pixel-perfect micro-adjustment. -->
    <div v-if="HOTSPOT_EDITOR_ENABLED" class="war-room__edit-bar">
      <button
        type="button"
        class="war-room__edit-toggle"
        :class="{ 'is-on': editing }"
        @click="editing = !editing"
      >
        {{ editing ? '✓ Done' : '✎ Edit nodes' }}
      </button>
      <template v-if="editing">
        <span class="war-room__edit-hint">
          drag icon / drag box = move each · handles = box size · wheel or +/− = icon size · arrows = nudge last
        </span>
        <button type="button" class="war-room__edit-copy" @click="copyConfig">
          Copy config
        </button>
      </template>
    </div>

    <div class="war-room__stage">
      <div
        ref="sceneRef"
        class="war-room__scene"
        :style="{ backgroundImage: `url(${warRoomBgUrl})` }"
      >
        <div class="war-room__hotspots">
          <template v-for="h in hotspots" :key="h.id">
            <!-- Transparent hit box: the actual clickable/interactive area,
                 sized (hitW × hitH) independently of the badge. In edit mode it
                 shows a dashed outline plus drag handles to resize it. -->
            <button
              type="button"
              class="war-room__hit"
              :class="{
                'war-room__hit--editing': editing,
                'war-room__hit--active': editId === h.id,
              }"
              :style="hitStyle(h)"
              :aria-label="h.label"
              @click="onSelect(h)"
              @pointerdown="onPointerDown($event, h)"
              @wheel="onWheel($event, h)"
            >
              <template v-if="editing">
                <span
                  v-for="hd in RESIZE_HANDLES"
                  :key="hd.name"
                  class="war-room__handle"
                  :class="`war-room__handle--${hd.name}`"
                  @pointerdown="onHandleDown($event, h, hd.dx, hd.dy)"
                ></span>
              </template>
            </button>

            <!-- Visual layer: badge medallion + label. Normally non-interactive
                 (clicks fall through to the hit box). In edit mode it becomes
                 draggable to move the icon independently of the hit box. `size`
                 drives the badge width; it glows on hover of the hit box and
                 stays lit for the active (selected) tab. -->
            <div
              class="war-room__node"
              :class="{
                'war-room__node--selected': isSelected(h),
                'war-room__node--editing': editing,
                'war-room__node--active': editing && editId === h.id && editKind === 'icon',
              }"
              :style="nodeStyle(h)"
              @pointerdown="onIconDown($event, h)"
              @wheel="onWheel($event, h)"
            >
              <img class="war-room__badge" :src="h.icon" :alt="h.label" draggable="false" />

              <!-- Dark plate with the title + a one-line hint of what the node
                   does; the badge sits on top of the plate (see reference art). -->
              <div class="war-room__plate">
                <span class="war-room__label">{{ h.label }}</span>
                <span class="war-room__desc">{{ h.desc }}</span>
              </div>

              <!-- Dev-only live coordinate readout while positioning. -->
              <span v-if="editing" class="war-room__coords">
                icon {{ h.x }},{{ h.y }} · {{ h.size }} — hit {{ h.hitX }},{{ h.hitY }} · {{ h.hitW }}×{{ h.hitH }}
              </span>
            </div>
          </template>
        </div>

        <div
          class="war-room__page"
          :class="{
            'war-room__page--campaign': activeTab === 'campaign',
            'war-room__page--custom': activeTab === 'custom',
          }"
        >
          <Campaign v-if="activeTab === 'campaign'" @close="activeTab = null" />
          <CustomGame
            v-else-if="activeTab === 'custom'"
            :initial-tab="customSubTab"
            @close="activeTab = null"
          />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, onBeforeUnmount } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import ExitButton from '@/components/ui/ExitButton.vue'
import Campaign from '@/views/Campaign.vue'
import CustomGame from '@/views/CustomGame.vue'
import warRoomBgUrl from '@/assets/background-images/war_room_bg.png'
import kingdomNodeUrl from '@/assets/background-images/nodes/kingdom-node.png'
import campaignNodeUrl from '@/assets/background-images/nodes/campaign-node.png'
import customGameNodeUrl from '@/assets/background-images/nodes/custom-game-node.png'

const router = useRouter()
const route = useRoute()

/* ----------------------------------------------------------------
 * Nodes: visible badge medallions (nodes/*.png) placed over the points of
 * interest baked into war_room_bg.png. Each node is TWO independently
 * positioned pieces, both in % of the scene so they stay locked to the artwork
 * at any window aspect ratio (mirrors KingdomView.vue):
 *   • the badge icon — centered at (x, y), width `size`, with a text label;
 *   • the clickable area — a separate box centered at (hitX, hitY), size
 *     hitW × hitH. It need not sit under the icon, so you can float the badge
 *     above a building while the click target covers the building itself.
 *
 * `action` picks the behavior: 'route' pushes a route, 'tab' toggles an
 * inline panel (Campaign / Custom Game) in the parchment slot below.
 *
 * To re-align a node: use the dev authoring toolbar (bottom-right) to enter
 * Edit mode, drag the icon or the click box independently / wheel or +/- to
 * resize the icon / handles to resize the click box / arrow-key to micro-nudge,
 * then "Copy config" and paste the numbers back into DEFAULT_NODES.
 * ---------------------------------------------------------------- */
type HotspotAction = 'route' | 'tab'
interface Hotspot {
  id: string
  label: string
  desc: string // one-line hint shown under the label
  x: number // badge (icon) center X, % of scene
  y: number // badge (icon) center Y, % of scene
  size: number // badge (icon) width, % of scene
  hitX: number // clickable area center X, % of scene
  hitY: number // clickable area center Y, % of scene
  hitW: number // clickable area width, % of scene
  hitH: number // clickable area height, % of scene
  icon: string
  action: HotspotAction
  route?: string
  tab?: string
}

const DEFAULT_NODES: Hotspot[] = [
  { id: 'kingdom', label: 'Kingdom', desc: 'Manage and develop your realm.', x: 49.9, y: 45.2, size: 4, hitX: 49.6, hitY: 40.7, hitW: 11.8, hitH: 18.8, icon: kingdomNodeUrl, action: 'route', route: '/kingdom' },
  { id: 'custom', label: 'Custom Game', desc: 'Create or join custom battles.', x: 74.9, y: 68.6, size: 4, hitX: 73.1, hitY: 66.6, hitW: 14.5, hitH: 13.8, icon: customGameNodeUrl, action: 'tab', tab: 'custom' },
  { id: 'campaign', label: 'Campaign', desc: 'Play through the main story.', x: 28.1, y: 49.9, size: 4, hitX: 31.3, hitY: 55.1, hitW: 15.5, hitH: 18.1, icon: campaignNodeUrl, action: 'tab', tab: 'campaign' },
]

// Reactive working copy so the dev positioning tool can mutate coordinates.
const hotspots = reactive(DEFAULT_NODES.map((h) => ({ ...h })))

// The badge (visual icon). Centered on x/y; `size` drives width, height auto.
function nodeStyle(h: Hotspot) {
  return { left: `${h.x}%`, top: `${h.y}%`, width: `${h.size}%` }
}

// The clickable hit area — its own center (hitX/hitY) and size (hitW/hitH),
// fully independent of the badge.
function hitStyle(h: Hotspot) {
  return { left: `${h.hitX}%`, top: `${h.hitY}%`, width: `${h.hitW}%`, height: `${h.hitH}%` }
}

// In-room tab state. The hotspots act as a tab bar: selecting one renders its
// content inline in the parchment slot instead of pushing a nested route, so
// Back always returns to the main menu rather than a /war-room/* sub-route.
// `null` means no tab open (bare room with just the hotspots showing).
const activeTab = ref<string | null>(null)

// Which Custom Game sub-tab to open when the custom panel mounts. Seeded from
// the `?sub=` query so lobby-return / deep-link flows can land directly on
// Find Game or Direct Connect (the old standalone routes now redirect here).
type CustomSubTab = 'start' | 'find' | 'direct'
const customSubTab = ref<CustomSubTab>('start')

// Honor `?tab=custom&sub=<start|find|direct>` on mount so redirects from the
// removed /custom, /create-game, /find-game and /direct-connect routes (and
// the leave-lobby flow) open the right panel/sub-tab.
if (route.query.tab === 'custom') {
  activeTab.value = 'custom'
  const sub = route.query.sub
  if (sub === 'find' || sub === 'direct' || sub === 'start') {
    customSubTab.value = sub
  }
}

function isSelected(h: Hotspot): boolean {
  return h.action === 'tab' && activeTab.value === h.tab
}

// Toggle the tab: clicking the active hotspot again closes it back to the
// bare room.
function selectTab(tab: string) {
  activeTab.value = activeTab.value === tab ? null : tab
}

function onSelect(h: Hotspot) {
  // Dragging in positioning mode must not also navigate.
  if (editing.value) return
  if (h.action === 'route' && h.route) {
    router.push(h.route)
  } else if (h.action === 'tab' && h.tab) {
    selectTab(h.tab)
  }
}

function onBack() {
  router.push('/')
}

/* ----------------------------------------------------------------
 * Dev-only node positioning tool ("micro-adjust").
 *
 * The authoring toolbar (bottom-right, dev builds only) toggles Edit mode.
 * While it's on:
 *   • drag a node to move it (x/y),
 *   • mouse-wheel over a node to resize the BADGE icon (size),
 *   • drag the edge/corner handles to resize the CLICKABLE AREA (hitW/hitH),
 *     independently of the badge,
 *   • click a node to select it, then nudge with the arrow keys — 0.1% per
 *     press for fine micro-adjustment, or Shift+arrow for 1% coarse steps.
 * Every change logs a paste-ready DEFAULT_NODES block; "Copy config" puts it
 * on the clipboard so you can paste the numbers back into the source above.
 *
 * Gated on import.meta.env.DEV && HOTSPOT_EDITOR_ENABLED so the listeners and
 * toolbar never attach in packaged builds — zero authoring surface in prod.
 * ---------------------------------------------------------------- */
const HOTSPOT_EDITOR_ENABLED = false

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

// Active drag gesture: move the icon (x/y), move the hit box (hitX/hitY), or
// resize the hit box along axes.
type DragMode =
  | { kind: 'move-icon' }
  | { kind: 'move-hit' }
  | { kind: 'resize-hit'; dx: number; dy: number }
let dragMode: DragMode | null = null
let dragTarget: Hotspot | null = null
let dragMoved = false

const round1 = (n: number) => Math.round(n * 10) / 10
const clamp = (n: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, n))

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
    // Resize the hit box symmetrically about its center (hitX/hitY): each
    // active edge grows the box to twice the center-to-pointer distance.
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

function beginDrag(e: PointerEvent, h: Hotspot, mode: DragMode) {
  if (!editing.value) return
  e.preventDefault()
  e.stopPropagation()
  editId.value = h.id
  editKind.value = mode.kind === 'move-icon' ? 'icon' : 'hit'
  dragTarget = h
  dragMode = mode
  dragMoved = false
  window.addEventListener('pointermove', onPointerMove)
  window.addEventListener('pointerup', endDrag)
}

// Drag the badge → move the icon.
function onIconDown(e: PointerEvent, h: Hotspot) {
  beginDrag(e, h, { kind: 'move-icon' })
}

// Drag the hit box body → move the clickable area.
function onPointerDown(e: PointerEvent, h: Hotspot) {
  beginDrag(e, h, { kind: 'move-hit' })
}

// Drag an edge/corner handle → resize the clickable area.
function onHandleDown(e: PointerEvent, h: Hotspot, dx: number, dy: number) {
  beginDrag(e, h, { kind: 'resize-hit', dx, dy })
}

// Mouse-wheel resizes the badge icon (Shift+wheel = fine 0.1% steps).
function onWheel(e: WheelEvent, h: Hotspot) {
  if (!editing.value) return
  e.preventDefault()
  editId.value = h.id
  editKind.value = 'icon'
  const step = (e.shiftKey ? 0.1 : 0.5) * (e.deltaY < 0 ? 1 : -1)
  h.size = round1(clamp(h.size + step, 1, 60))
  logConfig()
}

// Keyboard controls for the selected node (no precise hovering needed):
//   • arrows           — micro-nudge the last-touched piece (icon or hit box):
//                        0.1% fine, Shift = 1% coarse
//   • + / =  and  -    — resize the badge icon (0.5%, Shift = fine 0.1%)
// Which piece the arrows move follows editKind (set by the last drag/wheel):
// drag the badge → arrows move the icon; drag/resize the hit box → arrows move
// the click area.
function onKeyDown(e: KeyboardEvent) {
  if (!editing.value || !editId.value) return
  const target = hotspots.find((h) => h.id === editId.value)
  if (!target) return

  // Icon resize: '+'/'=' grow, '-'/'_' shrink.
  if (e.key === '+' || e.key === '=' || e.key === '-' || e.key === '_') {
    e.preventDefault()
    const grow = e.key === '+' || e.key === '='
    const step = (e.shiftKey ? 0.1 : 0.5) * (grow ? 1 : -1)
    target.size = round1(clamp(target.size + step, 1, 60))
    logConfig()
    return
  }

  // Position nudge — icon (x/y) or hit box (hitX/hitY) per editKind.
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

// Map each node back to its imported icon var name so the copied config line
// references the right import (custom's asset is customGameNodeUrl).
const ICON_VAR: Record<string, string> = {
  kingdom: 'kingdomNodeUrl',
  custom: 'customGameNodeUrl',
  campaign: 'campaignNodeUrl',
}

function configText(): string {
  const lines = hotspots.map((h) => {
    const extra =
      h.action === 'route'
        ? `action: 'route', route: '${h.route}'`
        : `action: 'tab', tab: '${h.tab}'`
    const icon = ICON_VAR[h.id] ?? 'kingdomNodeUrl'
    return `  { id: '${h.id}', label: '${h.label}', desc: '${h.desc}', x: ${h.x}, y: ${h.y}, size: ${h.size}, hitX: ${h.hitX}, hitY: ${h.hitY}, hitW: ${h.hitW}, hitH: ${h.hitH}, icon: ${icon}, ${extra} },`
  })
  return `const DEFAULT_NODES: Hotspot[] = [\n${lines.join('\n')}\n]`
}

function logConfig() {
  // eslint-disable-next-line no-console
  console.log(`[WarRoom nodes]\n${configText()}`)
}

function copyConfig() {
  navigator.clipboard?.writeText(configText())
  logConfig()
}

onMounted(() => {
  if (HOTSPOT_EDITOR_ENABLED) window.addEventListener('keydown', onKeyDown)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeyDown)
  window.removeEventListener('pointermove', onPointerMove)
  window.removeEventListener('pointerup', endDrag)
})
</script>

<style scoped>
.war-room {
  position: relative;
  z-index: 1;
  width: 100%;
  height: 100%;
  overflow: hidden;
  background-color: #05080d;
}

.war-room__back {
  position: absolute;
  top: 50px;
  left: 50px;
  z-index: 2;
}

.war-room__stage {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}

/*
 * Cover-style sizing: the scene preserves the background's aspect ratio
 * and grows until it covers the viewport on both axes. Overflow is clipped
 * by the stage. This lets us position hotspots in percentages and have
 * them stay locked to the artwork regardless of window aspect ratio.
 *
 * `--scene-min-width` is a hard floor: once the viewport gets narrower than
 * this, the scene stops shrinking and the stage crops it symmetrically on
 * the left/right (and top/bottom if needed) instead — so the active items
 * never shrink past a usable size. The locked aspect-ratio keeps the floor
 * proportional, so the artwork never distorts. Raise this number to crop
 * sooner / keep items larger; lower it to allow more shrinkage.
 */
.war-room__scene {
  --scene-min-width: 1280px;
  position: relative;
  aspect-ratio: 1162 / 830;
  min-width: max(100%, var(--scene-min-width));
  min-height: 100%;
  background-size: 100% 100%;
  background-position: center;
  background-repeat: no-repeat;
  image-rendering: pixelated;
}

.war-room__hotspots {
  position: absolute;
  inset: 0;
}

.war-room__page {
  position: absolute;
  left: calc(22% + 75px);
  right: calc(22% - 75px);
  top: 46%;
  bottom: 26%;
  pointer-events: none;
  /*
   * Establish a size query container so the nested page (Advancements) can
   * size its contents in container-query units and scale 1:1 with the
   * parchment slot — which itself already tracks the cover-fit scene.
   * `container-type: size` applies size/layout/style containment but NOT
   * paint, so node tooltips can still extend above the panel bounds.
   */
  container-type: size;
}

/* Campaign and Custom Game use a taller slot — they list content vertically
   and need more headroom than Advancements' single-row layout. Top is raised;
   the bottom edge is pushed 50px lower than the default slot so the panel
   extends further down while keeping the same horizontal bounds. */
.war-room__page--campaign,
.war-room__page--custom {
  top: calc(18% + 50px);
  bottom: calc(26% - 50px);
}

.war-room__page :deep(> *) {
  pointer-events: auto;
}

/*
 * Each node is two independently positioned layers:
 *   .war-room__hit  — a transparent button (the clickable/interactive area),
 *                     centered at hitX/hitY, sized hitW × hitH.
 *   .war-room__node — the visual badge + label, centered at x/y, sized by
 *                     `size`. It is pointer-events:none so clicks fall through
 *                     to the hit box (except in edit mode, where it becomes
 *                     draggable to reposition the icon on its own).
 * Hover/focus of the hit box lights the adjacent badge (they're DOM siblings,
 * hit first); the active (selected) tab stays lit and retints blue.
 */
.war-room__hit {
  position: absolute;
  transform: translate(-50%, -50%);
  padding: 0;
  border: 0;
  background-color: transparent;
}

.war-room__hit:focus-visible {
  outline: none;
}

.war-room__node {
  position: absolute;
  transform: translate(-50%, -50%);
  display: flex;
  flex-direction: column;
  align-items: center;
  pointer-events: none;
}

/* In edit mode the badge becomes draggable so the icon can be moved apart
   from the click box. */
.war-room__node--editing {
  pointer-events: auto;
}

/* The badge image. A steady golden drop-shadow reads it as an interactive
   marker; hover brightens and lifts it. */
.war-room__badge {
  position: relative;
  z-index: 1; /* sit on top of the plate below */
  width: 100%;
  height: auto;
  display: block;
  margin-bottom: -6px; /* overlap the top of the plate, as in the reference */
  filter: drop-shadow(0 2px 4px rgba(0, 0, 0, 0.6))
    drop-shadow(0 0 6px rgba(212, 168, 71, 0.35));
  transition:
    transform 160ms cubic-bezier(0.34, 1.56, 0.64, 1),
    filter 160ms ease;
}

.war-room__hit:hover + .war-room__node .war-room__badge,
.war-room__hit:focus-visible + .war-room__node .war-room__badge {
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

/* Selected (active tab) — keep the badge lit and retint the glow blue. */
.war-room__node--selected .war-room__badge {
  filter: drop-shadow(0 2px 4px rgba(0, 0, 0, 0.6))
    drop-shadow(0 0 16px rgba(106, 178, 255, 0.85));
}

/*
 * Plate — the dark backing box holding the title + hint. Near-opaque black
 * fill with a faint gold hairline and a soft outer shadow so its edges read
 * shadowy rather than as a hard box (mirrors the reference art). Top padding
 * leaves room for the badge that overlaps it.
 */
.war-room__plate {
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

/* Title — the node name. */
.war-room__label {
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

/* Hint — one line describing what the node does; wraps under the title. */
.war-room__desc {
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

.war-room__hit:hover + .war-room__node .war-room__label,
.war-room__hit:focus-visible + .war-room__node .war-room__label {
  color: #ffe9a8;
  text-shadow:
    0 1px 3px rgba(0, 0, 0, 0.9),
    0 0 12px rgba(255, 220, 140, 0.85);
}

.war-room__node--selected .war-room__label {
  color: #b8dcff;
  text-shadow:
    0 1px 3px rgba(0, 0, 0, 0.9),
    0 0 12px rgba(106, 178, 255, 0.7);
}

/* --- Dev-only positioning tool --- */

/* Clickable area: dashed box in edit mode; solid brighter ring when selected
   (the arrow-key target). Blue so it reads apart from the gold badge outline. */
.war-room__hit--editing {
  outline: 2px dashed rgba(106, 178, 255, 0.7);
  outline-offset: -1px;
  background-color: rgba(106, 178, 255, 0.08);
}

.war-room__hit--active {
  outline: 2px solid rgba(106, 178, 255, 0.95);
  background-color: rgba(106, 178, 255, 0.14);
}

/* Badge (icon) bounds in edit mode — gold dashed, to distinguish from the
   blue hit box; solid brighter ring when the icon is the arrow-key target. */
.war-room__node--editing .war-room__badge {
  outline: 1px dashed rgba(255, 220, 140, 0.7);
  outline-offset: 2px;
}

.war-room__node--active .war-room__badge {
  outline: 2px solid rgba(255, 220, 140, 0.95);
  outline-offset: 2px;
}

/* Hit-area resize handles — small grabbable squares on each edge/corner. */
.war-room__handle {
  position: absolute;
  width: 12px;
  height: 12px;
  background: #6ab2ff;
  border: 1px solid rgba(0, 0, 0, 0.7);
  border-radius: 2px;
  pointer-events: auto;
  z-index: 2;
}

.war-room__handle--n { top: 0; left: 50%; transform: translate(-50%, -50%); }
.war-room__handle--s { top: 100%; left: 50%; transform: translate(-50%, -50%); }
.war-room__handle--e { top: 50%; left: 100%; transform: translate(-50%, -50%); }
.war-room__handle--w { top: 50%; left: 0; transform: translate(-50%, -50%); }
.war-room__handle--ne { top: 0; left: 100%; transform: translate(-50%, -50%); }
.war-room__handle--nw { top: 0; left: 0; transform: translate(-50%, -50%); }
.war-room__handle--se { top: 100%; left: 100%; transform: translate(-50%, -50%); }
.war-room__handle--sw { top: 100%; left: 0; transform: translate(-50%, -50%); }

.war-room__coords {
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

/* Authoring toolbar — bottom-right, dev builds only. */
.war-room__edit-bar {
  position: absolute;
  right: 16px;
  bottom: 16px;
  z-index: 4;
  display: flex;
  gap: 10px;
  align-items: center;
}

.war-room__edit-hint {
  padding: 6px 12px;
  font-family: var(--font-mono, monospace);
  font-size: 12px;
  letter-spacing: 0.04em;
  color: #ffe9a8;
  background: rgba(0, 0, 0, 0.75);
  border: 1px solid rgba(255, 220, 140, 0.4);
  border-radius: 4px;
}

.war-room__edit-toggle,
.war-room__edit-copy {
  padding: 6px 12px;
  font-family: var(--font-mono, monospace);
  font-size: 12px;
  color: #ffe9a8;
  background: rgba(0, 0, 0, 0.75);
  border: 1px solid rgba(255, 220, 140, 0.4);
  border-radius: 4px;
}

.war-room__edit-toggle.is-on {
  color: #05080d;
  background: #f4d27a;
  border-color: #f4d27a;
}

.war-room__edit-copy {
  color: #05080d;
  background: #f4d27a;
  border-color: #f4d27a;
}
</style>
