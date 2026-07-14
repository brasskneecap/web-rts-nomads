import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import PathRankGrid from './PathRankGrid.vue'
import type { PathRankStats } from '@/game/units/pathEditorForm'

describe('PathRankGrid — resolved multiplier display', () => {
  it('renders base × mult = result inline so the author is not editing blind', () => {
    const wrapper = mount(PathRankGrid, {
      props: {
        baseStats: { damage: 18, attackRange: 100 },
        ranks: { bronze: { damageMultiplier: 1.75 } },
      },
    })
    const text = wrapper.text()
    expect(text).toContain('18')
    expect(text).toContain('1.75')
    expect(text).toContain('31.5')
  })

  it('shows a "(no base value)" hint instead of NaN when the base stat is missing', () => {
    const wrapper = mount(PathRankGrid, {
      props: {
        baseStats: {},
        ranks: { bronze: { damageMultiplier: 1.75 } },
      },
    })
    expect(wrapper.text()).not.toContain('NaN')
    expect(wrapper.text()).toContain('no base value')
  })
})

describe('PathRankGrid — editing emits update:ranks', () => {
  it('emits the new multiplier value on edit', async () => {
    const wrapper = mount(PathRankGrid, {
      props: {
        baseStats: { damage: 18 },
        ranks: { bronze: { damageMultiplier: 1.75 } },
      },
    })
    const input = wrapper.find('input[data-rank="bronze"][data-field="damageMultiplier"]')
    await input.setValue('2')

    const emitted = wrapper.emitted('update:ranks')
    expect(emitted).toBeTruthy()
    const last = emitted![emitted!.length - 1][0] as Record<string, PathRankStats>
    expect(last.bronze.damageMultiplier).toBe(2)
  })

  it('emits undefined (not 0), and drops the key, when the field is cleared', async () => {
    const wrapper = mount(PathRankGrid, {
      props: {
        baseStats: { damage: 18 },
        ranks: { bronze: { damageMultiplier: 1.75 } },
      },
    })
    const input = wrapper.find('input[data-rank="bronze"][data-field="damageMultiplier"]')
    await input.setValue('')

    const emitted = wrapper.emitted('update:ranks')!
    const last = emitted[emitted.length - 1][0] as Record<string, PathRankStats>
    expect(last.bronze.damageMultiplier).toBeUndefined()
    expect('damageMultiplier' in last.bronze).toBe(false)
  })
})

describe('PathRankGrid — attack-range conflict', () => {
  it('shows both the flat value and the multiplier result, plus a caution that the flat override wins', () => {
    const wrapper = mount(PathRankGrid, {
      props: {
        baseStats: { attackRange: 100 },
        ranks: { bronze: { attackRange: 150, attackRangeMultiplier: 2 } },
      },
    })
    const text = wrapper.text()
    expect(text).toContain('150')
    expect(text).toContain('200') // 100 × 2, the multiplier's would-be result
    expect(text.toLowerCase()).toContain('flat override wins')
  })

  it('shows no caution when only one of flat/multiplier is authored', () => {
    const wrapper = mount(PathRankGrid, {
      props: {
        baseStats: { attackRange: 100 },
        ranks: { bronze: { attackRange: 150 } },
      },
    })
    expect(wrapper.text().toLowerCase()).not.toContain('flat override wins')
  })
})

describe('PathRankGrid — undefined-vs-0 preserved', () => {
  it('displays an explicit 0 (not blank) and round-trips it as 0 through an emit', async () => {
    const wrapper = mount(PathRankGrid, {
      props: {
        baseStats: {},
        ranks: { bronze: { armor: 0 } },
      },
    })
    const input = wrapper.find('input[data-rank="bronze"][data-field="armor"]')
    expect((input.element as HTMLInputElement).value).toBe('0')

    await input.setValue('0')
    const emitted = wrapper.emitted('update:ranks')!
    const last = emitted[emitted.length - 1][0] as Record<string, PathRankStats>
    expect(last.bronze.armor).toBe(0)
  })
})

describe('PathRankGrid — fixed row order', () => {
  it('renders bronze/silver/gold rows even when ranks only has one of them', () => {
    const wrapper = mount(PathRankGrid, {
      props: {
        baseStats: {},
        ranks: { silver: { armor: 3 } },
      },
    })
    const text = wrapper.text()
    expect(text).toContain('bronze')
    expect(text).toContain('silver')
    expect(text).toContain('gold')
  })
})
