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

        <section class="options-section" aria-label="Display">
          <div class="options-section__title">Display</div>
          <div class="options-row">
            <span class="options-row__label">Fullscreen</span>
            <UiButton
              size="sm"
              :disabled="!isFullscreenSupported"
              @click="toggleFullscreen"
            >
              {{ isFullscreen ? 'Exit Fullscreen' : 'Enter Fullscreen' }}
            </UiButton>
          </div>
          <div v-if="!isFullscreenSupported" class="options-row__hint">
            Fullscreen is not available in this browser.
          </div>
        </section>

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
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import MenuPanel from '@/components/menu/MenuPanel.vue'
import VolumeSlider from '@/components/menu/VolumeSlider.vue'
import UiButton from '@/components/ui/UiButton.vue'
import { useAudioSettings } from '@/composables/useAudioSettings'
import {
  isInTauri,
  setFullscreen as tauriSetFullscreen,
  isFullscreen as tauriIsFullscreen,
} from '@/services/desktopBridge'

const router = useRouter()
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
        // Invoke failed — most often because the running shell predates
        // the set_fullscreen command (rebuild required). Re-query so the
        // label reflects reality instead of an optimistic toggle.
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

.options-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.options-row__label {
  font-size: 14px;
  font-weight: 600;
  color: #f5ead2;
  letter-spacing: 0.04em;
}

.options-row__hint {
  font-size: 11px;
  color: #a89060;
  font-style: italic;
}
</style>
