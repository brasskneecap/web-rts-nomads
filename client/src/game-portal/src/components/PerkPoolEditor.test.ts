import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import PerkPoolEditor from './PerkPoolEditor.vue'
import type { PerkEntry } from '@/game/units/pathEditorApi'

function mountEditor(pools: Record<string, PerkEntry[]>, catalog: PerkEntry[]) {
  return mount(PerkPoolEditor, {
    props: { unit: 'archer', path: 'trapper', pools, catalog },
  })
}

describe('PerkPoolEditor — rendering wired vs inert', () => {
  it('shows a Wired badge for a catalog-wired perk, and an empty-pool hint for an empty rank', () => {
    const catalog: PerkEntry[] = [
      { id: 'bloodlust', displayName: 'Bloodlust', wired: true },
    ]
    const wrapper = mountEditor(
      { bronze: [{ id: 'bloodlust', wired: true }], silver: [], gold: [] },
      catalog,
    )
    const text = wrapper.text()
    expect(text).toContain('bloodlust')
    expect(text).toContain('Wired')
    expect(text.toLowerCase()).toContain('empty pool')
  })

  it('shows the Inert caution for a perk id absent from the catalog', () => {
    const wrapper = mountEditor(
      { bronze: [], silver: [{ id: 'made_up_perk', wired: false }], gold: [] },
      [],
    )
    expect(wrapper.text()).toContain('Inert')
    expect(wrapper.text()).toContain('made_up_perk')
  })
})

describe('PerkPoolEditor — adding perks', () => {
  it('adding a brand-new id emits update:pools with it appended, badged Inert', async () => {
    const wrapper = mountEditor({ bronze: [], silver: [], gold: [] }, [])

    const input = wrapper.find('input[data-add-rank="silver"]')
    await input.setValue('made_up_perk')
    await wrapper.find('button[data-add-btn-rank="silver"]').trigger('click')

    const emitted = wrapper.emitted('update:pools')
    expect(emitted).toBeTruthy()
    const last = emitted![emitted!.length - 1][0] as Record<string, PerkEntry[]>
    expect(last.silver).toHaveLength(1)
    expect(last.silver[0].id).toBe('made_up_perk')

    await wrapper.setProps({ pools: last })
    const text = wrapper.text()
    expect(text).toContain('made_up_perk')
    expect(text).toContain('Inert')
  })

  it('adding an existing catalog id inherits its displayName and wired status', async () => {
    const catalog: PerkEntry[] = [{ id: 'ghost_arrow', displayName: 'Ghost Arrow', wired: true }]
    const wrapper = mountEditor({ bronze: [], silver: [], gold: [] }, catalog)

    await wrapper.find('input[data-add-rank="bronze"]').setValue('ghost_arrow')
    await wrapper.find('button[data-add-btn-rank="bronze"]').trigger('click')

    const emitted = wrapper.emitted('update:pools')!
    const last = emitted[emitted.length - 1][0] as Record<string, PerkEntry[]>
    expect(last.bronze[0]).toMatchObject({ id: 'ghost_arrow', displayName: 'Ghost Arrow', wired: true })

    await wrapper.setProps({ pools: last })
    const text = wrapper.text()
    expect(text).toContain('Ghost Arrow')
    expect(text).toContain('Wired')
  })

  it('rejects a duplicate id within the same rank without double-appending', async () => {
    const wrapper = mountEditor(
      { bronze: [{ id: 'bloodlust', wired: true }], silver: [], gold: [] },
      [{ id: 'bloodlust', displayName: 'Bloodlust', wired: true }],
    )

    await wrapper.find('input[data-add-rank="bronze"]').setValue('bloodlust')
    await wrapper.find('button[data-add-btn-rank="bronze"]').trigger('click')

    expect(wrapper.emitted('update:pools')).toBeUndefined()
    expect(wrapper.text().toLowerCase()).toContain('already')
  })

  it('shows a one-line honesty note that new perks are inert until wired in code', () => {
    const wrapper = mountEditor({ bronze: [], silver: [], gold: [] }, [])
    expect(wrapper.text()).toContain('New perks are inert until a developer wires their behavior in code.')
  })
})

describe('PerkPoolEditor — removing and reordering', () => {
  it('removing an entry emits update:pools without it', async () => {
    const wrapper = mountEditor(
      {
        bronze: [
          { id: 'bloodlust', wired: true },
          { id: 'ghost_arrow', wired: true },
        ],
        silver: [],
        gold: [],
      },
      [],
    )
    const removeButtons = wrapper.findAll('button[data-remove-rank="bronze"]')
    expect(removeButtons).toHaveLength(2)
    await removeButtons[0].trigger('click')

    const emitted = wrapper.emitted('update:pools')!
    const last = emitted[emitted.length - 1][0] as Record<string, PerkEntry[]>
    expect(last.bronze.map((p) => p.id)).toEqual(['ghost_arrow'])
  })

  it('moving an entry down swaps it with the next one', async () => {
    const wrapper = mountEditor(
      {
        bronze: [
          { id: 'bloodlust', wired: true },
          { id: 'ghost_arrow', wired: true },
        ],
        silver: [],
        gold: [],
      },
      [],
    )
    const downButtons = wrapper.findAll('button[data-move-down-rank="bronze"]')
    await downButtons[0].trigger('click')

    const emitted = wrapper.emitted('update:pools')!
    const last = emitted[emitted.length - 1][0] as Record<string, PerkEntry[]>
    expect(last.bronze.map((p) => p.id)).toEqual(['ghost_arrow', 'bloodlust'])
  })

  it('the first entry\'s move-up button is disabled', () => {
    const wrapper = mountEditor(
      { bronze: [{ id: 'bloodlust', wired: true }], silver: [], gold: [] },
      [],
    )
    const upButton = wrapper.find('button[data-move-up-rank="bronze"]')
    expect(upButton.attributes('disabled')).toBeDefined()
  })
})
