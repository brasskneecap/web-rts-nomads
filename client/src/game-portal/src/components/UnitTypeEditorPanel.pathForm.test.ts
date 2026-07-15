import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UnitTypeEditorPanel from './UnitTypeEditorPanel.vue'

// Stub every fetch the panel makes on mount with an empty-but-valid payload,
// keyed by URL suffix (mirrors UnitTypeEditorPanel.tree.test.ts's idiom).
function stubCatalogFetch(overrides: Record<string, unknown> = {}) {
  const map: Record<string, unknown> = {
    '/catalog/units': {
      units: [
        { type: 'archer', name: 'Archer', faction: 'human', hp: 120, damage: 18, attackSpeed: 1.2, moveSpeed: 60, attackRange: 5 },
      ],
    },
    '/catalog/paths': {
      paths: [
        {
          unit: 'archer',
          path: 'marksman',
          def: {
            path: 'marksman',
            description: 'A ranged specialist path.',
            ranks: { bronze: { damageMultiplier: 1.5, visionRange: 7 }, silver: {}, gold: {} },
          },
        },
      ],
    },
    '/catalog/factions': { factions: [{ id: 'human', displayName: 'Human' }] },
    '/catalog/archetypes': { archetypes: [] },
    '/catalog/projectiles': { projectiles: [] },
    '/catalog/abilities': { abilities: [] },
    '/catalog/damage-types': { damageTypes: [] },
    '/catalog/buildings': { buildings: [] },
    '/catalog/perks': {
      perks: [
        { id: 'piercing_shot', displayName: 'Piercing Shot', wired: true, unitType: 'archer', path: 'marksman', rank: 'bronze' },
        { id: 'ghost_arrow', displayName: 'Ghost Arrow', wired: false, unitType: 'archer', path: 'marksman', rank: 'silver' },
      ],
    },
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

afterEach(() => vi.restoreAllMocks())

// Reach a path: select its parent unit, open the Promotion Paths tab, then click
// the path's tab in the nested strip. Sections within a path are sub-tabbed but
// all stay mounted (v-show), so text assertions see every section.
async function selectMarksman(wrapper: ReturnType<typeof mount>) {
  await findButtonByText(wrapper, 'Archer').trigger('click')
  await findButtonByText(wrapper, 'Promotion Paths').trigger('click')
  await flushPromises()
  await findButtonByText(wrapper, 'marksman').trigger('click')
}

describe('UnitTypeEditorPanel path form — selecting an existing path', () => {
  it('shows the locked action-bar id, the per-rank vision override, a resolved rank-grid cell, and perk pool badges', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await selectMarksman(wrapper)

    // Identity: id locked for an existing path (lives in the action bar).
    const idInput = wrapper.find('#pe-id')
    expect((idInput.element as HTMLInputElement).value).toBe('marksman')
    expect(idInput.attributes('disabled')).toBeDefined()

    // Vision Range is now a per-rank flat override — bronze authored 7.
    const bronzeVision = wrapper.find('input[data-rank="bronze"][data-field="visionRange"]')
    expect((bronzeVision.element as HTMLInputElement).value).toBe('7')

    // Rank grid: base 18 damage × 1.5 = 27, resolved inline — proves the parent
    // unit's real stats reached the grid.
    const rankText = wrapper.text()
    expect(rankText).toContain('18')
    expect(rankText).toContain('27')

    // Perk pools: both a wired and an inert perk render with honest badges.
    const perkText = wrapper.text()
    expect(perkText).toContain('piercing_shot')
    expect(perkText).toContain('Wired')
    expect(perkText).toContain('ghost_arrow')
    expect(perkText).toContain('Inert')
  })
})

describe('UnitTypeEditorPanel path form — creating a new path', () => {
  it('shows an editable id, an empty rank grid, and empty perk pools', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()

    await findButtonByText(wrapper, 'Archer').trigger('click')
    await findButtonByText(wrapper, 'Promotion Paths').trigger('click')
    await flushPromises()
    await findButtonByText(wrapper, '+ New Path').trigger('click')

    const idInput = wrapper.find('#pe-id')
    expect((idInput.element as HTMLInputElement).value).toBe('')
    expect(idInput.attributes('disabled')).toBeUndefined()

    // Empty rank grid: all three ranks render, no resolved values anywhere.
    const rankText = wrapper.text()
    expect(rankText).toContain('bronze')
    expect(rankText).toContain('silver')
    expect(rankText).toContain('gold')
    expect(rankText).not.toContain('27')

    // Empty perk pools: the empty-pool hint, no perk ids from the catalog.
    const perkText = wrapper.text()
    expect(perkText.toLowerCase()).toContain('empty pool')
    expect(perkText).not.toContain('piercing_shot')
  })
})

describe('UnitTypeEditorPanel path form — base-unit mode regression', () => {
  it('still renders the unit form (Preview/Stats sections), not the path form', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()

    await findButtonByText(wrapper, 'Archer').trigger('click')

    expect(wrapper.text()).toContain('Preview')
    expect(wrapper.text()).toContain('Stats')
    // Path-only sections must not leak into base-unit mode.
    expect(wrapper.text()).not.toContain('Perk Pools')
    expect(wrapper.text()).not.toContain('Rank Stats')
  })
})
