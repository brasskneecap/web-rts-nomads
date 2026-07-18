import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import PreviewSceneControls from './PreviewSceneControls.vue'

describe('PreviewSceneControls', () => {
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
