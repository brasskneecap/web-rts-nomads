// Campaign domain types.
//
// A campaign is an ordered chain of levels. Each level points at one of the
// existing maps in the map catalog (`server/internal/game/catalog/maps/`) —
// campaigns NEVER own their own terrain data. They are a progression layer
// on top of the existing map system.
//
// Unlock state is derived: a level is unlocked when its `prerequisiteLevelId`
// is null, or when that prerequisite level appears in the player's
// `completedCampaignLevels` (persisted server-side on the PlayerProfile).
//
// Extension points marked with EXT-* comments below.

/** Status of a single campaign level for the current player. Derived from
 *  the campaign definition + the player's completed-level set. */
export type CampaignLevelStatus = 'locked' | 'unlocked' | 'completed'

export interface CampaignLevel {
  /** Stable, globally-unique level id (e.g. `forest_01`). Used as the key
   *  in `completedCampaignLevels` on the player profile. */
  id: string
  /** Human-readable label shown in the level list. */
  displayName: string
  /** Map catalog id the level launches. Must exist in
   *  `server/internal/game/catalog/maps/*.json`. */
  mapId: string
  /** ID of the level that must be completed before this one unlocks.
   *  `null` means no prerequisite (first level of a chain).
   *
   *  EXT-PREREQS: when richer prerequisites are needed (any-of, all-of,
   *  cross-campaign), replace this field with a `prerequisites: Prerequisite[]`
   *  union and update `isLevelUnlocked()` in `composables/useCampaign.ts`. */
  prerequisiteLevelId: string | null
  /** Optional short blurb shown on the level row. */
  description?: string

  // EXT-REWARDS: per-level reward payload (legend points, items, perks).
  // EXT-MODIFIERS: per-level gameplay modifiers (timer, fog density, etc.).
  // EXT-OBJECTIVES: per-level optional/bonus objectives for star ratings.
  // EXT-STORY: pre/post story text or cutscene id.
}

export interface Campaign {
  /** Stable campaign id (e.g. `forest`). */
  id: string
  /** Human-readable name shown on the campaign tab. */
  displayName: string
  /** Optional one-line description for the campaign header. */
  description?: string
  /** Levels in display order. The first level is the entry point. */
  levels: CampaignLevel[]
  /** Whether the campaign is currently unlockable. `true` keeps the tab in
   *  the strip but greys it out and blocks selection — used for advertising
   *  upcoming content (e.g. Swamp before its levels ship). Defaults to
   *  `false` when omitted.
   *
   *  EXT-LOCK: when richer unlock conditions land (account level, prior
   *  campaign completion, store entitlement), replace this boolean with an
   *  `unlockRequirement` union and update the locked-tab handling in
   *  `Campaign.vue` + `useCampaign.ts`. */
  locked?: boolean

  // EXT-BRANCHING: replace the flat `levels` array with a graph type if you
  // need branching paths or optional sub-campaigns. Keep `levels` as the
  // canonical flat list of nodes; add `edges` for the graph topology.
  // EXT-HIDDEN: separate from `locked` — add `isHiddenUntilUnlocked: boolean`
  // to hide a campaign entirely (no tab) until conditions are met.
}

/** A campaign level paired with its derived status for the current player. */
export interface CampaignLevelView {
  level: CampaignLevel
  status: CampaignLevelStatus
}
