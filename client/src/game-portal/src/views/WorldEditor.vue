<template>
  <div class="world-editor-view">
    <div class="world-editor-topbar world-editor-topbar--right">
      <ExitButton @click="router.push('/')" />
    </div>
    <WorldEditorPanel v-model="editorMap" />
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import WorldEditorPanel from '@/components/world-editor/WorldEditorPanel.vue'
import ExitButton from '@/components/ui/ExitButton.vue'
import { createEditorMapConfig } from '@/game/maps/mapConfig'

const router = useRouter()
const editorMap = ref(createEditorMapConfig())
</script>

<style scoped>
.world-editor-view {
  position: relative; z-index: 1; width: 100%; height: 100%;
  min-height: 0; min-width: 0; display: flex; overflow: hidden;
}
.world-editor-topbar { position: absolute; top: 16px; z-index: 20; }
.world-editor-topbar--right { right: 16px; }
</style>
