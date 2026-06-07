// Pure helpers over campaign data. The catalog itself is loaded from the
// server (see `services/campaignApi.ts` and
// `server/internal/game/catalog/campaigns/*.json`) — this module no longer
// owns campaign definitions. It exists as a home for level/campaign
// lookups that don't belong on the composable or the Vue component.

import type { Campaign } from '@/types/campaign'

/** Find which campaign owns the given level id. Returns null when not
 *  found. Useful in cases where the level id is known but the owning
 *  campaign needs to be resolved (e.g. seeding the campaign session). */
export function getCampaignForLevel(
  levelId: string,
  campaigns: ReadonlyArray<Campaign>,
): Campaign | null {
  for (const c of campaigns) {
    if (c.levels.some((l) => l.id === levelId)) return c
  }
  return null
}
