<template>
  <div class="lobby-player-list">
    <div
      v-for="i in maxPlayers"
      :key="i"
      class="lobby-player-list__slot"
      :class="{ 'lobby-player-list__slot--filled': players[i - 1] }"
    >
      <template v-if="players[i - 1]">
        <span class="lobby-player-list__id">{{ players[i - 1] }}</span>
        <span v-if="players[i - 1] === hostPlayerId" class="lobby-player-list__tag">(host)</span>
      </template>
      <span v-else class="lobby-player-list__empty">— empty —</span>
    </div>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  players: string[]
  hostPlayerId: string
  maxPlayers: number
}>()
</script>

<style scoped>
.lobby-player-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.lobby-player-list__slot {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  border-radius: 6px;
  border: 1px solid rgba(200, 164, 106, 0.16);
  background: rgba(255, 255, 255, 0.03);
  min-height: 40px;
}

.lobby-player-list__slot--filled {
  border-color: rgba(200, 164, 106, 0.3);
  background: rgba(200, 164, 106, 0.08);
}

.lobby-player-list__id {
  font-size: 13px;
  font-weight: 600;
  color: #f5ead2;
}

.lobby-player-list__tag {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
  color: #f7d88e;
  text-transform: uppercase;
}

.lobby-player-list__empty {
  font-size: 12px;
  color: rgba(203, 184, 147, 0.35);
}
</style>
