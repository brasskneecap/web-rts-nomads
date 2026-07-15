import { describe, expect, it } from 'vitest'
import { channelLoopFrameIndex } from './unitAnimation'

// channelLoopFrameIndex is the single source of truth for "which frame of the
// casting sheet does a channel show at step N". Both the in-game controller
// (UnitAnimationController.sample) and the editor's channel-loop preview call
// it, so the preview matches gameplay exactly. These pin its contract.
describe('channelLoopFrameIndex', () => {
  it('returns null when no loop start is authored', () => {
    expect(channelLoopFrameIndex(undefined, undefined, 0)).toBeNull()
    expect(channelLoopFrameIndex(undefined, 5, 3)).toBeNull()
  })

  it('returns null for a negative start (loop unset sentinel)', () => {
    expect(channelLoopFrameIndex(-1, 4, 2)).toBeNull()
  })

  it('loops one-way over [start, end] inclusive', () => {
    // start=3, end=5 -> frames 3,4,5 repeating.
    expect(channelLoopFrameIndex(3, 5, 0)).toBe(3)
    expect(channelLoopFrameIndex(3, 5, 1)).toBe(4)
    expect(channelLoopFrameIndex(3, 5, 2)).toBe(5)
    expect(channelLoopFrameIndex(3, 5, 3)).toBe(3) // wraps back to start
    expect(channelLoopFrameIndex(3, 5, 4)).toBe(4)
  })

  it('holds a single frame when start === end', () => {
    expect(channelLoopFrameIndex(3, 3, 0)).toBe(3)
    expect(channelLoopFrameIndex(3, 3, 7)).toBe(3)
  })

  it('holds a single frame when end < start (degenerate range)', () => {
    expect(channelLoopFrameIndex(5, 3, 0)).toBe(5)
    expect(channelLoopFrameIndex(5, 3, 9)).toBe(5)
  })

  it('holds a single frame at start when end is absent', () => {
    expect(channelLoopFrameIndex(3, undefined, 0)).toBe(3)
    expect(channelLoopFrameIndex(3, undefined, 4)).toBe(3)
  })

  it('wraps negative phases the same way the runtime controller does', () => {
    // count = 3; ((-1 % 3) + 3) % 3 = 2 -> start + 2 = 5
    expect(channelLoopFrameIndex(3, 5, -1)).toBe(5)
    expect(channelLoopFrameIndex(3, 5, -3)).toBe(3)
  })
})
