import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { shallowRef } from 'vue'
import type { ActionSchemaBundle } from '@/game/abilities/program/programSchema'
import AddActionDialog from './AddActionDialog.vue'
import { AbilityBuilderKey } from './AbilityBuilderContext'
import type { NodePath } from './programTree'

function makeSchema(): ActionSchemaBundle {
  return {
    actions: [
      { type: 'deal_damage', fields: [], runnable: true },
      { type: 'select_targets', fields: [], runnable: true },
      // Not (yet) executed by the runtime — should surface the
      // "display-only" marker.
      { type: 'camera_shake', fields: [], runnable: false },
      // Absent from the local category table — should fall into "Other".
      { type: 'custom', fields: [], runnable: true },
    ],
    enums: {},
  }
}

function makeBuilderStub(overrides: { schema?: ActionSchemaBundle | null } = {}) {
  const schema = shallowRef<ActionSchemaBundle | null>(overrides.schema ?? null)
  return {
    schema,
    addAction: vi.fn(),
  }
}

function mountDialog(
  builder: ReturnType<typeof makeBuilderStub>,
  props: { open?: boolean; triggerPath?: NodePath } = {},
  attachToBody = false,
) {
  return mount(AddActionDialog, {
    props: { open: props.open ?? true, triggerPath: props.triggerPath ?? [{ kind: 'trigger', id: 't1' }] },
    global: { provide: { [AbilityBuilderKey as unknown as string]: builder } },
    attachTo: attachToBody ? document.body : undefined,
  })
}

describe('AddActionDialog', () => {
  it('renders nothing when closed', () => {
    const builder = makeBuilderStub({ schema: makeSchema() })
    const wrapper = mountDialog(builder, { open: false })
    expect(wrapper.find('[data-test="add-action-overlay"]').exists()).toBe(false)
  })

  it('shows a loading state when the schema has not loaded yet', () => {
    const builder = makeBuilderStub({ schema: null })
    const wrapper = mountDialog(builder)

    expect(wrapper.text()).toContain('Loading actions')
    expect(wrapper.find('[data-test="add-action-entry"]').exists()).toBe(false)
  })

  it('renders every schema action as one flat list, with no category headings', () => {
    const builder = makeBuilderStub({ schema: makeSchema() })
    const wrapper = mountDialog(builder)

    // The category is what the pills filter BY — repeating it as a section
    // heading over the rows was redundant, so there are no headings at all.
    expect(wrapper.find('.aad-panel__category-label').exists()).toBe(false)

    const entries = wrapper.findAll('[data-test="add-action-entry"]')
    expect(entries).toHaveLength(4)
    expect(entries.map((e) => e.text()).some((t) => t.includes('Deal Damage'))).toBe(true)
  })

  it('orders the flat list by category, then alphabetically within one', () => {
    const builder = makeBuilderStub({ schema: makeSchema() })
    const wrapper = mountDialog(builder)

    // CATEGORY_ORDER is Targets -> Combat -> ... -> Presentation -> Other, so
    // related actions stay adjacent even without headings to group them.
    const types = wrapper.findAll('[data-test="add-action-entry"]').map((e) => e.attributes('data-type'))
    expect(types.indexOf('select_targets')).toBeLessThan(types.indexOf('deal_damage'))
    expect(types.indexOf('deal_damage')).toBeLessThan(types.indexOf('custom'))
  })

  it('only shows filter chips for categories that actually have schema entries', () => {
    const builder = makeBuilderStub({ schema: makeSchema() })
    const wrapper = mountDialog(builder)

    const chipLabels = wrapper.findAll('[data-test="add-action-chip"]').map((c) => c.text())
    // All + the 4 categories present in makeSchema() (Targets, Combat, Other,
    // Presentation) — World/Resources/Flow have no entries in this fixture.
    expect(chipLabels).toEqual(['All', 'Targets', 'Combat', 'Presentation', 'Other'])
  })

  it('filters entries by search text', async () => {
    const builder = makeBuilderStub({ schema: makeSchema() })
    const wrapper = mountDialog(builder)

    await wrapper.find('[data-test="add-action-search"]').setValue('damage')

    const entries = wrapper.findAll('[data-test="add-action-entry"]')
    expect(entries).toHaveLength(1)
    expect(entries[0].text()).toContain('Deal Damage')
  })

  it('filters entries by category chip', async () => {
    const builder = makeBuilderStub({ schema: makeSchema() })
    const wrapper = mountDialog(builder)

    const combatChip = wrapper.findAll('[data-test="add-action-chip"]').find((c) => c.text() === 'Combat')!
    await combatChip.trigger('click')

    const entries = wrapper.findAll('[data-test="add-action-entry"]')
    expect(entries).toHaveLength(1)
    expect(entries[0].text()).toContain('Deal Damage')
  })

  it('composes search and category chip filters as an AND', async () => {
    const builder = makeBuilderStub({ schema: makeSchema() })
    const wrapper = mountDialog(builder)

    const targetsChip = wrapper.findAll('[data-test="add-action-chip"]').find((c) => c.text() === 'Targets')!
    await targetsChip.trigger('click')
    await wrapper.find('[data-test="add-action-search"]').setValue('damage')

    // "damage" text matches nothing in the Targets category.
    expect(wrapper.findAll('[data-test="add-action-entry"]')).toHaveLength(0)
    expect(wrapper.find('.aad-panel__empty').exists()).toBe(true)
  })

  it('clicking an entry adds the action to the passed-in trigger and closes', async () => {
    const builder = makeBuilderStub({ schema: makeSchema() })
    const wrapper = mountDialog(builder, { triggerPath: [{ kind: 'trigger', id: 't2' }] })

    const entry = wrapper.findAll('[data-test="add-action-entry"]').find((e) => e.text().includes('Deal Damage'))!
    await entry.trigger('click')

    expect(builder.addAction).toHaveBeenCalledWith([{ kind: 'trigger', id: 't2' }], 'deal_damage')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('display-only entries are still addable and show their marker', async () => {
    const builder = makeBuilderStub({ schema: makeSchema() })
    const wrapper = mountDialog(builder)

    const entry = wrapper.findAll('[data-test="add-action-entry"]').find((e) => e.text().includes('Camera Shake'))!
    expect(entry.find('.aad-panel__badge').exists()).toBe(true)
    expect(entry.text()).toContain('display-only')

    await entry.trigger('click')
    expect(builder.addAction).toHaveBeenCalledWith([{ kind: 'trigger', id: 't1' }], 'camera_shake')
    expect(wrapper.emitted('close')).toHaveLength(1)

    const runnableEntry = wrapper.findAll('[data-test="add-action-entry"]').find((e) => e.text().includes('Deal Damage'))!
    expect(runnableEntry.find('.aad-panel__badge').exists()).toBe(false)
  })

  it('an unrecognized action type falls into the Other category', () => {
    const builder = makeBuilderStub({ schema: makeSchema() })
    const wrapper = mountDialog(builder)

    const otherEntries = wrapper.findAll('[data-test="add-action-entry"]').filter((e) => e.attributes('data-type') === 'custom')
    expect(otherEntries).toHaveLength(1)
  })

  it('clicking the backdrop closes the dialog', async () => {
    const builder = makeBuilderStub({ schema: makeSchema() })
    const wrapper = mountDialog(builder)

    await wrapper.find('[data-test="add-action-overlay"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('the Close button closes the dialog', async () => {
    const builder = makeBuilderStub({ schema: makeSchema() })
    const wrapper = mountDialog(builder)

    await wrapper.find('[data-test="add-action-close"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('pressing Escape closes the dialog', async () => {
    const builder = makeBuilderStub({ schema: makeSchema() })
    const wrapper = mountDialog(builder)

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('does not react to Escape once closed (no leftover window listener)', async () => {
    const builder = makeBuilderStub({ schema: makeSchema() })
    const wrapper = mountDialog(builder)

    await wrapper.setProps({ open: false })
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('focuses the search input on open', async () => {
    const builder = makeBuilderStub({ schema: makeSchema() })
    const wrapper = mountDialog(builder, {}, true)

    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    expect(document.activeElement).toBe(wrapper.find('[data-test="add-action-search"]').element)
    wrapper.unmount()
  })

  it('resets search and category filter each time the dialog reopens', async () => {
    const builder = makeBuilderStub({ schema: makeSchema() })
    const wrapper = mountDialog(builder, { open: false })

    await wrapper.setProps({ open: true })
    await wrapper.find('[data-test="add-action-search"]').setValue('damage')
    const combatChip = wrapper.findAll('[data-test="add-action-chip"]').find((c) => c.text() === 'Combat')!
    await combatChip.trigger('click')

    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })

    expect((wrapper.find('[data-test="add-action-search"]').element as HTMLInputElement).value).toBe('')
    expect(wrapper.findAll('[data-test="add-action-entry"]')).toHaveLength(4)
  })
})
