<template>
  <div v-if="objectives.length" class="match-objectives" role="status" aria-live="polite">
    <div class="match-objectives__header">Objectives</div>
    <ul class="match-objectives__list">
      <li
        v-for="obj in objectives"
        :key="obj.id"
        class="match-objective"
        :class="{
          'match-objective--completed': obj.completed,
          'match-objective--failed': !!obj.failed,
          'match-objective--required': !!obj.required,
        }"
      >
        <span
          class="match-objective__icon"
          aria-hidden="true"
        >{{ iconFor(obj) }}</span>
        <span class="match-objective__label">
          {{ obj.description || obj.id }}
        </span>
        <span class="match-objective__progress">
          {{ obj.current }} / {{ obj.requiredCount }}
        </span>
      </li>
    </ul>
  </div>
</template>

<script setup lang="ts">
import type { ObjectiveSnapshot } from '@/game/network/protocol'

const props = defineProps<{
  /** Live objective list from the snapshot. The parent component is
   *  responsible for hiding the panel for non-campaign matches; an empty
   *  array here just renders nothing so the wrapper stays simple. */
  objectives: ObjectiveSnapshot[]
}>()

/** Pick the leading icon per state. Failed (✗) wins over completed (✓);
 *  in-progress shows an empty box. Mirrors the icon set used by the
 *  Campaign.vue level-select rows for visual consistency. */
function iconFor(obj: ObjectiveSnapshot): string {
  if (obj.failed) return '✗'
  if (obj.completed) return '✓'
  return '□'
}

// Silence unused-prop warning on minimal templates (props is used in template binding).
void props
</script>

<style scoped>
/* Top-right placement under the resource tray. Position is set by the
   parent (Match.vue) — this component just lays out its own contents.
   The wrapper styling matches the parchment-on-tabletop aesthetic of
   the war-room HUD: warm sepia tones, soft shadow, no hard borders. */
.match-objectives {
  width: 280px;
  max-width: 90vw;
  pointer-events: auto;
  font-family: var(--font-title);
  color: #f4d27a;
  background: rgba(28, 18, 8, 0.78);
  border: 1px solid rgba(212, 168, 71, 0.45);
  border-radius: 4px;
  padding: 10px 12px;
  box-shadow:
    0 2px 6px rgba(0, 0, 0, 0.55),
    0 0 0 1px rgba(0, 0, 0, 0.25) inset;
}

.match-objectives__header {
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: #d7bb84;
  margin-bottom: 6px;
}

.match-objectives__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.match-objective {
  display: grid;
  grid-template-columns: 16px 1fr auto;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  font-family: var(--font-body);
  line-height: 1.3;
  color: #f4d27a;
}

.match-objective__icon {
  text-align: center;
  font-family: var(--font-title);
  font-size: 14px;
  font-weight: 700;
  color: rgba(244, 210, 122, 0.75);
}

.match-objective--completed .match-objective__icon {
  color: #c5e08a;
}

.match-objective--failed .match-objective__icon {
  color: #f08070;
}

.match-objective__label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.match-objective--required .match-objective__label {
  font-weight: 700;
}

/* Strikethrough on the label conveys failure status. Optional vs required
   is irrelevant here — both render with strikethrough per the spec. */
.match-objective--failed .match-objective__label {
  text-decoration: line-through;
  color: rgba(244, 210, 122, 0.55);
}

.match-objective__progress {
  font-size: 11px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  color: rgba(244, 210, 122, 0.85);
}

.match-objective--completed .match-objective__progress {
  color: #c5e08a;
}

.match-objective--failed .match-objective__progress {
  color: rgba(240, 128, 112, 0.65);
  text-decoration: line-through;
}
</style>
