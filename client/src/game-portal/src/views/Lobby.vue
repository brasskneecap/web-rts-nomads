<template>
  <div class="lobby" :style="rootVars">
    <UiPanel variant="worldMenu" :padding="0" class="lobby__panel" :style="assetVars">
      <div class="lobby__frame">
        <header class="lobby__titlebar">
          <span class="lobby__title">Lobby</span>
        </header>
        <PanelLobby :lobby-id="lobbyId" class="lobby__content" @back="onBack" />
      </div>
    </UiPanel>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import UiPanel from '@/components/ui/UiPanel.vue'
import PanelLobby from '@/components/menu/PanelLobby.vue'
import headerUrl from '@/assets/ui/themes/updated/world-panel-header.png'
import warRoomBgUrl from '@/assets/background-images/war_room_bg.png'

const router = useRouter()
const route = useRoute()

const lobbyId = computed(() => route.params.id as string)

// Header art exposed to scoped CSS (mirrors CustomGame's title bar) so the
// joiner's lobby frame matches the host's.
const assetVars = computed(() => ({
  '--cg-header': `url(${headerUrl})`,
}))

// Same war-room scene behind the panel as the host sees, so landing here
// doesn't read as a blank screen change.
const rootVars = computed(() => ({
  '--lobby-bg': `url(${warRoomBgUrl})`,
}))

// Routed lobby (joiner). Renders the same PanelLobby the host sees, framed by
// the same world-menu chrome. Leaving / the lobby vanishing routes back to the
// war-room's Find Game tab; match-start navigation is handled in the composable
// (inside PanelLobby's useLobbyRoom).
function onBack() {
  void router.push('/war-room?tab=custom&sub=find')
}
</script>

<style scoped>
.lobby {
  position: relative;
  z-index: 1;
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  background:
    var(--lobby-bg) center / cover no-repeat,
    #05080d;
  padding: 32px;
  box-sizing: border-box;
}

/* The world-menu frame. `container-type: size` gives the nested --s (used by
   PanelLobby and the title bar) a definite basis, mirroring the war-room slot. */
.lobby__panel {
  width: min(92vw, 1000px);
  height: min(88vh, 760px);
  display: flex;
  container-type: size;
}

.lobby__frame {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  min-height: 0;
  min-width: 0;
  --s: 0.093cqw;
  gap: calc(var(--s) * 12);
  padding: 0 calc(var(--s) * 6) calc(var(--s) * 6);
  color: #e9dbb8;
}

/* Header bar — same world-panel-header art + centered title as CustomGame. */
.lobby__titlebar {
  position: relative;
  align-self: center;
  flex: 0 0 auto;
  width: min(100%, calc(var(--s) * 760));
  aspect-ratio: 740 / 140;
  background: var(--cg-header) center / 100% 100% no-repeat;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-top: calc(var(--s) * -44 - 40px);
  z-index: 2;
}

.lobby__title {
  font-family: var(--font-title);
  font-size: calc(var(--s) * 34);
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #e7c88a;
  text-shadow:
    0 1px 2px rgba(0, 0, 0, 0.85),
    0 0 12px rgba(212, 168, 71, 0.3);
  /* Match CustomGame: centered on the wood bar and nudged 25px right. */
  transform: translate(25px, calc(var(--s) * 8));
}

.lobby__content {
  flex: 1 1 auto;
  min-height: 0;
}
</style>
