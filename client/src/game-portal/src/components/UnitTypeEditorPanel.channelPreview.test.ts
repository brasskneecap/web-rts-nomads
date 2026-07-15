import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UnitTypeEditorPanel from './UnitTypeEditorPanel.vue'
import {
  clearRuntimeSpriteSets,
  registerRuntimeSpriteSet,
  type StripAnimation,
  type UnitSpriteSet,
} from '@/game/rendering/unitSprites'

// The panel derives, per selected unit, whether it has a channelling ability
// (by cross-referencing the unit's ability ids against the ability catalog's
// channelType) and hands the channel-loop info to the sprite preview. These
// tests pin that wiring end-to-end: catalog -> derivation -> preview control.

function castingSet(key: string): UnitSpriteSet {
  const casting: StripAnimation = {
    frameCount: 6,
    frameWidth: 64,
    frameHeight: 64,
    directions: {} as StripAnimation['directions'],
  }
  return {
    key,
    size: { width: 64, height: 64 },
    rotations: {} as UnitSpriteSet['rotations'],
    animations: new Map([['casting', casting]]),
    beamOrigin: { x: 0, y: 0 },
  }
}

function stubApi() {
  const map: Record<string, unknown> = {
    '/catalog/units': {
      units: [
        { type: 'siphoner', name: 'Siphoner', faction: 'human', hp: 120, damage: 0, moveSpeed: 100, capabilities: ['move'], abilities: ['siphon_life'], channelLoop: { start: 3, end: 5 } },
        { type: 'soldier', name: 'Soldier', faction: 'human', hp: 175, damage: 12, attackRange: 60, attackSpeed: 1, moveSpeed: 100, capabilities: ['move', 'attack'] },
      ],
    },
    '/catalog/paths': { paths: [] },
    '/catalog/factions': { factions: [{ id: 'human', displayName: 'Human' }] },
    '/catalog/archetypes': { archetypes: [] },
    '/catalog/projectiles': { projectiles: [] },
    // Full ability defs (not just ids) so the panel can spot the channel one.
    '/catalog/abilities': {
      abilities: [
        { id: 'siphon_life', channelType: 'beam', tickIntervalSeconds: 0.5 },
        { id: 'arcane_bolt' },
      ],
    },
    '/catalog/damage-types': { damageTypes: [] },
    '/catalog/buildings': { buildings: [] },
    '/catalog/perks': { perks: [] },
  }
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const method = (init?.method ?? 'GET').toUpperCase()
    if (method === 'GET') {
      const key = Object.keys(map).find((k) => String(url).endsWith(k))
      return { ok: true, status: 200, json: async () => map[key ?? ''] ?? {} }
    }
    return { ok: true, status: 201, json: async () => ({ status: 'saved' }) }
  }) as unknown as typeof fetch)
}

function findButtonByText(wrapper: ReturnType<typeof mount>, text: string) {
  const btn = wrapper.findAll('button').find((b) => b.text() === text)
  if (!btn) throw new Error(`no button with text "${text}"`)
  return btn
}

function channelToggle(wrapper: ReturnType<typeof mount>) {
  return wrapper.findAll('button').find((b) => b.text().includes('Channel Loop'))
}

beforeEach(() => {
  vi.stubGlobal('requestAnimationFrame', () => 1)
  vi.stubGlobal('cancelAnimationFrame', () => {})
  registerRuntimeSpriteSet(castingSet('siphoner'))
  registerRuntimeSpriteSet(castingSet('soldier'))
})

afterEach(() => {
  clearRuntimeSpriteSets()
  vi.restoreAllMocks()
})

describe('UnitTypeEditorPanel — channel-loop preview wiring', () => {
  it('offers the Channel Loop control for a unit with a channelling ability', async () => {
    stubApi()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await findButtonByText(wrapper, 'Siphoner').trigger('click')
    await flushPromises()

    expect(channelToggle(wrapper), 'a channelling unit should offer the Channel Loop control').toBeTruthy()
  })

  it('does not offer it for a unit with no channelling ability', async () => {
    stubApi()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await findButtonByText(wrapper, 'Soldier').trigger('click')
    await flushPromises()

    expect(channelToggle(wrapper), 'a non-channelling unit should not offer the control').toBeFalsy()
  })
})
