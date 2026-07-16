import type { AuthoredProjectileDef } from './projectileEditorForm'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

export class EditorValidationError extends Error {
  serverMessage: string
  constructor(message: string) {
    super(message)
    this.name = 'EditorValidationError'
    this.serverMessage = message
  }
}

export async function fetchAuthoredProjectileDefs(): Promise<AuthoredProjectileDef[]> {
  const res = await fetch(`${API_BASE}/catalog/projectiles`)
  if (!res.ok) throw new Error(`Failed to load projectile defs: ${res.status}`)
  const data = (await res.json()) as { projectiles: AuthoredProjectileDef[] }
  return data.projectiles ?? []
}

export async function saveEditorProjectile(projectile: AuthoredProjectileDef): Promise<void> {
  const res = await fetch(`${API_BASE}/projectiles`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ projectile }),
  })
  if (res.status === 400) {
    const body = (await res.json()) as { error?: string; message?: string }
    if (body.error === 'validation_failed') throw new EditorValidationError(body.message ?? 'validation failed')
  }
  if (!res.ok) throw new Error(`Failed to save projectile: ${res.status}`)
}

export async function deleteEditorProjectile(id: string): Promise<'deleted' | 'reset'> {
  const res = await fetch(`${API_BASE}/projectiles/${encodeURIComponent(id)}`, { method: 'DELETE' })
  if (!res.ok) throw new Error(`Failed to delete projectile: ${res.status}`)
  const body = (await res.json()) as { status: 'deleted' | 'reset' }
  return body.status
}
