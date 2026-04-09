import { ref } from 'vue'
import { GameClient } from '@/game/core/GameClient'
import type { MapSize } from '@/game/network/protocol'

let client: GameClient | null = null

export function useGameClient() {
  const isRunning = ref(false)

  async function init(
    canvas: HTMLCanvasElement,
    mapSize: MapSize = 'large',
    options: { resume?: boolean } = {},
  ) {
    client?.stop()
    client = new GameClient(canvas, mapSize)
    await client.start(options)
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
    client?.stop()
    client = null
    isRunning.value = false
  }

  return {
    init,
    destroy,
    isRunning,
    leaveStoredMatch,
  }
}