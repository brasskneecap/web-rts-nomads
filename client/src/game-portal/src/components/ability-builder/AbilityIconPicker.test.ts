import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import AbilityIconPicker from './AbilityIconPicker.vue'

// jsdom has no 2D canvas context, so AbilityIconCanvas renders an inert canvas —
// the gallery buttons + slider still render and emit, which is what we assert.
function mountPicker(modelIcon = '') {
  return mount(AbilityIconPicker, { props: { modelIcon, abilityId: 'fireball' } })
}

describe('AbilityIconPicker', () => {
  it('lists effect and projectile options', async () => {
    const wrapper = mountPicker()
    const effects = wrapper.findAll('[data-test="ability-icon-effects"] button')
    expect(effects.length).toBeGreaterThan(0)
    // meteor is a known effect sprite sheet.
    expect(effects.some((b) => b.attributes('title') === 'meteor')).toBe(true)

    await wrapper.find('[data-test="ability-icon-tab-projectile"]').trigger('click')
    expect(wrapper.findAll('[data-test="ability-icon-projectiles"] button').length).toBeGreaterThan(0)
  })

  it('emits an effect icon with the chosen frame', async () => {
    const wrapper = mountPicker()
    await wrapper.find('[data-test="ability-icon-effects"] button[title="meteor"]').trigger('click')

    // meteor is a 16-frame sheet, so the frame slider appears.
    const slider = wrapper.find('[data-test="ability-icon-frame-slider"]')
    expect(slider.exists()).toBe(true)
    await slider.setValue('5')

    await wrapper.find('[data-test="ability-icon-confirm"]').trigger('click')
    expect(wrapper.emitted('update:icon')).toEqual([['effect:meteor@5']])
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('emits a projectile icon (no @frame for a single-frame sprite)', async () => {
    const wrapper = mountPicker()
    await wrapper.find('[data-test="ability-icon-tab-projectile"]').trigger('click')
    const proj = wrapper.find('[data-test="ability-icon-projectiles"] button[title="fire_bolt"]')
    expect(proj.exists()).toBe(true)
    await proj.trigger('click')
    await wrapper.find('[data-test="ability-icon-confirm"]').trigger('click')
    expect(wrapper.emitted('update:icon')).toEqual([['projectile:fire_bolt']])
  })

  it('emits a beam icon when a beam is chosen', async () => {
    const wrapper = mountPicker()
    await wrapper.find('[data-test="ability-icon-tab-beam"]').trigger('click')
    const beam = wrapper.find('[data-test="ability-icon-beams"] button[title="siphon_life"]')
    expect(beam.exists()).toBe(true)
    await beam.trigger('click')
    await wrapper.find('[data-test="ability-icon-confirm"]').trigger('click')
    expect(String(wrapper.emitted('update:icon')?.[0]?.[0])).toMatch(/^beam:siphon_life/)
  })

  it('seeds the draft from an existing effect icon', () => {
    const wrapper = mountPicker('effect:explosion@2')
    // The preview code shows the seeded value.
    expect(wrapper.find('[data-test="ability-icon-picker"]').text()).toContain('effect:explosion@2')
  })
})
