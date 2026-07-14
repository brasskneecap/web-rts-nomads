import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UnitTypeEditorPanel from './UnitTypeEditorPanel.vue'

// Stub every fetch the panel makes on mount with an empty-but-valid payload,
// keyed by URL suffix (mirrors AbilityEditorPanel.test.ts's idiom). Overrides
// let individual tests replace one entry (e.g. an empty /catalog/paths).
function stubCatalogFetch(overrides: Record<string, unknown> = {}) {
  const map: Record<string, unknown> = {
    '/catalog/units': { units: [{ type: 'archer', name: 'Archer', faction: 'human' }] },
    '/catalog/paths': {
      paths: [
        { unit: 'archer', path: 'marksman', def: { path: 'marksman', ranks: {} } },
        { unit: 'archer', path: 'trapper', def: { path: 'trapper', ranks: {} } },
      ],
    },
    '/catalog/factions': { factions: [{ id: 'human', displayName: 'Human' }] },
    '/catalog/archetypes': { archetypes: [] },
    '/catalog/projectiles': { projectiles: [] },
    '/catalog/abilities': { abilities: [] },
    '/catalog/damage-types': { damageTypes: [] },
    '/catalog/buildings': { buildings: [] },
    '/catalog/perks': { perks: [] },
    ...overrides,
  }
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    const key = Object.keys(map).find((k) => String(url).endsWith(k))
    return { ok: true, status: 200, json: async () => map[key ?? ''] ?? {} }
  }) as unknown as typeof fetch)
}

function findButtonByText(wrapper: ReturnType<typeof mount>, text: string) {
  const btn = wrapper.findAll('button').find((b) => b.text() === text)
  if (!btn) throw new Error(`no button with text "${text}"`)
  return btn
}

// Finds the <input> nested inside the <label> whose visible text STARTS WITH
// the given prefix (labels also render hint/placeholder text after the
// field name, so an exact match would be brittle).
function findLabeledInput(wrapper: ReturnType<typeof mount>, labelPrefix: string) {
  const label = wrapper.findAll('label').find((l) => l.text().startsWith(labelPrefix))
  if (!label) throw new Error(`no label starting with "${labelPrefix}"`)
  const input = label.find('input')
  if (!input.exists()) throw new Error(`label "${labelPrefix}" has no nested input`)
  return input
}

afterEach(() => vi.restoreAllMocks())

describe('UnitTypeEditorPanel tree list', () => {
  it('renders archer with an expand affordance; expanding shows its path children, collapsed by default', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()

    expect(wrapper.text()).toContain('Archer')
    const toggle = wrapper.find('button.unit-editor__tree-toggle')
    expect(toggle.exists()).toBe(true)

    // Collapsed by default: children not rendered.
    expect(wrapper.text()).not.toContain('marksman')
    expect(wrapper.text()).not.toContain('trapper')

    await toggle.trigger('click')

    expect(wrapper.text()).toContain('marksman')
    expect(wrapper.text()).toContain('trapper')
  })

  it('clicking a path child flips to path mode and renders the real path form', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()

    await wrapper.find('button.unit-editor__tree-toggle').trigger('click')
    await findButtonByText(wrapper, 'marksman').trigger('click')

    // Identity: id locked (existing path), parent unit shown read-only.
    const idInput = findLabeledInput(wrapper, 'Path ID')
    expect((idInput.element as HTMLInputElement).value).toBe('marksman')
    expect(idInput.attributes('disabled')).toBeDefined()
    const parentInput = findLabeledInput(wrapper, 'Parent Unit')
    expect((parentInput.element as HTMLInputElement).value).toBe('archer')

    // Path sections mirror the base unit's names (Stats/Combat/Abilities)
    // instead of a single "Overlay" section.
    expect(wrapper.text()).toContain('Stats')
    expect(wrapper.text()).toContain('Combat')
    expect(wrapper.text()).toContain('Abilities')
    expect(wrapper.text()).toContain('Ranks')
    expect(wrapper.text()).toContain('Perk Pools')
    // Path mode has its own Preview section (same name + top position as the
    // unit form, for consistency).
    expect(wrapper.text()).toContain('Preview')
    // Unit-mode-only sections must not render in path mode.
    expect(wrapper.text()).not.toContain('Gating')
  })

  it('a unit with no paths gets no expand toggle', async () => {
    stubCatalogFetch({
      '/catalog/units': { units: [{ type: 'grunt', name: 'Grunt', faction: 'orc' }] },
      '/catalog/paths': { paths: [] },
    })
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()

    expect(wrapper.find('button.unit-editor__tree-toggle').exists()).toBe(false)
  })
})

describe('UnitTypeEditorPanel New chooser (Base vs Path)', () => {
  it('offers Base Unit vs Path, and New Path (with a parent unit selected) enters path mode with a blank, unsaved-disabled form', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()

    // Select archer as the base unit first, so it becomes the New Path default parent.
    await findButtonByText(wrapper, 'Archer').trigger('click')

    await wrapper.find('button.unit-editor__new').trigger('click')
    expect(wrapper.text()).toContain('New Base Unit')
    expect(wrapper.text()).toContain('New Path')

    await findButtonByText(wrapper, 'New Path').trigger('click')

    const parentSelect = wrapper.find('select.unit-editor__new-path-parent')
    expect(parentSelect.exists()).toBe(true)
    expect((parentSelect.element as HTMLSelectElement).value).toBe('archer')

    await findButtonByText(wrapper, 'Create').trigger('click')

    // Entered path mode with a blank, EDITABLE id (a brand-new path) and the
    // parent unit locked in.
    const idInput = findLabeledInput(wrapper, 'Path ID')
    expect((idInput.element as HTMLInputElement).value).toBe('')
    expect(idInput.attributes('disabled')).toBeUndefined()
    const parentInput = findLabeledInput(wrapper, 'Parent Unit')
    expect((parentInput.element as HTMLInputElement).value).toBe('archer')

    // Save is disabled until an id exists — a brand-new form has none.
    const saveBtn = findButtonByText(wrapper, 'Save Path')
    expect(saveBtn.attributes('disabled')).toBeDefined()
  })

  it('New Base Unit reuses the existing base-unit creation flow untouched', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()

    await findButtonByText(wrapper, 'Archer').trigger('click')
    await wrapper.find('button.unit-editor__new').trigger('click')
    await findButtonByText(wrapper, 'New Base Unit').trigger('click')

    // Back in unit mode, with the base-unit form's Preview section visible
    // and no base unit selected (a blank new-unit form).
    expect(wrapper.text()).toContain('Preview')
    const saveBtn = findButtonByText(wrapper, 'Save')
    // Blank new unit has no type/faction set (faction only seeded from the
    // active filter, which is '' here), so Save must still be disabled.
    expect(saveBtn.attributes('disabled')).toBeDefined()
  })

  it('selecting an already-saved path (a non-blank id) leaves Save Path enabled', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()

    await wrapper.find('button.unit-editor__tree-toggle').trigger('click')
    await findButtonByText(wrapper, 'marksman').trigger('click')

    const saveBtn = findButtonByText(wrapper, 'Save Path')
    expect(saveBtn.attributes('disabled')).toBeUndefined()
  })
})

describe('UnitTypeEditorPanel base-unit mode (regression)', () => {
  it('selecting a base unit still shows unit mode (Preview/Identity sections, Save/Delete actions)', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()

    await findButtonByText(wrapper, 'Archer').trigger('click')

    expect(wrapper.text()).toContain('Preview')
    expect(wrapper.text()).toContain('Identity')
    expect(wrapper.find('.unit-editor__tree-unit-btn.is-selected').text()).toBe('Archer')
    expect(findButtonByText(wrapper, 'Save').exists()).toBe(true)
    expect(findButtonByText(wrapper, 'Delete').exists()).toBe(true)
    // Path-mode-only content must not leak into unit mode.
    expect(wrapper.text()).not.toContain('Parent unit:')
  })
})
