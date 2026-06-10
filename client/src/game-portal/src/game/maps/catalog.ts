import type { MapCatalogEntry, MapCatalogFile, NeutralGroupTierSummary } from '../network/protocol'
import type { BuildingDef } from './buildingDefs'
import type { ObstacleDef } from './obstacleDefs'
import type { UnitBounds, UnitDef } from './unitDefs'
import type { ActionIconDef } from './actionIconDefs'
import type { PerkDef } from './perkDefs'
import type { ItemDef } from './itemDefs'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

export async function fetchMapCatalog(): Promise<MapCatalogEntry[]> {
  const response = await fetch(`${API_BASE}/maps`)

  if (!response.ok) {
    throw new Error(`Failed to load maps: ${response.status}`)
  }

  const maps = (await response.json()) as MapCatalogEntry[]
  return maps
}

export async function fetchMapCatalogFile(mapId: string): Promise<MapCatalogFile> {
  const response = await fetch(`${API_BASE}/maps/${encodeURIComponent(mapId)}`)

  if (!response.ok) {
    throw new Error(`Failed to load map ${mapId}: ${response.status}`)
  }

  return (await response.json()) as MapCatalogFile
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
  const response = await fetch(`${API_BASE}/maps${query}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(entry),
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

export async function fetchBuildingDefs(): Promise<BuildingDef[]> {
  const response = await fetch(`${API_BASE}/catalog/buildings`)

  if (!response.ok) {
    throw new Error(`Failed to load building defs: ${response.status}`)
  }

  const data = (await response.json()) as { buildings: BuildingDef[] }
  return data.buildings
}

export async function fetchObstacleDefs(): Promise<ObstacleDef[]> {
  const response = await fetch(`${API_BASE}/catalog/obstacles`)

  if (!response.ok) {
    throw new Error(`Failed to load obstacle defs: ${response.status}`)
  }

  const data = (await response.json()) as { obstacles: ObstacleDef[] }
  return data.obstacles
}

export type PathBoundsEntry = { path: string; bounds: UnitBounds }

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
  return data.items
}

export async function fetchNeutralGroups(): Promise<NeutralGroupTierSummary[]> {
  const response = await fetch(`${API_BASE}/api/catalog/neutral-groups`)

  if (!response.ok) {
    throw new Error(`Failed to load neutral groups: ${response.status}`)
  }

  const data = (await response.json()) as { tiers: NeutralGroupTierSummary[] }
  return data.tiers
}
