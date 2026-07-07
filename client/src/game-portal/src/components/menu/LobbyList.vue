<template>
  <div class="lobby-list" role="table" aria-label="Open lobbies">
    <div class="lobby-list__header" role="row">
      <div class="lobby-list__cell lobby-list__cell--header" role="columnheader">Host</div>
      <div class="lobby-list__cell lobby-list__cell--header" role="columnheader">Map</div>
      <div class="lobby-list__cell lobby-list__cell--header" role="columnheader">Players</div>
    </div>

    <button
      v-for="lobby in lobbies"
      :key="lobby.id"
      class="lobby-list__row"
      role="row"
      type="button"
      @click="emit('join', lobby.id)"
    >
      <div class="lobby-list__cell" role="cell">{{ lobby.hostPlayerId }}</div>
      <div class="lobby-list__cell" role="cell">{{ lobby.mapName }}</div>
      <div class="lobby-list__cell" role="cell">{{ lobby.players.length }} / {{ lobby.maxPlayers }}</div>
    </button>

    <div v-if="lobbies.length === 0" class="lobby-list__empty">
      No open lobbies found.
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Lobby } from '@/game/network/protocol'

defineProps<{
  lobbies: readonly Lobby[]
}>()

const emit = defineEmits<{
  join: [id: string]
}>()
</script>

<style scoped>
.lobby-list {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.lobby-list__header {
  display: grid;
  grid-template-columns: 1fr 1fr auto;
  padding: 6px 14px;
}

/* Parchment palette — this list lives only inside the war-room Custom Game
   panel, so it's themed to match the parchment surface (dark ink on warm
   paper) rather than the dark UI panels. */
.lobby-list__row {
  display: grid;
  grid-template-columns: 1fr 1fr auto;
  padding: 10px 14px;
  border-radius: 6px;
  border: 1px solid rgba(58, 31, 10, 0.25);
  background: rgba(245, 234, 210, 0.45);
  cursor: pointer;
  text-align: left;
  transition: background 0.1s, border-color 0.1s;
}

.lobby-list__row:hover {
  background: rgba(230, 214, 178, 0.6);
  border-color: rgba(58, 31, 10, 0.4);
}

.lobby-list__row:focus-visible {
  outline: 2px solid #8a5a2a;
  outline-offset: 2px;
  border-radius: 6px;
}

.lobby-list__cell {
  font-size: 13px;
  color: #3a1f0a;
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  padding-right: 12px;
}

.lobby-list__cell--header {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: rgba(58, 31, 10, 0.75);
}

.lobby-list__empty {
  padding: 24px;
  text-align: center;
  color: rgba(58, 31, 10, 0.55);
  font-size: 13px;
}
</style>
