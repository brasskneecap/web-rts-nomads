import type { AuthoredPerkDef } from './perkEditorForm'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

// EditorValidationError carries the server's validation message for inline
// display beside Save. Body shape: {"error":"validation_failed","message":"..."}
export class EditorValidationError extends Error {
  serverMessage: string
  constructor(message: string) {
    super(message)
    this.name = 'EditorValidationError'
    this.serverMessage = message
  }
}

export async function fetchAuthoredPerkDefs(): Promise<AuthoredPerkDef[]> {
  const res = await fetch(`${API_BASE}/catalog/perks`)
  if (!res.ok) throw new Error(`Failed to load perk defs: ${res.status}`)
  const data = (await res.json()) as { perks: AuthoredPerkDef[] }
  return data.perks ?? []
}

export async function saveEditorPerk(perk: AuthoredPerkDef): Promise<void> {
  const res = await fetch(`${API_BASE}/perks`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ perk }),
  })
  if (res.status === 400) {
    const body = (await res.json()) as { error?: string; message?: string }
    if (body.error === 'validation_failed') throw new EditorValidationError(body.message ?? 'validation failed')
  }
  if (!res.ok) throw new Error(`Failed to save perk: ${res.status}`)
}

export async function deleteEditorPerk(id: string): Promise<'deleted' | 'reset'> {
  const res = await fetch(`${API_BASE}/perks/${encodeURIComponent(id)}`, { method: 'DELETE' })
  if (!res.ok) throw new Error(`Failed to delete perk: ${res.status}`)
  const body = (await res.json()) as { status: 'deleted' | 'reset' }
  return body.status
}
