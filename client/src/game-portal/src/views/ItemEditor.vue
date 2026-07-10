<template>
  <div class="item-editor-view">
    <div class="item-editor-topbar item-editor-topbar--right">
      <ExitButton @click="router.push('/')" />
    </div>
    <ItemEditorPanel />
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import ItemEditorPanel from '@/components/ItemEditorPanel.vue'
import ExitButton from '@/components/ui/ExitButton.vue'

const router = useRouter()
</script>

<style scoped>
.item-editor-view {
  position: relative;
  z-index: 1;
  width: 100%;
  height: 100%;
  min-height: 0;
  min-width: 0;
  display: flex;
  overflow: hidden;
}
.item-editor-topbar {
  position: absolute;
  top: 16px;
  z-index: 20;
}
.item-editor-topbar--right {
  right: 16px;
}
</style>
