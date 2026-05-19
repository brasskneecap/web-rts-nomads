<template>
  <div class="main-menu">
    <SteamStatusBadge class="main-menu__steam-badge" />

    <ResumeSessionCard
      v-if="hasResumableSession"
      :map-name="resumeMapName"
      @resume="onResume"
      @dismiss="onDismiss"
    />

    <div class="main-menu__layout">
      <div class="menu-logo-slot" aria-hidden="true"></div>

      <nav class="main-menu__nav" aria-label="Main menu">
        <MenuPanel>
          <UiButton size="lg" :disabled="true">Campaign</UiButton>
          <UiButton size="lg" @click="router.push('/custom')">Custom Game</UiButton>
          <UiButton size="lg" @click="router.push('/profile')">Profile</UiButton>
          <UiButton size="lg" :disabled="true">Upgrades</UiButton>
          <UiButton size="lg" @click="router.push('/editor')">Map Editor</UiButton>
        </MenuPanel>
      </nav>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useMapSelection } from '@/composables/useMapSelection'
import MenuPanel from '@/components/menu/MenuPanel.vue'
import ResumeSessionCard from '@/components/menu/ResumeSessionCard.vue'
import UiButton from '@/components/ui/UiButton.vue'
import SteamStatusBadge from '@/components/SteamStatusBadge.vue'

const HAS_ACTIVE_SESSION_KEY = 'webrts.hasActiveSession'
const MATCH_ID_STORAGE_KEY = 'webrts.matchId'
const MAP_ID_STORAGE_KEY = 'webrts.mapId'

const router = useRouter()
const { selectedMapName, selectedMapId } = useMapSelection()

const hasResumableSession = ref(false)

onMounted(() => {
  hasResumableSession.value =
    localStorage.getItem(HAS_ACTIVE_SESSION_KEY) === 'true' &&
    !!localStorage.getItem(MATCH_ID_STORAGE_KEY)
})

const resumeMapName = computed(() => {
  if (selectedMapName.value) return selectedMapName.value
  if (selectedMapId.value) return selectedMapId.value
  const rawMapId = localStorage.getItem(MAP_ID_STORAGE_KEY)
  if (rawMapId) return rawMapId
  return 'Unknown Map'
})

function onResume() {
  const matchId = localStorage.getItem(MATCH_ID_STORAGE_KEY)
  if (matchId) {
    void router.push(`/match/${matchId}`)
    return
  }
  onDismiss()
}

function onDismiss() {
  localStorage.removeItem(HAS_ACTIVE_SESSION_KEY)
  localStorage.removeItem(MATCH_ID_STORAGE_KEY)
  localStorage.removeItem(MAP_ID_STORAGE_KEY)
  hasResumableSession.value = false
}
</script>

<style scoped>
.main-menu {
  position: relative;
  z-index: 1;
  width: 100%;
  height: 100%;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  background: radial-gradient(circle at top, rgba(36, 55, 87, 0.35), transparent 48%);
}

.main-menu__steam-badge {
  position: absolute;
  top: 16px;
  right: 20px;
  z-index: 2;
}

.main-menu__layout {
  position: relative;
  z-index: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  padding: 48px;
  gap: 32px;
}

.menu-logo-slot {
  height: 64px;
}

.main-menu__nav {
  display: flex;
}

.main-menu__nav :deep(.menu-panel) {
  gap: 12px;
  min-width: 330px;
}

.main-menu__nav :deep(.ui-button--lg) {
  min-width: 270px;
  min-height: 84px;
  font-size: 24px;
  padding: 12px 36px;
}
</style>
