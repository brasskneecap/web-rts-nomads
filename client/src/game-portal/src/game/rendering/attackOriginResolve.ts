// resolveProjectileOriginLift picks the screen-space origin offset for a
// projectile: the authored attackOrigin lift when present, otherwise the
// caller-supplied geometric fallback (spriteBodyCenterLift / attackVisual —
// exactly today's behavior). Authored wins; null ⇒ unchanged. Pure so it's
// testable without a canvas.
//
// Also reused (as-is, same name) by CanvasRenderer's channel-beam origin:
// the authored attackOrigin block is an ABSOLUTE offset regardless of which
// visual effect reads it, so it must REPLACE the geometric lift there too,
// not add to it — same precedence, same function, one shared/tested
// implementation instead of a second copy that could drift.
export function resolveProjectileOriginLift(
  authored: { x: number; y: number } | null,
  geometricFallback: { x: number; y: number },
): { x: number; y: number } {
  return authored ?? geometricFallback
}
