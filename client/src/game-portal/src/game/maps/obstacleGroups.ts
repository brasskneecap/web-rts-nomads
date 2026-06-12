import type {
  JsonObject,
  ObstacleCapability,
  ObstacleTile,
  ObstacleType,
  ResourceType,
} from '../network/protocol'

// The map catalog (disk + /maps API) stores obstacles grouped by type: the
// shared metadata once, plus a list of [x, y] locations. The in-game wire and
// the editor's working model both use the flat ObstacleTile[] form, so these
// helpers convert at the catalog I/O boundary (see catalog.ts). Mirrors the
// server's groupObstacles / expandObstacleGroups (obstacle_groups.go).

export type ObstacleGroupWire = {
  width?: number
  height?: number
  capabilities?: ObstacleCapability[]
  resourceType?: ResourceType
  resourceAmount?: number
  maxHp?: number
  metadata?: JsonObject
  // [x, y] grid cells holding an obstacle of this type.
  locations: [number, number][]
}

export type ObstacleGroups = Record<string, ObstacleGroupWire>

// expandObstacleGroups turns the grouped wire form (or a legacy flat array, for
// back-compat) into the flat ObstacleTile[] the editor and renderer use. Ids
// are intentionally omitted — they're regenerated from coordinates downstream.
export function expandObstacleGroups(
  obstacles: ObstacleGroups | ObstacleTile[] | null | undefined,
): ObstacleTile[] {
  if (!obstacles) return []
  if (Array.isArray(obstacles)) return obstacles // legacy flat array
  const out: ObstacleTile[] = []
  for (const type of Object.keys(obstacles).sort()) {
    const group = obstacles[type]
    for (const [x, y] of group.locations ?? []) {
      out.push({
        x,
        y,
        obstacle: type as ObstacleType,
        ...(group.width !== undefined ? { width: group.width } : {}),
        ...(group.height !== undefined ? { height: group.height } : {}),
        ...(group.capabilities ? { capabilities: [...group.capabilities] } : {}),
        ...(group.resourceType !== undefined ? { resourceType: group.resourceType } : {}),
        ...(group.resourceAmount !== undefined ? { resourceAmount: group.resourceAmount } : {}),
        ...(group.maxHp !== undefined ? { maxHp: group.maxHp } : {}),
        ...(group.metadata ? { metadata: { ...group.metadata } } : {}),
      })
    }
  }
  return out
}

// groupObstacles collapses flat tiles into per-type groups for the catalog wire
// format. Metadata is taken from the first tile of each type (all share it);
// per-tile id and runtime hp are dropped. Adding/removing an obstacle in the
// editor pushes/removes an entry in the resulting `locations` array.
export function groupObstacles(tiles: ObstacleTile[]): ObstacleGroups {
  const groups: ObstacleGroups = {}
  for (const tile of tiles) {
    let group = groups[tile.obstacle]
    if (!group) {
      group = {
        ...(tile.width !== undefined ? { width: tile.width } : {}),
        ...(tile.height !== undefined ? { height: tile.height } : {}),
        ...(tile.capabilities?.length ? { capabilities: [...tile.capabilities] } : {}),
        ...(tile.resourceType !== undefined ? { resourceType: tile.resourceType } : {}),
        ...(tile.resourceAmount !== undefined ? { resourceAmount: tile.resourceAmount } : {}),
        ...(tile.maxHp !== undefined ? { maxHp: tile.maxHp } : {}),
        ...(tile.metadata ? { metadata: { ...tile.metadata } } : {}),
        locations: [],
      }
      groups[tile.obstacle] = group
    }
    group.locations.push([tile.x, tile.y])
  }
  return groups
}
