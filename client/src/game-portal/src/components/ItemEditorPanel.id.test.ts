import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ItemEditorPanel from './ItemEditorPanel.vue'

const BROAD_SWORD = {
  id: 'broad_sword', displayName: 'Broad Sword', iconKey: 'broad_sword',
  kind: 'equipment', tier: 'common', costGold: 50,
}

function stubCatalogFetch(items: unknown[] = []) {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    const map: Record<string, unknown> = {
      '/catalog/items': { items },
      '/catalog/recipes': { recipes: [] },
      '/catalog/procs': { procs: [] },
    }
    const key = Object.keys(map).find((k) => String(url).endsWith(k))
    return { ok: true, status: 200, json: async () => map[key ?? ''] ?? {} }
  }) as unknown as typeof fetch)
}

afterEach(() => vi.restoreAllMocks())

async function mountPanel(items: unknown[] = []) {
  stubCatalogFetch(items)
  const wrapper = mount(ItemEditorPanel)
  await flushPromises()
  return wrapper
}

const clickButton = (wrapper: Awaited<ReturnType<typeof mountPanel>>, label: string) =>
  wrapper.findAll('button').find((b) => b.text() === label)!.trigger('click')

const idValue = (wrapper: Awaited<ReturnType<typeof mountPanel>>) =>
  (wrapper.find('#ie-id').element as HTMLInputElement).value

describe('ItemEditorPanel — id is slugged from the display name', () => {
  it('fills the id in as the author types the name', async () => {
    const wrapper = await mountPanel()
    await clickButton(wrapper, 'Add New Item')

    await wrapper.find('#ie-display-name').setValue('Fire Sword')
    expect(idValue(wrapper)).toBe('fire_sword')

    // It keeps tracking the name until the author touches the id.
    await wrapper.find('#ie-display-name').setValue("Ranger's Longbow +2")
    expect(idValue(wrapper)).toBe('ranger_s_longbow_2')
  })

  it('stops tracking the name once the id is hand-edited', async () => {
    const wrapper = await mountPanel()
    await clickButton(wrapper, 'Add New Item')
    await wrapper.find('#ie-display-name').setValue('Fire Sword')

    await wrapper.find('#ie-id').setValue('flamebrand')
    await wrapper.find('#ie-display-name').setValue('Frost Sword')

    expect(idValue(wrapper)).toBe('flamebrand')
  })

  it('slugs a hand-typed id too, so it can never be saved invalid', async () => {
    const wrapper = await mountPanel()
    await clickButton(wrapper, 'Add New Item')

    await wrapper.find('#ie-id').setValue('Fire Sword!')
    expect(idValue(wrapper)).toBe('fire_sword_')
  })

  it('an existing item keeps its id — the name is free to change', async () => {
    const wrapper = await mountPanel([BROAD_SWORD])
    await clickButton(wrapper, 'Broad Sword')

    await wrapper.find('#ie-display-name').setValue('Bastard Sword')

    // The id is the primary key: renaming it would orphan the item's icon and
    // every recipe/shop/loot reference to it. So it is locked, and it does not
    // follow the new name.
    expect((wrapper.find('#ie-id').element as HTMLInputElement).disabled).toBe(true)
    expect(idValue(wrapper)).toBe('broad_sword')
  })

  it('a duplicate starts from the copied name, so its id is unique out of the box', async () => {
    const wrapper = await mountPanel([BROAD_SWORD])
    await clickButton(wrapper, 'Broad Sword')
    await wrapper.find('[aria-label="Duplicate"]').trigger('click')

    expect((wrapper.find('#ie-display-name').element as HTMLInputElement).value).toBe('Broad Sword Copy')
    expect(idValue(wrapper)).toBe('broad_sword_copy')
  })
})
