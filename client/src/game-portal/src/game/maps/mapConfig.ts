import type {
  BuildingTile,
  BuildingType,
  JsonObject,
  MapConfig,
  NeutralSpawn,
  ObstacleTile,
  ObstacleType,
  PlacedUnit,
  TerrainTile,
  TerrainType,
  TileInstance,
} from '../network/protocol'
import { BUILDING_DEF_MAP } from './buildingDefs'
import { OBSTACLE_DEF_MAP } from './obstacleDefs'
import type { ObstacleCapability, ResourceType } from '../network/protocol'

export const DEFAULT_CELL_SIZE = 64
export const DEFAULT_GRASS_COLOR = '#365b2c'

export const MAP_EDITOR_PRESETS = [
  { label: 'Small', cols: 48, rows: 32 },
  { label: 'Medium', cols: 64, rows: 48 },
  { label: 'Large', cols: 96, rows: 64 },
] as const

export function createEditorMapConfig(
  cols = 24,
  rows = 18,
  existing?: Partial<MapConfig>,
): MapConfig {
  const cellSize = existing?.cellSize ?? DEFAULT_CELL_SIZE
  const safeCols = clampGridDimension(cols)
  const safeRows = clampGridDimension(rows)

  return sanitizeMapConfig({
    id: existing?.id ?? 'editor-draft',
    name: existing?.name ?? 'Editor Draft',
    description: existing?.description ?? '',
    width: safeCols * cellSize,
    height: safeRows * cellSize,
    gridCols: safeCols,
    gridRows: safeRows,
    cellSize,
    terrain: existing?.terrain ?? [],
    tiles: existing?.tiles ?? [],
    defaultTile: existing?.defaultTile,
    obstacles: existing?.obstacles ?? [],
    buildings: existing?.buildings ?? [],
    placedUnits: existing?.placedUnits,
    neutralSpawns: existing?.neutralSpawns,
    waveConfig: existing?.waveConfig,
    campaign: existing?.campaign,
  })
}

export function sanitizeMapConfig(map: MapConfig): MapConfig {
  const cellSize = clampCellSize(map.cellSize)
  const gridCols = clampGridDimension(map.gridCols)
  const gridRows = clampGridDimension(map.gridRows)

  return {
    id: map.id,
    name: map.name,
    description: map.description ?? '',
    cellSize,
    gridCols,
    gridRows,
    width: gridCols * cellSize,
    height: gridRows * cellSize,
    terrain: dedupeTerrainTiles(map.terrain ?? [], gridCols, gridRows),
    tiles: dedupeTiles(map.tiles ?? [], gridCols, gridRows),
    defaultTile: map.defaultTile,
    obstacles: dedupeObstacleTiles(map.obstacles ?? [], gridCols, gridRows),
    buildings: dedupeBuildings(map.buildings ?? [], gridCols, gridRows),
    waveConfig: map.waveConfig,
    debug: map.debug,
    placedUnits: clampPlacedUnits(map.placedUnits ?? [], gridCols, gridRows),
    neutralSpawns: clampNeutralSpawns(map.neutralSpawns ?? [], gridCols, gridRows),
    campaign: map.campaign,
  }
}

export function setTilePaint(
  map: MapConfig,
  x: number,
  y: number,
  tile: Omit<TileInstance, 'x' | 'y'> | null,
): MapConfig {
  const nextTiles = (map.tiles ?? []).filter((t) => t.x !== x || t.y !== y)

  if (tile) {
    nextTiles.push({ x, y, ...tile })
  }

  return sanitizeMapConfig({
    ...map,
    tiles: nextTiles,
  })
}

export function resizeMapConfig(map: MapConfig, cols: number, rows: number): MapConfig {
  return sanitizeMapConfig({
    ...map,
    gridCols: cols,
    gridRows: rows,
  })
}

export function setTerrainTile(
  map: MapConfig,
  x: number,
  y: number,
  terrain: TerrainType | null,
): MapConfig {
  const nextTerrain = map.terrain.filter((tile) => tile.x !== x || tile.y !== y)

  if (terrain) {
    nextTerrain.push({ x, y, terrain })
  }

  return sanitizeMapConfig({
    ...map,
    terrain: nextTerrain,
  })
}

export function setObstacleTile(
  map: MapConfig,
  x: number,
  y: number,
  obstacle: ObstacleType | null,
): MapConfig {
  const nextObstacles = map.obstacles.filter((tile) => tile.x !== x || tile.y !== y)

  if (obstacle) {
    nextObstacles.push(createObstacleTile(obstacle, x, y))
  }

  return sanitizeMapConfig({
    ...map,
    obstacles: nextObstacles,
  })
}

// Catalog-driven obstacle construction: pulls footprint, capabilities, and
// resource values from the active obstacle def so the editor's output matches
// what the server would apply at runtime. Mirrors createBuildingTile.
function createObstacleTile(obstacle: ObstacleType, x: number, y: number): ObstacleTile {
  const def = OBSTACLE_DEF_MAP.get(obstacle)
  const width = def?.width ?? 1
  const height = def?.height ?? 1
  const maxHp = def?.maxHp ?? 0
  const capabilities = def?.capabilities
    ? ([...def.capabilities] as ObstacleCapability[])
    : undefined
  const resourceFields =
    def?.resourceType && def.resourceAmount !== undefined
      ? {
          resourceType: def.resourceType as ResourceType,
          resourceAmount: def.resourceAmount,
        }
      : {}

  return {
    x,
    y,
    obstacle,
    width,
    height,
    ...(capabilities && capabilities.length > 0 ? { capabilities } : {}),
    ...resourceFields,
    ...(maxHp > 0 ? { maxHp, hp: maxHp } : {}),
  }
}

export function setBuildingTile(
  map: MapConfig,
  x: number,
  y: number,
  buildingType: BuildingType | null,
  metadata?: JsonObject,
): MapConfig {
  const nextBuildings = map.buildings.filter(
    (building) => !doesBuildingCoverCell(building, x, y),
  )

  if (buildingType) {
    const tile = createBuildingTile(buildingType, x, y)
    if (metadata) {
      tile.metadata = { ...(tile.metadata ?? {}), ...metadata }
    }
    nextBuildings.push(tile)
  }

  return sanitizeMapConfig({
    ...map,
    buildings: nextBuildings,
  })
}

export function getBuildingColor(
  buildingType: BuildingType,
  occupied = true,
  ownerColor?: string | null,
): string {
  const def = BUILDING_DEF_MAP.get(buildingType)
  if (def) {
    return occupied ? (ownerColor ?? def.color) : '#64748b'
  }
  switch (buildingType) {
    case 'goldmine':
      return '#ca8a04'
    case 'enemy-spawnpoint':
      return '#991b1b'
    case 'spawn-point':
      return '#0ea5e9'
    default:
      return occupied ? (ownerColor ?? '#64748b') : '#64748b'
  }
}

export function getTerrainColor(terrain: TerrainType): string {
  switch (terrain) {
    case 'dirt':
      return '#6b4f34'
    case 'grass':
      return '#3e7d2a'
  }
}

export function getObstacleColor(obstacle: ObstacleType): string {
  switch (obstacle) {
    case 'wall':
      return '#9ca3af'
    case 'tree':
      return '#27543c'
    default:
      return '#7c4a2f'
  }
}

// Minimap POI color for a neutral camp by tier. Tiers >= 3 saturate at dark
// red so the palette keeps reading as a "low / mid / high danger" scale even
// when more tier files are added later. Tier <= 0 is defensive and falls
// back to tier-1 green.
export function getNeutralSpawnTierColor(tier: number): string {
  if (tier <= 1) return '#006400' // dark green
  if (tier === 2) return '#b8860b' // dark goldenrod (dark yellow)
  return '#8b0000' // dark red (tier 3+)
}

function dedupeTerrainTiles(
  tiles: TerrainTile[],
  gridCols: number,
  gridRows: number,
): TerrainTile[] {
  const unique = new Map<string, TerrainTile>()

  for (const tile of tiles) {
    if (!isWithinGrid(tile.x, tile.y, gridCols, gridRows)) continue
    unique.set(`${tile.x}:${tile.y}`, {
      x: tile.x,
      y: tile.y,
      terrain: tile.terrain,
    })
  }

  return Array.from(unique.values()).sort(sortTiles)
}

function dedupeTiles(
  tiles: TileInstance[],
  gridCols: number,
  gridRows: number,
): TileInstance[] {
  const unique = new Map<string, TileInstance>()

  for (const tile of tiles) {
    if (!isWithinGrid(tile.x, tile.y, gridCols, gridRows)) continue
    unique.set(`${tile.x}:${tile.y}`, {
      x: tile.x,
      y: tile.y,
      sheet: tile.sheet,
      sx: tile.sx,
      sy: tile.sy,
    })
  }

  return Array.from(unique.values()).sort(sortTiles)
}

function dedupeObstacleTiles(
  tiles: ObstacleTile[],
  gridCols: number,
  gridRows: number,
): ObstacleTile[] {
  const unique = new Map<string, ObstacleTile>()

  for (const tile of tiles) {
    if (!isWithinGrid(tile.x, tile.y, gridCols, gridRows)) continue
    const normalized: ObstacleTile = {
      x: tile.x,
      y: tile.y,
      obstacle: tile.obstacle,
    }
    if (tile.id) normalized.id = tile.id
    if (tile.width) normalized.width = tile.width
    if (tile.height) normalized.height = tile.height
    if (tile.capabilities?.length) normalized.capabilities = [...tile.capabilities]
    if (tile.resourceType) normalized.resourceType = tile.resourceType
    if (tile.resourceAmount !== undefined) normalized.resourceAmount = tile.resourceAmount
    if (tile.hp !== undefined) normalized.hp = tile.hp
    if (tile.maxHp !== undefined) normalized.maxHp = tile.maxHp
    if (tile.metadata) normalized.metadata = { ...tile.metadata }
    unique.set(`${tile.x}:${tile.y}`, normalized)
  }

  return Array.from(unique.values()).sort(sortTiles)
}

function dedupeBuildings(
  buildings: BuildingTile[],
  gridCols: number,
  gridRows: number,
): BuildingTile[] {
  const normalized: BuildingTile[] = []
  const occupiedCells = new Set<string>()

  for (const building of buildings) {
    const nextBuilding = normalizeBuildingTile(building)

    if (!isWithinGrid(nextBuilding.x, nextBuilding.y, gridCols, gridRows)) continue
    if (nextBuilding.x + nextBuilding.width > gridCols) continue
    if (nextBuilding.y + nextBuilding.height > gridRows) continue

    const cells = getBuildingCells(nextBuilding)
    if (cells.some((cell) => occupiedCells.has(cell))) continue

    for (const cell of cells) {
      occupiedCells.add(cell)
    }

    normalized.push(nextBuilding)
  }

  return normalized.sort(sortTiles)
}

function sortTiles(a: { x: number; y: number }, b: { x: number; y: number }) {
  if (a.y !== b.y) {
    return a.y - b.y
  }

  return a.x - b.x
}

function isWithinGrid(x: number, y: number, gridCols: number, gridRows: number) {
  return x >= 0 && x < gridCols && y >= 0 && y < gridRows
}

function clampGridDimension(value: number) {
  return Math.max(6, Math.min(Math.round(value), 500))
}

function clampCellSize(value: number) {
  return Math.max(16, Math.min(Math.round(value), 128))
}

function createBuildingTile(buildingType: BuildingType, x: number, y: number): BuildingTile {
  if (buildingType === 'spawn-point') {
    return {
      id: `spawn-point-${x}-${y}`,
      buildingType,
      x,
      y,
      width: 1,
      height: 1,
      occupied: false,
      visible: false,
      ownerId: null,
      capabilities: [],
      metadata: {
        townhallId: null,
        fillOrder: 0,
      },
    }
  }

  // Catalog-driven path: derive dimensions, capabilities, spawn types, and
  // resource fields from the live building def. Neutral/enemy defs default to
  // existing+visible on the map (they're not player-constructed).
  const def = BUILDING_DEF_MAP.get(buildingType)
  const buildingClass = def?.class ?? 'player'
  const isWorldPlaced = buildingClass !== 'player'
  const capabilities = def ? [...def.capabilities] : []
  const width = def?.width ?? 1
  const height = def?.height ?? 1
  const spawnUnitTypes = def?.spawnUnitTypes?.length ? [...def.spawnUnitTypes] : undefined
  const metadata = def ? { ...def.metadata } : {}
  const resourceFields =
    def?.resourceType && def.resourceAmount !== undefined
      ? { resourceType: def.resourceType, resourceAmount: def.resourceAmount }
      : {}

  return {
    id: `${buildingType}-${x}-${y}`,
    buildingType,
    x,
    y,
    width,
    height,
    occupied: isWorldPlaced,
    visible: isWorldPlaced,
    ownerId: null,
    capabilities,
    ...(spawnUnitTypes ? { spawnUnitTypes } : {}),
    ...resourceFields,
    metadata,
  }
}

function normalizeBuildingTile(building: BuildingTile): BuildingTile {
  const base = createBuildingTile(building.buildingType, building.x, building.y)

  return {
    ...base,
    ...building,
    width: Math.max(1, Math.round(building.width ?? base.width)),
    height: Math.max(1, Math.round(building.height ?? base.height)),
    occupied: building.occupied ?? base.occupied,
    visible: building.visible ?? base.visible,
    capabilities: building.capabilities?.length
      ? Array.from(new Set(building.capabilities))
      : [...base.capabilities],
    metadata: {
      ...(base.metadata ?? {}),
      ...(building.metadata ?? {}),
    },
  }
}

function clampPlacedUnits(
  units: PlacedUnit[],
  gridCols: number,
  gridRows: number,
): PlacedUnit[] {
  return units.filter((u) => isWithinGrid(u.x, u.y, gridCols, gridRows))
}

function clampNeutralSpawns(
  spawns: NeutralSpawn[],
  gridCols: number,
  gridRows: number,
): NeutralSpawn[] | undefined {
  const filtered = spawns.filter((s) => isWithinGrid(s.x, s.y, gridCols, gridRows))
  return filtered.length > 0 ? filtered : undefined
}

function doesBuildingCoverCell(building: BuildingTile, x: number, y: number) {
  return (
    x >= building.x &&
    x < building.x + building.width &&
    y >= building.y &&
    y < building.y + building.height
  )
}

function getBuildingCells(building: BuildingTile) {
  const cells: string[] = []

  for (let cellY = building.y; cellY < building.y + building.height; cellY++) {
    for (let cellX = building.x; cellX < building.x + building.width; cellX++) {
      cells.push(`${cellX}:${cellY}`)
    }
  }

  return cells
}
