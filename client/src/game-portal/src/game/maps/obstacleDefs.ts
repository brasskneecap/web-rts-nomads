import type { ObstacleType } from '../network/protocol'

// ObstacleRenderDef overrides where and how large an obstacle sprite is
// drawn relative to its grid footprint. All four fields are expressed in
// *cell units* (not pixels), so zoom and cellSize stay decoupled. The grid
// footprint used by pathing and selection hit-testing is unaffected — this
// only changes the visual bounding box.
//
// Zero / missing fields mean "use default" (offset=0, width/height = the
// obstacle's grid footprint).
export type ObstacleRenderDef = {
  offsetX?: number
  offsetY?: number
  width?: number
  height?: number
}

// ObstacleSelectionRingDef nudges where the yellow selection/hover ring
// sits relative to the obstacle's grid footprint. All fields are in cell
// units. Defaults: centered horizontally, near the footprint bottom, with
// radii derived from the footprint width. Mirrors the Go-side struct.
export type ObstacleSelectionRingDef = {
  offsetX?: number
  offsetY?: number
  radiusX?: number
  radiusY?: number
}

// ObstacleDef mirrors server/internal/game/obstacle_defs.go:ObstacleDef.
// Only the fields the client actually reads are typed here; anything else
// the server sends is ignored by TypeScript.
export type ObstacleDef = {
  type: ObstacleType
  width?: number
  height?: number
  maxHp?: number
  selectable?: boolean
  blocksPathing?: boolean
  resourceType?: string
  resourceAmount?: number
  capabilities?: string[]
  color?: string
  label?: string
  render?: ObstacleRenderDef
  selectionRing?: ObstacleSelectionRingDef
}

export let OBSTACLE_DEFS: ObstacleDef[] = []
export let OBSTACLE_DEF_MAP = new Map<string, ObstacleDef>()

export function initObstacleDefs(defs: ObstacleDef[]): void {
  OBSTACLE_DEFS = defs
  OBSTACLE_DEF_MAP = new Map(defs.map((def) => [def.type, def]))
}
