## Why

Campaign levels today are run-and-forget: a level points at a map, the player wins or loses against the map's hard-coded victory conditions, and the only thing persisted is "this level was beaten." There is no way to author per-level goals beyond the map's victory rule, no way to read what the player accomplished after the match, and no way to compare players in a multiplayer campaign. The placeholder "objectives" list rendered on the Campaign panel today (`Task 1`, `Task 2`, `Task 3`) reflects that gap.

Separately, the existing engine has a `VictoryCondition` system (`killUnit`, `destroyBuilding`, `surviveWaves`) attached to map JSON. It works but it is map-scoped: the same map can't be reused with different goals, custom games and campaigns share the same evaluation rules, and the system can only express "complete this and win" — not "do this for credit, complete it whenever, optional bonus on top of the win." The user spec explicitly asks for objectives that can be optional, that can fail (time-boxed), that can be team or per-player, and that persist independently of beating the level.

This change replaces the map-scoped `VictoryCondition` system with a richer campaign-level objective system, adds a per-player match metrics tracker, and wires both into the level select, in-match HUD, and a unified end-of-round recap that also handles forfeits.

## What Changes

### Server (Go)

- Introduce a generic **match objective** system: catalog-driven definitions on `CampaignLevelDef.Objectives`, a typed registry of objective handlers, and per-tick evaluation that produces sticky completion/failure state.
- Ship six objective types in the initial registry: `kill_camps`, `build_buildings`, `collect_resource`, `kill_camps_before_wave`, `rank_units`, `survive_waves`. Each declares a `scope` (`"team"` or `"player"`, default `"team"`) and `required` (default `false`). `survive_waves` is included so wave-completion victory conditions migrated from the legacy `surviveWaves` map condition flow through the registry from day one.
- Add a **match metrics** tracker per player (`Player.Metrics`): cumulative gold/wood earned, enemies killed, buildings built by type, neutral camps killed by tier, units trained by type, units by current rank, waves cleared. Metrics are bumped by one-line hooks in the existing event paths (deposit, building complete, kill, camp cleared, rank-up, wave done).
- Remove `MapConfig.VictoryConditions`. Existing victory rules that were attached to maps used by campaigns are migrated into the corresponding campaign level's `objectives` array (as `required: true`, `scope: "team"` entries). Unreferenced map victory conditions are dropped — the maps move to a "no objectives" Custom Game posture.
- Extend the match victory rule: a player wins when the existing wave/townhall condition is met **AND** every objective marked `required` has completed. Optional objectives never gate victory.
- Extend the UI snapshot (`MatchSnapshotMessage`):
  - Add per-player `metrics` to the existing `Players[]` sub-state so every player can see every other player's totals (for the end-of-round comparison).
  - Replace the existing `Victory.Objectives[]` shape with a richer `ObjectiveSnapshot` carrying `scope`, `required`, `current`, `required` count, `completed`, `failed`, plus a human `description`.
- Bump the profile schema from v5 to v6:
  - Add `CompletedCampaignObjectives map[string][]string` to `PlayerProfile` (key = `"<campaignId>/<levelId>"`, value = sorted-set of completed objective IDs).
  - Forward migration initialises the field to an empty map for existing profiles.
- Add `POST /api/profile/campaign/complete-objectives` (batch): body `{campaignId, levelId, objectiveIds: string[]}`, idempotent, merges into the sorted set.

### Client (TS / Vue 3)

- Mirror the new `Objective` / `ObjectiveProgress` / `MatchMetrics` types in `client/src/game-portal/src/types/`.
- `Campaign.vue`: replace the placeholder objectives list with real per-level data. Each selected level shows its objectives below the map; objectives the player has previously completed render with a `✓`, never-completed render with a `□`, failed time-boxed objectives (live in-match only) render with an `✗` and strikethrough.
- New `MatchObjectivesPanel.vue`: in-match HUD, top-right under the resource tray, lists active campaign objectives with live current/required counts and the same `✓` / `□` / `✗` treatment.
- New `MatchEndRecap.vue`: unified victory/defeat/forfeit overlay. Replaces the current pair of single-line cards in `Match.vue`. Sections: header (Victory / Defeat / Forfeit), objective recap, per-player metrics columns side-by-side, exit button. Always opens when the match ends, regardless of outcome.
- `Match.vue` Exit Game wiring: clicking Exit Game during a live match triggers the forfeit flow — show the recap (with Forfeit framing), then `exitGame()` on the user dismissing it.
- On match end (victory, defeat, or forfeit), the client batches the list of objectives marked `completed: true` (NOT `failed`) and calls `markCampaignObjectivesComplete()` before navigating away. The legacy `markCampaignLevelComplete` still fires on victory only.

### Catalog data

- Populate `server/internal/game/catalog/campaigns/forest.json` with real objectives across the three Forest levels — a mix of required (the wave/townhall goal that today lives on the map), optional team objectives, and optional player objectives. Swamp stays an empty `levels: []` placeholder.
- Strip `victoryConditions` from every `server/internal/game/catalog/maps/*.json`. Conditions on maps referenced by a campaign level move into that level's `objectives`; conditions on maps not referenced (most of them) are dropped, with their JSON snapshots archived in a one-off `migration_notes.md` for review.

## Capabilities

### New Capabilities

- `match-objectives` — generic catalog-driven match-time objective system: type registry, per-tick evaluation, scope (team / player), required vs optional, sticky completion / failure, per-player match metrics, snapshot exposure.
- `campaign-progression` — campaign-level integration: objectives field on `CampaignLevelDef`, profile persistence of completed objectives by campaign + level, level-select objective display, in-match objective HUD, unified victory / defeat / forfeit end-of-round recap, Exit Game = forfeit flow.

### Modified Capabilities

<!-- No existing OpenSpec capability versions cover map victory conditions or campaign progression today — both arrive as new capabilities. The behavioural removal of `MapConfig.VictoryConditions` is captured under `match-objectives` (the system that supersedes it) rather than as a delta on an absent map-progression spec. -->

## Impact

- **Server (Go):**
  - `server/internal/game/match_metrics.go` (new) — `MatchMetrics` struct, helpers.
  - `server/internal/game/objective_defs.go` (new) — types, registry, JSON loader, validation.
  - `server/internal/game/objective_handlers.go` (new) — 5 evaluators + validators.
  - `server/internal/game/campaign_defs.go` — extend `CampaignLevelDef` with `Objectives []ObjectiveDef`; validate at catalog load.
  - `server/internal/game/state.go` — `Player.Metrics`; objective state slice on `GameState`; updated `snapshotForPlayerLocked`; extended victory rule.
  - `server/internal/game/damage_pipeline.go`, `state_workers.go`, `state_buildings.go`, `state_neutral_camps.go`, `state_waves.go`, `progression.go` — one-line metric hooks each.
  - `server/internal/game/catalog/campaigns/forest.json` — real objectives.
  - `server/internal/game/catalog/maps/*.json` — strip `victoryConditions`.
  - `server/internal/profile/types.go` — `CompletedCampaignObjectives`, bump `CurrentVersion` 5→6.
  - `server/internal/profile/store.go` — v5→v6 migration.
  - `server/internal/http/profile_handlers.go` — `POST /api/profile/campaign/complete-objectives`.
- **Client (Vue / TS):**
  - `client/src/game-portal/src/types/campaign.ts`, `types/profile.ts` — type updates.
  - `client/src/game-portal/src/services/profileApi.ts` — `markCampaignObjectivesComplete`.
  - `client/src/game-portal/src/views/Campaign.vue` — bind real objectives to selected level.
  - `client/src/game-portal/src/views/Match.vue` — mount panel + recap, wire Exit-as-forfeit, end-of-match batch write.
  - `client/src/game-portal/src/components/match/MatchObjectivesPanel.vue` (new).
  - `client/src/game-portal/src/components/match/MatchEndRecap.vue` (new).
- **Protocol wire format:** `Players[i]` gains `metrics`; `Victory.Objectives[]` shape changes (additive on the wire — old clients ignore new fields, but the existing fields `id`, `completed`, `progress` remain). Map JSON loses `victoryConditions` (server-validated).
- **Invariants:** no new `*Unit`/`*BuildingTile` fields on persisted structs; no new I/O on the tick path (objective evaluation reads `Player.Metrics` and `GameState`, never the catalog or profile store); no client-side simulation introduced (the new objective panel renders snapshot fields only).

## Out of Scope

- **Campaign / objective editor UI.** The map editor's existing "Victory Conditions" card is removed (since map victory conditions are removed). A campaign-level objectives editor is a separate change ("editor: campaign objective authoring") that ships against a different surface area. For this change, objectives are hand-authored in `catalog/campaigns/*.json`.
- **Custom Game objectives.** With map victory conditions removed, Custom Game runs without objectives in this change. A future change can add a "select an objective set when creating a custom lobby" flow.
- **Migrating legacy `VictoryCondition` handlers into the new registry.** The old `markObjectiveKillLocked` path and its sibling counters are deleted outright — there are no remaining authored callers after the catalog migration. This is not a coexistence story; it is a replacement.
- **Per-objective explicit forfeit endpoint.** Forfeit is implicit (Exit Game during a live match). An explicit "Forfeit" button on the pause menu can come later if desired.
- **Achievement / star-rating UI.** Counting completed objectives per level toward a star rating is a future polish change.
