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

// An item defines only itself: its stats and its own purchase price. Crafting
// rides beside it in `crafting` (absent = not craftable, which drops any recipe
// the item had), and the server turns that into the paired recipe def. WHERE an
// item is available (shops, loot) is a shop-level concern edited elsewhere —
// not part of this request.
export type EditorSaveRequest = {
  item: Record<string, unknown>
  crafting?: EditorSaveCrafting
}

// The recipe half of a save. The two gold fields buy different things:
// craftCostGold is charged per craft at the Artificer; recipeCostGold is
// charged once at a Recipe Shop to learn the recipe.
export type EditorSaveCrafting = {
  inputs: string[]
  craftCostGold: number
  recipeCostGold: number
  starter: boolean
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

/**
 * What the destructive action did:
 * - `deleted`  — an author-created item was removed.
 * - `reverted` — a shipped item went back to the state before the last save.
 * - `reset`    — a shipped item went back to the catalog default (no undo step
 *                was left, e.g. after a server restart or a second reset).
 */
export type EditorItemRemoveStatus = 'deleted' | 'reverted' | 'reset'

export async function deleteEditorItem(id: string): Promise<EditorItemRemoveStatus> {
  const response = await fetch(`${API_BASE}/items/${encodeURIComponent(id)}`, { method: 'DELETE' })
  if (!response.ok) {
    const text = await response.text().catch(() => response.statusText)
    throw new Error(text || `Server error ${response.status}`)
  }
  const body = (await response.json()) as { status: EditorItemRemoveStatus }
  return body.status
}

/** Server-side cap (maxItemIconBytes in item_persistence.go). Enforced here too
 *  — the server uses http.MaxBytesReader, which closes the connection as soon
 *  as the limit is passed, and the browser reports that reset as an opaque
 *  "Failed to fetch" instead of surfacing the server's 400. Checking up front
 *  is the only way the author gets a message that says what went wrong. */
export const MAX_ITEM_ICON_BYTES = 256 * 1024

export async function uploadItemIcon(id: string, file: Blob): Promise<void> {
  if (file.size > MAX_ITEM_ICON_BYTES) {
    const kb = Math.round(file.size / 1024)
    throw new Error(`Icon is ${kb} KB — the limit is ${MAX_ITEM_ICON_BYTES / 1024} KB. Use a smaller PNG.`)
  }
  let response: Response
  try {
    response = await fetch(`${API_BASE}/items/${encodeURIComponent(id)}/image`, {
      method: 'POST',
      headers: { 'Content-Type': 'image/png' },
      body: file,
    })
  } catch (err) {
    // fetch only rejects on a network-level failure; say so rather than
    // letting a bare "Failed to fetch" reach the UI.
    throw new Error(`Could not reach the server to upload the icon (${err instanceof Error ? err.message : String(err)}).`)
  }
  if (!response.ok) {
    // The route answers with {"error","message"}; show the message, not the JSON.
    const text = await response.text().catch(() => '')
    let detail = text
    try {
      const body = JSON.parse(text) as { message?: string }
      if (body.message) detail = body.message
    } catch {
      // not JSON — fall back to the raw text
    }
    throw new Error(detail || `Icon upload failed (${response.status})`)
  }
}

export function itemIconUrl(id: string): string {
  return `${API_BASE}/catalog/items/${encodeURIComponent(id)}/image`
}
