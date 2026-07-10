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

/* Dark war-room palette — this list lives inside the Custom Game panel's dark
   inner panels, so cream/gold text on dark rows with a brass edge (matches the
   map list). */
.lobby-list__row {
  display: grid;
  grid-template-columns: 1fr 1fr auto;
  padding: 10px 14px;
  border-radius: 6px;
  border: 1px solid rgba(198, 158, 90, 0.3);
  background: rgba(0, 0, 0, 0.28);
  cursor: pointer;
  text-align: left;
  transition: background 0.1s, border-color 0.1s, filter 0.1s;
}

.lobby-list__row:hover {
  background: rgba(0, 0, 0, 0.42);
  border-color: rgba(198, 158, 90, 0.55);
  filter: brightness(1.1);
}

.lobby-list__row:focus-visible {
  outline: 2px solid #c9a765;
  outline-offset: 2px;
  border-radius: 6px;
}

.lobby-list__cell {
  font-size: 13px;
  color: #e9dbb8;
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
  color: #e0bd7f;
}

.lobby-list__empty {
  padding: 24px;
  text-align: center;
  color: rgba(233, 219, 184, 0.55);
  font-size: 13px;
}
</style>
