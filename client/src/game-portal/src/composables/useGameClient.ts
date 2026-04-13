import { onBeforeUnmount, ref } from 'vue'
import { GameClient, type GameUiSnapshot } from '@/game/core/GameClient'

let client: GameClient | null = null

const emptyUiSnapshot: GameUiSnapshot = {
  player: {
    playerId: null,
    color: null,
    totalUnits: 0,
    selectedUnits: 0,
    totalHp: 0,
    resources: [],
  },
  selectedUnits: [],
  selection: {
    kind: 'none',
    title: 'No Selection',
    subtitle: 'Select a unit or building to inspect details and actions.',
    actions: [],
  },
}

export function useGameClient() {
  const isRunning = ref(false)
  const ui = ref<GameUiSnapshot>(emptyUiSnapshot)
  let rafId = 0

  function syncUi() {
    ui.value = client?.getUiSnapshot() ?? emptyUiSnapshot

    if (client) {
      rafId = requestAnimationFrame(syncUi)
    }
  }

  function stopUiSync() {
    if (rafId) {
      cancelAnimationFrame(rafId)
      rafId = 0
    }
  }

  async function init(
    canvas: HTMLCanvasElement,
    mapId = '',
    options: { resume?: boolean } = {},
  ) {
    client?.stop()
    stopUiSync()
    client = new GameClient(canvas, mapId)
    await client.start(options)
    syncUi()
    isRunning.value = true
  }

  async function leaveStoredMatch() {
    if (!client) {
      const tempCanvas = document.createElement('canvas')
      client = new GameClient(tempCanvas)
    }
    await client.leaveStoredMatch()
  }

  function destroy() {
    stopUiSync()
    client?.stop()
    client = null
    ui.value = emptyUiSnapshot
    isRunning.value = false
  }

  onBeforeUnmount(() => {
    destroy()
  })

  return {
    init,
    destroy,
    isRunning,
    leaveStoredMatch,
    ui,
  }
}
