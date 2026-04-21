// Makes an HTML panel freely draggable by applying a CSS translate offset
// on top of its existing anchor (top/left/right/bottom). The offset persists
// across reloads under localStorage key `panel-offset:<storageKey>`.
//
// Usage from a panel component:
//
//   const drag = useDraggablePanel('battle-tracker')
//   // on the root: :style="drag.style"
//   // on the grab handle: v-bind="drag.handleBindings"

import { computed, ref } from 'vue'

type Offset = { x: number; y: number }

const STORAGE_PREFIX = 'panel-offset:'

function loadOffset(storageKey: string): Offset {
  try {
    const raw = localStorage.getItem(STORAGE_PREFIX + storageKey)
    if (!raw) return { x: 0, y: 0 }
    const parsed = JSON.parse(raw) as Partial<Offset>
    if (typeof parsed.x === 'number' && typeof parsed.y === 'number') {
      return { x: parsed.x, y: parsed.y }
    }
  } catch {
    /* corrupt entry — fall through to default */
  }
  return { x: 0, y: 0 }
}

function saveOffset(storageKey: string, offset: Offset) {
  try {
    localStorage.setItem(STORAGE_PREFIX + storageKey, JSON.stringify(offset))
  } catch {
    /* quota / privacy mode — position won't persist, but dragging still works this session */
  }
}

export function useDraggablePanel(storageKey: string) {
  const offset = ref<Offset>(loadOffset(storageKey))
  const dragging = ref(false)

  let startX = 0
  let startY = 0
  let originX = 0
  let originY = 0

  function onPointerDown(e: PointerEvent) {
    // Only primary button; ignore if the pointer started on an interactive
    // child (buttons / selects / inputs) so those still behave normally.
    if (e.button !== 0) return
    const target = e.target as HTMLElement | null
    if (target?.closest('button, input, select, textarea, a')) return

    dragging.value = true
    startX = e.clientX
    startY = e.clientY
    originX = offset.value.x
    originY = offset.value.y
    ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
    e.preventDefault()
  }

  function onPointerMove(e: PointerEvent) {
    if (!dragging.value) return
    offset.value = {
      x: originX + (e.clientX - startX),
      y: originY + (e.clientY - startY),
    }
  }

  function onPointerUp(e: PointerEvent) {
    if (!dragging.value) return
    dragging.value = false
    saveOffset(storageKey, offset.value)
    try {
      ;(e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId)
    } catch {
      /* pointer already released by the browser — nothing to clean up */
    }
  }

  const style = computed(() => ({
    transform: `translate(${offset.value.x}px, ${offset.value.y}px)`,
  }))

  const handleBindings = {
    onPointerdown: onPointerDown,
    onPointermove: onPointerMove,
    onPointerup: onPointerUp,
    onPointercancel: onPointerUp,
  }

  return { style, dragging, handleBindings }
}
