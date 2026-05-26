<template>
  <div
    class="match-settings"
    role="dialog"
    aria-modal="true"
    aria-label="Settings"
    @click.self="onBackdropClick"
  >
    <div class="match-settings__panel-wrap">
      <UiPanel class="match-settings__panel" :padding="28">
        <button
          type="button"
          class="match-settings__close"
          aria-label="Close settings"
          @click="emit('close')"
        >×</button>

        <header class="match-settings__header">
          <h2 class="match-settings__title">Settings</h2>
        </header>

        <SettingsPanel />
      </UiPanel>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onBeforeUnmount } from 'vue'
import UiPanel from '@/components/ui/UiPanel.vue'
import SettingsPanel from '@/components/menu/SettingsPanel.vue'

const emit = defineEmits<{
  close: []
}>()

function onBackdropClick() {
  emit('close')
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
</style>
