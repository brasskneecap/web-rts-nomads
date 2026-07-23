import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import PreviewSceneControls, { type PreviewSceneConfig } from './PreviewSceneControls.vue'

// lastConfig reads the most recent emitted scene config.
function lastConfig(wrapper: ReturnType<typeof mount>): PreviewSceneConfig {
  const events = wrapper.emitted('update:modelValue')!
  return events[events.length - 1][0] as PreviewSceneConfig
}

describe('PreviewSceneControls', () => {
  describe('caster perks', () => {
    const perkOptions = [
      { id: 'lasting_flames', label: 'Lasting Flames' },
      { id: 'wider_nets', label: 'Wider Nets' },
      { id: 'overload_protocol', label: 'Overload Protocol' },
    ]

    // A unit carries at most ONE perk per rank, and which perks a rank offers
    // comes from the PATH's own perksByRank — the sole source of a perk's rank.
    function stubCatalogs() {
      vi.stubGlobal('fetch', vi.fn(async (url: string) => {
        const u = String(url)
        if (u.endsWith('/catalog/units')) {
          return {
            ok: true,
            json: async () => ({
              units: [{ type: 'archer', name: 'Archer' }],
              pathsByUnit: { archer: ['trapper'] },
            }),
          }
        }
        if (u.endsWith('/catalog/paths')) {
          return {
            ok: true,
            json: async () => ({
              paths: [{
                path: 'trapper',
                def: {
                  perksByRank: {
                    silver: ['wider_nets', 'lasting_flames'],
                    gold: ['overload_protocol'],
                  },
                },
              }],
            }),
          }
        }
        return { ok: true, json: async () => ({}) }
      }) as unknown as typeof fetch)
    }

    // Ranks up to GOLD so every row the path grants is visible — the rank filter
    // has its own tests below. (The fixture path, like the real trapper, grants
    // no bronze perks at all, so a bronze caster would see no rows.)
    async function mountWithPath(rank = 'gold') {
      stubCatalogs()
      const wrapper = mount(PreviewSceneControls, { props: { perkOptions } })
      await flushPromises()
      await wrapper.find('[data-test="preview-caster-unit"]').setValue('archer')
      await wrapper.find('[data-test="preview-caster-path"]').setValue('trapper')
      await wrapper.find('[data-test="preview-caster-rank"]').setValue(rank)
      return wrapper
    }

    function lastPerks(wrapper: ReturnType<typeof mount>): string[] {
      const emitted = wrapper.emitted('update:modelValue')!
      return (emitted[emitted.length - 1][0] as { casterPerks: string[] }).casterPerks
    }

    it('offers no perk rows until a path is chosen', async () => {
      stubCatalogs()
      const wrapper = mount(PreviewSceneControls, { props: { perkOptions } })
      await flushPromises()
      expect(wrapper.find('[data-test="preview-perks"]').exists()).toBe(false)
      expect(lastPerks(wrapper)).toEqual([])
    })

    // One dropdown per rank the path actually grants at — bronze has no bucket
    // here, so no Bronze row rather than an empty one.
    it('renders one dropdown per rank the path grants perks at', async () => {
      const wrapper = await mountWithPath()
      expect(wrapper.find('[data-test="preview-perk-bronze"]').exists()).toBe(false)
      expect(wrapper.find('[data-test="preview-perk-silver"]').exists()).toBe(true)
      expect(wrapper.find('[data-test="preview-perk-gold"]').exists()).toBe(true)
    })

    it("offers only that rank's perks, by display name", async () => {
      const wrapper = await mountWithPath()
      const silver = wrapper.find('[data-test="preview-perk-silver"]').findAll('option')
      expect(silver.map((o) => o.attributes('value'))).toEqual(['', 'lasting_flames', 'wider_nets'])
      expect(silver[1].text()).toBe('Lasting Flames')

      const gold = wrapper.find('[data-test="preview-perk-gold"]').findAll('option')
      expect(gold.map((o) => o.attributes('value'))).toEqual(['', 'overload_protocol'])
    })

    it('grants the picked perk, one per rank', async () => {
      const wrapper = await mountWithPath()
      await wrapper.find('[data-test="preview-perk-silver"]').setValue('lasting_flames')
      expect(lastPerks(wrapper)).toEqual(['lasting_flames'])

      await wrapper.find('[data-test="preview-perk-gold"]').setValue('overload_protocol')
      expect(lastPerks(wrapper)).toEqual(['lasting_flames', 'overload_protocol'])
    })

    // Picking a second perk at the same rank REPLACES the first — a unit cannot
    // carry two silver perks, and a checkbox list let you build exactly that.
    it('replaces the perk at a rank rather than adding to it', async () => {
      const wrapper = await mountWithPath()
      await wrapper.find('[data-test="preview-perk-silver"]').setValue('lasting_flames')
      await wrapper.find('[data-test="preview-perk-silver"]').setValue('wider_nets')
      expect(lastPerks(wrapper)).toEqual(['wider_nets'])
    })

    it('clears back to none', async () => {
      const wrapper = await mountWithPath()
      await wrapper.find('[data-test="preview-perk-silver"]').setValue('wider_nets')
      await wrapper.find('[data-test="preview-perk-silver"]').setValue('')
      expect(lastPerks(wrapper)).toEqual([])
    })

    // A perk belongs to one path, so a leftover pick would describe a unit that
    // cannot exist.
    // The perk dropdowns are more of the same kind of choice as caster/rank/
    // path, so they share that row and wrap with it rather than each taking a
    // line. The wrapper survives only as a v-if + test hook; display:contents
    // is what keeps it out of the layout.
    it('puts the perk fields in the same wrapping row as the other selections', async () => {
      const wrapper = await mountWithPath()
      const row = wrapper.find('.pv-scene__row')
      expect(row.find('[data-test="preview-caster-path"]').exists()).toBe(true)
      expect(row.find('[data-test="preview-perk-silver"]').exists()).toBe(true)
    })

    // A unit only carries perks from the ranks it has actually reached, so the
    // rows follow the chosen rank: bronze offers bronze alone, gold offers
    // everything the path grants.
    // A promotion path is EARNED at bronze, so a pathed unit is never at base.
    // Picking one promotes the rank and retires the Base option.
    it('promotes to bronze when a path is picked, and stops offering base', async () => {
      stubCatalogs()
      const wrapper = mount(PreviewSceneControls, { props: { perkOptions } })
      await flushPromises()
      const rank = wrapper.find('[data-test="preview-caster-rank"]')
      expect(rank.findAll('option').map((o) => o.attributes('value'))).toContain('')

      await wrapper.find('[data-test="preview-caster-unit"]').setValue('archer')
      await wrapper.find('[data-test="preview-caster-path"]').setValue('trapper')

      expect((rank.element as HTMLSelectElement).value).toBe('bronze')
      expect(rank.findAll('option').map((o) => o.attributes('value'))).not.toContain('')
      // The fixture path grants no bronze perks (nor does the real trapper), so
      // a bronze caster honestly has none to pick.
      expect(wrapper.findAll('[data-test^="preview-perk-"]')).toHaveLength(0)
    })

    it('shows only the ranks at or below the chosen rank', async () => {
      const wrapper = await mountWithPath()
      expect(wrapper.findAll('[data-test^="preview-perk-"]')).toHaveLength(2)

      await wrapper.find('[data-test="preview-caster-rank"]').setValue('silver')
      expect(wrapper.find('[data-test="preview-perk-silver"]').exists()).toBe(true)
      expect(wrapper.find('[data-test="preview-perk-gold"]').exists()).toBe(false)

      await wrapper.find('[data-test="preview-caster-rank"]').setValue('gold')
      expect(wrapper.find('[data-test="preview-perk-silver"]').exists()).toBe(true)
      expect(wrapper.find('[data-test="preview-perk-gold"]').exists()).toBe(true)
    })

    // The one that matters: a perk chosen at gold must not keep riding along in
    // the request after the caster drops to silver — it would be previewing a
    // unit that cannot exist.
    it('retires a perk above the chosen rank', async () => {
      const wrapper = await mountWithPath()
      await wrapper.find('[data-test="preview-perk-gold"]').setValue('overload_protocol')
      await wrapper.find('[data-test="preview-perk-silver"]').setValue('wider_nets')
      expect(lastPerks(wrapper)).toEqual(['wider_nets', 'overload_protocol'])

      await wrapper.find('[data-test="preview-caster-rank"]').setValue('silver')
      expect(lastPerks(wrapper)).toEqual(['wider_nets'])
    })

    it('drops the picks when the path changes', async () => {
      const wrapper = await mountWithPath()
      await wrapper.find('[data-test="preview-perk-gold"]').setValue('overload_protocol')
      expect(lastPerks(wrapper)).toEqual(['overload_protocol'])

      await wrapper.find('[data-test="preview-caster-path"]').setValue('')
      expect(lastPerks(wrapper)).toEqual([])
    })
  })

  it('emits a default config on mount (matching defaultPreviewRequest)', () => {
    const wrapper = mount(PreviewSceneControls)
    const emitted = wrapper.emitted('update:modelValue')
    expect(emitted).toBeTruthy()
    const config = emitted![emitted!.length - 1][0] as {
      enemyCount: number
      allyCount: number
      targetSelector: string
      seed: number
      durationSeconds: number
    }
    expect(config.enemyCount).toBe(1)
    expect(config.allyCount).toBe(1)
    expect(config.targetSelector).toBe('first_enemy')
    expect(config.seed).toBe(1)
    expect(config.durationSeconds).toBe(3)
  })

  it('changing enemy count emits the new count, not a units array', async () => {
    const wrapper = mount(PreviewSceneControls)
    await wrapper.find('[data-test="preview-enemy-count"]').setValue(3)
    const emitted = wrapper.emitted('update:modelValue')!
    const config = emitted[emitted.length - 1][0] as { enemyCount: number }
    expect(config.enemyCount).toBe(3)
  })

  it('changing ally count emits the new count', async () => {
    const wrapper = mount(PreviewSceneControls)
    await wrapper.find('[data-test="preview-ally-count"]').setValue(5)
    const emitted = wrapper.emitted('update:modelValue')!
    const config = emitted[emitted.length - 1][0] as { allyCount: number }
    expect(config.allyCount).toBe(5)
  })

  it('changing the target selector to "First ally" emits that selector', async () => {
    const wrapper = mount(PreviewSceneControls)
    await wrapper.find('[data-test="preview-target-selector"]').setValue('first_ally')
    const emitted = wrapper.emitted('update:modelValue')!
    const config = emitted[emitted.length - 1][0] as { targetSelector: string }
    expect(config.targetSelector).toBe('first_ally')
  })

  it('changing the target selector to "Self" emits that selector', async () => {
    const wrapper = mount(PreviewSceneControls)
    await wrapper.find('[data-test="preview-target-selector"]').setValue('self')
    const emitted = wrapper.emitted('update:modelValue')!
    const config = emitted[emitted.length - 1][0] as { targetSelector: string }
    expect(config.targetSelector).toBe('self')
  })

  it('changing seed/duration emits the raw values', async () => {
    const wrapper = mount(PreviewSceneControls)
    await wrapper.find('[data-test="preview-seed"]').setValue(42)
    await wrapper.find('[data-test="preview-duration"]').setValue(5)
    const emitted = wrapper.emitted('update:modelValue')!
    const config = emitted[emitted.length - 1][0] as { seed: number; durationSeconds: number }
    expect(config.seed).toBe(42)
    expect(config.durationSeconds).toBe(5)
  })

  it('hides the Charge field and emits casterCharge 0 for a non-charge ability', () => {
    const wrapper = mount(PreviewSceneControls)
    expect(wrapper.find('[data-test="preview-caster-charge"]').exists()).toBe(false)
    const emitted = wrapper.emitted('update:modelValue')!
    const config = emitted[emitted.length - 1][0] as { casterCharge: number }
    expect(config.casterCharge).toBe(0)
  })

  it('shows the Charge field prefilled to chargeRequired for a charge-fire ability', () => {
    const wrapper = mount(PreviewSceneControls, { props: { chargeRequired: 30 } })
    const field = wrapper.find('[data-test="preview-caster-charge"]')
    expect(field.exists()).toBe(true)
    // Prefilled so one volley is ready on the first Run.
    expect((field.element as HTMLInputElement).value).toBe('30')
    const emitted = wrapper.emitted('update:modelValue')!
    const config = emitted[emitted.length - 1][0] as { casterCharge: number }
    expect(config.casterCharge).toBe(30)
  })

  it('emits an edited charge value', async () => {
    const wrapper = mount(PreviewSceneControls, { props: { chargeRequired: 30 } })
    await wrapper.find('[data-test="preview-caster-charge"]').setValue(90)
    const emitted = wrapper.emitted('update:modelValue')!
    const config = emitted[emitted.length - 1][0] as { casterCharge: number }
    expect(config.casterCharge).toBe(90)
  })

  it('collapses and expands the controls via the section-card toggle', async () => {
    const wrapper = mount(PreviewSceneControls)
    const toggle = wrapper.find('[data-test="section-card-toggle"]')
    const bodyStyle = () => wrapper.find('.ed-card__body').attributes('style') ?? ''

    // Expanded by default: toggle marked expanded, body not display:none.
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(bodyStyle()).not.toContain('display: none')

    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect(bodyStyle()).toContain('display: none')

    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(bodyStyle()).not.toContain('display: none')
  })

  it('resets casterCharge to 0 when switching from a charge ability to a non-charge one', async () => {
    const wrapper = mount(PreviewSceneControls, { props: { chargeRequired: 30 } })
    let emitted = wrapper.emitted('update:modelValue')!
    expect((emitted[emitted.length - 1][0] as { casterCharge: number }).casterCharge).toBe(30)

    // Switch to a non-charge ability: the field hides AND the stale value must
    // not keep riding along in the emitted config.
    await wrapper.setProps({ chargeRequired: null })
    expect(wrapper.find('[data-test="preview-caster-charge"]').exists()).toBe(false)
    emitted = wrapper.emitted('update:modelValue')!
    expect((emitted[emitted.length - 1][0] as { casterCharge: number }).casterCharge).toBe(0)
  })
})

// The caster picker and rank selector are what make CASTER-SCALED abilities
// testable: deal_damage's adRatio/apRatio read the caster's stats, and rank
// changes those stats, so a preview locked to one hardcoded unit at one rank
// could show neither.
describe('PreviewSceneControls caster + rank', () => {
  it('defaults both to empty so the harness default (adept) is used', async () => {
    const wrapper = mount(PreviewSceneControls)
    await flushPromises()

    const last = lastConfig(wrapper)
    expect(last.casterUnitType).toBe('')
    expect(last.casterRank).toBe('')
  })

  // Also covers that the picker is CATALOG-DRIVEN: with the fetch stubbed the
  // option exists and can be chosen, so the list can never offer a unit the
  // catalog doesn't have.
  it('lists catalog units and emits the chosen one', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({ units: [{ type: 'archer', name: 'Archer' }, { type: 'adept', name: 'Adept' }] }),
    })))
    const wrapper = mount(PreviewSceneControls)
    await flushPromises()

    const select = wrapper.find('[data-test="preview-caster-unit"]')
    const values = select.findAll('option').map((o) => o.attributes('value'))
    expect(values).toEqual(['', 'adept', 'archer']) // default first, then sorted by label

    await select.setValue('archer')
    expect(lastConfig(wrapper).casterUnitType).toBe('archer')
    vi.unstubAllGlobals()
  })

  it('emits the chosen rank', async () => {
    const wrapper = mount(PreviewSceneControls)
    await flushPromises()

    await wrapper.find('[data-test="preview-caster-rank"]').setValue('gold')
    expect(lastConfig(wrapper).casterRank).toBe('gold')
  })

  // The path is what turns a rank into real stats, and a path belongs to ONE
  // unit — so the options must follow the chosen caster, and a leftover
  // selection must not survive a caster change into an incoherent pair.
  it('offers the chosen caster paths and clears the path when the caster changes', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({
        units: [{ type: 'archer', name: 'Archer' }, { type: 'adept', name: 'Adept' }],
        pathsByUnit: { archer: ['marksman', 'trapper'], adept: ['arch_mage'] },
      }),
    })))
    const wrapper = mount(PreviewSceneControls)
    await flushPromises()

    await wrapper.find('[data-test="preview-caster-unit"]').setValue('archer')
    await flushPromises()

    const pathValues = wrapper.find('[data-test="preview-caster-path"]').findAll('option').map((o) => o.attributes('value'))
    expect(pathValues).toEqual(['', 'marksman', 'trapper'])

    await wrapper.find('[data-test="preview-caster-path"]').setValue('trapper')
    expect(lastConfig(wrapper).casterPath).toBe('trapper')

    // Switching caster must drop the now-invalid path.
    await wrapper.find('[data-test="preview-caster-unit"]').setValue('adept')
    await flushPromises()
    expect(lastConfig(wrapper).casterPath).toBe('')

    vi.unstubAllGlobals()
  })

  it('always offers the default option even when the catalog fetch fails', async () => {
    const wrapper = mount(PreviewSceneControls)
    await flushPromises()

    const options = wrapper.find('[data-test="preview-caster-unit"]').findAll('option')
    expect(options.length).toBeGreaterThanOrEqual(1)
    expect(options[0].attributes('value')).toBe('')
  })

  // Off by default: an untouched preview shows the ability acting alone, which
  // is the honest baseline for what it does by itself.
  it('emits alliesAttack false until the box is ticked', async () => {
    const wrapper = mount(PreviewSceneControls)
    expect(lastConfig(wrapper).alliesAttack).toBe(false)

    await wrapper.find('[data-test="preview-allies-attack"]').setValue(true)
    expect(lastConfig(wrapper).alliesAttack).toBe(true)
  })

  // The mirror control: enemies swing back, for previewing a debuff on an
  // enemy's own outgoing damage. Same default-off, independent of allies.
  it('emits enemiesAttack false until the box is ticked', async () => {
    const wrapper = mount(PreviewSceneControls)
    expect(lastConfig(wrapper).enemiesAttack).toBe(false)

    await wrapper.find('[data-test="preview-enemies-attack"]').setValue(true)
    expect(lastConfig(wrapper).enemiesAttack).toBe(true)
    // Independent toggles — enabling enemies must not flip allies.
    expect(lastConfig(wrapper).alliesAttack).toBe(false)
  })
})
