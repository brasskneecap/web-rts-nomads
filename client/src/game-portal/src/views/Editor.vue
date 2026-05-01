<template>
  <div class="editor-view">
    <div class="editor-topbar editor-topbar--right">
      <UiButton size="sm" @click="router.push('/')">Back</UiButton>
    </div>
    <MapEditorPanel v-model="editorMap" />
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import MapEditorPanel from '@/components/MapEditorPanel.vue'
import UiButton from '@/components/ui/UiButton.vue'
import { createEditorMapConfig } from '@/game/maps/mapConfig'

const MAP_EDITOR_STORAGE_KEY = 'webrts.mapEditorDraft'
const router = useRouter()

function getStoredEditorMap() {
  const stored = localStorage.getItem(MAP_EDITOR_STORAGE_KEY)
  if (!stored) return createEditorMapConfig()
  try {
    return createEditorMapConfig(undefined, undefined, JSON.parse(stored))
  } catch {
    return createEditorMapConfig()
  }
}

const editorMap = ref(getStoredEditorMap())

watch(editorMap, (val) => {
  localStorage.setItem(MAP_EDITOR_STORAGE_KEY, JSON.stringify(val))
}, { deep: true })
</script>

<style scoped>
.editor-view {
  position: relative;
  width: 100%;
  height: 100%;
  min-height: 0;
  min-width: 0;
  display: flex;
  overflow: hidden;
  background: #05080d;
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
