<template>
  <div class="in-game-hud-root">
    <MatchHud v-if="active" :ui="ui" />

    <div
      v-if="active && (ui.objectives.length || ui.zoneCaptureCards.length || ui.zoneInspection)"
      class="match-objectives-anchor"
    >
      <MatchObjectivesPanel v-if="ui.objectives.length" :objectives="ui.objectives" />
      <ZoneCapturePanel v-if="ui.zoneCaptureCards.length" :cards="ui.zoneCaptureCards" />
      <ZoneInspectionPanel v-if="ui.zoneInspection" :info="ui.zoneInspection" />
    </div>

    <WaveUpgradeModal
      v-if="active && ui.waveUpgrade"
      :upgrade="ui.waveUpgrade!"
      :units="ui.allPlayerUnits.filter(u => u.unitType !== 'worker')"
      :send-choice="hud.sendWaveUpgradeChoice"
      :send-reroll="hud.sendWaveUpgradeReroll"
      :paused="ui.paused"
      :paused-since-ms="ui.pausedSinceMs"
    />

    <BattleTrackerPanel v-if="active" :ui="ui" />

    <DebugSpawnPanel
      v-if="active"
      :ui="ui"
      :targeting-active="debugSpawnTargetingActive"
      :begin-debug-spawn="hud.beginDebugSpawn"
      :cancel-debug-spawn="hud.cancelDebugSpawn"
    />

    <div v-if="active && ui.paused" class="pause-banner" role="status" aria-live="polite">
      <div class="pause-banner__title">Game Paused</div>
      <div class="pause-banner__sub">{{ pausedByLabel }} Open Settings to resume.</div>
    </div>

    <div class="match-stage">
      <slot />
      <SelectionHud
        v-if="active"
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
        v-if="active"
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
        v-if="active && itemsBarVisible"
        :vault="ui.vault"
        :active-instance-id="ui.itemTargeting?.instanceId ?? null"
        @use="onItemUse"
      />
      <MatchSettingsModal
        v-if="active && matchSettingsOpen"
        :paused="ui.paused"
        @close="matchSettingsOpen = false"
        @toggle-pause="(next) => hud.sendSetPause(next)"
        @exit-game="() => { matchSettingsOpen = false; $emit('exit') }"
      />
      <MatchMenu
        v-if="active && matchMenuOpen"
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
      v-if="active"
      :drop="ui.hoveredLootDrop"
      :cursor-client-x="ui.cursorClientX"
      :cursor-client-y="ui.cursorClientY"
    />

    <DebugHud v-if="active && debugHudVisible" :stats="ui.netStats" />
  </div>
</template>

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

const props = defineProps<{ hud: HudApi; active: boolean }>()
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
  if (!props.active) return
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
  if (!props.active) return
  if (e.code !== 'Escape') return
  if (matchSettingsOpen.value) return
  if (!matchMenuOpen.value) return
  matchMenuOpen.value = false
  e.preventDefault()
  e.stopPropagation()
}

function onDebugHudHotkey(e: KeyboardEvent) {
  if (!props.active) return
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

/* Anchor for the campaign objectives HUD panel. Sits under the resource
   tray (top-right). The MatchHud header reserves ~64px at the top; we
   start just below that. Pointer-events handled inside the panel itself. */
.match-objectives-anchor {
  position: absolute;
  top: 70px;
  right: 16px;
  z-index: 15;
  pointer-events: none;
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: flex-end;
}

.pause-banner {
  position: absolute;
  top: 24px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 30;
  padding: 14px 28px;
  border-radius: 10px;
  background: linear-gradient(180deg, rgba(15, 23, 42, 0.92), rgba(8, 12, 20, 0.96));
  border: 1px solid rgba(220, 180, 100, 0.45);
  color: #f5ead2;
  text-align: center;
  pointer-events: none;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.55);
}

.pause-banner__title {
  font-size: 18px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #f7d88e;
}

.pause-banner__sub {
  margin-top: 4px;
  font-size: 12px;
  letter-spacing: 0.04em;
  color: #cbb893;
}
</style>
