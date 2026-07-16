import type { AuthoredEffectDef } from './effectEditorForm'

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

export async function fetchAuthoredEffectDefs(): Promise<AuthoredEffectDef[]> {
  const res = await fetch(`${API_BASE}/catalog/effects`)
  if (!res.ok) throw new Error(`Failed to load effect defs: ${res.status}`)
  const data = (await res.json()) as { effects: AuthoredEffectDef[] }
  return data.effects ?? []
}

export async function saveEditorEffect(effect: AuthoredEffectDef): Promise<void> {
  const res = await fetch(`${API_BASE}/effects`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ effect }),
  })
  if (res.status === 400) {
    const body = (await res.json()) as { error?: string; message?: string }
    if (body.error === 'validation_failed') throw new EditorValidationError(body.message ?? 'validation failed')
  }
  if (!res.ok) throw new Error(`Failed to save effect: ${res.status}`)
}

export async function deleteEditorEffect(id: string): Promise<'deleted' | 'reset'> {
  const res = await fetch(`${API_BASE}/effects/${encodeURIComponent(id)}`, { method: 'DELETE' })
  if (!res.ok) throw new Error(`Failed to delete effect: ${res.status}`)
  const body = (await res.json()) as { status: 'deleted' | 'reset' }
  return body.status
}
