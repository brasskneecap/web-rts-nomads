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
