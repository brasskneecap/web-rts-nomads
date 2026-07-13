import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { shallowMount } from '@vue/test-utils'
import PlaytestHud from './PlaytestHud.vue'
import MatchHud from '@/components/MatchHud.vue'
import SelectionHud from '@/components/SelectionHud.vue'
import CommanderActionBar from '@/components/CommanderActionBar.vue'

function mkHud(overrides: Record<string, unknown> = {}) {
  const ui = ref({
    selectedUnits: [],
    selection: { kind: 'none', title: '', subtitle: '', details: [], actions: [] },
    commanderAbilities: [],
    commanderTargetingAbilityId: null as string | null,
    objectives: [],
    zoneCaptureCards: [],
    zoneInspection: null,
    paused: false,
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
    beginCommanderAbility: vi.fn(),
    cancelCommanderAbility: vi.fn(),
  }
}

describe('PlaytestHud', () => {
  it('renders the core HUD components fed by the hud ui', () => {
    const hud = mkHud()
    const wrapper = shallowMount(PlaytestHud, { props: { hud: hud as any } })
    expect(wrapper.findComponent(MatchHud).exists()).toBe(true)
    expect(wrapper.findComponent(SelectionHud).exists()).toBe(true)
    expect(wrapper.findComponent(CommanderActionBar).exists()).toBe(true)
    expect(wrapper.findComponent(SelectionHud).props('ui')).toBe(hud.ui.value)
  })

  it('forwards a selection action to the client', () => {
    const hud = mkHud()
    const wrapper = shallowMount(PlaytestHud, { props: { hud: hud as any } })
    wrapper.findComponent(SelectionHud).vm.$emit('action', 'move')
    expect(hud.performSelectionAction).toHaveBeenCalledWith('move')
  })

  it('casts a commander ability, toggling cancel when already targeting it', () => {
    const hud = mkHud()
    const wrapper = shallowMount(PlaytestHud, { props: { hud: hud as any } })
    wrapper.findComponent(CommanderActionBar).vm.$emit('cast', 'fireball')
    expect(hud.beginCommanderAbility).toHaveBeenCalledWith('fireball')

    hud.ui.value.commanderTargetingAbilityId = 'fireball'
    wrapper.findComponent(CommanderActionBar).vm.$emit('cast', 'fireball')
    expect(hud.cancelCommanderAbility).toHaveBeenCalledTimes(1)
  })
})
