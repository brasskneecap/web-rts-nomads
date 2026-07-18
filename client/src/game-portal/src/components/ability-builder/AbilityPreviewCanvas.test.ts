import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { MatchSnapshotMessage } from '@/game/network/protocol'
import type { PreviewFrame, PreviewSceneUnit } from '@/game/abilities/program/programPreview'
import AbilityPreviewCanvas from './AbilityPreviewCanvas.vue'
import { getUnitBodyRect } from '@/game/rendering/unitSprites'

// bodyCenter returns the CENTER of a unit's visible sprite body (the same
// world-space rect the drag hit-test uses via isPointInUnitBody). The hit-test
// targets the sprite BODY, which sits ABOVE the unit's feet (unit.x/y), so a
// click must land inside the body — not on the exact feet — to grab the unit.
// With the drag block's identity camera, world coords === client coords.
function bodyCenter(x: number, y: number, unitType: string): { x: number; y: number } {
  const r = getUnitBodyRect({ x, y, unitType })
  return { x: (r.minX + r.maxX) / 2, y: (r.minY + r.maxY) / 2 }
}

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

  // Phase 6b: edit mode (frames: [] WITH a live scene to place) is a distinct
  // third state from both "idle state" (frames: [], no scene either) and
  // "populated frames" (a real replay) above.
  describe('edit mode (frames: [], sceneUnits: non-empty)', () => {
    const sceneUnits: PreviewSceneUnit[] = [
      { team: 'enemy', x: 720, y: 500, hp: 200, maxHp: 200 },
      { team: 'ally', x: 520, y: 500, hp: 40, maxHp: 100 },
    ]

    it('does NOT show the idle placeholder once there is a scene to place, even with no frames', () => {
      const wrapper = mount(AbilityPreviewCanvas, {
        props: { frames: [], currentTick: 0, sceneUnits, casterX: 600, casterY: 500 },
      })
      expect(wrapper.find('[data-test="preview-canvas-empty"]').exists()).toBe(false)
    })

    it('shows the idle placeholder when both frames AND sceneUnits are empty (truly empty)', () => {
      const wrapper = mount(AbilityPreviewCanvas, { props: { frames: [], currentTick: 0, sceneUnits: [] } })
      expect(wrapper.find('[data-test="preview-canvas-empty"]').exists()).toBe(true)
    })

    it('mounts the drag-to-place layer while frames is empty', () => {
      const wrapper = mount(AbilityPreviewCanvas, {
        props: { frames: [], currentTick: 0, sceneUnits, casterX: 600, casterY: 500 },
      })
      expect(wrapper.find('[data-test="preview-drag-layer"]').exists()).toBe(true)
    })

    it('removes the drag-to-place layer once real frames exist (no interactive layer during replay)', () => {
      const wrapper = mount(AbilityPreviewCanvas, {
        props: { frames: makeFrames(4), currentTick: 0, sceneUnits, casterX: 600, casterY: 500 },
      })
      expect(wrapper.find('[data-test="preview-drag-layer"]').exists()).toBe(false)
    })

    it('replay-only playback controls stay disabled in edit mode regardless of sceneUnits', () => {
      const wrapper = mount(AbilityPreviewCanvas, {
        props: { frames: [], currentTick: 0, sceneUnits, casterX: 600, casterY: 500 },
      })
      expect(wrapper.find('[data-test="preview-play-toggle"]').attributes('disabled')).toBeDefined()
      expect(wrapper.find('[data-test="preview-scrub"]').attributes('disabled')).toBeDefined()
    })
  })

  // Phase 6b drag lifecycle. jsdom/happy-dom's canvas getContext('2d') returns
  // null by default, which normally makes onMounted bail entirely (no
  // renderer/camera — see the module's "Stay inert" comment) — every OTHER
  // describe block above relies on exactly that inertness. Here we stub a
  // permissive fake 2D context (same technique as
  // game/rendering/CanvasRenderer.clock.test.ts) so onMounted actually builds
  // a real Camera, letting us exercise hit-testing/dragging for real.
  //
  // Camera math is made deterministic without pinning any camera internals:
  // happy-dom's canvas elements report clientWidth/clientHeight === 0 (no
  // real layout engine), and CanvasRenderer's own resize() sets
  // `canvas.width = canvas.clientWidth` on construction — so canvas.width
  // stays 0 for the lifetime of the test, which makes refreshCamera's own
  // `canvas.width <= 0` guard bail on every call. The Camera therefore never
  // leaves its class-field defaults (x: 0, y: 0, zoom: 1), turning
  // world<->screen into the identity mapping — screen coordinates simply ARE
  // world coordinates, with no camera-fit arithmetic to reproduce in the
  // test. happy-dom's getBoundingClientRect() likewise defaults to an
  // all-zero rect, so `clientX/Y` themselves are already "stage-relative
  // screen" coordinates with no offset to subtract.
  describe('drag lifecycle', () => {
    function makeFakeContext(): CanvasRenderingContext2D {
      const fakeMetrics = { width: 0 }
      const fakeGradient = { addColorStop: () => {} }
      const handler: ProxyHandler<Record<string, unknown>> = {
        get(_target, prop) {
          if (prop === 'canvas') return undefined
          if (prop === 'measureText') return () => fakeMetrics
          if (prop === 'createRadialGradient' || prop === 'createLinearGradient') return () => fakeGradient
          return () => undefined
        },
        set() {
          return true
        },
      }
      return new Proxy({}, handler) as unknown as CanvasRenderingContext2D
    }

    beforeEach(() => {
      vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockImplementation(
        () => makeFakeContext() as unknown as RenderingContext,
      )
    })

    afterEach(() => {
      vi.restoreAllMocks()
    })

    const sceneUnits: PreviewSceneUnit[] = [{ team: 'enemy', x: 720, y: 500, hp: 200, maxHp: 200 }]

    it('a pointerdown+pointermove on a scene unit emits its new world position', async () => {
      const wrapper = mount(AbilityPreviewCanvas, {
        props: { frames: [], currentTick: 0, sceneUnits, casterX: 600, casterY: 500 },
      })
      const layer = wrapper.find('[data-test="preview-drag-layer"]')

      // Camera is the identity mapping (see the describe block's doc comment),
      // so clientX/Y === world coordinates. Grab the enemy by its sprite BODY
      // (which sits above its feet at (720,500)), then drag the pointer by a
      // known delta. The grab offset is preserved, so the unit's anchor shifts
      // by exactly that delta (it does NOT teleport its anchor onto the cursor).
      const grab = bodyCenter(720, 500, 'raider')
      const dx = 20
      const dy = 10
      await layer.trigger('pointerdown', { clientX: grab.x, clientY: grab.y, pointerId: 1 })
      await layer.trigger('pointermove', { clientX: grab.x + dx, clientY: grab.y + dy, pointerId: 1 })

      const emitted = wrapper.emitted('update:scene-unit')
      expect(emitted).toBeTruthy()
      expect(emitted![emitted!.length - 1]).toEqual([{ index: 0, x: 720 + dx, y: 500 + dy }])
    })

    it('a pointerdown+pointermove on the caster emits its new world position', async () => {
      const wrapper = mount(AbilityPreviewCanvas, {
        props: { frames: [], currentTick: 0, sceneUnits, casterX: 600, casterY: 500 },
      })
      const layer = wrapper.find('[data-test="preview-drag-layer"]')

      // Grab the caster by its body, drag by a known delta; the grab offset is
      // preserved so the anchor moves by exactly that delta.
      const grab = bodyCenter(600, 500, 'adept')
      const dx = 50
      const dy = -20
      await layer.trigger('pointerdown', { clientX: grab.x, clientY: grab.y, pointerId: 1 })
      await layer.trigger('pointermove', { clientX: grab.x + dx, clientY: grab.y + dy, pointerId: 1 })

      const emitted = wrapper.emitted('update:caster')
      expect(emitted).toBeTruthy()
      expect(emitted![emitted!.length - 1]).toEqual([{ x: 600 + dx, y: 500 + dy }])
    })

    it('a pointerdown on empty stage (far from caster/units) starts no drag — a later move emits nothing', async () => {
      const wrapper = mount(AbilityPreviewCanvas, {
        props: { frames: [], currentTick: 0, sceneUnits, casterX: 600, casterY: 500 },
      })
      const layer = wrapper.find('[data-test="preview-drag-layer"]')

      await layer.trigger('pointerdown', { clientX: 5000, clientY: 5000, pointerId: 1 })
      await layer.trigger('pointermove', { clientX: 5100, clientY: 5100, pointerId: 1 })

      expect(wrapper.emitted('update:scene-unit')).toBeFalsy()
      expect(wrapper.emitted('update:caster')).toBeFalsy()
    })

    it('pointerup ends the drag — a subsequent pointermove with the same pointerId emits nothing further', async () => {
      const wrapper = mount(AbilityPreviewCanvas, {
        props: { frames: [], currentTick: 0, sceneUnits, casterX: 600, casterY: 500 },
      })
      const layer = wrapper.find('[data-test="preview-drag-layer"]')

      const grab = bodyCenter(720, 500, 'raider')
      await layer.trigger('pointerdown', { clientX: grab.x, clientY: grab.y, pointerId: 1 })
      await layer.trigger('pointermove', { clientX: 740, clientY: 510, pointerId: 1 })
      await layer.trigger('pointerup', { pointerId: 1 })
      await layer.trigger('pointermove', { clientX: 900, clientY: 900, pointerId: 1 })

      const emitted = wrapper.emitted('update:scene-unit')!
      // Only the ONE move between pointerdown and pointerup produced an emit.
      expect(emitted).toHaveLength(1)
    })
  })
})
