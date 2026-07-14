import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ItemEditorPanel from './ItemEditorPanel.vue'

// Two ingredients the crafting dropdowns can select.
const INGREDIENTS = [
  { id: 'iron_ingot', displayName: 'Iron Ingot', iconKey: 'iron_ingot', kind: 'equipment', tier: 'common', costGold: 10 },
  { id: 'wooden_hilt', displayName: 'Wooden Hilt', iconKey: 'wooden_hilt', kind: 'equipment', tier: 'common', costGold: 5 },
]

// Stub every catalog fetch the panel makes on mount, keyed by URL suffix.
function stubCatalogFetch(items: unknown[] = []) {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    const map: Record<string, unknown> = {
      '/catalog/items': { items },
      '/catalog/recipes': { recipes: [] },
      '/catalog/procs': {
        procs: [
          { id: 'fire_bolt_ignite', damage: 25, damageType: 'fire', projectileID: 'fire_bolt', burnDamagePerSecond: 8 },
          { id: 'lightning_chain', damage: 30, damageType: 'lightning', projectileID: 'lightning_bolt', bounceCount: 2 },
        ],
      },
    }
    const key = Object.keys(map).find((k) => String(url).endsWith(k))
    return { ok: true, status: 200, json: async () => map[key ?? ''] ?? {} }
  }) as unknown as typeof fetch)
}

afterEach(() => vi.restoreAllMocks())

// Opens a blank item — the panel's starting state for authoring procs. Every
// section is visible at once now (no accordion), so there is nothing to expand.
async function mountWithNewItem(items: unknown[] = []) {
  stubCatalogFetch(items)
  const wrapper = mount(ItemEditorPanel)
  await flushPromises()
  const newItem = wrapper.findAll('button').find((b) => b.text() === 'Add New Item')
  await newItem!.trigger('click')
  return wrapper
}

/** A blank item, with two other items in the catalog to use as ingredients. */
const mountWithCraftableCatalog = () => mountWithNewItem(INGREDIENTS)

const addProcButton = (wrapper: ReturnType<typeof mount>) =>
  wrapper.findAll('button').find((b) => b.text() === '+ Add Proc')!

describe('ItemEditorPanel — procs', () => {
  it('starts with no procs and adds one per click, each with its own trigger and effect', async () => {
    const wrapper = await mountWithNewItem()
    expect(wrapper.text()).toContain('No procs')
    expect(wrapper.findAll('.proc-block')).toHaveLength(0)

    await addProcButton(wrapper).trigger('click')
    await addProcButton(wrapper).trigger('click')

    const blocks = wrapper.findAll('.proc-block')
    expect(blocks).toHaveLength(2)

    // Each block owns an independent trigger + effect select — two procs on the
    // same trigger is a legal, and the interesting, case.
    await blocks[0].find('select#ie-proc-0-trigger').setValue('onHit')
    await blocks[0].find('select#ie-proc-0-effect').setValue('fire_bolt_ignite')
    await blocks[1].find('select#ie-proc-1-trigger').setValue('onHit')
    await blocks[1].find('select#ie-proc-1-effect').setValue('lightning_chain')

    expect((wrapper.find('select#ie-proc-0-effect').element as HTMLSelectElement).value).toBe('fire_bolt_ignite')
    expect((wrapper.find('select#ie-proc-1-effect').element as HTMLSelectElement).value).toBe('lightning_chain')
  })

  it('removes the proc the user asked for, not the last one', async () => {
    const wrapper = await mountWithNewItem()
    await addProcButton(wrapper).trigger('click')
    await addProcButton(wrapper).trigger('click')
    await wrapper.find('select#ie-proc-0-effect').setValue('fire_bolt_ignite')
    await wrapper.find('select#ie-proc-1-effect').setValue('lightning_chain')

    // Remove the FIRST proc; the second must survive and shift up into index 0.
    await wrapper.findAll('.proc-block__remove')[0].trigger('click')

    expect(wrapper.findAll('.proc-block')).toHaveLength(1)
    expect((wrapper.find('select#ie-proc-0-effect').element as HTMLSelectElement).value).toBe('lightning_chain')
  })

  it('summarises override count on the row and reveals the fields behind the pencil', async () => {
    const wrapper = await mountWithNewItem()
    await addProcButton(wrapper).trigger('click')
    await wrapper.find('select#ie-proc-0-effect').setValue('fire_bolt_ignite')

    // Collapsed by default: the row reports no overrides and the fields are hidden.
    expect(wrapper.find('.proc-row__ovr').text()).toBe('no overrides')
    expect(wrapper.find('#ie-proc-0-damage').exists()).toBe(false)

    await wrapper.findAll('.proc-row__act')[0].trigger('click')
    const damage = wrapper.find('#ie-proc-0-damage')
    expect(damage.exists()).toBe(true)
    // The effect's own value shows as the placeholder — blank means "inherit".
    expect(damage.attributes('placeholder')).toBe('25')

    await damage.setValue('40')
    expect(wrapper.find('.proc-row__ovr').text()).toBe('1 override')
  })

  it('shows the description as read-only generated text, matching the match tooltip', async () => {
    const wrapper = await mountWithNewItem()
    await wrapper.find('#ie-display-name').setValue('Storm Brand')
    await wrapper.find('#ie-mod-damage').setValue('5')
    await addProcButton(wrapper).trigger('click')
    await wrapper.find('select#ie-proc-0-effect').setValue('fire_bolt_ignite')

    const desc = wrapper.find('#ie-description')
    // Not authorable — it is derived from the stats.
    expect(desc.attributes('readonly')).toBeDefined()
    // Same text the in-game tooltip builds (comma-joined stat block).
    expect((desc.element as HTMLTextAreaElement).value)
      .toBe('+5 Damage, 10% on hit: 25 Fire bolt')
  })

  it('renders the live preview card from the draft, resolving proc effects client-side', async () => {
    const wrapper = await mountWithNewItem()
    await wrapper.find('#ie-display-name').setValue('Storm Brand')
    await wrapper.find('#ie-cost-gold').setValue('35')
    await addProcButton(wrapper).trigger('click')
    await wrapper.find('select#ie-proc-0-effect').setValue('lightning_chain')
    await wrapper.find('#ie-proc-0-chance').setValue('25')

    const preview = wrapper.find('.ipc')
    expect(preview.text()).toContain('Storm Brand')
    expect(preview.text()).toContain('Common')
    // Damage/element come from the proc-effect catalog, not from the form.
    expect(preview.text()).toContain('25% on hit: 30 Lightning bolt')
    // Cost is "Cost:" + the gold coin + the number (no "Gold" word).
    expect(preview.find('.ipc__cost').text()).toBe('Cost: 35')
    expect(preview.find('.ipc__coin').exists()).toBe(true)
  })

  it('shows a Crafted line with an icon per ingredient once the item is craftable', async () => {
    const wrapper = await mountWithCraftableCatalog()

    // Not craftable yet: no Crafted line.
    expect(wrapper.find('.ipc__craft').exists()).toBe(false)

    await wrapper.find('#ie-crafting-source').setValue('recipe')
    await wrapper.find('#ie-recipe-cost').setValue('150')
    await wrapper.find('#ie-crafting-input-0').setValue('iron_ingot')
    await wrapper.find('#ie-crafting-input-1').setValue('wooden_hilt')

    const craft = wrapper.find('.ipc__craft')
    expect(craft.exists()).toBe(true)
    expect(craft.text()).toContain('Crafted:')
    expect(craft.text()).toContain('150')

    // One icon per ingredient, each labelled with the item it stands for.
    const icons = craft.findAll('.ipc__ingredient')
    expect(icons).toHaveLength(2)
    expect(icons.map((i) => i.attributes('alt'))).toEqual(['Iron Ingot', 'Wooden Hilt'])
  })

  it('hovering an ingredient shows the real in-game item tooltip', async () => {
    const wrapper = await mountWithCraftableCatalog()
    await wrapper.find('#ie-crafting-source').setValue('recipe')
    await wrapper.find('#ie-crafting-input-0').setValue('iron_ingot')

    // Nothing hovered → no tooltip anywhere.
    expect(document.querySelector('.item-tooltip')).toBeNull()

    await wrapper.find('.ipc__ingredient').trigger('mouseenter')

    // The tooltip teleports to <body>, so it is queried from the document.
    const tip = document.querySelector('.item-tooltip')
    expect(tip).not.toBeNull()
    expect(tip!.textContent).toContain('Iron Ingot')
    expect(tip!.textContent).toContain('Common') // tier line

    await wrapper.find('.ipc__ingredient').trigger('mouseleave')
    expect(document.querySelector('.item-tooltip')).toBeNull()
  })
})
