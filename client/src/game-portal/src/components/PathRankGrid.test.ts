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

describe('PathRankGrid — attack-range mode (flat OR multiplier)', () => {
  it('in Flat mode edits attackRange and resolves it inline', () => {
    const wrapper = mount(PathRankGrid, {
      props: {
        baseStats: { attackRange: 100 },
        ranks: { bronze: { attackRange: 150 } },
      },
    })
    const mode = wrapper.find('select[data-rank="bronze"]')
    expect((mode.element as HTMLSelectElement).value).toBe('flat')
    const input = wrapper.find('input[data-rank="bronze"][data-field="attackRange"]')
    expect((input.element as HTMLInputElement).value).toBe('150')
    expect(wrapper.text()).toContain('Flat: 150')
  })

  it('in Multiplier mode edits the multiplier and shows base × mult = result', () => {
    const wrapper = mount(PathRankGrid, {
      props: {
        baseStats: { attackRange: 100 },
        ranks: { bronze: { attackRangeMultiplier: 2 } },
      },
    })
    const mode = wrapper.find('select[data-rank="bronze"]')
    expect((mode.element as HTMLSelectElement).value).toBe('mult')
    expect(wrapper.text()).toContain('200') // 100 × 2
  })

  it('switching mode clears the other field so both are never set at once', async () => {
    const wrapper = mount(PathRankGrid, {
      props: {
        baseStats: { attackRange: 100 },
        ranks: { bronze: { attackRange: 150 } },
      },
    })
    await wrapper.find('select[data-rank="bronze"]').setValue('mult')

    const emitted = wrapper.emitted('update:ranks')!
    const last = emitted[emitted.length - 1][0] as Record<string, PathRankStats>
    expect('attackRange' in last.bronze).toBe(false)
    expect(last.bronze.attackRangeMultiplier).toBeUndefined()
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
