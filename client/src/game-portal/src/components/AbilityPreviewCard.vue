<template>
  <!-- The ability editor's PREVIEW card. It renders the same prose the in-game
       action-bar tooltip shows (the description resolved server-side — see
       AbilityDef.EffectiveDescription), but is deliberately its own
       presentation, not the tooltip component. Parchment, so it reads as the
       ability's own card; text is INK for contrast on tan (mirrors
       ItemPreviewCard). -->
  <UiPanel variant="worldInner" :padding="0" repeat="stretch" class="apc">
    <div class="apc__inner">
      <div class="apc__name">{{ name || 'Untitled' }}</div>
      <div v-if="subtitle" class="apc__subtitle">{{ subtitle }}</div>

      <hr class="apc__rule" />

      <p v-if="description" class="apc__desc">{{ description }}</p>
      <p v-else class="apc__empty">No description yet.</p>

      <template v-if="lines.length">
        <hr class="apc__rule" />
        <ul class="apc__lines">
          <li v-for="(line, i) in lines" :key="i">{{ line }}</li>
        </ul>
      </template>
    </div>
  </UiPanel>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import UiPanel from '@/components/ui/UiPanel.vue'

const props = defineProps<{
  /** Player-facing ability name. */
  name: string
  /** The resolved tooltip prose (author override or server-generated). */
  description: string
  /** Stat readouts (mana / cooldown / range / …) — plain field values, one per
   *  line. These are simple readouts, NOT the generated prose. */
  lines: string[]
  /** Ability type label ("Spell" / "Passive"), shown in the subtitle. */
  typeLabel?: string
  /** Damage school / element ("fire", "holy", …), shown in the subtitle. */
  school?: string
}>()

// A single subtitle line combines the type and school when present, e.g.
// "Spell · fire" — either half may be absent.
const subtitle = computed(() => {
  return [props.typeLabel, props.school].filter(Boolean).join(' · ')
})
</script>

<style scoped>
.apc {
  min-width: 0;
  /* Parchment fill is stretched, so smooth it (UiPanel's pixelated rendering
     would block up the scaled texture). Matches ItemPreviewCard. */
  image-rendering: auto;
}

/* Ink on parchment — the panel art is the background; every color below is
   chosen for contrast on tan (~#b09a72), not the editor's gold/white. */
.apc__inner {
  padding: 14px 16px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  color: #24160a;
}

.apc__name {
  font-family: var(--font-title);
  font-size: 1.25rem;
  font-weight: 800;
  letter-spacing: 0.02em;
  color: #17100a;
  line-height: 1.2;
}

.apc__subtitle {
  font-family: var(--font-unit);
  font-size: 0.74rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #2f1e04;
}

.apc__rule {
  width: 100%;
  height: 1px;
  margin: 4px 0;
  border: 0;
  background: rgba(36, 22, 10, 0.28);
}

.apc__desc {
  margin: 0;
  font-size: 0.9rem;
  font-weight: 600;
  line-height: 1.4;
  white-space: pre-line;
}

.apc__empty {
  margin: 0;
  font-size: 0.82rem;
  font-style: italic;
  color: rgba(36, 22, 10, 0.62);
}

.apc__lines {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.apc__lines li {
  font-size: 0.82rem;
  font-weight: 600;
  line-height: 1.35;
}
</style>
