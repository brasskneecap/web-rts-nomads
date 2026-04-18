import { onBeforeUnmount, ref } from 'vue'
import { GameClient, type GameUiSnapshot } from '@/game/core/GameClient'
import type { ConnectionState } from '@/game/network/protocol'

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
    details: [],
    actions: [],
  },
  notifications: [],
  wave: {
    enabled: false,
    currentWave: 0,
    totalWaves: 0,
    state: '',
    timer: 0,
    waveDuration: 0,
  },
}

export function useGameClient() {
  const isRunning = ref(false)
  const ui = ref<GameUiSnapshot>(emptyUiSnapshot)
  const connectionState = ref<ConnectionState>('idle')
  // Attempt counters are polled from the client each time the state changes so
  // the overlay can display "attempt N of M" without needing a separate channel.
  const reconnectAttempt = ref(0)
  const maxReconnectAttempts = ref(0)

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

    // Wire the connection state callback. This runs outside the RAF loop so
    // connection state changes are never masked by the snapshot polling rhythm.
    client.onConnectionStateChange = (state) => {
      connectionState.value = state
      reconnectAttempt.value = client?.reconnectAttempt ?? 0
      maxReconnectAttempts.value = client?.maxReconnectAttempts ?? 0
    }

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
    connectionState.value = 'idle'
  }

  function performSelectionAction(actionId: string) {
    client?.performSelectionAction(actionId)
  }

  function retryReconnect() {
    client?.retryReconnect()
  }

  onBeforeUnmount(() => {
    destroy()
  })

  return {
    init,
    destroy,
    isRunning,
    leaveStoredMatch,
    performSelectionAction,
    retryReconnect,
    ui,
    connectionState,
    reconnectAttempt,
    maxReconnectAttempts,
  }
}
