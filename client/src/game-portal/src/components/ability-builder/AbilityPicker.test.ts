import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import AbilityPicker from './AbilityPicker.vue'

const abilities = [
  { id: 'siphon_life', displayName: 'Siphon Life', description: 'Drains HP from the target.' },
  { id: 'frost_bolt', displayName: 'Frost Bolt', generatedDescription: 'Chills the target on hit.' },
]

describe('AbilityPicker', () => {
  it('lists abilities, shows the description on click, and Select emits the id', async () => {
    const wrapper = mount(AbilityPicker, { props: { abilities } })
    expect(wrapper.find('[data-test="ability-picker-item-siphon_life"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="ability-picker-item-frost_bolt"]').exists()).toBe(true)

    const confirm = wrapper.find('[data-test="ability-picker-confirm"]')
    expect((confirm.element as HTMLButtonElement).disabled).toBe(true)

    await wrapper.find('[data-test="ability-picker-item-frost_bolt"]').trigger('click')
    expect(wrapper.text()).toContain('Chills the target on hit.')
    expect((confirm.element as HTMLButtonElement).disabled).toBe(false)

    await confirm.trigger('click')
    expect(wrapper.emitted('select')?.[0]).toEqual(['frost_bolt'])
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('falls back to the description override, then generated, then a placeholder', async () => {
    const wrapper = mount(AbilityPicker, { props: { abilities: [{ id: 'bare' }] } })
    await wrapper.find('[data-test="ability-picker-item-bare"]').trigger('click')
    expect(wrapper.text()).toContain('(no description)')
  })

  it('filters the list by search text', async () => {
    const wrapper = mount(AbilityPicker, { props: { abilities } })
    await wrapper.find('input[aria-label="Search abilities"]').setValue('frost')
    expect(wrapper.find('[data-test="ability-picker-item-frost_bolt"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="ability-picker-item-siphon_life"]').exists()).toBe(false)
  })

  it('seeds the draft from modelValue so Select is immediately usable', async () => {
    const wrapper = mount(AbilityPicker, { props: { abilities, modelValue: 'siphon_life' } })
    expect((wrapper.find('[data-test="ability-picker-confirm"]').element as HTMLButtonElement).disabled).toBe(false)
    await wrapper.find('[data-test="ability-picker-confirm"]').trigger('click')
    expect(wrapper.emitted('select')?.[0]).toEqual(['siphon_life'])
  })

  it('offers a None entry that clears the value (emits empty string)', async () => {
    const wrapper = mount(AbilityPicker, { props: { abilities } })
    await wrapper.find('[data-test="ability-picker-none"]').trigger('click')
    const confirm = wrapper.find('[data-test="ability-picker-confirm"]')
    expect((confirm.element as HTMLButtonElement).disabled).toBe(false)
    await confirm.trigger('click')
    expect(wrapper.emitted('select')?.[0]).toEqual([''])
  })

  it('offers a Use-"<query>" entry for a tag / custom id not in the list', async () => {
    const wrapper = mount(AbilityPicker, { props: { abilities } })
    await wrapper.find('input[aria-label="Search abilities"]').setValue('tag:trap')
    const use = wrapper.find('[data-test="ability-picker-use"]')
    expect(use.exists()).toBe(true)
    await use.trigger('click')
    await wrapper.find('[data-test="ability-picker-confirm"]').trigger('click')
    expect(wrapper.emitted('select')?.[0]).toEqual(['tag:trap'])
  })

  it('does not offer Use-entry when the query exactly matches a listed ability', async () => {
    const wrapper = mount(AbilityPicker, { props: { abilities } })
    await wrapper.find('input[aria-label="Search abilities"]').setValue('frost_bolt')
    expect(wrapper.find('[data-test="ability-picker-use"]').exists()).toBe(false)
  })
})
