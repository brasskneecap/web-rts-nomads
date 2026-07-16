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

// The Items-style editor renders the unit list, Save/Delete and the id in shared
// primitives (EditorSidebar / EditorHeader / SectionCard). Selecting a unit is a
// click on its sidebar row button; promotion paths now live inside the unit's
// "Promotion Paths" tab — open it, then the paths are a nested tab strip and the
// selected path edits inline (compact action bar + its own sub-tabs).
function findButtonByText(wrapper: ReturnType<typeof mount>, text: string) {
  const btn = wrapper.findAll('button').find((b) => b.text() === text)
  if (!btn) throw new Error(`no button with text "${text}"`)
  return btn
}

function maybeButtonByText(wrapper: ReturnType<typeof mount>, text: string) {
  return wrapper.findAll('button').find((b) => b.text() === text)
}

// Open the unit's Promotion Paths tab (auto-opens the first existing path).
async function openPathsTab(wrapper: ReturnType<typeof mount>) {
  await findButtonByText(wrapper, 'Promotion Paths').trigger('click')
  await flushPromises()
}

afterEach(() => vi.restoreAllMocks())

describe('UnitTypeEditorPanel unit list + promotion paths', () => {
  it('lists units in the sidebar; selecting a unit reveals its promotion paths (hidden until then)', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()

    expect(wrapper.text()).toContain('Archer')

    // The blank new-unit form is open by default: its paths are not shown until
    // the unit is selected and its Promotion Paths tab opened.
    expect(wrapper.text()).not.toContain('marksman')
    expect(wrapper.text()).not.toContain('trapper')

    await findButtonByText(wrapper, 'Archer').trigger('click')
    await openPathsTab(wrapper)

    expect(wrapper.text()).toContain('marksman')
    expect(wrapper.text()).toContain('trapper')
  })

  it('selecting a promotion-path tab edits it inline (no page swap) with the real path form', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()

    await findButtonByText(wrapper, 'Archer').trigger('click')
    await openPathsTab(wrapper)
    await findButtonByText(wrapper, 'marksman').trigger('click')

    // Action bar: id locked (existing path); Identity card parent read-only.
    const idInput = wrapper.find('#pe-id')
    expect((idInput.element as HTMLInputElement).value).toBe('marksman')
    expect(idInput.attributes('disabled')).toBeDefined()
    const parentInput = wrapper.find('#pe-parent')
    expect((parentInput.element as HTMLInputElement).value).toBe('archer')

    // Path sub-tab sections: Combat/Abilities/Rank Stats live under the Combat
    // tab, plus Identity/Preview — all mounted (v-show). Perk pools were
    // retired from the unit editor (perks are now standalone defs edited in
    // the world-editor Perks screen).
    expect(wrapper.text()).toContain('Combat')
    expect(wrapper.text()).toContain('Abilities')
    expect(wrapper.text()).toContain('Rank Stats')
    expect(wrapper.text()).toContain('Preview')
    // Unit-only sections must not render while the path editor is showing.
    expect(wrapper.text()).not.toContain('Gating')
  })

  it('a unit with no paths shows an add-a-path hint under the Paths tab', async () => {
    stubCatalogFetch({
      '/catalog/units': { units: [{ type: 'grunt', name: 'Grunt', faction: 'orc' }] },
      '/catalog/paths': { paths: [] },
    })
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()

    await findButtonByText(wrapper, 'Grunt').trigger('click')
    await openPathsTab(wrapper)
    expect(wrapper.text()).toContain('Select a path, or add a new one.')
    // The New Path affordance is always available in the strip.
    expect(maybeButtonByText(wrapper, '+ New Path')).toBeDefined()
  })
})

describe('UnitTypeEditorPanel create flows (unit vs path)', () => {
  it('New Path (from the Paths tab) opens a blank, save-disabled path form inline', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()

    // Select archer first — the "+ New Path" affordance uses it as the parent.
    await findButtonByText(wrapper, 'Archer').trigger('click')
    await openPathsTab(wrapper)
    await findButtonByText(wrapper, '+ New Path').trigger('click')

    // A blank, EDITABLE id (a brand-new path) and the parent unit locked in.
    const idInput = wrapper.find('#pe-id')
    expect((idInput.element as HTMLInputElement).value).toBe('')
    expect(idInput.attributes('disabled')).toBeUndefined()
    const parentInput = wrapper.find('#pe-parent')
    expect((parentInput.element as HTMLInputElement).value).toBe('archer')

    // Save Path is disabled until an id exists — a brand-new form has none.
    const saveBtn = findButtonByText(wrapper, 'Save Path')
    expect(saveBtn.attributes('disabled')).toBeDefined()
  })

  it('Add New Unit opens a blank base-unit form (save disabled until type/faction set)', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()

    await findButtonByText(wrapper, 'Add New Unit').trigger('click')

    expect(wrapper.text()).toContain('Preview')
    expect(wrapper.text()).toContain('Identity')
    const saveBtn = findButtonByText(wrapper, 'Save')
    expect(saveBtn.attributes('disabled')).toBeDefined()
  })

  it('selecting an already-saved path (a non-blank id) leaves Save enabled', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()

    await findButtonByText(wrapper, 'Archer').trigger('click')
    await openPathsTab(wrapper)
    await findButtonByText(wrapper, 'marksman').trigger('click')

    const saveBtn = findButtonByText(wrapper, 'Save Path')
    expect(saveBtn.attributes('disabled')).toBeUndefined()
  })
})

describe('UnitTypeEditorPanel base-unit mode (regression)', () => {
  it('selecting a base unit shows unit mode (Preview/Identity, Save/Delete, selected row)', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()

    await findButtonByText(wrapper, 'Archer').trigger('click')

    expect(wrapper.text()).toContain('Preview')
    expect(wrapper.text()).toContain('Identity')
    expect(wrapper.find('.ed-side__row--on .ed-side__name').text()).toBe('Archer')
    expect(maybeButtonByText(wrapper, 'Save')).toBeDefined()
    expect(maybeButtonByText(wrapper, 'Delete')).toBeDefined()
    // Path-mode-only content must not leak into unit mode.
    expect(wrapper.text()).not.toContain('Perk Pools')
  })
})
