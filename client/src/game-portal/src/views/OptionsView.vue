<template>
  <div class="options-view">
    <div class="options-view__layout">
      <div class="options-view__back-row">
        <UiButton size="sm" @click="router.back()">Back</UiButton>
      </div>

      <MenuPanel class="options-view__panel">
        <header class="options-view__header">
          <h1 class="options-view__title">Options</h1>
        </header>

        <section class="options-section" aria-label="Audio">
          <div class="options-section__title">Audio</div>

          <VolumeSlider
            id="volume-master"
            label="Master"
            :model-value="masterVolume"
            @update:model-value="masterVolume = $event"
          />
          <VolumeSlider
            id="volume-music"
            label="Music"
            :model-value="musicVolume"
            @update:model-value="musicVolume = $event"
          />
          <VolumeSlider
            id="volume-sfx"
            label="Sound Effects"
            :model-value="sfxVolume"
            @update:model-value="sfxVolume = $event"
          />
        </section>
      </MenuPanel>
    </div>
  </div>
</template>


<script setup lang="ts">
import { useRouter } from 'vue-router'
import MenuPanel from '@/components/menu/MenuPanel.vue'
import VolumeSlider from '@/components/menu/VolumeSlider.vue'
import UiButton from '@/components/ui/UiButton.vue'
import { useAudioSettings } from '@/composables/useAudioSettings'

const router = useRouter()
const { masterVolume, musicVolume, sfxVolume } = useAudioSettings()
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

.options-section {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.options-section__title {
  font-size: 14px;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: #d4b87a;
  margin-bottom: 4px;
}
</style>
