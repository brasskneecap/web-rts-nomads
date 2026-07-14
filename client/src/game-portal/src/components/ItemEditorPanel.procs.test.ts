import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ItemEditorPanel from './ItemEditorPanel.vue'

// Stub every catalog fetch the panel makes on mount, keyed by URL suffix.
function stubCatalogFetch() {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    const map: Record<string, unknown> = {
      '/catalog/items': { items: [] },
      '/catalog/recipes': { recipes: [] },
      '/catalog/procs': {
        procs: [
          { id: 'fire_bolt_ignite', damage: 25, damageType: 'fire', burnDamagePerSecond: 8 },
          { id: 'lightning_chain', damage: 25, damageType: 'lightning', bounceCount: 2 },
        ],
      },
    }
    const key = Object.keys(map).find((k) => String(url).endsWith(k))
    return { ok: true, status: 200, json: async () => map[key ?? ''] ?? {} }
  }) as unknown as typeof fetch)
}

afterEach(() => vi.restoreAllMocks())

// Opens a blank item on the Procs section — the panel's starting state for
// authoring procs.
async function mountOnProcsSection() {
  stubCatalogFetch()
  const wrapper = mount(ItemEditorPanel)
  await flushPromises()
  const newItem = wrapper.findAll('button').find((b) => b.text() === 'New Item')
  await newItem!.trigger('click')
  const procsSection = wrapper.findAll('button').find((b) => b.text() === 'Procs')
  await procsSection!.trigger('click')
  return wrapper
}

const addProcButton = (wrapper: ReturnType<typeof mount>) =>
  wrapper.findAll('button').find((b) => b.text() === '+ Add Proc')!

describe('ItemEditorPanel — procs', () => {
  it('starts with no procs and adds one per click, each with its own trigger and effect', async () => {
    const wrapper = await mountOnProcsSection()
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
    const wrapper = await mountOnProcsSection()
    await addProcButton(wrapper).trigger('click')
    await addProcButton(wrapper).trigger('click')
    await wrapper.find('select#ie-proc-0-effect').setValue('fire_bolt_ignite')
    await wrapper.find('select#ie-proc-1-effect').setValue('lightning_chain')

    // Remove the FIRST proc; the second must survive and shift up into index 0.
    const removes = wrapper.findAll('.proc-block__remove')
    await removes[0].trigger('click')

    expect(wrapper.findAll('.proc-block')).toHaveLength(1)
    expect((wrapper.find('select#ie-proc-0-effect').element as HTMLSelectElement).value).toBe('lightning_chain')
  })
})
