<template>
  <div class="panel-lobby">
    <div class="panel-lobby__header">
      <button
        type="button"
        class="panel-lobby__back"
        aria-label="Back"
        @click="leaveAndGoBack"
      >
        &larr; Back
      </button>
      <div class="panel-lobby__header-info">
        <span class="panel-lobby__title">{{ lobby?.mapName ?? 'Lobby' }}</span>
        <span class="panel-lobby__slots">
          {{ lobby?.players.length ?? 0 }} / {{ lobby?.maxPlayers ?? 4 }} Players
        </span>
        <span v-if="showMapVersionPlaceholder" class="panel-lobby__map-version-hint">
          Host's custom map — loads at start
        </span>
      </div>
    </div>

    <div v-if="lobby" class="panel-lobby__body">
      <div class="panel-lobby__section-label">Players</div>
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

      <div v-if="!isHost" class="panel-lobby__waiting">
        Waiting for the host to start the game…
      </div>
      <div v-if="startError" class="panel-lobby__error">{{ startError }}</div>
      <div v-if="inviteError" class="panel-lobby__error">{{ inviteError }}</div>
    </div>

    <div v-else class="panel-lobby__not-found">
      Lobby not found.
    </div>

    <div class="panel-lobby__footer">
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
        {{ inviteBusy ? 'Opening overlay…' : 'Invite Friend' }}
      </button>
      <button
        v-if="isHost"
        type="button"
        class="cg-action cg-action--start"
        :disabled="!lobby || isStarting"
        @click="startGame"
      >
        {{ isStarting ? 'Starting…' : 'Start Game' }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { toRef } from 'vue'
import { formatDisplayName } from '@/composables/usePlayer'
import { useLobbyRoom } from '@/composables/useLobbyRoom'

const props = defineProps<{
  lobbyId: string
}>()

const emit = defineEmits<{
  (e: 'back'): void
}>()

// Shared in-panel lobby, hosted inside a parchment panel (Campaign level
// list and Custom Game → Start Game). Leaving / the lobby vanishing pops
// back to whatever the host panel was showing (the caller listens on @back).
// Match-start navigation is handled inside the composable (→ /match/:id).
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
</script>

<style scoped>
.panel-lobby {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 14);
  min-height: 0;
  color: #3a1f0a;
}

.panel-lobby__header {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: calc(var(--s) * 16);
}

.panel-lobby__back {
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: calc(var(--s) * 14);
  font-weight: 700;
  letter-spacing: 0.05em;
  padding: calc(var(--s) * 6) calc(var(--s) * 14);
  border-radius: calc(var(--s) * 4);
  border: 1px solid rgba(58, 31, 10, 0.5);
  color: #2a1505;
  background: linear-gradient(180deg, #c0a98a 0%, #8a7350 100%);
}

.panel-lobby__header-info {
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 2);
}

.panel-lobby__title {
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: calc(var(--s) * 22);
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.panel-lobby__slots {
  font-size: calc(var(--s) * 12);
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: rgba(58, 31, 10, 0.7);
}

.panel-lobby__map-version-hint {
  font-size: calc(var(--s) * 11);
  font-style: italic;
  color: rgba(122, 80, 20, 0.85);
  letter-spacing: 0.04em;
}

.panel-lobby__body {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 8);
  min-height: 0;
  overflow-y: auto;
}

.panel-lobby__section-label {
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: calc(var(--s) * 14);
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: rgba(58, 31, 10, 0.75);
}

.panel-lobby__players {
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 6);
}

.panel-lobby__slot {
  display: flex;
  align-items: center;
  gap: calc(var(--s) * 10);
  padding: calc(var(--s) * 10) calc(var(--s) * 14);
  border-radius: calc(var(--s) * 4);
  border: 1px solid rgba(58, 31, 10, 0.25);
  background: rgba(245, 234, 210, 0.4);
  min-height: calc(var(--s) * 40);
}

.panel-lobby__slot--filled {
  border-color: #8a5a2a;
  background: rgba(200, 180, 110, 0.5);
}

.panel-lobby__player-id {
  font-size: calc(var(--s) * 14);
  font-weight: 600;
  color: #2a1505;
}

.panel-lobby__player-tag {
  font-size: calc(var(--s) * 11);
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #7a3a10;
}

.panel-lobby__player-empty {
  font-size: calc(var(--s) * 12);
  color: rgba(58, 31, 10, 0.4);
}

.panel-lobby__waiting {
  font-size: calc(var(--s) * 13);
  font-style: italic;
  color: rgba(58, 31, 10, 0.7);
}

.panel-lobby__error {
  font-size: calc(var(--s) * 13);
  color: #7a1a1a;
}

.panel-lobby__not-found {
  color: rgba(58, 31, 10, 0.55);
  font-size: calc(var(--s) * 14);
  text-align: center;
  padding: calc(var(--s) * 40) 0;
}

.panel-lobby__footer {
  flex: 0 0 auto;
  display: flex;
  gap: calc(var(--s) * 10);
  justify-content: flex-end;
  align-items: center;
}

.panel-lobby__steam-pending {
  font-size: calc(var(--s) * 12);
  font-style: italic;
  color: rgba(58, 31, 10, 0.65);
  padding-right: calc(var(--s) * 8);
}

/* Shared parchment action button — matches the Campaign panel's action
   buttons so the lobby reads as part of the same parchment surface. */
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
  background: linear-gradient(180deg, #c0a98a 0%, #8a7350 100%);
  min-width: calc(var(--s) * 130);
}

.cg-action--start {
  background: linear-gradient(180deg, #d8b06a 0%, #a87a36 100%);
}

.cg-action--muted {
  background: linear-gradient(180deg, #c0a98a 0%, #8a7350 100%);
}

.cg-action:disabled {
  background: rgba(180, 160, 110, 0.4);
  color: rgba(58, 31, 10, 0.45);
  /* `cursor: not-allowed` is the system semantic for "forbidden action" — the
     project rule (CLAUDE.md → AI_RULES.md) allows it on disabled states. */
  cursor: not-allowed;
}
</style>
