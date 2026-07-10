<template>
  <div class="panel-lobby" :style="assetVars">
    <div class="panel-lobby__header-info">
      <span class="panel-lobby__title">{{ lobby?.mapName ?? 'Lobby' }}</span>
      <span class="panel-lobby__slots">
        {{ lobby?.players.length ?? 0 }} / {{ lobby?.maxPlayers ?? 4 }} Players
      </span>
      <span v-if="showMapVersionPlaceholder" class="panel-lobby__map-version-hint">
        Host's custom map — loads at start
      </span>
    </div>

    <div v-if="lobby" class="panel-lobby__body">
      <!-- Left: players. -->
      <div class="panel-lobby__left">
        <div class="panel-lobby__section-label">Players</div>
        <UiPanel variant="innerPanel" :padding="0" class="panel-lobby__players-panel">
          <div class="panel-lobby__players">
            <div
              v-for="i in lobby.maxPlayers"
              :key="i"
              class="panel-lobby__slot"
              :class="{ 'panel-lobby__slot--filled': lobby.players[i - 1] }"
            >
              <template v-if="lobby.players[i - 1]">
                <span class="panel-lobby__player-id">{{ formatDisplayName(lobby.players[i - 1]) }}</span>
                <span
                  v-if="lobby.players[i - 1] === lobby.hostPlayerId"
                  class="panel-lobby__player-tag"
                >(host)</span>
              </template>
              <span v-else class="panel-lobby__player-empty">— empty —</span>
            </div>
          </div>
        </UiPanel>
        <div v-if="!isHost" class="panel-lobby__waiting">
          Waiting for the host to start the game…
        </div>
        <div v-if="startError" class="panel-lobby__error">{{ startError }}</div>
        <div v-if="inviteError" class="panel-lobby__error">{{ inviteError }}</div>
      </div>

      <!-- Right: map preview + details (same as Start Game). -->
      <div class="panel-lobby__right">
        <UiPanel variant="worldInner" :padding="0" class="panel-lobby__preview-panel">
          <div class="panel-lobby__preview">
            <MinimapPreview
              :map="selectedMap"
              :show-metadata="false"
              :max-display-size="240"
            />
          </div>
        </UiPanel>

        <UiPanel variant="innerPanel" :padding="0" class="panel-lobby__detail-panel">
          <div class="panel-lobby__detail">
            <div class="panel-lobby__detail-title">
              {{ selectedMap ? selectedMap.name : (lobby.mapName || 'Map') }}
            </div>
            <dl v-if="selectedMap" class="panel-lobby__detail-grid">
              <dt>Size:</dt>
              <dd>{{ selectedMap.gridCols }} x {{ selectedMap.gridRows }}</dd>
              <dt>Players:</dt>
              <dd>1 - {{ Math.max(1, selectedMap.spawnPointCount) }}</dd>
              <dt>Description:</dt>
              <dd>{{ selectedMap.description || '—' }}</dd>
            </dl>
            <div v-else class="panel-lobby__detail-empty">
              Map details load at start.
            </div>
          </div>
        </UiPanel>
      </div>
    </div>

    <div v-else class="panel-lobby__not-found">
      Lobby not found.
    </div>

    <div class="panel-lobby__footer">
      <BackButton @click="leaveAndGoBack" />

      <div class="panel-lobby__footer-right">
        <span
          v-if="isHost && steamLobbyPending && !steamLobbyId"
          class="panel-lobby__steam-pending"
        >
          Setting up Steam invite…
        </span>
        <button
          v-if="isHost && steamLobbyId"
          type="button"
          class="cg-action cg-action--muted"
          :disabled="inviteBusy"
          @click="onInvite"
        >
          <span class="cg-action__label">{{ inviteBusy ? 'Opening overlay…' : 'Invite Friend' }}</span>
        </button>
        <button
          v-if="isHost"
          type="button"
          class="cg-action cg-action--start"
          :disabled="!lobby || isStarting"
          @click="startGame"
        >
          <span class="cg-action__label">{{ isStarting ? 'Starting…' : 'Start Game' }}</span>
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, toRef } from 'vue'
import { formatDisplayName } from '@/composables/usePlayer'
import { useLobbyRoom } from '@/composables/useLobbyRoom'
import { fetchMapCatalog } from '@/game/maps/catalog'
import type { MapCatalogEntry } from '@/game/network/protocol'
import UiPanel from '@/components/ui/UiPanel.vue'
import MinimapPreview from '@/components/menu/MinimapPreview.vue'
import BackButton from '@/components/menu/custom-game/BackButton.vue'
import activeBtnUrl from '@/assets/ui/themes/updated/war-room/war-room-active-button.png'
import inactiveBtnUrl from '@/assets/ui/themes/updated/war-room/war-room-inactive-button.png'

const props = defineProps<{
  lobbyId: string
}>()

const emit = defineEmits<{
  (e: 'back'): void
}>()

// Button art exposed to scoped CSS as custom properties.
const assetVars = computed(() => ({
  '--btn-active': `url(${activeBtnUrl})`,
  '--btn-inactive': `url(${inactiveBtnUrl})`,
}))

// Shared lobby room, used both here (host, hosted inside the Custom Game /
// Campaign panel) and by the joiner's routed Lobby.vue — so both players see
// the identical panel. Leaving / the lobby vanishing pops back to whatever the
// caller shows (it listens on @back). Match-start navigation is handled inside
// the composable (→ /match/:id).
const {
  lobby,
  isHost,
  isStarting,
  startError,
  inviteError,
  inviteBusy,
  steamLobbyId,
  steamLobbyPending,
  showMapVersionPlaceholder,
  startGame,
  onInvite,
  leaveAndGoBack,
} = useLobbyRoom(toRef(props, 'lobbyId'), {
  onLeave: () => emit('back'),
})

// Map catalog entry for the lobby's map, for the preview + details column.
// Best-effort: a joiner on a host's custom map may not have it locally, in
// which case selectedMap is null and the column shows a placeholder.
const mapCatalog = ref<MapCatalogEntry[]>([])
const selectedMap = computed(
  () => mapCatalog.value.find((m) => m.id === lobby.value?.mapId) ?? null,
)

onMounted(async () => {
  try {
    mapCatalog.value = await fetchMapCatalog()
  } catch {
    /* preview column falls back to the placeholder */
  }
})
</script>

<style scoped>
.panel-lobby {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 12);
  min-height: 0;
  color: #e9dbb8;
}

.panel-lobby__header-info {
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 2);
}

.panel-lobby__title {
  font-family: var(--font-title);
  font-size: calc(var(--s) * 22);
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #e7c88a;
}

.panel-lobby__slots {
  font-size: calc(var(--s) * 12);
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #c7a768;
}

.panel-lobby__map-version-hint {
  font-size: calc(var(--s) * 11);
  font-style: italic;
  color: rgba(224, 189, 127, 0.85);
  letter-spacing: 0.04em;
}

/* Two-column body: players left, map preview + details right. */
.panel-lobby__body {
  flex: 1 1 auto;
  display: grid;
  grid-template-columns:
    minmax(0, 1fr)
    minmax(0, calc(var(--s) * 360));
  grid-template-rows: minmax(0, 1fr);
  gap: calc(var(--s) * 18);
  min-height: 0;
}

.panel-lobby__left {
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 8);
  min-height: 0;
  min-width: 0;
}

/* Gold section label flanked by short rules — matches the Custom Game tabs. */
.panel-lobby__section-label {
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

.panel-lobby__section-label::before,
.panel-lobby__section-label::after {
  content: '';
  height: 1px;
  width: calc(var(--s) * 16);
  background: rgba(224, 189, 127, 0.6);
}

/* Players on an inner-panel well. */
.panel-lobby__players-panel {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
}

.panel-lobby__players {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 6);
  padding: calc(var(--s) * 8);
  box-sizing: border-box;
}

.panel-lobby__slot {
  display: flex;
  align-items: center;
  gap: calc(var(--s) * 10);
  padding: calc(var(--s) * 10) calc(var(--s) * 14);
  border-radius: calc(var(--s) * 4);
  border: 1px solid rgba(198, 158, 90, 0.3);
  background: rgba(0, 0, 0, 0.28);
  min-height: calc(var(--s) * 40);
}

.panel-lobby__slot--filled {
  border-color: rgba(198, 158, 90, 0.6);
  background: rgba(0, 0, 0, 0.42);
}

.panel-lobby__player-id {
  font-size: calc(var(--s) * 14);
  font-weight: 600;
  color: #f0e2c0;
}

.panel-lobby__player-tag {
  font-size: calc(var(--s) * 11);
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #e0bd7f;
}

.panel-lobby__player-empty {
  font-size: calc(var(--s) * 12);
  color: rgba(233, 219, 184, 0.4);
}

.panel-lobby__waiting {
  font-size: calc(var(--s) * 13);
  font-style: italic;
  color: rgba(233, 219, 184, 0.7);
}

.panel-lobby__error {
  font-size: calc(var(--s) * 13);
  color: #e88a6a;
}

/* Right column — map preview + details, mirroring the Start Game tab. */
.panel-lobby__right {
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 14);
  min-height: 0;
}

.panel-lobby__preview-panel {
  flex: 0 0 auto;
  display: flex;
}

.panel-lobby__preview {
  flex: 1 1 auto;
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 0;
}

.panel-lobby__preview :deep(.minimap-preview--bare) {
  width: fit-content;
  height: auto;
  min-height: 0;
  border: 0;
  background: transparent;
  padding: 0;
  box-sizing: border-box;
}

.panel-lobby__preview :deep(.minimap-preview__empty--bare) {
  color: rgba(233, 219, 184, 0.5);
}

.panel-lobby__detail-panel {
  flex: 1 1 auto;
  display: flex;
}

.panel-lobby__detail {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 2);
  padding: calc(var(--s) * 8) calc(var(--s) * 10);
  min-width: 0;
}

.panel-lobby__detail-title {
  font-family: var(--font-title);
  font-size: calc(var(--s) * 15);
  font-weight: 700;
  letter-spacing: 0.04em;
  color: #e7c88a;
}

.panel-lobby__detail-grid {
  display: grid;
  grid-template-columns: max-content 1fr;
  column-gap: calc(var(--s) * 10);
  row-gap: calc(var(--s) * 1);
  margin: 0;
}

.panel-lobby__detail-grid dt {
  font-family: var(--font-title);
  font-size: calc(var(--s) * 10);
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #c7a768;
}

.panel-lobby__detail-grid dd {
  margin: 0;
  font-size: calc(var(--s) * 11);
  color: #e9dbb8;
}

.panel-lobby__detail-empty {
  font-size: calc(var(--s) * 11);
  font-style: italic;
  color: rgba(233, 219, 184, 0.55);
}

.panel-lobby__not-found {
  color: rgba(233, 219, 184, 0.55);
  font-size: calc(var(--s) * 14);
  text-align: center;
  padding: calc(var(--s) * 40) 0;
}

/* Footer — Back on the bottom-left, host actions on the bottom-right. */
.panel-lobby__footer {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: calc(var(--s) * 10);
}

.panel-lobby__footer-right {
  display: flex;
  align-items: center;
  gap: calc(var(--s) * 10);
}

.panel-lobby__steam-pending {
  font-size: calc(var(--s) * 12);
  font-style: italic;
  color: rgba(233, 219, 184, 0.65);
  padding-right: calc(var(--s) * 8);
}

/* War-room button art — blue active for Start Game, dark for Back / Invite. */
.cg-action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: calc(var(--s) * 130);
  padding: calc(var(--s) * 6) calc(var(--s) * 18);
  background: none;
  border: calc(var(--s) * 15) solid transparent;
  border-image-source: var(--btn-inactive);
  border-image-slice: 14 fill;
  border-image-width: calc(var(--s) * 15);
  border-image-repeat: stretch;
  image-rendering: pixelated;
  transition:
    filter 120ms ease,
    transform 80ms ease;
}

.cg-action__label {
  font-family: var(--font-title);
  font-size: calc(var(--s) * 14);
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #f4e3b6;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.7);
}

.cg-action--start {
  border-image-source: var(--btn-active);
}

.cg-action--muted {
  border-image-source: var(--btn-inactive);
}

.cg-action:hover:not(:disabled) {
  filter: brightness(1.12);
}

.cg-action:active:not(:disabled) {
  filter: brightness(0.9);
  transform: translateY(1px);
}

.cg-action:disabled {
  cursor: not-allowed;
  filter: grayscale(0.4) brightness(0.8);
}

.cg-action:disabled .cg-action__label {
  color: rgba(244, 227, 182, 0.4);
}
</style>
