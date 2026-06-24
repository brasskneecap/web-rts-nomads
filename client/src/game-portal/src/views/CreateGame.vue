<template>
  <div class="create-game">
    <div class="create-game__layout">
      <header class="create-game__header">
        <ExitButton @click="router.push('/custom')" />
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
          :disabled="!selectedMapId || isLoadingMaps || isCreating"
          @click="createLobbyAndNavigate"
        >
          {{ isCreating ? 'Creating lobby…' : 'Create Lobby' }}
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
import { usePlayer } from '@/composables/usePlayer'
import { createMultiplayerLobby } from '@/composables/useLobbyCreation'
import { putMapVersion } from '@/services/mapVersionCache'
import UiButton from '@/components/ui/UiButton.vue'
import ExitButton from '@/components/ui/ExitButton.vue'
import UiPanel from '@/components/ui/UiPanel.vue'
import MapList from '@/components/menu/MapList.vue'
import MinimapPreview from '@/components/menu/MinimapPreview.vue'

const router = useRouter()
const { playerId } = usePlayer()

const mapCatalog = ref<MapCatalogEntry[]>([])
const isLoadingMaps = ref(true)
const mapsLoadError = ref('')
const selectedMapId = ref('')

// Guards against double-clicks while createLobbyAndNavigate is in flight.
// Steam's LobbyCreated_t callback can take 1–2s; without this guard the
// user clicks repeatedly thinking nothing happened and ends up creating
// N stale lobbies that linger until they quit Steam.
const isCreating = ref(false)

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
    // Seed the map-version cache for every locally-known map that has a
    // contentHash. This lets FindGame/Lobby correctly identify when the
    // joiner already has the host's exact version without waiting for a
    // WelcomeMessage. Best-effort: errors from putMapVersion are swallowed.
    for (const m of maps) {
      if (m.contentHash) {
        putMapVersion({
          id: m.id,
          name: m.name,
          contentHash: m.contentHash,
          version: m.version ?? '',
          gridCols: m.gridCols,
          gridRows: m.gridRows,
          spawnPointCount: m.spawnPointCount,
        })
      }
    }
    // Hide campaign-tagged maps from the Custom Game lobby. Campaign maps
    // are only reachable from the Campaign panel so the objectives + level
    // gating apply. To use a campaign map's geometry in custom games,
    // duplicate-and-rename it in the editor without the campaign tag.
    const customGameMaps = maps.filter((m) => !m.campaignId)
    mapCatalog.value = customGameMaps
    if (customGameMaps.length > 0 && !selectedMapId.value) {
      selectedMapId.value = customGameMaps[0].id
    }
  } catch (err) {
    mapsLoadError.value = err instanceof Error ? err.message : 'Failed to load maps.'
  } finally {
    isLoadingMaps.value = false
  }
}

async function createLobbyAndNavigate() {
  if (!selectedMapId.value || isCreating.value) return
  isCreating.value = true
  try {
    const mapEntry = selectedMap.value
    const created = await createMultiplayerLobby({
      mapId: selectedMapId.value,
      hostPlayerId: playerId.value,
      mapHash: mapEntry?.contentHash,
      mapVersion: mapEntry?.version,
    })
    void router.push(`/lobby/${created.id}`)
  } catch (err) {
    mapsLoadError.value = err instanceof Error ? err.message : 'Failed to create lobby.'
  } finally {
    isCreating.value = false
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
