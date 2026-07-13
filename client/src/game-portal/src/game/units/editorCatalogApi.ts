import { EditorValidationError } from './unitEditorApi'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

// FactionDef mirrors the Go FactionDef (server/internal/game/faction_defs.go).
// A faction directory without a faction.json still yields a record — the server
// synthesizes one — so every faction the editor sees has a display name.
export interface FactionDef {
  id: string
  displayName: string
  order?: number
}

// Raw Go struct shapes for the catalog list endpoints, typed only far enough
// to extract the identifier field. See server/internal/game/{projectile_defs,
// ability_defs,building_defs}.go for the full authored shape.
interface ProjectileDefRaw {
  id: string
}
interface AbilityDefRaw {
  id: string
}
interface BuildingDefRaw {
  type: string
}

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`)
  if (!res.ok) throw new Error(`Failed to load ${path}: ${res.status}`)
  return (await res.json()) as T
}

async function throwIfValidationFailed(res: Response): Promise<void> {
  if (res.status !== 400) return
  const body = (await res.json()) as { error?: string; message?: string }
  if (body.error === 'validation_failed') throw new EditorValidationError(body.message ?? 'validation failed')
}

export async function fetchFactions(): Promise<FactionDef[]> {
  const data = await getJSON<{ factions?: FactionDef[] }>('/catalog/factions')
  return data.factions ?? []
}

export async function saveFaction(faction: FactionDef): Promise<void> {
  const res = await fetch(`${API_BASE}/factions`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ faction }),
  })
  await throwIfValidationFailed(res)
  if (!res.ok) throw new Error(`Failed to save faction: ${res.status}`)
}

export async function deleteFaction(id: string): Promise<void> {
  const res = await fetch(`${API_BASE}/factions/${encodeURIComponent(id)}`, { method: 'DELETE' })
  await throwIfValidationFailed(res)
  if (!res.ok) throw new Error(`Failed to delete faction: ${res.status}`)
}

export async function fetchArchetypes(): Promise<string[]> {
  const data = await getJSON<{ archetypes?: string[] }>('/catalog/archetypes')
  return (data.archetypes ?? []).slice().sort()
}

export async function fetchProjectileIds(): Promise<string[]> {
  const data = await getJSON<{ projectiles?: ProjectileDefRaw[] }>('/catalog/projectiles')
  return (data.projectiles ?? []).map((p) => p.id).sort()
}

export async function fetchAbilityIds(): Promise<string[]> {
  const data = await getJSON<{ abilities?: AbilityDefRaw[] }>('/catalog/abilities')
  return (data.abilities ?? []).map((a) => a.id).sort()
}

export async function fetchDamageTypes(): Promise<string[]> {
  const data = await getJSON<{ damageTypes?: string[] }>('/catalog/damage-types')
  return (data.damageTypes ?? []).slice().sort()
}

export async function fetchBuildingIds(): Promise<string[]> {
  const data = await getJSON<{ buildings?: BuildingDefRaw[] }>('/catalog/buildings')
  return (data.buildings ?? []).map((b) => b.type).sort()
}

// One packed-art file in a saveUnitArt payload — mirrors the server's
// UnitArtFile (server/internal/game/unit_art.go). Name is a forward-slash
// path relative to the unit's art directory (e.g. `sprites.json`,
// `packed/walking.png`); ContentBase64 is its raw bytes, base64-encoded.
export interface UnitArtUploadFile {
  name: string
  contentBase64: string
}

// saveUnitArt POSTs a packed art set (sprites.json + packed sheets + optional
// portrait) to the writable art dir. Base unit -> path omitted; promotion
// path -> path set (Phase 5). Mirrors saveFaction's 400 art_rejected handling.
export async function saveUnitArt(payload: {
  faction: string
  unit: string
  path?: string
  files: UnitArtUploadFile[]
}): Promise<void> {
  const res = await fetch(`${API_BASE}/unit-art`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  if (res.status === 400) {
    const body = (await res.json()) as { error?: string; message?: string }
    if (body.error === 'art_rejected') throw new EditorValidationError(body.message ?? 'art rejected')
  }
  if (!res.ok) throw new Error(`Failed to save unit art: ${res.status}`)
}
