import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ItemCatalogEditor from './ItemCatalogEditor.vue'

const LISTS = [
  { id: 'basic_weapons', name: 'Basic Weapons', maxRoll: 15, entries: [
    { item: 'broad_sword', min: 1, max: 10 }, { item: 'scimitar', min: 11, max: 15 },
  ] },
]
const TABLES = [
  { id: 'raider_loot', name: 'Raider Loot', maxRoll: 100, rows: [
    { min: 1, max: 50, resources: { gold: 50, wood: 15 } },
    { min: 51, max: 60, nothing: true },
    { min: 61, max: 100, list: 'basic_weapons' },
  ] },
]

const posted: unknown[] = []

function stubFetch() {
  posted.length = 0
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    if (init?.method === 'POST' && u.endsWith('/tables')) {
      posted.push(JSON.parse(String(init.body)))
      return { ok: true, status: 201, json: async () => ({ status: 'saved' }) }
    }
    const map: Record<string, unknown> = {
      '/catalog/items': { items: [] },
      '/catalog/lists': { lists: LISTS },
      '/catalog/tables': { tables: TABLES },
      '/catalog/procs': { procs: [] },
    }
    const key = Object.keys(map).find((k) => u.endsWith(k))
    return { ok: true, status: 200, json: async () => map[key ?? ''] ?? {} }
  }) as unknown as typeof fetch)
}

afterEach(() => vi.restoreAllMocks())

async function mountOnTablesTab() {
  stubFetch()
  const wrapper = mount(ItemCatalogEditor)
  await flushPromises()
  await wrapper.findAll('[role="tab"]').find((t) => t.text() === 'Tables')!.trigger('click')
  await flushPromises()
  return wrapper
}

describe('TableEditorPanel', () => {
  it('lists the catalog tables in the sidebar', async () => {
    const wrapper = await mountOnTablesTab()
    expect(wrapper.text()).toContain('Raider Loot')
  })

  it('a loaded, fully-covered table validates as complete', async () => {
    const wrapper = await mountOnTablesTab()
    await wrapper.findAll('button').find((b) => b.text() === 'Raider Loot')!.trigger('click')
    await flushPromises()

    // No coverage strip any more — the checklist carries it.
    expect(wrapper.text()).toContain('The die is fully covered.')
    const save = wrapper.findAll('button').find((b) => b.text() === 'Save')!
    expect((save.element as HTMLButtonElement).disabled).toBe(false)
  })

  it('blocks a save when the die has a gap, and says which rolls', async () => {
    const wrapper = await mountOnTablesTab()
    await wrapper.findAll('button').find((b) => b.text() === 'Add New Table')!.trigger('click')
    await wrapper.find('#te-name').setValue('Gappy')
    // One nothing row 1–50 on a d100 → 51–100 uncovered.
    await wrapper.find('#te-maxroll').setValue('100')

    const rowMaxes = wrapper.findAll('input[type="number"]')
    // Find the row's max input (aria-label "Row 1 max") and set it to 50.
    const rowMax = wrapper.findAll('input').find((i) => i.attributes('aria-label') === 'Row 1 max')!
    await rowMax.setValue('50')
    void rowMaxes

    expect(wrapper.text()).toContain('land on nothing')
    const save = wrapper.findAll('button').find((b) => b.text() === 'Save')!
    expect((save.element as HTMLButtonElement).disabled).toBe(true)
  })

  it('saves a complete table', async () => {
    const wrapper = await mountOnTablesTab()
    await wrapper.findAll('button').find((b) => b.text() === 'Add New Table')!.trigger('click')
    await wrapper.find('#te-name').setValue('Simple')
    // New table starts with one nothing row covering 1..100 — already complete.
    const save = wrapper.findAll('button').find((b) => b.text() === 'Save')!
    expect((save.element as HTMLButtonElement).disabled).toBe(false)
    await save.trigger('click')
    await flushPromises()

    expect(posted).toEqual([{
      table: { id: 'simple', name: 'Simple', maxRoll: 100, rows: [{ min: 1, max: 100, nothing: true }] },
    }])
  })
})
