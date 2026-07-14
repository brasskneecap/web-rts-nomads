<template>
  <!-- innerPanel = the plain dark-wood 9-slice; the brass hairline under the
       title is ours, so a card reads as a card without a second frame. -->
  <UiPanel variant="innerPanel" :padding="0" class="ed-card">
    <div class="ed-card__inner">
      <header class="ed-card__head">
        <span class="ed-card__title">
          <span v-if="index != null" class="ed-card__index">{{ index }}.</span>
          {{ title }}
        </span>
        <slot name="head-action" />
      </header>
      <div class="ed-card__body">
        <slot />
      </div>
    </div>
  </UiPanel>
</template>

<script setup lang="ts">
import UiPanel from '@/components/ui/UiPanel.vue'

defineProps<{
  title: string
  /** Optional leading number, matching the editor's section order. */
  index?: number
}>()
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
