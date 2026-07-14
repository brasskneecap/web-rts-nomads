import { throwIfValidationFailed } from './editorCatalogApi'
import type { AuthoredPathDef } from './pathEditorForm'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

// EditorPathEntry mirrors the Go EditorPathEntry (server/internal/game/
// path_editor.go): one merged (embedded + overlay, overlay wins) promotion
// path, as returned by GET /catalog/paths. `def` is the raw pathCatalogFile
// JSON (json.RawMessage server-side, so it arrives here as a plain object,
// not a double-encoded string) — the full AuthoredPathDef shape from Task 1's
// pathEditorForm, minus the client-only `parentUnit` field (the owning unit
// is carried separately on `unit`).
export interface EditorPathEntry {
  unit: string
  path: string
  def: AuthoredPathDef
}

// PerkEntry mirrors the Go PerkDef (server/internal/game/perk_defs.go) as
// emitted by ListPerkDefs / the /catalog/perks and /perks routes. Field names
// match the Go json tags exactly (checked against perk_defs.go's PerkDef
// struct). `wired` is a derived, presentation-only field populated ONLY by
// ListPerkDefs (i.e. only on GET /catalog/perks reads) — sending it back on a
// POST /perks save is harmless (the server's perkEntryJSON decode target
// simply ignores unknown fields) but it carries no meaning there.
export interface PerkEntry {
  id: string
  displayName?: string
  description?: string
  tooltipTemplate?: string
  tooltipTemplateByTrap?: Record<string, string>
  tooltipTemplateByOwnedPerk?: Record<string, string>
  icon?: string
  unitType?: string
  path?: string
  rank?: string
  requiresPerk?: string
  config?: Record<string, number>
  configByRank?: Record<string, Record<string, number>>
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  effect?: Record<string, any>
  grantsAbilities?: string[]
  wired: boolean
}

// fetchPaths loads the full merged (embedded + overlay) promotion-path
// catalog for the path editor's list view.
export async function fetchPaths(): Promise<EditorPathEntry[]> {
  const res = await fetch(`${API_BASE}/catalog/paths`)
  if (!res.ok) throw new Error(`Failed to load path defs: ${res.status}`)
  const data = (await res.json()) as { paths: EditorPathEntry[] }
  return data.paths
}

// savePath persists an authored path def. `req` is exactly the shape
// saveRequestFromPathForm (Task 1) returns — the caller does not need to
// reshape anything.
export async function savePath(req: { unit: string; path: AuthoredPathDef }): Promise<void> {
  const res = await fetch(`${API_BASE}/paths`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  })
  await throwIfValidationFailed(res)
  if (!res.ok) throw new Error(`Failed to save path: ${res.status}`)
}

// deletePath removes an editor override for a path. The server rejects the
// delete (400 validation_failed -> EditorValidationError) if any unit's
// pathChances still references this path id.
export async function deletePath(id: string): Promise<{ status: string }> {
  const res = await fetch(`${API_BASE}/paths/${encodeURIComponent(id)}`, { method: 'DELETE' })
  await throwIfValidationFailed(res)
  if (!res.ok) throw new Error(`Failed to delete path: ${res.status}`)
  const body = (await res.json()) as { status: string }
  return body
}

// savePerks persists a unit/path/rank's perk pool (REPLACE semantics for that
// one rank's list, mirroring the abilities REPLACE-list convention).
export async function savePerks(req: {
  unit: string
  path: string
  rank: string
  perks: PerkEntry[]
}): Promise<void> {
  const res = await fetch(`${API_BASE}/perks`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  })
  await throwIfValidationFailed(res)
  if (!res.ok) throw new Error(`Failed to save perks: ${res.status}`)
}

// deletePerks removes the editor override for one unit/path/rank's perk pool.
export async function deletePerks(unit: string, path: string, rank: string): Promise<{ status: string }> {
  const res = await fetch(
    `${API_BASE}/perks/${encodeURIComponent(unit)}/${encodeURIComponent(path)}/${encodeURIComponent(rank)}`,
    { method: 'DELETE' },
  )
  await throwIfValidationFailed(res)
  if (!res.ok) throw new Error(`Failed to delete perks: ${res.status}`)
  const body = (await res.json()) as { status: string }
  return body
}

// fetchPerkCatalog loads the full perk catalog (every unit/path/rank's perk
// pool) for the perk editor's list view, including the `wired` flag.
export async function fetchPerkCatalog(): Promise<PerkEntry[]> {
  const res = await fetch(`${API_BASE}/catalog/perks`)
  if (!res.ok) throw new Error(`Failed to load perk catalog: ${res.status}`)
  const data = (await res.json()) as { perks: PerkEntry[] }
  return data.perks
}
