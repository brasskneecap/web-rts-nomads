import type { UnitDirection } from './unitSprites'

export type UnitAnimationName = 'idle' | 'walking' | 'attacking' | 'chopping' | 'carrying_gold' | 'whirlwind'

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
const MOVING_THRESHOLD_PX_PER_MS = 0.005
// How strongly the non-dominant axis must exceed the dominant one to flip
// facing. 0 = no hysteresis (flips on any tie), 0.4 = the new axis must be
// >40% larger than the current one. Prevents diagonal jitter.
const FACING_HYSTERESIS = 0.4

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
    serverMoving: boolean | undefined,
    attackFacing: { dx: number; dy: number } | null,
    attackFrameDurationMs: number | undefined,
    renderTime: number,
    carriedResource: string | undefined,
    unitType: string | undefined,
    whirlwindActive: boolean,
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
    const interpSpeed = Math.hypot(dx, dy) / dt
    const interpolatedMoving = interpSpeed > MOVING_THRESHOLD_PX_PER_MS

    // Facing — priority: attack target, then movement, then sticky last value.
    let direction = state.direction
    if (status === 'Attacking' && attackFacing) {
      direction = classifyDirection(attackFacing.dx, attackFacing.dy, state.direction)
    } else if (interpolatedMoving) {
      direction = classifyDirection(dx, dy, state.direction)
    }

    // Animation — attacking/chopping stay sticky even if the server ticks
    // `moving` briefly; walking requires either the server flag or visible
    // interpolation movement so we don't freeze mid-stride between snapshots.
    const animation = pickAnimation(
      status,
      serverMoving === true || interpolatedMoving,
      carriedResource,
      unitType,
      whirlwindActive,
    )

    if (animation !== state.animation) {
      state.animation = animation
      state.animStartedAt = renderTime
    }
    state.direction = direction
    state.lastX = x
    state.lastY = y
    state.lastSampleAt = renderTime

    const frameMs =
      animation === 'attacking' && attackFrameDurationMs && attackFrameDurationMs > 0
        ? attackFrameDurationMs
        : this.frameDurationMs
    const frameIndex = Math.floor((renderTime - state.animStartedAt) / frameMs)

    return { direction, animation, frameIndex }
  }

  prune(activeIds: Set<number>) {
    for (const id of this.states.keys()) {
      if (!activeIds.has(id)) this.states.delete(id)
    }
  }
}

// Picks a cardinal direction from a vector, biased toward `current` so small
// diagonal wobble doesn't flip facing. To switch axes (e.g. east → north),
// the new axis must exceed the old one by FACING_HYSTERESIS. Same-axis flips
// (east → west when dx goes negative) happen naturally.
function classifyDirection(
  dx: number,
  dy: number,
  current: UnitDirection,
): UnitDirection {
  const ax = Math.abs(dx)
  const ay = Math.abs(dy)
  if (ax === 0 && ay === 0) return current

  const currentOnX = current === 'east' || current === 'west'
  if (currentOnX) {
    if (ay > ax * (1 + FACING_HYSTERESIS)) {
      return dy >= 0 ? 'south' : 'north'
    }
    return dx >= 0 ? 'east' : 'west'
  }
  if (ax > ay * (1 + FACING_HYSTERESIS)) {
    return dx >= 0 ? 'east' : 'west'
  }
  return dy >= 0 ? 'south' : 'north'
}

function pickAnimation(
  status: string | undefined,
  moving: boolean,
  carriedResource: string | undefined,
  unitType: string | undefined,
  whirlwindActive: boolean,
): UnitAnimationName {
  // Whirlwind is a bonus-attack VFX overlay: while the server's
  // whirlwind_core buff is live (WhirlwindAnimRemaining > 0), the spin
  // animation replaces whatever the unit would otherwise play. Regular
  // attacks keep firing underneath on their normal cooldown — this only
  // swaps the visual, not any game logic.
  if (whirlwindActive) return 'whirlwind'

  if (status === 'Attacking') {
    // Workers have no dedicated attack sprite — reuse the chopping animation
    // so their melee swing reads correctly (same axe arc they use on trees).
    if (unitType === 'worker') return 'chopping'
    return 'attacking'
  }
  if (status === 'Chopping Wood') return 'chopping'
  if (moving) {
    if (carriedResource === 'gold') return 'carrying_gold'
    return 'walking'
  }
  return 'idle'
}
