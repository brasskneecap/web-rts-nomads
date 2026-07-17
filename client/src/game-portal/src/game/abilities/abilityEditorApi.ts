import type { AuthoredAbilityDef } from './abilityEditorForm'
import { parseActionSchemaResponse, type ActionSchemaBundle } from './program/programSchema'
import type { ValidationIssue } from './program/programValidation'
import { parsePreviewResult, type PreviewRequest, type PreviewResult } from './program/programPreview'

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

// fetchActionSchema loads the composable action schema catalog (field
// definitions per action type + shared enums) that drives the Phase 5b
// editor's dynamic form rendering.
export async function fetchActionSchema(): Promise<ActionSchemaBundle> {
  const raw = await getJson<unknown>('/catalog/action-schema')
  return parseActionSchemaResponse(raw)
}

// validateAbilityProgram asks the server (the authoritative validator) to
// structurally validate an in-progress authored ability without saving it,
// returning the full issue list (errors + warnings) for inline display.
export async function validateAbilityProgram(ability: AuthoredAbilityDef): Promise<ValidationIssue[]> {
  const res = await fetch(`${API_BASE}/abilities/validate`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ability }),
  })
  if (!res.ok) throw new Error(`Failed to validate ability: ${res.status}`)
  const body = (await res.json()) as { issues?: ValidationIssue[] }
  return body.issues ?? []
}

// convertAbility asks the server to compile a legacy ability's mechanic
// fields into an equivalent composable AbilityProgram, returning the
// converted def alongside any lossy-conversion warnings and whether the
// result is currently runnable by the action registry.
export async function convertAbility(
  id: string,
): Promise<{ ability: AuthoredAbilityDef; warnings: string[]; runnable: boolean }> {
  const res = await fetch(`${API_BASE}/abilities/${encodeURIComponent(id)}/convert`, { method: 'POST' })
  if (res.status === 404) throw new Error(`Ability not found: ${id}`)
  if (!res.ok) throw new Error(`Failed to convert ability: ${res.status}`)
  const body = (await res.json()) as { ability: AuthoredAbilityDef; warnings?: string[]; runnable?: boolean }
  return { ability: body.ability, warnings: body.warnings ?? [], runnable: !!body.runnable }
}

// runAbilityPreview asks the server to execute an authored ability against a
// synthetic scene (the authoritative simulation, not a client-side
// re-implementation) and returns the resulting execution trace + unit HP
// deltas for the Phase 6a preview panel.
export async function runAbilityPreview(req: PreviewRequest): Promise<PreviewResult> {
  const res = await fetch(`${API_BASE}/abilities/preview`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  })
  if (!res.ok) {
    const body = (await res.json().catch(() => null)) as { message?: string } | null
    throw new Error(body?.message ?? `Failed to preview ability: ${res.status}`)
  }
  return parsePreviewResult(await res.json())
}
