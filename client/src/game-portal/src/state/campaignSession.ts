// Single-slot reactive state tying the currently-playing match to its
// campaign level (when any). Set when the player clicks a campaign level in
// the War Room, read in Match.vue's match-end hook so we know which level id
// to mark complete on victory.
//
// Mirrors the steamLobbyState.ts pattern (single-slot, reactive, no Pinia).
// The session is cleared on match exit so a subsequent /custom flow doesn't
// inherit a stale campaign id.

import { ref } from 'vue'

export interface CampaignSession {
  campaignId: string
  levelId: string
  /** Map id the level launched on. Stored for diagnostics; the actual map
   *  selection flow uses the lobby's own mapId field. */
  mapId: string
}

/** Active campaign session, or null when the current match is not part of
 *  a campaign (custom game, find-game flow). */
export const campaignSession = ref<CampaignSession | null>(null)

/** Mark the upcoming match as a campaign run. Called by the campaign-level
 *  click handler before creating the lobby. */
export function beginCampaignSession(session: CampaignSession): void {
  campaignSession.value = session
}

/** Drop the active campaign session. Called after match end (whether the
 *  level was completed, lost, or abandoned) so the next match starts clean. */
export function clearCampaignSession(): void {
  campaignSession.value = null
}
