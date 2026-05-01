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
// Each of the 8 facings owns a 45° arc; `current` gets this many extra
// degrees of stickiness before a neighbor can take over, which suppresses
// jitter at the boundaries between adjacent facings.
const FACING_HYSTERESIS_DEG = 7.5

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
    actionFacing: { dx: number; dy: number } | null,
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

    // Facing — priority: action target (attack/work), then movement, then
    // sticky last value. The action-target hint lets stationary units (chopping
    // a tree, constructing a building, attacking from melee range) orient
    // toward their target even though no interpolated movement is happening.
    let direction = state.direction
    if (actionFacing) {
      direction = classifyDirection(actionFacing.dx, actionFacing.dy, state.direction)
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

// Compass angles in screen space (x right, y down): east = 0°, increasing
// clockwise toward south = 90°, etc. Used for 8-way snapping.
const DIRECTION_ANGLE_DEG: Record<UnitDirection, number> = {
  east: 0,
  'south-east': 45,
  south: 90,
  'south-west': 135,
  west: 180,
  'north-west': 225,
  north: 270,
  'north-east': 315,
}

// Smallest angular distance (degrees) between two compass angles in [0,360).
function angularDistanceDeg(a: number, b: number): number {
  const d = Math.abs(a - b) % 360
  return d > 180 ? 360 - d : d
}

// Picks the closest of 8 compass directions for a movement/facing vector,
// biased toward `current` so small wobble doesn't flip facing. Each
// direction owns a 45° arc; `current` keeps an extra FACING_HYSTERESIS_DEG
// of stickiness, which suppresses jitter at the boundaries (and between
// adjacent diagonal/cardinal pairs). Units that only ship 4-way sprites
// degrade to the nearest cardinal at draw time via `pickDirection`.
function classifyDirection(
  dx: number,
  dy: number,
  current: UnitDirection,
): UnitDirection {
  if (dx === 0 && dy === 0) return current
  const angle = ((Math.atan2(dy, dx) * 180) / Math.PI + 360) % 360

  let best = current
  let bestScore = Infinity
  for (const dir of Object.keys(DIRECTION_ANGLE_DEG) as UnitDirection[]) {
    const dist = angularDistanceDeg(angle, DIRECTION_ANGLE_DEG[dir])
    const score = dir === current ? dist - FACING_HYSTERESIS_DEG : dist
    if (score < bestScore) {
      bestScore = score
      best = dir
    }
  }
  return best
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
