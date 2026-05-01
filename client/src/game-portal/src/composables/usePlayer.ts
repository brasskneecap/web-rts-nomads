import { computed, ref } from 'vue'

const PLAYER_ID_STORAGE_KEY = 'webrts.playerId'

function getOrCreatePlayerId(): string {
  const existing = localStorage.getItem(PLAYER_ID_STORAGE_KEY)
  if (existing) return existing
  const created = `player-${Math.random().toString(36).slice(2, 8)}`
  localStorage.setItem(PLAYER_ID_STORAGE_KEY, created)
  return created
}

const playerId = ref(getOrCreatePlayerId())

export function usePlayer() {
  const displayName = computed(() => playerId.value)
  return { playerId, displayName }
}
