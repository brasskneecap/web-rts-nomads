<template>
  <div class="options-view">
    <div class="options-view__layout">
      <div class="options-view__back-row">
        <UiButton size="sm" @click="router.back()">Back</UiButton>
      </div>

      <MenuPanel class="options-view__panel">
        <header class="options-view__header">
          <h1 class="options-view__title">Settings</h1>
        </header>

        <SettingsPanel />
      </MenuPanel>
    </div>
  </div>
</template>


<script setup lang="ts">
import { useRouter } from 'vue-router'
import MenuPanel from '@/components/menu/MenuPanel.vue'
import SettingsPanel from '@/components/menu/SettingsPanel.vue'
import UiButton from '@/components/ui/UiButton.vue'

const router = useRouter()
</script>

<style scoped>
.options-view {
  position: relative;
  z-index: 1;
  width: 100%;
  height: 100%;
  overflow: auto;
}

.options-view__layout {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 24px;
  padding: 48px;
}

.options-view__back-row {
  width: 100%;
  max-width: 520px;
  display: flex;
  justify-content: flex-start;
}

.options-view__header {
  display: flex;
  align-items: center;
  gap: 16px;
  padding-bottom: 12px;
  margin-bottom: 4px;
  border-bottom: 1px solid rgba(212, 168, 79, 0.35);
}

.options-view__title {
  margin: 0;
  font-size: 28px;
  font-weight: 700;
  color: #f5ead2;
  letter-spacing: 0.04em;
}

.options-view__panel {
  width: 100%;
  max-width: 520px;
}
</style>
