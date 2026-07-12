import { ref } from 'vue'
import type { MapConfig, MapCatalogFile } from '@/game/network/protocol'
import { saveMapCatalogFile } from '@/game/maps/catalog'
import { GameClient } from '@/game/core/GameClient'

export const scratchMapId = '__world_editor_scratch__'

// resolvePlaytestMapId picks the id to run: the working map's real id, or the
// reserved scratch id for a never-saved draft.
export function resolvePlaytestMapId(map: Pick<MapConfig, 'id'>): string {
  if (!map.id || map.id === 'editor-draft') return scratchMapId
  return map.id
}

export function usePlaytest(getPlayCanvas: () => HTMLCanvasElement | null) {
  const playing = ref(false)
  let client: GameClient | null = null

  // start: persist the current editor map (so the server can match it,
  // including unsaved placements), then run an ephemeral match on the play
  // canvas via a real GameClient.
  async function start(file: MapCatalogFile) {
    // Reentrancy guard: a second click while already playing (or while a
    // prior start() is still in flight) must not orphan the existing
    // client's rAF render loop and websocket.
    if (playing.value) return
    const canvas = getPlayCanvas()
    if (!canvas) return
    try {
      const mapId = resolvePlaytestMapId(file)
      // Persist under the resolved id (scratch for drafts) so join_match can find it.
      await saveMapCatalogFile({ ...file, id: mapId })
      client = new GameClient(canvas, mapId)
      await client.start({ ephemeral: true })
      playing.value = true
    } catch (err) {
      // Tear down any partially-constructed client and surface the failure
      // to the caller instead of leaving a silent, half-started playtest.
      client?.stop()
      client = null
      playing.value = false
      throw err
    }
  }

  // stop: tear down the match. The editor's own MapConfig is untouched, so the
  // caller simply re-shows the editor canvas — placements "snap back" for free.
  function stop() {
    if (client) {
      client.stop()
      client = null
    }
    playing.value = false
  }

  return { playing, start, stop }
}
