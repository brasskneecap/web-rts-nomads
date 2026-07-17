<template>
  <div class="ability-editor-view">
    <div class="ability-editor-view__topbar">
      <ExitButton @click="router.push('/')" />
    </div>
    <AbilityBuilderPanel class="ability-editor-view__panel" />
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import AbilityBuilderPanel from '@/components/ability-builder/AbilityBuilderPanel.vue'
import ExitButton from '@/components/ui/ExitButton.vue'
const router = useRouter()
</script>

<style scoped>
/* A flex COLUMN (topbar row above, panel row below) rather than the old
   absolutely-positioned topbar: the preview rail (Task 5) now sits at the
   panel's top-right corner, exactly where the absolute topbar used to float
   harmlessly over an inspector rail. Stacking them as sibling flex rows
   keeps the ExitButton clear of the renderer without the panel needing to
   know the topbar exists (no preview-specific padding hacks). */
.ability-editor-view {
  width: 100%;
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.ability-editor-view__topbar {
  flex: 0 0 auto;
  display: flex;
  justify-content: flex-end;
  padding: 16px 16px 0;
}
.ability-editor-view__panel {
  flex: 1 1 auto;
  min-height: 0;
}
</style>
