// PerkBuilderPanel.test.ts
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import PerkBuilderPanel from './PerkBuilderPanel.vue'

// Reset/Delete now go through the themed confirm dialog (ask / confirmDelete).
// Auto-confirm here so existing round-trip flows aren't blocked on a modal.
vi.mock('@/components/ui/useConfirmDialog', () => ({ ask: vi.fn(() => Promise.resolve(true)) }))

const markerTrap = {
  id: 'marker_trap',
  program: { triggers: [{ id: 'cast', type: 'on_cast_complete', actions: [{ id: 'zone', type: 'create_zone', config: { name: 'Z', radius: 115, duration: 12, triggers: [{ id: 'entered', type: 'on_tick', actions: [{ id: 'pick_enemy', type: 'select_targets', target: { radius: 110 } }, { id: 'mark', type: 'apply_status_duration', config: { name: 'Marked', duration: 4 } }] }] } }] }] },
}

const amplified = {
  id: 'amplified_effects',
  displayName: 'Amplified Effects',
  path: 'trapper',
  statModifiers: [{ stat: 'abilityDamage', op: 'multiply', value: 1.35 }],
  abilityStats: [{ stat: 'damageTaken', flat: 0.15 }, { stat: 'moveSpeed', flat: -0.15 }],
  abilityFields: [{ target: 'marker_trap', action: 'mark', field: 'duration', op: 'multiply', value: 1.35 }],
  wired: false,
}

const zealous = {
  id: 'zealous_march', path: 'cleric',
  auras: [{ radius: 192, targets: 'allies', includeSelf: true, stacking: 'max', perAdditionalSource: 0.05, statModifiers: [{ stat: 'moveSpeed', op: 'add', value: 0.3 }] }],
  wired: false,
}

function stub(perk: Record<string, unknown>, sink?: Array<Record<string, unknown>>) {
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    if (init?.method === 'POST' && u.endsWith('/perks')) {
      sink?.push((JSON.parse(String(init.body)) as { perk: Record<string, unknown> }).perk)
      return { ok: true, status: 200, json: async () => ({}) }
    }
    if (u.endsWith('/catalog/perks')) return { ok: true, status: 200, json: async () => ({ perks: [perk] }) }
    if (u.endsWith('/catalog/units')) return { ok: true, status: 200, json: async () => ({ units: [], paths: [], pathsByUnit: { archer: ['trapper'], acolyte: ['cleric'] } }) }
    if (u.endsWith('/catalog/abilities')) return { ok: true, status: 200, json: async () => ({ abilities: [markerTrap] }) }
    if (u.endsWith('/catalog/ability-stats')) return { ok: true, status: 200, json: async () => ({ stats: [{ id: 'damageTaken', label: 'Vulnerable (Damage Taken)', inflicted: true }, { id: 'moveSpeed', label: 'Move Speed', inflicted: true }] }) }
    return { ok: true, status: 200, json: async () => ({}) }
  }) as unknown as typeof fetch)
}

async function openPerk(wrapper: VueWrapper, unit: string, path: string) {
  await wrapper.findAll('.pk-side__unit').find((b) => b.text().includes(unit))!.trigger('click')
  await wrapper.findAll('.pk-side__path-label').find((b) => b.text().includes(path))!.trigger('click')
  await wrapper.find('[data-test="perk-row"]').trigger('click')
  await flushPromises()
}

afterEach(() => vi.restoreAllMocks())

describe('PerkBuilderPanel', () => {
  it('projects a perk into one card per modifier, in kind order', async () => {
    stub(amplified)
    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Archer', 'trapper')

    const cards = wrapper.findAll('[data-test="perk-modifier-card"]')
    expect(cards).toHaveLength(4)
    expect(cards.map((c) => c.attributes('data-kind'))).toEqual(['unitStat', 'abilityStat', 'abilityStat', 'abilityField'])
    // unitStat summary shows the ×multiplier; stat label comes from the real
    // registry so assert only the stable multiplier text.
    expect(cards[0].text()).toContain('×1.35')
    // abilityField summary is deterministic (ability id is used verbatim as its label).
    expect(cards[3].text()).toContain('marker_trap ▸ mark ▸ duration ×1.35')
  })

  it('shows the empty inspector until a card is selected, then edits that card', async () => {
    stub(amplified)
    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Archer', 'trapper')

    expect(wrapper.find('[data-test="perk-inspector-empty"]').exists()).toBe(true)

    await wrapper.findAll('[data-test="perk-modifier-card"]')[0].trigger('click')
    const inspector = wrapper.find('[data-test="perk-inspector"]')
    expect(inspector.exists()).toBe(true)
    expect((inspector.find('select[aria-label="Stat"]').element as HTMLSelectElement).value).toBe('abilityDamage')
    expect((inspector.find('input[aria-label="Value"]').element as HTMLInputElement).value).toBe('1.35')
  })

  it('round-trips Amplified Effects unedited through save (all 3 slice arrays byte-identical)', async () => {
    const sink: Array<Record<string, unknown>> = []
    stub(amplified, sink)
    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Archer', 'trapper')

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()

    expect(sink).toHaveLength(1)
    expect(sink[0].statModifiers).toEqual(amplified.statModifiers)
    expect(sink[0].abilityStats).toEqual(amplified.abilityStats)
    expect(sink[0].abilityFields).toEqual(amplified.abilityFields)
    expect('generatedDescription' in sink[0]).toBe(false)
  })

  it('preserves an aura through save without editing it', async () => {
    const sink: Array<Record<string, unknown>> = []
    stub(zealous, sink)
    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Acolyte', 'cleric')

    expect(wrapper.find('[data-test="perk-modifier-card"][data-kind="aura"]').exists()).toBe(true)

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()
    expect(sink[0].auras).toEqual(zealous.auras)
  })

  it('edits an ability-field value and round-trips the change (multiply stays off the wire)', async () => {
    const sink: Array<Record<string, unknown>> = []
    stub(amplified, sink)
    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Archer', 'trapper')

    await wrapper.find('[data-test="perk-modifier-card"][data-kind="abilityField"]').trigger('click')
    const valueInput = wrapper.find('[data-test="perk-inspector"] input[aria-label="Value"]')
    await valueInput.setValue('1.5')

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()
    const fields = sink[0].abilityFields as Array<{ value: number; op?: string }>
    expect(fields[0].value).toBe(1.5)
    expect(fields[0]).not.toHaveProperty('op')
  })

  it('keeps the selection on the right modifier after deleting a lower-indexed sibling', async () => {
    stub(amplified)
    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Archer', 'trapper')

    // Cards: [unitStat(0), abilityStat:damageTaken(1), abilityStat:moveSpeed(2), abilityField(3)].
    // Select the SECOND ability-stat card (moveSpeed).
    const cards = wrapper.findAll('[data-test="perk-modifier-card"]')
    await cards[2].trigger('click')
    let inspector = wrapper.find('[data-test="perk-inspector"]')
    expect((inspector.find('select[aria-label="Ability Stat"]').element as HTMLSelectElement).value).toBe('moveSpeed')

    // Delete the FIRST ability-stat card (damageTaken, a lower index in the same array).
    await cards[1].find('button[aria-label="Delete modifier"]').trigger('click')
    await flushPromises()

    // Selection must follow moveSpeed (now re-indexed), NOT slide to another modifier or empty out.
    inspector = wrapper.find('[data-test="perk-inspector"]')
    expect(inspector.exists()).toBe(true)
    expect((inspector.find('select[aria-label="Ability Stat"]').element as HTMLSelectElement).value).toBe('moveSpeed')
  })

  it('adds a unit-stat modifier via quick-add and it appears as a new card', async () => {
    stub(amplified)
    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Archer', 'trapper')

    await wrapper.find('[data-test="quick-add-unitStat"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-test="perk-modifier-card"][data-kind="unitStat"]')).toHaveLength(2)
    expect(wrapper.find('[data-test="perk-inspector"]').exists()).toBe(true)
  })

  it('adds a new ability rider via the add-menu and it becomes an editable card', async () => {
    stub(amplified)
    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Archer', 'trapper')

    // Open the add-menu and pick Ability Rider.
    await wrapper.find('[data-test="add-modifier"]').trigger('click')
    const riderItem = wrapper.findAll('[role="menuitem"]').find((b) => b.text().includes('Ability Rider'))
    expect(riderItem).toBeTruthy()
    await riderItem!.trigger('click')
    await flushPromises()

    // A new abilityRider card exists and is auto-selected → its inspector renders.
    expect(wrapper.find('[data-test="perk-modifier-card"][data-kind="abilityRider"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="perk-inspector"]').exists()).toBe(true)
  })

  it('loads beam_mastery ability modifier and round-trips it unedited', async () => {
    const beam = {
      id: 'beam_mastery', path: 'siphoner',
      abilityModifiers: [{ target: 'siphon_life', manaCostMult: 0.5, rangeMult: 1.25 }],
      wired: false,
    }
    const sink: Array<Record<string, unknown>> = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      const u = String(url)
      if (init?.method === 'POST' && u.endsWith('/perks')) { sink.push((JSON.parse(String(init.body)) as { perk: Record<string, unknown> }).perk); return { ok: true, status: 200, json: async () => ({}) } }
      if (u.endsWith('/catalog/perks')) return { ok: true, status: 200, json: async () => ({ perks: [beam] }) }
      if (u.endsWith('/catalog/units')) return { ok: true, status: 200, json: async () => ({ units: [], paths: [], pathsByUnit: { acolyte: ['siphoner'] } }) }
      if (u.endsWith('/catalog/abilities')) return { ok: true, status: 200, json: async () => ({ abilities: [{ id: 'siphon_life' }] }) }
      return { ok: true, status: 200, json: async () => ({}) }
    }) as unknown as typeof fetch)

    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Acolyte', 'siphoner')

    const card = wrapper.find('[data-test="perk-modifier-card"][data-kind="abilityModifier"]')
    expect(card.exists()).toBe(true)
    await card.trigger('click')
    const inspector = wrapper.find('[data-test="perk-inspector"]')
    expect(inspector.find('button[aria-label="Target Ability"]').text()).toContain('siphon_life')
    expect((inspector.find('input[aria-label="Mana mult"]').element as HTMLInputElement).value).toBe('0.5')

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()
    expect(sink[0].abilityModifiers).toEqual(beam.abilityModifiers)
  })

  it('edits a granted ability id and round-trips the change', async () => {
    const perk = { id: 'granter', path: 'siphoner', grantsAbilities: ['dash', 'blink'], wired: false }
    const sink: Array<Record<string, unknown>> = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      const u = String(url)
      if (init?.method === 'POST' && u.endsWith('/perks')) { sink.push((JSON.parse(String(init.body)) as { perk: Record<string, unknown> }).perk); return { ok: true, status: 200, json: async () => ({}) } }
      if (u.endsWith('/catalog/perks')) return { ok: true, status: 200, json: async () => ({ perks: [perk] }) }
      if (u.endsWith('/catalog/units')) return { ok: true, status: 200, json: async () => ({ units: [], paths: [], pathsByUnit: { acolyte: ['siphoner'] } }) }
      if (u.endsWith('/catalog/abilities')) return { ok: true, status: 200, json: async () => ({ abilities: [{ id: 'dash' }, { id: 'blink' }, { id: 'sprint' }] }) }
      return { ok: true, status: 200, json: async () => ({}) }
    }) as unknown as typeof fetch)

    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Acolyte', 'siphoner')

    // Two grant cards, one per granted ability.
    const cards = wrapper.findAll('[data-test="perk-modifier-card"][data-kind="grantAbility"]')
    expect(cards).toHaveLength(2)

    // Select the FIRST grant card, confirm it shows 'dash', change it to 'sprint'.
    await cards[0].trigger('click')
    expect(wrapper.find('button[aria-label="Granted Ability"]').text()).toContain('dash')
    await wrapper.find('button[aria-label="Granted Ability"]').trigger('click')
    await wrapper.find('[data-test="ability-picker-item-sprint"]').trigger('click')
    await wrapper.find('[data-test="ability-picker-confirm"]').trigger('click')

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()
    expect(sink[0].grantsAbilities).toEqual(['sprint', 'blink'])
  })

  it('loads a perk modifier (target + ops) and round-trips it unedited', async () => {
    const perk = {
      id: 'ascended', path: 'siphoner',
      config: { boost: 0.2 },
      perkModifiers: [{ target: 'beam_mastery', ops: [{ targetKey: 'damageMultiplier', op: 'mult', sourceKey: 'boost' }] }],
      wired: false,
    }
    const sink: Array<Record<string, unknown>> = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      const u = String(url)
      if (init?.method === 'POST' && u.endsWith('/perks')) { sink.push((JSON.parse(String(init.body)) as { perk: Record<string, unknown> }).perk); return { ok: true, status: 200, json: async () => ({}) } }
      if (u.endsWith('/catalog/perks')) return { ok: true, status: 200, json: async () => ({ perks: [perk, { id: 'beam_mastery', path: 'siphoner', config: { damageMultiplier: 1.5 } }] }) }
      if (u.endsWith('/catalog/units')) return { ok: true, status: 200, json: async () => ({ units: [], paths: [], pathsByUnit: { acolyte: ['siphoner'] } }) }
      return { ok: true, status: 200, json: async () => ({}) }
    }) as unknown as typeof fetch)

    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    // Two siphoner perks exist; open 'ascended' specifically.
    await wrapper.findAll('.pk-side__unit').find((b) => b.text().includes('Acolyte'))!.trigger('click')
    await wrapper.findAll('.pk-side__path-label').find((b) => b.text().includes('siphoner'))!.trigger('click')
    await wrapper.findAll('[data-test="perk-row"]').find((r) => r.text().includes('ascended'))!.trigger('click')
    await flushPromises()

    const card = wrapper.find('[data-test="perk-modifier-card"][data-kind="perkModifier"]')
    expect(card.exists()).toBe(true)
    await card.trigger('click')
    const inspector = wrapper.find('[data-test="perk-inspector"]')
    expect((inspector.find('input[aria-label="Target Perk"]').element as HTMLInputElement).value).toBe('beam_mastery')
    expect((inspector.find('input[aria-label="Op 1 target key"]').element as HTMLInputElement).value).toBe('damageMultiplier')
    expect((inspector.find('input[aria-label="Op 1 source key"]').element as HTMLInputElement).value).toBe('boost')

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()
    expect(sink[0].perkModifiers).toEqual(perk.perkModifiers)
  })

  it('lets you add a perk-modifier op and incrementally fill it without losing the row', async () => {
    const perk = { id: 'ascended', path: 'siphoner', config: { boost: 0.2 }, perkModifiers: [{ target: 'beam_mastery', ops: [{ targetKey: 'damageMultiplier', op: 'mult', sourceKey: 'boost' }] }], wired: false }
    const sink: Array<Record<string, unknown>> = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      const u = String(url)
      if (init?.method === 'POST' && u.endsWith('/perks')) { sink.push((JSON.parse(String(init.body)) as { perk: Record<string, unknown> }).perk); return { ok: true, status: 200, json: async () => ({}) } }
      if (u.endsWith('/catalog/perks')) return { ok: true, status: 200, json: async () => ({ perks: [perk, { id: 'beam_mastery', path: 'siphoner', config: { damageMultiplier: 1.5, healingMultiplier: 1.5 } }] }) }
      if (u.endsWith('/catalog/units')) return { ok: true, status: 200, json: async () => ({ units: [], paths: [], pathsByUnit: { acolyte: ['siphoner'] } }) }
      return { ok: true, status: 200, json: async () => ({}) }
    }) as unknown as typeof fetch)

    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await wrapper.findAll('.pk-side__unit').find((b) => b.text().includes('Acolyte'))!.trigger('click')
    await wrapper.findAll('.pk-side__path-label').find((b) => b.text().includes('siphoner'))!.trigger('click')
    await wrapper.findAll('[data-test="perk-row"]').find((r) => r.text().includes('ascended'))!.trigger('click')
    await flushPromises()
    await wrapper.find('[data-test="perk-modifier-card"][data-kind="perkModifier"]').trigger('click')

    // Add a second op — the row must appear and persist.
    await wrapper.findAll('button').find((b) => b.text().includes('Add Op'))!.trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-test="perk-inspector"] input[aria-label$="target key"]')).toHaveLength(2)

    // Type ONLY the target key of op 2 — the row must NOT vanish.
    const op2Target = wrapper.find('input[aria-label="Op 2 target key"]')
    await op2Target.setValue('healingMultiplier')
    await flushPromises()
    expect(wrapper.findAll('[data-test="perk-inspector"] input[aria-label$="target key"]')).toHaveLength(2)

    // Finish op 2 and save → both ops round-trip.
    await wrapper.find('input[aria-label="Op 2 source key"]').setValue('boost')
    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()
    expect(sink[0].perkModifiers).toEqual([{ target: 'beam_mastery', ops: [
      { targetKey: 'damageMultiplier', op: 'mult', sourceKey: 'boost' },
      { targetKey: 'healingMultiplier', op: 'mult', sourceKey: 'boost' },
    ] }])
  })

  it('drops an incomplete perk-modifier op on save', async () => {
    const perk = { id: 'ascended', path: 'siphoner', config: { boost: 0.2 }, perkModifiers: [{ target: 'beam_mastery', ops: [{ targetKey: 'damageMultiplier', op: 'mult', sourceKey: 'boost' }] }], wired: false }
    const sink: Array<Record<string, unknown>> = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      const u = String(url)
      if (init?.method === 'POST' && u.endsWith('/perks')) { sink.push((JSON.parse(String(init.body)) as { perk: Record<string, unknown> }).perk); return { ok: true, status: 200, json: async () => ({}) } }
      if (u.endsWith('/catalog/perks')) return { ok: true, status: 200, json: async () => ({ perks: [perk, { id: 'beam_mastery', path: 'siphoner', config: { damageMultiplier: 1.5 } }] }) }
      if (u.endsWith('/catalog/units')) return { ok: true, status: 200, json: async () => ({ units: [], paths: [], pathsByUnit: { acolyte: ['siphoner'] } }) }
      return { ok: true, status: 200, json: async () => ({}) }
    }) as unknown as typeof fetch)

    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await wrapper.findAll('.pk-side__unit').find((b) => b.text().includes('Acolyte'))!.trigger('click')
    await wrapper.findAll('.pk-side__path-label').find((b) => b.text().includes('siphoner'))!.trigger('click')
    await wrapper.findAll('[data-test="perk-row"]').find((r) => r.text().includes('ascended'))!.trigger('click')
    await flushPromises()
    await wrapper.find('[data-test="perk-modifier-card"][data-kind="perkModifier"]').trigger('click')

    // Add a blank second op and leave it incomplete, then save.
    await wrapper.findAll('button').find((b) => b.text().includes('Add Op'))!.trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()
    // Only the complete op survives.
    expect(sink[0].perkModifiers).toEqual([{ target: 'beam_mastery', ops: [{ targetKey: 'damageMultiplier', op: 'mult', sourceKey: 'boost' }] }])
  })

  // The real shared_suffering perk (server/internal/game/catalog/perks/
  // siphoner/shared_suffering/shared_suffering.json) — the canonical
  // load/round-trip fixture for the Ability Riders section. Mirrors
  // sharedSufferingPerk() in the classic PerkEditorPanel.test.ts.
  function sharedSufferingPerk() {
    return {
      id: 'shared_suffering',
      displayName: 'Shared Suffering',
      description: 'A portion of Siphon Life damage echoes to every nearby enemy.',
      path: 'siphoner',
      config: { damageSharePercent: 0.4, radius: 120 },
      abilityRiders: [
        {
          target: 'siphon_life',
          trigger: 'on_tick',
          actions: [
            {
              id: 'echo_targets',
              type: 'select_targets',
              target: {
                source: 'all_in_scene',
                origin: 'initial_target_position',
                relations: ['enemy'],
                radius: 120,
                excludeCurrentEvent: true,
                aliveState: 'alive',
              },
            },
            {
              id: 'echo_dmg',
              type: 'deal_damage',
              config: { amountRef: 'trigger_damage', amountMult: 0.4, type: 'shadow' },
            },
          ],
        },
      ],
      wired: false,
    }
  }

  // stubFetchWithRider covers every catalog endpoint RiderEditor's schema/
  // catalogs fetch touches (action-schema, damage-types, abilities, units),
  // plus /perks POST interception for the save round-trip. Mirrors
  // stubFetchWithRider() in the classic PerkEditorPanel.test.ts.
  function stubFetchWithRider(sink: Array<Record<string, unknown>>) {
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      const u = String(url)
      if (init?.method === 'POST' && u.endsWith('/perks')) {
        const body = JSON.parse(String(init.body)) as { perk: Record<string, unknown> }
        sink.push(body.perk)
        return { ok: true, status: 200, json: async () => ({}) }
      }
      if (u.endsWith('/catalog/perks')) {
        return { ok: true, status: 200, json: async () => ({ perks: [sharedSufferingPerk()] }) }
      }
      if (u.endsWith('/catalog/units')) {
        return { ok: true, status: 200, json: async () => ({ units: [{ type: 'acolyte' }], paths: [], pathsByUnit: { acolyte: ['siphoner'] } }) }
      }
      if (u.endsWith('/catalog/abilities')) {
        return { ok: true, status: 200, json: async () => ({ abilities: [{ id: 'siphon_life' }, { id: 'frost_bolt' }] }) }
      }
      if (u.endsWith('/catalog/action-schema')) {
        return {
          ok: true,
          status: 200,
          json: async () => ({
            actions: [
              {
                type: 'select_targets',
                runnable: true,
                fields: [{ key: 'target', label: 'Target Selection', control: 'target_query', section: 'Targeting' }],
              },
              {
                type: 'deal_damage',
                runnable: true,
                fields: [
                  { key: 'amount', label: 'Amount', control: 'number', section: 'Properties' },
                  { key: 'type', label: 'Damage Type', control: 'enum', section: 'Properties' },
                  { key: 'amountRef', label: 'Amount From (context)', control: 'text', section: 'Properties' },
                  { key: 'amountMult', label: 'Amount ×', control: 'number', section: 'Properties' },
                ],
              },
            ],
            enums: { triggerTypes: ['on_cast_complete', 'on_tick'], actionTypes: ['select_targets', 'deal_damage'] },
          }),
        }
      }
      if (u.endsWith('/catalog/damage-types')) {
        return { ok: true, status: 200, json: async () => ({ damageTypes: ['shadow', 'fire', 'physical'] }) }
      }
      return { ok: true, status: 200, json: async () => ({}) }
    }) as unknown as typeof fetch)
  }

  it('loads shared_suffering rider (2 actions) and round-trips it unedited', async () => {
    const shared = sharedSufferingPerk()
    const sink: Array<Record<string, unknown>> = []
    stubFetchWithRider(sink)
    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Acolyte', 'siphoner')

    await wrapper.find('[data-test="perk-modifier-card"][data-kind="abilityRider"]').trigger('click')
    await flushPromises()
    const cards = wrapper.findAll('[data-test="flow-action-card"]')
    expect(cards).toHaveLength(2)

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()
    expect(sink[0].abilityRiders).toEqual(shared.abilityRiders)
  })

  it('editing a rider action config round-trips the change', async () => {
    const shared = sharedSufferingPerk()
    const sink: Array<Record<string, unknown>> = []
    stubFetchWithRider(sink)
    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Acolyte', 'siphoner')

    await wrapper.find('[data-test="perk-modifier-card"][data-kind="abilityRider"]').trigger('click')
    await flushPromises()

    const dealDamageCard = wrapper.findAll('[data-test="flow-action-card"]').find((c) => c.text().includes('Deal Damage'))!
    await dealDamageCard.find('.flow-action__body').trigger('click')

    const inspector = wrapper.find('[data-test="rider-inspector"]')
    const amountMultInput = inspector.findAll('input[type="number"]').find((i) => (i.element as HTMLInputElement).value === '0.4')!
    ;(amountMultInput.element as HTMLInputElement).value = '0.6'
    await amountMultInput.trigger('input')
    await amountMultInput.trigger('change')

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()

    const riders = sink[0].abilityRiders as Array<{ target: string; trigger: string; actions: Array<{ id: string; config?: Record<string, unknown> }> }>
    expect(riders).toHaveLength(1)
    expect(riders[0].target).toBe('siphon_life')
    expect(riders[0].trigger).toBe('on_tick')
    expect(riders[0].actions[1].config).toEqual({ amountRef: 'trigger_damage', amountMult: 0.6, type: 'shadow' })
    // select_targets (untouched) still round-trips its own target query intact.
    expect(riders[0].actions[0]).toEqual(shared.abilityRiders[0].actions[0])
  })

  it('loads zealous_march aura via AuraEditor and round-trips it unedited', async () => {
    const perk = {
      id: 'zealous_march', path: 'cleric',
      config: { moveSpeedMultiplier: 0.3, radiusPixels: 192, stackBonus: 0.05 },
      auras: [{ radius: 192, targets: 'allies', includeSelf: true, stacking: 'max', perAdditionalSource: 0.05, statModifiers: [{ stat: 'moveSpeed', op: 'add', value: 0.3 }] }],
      wired: false,
    }
    const sink: Array<Record<string, unknown>> = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      const u = String(url)
      if (init?.method === 'POST' && u.endsWith('/perks')) { sink.push((JSON.parse(String(init.body)) as { perk: Record<string, unknown> }).perk); return { ok: true, status: 200, json: async () => ({}) } }
      if (u.endsWith('/catalog/perks')) return { ok: true, status: 200, json: async () => ({ perks: [perk] }) }
      if (u.endsWith('/catalog/units')) return { ok: true, status: 200, json: async () => ({ units: [], paths: [], pathsByUnit: { acolyte: ['cleric'] } }) }
      return { ok: true, status: 200, json: async () => ({}) }
    }) as unknown as typeof fetch)

    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Acolyte', 'cleric')

    const card = wrapper.find('[data-test="perk-modifier-card"][data-kind="aura"]')
    expect(card.exists()).toBe(true)
    await card.trigger('click')
    // AuraEditor renders inside the inspector, seeded from the PerkAura.
    const inspector = wrapper.find('[data-test="perk-inspector"]')
    expect((inspector.find('input[aria-label="Aura radius"]').element as HTMLInputElement).value).toBe('192')

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()
    expect(sink[0].auras).toEqual(perk.auras)
  })

  it('loads a cosmetic effect and round-trips it unedited', async () => {
    const perk = { id: 'burner', path: 'trapper', effect: { name: 'burning', target: 'enemies', durationSeconds: 3 }, wired: false }
    const sink: Array<Record<string, unknown>> = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      const u = String(url)
      if (init?.method === 'POST' && u.endsWith('/perks')) { sink.push((JSON.parse(String(init.body)) as { perk: Record<string, unknown> }).perk); return { ok: true, status: 200, json: async () => ({}) } }
      if (u.endsWith('/catalog/perks')) return { ok: true, status: 200, json: async () => ({ perks: [perk] }) }
      if (u.endsWith('/catalog/units')) return { ok: true, status: 200, json: async () => ({ units: [], paths: [], pathsByUnit: { archer: ['trapper'] } }) }
      return { ok: true, status: 200, json: async () => ({}) }
    }) as unknown as typeof fetch)

    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Archer', 'trapper')

    const card = wrapper.find('[data-test="perk-modifier-card"][data-kind="effect"]')
    expect(card.exists()).toBe(true)
    await card.trigger('click')
    const inspector = wrapper.find('[data-test="perk-inspector"]')
    expect((inspector.find('input[aria-label="Effect name"]').element as HTMLInputElement).value).toBe('burning')

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()
    expect(sink[0].effect).toEqual(perk.effect)
  })

  it('adds an effect via quick-add but drops it on save when the name is left blank', async () => {
    const sink: Array<Record<string, unknown>> = []
    stub(amplified, sink) // amplified is a trapper perk with no effect
    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Archer', 'trapper')

    // Add an effect via quick-add (leaves name blank).
    await wrapper.find('[data-test="quick-add-effect"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="perk-modifier-card"][data-kind="effect"]').exists()).toBe(true)

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()
    // Blank-name effect is cleaned out by the hub's save step.
    expect('effect' in sink[0]).toBe(false)
  })

  it('Reset reverts unsaved edits to the last-saved state', async () => {
    stub(amplified)
    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Archer', 'trapper')

    await wrapper.find('[data-test="perk-modifier-card"][data-kind="abilityField"]').trigger('click')
    await wrapper.find('[data-test="perk-inspector"] input[aria-label="Value"]').setValue('1.5')

    await wrapper.findAll('button').find((b) => b.text() === 'Reset')!.trigger('click')
    await flushPromises()

    // Re-inspect the ability-field card — value is back to the saved 1.35.
    await wrapper.find('[data-test="perk-modifier-card"][data-kind="abilityField"]').trigger('click')
    expect((wrapper.find('[data-test="perk-inspector"] input[aria-label="Value"]').element as HTMLInputElement).value).toBe('1.35')
  })

  it('switches tabs between Identity, Modifiers, and Config', async () => {
    stub(amplified)
    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Archer', 'trapper')

    // Modifiers tab is default → cards visible.
    expect(wrapper.find('[data-test="perk-modifier-card"]').exists()).toBe(true)

    // Identity tab → Display Name field present, cards gone.
    await wrapper.find('[data-test="perk-builder-tab-identity"]').trigger('click')
    expect(wrapper.findAll('label').some((l) => l.text().includes('Display Name'))).toBe(true)
    expect(wrapper.find('[data-test="perk-modifier-card"]').exists()).toBe(false)

    // Config tab → Add Config Value button present.
    await wrapper.find('[data-test="perk-builder-tab-config"]').trigger('click')
    expect(wrapper.findAll('button').some((b) => b.text().includes('Add Config Value'))).toBe(true)
  })

  it('Delete removes the open perk via the server', async () => {
    const deletes: string[] = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      const u = String(url)
      if (init?.method === 'DELETE' && u.includes('/perks/')) { deletes.push(u); return { ok: true, status: 200, json: async () => ({ id: 'amplified_effects', status: 'reset' }) } }
      if (u.endsWith('/catalog/perks')) return { ok: true, status: 200, json: async () => ({ perks: [amplified] }) }
      if (u.endsWith('/catalog/units')) return { ok: true, status: 200, json: async () => ({ units: [], paths: [], pathsByUnit: { archer: ['trapper'] } }) }
      if (u.endsWith('/catalog/abilities')) return { ok: true, status: 200, json: async () => ({ abilities: [markerTrap] }) }
      if (u.endsWith('/catalog/ability-stats')) return { ok: true, status: 200, json: async () => ({ stats: [] }) }
      return { ok: true, status: 200, json: async () => ({}) }
    }) as unknown as typeof fetch)

    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Archer', 'trapper')

    await wrapper.findAll('button').find((b) => b.text() === 'Delete')!.trigger('click')
    await flushPromises()
    expect(deletes.some((u) => u.includes('/perks/amplified_effects'))).toBe(true)
  })

  it('picks a granted ability via the Ability Picker modal', async () => {
    const perk = { id: 'granter', path: 'siphoner', grantsAbilities: ['dash'], wired: false }
    const sink: Array<Record<string, unknown>> = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      const u = String(url)
      if (init?.method === 'POST' && u.endsWith('/perks')) { sink.push((JSON.parse(String(init.body)) as { perk: Record<string, unknown> }).perk); return { ok: true, status: 200, json: async () => ({}) } }
      if (u.endsWith('/catalog/perks')) return { ok: true, status: 200, json: async () => ({ perks: [perk] }) }
      if (u.endsWith('/catalog/units')) return { ok: true, status: 200, json: async () => ({ units: [], paths: [], pathsByUnit: { acolyte: ['siphoner'] } }) }
      if (u.endsWith('/catalog/abilities')) return { ok: true, status: 200, json: async () => ({ abilities: [{ id: 'dash', displayName: 'Dash' }, { id: 'sprint', displayName: 'Sprint' }] }) }
      return { ok: true, status: 200, json: async () => ({}) }
    }) as unknown as typeof fetch)

    const wrapper = mount(PerkBuilderPanel)
    await flushPromises()
    await openPerk(wrapper, 'Acolyte', 'siphoner')

    await wrapper.find('[data-test="perk-modifier-card"][data-kind="grantAbility"]').trigger('click')
    // open the picker via the Granted Ability dropdown
    await wrapper.find('button[aria-label="Granted Ability"]').trigger('click')
    expect(wrapper.find('[data-test="ability-picker"]').exists()).toBe(true)

    // choose 'sprint' and confirm
    await wrapper.find('[data-test="ability-picker-item-sprint"]').trigger('click')
    await wrapper.find('[data-test="ability-picker-confirm"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="perk-inspector"] button[aria-label="Granted Ability"]').text()).toContain('sprint')

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()
    expect(sink[0].grantsAbilities).toEqual(['sprint'])
  })
})
