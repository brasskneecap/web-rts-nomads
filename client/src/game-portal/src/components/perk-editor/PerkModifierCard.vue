<template>
  <div
    class="pm-card"
    :class="{ 'pm-card--on': selected }"
    :style="{ '--mk-accent': entry.meta.accent }"
    data-test="perk-modifier-card"
    :data-kind="entry.kind"
    role="button"
    tabindex="0"
    @click="$emit('select')"
    @keydown.enter.prevent="$emit('select')"
    @keydown.space.prevent="$emit('select')"
  >
    <span class="pm-card__accent" aria-hidden="true" />
    <span class="pm-card__icon" aria-hidden="true">{{ entry.meta.icon }}</span>
    <span class="pm-card__body">
      <span class="pm-card__type">{{ entry.meta.label }}</span>
      <span class="pm-card__summary">{{ entry.summary }}</span>
    </span>
    <span class="pm-card__actions">
      <button type="button" class="pm-card__act" title="Duplicate" aria-label="Duplicate modifier" @click.stop="$emit('duplicate')">⧉</button>
      <button type="button" class="pm-card__act" title="Delete" aria-label="Delete modifier" @click.stop="$emit('delete')">✕</button>
    </span>
  </div>
</template>

<script setup lang="ts">
import type { ModifierEntry } from './perkModifierModel'
defineProps<{ entry: ModifierEntry; selected: boolean }>()
defineEmits<{ select: []; duplicate: []; delete: [] }>()
</script>

<style scoped>
/* Dark-steel card, thin bronze border, per-kind accent bar, soft inner shadow —
   the forge-theme .ed-card look, applied by hand here since a modifier card is
   a bespoke compact row rather than a titled SectionCard. */
.pm-card {
  position: relative;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px 8px 14px;
  background: #0b0906;
  border: 1px solid rgba(226, 182, 92, 0.34);
  border-radius: var(--ed-radius);
  box-shadow: inset 0 1px 0 rgba(242, 208, 144, 0.06);
  min-width: 0;
}
.pm-card--on {
  border-color: var(--ed-brass);
  box-shadow: 0 0 0 1px rgba(212, 168, 71, 0.35), inset 0 1px 0 rgba(242, 208, 144, 0.06);
}
.pm-card__accent {
  position: absolute;
  left: 0; top: 4px; bottom: 4px;
  width: 3px;
  border-radius: 3px;
  background: var(--mk-accent);
}
.pm-card__icon {
  flex: 0 0 auto;
  width: 20px;
  text-align: center;
  color: var(--mk-accent);
  font-size: 0.95rem;
}
.pm-card__body { display: flex; flex-direction: column; gap: 1px; min-width: 0; flex: 1 1 auto; }
.pm-card__type {
  font-size: 0.62rem;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--mk-accent);
}
.pm-card__summary {
  font-size: 0.82rem;
  color: var(--ed-text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.pm-card__actions { display: flex; gap: 2px; opacity: 0; flex: 0 0 auto; }
.pm-card:hover .pm-card__actions,
.pm-card--on .pm-card__actions { opacity: 1; }
.pm-card__act {
  padding: 2px 6px;
  font-size: 0.76rem;
  line-height: 1;
  color: var(--ed-text-dim);
  background: none;
  border: 1px solid transparent;
  border-radius: 4px;
}
.pm-card__act:hover { color: var(--ed-brass); border-color: var(--ed-line); }
</style>
