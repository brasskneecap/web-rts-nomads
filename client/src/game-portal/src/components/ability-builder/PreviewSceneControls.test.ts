import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import PreviewSceneControls from './PreviewSceneControls.vue'

describe('PreviewSceneControls', () => {
  describe('force-branch toggles', () => {
    const conditionals = [
      { id: 'deliver', summary: 'Conditional — has perk: Lasting Flames' },
      { id: 'gold', summary: 'Conditional — has perk: Overload Protocol' },
    ]

    function lastConfig(wrapper: ReturnType<typeof mount>) {
      const emitted = wrapper.emitted('update:modelValue')!
      return emitted[emitted.length - 1][0] as { conditionalOverrides: Record<string, boolean> }
    }

    it('renders nothing when the program has no conditionals', () => {
      const wrapper = mount(PreviewSceneControls)
      expect(wrapper.find('[data-test="preview-conditionals"]').exists()).toBe(false)
      expect(lastConfig(wrapper).conditionalOverrides).toEqual({})
    })

    it('renders one checkbox per conditional, labelled with its branch summary', () => {
      const wrapper = mount(PreviewSceneControls, { props: { conditionals } })
      expect(wrapper.find('[data-test="preview-conditionals"]').exists()).toBe(true)
      expect(wrapper.find('[data-test="preview-conditional-deliver"]').text()).toContain('Lasting Flames')
      expect(wrapper.find('[data-test="preview-conditional-gold"]').text()).toContain('Overload Protocol')
    })

    // An untouched conditional must send NO override, so a preview that nobody
    // has configured behaves exactly as it did before this control existed.
    it('emits no overrides until a box is actually toggled', () => {
      const wrapper = mount(PreviewSceneControls, { props: { conditionals } })
      expect(lastConfig(wrapper).conditionalOverrides).toEqual({})
    })

    it('checking a box forces that conditional true, leaving the others untouched', async () => {
      const wrapper = mount(PreviewSceneControls, { props: { conditionals } })
      await wrapper.find('[data-test="preview-conditional-deliver"] input').setValue(true)
      expect(lastConfig(wrapper).conditionalOverrides).toEqual({ deliver: true })
    })

    it('tracks several conditionals independently', async () => {
      const wrapper = mount(PreviewSceneControls, { props: { conditionals } })
      await wrapper.find('[data-test="preview-conditional-deliver"] input').setValue(true)
      await wrapper.find('[data-test="preview-conditional-gold"] input').setValue(true)
      await wrapper.find('[data-test="preview-conditional-deliver"] input').setValue(false)
      expect(lastConfig(wrapper).conditionalOverrides).toEqual({ deliver: false, gold: true })
    })

    // A deleted branch's override must not keep riding along in every later
    // request — and must not come back to life if an action reuses that id.
    it('drops overrides for conditionals that no longer exist', async () => {
      const wrapper = mount(PreviewSceneControls, { props: { conditionals } })
      await wrapper.find('[data-test="preview-conditional-gold"] input').setValue(true)
      expect(lastConfig(wrapper).conditionalOverrides).toEqual({ gold: true })

      await wrapper.setProps({ conditionals: [conditionals[0]] })
      expect(lastConfig(wrapper).conditionalOverrides).toEqual({})
    })
  })

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

  it('collapses and expands the controls via the section-card toggle', async () => {
    const wrapper = mount(PreviewSceneControls)
    const toggle = wrapper.find('[data-test="section-card-toggle"]')
    const bodyStyle = () => wrapper.find('.ed-card__body').attributes('style') ?? ''

    // Expanded by default: toggle marked expanded, body not display:none.
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(bodyStyle()).not.toContain('display: none')

    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect(bodyStyle()).toContain('display: none')

    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(bodyStyle()).not.toContain('display: none')
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
