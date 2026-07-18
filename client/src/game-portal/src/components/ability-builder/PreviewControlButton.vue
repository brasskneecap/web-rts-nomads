<template>
  <!-- A single square icon button shared by every preview control (Run, Edit,
       Play/Pause, Restart) so they read as one toolbar regardless of which
       component renders them. `data-test` / native click fall through to this
       root button; `disabled`/`icon`/`label`/`active` are consumed props. -->
  <button
    type="button"
    class="pv-ctl"
    :class="{ 'pv-ctl--active': active }"
    :disabled="disabled"
    :aria-label="label"
    :aria-pressed="active ? true : undefined"
    :title="label"
  >
    <svg class="pv-ctl__icon" viewBox="0 0 24 24" aria-hidden="true">
      <path v-if="icon === 'play'" d="M8 5v14l11-7z" />
      <template v-else-if="icon === 'pause'">
        <rect x="6" y="5" width="4" height="14" rx="1" />
        <rect x="14" y="5" width="4" height="14" rx="1" />
      </template>
      <path
        v-else-if="icon === 'restart'"
        d="M17.65 6.35A8 8 0 1 0 19.73 14h-2.08A6 6 0 1 1 16.24 7.76L13 11h7V4l-2.35 2.35z"
      />
      <path
        v-else-if="icon === 'edit'"
        d="M3 17.25V21h3.75L17.81 9.94l-3.75-3.75L3 17.25zM20.71 7.04a1 1 0 0 0 0-1.41l-2.34-2.34a1 1 0 0 0-1.41 0l-1.83 1.83 3.75 3.75 1.83-1.83z"
      />
      <path v-else-if="icon === 'run'" d="M7 2v11h3v9l7-12h-4l4-8z" />
    </svg>
  </button>
</template>

<script setup lang="ts">
export type ControlIcon = 'play' | 'pause' | 'restart' | 'edit' | 'run'

defineProps<{
  icon: ControlIcon
  /** Accessible name + tooltip (icon-only buttons need this). */
  label: string
  disabled?: boolean
  /** Brass "on" styling (e.g. the active speed / a toggled state). */
  active?: boolean
}>()
</script>

<style scoped>
.pv-ctl {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  padding: 0;
  color: var(--ed-text);
  background: rgba(15, 23, 42, 0.35);
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}

.pv-ctl:hover:not(:disabled) {
  color: var(--ed-brass);
  border-color: var(--ed-line-strong);
}

.pv-ctl:disabled {
  opacity: 0.55;
}

.pv-ctl--active {
  color: var(--ed-brass);
  border-color: var(--ed-line-strong);
  background: rgba(212, 168, 71, 0.14);
}

.pv-ctl__icon {
  width: 16px;
  height: 16px;
  fill: currentColor;
}
</style>
