<template>
  <div class="world-editor-view">
    <WorldEditorPanel v-model="editorMap" />
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import WorldEditorPanel from '@/components/world-editor/WorldEditorPanel.vue'
import { createEditorMapConfig } from '@/game/maps/mapConfig'

const editorMap = ref(createEditorMapConfig())
</script>

<style scoped>
.world-editor-view {
  position: relative; z-index: 1; width: 100%; height: 100%;
  min-height: 0; min-width: 0; display: flex; overflow: hidden;
}
</style>
