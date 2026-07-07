<template>
  <div class="lobby">
    <div class="lobby__layout">
      <header class="lobby__header">
        <ExitButton @click="leaveAndGoBack" />
        <div class="lobby__header-info">
          <h1 class="lobby__title">{{ lobby?.mapName ?? 'Lobby' }}</h1>
          <span class="lobby__slots">{{ lobby?.players.length ?? 0 }} / {{ lobby?.maxPlayers ?? 4 }} Players</span>
          <span v-if="showMapVersionPlaceholder" class="lobby__map-version-hint">
            Host's custom map — loads at start
          </span>
        </div>
      </header>

      <div v-if="lobby" class="lobby__body">
        <UiPanel class="lobby__players-panel" :padding="16">
          <div class="lobby__section-label">Players</div>
          <LobbyPlayerList
            :players="lobby.players"
            :host-player-id="lobby.hostPlayerId"
            :max-players="lobby.maxPlayers"
          />
        </UiPanel>
        <div v-if="!isHost" class="lobby__waiting">
          Waiting for the host to start the game…
        </div>
        <div v-if="startError" class="lobby__error">{{ startError }}</div>
        <div v-if="inviteError" class="lobby__error">{{ inviteError }}</div>
      </div>

      <div v-else class="lobby__not-found">
        Lobby not found.
      </div>

      <footer class="lobby__footer">
        <span
          v-if="isHost && steamLobbyPending && !steamLobbyId"
          class="lobby__steam-pending"
        >
          Setting up Steam invite…
        </span>
        <UiButton
          v-if="isHost && steamLobbyId"
          size="md"
          :disabled="inviteBusy"
          @click="onInvite"
        >
          {{ inviteBusy ? 'Opening overlay…' : 'Invite Friend' }}
        </UiButton>
        <UiButton
          v-if="isHost"
          size="lg"
          :disabled="!lobby || isStarting"
          @click="startGame"
        >
          {{ isStarting ? 'Starting…' : 'Start Game' }}
        </UiButton>
        <UiButton size="md" @click="leaveAndGoBack">Leave</UiButton>
      </footer>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useLobbyRoom } from '@/composables/useLobbyRoom'
import UiPanel from '@/components/ui/UiPanel.vue'
import UiButton from '@/components/ui/UiButton.vue'
import ExitButton from '@/components/ui/ExitButton.vue'
import LobbyPlayerList from '@/components/menu/LobbyPlayerList.vue'

const router = useRouter()
const route = useRoute()

const lobbyId = computed(() => route.params.id as string)

// Routed lobby (Custom Game flow). Leaving / the lobby vanishing routes back
// to the war-room's Find Game tab. Match-start navigation is handled inside
// the composable.
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
} = useLobbyRoom(lobbyId, {
  onLeave: () => { void router.push('/war-room?tab=custom&sub=find') },
})
</script>

<style scoped>
.lobby {
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

.lobby__layout {
  display: flex;
  flex-direction: column;
  gap: 24px;
  width: 100%;
  max-width: 600px;
}

.lobby__header {
  display: flex;
  align-items: center;
  gap: 20px;
}

.lobby__header-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.lobby__title {
  font-size: 22px;
  font-weight: 700;
  color: #f5ead2;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  margin: 0;
}

.lobby__slots {
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.08em;
  color: #d7bb84;
  text-transform: uppercase;
}

.lobby__body {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.lobby__players-panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.lobby__section-label {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: #d7bb84;
}

.lobby__footer {
  display: flex;
  gap: 12px;
  justify-content: flex-end;
}

.lobby__not-found {
  color: #8899bb;
  font-size: 14px;
  text-align: center;
  padding: 40px 0;
}

.lobby__error {
  font-size: 13px;
  color: #f07070;
}

.lobby__waiting {
  font-size: 13px;
  font-style: italic;
  color: rgba(245, 234, 210, 0.75);
}

.lobby__steam-pending {
  font-size: 12px;
  font-style: italic;
  color: rgba(245, 234, 210, 0.65);
  align-self: center;
  padding-right: 8px;
}

.lobby__map-version-hint {
  font-size: 11px;
  font-style: italic;
  color: rgba(215, 187, 132, 0.7);
  letter-spacing: 0.04em;
}
</style>
