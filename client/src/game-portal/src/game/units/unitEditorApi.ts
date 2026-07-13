import type { AuthoredUnitDef } from './unitEditorForm'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

// EditorValidationError carries the server's validation message for inline
// display beside the Save button (the server is the validator — see spec).
// Server error body shape confirmed from server/internal/http/profile_handlers.go
// writeJSONError (used by the /units handlers in editor_handlers.go):
//   json.NewEncoder(w).Encode(map[string]string{"error": code, "message": msg})
// i.e. {"error":"validation_failed","message":"..."} — matches the brief.
export class EditorValidationError extends Error {
  serverMessage: string
  constructor(message: string) {
    super(message)
    this.name = 'EditorValidationError'
    this.serverMessage = message
  }
}

// fetchAuthoredUnitDefs loads the merged (overlay-over-embed) unit defs as raw
// authored objects, preserving every JSON key (incl. art blobs) at runtime.
export async function fetchAuthoredUnitDefs(): Promise<AuthoredUnitDef[]> {
  const res = await fetch(`${API_BASE}/catalog/units`)
  if (!res.ok) throw new Error(`Failed to load unit defs: ${res.status}`)
  const data = (await res.json()) as { units: AuthoredUnitDef[] }
  return data.units
}

export async function saveEditorUnit(unit: AuthoredUnitDef): Promise<void> {
  const res = await fetch(`${API_BASE}/units`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ unit }),
  })
  if (res.status === 400) {
    const body = (await res.json()) as { error?: string; message?: string }
    if (body.error === 'validation_failed') throw new EditorValidationError(body.message ?? 'validation failed')
  }
  if (!res.ok) throw new Error(`Failed to save unit: ${res.status}`)
}

export async function deleteEditorUnit(type: string): Promise<'deleted' | 'reset'> {
  const res = await fetch(`${API_BASE}/units/${encodeURIComponent(type)}`, { method: 'DELETE' })
  if (!res.ok) throw new Error(`Failed to delete unit: ${res.status}`)
  const body = (await res.json()) as { status: 'deleted' | 'reset' }
  return body.status
}
