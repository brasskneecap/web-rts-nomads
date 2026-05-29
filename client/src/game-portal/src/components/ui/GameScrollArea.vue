<template>
  <div class="game-scroll" ref="root">
    <div class="game-scroll__viewport" ref="viewport" @scroll="recalc">
      <slot />
    </div>
    <div
      class="game-scroll__track"
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
  const localY = e.clientY - rect.top
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
  padding-right: 12px;
}

.game-scroll__viewport::-webkit-scrollbar {
  display: none;
}

.game-scroll__track {
  position: absolute;
  top: 2px;
  bottom: 2px;
  right: 2px;
  width: 8px;
  border-radius: 4px;
  background: transparent;
}

.game-scroll__thumb {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  background: rgba(210, 176, 113, 0.4);
  border-radius: 4px;
  transition: background 120ms ease;
}

.game-scroll__thumb:hover,
.game-scroll__thumb--dragging {
  background: rgba(210, 176, 113, 0.65);
}
</style>
