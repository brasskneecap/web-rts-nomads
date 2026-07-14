<template>
  <button
    class="ui-button"
    :class="[`ui-button--${size}`, { 'ui-button--selected': selected, 'ui-button--disabled': disabled }]"
    :style="{
      '--ui-panel-image': `url(${art.image})`,
      '--ui-panel-slice': String(art.slice),
    }"
    :disabled="disabled"
    :aria-pressed="selected"
    type="button"
    @mouseenter="onMouseEnter"
    @click="onClick"
  >
    <slot />
  </button>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import uiPanelUrl from '@/assets/ui/themes/default/ui_panel.png'
import activeButtonUrl from '@/assets/ui/themes/updated/active-button.png'
import secondaryButtonUrl from '@/assets/ui/themes/updated/secondary-button.png'
import theme from '@/assets/ui/themes/default/theme.json'
import { useSoundHooks } from '@/composables/useSoundHooks'

const props = withDefaults(defineProps<{
  disabled?: boolean
  selected?: boolean
  size?: 'sm' | 'md' | 'lg'
  /**
   * Which button plate to paint.
   * - `default`: the original ui_panel art (every pre-existing call site).
   * - `active`: the blue "primary action" plate (Save, Add New, Gallery).
   * - `secondary`: the dark stone plate for the lesser action beside it
   *   (Reset / Delete).
   */
  variant?: 'default' | 'active' | 'secondary'
}>(), {
  disabled: false,
  selected: false,
  size: 'md',
  variant: 'default',
})

// Slices are the brass frame thickness of each plate; `fill` stretches the
// middle, so the art scales to any label width without distorting the corners.
const BUTTON_ART = {
  default: { image: uiPanelUrl, slice: theme.uiPanel.slice },
  active: { image: activeButtonUrl, slice: 14 },
  secondary: { image: secondaryButtonUrl, slice: 14 },
} as const

const art = computed(() => BUTTON_ART[props.variant])

const emit = defineEmits<{
  click: []
}>()

const { playHover, playClick } = useSoundHooks()

function onMouseEnter() {
  if (!props.disabled) playHover()
}

function onClick() {
  if (!props.disabled) {
    playClick()
    emit('click')
  }
}
</script>

<style scoped>
.ui-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  font-family: var(--font-button);
  font-weight: 600;
  letter-spacing: 0.06em;
  color: #f5ead2;
  text-align: center;
  border: calc(var(--ui-panel-slice) * 1px) solid transparent;
  border-image-source: var(--ui-panel-image);
  border-image-slice: var(--ui-panel-slice) fill;
  border-image-width: calc(var(--ui-panel-slice) * 1px);
  border-image-repeat: round;
  image-rendering: pixelated;
  background: none;
  padding: 0;
  /* No filter transition: animating brightness on a border-image element with
     image-rendering: pixelated causes GPU layer churn that flashes nearby
     icon-container PNG backgrounds in shared compositing groups. */
}

.ui-button--sm {
  min-width: 80px;
  min-height: 36px;
  font-size: 12px;
  padding: 2px 8px;
}

.ui-button--md {
  min-width: 120px;
  min-height: 44px;
  font-size: 14px;
  padding: 4px 16px;
}

.ui-button--lg {
  min-width: 180px;
  min-height: 56px;
  font-size: 16px;
  padding: 8px 24px;
}

.ui-button:hover:not(:disabled):not(.ui-button--disabled) {
  filter: brightness(1.18);
}

.ui-button:active:not(:disabled):not(.ui-button--disabled) {
  filter: brightness(0.88);
}

.ui-button--selected {
  filter: brightness(1.3);
}

.ui-button--disabled,
.ui-button:disabled {
  opacity: 0.4;
  cursor: not-allowed;
  filter: none;
}

.ui-button:focus-visible {
  outline: 2px solid rgba(247, 216, 142, 0.9);
  outline-offset: 3px;
  border-radius: 4px;
}
</style>
