import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import PathRankPanel, { type RankStats } from './PathRankPanel.vue'

const BASE = { hp: 100, damage: 20, maxMana: 50, attackSpeed: 1, moveSpeed: 60, healthRegenRate: 1, attackRange: 200 }

function mountPanel(opts: {
  rank?: string
  previousRank?: string
  previousStats?: RankStats
  stats?: RankStats
} = {}) {
  return mount(PathRankPanel, {
    props: {
      rank: opts.rank ?? 'silver',
      previousRank: opts.previousRank ?? 'bronze',
      previousStats: opts.previousStats ?? {},
      baseStats: BASE,
      stats: opts.stats ?? {},
    },
  })
}

const lastStats = (w: ReturnType<typeof mountPanel>): RankStats => {
  const events = w.emitted('update:stats')!
  return events[events.length - 1][0] as RankStats
}

describe('PathRankPanel', () => {
  it('resolves a multiplier against the parent unit base', () => {
    const w = mountPanel({ stats: { damageMultiplier: 1.5 } })
    expect(w.text()).toContain('20 × 1.5 = 30')
  })

  // The blocks are ABSOLUTE per rank, which reads fine in an all-ranks table but
  // is misleading one rank at a time — an author editing silver alone has no
  // idea what bronze already gave. So the previous rank's value is always shown.
  it('shows what the previous rank set', () => {
    const w = mountPanel({ previousStats: { maxHPMultiplier: 1.2 }, stats: {} })
    expect(w.text()).toContain('inherits 1.2 from bronze')
  })

  it('says so when a rank matches the one before it', () => {
    const w = mountPanel({ previousStats: { maxHPMultiplier: 1.2 }, stats: { maxHPMultiplier: 1.2 } })
    expect(w.text()).toContain('same as bronze')
  })

  it('shows the previous value when this rank raises it', () => {
    const w = mountPanel({ previousStats: { maxHPMultiplier: 1.2 }, stats: { maxHPMultiplier: 1.4 } })
    expect(w.text()).toContain('bronze was 1.2')
  })

  it('floors the input at the previous rank', () => {
    const w = mountPanel({ previousStats: { maxHPMultiplier: 1.2 } })
    expect(w.find('[data-test="rank-silver-mult-maxHPMultiplier"]').attributes('min')).toBe('1.2')
  })

  // The hard floor: a promotion must never weaken a unit.
  it('clamps a lower value up to the previous rank on commit', async () => {
    const w = mountPanel({ previousStats: { maxHPMultiplier: 1.2 } })
    const input = w.find('[data-test="rank-silver-mult-maxHPMultiplier"]')

    await input.setValue('1.05')
    await input.trigger('change')
    expect(lastStats(w).maxHPMultiplier).toBe(1.2)
  })

  it('accepts a higher value untouched', async () => {
    const w = mountPanel({ previousStats: { maxHPMultiplier: 1.2 } })
    const input = w.find('[data-test="rank-silver-mult-maxHPMultiplier"]')

    await input.setValue('1.4')
    await input.trigger('change')
    expect(lastStats(w).maxHPMultiplier).toBe(1.4)
  })

  // Clamping every keystroke would fight the user: typing "1" on the way to
  // "1.5" under a floor of 1.2 would snap to 1.2 before the "." arrived.
  it('does not clamp while typing, only on commit', async () => {
    const w = mountPanel({ previousStats: { maxHPMultiplier: 1.2 } })
    const input = w.find('[data-test="rank-silver-mult-maxHPMultiplier"]')

    // setValue fires input AND change, which is exactly the commit this test is
    // trying to avoid — so drive the `input` event alone.
    ;(input.element as HTMLInputElement).value = '1'
    await input.trigger('input')
    expect(lastStats(w).maxHPMultiplier).toBe(1)

    // ...and the same value on commit IS clamped.
    await input.trigger('change')
    expect(lastStats(w).maxHPMultiplier).toBe(1.2)
  })

  // The FIRST rank has no floor. Flooring it at 1.0 would flag shipping data —
  // arch_mage authors healthRegenMultiplier: 0 across all three ranks, which is
  // a deliberate "no regen", not a regression.
  it('leaves the first rank unfloored', async () => {
    const w = mountPanel({ rank: 'bronze', previousRank: '', previousStats: {} })
    const input = w.find('[data-test="rank-bronze-mult-healthRegenMultiplier"]')
    expect(input.attributes('min')).toBeUndefined()

    await input.setValue('0')
    await input.trigger('change')
    expect(lastStats(w).healthRegenMultiplier).toBe(0)
    expect(w.text()).toContain('The first rank')
  })

  it('clears a field when emptied', async () => {
    const w = mountPanel({ stats: { armor: 5 } })
    const input = w.find('[data-test="rank-silver-add-armor"]')

    await input.setValue('')
    await input.trigger('change')
    expect('armor' in lastStats(w)).toBe(false)
  })
})

// A stat with no typed rank field (ability power, crit chance, lifesteal) has to
// MIRROR down the ranks: adding ability power to the base unit must make it
// editable at every rank immediately, not stay invisible until re-added by hand.
// These are absolute per rank — a multiplier on a base of 0 is always 0.
describe('PathRankPanel unit stats', () => {
  const LABELS = { abilityPower: 'Ability Power', lifesteal: 'Lifesteal' }

  function mountWithUnitStats(opts: {
    unitBaseStats?: Record<string, number>
    previousBaseStats?: Record<string, number>
    stats?: RankStats
  }) {
    return mount(PathRankPanel, {
      props: {
        rank: 'silver',
        previousRank: 'bronze',
        previousStats: {},
        baseStats: BASE,
        stats: opts.stats ?? {},
        unitBaseStats: opts.unitBaseStats ?? {},
        previousBaseStats: opts.previousBaseStats ?? {},
        statLabels: LABELS,
      },
    })
  }

  it('shows a row for a stat the base unit authored, labelled from the registry', () => {
    const w = mountWithUnitStats({ unitBaseStats: { abilityPower: 20 } })
    expect(w.find('[data-test="rank-silver-base-abilityPower"]').exists()).toBe(true)
    expect(w.text()).toContain('Ability Power')
    expect(w.text()).toContain('inherits 20 from the unit')
  })

  it('shows a row for a stat only an earlier rank authored', () => {
    const w = mountWithUnitStats({ previousBaseStats: { lifesteal: 0.1 } })
    expect(w.find('[data-test="rank-silver-base-lifesteal"]').exists()).toBe(true)
    expect(w.text()).toContain('inherits 0.1 from bronze')
  })

  it('unions the unit, the previous rank and this rank', () => {
    const w = mountWithUnitStats({
      unitBaseStats: { abilityPower: 5 },
      previousBaseStats: { lifesteal: 0.1 },
      stats: { baseStats: { critChance: 0.2 } } as unknown as RankStats,
    })
    for (const id of ['abilityPower', 'lifesteal', 'critChance']) {
      expect(w.find(`[data-test="rank-silver-base-${id}"]`).exists(), id).toBe(true)
    }
  })

  // The nearest authored value behind this rank is the floor: the previous rank
  // if it set one, otherwise the unit's own base.
  it('floors at the previous rank, falling back to the unit base', () => {
    const prevWins = mountWithUnitStats({
      unitBaseStats: { abilityPower: 5 },
      previousBaseStats: { abilityPower: 12 },
    })
    expect(prevWins.find('[data-test="rank-silver-base-abilityPower"]').attributes('min')).toBe('12')

    const unitOnly = mountWithUnitStats({ unitBaseStats: { abilityPower: 5 } })
    expect(unitOnly.find('[data-test="rank-silver-base-abilityPower"]').attributes('min')).toBe('5')
  })

  it('clamps a lower value up on commit', async () => {
    const w = mountWithUnitStats({ previousBaseStats: { abilityPower: 12 } })
    const input = w.find('[data-test="rank-silver-base-abilityPower"]')

    await input.setValue('4')
    await input.trigger('change')
    const emitted = w.emitted('update:baseStats')!
    expect((emitted[emitted.length - 1][0] as Record<string, number>).abilityPower).toBe(12)
  })

  it('renders no Unit Stats group when nothing authored one', () => {
    const w = mountWithUnitStats({})
    expect(w.text()).not.toContain('Unit Stats')
  })
})

// A rank must be able to INTRODUCE a stat the unit never authored — bronze
// adding ability power, which then appears (and is floored) at silver and gold.
// Mirroring alone was not enough: without this the stat had to exist on the base
// unit first, so a path could never grant one at rank-up.
describe('PathRankPanel adding a unit stat at a rank', () => {
  const ADDABLE = ['abilityPower', 'critChance', 'lifesteal']
  const LABELS = { abilityPower: 'Ability Power', critChance: 'Crit Chance', lifesteal: 'Lifesteal' }

  function mountBronze(stats: RankStats = {}) {
    return mount(PathRankPanel, {
      props: {
        rank: 'bronze',
        previousRank: '',
        previousStats: {},
        baseStats: BASE,
        stats,
        unitBaseStats: {},
        previousBaseStats: {},
        statLabels: LABELS,
        addableStatIds: ADDABLE,
      },
    })
  }

  const lastBaseStats = (w: ReturnType<typeof mountBronze>) => {
    const events = w.emitted('update:baseStats')!
    return events[events.length - 1][0] as Record<string, number>
  }

  it('offers every base-authorable stat when none are set', () => {
    const w = mountBronze()
    const values = w.find('[data-test="rank-bronze-add-stat"]').findAll('option').map((o) => o.attributes('value'))
    // Placeholder first, then every addable stat sorted by LABEL.
    expect(values).toEqual(['', 'abilityPower', 'critChance', 'lifesteal'])
  })

  it('adds a stat seeded at 0 so the row is immediately editable', async () => {
    const w = mountBronze()
    await w.find('[data-test="rank-bronze-add-stat"]').setValue('abilityPower')
    expect(lastBaseStats(w)).toEqual({ abilityPower: 0 })
  })

  it('stops offering a stat that already has a row', () => {
    const w = mountBronze({ baseStats: { abilityPower: 10 } } as unknown as RankStats)
    const values = w.find('[data-test="rank-bronze-add-stat"]').findAll('option').map((o) => o.attributes('value'))
    expect(values).not.toContain('abilityPower')
  })

  it('can remove a stat this rank introduced', async () => {
    const w = mountBronze({ baseStats: { abilityPower: 10 } } as unknown as RankStats)
    await w.find('[data-test="rank-bronze-remove-abilityPower"]').trigger('click')
    expect(lastBaseStats(w)).toEqual({})
  })

  // A purely inherited row has nothing of this rank's to take back.
  it('offers no remove on a row this rank has not set', () => {
    const w = mount(PathRankPanel, {
      props: {
        rank: 'silver',
        previousRank: 'bronze',
        previousStats: {},
        baseStats: BASE,
        stats: {},
        unitBaseStats: {},
        previousBaseStats: { abilityPower: 10 },
        statLabels: LABELS,
        addableStatIds: ADDABLE,
      },
    })
    expect(w.find('[data-test="rank-silver-remove-abilityPower"]').exists()).toBe(false)
    expect(w.find('[data-test="rank-silver-base-abilityPower"]').attributes('min')).toBe('10')
  })

  // The base unit is NOT a blocker. A stat bronze introduced is bronze's to
  // remove even when the unit itself carries one — the row just reverts to the
  // unit's value. Only a LOWER RANK holding a value blocks removal.
  it('removes a value it set even when the unit has one', () => {
    const w = mount(PathRankPanel, {
      props: {
        rank: 'bronze', previousRank: '', previousStats: {}, baseStats: BASE,
        stats: { baseStats: { abilityPower: 30 } } as unknown as RankStats,
        unitBaseStats: { abilityPower: 20 },
        previousBaseStats: {},
        statLabels: LABELS, addableStatIds: ADDABLE,
      },
    })
    // A bare ✕, matching every other removable row in this editor.
    const btn = w.find('[data-test="rank-bronze-remove-abilityPower"]')
    expect(btn.exists()).toBe(true)
    expect(btn.text()).toBe('✕')
  })

  it('blocks removal while a LOWER RANK still sets the stat', () => {
    const w = mount(PathRankPanel, {
      props: {
        rank: 'silver', previousRank: 'bronze', previousStats: {}, baseStats: BASE,
        stats: { baseStats: { abilityPower: 30 } } as unknown as RankStats,
        unitBaseStats: {},
        previousBaseStats: { abilityPower: 10 },
        statLabels: LABELS, addableStatIds: ADDABLE,
      },
    })
    expect(w.find('[data-test="rank-silver-remove-abilityPower"]').exists()).toBe(false)
  })
})
