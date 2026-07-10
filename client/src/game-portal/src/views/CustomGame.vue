<template>
  <!-- Outer world-menu frame (black chrome). Inside: a brass header title bar,
       the asset-backed tab strip, then the per-tab content on dark inner
       panels. Mirrors the base menu panel system (world-menu-panel + the
       war-room asset set). -->
  <UiPanel variant="worldMenu" :padding="0" class="custom-game" :style="assetVars">
    <div class="custom-game__frame">
      <!-- Brass header bar (world-panel-header art) with the centered title. -->
      <header class="custom-game__titlebar">
        <span class="custom-game__title">Custom Game</span>
      </header>

      <!-- In-panel lobby view. When the host creates a lobby from Start Game,
           the lobby is hosted here inside the same frame (mirrors the Campaign
           panel); its Back button pops back to the tab strip (@back). -->
      <PanelLobby
        v-if="view === 'lobby'"
        class="custom-game__lobby"
        :lobby-id="activeLobbyId"
        @back="view = 'tabs'"
      />

      <template v-else>
        <!-- Tab strip. Active tab uses the blue war-room button art, inactive
             tabs use the dark button art (per design direction). -->
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

        <div class="custom-game__content">
          <StartGameTab
            v-if="activeTab === 'start'"
            @lobby-created="onLobbyCreated"
            @back="emit('close')"
          />
          <FindGameTab
            v-else-if="activeTab === 'find'"
            @lobby-joined="onLobbyCreated"
            @back="emit('close')"
          />
          <DirectConnectTab v-else-if="activeTab === 'direct'" @back="emit('close')" />
        </div>
      </template>
    </div>
  </UiPanel>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import UiPanel from '@/components/ui/UiPanel.vue'
import StartGameTab from '@/components/menu/custom-game/StartGameTab.vue'
import FindGameTab from '@/components/menu/custom-game/FindGameTab.vue'
import DirectConnectTab from '@/components/menu/custom-game/DirectConnectTab.vue'
import PanelLobby from '@/components/menu/PanelLobby.vue'
import headerUrl from '@/assets/ui/themes/updated/world-panel-header.png'
import tabActiveUrl from '@/assets/ui/themes/updated/war-room/war-room-active-button.png'
import tabInactiveUrl from '@/assets/ui/themes/updated/war-room/war-room-inactive-button.png'

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

// Asset URLs exposed to scoped CSS as custom properties (scoped styles can't
// import assets directly). Mirrors how UiPanel wires its border-image source.
const assetVars = computed(() => ({
  '--cg-header': `url(${headerUrl})`,
  '--cg-tab-active': `url(${tabActiveUrl})`,
  '--cg-tab-inactive': `url(${tabInactiveUrl})`,
}))

const TABS: { id: CustomTab; label: string }[] = [
  { id: 'start', label: 'Start Game' },
  { id: 'find', label: 'Find Game' },
  { id: 'direct', label: 'Direct Connect' },
]

const activeTab = ref<CustomTab>(props.initialTab)

// Which sub-view the panel is showing. 'tabs' is the Start/Find/Direct tab
// strip; 'lobby' hosts the created lobby inline (PanelLobby) so creating a
// lobby never leaves the war-room. `activeLobbyId` is the lobby the in-panel
// view polls while `view === 'lobby'`. Mirrors Campaign.vue.
const view = ref<'tabs' | 'lobby'>('tabs')
const activeLobbyId = ref<string>('')

/** StartGameTab created a lobby: host it inline in this panel (view = 'lobby')
 *  instead of routing to /lobby/:id. The in-panel lobby's Back button pops
 *  back to the tab strip (Start Game stays selected). */
function onLobbyCreated(lobbyId: string) {
  activeLobbyId.value = lobbyId
  view.value = 'lobby'
}
</script>

<style scoped>
/* Outer world-menu (black) frame filling the war-room slot. */
.custom-game {
  position: absolute;
  inset: 0;
  display: flex;
  box-sizing: border-box;
}

/* Frame stacks the header bar, tab strip and content. --s (container-query
   scale unit) is declared here so all children share one scale. */
.custom-game__frame {
  position: relative;
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  align-items: stretch;
  min-height: 0;
  min-width: 0;
  --s: 0.0929cqw;
  gap: calc(var(--s) * 12);
  color: #e9dbb8;
}

/* Header bar — the world-panel-header art (shield + wood plaque). Aspect-ratio
   locked to the source so the shield never distorts; capped in width and
   centered so it reads as an inset plaque rather than a full-width band. */
.custom-game__titlebar {
  position: relative;
  align-self: center;
  flex: 0 0 auto;
  width: min(100%, calc(var(--s) * 760));
  aspect-ratio: 740 / 140;
  background: var(--cg-header) center / 100% 100% no-repeat;
  display: flex;
  align-items: center;
  justify-content: center;
  /* Raise the header so ~30% of it sits above the top of the main panel (it
     straddles the outer frame's top edge, like a mounted plaque). Negative
     margin (not transform) so the tab strip below follows up with no gap.
     ≈30% of the header's height at nominal width; tune with the panel size.
     Plus an extra fixed 40px lift. */
  margin-top: calc(var(--s) * -44 - 40px);
  z-index: 2;
}

.custom-game__title {
  font-family: var(--font-title);
  font-size: calc(var(--s) * 34);
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #e7c88a;
  text-shadow:
    0 1px 2px rgba(0, 0, 0, 0.85),
    0 0 12px rgba(212, 168, 71, 0.3);
  /* Nudged down to sit centered on the wood bar (below the shield overhang)
     and pushed 25px to the right. */
  transform: translate(25px, calc(var(--s) * 8));
}

/* Tab strip. Each tab is a button-art plaque (border-image so the brass frame
   stays crisp at any width); the interior fill comes from the art. */
.custom-game__tabs {
  flex: 0 0 auto;
  display: flex;
  gap: calc(var(--s) * 10);
  justify-content: flex-start;
  padding: 0 calc(var(--s) * 6);
}

.custom-game__tab {
  flex: 0 0 auto;
  min-width: calc(var(--s) * 150);
  padding: calc(var(--s) * 6) calc(var(--s) * 20);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: none;
  border: calc(var(--s) * 14) solid transparent;
  border-image-source: var(--cg-tab-inactive);
  border-image-slice: 14 fill;
  border-image-width: calc(var(--s) * 14);
  border-image-repeat: stretch;
  image-rendering: pixelated;
  transition:
    filter 120ms ease,
    transform 80ms ease;
}

/* Inactive tabs: brighten on hover, press down on click. The active tab keeps
   its blue art untouched. */
.custom-game__tab:not(.custom-game__tab--active):hover {
  filter: brightness(1.15);
}

.custom-game__tab:not(.custom-game__tab--active):active {
  filter: brightness(0.9);
  transform: translateY(1px);
}

.custom-game__tab-label {
  font-family: var(--font-title);
  font-size: calc(var(--s) * 16);
  font-weight: 700;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: rgba(233, 219, 184, 0.6);
  white-space: nowrap;
}

.custom-game__tab:hover .custom-game__tab-label {
  color: #e9dbb8;
}

.custom-game__tab--active {
  border-image-source: var(--cg-tab-active);
}

.custom-game__tab--active .custom-game__tab-label {
  color: #f4e3b6;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.7);
}

/* Per-tab content region. Fills the remaining frame height; tabs manage their
   own inner panels. --s carried down so tab content shares the scale. */
.custom-game__content,
.custom-game__lobby {
  flex: 1 1 auto;
  min-height: 0;
  min-width: 0;
  display: flex;
  flex-direction: column;
  padding: 0 calc(var(--s) * 6) calc(var(--s) * 6);
}
</style>
