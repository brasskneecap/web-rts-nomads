import { ref, watch } from 'vue'
import type { MapId } from '@/game/network/protocol'

const MAP_ID_STORAGE_KEY = 'webrts.mapId'

const selectedMapId = ref<MapId>(localStorage.getItem(MAP_ID_STORAGE_KEY) ?? '')
const selectedMapName = ref<string>('')

watch(selectedMapId, (val) => {
  if (val) {
    localStorage.setItem(MAP_ID_STORAGE_KEY, val)
  }
})

export function useMapSelection() {
  function setSelectedMapId(id: MapId, name = '') {
    selectedMapId.value = id
    selectedMapName.value = name
  }

  return { selectedMapId, selectedMapName, setSelectedMapId }
}
