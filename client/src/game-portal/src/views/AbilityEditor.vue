<template>
  <div class="ability-editor-view">
    <div class="ability-editor-view__topbar">
      <ExitButton @click="router.push('/')" />
    </div>
    <AbilityEditorPanel />
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import AbilityEditorPanel from '@/components/AbilityEditorPanel.vue'
import ExitButton from '@/components/ui/ExitButton.vue'
const router = useRouter()
</script>

<style scoped>
.ability-editor-view { position: relative; width: 100%; height: 100%; min-height: 0; display: flex; overflow: hidden; }
.ability-editor-view__topbar { position: absolute; top: 16px; right: 16px; z-index: 20; }
</style>
