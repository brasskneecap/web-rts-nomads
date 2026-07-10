<template>
  <div class="main-menu">
    <ResumeSessionCard
      v-if="hasResumableSession"
      :map-name="resumeMapName"
      @resume="onResume"
      @dismiss="onDismiss"
    />

    <!--
      The menu entries are inscribed directly onto the wooden planks of the sign
      in main-menu.png. The background is painted by MenuChrome with
      `background-size: cover`, so `.menu-sign__frame` reproduces the exact cover
      geometry (same aspect ratio, same centre-crop) and every entry is placed by
      percentage of that frame — this keeps each label glued to its plank at any
      viewport size. Plank centres were measured from the source art and can be
      re-tuned live with the dev overlay (press ` in a dev build).
    -->
    <div class="menu-sign" aria-hidden="false">
      <nav ref="frameEl" class="menu-sign__frame" aria-label="Main menu">
        <UiButton
          v-for="(entry, i) in tune.entries"
          :key="entry.to"
          class="menu-sign__entry"
          :class="{ 'menu-sign__entry--tuning': tuning, 'menu-sign__entry--selected': tuning && selected === i }"
          :style="{
            top: entry.top + '%',
            left: tune.left + '%',
            width: tune.width + '%',
            fontSize: `min(${tune.fontCqh}cqh, ${tune.fontCqw}cqw)`,
          }"
          @pointerdown="onEntryPointerDown($event, i)"
          @click="onEntryClick(entry.to)"
        >
          {{ entry.label }}
        </UiButton>
      </nav>
    </div>

    <!-- Dev-only layout tuner. Stripped from production builds (import.meta.env.DEV). -->
    <div v-if="tunerEnabled && tuning" class="menu-tuner" @pointerdown.stop>
      <div class="menu-tuner__title">Sign tuner <span>· ` to close</span></div>
      <p class="menu-tuner__hint">Drag a label vertically to place it on its plank, or edit values below.</p>

      <div class="menu-tuner__grid">
        <label v-for="(entry, i) in tune.entries" :key="entry.to" class="menu-tuner__row">
          <span class="menu-tuner__name">{{ entry.label }}</span>
          <span class="menu-tuner__field">top
            <input type="number" step="0.1" v-model.number="entry.top"
              @focus="selected = i" />%
          </span>
        </label>
      </div>

      <div class="menu-tuner__grid menu-tuner__grid--shared">
        <label class="menu-tuner__field">left
          <input type="number" step="0.1" v-model.number="tune.left" />%
        </label>
        <label class="menu-tuner__field">width
          <input type="number" step="0.1" v-model.number="tune.width" />%
        </label>
        <label class="menu-tuner__field">font cqh
          <input type="number" step="0.1" v-model.number="tune.fontCqh" />
        </label>
        <label class="menu-tuner__field">font cqw
          <input type="number" step="0.1" v-model.number="tune.fontCqw" />
        </label>
      </div>

      <pre class="menu-tuner__readout">{{ readout }}</pre>
      <div class="menu-tuner__actions">
        <button type="button" @click="copyReadout">{{ copied ? 'Copied!' : 'Copy values' }}</button>
        <button type="button" @click="resetTune">Reset</button>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
// Layout of the menu entries on the sign. `top` is the vertical centre of each
// plank as a percentage of the sign image height; `left`/`width` and the font
// caps are shared. These are the shipped defaults — the dev tuner mutates them
// live so new values can be dialled in, then pasted back here.
//
// Declared in a plain (non-setup) script block so DEFAULTS/MENU_ENTRIES can be
// exported as named exports for testing — `<script setup>` only exposes a
// component's default export, not named bindings. Consts declared here are
// still visible inside `<script setup>` below (standard SFC two-block pattern).
export type Entry = { label: string; to: string; top: number }
export const DEFAULTS = {
  left: 52.6,
  width: 21,
  fontCqh: 3.2,
  fontCqw: 2.9,
  entries: [
    // The sign art (main-menu.png) has FIVE plank rows at ~6.42% intervals;
    // the original four tops sat on planks 1-4, so Item Editor takes plank 4
    // and Settings moves down to plank 5 (75.25 + 6.43). Fine-tune with the
    // sign tuner (localStorage 'webrts.signTuner'='1' + backtick).
    { label: 'Start Game', to: '/war-room', top: 55.97 },
    { label: 'Profile', to: '/profile', top: 62.34 },
    { label: 'Map Editor', to: '/editor', top: 68.79 },
    { label: 'Item Editor', to: '/item-editor', top: 75.25 },
    { label: 'Settings', to: '/options', top: 81.68 },
  ] as Entry[],
}
export const MENU_ENTRIES = DEFAULTS.entries
</script>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import { useMapSelection } from '@/composables/useMapSelection'
import ResumeSessionCard from '@/components/menu/ResumeSessionCard.vue'
import UiButton from '@/components/ui/UiButton.vue'

const HAS_ACTIVE_SESSION_KEY = 'webrts.hasActiveSession'
const MATCH_ID_STORAGE_KEY = 'webrts.matchId'
const MAP_ID_STORAGE_KEY = 'webrts.mapId'

const router = useRouter()
const { selectedMapName, selectedMapId } = useMapSelection()

const tune = reactive(structuredClone(DEFAULTS))

// ---- Dev-only layout tuner --------------------------------------------------
// Off by default. It is compiled out of production builds (import.meta.env.DEV)
// and, even in a dev build, stays dormant until explicitly enabled — so once the
// sign is dialled in the tuner never gets in the way. To bring it back, run in
// the browser console then reload (` toggles the panel):
//   localStorage.setItem('webrts.signTuner', '1')
// and to switch it off again:
//   localStorage.removeItem('webrts.signTuner')
const tunerEnabled =
  import.meta.env.DEV && localStorage.getItem('webrts.signTuner') === '1'
const tuning = ref(false)
const selected = ref(-1)
const copied = ref(false)
const frameEl = ref<HTMLElement | null>(null)

const round = (n: number) => Math.round(n * 100) / 100

const readout = computed(() => {
  const tops = tune.entries.map((e) => `  { label: '${e.label}', to: '${e.to}', top: ${round(e.top)} },`).join('\n')
  return `left: ${round(tune.left)}, width: ${round(tune.width)}, fontCqh: ${round(tune.fontCqh)}, fontCqw: ${round(tune.fontCqw)}\nentries:\n${tops}`
})

function onKeydown(e: KeyboardEvent) {
  if (!tunerEnabled) return
  const target = e.target as HTMLElement | null
  if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA')) return
  if (e.key === '`') {
    e.preventDefault()
    tuning.value = !tuning.value
  }
}

// Vertical drag: while tuning, dragging a label moves its plank position. The
// delta is expressed as a percentage of the frame (= sign image) height so it
// matches the units stored in `top`.
let dragIndex = -1
let dragStartY = 0
let dragStartTop = 0

function onEntryPointerDown(e: PointerEvent, i: number) {
  if (!tuning.value) return
  e.preventDefault()
  selected.value = i
  dragIndex = i
  dragStartY = e.clientY
  dragStartTop = tune.entries[i].top
  window.addEventListener('pointermove', onDragMove)
  window.addEventListener('pointerup', onDragEnd)
}

function onDragMove(e: PointerEvent) {
  if (dragIndex < 0) return
  const h = frameEl.value?.clientHeight || window.innerHeight
  const dy = e.clientY - dragStartY
  tune.entries[dragIndex].top = round(dragStartTop + (dy / h) * 100)
}

function onDragEnd() {
  dragIndex = -1
  window.removeEventListener('pointermove', onDragMove)
  window.removeEventListener('pointerup', onDragEnd)
}

function onEntryClick(to: string) {
  // In tuning mode, clicks select/drag instead of navigating.
  if (tuning.value) return
  router.push(to)
}

async function copyReadout() {
  try {
    await navigator.clipboard.writeText(readout.value)
    copied.value = true
    setTimeout(() => (copied.value = false), 1200)
  } catch {
    copied.value = false
  }
}

function resetTune() {
  const d = structuredClone(DEFAULTS)
  tune.left = d.left
  tune.width = d.width
  tune.fontCqh = d.fontCqh
  tune.fontCqw = d.fontCqw
  tune.entries.splice(0, tune.entries.length, ...d.entries)
}

// ---- Resume-session card ----------------------------------------------------
const hasResumableSession = ref(false)

onMounted(() => {
  hasResumableSession.value =
    localStorage.getItem(HAS_ACTIVE_SESSION_KEY) === 'true' &&
    !!localStorage.getItem(MATCH_ID_STORAGE_KEY)
  if (tunerEnabled) window.addEventListener('keydown', onKeydown)
})

onBeforeUnmount(() => {
  if (tunerEnabled) window.removeEventListener('keydown', onKeydown)
  onDragEnd()
})

const resumeMapName = computed(() => {
  if (selectedMapName.value) return selectedMapName.value
  if (selectedMapId.value) return selectedMapId.value
  const rawMapId = localStorage.getItem(MAP_ID_STORAGE_KEY)
  if (rawMapId) return rawMapId
  return 'Unknown Map'
})

function onResume() {
  const matchId = localStorage.getItem(MATCH_ID_STORAGE_KEY)
  if (matchId) {
    void router.push(`/match/${matchId}`)
    return
  }
  onDismiss()
}

function onDismiss() {
  localStorage.removeItem(HAS_ACTIVE_SESSION_KEY)
  localStorage.removeItem(MATCH_ID_STORAGE_KEY)
  localStorage.removeItem(MAP_ID_STORAGE_KEY)
  hasResumableSession.value = false
}
</script>

<style scoped>
.main-menu {
  position: relative;
  z-index: 1;
  width: 100%;
  height: 100%;
  overflow: hidden;
}

/*
 * `.menu-sign__frame` mirrors MenuChrome's `background-size: cover` geometry:
 * the sign image is 1502x1047 (aspect 1.43457). Cover renders it at
 * width  = max(100vw, 100vh * 1.43457)
 * height = max(100vh, 100vw * 0.69707)
 * centred in the viewport. Reproducing that here means percentage-positioned
 * children land on the same pixels as the painted background at every size.
 */
.menu-sign {
  position: fixed;
  inset: 0;
  z-index: 1;
  pointer-events: none;
}

.menu-sign__frame {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  width: max(100vw, 100vh * 1.43457);
  height: max(100vh, 100vw * 0.69707);
  /* Establish a size container so entries can be sized in cqh/cqw units that
     track the sign's on-screen scale (a plank is ~6.3% of the frame height). */
  container-type: size;
}

/*
 * Each entry is centred horizontally on the sign and pinned to its plank. The
 * `left`, `width`, `top` and font-size are supplied inline from the tuning
 * config so the dev overlay can adjust them live.
 */
.menu-sign__frame :deep(.menu-sign__entry) {
  position: absolute;
  transform: translate(-50%, -50%);
  min-width: 0;
  min-height: 0;
  padding: 0;
  border: 0;
  border-image: none;
  background: none;
  pointer-events: auto;
  font-family: var(--font-title);
  font-weight: 600; /* Cinzel SemiBold */
  line-height: 1;
  letter-spacing: 4px;
  text-transform: uppercase;
  white-space: nowrap;
  color: #c8a85a; /* aged gold — sits on the wood rather than glowing */
  text-decoration: none;
  -webkit-text-stroke: 1px #2a1a10; /* 1px dark outline, painted behind the fill */
  paint-order: stroke fill;
  text-shadow: 0 2px 2px rgba(0, 0, 0, 0.42); /* subtle 2px downward shadow */
  transition: color 120ms ease;
}

/*
 * Soft pool of light on the plank behind the text. It can't move the plank
 * (that's baked into the background art), so a warm, soft-edged glow that fades
 * in on hover reads as light catching the wood; on press it flips to a subtle
 * darkening. Sits behind the text via the entry's transform stacking context.
 */
.menu-sign__frame :deep(.menu-sign__entry)::before {
  content: '';
  position: absolute;
  inset: -35% -6%;
  z-index: -1;
  border-radius: 45% / 60%;
  background: radial-gradient(
    ellipse at center,
    rgba(255, 231, 176, 0.16),
    rgba(255, 231, 176, 0) 70%
  );
  opacity: 0;
  transition: opacity 120ms ease, background 120ms ease;
  pointer-events: none;
}

.menu-sign__frame :deep(.menu-sign__entry:hover:not(:disabled)) {
  filter: none;
  color: #f2d88a; /* bright gold — clear, non-neon hover jump */
}

.menu-sign__frame :deep(.menu-sign__entry:hover:not(:disabled))::before {
  opacity: 1;
}

.menu-sign__frame :deep(.menu-sign__entry:active:not(:disabled)) {
  filter: none;
  color: #b08f48; /* slightly darker gold on press */
}

.menu-sign__frame :deep(.menu-sign__entry:active:not(:disabled))::before {
  opacity: 1;
  background: radial-gradient(
    ellipse at center,
    rgba(0, 0, 0, 0.22),
    rgba(0, 0, 0, 0) 72%
  );
}

/* Tuning affordances (dev only) */
.menu-sign__frame :deep(.menu-sign__entry--tuning) {
  outline: 1px dashed rgba(255, 220, 140, 0.5);
  outline-offset: 4px;
}

.menu-sign__frame :deep(.menu-sign__entry--selected) {
  outline: 1px solid rgba(120, 220, 255, 0.9);
  background: rgba(60, 130, 200, 0.18);
}

/* ---- Dev tuner panel ---- */
.menu-tuner {
  position: fixed;
  top: 16px;
  left: 16px;
  z-index: 50;
  width: 300px;
  padding: 12px 14px;
  border-radius: 8px;
  background: rgba(12, 16, 24, 0.92);
  border: 1px solid rgba(245, 234, 210, 0.25);
  color: #f5ead2;
  font-family: var(--font-ui, system-ui, sans-serif);
  font-size: 12px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
}

.menu-tuner__title {
  font-weight: 700;
  font-size: 13px;
  letter-spacing: 0.04em;
  margin-bottom: 4px;
}

.menu-tuner__title span {
  opacity: 0.6;
  font-weight: 400;
}

.menu-tuner__hint {
  margin: 0 0 10px;
  opacity: 0.7;
  line-height: 1.35;
}

.menu-tuner__grid {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.menu-tuner__grid--shared {
  flex-direction: row;
  flex-wrap: wrap;
  gap: 8px 12px;
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px solid rgba(245, 234, 210, 0.15);
}

.menu-tuner__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.menu-tuner__name {
  opacity: 0.85;
}

.menu-tuner__field {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  opacity: 0.85;
}

.menu-tuner input {
  width: 58px;
  padding: 2px 4px;
  border-radius: 4px;
  border: 1px solid rgba(245, 234, 210, 0.3);
  background: rgba(0, 0, 0, 0.35);
  color: #f5ead2;
  font: inherit;
  text-align: right;
}

.menu-tuner__readout {
  margin: 10px 0 8px;
  padding: 8px;
  max-height: 140px;
  overflow: auto;
  border-radius: 4px;
  background: rgba(0, 0, 0, 0.4);
  font-family: ui-monospace, monospace;
  font-size: 11px;
  line-height: 1.4;
  white-space: pre-wrap;
  word-break: break-word;
}

.menu-tuner__actions {
  display: flex;
  gap: 8px;
}

.menu-tuner__actions button {
  flex: 1;
  padding: 6px 8px;
  border-radius: 4px;
  border: 1px solid rgba(245, 234, 210, 0.3);
  background: rgba(245, 234, 210, 0.1);
  color: #f5ead2;
  font: inherit;
  font-weight: 600;
}

.menu-tuner__actions button:hover {
  background: rgba(245, 234, 210, 0.2);
}
</style>
