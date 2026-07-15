<template>
  <!-- The editors' first tab strip. Lifted from MatchMenu's tablist so the
       accessible semantics (role=tab / aria-selected / aria-controls) are
       written once rather than re-derived per editor. Arrow keys move between
       tabs, which is what a tablist is expected to do and what a row of plain
       buttons would not. -->
  <div class="ed-tabs" role="tablist" :aria-label="label">
    <button
      v-for="(tab, i) in tabs"
      :key="tab.id"
      ref="tabButtons"
      type="button"
      class="ed-tabs__tab"
      :class="{ 'ed-tabs__tab--active': tab.id === modelValue }"
      role="tab"
      :aria-selected="tab.id === modelValue"
      :aria-controls="`${idPrefix}-panel-${tab.id}`"
      :tabindex="tab.id === modelValue ? 0 : -1"
      @click="select(tab.id)"
      @keydown.right.prevent="move(i, 1)"
      @keydown.left.prevent="move(i, -1)"
      @keydown.home.prevent="move(0, 0)"
      @keydown.end.prevent="move(tabs.length - 1, 0)"
    >
      {{ tab.label }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'

export type EditorTab = { id: string; label: string }

const props = defineProps<{
  tabs: EditorTab[]
  /** The active tab id (v-model). */
  modelValue: string
  /** Prefixes the aria-controls id so several tablists can coexist on a page. */
  idPrefix: string
  /** Accessible name for the tablist itself, e.g. "Item catalog sections". */
  label: string
}>()

const emit = defineEmits<{ 'update:modelValue': [string] }>()

const tabButtons = ref<HTMLButtonElement[]>([])

function select(id: string) {
  emit('update:modelValue', id)
}

// Arrow keys wrap around, and move focus with the selection so the keyboard
// user lands where they just navigated to.
function move(from: number, delta: number) {
  const next = (from + delta + props.tabs.length) % props.tabs.length
  select(props.tabs[next].id)
  tabButtons.value[next]?.focus()
}
</script>

<style scoped>
.ed-tabs {
  display: flex;
  gap: 4px;
  flex-shrink: 0;
}

.ed-tabs__tab {
  padding: 8px 20px;
  font-family: var(--font-display);
  font-size: 0.95rem;
  letter-spacing: 0.04em;
  color: var(--ed-text-dim, #9a8f7d);
  background: var(--ed-tab-bg, rgba(20, 16, 12, 0.6));
  border: 1px solid var(--ed-border, #3a3229);
  border-bottom: none;
  border-radius: 4px 4px 0 0;
}

.ed-tabs__tab--active {
  color: var(--ed-text, #e8dcc4);
  background: var(--ed-tab-bg-active, rgba(58, 50, 41, 0.9));
  border-color: var(--ed-accent, #b8964f);
}

.ed-tabs__tab:focus-visible {
  outline: 2px solid var(--ed-accent, #b8964f);
  outline-offset: -2px;
}
</style>
