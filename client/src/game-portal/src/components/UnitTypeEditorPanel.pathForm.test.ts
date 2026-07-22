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
    // Ability-stat rows are server-derived; without them the "Add a stat…"
    // picker has nothing to offer and does not render.
    '/catalog/ability-stats': { stats: [
      { id: 'radius', label: 'Radius', kind: 'radius' },
      { id: 'duration', label: 'Duration', kind: 'duration' },
      { id: 'count', label: 'Count', kind: 'count', flatOnly: true },
    ] },
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
  it('shows the locked action-bar id, the per-rank vision override, and a resolved rank stat', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await selectMarksman(wrapper)

    // Identity: id locked for an existing path (lives in the action bar).
    const idInput = wrapper.find('#pe-id')
    expect((idInput.element as HTMLInputElement).value).toBe('marksman')
    expect(idInput.attributes('disabled')).toBeDefined()

    // Vision Range is a per-rank flat override — bronze authored 7. Selectors
    // are rank-scoped because every rank tab is mounted at once (v-show).
    const bronzeVision = wrapper.find('[data-test="rank-bronze-add-visionRange"]')
    expect((bronzeVision.element as HTMLInputElement).value).toBe('7')

    // base 18 damage × 1.5 = 27, resolved inline — proves the parent unit's
    // real stats reached the per-rank panel.
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

// The three rank tabs each own one block (slot + stats + ability stats). This
// exists because they shipped EMPTY: v-for and v-show were on the same element,
// and the shown/hidden state did not track the active tab — every rank block
// stayed display:none no matter which tab was selected. Neither type-checking
// nor a text assertion catches that, because v-show keeps the content in the
// DOM; only the computed visibility shows it.
describe('UnitTypeEditorPanel path form — rank tabs', () => {
  it('shows exactly the selected rank block and hides the others', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await selectMarksman(wrapper)

    const hidden = (i: number) =>
      wrapper.findAll('.unit-editor__rank-slot-stack')[i].attributes('style')?.includes('display: none') ?? false

    // Identity is the default tab: no rank block is showing.
    expect([hidden(0), hidden(1), hidden(2)]).toEqual([true, true, true])

    for (const [i, rank] of ['bronze', 'silver', 'gold'].entries()) {
      await wrapper.find(`[data-test="unit-path-tab-${rank}"]`).trigger('click')
      await flushPromises()
      expect(
        [hidden(0), hidden(1), hidden(2)],
        `after selecting ${rank}, only its block should show`,
      ).toEqual([0, 1, 2].map((n) => n !== i))
    }
  })

  it('gives each rank tab its slot, stats and ability-stats cards', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await selectMarksman(wrapper)

    await wrapper.find('[data-test="unit-path-tab-silver"]').trigger('click')
    await flushPromises()

    const silver = wrapper.findAll('.unit-editor__rank-slot-stack')[1]
    expect(silver.find('[data-test="slot-type-silver"]').exists()).toBe(true)
    expect(silver.find('[data-test="rank-silver-mult-maxHPMultiplier"]').exists()).toBe(true)
    // Inheritance context — silver must say what bronze already gave.
    expect(silver.text()).toContain('carry forward from')
  })
})

// A stat added to the UNIT must show in the rank tabs immediately. It used to
// require saving first, because the rank panels read the loaded catalog rather
// than the live form — so the author had to commit a change to find out what it
// would do, which is the opposite of how the rest of this editor behaves.
describe('UnitTypeEditorPanel path form — unit stats reach the ranks live', () => {
  it('mirrors a base stat added to the unit without a save', async () => {
    stubCatalogFetch({
      '/catalog/units': {
        units: [
          {
            type: 'archer', name: 'Archer', faction: 'human',
            hp: 120, damage: 18, attackSpeed: 1.2, moveSpeed: 60, attackRange: 5,
            // Nothing authored on disk — the row must come from the live edit.
          },
        ],
      },
    })
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await selectMarksman(wrapper)

    // Not there yet.
    expect(wrapper.find('[data-test="rank-bronze-base-abilityPower"]').exists()).toBe(false)

    // Simulate the unit's Base Stats row being added, WITHOUT saving.
    const vm = wrapper.vm as unknown as { form: { baseStats?: Record<string, number> } }
    vm.form.baseStats = { abilityPower: 20 }
    await flushPromises()

    expect(wrapper.find('[data-test="rank-bronze-base-abilityPower"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('inherits 20 from the unit')
  })
})

// Per-rank unit stats: introduce one at a rank, and it must reach EVERY higher
// rank and disappear from all of them when taken back.
describe('UnitTypeEditorPanel path form — unit stats across ranks', () => {
  async function openBronze() {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await selectMarksman(wrapper)
    await wrapper.find('[data-test="unit-path-tab-bronze"]').trigger('click')
    await flushPromises()
    return wrapper
  }

  // Inheritance is TRANSITIVE. Looking only one rank back meant a value set at
  // bronze vanished from gold whenever silver happened to author nothing — so
  // gold had no inherited value and no floor, and could be set below bronze.
  it('carries a bronze stat up to gold through an untouched silver', async () => {
    const wrapper = await openBronze()
    await wrapper.find('[data-test="rank-bronze-add-stat"]').setValue('abilityPower')
    await flushPromises()

    expect(wrapper.find('[data-test="rank-silver-base-abilityPower"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="rank-gold-base-abilityPower"]').exists()).toBe(true)
  })

  // Only the rank that OWNS the value gets an ✕; the ranks inheriting it do not.
  it('offers the remove only on the rank that set it', async () => {
    const wrapper = await openBronze()
    await wrapper.find('[data-test="rank-bronze-add-stat"]').setValue('abilityPower')
    await flushPromises()

    expect(wrapper.find('[data-test="rank-bronze-remove-abilityPower"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="rank-silver-remove-abilityPower"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="rank-gold-remove-abilityPower"]').exists()).toBe(false)
  })

  it('clears the stat from every rank when the owning rank removes it', async () => {
    const wrapper = await openBronze()
    await wrapper.find('[data-test="rank-bronze-add-stat"]').setValue('abilityPower')
    await flushPromises()

    await wrapper.find('[data-test="rank-bronze-remove-abilityPower"]').trigger('click')
    await flushPromises()

    for (const rank of ['bronze', 'silver', 'gold']) {
      expect(
        wrapper.find(`[data-test="rank-${rank}-base-abilityPower"]`).exists(),
        `${rank} should no longer show the stat`,
      ).toBe(false)
    }
  })
})

// Rank Ability Stats follow the SAME rules as the per-rank unit stats:
// transitive inheritance, a floor, and removal only at the rank that owns the
// value. They shipped without any of it — the section was a plain add/remove
// grid, so a gold block could silently be set below bronze's.
describe('UnitTypeEditorPanel path form — rank ability stats inherit', () => {
  async function addDurationAtBronze() {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await selectMarksman(wrapper)
    await wrapper.find('[data-test="unit-path-tab-bronze"]').trigger('click')
    await flushPromises()

    const stacks = () => wrapper.findAll('.unit-editor__rank-slot-stack')
    const bronze = stacks()[0]
    // A stat is chosen from the "Add a stat…" picker — the same flow the
    // per-rank unit stats use.
    await bronze.find('[data-test="ability-stat-add"]').setValue('duration')
    await flushPromises()
    const flat = bronze.find('[data-test="ability-stat-flat"]')
    await flat.setValue('5')
    await flat.trigger('change')
    await flushPromises()
    return { wrapper, stacks }
  }

  it('carries a bronze ability stat to silver and gold, floored, shown as a placeholder', async () => {
    const { stacks } = await addDurationAtBronze()

    for (const i of [1, 2]) {
      const rank = stacks()[i]
      expect(rank.findAll('[data-test="ability-stat-row"]').length, `rank ${i}`).toBe(1)
      const flat = rank.find('[data-test="ability-stat-flat"]')
      expect(flat.attributes('min'), `rank ${i} floor`).toBe('5')
      // Faded placeholder, blank field — "not set here", not "set to 5".
      expect(flat.attributes('placeholder'), `rank ${i} placeholder`).toBe('5')
      expect((flat.element as HTMLInputElement).value, `rank ${i} value`).toBe('')
    }
  })

  it('offers the remove only at the rank that set it', async () => {
    const { stacks } = await addDurationAtBronze()
    expect(stacks()[0].find('[data-test="ability-stat-remove-duration"]').exists()).toBe(true)
    expect(stacks()[1].find('[data-test="ability-stat-remove-duration"]').exists()).toBe(false)
    expect(stacks()[2].find('[data-test="ability-stat-remove-duration"]').exists()).toBe(false)
  })

  it('clears it from every rank when the owning rank removes it', async () => {
    const { stacks } = await addDurationAtBronze()
    await stacks()[0].find('[data-test="ability-stat-remove-duration"]').trigger('click')
    await flushPromises()

    for (const i of [0, 1, 2]) {
      expect(stacks()[i].findAll('[data-test="ability-stat-row"]').length, `rank ${i}`).toBe(0)
    }
  })
})

// A broad ability stat on the UNIT already applies at every rank, so bronze must
// show it as inherited and floor against it. It was invisible there — a rank
// could be authored below what the unit itself grants, with no sign the unit
// granted anything.
describe('UnitTypeEditorPanel path form — unit ability stats reach the ranks', () => {
  it('shows a unit ability stat as inherited at bronze, floored, without a save', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await selectMarksman(wrapper)

    const stacks = () => wrapper.findAll('.unit-editor__rank-slot-stack')
    expect(stacks()[0].findAll('[data-test="ability-stat-row"]')).toHaveLength(0)

    // Edit the UNIT's own Ability Stats — no save.
    const vm = wrapper.vm as unknown as {
      form: { abilityStats?: Record<string, { flat?: number; pct?: number }> }
    }
    vm.form.abilityStats = { radius: { pct: 0.15 } }
    await flushPromises()

    for (const [i, rank] of ['bronze', 'silver', 'gold'].entries()) {
      const pct = stacks()[i].find('[data-test="ability-stat-pct"]')
      expect(pct.exists(), `${rank} row`).toBe(true)
      // 0.15 stored -> 15 shown, as a faded placeholder on a blank field.
      expect(pct.attributes('placeholder'), `${rank} placeholder`).toBe('15')
      expect(pct.attributes('min'), `${rank} floor`).toBe('15')
      expect((pct.element as HTMLInputElement).value, `${rank} value`).toBe('')
    }
  })

  // An inherited-from-the-unit row is not a rank's to delete.
  it('offers no remove on a row that comes from the unit', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await selectMarksman(wrapper)

    const vm = wrapper.vm as unknown as {
      form: { abilityStats?: Record<string, { flat?: number; pct?: number }> }
    }
    vm.form.abilityStats = { radius: { pct: 0.15 } }
    await flushPromises()

    expect(
      wrapper.findAll('.unit-editor__rank-slot-stack')[0].find('[data-test="ability-stat-remove-radius"]').exists(),
    ).toBe(false)
  })
})

// The unit's own EditorHeader lives OUTSIDE the scroll viewport, so its Save
// stays put on its own. Save Path did not: it sat in a bar that scrolled away
// with the cards, and on a long rank tab it was simply gone. Both header rows
// now live in one pinned container.
//
// These are STRUCTURE assertions. jsdom applies no scoped CSS, so nothing here
// proves `position: sticky` actually pins — that needs eyes on the running app.
// What they do prevent is a refactor quietly moving the actions back out of the
// header, which is what would un-pin them.
describe('UnitTypeEditorPanel path header', () => {
  it('keeps the path strip and the section-tab row in one header container', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await selectMarksman(wrapper)

    const header = wrapper.find('.unit-editor__path-header')
    expect(header.exists()).toBe(true)
    expect(header.find('.unit-editor__path-strip').exists()).toBe(true)
    expect(header.find('.unit-editor__path-tabrow').exists()).toBe(true)
  })

  it('puts the id and both path actions on the section-tab row', async () => {
    stubCatalogFetch()
    const wrapper = mount(UnitTypeEditorPanel)
    await flushPromises()
    await selectMarksman(wrapper)

    const tabrow = wrapper.find('.unit-editor__path-tabrow')
    // The section tablist itself, so the actions really are sharing its line.
    expect(tabrow.find('[data-test="unit-path-tab-identity"]').exists()).toBe(true)
    expect(tabrow.find('#pe-id').exists()).toBe(true)

    const labels = tabrow.findAll('button').map((b) => b.text())
    expect(labels).toContain('Save Path')
    expect(labels).toContain('Delete Path')
  })
})
