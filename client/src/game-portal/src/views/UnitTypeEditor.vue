<template>
  <div class="unit-type-editor-view">
    <div class="unit-type-editor-view__topbar">
      <ExitButton @click="router.push('/')" />
    </div>
    <UnitTypeEditorPanel />
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import UnitTypeEditorPanel from '@/components/UnitTypeEditorPanel.vue'
import ExitButton from '@/components/ui/ExitButton.vue'
const router = useRouter()
</script>

<style scoped>
.unit-type-editor-view { position: relative; width: 100%; height: 100%; min-height: 0; display: flex; overflow: hidden; }
.unit-type-editor-view__topbar { position: absolute; top: 16px; right: 16px; z-index: 20; }
</style>
