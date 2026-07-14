import type { AuthoredAbilityDef } from './abilityEditorForm'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

// EditorValidationError carries the server's validation message for inline
// display beside Save (the server is the validator). Body shape:
//   {"error":"validation_failed","message":"..."}
export class EditorValidationError extends Error {
  serverMessage: string
  constructor(message: string) {
    super(message)
    this.name = 'EditorValidationError'
    this.serverMessage = message
  }
}

async function getJson<T>(path: string): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`)
  if (!res.ok) throw new Error(`Failed to load ${path}: ${res.status}`)
  return (await res.json()) as T
}

export async function fetchAuthoredAbilityDefs(): Promise<AuthoredAbilityDef[]> {
  const data = await getJson<{ abilities: AuthoredAbilityDef[] }>('/catalog/abilities')
  return data.abilities ?? []
}

export async function fetchProjectileIds(): Promise<string[]> {
  const data = await getJson<{ projectiles: { id: string }[] }>('/catalog/projectiles')
  return (data.projectiles ?? []).map((p) => p.id)
}

export async function fetchEffectIds(): Promise<string[]> {
  const data = await getJson<{ effects: { id: string }[] }>('/catalog/effects')
  return (data.effects ?? []).map((e) => e.id)
}

export async function fetchAutoCastSelectors(): Promise<string[]> {
  const data = await getJson<{ autoCastSelectors: string[] }>('/catalog/autocast-selectors')
  return data.autoCastSelectors ?? []
}

export async function fetchAbilityCategories(): Promise<string[]> {
  const data = await getJson<{ abilityCategories: string[] }>('/catalog/ability-categories')
  return data.abilityCategories ?? []
}

export async function fetchDamageTypes(): Promise<string[]> {
  const data = await getJson<{ damageTypes: string[] }>('/catalog/damage-types')
  return data.damageTypes ?? []
}

export async function saveEditorAbility(ability: AuthoredAbilityDef): Promise<void> {
  const res = await fetch(`${API_BASE}/abilities`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ability }),
  })
  if (res.status === 400) {
    const body = (await res.json()) as { error?: string; message?: string }
    if (body.error === 'validation_failed') throw new EditorValidationError(body.message ?? 'validation failed')
  }
  if (!res.ok) throw new Error(`Failed to save ability: ${res.status}`)
}

export async function deleteEditorAbility(id: string): Promise<'deleted' | 'reset'> {
  const res = await fetch(`${API_BASE}/abilities/${encodeURIComponent(id)}`, { method: 'DELETE' })
  if (!res.ok) throw new Error(`Failed to delete ability: ${res.status}`)
  const body = (await res.json()) as { status: 'deleted' | 'reset' }
  return body.status
}

// uploadAbilityIcon posts a raw PNG blob for the ability; the server stores it
// and forces the ability's Icon key to its id. Save the ability def first.
export async function uploadAbilityIcon(id: string, file: Blob): Promise<void> {
  const res = await fetch(`${API_BASE}/abilities/${encodeURIComponent(id)}/image`, {
    method: 'POST',
    headers: { 'Content-Type': 'image/png' },
    body: file,
  })
  if (!res.ok) {
    const body = (await res.json().catch(() => null)) as { message?: string } | null
    throw new Error(body?.message ?? `Failed to upload ability icon: ${res.status}`)
  }
}

export function abilityIconUrl(id: string): string {
  return `${API_BASE}/catalog/abilities/${encodeURIComponent(id)}/image`
}
