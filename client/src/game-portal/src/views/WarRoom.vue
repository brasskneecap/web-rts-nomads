<template>
  <div class="war-room">
    <div class="war-room__back">
      <ExitButton aria-label="Back" @click="onBack" />
    </div>

    <div class="war-room__stage">
      <div
        ref="sceneRef"
        class="war-room__scene"
        :style="{ backgroundImage: `url(${warRoomBgUrl})` }"
      >
        <div class="war-room__hotspots">
          <button
            v-for="h in hotspots"
            :key="h.id"
            type="button"
            class="war-room__hotspot"
            :class="{
              'war-room__hotspot--selected': isSelected(h),
              'war-room__hotspot--editing': editing,
            }"
            :style="hotspotStyle(h)"
            :aria-label="h.label"
            @click="onSelect(h)"
            @pointerdown="onPointerDown($event, h)"
            @wheel="onWheel($event, h)"
          >
            <!-- Orb & Beam: a soft halo orb over the point of interest with a
                 beam rising to the label — the same "highlight" FX the Kingdom
                 view uses. There is no icon: the artwork's own points of
                 interest show through, and the glow appears on hover. -->
            <span class="fx fx-beam"></span>
            <span class="fx fx-orb"></span>

            <span class="war-room__label">{{ h.label }}</span>

            <!-- Dev-only live coordinate readout while positioning. -->
            <span v-if="editing" class="war-room__coords">
              {{ h.x }}, {{ h.y }} · {{ h.w }}×{{ h.h }}
            </span>

            <!-- Dev-only resize handles: drag an edge/corner to reshape the
                 box. Resizes symmetrically about the center. -->
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
        </div>

        <!-- Dev-only authoring hint, shown while Alt is held. -->
        <div v-if="editing" class="war-room__edit-hint">
          <span>EDIT MODE — drag to place · wheel = width · shift+wheel = height</span>
          <button type="button" class="war-room__edit-copy" @click="copyConfig">
            Copy config
          </button>
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

const router = useRouter()
const route = useRoute()

/* ----------------------------------------------------------------
 * Hotspots: transparent buttons positioned over the points of interest
 * baked into war_room_bg.png. x/y are CENTER positions, w/h are size —
 * all percentages of the scene, so they stay locked to the artwork at any
 * window aspect ratio (mirrors KingdomView.vue).
 *
 * `action` picks the behavior: 'route' pushes a route, 'tab' toggles an
 * inline panel (Campaign / Custom Game) in the parchment slot below.
 *
 * To re-align a hotspot: hold Alt (dev builds only) to enter positioning
 * mode, drag it onto its point of interest, then use "Copy config" (or the
 * console log) and paste the numbers back into DEFAULT_HOTSPOTS.
 * ---------------------------------------------------------------- */
type HotspotAction = 'route' | 'tab'
interface Hotspot {
  id: string
  label: string
  x: number
  y: number
  w: number
  h: number
  action: HotspotAction
  route?: string
  tab?: string
}

const DEFAULT_HOTSPOTS: Hotspot[] = [
  { id: 'kingdom', label: 'Kingdom', x: 35.2, y: 43.8, w: 13.3, h: 21.3, action: 'route', route: '/kingdom' },
  { id: 'custom', label: 'Custom Game', x: 68.3, y: 64.6, w: 10.9, h: 13.2, action: 'tab', tab: 'custom' },
  { id: 'campaign', label: 'Campaign', x: 58.3, y: 46.6, w: 11.8, h: 15, action: 'tab', tab: 'campaign' },
]

// Reactive working copy so the dev positioning tool can mutate coordinates.
const hotspots = reactive(DEFAULT_HOTSPOTS.map((h) => ({ ...h })))

function hotspotStyle(h: Hotspot) {
  return { left: `${h.x}%`, top: `${h.y}%`, width: `${h.w}%`, height: `${h.h}%` }
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
 * Dev-only hotspot positioning tool.
 *
 * Hold Alt to enter positioning mode: hotspots become draggable, show a live
 * coordinate readout, and a "Copy config" affordance appears. Drag to place,
 * mouse-wheel to resize width (Shift+wheel for height). Every change logs a
 * paste-ready DEFAULT_HOTSPOTS block to the console.
 *
 * Gated on import.meta.env.DEV so the listeners never attach in packaged
 * builds — there is zero authoring surface in production.
 *
 * Flip HOTSPOT_EDITOR_ENABLED back to true to re-enable the Alt-drag tool.
 * All the machinery below stays intact; this just skips wiring up the
 * Alt key listener so nothing responds to it for now.
 * ---------------------------------------------------------------- */
const HOTSPOT_EDITOR_ENABLED = false

const editing = ref(false)
const sceneRef = ref<HTMLElement | null>(null)

// The eight resize handles. dx/dy pick which axes a handle affects
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

// Active drag gesture: either moving the whole box or resizing along axes.
type DragMode = { kind: 'move' } | { kind: 'resize'; dx: number; dy: number }
let dragMode: DragMode | null = null
let dragTarget: Hotspot | null = null

const round1 = (n: number) => Math.round(n * 10) / 10
const clamp = (n: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, n))

function onKeyDown(e: KeyboardEvent) {
  if (e.key === 'Alt') editing.value = true
}
function onKeyUp(e: KeyboardEvent) {
  if (e.key === 'Alt') editing.value = false
}

function onPointerMove(e: PointerEvent) {
  if (!dragTarget || !dragMode || !sceneRef.value) return
  const rect = sceneRef.value.getBoundingClientRect()
  const px = ((e.clientX - rect.left) / rect.width) * 100
  const py = ((e.clientY - rect.top) / rect.height) * 100
  if (dragMode.kind === 'move') {
    dragTarget.x = round1(clamp(px, 0, 100))
    dragTarget.y = round1(clamp(py, 0, 100))
  } else {
    // Symmetric about the center: the box grows to twice the distance from
    // its center to the pointer along each active axis.
    if (dragMode.dx !== 0) dragTarget.w = round1(clamp(2 * Math.abs(px - dragTarget.x), 1, 100))
    if (dragMode.dy !== 0) dragTarget.h = round1(clamp(2 * Math.abs(py - dragTarget.y), 1, 100))
  }
}

function endDrag() {
  if (dragTarget) logConfig()
  dragTarget = null
  dragMode = null
  window.removeEventListener('pointermove', onPointerMove)
  window.removeEventListener('pointerup', endDrag)
}

function beginDrag(e: PointerEvent, h: Hotspot, mode: DragMode) {
  if (!editing.value) return
  e.preventDefault()
  e.stopPropagation()
  dragTarget = h
  dragMode = mode
  window.addEventListener('pointermove', onPointerMove)
  window.addEventListener('pointerup', endDrag)
}

function onPointerDown(e: PointerEvent, h: Hotspot) {
  beginDrag(e, h, { kind: 'move' })
}

function onHandleDown(e: PointerEvent, h: Hotspot, dx: number, dy: number) {
  beginDrag(e, h, { kind: 'resize', dx, dy })
}

function onWheel(e: WheelEvent, h: Hotspot) {
  if (!editing.value) return
  e.preventDefault()
  const delta = e.deltaY < 0 ? 0.5 : -0.5
  if (e.shiftKey) h.h = round1(clamp(h.h + delta, 1, 100))
  else h.w = round1(clamp(h.w + delta, 1, 100))
  logConfig()
}

function configText(): string {
  const lines = hotspots.map((h) => {
    const extra =
      h.action === 'route'
        ? `action: 'route', route: '${h.route}'`
        : `action: 'tab', tab: '${h.tab}'`
    return `  { id: '${h.id}', label: '${h.label}', x: ${h.x}, y: ${h.y}, w: ${h.w}, h: ${h.h}, ${extra} },`
  })
  return `const DEFAULT_HOTSPOTS: Hotspot[] = [\n${lines.join('\n')}\n]`
}

function logConfig() {
  // eslint-disable-next-line no-console
  console.log(`[WarRoom hotspots]\n${configText()}`)
}

function copyConfig() {
  navigator.clipboard?.writeText(configText())
  logConfig()
}

onMounted(() => {
  if (import.meta.env.DEV && HOTSPOT_EDITOR_ENABLED) {
    window.addEventListener('keydown', onKeyDown)
    window.addEventListener('keyup', onKeyUp)
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeyDown)
  window.removeEventListener('keyup', onKeyUp)
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

/* Larger exit icon (2x the base) pinned to the top-left, matching meta views. */
.war-room__back :deep(.exit-button) {
  width: 112px;
  height: 112px;
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
 * Hotspots are invisible hit-targets over the map's points of interest — the
 * artwork shows through. The orb/beam "highlight" FX appears on hover/focus,
 * and stays lit for the active (selected) tab.
 */
.war-room__hotspot {
  position: absolute;
  transform: translate(-50%, -50%);
  padding: 0;
  border: 0;
  background-color: transparent;
}

.war-room__hotspot:focus-visible {
  outline: none;
}

/* Shared base for the orb/beam layers. */
.fx {
  position: absolute;
  left: 50%;
  pointer-events: none;
  opacity: 0;
  z-index: 0;
}

/*
 * Orb — a soft, edgeless halo of golden light centered under the label.
 * Anchored at (--orb-x, --orb-y) within the hotspot box; bottom + translateY(50%)
 * puts the orb CENTER on the anchor line. The glow uses drop-shadow (follows the
 * shape), and the hover pulse animates it. Colors are vars so the selected
 * (active-tab) state can retint the same shape blue.
 */
.fx-orb {
  --orb-size: 34px;
  --orb-soft: 5px;
  --orb-core: rgba(255, 243, 214, 0.95);
  --orb-mid: rgba(212, 168, 71, 0.55);
  --orb-glow: rgba(212, 168, 71, 0.5);
  left: var(--orb-x, 50%);
  bottom: calc(100% - var(--orb-y, 90%));
  width: var(--orb-size);
  height: var(--orb-size);
  border-radius: 50%;
  background: radial-gradient(
    circle at 50% 45%,
    var(--orb-core),
    var(--orb-mid) 45%,
    transparent 75%
  );
  mix-blend-mode: screen;
  filter: drop-shadow(0 0 6px var(--orb-glow)) blur(var(--orb-soft));
  transform: translate(-50%, 50%) scale(0);
  transition:
    opacity 160ms ease,
    transform 260ms cubic-bezier(0.34, 1.56, 0.64, 1); /* bounce-in */
}

.war-room__hotspot:hover .fx-orb,
.war-room__hotspot:focus-visible .fx-orb {
  opacity: 1;
  transform: translate(-50%, 50%) scale(1);
  animation: fx-orb-pulse 1.6s ease-in-out infinite;
}

@keyframes fx-orb-pulse {
  0%,
  100% {
    filter: drop-shadow(0 0 6px var(--orb-glow)) blur(var(--orb-soft));
  }
  50% {
    filter: drop-shadow(0 0 13px var(--orb-glow)) blur(var(--orb-soft));
  }
}

/*
 * Beam — rises straight up from the orb center to the label. Its bottom edge
 * sits on the anchor line; --beam-h is how far up it reaches (% of hotspot).
 */
.fx-beam {
  --beam-w: 16px;
  --beam-color: rgba(212, 168, 71, 0.85);
  --beam-color-mid: rgba(212, 168, 71, 0.35);
  left: var(--orb-x, 50%);
  bottom: calc(100% - var(--orb-y, 90%));
  width: var(--beam-w);
  height: var(--beam-h, 105%);
  background: linear-gradient(
    to top,
    var(--beam-color),
    var(--beam-color-mid) 50%,
    transparent 100%
  );
  /* 16px wide at bottom -> ~10px at top: inset (16-10)/2 / 16 = 18.75%. */
  clip-path: polygon(0% 100%, 100% 100%, 81.25% 0%, 18.75% 0%);
  filter: blur(3px);
  mix-blend-mode: screen;
  transform: translateX(-50%) scaleY(0);
  transform-origin: bottom center;
  /* 150ms delay so the orb appears first, then the beam grows up. */
  transition:
    opacity 200ms ease 150ms,
    transform 320ms cubic-bezier(0.22, 1, 0.36, 1) 150ms;
}

.war-room__hotspot:hover .fx-beam,
.war-room__hotspot:focus-visible .fx-beam {
  opacity: 1;
  transform: translateX(-50%) scaleY(1);
  animation: fx-beam-shimmer 1.8s ease-in-out infinite 0.4s;
}

@keyframes fx-beam-shimmer {
  0%,
  100% {
    filter: blur(3px) brightness(1);
  }
  50% {
    filter: blur(3px) brightness(1.3);
  }
}

/*
 * Selected (active tab) — keep the highlight lit and retint it blue so the
 * open panel's hotspot reads as active even without a hover.
 */
.war-room__hotspot--selected .fx-orb {
  --orb-core: rgba(214, 235, 255, 0.95);
  --orb-mid: rgba(106, 178, 255, 0.6);
  --orb-glow: rgba(106, 178, 255, 0.65);
  opacity: 1;
  transform: translate(-50%, 50%) scale(1);
}

.war-room__hotspot--selected .fx-beam {
  --beam-color: rgba(106, 178, 255, 0.85);
  --beam-color-mid: rgba(106, 178, 255, 0.35);
  opacity: 1;
  transform: translateX(-50%) scaleY(1);
}

/* Label */
.war-room__label {
  position: absolute;
  bottom: 100%;
  left: 50%;
  z-index: 1;
  transform: translateX(-50%);
  margin-bottom: 8px;
  font-family: var(--font-title);
  font-size: clamp(12px, 1.2vw, 20px);
  font-weight: 700;
  letter-spacing: 0.06em;
  white-space: nowrap;
  color: #f4d27a;
  text-shadow:
    0 0 6px rgba(0, 0, 0, 0.9),
    0 1px 2px rgba(0, 0, 0, 0.9),
    0 0 10px rgba(255, 200, 100, 0.25);
  pointer-events: none;
  transition:
    color 140ms ease,
    text-shadow 140ms ease,
    transform 140ms ease;
}

.war-room__hotspot:hover .war-room__label,
.war-room__hotspot:focus-visible .war-room__label {
  transform: translateX(-50%) translateY(-2px);
  color: #ffe9a8;
  text-shadow:
    0 0 6px rgba(0, 0, 0, 0.95),
    0 1px 2px rgba(0, 0, 0, 0.95),
    0 0 12px rgba(255, 220, 140, 0.95),
    0 0 24px rgba(255, 200, 100, 0.7);
}

.war-room__hotspot--selected .war-room__label {
  color: #b8dcff;
  text-shadow:
    0 0 6px rgba(0, 0, 0, 0.9),
    0 1px 2px rgba(0, 0, 0, 0.9),
    0 0 12px rgba(106, 178, 255, 0.7);
}

/* --- Dev-only positioning tool (Alt held) --- */
.war-room__hotspot--editing {
  outline: 2px dashed rgba(255, 220, 140, 0.85);
  outline-offset: -2px;
  background-color: rgba(255, 220, 140, 0.08);
}

/* Resize handles — small grabbable squares on each edge/corner of the box. */
.war-room__handle {
  position: absolute;
  width: 12px;
  height: 12px;
  background: #f4d27a;
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
  position: absolute;
  top: 100%;
  left: 50%;
  transform: translateX(-50%);
  margin-top: 14px;
  padding: 1px 5px;
  font-family: var(--font-mono, monospace);
  font-size: 11px;
  white-space: nowrap;
  color: #ffe9a8;
  background: rgba(0, 0, 0, 0.7);
  border-radius: 3px;
  pointer-events: none;
}

.war-room__edit-hint {
  position: absolute;
  top: 12px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 3;
  display: flex;
  gap: 12px;
  align-items: center;
  padding: 6px 12px;
  font-family: var(--font-mono, monospace);
  font-size: 12px;
  letter-spacing: 0.04em;
  color: #ffe9a8;
  background: rgba(0, 0, 0, 0.75);
  border: 1px solid rgba(255, 220, 140, 0.4);
  border-radius: 4px;
}

.war-room__edit-copy {
  padding: 3px 10px;
  font: inherit;
  color: #05080d;
  background: #f4d27a;
  border: 0;
  border-radius: 3px;
}
</style>
