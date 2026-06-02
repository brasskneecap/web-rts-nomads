<template>
  <div class="ui-tooltip" role="tooltip" aria-live="polite">
    <div v-if="title" class="ui-tooltip__title">{{ title }}</div>
    <div v-if="body" class="ui-tooltip__body">{{ body }}</div>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  title?: string
  body?: string
}>()
</script>

<!--
  Non-scoped styles so any parent can target `.ui-tooltip` to drive show/hide
  via its own :hover / :focus-within rule, matching the existing pattern in
  SelectionHud.vue's perk-tooltip. The trigger element should be
  position: relative so the tooltip anchors above it.
-->
<style>
.ui-tooltip {
  position: absolute;
  bottom: calc(100% + 6px);
  left: 50%;
  transform: translateX(-50%);
  min-width: 160px;
  max-width: 240px;
  padding: 7px 10px;
  border-radius: 8px;
  background: linear-gradient(180deg, rgba(34, 22, 10, 0.98), rgba(20, 12, 4, 0.98));
  border: 1px solid rgba(200, 164, 106, 0.45);
  box-shadow: 0 6px 18px rgba(0, 0, 0, 0.5);
  color: #f5ead2;
  text-align: left;
  opacity: 0;
  visibility: hidden;
  transition: opacity 0.12s ease-out;
  pointer-events: none;
  z-index: 10;
  white-space: normal;
}

.ui-tooltip__title {
  font-size: 13px;
  font-weight: 700;
  color: #fff2d6;
  margin-bottom: 4px;
  line-height: 1.5;
}

.ui-tooltip__body {
  font-size: 12px;
  line-height: 1.5;
  color: #d4b87a;
  white-space: pre-line;
}
</style>
