import type { MapCatalogEntry, MapCatalogFile } from '../network/protocol'

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
