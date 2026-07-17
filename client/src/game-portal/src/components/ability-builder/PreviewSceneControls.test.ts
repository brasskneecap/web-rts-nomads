import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import PreviewSceneControls from './PreviewSceneControls.vue'
import { PREVIEW_SCENE_ORIGIN } from './previewScene'

describe('PreviewSceneControls', () => {
  it('emits a default scene on mount (matching defaultPreviewRequest)', () => {
    const wrapper = mount(PreviewSceneControls)
    const emitted = wrapper.emitted('update:modelValue')
    expect(emitted).toBeTruthy()
    const scene = emitted![emitted!.length - 1][0] as { units: unknown[]; target: number; seed: number; durationSeconds: number }
    expect(scene.units).toHaveLength(2) // 1 enemy + 1 ally, matching defaultPreviewRequest
    expect(scene.target).toBe(0)
    expect(scene.seed).toBe(1)
    expect(scene.durationSeconds).toBe(3)
  })

  it('changing enemy count rebuilds units[]', async () => {
    const wrapper = mount(PreviewSceneControls)
    await wrapper.find('[data-test="preview-enemy-count"]').setValue(3)
    const emitted = wrapper.emitted('update:modelValue')!
    const scene = emitted[emitted.length - 1][0] as { units: { team: string }[] }
    expect(scene.units.filter((u) => u.team === 'enemy')).toHaveLength(3)
  })

  it('changing the target selector to "First ally" retargets to the first ally unit', async () => {
    const wrapper = mount(PreviewSceneControls)
    await wrapper.find('[data-test="preview-target-selector"]').setValue('first_ally')
    const emitted = wrapper.emitted('update:modelValue')!
    const scene = emitted[emitted.length - 1][0] as { units: { team: string }[]; target: number }
    // 1 enemy at index 0, so the first ally lands at index 1.
    expect(scene.target).toBe(1)
    expect(scene.units[scene.target].team).toBe('ally')
  })

  it('changing the target selector to "Self" clears the unit target', async () => {
    const wrapper = mount(PreviewSceneControls)
    await wrapper.find('[data-test="preview-target-selector"]').setValue('self')
    const emitted = wrapper.emitted('update:modelValue')!
    const scene = emitted[emitted.length - 1][0] as { target: number; castX: number; castY: number }
    expect(scene.target).toBe(-1)
    // A self-cast's ground point is wherever the caster stands — the scene
    // origin. Derived from the constant, not pinned to literals, so moving
    // the scene doesn't require editing this expectation.
    expect(scene.castX).toBe(PREVIEW_SCENE_ORIGIN.x)
    expect(scene.castY).toBe(PREVIEW_SCENE_ORIGIN.y)
  })

  it('lays the whole scene out on the map, not around the world origin', () => {
    // Regression: the caster used to sit at (0,0) with allies at negative X,
    // which put them off the map's terrain (it spans [0,w]x[0,h]) and rendered
    // the dummies over a black void. Every unit must be at positive coords.
    const wrapper = mount(PreviewSceneControls)
    const emitted = wrapper.emitted('update:modelValue')!
    const scene = emitted[emitted.length - 1][0] as { units: { x: number; y: number }[] }
    expect(scene.units.length).toBeGreaterThan(0)
    for (const u of scene.units) {
      expect(u.x).toBeGreaterThan(0)
      expect(u.y).toBeGreaterThan(0)
    }
  })
})
