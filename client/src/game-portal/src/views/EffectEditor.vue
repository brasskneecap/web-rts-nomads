<template>
  <div class="effect-editor-view">
    <div class="effect-editor-view__topbar">
      <ExitButton @click="router.push('/')" />
    </div>
    <EffectEditorPanel />
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import EffectEditorPanel from '@/components/EffectEditorPanel.vue'
import ExitButton from '@/components/ui/ExitButton.vue'
const router = useRouter()
</script>

<style scoped>
.effect-editor-view { position: relative; width: 100%; height: 100%; min-height: 0; display: flex; overflow: hidden; }
.effect-editor-view__topbar { position: absolute; top: 16px; right: 16px; z-index: 20; }
</style>
