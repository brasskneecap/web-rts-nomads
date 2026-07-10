<template>
  <div class="game-scroll" ref="root" :style="scrollbarStyle">
    <div class="game-scroll__viewport" ref="viewport" @scroll="recalc">
      <slot />
    </div>
    <div
      class="game-scroll__track"
      :class="{ 'game-scroll__track--scrollable': thumbVisible }"
      ref="track"
      @mousedown.self="onTrackPress"
    >
      <div
        v-show="thumbVisible"
        class="game-scroll__thumb"
        :class="{ 'game-scroll__thumb--dragging': dragging }"
        :style="{ height: thumbHeight + 'px', transform: `translateY(${thumbTop}px)` }"
        @mousedown="onThumbPress"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import scrollbarUrl from '@/assets/ui/themes/updated/scrollbar.png'

// The ornate rail art (20x550) exposed to scoped CSS as a custom property.
const scrollbarStyle = { '--scrollbar-img': `url(${scrollbarUrl})` }

const root = ref<HTMLDivElement | null>(null)
const viewport = ref<HTMLDivElement | null>(null)
const track = ref<HTMLDivElement | null>(null)

const thumbVisible = ref(false)
const thumbHeight = ref(0)
const thumbTop = ref(0)
const dragging = ref(false)

let trackHeight = 0
let dragStartClientY = 0
let dragStartScrollTop = 0
let resizeObserver: ResizeObserver | null = null
let mutationObserver: MutationObserver | null = null

const MIN_THUMB = 24

function recalc() {
  const vp = viewport.value
  const tr = track.value
  if (!vp || !tr) return
  trackHeight = tr.clientHeight
  const scrollHeight = vp.scrollHeight
  const clientHeight = vp.clientHeight
  if (scrollHeight <= clientHeight + 1 || trackHeight <= 0) {
    thumbVisible.value = false
    return
  }
  thumbVisible.value = true
  const ratio = clientHeight / scrollHeight
  const h = Math.max(MIN_THUMB, Math.round(trackHeight * ratio))
  thumbHeight.value = h
  const maxThumbTop = Math.max(0, trackHeight - h)
  const maxScrollTop = scrollHeight - clientHeight
  thumbTop.value = maxScrollTop > 0 ? (vp.scrollTop / maxScrollTop) * maxThumbTop : 0
}

function onThumbPress(e: MouseEvent) {
  e.preventDefault()
  const vp = viewport.value
  if (!vp) return
  dragging.value = true
  dragStartClientY = e.clientY
  dragStartScrollTop = vp.scrollTop
  window.addEventListener('mousemove', onDragMove)
  window.addEventListener('mouseup', onDragEnd)
}

function onDragMove(e: MouseEvent) {
  const vp = viewport.value
  if (!vp || !dragging.value) return
  const maxThumbTop = Math.max(0, trackHeight - thumbHeight.value)
  if (maxThumbTop <= 0) return
  const maxScrollTop = vp.scrollHeight - vp.clientHeight
  const deltaPx = e.clientY - dragStartClientY
  const scrollDelta = (deltaPx / maxThumbTop) * maxScrollTop
  vp.scrollTop = dragStartScrollTop + scrollDelta
}

function onDragEnd() {
  dragging.value = false
  window.removeEventListener('mousemove', onDragMove)
  window.removeEventListener('mouseup', onDragEnd)
}

function onTrackPress(e: MouseEvent) {
  const vp = viewport.value
  const tr = track.value
  if (!vp || !tr) return
  const rect = tr.getBoundingClientRect()
  // Subtract the top border (the cap inset) so localY is measured from the
  // rail region the thumb actually travels in.
  const localY = e.clientY - rect.top - tr.clientTop
  const targetThumbTop = localY - thumbHeight.value / 2
  const maxThumbTop = Math.max(0, trackHeight - thumbHeight.value)
  const clamped = Math.max(0, Math.min(maxThumbTop, targetThumbTop))
  const maxScrollTop = vp.scrollHeight - vp.clientHeight
  vp.scrollTop = maxThumbTop > 0 ? (clamped / maxThumbTop) * maxScrollTop : 0
}

onMounted(() => {
  const vp = viewport.value
  const r = root.value
  if (!vp || !r) return
  recalc()
  resizeObserver = new ResizeObserver(recalc)
  resizeObserver.observe(vp)
  resizeObserver.observe(r)
  mutationObserver = new MutationObserver(recalc)
  mutationObserver.observe(vp, { childList: true, subtree: true, characterData: true })
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  mutationObserver?.disconnect()
  window.removeEventListener('mousemove', onDragMove)
  window.removeEventListener('mouseup', onDragEnd)
})
</script>

<style scoped>
.game-scroll {
  position: relative;
  width: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.game-scroll__viewport {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
  scrollbar-width: none;
  padding-right: 20px;
}

.game-scroll__viewport::-webkit-scrollbar {
  display: none;
}

.game-scroll__track {
  /* Transparent top/bottom border = the cap inset. It shrinks the track's
     clientHeight (which the thumb math reads), so the thumb is confined to the
     straight rail between the caps and never rides up over them. */
  --cap: 26px;
  /* Bottom inset is 4px larger than the top so the thumb stops a bit higher
     from the bottom cap (the rail art itself is unaffected). */
  --cap-bottom: 30px;
  position: absolute;
  top: 2px;
  bottom: 2px;
  right: 0;
  width: 16px;
  border: 0 solid transparent;
  border-width: var(--cap) 0 var(--cap-bottom);
  background: transparent;
}

/* The rail art (scrollbar.png) as a background layer, so the thumb math (which
   reads the track's clientHeight) stays untouched. It extends past the track's
   padding box (top/bottom: -var(--cap)) to cover the full height, so the brass
   caps render at the true top/bottom while the middle rail fills the region the
   thumb slides in. pointer-events:none so track clicks still reach the track.
   Only shown when the content actually overflows. */
.game-scroll__track::before {
  content: '';
  position: absolute;
  top: calc(-1 * var(--cap));
  bottom: calc(-1 * var(--cap-bottom));
  left: 0;
  right: 0;
  display: none;
  border-style: solid;
  border-width: var(--cap) 0;
  border-image-source: var(--scrollbar-img);
  border-image-slice: 40 0 fill;
  border-image-width: var(--cap) 0;
  border-image-repeat: stretch;
  image-rendering: pixelated;
  pointer-events: none;
}

.game-scroll__track--scrollable::before {
  display: block;
}

.game-scroll__thumb {
  position: absolute;
  top: 0;
  left: 3px;
  right: 3px;
  /* Brass gradient anchored on the caps' measured mid-tone (#876020). Kept
     muted at the top so the highlight doesn't read as distracting. */
  background: linear-gradient(180deg, #a97e37 0%, #8f6829 50%, #75531d 100%);
  border: 1px solid rgba(58, 40, 14, 0.9);
  border-radius: 5px;
  box-shadow:
    inset 0 1px 0 rgba(214, 178, 112, 0.35),
    inset 0 -1px 2px rgba(0, 0, 0, 0.45);
  transition: filter 120ms ease;
}

.game-scroll__thumb:hover,
.game-scroll__thumb--dragging {
  filter: brightness(1.15);
}
</style>
