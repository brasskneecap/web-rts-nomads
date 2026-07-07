<template>
  <UiPanel variant="parchment" :padding="0" class="custom-game">
    <button
      type="button"
      class="custom-game__close"
      aria-label="Close custom game panel"
      @click="emit('close')"
    >
      &times;
    </button>
    <div class="custom-game__inner">
      <div class="custom-game__header">
        <h1 class="custom-game__title">Custom Game</h1>
      </div>

      <!-- Tab strip. Mirrors the Campaign panel's tabs: each tab swaps the
           content rendered below inside this single parchment panel. -->
      <div class="custom-game__tabs" role="tablist">
        <button
          v-for="tab in TABS"
          :key="tab.id"
          type="button"
          role="tab"
          :aria-selected="tab.id === activeTab"
          class="custom-game__tab"
          :class="{ 'custom-game__tab--active': tab.id === activeTab }"
          @click="activeTab = tab.id"
        >
          <span class="custom-game__tab-label">{{ tab.label }}</span>
        </button>
      </div>

      <div class="custom-game__body">
        <StartGameTab v-if="activeTab === 'start'" />
        <FindGameTab v-else-if="activeTab === 'find'" />
        <DirectConnectTab v-else-if="activeTab === 'direct'" />
      </div>
    </div>
  </UiPanel>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import UiPanel from '@/components/ui/UiPanel.vue'
import StartGameTab from '@/components/menu/custom-game/StartGameTab.vue'
import FindGameTab from '@/components/menu/custom-game/FindGameTab.vue'
import DirectConnectTab from '@/components/menu/custom-game/DirectConnectTab.vue'

type CustomTab = 'start' | 'find' | 'direct'

const props = withDefaults(
  defineProps<{
    /** Sub-tab to open on mount. Lets deep-links / lobby-return flows land
     *  directly on Find Game or Direct Connect (see WarRoom `?sub=` query). */
    initialTab?: CustomTab
  }>(),
  { initialTab: 'start' },
)

const emit = defineEmits<{
  (e: 'close'): void
}>()

const TABS: { id: CustomTab; label: string }[] = [
  { id: 'start', label: 'Start Game' },
  { id: 'find', label: 'Find Game' },
  { id: 'direct', label: 'Direct Connect' },
]

const activeTab = ref<CustomTab>(props.initialTab)
</script>

<style scoped>
/* Parchment-panel wrapper. Fills the war-room parchment slot; the UiPanel
   itself draws the 9-slice parchment border-image via its own scoped CSS.
   Mirrors the Campaign panel so both slots share the same frame. */
.custom-game {
  position: absolute;
  inset: 0;
  display: flex;
  box-sizing: border-box;
}

/* Close X — sits in the top-right of the parchment frame, matching Campaign. */
.custom-game__close {
  position: absolute;
  top: calc(var(--s) * 18);
  right: calc(var(--s) * 22);
  z-index: 2;
  width: calc(var(--s) * 36);
  height: calc(var(--s) * 36);
  display: flex;
  align-items: center;
  justify-content: center;
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: calc(var(--s) * 30);
  font-weight: 700;
  line-height: 1;
  color: #3a1f0a;
  background: transparent;
  border: 0;
  padding: 0;
  /* Redeclared here because the close button is a sibling of __inner, not a
     descendant, so it can't inherit --s from it. */
  --s: 0.0929cqw;
}

.custom-game__close:hover,
.custom-game__close:focus-visible {
  color: #7a3a10;
  outline: none;
}

.custom-game__inner {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  padding: 2% 3%;
  box-sizing: border-box;
  color: #3a1f0a;
  /* Single scale unit driving sizes below. Matches the Campaign / Advancements
     panels so all content slots share visual scale at the same parchment size. */
  --s: 0.0929cqw;
  gap: calc(var(--s) * 12);
  overflow-y: auto;
  min-height: 0;
}

.custom-game__header {
  flex: 0 0 auto;
  display: flex;
  justify-content: flex-start;
}

.custom-game__title {
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: calc(var(--s) * 28);
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  margin: 0;
}

.custom-game__tabs {
  flex: 0 0 auto;
  display: flex;
  gap: calc(var(--s) * 8);
  border-bottom: 1px solid rgba(58, 31, 10, 0.25);
}

.custom-game__tab {
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: calc(var(--s) * 16);
  font-weight: 700;
  letter-spacing: 0.05em;
  padding: calc(var(--s) * 6) calc(var(--s) * 12);
  background: transparent;
  border: 0;
  color: rgba(58, 31, 10, 0.6);
  border-bottom: 2px solid transparent;
  display: inline-flex;
  align-items: center;
  gap: calc(var(--s) * 6);
}

.custom-game__tab--active {
  color: #3a1f0a;
  border-bottom-color: #8a5a2a;
}

.custom-game__body {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  min-height: 0;
}
</style>
