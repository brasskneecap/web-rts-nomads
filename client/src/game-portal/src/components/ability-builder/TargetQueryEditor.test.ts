import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import type { TargetQueryDef } from '@/game/abilities/program/abilityProgram'
import FilterableSelect from '@/components/editor/FilterableSelect.vue'
import TargetQueryEditor from './TargetQueryEditor.vue'

function mountEditor(modelValue: TargetQueryDef | undefined, enums: Record<string, string[]> = {}) {
  return mount(TargetQueryEditor, { props: { modelValue, enums } })
}

describe('TargetQueryEditor', () => {
  it('defaults to an all_in_scene query when modelValue is undefined', () => {
    const wrapper = mountEditor(undefined)
    const selects = wrapper.findAllComponents(FilterableSelect)
    // Source is the first FilterableSelect rendered.
    expect(selects[0].props('modelValue')).toBe('all_in_scene')
  })

  it('renders relation checkboxes from the enums bundle and toggling one emits a merged query', async () => {
    const wrapper = mountEditor({ source: 'all_in_scene', relations: ['ally'] }, { relations: ['self', 'ally', 'enemy'] })
    const checkboxes = wrapper.findAll('.tqe-checkgroup input[type="checkbox"]')
    expect(checkboxes).toHaveLength(3)
    // ally is pre-checked.
    expect((checkboxes[1].element as HTMLInputElement).checked).toBe(true)

    await checkboxes[2].setValue(true) // check "enemy"

    const emitted = wrapper.emitted('update:modelValue')
    expect(emitted).toBeTruthy()
    const merged = emitted![0][0] as TargetQueryDef
    expect(merged.source).toBe('all_in_scene')
    expect(merged.relations).toEqual(['ally', 'enemy'])
  })

  it('changing the source select commits a merged query preserving other fields', async () => {
    const wrapper = mountEditor({ source: 'caster', maxCount: 3 })
    const sourceSelect = wrapper.findAllComponents(FilterableSelect)[0]

    await sourceSelect.vm.$emit('update:modelValue', 'all_in_scene')

    const merged = wrapper.emitted('update:modelValue')![0][0] as TargetQueryDef
    expect(merged.source).toBe('all_in_scene')
    expect(merged.maxCount).toBe(3)
  })

  it('commits radius as a plain number on change (not per keystroke)', async () => {
    const wrapper = mountEditor({ source: 'all_in_scene', radius: 100 })
    const radiusInput = wrapper.find('#tqe-radius')
    expect((radiusInput.element as HTMLInputElement).value).toBe('100')

    // wrapper.setValue() fires both 'input' and 'change' — set the value and
    // fire 'input' alone first to simulate a single keystroke.
    ;(radiusInput.element as HTMLInputElement).value = '250'
    await radiusInput.trigger('input')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()

    await radiusInput.trigger('change')
    const merged = wrapper.emitted('update:modelValue')![0][0] as TargetQueryDef
    expect(merged.radius).toBe(250)
  })

  it('toggling includeInitialTarget / excludeSource commits immediately', async () => {
    const wrapper = mountEditor({ source: 'all_in_scene' })
    const [includeInitial, excludeSource] = wrapper.findAll('label.ed-check input[type="checkbox"]').slice(-2)

    await includeInitial.setValue(true)
    let merged = wrapper.emitted('update:modelValue')!.at(-1)![0] as TargetQueryDef
    expect(merged.includeInitialTarget).toBe(true)

    await excludeSource.setValue(true)
    merged = wrapper.emitted('update:modelValue')!.at(-1)![0] as TargetQueryDef
    expect(merged.excludeSource).toBe(true)
  })
})
