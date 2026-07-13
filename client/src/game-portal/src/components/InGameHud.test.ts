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
    const w = shallowMount(InGameHud, { props: { hud: hud as any, active: true } })
    expect(w.findComponent(MatchHud).exists()).toBe(true)
    expect(w.findComponent(SelectionHud).exists()).toBe(true)
    expect(w.findComponent(MatchMenuLauncher).exists()).toBe(true)
    expect(w.findComponent(SelectionHud).props('ui')).toBe(hud.ui.value)
  })

  it('renders the game canvas passed via the default slot', () => {
    const hud = mkHud()
    const w = shallowMount(InGameHud, {
      props: { hud: hud as any, active: true },
      slots: { default: '<canvas class="test-canvas"></canvas>' },
    })
    expect(w.find('canvas.test-canvas').exists()).toBe(true)
  })

  it('renders the canvas slot but no HUD when inactive', () => {
    const hud = mkHud()
    const w = shallowMount(InGameHud, {
      props: { hud: hud as any, active: false },
      slots: { default: '<canvas class="test-canvas"></canvas>' },
    })
    expect(w.find('canvas.test-canvas').exists()).toBe(true)
    expect(w.findComponent(MatchHud).exists()).toBe(false)
    expect(w.findComponent(SelectionHud).exists()).toBe(false)
  })

  it('opens the match menu on a tab, forwards a shop purchase, and forwards craft', async () => {
    const hud = mkHud()
    const w = shallowMount(InGameHud, { props: { hud: hud as any, active: true } })
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
    const w = shallowMount(InGameHud, { props: { hud: hud as any, active: true } })
    w.findComponent(SelectionHud).vm.$emit('action', 'move')
    expect(hud.performSelectionAction).toHaveBeenCalledWith('move')
    w.findComponent(MatchMenuLauncher).vm.$emit('cast-ability', 'fireball')
    expect(hud.beginCommanderAbility).toHaveBeenCalledWith('fireball')
  })

  it('opens settings, and its exit-game bubbles as the component exit emit', async () => {
    const hud = mkHud()
    const w = shallowMount(InGameHud, { props: { hud: hud as any, active: true } })
    w.findComponent(MatchMenuLauncher).vm.$emit('settings')
    await w.vm.$nextTick()
    expect(w.findComponent(MatchSettingsModal).exists()).toBe(true)
    w.findComponent(MatchSettingsModal).vm.$emit('exit-game')
    expect(w.emitted('exit')).toBeTruthy()
  })
})
