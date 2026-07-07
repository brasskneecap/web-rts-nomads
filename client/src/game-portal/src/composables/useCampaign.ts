// Campaign composable — bundles the server-loaded catalog with player
// progress (`useProfile`) and the start-level flow.
//
// Catalog source: GET /api/catalog/campaigns (see
// `server/internal/game/catalog/campaigns/*.json`). Loaded once into a
// module-level singleton, mirroring the useProfile pattern, so multiple
// component mounts share one fetch.
//
// Pure functions (`computeLevelStatus`, `isLevelUnlocked`) are exported so
// the level-list UI can render status without going through Vue reactivity.

import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { getCampaignForLevel } from '@/data/campaigns'
import type { Campaign, CampaignLevel, CampaignLevelStatus, CampaignLevelView } from '@/types/campaign'
import { useProfile } from '@/composables/useProfile'
import { useLobbies } from '@/composables/useLobbies'
import { usePlayer } from '@/composables/usePlayer'
import { createMultiplayerLobby } from '@/composables/useLobbyCreation'
import { fetchCampaignCatalog } from '@/services/campaignApi'
import { markCampaignLevelComplete } from '@/services/profileApi'
import { beginCampaignSession, clearCampaignSession } from '@/state/campaignSession'

// Module-level singletons — one fetch shared across every Campaign.vue
// mount, profile-driven status recomputed reactively.
const campaigns = ref<ReadonlyArray<Campaign>>([])
const isLoading = ref(false)
const loadError = ref<string>('')
let initialized = false

async function ensureCatalogLoaded(): Promise<void> {
  if (initialized) return
  initialized = true
  isLoading.value = true
  loadError.value = ''
  try {
    campaigns.value = await fetchCampaignCatalog()
  } catch (err) {
    // Allow retry on the next mount when the fetch fails (network blip,
    // server cold-start). The Campaign panel surfaces `loadError` so the
    // user can see something went wrong rather than an empty tab strip.
    initialized = false
    loadError.value = err instanceof Error ? err.message : 'Failed to load campaigns.'
  } finally {
    isLoading.value = false
  }
}

/** Pure: is `level` unlocked given the set of completed level IDs?
 *
 *  EXT-PREREQS: when richer prerequisites land (any-of, all-of,
 *  cross-campaign, ownership gates), update this single function. All
 *  callers go through here. */
export function isLevelUnlocked(
  level: CampaignLevel,
  completedLevels: ReadonlySet<string>,
): boolean {
  if (level.prerequisiteLevelId === null) return true
  return completedLevels.has(level.prerequisiteLevelId)
}

/** Pure: derive a level's status from completion state. */
export function computeLevelStatus(
  level: CampaignLevel,
  completedLevels: ReadonlySet<string>,
): CampaignLevelStatus {
  if (completedLevels.has(level.id)) return 'completed'
  if (isLevelUnlocked(level, completedLevels)) return 'unlocked'
  return 'locked'
}

export function useCampaign() {
  const router = useRouter()
  const { profile, refresh: refreshProfile } = useProfile()
  const { startLobby } = useLobbies()
  const { playerId } = usePlayer()

  /** Reactive set of completed level IDs from the player profile. Empty set
   *  while the profile is still loading. */
  const completedLevels = computed<ReadonlySet<string>>(
    () => new Set(profile.value?.completedCampaignLevels ?? []),
  )

  /** Reactive lookup of "is this objective ever completed for this level?"
   *  Reads from the profile's `completedCampaignObjectives` map keyed by
   *  `"<campaignId>/<levelId>"`. The level-select UI (Campaign.vue, §13)
   *  uses this to render the ✓ / □ icon per objective row.
   *
   *  Returns false when the profile hasn't loaded yet; the panel re-renders
   *  reactively once the profile arrives. */
  function isObjectiveCompletedForLevel(
    campaignId: string,
    levelId: string,
    objectiveId: string,
  ): boolean {
    const key = `${campaignId}/${levelId}`
    const set = profile.value?.completedCampaignObjectives?.[key]
    if (!set) return false
    return set.includes(objectiveId)
  }

  /** Reactive view of all campaigns with each level's status precomputed. */
  const campaignsView = computed<ReadonlyArray<{ campaign: Campaign; levels: CampaignLevelView[] }>>(
    () => {
      const done = completedLevels.value
      return campaigns.value.map((campaign) => ({
        campaign,
        levels: campaign.levels.map((level) => ({
          level,
          status: computeLevelStatus(level, done),
        })),
      }))
    },
  )

  /** Shared setup for both Start and Lobby paths: validate that the level
   *  belongs to a known campaign, seed the campaign session, and create the
   *  lobby. Returns the new lobby id; the caller decides whether to
   *  auto-start (Start button) or host the lobby in-panel (Lobby button).
   *
   *  Throws on lobby-creation failure; the campaign session is cleared
   *  before the throw so a later retry doesn't inherit a stale tag. */
  async function createCampaignLobby(level: CampaignLevel): Promise<string> {
    const owningCampaign = getCampaignForLevel(level.id, campaigns.value)
    if (!owningCampaign) {
      throw new Error(`campaign for level ${level.id} not found`)
    }

    // Seed campaign-session state BEFORE creating the lobby. Match.vue reads
    // this on victory; setting it first means the session is in place even
    // if the auto-start path races ahead of any router navigation watcher.
    beginCampaignSession({
      campaignId: owningCampaign.id,
      levelId: level.id,
      mapId: level.mapId,
      levelDisplayName: level.displayName,
    })

    try {
      const lobby = await createMultiplayerLobby({
        mapId: level.mapId,
        hostPlayerId: playerId.value,
        // Tell the server which campaign level this lobby is for. The
        // server installs that level's authored objectives on the
        // GameState at match start (see §7 of the change). Custom Game
        // lobbies leave this undefined.
        campaignLevelId: level.id,
      })
      return lobby.id
    } catch (err) {
      clearCampaignSession()
      throw err
    }
  }

  /** Start path: create the lobby, immediately start the match, and route
   *  into /match/:id. Single-player ergonomics — no waiting in the lobby.
   *  On a server-side start failure, falls back to the lobby view so the
   *  player can retry manually. */
  async function startCampaignLevel(level: CampaignLevel): Promise<void> {
    const lobbyId = await createCampaignLobby(level)
    try {
      const started = await startLobby({ id: lobbyId, playerId: playerId.value })
      if (started.status === 'started' && started.matchId) {
        await router.push(`/match/${started.matchId}`)
        return
      }
    } catch (err) {
      console.error('[Campaign] auto-start failed, falling back to lobby:', err)
    }
    await router.push(`/lobby/${lobbyId}`)
  }

  /** Lobby path: create the lobby and route to /lobby/:id so the host can
   *  invite friends over Steam before clicking Start. Same lobby Custom Game
   *  uses; the campaign session rides alongside so Match.vue records
   *  completion on victory regardless of how many players joined. */
  async function openCampaignLobby(level: CampaignLevel): Promise<void> {
    const lobbyId = await createCampaignLobby(level)
    await router.push(`/lobby/${lobbyId}`)
  }

  /** Mark a level complete on the server and refresh the local profile so
   *  the next campaign-panel mount reflects the new unlock state.
   *  Call from the match-end victory hook. Idempotent server-side. */
  async function completeLevel(levelId: string): Promise<void> {
    await markCampaignLevelComplete(levelId)
    await refreshProfile()
  }

  return {
    campaignsView,
    completedLevels,
    isObjectiveCompletedForLevel,
    isLoading,
    loadError,
    initialize: ensureCatalogLoaded,
    startCampaignLevel,
    // Creates the campaign lobby (seeding the campaign session) and returns its
    // id WITHOUT navigating, so the Campaign panel can host the lobby inline in
    // the parchment panel instead of routing to /lobby/:id.
    createCampaignLobby,
    openCampaignLobby,
    completeLevel,
  }
}
