import type {
  BuildingCapability,
  BuildingTile,
  BuildingType,
  MapConfig,
  ObstacleTile,
  ObstacleType,
  TerrainTile,
  TerrainType,
} from '../network/protocol'
import { BUILDING_DEF_MAP } from './buildingDefs'

export const DEFAULT_CELL_SIZE = 64
export const DEFAULT_GRASS_COLOR = '#365b2c'

const BUILDING_CAPABILITY_SETS: Record<BuildingType, BuildingCapability[]> = {
  goldmine: ['resource-source'],
  townhall: ['unit-spawner', 'occupiable', 'deposit-point'],
  tree: ['resource-source'],
  barracks: ['unit-spawner'],
  farm: [],
  'enemy-spawnpoint': ['enemy-spawner'],
}

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
    obstacles: existing?.obstacles ?? [],
    buildings: existing?.buildings ?? [],
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
    obstacles: dedupeObstacleTiles(map.obstacles ?? [], gridCols, gridRows),
    buildings: dedupeBuildings(map.buildings ?? [], gridCols, gridRows),
  }
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
    nextObstacles.push({ x, y, obstacle })
  }

  return sanitizeMapConfig({
    ...map,
    obstacles: nextObstacles,
  })
}

export function setBuildingTile(
  map: MapConfig,
  x: number,
  y: number,
  buildingType: BuildingType | null,
  metadata?: Record<string, string | number | boolean | null>,
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
    case 'tree':
      return '#2d6a4f'
    case 'enemy-spawnpoint':
      return '#991b1b'
  }
}

export function getTerrainColor(terrain: TerrainType): string {
  switch (terrain) {
    case 'dirt':
      return '#6b4f34'
    case 'water':
      return '#1f4f78'
    case 'forest':
      return '#1d5a34'
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

function dedupeObstacleTiles(
  tiles: ObstacleTile[],
  gridCols: number,
  gridRows: number,
): ObstacleTile[] {
  const unique = new Map<string, ObstacleTile>()

  for (const tile of tiles) {
    if (!isWithinGrid(tile.x, tile.y, gridCols, gridRows)) continue
    unique.set(`${tile.x}:${tile.y}`, {
      x: tile.x,
      y: tile.y,
      obstacle: tile.obstacle,
    })
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
  if (buildingType === 'goldmine') {
    return {
      id: `goldmine-${x}-${y}`,
      buildingType,
      x,
      y,
      width: 2,
      height: 2,
      occupied: true,
      visible: true,
      ownerId: null,
      capabilities: [...BUILDING_CAPABILITY_SETS[buildingType]],
      resourceType: 'gold',
      resourceAmount: 15000,
      metadata: {
        gatherRate: 10,
      },
    }
  }

  if (buildingType === 'tree') {
    return {
      id: `tree-${x}-${y}`,
      buildingType,
      x,
      y,
      width: 1,
      height: 1,
      occupied: true,
      visible: true,
      ownerId: null,
      capabilities: [...BUILDING_CAPABILITY_SETS[buildingType]],
      resourceType: 'wood',
      resourceAmount: 1000,
      metadata: {},
    }
  }

  if (buildingType === 'barracks') {
    return {
      id: `barracks-${x}-${y}`,
      buildingType,
      x,
      y,
      width: 2,
      height: 2,
      occupied: false,
      visible: false,
      ownerId: null,
      capabilities: [...BUILDING_CAPABILITY_SETS[buildingType]],
      spawnUnitTypes: ['soldier'],
      metadata: {
        spawnTimeSoldier: 10,
      },
    }
  }

  if (buildingType === 'enemy-spawnpoint') {
    return {
      id: `enemy-spawnpoint-${x}-${y}`,
      buildingType,
      x,
      y,
      width: 2,
      height: 2,
      occupied: true,
      visible: true,
      ownerId: null,
      capabilities: [...BUILDING_CAPABILITY_SETS[buildingType]],
      metadata: {
        spawnDelaySeconds: 60,
        spawnIntervalSeconds: 10,
      },
    }
  }

  return {
    id: `townhall-${x}-${y}`,
    buildingType,
    x,
    y,
    width: 3,
    height: 3,
    occupied: false,
    visible: false,
    ownerId: null,
    capabilities: [...BUILDING_CAPABILITY_SETS[buildingType]],
    spawnUnitTypes: ['worker'],
    metadata: {
      occupiedLabel: 'occupied',
      unoccupiedLabel: 'unoccupied',
      spawnTimeWorker: 5,
    },
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
