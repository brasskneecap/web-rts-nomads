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

  // A stat is chosen from the "Add a stat…" picker, exactly as the per-rank unit
  // stats do it. The row then names a real stat from the moment it exists —
  // there is no half-made row, which is what the old pick-from-the-row flow left
  // lying around for a re-seed to destroy.
  it('adds a row from the picker, with no value set', async () => {
    const w = mountEditor()
    expect(w.findAll('[data-test="ability-stat-row"]')).toHaveLength(0)

    await w.find('[data-test="ability-stat-add"]').setValue('radius')
    expect(w.findAll('[data-test="ability-stat-row"]')).toHaveLength(1)
    expect(w.find('[data-test="ability-stat-name-radius"]').text()).toBe('Radius')
    // Blank, not zero — nothing has been authored yet.
    expect((w.find('[data-test="ability-stat-flat"]').element as HTMLInputElement).value).toBe('')
  })

  it('stops offering a stat that already has a row', () => {
    const w = mountEditor({ radius: { pct: 0.1 } })
    const values = w.find('[data-test="ability-stat-add"]').findAll('option').map((o) => o.attributes('value'))
    expect(values).not.toContain('radius')
    expect(values).toContain('duration')
  })

  // A flat-only stat must never carry a pct — the server rejects it outright and
  // the whole def becomes unsaveable.
  it('offers no percentage field for a flat-only stat', async () => {
    const w = mountEditor()
    await w.find('[data-test="ability-stat-add"]').setValue('count')
    expect(w.find('[data-test="ability-stat-pct"]').exists()).toBe(false)
    expect(w.find('[data-test="ability-stat-flatonly"]').exists()).toBe(true)
  })

  // An id the server no longer offers must still render as its own row rather
  // than vanishing and taking the authored value with it.
  it('keeps an unknown authored id as a row, labelled by its id', () => {
    const w = mountEditor({ 'gone.duration': { flat: 3 } })
    expect(w.find('[data-test="ability-stat-name-gone.duration"]').text()).toBe('gone.duration')
  })

  it('omits a zero contribution rather than writing flat: 0', async () => {
    const w = mountEditor({ radius: { flat: 5 } })
    await w.find('[data-test="ability-stat-flat"]').setValue('0')
    expect(lastEmit(w)).toEqual({ radius: {} })
  })
})

// Inheritance is used by the per-rank tabs (bronze -> silver -> gold). The unit
// and item editors pass no `inherited` and keep the plain add/remove behaviour.
describe('AbilityStatsEditor inheritance', () => {
  function mountInheriting(authored: Record<string, { flat?: number; pct?: number }> = {}) {
    return mount(AbilityStatsEditor, {
      props: {
        modelValue: authored,
        defs: DEFS,
        inherited: { duration: { flat: 5 } },
        inheritedFrom: 'bronze',
      },
    })
  }

  // The inherited value shows as a FADED PLACEHOLDER in the field, not as a
  // worded note — same treatment as the per-rank unit stats, and far less text
  // per row now that rows sit side by side.
  it('shows an inherited stat as a row with the value as a placeholder', () => {
    const w = mountInheriting()
    expect(w.findAll('[data-test="ability-stat-row"]')).toHaveLength(1)

    const flat = w.find('[data-test="ability-stat-flat"]')
    expect(flat.attributes('placeholder')).toBe('5')
    // The field itself is BLANK — "not set here" must read differently from
    // "set to the same value", or the remove rule and the emit would disagree.
    expect((flat.element as HTMLInputElement).value).toBe('')
  })

  it('floors the inputs at the inherited value', () => {
    const w = mountInheriting()
    expect(w.find('[data-test="ability-stat-flat"]').attributes('min')).toBe('5')
  })

  // An inherited row is not this level's to remove or re-point.
  it('offers no remove on an inherited row', () => {
    const w = mountInheriting()
    expect(w.find('[data-test="ability-stat-remove-duration"]').exists()).toBe(false)
    expect(w.find('[data-test="ability-stat-name-duration"]').text()).toBe('Duration')
  })

  // THE SUBTLE ONE: displaying an inherited value must not make this level OWN
  // it. Emitting it would give the row a remove button here, and taking the stat
  // back at the rank that really set it would leave orphaned copies behind.
  it('does not author an inherited value it merely displays', async () => {
    const w = mountInheriting()
    await w.vm.$nextTick()
    const events = w.emitted('update:modelValue')
    if (events) expect(events[events.length - 1][0]).toEqual({})
  })

  it('authors the value once it is raised above the inherited one', async () => {
    const w = mountInheriting()
    const flat = w.find('[data-test="ability-stat-flat"]')
    await flat.setValue('9')
    await flat.trigger('change')
    const events = w.emitted('update:modelValue')!
    expect(events[events.length - 1][0]).toEqual({ duration: { flat: 9 } })
  })

  it('clamps back up to the inherited value on commit', async () => {
    const w = mountInheriting()
    const flat = w.find('[data-test="ability-stat-flat"]')
    await flat.setValue('2')
    await flat.trigger('change')
    expect((flat.element as HTMLInputElement).value).toBe('5')
  })
})

// The rows are laid out by THIS component, not by RepeatableList: its .ed-list
// is a flex column shared by every editor and also holds the empty text and the
// add button, so styling it would have gridded those too. An earlier attempt
// targeted class names RepeatableList does not have, so the CSS was dead and
// every row stayed full-width.
describe('AbilityStatsEditor layout', () => {
  // Stacked like the per-rank unit stats: a plain NAME label above the fields,
  // with the ✕ to the RIGHT of those fields.
  it('puts the stat name above its fields and the remove beside them', () => {
    const w = mountEditor({ radius: { pct: 0.1 } })
    const row = w.find('[data-test="ability-stat-row"]')

    expect(row.find('.abilstat-label').text()).toBe('Radius')
    expect(row.find('.abilstat-fields [data-test="ability-stat-flat"]').exists()).toBe(true)
    expect(row.find('.abilstat-fields [data-test="ability-stat-remove-radius"]').exists()).toBe(true)
  })

  it('wraps its rows in a grid, with the add picker outside it', () => {
    const w = mountEditor({ radius: { pct: 0.1 }, duration: { flat: 2 } })

    const grid = w.find('.abilstat-grid')
    expect(grid.findAll('[data-test="ability-stat-row"]')).toHaveLength(2)
    expect(grid.find('[data-test="ability-stat-add"]').exists()).toBe(false)
  })
})
