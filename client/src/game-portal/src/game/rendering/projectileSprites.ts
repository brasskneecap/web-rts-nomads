import type { ProjectileSnapshot } from '../network/protocol'
import {
  getProjectileSpriteSet,
  projectileImageReady,
  registeredProjectileSpriteIds,
} from './projectileSpriteSheets'

/**
 * Projectile sprite / draw registry.
 *
 * Each in-flight projectile carries a `variant` string (defaults to the
 * attacker's unit type on the server). The renderer calls
 * `drawProjectileForVariant` every frame with the projectile's resolved
 * world-space position + heading; the matching `ProjectileDrawFn` is
 * responsible for drawing the body.
 *
 * To add a new projectile visual:
 *   1. Implement a ProjectileDrawFn that draws at (0, 0) along the +x axis.
 *      The renderer handles translate/rotate/zoom before calling you.
 *   2. Register it in PROJECTILE_DRAW_REGISTRY under the variant key the
 *      server emits (e.g. "fire_arrow").
 *
 * Future work: replace the procedural fallback with an image-sprite draw fn
 * once art exists. The signature is stable — callers won't change.
 */

export type ProjectileDrawContext = {
  /** Current camera zoom — use for line widths / sprite scaling that should stay pixel-constant. */
  zoom: number
  /** The full snapshot — variants that need progress, ownerId, etc. read from here. */
  projectile: ProjectileSnapshot
}

export type ProjectileDrawFn = (ctx: CanvasRenderingContext2D, draw: ProjectileDrawContext) => void

// Natural arrow palette — intentionally NOT team-colored so arrows always
// read as "an arrow" regardless of who shot it. Swap these out per-variant
// if a future projectile wants a different material (e.g. fire arrows).
const ARROW_WOOD = '#8b5a2b'
const ARROW_WOOD_DARK = '#5c3a1c'
const ARROW_TIP = '#c9cdd4'
const ARROW_TIP_HIGHLIGHT = '#f1f3f6'
const ARROW_FLETCH = '#f5f3ec'
const ARROW_FLETCH_DARK = '#c7c2ad'

/**
 * Procedural fallback draw — a small arrow along the +x axis with a wooden
 * shaft, metallic tip, and pale fletching. Used whenever a variant has no
 * registered sprite draw fn.
 */
const drawDefaultProjectile: ProjectileDrawFn = (ctx, { zoom }) => {
  const length = 16
  const shaftEndX = length / 2 - 4 // where the metal head begins
  const tailX = -length / 2
  const headTipX = length / 2
  const headHalfHeight = 2.4
  const fletchLength = 5
  const fletchHalfHeight = 2.2

  ctx.lineJoin = 'round'
  ctx.lineCap = 'round'

  // Fletching: two small chevrons at the tail, drawn as filled triangles so
  // they read at low zoom. Back feather first (darker) for a bit of depth.
  ctx.fillStyle = ARROW_FLETCH_DARK
  ctx.beginPath()
  ctx.moveTo(tailX, 0)
  ctx.lineTo(tailX - fletchLength, -fletchHalfHeight)
  ctx.lineTo(tailX - fletchLength * 0.4, 0)
  ctx.lineTo(tailX - fletchLength, fletchHalfHeight)
  ctx.closePath()
  ctx.fill()

  ctx.fillStyle = ARROW_FLETCH
  ctx.beginPath()
  ctx.moveTo(tailX + 1, 0)
  ctx.lineTo(tailX - fletchLength + 1, -fletchHalfHeight * 0.75)
  ctx.lineTo(tailX - fletchLength * 0.4 + 1, 0)
  ctx.lineTo(tailX - fletchLength + 1, fletchHalfHeight * 0.75)
  ctx.closePath()
  ctx.fill()

  // Shaft: darker underside line for subtle shading, then the wood tone on top.
  ctx.strokeStyle = ARROW_WOOD_DARK
  ctx.lineWidth = 2.2 / zoom
  ctx.beginPath()
  ctx.moveTo(tailX, 0.6)
  ctx.lineTo(shaftEndX, 0.6)
  ctx.stroke()

  ctx.strokeStyle = ARROW_WOOD
  ctx.lineWidth = 1.6 / zoom
  ctx.beginPath()
  ctx.moveTo(tailX, 0)
  ctx.lineTo(shaftEndX, 0)
  ctx.stroke()

  // Metallic head: filled triangle with a brighter highlight stroke on the
  // top edge to suggest a bevel.
  ctx.fillStyle = ARROW_TIP
  ctx.beginPath()
  ctx.moveTo(headTipX, 0)
  ctx.lineTo(shaftEndX, -headHalfHeight)
  ctx.lineTo(shaftEndX, headHalfHeight)
  ctx.closePath()
  ctx.fill()

  ctx.strokeStyle = ARROW_TIP_HIGHLIGHT
  ctx.lineWidth = 0.75 / zoom
  ctx.beginPath()
  ctx.moveTo(headTipX, 0)
  ctx.lineTo(shaftEndX, -headHalfHeight)
  ctx.stroke()
}

/**
 * Registry mapping a projectile `variant` (unit type by default, or a
 * perk-override tag) to its draw fn. Unregistered variants fall back to the
 * default procedural arrow.
 */
export const PROJECTILE_DRAW_REGISTRY: Record<string, ProjectileDrawFn> = {}

// ── Sprite-backed projectiles ────────────────────────────────────────────────
//
// The renderer has already applied translate(x,y) + rotate(headingAngle)
// before calling the draw fn, so a sprite that points along +x in its art is
// oriented to the flight direction "for free". Each projectile is a single
// flat sprite.png (authored pointing +x / "east"); we just blit it centered
// and let that existing rotation do the work.

// TODO(tune, visual-only — no client test runner): world-pixel scale applied
// to a projectile sprite's native frame size. fire_bolt art is 48×48; 0.5 ≈
// 24px, comparable to the procedural arrow's ~16px length. Eyeball + adjust.
const PROJECTILE_SPRITE_SCALE = 0.5

// TODO(tune, visual-only): extra rotation (radians) applied if a sprite's
// authored "forward" is not +x (screen-right). sprite.png art is expected to
// point +x already, so 0 should be correct — bump by ±Math.PI/2 etc. only if
// it visually points the wrong way in-game.
const PROJECTILE_SPRITE_ANGLE_OFFSET = 0

// Fallback native frame size used until the image has decoded (naturalWidth
// reads 0 before then). Matches the historical fire_bolt art dimensions.
const PROJECTILE_SPRITE_FALLBACK_SIZE = 48

// Builds a ProjectileDrawFn that blits a loaded projectile sprite centered at
// the origin. Falls back to the procedural arrow until the image has decoded
// (or if the sprite is missing) so there is no blank/flicker frame. The draw
// size derives from the image's own pixel dimensions, so a new sprite.png of
// any size renders correctly without per-projectile config.
function makeSpriteProjectileDraw(spriteId: string): ProjectileDrawFn {
  return (ctx, drawCtx) => {
    const set = getProjectileSpriteSet(spriteId)
    if (!set || !projectileImageReady(set.image)) {
      drawDefaultProjectile(ctx, drawCtx)
      return
    }
    const img = set.image
    // Per-shot multiplier on top of the global base scale. The server
    // resolves it from the firing unit's projectileScale (unit def or its
    // promotion-path override) and sends it on the snapshot, so two units
    // sharing one projectile sprite can render it at different sizes.
    // Absent / <= 0 ⇒ 1× (every existing projectile unchanged).
    const shotScale = drawCtx.projectile.scale
    const scale =
      shotScale && shotScale > 0
        ? PROJECTILE_SPRITE_SCALE * shotScale
        : PROJECTILE_SPRITE_SCALE
    const w = (img.naturalWidth || PROJECTILE_SPRITE_FALLBACK_SIZE) * scale
    const h = (img.naturalHeight || PROJECTILE_SPRITE_FALLBACK_SIZE) * scale
    const prevSmoothing = ctx.imageSmoothingEnabled
    ctx.imageSmoothingEnabled = false // pixel art — keep crisp
    if (PROJECTILE_SPRITE_ANGLE_OFFSET !== 0) {
      ctx.rotate(PROJECTILE_SPRITE_ANGLE_OFFSET)
    }
    ctx.drawImage(img, -w / 2, -h / 2, w, h)
    ctx.imageSmoothingEnabled = prevSmoothing
  }
}

// Auto-register a sprite draw fn for every projectile that ships art, so a
// new ProjectileDef with a sprites.json "just works" without editing this
// file. A hand-written entry added to the registry later still wins (this
// only fills ids that aren't already registered).
for (const id of registeredProjectileSpriteIds()) {
  if (!PROJECTILE_DRAW_REGISTRY[id]) {
    PROJECTILE_DRAW_REGISTRY[id] = makeSpriteProjectileDraw(id)
  }
}

/**
 * Resolve + invoke the right draw fn for a projectile. The caller must have
 * already applied translate/rotate so that drawing along the +x axis at the
 * origin renders correctly in world space.
 */
export function drawProjectileForVariant(
  ctx: CanvasRenderingContext2D,
  drawCtx: ProjectileDrawContext,
) {
  const variant = drawCtx.projectile.variant
  const fn = (variant && PROJECTILE_DRAW_REGISTRY[variant]) || drawDefaultProjectile
  fn(ctx, drawCtx)
}
