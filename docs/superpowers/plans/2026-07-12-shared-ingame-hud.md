# Shared In-Game HUD Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract `Match.vue`'s full in-match HUD into one shared `InGameHud.vue` rendered by BOTH the real match and the world-editor playtest, so the playtest has every real-game option (shop, vault, upgrades, settings, wave upgrades, …) and future HUD changes update both places.

**Architecture:** `InGameHud.vue` owns the HUD template + the `.match-stage` positioning wrapper + a default `<slot>` for the game canvas, with a `display:contents` root so it is layout-transparent. Each host (`Match.vue`, the editor) provides its canvas via the slot and a `.match-view`-style flex-column container. The one cross-boundary action (leave match) is a single `exit` emit. Route/lifecycle logic stays entirely in `Match.vue`.

**Tech Stack:** Vue 3 + TypeScript SPA (`client/src/game-portal`, vitest + @vue/test-utils).

**Spec:** `docs/superpowers/specs/2026-07-12-shared-ingame-hud-design.md`

## Global Constraints

- Branch: all work on `unit-types-editor` (verify, never switch).
- The **real match must behave and lay out identically** after the refactor (presentation-only move). This is a hard gate — a manual `/match` E2E must confirm it before the branch is "done."
- `InGameHud` imports NO router / route / `campaignSession` / `matchEndState` — the shell (`Match.vue`) owns all navigation/lifecycle. The ONLY cross-boundary contract is the `exit` emit.
- `InGameHud` reads state from its `hud` prop (`ui = hud.ui.value`) and calls `hud.*` forwarders; it owns only its own menu/settings/items/debug open-state. It does NOT call `useGameClient()` itself (that would create a second instance with a dead `ui`).
- `InGameHud` root is `display: contents` (layout-transparent); the canvas is passed in via `<slot/>` and stays owned (ref) by the host.
- Objectives gate simplifies to `ui.objectives.length` (drop the `campaignSession` gate) — equivalent because non-campaign matches carry no objectives; the real-match E2E confirms no regression.
- No literal `cursor:` declarations in new/changed component CSS except `cursor: not-allowed` on forbidden-action states.
- Ephemeral playtest invariant preserved: fresh match per Play, no reward persistence (already server-gated).
- Client commands from `client/src/game-portal`. The build gate is `npm run build` (runs `vue-tsc -b`, enforces `noUnusedLocals`) — NOT just `vue-tsc --noEmit`.

---

### Task 1: Create `InGameHud.vue` + unit test

**Files:**
- Create: `client/src/game-portal/src/components/InGameHud.vue`
- Test: `client/src/game-portal/src/components/InGameHud.test.ts`

**Interfaces:**
- Consumes: the `useGameClient()` handle as prop `hud` (typed `ReturnType<typeof import('@/composables/useGameClient').useGameClient>`). Reads `hud.ui.value` (`GameUiSnapshot`) and calls its forwarders (`performSelectionAction`, `selectUnitOnly`, `deselectUnit`, `setMinimapPanelRect`, `sendUseConsumable`, `sendUnequipItem`, `sendEquipItem`, `sendWaveUpgradeChoice`, `sendWaveUpgradeReroll`, `beginDebugSpawn`, `cancelDebugSpawn`, `purchaseUpgrade`, `cancelUpgrade`, `setVaultSelectedInstanceId`, `sendTransferItem`, `sendUseItemOnUnit`, `focusUnit`, `sendPurchaseItem`, `sendPurchaseRecipe`, `rerollShop`, `craftItem`, `beginCommanderAbility`, `cancelCommanderAbility`, `beginItemUse`, `cancelItemUse`, `sendSetPause` — all confirmed present on the handle).
- Produces: `<InGameHud :hud="…" @exit="…"><canvas …/></InGameHud>`. Emits `exit` (raised by the settings modal's "exit game"). Default slot = the game canvas.

- [ ] **Step 1: Write the failing test**

Create `client/src/game-portal/src/components/InGameHud.test.ts`:
```ts
import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { shallowMount } from '@vue/test-utils'
import InGameHud from './InGameHud.vue'
import MatchHud from '@/components/MatchHud.vue'
import SelectionHud from '@/components/SelectionHud.vue'
import MatchMenu from '@/components/MatchMenu.vue'
import MatchMenuLauncher from '@/components/MatchMenuLauncher.vue'
import MatchSettingsModal from '@/components/MatchSettingsModal.vue'

function mkHud(overrides: Record<string, unknown> = {}) {
  const ui = ref({
    selectedUnits: [],
    selection: { kind: 'none', title: '', subtitle: '', details: [], actions: [] },
    commanderAbilities: [],
    commanderTargetingAbilityId: null,
    objectives: [],
    zoneCaptureCards: [],
    zoneInspection: null,
    waveUpgrade: null,
    paused: false,
    pausedBy: '',
    pausedSinceMs: 0,
    vault: [],
    itemTargeting: null,
    shopCatalog: [],
    shopRerollsRemaining: 0,
    upgrades: [],
    vaultSelectedInstanceId: null,
    allPlayerUnits: [],
    craftCatalog: [],
    hasArtificer: false,
    debugSpawnTargetingActive: false,
    hoveredLootDrop: null,
    cursorClientX: 0,
    cursorClientY: 0,
    netStats: {},
    player: { playerId: 'p1', color: '#fff', totalUnits: 0, selectedUnits: 0, totalHp: 0, resources: [] },
    wave: { enabled: false },
    notifications: [],
    ...overrides,
  })
  return {
    ui,
    performSelectionAction: vi.fn(),
    selectUnitOnly: vi.fn(),
    deselectUnit: vi.fn(),
    setMinimapPanelRect: vi.fn(),
    sendUseConsumable: vi.fn(),
    sendUnequipItem: vi.fn(),
    sendEquipItem: vi.fn(),
    sendWaveUpgradeChoice: vi.fn(),
    sendWaveUpgradeReroll: vi.fn(),
    beginDebugSpawn: vi.fn(),
    cancelDebugSpawn: vi.fn(),
    purchaseUpgrade: vi.fn(),
    cancelUpgrade: vi.fn(),
    setVaultSelectedInstanceId: vi.fn(),
    sendTransferItem: vi.fn(),
    sendUseItemOnUnit: vi.fn(),
    focusUnit: vi.fn(),
    sendPurchaseItem: vi.fn(),
    sendPurchaseRecipe: vi.fn(),
    rerollShop: vi.fn(),
    craftItem: vi.fn(),
    beginCommanderAbility: vi.fn(),
    cancelCommanderAbility: vi.fn(),
    beginItemUse: vi.fn(),
    cancelItemUse: vi.fn(),
    sendSetPause: vi.fn(),
  }
}

describe('InGameHud', () => {
  it('renders the core HUD components fed by hud.ui', () => {
    const hud = mkHud()
    const w = shallowMount(InGameHud, { props: { hud: hud as any } })
    expect(w.findComponent(MatchHud).exists()).toBe(true)
    expect(w.findComponent(SelectionHud).exists()).toBe(true)
    expect(w.findComponent(MatchMenuLauncher).exists()).toBe(true)
    expect(w.findComponent(SelectionHud).props('ui')).toBe(hud.ui.value)
  })

  it('renders the game canvas passed via the default slot', () => {
    const hud = mkHud()
    const w = shallowMount(InGameHud, {
      props: { hud: hud as any },
      slots: { default: '<canvas class="test-canvas"></canvas>' },
    })
    expect(w.find('canvas.test-canvas').exists()).toBe(true)
  })

  it('opens the match menu on a tab, forwards a shop purchase, and forwards craft', async () => {
    const hud = mkHud()
    const w = shallowMount(InGameHud, { props: { hud: hud as any } })
    // Menu hidden until a tab is opened
    expect(w.findComponent(MatchMenu).exists()).toBe(false)
    w.findComponent(MatchMenuLauncher).vm.$emit('open', 'shop')
    await w.vm.$nextTick()
    expect(w.findComponent(MatchMenu).exists()).toBe(true)
    w.findComponent(MatchMenu).vm.$emit('purchase', { itemId: 'sword', buildingId: 'b1' })
    expect(hud.sendPurchaseItem).toHaveBeenCalledWith('b1', 'sword')
    w.findComponent(MatchMenu).vm.$emit('craft', 'recipe1')
    expect(hud.craftItem).toHaveBeenCalledWith('recipe1')
  })

  it('forwards a selection action and casts a commander ability (toggle)', () => {
    const hud = mkHud()
    const w = shallowMount(InGameHud, { props: { hud: hud as any } })
    w.findComponent(SelectionHud).vm.$emit('action', 'move')
    expect(hud.performSelectionAction).toHaveBeenCalledWith('move')
    w.findComponent(MatchMenuLauncher).vm.$emit('cast-ability', 'fireball')
    expect(hud.beginCommanderAbility).toHaveBeenCalledWith('fireball')
  })

  it('opens settings, and its exit-game bubbles as the component exit emit', async () => {
    const hud = mkHud()
    const w = shallowMount(InGameHud, { props: { hud: hud as any } })
    w.findComponent(MatchMenuLauncher).vm.$emit('settings')
    await w.vm.$nextTick()
    expect(w.findComponent(MatchSettingsModal).exists()).toBe(true)
    w.findComponent(MatchSettingsModal).vm.$emit('exit-game')
    expect(w.emitted('exit')).toBeTruthy()
  })
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd client/src/game-portal && npm run test -- InGameHud`
Expected: FAIL — `InGameHud.vue` does not exist.

- [ ] **Step 3: Create `InGameHud.vue` — template**

The template reproduces Match.vue's HUD structure EXACTLY, with these mechanical transforms: (a) drop every `hasStarted &&` guard (the host only mounts InGameHud when started); (b) bare forwarder names become `hud.<name>`; (c) the objectives gate drops `campaignSession`; (d) the settings `exit-game` calls `$emit('exit')` instead of `requestForfeit`; (e) the canvas is a `<slot/>` inside `.match-stage`. Local refs/handlers (`matchMenuOpen`, `matchMenuTab`, `itemsBarVisible`, `matchSettingsOpen`, `debugHudVisible`, `openMenuTab`, `onCommanderCast`, `onItemUse`) are declared in this component's script (Step 4).

```vue
<template>
  <div class="in-game-hud-root">
    <MatchHud :ui="ui" />

    <div
      v-if="ui.objectives.length || ui.zoneCaptureCards.length || ui.zoneInspection"
      class="match-objectives-anchor"
    >
      <MatchObjectivesPanel v-if="ui.objectives.length" :objectives="ui.objectives" />
      <ZoneCapturePanel v-if="ui.zoneCaptureCards.length" :cards="ui.zoneCaptureCards" />
      <ZoneInspectionPanel v-if="ui.zoneInspection" :info="ui.zoneInspection" />
    </div>

    <WaveUpgradeModal
      v-if="ui.waveUpgrade"
      :upgrade="ui.waveUpgrade!"
      :units="ui.allPlayerUnits.filter(u => u.unitType !== 'worker')"
      :send-choice="hud.sendWaveUpgradeChoice"
      :send-reroll="hud.sendWaveUpgradeReroll"
      :paused="ui.paused"
      :paused-since-ms="ui.pausedSinceMs"
    />

    <BattleTrackerPanel :ui="ui" />

    <DebugSpawnPanel
      :ui="ui"
      :targeting-active="debugSpawnTargetingActive"
      :begin-debug-spawn="hud.beginDebugSpawn"
      :cancel-debug-spawn="hud.cancelDebugSpawn"
    />

    <div v-if="ui.paused" class="pause-banner" role="status" aria-live="polite">
      <div class="pause-banner__title">Game Paused</div>
      <div class="pause-banner__sub">{{ pausedByLabel }} Open Settings to resume.</div>
    </div>

    <div class="match-stage">
      <slot />
      <SelectionHud
        :ui="ui"
        @action="hud.performSelectionAction"
        @select-unit="hud.selectUnitOnly"
        @deselect-unit="hud.deselectUnit"
        @minimap-rect="hud.setMinimapPanelRect"
        @use-consumable="({ unitId, slotIndex }) => hud.sendUseConsumable(unitId, slotIndex)"
        @unequip-item="({ unitId, slotIndex }) => hud.sendUnequipItem(unitId, slotIndex)"
        @equip-item="({ unitId, slotIndex, instanceId }) => hud.sendEquipItem(unitId, slotIndex, instanceId)"
      />
      <MatchMenuLauncher
        :active-tab="matchMenuOpen ? matchMenuTab : null"
        :abilities="ui.commanderAbilities"
        :active-ability-id="ui.commanderTargetingAbilityId"
        :items-bar-visible="itemsBarVisible"
        @open="openMenuTab"
        @cast-ability="onCommanderCast"
        @toggle-items="itemsBarVisible = !itemsBarVisible"
        @settings="matchSettingsOpen = !matchSettingsOpen"
      />
      <ItemsBar
        v-if="itemsBarVisible"
        :vault="ui.vault"
        :active-instance-id="ui.itemTargeting?.instanceId ?? null"
        @use="onItemUse"
      />
      <MatchSettingsModal
        v-if="matchSettingsOpen"
        :paused="ui.paused"
        @close="matchSettingsOpen = false"
        @toggle-pause="(next) => hud.sendSetPause(next)"
        @exit-game="() => { matchSettingsOpen = false; $emit('exit') }"
      />
      <MatchMenu
        v-if="matchMenuOpen"
        v-model:active-tab="matchMenuTab"
        :shop-catalog="ui.shopCatalog"
        :shop-rerolls-remaining="ui.shopRerollsRemaining"
        :upgrades="ui.upgrades"
        :on-purchase-upgrade="hud.purchaseUpgrade"
        :on-cancel-upgrade="hud.cancelUpgrade"
        :vault="ui.vault"
        :vault-selected-instance-id="ui.vaultSelectedInstanceId"
        :units="ui.allPlayerUnits"
        :on-select-vault-item="hud.setVaultSelectedInstanceId"
        :on-equip-item="hud.sendEquipItem"
        :on-unequip-item="hud.sendUnequipItem"
        :on-use-consumable="hud.sendUseConsumable"
        :on-transfer-item="hud.sendTransferItem"
        :on-use-item-on-unit="hud.sendUseItemOnUnit"
        :on-focus-unit="hud.focusUnit"
        :craft-catalog="ui.craftCatalog"
        :has-artificer="ui.hasArtificer"
        @close="matchMenuOpen = false"
        @purchase="({ itemId, buildingId }) => hud.sendPurchaseItem(buildingId, itemId)"
        @purchase-recipe="({ recipeId, buildingId }) => hud.sendPurchaseRecipe(buildingId, recipeId)"
        @reroll="(buildingId) => hud.rerollShop(buildingId)"
        @craft="hud.craftItem"
      />
    </div>

    <LootDropTooltip
      :drop="ui.hoveredLootDrop"
      :cursor-client-x="ui.cursorClientX"
      :cursor-client-y="ui.cursorClientY"
    />

    <DebugHud v-if="debugHudVisible" :stats="ui.netStats" />
  </div>
</template>
```

- [ ] **Step 4: Create `InGameHud.vue` — script**

Move the local UI refs, computeds, handlers, and the three keydown listeners out of `Match.vue` (verbatim bodies below). Forwarder calls inside handlers use `props.hud`.

```vue
<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import MatchHud from '@/components/MatchHud.vue'
import MatchObjectivesPanel from '@/components/match/MatchObjectivesPanel.vue'
import ZoneCapturePanel from '@/components/match/ZoneCapturePanel.vue'
import ZoneInspectionPanel from '@/components/match/ZoneInspectionPanel.vue'
import SelectionHud from '@/components/SelectionHud.vue'
import BattleTrackerPanel from '@/components/BattleTrackerPanel.vue'
import DebugSpawnPanel from '@/components/DebugSpawnPanel.vue'
import WaveUpgradeModal from '@/components/WaveUpgradeModal.vue'
import MatchMenu from '@/components/MatchMenu.vue'
import MatchMenuLauncher from '@/components/MatchMenuLauncher.vue'
import ItemsBar from '@/components/ItemsBar.vue'
import MatchSettingsModal from '@/components/MatchSettingsModal.vue'
import LootDropTooltip from '@/components/LootDropTooltip.vue'
import DebugHud from '@/components/DebugHud.vue'
import { BUILDABLE_BUILDING_DEFS } from '@/game/maps/buildingDefs'

type HudApi = ReturnType<typeof import('@/composables/useGameClient').useGameClient>

const props = defineProps<{ hud: HudApi }>()
defineEmits<{ exit: [] }>()

const ui = computed(() => props.hud.ui.value)

// ---- local HUD UI state (moved verbatim from Match.vue) ----
const itemsBarVisible = ref(true)
const matchMenuOpen = ref(false)
const matchMenuTab = ref<string>('shop')
const matchSettingsOpen = ref(false)
const debugHudVisible = ref(false)

const debugSpawnTargetingActive = computed(() => ui.value.debugSpawnTargetingActive)

const pausedByLabel = computed(() => {
  const id = ui.value.pausedBy
  if (!id) return ''
  if (ui.value.player.playerId && id === ui.value.player.playerId) {
    return 'Paused by you.'
  }
  return `Paused by ${id}.`
})

function onCommanderCast(abilityId: string) {
  if (ui.value.commanderTargetingAbilityId === abilityId) {
    props.hud.cancelCommanderAbility()
    return
  }
  props.hud.beginCommanderAbility(abilityId)
}

function onItemUse(instanceId: number, itemId: string) {
  if (ui.value.itemTargeting?.instanceId === instanceId) {
    props.hud.cancelItemUse()
    return
  }
  props.hud.beginItemUse(instanceId, itemId)
}

function openMenuTab(tabId: string) {
  if (matchMenuOpen.value && matchMenuTab.value === tabId) {
    matchMenuOpen.value = false
    return
  }
  matchMenuTab.value = tabId
  matchMenuOpen.value = true
}

// ---- menu / items / debug keyboard shortcuts (moved verbatim from Match.vue) ----
const MATCH_MENU_HOTKEYS: Record<string, string> = {
  KeyS: 'shop',
  KeyU: 'upgrades',
  KeyV: 'vault',
  KeyC: 'craft',
}

function isTextInputFocused() {
  const el = document.activeElement as HTMLElement | null
  if (!el) return false
  if (el.isContentEditable) return true
  const tag = el.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT'
}

function selectionWouldHandleKey(letter: string): boolean {
  const lower = letter.toLowerCase()
  const actions = ui.value.selection?.actions
  if (!actions || actions.length === 0) return false

  for (const def of BUILDABLE_BUILDING_DEFS) {
    if (!def.hotkey || def.hotkey.toLowerCase() !== lower) continue
    const buildSpecificId = `build-${def.type}`
    if (actions.some((a) => a.id === buildSpecificId && !a.disabled)) return true
  }

  const staticUnitHotkeys: Record<string, string> = {
    m: 'move', r: 'repair', g: 'gather', a: 'attack', h: 'hold', p: 'patrol',
  }
  const staticActionId = staticUnitHotkeys[lower]
  if (staticActionId && actions.some((a) => a.id === staticActionId && !a.disabled)) return true

  return false
}

function onMatchMenuHotkey(e: KeyboardEvent) {
  const isItemsBarKey = e.code === 'KeyI'
  if (!(e.code in MATCH_MENU_HOTKEYS) && !isItemsBarKey) return
  if (e.repeat || e.ctrlKey || e.altKey || e.metaKey || e.shiftKey) return
  if (isTextInputFocused()) return

  const letter = e.code.startsWith('Key') ? e.code.slice(3).toLowerCase() : ''
  if (letter && selectionWouldHandleKey(letter)) return

  if (isItemsBarKey) {
    itemsBarVisible.value = !itemsBarVisible.value
    e.preventDefault()
    return
  }

  const targetTab = MATCH_MENU_HOTKEYS[e.code]
  if (matchMenuOpen.value && matchMenuTab.value === targetTab) {
    matchMenuOpen.value = false
  } else {
    matchMenuTab.value = targetTab
    matchMenuOpen.value = true
  }
  e.preventDefault()
}

function onMatchMenuEscape(e: KeyboardEvent) {
  if (e.code !== 'Escape') return
  if (matchSettingsOpen.value) return
  if (!matchMenuOpen.value) return
  matchMenuOpen.value = false
  e.preventDefault()
  e.stopPropagation()
}

function onDebugHudHotkey(e: KeyboardEvent) {
  if (e.code !== 'F3') return
  if (e.repeat || e.ctrlKey || e.altKey || e.metaKey || e.shiftKey) return
  debugHudVisible.value = !debugHudVisible.value
  e.preventDefault()
}

onMounted(() => {
  window.addEventListener('keydown', onMatchMenuHotkey)
  window.addEventListener('keydown', onMatchMenuEscape, { capture: true })
  window.addEventListener('keydown', onDebugHudHotkey)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onMatchMenuHotkey)
  window.removeEventListener('keydown', onMatchMenuEscape, { capture: true })
  window.removeEventListener('keydown', onDebugHudHotkey)
})
</script>
```

Note: `onMatchMenuHotkey` drops the `if (!hasStarted.value) return` guard (there is no `hasStarted` in this component — it only exists while mounted, which is exactly "started"). The listeners now register in `onMounted`/`onBeforeUnmount` (Match.vue registered them at script top-level; a component owning its own listeners is the correct pattern and matches the eventual unmount cleanup).

- [ ] **Step 5: Create `InGameHud.vue` — styles**

The root is layout-transparent; `.match-stage` (moved from Match.vue) is the positioned canvas host. Copy the `.match-objectives-anchor` and `.pause-banner` rules verbatim from `Match.vue`'s `<style scoped>` (they style HUD pieces that moved here). Do NOT copy `.game-canvas`/`.we-play-canvas` (those style the slotted canvas and stay with each host).

```vue
<style scoped>
/* Layout-transparent: InGameHud must not introduce a containing block, or the
   real match's HUD positioning would shift. Its children participate in the
   host's layout exactly as they did inline in Match.vue. */
.in-game-hud-root { display: contents; }

/* Moved from Match.vue: the canvas host + positioned-HUD context. */
.match-stage {
  position: relative;
  flex: 1 1 auto;
  min-height: 0;
}

/* ↓ paste the exact .match-objectives-anchor and .pause-banner rules from
   Match.vue's scoped style block here (verbatim). */
</style>
```
(When implementing, read `Match.vue`'s scoped style and paste the `.match-objectives-anchor` + `.pause-banner*` rules here exactly; then delete them from `Match.vue` in Task 2.)

- [ ] **Step 6: Run tests + typecheck**

Run: `cd client/src/game-portal && npm run test -- InGameHud`
Expected: PASS (all cases).
Run: `cd client/src/game-portal && npm run build`
Expected: clean (`vue-tsc -b` enforces noUnusedLocals — ensure no unused import/local; e.g. `MatchMenuLauncher`/`MatchMenu` etc. are all referenced in the template).

- [ ] **Step 7: Commit**

```bash
git add client/src/game-portal/src/components/InGameHud.vue client/src/game-portal/src/components/InGameHud.test.ts
git commit -m "Add shared InGameHud (extracted from Match.vue)"
```

---

### Task 2: Rewire `Match.vue` to use `InGameHud`

**Files:**
- Modify: `client/src/game-portal/src/views/Match.vue`

**Interfaces:**
- Consumes: `InGameHud` (Task 1). `<InGameHud :hud="gameClientApi" @exit="requestForfeit">` with the canvas as slot content.

- [ ] **Step 1: Keep the whole `useGameClient()` handle**

In `Match.vue`, change the destructure so the full handle is retained for passing to `InGameHud` while the shell keeps the members it uses. Replace:
```ts
const {
  init, destroy, leaveStoredMatch, performSelectionAction, retryReconnect,
  beginDebugSpawn, cancelDebugSpawn, selectUnitOnly, focusUnit, deselectUnit,
  setMinimapPanelRect, sendPurchaseItem, sendPurchaseRecipe, rerollShop, craftItem,
  purchaseUpgrade, cancelUpgrade, sendEquipItem, sendUnequipItem, sendUseConsumable,
  sendTransferItem, sendUseItemOnUnit, setVaultSelectedInstanceId, sendWaveUpgradeChoice,
  sendWaveUpgradeReroll, sendSetPause, beginCommanderAbility, cancelCommanderAbility,
  beginItemUse, cancelItemUse, ui, connectionState, currentMatchId, reconnectAttempt,
  maxReconnectAttempts,
} = useGameClient()
```
with (keep only the members the SHELL still uses; expose the whole handle as `gameClientApi`):
```ts
const gameClientApi = useGameClient()
const {
  init, destroy, leaveStoredMatch, retryReconnect,
  ui, connectionState, currentMatchId, reconnectAttempt, maxReconnectAttempts,
} = gameClientApi
```
(The removed forwarders — `performSelectionAction`, `sendPurchaseItem`, `beginCommanderAbility`, etc. — were used only by the HUD, which now reaches them via `hud.*` inside `InGameHud`. `ui` stays because the shell's watchers/computeds read it and it is passed into `InGameHud` via the handle.)

- [ ] **Step 2: Delete the moved script members from `Match.vue`**

Remove from `Match.vue`'s `<script setup>` (they now live in `InGameHud`):
- HUD child imports: `MatchHud`, `MatchObjectivesPanel`, `ZoneCapturePanel`, `ZoneInspectionPanel`, `SelectionHud`, `BattleTrackerPanel`, `DebugSpawnPanel`, `WaveUpgradeModal`, `MatchMenu`, `MatchMenuLauncher`, `ItemsBar`, `MatchSettingsModal`, `LootDropTooltip`, `DebugHud`. (KEEP `CampaignVictoryModal` — shell.)
- Local refs: `itemsBarVisible`, `matchMenuOpen`, `matchMenuTab`, `matchSettingsOpen`, `debugHudVisible`.
- Computeds: `debugSpawnTargetingActive`, `pausedByLabel`.
- Handlers: `onCommanderCast`, `onItemUse`, `openMenuTab`.
- Keyboard: `MATCH_MENU_HOTKEYS`, `isTextInputFocused`, `selectionWouldHandleKey`, `onMatchMenuHotkey`, `onMatchMenuEscape`, `onDebugHudHotkey`, AND their three `window.addEventListener('keydown', …)` lines (keep the `beforeunload`/`markActiveSession` listener) and the three matching `removeEventListener` lines in `onBeforeUnmount` (keep `destroy()` + `setCursorGrab(false)` + the beforeunload removal).
- If `BUILDABLE_BUILDING_DEFS` is now unused in `Match.vue`, remove its import (it moved to `InGameHud`). Verify with a grep before removing.

- [ ] **Step 3: Replace the HUD template block in `Match.vue`**

In `Match.vue`'s template, remove the entire HUD block (MatchHud, objectives-anchor, WaveUpgradeModal, BattleTrackerPanel, DebugSpawnPanel, pause-banner, the whole `.match-stage` div with its 5 HUD children, LootDropTooltip, DebugHud) and replace the `.match-stage` region with `<InGameHud>` wrapping the canvas. KEEP the resume prompt, `CampaignVictoryModal`, and the disconnect-overlay exactly as they are. Result (the middle of the template):
```vue
    <!-- resume prompt stays here, unchanged -->

    <CampaignVictoryModal
      v-if="hasStarted && victoryPopupOpen"
      @continue="onCampaignVictoryContinue"
      @exit="onCampaignVictoryExit"
    />

    <!-- disconnect-overlay stays here, unchanged -->

    <InGameHud v-if="hasStarted" :hud="gameClientApi" @exit="requestForfeit">
      <canvas ref="canvas" class="game-canvas"></canvas>
    </InGameHud>
```
Import `InGameHud` (`import InGameHud from '@/components/InGameHud.vue'`). Because `InGameHud`'s root is `display: contents`, `MatchHud` (in-flow) and `.match-stage` (flex child) participate in `.match-view`'s flex column exactly as before; the `<canvas ref="canvas">` stays owned by `Match.vue` (the ref resolves in this component's scope) and `init(canvas.value, …)` is unchanged.

- [ ] **Step 4: Delete the moved styles from `Match.vue`**

Remove the `.match-objectives-anchor`, `.pause-banner*`, and `.match-stage` rules from `Match.vue`'s `<style scoped>` (they moved to `InGameHud`). KEEP `.match-view`, `.game-canvas`, the disconnect-overlay styles, the resume `.menu` styles, and everything else the shell renders.

- [ ] **Step 5: Build + full suite + commit**

Run: `cd client/src/game-portal && npm run build`
Expected: clean (no unused imports/locals in `Match.vue`).
Run: `cd client/src/game-portal && npm run test`
Expected: green (whole suite).
```bash
git add client/src/game-portal/src/views/Match.vue
git commit -m "Match.vue: render the shared InGameHud (HUD extracted)"
```
(The real-match manual E2E is the hard gate in Task 4 — do NOT declare the match verified from unit tests alone.)

---

### Task 3: Rewire the editor playtest to use `InGameHud`

**Files:**
- Modify: `client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue`
- Delete: `client/src/game-portal/src/components/world-editor/PlaytestHud.vue`
- Delete: `client/src/game-portal/src/components/world-editor/PlaytestHud.test.ts`

**Interfaces:**
- Consumes: `InGameHud` (Task 1); `usePlaytest()`'s `gameClient` handle + `playCanvas` ref (unchanged).

- [ ] **Step 1: Replace the play-canvas + PlaytestHud region with an InGameHud stage**

In `WorldEditorPanel.vue`, find the current play region (the `<canvas v-show="playtestPlaying" ref="playCanvas" class="we-play-canvas">`, the `<PlaytestHud … />`, and the `<PlaytestBar … />` inside `.canvas-frame`). Replace the play canvas + PlaytestHud with a flex-column stage that hosts `InGameHud` (mirroring `.match-view`), keeping the canvas as slot content, and keep `PlaytestBar`:
```vue
        <div v-if="playtestPlaying" class="playtest-stage">
          <InGameHud :hud="playtestGameClient" @exit="stopPlaytest">
            <canvas ref="playCanvas" class="we-play-canvas"></canvas>
          </InGameHud>
        </div>
        <PlaytestBar
          v-if="playtestPlaying"
          :paused="playtestPaused"
          @toggle-pause="togglePlaytestPause"
          @reset="stopPlaytest"
        />
```
NOTE: `playCanvas` stays a `WorldEditorPanel` ref (slot content), so `usePlaytest(() => playCanvas.value)` is unchanged. The editor placement canvas (`<canvas v-show="!playtestPlaying" ref="canvas" class="editor-canvas">`) and the editor minimap stay as they are.

- [ ] **Step 2: Swap the import; add the `.playtest-stage` style; fix the play-canvas z-index**

Replace the `PlaytestHud` import with `InGameHud`:
```ts
import InGameHud from '@/components/InGameHud.vue'
```
(remove `import PlaytestHud from '@/components/world-editor/PlaytestHud.vue'`).
Add the `.playtest-stage` rule (a `.match-view`-style flex column filling `.canvas-frame`, above the hidden editor canvas, below the `PlaytestBar` at z-index 30):
```css
.playtest-stage {
  position: absolute;
  inset: 0;
  z-index: 26;
  display: flex;
  flex-direction: column;
  min-height: 0;
}
```
Change `.we-play-canvas` to drop its `z-index: 25` (the canvas now lives inside `InGameHud`'s `.match-stage`, and must sit UNDER the HUD panels exactly like the real match's `.game-canvas`, which has no z-index):
```css
.we-play-canvas {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  display: block;
  background: #0a0a0a;
}
```
(remove the `z-index: 25;` line only; keep the rest.)

- [ ] **Step 3: Delete `PlaytestHud.vue` + its test**

```bash
git rm client/src/game-portal/src/components/world-editor/PlaytestHud.vue client/src/game-portal/src/components/world-editor/PlaytestHud.test.ts
```

- [ ] **Step 4: Build + commit**

Run: `cd client/src/game-portal && npm run build`
Expected: clean (no dangling `PlaytestHud` import/reference).
Run: `cd client/src/game-portal && npm run test`
Expected: green (the deleted PlaytestHud suite is gone; usePlaytest + InGameHud suites pass).
```bash
git add client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue
git commit -m "World editor: playtest uses the shared InGameHud (full match parity)"
```

---

### Task 4: Verification sweep + manual E2E gates

**Files:** fixes only.

- [ ] **Step 1: Full client gates**

Run: `cd client/src/game-portal && npm run test`
Expected: green (InGameHud + usePlaytest suites; no PlaytestHud suite).
Run: `cd client/src/game-portal && npm run build`
Expected: clean.

- [ ] **Step 2: No literal cursor / no leftover PlaytestHud**

Run: `cd "$(git rev-parse --show-toplevel)" && grep -n "cursor:" client/src/game-portal/src/components/InGameHud.vue || echo "none (good)"`
Expected: `none (good)`.
Run: `cd "$(git rev-parse --show-toplevel)" && grep -rn "PlaytestHud" client/src/game-portal/src || echo "no references (good)"`
Expected: `no references (good)`.

- [ ] **Step 3: Manual E2E — real match (HARD GATE)**

Start a real game (`/match` via Custom Game or Campaign). Verify the live game is UNCHANGED: resource bar + selection panel + minimap position identically; open the match menu (S/U/V/C hotkeys + launcher), buy from shop, craft, equip from vault, open settings (pause + exit-to-results works), wave-upgrade modal appears on a wave, disconnect overlay + resume prompt still function. Any layout shift or broken control here is a blocking regression — fix before proceeding.

- [ ] **Step 4: Manual E2E — playtest**

World editor → drop units → Play. Verify FULL parity: resource bar, selection panel + stats + commands, minimap, ability bar, shop/vault/upgrades menu, items bar, settings, and waves running. Exit via settings "exit game" OR the `↺ Reset` bar → both return to the editor. Confirm (server logs / profile) NO Dominion Points or recipes persisted from the playtest.

- [ ] **Step 5: Commit any fixes**

```bash
git add -A client/
git commit -m "Shared in-game HUD: verification fixes"
```
(Skip if clean.)
