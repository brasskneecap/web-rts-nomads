// Shared coordination for every InfoTip.vue instance on the page: a unique
// id per instance (for aria-describedby) and a single module-level "which one
// is click-pinned open" ref, so pinning a new tip open silently un-pins
// whichever was open before rather than letting several stack up at once.
//
// Module-level (not per-component) state is the point here — every InfoTip
// that calls this composable shares the SAME `pinnedId` ref, because ES
// modules are singletons and this file's top-level state runs once.
import { ref } from 'vue'

let nextId = 0
const pinnedId = ref<string | null>(null)

export function useInfoTipId(): string {
  return `info-tip-${++nextId}`
}

export function useInfoTipPinning(id: string) {
  return {
    isPinned: () => pinnedId.value === id,
    toggle: () => {
      pinnedId.value = pinnedId.value === id ? null : id
    },
    unpin: () => {
      if (pinnedId.value === id) pinnedId.value = null
    },
  }
}
