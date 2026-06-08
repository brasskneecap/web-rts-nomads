## 1. Profile schema migration (v5 → v6)

- [x] 1.1 In `server/internal/profile/types.go`, add `CompletedCampaignObjectives map[string][]string` to `PlayerProfile`. Bump `CurrentVersion` from 5 to 6.
- [x] 1.2 In `server/internal/profile/store.go`'s `migrateProfile`, add a v5→v6 step that initialises `CompletedCampaignObjectives` to a non-nil empty map. Stamp `p.Version = CurrentVersion`.
- [x] 1.3 In `server/internal/profile/manager.go`'s `newDefaultProfile`, seed `CompletedCampaignObjectives: map[string][]string{}`.
- [x] 1.4 Add a test that loads a hand-rolled v5 JSON fixture and asserts `CompletedCampaignObjectives` is non-nil and empty after read, and that subsequent `WithLocked` writes the file as v6.

## 2. Match metrics (`MatchMetrics` struct + event hooks)

- [x] 2.1 Create `server/internal/game/match_metrics.go` declaring `MatchMetrics` with the fields listed in `design.md`. Provide a `NewMatchMetrics()` helper that returns a value with all maps initialised to non-nil empty maps.
- [x] 2.2 In `server/internal/game/state.go`, add `Metrics MatchMetrics` to the `Player` struct and initialise it via `NewMatchMetrics()` in every player-construction path.
- [x] 2.3 In `state_workers.go` `completeReturnDepositLocked`, after the resource deposit, bump the owner's `Metrics.TotalGoldEarned` or `TotalWoodEarned` by the deposited amount.
- [x] 2.4 In `state_buildings.go` (where building HP first reaches `maxHp` during construction — extend `chargeBuildingRepairLocked` or its sibling that detects the transition), bump owner's `Metrics.BuildingsBuilt` and `Metrics.BuildingsBuiltByType[buildingType]`.
- [x] 2.5 In `damage_pipeline.go` `drainPendingDeathsLocked`, on every confirmed kill where attacker and victim are on different teams, bump attacker-owner's `Metrics.TotalEnemiesKilled`.
- [x] 2.6 In `state_neutral_camps.go` `onUnitRemovedFromCampLocked`, when the camp transitions to cleared (i.e., the path that today calls `maybeDropChestForCampLocked`), bump each player's `Metrics.NeutralCampsKilled` and `Metrics.NeutralCampsKilledByTier[camp.CurrentTier]`. (Camp clears are team-scope for award; the increment applies to every player in the team that landed the killing blow.)
- [x] 2.7 In `state_waves.go` `tickWaveLocked`, when the wave state transitions out of `"active"` into `"upgrade"` or `"complete"`, bump every team-aligned player's `Metrics.WavesCleared`.
- [x] 2.8 In `progression.go` `onUnitRankUpLocked`, after `unit.Rank` updates, recompute the owner's `Metrics.UnitsByRank` map (count alive units at each rank). Also fire a one-time bump on unit-spawn into `Metrics.UnitsTrained` and `Metrics.UnitsTrainedByType[unitType]`.
- [x] 2.9 Add unit tests covering: a deposit increments the right counter; a kill increments only the attacker-owner's count, never the victim's; a camp clear increments by tier; a wave-complete event bumps `WavesCleared` exactly once.

## 3. Objective definitions & registry

- [x] 3.1 Create `server/internal/game/objective_defs.go` with `ObjectiveDef` (id, type, description, scope, required, config raw), `ObjectiveScope` enum (`ObjectiveScopeTeam`, `ObjectiveScopePlayer`), `ObjectiveState` (objectiveId, scope, current, required, completed, failed), and the `objectiveHandler` struct (`parseConfig`, `validate`, `evaluate`).
- [x] 3.2 In the same file, declare `objectiveRegistry = map[string]objectiveHandler{}` and a `registerObjective(typeKey, handler)` helper. Provide `GetObjectiveHandler(typeKey)` for catalog load and a `ListObjectiveTypes()` returning sorted keys (used by client-side schema discovery in a later editor change).
- [x] 3.3 Wire startup-time validation: every `ObjectiveDef` loaded from a `CampaignLevelDef` must have a registered `type`, a parseable `config`, and pass the handler's `validate`. On failure, panic with the file path, level id, and the offending objective id.
- [x] 3.4 Add `scope` parsing: missing → `ObjectiveScopeTeam`; explicit `"team"` or `"player"` accepted; any other value panics with the file path and objective id.
- [x] 3.5 Add `required` parsing: missing → `false`.

## 4. Objective handlers (the six initial types)

- [x] 4.1 Create `server/internal/game/objective_handlers.go`. For each handler, the `evaluate` function early-returns when `state.Completed || state.Failed` — those bits are absorbing.
- [x] 4.2 Implement `kill_camps`. Config: `{campTier?: int, count: int}`. `count` must be > 0; `campTier` if present must be ≥ 1. Evaluation reads `metrics.NeutralCampsKilled` (when `campTier` omitted) or `metrics.NeutralCampsKilledByTier[campTier]`. `state.Current = readValue`; on `state.Current >= cfg.Count`, set `state.Completed = true`.
- [x] 4.3 Implement `build_buildings`. Config: `{buildingType: string, count: int}`. `count > 0`; `buildingType` must be a known building catalog entry. Read `metrics.BuildingsBuiltByType[buildingType]`. Complete on `>= cfg.Count`.
- [x] 4.4 Implement `collect_resource`. Config: `{resource: "gold"|"wood", amount: int}`. `amount > 0`; resource must be one of the two literals. Read `metrics.TotalGoldEarned` or `metrics.TotalWoodEarned`. Complete on `>= cfg.Amount`.
- [x] 4.5 Implement `kill_camps_before_wave`. Config: `{campTier?: int, count: int, beforeWave: int}`. `count > 0`, `beforeWave > 0`. Evaluation reads camp count (with optional tier filter); completes on `>= cfg.Count`. Failure: if `state.Completed == false` AND `s.WaveManager.CurrentWave >= cfg.BeforeWave` AND `s.WaveManager.State == "active"`, set `state.Failed = true`.
- [x] 4.6 Implement `rank_units`. Config: `{rank: "bronze"|"silver"|"gold", count: int}`. `count > 0`. Read `metrics.UnitsByRank[rank]`. Complete on `>= cfg.Count`. (Document in a comment: "currently-at-or-above-rank, not cumulative rank-ups.")
- [x] 4.7 Implement `survive_waves`. Config: `{wavesToSurvive: int}`. `wavesToSurvive` must be > 0. Read `metrics.WavesCleared`. Complete on `>= cfg.WavesToSurvive`. Document that this handler is naturally team-scope (waves are team-wide), and that authoring it as `required: true` is how a campaign level expresses a wave-completion victory condition through the registry. The legacy wave/townhall victory rule still ANDs with this — see Decision in `design.md`.
- [x] 4.8 Register each handler in an `init()` block in `objective_handlers.go` so the registry is wired before any catalog load runs.
- [x] 4.9 Tests per handler: valid configs parse; invalid configs panic at validation; an in-progress state completes when threshold is met; a failed `kill_camps_before_wave` stays failed across subsequent ticks; a completed objective stays completed regardless of metric drops; a `survive_waves` objective with `wavesToSurvive: 3` completes the tick `WavesCleared` reaches 3 (not before).

## 5. Catalog load: objectives on `CampaignLevelDef`

- [x] 5.1 In `server/internal/game/campaign_defs.go`, extend `CampaignLevelDef` with `Objectives []ObjectiveDef \`json:"objectives,omitempty"\``. Normalise nil to an empty slice on load.
- [x] 5.2 In the campaign loader (`loadCampaignDefs`), after parsing each level, walk `Objectives` and run handler validation per task 3.3. Reject duplicate objective IDs within a level.
- [x] 5.3 Add tests: a campaign JSON with a valid mix of objectives loads cleanly; duplicate objective IDs panic; unknown type panics; missing required handler field panics.

## 6. Catalog migration: Forest level objectives + map `victoryConditions` removal

- [x] 6.1 Survey every `server/internal/game/catalog/maps/*.json` for a `victoryConditions` field. For each, note (a) the conditions present and (b) whether any campaign level in `catalog/campaigns/*.json` references that map.
- [x] 6.2 Author `server/internal/game/catalog/campaigns/forest.json` objectives:
   - **forest_01**: required team objective derived from the existing map win rule; one optional team objective and one optional player objective for testing the new categories.
   - **forest_02**, **forest_03**: same pattern, content chosen to exercise `kill_camps_before_wave` and `rank_units` on at least one level so all five handlers see live use.
- [x] 6.3 Strip the `victoryConditions` field from every map JSON file. Write `server/internal/game/catalog/maps/migration_notes.md` listing every map and every condition removed, classified as "moved → <campaign>/<level>" or "dropped — no campaign reference."
- [x] 6.4 Update `protocol.MapConfig` to remove the `VictoryConditions` field; update every reference in Go and TypeScript so the compile passes.
- [x] 6.5 Update the map editor's `MapEditorPanel.vue` to remove the Victory Conditions card. (Adding a campaign objectives card is out of scope for this change.)

## 7. Match-start: instantiate live objective state from the campaign level

- [x] 7.1 Extend the match-start path (`GameState` construction or the `match` package's lobby→game handoff) so a campaign-launched match receives the `CampaignLevelDef.Objectives` list.
- [x] 7.2 On `GameState`, add `objectives []objectiveRuntime` where each runtime carries the `ObjectiveDef`, the parsed config, a per-team `ObjectiveState`, and (if `scope == "player"`) a `map[playerID]ObjectiveState`.
- [x] 7.3 Initialise `state.Required` from the handler's config (`count` or `amount`).
- [x] 7.4 For non-campaign matches (Custom Game, find-game flow), the objectives slice is empty — the rest of the pipeline tolerates an empty slice.

## 8. Tick-level evaluation

- [x] 8.1 In `GameState.Update(dt)`, after metric-bumping subsystems have run and before the snapshot is built, call `evaluateObjectivesLocked()`.
- [x] 8.2 `evaluateObjectivesLocked()` iterates `state.objectives`. For team-scope objectives, build a transient `teamMetrics MatchMetrics` (sum across all `state.Players[].Metrics`) and call `handler.evaluate(s, nil, cfg, &teamState)`. For player-scope, call `handler.evaluate(s, &player, cfg, &playerState[p.ID])` per player.
- [x] 8.3 Enforce monotonicity at the entry of every evaluator call: if `state.Completed || state.Failed`, no-op.

## 9. Victory rule extension

- [x] 9.1 Where the existing victory check sets `victoryAchieved` (in `state.go` or its sibling that today honours `MapConfig.VictoryConditions`), replace the legacy branch with: `victoryAchieved = waveOrTownhallConditionMet && allRequiredObjectivesCompleted()`.
- [x] 9.2 Add `allRequiredObjectivesCompleted()` that walks `state.objectives`, ignores `required == false`, and returns false the moment it sees one with `state.Completed == false`.
- [x] 9.3 Delete `state.objectiveKillCounts`, `state.objectiveCompleted`, `markObjectiveKillLocked`, and anywhere `metadata["objectiveId"]` was read from map JSON to drive the legacy counter.
- [x] 9.4 Add a test: a campaign level with one required `survive_waves` objective (`wavesToSurvive` equal to the map's `totalWaves`) and one optional `kill_camps` objective. Verify victory fires only when both the legacy wave/townhall rule is satisfied AND the required `survive_waves` objective is complete (which line up by construction here), and that completing/not-completing the optional camps objective never blocks victory.

## 10. Snapshot extension

- [x] 10.1 In the `protocol` package, add `metrics MatchMetrics` to the per-player snapshot type. Add the richer `ObjectiveSnapshot` shape: `{id, type, description, scope, required, current, requiredCount, completed, failed}`.
- [x] 10.2 In `MarshalSnapshotForPlayer` / `snapshotForPlayerLocked`, populate per-player `metrics`. Populate `objectives []ObjectiveSnapshot` from `state.objectives`: team-scope objectives copy `teamState`; player-scope objectives copy the viewer's own `playerState`.
- [x] 10.3 Remove the legacy `Victory.Objectives[]` fields `label`, `progress`, `count`. (No external client reads them.)
- [x] 10.4 Add a test that a multiplayer campaign snapshot for Player A shows Player A's player-scope objective state, AND that the same snapshot includes Player B's `metrics` block at end of round shape.

## 11. HTTP endpoint: complete-objectives

- [x] 11.1 In `server/internal/http/profile_handlers.go`, register `POST /api/profile/campaign/complete-objectives`. Body: `{campaignId: string, levelId: string, objectiveIds: []string}`. Header: `X-Player-ID`.
- [x] 11.2 Validate: `campaignId` non-empty, `levelId` non-empty, `objectiveIds` non-nil (may be empty). On invalid body, return 400 with `error: "invalid_body"`.
- [x] 11.3 Inside `pm.WithLocked`, set `key := campaignId + "/" + levelId`; merge `objectiveIds` into the sorted-set value at `p.CompletedCampaignObjectives[key]` using the existing `addToSortedSet` helper. Idempotent.
- [x] 11.4 Return the updated profile.
- [x] 11.5 Tests: empty objectiveIds → no-op write; repeat call with the same IDs → state unchanged; new IDs merge into existing set; missing player ID header rejected.

## 12. Client types and API

- [x] 12.1 In `client/src/game-portal/src/types/campaign.ts`, add `Objective`, `ObjectiveScope`, `ObjectiveProgress` types matching the server schema. Extend `CampaignLevel` with `objectives?: Objective[]` (optional in JSON; loader treats absent as `[]`).
- [x] 12.2 In `client/src/game-portal/src/types/profile.ts`, add `completedCampaignObjectives: Record<string, string[]>` to `PlayerProfile`.
- [x] 12.3 In `client/src/game-portal/src/services/profileApi.ts`, add `markCampaignObjectivesComplete(campaignId, levelId, objectiveIds)`.
- [x] 12.4 In `client/src/game-portal/src/game/network/protocol.ts` (or sibling type file), add the new `ObjectiveSnapshot` shape and `MatchMetrics` mirror.

## 13. Campaign view: render real per-level objectives

- [x] 13.1 In `client/src/game-portal/src/composables/useCampaign.ts`, expose per-level completion status using the profile's `completedCampaignObjectives` set keyed by `"<campaignId>/<levelId>"`.
- [x] 13.2 In `client/src/game-portal/src/views/Campaign.vue`:
   - Delete the `placeholderObjectives` constant.
   - Replace the placeholder list rendering with a list driven by `selectedLevelView.level.objectives`.
   - Each row shows `✓` for any objective ID in the profile's completed set, `□` otherwise. Required objectives use a distinctive (e.g. underlined) label; optional are plain.
   - Show an empty-state message when the level has no objectives ("No objectives for this level").

## 14. In-match objective HUD panel

- [x] 14.1 Create `client/src/game-portal/src/components/match/MatchObjectivesPanel.vue`. Reads `ui.objectives` (the per-tick snapshot list).
- [x] 14.2 Render rows: leading icon `✓`/`□`/`✗` per `state.completed`/`state.failed`/in-progress, description, trailing `current / requiredCount`. Failed rows render with a strikethrough; required rows keep an emphasis treatment.
- [x] 14.3 In `client/src/game-portal/src/views/Match.vue`, mount the panel under the existing resource tray (top-right) only when the active session is a campaign session (`campaignSession.value != null`) AND `ui.objectives.length > 0`. Custom Game does not show the panel.

## 15. Unified end-of-round recap overlay

- [x] 15.1 Create `client/src/game-portal/src/components/match/MatchEndRecap.vue`. Props: `outcome: 'victory' | 'defeat' | 'forfeit'`, `objectives: ObjectiveSnapshot[]`, `players: PlayerSnapshotWithMetrics[]`, `viewerId: string`. Emits `close`.
- [x] 15.2 Layout: header (Victory ★ / Defeat / Forfeit), objective recap section (same icon treatment as the in-match panel; failed objectives stay `✗ strikethrough`), per-player metrics columns (one column per player in the lobby; the viewer's column is annotated `*`), and a single Return-to-Menu button that emits `close`.
- [x] 15.3 In `Match.vue`, delete the existing `.victory-overlay` and `.defeat-overlay` markup. Replace with one `<MatchEndRecap>` rendered when `endOfMatch != null`. `endOfMatch` is a reactive computed: `'victory'` when `isVictorious`, `'defeat'` when `ui.isDefeated`, `'forfeit'` when the player clicked Exit Game during a live match.
- [x] 15.4 The `@close` handler is the canonical post-match exit: await the batch `markCampaignObjectivesComplete()` write (silent on success, surface an error toast on failure), then call the existing `exitGame()` to leave the match and navigate.

## 16. Exit-Game = forfeit wiring

- [x] 16.1 In `Match.vue`'s `MatchHud @exit="exitGame"` binding, change the bound handler to a new `requestForfeit()` that sets a local `forfeitRequested = true` ref (which feeds into the `endOfMatch` computed).
- [x] 16.2 The recap's `@close` handler is now the only call site for the actual `exitGame()` (which leaves the lobby, destroys the client, and navigates to `/`). The legacy direct-exit path from the HUD menu is removed.
- [x] 16.3 Update the disconnect overlay path: when the player clicks `Return to Menu` from a failed reconnect, route through the same recap-then-exit flow so any earned objectives are still written.

## 17. End-to-end verification

- [ ] 17.1 `go build ./...` and `go test ./...` from `server/` pass.
- [ ] 17.2 `npx vue-tsc --noEmit` from `client/src/game-portal` passes.
- [ ] 17.3 Manual play: launch Forest 1 in single-player; verify the in-match panel shows the level's objectives with live progress; complete the required objective; the recap overlay appears with the correct outcome; the profile (via `GET /api/profile`) now contains the completed objective IDs under `"forest/forest_01"`.
- [ ] 17.4 Manual play: launch Forest 1, click Exit Game mid-match; verify the recap opens with `Forfeit` framing and that the partial completions ARE written to the profile after dismissing the recap.
- [ ] 17.5 Manual play: launch Forest 1 a second time after completing some objectives in run 1; verify the in-match panel starts at zero progress (achievement mode); verify the Campaign panel level row shows the previously-completed objectives as `✓` from the profile.
- [ ] 17.6 Manual play: launch a Custom Game on `exploration.json`; verify no in-match objective panel renders and no recap-time profile write happens.
