<template>
  <div class="projectile-editor-view">
    <div class="projectile-editor-view__topbar">
      <ExitButton @click="router.push('/')" />
    </div>
    <ProjectileEditorPanel />
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import ProjectileEditorPanel from '@/components/ProjectileEditorPanel.vue'
import ExitButton from '@/components/ui/ExitButton.vue'
const router = useRouter()
</script>

<style scoped>
.projectile-editor-view { position: relative; width: 100%; height: 100%; min-height: 0; display: flex; overflow: hidden; }
.projectile-editor-view__topbar { position: absolute; top: 16px; right: 16px; z-index: 20; }
</style>
