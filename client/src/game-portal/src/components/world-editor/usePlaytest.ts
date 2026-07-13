import { computed, ref } from 'vue'
import type { MapConfig, MapCatalogFile } from '@/game/network/protocol'
import { saveMapCatalogFile } from '@/game/maps/catalog'
import { useGameClient } from '@/composables/useGameClient'

export const scratchMapId = '__world_editor_scratch__'

// resolvePlaytestMapId picks the id to run: the working map's real id, or the
// reserved scratch id for a never-saved draft.
export function resolvePlaytestMapId(map: Pick<MapConfig, 'id'>): string {
  if (!map.id || map.id === 'editor-draft') return scratchMapId
  return map.id
}

// usePlaytest owns the single useGameClient() instance the world-editor
// playtest uses. Running the match through the shared composable (rather than a
// private GameClient) is what lets the in-game HUD — which reads the composable
// snapshot — render the live playtest. The returned `gameClient` is handed to
// the shared InGameHud so it can read `ui` and forward commands.
export function usePlaytest(getPlayCanvas: () => HTMLCanvasElement | null) {
  const gameClient = useGameClient()
  const playing = ref(false)
  // Authoritative pause state comes from the server snapshot.
  const paused = computed(() => gameClient.ui.value.paused)
  // Synchronous in-flight marker (playing flips true only after the awaits).
  let starting = false

  async function start(file: MapCatalogFile) {
    if (playing.value || starting) return
    const canvas = getPlayCanvas()
    if (!canvas) return
    starting = true
    try {
      const mapId = resolvePlaytestMapId(file)
      await saveMapCatalogFile({ ...file, id: mapId })
      await gameClient.init(canvas, mapId, { ephemeral: true })
      playing.value = true
    } catch (err) {
      gameClient.destroy()
      playing.value = false
      throw err
    } finally {
      starting = false
    }
  }

  // togglePause freezes/continues the running match via the server set_pause
  // command; the button label reads the authoritative `paused` computed.
  function togglePause() {
    if (!playing.value) return
    gameClient.sendSetPause(!gameClient.ui.value.paused)
  }

  // stop tears the match down. The editor's MapConfig is untouched, so
  // re-showing the editor canvas restores the placements.
  function stop() {
    gameClient.destroy()
    playing.value = false
  }

  return { playing, paused, start, stop, togglePause, gameClient }
}
