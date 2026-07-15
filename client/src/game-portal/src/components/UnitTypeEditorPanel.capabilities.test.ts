import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UnitTypeEditorPanel from './UnitTypeEditorPanel.vue'

// Capabilities are no longer authored directly — the form derives them from
// intent flags (move speed, non-combat, can-gather, builder). These tests pin
// that derivation, both on LOAD (flags reflect an existing unit's capabilities)
// and on SAVE (the persisted capabilities list is rebuilt from the flags).
interface RecordedCall { method: string; url: string; body?: unknown }

function stubApi(getOverrides: Record<string, unknown> = {}) {
  const calls: RecordedCall[] = []
  const map: Record<string, unknown> = {
    '/catalog/units': {
      units: [
        { type: 'soldier', name: 'Soldier', faction: 'human', hp: 175, damage: 12, attackRange: 60, attackSpeed: 1, moveSpeed: 100, capabilities: ['move', 'attack'] },
        { type: 'worker', name: 'Worker', faction: 'human', hp: 100, damage: 5, attackRange: 20, attackSpeed: 1, moveSpeed: 100, goldGatherAmount: 20, woodGatherAmount: 15, capabilities: ['move', 'gather', 'build', 'attack'] },
      ],
    },
    '/catalog/paths': { paths: [] },
    '/catalog/factions': { factions: [{ id: 'human', displayName: 'Human' }] },
    '/catalog/archetypes': { archetypes: [] },
    '/catalog/projectiles': { projectiles: [] },
    '/catalog/abilities': { abilities: [] },
    '/catalog/damage-types': { damageTypes: [] },
    '/catalog/buildings': { buildings: [] },
    '/catalog/perks': { perks: [] },
    ...getOverrides,
  }
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const method = (init?.method ?? 'GET').toUpperCase()
    const body = init?.body ? JSON.parse(init.body as string) : undefined
    calls.push({ method, url: String(url), body })
    if (method === 'GET') {
      const key = Object.keys(map).find((k) => String(url).endsWith(k))
      return { ok: true, status: 200, json: async () => map[key ?? ''] ?? {} }
    }
    return { ok: true, status: method === 'POST' ? 201 : 200, json: async () => ({ status: 'saved' }) }
  }) as unknown as typeof fetch)
  return calls
}

function findButtonByText(wrapper: ReturnType<typeof mount>, text: string) {
  const btn = wrapper.findAll('button').find((b) => b.text() === text)
  if (!btn) throw new Error(`no button with text "${text}"`)
  return btn
}

function checked(wrapper: ReturnType<typeof mount>, selector: string): boolean {
  return (wrapper.find(selector).element as HTMLInputElement).checked
}

afterEach(() => vi.restoreAllMocks())

describe('UnitTypeEditorPanel — capability flags (load)', () => {
  it('a gatherer/builder loads with Can-gather + Builder on and shows gather amounts', async () => {
    stubApi()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await findButtonByText(wrapper, 'Worker').trigger('click')

    expect(checked(wrapper, '#ue-cangather')).toBe(true)
    expect(checked(wrapper, '#ue-builder')).toBe(true)
    // Gather amount fields are revealed by the Can-gather flag.
    expect(wrapper.find('#ue-gold-gather').exists()).toBe(true)
    expect(wrapper.find('#ue-wood-gather').exists()).toBe(true)
  })

  it('a plain combat unit loads with those flags off and hides gather amounts', async () => {
    stubApi()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await findButtonByText(wrapper, 'Soldier').trigger('click')

    expect(checked(wrapper, '#ue-cangather')).toBe(false)
    expect(checked(wrapper, '#ue-builder')).toBe(false)
    expect(wrapper.find('#ue-gold-gather').exists()).toBe(false)
  })
})

describe('UnitTypeEditorPanel — capability flags (save)', () => {
  it('rebuilds the capabilities list from the flags on save', async () => {
    const calls = stubApi()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await findButtonByText(wrapper, 'Soldier').trigger('click')

    // Flag it non-combat (drops attack) and a builder (adds build). Move speed
    // stays 100, so move survives; can-gather stays off, so no gather.
    await wrapper.find('#ue-noncombat').setValue(true)
    await wrapper.find('#ue-builder').setValue(true)

    await findButtonByText(wrapper, 'Save').trigger('click')
    await flushPromises()

    const post = calls.find((c) => c.method === 'POST' && c.url.endsWith('/units'))
    expect(post).toBeDefined()
    const caps = (post!.body as { unit: { capabilities?: string[] } }).unit.capabilities ?? []
    expect(caps).toContain('move')
    expect(caps).toContain('build')
    expect(caps).not.toContain('attack')
    expect(caps).not.toContain('gather')
  })
})
