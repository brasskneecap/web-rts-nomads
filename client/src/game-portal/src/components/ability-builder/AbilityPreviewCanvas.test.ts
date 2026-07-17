import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import type { MatchSnapshotMessage } from '@/game/network/protocol'
import type { PreviewFrame } from '@/game/abilities/program/programPreview'
import AbilityPreviewCanvas from './AbilityPreviewCanvas.vue'

// makeFrames: minimal PreviewFrame stubs at the real 20fps capture cadence
// (PREVIEW_FRAME_DT_SECONDS). jsdom has no real 2D canvas context, so
// onMounted bails before ever reading `snapshot`'s contents — the cast below
// is safe for test purposes only (mirrors AbilityPreviewPanel.test.ts).
function makeFrames(n: number): PreviewFrame[] {
  return Array.from({ length: n }, (_, i) => ({
    tick: i,
    t: i * 0.05,
    snapshot: {} as MatchSnapshotMessage,
  }))
}

describe('AbilityPreviewCanvas', () => {
  // Task 5: the panel now mounts this component unconditionally, even before
  // any preview has run, so `frames: []` is a real first-paint state, not a
  // transient gap between runs — this whole describe block is that coverage.
  describe('idle state (frames: [])', () => {
    it('mounts without throwing and shows the idle placeholder', () => {
      expect(() => mount(AbilityPreviewCanvas, { props: { frames: [], currentTick: 0 } })).not.toThrow()
      const wrapper = mount(AbilityPreviewCanvas, { props: { frames: [], currentTick: 0 } })
      expect(wrapper.find('[data-test="preview-canvas-empty"]').exists()).toBe(true)
      expect(wrapper.find('[data-test="preview-canvas-empty"]').text()).toContain('Run a preview')
    })

    it('disables every playback control', () => {
      const wrapper = mount(AbilityPreviewCanvas, { props: { frames: [], currentTick: 0 } })
      expect(wrapper.find('[data-test="preview-play-toggle"]').attributes('disabled')).toBeDefined()
      expect(wrapper.find('[data-test="preview-restart"]').attributes('disabled')).toBeDefined()
      expect(wrapper.find('[data-test="preview-scrub"]').attributes('disabled')).toBeDefined()
      expect(wrapper.find('[data-test="preview-speed-0.5"]').attributes('disabled')).toBeDefined()
      expect(wrapper.find('[data-test="preview-speed-1"]').attributes('disabled')).toBeDefined()
      expect(wrapper.find('[data-test="preview-speed-2"]').attributes('disabled')).toBeDefined()
    })

    it('the scrub range collapses to [0, 0] rather than going negative', () => {
      const wrapper = mount(AbilityPreviewCanvas, { props: { frames: [], currentTick: 0 } })
      const scrub = wrapper.find('[data-test="preview-scrub"]')
      expect(scrub.attributes('max')).toBe('0')
      expect(scrub.attributes('value')).toBe('0')
    })

    it('the time readout has no NaN/negative artifacts', () => {
      const wrapper = mount(AbilityPreviewCanvas, { props: { frames: [], currentTick: 0 } })
      const readout = wrapper.find('[data-test="preview-time-readout"]').text()
      expect(readout).not.toMatch(/NaN/)
      expect(readout).not.toMatch(/-/)
      expect(readout).toBe('0.00s / 0.00s')
    })

    it('does not throw when currentTick starts out-of-range for an empty frame list', () => {
      expect(() => mount(AbilityPreviewCanvas, { props: { frames: [], currentTick: 7 } })).not.toThrow()
    })
  })

  describe('populated frames', () => {
    it('hides the idle placeholder and enables playback controls', () => {
      const wrapper = mount(AbilityPreviewCanvas, { props: { frames: makeFrames(6), currentTick: 0 } })
      expect(wrapper.find('[data-test="preview-canvas-empty"]').exists()).toBe(false)
      expect(wrapper.find('[data-test="preview-play-toggle"]').attributes('disabled')).toBeUndefined()
      expect(wrapper.find('[data-test="preview-scrub"]').attributes('disabled')).toBeUndefined()
      expect(wrapper.find('[data-test="preview-scrub"]').attributes('max')).toBe('5')
    })

    it('going from idle to populated frames (a live run completing) does not throw', async () => {
      const wrapper = mount(AbilityPreviewCanvas, { props: { frames: [], currentTick: 0 } })
      await wrapper.setProps({ frames: makeFrames(4) })
      expect(wrapper.find('[data-test="preview-canvas-empty"]').exists()).toBe(false)
      expect(wrapper.find('[data-test="preview-scrub"]').attributes('max')).toBe('3')
    })

    it('going from populated back to idle (e.g. a run error clears frames) does not throw', async () => {
      const wrapper = mount(AbilityPreviewCanvas, { props: { frames: makeFrames(4), currentTick: 2 } })
      await wrapper.setProps({ frames: [] })
      expect(wrapper.find('[data-test="preview-canvas-empty"]').exists()).toBe(true)
      expect(wrapper.find('[data-test="preview-scrub"]').attributes('max')).toBe('0')
    })
  })
})
