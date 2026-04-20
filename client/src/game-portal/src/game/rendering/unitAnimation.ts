import type { UnitDirection } from './unitSprites'

export type UnitAnimationName = 'idle' | 'walking' | 'attack' | 'chop'

export interface UnitAnimationSample {
  direction: UnitDirection
  animation: UnitAnimationName
  frameIndex: number
}

interface UnitAnimState {
  direction: UnitDirection
  animation: UnitAnimationName
  animStartedAt: number
  lastX: number
  lastY: number
  lastSampleAt: number
}

const DEFAULT_FRAME_MS = 125
// px/ms of interpolated movement below which we treat the unit as standing still.
// Low enough that slow walkers still read as walking, high enough to ignore
// sub-pixel jitter between interpolation frames.
const MOVING_THRESHOLD_PX_PER_MS = 0.005

export class UnitAnimationController {
  private states = new Map<number, UnitAnimState>()
  private frameDurationMs: number

  constructor(frameDurationMs = DEFAULT_FRAME_MS) {
    this.frameDurationMs = frameDurationMs
  }

  sample(
    unitId: number,
    x: number,
    y: number,
    status: string | undefined,
    targetX: number | undefined,
    targetY: number | undefined,
    renderTime: number,
  ): UnitAnimationSample {
    let state = this.states.get(unitId)
    if (!state) {
      state = {
        direction: 'south',
        animation: 'idle',
        animStartedAt: renderTime,
        lastX: x,
        lastY: y,
        lastSampleAt: renderTime,
      }
      this.states.set(unitId, state)
    }

    const dt = Math.max(1, renderTime - state.lastSampleAt)
    const dx = x - state.lastX
    const dy = y - state.lastY
    const speed = Math.hypot(dx, dy) / dt
    const moving = speed > MOVING_THRESHOLD_PX_PER_MS

    let direction = state.direction
    if (moving) {
      direction = classifyDirection(dx, dy)
    } else if (
      status === 'Attacking' &&
      targetX !== undefined &&
      targetY !== undefined
    ) {
      direction = classifyDirection(targetX - x, targetY - y)
    }

    const animation = pickAnimation(status, moving)

    if (animation !== state.animation) {
      state.animation = animation
      state.animStartedAt = renderTime
    }
    state.direction = direction
    state.lastX = x
    state.lastY = y
    state.lastSampleAt = renderTime

    const frameIndex = Math.floor(
      (renderTime - state.animStartedAt) / this.frameDurationMs,
    )

    return { direction, animation, frameIndex }
  }

  prune(activeIds: Set<number>) {
    for (const id of this.states.keys()) {
      if (!activeIds.has(id)) this.states.delete(id)
    }
  }
}

function classifyDirection(dx: number, dy: number): UnitDirection {
  if (Math.abs(dx) >= Math.abs(dy)) {
    return dx >= 0 ? 'east' : 'west'
  }
  return dy >= 0 ? 'south' : 'north'
}

function pickAnimation(
  status: string | undefined,
  moving: boolean,
): UnitAnimationName {
  if (status === 'Attacking') return 'attack'
  if (status === 'Chopping Wood') return 'chop'
  if (moving) return 'walking'
  return 'idle'
}
