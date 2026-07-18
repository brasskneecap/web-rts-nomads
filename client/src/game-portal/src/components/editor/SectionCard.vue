<template>
  <!-- innerPanel = the plain dark-wood 9-slice; the brass hairline under the
       title is ours, so a card reads as a card without a second frame. A card
       can opt into a different UiPanel skin (e.g. worldMenu) via `variant`. -->
  <UiPanel :variant="variant" :padding="0" class="ed-card">
    <div class="ed-card__inner">
      <header class="ed-card__head" :class="{ 'ed-card__head--collapsed': collapsible && collapsed }">
        <!-- Collapsible cards make the title a toggle button with a leading
             chevron; non-collapsible cards keep a plain title span, so every
             existing SectionCard renders byte-identically. -->
        <button
          v-if="collapsible"
          type="button"
          class="ed-card__toggle"
          :aria-expanded="!collapsed"
          data-test="section-card-toggle"
          @click="collapsed = !collapsed"
        >
          <span class="ed-card__chevron" :class="{ 'ed-card__chevron--collapsed': collapsed }" aria-hidden="true" />
          <span class="ed-card__title">
            <span v-if="index != null" class="ed-card__index">{{ index }}.</span>
            {{ title }}
          </span>
        </button>
        <span v-else class="ed-card__title">
          <span v-if="index != null" class="ed-card__index">{{ index }}.</span>
          {{ title }}
        </span>
        <slot name="head-action" />
      </header>
      <div v-show="!collapsible || !collapsed" class="ed-card__body">
        <slot />
      </div>
    </div>
  </UiPanel>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import UiPanel from '@/components/ui/UiPanel.vue'

const props = withDefaults(defineProps<{
  title: string
  /** Optional leading number, matching the editor's section order. */
  index?: number
  /** UiPanel skin for the card frame. Defaults to the plain inner panel. */
  variant?: 'default' | 'parchment' | 'footer' | 'worldMenu' | 'worldInner' | 'warRoomInner' | 'innerPanel'
  /** Opt-in: show a chevron toggle that hides the body. Off by default. */
  collapsible?: boolean
  /** Initial collapsed state when `collapsible`. Ignored otherwise. */
  defaultCollapsed?: boolean
}>(), {
  variant: 'innerPanel',
  collapsible: false,
  defaultCollapsed: false,
})

// Uncontrolled collapse state — seeded once from defaultCollapsed. A parent that
// needs to drive it can be added later; no consumer needs that yet.
const collapsed = ref(props.defaultCollapsed)
</script>

<style scoped>
.ed-card {
  min-width: 0;
}

.ed-card__inner {
  padding: 10px 12px 12px;
  display: flex;
  flex-direction: column;
  gap: var(--ed-gap);
}

.ed-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding-bottom: 6px;
  border-bottom: 1px solid var(--ed-line);
}

/* When collapsed the hairline under the header reads as clutter (there's no
   body beneath it), so drop it. */
.ed-card__head--collapsed {
  padding-bottom: 0;
  border-bottom: none;
}

.ed-card__toggle {
  display: flex;
  align-items: center;
  gap: 7px;
  flex: 1 1 auto;
  min-width: 0;
  padding: 0;
  background: none;
  border: 0;
  text-align: left;
  color: inherit;
  font: inherit;
}

/* A small CSS chevron (no icon font dependency) that points down when expanded
   and right when collapsed. */
.ed-card__chevron {
  flex: 0 0 auto;
  width: 0;
  height: 0;
  border-left: 5px solid transparent;
  border-right: 5px solid transparent;
  border-top: 6px solid var(--ed-brass);
  transition: transform 0.12s ease;
}

.ed-card__chevron--collapsed {
  transform: rotate(-90deg);
}

.ed-card__title {
  font-family: var(--font-title);
  font-size: 0.78rem;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--ed-brass);
}

.ed-card__index {
  color: var(--ed-brass-dim);
}

.ed-card__body {
  display: flex;
  flex-direction: column;
  gap: var(--ed-gap);
  min-width: 0;
}
</style>
