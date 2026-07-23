import type {
  MapCatalogEntry,
  MapCatalogFile,
  MapCatalogMapPayload,
  NeutralGroupTierSummary,
  ObstacleTile,
  TerrainTile,
  TileInstance,
} from '../network/protocol'
import { registerUploadedIcons } from '../rendering/itemAssets'
import {
  expandObstacleGroups,
  groupObstacles,
  type ObstacleGroups,
} from './obstacleGroups'
import {
  expandTerrainGroups,
  expandTileGroups,
  groupTerrain,
  groupTiles,
  type TerrainGroups,
  type TileGroupWire,
} from './terrainTileGroups'
import type { BuildingDef, BuildingStyleRenderDef } from './buildingDefs'
import type { ObstacleDef } from './obstacleDefs'
import type { UnitAttackOrigin, UnitBounds, UnitDef, UnitShadow } from './unitDefs'
import type { ActionIconDef } from './actionIconDefs'
import type { PerkDef } from './perkDefs'
import type { ItemDef } from './itemDefs'
import type { ListDef } from './listDefs'
import type { TableDef } from './tableDefs'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

export async function fetchMapCatalog(): Promise<MapCatalogEntry[]> {
  const response = await fetch(`${API_BASE}/maps`)

  if (!response.ok) {
    throw new Error(`Failed to load maps: ${response.status}`)
  }

  const maps = (await response.json()) as MapCatalogEntry[]
  return maps
}

// MapCatalogFileWire is the on-the-wire shape of a map catalog file: identical
// to MapCatalogFile except its obstacles are grouped by type (or, for older
// maps, a legacy flat array). fetchMapCatalogFile / saveMapCatalogFile convert
// between this and the editor's flat MapCatalogFile so the editor and renderer
// only ever deal with ObstacleTile[].
type MapCatalogFileWire = Omit<MapCatalogFile, 'map'> & {
  map: Omit<MapCatalogMapPayload, 'obstacles' | 'terrain' | 'tiles'> & {
    obstacles?: ObstacleGroups | ObstacleTile[]
    terrain?: TerrainGroups | TerrainTile[]
    tiles?: TileGroupWire[] | TileInstance[]
  }
}

export async function fetchMapCatalogFile(mapId: string): Promise<MapCatalogFile> {
  const response = await fetch(`${API_BASE}/maps/${encodeURIComponent(mapId)}`)

  if (!response.ok) {
    throw new Error(`Failed to load map ${mapId}: ${response.status}`)
  }

  const raw = (await response.json()) as MapCatalogFileWire
  return {
    ...raw,
    map: {
      ...raw.map,
      terrain: expandTerrainGroups(raw.map.terrain),
      tiles: expandTileGroups(raw.map.tiles),
      obstacles: expandObstacleGroups(raw.map.obstacles),
    },
  }
}

// LevelConflict describes a campaign (campaignId, levelId) already owned by a
// different map — returned by the server (409) when a campaign-tagged save
// collides with an existing level and the caller didn't opt into reassignment.
export type LevelConflict = {
  campaignId: string
  levelId: string
  ownerMapId: string
  ownerMapName: string
}

// LevelConflictError is thrown by saveMapCatalogFile on a 409 so the editor can
// detect the collision and offer to reassign the level to the new map.
export class LevelConflictError extends Error {
  readonly conflict: LevelConflict
  constructor(conflict: LevelConflict) {
    super(
      `Level "${conflict.levelId}" is already owned by map "${conflict.ownerMapName}"`,
    )
    this.name = 'LevelConflictError'
    this.conflict = conflict
  }
}

// saveMapCatalogFile POSTs a map. With opts.reassignLevel the server will
// resolve a campaign level-ownership conflict by clearing the previous owner's
// campaign block instead of rejecting. On a 409 (conflict, not reassigning) it
// throws LevelConflictError carrying the conflict details.
export async function saveMapCatalogFile(
  entry: MapCatalogFile,
  opts: { reassignLevel?: boolean } = {},
): Promise<void> {
  const query = opts.reassignLevel ? '?reassignLevel=true' : ''
  // Convert the editor's flat terrain / tiles / obstacles to the grouped catalog
  // format. Painting / erasing a cell in the editor adds / removes its [x, y]
  // entry in the per-kind coordinate / `locations` array produced here.
  const wire: MapCatalogFileWire = {
    ...entry,
    map: {
      ...entry.map,
      terrain: groupTerrain(entry.map.terrain ?? []),
      tiles: groupTiles(entry.map.tiles ?? []),
      obstacles: groupObstacles(entry.map.obstacles ?? []),
    },
  }
  const response = await fetch(`${API_BASE}/maps${query}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(wire),
  })
  if (response.status === 409) {
    const body = (await response.json().catch(() => null)) as
      | { conflict?: LevelConflict }
      | null
    if (body?.conflict) {
      throw new LevelConflictError(body.conflict)
    }
    throw new Error('Level conflict')
  }
  if (!response.ok) {
    const text = await response.text().catch(() => response.statusText)
    throw new Error(text || `Server error ${response.status}`)
  }
}

export async function fetchBuildingDefs(): Promise<{
  buildings: BuildingDef[]
  buildingStyles: Record<string, Record<string, BuildingStyleRenderDef>>
}> {
  const response = await fetch(`${API_BASE}/catalog/buildings`)

  if (!response.ok) {
    throw new Error(`Failed to load building defs: ${response.status}`)
  }

  const data = (await response.json()) as {
    buildings: BuildingDef[]
    buildingStyles?: Record<string, Record<string, BuildingStyleRenderDef>>
  }
  return { buildings: data.buildings, buildingStyles: data.buildingStyles ?? {} }
}

export async function fetchObstacleDefs(): Promise<ObstacleDef[]> {
  const response = await fetch(`${API_BASE}/catalog/obstacles`)

  if (!response.ok) {
    throw new Error(`Failed to load obstacle defs: ${response.status}`)
  }

  const data = (await response.json()) as { obstacles: ObstacleDef[] }
  return data.obstacles
}

// `attackOrigin` is independently optional server-side (a path authoring only
// bounds, or only attackOrigin, still appears once in the union'd
// /catalog/units `paths` list — see the server's ListPathBounds doc comment).
// `bounds` keeps its existing (pre-this-change) type here — initPathBounds's
// existing null-tolerant `if (pathBounds)` check in getUnitBoundsFor already
// handles a bounds-less entry correctly at runtime; widening its static type
// is out of scope for this task.
export type PathBoundsEntry = { path: string; bounds: UnitBounds; attackOrigin?: UnitAttackOrigin | null; shadow?: UnitShadow | null }

export async function fetchUnitDefs(): Promise<{
  units: UnitDef[]
  paths: PathBoundsEntry[]
  pathsByUnit: Record<string, string[]>
}> {
  const response = await fetch(`${API_BASE}/catalog/units`)

  if (!response.ok) {
    throw new Error(`Failed to load unit defs: ${response.status}`)
  }

  const data = (await response.json()) as {
    units: UnitDef[]
    paths?: PathBoundsEntry[]
    pathsByUnit?: Record<string, string[]>
  }
  return {
    units: data.units,
    paths: data.paths ?? [],
    pathsByUnit: data.pathsByUnit ?? {},
  }
}

export async function fetchPerkDefs(): Promise<PerkDef[]> {
  const response = await fetch(`${API_BASE}/catalog/perks`)

  if (!response.ok) {
    throw new Error(`Failed to load perk defs: ${response.status}`)
  }

  const data = (await response.json()) as { perks: PerkDef[] }
  return data.perks
}

// fetchPathPerkRanks loads just the perksByRank bucket from every promotion
// path (GET /catalog/paths — the same read-only route the path editor uses).
// Gameplay needs this to resolve which rank cell a unit's owned perk belongs
// in (see PERK_RANK_BY_ID_MAP / initPerkRanksFromPaths in maps/perkDefs.ts),
// now that PerkDef carries no innate rank of its own. Only perksByRank is
// pulled out of each path's def — the rest of the authored path shape is the
// editor's concern (pathEditorApi.ts's fetchPaths), not gameplay's.
export async function fetchPathPerkRanks(): Promise<Array<Record<string, string[]> | undefined>> {
  const response = await fetch(`${API_BASE}/catalog/paths`)

  if (!response.ok) {
    throw new Error(`Failed to load path defs: ${response.status}`)
  }

  const data = (await response.json()) as { paths: Array<{ def?: { perksByRank?: Record<string, string[]> } }> }
  return data.paths.map((p) => p.def?.perksByRank)
}

export async function fetchActionIcons(): Promise<ActionIconDef[]> {
  const response = await fetch(`${API_BASE}/catalog/action-icons`)

  if (!response.ok) {
    throw new Error(`Failed to load action icons: ${response.status}`)
  }

  const data = (await response.json()) as { icons: ActionIconDef[] }
  return data.icons
}

export async function fetchItemDefs(): Promise<ItemDef[]> {
  const response = await fetch(`${API_BASE}/catalog/items`)

  if (!response.ok) {
    throw new Error(`Failed to load item defs: ${response.status}`)
  }

  const data = (await response.json()) as { items: ItemDef[] }
  // Teach the asset layer which items have an author-uploaded icon, so it can
  // serve that instead of the bundled art. Done here rather than at each call
  // site: every surface that shows item icons loads the catalog through this
  // function, so an upload becomes visible everywhere after one refresh.
  registerUploadedIcons(data.items)
  return data.items
}

// There is no fetchRecipeDefs: an item IS its own recipe (ItemDef.crafting), so
// the item catalog already carries every recipe.

// Lists (catalog/lists/<id>.json) — one route, because there is one kind of list.
// Replaces the old /catalog/item-lists + /catalog/recipe-lists pair; what a list
// MEANS is decided by the building that consumes it, not by the list.
export async function fetchLists(): Promise<ListDef[]> {
  const response = await fetch(`${API_BASE}/catalog/lists`)

  if (!response.ok) {
    throw new Error(`Failed to load lists: ${response.status}`)
  }

  const data = (await response.json()) as { lists: ListDef[] }
  return data.lists
}

// Tables (catalog/tables/<id>.json) — a weighted roll over lists, resource
// grants and no-drop outcomes. What a camp rolls when cleared, and what a shop
// rolls to stock. This is what map authors bind to a camp's loot source.
export async function fetchTables(): Promise<TableDef[]> {
  const response = await fetch(`${API_BASE}/catalog/tables`)

  if (!response.ok) {
    throw new Error(`Failed to load tables: ${response.status}`)
  }

  const data = (await response.json()) as { tables: TableDef[] }
  return data.tables
}

export async function fetchNeutralGroups(): Promise<NeutralGroupTierSummary[]> {
  const response = await fetch(`${API_BASE}/api/catalog/neutral-groups`)

  if (!response.ok) {
    throw new Error(`Failed to load neutral groups: ${response.status}`)
  }

  const data = (await response.json()) as { tiers: NeutralGroupTierSummary[] }
  return data.tiers
}
