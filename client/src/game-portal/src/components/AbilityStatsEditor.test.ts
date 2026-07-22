import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import AbilityStatsEditor, { type AbilityStatDef } from './AbilityStatsEditor.vue'

const DEFS: AbilityStatDef[] = [
  { id: 'radius', label: 'Radius', kind: 'radius' },
  { id: 'duration', label: 'Duration', kind: 'duration' },
  { id: 'create_zone.duration', label: 'Zone Duration', kind: 'duration', action: 'create_zone' },
  { id: 'count', label: 'Count', kind: 'count', flatOnly: true },
  { id: 'loop.count', label: 'Loop Count', kind: 'count', action: 'loop', flatOnly: true },
]

function mountEditor(modelValue: Record<string, { flat?: number; pct?: number }> = {}) {
  return mount(AbilityStatsEditor, { props: { modelValue, defs: DEFS } })
}

const lastEmit = (w: ReturnType<typeof mountEditor>) => {
  const events = w.emitted('update:modelValue')
  return events ? (events[events.length - 1][0] as Record<string, { flat?: number; pct?: number }>) : undefined
}

describe('AbilityStatsEditor', () => {
  it('renders one row per authored stat and nothing when empty', () => {
    expect(mountEditor().findAll('[data-test="ability-stat-row"]')).toHaveLength(0)
    const w = mountEditor({ radius: { pct: 0.15 }, 'create_zone.duration': { flat: 2 } })
    expect(w.findAll('[data-test="ability-stat-row"]')).toHaveLength(2)
  })

  // The wire format is a FRACTION (0.15) but a designer thinks in whole percent
  // (15). The conversion lives at this boundary so neither side has to know
  // about the other — this asserts both directions.
  it('shows a stored fraction as whole percent, and stores typed percent as a fraction', async () => {
    const w = mountEditor({ radius: { pct: 0.15 } })
    const pct = w.find('[data-test="ability-stat-pct"]')
    expect((pct.element as HTMLInputElement).value).toBe('15')

    await pct.setValue('30')
    expect(lastEmit(w)).toEqual({ radius: { pct: 0.3 } })
  })

  it('emits flat and percent together', async () => {
    const w = mountEditor({ duration: { flat: 2 } })
    await w.find('[data-test="ability-stat-pct"]').setValue('50')
    expect(lastEmit(w)).toEqual({ duration: { flat: 2, pct: 0.5 } })
  })

  // A whole quantity takes no percentage — the server rejects one outright, so
  // offering the input would be offering a save error.
  it('hides the percentage input for a flat-only stat', () => {
    const w = mountEditor({ 'loop.count': { flat: 1 } })
    expect(w.find('[data-test="ability-stat-pct"]').exists()).toBe(false)
    expect(w.find('[data-test="ability-stat-flatonly"]').exists()).toBe(true)
    expect(w.find('[data-test="ability-stat-flat"]').exists()).toBe(true)
  })

  it('keeps the percentage input for a continuous stat', () => {
    const w = mountEditor({ radius: { pct: 0.1 } })
    expect(w.find('[data-test="ability-stat-pct"]').exists()).toBe(true)
    expect(w.find('[data-test="ability-stat-flatonly"]').exists()).toBe(false)
  })

  // Regression guard: typing a percentage and THEN switching the row to a count
  // must not ship a pct the server will reject and make the whole def unsaveable.
  it('drops a percentage when the row is switched to a flat-only stat', async () => {
    const w = mountEditor({ radius: { pct: 0.25 } })
    const select = w.findComponent({ name: 'FilterableSelect' })
    await select.vm.$emit('update:modelValue', 'count')
    expect(lastEmit(w)).toEqual({ count: {} })
  })

  it('ignores a row whose stat has not been picked yet', async () => {
    const w = mountEditor()
    await w.find('button').trigger('click') // the RepeatableList "Add" button
    expect(w.findAll('[data-test="ability-stat-row"]')).toHaveLength(1)
    expect(lastEmit(w)).toEqual({})
  })

  // An id the server no longer offers must stay visible rather than silently
  // vanishing from the picker and taking the authored value with it.
  it('keeps an unknown authored id as an option', () => {
    const w = mountEditor({ 'gone.duration': { flat: 3 } })
    const opts = w.findComponent({ name: 'FilterableSelect' }).props('options') as { id: string }[]
    expect(opts.map((o) => o.id)).toContain('gone.duration')
  })

  it('omits a zero contribution rather than writing flat: 0', async () => {
    const w = mountEditor({ radius: { flat: 5 } })
    await w.find('[data-test="ability-stat-flat"]').setValue('0')
    expect(lastEmit(w)).toEqual({ radius: {} })
  })
})
