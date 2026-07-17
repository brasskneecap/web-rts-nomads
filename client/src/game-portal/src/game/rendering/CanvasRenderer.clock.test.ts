// CanvasRenderer.clock.test.ts — exercises the N3 injectable render clock
// (see CanvasRenderer's "Injectable render clock" doc comment). jsdom's
// HTMLCanvasElement.getContext('2d') returns null, which normally makes
// CanvasRenderer's constructor throw ("Canvas not supported") before any of
// its logic runs — every existing test that touches AbilityPreviewCanvas.vue
// works around this by asserting the component stays inert. Here we instead
// stub getContext with a permissive fake 2D context (every method/property
// access resolves to a no-op function or 0) so render() actually executes,
// letting us assert the SEAM itself rather than only the pure helper
// functions around it: the constructor defaults to the real clock, an
// injected clock is honored in its place, and repeated renders of the same
// injected instant are idempotent.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { GameState } from '../core/GameState'
import { Camera } from './Camera'
import { CanvasRenderer } from './CanvasRenderer'

// A single Proxy handles every 2D context method/property CanvasRenderer's
// render() touches (fillRect, save/restore, scale/translate, arc, gradients,
// measureText, etc.) without enumerating them: property reads return a
// callable no-op that itself returns a minimal fake result (covers
// ctx.createRadialGradient().addColorStop(...) chains and
// ctx.measureText(...).width), and property writes (fillStyle, font, …) are
// silently accepted.
function makeFakeContext(): CanvasRenderingContext2D {
  const fakeMetrics = { width: 0 }
  const fakeGradient = { addColorStop: () => {} }
  const handler: ProxyHandler<Record<string, unknown>> = {
    get(_target, prop) {
      if (prop === 'canvas') return undefined
      if (prop === 'measureText') return () => fakeMetrics
      if (prop === 'createRadialGradient' || prop === 'createLinearGradient') {
        return () => fakeGradient
      }
      // Any other method call is a harmless no-op; any other property read
      // resolves to a fresh no-op callable too (covers chained calls this
      // stub doesn't know about ahead of time).
      return () => undefined
    },
    set() {
      return true
    },
  }
  return new Proxy({}, handler) as unknown as CanvasRenderingContext2D
}

function makeCanvas(): HTMLCanvasElement {
  const canvas = document.createElement('canvas')
  const fakeCtx = makeFakeContext()
  vi.spyOn(canvas, 'getContext').mockReturnValue(fakeCtx as unknown as RenderingContext)
  return canvas
}

// Stubs HTMLCanvasElement.prototype.getContext globally too — CanvasRenderer's
// constructor creates its OWN offscreen fog canvas via
// document.createElement('canvas') internally, which wouldn't go through the
// instance-level spy above.
beforeEach(() => {
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockImplementation(
    () => makeFakeContext() as unknown as RenderingContext,
  )
})

afterEach(() => {
  vi.restoreAllMocks()
})

function makeRenderer(timeSource?: () => number): CanvasRenderer {
  const canvas = makeCanvas()
  const state = new GameState()
  const camera = new Camera()
  return timeSource
    ? new CanvasRenderer(canvas, state, camera, timeSource)
    : new CanvasRenderer(canvas, state, camera)
}

describe('CanvasRenderer render clock (N3)', () => {
  it('defaults to the real clock: render() reads performance.now() when no timeSource is injected', () => {
    const nowSpy = vi.spyOn(performance, 'now')
    const renderer = makeRenderer()
    const callsBefore = nowSpy.mock.calls.length
    renderer.render()
    expect(nowSpy.mock.calls.length).toBeGreaterThan(callsBefore)
  })

  it('an injected clock is used in place of the real one: render() calls the injected function, not performance.now()', () => {
    const nowSpy = vi.spyOn(performance, 'now')
    const injected = vi.fn(() => 12345)
    const renderer = makeRenderer(injected)

    const nowCallsBefore = nowSpy.mock.calls.length
    renderer.render()

    expect(injected).toHaveBeenCalled()
    // The live/default path is the ONLY caller of performance.now() inside
    // CanvasRenderer (see the constructor's default param) — with an
    // injected clock supplied, render() must not fall back to it.
    expect(nowSpy.mock.calls.length).toBe(nowCallsBefore)
  })

  it('frame N renders identically twice: a fixed injected clock produces the same renderTime both times (idempotence)', () => {
    const injected = vi.fn(() => 700) // e.g. previewClockMs(14)
    const renderer = makeRenderer(injected)

    renderer.render()
    const callsAfterFirst = injected.mock.results.map((r) => r.value)
    renderer.render()
    const callsAfterSecond = injected.mock.results.slice(callsAfterFirst.length).map((r) => r.value)

    // Every read the second render() takes returns the exact same instant as
    // the first — pausing/re-rendering on the same frame is stable.
    expect(callsAfterSecond.every((v) => v === 700)).toBe(true)
    expect(callsAfterFirst.every((v) => v === 700)).toBe(true)
  })

  it('does not throw constructing/rendering with a scrubbed-backward (non-monotonic) injected clock', () => {
    let frameIndex = 10
    const renderer = makeRenderer(() => frameIndex * 50)
    expect(() => renderer.render()).not.toThrow()
    frameIndex = 2 // scrub backward
    expect(() => renderer.render()).not.toThrow()
  })
})
