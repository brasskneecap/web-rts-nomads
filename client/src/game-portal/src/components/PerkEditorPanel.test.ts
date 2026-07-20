import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import PerkEditorPanel from './PerkEditorPanel.vue'
import AuraEditor from './perk-editor/AuraEditor.vue'

function stubFetch() {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (String(url).endsWith('/catalog/perks')) {
      return {
        ok: true,
        status: 200,
        json: async () => ({ perks: [{ id: 'bloodlust', displayName: 'Bloodlust', rank: 'bronze', wired: true }] }),
      }
    }
    if (String(url).endsWith('/catalog/units')) {
      return { ok: true, status: 200, json: async () => ({ units: [{ type: 'soldier' }], paths: [], pathsByUnit: {} }) }
    }
    return { ok: true, status: 200, json: async () => ({}) }
  }) as unknown as typeof fetch)
}

function stubFetchGrouped() {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (String(url).endsWith('/catalog/perks')) {
      return {
        ok: true,
        status: 200,
        json: async () => ({
          perks: [
            { id: 'lingering_hex', path: 'siphoner' },
            { id: 'caltrops', path: 'trapper' },
            { id: 'g_perk' },
          ],
        }),
      }
    }
    if (String(url).endsWith('/catalog/units')) {
      return {
        ok: true,
        status: 200,
        json: async () => ({
          units: [],
          paths: [],
          pathsByUnit: { acolyte: ['cleric', 'siphoner'], archer: ['marksman', 'trapper'] },
        }),
      }
    }
    return { ok: true, status: 200, json: async () => ({}) }
  }) as unknown as typeof fetch)
}

// Same catalog fixtures as stubFetchGrouped, but also intercepts the perk
// save POST (perkEditorApi.saveEditorPerk -> POST /perks with { perk })
// and records the decoded body so tests can assert on the association
// (form.path) the request carried.
function stubFetchGroupedWithSave(savedPerks: Array<Record<string, unknown>>) {
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === 'POST' && String(url).endsWith('/perks')) {
      const body = JSON.parse(String(init.body)) as { perk: Record<string, unknown> }
      savedPerks.push(body.perk)
      return { ok: true, status: 200, json: async () => ({}) }
    }
    if (String(url).endsWith('/catalog/perks')) {
      return {
        ok: true,
        status: 200,
        json: async () => ({
          perks: [
            { id: 'lingering_hex', path: 'siphoner' },
            { id: 'caltrops', path: 'trapper' },
            { id: 'g_perk' },
          ],
        }),
      }
    }
    if (String(url).endsWith('/catalog/units')) {
      return {
        ok: true,
        status: 200,
        json: async () => ({
          units: [],
          paths: [],
          pathsByUnit: { acolyte: ['cleric', 'siphoner'], archer: ['marksman', 'trapper'] },
        }),
      }
    }
    return { ok: true, status: 200, json: async () => ({}) }
  }) as unknown as typeof fetch)
}

function inputForLabel(wrapper: VueWrapper, labelText: string) {
  const label = wrapper.findAll('label').find((l) => l.text().startsWith(labelText))
  if (!label) throw new Error(`label not found: ${labelText}`)
  return label.find('input')
}

// Groups start collapsed; click a unit or path header (matched by its text) to
// expand it. Used to reveal sub-headers / perk rows before asserting on them.
async function clickHeader(wrapper: VueWrapper, text: string) {
  const headers = [
    ...wrapper.findAll('.perk-editor__group-unit'),
    ...wrapper.findAll('.perk-editor__group-path-label'),
  ]
  const header = headers.find((h) => h.text().includes(text))
  if (!header) throw new Error(`group header not found: ${text}`)
  await header.trigger('click')
}

// The real shared_suffering perk (server/internal/game/catalog/perks/
// siphoner/shared_suffering/shared_suffering.json) — the canonical
// load/round-trip fixture for the Ability Riders section.
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
// catalogs fetch touches (action-schema, damage-types, ability-categories,
// autocast-selectors, effects, projectiles, abilities, units), plus /perks
// POST interception for the save round-trip.
function stubFetchWithRider(savedPerks: Array<Record<string, unknown>>) {
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    if (init?.method === 'POST' && u.endsWith('/perks')) {
      const body = JSON.parse(String(init.body)) as { perk: Record<string, unknown> }
      savedPerks.push(body.perk)
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

// The real beam_mastery perk (server/internal/game/catalog/perks/siphoner/
// beam_mastery/beam_mastery.json) — the canonical load/round-trip fixture
// for abilityModifiers: all 4 mults (damage/heal/mana/range) set.
function beamMasteryPerk() {
  return {
    id: 'beam_mastery',
    displayName: 'Beam Mastery',
    description: 'Siphon Life deals more damage, heals more, reaches further, and costs less mana. Adds an extra chain target when paired with Chain Siphon.',
    path: 'siphoner',
    config: {
      chainAdditionalTargetBonus: 1,
      damageMultiplier: 1.5,
      healingMultiplier: 1.5,
      manaCostMultiplier: 0.5,
      rangeMultiplier: 1.25,
    },
    abilityModifiers: [
      { target: 'siphon_life', damageMult: 1.5, healMult: 1.5, manaCostMult: 0.5, rangeMult: 1.25 },
    ],
    wired: false,
  }
}

// A synthetic perk carrying statModifiers (server shape: PerkDef.statModifiers
// is a list of { stat, op, value, stage? }). Two rows exercise both the
// default-omitted stage ("base") and an explicit "final" stage, and both ops.
function statBoostPerk() {
  return {
    id: 'stat_boost',
    displayName: 'Stat Boost',
    path: 'siphoner',
    statModifiers: [
      { stat: 'maxHp', op: 'add', value: 25 },
      { stat: 'damage', op: 'multiply', value: 1.2, stage: 'final' },
    ],
    wired: false,
  }
}

function stubFetchWithStatModifiers(savedPerks: Array<Record<string, unknown>>) {
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    if (init?.method === 'POST' && u.endsWith('/perks')) {
      const body = JSON.parse(String(init.body)) as { perk: Record<string, unknown> }
      savedPerks.push(body.perk)
      return { ok: true, status: 200, json: async () => ({}) }
    }
    if (u.endsWith('/catalog/perks')) {
      return { ok: true, status: 200, json: async () => ({ perks: [statBoostPerk()] }) }
    }
    if (u.endsWith('/catalog/units')) {
      return { ok: true, status: 200, json: async () => ({ units: [{ type: 'acolyte' }], paths: [], pathsByUnit: { acolyte: ['siphoner'] } }) }
    }
    return { ok: true, status: 200, json: async () => ({}) }
  }) as unknown as typeof fetch)
}

// The real zealous_march perk (server/internal/game/catalog/perks/cleric/
// zealous_march/zealous_march.json) — the canonical load/round-trip fixture
// for the Auras section.
function zealousMarchPerk() {
  return {
    id: 'zealous_march',
    displayName: 'Zealous March',
    description: 'Nearby allies gain bonus movement speed. Additional Clerics add a smaller stacking bonus.',
    path: 'cleric',
    config: { moveSpeedMultiplier: 0.3, radiusPixels: 192, stackBonus: 0.05 },
    auras: [
      {
        radius: 192,
        targets: 'allies',
        includeSelf: true,
        stacking: 'max',
        perAdditionalSource: 0.05,
        statModifiers: [{ stat: 'moveSpeed', op: 'add', value: 0.3 }],
      },
    ],
    wired: false,
  }
}

// zealousMarchPerkWithRingColor is zealousMarchPerk() with a ringColor
// override authored on its one aura — the fixture for the Ring Color
// round-trip / clear tests below.
function zealousMarchPerkWithRingColor() {
  const perk = zealousMarchPerk()
  return { ...perk, auras: [{ ...perk.auras[0], ringColor: '#38bdf8' }] }
}

function stubFetchWithAuras(savedPerks: Array<Record<string, unknown>>, perk: Record<string, unknown> = zealousMarchPerk()) {
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    if (init?.method === 'POST' && u.endsWith('/perks')) {
      const body = JSON.parse(String(init.body)) as { perk: Record<string, unknown> }
      savedPerks.push(body.perk)
      return { ok: true, status: 200, json: async () => ({}) }
    }
    if (u.endsWith('/catalog/perks')) {
      return { ok: true, status: 200, json: async () => ({ perks: [perk] }) }
    }
    if (u.endsWith('/catalog/units')) {
      return { ok: true, status: 200, json: async () => ({ units: [{ type: 'acolyte' }], paths: [], pathsByUnit: { acolyte: ['cleric'] } }) }
    }
    return { ok: true, status: 200, json: async () => ({}) }
  }) as unknown as typeof fetch)
}

function stubFetchWithAbilityModifiers(savedPerks: Array<Record<string, unknown>>) {
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    if (init?.method === 'POST' && u.endsWith('/perks')) {
      const body = JSON.parse(String(init.body)) as { perk: Record<string, unknown> }
      savedPerks.push(body.perk)
      return { ok: true, status: 200, json: async () => ({}) }
    }
    if (u.endsWith('/catalog/perks')) {
      return { ok: true, status: 200, json: async () => ({ perks: [beamMasteryPerk()] }) }
    }
    if (u.endsWith('/catalog/units')) {
      return { ok: true, status: 200, json: async () => ({ units: [{ type: 'acolyte' }], paths: [], pathsByUnit: { acolyte: ['siphoner'] } }) }
    }
    if (u.endsWith('/catalog/abilities')) {
      return { ok: true, status: 200, json: async () => ({ abilities: [{ id: 'siphon_life' }] }) }
    }
    return { ok: true, status: 200, json: async () => ({}) }
  }) as unknown as typeof fetch)
}

afterEach(() => vi.restoreAllMocks())

describe('PerkEditorPanel', () => {
  it('lists a perk from the catalog once its group is expanded', async () => {
    stubFetch()
    const wrapper = mount(PerkEditorPanel)
    await flushPromises()
    // Groups start collapsed: the unit header shows, the perk row does not.
    expect(wrapper.text()).toContain('Generic')
    expect(wrapper.text()).not.toContain('Bloodlust')
    await clickHeader(wrapper, 'Generic')
    expect(wrapper.text()).toContain('Bloodlust')
  })

  it('groups perks by unit and path in the sidebar, with a Generic bucket', async () => {
    stubFetchGrouped()
    const wrapper = mount(PerkEditorPanel)
    await flushPromises()

    // Everything starts collapsed — only the unit headers are visible.
    expect(wrapper.text()).toContain('Acolyte')
    expect(wrapper.text()).toContain('Generic')
    expect(wrapper.findAll('[data-test="perk-row"]')).toHaveLength(0)

    // Expanding a unit reveals its path sub-headers.
    await clickHeader(wrapper, 'Acolyte')
    expect(wrapper.text()).toContain('siphoner')

    // Drill into each group to reveal its perks (Generic has no path sub-level).
    await clickHeader(wrapper, 'siphoner')
    await clickHeader(wrapper, 'Archer')
    await clickHeader(wrapper, 'trapper')
    await clickHeader(wrapper, 'Generic')

    const rowIds = wrapper.findAll('[data-test="perk-row"]').map((r) => r.text())
    expect(rowIds.some((t) => t.includes('lingering_hex'))).toBe(true)
    expect(rowIds.some((t) => t.includes('caltrops'))).toBe(true)
    expect(rowIds.some((t) => t.includes('g_perk'))).toBe(true)
  })

  it('lets a new perk pick its association and saves it under the chosen path', async () => {
    const savedPerks: Array<Record<string, unknown>> = []
    stubFetchGroupedWithSave(savedPerks)
    const wrapper = mount(PerkEditorPanel)
    await flushPromises()

    await wrapper.find('.perk-editor__new').trigger('click')

    // Creating a new perk: association is an editable select, not the
    // read-only input shown when editing an existing perk.
    const select = wrapper.find('select[data-test="association-select"]')
    expect(select.exists()).toBe(true)
    const associationLabel = wrapper.findAll('label').find((l) => l.text().startsWith('Association'))!
    expect(associationLabel.find('input[disabled]').exists()).toBe(false)

    await select.setValue('siphoner')
    await inputForLabel(wrapper, 'Id').setValue('new_perk')
    await inputForLabel(wrapper, 'Display Name').setValue('New Perk')

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()

    expect(savedPerks).toHaveLength(1)
    expect(savedPerks[0].path).toBe('siphoner')
  })

  it('saves a new perk with no path when Generic is chosen', async () => {
    const savedPerks: Array<Record<string, unknown>> = []
    stubFetchGroupedWithSave(savedPerks)
    const wrapper = mount(PerkEditorPanel)
    await flushPromises()

    await wrapper.find('.perk-editor__new').trigger('click')

    const select = wrapper.find('select[data-test="association-select"]')
    await select.setValue('')
    await inputForLabel(wrapper, 'Id').setValue('generic_perk')
    await inputForLabel(wrapper, 'Display Name').setValue('Generic Perk')

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()

    expect(savedPerks).toHaveLength(1)
    expect('path' in savedPerks[0]).toBe(false)
  })

  it('loads shared_suffering\'s ability rider and round-trips it (unedited) through save', async () => {
    const savedPerks: Array<Record<string, unknown>> = []
    stubFetchWithRider(savedPerks)
    const wrapper = mount(PerkEditorPanel)
    await flushPromises()

    await clickHeader(wrapper, 'Acolyte')
    await clickHeader(wrapper, 'siphoner')
    await wrapper.find('[data-test="perk-row"]').trigger('click')
    await flushPromises()

    // The rider's two actions render as real FlowActionCards.
    const cards = wrapper.findAll('[data-test="flow-action-card"]')
    expect(cards).toHaveLength(2)
    expect(cards[0].text()).toContain('Select Targets')
    expect(cards[1].text()).toContain('Deal Damage')

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()

    expect(savedPerks).toHaveLength(1)
    expect(savedPerks[0].abilityRiders).toEqual(sharedSufferingPerk().abilityRiders)
  })

  it('editing a rider action config (deal_damage amountMult) round-trips the change through save', async () => {
    const savedPerks: Array<Record<string, unknown>> = []
    stubFetchWithRider(savedPerks)
    const wrapper = mount(PerkEditorPanel)
    await flushPromises()

    await clickHeader(wrapper, 'Acolyte')
    await clickHeader(wrapper, 'siphoner')
    await wrapper.find('[data-test="perk-row"]').trigger('click')
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

    expect(savedPerks).toHaveLength(1)
    const riders = savedPerks[0].abilityRiders as Array<{ target: string; trigger: string; actions: Array<{ id: string; config?: Record<string, unknown> }> }>
    expect(riders).toHaveLength(1)
    expect(riders[0].target).toBe('siphon_life')
    expect(riders[0].trigger).toBe('on_tick')
    expect(riders[0].actions[1].config).toEqual({ amountRef: 'trigger_damage', amountMult: 0.6, type: 'shadow' })
    // select_targets (untouched) still round-trips its own target query intact.
    expect(riders[0].actions[0]).toEqual(sharedSufferingPerk().abilityRiders[0].actions[0])
  })

  it('loads beam_mastery\'s ability modifier (4 mults set) and round-trips it unedited through save', async () => {
    const savedPerks: Array<Record<string, unknown>> = []
    stubFetchWithAbilityModifiers(savedPerks)
    const wrapper = mount(PerkEditorPanel)
    await flushPromises()

    await clickHeader(wrapper, 'Acolyte')
    await clickHeader(wrapper, 'siphoner')
    await wrapper.find('[data-test="perk-row"]').trigger('click')
    await flushPromises()

    // The row is populated with exactly the 4 authored mults.
    expect((wrapper.find('input[aria-label="Ability Modifier 1 target"]').element as HTMLInputElement).value).toBe('siphon_life')
    expect((wrapper.find('input[aria-label="Ability Modifier 1 damage mult"]').element as HTMLInputElement).value).toBe('1.5')
    expect((wrapper.find('input[aria-label="Ability Modifier 1 heal mult"]').element as HTMLInputElement).value).toBe('1.5')
    expect((wrapper.find('input[aria-label="Ability Modifier 1 mana cost mult"]').element as HTMLInputElement).value).toBe('0.5')
    expect((wrapper.find('input[aria-label="Ability Modifier 1 range mult"]').element as HTMLInputElement).value).toBe('1.25')

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()

    // Saving unedited must reproduce the original array exactly — blanks
    // stay absent (no stray 0s written), no key reordering/loss, single row.
    expect(savedPerks).toHaveLength(1)
    expect(savedPerks[0].abilityModifiers).toEqual(beamMasteryPerk().abilityModifiers)
  })

  it('loads stat_boost\'s stat modifiers (add + multiply, base + final) and round-trips them unedited through save', async () => {
    const savedPerks: Array<Record<string, unknown>> = []
    stubFetchWithStatModifiers(savedPerks)
    const wrapper = mount(PerkEditorPanel)
    await flushPromises()

    await clickHeader(wrapper, 'Acolyte')
    await clickHeader(wrapper, 'siphoner')
    await wrapper.find('[data-test="perk-row"]').trigger('click')
    await flushPromises()

    // Row 1: maxHp / add / 25 / base (stage omitted in the source -> defaults to base).
    const statSelect1 = wrapper.find('select[aria-label="Stat Modifier 1 stat"]')
    expect((statSelect1.element as HTMLSelectElement).value).toBe('maxHp')
    expect((wrapper.find('select[aria-label="Stat Modifier 1 op"]').element as HTMLSelectElement).value).toBe('add')
    expect((wrapper.find('input[aria-label="Stat Modifier 1 value"]').element as HTMLInputElement).value).toBe('25')
    expect((wrapper.find('select[aria-label="Stat Modifier 1 stage"]').element as HTMLSelectElement).value).toBe('base')

    // Row 2: damage / multiply / 1.2 / final.
    expect((wrapper.find('select[aria-label="Stat Modifier 2 stat"]').element as HTMLSelectElement).value).toBe('damage')
    expect((wrapper.find('select[aria-label="Stat Modifier 2 op"]').element as HTMLSelectElement).value).toBe('multiply')
    expect((wrapper.find('input[aria-label="Stat Modifier 2 value"]').element as HTMLInputElement).value).toBe('1.2')
    expect((wrapper.find('select[aria-label="Stat Modifier 2 stage"]').element as HTMLSelectElement).value).toBe('final')

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()

    expect(savedPerks).toHaveLength(1)
    expect(savedPerks[0].statModifiers).toEqual(statBoostPerk().statModifiers)
  })

  it('loads zealous_march\'s aura and round-trips it unedited through save', async () => {
    const savedPerks: Array<Record<string, unknown>> = []
    stubFetchWithAuras(savedPerks)
    const wrapper = mount(PerkEditorPanel)
    await flushPromises()

    await clickHeader(wrapper, 'Acolyte')
    await clickHeader(wrapper, 'cleric')
    await wrapper.find('[data-test="perk-row"]').trigger('click')
    await flushPromises()

    expect(wrapper.findComponent(AuraEditor).exists()).toBe(true)
    expect((wrapper.find('input[aria-label="Aura radius"]').element as HTMLInputElement).value).toBe('192')
    expect((wrapper.find('select[aria-label="Aura targets"]').element as HTMLSelectElement).value).toBe('allies')
    expect((wrapper.find('input[aria-label="Aura include self"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.find('input[aria-label="Aura per additional source"]').element as HTMLInputElement).value).toBe('0.05')
    expect((wrapper.find('select[aria-label="Aura Stat 1 stat"]').element as HTMLSelectElement).value).toBe('moveSpeed')
    expect((wrapper.find('input[aria-label="Aura Stat 1 value"]').element as HTMLInputElement).value).toBe('0.3')

    // No op/stage controls are rendered anywhere in the Auras section — the
    // server rejects anything but op:"add"/stage omitted for aura stat rows.
    const aurasSectionHeading = wrapper.findAll('.perk-editor__section-title').find((h) => h.text() === 'Auras')!
    const aurasSection = aurasSectionHeading.element.closest('.perk-editor__section')!
    expect(aurasSection.querySelector('[aria-label="Aura Stat 1 op"]')).toBeNull()
    expect(aurasSection.querySelector('[aria-label="Aura Stat 1 stage"]')).toBeNull()

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()

    expect(savedPerks).toHaveLength(1)
    expect(savedPerks[0].auras).toEqual(zealousMarchPerk().auras)
  })

  it('loads an aura ringColor override, shows it checked with the authored color, and round-trips it unedited through save', async () => {
    const savedPerks: Array<Record<string, unknown>> = []
    stubFetchWithAuras(savedPerks, zealousMarchPerkWithRingColor())
    const wrapper = mount(PerkEditorPanel)
    await flushPromises()

    await clickHeader(wrapper, 'Acolyte')
    await clickHeader(wrapper, 'cleric')
    await wrapper.find('[data-test="perk-row"]').trigger('click')
    await flushPromises()

    const checkbox = wrapper.find('input[aria-label="Override aura ring color"]')
    expect((checkbox.element as HTMLInputElement).checked).toBe(true)
    const colorInput = wrapper.find('input[aria-label="Aura ring color"]')
    expect((colorInput.element as HTMLInputElement).value).toBe('#38bdf8')

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()

    expect(savedPerks).toHaveLength(1)
    expect(savedPerks[0].auras).toEqual(zealousMarchPerkWithRingColor().auras)
  })

  it('unchecking the ring color override omits ringColor from the saved aura', async () => {
    const savedPerks: Array<Record<string, unknown>> = []
    stubFetchWithAuras(savedPerks, zealousMarchPerkWithRingColor())
    const wrapper = mount(PerkEditorPanel)
    await flushPromises()

    await clickHeader(wrapper, 'Acolyte')
    await clickHeader(wrapper, 'cleric')
    await wrapper.find('[data-test="perk-row"]').trigger('click')
    await flushPromises()

    const checkbox = wrapper.find('input[aria-label="Override aura ring color"]')
    await checkbox.setValue(false)
    // Unchecking hides the color input entirely, not just clears its value.
    expect(wrapper.find('input[aria-label="Aura ring color"]').exists()).toBe(false)

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()

    expect(savedPerks).toHaveLength(1)
    const auras = savedPerks[0].auras as Array<Record<string, unknown>>
    expect(auras).toHaveLength(1)
    expect('ringColor' in auras[0]).toBe(false)
  })

  it('excludes aura-only stats from the Unit Stat Modifiers dropdown but includes them in the Aura stat dropdown', async () => {
    // aura-only stats (armorPercent, projectileDamageReduction — statDef.
    // AuraOnly, stat_modifiers.go) have no top-level fold site: the server
    // rejects them on a top-level statModifiers entry, so the Unit Stat
    // Modifiers dropdown must not offer them at all. The IDENTICAL stats
    // remain valid inside an aura's stat contributions, so the Aura stat
    // dropdown must keep offering them.
    stubFetchWithAuras([])
    const wrapper = mount(PerkEditorPanel)
    await flushPromises()

    await clickHeader(wrapper, 'Acolyte')
    await clickHeader(wrapper, 'cleric')
    await wrapper.find('[data-test="perk-row"]').trigger('click')
    await flushPromises()

    // Add a Unit Stat Modifiers row to expose its stat <select>.
    await wrapper.findAll('button').find((b) => b.text() === '+ Add Stat Modifier')!.trigger('click')
    await flushPromises()

    const selfStatSelect = wrapper.find('select[aria-label="Stat Modifier 1 stat"]')
    const selfOptionValues = selfStatSelect.findAll('option').map((o) => (o.element as HTMLOptionElement).value)
    expect(selfOptionValues).not.toContain('armorPercent')
    expect(selfOptionValues).not.toContain('projectileDamageReduction')
    expect(selfOptionValues).toContain('maxHp')

    // zealous_march already carries one aura stat row (moveSpeed) — assert
    // against ITS dropdown's options.
    const auraStatSelect = wrapper.find('select[aria-label="Aura Stat 1 stat"]')
    const auraOptionValues = auraStatSelect.findAll('option').map((o) => (o.element as HTMLOptionElement).value)
    expect(auraOptionValues).toContain('armorPercent')
    expect(auraOptionValues).toContain('projectileDamageReduction')
  })

  it('displays a loaded perk\'s generatedDescription in the Tooltip section, read-only', async () => {
    stubFetchGroupedWithGeneratedDescription()
    const wrapper = mount(PerkEditorPanel)
    await flushPromises()

    await clickHeader(wrapper, 'Generic')
    await wrapper.find('[data-test="perk-row"]').trigger('click')
    await flushPromises()

    const generatedField = wrapper.find('textarea.perk-editor__generated')
    expect(generatedField.exists()).toBe(true)
    expect((generatedField.element as HTMLTextAreaElement).value).toBe('+90 Max Health.')
    expect(generatedField.attributes('readonly')).toBeDefined()
  })

  it('does NOT include generatedDescription in the emitted save payload', async () => {
    const savedPerks: Array<Record<string, unknown>> = []
    stubFetchGroupedWithGeneratedDescription(savedPerks)
    const wrapper = mount(PerkEditorPanel)
    await flushPromises()

    await clickHeader(wrapper, 'Generic')
    await wrapper.find('[data-test="perk-row"]').trigger('click')
    await flushPromises()

    await wrapper.findAll('button').find((b) => b.text() === 'Save')!.trigger('click')
    await flushPromises()

    expect(savedPerks).toHaveLength(1)
    expect('generatedDescription' in savedPerks[0]).toBe(false)
  })
})

// stubFetchGroupedWithGeneratedDescription serves a single generic perk
// (hold_the_line) carrying a server-computed generatedDescription, and
// intercepts the /perks save POST when a savedPerks sink is provided.
function stubFetchGroupedWithGeneratedDescription(savedPerks?: Array<Record<string, unknown>>) {
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    if (init?.method === 'POST' && u.endsWith('/perks')) {
      const body = JSON.parse(String(init.body)) as { perk: Record<string, unknown> }
      savedPerks?.push(body.perk)
      return { ok: true, status: 200, json: async () => ({}) }
    }
    if (u.endsWith('/catalog/perks')) {
      return {
        ok: true,
        status: 200,
        json: async () => ({
          perks: [
            {
              id: 'hold_the_line',
              displayName: 'Hold the Line',
              statModifiers: [{ stat: 'maxHp', op: 'add', value: 90 }],
              generatedDescription: '+90 Max Health.',
              wired: true,
            },
          ],
        }),
      }
    }
    if (u.endsWith('/catalog/units')) {
      return { ok: true, status: 200, json: async () => ({ units: [], paths: [], pathsByUnit: {} }) }
    }
    return { ok: true, status: 200, json: async () => ({}) }
  }) as unknown as typeof fetch)
}
