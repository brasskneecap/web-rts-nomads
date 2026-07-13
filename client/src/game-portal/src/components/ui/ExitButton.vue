<template>
  <button
    type="button"
    class="exit-button"
    :style="{
      '--ui-panel-image': `url(${panelUrl})`,
      '--ui-panel-slice': String(theme.warRoomInnerPanel.slice),
    }"
    :aria-label="ariaLabel ?? `Return to ${destination}`"
    @click="$emit('click', $event)"
  >
    <span class="exit-button__title">Return</span>
    <span class="exit-button__destination">to {{ destination }}</span>
  </button>
</template>

<script setup lang="ts">
import panelUrl from '@/assets/ui/themes/updated/war-room/war-room-inner-panel.png'
import theme from '@/assets/ui/themes/default/theme.json'

withDefaults(
  defineProps<{
    /** Where the button sends the player, e.g. "Kingdom", "War Room", "Main Menu". */
    destination?: string
    ariaLabel?: string
  }>(),
  { destination: 'Main Menu', ariaLabel: undefined },
)

defineEmits<{ click: [MouseEvent] }>()
</script>

<style scoped>
.exit-button {
  display: inline-flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 1px;
  min-width: 170px;
  padding: 0 4px;
  border: calc(var(--ui-panel-slice) * 1px) solid transparent;
  border-image-source: var(--ui-panel-image);
  border-image-slice: var(--ui-panel-slice) fill;
  border-image-width: calc(var(--ui-panel-slice) * 1px);
  border-image-repeat: round;
  image-rendering: pixelated;
  background: none;
  text-align: center;
}

.exit-button__title {
  font-family: var(--font-title);
  font-size: 18px;
  font-weight: 700;
  letter-spacing: 0.06em;
  line-height: 1.1;
  text-transform: uppercase;
  white-space: nowrap;
  color: #f4d27a;
  text-shadow: 0 1px 3px rgba(0, 0, 0, 0.9);
}

.exit-button__destination {
  font-family: var(--font-body, var(--font-title));
  font-size: 11px;
  font-weight: 500;
  letter-spacing: 0.06em;
  line-height: 1.2;
  text-transform: uppercase;
  white-space: nowrap;
  color: #d8c8a2;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.9);
}

.exit-button:hover,
.exit-button:focus-visible {
  filter: brightness(1.18);
  outline: none;
}

.exit-button:active {
  filter: brightness(0.88);
}

.exit-button:focus-visible {
  outline: 2px solid rgba(247, 216, 142, 0.9);
  outline-offset: 3px;
}
</style>
