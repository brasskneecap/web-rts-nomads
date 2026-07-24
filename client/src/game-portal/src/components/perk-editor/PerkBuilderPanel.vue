<template>
  <UiPanel variant="worldMenu" :padding="0" class="pb ed-shell ed-theme-forge">
    <div class="pb__grid">
      <header class="pb__header">
        <EditorHeader
          :title="builder.form.value.displayName || builder.form.value.id || 'New Perk'"
          :badge="builder.form.value.wired ? 'Wired' : 'Not Wired'"
          :badge-color="builder.form.value.wired ? 'var(--ed-ok)' : 'var(--ed-danger)'"
          :id="builder.form.value.id"
          :id-editable="builder.selectedId.value === null"
          :saving="builder.busy.value"
          :save-disabled="builder.busy.value || !builder.form.value.id"
          :saved-label="builder.statusMessage.value"
          :error="builder.saveError.value"
          reset-label="Reset"
          :reset-disabled="builder.busy.value"
          :remove-label="builder.selectedId.value ? 'Delete' : ''"
          @save="builder.save"
          @reset="onReset"
          @remove="onRemove"
          @update:id="(v: string) => (builder.form.value = { ...builder.form.value, id: v })"
        />
      </header>

      <aside class="pb__sidebar">
        <PerkSidebar
          :perks="builder.perks.value"
          :paths-by-unit="builder.pathsByUnit.value"
          :selected-id="builder.selectedId.value"
          :load-error="builder.loadError.value"
          @select="onSelect"
          @new="builder.newPerk"
        />
      </aside>

      <main class="pb__content">
        <EditorTabs :tabs="tabs" v-model="activeTab" id-prefix="perk-builder" label="Perk sections" />
        <div class="pb__panel" role="tabpanel">
          <PerkIdentityTab v-if="activeTab === 'identity'" />
          <div v-else-if="activeTab === 'modifiers'" class="pb__mods">
            <GameScrollArea class="pb__mods-col"><PerkModifierStack /></GameScrollArea>
            <GameScrollArea class="pb__mods-col"><PerkModifierInspector /></GameScrollArea>
          </div>
          <PerkConfigTab v-else />
        </div>
      </main>
    </div>

    <datalist id="perk-builder-perk-ids">
      <option v-for="p in builder.perks.value" :key="p.id" :value="p.id" />
    </datalist>
  </UiPanel>
</template>

<script setup lang="ts">
import { onMounted, provide, ref } from 'vue'
import UiPanel from '@/components/ui/UiPanel.vue'
import EditorHeader from '@/components/editor/EditorHeader.vue'
import EditorTabs, { type EditorTab } from '@/components/editor/EditorTabs.vue'
import GameScrollArea from '@/components/ui/GameScrollArea.vue'
import { confirmDelete } from '@/components/editor/confirmDelete'
import { ask } from '@/components/ui/useConfirmDialog'
import PerkSidebar from './PerkSidebar.vue'
import PerkModifierStack from './PerkModifierStack.vue'
import PerkModifierInspector from './PerkModifierInspector.vue'
import PerkIdentityTab from './PerkIdentityTab.vue'
import PerkConfigTab from './PerkConfigTab.vue'
import { usePerkBuilder } from './usePerkBuilder'
import { PerkBuilderKey } from './PerkBuilderContext'
import '@/components/editor/editor-controls.css'
import '@/components/editor/editor-theme.css'

const builder = usePerkBuilder()
provide(PerkBuilderKey, builder)

const tabs: EditorTab[] = [
  { id: 'identity', label: 'Identity' },
  { id: 'modifiers', label: 'Modifiers' },
  { id: 'config', label: 'Config' },
]
const activeTab = ref('modifiers')

function onSelect(id: string) {
  const def = builder.perks.value.find((p) => p.id === id)
  if (def) builder.selectPerk(def)
}
async function onReset() {
  if (!(await ask({ title: 'Discard unsaved changes?', lines: ['Revert this perk to its last-saved state.'], confirmLabel: 'Reset', cancelLabel: 'Cancel' }))) return
  builder.resetPerk()
}
async function onRemove() {
  if (!builder.selectedId.value) return
  if (!(await confirmDelete('perk', builder.selectedId.value, undefined, 'If it ships with the game it will reset to its built-in default; a custom one is removed.'))) return
  await builder.removePerk()
}

onMounted(builder.load)
</script>

<style scoped>
.pb { width: 100%; height: 100%; min-height: 0; }
.pb__grid {
  display: grid;
  grid-template-columns: minmax(220px, 260px) minmax(0, 1fr);
  grid-template-rows: auto minmax(0, 1fr);
  grid-template-areas: "header header" "sidebar content";
  gap: var(--ed-gap);
  padding: var(--ed-gap);
  width: 100%; height: 100%; min-height: 0; box-sizing: border-box;
}
.pb__header { grid-area: header; min-width: 0; }
.pb__sidebar { grid-area: sidebar; min-height: 0; min-width: 0; display: flex; flex-direction: column; }
.pb__content { grid-area: content; min-height: 0; min-width: 0; display: flex; flex-direction: column; gap: var(--ed-gap); }
.pb__panel { flex: 1 1 auto; min-height: 0; display: flex; flex-direction: column; }
.pb__mods { display: grid; grid-template-columns: minmax(0, 1fr) minmax(300px, 380px); gap: var(--ed-gap); min-height: 0; flex: 1 1 auto; }
.pb__mods-col { min-height: 0; }
@media (max-width: 1100px) {
  .pb__mods { grid-template-columns: minmax(0, 1fr); }
}
</style>
