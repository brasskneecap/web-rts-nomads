// PreviewOverlays: pure, DOM-free screen-space geometry for the ability
// preview replay's range/AoE ring overlays (AbilityPreviewCanvas.vue's
// second, transparent canvas layered over the renderer's own).
//
// Projection MUST match CanvasRenderer's own world->screen transform exactly
// or the rings won't line up with the sprites drawn underneath. CanvasRenderer
// establishes its camera transform once per frame via
// `ctx.scale(camera.zoom, camera.zoom); ctx.translate(-camera.x, -camera.y)`
// (see CanvasRenderer.ts's render()), which is algebraically identical to
// Camera.worldToScreen(): `screenX = (worldX - camera.x) * zoom`. This module
// replicates that formula directly (rather than constructing a throwaway
// Camera instance) so it stays a plain function of primitive inputs.
// A world-space radius scales by `zoom` alone (no translation term).

export interface OverlayCircle {
  cx: number
  cy: number
  radius: number
}

export interface OverlayInput {
  /** World-space cast range radius, centered on the caster. Absent/0 -> no ring. */
  castRange?: number
  /** World-space AoE radius, centered on the cast/impact point. Absent/0 -> no disc. */
  aoeRadius?: number
  casterX: number
  casterY: number
  castX: number
  castY: number
  showCastRange: boolean
  showAoe: boolean
}

export interface OverlayCamera {
  x: number
  y: number
  zoom: number
}

// Exported (not just used internally for the range/AoE rings above) so the
// preview canvas's drag-to-place hit-testing (Phase 6b) can project a scene
// unit's world position to screen space with the exact same formula the
// rings use, guaranteeing a dragged unit's hitbox always lines up with
// whatever the renderer/overlays actually painted at that world point.
export function worldToScreen(worldX: number, worldY: number, cam: OverlayCamera) {
  return {
    x: (worldX - cam.x) * cam.zoom,
    y: (worldY - cam.y) * cam.zoom,
  }
}

// screenToWorld is the EXACT algebraic inverse of worldToScreen above —
// solve `screenX = (worldX - cam.x) * cam.zoom` for worldX. Used by the
// preview canvas's drag handlers to convert a pointer event's screen
// position into the world position a dragged unit/caster should move to.
export function screenToWorld(screenX: number, screenY: number, cam: OverlayCamera) {
  return {
    x: screenX / cam.zoom + cam.x,
    y: screenY / cam.zoom + cam.y,
  }
}

// _canvasW/_canvasH are accepted for interface symmetry with the caller
// (and because a future overlay primitive may need to clip/center against
// the viewport) but are unused by the current circle projection, which is a
// direct world->screen point + radius scale with no viewport-relative term.
// Underscore-prefixed so `noUnusedParameters` doesn't flag them.
export function overlayCircles(
  input: OverlayInput,
  cam: OverlayCamera,
  _canvasW: number,
  _canvasH: number,
): { castRange: OverlayCircle | null; aoe: OverlayCircle | null } {
  let castRange: OverlayCircle | null = null
  if (input.showCastRange && input.castRange) {
    const p = worldToScreen(input.casterX, input.casterY, cam)
    castRange = { cx: p.x, cy: p.y, radius: input.castRange * cam.zoom }
  }

  let aoe: OverlayCircle | null = null
  if (input.showAoe && input.aoeRadius) {
    const p = worldToScreen(input.castX, input.castY, cam)
    aoe = { cx: p.x, cy: p.y, radius: input.aoeRadius * cam.zoom }
  }

  return { castRange, aoe }
}
