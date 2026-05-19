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
import { useLobbies } from '@/composables/useLobbies'
import { usePlayer } from '@/composables/usePlayer'
import { getSteamPlayer, openLobby } from '@/services/desktopBridge'
import { STEAM_LOBBY_ID_KEY } from '@/game/network/NetworkClient'
import {
  beginSteamLobbyPairing,
  completeSteamLobbyPairing,
} from '@/state/steamLobbyState'
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
  console.log('[CreateGame] click received', {
    selectedMapId: selectedMapId.value,
    isCreating: isCreating.value,
  })
  if (!selectedMapId.value || isCreating.value) {
    console.log('[CreateGame] guard tripped — early return')
    return
  }
  isCreating.value = true
  try {
    // Step 1: create the local lobby. Fast (~50ms HTTP POST).
    console.log('[CreateGame] POST /lobbies …')
    const created = await createLobby({ mapId: selectedMapId.value, hostPlayerId: playerId.value })
    console.log('[CreateGame] local lobby created', created.id)

    // Step 2: §14R-B + optimistic-nav fix. We do NOT await the Steam
    // lobby creation here — LobbyCreated_t latency is 1–2s and blocking
    // navigation that long makes the button feel broken. Instead we:
    //   (a) seed the reactive Steam-pairing state as "pending"
    //   (b) navigate to /lobby/<id> immediately
    //   (c) run openLobby in the background; when it resolves it writes
    //       both sessionStorage (so reload works) and the reactive state
    //       (so Lobby.vue's Invite button shows up live without remount)
    // If Steam is unavailable the background promise resolves quickly
    // with null and the Invite button simply never appears.
    beginSteamLobbyPairing(created.id)
    // Clear any stale sessionStorage from a previous run before the
    // background promise has had a chance to write — Lobby.vue's mount
    // would otherwise pick up a dead Steam lobby id.
    try {
      sessionStorage.removeItem(STEAM_LOBBY_ID_KEY)
    } catch {
      /* sessionStorage may be sandboxed; non-fatal */
    }

    void runBackgroundSteamLobbyCreate(created.id, selectedMapId.value)
    console.log('[CreateGame] navigating to /lobby/' + created.id)
    void router.push(`/lobby/${created.id}`)
  } catch (err) {
    console.error('[CreateGame] failed:', err)
    mapsLoadError.value = err instanceof Error ? err.message : 'Failed to create lobby.'
  } finally {
    isCreating.value = false
  }
}

/** Runs openLobby off the click-handler critical path. On success writes
 *  both sessionStorage (so a /lobby reload still finds the Steam lobby
 *  id) and the reactive pairing state (so Lobby.vue's Invite button
 *  becomes live without needing a remount). On failure logs and clears
 *  the pending state so the UI stops showing "connecting…". */
async function runBackgroundSteamLobbyCreate(
  localLobbyId: string,
  mapId: string,
): Promise<void> {
  console.log('[SteamCreate] start for localLobbyId=' + localLobbyId)
  try {
    console.log('[SteamCreate] getSteamPlayer …')
    const steamPlayer = await getSteamPlayer()
    console.log('[SteamCreate] getSteamPlayer →', steamPlayer)
    if (!steamPlayer) {
      console.warn('[SteamCreate] Steam unavailable — no Invite button this session')
      completeSteamLobbyPairing(localLobbyId, null)
      return
    }
    console.log('[SteamCreate] openLobby …')
    const handle = await openLobby({
      maxPlayers: 4,
      mapId,
      localLobbyId,
      hostPersona: steamPlayer.personaName,
    })
    console.log('[SteamCreate] openLobby →', handle)
    const steamLobbyId = handle?.lobbyId ?? null
    if (steamLobbyId) {
      try {
        sessionStorage.setItem(STEAM_LOBBY_ID_KEY, steamLobbyId)
      } catch {
        /* sessionStorage may be sandboxed; non-fatal */
      }
    }
    console.log('[SteamCreate] completeSteamLobbyPairing', { localLobbyId, steamLobbyId })
    completeSteamLobbyPairing(localLobbyId, steamLobbyId)
  } catch (err) {
    console.error('[SteamCreate] failed:', err)
    completeSteamLobbyPairing(localLobbyId, null)
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
