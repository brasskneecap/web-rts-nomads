<template>
  <div class="create-game">
    <div class="create-game__layout">
      <header class="create-game__header">
        <UiButton size="sm" @click="router.push('/custom')">Back</UiButton>
        <h1 class="create-game__title">Create Lobby</h1>
      </header>

      <div class="create-game__body">
        <UiPanel class="create-game__left" :padding="16">
          <div class="create-game__section-label">Select Map</div>
          <MapList
            :maps="mapCatalog"
            :selected-map-id="selectedMapId"
            :loading="isLoadingMaps"
            @update:selected-map-id="onMapSelected"
          />
          <div v-if="mapsLoadError" class="create-game__error">{{ mapsLoadError }}</div>
        </UiPanel>

        <div class="create-game__right">
          <MinimapPreview :map="selectedMap" />
        </div>
      </div>

      <footer class="create-game__footer">
        <UiButton
          size="lg"
          :disabled="!selectedMapId || isLoadingMaps"
          @click="createLobbyAndNavigate"
        >
          Create Lobby
        </UiButton>
      </footer>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { fetchMapCatalog } from '@/game/maps/catalog'
import type { MapCatalogEntry } from '@/game/network/protocol'
import { useLobbies } from '@/composables/useLobbies'
import { usePlayer } from '@/composables/usePlayer'
import UiButton from '@/components/ui/UiButton.vue'
import UiPanel from '@/components/ui/UiPanel.vue'
import MapList from '@/components/menu/MapList.vue'
import MinimapPreview from '@/components/menu/MinimapPreview.vue'

const router = useRouter()
const { createLobby } = useLobbies()
const { playerId } = usePlayer()

const mapCatalog = ref<MapCatalogEntry[]>([])
const isLoadingMaps = ref(true)
const mapsLoadError = ref('')
const selectedMapId = ref('')

const selectedMap = computed(
  () => mapCatalog.value.find((m) => m.id === selectedMapId.value) ?? null,
)

function onMapSelected(id: string) {
  selectedMapId.value = id
}

async function loadMapCatalog() {
  isLoadingMaps.value = true
  mapsLoadError.value = ''
  try {
    const maps = await fetchMapCatalog()
    mapCatalog.value = maps
    if (maps.length > 0 && !selectedMapId.value) {
      selectedMapId.value = maps[0].id
    }
  } catch (err) {
    mapsLoadError.value = err instanceof Error ? err.message : 'Failed to load maps.'
  } finally {
    isLoadingMaps.value = false
  }
}

async function createLobbyAndNavigate() {
  if (!selectedMapId.value) return
  try {
    const created = await createLobby({ mapId: selectedMapId.value, hostPlayerId: playerId.value })
    void router.push(`/lobby/${created.id}`)
  } catch (err) {
    mapsLoadError.value = err instanceof Error ? err.message : 'Failed to create lobby.'
  }
}

onMounted(() => {
  void loadMapCatalog()
})
</script>

<style scoped>
.create-game {
  position: relative;
  z-index: 1;
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  background: radial-gradient(circle at top, rgba(36, 55, 87, 0.35), transparent 48%);
  padding: 32px;
  box-sizing: border-box;
}

.create-game__layout {
  display: flex;
  flex-direction: column;
  gap: 24px;
  width: 100%;
  max-width: 900px;
  height: 100%;
  max-height: 700px;
}

.create-game__header {
  display: flex;
  align-items: center;
  gap: 20px;
}

.create-game__title {
  font-size: 24px;
  font-weight: 700;
  color: #f5ead2;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  margin: 0;
}

.create-game__body {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 20px;
  flex: 1 1 auto;
  min-height: 0;
}

.create-game__left {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-height: 0;
}

.create-game__right {
  min-height: 0;
}

.create-game__section-label {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: #d7bb84;
}

.create-game__footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}

.create-game__error {
  font-size: 13px;
  color: #f07070;
}
</style>
