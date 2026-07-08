<template>
  <div
    class="match-settings"
    role="dialog"
    aria-modal="true"
    aria-label="Settings"
    :style="{
      '--ui-window-image': `url(${mainWindowPanelUrl})`,
      '--ui-button-image': `url(${buttonUrl})`,
    }"
  >
    <div class="match-settings__panel-wrap">
      <div class="match-settings__panel">
        <button
          type="button"
          class="match-settings__close"
          aria-label="Close settings"
          @click="emit('close')"
        >×</button>

        <header class="match-settings__header">
          <h2 class="match-settings__title">Settings</h2>
        </header>

        <section class="match-settings__section" aria-label="Match">
          <div class="match-settings__section-title">Match</div>
          <div class="match-settings__row">
            <span class="match-settings__label">{{ paused ? 'Game Paused' : 'Pause Game' }}</span>
            <UiButton size="sm" @click="onTogglePause">
              {{ paused ? 'Resume Game' : 'Pause Game' }}
            </UiButton>
          </div>
          <div class="match-settings__row">
            <span class="match-settings__label">Leave this match</span>
            <UiButton size="sm" @click="emit('exit-game')">Exit Game</UiButton>
          </div>
        </section>

        <SettingsPanel />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onBeforeUnmount } from 'vue'
import UiButton from '@/components/ui/UiButton.vue'
import SettingsPanel from '@/components/menu/SettingsPanel.vue'
import mainWindowPanelUrl from '@/assets/ui/themes/updated/main-window-panel.png'
import buttonUrl from '@/assets/ui/themes/updated/button.png'

const props = defineProps<{
  paused: boolean
}>()

const emit = defineEmits<{
  close: []
  'toggle-pause': [next: boolean]
  'exit-game': []
}>()

function onTogglePause() {
  emit('toggle-pause', !props.paused)
}

function onKeydown(e: KeyboardEvent) {
  if (e.code === 'Escape') {
    emit('close')
    e.preventDefault()
    e.stopPropagation()
  }
}

onMounted(() => {
  window.addEventListener('keydown', onKeydown, { capture: true })
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown, { capture: true })
})
</script>

<style scoped>
.match-settings {
  position: absolute;
  inset: 0;
  z-index: 40;
  background: rgba(0, 0, 0, 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
  pointer-events: auto;
}

.match-settings__panel-wrap {
  width: 100%;
  max-width: 520px;
  margin: 24px;
}

.match-settings__panel {
  position: relative;
  width: 100%;
  box-sizing: border-box;
  padding: 18px;
  /* Main-window-panel frame with `fill` (wood interior backs the content).
     Slice keeps the full 44px corner art; rendered at 40px so it fits this
     smaller modal. Detailed art → smooth rendering. */
  border: 40px solid transparent;
  border-image-source: var(--ui-window-image);
  border-image-slice: 44 fill;
  border-image-width: 40px;
  border-image-repeat: round;
  image-rendering: auto;
  box-shadow: 0 18px 48px rgba(0, 0, 0, 0.65);
}

/* All buttons in the settings popup (Pause/Exit + the SettingsPanel's Fullscreen
   toggle) use the button.png art. Only actual UiButtons match — the audio
   sliders aren't affected. Sized to the button aspect so the corner brackets and
   top diamond don't distort. UiButton's own hover/active filters still apply. */
.match-settings :deep(.ui-button) {
  border: 0;
  padding: 0 16px;
  min-width: 168px;
  min-height: 60px;
  background: var(--ui-button-image) center / 100% 100% no-repeat;
  image-rendering: auto;
  color: #f6ecd2;
}

.match-settings__close {
  position: absolute;
  top: 8px;
  right: 8px;
  width: 28px;
  height: 28px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  background: transparent;
  border: 0;
  color: #f5ead2;
  font-size: 22px;
  font-weight: 700;
  line-height: 1;
  cursor: pointer;
  z-index: 1;
}

.match-settings__close:hover {
  color: #fff;
  filter: brightness(1.15);
}

.match-settings__close:focus-visible {
  outline: 2px solid rgba(247, 216, 142, 0.9);
  outline-offset: 2px;
  border-radius: 4px;
}

.match-settings__header {
  display: flex;
  align-items: center;
  padding-bottom: 12px;
  margin-bottom: 16px;
  border-bottom: 1px solid rgba(212, 168, 79, 0.35);
}

.match-settings__title {
  margin: 0;
  font-size: 22px;
  font-weight: 700;
  color: #f5ead2;
  letter-spacing: 0.04em;
}

.match-settings__section {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-bottom: 20px;
}

.match-settings__section-title {
  font-size: 14px;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: #d4b87a;
}

.match-settings__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.match-settings__label {
  font-size: 14px;
  font-weight: 600;
  color: #f5ead2;
  letter-spacing: 0.04em;
}
</style>
