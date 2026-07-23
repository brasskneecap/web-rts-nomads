import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import AnimationPicker from './AnimationPicker.vue'
import { listObjectSpriteKeys, listObjectAnimationStates } from '@/game/rendering/objectSprites'
import { listEffectNames } from '@/game/rendering/effectSprites'

function mountPicker(modelAnimation?: string) {
  return mount(AnimationPicker, { props: { abilityId: 'test_ability', modelAnimation } })
}

describe('AnimationPicker', () => {
  it('shows all five source tabs', () => {
    const w = mountPicker()
    for (const id of ['effect', 'projectile', 'beam', 'object', 'upload']) {
      expect(w.find(`[data-test="animation-tab-${id}"]`).exists()).toBe(true)
    }
  })

  it('emits an effect scheme when an effect is chosen and confirmed', async () => {
    const name = listEffectNames()[0]
    expect(name).toBeTruthy()
    const w = mountPicker()
    await w.find('[data-test="animation-tab-effect"]').trigger('click')
    // Click the gallery tile whose title is the effect name.
    const tile = w.findAll('[data-test="animation-effects"] .anp__item').find((b) => b.attributes('title') === name)
    await tile!.trigger('click')
    await w.find('[data-test="animation-confirm"]').trigger('click')
    expect(w.emitted('update:animation')?.[0]).toEqual([`effect:${name}`])
  })

  it('lets you pick an object AND its animation state, emitting object:<key>@<state>', async () => {
    // Find a checked-in object that defines more than one animation state.
    const multi = listObjectSpriteKeys().find((k) => listObjectAnimationStates(k).length > 1)
    expect(multi).toBeTruthy()
    const nonIdle = listObjectAnimationStates(multi!).find((s) => s !== 'idle')!

    const w = mountPicker()
    await w.find('[data-test="animation-tab-object"]').trigger('click')
    const tile = w.findAll('[data-test="animation-objects"] .anp__item').find((b) => b.attributes('title') === multi)
    await tile!.trigger('click')
    // State selector appears once a multi-state object is chosen.
    const stateBtn = w.find(`[data-test="animation-object-state-${nonIdle}"]`)
    expect(stateBtn.exists()).toBe(true)
    await stateBtn.trigger('click')
    await w.find('[data-test="animation-confirm"]').trigger('click')
    expect(w.emitted('update:animation')?.[0]).toEqual([`object:${multi}@${nonIdle}`])
  })

  it('seeds the draft from an existing object@state value (opens on the Objects tab)', () => {
    const multi = listObjectSpriteKeys().find((k) => listObjectAnimationStates(k).length > 1)!
    const nonIdle = listObjectAnimationStates(multi).find((s) => s !== 'idle')!
    const w = mountPicker(`object:${multi}@${nonIdle}`)
    // The Objects gallery is the visible one, and the seeded state is selected.
    expect(w.find('[data-test="animation-objects"]').exists()).toBe(true)
    expect(w.find(`[data-test="animation-object-state-${nonIdle}"]`).classes()).toContain('anp__state--sel')
  })
})
