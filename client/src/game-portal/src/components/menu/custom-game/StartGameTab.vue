<template>
  <div class="cg-start" :style="assetVars">
    <!-- The whole map-selection area (Select Map list + map preview/detail)
         sits on a single dark inner panel (war-room-inner-panel art). -->
    <UiPanel variant="warRoomInner" :padding="0" class="cg-start__panel">
      <div class="cg-start__body">
        <!-- Left: Select Map list, on an inner-panel with the label above it. -->
        <div class="cg-start__left">
          <div class="cg-start__section-label">Select Map</div>
          <UiPanel variant="innerPanel" :padding="0" class="cg-start__list-panel">
            <MapList
              class="cg-start__maps"
              :maps="mapCatalog"
              :selected-map-id="selectedMapId"
              :loading="isLoadingMaps"
              @update:selected-map-id="onMapSelected"
            />
          </UiPanel>
          <div v-if="mapsLoadError" class="cg-start__error">{{ mapsLoadError }}</div>
        </div>

        <!-- Right: map preview on a world-inner panel, detail on an inner-panel. -->
        <div class="cg-start__right">
          <UiPanel variant="worldInner" :padding="0" class="cg-start__preview-panel">
            <div class="cg-start__preview">
              <MinimapPreview
                :map="selectedMap"
                :show-metadata="false"
                :max-display-size="200"
              />
            </div>
          </UiPanel>

          <UiPanel variant="innerPanel" :padding="0" class="cg-start__detail-panel">
            <div class="cg-start__detail">
              <div class="cg-start__detail-title">
                {{ selectedMap ? selectedMap.name : 'No map selected' }}
              </div>
              <dl v-if="selectedMap" class="cg-start__detail-grid">
                <dt>Size:</dt>
                <dd>{{ selectedMap.gridCols }} x {{ selectedMap.gridRows }}</dd>
                <dt>Players:</dt>
                <dd>1 - {{ Math.max(1, selectedMap.spawnPointCount) }}</dd>
                <dt>Description:</dt>
                <dd>{{ selectedMap.description || '—' }}</dd>
              </dl>
            </div>
          </UiPanel>
        </div>
      </div>
    </UiPanel>

    <!-- Footer, below the panel: Back (dark button art) on the left, then a
         smaller Create Lobby (blue button art). Back replaces the old close X. -->
    <div class="cg-start__footer">
      <BackButton @click="emit('back')" />
      <button
        type="button"
        class="cg-btn cg-btn--create"
        :disabled="!selectedMapId || isLoadingMaps || isCreating"
        @click="onCreateLobby"
      >
        <span class="cg-btn__label">{{ isCreating ? 'Creating lobby…' : 'Create Lobby' }}</span>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { fetchMapCatalog } from '@/game/maps/catalog'
import type { MapCatalogEntry } from '@/game/network/protocol'
import { usePlayer } from '@/composables/usePlayer'
import { createMultiplayerLobby } from '@/composables/useLobbyCreation'
import { putMapVersion } from '@/services/mapVersionCache'
import MapList from '@/components/menu/MapList.vue'
import MinimapPreview from '@/components/menu/MinimapPreview.vue'
import UiPanel from '@/components/ui/UiPanel.vue'
import BackButton from '@/components/menu/custom-game/BackButton.vue'
import createBtnUrl from '@/assets/ui/themes/updated/war-room/war-room-active-button.png'

// Emits the newly-created lobby id up to CustomGame so it can host the lobby
// inline in the same panel (mirrors the Campaign panel's Lobby flow) instead
// of routing to the standalone /lobby/:id page. `back` asks CustomGame to
// close the panel (the footer Back button replaces the old close X).
const emit = defineEmits<{
  (e: 'lobby-created', lobbyId: string): void
  (e: 'back'): void
}>()

const { playerId } = usePlayer()

const mapCatalog = ref<MapCatalogEntry[]>([])
const isLoadingMaps = ref(true)
const mapsLoadError = ref('')
const selectedMapId = ref('')

// Create-Lobby button art, exposed to scoped CSS as a custom property.
const assetVars = computed(() => ({
  '--cg-create-btn': `url(${createBtnUrl})`,
}))

// Guards against double-clicks while onCreateLobby is in flight.
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

async function onCreateLobby() {
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
    emit('lobby-created', created.id)
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

/* Single dark inner panel (war-room-inner-panel art) wrapping the whole
   map-selection area. */
.cg-start__panel {
  flex: 1 1 auto;
  display: flex;
  min-height: 0;
  min-width: 0;
}

/* Two-column body inside the panel: map list left, preview + detail + action
   right. Padding sits here (the panel itself renders with padding 0). */
.cg-start__body {
  flex: 1 1 auto;
  display: grid;
  grid-template-columns:
    minmax(0, calc(var(--s) * 460))
    minmax(0, calc(var(--s) * 380));
  grid-template-rows: minmax(0, 1fr);
  gap: calc(var(--s) * 20);
  padding: calc(var(--s) * 14) calc(var(--s) * 16);
  min-height: 0;
  min-width: 0;
}

/* Left column — Select Map list. Lifted by the same amount as the preview
   panel so the "Select Map" label lines up with the top of the map container. */
.cg-start__left {
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 8);
  margin-top: calc(var(--s) * -10);
  min-height: 0;
  min-width: 0;
}

/* World-inner panel wrapping the map list. */
.cg-start__list-panel {
  flex: 1 1 auto;
  display: flex;
  min-height: 0;
  min-width: 0;
}

.cg-start__right {
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 14);
  min-height: 0;
}

/* Section label — gold, flanked by short rules like the mockup. */
.cg-start__section-label {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: calc(var(--s) * 8);
  font-family: var(--font-title);
  font-size: calc(var(--s) * 15);
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: #e0bd7f;
}

.cg-start__section-label::before,
.cg-start__section-label::after {
  content: '';
  height: 1px;
  width: calc(var(--s) * 16);
  background: rgba(224, 189, 127, 0.6);
}

.cg-start__section-label::after {
  flex: 1 1 auto;
}

.cg-start__maps {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
  padding: calc(var(--s) * 6);
  box-sizing: border-box;
}

.cg-start__maps :deep(.map-list__scroll) {
  flex: 1 1 auto;
  max-height: none;
  min-height: 0;
}

/* World-inner panel around the map preview. Takes whatever vertical space the
   details below don't need (details size to their content), so the map shrinks
   to fit instead of the details overflowing. A min-height floor keeps the map
   visible even when the description is long. */
.cg-start__preview-panel {
  flex: 1 1 auto;
  display: flex;
  min-height: calc(var(--s) * 90);
  margin-top: calc(var(--s) * -10);
}

/* No padding so the map sits flush against the world-inner frame's edges. */
.cg-start__preview {
  flex: 1 1 auto;
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 0;
  overflow: hidden;
}

/* Bare minimap fills the preview box so the canvas below can scale down to it. */
.cg-start__preview :deep(.minimap-preview--bare) {
  width: 100%;
  height: 100%;
  min-height: 0;
  border: 0;
  background: transparent;
  padding: 0;
  box-sizing: border-box;
}

/* Let the canvas scale down to fit the (shrinking) preview box while keeping the
   map's aspect ratio. Overrides the component's fixed inline px size; the
   max-display-size still caps the resolution so it never upscales past it. */
.cg-start__preview :deep(.minimap-preview__canvas) {
  width: auto !important;
  height: auto !important;
  max-width: 100%;
  max-height: 100%;
}

.cg-start__preview :deep(.minimap-preview__empty--bare) {
  color: rgba(233, 219, 184, 0.5);
}

/* Inner-panel frame around the map details. Sizes to its content (the map above
   gives up space to it), so the detail rows are always fully visible without
   scrolling. */
.cg-start__detail-panel {
  flex: 0 0 auto;
  display: flex;
}

/* Detail block — gold labels, cream values on the dark panel. Text sized down
   so it fits cleanly inside the panel; content pulled up ~10px. */
.cg-start__detail {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 2);
  padding: calc(var(--s) * 8) calc(var(--s) * 10);
  margin-top: -10px;
  min-width: 0;
}

.cg-start__detail-title {
  font-family: var(--font-title);
  font-size: calc(var(--s) * 15);
  font-weight: 700;
  letter-spacing: 0.04em;
  color: #e7c88a;
}

.cg-start__detail-grid {
  display: grid;
  grid-template-columns: max-content 1fr;
  column-gap: calc(var(--s) * 10);
  row-gap: 0;
  margin: 0;
  line-height: 1.25;
}

.cg-start__detail-grid dt {
  font-family: var(--font-title);
  font-size: calc(var(--s) * 10);
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #c7a768;
}

.cg-start__detail-grid dd {
  margin: 0;
  font-size: calc(var(--s) * 11);
  color: #e9dbb8;
}

.cg-start__error {
  font-size: calc(var(--s) * 13);
  color: #e88a6a;
}

/* Footer below the panel: Back on the left, then a smaller Create Lobby. */
/* Shared footer geometry across all tabs so the Back button never shifts when
   switching tabs: pinned to the bottom, same top padding, no bottom padding. */
.cg-start__footer {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: calc(var(--s) * 12);
  padding-top: calc(var(--s) * 12);
}

/* Footer buttons use the war-room button art via border-image (frame stays
   crisp). ~50% narrower than the old full-width Create Lobby. */
.cg-btn {
  flex: 0 0 auto;
  min-width: calc(var(--s) * 150);
  padding: calc(var(--s) * 4) calc(var(--s) * 16);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: none;
  border: calc(var(--s) * 16) solid transparent;
  border-image-slice: 16 fill;
  border-image-width: calc(var(--s) * 16);
  border-image-repeat: stretch;
  image-rendering: pixelated;
  transition:
    filter 120ms ease,
    transform 80ms ease;
}

/* Hover: brighten the button art. Active (click): dim + press down 1px. */
.cg-btn:hover:not(:disabled) {
  filter: brightness(1.12);
}

.cg-btn:active:not(:disabled) {
  filter: brightness(0.9);
  transform: translateY(1px);
}

.cg-btn--create {
  border-image-source: var(--cg-create-btn);
  /* Push Create Lobby to the right edge of the footer; Back stays left. */
  margin-left: auto;
}

.cg-btn__label {
  font-family: var(--font-title);
  font-size: calc(var(--s) * 15);
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #f4e3b6;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.7);
}

.cg-btn:disabled .cg-btn__label {
  color: rgba(244, 227, 182, 0.4);
}

.cg-btn:disabled {
  /* `cursor: not-allowed` is the system semantic for "forbidden action" — the
     project rule (CLAUDE.md → AI_RULES.md) allows it on disabled states. */
  cursor: not-allowed;
  filter: grayscale(0.4) brightness(0.8);
}
</style>
