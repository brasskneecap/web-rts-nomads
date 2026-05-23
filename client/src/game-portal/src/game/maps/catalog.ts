import type { MapCatalogEntry, MapCatalogFile } from '../network/protocol'
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

export async function saveMapCatalogFile(entry: MapCatalogFile): Promise<void> {
  const response = await fetch(`${API_BASE}/maps`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(entry),
  })
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
