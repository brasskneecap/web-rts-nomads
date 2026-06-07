// Client-side accessors for the server campaign catalog.
// Mirrors the pattern in `profileApi.ts` and `game/maps/catalog.ts`.

import type { Campaign } from '@/types/campaign'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

/** Fetch all campaigns from the server. The payload shape matches the
 *  `Campaign` type — server-side `CampaignDef` is authored to serialize
 *  to the same JSON shape (see server/internal/game/campaign_defs.go).
 *  Throws on HTTP failure; callers surface the error in the UI. */
export async function fetchCampaignCatalog(): Promise<Campaign[]> {
  const res = await fetch(`${API_BASE}/api/catalog/campaigns`)
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText)
    throw new Error(text || `Failed to load campaigns (${res.status})`)
  }
  const body = (await res.json()) as { campaigns: Campaign[] }
  return body.campaigns ?? []
}
