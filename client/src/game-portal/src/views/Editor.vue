<template>
  <div class="editor-view">
    <div class="editor-topbar editor-topbar--right">
      <ExitButton @click="router.push('/')" />
    </div>
    <MapEditorPanel v-model="editorMap" />
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import MapEditorPanel from '@/components/MapEditorPanel.vue'
import ExitButton from '@/components/ui/ExitButton.vue'
import { createEditorMapConfig } from '@/game/maps/mapConfig'

const router = useRouter()

const editorMap = ref(createEditorMapConfig())
</script>

<style scoped>
.editor-view {
  position: relative;
  z-index: 1;
  width: 100%;
  height: 100%;
  min-height: 0;
  min-width: 0;
  display: flex;
  overflow: hidden;
}

.editor-topbar {
  position: absolute;
  top: 16px;
  z-index: 20;
}

.editor-topbar--right {
  right: 16px;
}
</style>
