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
  it('shows the locked action-bar id, the per-rank vision override, and a resolved rank-grid cell', async () => {
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
  })
})

describe('UnitTypeEditorPanel path form — creating a new path', () => {
  it('shows an editable id and an empty rank grid', async () => {
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
  })
})

describe('UnitTypeEditorPanel path form — Perk References section', () => {
  it('lists currently-referenced perks (with an inert hint), excludes them from the add picker, and add/remove round-trips into perksByRank', async () => {
    stubCatalogFetch({
      '/catalog/paths': {
        paths: [
          {
            unit: 'archer',
            path: 'marksman',
            def: {
              path: 'marksman',
              description: 'A ranged specialist path.',
              ranks: { bronze: { damageMultiplier: 1.5, visionRange: 7 }, silver: {}, gold: {} },
              perksByRank: { bronze: ['perk_wired', 'perk_inert'] },
            },
          },
        ],
      },
      '/catalog/perks': {
        perks: [
          { id: 'perk_wired', displayName: 'Wired Perk', wired: true },
          { id: 'perk_inert', displayName: 'Inert Perk', wired: false },
          { id: 'perk_new', displayName: 'New Perk', wired: true },
        ],
      },
    })
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await selectMarksman(wrapper)

    // Pre-authored refs render with their catalog displayName; the unwired
    // one carries the inert hint.
    const text = wrapper.text()
    expect(text).toContain('Wired Perk')
    expect(text).toContain('Inert Perk')
    expect(text).toContain('inert')

    // The bronze add picker excludes already-referenced ids and offers the
    // not-yet-referenced catalog perk.
    const bronzeAdd = wrapper.find('select[aria-label="Add perk to bronze"]')
    expect(bronzeAdd.exists()).toBe(true)
    expect(bronzeAdd.text()).not.toContain('Wired Perk')
    expect(bronzeAdd.text()).not.toContain('Inert Perk')
    expect(bronzeAdd.text()).toContain('New Perk')

    // Selecting it writes into pathForm.perksByRank.bronze — reflected as a
    // new list row with a remove button.
    await bronzeAdd.setValue('perk_new')
    await flushPromises()
    expect(wrapper.text()).toContain('New Perk')
    expect(wrapper.findAll('button[title="Remove perk_new"]').length).toBe(1)

    // Removing it drops it back out of perksByRank.bronze.
    await wrapper.find('button[title="Remove perk_new"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('button[title="Remove perk_new"]').length).toBe(0)
  })

  it('filters the add picker to perks associated with this path or generic (no association), excluding other paths', async () => {
    stubCatalogFetch({
      '/catalog/paths': {
        paths: [
          {
            unit: 'archer',
            path: 'siphoner',
            def: {
              path: 'siphoner',
              description: 'A life-drain specialist path.',
              ranks: { bronze: { damageMultiplier: 1.5, visionRange: 7 }, silver: {}, gold: {} },
            },
          },
        ],
      },
      '/catalog/perks': {
        perks: [
          { id: 'perk_siphoner', displayName: 'Siphoner Perk', wired: true, path: 'siphoner' },
          { id: 'perk_generic', displayName: 'Generic Perk', wired: true, path: '' },
          { id: 'perk_trapper', displayName: 'Trapper Perk', wired: true, path: 'trapper' },
        ],
      },
    })
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await findButtonByText(wrapper, 'Archer').trigger('click')
    await findButtonByText(wrapper, 'Promotion Paths').trigger('click')
    await flushPromises()
    await findButtonByText(wrapper, 'siphoner').trigger('click')

    const bronzeAdd = wrapper.find('select[aria-label="Add perk to bronze"]')
    expect(bronzeAdd.exists()).toBe(true)
    expect(bronzeAdd.text()).toContain('Siphoner Perk')
    expect(bronzeAdd.text()).toContain('Generic Perk')
    expect(bronzeAdd.text()).not.toContain('Trapper Perk')
  })
})

describe('UnitTypeEditorPanel path form — Ability Pool slots', () => {
  function stubWithAbilityPools(overrides: Record<string, unknown> = {}) {
    stubCatalogFetch({
      '/catalog/paths': {
        paths: [
          {
            unit: 'archer',
            path: 'marksman',
            def: {
              path: 'marksman',
              description: 'A ranged specialist path.',
              abilities: ['arcane_missiles'],
              ranks: { bronze: { damageMultiplier: 1.5, visionRange: 7 }, silver: {}, gold: {} },
              abilityPoolsByRank: { silver: ['chain_lightning'] },
            },
          },
        ],
      },
      '/catalog/abilities': {
        abilities: [
          { id: 'arcane_missiles', displayName: 'Arcane Missiles' },
          { id: 'fireball', displayName: 'Fireball' },
          { id: 'chain_lightning', displayName: 'Chain Lightning' },
          { id: 'meteor', displayName: 'Meteor' },
        ],
      },
      ...overrides,
    })
  }

  it('switching a rank to Ability slot reveals the ability picker, writes an empty pool, and clears perks for that rank', async () => {
    stubCatalogFetch({
      '/catalog/paths': {
        paths: [
          {
            unit: 'archer',
            path: 'marksman',
            def: {
              path: 'marksman',
              description: 'A ranged specialist path.',
              ranks: { bronze: { damageMultiplier: 1.5, visionRange: 7 }, silver: {}, gold: {} },
              perksByRank: { bronze: ['perk_wired'] },
            },
          },
        ],
      },
      '/catalog/perks': {
        perks: [{ id: 'perk_wired', displayName: 'Wired Perk', wired: true }],
      },
      '/catalog/abilities': {
        abilities: [{ id: 'fireball', displayName: 'Fireball' }],
      },
    })
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await selectMarksman(wrapper)

    // Starts as a Perk slot: the perk-add picker is present, the ability-add
    // picker is not, and bronze's referenced perk shows a remove button.
    expect(wrapper.find('select[aria-label="Add perk to bronze"]').exists()).toBe(true)
    expect(wrapper.find('select[data-test="ability-add-bronze"]').exists()).toBe(false)
    expect(wrapper.findAll('button[title="Remove perk_wired"]').length).toBe(1)

    // Flip bronze to an Ability slot.
    const abilityRadio = wrapper.find('input[type="radio"][name="slot-type-bronze"][value="ability"]')
    expect(abilityRadio.exists()).toBe(true)
    await abilityRadio.setValue()
    await flushPromises()

    // The ability picker appears, the perk picker/list is gone, and the
    // previously-referenced perk was cleared out of perksByRank.bronze (its
    // remove button — the unambiguous signal that it was actually referenced
    // by this rank, as opposed to merely appearing as add-picker text on
    // another rank — is gone).
    expect(wrapper.find('select[data-test="ability-add-bronze"]').exists()).toBe(true)
    expect(wrapper.find('select[aria-label="Add perk to bronze"]').exists()).toBe(false)
    expect(wrapper.findAll('button[title="Remove perk_wired"]').length).toBe(0)

    const vm = wrapper.vm as unknown as { pathForm: { abilityPoolsByRank?: Record<string, string[]>; perksByRank?: Record<string, string[]> } }
    expect(vm.pathForm.abilityPoolsByRank?.bronze).toEqual([])
    expect(vm.pathForm.perksByRank?.bronze ?? []).toEqual([])
  })

  it('excludes base abilities and this-rank picks, but still offers an ability already used in ANOTHER rank\'s pool', async () => {
    stubWithAbilityPools()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await selectMarksman(wrapper)

    // Silver is already an Ability slot (key present on abilityPoolsByRank)
    // with chain_lightning in its pool.
    const silverAdd = wrapper.find('select[data-test="ability-add-silver"]')
    expect(silverAdd.exists()).toBe(true)

    // Excluded: arcane_missiles (base ability, always granted) and
    // chain_lightning (already in THIS rank's — silver's — pool).
    expect(silverAdd.text()).not.toContain('Arcane Missiles')
    expect(silverAdd.text()).not.toContain('Chain Lightning')
    // Included: fireball/meteor, neither base nor in silver's pool.
    expect(silverAdd.text()).toContain('Fireball')
    expect(silverAdd.text()).toContain('Meteor')

    // Now flip bronze to an Ability slot too and confirm chain_lightning —
    // which lives in silver's pool, a DIFFERENT rank — is still offered to
    // bronze. This is the corrected cross-rank-sharing behavior.
    const bronzeAbilityRadio = wrapper.find('input[type="radio"][name="slot-type-bronze"][value="ability"]')
    await bronzeAbilityRadio.setValue()
    await flushPromises()
    const bronzeAdd = wrapper.find('select[data-test="ability-add-bronze"]')
    expect(bronzeAdd.exists()).toBe(true)
    expect(bronzeAdd.text()).toContain('Chain Lightning')
    expect(bronzeAdd.text()).not.toContain('Arcane Missiles')
  })

  it('adding an ability writes it into abilityPoolsByRank[rank]; removing it takes it back out', async () => {
    stubWithAbilityPools()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await selectMarksman(wrapper)

    const silverAdd = wrapper.find('select[data-test="ability-add-silver"]')
    await silverAdd.setValue('fireball')
    await flushPromises()

    expect(wrapper.text()).toContain('Fireball')
    expect(wrapper.findAll('button[title="Remove fireball"]').length).toBe(1)

    await wrapper.find('button[title="Remove fireball"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('button[title="Remove fireball"]').length).toBe(0)
  })

  it('restores a rank\'s ability pool when toggling ability→perk→ability (no wipe)', async () => {
    stubWithAbilityPools()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await selectMarksman(wrapper)

    const vm = wrapper.vm as unknown as {
      pathForm: { abilityPoolsByRank?: Record<string, string[]>; perksByRank?: Record<string, string[]> }
    }

    // Bronze → Ability slot, populate two abilities.
    await wrapper.find('input[type="radio"][name="slot-type-bronze"][value="ability"]').setValue()
    await flushPromises()
    await wrapper.find('select[data-test="ability-add-bronze"]').setValue('fireball')
    await flushPromises()
    await wrapper.find('select[data-test="ability-add-bronze"]').setValue('meteor')
    await flushPromises()
    expect(vm.pathForm.abilityPoolsByRank?.bronze).toEqual(['fireball', 'meteor'])

    // Bronze → Perk slot: the ability pool leaves the persisted form...
    await wrapper.find('input[type="radio"][name="slot-type-bronze"][value="perk"]').setValue()
    await flushPromises()
    expect(vm.pathForm.abilityPoolsByRank?.bronze).toBeUndefined()

    // ...but toggling back restores the stashed list, not an empty pool.
    await wrapper.find('input[type="radio"][name="slot-type-bronze"][value="ability"]').setValue()
    await flushPromises()
    expect(vm.pathForm.abilityPoolsByRank?.bronze).toEqual(['fireball', 'meteor'])
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
