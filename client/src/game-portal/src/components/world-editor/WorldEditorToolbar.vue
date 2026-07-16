<template>
  <div class="we-toolbar" role="toolbar">
    <button
      v-for="c in categories"
      :key="c.id"
      type="button"
      class="we-toolbar__btn"
      :class="{ 'we-toolbar__btn--disabled': !c.enabled, 'we-toolbar__btn--active': activeId === c.id }"
      :disabled="!c.enabled"
      :title="c.enabled ? c.label : c.label + ' — coming soon'"
      @click="c.enabled && emit('select', c.id)"
    >
      {{ c.label }}
    </button>
  </div>
</template>

<script lang="ts">
// Declared in a plain (non-setup) script block so WORLD_EDITOR_CATEGORIES can
// be exported as a named export for testing — `<script setup>` only exposes a
// component's default export, not named bindings. Consts declared here are
// still visible inside `<script setup>` below (standard SFC two-block
// pattern; mirrors MENU_ENTRIES in MainMenu.vue).
export type WorldEditorCategory = { id: string; label: string; enabled: boolean }
// Full vision, with milestone-1 categories enabled and later sub-projects
// visible-but-disabled so the roadmap is discoverable in the UI.
export const WORLD_EDITOR_CATEGORIES: WorldEditorCategory[] = [
  // One entry for the map itself — terrain / obstacles / buildings / units are
  // brush modes inside the Paint section, not separate editors.
  { id: 'map', label: 'Map', enabled: true },
  { id: 'items', label: 'Items', enabled: true },
  { id: 'unit-types', label: 'Units', enabled: true },
  { id: 'perks', label: 'Perks', enabled: true },
  { id: 'abilities', label: 'Abilities', enabled: true },
  { id: 'effects', label: 'Effects', enabled: true },
  { id: 'projectiles', label: 'Projectiles', enabled: true },
  { id: 'campaigns', label: 'Campaigns', enabled: false },
  { id: 'play', label: '▶ Play', enabled: true },
  { id: 'exit', label: 'Exit', enabled: true },
]
</script>

<script setup lang="ts">
defineProps<{ activeId?: string }>()
const emit = defineEmits<{ select: [string] }>()
const categories = WORLD_EDITOR_CATEGORIES
</script>

<style scoped>
.we-toolbar { display: flex; gap: 4px; padding: 6px 8px; flex-wrap: wrap;
  background: rgba(3, 8, 14, 0.92); border-bottom: 1px solid rgba(148,163,184,0.22); }
.we-toolbar__btn { padding: 6px 10px; border-radius: 8px; font-size: 0.78rem;
  color: #f8fafc; background: rgba(25,35,52,0.9); border: 1px solid rgba(148,163,184,0.2); }
.we-toolbar__btn--active { border-color: rgba(215,187,132,0.7); }
.we-toolbar__btn--disabled { opacity: 0.4; cursor: not-allowed; }
</style>
