import type { MapCatalogEntry, MapCatalogFile } from '../network/protocol'
import type { BuildingDef } from './buildingDefs'
import type { UnitDef } from './unitDefs'
import type { ActionIconDef } from './actionIconDefs'

export async function fetchMapCatalog(): Promise<MapCatalogEntry[]> {
  const response = await fetch('http://localhost:8080/maps')

  if (!response.ok) {
    throw new Error(`Failed to load maps: ${response.status}`)
  }

  const maps = (await response.json()) as MapCatalogEntry[]
  return maps
}

export async function fetchMapCatalogFile(mapId: string): Promise<MapCatalogFile> {
  const response = await fetch(`http://localhost:8080/maps/${encodeURIComponent(mapId)}`)

  if (!response.ok) {
    throw new Error(`Failed to load map ${mapId}: ${response.status}`)
  }

  return (await response.json()) as MapCatalogFile
}

export async function fetchBuildingDefs(): Promise<BuildingDef[]> {
  const response = await fetch('http://localhost:8080/catalog/buildings')

  if (!response.ok) {
    throw new Error(`Failed to load building defs: ${response.status}`)
  }

  const data = (await response.json()) as { buildings: BuildingDef[] }
  return data.buildings
}

export async function fetchUnitDefs(): Promise<UnitDef[]> {
  const response = await fetch('http://localhost:8080/catalog/units')

  if (!response.ok) {
    throw new Error(`Failed to load unit defs: ${response.status}`)
  }

  const data = (await response.json()) as { units: UnitDef[] }
  return data.units
}

export async function fetchActionIcons(): Promise<ActionIconDef[]> {
  const response = await fetch('http://localhost:8080/catalog/action-icons')

  if (!response.ok) {
    throw new Error(`Failed to load action icons: ${response.status}`)
  }

  const data = (await response.json()) as { icons: ActionIconDef[] }
  return data.icons
}
