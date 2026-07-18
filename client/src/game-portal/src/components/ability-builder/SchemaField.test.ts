import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import type { SchemaField as SchemaFieldDescriptor } from '@/game/abilities/program/programSchema'
import FilterableSelect from '@/components/editor/FilterableSelect.vue'
import SchemaField from './SchemaField.vue'
import type { AbilityBuilderCatalogs } from './useAbilityBuilder'

function emptyCatalogs(): AbilityBuilderCatalogs {
  return { effects: [], projectiles: [], damageTypes: [], categories: [], autoCastSelectors: [], unitTypes: [] }
}

function mountField(
  field: SchemaFieldDescriptor,
  modelValue: unknown,
  opts: {
    enums?: Record<string, string[]>
    catalogs?: AbilityBuilderCatalogs
    loopVars?: string[]
    variableCapable?: boolean
  } = {},
) {
  return mount(SchemaField, {
    props: {
      field,
      modelValue,
      enums: opts.enums ?? {},
      catalogs: opts.catalogs ?? emptyCatalogs(),
      loopVars: opts.loopVars,
      variableCapable: opts.variableCapable,
    },
  })
}

describe('SchemaField', () => {
  it('renders a number control and commits the coerced value on change (not on input)', async () => {
    const wrapper = mountField({ key: 'amount', label: 'Amount', control: 'number' }, 10)
    const input = wrapper.find('input[type="number"]')
    expect((input.element as HTMLInputElement).value).toBe('10')

    // NOTE: VTU's wrapper.setValue() fires BOTH 'input' and 'change', so a
    // bare keystroke is simulated by setting .value and triggering 'input'
    // alone (matching what a real browser does per character typed).
    ;(input.element as HTMLInputElement).value = '25'
    await input.trigger('input')
    // input alone must NOT commit — protects undo from per-keystroke floods.
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()

    await input.trigger('change')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([25])
  })

  it('renders a text control and commits the raw string on change', async () => {
    const wrapper = mountField({ key: 'as', label: 'Store As', control: 'text' }, 'hits')
    const input = wrapper.find('input[type="text"]')
    expect((input.element as HTMLInputElement).value).toBe('hits')

    ;(input.element as HTMLInputElement).value = 'targets'
    await input.trigger('input')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()

    await input.trigger('change')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['targets'])
  })

  it('renders a boolean control as a checkbox and commits immediately on change', async () => {
    const wrapper = mountField({ key: 'disabled', label: 'Disabled', control: 'boolean' }, false)
    const checkbox = wrapper.find('input[type="checkbox"]')
    expect((checkbox.element as HTMLInputElement).checked).toBe(false)

    await checkbox.setValue(true)
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([true])
  })

  it('renders an enum control with explicit options via FilterableSelect and commits on select', async () => {
    const wrapper = mountField(
      { key: 'status', label: 'Status', control: 'enum', options: ['slow', 'stun', 'burn'] },
      'slow',
    )
    const select = wrapper.findComponent(FilterableSelect)
    expect(select.exists()).toBe(true)
    expect(select.props('options')).toEqual([
      { id: 'slow', label: 'slow' },
      { id: 'stun', label: 'stun' },
      { id: 'burn', label: 'burn' },
    ])

    await select.vm.$emit('update:modelValue', 'stun')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['stun'])
  })

  it('falls back to a plain text input for an enum control with no resolvable options', () => {
    // "aliveState" has no explicit options and no key/label heuristic match
    // (no matching catalog or enums-bundle entry) — per SchemaField's
    // resolveOptionList doc comment, that's an intentional text fallback.
    const wrapper = mountField({ key: 'aliveState', label: 'Alive State', control: 'enum' }, '')
    expect(wrapper.findComponent(FilterableSelect).exists()).toBe(false)
    expect(wrapper.find('input[type="text"]').exists()).toBe(true)
  })

  it('resolves an enum control against a catalog via the key heuristic (damage type)', () => {
    const wrapper = mountField(
      { key: 'type', label: 'Damage Type', control: 'enum' },
      'fire',
      { catalogs: { ...emptyCatalogs(), damageTypes: ['fire', 'cold', 'physical'] } },
    )
    const select = wrapper.findComponent(FilterableSelect)
    expect(select.props('options').map((o: { id: string }) => o.id)).toEqual(['fire', 'cold', 'physical'])
  })

  it('renders a sentinel_number control: checked hides the number input and commits the sentinel', async () => {
    const wrapper = mountField({ key: 'castRange', label: 'Cast Range', control: 'sentinel_number' }, 200)
    expect(wrapper.find('input[type="number"]').exists()).toBe(true)

    const checkbox = wrapper.find('input[type="checkbox"]')
    await checkbox.setValue(true)
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['match_attack_range'])
  })

  it('renders a sentinel_number control bound to the sentinel string with no number input shown', () => {
    const wrapper = mountField({ key: 'castRange', label: 'Cast Range', control: 'sentinel_number' }, 'match_attack_range')
    const checkbox = wrapper.find('input[type="checkbox"]').element as HTMLInputElement
    expect(checkbox.checked).toBe(true)
    expect(wrapper.find('input[type="number"]').exists()).toBe(false)
  })

  it('unchecking sentinel_number reverts to a numeric 0 and shows the number input again', async () => {
    const wrapper = mountField({ key: 'castRange', label: 'Cast Range', control: 'sentinel_number' }, 'match_attack_range')
    const checkbox = wrapper.find('input[type="checkbox"]')
    await checkbox.setValue(false)
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([0])
  })

  it('falls back to a text input for an unrecognized control without crashing', () => {
    const wrapper = mountField({ key: 'weird', label: 'Weird', control: 'totally_unknown_control' }, 'x')
    expect(wrapper.find('input[type="text"]').exists()).toBe(true)
    expect((wrapper.find('input[type="text"]').element as HTMLInputElement).value).toBe('x')
  })
})

describe('SchemaField number field: literal-or-variable (loop bodies)', () => {
  const amount: SchemaFieldDescriptor = { key: 'amount', label: 'Amount', control: 'number' }

  it('stays a plain number input when NOT variable-capable (no regression)', () => {
    const wrapper = mountField(amount, 10)
    expect(wrapper.find('[data-test="numvar"]').exists()).toBe(false)
    expect(wrapper.find('input[type="number"]').exists()).toBe(true)
  })

  it('offers a Number/Variable selector inside a loop; a literal value starts in Number mode', () => {
    const wrapper = mountField(amount, 10, { variableCapable: true, loopVars: ['a', 'b'] })
    expect(wrapper.find('[data-test="numvar"]').exists()).toBe(true)
    expect((wrapper.find('[data-test="numvar-mode"]').element as HTMLSelectElement).value).toBe('number')
    expect((wrapper.find('[data-test="numvar-number"]').element as HTMLInputElement).value).toBe('10')
  })

  it('a variable value starts in Variable mode with that variable selected', () => {
    const wrapper = mountField(amount, 'a', { variableCapable: true, loopVars: ['a', 'b'] })
    expect((wrapper.find('[data-test="numvar-mode"]').element as HTMLSelectElement).value).toBe('variable')
    const pick = wrapper.find('[data-test="numvar-variable"]')
    expect((pick.element as HTMLSelectElement).value).toBe('a')
    // The available variables are the loop's.
    expect(pick.findAll('option').map((o) => (o.element as HTMLOptionElement).value)).toEqual(['', 'a', 'b'])
  })

  it('switching to Variable mode shows the (blank) variable dropdown; picking one emits the letter', async () => {
    const wrapper = mountField(amount, 10, { variableCapable: true, loopVars: ['a'] })
    await wrapper.find('[data-test="numvar-mode"]').setValue('variable')
    const pick = wrapper.find('[data-test="numvar-variable"]')
    expect(pick.exists()).toBe(true)
    expect((pick.element as HTMLSelectElement).value).toBe('') // nothing picked yet
    await pick.setValue('a')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['a'])
  })

  it('switching from Variable back to Number commits a real number', async () => {
    const wrapper = mountField(amount, 'a', { variableCapable: true, loopVars: ['a'] })
    await wrapper.find('[data-test="numvar-mode"]').setValue('number')
    // The old "a" can't be a number, so it commits 0 rather than NaN.
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([0])
  })

  it('the variable dropdown is empty when the loop declares no variables', () => {
    const wrapper = mountField(amount, 'a', { variableCapable: true, loopVars: [] })
    const pick = wrapper.find('[data-test="numvar-variable"]')
    // Only the "—" placeholder.
    expect(pick.findAll('option').map((o) => (o.element as HTMLOptionElement).value)).toEqual([''])
  })
})
