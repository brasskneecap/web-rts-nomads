<template>
  <div class="settings-panel">
    <section class="settings-panel__section" aria-label="Display">
      <div class="settings-panel__section-title">Display</div>
      <div class="settings-panel__row">
        <span class="settings-panel__label">Fullscreen</span>
        <UiButton
          size="sm"
          :disabled="!isFullscreenSupported"
          @click="toggleFullscreen"
        >
          {{ isFullscreen ? 'Exit Fullscreen' : 'Enter Fullscreen' }}
        </UiButton>
      </div>
      <div v-if="!isFullscreenSupported" class="settings-panel__hint">
        Fullscreen is not available in this browser.
      </div>
    </section>

    <section class="settings-panel__section" aria-label="Audio">
      <div class="settings-panel__section-title">Audio</div>

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
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import VolumeSlider from '@/components/menu/VolumeSlider.vue'
import UiButton from '@/components/ui/UiButton.vue'
import { useAudioSettings } from '@/composables/useAudioSettings'
import {
  isInTauri,
  setFullscreen as tauriSetFullscreen,
  isFullscreen as tauriIsFullscreen,
} from '@/services/desktopBridge'

const { masterVolume, musicVolume, sfxVolume } = useAudioSettings()

// Fullscreen handling splits two ways:
//   - Tauri (Steam build): uses the native window API via desktopBridge so
//     we get real OS fullscreen, not webview-fullscreen. State is queried
//     on mount; the `fullscreenchange` DOM event does NOT fire for the
//     native toggle, so we update locally after each click.
//   - Browser dev / web build: uses the standard Fullscreen API and listens
//     to `fullscreenchange` so external toggles (F11, Esc) stay in sync.
const inTauri = isInTauri()
const isFullscreenSupported =
  inTauri || (typeof document !== 'undefined' && document.fullscreenEnabled)
const isFullscreen = ref(false)

function syncBrowserFullscreen() {
  isFullscreen.value = !!document.fullscreenElement
}

async function toggleFullscreen() {
  try {
    if (inTauri) {
      const next = !isFullscreen.value
      const ok = await tauriSetFullscreen(next)
      if (ok) {
        isFullscreen.value = next
      } else {
        console.warn(
          'Tauri set_fullscreen returned false; the desktop shell may need a rebuild ' +
          '(cargo tauri dev / packaging step) to register the new command.',
        )
        isFullscreen.value = await tauriIsFullscreen()
      }
      return
    }
    if (document.fullscreenElement) {
      await document.exitFullscreen()
    } else {
      await document.documentElement.requestFullscreen()
    }
  } catch (e) {
    console.warn('toggleFullscreen failed:', e)
  }
}

onMounted(async () => {
  if (inTauri) {
    isFullscreen.value = await tauriIsFullscreen()
  } else {
    isFullscreen.value = !!document.fullscreenElement
    document.addEventListener('fullscreenchange', syncBrowserFullscreen)
  }
})

onBeforeUnmount(() => {
  if (!inTauri) {
    document.removeEventListener('fullscreenchange', syncBrowserFullscreen)
  }
})
</script>

<style scoped>
.settings-panel {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.settings-panel__section {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.settings-panel__section-title {
  font-size: 14px;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: #d4b87a;
  margin-bottom: 4px;
}

.settings-panel__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.settings-panel__label {
  font-size: 14px;
  font-weight: 600;
  color: #f5ead2;
  letter-spacing: 0.04em;
}

.settings-panel__hint {
  font-size: 11px;
  color: #a89060;
  font-style: italic;
}
</style>
