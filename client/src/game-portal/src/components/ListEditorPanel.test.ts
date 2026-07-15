import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ItemCatalogEditor from './ItemCatalogEditor.vue'

const ITEMS = [
  { id: 'broad_sword', displayName: 'Broad Sword', iconKey: 'broad_sword', kind: 'equipment', tier: 'common', costGold: 50 },
  { id: 'potion', displayName: 'Potion', iconKey: 'potion', kind: 'consumable', tier: 'common', costGold: 20 },
  {
    id: 'fire_sword', displayName: 'Fire Sword', iconKey: 'fire_sword', kind: 'equipment', tier: 'rare', costGold: 0,
    crafting: { inputs: ['broad_sword', 'fire_ring'], craftCostGold: 150, recipeCostGold: 300 },
  },
]

const LISTS = [{ id: 'marketplace', name: 'Marketplace', items: ['broad_sword'] }]

// Captures what the panel POSTs so a save can be asserted end-to-end.
const posted: unknown[] = []

function stubFetch() {
  posted.length = 0
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    if (init?.method === 'POST' && u.endsWith('/lists')) {
      posted.push(JSON.parse(String(init.body)))
      return { ok: true, status: 201, json: async () => ({ status: 'saved' }) }
    }
    const map: Record<string, unknown> = {
      '/catalog/items': { items: ITEMS },
      '/catalog/lists': { lists: LISTS },
      '/catalog/tables': { tables: [] },
      '/catalog/procs': { procs: [] },
    }
    const key = Object.keys(map).find((k) => u.endsWith(k))
    return { ok: true, status: 200, json: async () => map[key ?? ''] ?? {} }
  }) as unknown as typeof fetch)
}

afterEach(() => vi.restoreAllMocks())

async function mountOnListsTab() {
  stubFetch()
  const wrapper = mount(ItemCatalogEditor)
  await flushPromises()
  const listsTab = wrapper.findAll('[role="tab"]').find((t) => t.text() === 'Lists')!
  await listsTab.trigger('click')
  await flushPromises()
  return wrapper
}

describe('ItemCatalogEditor — Items | Lists tabs', () => {
  it('opens on Items and switches to Lists', async () => {
    stubFetch()
    const wrapper = mount(ItemCatalogEditor)
    await flushPromises()

    const tabs = wrapper.findAll('[role="tab"]')
    expect(tabs.map((t) => t.text())).toEqual(['Items', 'Lists', 'Tables'])
    expect(tabs[0].attributes('aria-selected')).toBe('true')

    await tabs[1].trigger('click')
    expect(wrapper.findAll('[role="tab"]')[1].attributes('aria-selected')).toBe('true')
  })

  // The active tab REPLACES the work surface — it does not sit beside it. Both
  // panels stay mounted (so an in-progress edit survives a tab switch), which
  // means "which one is showing" is a question about display, not presence.
  //
  // Asserted on the panes' own display style rather than wrapper.isVisible():
  // the panels sit INSIDE the hidden pane, and isVisible() does not reliably
  // walk up to a display:none ancestor in this setup — it would report both
  // panels visible and quietly pass even when the layout is broken.
  it('shows exactly one panel at a time', async () => {
    stubFetch()
    const wrapper = mount(ItemCatalogEditor)
    await flushPromises()

    const hidden = () =>
      wrapper.findAll('.item-catalog-editor__pane')
        .map((p) => (p.attributes('style') ?? '').includes('display: none'))

    // Items tab: only the items pane shows.
    expect(hidden()).toEqual([false, true, true])

    await wrapper.findAll('[role="tab"]').find((t) => t.text() === 'Lists')!.trigger('click')
    expect(hidden()).toEqual([true, false, true])

    await wrapper.findAll('[role="tab"]').find((t) => t.text() === 'Tables')!.trigger('click')
    expect(hidden()).toEqual([true, true, false])
  })
})

describe('ListEditorPanel', () => {
  it('lists the catalog lists in the sidebar', async () => {
    const wrapper = await mountOnListsTab()
    expect(wrapper.text()).toContain('Marketplace')
  })

  it('creates a list and POSTs it', async () => {
    const wrapper = await mountOnListsTab()
    await wrapper.findAll('button').find((b) => b.text() === 'Add New List')!.trigger('click')

    await wrapper.find('#le-name').setValue('Elemental Gear')
    // The id slugs itself from the name, like the item editor.
    expect((wrapper.find('#le-id').element as HTMLInputElement).value).toBe('elemental_gear')

    await wrapper.find('#le-item-0').setValue('fire_sword')
    await wrapper.findAll('button').find((b) => b.text() === '+ Add Item')!.trigger('click')
    await wrapper.find('#le-item-1').setValue('broad_sword')

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()

    expect(posted).toEqual([{
      list: { id: 'elemental_gear', name: 'Elemental Gear', items: ['fire_sword', 'broad_sword'] },
    }])
  })

  it('refuses to save an empty list', async () => {
    const wrapper = await mountOnListsTab()
    await wrapper.findAll('button').find((b) => b.text() === 'Add New List')!.trigger('click')
    await wrapper.find('#le-name').setValue('Empty')

    expect(wrapper.text()).toContain('A list needs at least one item.')
    const save = wrapper.findAll('button').find((b) => b.text() === 'Save')!
    expect((save.element as HTMLButtonElement).disabled).toBe(true)
  })

  // The warning is the safety net for an UNTYPED list: it says what will happen
  // and lets the author decide, because the same list can be nonsense on an
  // Artificer and exactly right as a loot pool.
  it('warns about non-craftable members but still allows the save', async () => {
    const wrapper = await mountOnListsTab()
    await wrapper.findAll('button').find((b) => b.text() === 'Add New List')!.trigger('click')
    await wrapper.find('#le-name').setValue('Mixed Bag')
    await wrapper.find('#le-item-0').setValue('broad_sword') // not craftable

    expect(wrapper.text()).toContain('not craftable')
    expect(wrapper.text()).toContain('will ignore them')

    // Warned, not blocked.
    const save = wrapper.findAll('button').find((b) => b.text() === 'Save')!
    expect((save.element as HTMLButtonElement).disabled).toBe(false)
    await save.trigger('click')
    await flushPromises()
    expect(posted).toHaveLength(1)
  })

  it('shows no warning when every member is craftable', async () => {
    const wrapper = await mountOnListsTab()
    await wrapper.findAll('button').find((b) => b.text() === 'Add New List')!.trigger('click')
    await wrapper.find('#le-name').setValue('Craftables')
    await wrapper.find('#le-item-0').setValue('fire_sword')

    expect(wrapper.text()).not.toContain('not craftable')
  })
})
