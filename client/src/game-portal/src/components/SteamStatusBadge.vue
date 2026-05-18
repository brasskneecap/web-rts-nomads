<template>
  <div v-if="status === 'loading'" class="steam-badge steam-badge--loading">
    <span class="steam-badge__dot" />
    Checking Steam…
  </div>
  <div v-else-if="player" class="steam-badge steam-badge--online">
    <span class="steam-badge__dot" />
    Signed in as <strong>{{ player.personaName }}</strong>
  </div>
  <div v-else class="steam-badge steam-badge--offline" :title="offlineTooltip">
    <span class="steam-badge__dot" />
    Steam unavailable
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { getSteamPlayer, type LocalSteamPlayer } from '@/services/desktopBridge'

type Status = 'loading' | 'ready'

const status = ref<Status>('loading')
const player = ref<LocalSteamPlayer | null>(null)

const offlineTooltip =
  'Steam features are off because either (a) you launched outside the Tauri shell, ' +
  '(b) the build wasn\'t compiled with --features steam, or (c) Steam isn\'t running. ' +
  'Direct Connect MP still works.'

onMounted(async () => {
  try {
    player.value = await getSteamPlayer()
  } catch {
    player.value = null
  } finally {
    status.value = 'ready'
  }
})
</script>

<style scoped>
.steam-badge {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  border-radius: 4px;
  font-size: 12px;
  letter-spacing: 0.04em;
  color: #f5ead2;
  background: rgba(0, 0, 0, 0.35);
  border: 1px solid rgba(245, 234, 210, 0.18);
  user-select: none;
}

.steam-badge__dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #888;
}

.steam-badge--loading .steam-badge__dot {
  background: #cdb464;
}

.steam-badge--online .steam-badge__dot {
  background: #6dbf6d;
  box-shadow: 0 0 4px rgba(109, 191, 109, 0.6);
}

.steam-badge--offline {
  cursor: help;
}

.steam-badge--offline .steam-badge__dot {
  background: #888;
}
</style>
