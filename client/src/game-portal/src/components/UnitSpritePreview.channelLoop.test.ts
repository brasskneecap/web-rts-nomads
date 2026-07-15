import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UnitSpritePreview from './UnitSpritePreview.vue'
import {
  clearRuntimeSpriteSets,
  registerRuntimeSpriteSet,
  type StripAnimation,
  type UnitSpriteSet,
} from '@/game/rendering/unitSprites'

// The component starts a requestAnimationFrame loop on setup; happy-dom may not
// provide rAF, and the channel-loop assertions here are all about the
// synchronous state produced by toggling the mode (the per-frame looping over
// time is pinned separately by channelLoopFrameIndex's unit tests).
beforeEach(() => {
  vi.stubGlobal('requestAnimationFrame', () => 1)
  vi.stubGlobal('cancelAnimationFrame', () => {})
  // A sprite set that actually has a `casting` sheet, so the channel-loop
  // preview has real frames to loop over.
  const casting: StripAnimation = {
    frameCount: 6,
    frameWidth: 64,
    frameHeight: 64,
    directions: {} as StripAnimation['directions'],
  }
  const fake: UnitSpriteSet = {
    key: 'siphoner',
    size: { width: 64, height: 64 },
    rotations: {} as UnitSpriteSet['rotations'],
    animations: new Map([['casting', casting]]),
    beamOrigin: { x: 0, y: 0 },
  }
  registerRuntimeSpriteSet(fake)
})

afterEach(() => {
  clearRuntimeSpriteSets()
  vi.restoreAllMocks()
})

function channelToggle(wrapper: ReturnType<typeof mount>) {
  return wrapper.findAll('button').find((b) => b.text().includes('Channel Loop'))
}

function frameRange(wrapper: ReturnType<typeof mount>): HTMLInputElement {
  return wrapper.find('.sprite-preview__scrub input[type="range"]').element as HTMLInputElement
}

describe('UnitSpritePreview channel-loop preview', () => {
  it('shows no Channel Loop control when the unit has no channeling ability', async () => {
    const wrapper = mount(UnitSpritePreview, { props: { unitKey: 'siphoner' } })
    await flushPromises()
    expect(channelToggle(wrapper)).toBeFalsy()
  })

  it('shows a Channel Loop control when the unit has a channeling ability', async () => {
    const wrapper = mount(UnitSpritePreview, {
      props: {
        unitKey: 'siphoner',
        channelAbility: { id: 'siphon_life', tickIntervalSeconds: 0.5 },
        channelLoop: { start: 3, end: 5 },
      },
    })
    await flushPromises()
    expect(channelToggle(wrapper)).toBeTruthy()
  })

  it('entering channel mode plays the casting sheet pinned to the loop start frame', async () => {
    const wrapper = mount(UnitSpritePreview, {
      props: {
        unitKey: 'siphoner',
        channelAbility: { id: 'siphon_life', tickIntervalSeconds: 0.5 },
        channelLoop: { start: 3, end: 5 },
      },
    })
    await flushPromises()

    await channelToggle(wrapper)!.trigger('click')
    await flushPromises()

    // Animation forced to casting, and the loop starts at frame `start` (3).
    const anim = wrapper.find('.sprite-preview__controls select').element as HTMLSelectElement
    expect(anim.value).toBe('casting')
    expect(frameRange(wrapper).value).toBe('3')
    // The control reads as active while channel mode is on.
    expect(channelToggle(wrapper)!.classes()).toContain('is-active')
  })

  it('leaves channel mode (and stops pinning) when toggled off', async () => {
    const wrapper = mount(UnitSpritePreview, {
      props: {
        unitKey: 'siphoner',
        channelAbility: { id: 'siphon_life', tickIntervalSeconds: 0.5 },
        channelLoop: { start: 3, end: 5 },
      },
    })
    await flushPromises()

    await channelToggle(wrapper)!.trigger('click')
    await flushPromises()
    await channelToggle(wrapper)!.trigger('click')
    await flushPromises()

    expect(channelToggle(wrapper)!.classes()).not.toContain('is-active')
  })
})
