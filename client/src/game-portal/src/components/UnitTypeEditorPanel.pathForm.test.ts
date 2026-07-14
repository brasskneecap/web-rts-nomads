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
            visionRange: 6,
            ranks: { bronze: { damageMultiplier: 1.5 }, silver: {}, gold: {} },
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

function findLabeledInput(wrapper: ReturnType<typeof mount>, labelPrefix: string) {
  const label = wrapper.findAll('label').find((l) => l.text().startsWith(labelPrefix))
  if (!label) throw new Error(`no label starting with "${labelPrefix}"`)
  const input = label.find('input')
  if (!input.exists()) throw new Error(`label "${labelPrefix}" has no nested input`)
  return input
}

afterEach(() => vi.restoreAllMocks())

async function expandAndSelectMarksman(wrapper: ReturnType<typeof mount>) {
  await wrapper.find('button.unit-editor__tree-toggle').trigger('click')
  await findButtonByText(wrapper, 'marksman').trigger('click')
}

// Path-mode sections are a collapsible accordion (only Identity starts open,
// same as the unit form) — a section's field markup isn't in the DOM at all
// until its summary button is clicked open.
async function openPathSection(wrapper: ReturnType<typeof mount>, summaryText: string) {
  await wrapper.findAll('button.unit-editor__section-summary')
    .find((b) => b.text() === summaryText)!
    .trigger('click')
}

describe('UnitTypeEditorPanel path form — selecting an existing path', () => {
  it('shows the locked Identity id, the Stats overlay value, a resolved rank-grid cell, and perk pool badges', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await expandAndSelectMarksman(wrapper)

    // Identity: id locked for an existing path.
    const idInput = findLabeledInput(wrapper, 'Path ID')
    expect((idInput.element as HTMLInputElement).value).toBe('marksman')
    expect(idInput.attributes('disabled')).toBeDefined()

    // Stats: the path's own authored Vision Range override shows up (Vision
    // lives in Stats, matching the base unit's layout).
    await openPathSection(wrapper, 'Stats')
    const visionInput = findLabeledInput(wrapper, 'Vision Range')
    expect((visionInput.element as HTMLInputElement).value).toBe('6')

    // Rank grid: base 18 damage × 1.5 = 27, resolved inline (Task 4a's honesty
    // requirement) — proves the parent unit's real stats reached the grid.
    await openPathSection(wrapper, 'Ranks')
    const rankText = wrapper.text()
    expect(rankText).toContain('18')
    expect(rankText).toContain('27')

    // Perk pools: both a wired and an inert perk render with honest badges.
    await openPathSection(wrapper, 'Perk Pools')
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
    await wrapper.find('button.unit-editor__new').trigger('click')
    await findButtonByText(wrapper, 'New Path').trigger('click')
    await findButtonByText(wrapper, 'Create').trigger('click')

    const idInput = findLabeledInput(wrapper, 'Path ID')
    expect((idInput.element as HTMLInputElement).value).toBe('')
    expect(idInput.attributes('disabled')).toBeUndefined()

    // Empty rank grid: all three ranks render, no resolved values anywhere.
    await openPathSection(wrapper, 'Ranks')
    const rankText = wrapper.text()
    expect(rankText).toContain('bronze')
    expect(rankText).toContain('silver')
    expect(rankText).toContain('gold')
    expect(rankText).not.toContain('27')

    // Empty perk pools: the empty-pool hint, no perk ids from the catalog.
    await openPathSection(wrapper, 'Perk Pools')
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
    expect(wrapper.text()).not.toContain('Ranks')
  })
})
