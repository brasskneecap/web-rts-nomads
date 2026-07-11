const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

export type ProcEffectDef = {
  id: string
  damage: number
  damageType: string
  projectileID: string
  projectileScale?: number
  bounceCount?: number
  bounceRange?: number
  bounceDamageFalloff?: number
  slowMultiplier?: number
  slowDurationSeconds?: number
  burnDamagePerSecond?: number
  burnDurationSeconds?: number
}

// An item defines only itself (stats + its own costs). Craftability rides on
// the item as isRecipe/recipeCost; `inputs` are the recipe ingredients the
// server uses to sync the paired recipe def. WHERE an item is available (shops,
// loot) is a shop-level concern edited elsewhere — not part of this request.
export type EditorSaveRequest = {
  item: Record<string, unknown>
  inputs: string[]
}

// EditorValidationError carries the server's validation message for inline
// display beside the Save button (the server is the validator — see spec).
export class EditorValidationError extends Error {
  readonly serverMessage: string
  constructor(message: string) {
    super(message)
    this.name = 'EditorValidationError'
    this.serverMessage = message
  }
}

export async function fetchProcEffectDefs(): Promise<ProcEffectDef[]> {
  const response = await fetch(`${API_BASE}/catalog/procs`)
  if (!response.ok) throw new Error(`Failed to load proc effects: ${response.status}`)
  const data = (await response.json()) as { procs: ProcEffectDef[] }
  return data.procs
}

export async function saveEditorItem(req: EditorSaveRequest): Promise<void> {
  const response = await fetch(`${API_BASE}/items`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  })
  if (response.status === 400) {
    const body = (await response.json().catch(() => null)) as { error?: string; message?: string } | null
    if (body?.error === 'validation_failed') {
      throw new EditorValidationError(body.message ?? 'Validation failed')
    }
    throw new Error(body?.message ?? `Bad request (400)`)
  }
  if (!response.ok) {
    const text = await response.text().catch(() => response.statusText)
    throw new Error(text || `Server error ${response.status}`)
  }
}

export async function deleteEditorItem(id: string): Promise<'deleted' | 'reset'> {
  const response = await fetch(`${API_BASE}/items/${encodeURIComponent(id)}`, { method: 'DELETE' })
  if (!response.ok) {
    const text = await response.text().catch(() => response.statusText)
    throw new Error(text || `Server error ${response.status}`)
  }
  const body = (await response.json()) as { status: 'deleted' | 'reset' }
  return body.status
}

export async function uploadItemIcon(id: string, file: Blob): Promise<void> {
  const response = await fetch(`${API_BASE}/items/${encodeURIComponent(id)}/image`, {
    method: 'POST',
    headers: { 'Content-Type': 'image/png' },
    body: file,
  })
  if (!response.ok) {
    const text = await response.text().catch(() => response.statusText)
    throw new Error(text || `Icon upload failed (${response.status})`)
  }
}

export function itemIconUrl(id: string): string {
  return `${API_BASE}/catalog/items/${encodeURIComponent(id)}/image`
}
