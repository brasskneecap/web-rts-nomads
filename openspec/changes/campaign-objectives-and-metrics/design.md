## Context

The game's authoritative simulation lives in the Go server (`server/internal/game/`); all state mutates inside `GameState.Update(dt)` under a single lock, and a `MatchSnapshotMessage` is serialised per-viewer each tick and streamed to the Vue client (`client/src/game-portal/src/`). Win/lose detection today combines two systems:

- A short list of bespoke booleans on `GameState` (`victoryAchieved`, plus `objectiveKillCounts map[string]int` and `objectiveCompleted map[string]bool`) driven by per-map `VictoryCondition` rules of type `killUnit`, `destroyBuilding`, or `surviveWaves`. Map JSON authors objective IDs into spawnpoint/unit metadata, and `markObjectiveKillLocked` increments counts on death.
- The wave system (`tickWaveLocked` in `state_waves.go`) which reaches "complete" when all waves are cleared.

The client computes `ui.isVictory` and `ui.isDefeated` off this snapshot data. `Match.vue` renders a two-line overlay (victory or defeat) when these flip true. Campaign session state — `{campaignId, levelId, mapId}` — flows through a client-only ref (`state/campaignSession.ts`) so the victory watcher can fire `markCampaignLevelComplete()` against the profile store.

Persistence is per-player JSON files (`./profiles/<id>.json`) with forward-only schema migration. Schema is currently at v5 (added `CompletedCampaignLevels []string` in the most recent change). The campaign catalog is also data-driven: `//go:embed catalog/campaigns/*.json` in `campaign_defs.go`, served by `GET /api/catalog/campaigns`.

Constraints for this change:

- **Server is authoritative** (project rule, `AI_RULES.md`). The client renders snapshot data only; objective progress is computed server-side.
- **Tick determinism.** Anything plumbed into the tick path must avoid wall-clock time, unseeded RNG, or map iteration that drives outcomes. Objective evaluation uses counters and `GameState` fields only.
- **ID-based target references** (`AI_RULES.md`). Objective state carries IDs and strings, never `*Unit` / `*BuildingTile`.
- **Catalog-driven extensibility.** A new objective type must be addable by registering a handler — never by special-casing engine call sites.
- **No new I/O on the tick path.** Evaluators read pre-aggregated `Player.Metrics` and existing `GameState` fields. Persistence writes happen exactly once at match end.
- **No backwards-compatibility shims for VictoryCondition.** Per the project rule "Avoid backwards-compatibility hacks," this change removes the system outright and migrates authored data forward at catalog edit time. No coexistence path.

## Goals / Non-Goals

**Goals:**

- A single registry-based objective system that supersedes the existing `VictoryCondition` story.
- Six objective types covering the user-facing spec plus the migrated wave condition: `kill_camps`, `build_buildings`, `collect_resource`, `kill_camps_before_wave`, `rank_units`, `survive_waves`. Adding a seventh type means registering one handler.
- Per-player match metrics tracker streamed in the snapshot so every viewer can see every player's totals at end of round.
- Distinguish team-scope vs player-scope objectives, defaulting to team.
- Distinguish required vs optional objectives, defaulting to optional. Required gates victory in AND with the existing wave/townhall rule.
- Persistence keyed by campaign + level, written once at match end (any outcome, including forfeit).
- A single unified end-of-round overlay component for victory, defeat, and forfeit — same render path, different framing.
- "Exit Game during a live match" routes through the forfeit flow, not straight to the menu.

**Non-Goals:**

- A campaign / objective editor UI. Hand-authored JSON for this change; editor work is a separate proposal.
- Custom Game objectives. With map `VictoryConditions` removed, Custom Game has no objective system in this change.
- Optional / bonus rewards (legend points, items, perks) gated on completing optional objectives. The data model leaves room for `reward` payloads in a future change but does not implement them now.
- Star-rating UI on level rows (3 objectives → 3 stars). Future polish.
- Explicit "Forfeit" button on the pause menu. Exit Game is the only forfeit affordance in this change.
- Replay-from-completed semantics other than "achievement mode": the in-match panel always starts at zero progress; previously-completed objectives surface only via the profile's union of all-time completions when the panel renders.

## Decisions

### Decision: One generic registry + typed configs, mirroring `profile_upgrade_defs.go`

Objectives are loaded from `CampaignLevelDef.Objectives []ObjectiveDef` (JSON authored inside `catalog/campaigns/*.json`). Each `ObjectiveDef` carries an `id`, a `type` string used as the registry dispatch key, a human `description`, a `scope` enum, a `required` bool, and a `config json.RawMessage` parsed by the registered handler. A `map[string]objectiveHandler` registry in `objective_defs.go` provides three hooks per type:

- `parseConfig(raw) (any, error)` — turn the raw JSON into a typed config struct.
- `validate(filename, cfg)` — startup-time invariant check (panics on bad data, naming the offending file, identical to the upgrade catalog pattern).
- `evaluate(s *GameState, scopedPlayer *Player, cfg any, state *ObjectiveState)` — per-tick state update.

This decision intentionally mirrors `profileUpgradeEffectRegistry` so the codebase has one extension pattern for catalog-driven engine systems.

**Alternatives considered:**

- (a) **Reuse `VictoryCondition` by adding `kind`/`scope` fields.** Rejected. `VictoryCondition` is map-scoped, evaluated by ad-hoc switch statements in death/wave hooks, and binds win/lose to objective completion 1:1. Bolting a registry onto it preserves the awkward parts of the legacy design.
- (b) **One `ObjectiveDef` struct per type.** Rejected. A flat union explodes the catalog schema and forces every type to know about every other type's fields. `json.RawMessage` + per-type config struct is the same pattern the rest of the catalog uses for typed effects.
- (c) **Client-side evaluation off the existing UI snapshot.** Rejected per project rule "server is authoritative" — and required by the `kill_camps_before_wave` failure mode, which is timing-sensitive against `WaveManager.State`.

### Decision: `MatchMetrics` is per-player on `Player`, not per-match on `GameState`

Add `Metrics MatchMetrics` to the existing `Player` struct in `state.go`. Fields (all primitives or `map[K]int`):

```
type MatchMetrics struct {
    TotalGoldEarned             int
    TotalWoodEarned             int
    TotalEnemiesKilled          int
    BuildingsBuilt              int
    BuildingsBuiltByType        map[string]int
    NeutralCampsKilled          int
    NeutralCampsKilledByTier    map[int]int
    UnitsTrained                int
    UnitsTrainedByType          map[string]int
    UnitsByRank                 map[string]int   // recomputed; not a counter
    WavesCleared                int
}
```

Team-scope evaluators sum across `state.Players[].Metrics`. Player-scope evaluators read the viewer's own metrics. This keeps the metric source-of-truth single (per-player) and pushes aggregation into the evaluator, not the data layer.

`UnitsByRank` is intentionally a derived value, not a counter. The user's preferred semantic (Q5 in the design conversation) is "have N units currently at rank X or higher." It is recomputed in `onUnitRankUpLocked` and on rank-changing perk paths, not on a per-tick scan. If we later want cumulative rank-ups, we add a sibling `CumulativeRankUps map[string]int` without breaking existing types.

**Alternatives considered:**

- (a) **Match-level aggregate counters on `GameState`.** Rejected. Multiplayer campaigns lose per-player columns at end of round, and the "did Player A complete a player-scope build_buildings" question becomes a scan.
- (b) **Two structs — granular per-player + aggregate per-match.** Rejected as premature; team-scope evaluators summing across players is a tiny O(N players) loop, called at most a few times per tick.

### Decision: Scope = `"team" | "player"`, default `"team"`

A per-objective field. Team-scope objectives display the same progress to every viewer in the lobby and write to every player's profile if completed. Player-scope objectives compute against the viewer's own metrics and write to that viewer's profile only.

The scope of an objective also dictates which `ObjectiveSnapshot` the viewer sees:

```
For each viewer V:
  for each objective O:
    if O.scope == "team":  state = teamState[O.id]
    if O.scope == "player": state = perPlayerState[V.id][O.id]
```

`teamState` is computed once per tick; `perPlayerState` is computed for each viewer when their snapshot is built. The snapshot's `ObjectiveSnapshot[]` array is therefore per-viewer (different players in the same campaign lobby can see different progress on player-scope objectives, but identical progress on team-scope ones).

**Alternatives considered:**

- **Always-per-player scope.** Rejected; the user's example "kill X enemies" is naturally a team count, and forcing the author to write "kill X enemies, scope: player, count: 25" for a co-op lobby is unergonomic.
- **Always-team scope.** Rejected; "build X buildings" is meaningfully per-player.

### Decision: `required` bool, default `false`. Required gates victory in AND with the legacy wave/townhall rule

The existing victory rule (a townhall destroyed or all waves cleared, depending on the map's `winCondition`) continues to exist. The new rule layered on top:

```
isVictory := existing_rule_satisfied AND every_required_objective.completed
```

Optional objectives can be completed or failed without affecting victory. The author's mental model is "required = the win condition; optional = bonus, just for tracking."

The legacy `VictoryCondition` system is retired in the same change: the existing `killUnit` / `destroyBuilding` / `surviveWaves` rules that today gated wave-style victory get migrated by the catalog rewriter into the relevant campaign level as `required: true` entries.  `surviveWaves` migrates directly to the registered `survive_waves` handler (shipped in this change); `killUnit` / `destroyBuilding` flavours that do not have a 1:1 registered handler get rewritten as the closest equivalent (e.g. team kill counts re-expressed via the metrics-driven `kill_camps` handler when the targets were neutral camps). Any condition that cannot be cleanly re-expressed by the initial six handlers is enumerated in `catalog/maps/migration_notes.md` for follow-up — the migration intentionally does not invent extra handlers under deadline.

About the AND-gate with `survive_waves` registered: a campaign level that wants "win by surviving N waves" expresses this as a `survive_waves` required objective AND a map with `totalWaves = N`. Both must hold for victory to fire. Authoring `wavesToSurvive < map.totalWaves` is an author error (the objective completes early, but the legacy rule still gates the actual win moment); the loader does not validate this cross-reference today.

**Alternatives considered:**

- **OR-gate (required objectives complete OR the wave/townhall condition).** Rejected; lets the player skip the required objective and still win.
- **Required = the only win condition (no legacy fallback at all).** Rejected. While `survive_waves` covers the most common migration case, some maps express victory via townhall destruction or other conditions for which no registered handler exists yet. Removing the legacy fallback would force us to invent handlers under migration deadline, against the project rule "Don't design for hypothetical future requirements." The AND-gate stays; a future change can introduce `destroy_buildings` and collapse the fallback as authoring conventions catch up.

### Decision: Replay is achievement mode

The in-match objective panel always starts at zero current progress, even if the player has previously completed an objective. The profile aggregates the union of completed-objective IDs across all attempts (per `<campaignId>/<levelId>` key).

This is the roguelike-flavoured choice: each attempt is its own run, the "achievement" sticks to the profile. The level-select UI takes its display from the profile (✓ shows for any objective ever completed in any prior attempt); the in-match HUD shows the live state of this attempt only.

A side benefit: no schema is needed to remember "you completed this in attempt #3 vs #4" — the profile only stores the set of objective IDs ever completed.

**Alternatives considered:**

- **Checkpoint mode.** In-match panel renders previously-completed objectives as already done; the player can skip them. Rejected; it makes replays trivially short and removes the player's incentive to engage with completed objectives.

### Decision: Map `VictoryConditions` is removed outright, conditions migrate into campaign level JSON

`MapConfig.VictoryConditions` and the in-tree handlers (`markObjectiveKillLocked`, `objectiveKillCounts`, `objectiveCompleted`, the related branches in `tickWaveLocked` and the death pipeline) are deleted. A one-shot migration walks every authored map's `victoryConditions`, finds the campaign levels that reference that map, and folds the conditions into the campaign level's `objectives` array. Conditions on maps not referenced by any campaign are dropped (recorded to a one-time `migration_notes.md` for review).

The map editor's "Victory Conditions" card is removed in the same change. Authoring objectives now happens at the campaign level (hand-edited JSON in this change; a campaign editor view is a separate proposal).

**Alternatives considered:**

- **Coexist with `VictoryCondition`, migrate later.** Rejected per the user's direct answer ("Map objectives should go away. We will now use campaign objectives as the go to."). Coexistence would require evaluating two systems each tick and keep the legacy system on life support for an indeterminate window.
- **Keep `VictoryCondition` for Custom Game.** Rejected. Custom Game in this change runs without objectives. If we later want them, the objective system already exists and the lobby creation flow can attach an objective set.

### Decision: Exit Game during a live match = forfeit, opens the recap overlay

Today, `Match.vue`'s Exit Game button calls `exitGame()` which leaves the match, clears state, and navigates to `/`. After this change, the same button instead opens the unified `MatchEndRecap` overlay with a "Forfeit" header; the recap's Return-to-Menu button is the path that calls `exitGame()` and writes completed objectives.

Defeat and victory overlays merge into the same `MatchEndRecap` component (header text differs: "Victory ★" / "Defeat" / "Forfeit"). The recap shows the objective list and per-player metric columns; clicking Return-to-Menu always triggers the batch `markCampaignObjectivesComplete()` write before navigation.

Persistence happens on every outcome (victory, defeat, forfeit). Only objectives whose `state.completed == true && state.failed == false` are written. Failed time-boxed objectives — `kill_camps_before_wave` whose deadline passed — are surfaced in the recap with `✗ strikethrough` but never written to the profile.

**Alternatives considered:**

- **Three different overlay components.** Rejected; the layout is identical except for the header.
- **Persistence on victory only.** Rejected per the user's direct answer (Q3, "Objectives that are completed before defeat are tracked"). Tracking partial progress on losses is a meaningful retention signal for a roguelike.

### Decision: Profile schema v5 → v6, batch endpoint for objective writes

Add `CompletedCampaignObjectives map[string][]string` to `PlayerProfile`. Key is `"<campaignId>/<levelId>"`; value is a sorted, deduped set of objective IDs. The forward migration in `store.go` initialises the field to an empty map for v5 profiles.

The endpoint is `POST /api/profile/campaign/complete-objectives` with body `{campaignId, levelId, objectiveIds: string[]}`. The server merges into the sorted set (idempotent — re-completing yields the same final state). One call per match end is enough; per-objective writes during a match would be I/O on the (already authoritative) tick path and could lose work on disconnect.

The legacy `markCampaignLevelComplete` endpoint stays for the "level beaten" signal — orthogonal to objective completion. A player can complete objectives during a run that ends in defeat and still not have the level marked beaten; the two write paths fire independently.

**Alternatives considered:**

- **Per-objective endpoint, called as each completes.** Rejected; busy work for transient state, lossy on disconnect, and the snapshot already gives the client live progress without writing to disk.
- **One JSON blob keyed by campaignId, value = `{levelId: objectiveIds[]}`.** Rejected for ergonomics — flat `"campaignId/levelId"` keys are easier to migrate forward and easier to render in the level-select UI (one Set lookup per row).

### Decision: Snapshot extension is additive on `Players[]` and re-shapes `Victory.Objectives[]`

`MatchSnapshotMessage.Players[i]` gains a `metrics: MatchMetrics` field. Existing fields untouched; old clients ignoring the new field is harmless.

`Victory.Objectives[]` (already on the snapshot today as a flat array of `ObjectiveSnapshot`) is repurposed: same field name, new shape. Today's snapshot carries `{id, type, label, completed, progress, count}`. The new shape carries `{id, type, description, scope, required, current, required, completed, failed}` — strictly richer. The legacy fields `label` (now `description`), `progress` (now `current`), `count` (now `required`) are renamed in the same PR; no client outside this change reads them.

Per-viewer differences for player-scope objectives mean the snapshot has to be built per-viewer (as it already is via `MarshalSnapshotForPlayer`). Team-scope objectives compute once per tick and copy.

## Risks / Trade-offs

- **Risk:** Migrating map `victoryConditions` mid-flight could lose authored test setups. → **Mitigation:** The catalog rewriter writes a `migration_notes.md` summarising every deleted/moved condition by map ID, reviewed before the PR lands. Existing tests are updated to author objectives against the new shape in the same commit.
- **Risk:** Per-viewer player-scope objectives expand snapshot CPU work. → **Mitigation:** Each viewer's per-player state is at most `O(player_scope_objectives)` work — typically <10 per level. The aggregate per-tick cost is dwarfed by existing unit/projectile serialisation.
- **Risk:** `kill_camps_before_wave` evaluators flicker into `failed` if wave state momentarily looks active during a single tick. → **Mitigation:** Failure is sticky; once `state.Failed = true`, the evaluator returns immediately on subsequent ticks. The flicker can only set the bit; it cannot unset it.
- **Risk:** Profile write at forfeit may race with `exitGame()`'s nav. → **Mitigation:** The recap's Return-to-Menu handler awaits the batch write before calling `exitGame()`. Failure to write surfaces as an error toast on the recap; the user can retry without losing the recap data.
- **Risk:** Existing campaign saves carry `CompletedCampaignLevels` populated but `CompletedCampaignObjectives` empty after migration. → **Acceptance:** This is the intended behaviour. The player has beaten the level historically (so the level-select check still shows them as completed) but didn't complete any objectives historically (because objectives didn't exist). They can replay to earn the objective ticks.
- **Trade-off:** Five hard-coded objective types in the initial registry means every gameplay flavour we want needs a registered handler. → **Acceptance:** The registry pattern is the right level of extensibility — it forces a typed handler per type rather than a generic JSON DSL that's hard to read and lint. The slope from "ship a new type" to "lots of new types" is one handler each.
