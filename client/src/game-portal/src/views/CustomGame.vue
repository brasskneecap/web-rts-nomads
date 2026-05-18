<template>
  <div class="custom-game">
    <div class="custom-game__layout">
      <header class="custom-game__header">
        <UiButton size="sm" @click="router.push('/')">Back</UiButton>
        <h1 class="custom-game__title">Custom Game</h1>
      </header>

      <div class="custom-game__cards">
        <div class="custom-game__card">
          <UiPanel :padding="32" class="custom-game__card-panel">
            <div class="custom-game__card-label">Start Game</div>
            <div class="custom-game__card-desc">Create a new lobby and invite friends.</div>
            <UiButton size="lg" @click="router.push('/create-game')">Start Game</UiButton>
          </UiPanel>
        </div>

        <div class="custom-game__card">
          <UiPanel :padding="32" class="custom-game__card-panel">
            <div class="custom-game__card-label">Find Game</div>
            <div class="custom-game__card-desc">Browse and join open lobbies.</div>
            <UiButton size="lg" @click="router.push('/find-game')">Find Game</UiButton>
          </UiPanel>
        </div>

        <div class="custom-game__card">
          <UiPanel :padding="32" class="custom-game__card-panel">
            <div class="custom-game__card-label">Direct Connect</div>
            <div class="custom-game__card-desc">Host LAN/Internet games or join a friend's address.</div>
            <UiButton size="lg" @click="router.push('/direct-connect')">Direct Connect</UiButton>
          </UiPanel>
        </div>

        <div class="custom-game__card">
          <UiPanel :padding="32" class="custom-game__card-panel">
            <div class="custom-game__card-label">Steam Multiplayer</div>
            <div class="custom-game__card-desc">Friends-only lobbies via Steam invites.</div>
            <UiButton size="lg" @click="router.push('/steam-mp')">Steam Multiplayer</UiButton>
          </UiPanel>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import UiPanel from '@/components/ui/UiPanel.vue'
import UiButton from '@/components/ui/UiButton.vue'

const router = useRouter()
</script>

<style scoped>
.custom-game {
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

.custom-game__layout {
  display: flex;
  flex-direction: column;
  gap: 32px;
  max-width: 800px;
  width: 100%;
}

.custom-game__header {
  display: flex;
  align-items: center;
  gap: 20px;
}

.custom-game__title {
  font-size: 24px;
  font-weight: 700;
  color: #f5ead2;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  margin: 0;
}

.custom-game__cards {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 20px;
}

.custom-game__card-panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
  align-items: flex-start;
}

.custom-game__card-label {
  font-size: 20px;
  font-weight: 700;
  color: #f7d88e;
}

.custom-game__card-desc {
  font-size: 13px;
  color: #cbb893;
  line-height: 1.5;
}
</style>
