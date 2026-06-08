## ADDED Requirements

### Requirement: Campaign levels carry optional objectives

A `CampaignLevelDef` SHALL include an optional `objectives` array. When omitted from JSON, the loaded slice SHALL be empty (not nil). The array contents SHALL conform to the `match-objectives` capability — the catalog loader validates objective types, scopes, required flags, and configs via the registered handlers, panicking at startup on invalid entries.

#### Scenario: Level without objectives loads cleanly
- **WHEN** a campaign level JSON omits the `objectives` field
- **THEN** the loaded `CampaignLevelDef.Objectives` is a non-nil empty slice and the level is reachable from the campaign panel without an objectives section

#### Scenario: Level with objectives loads with validation
- **WHEN** a campaign level JSON declares an `objectives` array that includes at least one entry of each registered objective type
- **THEN** every entry is loaded with the parsed config and the campaign panel renders the level's objective list

### Requirement: Match-start objective instantiation

When a campaign-launched match begins, the system SHALL initialise a per-match objective runtime state derived from the launching `CampaignLevelDef.Objectives` list. Each objective SHALL have its `state.Required` populated from the handler config (typically `count` or `amount`). Team-scope objectives SHALL have a single shared state; player-scope objectives SHALL have one state instance per player. Matches launched outside a campaign flow SHALL have an empty objective runtime — no objectives evaluated, no objectives in the snapshot.

#### Scenario: Campaign launch carries the level's objectives
- **WHEN** the client launches Forest 1 via the Campaign panel
- **THEN** the resulting `GameState` has objective runtime entries matching every objective declared in `forest_01.objectives`, each at `current: 0, completed: false, failed: false`

#### Scenario: Custom Game launch has no objectives
- **WHEN** the client launches a Custom Game on `exploration.json`
- **THEN** the resulting `GameState` has zero objective runtime entries and the snapshot's `objectives` array is empty

### Requirement: Victory rule = legacy condition AND every required objective complete

A match SHALL be considered victorious for a team when both of the following hold simultaneously: the existing wave/townhall victory rule is satisfied for that team; and every objective with `required: true` has `state.Completed == true`. Objectives with `required: false` (optional bonus objectives) SHALL NOT affect the victory determination regardless of their completion status.

#### Scenario: Optional objectives do not gate victory
- **WHEN** the wave/townhall rule is satisfied and a level has one required objective complete and one optional objective incomplete
- **THEN** victory fires

#### Scenario: Required objectives gate victory
- **WHEN** the wave/townhall rule is satisfied and a level has one required objective that is not yet complete
- **THEN** victory does not fire until the required objective completes

#### Scenario: Required objective alone does not win without the legacy rule
- **WHEN** every required objective on the level completes but the wave/townhall rule is not yet satisfied
- **THEN** victory does not fire

### Requirement: Profile persistence — completed objectives by campaign and level

The system SHALL persist completed objective IDs on `PlayerProfile.CompletedCampaignObjectives map[string][]string`. The map key SHALL be exactly the string `"<campaignId>/<levelId>"`. The value SHALL be a sorted-set (sorted alphabetically, deduplicated) of objective IDs the player has completed in any past attempt of that level. The `PlayerProfile` schema version SHALL be incremented when this field is introduced. Profiles written under the prior version SHALL be migrated forward on read by initialising the field to a non-nil empty map.

#### Scenario: New profile starts with no completed objectives
- **WHEN** a brand-new profile is created via `GetOrCreate`
- **THEN** `CompletedCampaignObjectives` is a non-nil empty map and the profile's `Version` equals the new current version

#### Scenario: Existing v5 profile migrates forward on read
- **WHEN** the server loads a profile that was written under the prior schema version and lacks `completedCampaignObjectives`
- **THEN** the loaded profile's `CompletedCampaignObjectives` is a non-nil empty map and the next mutation re-saves the profile under the new version

### Requirement: Complete-objectives endpoint

The system SHALL expose `POST /api/profile/campaign/complete-objectives` taking a JSON body `{campaignId: string, levelId: string, objectiveIds: []string}` and the `X-Player-ID` header. The endpoint SHALL be idempotent: invoking it with the same arguments multiple times SHALL produce the same final state. Inside a `WithLocked` profile mutation, the server SHALL merge `objectiveIds` into the sorted set at `CompletedCampaignObjectives["<campaignId>/<levelId>"]`. On success the endpoint SHALL return the updated profile.

#### Scenario: First call records the objectives
- **WHEN** a player POSTs `{campaignId: "forest", levelId: "forest_01", objectiveIds: ["clear_t1_camps", "build_barracks"]}`
- **THEN** `profile.CompletedCampaignObjectives["forest/forest_01"]` equals `["build_barracks", "clear_t1_camps"]` (sorted)

#### Scenario: Repeat call is a no-op on state
- **WHEN** the same player issues the same POST a second time
- **THEN** the profile state is unchanged from the first call

#### Scenario: Subsequent call adds new IDs to the existing set
- **WHEN** a player whose existing set is `["build_barracks"]` POSTs `{objectiveIds: ["build_barracks", "rank_units"]}`
- **THEN** the resulting set is `["build_barracks", "rank_units"]`

#### Scenario: Empty objectiveIds array is accepted as a no-op
- **WHEN** a player POSTs `{objectiveIds: []}`
- **THEN** the endpoint returns 200 and the profile state is unchanged

### Requirement: End-of-match persistence trigger

The client SHALL invoke the complete-objectives endpoint exactly once per match end, regardless of outcome (victory, defeat, or forfeit). The set of objective IDs submitted SHALL include only objectives whose `state.Completed == true && state.Failed == false` at the moment the match ends. Failed time-boxed objectives SHALL NOT be written to the profile. The legacy `markCampaignLevelComplete` endpoint SHALL continue to fire only on victory; it tracks the orthogonal "level beaten" signal.

#### Scenario: Forfeit writes completed objectives
- **WHEN** the player completes one optional objective and then forfeits via Exit Game
- **THEN** the complete-objectives endpoint is invoked with that objective's id, the profile records it, and the level is NOT marked beaten

#### Scenario: Defeat with no completions writes an empty list
- **WHEN** the player is defeated without completing any objective
- **THEN** the complete-objectives endpoint is invoked with `objectiveIds: []`, the profile state is unchanged, and the level is NOT marked beaten

#### Scenario: Victory writes both completions and the level-beaten signal
- **WHEN** the player wins after completing two objectives
- **THEN** the complete-objectives endpoint is invoked with both ids AND the existing `markCampaignLevelComplete` is invoked for the level

### Requirement: Replay starts at zero progress; profile aggregates all-time

For every campaign-launched match, the per-match objective runtime SHALL start with each objective's `current: 0, completed: false, failed: false` regardless of prior completions recorded on the profile. The level-select UI SHALL display completion status from the union of all-time completed objective IDs in the profile, NOT from the live per-match state. The end-of-match write SHALL merge new completions into the profile's existing set without overwriting prior completions.

#### Scenario: Replay shows fresh in-match progress
- **WHEN** a player has previously completed `clear_t1_camps` on Forest 1 and launches Forest 1 again
- **THEN** the in-match objectives panel shows `current: 0, completed: false` for `clear_t1_camps`

#### Scenario: Level select reflects all-time completion
- **WHEN** a player has previously completed `clear_t1_camps` on Forest 1
- **THEN** the Campaign panel's Forest 1 row shows `clear_t1_camps` with a `✓` icon

### Requirement: Level-select objective display

The Campaign panel SHALL render each level's objectives below the map preview when a level is selected. Each objective row SHALL show a checked icon (`✓`) when the objective id is present in the profile's `CompletedCampaignObjectives["<campaignId>/<levelId>"]` set, and an unchecked icon (`□`) otherwise. Required objectives SHALL be visually distinguished from optional objectives (e.g. by emphasis on the label).

#### Scenario: Completed and not-yet-completed objectives render distinctly
- **WHEN** a level has three objectives and the profile records one of them as completed
- **THEN** that objective row shows `✓` and the other two rows show `□`

#### Scenario: Level with no objectives shows an empty-state message
- **WHEN** the selected level's `objectives` array is empty
- **THEN** the panel renders a short empty-state message in place of an objectives list

### Requirement: In-match objective HUD

The match HUD SHALL render a campaign objectives panel only when the active session is a campaign session (`campaignSession` is non-null) AND the snapshot's `objectives` array is non-empty. The panel SHALL render one row per objective with a leading icon: `✓` for completed, `□` for in-progress, and `✗` with strikethrough text for failed. Each row SHALL show the objective description and a trailing `current / requiredCount` count. Required objectives SHALL retain their visual emphasis in this panel. The panel SHALL position to the top-right of the match HUD without overlapping the existing wave indicator (top-center) or resource tray.

#### Scenario: Custom Game hides the objective panel
- **WHEN** the active session is a Custom Game and the snapshot's `objectives` array is empty
- **THEN** the match HUD does not render the panel

#### Scenario: Campaign match shows the objective panel
- **WHEN** the active session is a campaign session and the snapshot has at least one objective
- **THEN** the panel renders below the resource tray with one row per objective

#### Scenario: Failed objective renders with strikethrough
- **WHEN** an objective's state is `failed: true`
- **THEN** the row shows the `✗` icon and the description text is rendered with a strikethrough decoration

### Requirement: Unified end-of-round recap overlay

The match view SHALL render a single overlay component to handle victory, defeat, and forfeit outcomes. The overlay SHALL display: a header reflecting the outcome (`Victory ★`, `Defeat`, or `Forfeit`); an objective recap section listing every objective with its final state and using the same icon scheme as the in-match panel; a per-player metrics section with one column per player in the lobby (the viewer's column annotated with a `*` indicator); and a single `Return to Menu` action that triggers the post-match cleanup flow. The overlay SHALL replace the existing separate victory and defeat overlays — no separate `victory-card` or `defeat-card` markup remains.

#### Scenario: Victory overlay renders with all sections
- **WHEN** the match enters the victory state
- **THEN** the overlay shows the Victory header, the objective recap, the per-player metrics columns, and the Return to Menu button

#### Scenario: Forfeit overlay renders with the same sections
- **WHEN** the player clicks Exit Game during an active match
- **THEN** the overlay shows the Forfeit header and the same recap and metrics sections

#### Scenario: Return to Menu writes objectives before navigation
- **WHEN** the player clicks Return to Menu from any of the three outcomes
- **THEN** the complete-objectives endpoint is invoked and awaited before the match view navigates to the main menu

### Requirement: Exit Game during a live match opens the recap as a forfeit

The match HUD's Exit Game affordance SHALL no longer perform a direct exit while a match is in progress. Instead, clicking Exit Game during an active match SHALL open the end-of-round recap overlay with the `forfeit` outcome. The recap's `Return to Menu` action SHALL be the canonical exit path that runs `exitGame()` and navigates to the main menu after the persistence write completes.

#### Scenario: Exit Game during play opens the recap
- **WHEN** the player clicks Exit Game while `hasStarted` is true and neither victory nor defeat has fired
- **THEN** the recap overlay opens with the Forfeit header and the previously-completed in-match objectives listed with their current state

#### Scenario: Exit Game after victory does not double-fire
- **WHEN** the player clicks Return to Menu after the victory recap has already opened
- **THEN** the existing persistence + navigation flow runs once, with no additional forfeit overlay shown
