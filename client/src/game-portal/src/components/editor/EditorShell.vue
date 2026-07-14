<template>
  <!-- worldMenu = the black/brass outer frame used by the meta screens
       (advancements, custom game), so an editor reads as part of the same
       world rather than a bolted-on tool. -->
  <UiPanel variant="worldMenu" :padding="0" class="ed-shell">
    <div class="ed-shell__grid" :class="{ 'ed-shell__grid--no-rail': !$slots.rail }">
      <aside class="ed-shell__sidebar">
        <slot name="sidebar" />
      </aside>

      <main class="ed-shell__main">
        <slot name="main" />
      </main>

      <aside v-if="$slots.rail" class="ed-shell__rail">
        <slot name="rail" />
      </aside>
    </div>
  </UiPanel>
</template>

<script setup lang="ts">
import UiPanel from '@/components/ui/UiPanel.vue'
// Form chrome for every control rendered inside .ed-shell. Imported here (not
// in each field component) so a single import covers the whole editor.
import './editor-controls.css'
</script>

<style scoped>
.ed-shell {
  width: 100%;
  height: 100%;
  min-height: 0;
  min-width: 0;
}

/* Sidebar | main | rail. The rail is optional — an editor that doesn't need a
   preview collapses to two columns with no layout change of its own. */
.ed-shell__grid {
  display: grid;
  grid-template-columns: minmax(220px, 260px) minmax(0, 1fr) minmax(280px, 340px);
  gap: var(--ed-gap);
  padding: var(--ed-gap);
  width: 100%;
  height: 100%;
  min-height: 0;
  box-sizing: border-box;
}

.ed-shell__grid--no-rail {
  grid-template-columns: minmax(220px, 260px) minmax(0, 1fr);
}

.ed-shell__sidebar,
.ed-shell__rail {
  min-height: 0;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.ed-shell__main {
  min-height: 0;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: var(--ed-gap);
  overflow: hidden;
}

/* Below ~1200px the rail is the first thing to go — the form is the work
   surface, the preview is a companion. */
@media (max-width: 1200px) {
  .ed-shell__grid,
  .ed-shell__grid--no-rail {
    grid-template-columns: minmax(200px, 240px) minmax(0, 1fr);
  }

  .ed-shell__rail {
    display: none;
  }
}
</style>
