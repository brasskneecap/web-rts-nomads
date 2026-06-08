// Single-slot reactive state that survives the route change from a live
// match (`/match/:id`) into the end-of-match recap screen (`/match-end`).
//
// Why this exists: Match.vue tears its GameClient down when the route
// changes — `ui.value.objectives`, `ui.value.players`, etc. are gone the
// moment we navigate away. Capturing the relevant subset here keeps the
// recap screen renderable without dragging the whole match-state along.
//
// Mirrors the pattern in steamLobbyState.ts / campaignSession.ts:
// module-level ref + small setters, no Pinia.

import { ref } from 'vue'
import type { ObjectiveSnapshot, PlayerSnapshot } from '@/game/network/protocol'
import type { MatchEndOutcome } from '@/components/match/matchEndOutcome'

export interface MatchEndSnapshot {
  /** Outcome that brought the player to the recap screen. Drives the
   *  header text + accent color (Victory / Defeat / Forfeit). */
  outcome: MatchEndOutcome
  /** Final objective state for every objective installed on the match.
   *  Already filtered to the viewer's perspective by the server (team-
   *  scope = team aggregate; player-scope = viewer's own state). */
  objectives: ObjectiveSnapshot[]
  /** Player snapshot blocks for every player in the lobby. The recap
   *  filters AI sentinels (enemy / neutral) before rendering. */
  players: PlayerSnapshot[]
  /** Local viewer's player id. Drives the "(You)" annotation and
   *  viewer-first sort order in the metrics table. */
  viewerId: string
  /** Campaign + level identifiers, set when the match was launched
   *  from the Campaign panel. Null/empty for Custom Game / find-game
   *  matches. The recap's "Return to Menu" handler reads these to
   *  write `markCampaignObjectivesComplete` before navigating away. */
  campaignId: string | null
  levelId: string | null
  /** Short display label for the subtitle (e.g. "Forest 1"). Stored
   *  here so the recap doesn't need to re-resolve it through the
   *  campaign catalog after the route change. */
  levelDisplayName?: string
}

/** Current end-of-match snapshot, or null when no recap is pending.
 *  Set by Match.vue immediately before pushing to `/match-end`; cleared
 *  by MatchEnd.vue's "Return to Menu" handler. */
export const matchEndSnapshot = ref<MatchEndSnapshot | null>(null)

/** Set the snapshot before navigating to the recap route. Subsequent
 *  calls overwrite — there's no use-case for stacking recaps. */
export function setMatchEndSnapshot(snap: MatchEndSnapshot): void {
  matchEndSnapshot.value = snap
}

/** Drop the snapshot. Called by MatchEnd.vue after the user dismisses
 *  the recap, so a stray navigation back to `/match-end` doesn't
 *  re-show stale data. */
export function clearMatchEndSnapshot(): void {
  matchEndSnapshot.value = null
}
