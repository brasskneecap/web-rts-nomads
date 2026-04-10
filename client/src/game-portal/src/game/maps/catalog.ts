import type { MapCatalogEntry } from '../network/protocol'

export async function fetchMapCatalog(): Promise<MapCatalogEntry[]> {
  const response = await fetch('http://localhost:8080/maps')

  if (!response.ok) {
    throw new Error(`Failed to load maps: ${response.status}`)
  }

  const maps = (await response.json()) as MapCatalogEntry[]
  return maps
}
