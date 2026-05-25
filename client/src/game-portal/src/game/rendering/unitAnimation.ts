import type { UnitDirection } from './unitSprites'

export type UnitAnimationName = 'idle' | 'walking' | 'attacking' | 'casting' | 'chopping' | 'repairing' | 'carrying_gold'

export interface UnitAnimationSample {
  direction: UnitDirection
  animation: UnitAnimationName
  frameIndex: number
}

/** Timing inputs for a unit's attack animation, derived from its effective
 *  attackSpeed. `cycleMs` is the full server-side cooldown (= 1000/attackSpeed);
 *  `animDurationMs` is how long the animation actually plays within that cycle
 *  (capped at 1s — slow attackers play the swing briskly, then idle until the
 *  next swing). `frameDurationMs` is `animDurationMs / frameCount`. */
export interface AttackAnimationTiming {
  frameDurationMs: number
  animDurationMs: number
  cycleMs: number
}

interface UnitAnimState {
  direction: UnitDirection
  animation: UnitAnimationName
  animStartedAt: number
  lastX: number
  lastY: number
  lastSampleAt: number
  // Debug-only: previous tick's cycleElapsed value, used by the cycle-wrap
  // logger to detect modulo wraps. Reset to undefined on animation change so
  // the new animation's first sample doesn't print a spurious wrap.
  debugLastCycleElapsed?: number
}

const DEFAULT_FRAME_MS = 125
// px/ms of interpolated movement below which we treat the unit as standing still.
const MOVING_THRESHOLD_PX_PER_MS = 0.005
// Each of the 8 facings owns a 45° arc; `current` gets this many extra
// degrees of stickiness before a neighbor can take over, which suppresses
// jitter at the boundaries between adjacent facings.
const FACING_HYSTERESIS_DEG = 7.5
// Mirrors the server's attackDamageDeliveryFraction. Used only by the debug
// cycle-wrap log to predict when the server will apply damage relative to the
// animation cycle. If you tune the server constant, mirror it here for the
// prediction string to stay accurate; the runtime animation doesn't depend on
// this value (frame timing is driven by attackTiming.frameDurationMs).
const DEBUG_DAMAGE_DELIVERY_FRACTION = 0.7

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
    attackTiming: AttackAnimationTiming | undefined,
    renderTime: number,
    carriedResource: string | undefined,
    unitType: string | undefined,
    path: string | undefined,
    ownerId: string | undefined,
    localPlayerId: string | null | undefined,
    flyer: boolean | undefined,
    channelLoopStart: number | undefined,
    channelLoopEnd: number | undefined,
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
    const moving = serverMoving === true || interpolatedMoving
    const animation = pickAnimation(
      status,
      moving,
      carriedResource,
      unitType,
      path,
      flyer === true,
    )

    if (animation !== state.animation) {
      // Debug: when the toggle is enabled in DevTools, log every time a unit
      // enters the attacking pose so the timing can be correlated with the
      // damage popup logs emitted by GameState.emitDamageEvent.
      //
      // Enable:        window.debugAttackTiming = true
      // Filter to me:  window.debugAttackTimingMineOnly = true
      // Filter type:   window.debugAttackTimingUnitType = 'raider_brute'
      if (animation === 'attacking' && shouldLogAtkTiming(unitType, ownerId, localPlayerId)) {
        const cyc = attackTiming?.cycleMs ?? 0
        const anim = attackTiming?.animDurationMs ?? 0
        // eslint-disable-next-line no-console
        console.log(
          `[atk-timing] anim-start unit=${unitId} type=${unitType} t=${renderTime.toFixed(0)}ms cycle=${cyc.toFixed(0)} animDur=${anim.toFixed(0)}`,
        )
      }
      state.animation = animation
      state.animStartedAt = renderTime
      state.debugLastCycleElapsed = undefined
    }
    state.direction = direction
    state.lastX = x
    state.lastY = y
    state.lastSampleAt = renderTime

    // Attacking has bespoke timing tied to the unit's cooldown. The animation
    // window is capped at 1s; for cooldowns longer than that the unit goes
    // idle for the remainder of each cycle. Modulo against `cycleMs` makes the
    // animation re-fire on every server cooldown without the client needing a
    // dedicated "attack started" event — the server keeps emitting status
    // "Attacking" continuously, and animStartedAt only resets when the unit
    // leaves the attacking state, so the modulo gives us per-swing phase.
    if (animation === 'attacking' && attackTiming && attackTiming.cycleMs > 0) {
      const cycleElapsed = (renderTime - state.animStartedAt) % attackTiming.cycleMs
      // Debug: log the start of each animation cycle (modulo wrap) so cycles
      // beyond the first are visible too. Detected by the cycleElapsed value
      // landing in the first frame window after having been past it.
      if (shouldLogAtkTiming(unitType, ownerId, localPlayerId)) {
        const lastElapsed = state.debugLastCycleElapsed
        if (lastElapsed !== undefined && lastElapsed > cycleElapsed) {
          const cycleStart = state.animStartedAt + Math.floor((renderTime - state.animStartedAt) / attackTiming.cycleMs) * attackTiming.cycleMs
          const predictedDamageAt = cycleStart + attackTiming.animDurationMs * DEBUG_DAMAGE_DELIVERY_FRACTION
          // eslint-disable-next-line no-console
          console.log(
            `[atk-timing] cycle-wrap unit=${unitId} type=${unitType} t=${renderTime.toFixed(0)}ms cycle=${attackTiming.cycleMs.toFixed(0)} (server should fire damage ~${predictedDamageAt.toFixed(0)}ms)`,
          )
        }
        state.debugLastCycleElapsed = cycleElapsed
      }
      if (cycleElapsed >= attackTiming.animDurationMs) {
        // Flyers keep flapping during the rest portion of the attack cycle —
        // a frozen idle pose mid-air reads as the unit suddenly hovering
        // motionless. Free-running phase off renderTime so the wings beat
        // continuously across attack/idle transitions.
        if (flyer === true) {
          const flapFrame = Math.floor(renderTime / this.frameDurationMs)
          return { direction, animation: 'walking', frameIndex: flapFrame }
        }
        return { direction, animation: 'idle', frameIndex: 0 }
      }
      const frameIndex = Math.floor(cycleElapsed / attackTiming.frameDurationMs)
      return { direction, animation, frameIndex }
    }

    // Stationary worker holding gold: pin to frame 0 of carrying_gold so the
    // idle pose still shows the hold instead of reverting to the empty-handed
    // rotation. pickAnimation routes the idle case here by returning
    // 'carrying_gold' even when not moving.
    if (animation === 'carrying_gold' && !moving) {
      return { direction, animation, frameIndex: 0 }
    }

    // Channeled-beam loop: drive the casting sprite as a one-way loop over
    // [channelLoopStart, channelLoopEnd] inclusive at the unit's normal frame
    // cadence, producing a sustained-but-living pose for the channel.
    // Gated on status === 'Channeling' so casts and channels can share the
    // casting sheet without the cast ever freezing. start == end produces a
    // single held frame (loop degenerates to a hold). Negative/absent fields
    // fall through to the normal cycling default. Out-of-range positive
    // values modulo into the sheet at draw time via getUnitFrame.
    if (status === 'Channeling' && animation === 'casting') {
      const start = typeof channelLoopStart === 'number' && channelLoopStart >= 0 ? channelLoopStart : -1
      const endRaw = typeof channelLoopEnd === 'number' ? channelLoopEnd : start
      if (start >= 0) {
        const end = endRaw < start ? start : endRaw
        const frameCount = end - start + 1
        const phase = Math.floor((renderTime - state.animStartedAt) / this.frameDurationMs)
        const frameIndex = start + (frameCount > 0 ? ((phase % frameCount) + frameCount) % frameCount : 0)
        return { direction, animation, frameIndex }
      }
    }

    const frameIndex = Math.floor((renderTime - state.animStartedAt) / this.frameDurationMs)
    return { direction, animation, frameIndex }
  }

  prune(activeIds: Set<number>) {
    for (const id of this.states.keys()) {
      if (!activeIds.has(id)) this.states.delete(id)
    }
  }

  // Read-only view of a unit's persisted animation state for systems outside
  // the unit render pass (e.g. trees that need to shake in time with a
  // worker's chopping cycle). Returns null until the unit has been sampled
  // at least once. The caller knows the frame count of the animation it
  // cares about, so it computes phase itself.
  peekAnimation(unitId: number): { animation: UnitAnimationName; animStartedAt: number; frameDurationMs: number } | null {
    const state = this.states.get(unitId)
    if (!state) return null
    return {
      animation: state.animation,
      animStartedAt: state.animStartedAt,
      frameDurationMs: this.frameDurationMs,
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

// shouldLogAtkTiming reads the debug toggles set via the DevTools console and
// decides whether to emit a debug-log line for the given unit. Toggles:
//   window.debugAttackTiming         — master enable (truthy)
//   window.debugAttackTimingMineOnly — true → drop logs for enemy-owned units
//   window.debugAttackTimingUnitType — string → only log this unit type
// Note: "mine only" treats the enemy sentinel ownerId ('__enemy__') as not
// mine. In multiplayer this matches "real player" rather than the local
// player specifically — pass localPlayerId if a strict "owned by me" filter
// is needed (currently we just compare to the enemy sentinel).
function shouldLogAtkTiming(
  unitType: string | undefined,
  ownerId: string | undefined,
  localPlayerId: string | null | undefined,
): boolean {
  const win = globalThis as {
    debugAttackTiming?: boolean
    debugAttackTimingMineOnly?: boolean
    debugAttackTimingUnitType?: string
  }
  if (!win.debugAttackTiming) return false
  if (win.debugAttackTimingMineOnly) {
    if (localPlayerId) {
      if (ownerId !== localPlayerId) return false
    } else if (ownerId === '__enemy__') {
      return false
    }
  }
  if (win.debugAttackTimingUnitType && unitType !== win.debugAttackTimingUnitType) {
    return false
  }
  return true
}

function pickAnimation(
  status: string | undefined,
  moving: boolean,
  carriedResource: string | undefined,
  unitType: string | undefined,
  path: string | undefined,
  flyer: boolean,
): UnitAnimationName {
  if (status === 'Attacking') {
    // Workers have no dedicated attack sprite — reuse the chopping animation
    // so their melee swing reads correctly (same axe arc they use on trees).
    if (unitType === 'worker') return 'chopping'
    return 'attacking'
  }
  // Spell-cast slot, distinct from 'Attacking' (basic attack). Checked before
  // moving/idle so a cast always reads clearly and "interrupts" the idle pose.
  // Units without a dedicated casting sheet fall back to their attacking
  // animation via ANIMATION_FALLBACK['casting'] = 'attacking' in unitSprites.ts
  // — better than freezing in the neutral rotation pose. Base Acolyte has
  // its own casting sheet and is unaffected.
  //
  // 'Channeling' shares the casting sprite sheet — the channel lifecycle
  // (Siphon Life) pins frameIndex to UnitSnapshot.channelHoldFrame rather
  // than cycling. The pin is applied in UnitAnimationController.sample().
  if (status === 'Casting' || status === 'Channeling') return 'casting'
  if (status === 'Chopping Wood') return 'chopping'
  // All construction/repair statuses — including paused variants emitted when
  // no workers are assigned — map to the same hammer-swing animation.
  // 'Building (Paused)' and 'Repairing (Paused)' are new server statuses
  // introduced by the worker-construction rework.
  if (
    status === 'Building' ||
    status === 'Building (Paused)' ||
    status === 'Repairing' ||
    status === 'Repairing (Paused)'
  ) return 'repairing'
  if (moving) {
    if (carriedResource === 'gold') return 'carrying_gold'
    return 'walking'
  }
  // Worker carrying gold but standing still: use carrying_gold so the caller
  // (UnitAnimationController.sample) can pin frame 0 as the hold-pose idle,
  // instead of the empty-handed rotation.
  if (carriedResource === 'gold') return 'carrying_gold'
  // Flyers have no idle pose — a stationary flap reads as floating mid-air,
  // so reuse the walking animation when the unit is otherwise idle. The
  // Arch Mage isn't an actual flyer (still ground-targetable, renders at
  // ground level) but is visually hovering: its walking cycle has a gentle
  // bob, so reusing it as the idle pose sells the float in place of a
  // static stance. Paired with the lifted bounds in arch_mage.json.
  if (flyer || path === 'arch_mage') return 'walking'
  return 'idle'
}
