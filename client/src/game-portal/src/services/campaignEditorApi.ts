// Write API for the Campaigns editor. Reads still go through campaignApi.ts
// (`fetchCampaignCatalog`). Campaign HEADERS are authored here; a campaign's
// LEVELS are authored by assigning maps, which write their own campaign block
// through saveMapCatalogFile (see game/maps/catalog.ts).

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

export interface CampaignHeaderInput {
  id: string
  displayName: string
  description: string
  sortOrder: number
  locked: boolean
}

// Thrown on a 400 validation_failed from the server (author-fixable — the
// message names the problem, e.g. a bad id or a still-referenced campaign).
export class CampaignSaveError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'CampaignSaveError'
  }
}

async function readError(res: Response): Promise<string> {
  const body = (await res.json().catch(() => null)) as { message?: string } | null
  return body?.message ?? `Server error ${res.status}`
}

// Create or update a campaign header. POST /api/catalog/campaigns.
export async function saveCampaignHeader(input: CampaignHeaderInput): Promise<void> {
  const res = await fetch(`${API_BASE}/api/catalog/campaigns`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  })
  if (res.status === 400) {
    throw new CampaignSaveError(await readError(res))
  }
  if (!res.ok) {
    throw new Error(await readError(res))
  }
}

// Delete an author-created campaign. DELETE /api/catalog/campaigns/{id}. The
// server refuses (400) if the campaign is built-in or still referenced by maps.
export async function deleteCampaign(id: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/catalog/campaigns/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
  if (res.status === 400) {
    throw new CampaignSaveError(await readError(res))
  }
  if (!res.ok) {
    throw new Error(await readError(res))
  }
}
