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

/** Whether an objective is evaluated against team-aggregated metrics or each
 *  player's own. Mirrors the server's `ObjectiveScope` enum. */
export type ObjectiveScope = 'team' | 'player'

/** Static, catalog-loaded definition of one objective attached to a campaign
 *  level. Mirrors the server's `ObjectiveDef` JSON shape (see
 *  `server/internal/game/objective_defs.go`). The handler-typed `config`
 *  is preserved verbatim — the client doesn't validate it; the server
 *  panics at startup on bad data. */
export interface Objective {
  /** Stable id, unique within the level. Persisted to
   *  `completedCampaignObjectives` on the player profile. */
  id: string
  /** Dispatch key identifying the handler. One of `kill_camps`,
   *  `build_buildings`, `collect_resource`, `kill_camps_before_wave`,
   *  `rank_units`, `survive_waves`. */
  type: string
  /** Human-readable summary shown in the level select and in-match HUD. */
  description?: string
  /** Defaults to `'team'` server-side when omitted. */
  scope?: ObjectiveScope
  /** When true, this objective gates victory via the §9 AND-rule. */
  required?: boolean
  /** DP reward granted the first time (ever, per player) this objective is
   *  completed. Absent/0 = no reward. */
  rewardDominionPoints?: number
  /** Conquest Badge reward granted on first-ever completion. Absent/0 = no reward. */
  rewardConquestBadges?: number
  /** Opaque to the client — passed through to the server handler. */
  config?: Record<string, unknown>
}

/** Per-tick wire shape of one objective's state from the snapshot's
 *  viewer's perspective. Carried inside `VictorySnapshot.objectives[]`
 *  on every match snapshot. Mirrors the server's `ObjectiveSnapshot`
 *  (see `server/pkg/protocol/messages.go`). */
export interface ObjectiveProgress {
  id: string
  type: string
  description?: string
  scope: ObjectiveScope
  required?: boolean
  current: number
  requiredCount: number
  completed: boolean
  failed?: boolean
  /** DP reward for first-ever completion; mirrors the server snapshot. */
  rewardDominionPoints?: number
  /** Conquest Badge reward for first-ever completion; mirrors the server snapshot. */
  rewardConquestBadges?: number
}

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
  /** Per-level objectives. Server-authored in `catalog/campaigns/*.json`;
   *  the campaign panel renders each row with a check/uncheck based on
   *  the player profile's `completedCampaignObjectives` set. May be
   *  empty / absent — a level with no objectives wins purely on the
   *  legacy wave/townhall rule. */
  objectives?: Objective[]

  // EXT-REWARDS: per-level reward payload (dominion points, items, perks).
  // EXT-MODIFIERS: per-level gameplay modifiers (timer, fog density, etc.).
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
