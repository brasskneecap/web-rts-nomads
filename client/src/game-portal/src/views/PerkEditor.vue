<template>
  <div class="perk-editor-view">
    <div class="perk-editor-view__topbar">
      <div class="perk-editor-view__mode" role="group" aria-label="Editor mode">
        <button type="button" :class="{ 'is-on': mode === 'builder' }" @click="mode = 'builder'">New Builder</button>
        <button type="button" :class="{ 'is-on': mode === 'classic' }" @click="mode = 'classic'">Classic</button>
      </div>
      <ExitButton @click="router.push('/')" />
    </div>
    <PerkBuilderPanel v-if="mode === 'builder'" />
    <PerkEditorPanel v-else />
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import PerkEditorPanel from '@/components/PerkEditorPanel.vue'
import PerkBuilderPanel from '@/components/perk-editor/PerkBuilderPanel.vue'
import ExitButton from '@/components/ui/ExitButton.vue'
const router = useRouter()
// Default to the new builder so the redesign is what users see; Classic remains
// one click away for the modifier kinds it can't edit yet.
const mode = ref<'builder' | 'classic'>('builder')
</script>

<style scoped>
.perk-editor-view { position: relative; width: 100%; height: 100%; min-height: 0; display: flex; overflow: hidden; }
.perk-editor-view__topbar { position: absolute; top: 16px; right: 16px; z-index: 20; display: flex; gap: 8px; align-items: center; }
.perk-editor-view__mode { display: flex; border: 1px solid var(--ed-line-strong); border-radius: 6px; overflow: hidden; }
.perk-editor-view__mode button { padding: 4px 10px; font-size: 0.72rem; background: var(--ed-field); color: var(--ed-text-dim); border: 0; }
.perk-editor-view__mode button.is-on { background: rgba(212, 168, 71, 0.18); color: var(--ed-brass); }
</style>
