<template>
  <div class="cg-start">
    <div class="cg-start__body">
      <div class="cg-start__left">
        <div class="cg-start__section-label">Select Map</div>
        <MapList
          class="cg-start__maps"
          :maps="mapCatalog"
          :selected-map-id="selectedMapId"
          :loading="isLoadingMaps"
          @update:selected-map-id="onMapSelected"
        />
        <div v-if="mapsLoadError" class="cg-start__error">{{ mapsLoadError }}</div>
      </div>

      <div class="cg-start__right">
        <div class="cg-start__preview">
          <MinimapPreview
            :map="selectedMap"
            :show-metadata="false"
            :max-display-size="220"
          />
        </div>

        <button
          type="button"
          class="cg-action cg-action--start"
          :disabled="!selectedMapId || isLoadingMaps || isCreating"
          @click="createLobbyAndNavigate"
        >
          {{ isCreating ? 'Creating lobby…' : 'Create Lobby' }}
        </button>
      </div>
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
.cg-start {
  display: flex;
  flex-direction: column;
  min-height: 0;
  flex: 1 1 auto;
}

/* Two-column body: map list left, preview + action right — mirrors the
   Campaign panel's two-column detail layout so both parchment panels share
   the same proportions at the same slot size. */
.cg-start__body {
  flex: 1 1 auto;
  display: grid;
  grid-template-columns:
    minmax(0, calc(var(--s) * 420))
    minmax(0, calc(var(--s) * 360));
  /* Single row that fills the panel height so the left column (and the map
     list inside it) can flex to the full available space before scrolling,
     rather than collapsing to content height. */
  grid-template-rows: minmax(0, 1fr);
  gap: calc(var(--s) * 18);
  justify-content: center;
  min-height: 0;
}

.cg-start__left {
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 8);
  min-height: 0;
}

.cg-start__section-label {
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: calc(var(--s) * 14);
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: rgba(58, 31, 10, 0.75);
}

/* Let the map list grow to fill the whole left column instead of MapList's
   default fixed 360px cap, so the parchment slot's headroom is used before a
   scrollbar appears. The :deep override releases the component's max-height
   and lets its inner scroll area flex to the available height. */
.cg-start__maps {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.cg-start__maps :deep(.map-list__scroll) {
  flex: 1 1 auto;
  max-height: none;
  min-height: 0;
}

.cg-start__right {
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 12);
  min-height: 0;
}

.cg-start__preview {
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 6);
}

/* Frame the bare minimap the same way the Campaign panel does, so the
   preview reads as part of the parchment surface. */
.cg-start__preview :deep(.minimap-preview--bare) {
  align-self: flex-start;
  width: fit-content;
  height: auto;
  min-height: 0;
  border: 1px solid #8a5a2a;
  border-radius: calc(var(--s) * 4);
  background: rgba(245, 234, 210, 0.45);
  padding: 8px;
  box-sizing: border-box;
}

.cg-start__preview :deep(.minimap-preview__empty--bare) {
  color: rgba(58, 31, 10, 0.55);
}

.cg-start__error {
  font-size: calc(var(--s) * 13);
  color: #7a1a1a;
}

.cg-action {
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: calc(var(--s) * 14);
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  padding: calc(var(--s) * 8) calc(var(--s) * 18);
  border-radius: calc(var(--s) * 4);
  border: 1px solid rgba(58, 31, 10, 0.55);
  color: #2a1505;
  align-self: flex-start;
  min-width: calc(var(--s) * 160);
}

.cg-action--start {
  background: linear-gradient(180deg, #d8b06a 0%, #a87a36 100%);
}

.cg-action:disabled {
  background: rgba(180, 160, 110, 0.4);
  color: rgba(58, 31, 10, 0.45);
  /* `cursor: not-allowed` is the system semantic for "forbidden action" — the
     project rule (CLAUDE.md → AI_RULES.md) allows it on disabled states. */
  cursor: not-allowed;
}
</style>
