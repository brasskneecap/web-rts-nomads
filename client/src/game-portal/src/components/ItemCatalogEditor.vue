<template>
  <!-- Items | Lists. The item catalog and the lists that group it are the same
       job — "what exists" and "where it shows up" — so they live behind one tab
       strip rather than two routes. -->
  <div class="item-catalog-editor">
    <EditorTabs
      v-model="activeTab"
      :tabs="TABS"
      id-prefix="item-catalog"
      label="Item catalog sections"
    />
    <div
      :id="`item-catalog-panel-${activeTab}`"
      class="item-catalog-editor__panel"
      role="tabpanel"
    >
      <!-- Each panel is kept mounted (v-show, not v-if) so switching tabs does
           not discard an in-progress edit or re-fetch the catalog.
           The v-show sits on these WRAPPERS, not on the panel components:
           ItemEditorPanel has two root nodes (its shell + the icon-gallery
           overlay), and Vue cannot apply a directive to a multi-root component
           — it silently no-ops, which left both editors on screen side by side. -->
      <div v-show="activeTab === 'items'" class="item-catalog-editor__pane">
        <ItemEditorPanel />
      </div>
      <div v-show="activeTab === 'lists'" class="item-catalog-editor__pane">
        <ListEditorPanel />
      </div>
      <div v-show="activeTab === 'tables'" class="item-catalog-editor__pane">
        <TableEditorPanel />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import EditorTabs from '@/components/editor/EditorTabs.vue'
import type { EditorTab } from '@/components/editor/EditorTabs.vue'
import ItemEditorPanel from '@/components/ItemEditorPanel.vue'
import ListEditorPanel from '@/components/ListEditorPanel.vue'
import TableEditorPanel from '@/components/TableEditorPanel.vue'

const TABS: EditorTab[] = [
  { id: 'items', label: 'Items' },
  { id: 'lists', label: 'Lists' },
  { id: 'tables', label: 'Tables' },
]

const activeTab = ref('items')
</script>

<style scoped>
.item-catalog-editor {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  min-height: 0;
  min-width: 0;
  /* Room for the standalone view's Exit button, which floats top-right. */
  padding-right: 64px;
  box-sizing: border-box;
}

.item-catalog-editor__panel {
  flex: 1;
  min-height: 0;
  min-width: 0;
  display: flex;
}

/* The hidden pane is display:none (v-show), so the visible one is the only flex
   child and takes the full width — the tabs swap the work surface rather than
   splitting it. */
.item-catalog-editor__pane {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
}

.item-catalog-editor__pane > * {
  flex: 1;
  min-width: 0;
}
</style>
