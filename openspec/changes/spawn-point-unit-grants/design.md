## Context

Today there are three distinct paths that put a player-owned unit on the map at match start:

1. **Authored placed units** — `spawnPlacedUnitsForPlayerLocked` reads `MapConfig.PlacedUnits` and spawns each at its authored grid cell.
2. **Spawn-point authored units** — the `spawn-point` building's `metadata.spawnUnits` is consumed by the existing spawn-point spawn pipeline (already anchored to the spawn-point building).
3. **Extra starting workers from upgrades** — `spawnExtraStartingWorkersLocked` reads `Player.ExtraStartingWorkers int` and spawns each at the nearest walkable cell to the **townhall center**.

Path (3) is the only one not anchored on the spawn-point, and it's the only one that hardcodes a unit type into both the `Player` struct (`ExtraStartingWorkers`) and the spawn loop (`spawnPlayerUnitLocked("worker", ...)`). It was acceptable when "additional worker" was the only upgrade with this shape; it stops scaling as soon as a second upgrade ("additional soldier", "additional ranger") needs the same mechanic.

A separate but adjacent system, **wave upgrades** (`upgrade_apply.go`), offers no way to grant a unit at all. Today's wave upgrades are: stat multiplier, XP grant, equipment drop, resources. There is no `spawnUnit` effect type.

Constraints from `AI_RULES.md` that shape the design:

- **ID-based targeting invariant.** No new `*Unit` or `*BuildingTile` field on any struct that outlives a tick. The helper will resolve the spawn-point building under the lock, use it for placement math, and return.
- **`*Locked` discipline.** The helper assumes `s.mu` is already held. Both call sites (`EnsurePlayerWithUpgrades`, `applyUpgradeLocked`) already hold the lock.
- **Determinism.** No wall-clock time, no unseeded RNG. The helper uses `findNearestWalkable` (deterministic) and iterates the `ExtraStartingUnits` map in **sorted key order** to avoid Go's randomized map iteration leaking into spawn order.
- **No tick-path I/O.** The helper is only invoked at match start and at wave-upgrade resolution, never inside the tick loop.

## Goals / Non-Goals

**Goals:**

- One generic helper that spawns N units of any catalog `unitType` for a player, anchored on that player's assigned `spawn-point` building.
- Replace the worker-specific `Player.ExtraStartingUnits` map (generic) for the existing `extraStartingUnit` profile-upgrade effect, without changing its catalog JSON shape or rank math.
- Wire a new `spawnUnit` wave-upgrade effect type into `upgrade_apply.go` using the same helper.
- Ship `spawn_soldier_rare` and `spawn_ranger_rare` wave upgrades as `unlimited: true` rare-tier picks, each granting 1 unit.
- Strict: skip + warn when a player has no assigned spawn-point (no townhall fallback).

**Non-Goals:**

- Not modifying authored placed-unit spawning (path 1) or `spawn-point` `metadata.spawnUnits` (path 2). Those keep their current behavior.
- Not changing the wave-upgrade UI; new upgrades render through the existing modal.
- Not redesigning the profile-upgrade catalog. The `extraStartingUnit` effect's JSON shape (`unitType`, `countPerRank`) is unchanged; only the field it writes into and the spawn anchor change.
- Not introducing a multi-unit count to the new `spawnUnit` wave effect at this stage — `count` is in the schema but the two initial JSONs ship with `count: 1`.

## Decisions

### Decision: One shared helper called from both call sites

```go
// spawnUnitsForPlayerAtSpawnPointLocked spawns `count` units of `unitType`
// for `player`, anchored on the player's assigned spawn-point building.
// When the player has no spawn-point (no townhall fallback), logs a warning
// and returns without spawning. Must be called under s.mu write lock.
func (s *GameState) spawnUnitsForPlayerAtSpawnPointLocked(player *Player, unitType string, count int)
```

The two call sites are:

- **Match start** (`EnsurePlayerWithUpgrades` in `state.go`): replaces the existing `spawnExtraStartingWorkersLocked(player, townhall, color)` call. Iterates `player.ExtraStartingUnits` in sorted key order and calls the helper once per entry with `(player, unitType, count)`.
- **Wave-upgrade pick** (`applyUpgradeLocked` in `upgrade_apply.go`): a new `case upgradeEffectTypeSpawnUnit` calls the helper with `(player, def.Effect.UnitType, def.Effect.Count)`.

**Why one helper, not two:** path (3) and the new wave path do the exact same thing — resolve the player's spawn-point, find the nearest walkable cell, spawn N units of type T. Duplicating the loop would create two places to keep the anchor logic in sync.

**Alternative considered:** Anchor on the townhall and let map authors place the spawn-point on top of (or beside) the townhall. Rejected — the user requirement is explicit that the spawn-point is the anchor; that also matches path (2) and keeps all three paths consistent on a single anchor concept.

### Decision: `Player.ExtraStartingUnits map[string]int` replaces `ExtraStartingWorkers int`

```go
// On Player:
ExtraStartingUnits map[string]int // unitType → count granted by profile upgrades at match start
```

The `extraStartingUnit` effect handler writes:
```go
player.ExtraStartingUnits[effect.UnitType] += rank * effect.CountPerRank
```

When a future profile upgrade ships with `effect.UnitType: "soldier"`, the same code path applies — no engine edit needed.

**Migration concerns:** None. `Player` is in-memory match state, not persisted. The persisted side (`PlayerProfile.OwnedUpgradeRanks`) is unaffected. The one place outside `state.go` and `profile_upgrade_defs.go` that reads `ExtraStartingWorkers` is `profile_upgrade_match_test.go`, which is updated in lockstep.

**Alternative considered:** Keep `ExtraStartingWorkers` plus add `ExtraStartingSoldiers`, `ExtraStartingRangers` as we go. Rejected — violates "no hardcoding around workers" and turns every new upgrade into an engine edit.

### Decision: Strict spawn-point requirement, no townhall fallback

If `findPlayerSpawnPointLocked(playerID)` returns nil:
- Log `slog.Warn("spawnUnitsForPlayerAtSpawnPointLocked: no spawn-point for player; skipping", "playerID", ..., "unitType", ..., "count", ...)`.
- Return without spawning.

This is consistent with the user's stated preference and surfaces map-authoring gaps as visible warnings rather than silent behavior changes. Every map currently shipped under `catalog/maps/` that supports player slots already authors player-class spawn-points (verified via grep at design time), so this is not a regression for the current map set.

**Alternative considered:** Fall back to the townhall center (today's behavior). Rejected per the user. Synthesize a spawn-point on the fly from the townhall — rejected because it would silently re-introduce the townhall-anchored behavior for any map that omits a spawn-point, defeating the purpose of the change.

### Decision: New `spawnUnit` effect type sits alongside existing wave-upgrade effects

Add a constant and a load-time validation branch:

```go
// upgrade_defs.go
const upgradeEffectTypeSpawnUnit = "spawnUnit"

// UpgradeEffect now also reads:
//   UnitType string `json:"unitType,omitempty"`
//   Count    int    `json:"count,omitempty"`
```

At load time the loader requires `UnitType` to be in `unitDefsByID` and `Count > 0`.

`applyUpgradeLocked` gets one new case:

```go
case upgradeEffectTypeSpawnUnit:
    s.spawnUnitsForPlayerAtSpawnPointLocked(player, def.Effect.UnitType, def.Effect.Count)
```

Stack tracking (`UpgradeStacks`) is incremented for non-`Unlimited` upgrades upstream, so the new upgrades simply set `unlimited: true` to opt out — same pattern the existing `resources` upgrades use.

**Alternative considered:** Reuse `Scope` + a sentinel `Stat` value. Rejected — scope is the *targeting selector* and stat is the *what* of stat upgrades. Overloading those for a side effect (spawning a unit) would mislead future readers.

### Decision: `spawn-point` lookup uses the existing `playerLabel` mechanism

`findPlayerLabelLocked(playerID)` already walks `MapConfig.Buildings` to find the spawn-point linked to the player's claimed townhall. The new private helper `findPlayerSpawnPointLocked(playerID)` does the same walk but returns the `*protocol.BuildingTile` directly, so the spawn helper can use `b.X`, `b.Y`, `b.Width`, `b.Height` for placement math.

```go
func (s *GameState) findPlayerSpawnPointLocked(playerID string) *protocol.BuildingTile {
    // For each MapConfig.Buildings with BuildingType=="spawn-point":
    //   resolve via metadata.playerLabel → townhall → ownership
    //   return on match
    // Returns nil if no spawn-point belongs to this player.
}
```

Placement math mirrors `spawnExtraStartingWorkersLocked` today, swapping the anchor:

```go
cellSize := s.MapConfig.CellSize
centerX := (float64(sp.X) + float64(sp.Width)/2) * cellSize
centerY := (float64(sp.Y) + float64(sp.Height)/2) * cellSize
blocked := s.getBlockedCellsLocked()
for i := 0; i < count; i++ {
    center := s.worldToGrid(centerX, centerY)
    spawnCell, ok := s.findNearestWalkable(center, blocked)
    if !ok { slog.Warn(...); continue }
    spawnPos := s.gridToWorldCenter(spawnCell)
    s.spawnPlayerUnitLocked(unitType, player.ID, player.Color, spawnPos)
}
```

This intentionally keeps the same `blocked`-snapshot-before-loop pattern the existing extra-workers code uses. Spawned units share soft-collision so co-located positions resolve themselves on the first tick.

## Risks / Trade-offs

- **Risk:** A map that authors a townhall but no spawn-point will silently grant no extra workers / no wave-upgrade-spawned units, only a log warning. → **Mitigation:** All currently shipped maps with player slots already author spawn-points (verified). Add a test that exercises the warning path with a townhall-only map to lock the behavior in.
- **Risk:** Removing `ExtraStartingWorkers` is a breaking field rename. → **Mitigation:** `Player` is not persisted across processes; the only readers are inside `server/internal/game/`. All occurrences are mechanically rewritten in this change (see Impact in proposal.md and tasks below).
- **Risk:** The in-flight `legend-points-upgrade-system` change still references `ExtraStartingWorkers` in its archived-but-not-yet-merged spec text. → **Mitigation:** Capture the rename in `tasks.md` and call it out in proposal Impact. When archiving order resolves, the sister change's spec must be updated to reference the new field; this is a documentation reconciliation, not a code conflict.
- **Trade-off:** `findPlayerSpawnPointLocked` walks `MapConfig.Buildings` linearly each call. `EnsurePlayerWithUpgrades` runs once per player join, and `applyUpgradeLocked` runs once per wave-upgrade pick — both off the tick path. A linear walk is fine here; introducing an index would be premature.

## Migration Plan

No external migration. Steps:

1. Add `ExtraStartingUnits map[string]int` on `Player`; remove `ExtraStartingWorkers`. Update the one test.
2. Update the `extraStartingUnit` profile-upgrade effect handler to write into the map.
3. Add `spawnUnitsForPlayerAtSpawnPointLocked` and `findPlayerSpawnPointLocked`; delete `spawnExtraStartingWorkersLocked`.
4. Update `EnsurePlayerWithUpgrades` to iterate the map and call the new helper.
5. Add the `spawnUnit` effect type to `upgrade_defs.go` (constant, validation) and `upgrade_apply.go` (dispatch case).
6. Add the two new wave-upgrade JSONs.
7. Tests.

Rollback is a single revert — no data file changes.

## Open Questions

None. Anchor (spawn-point), fallback (skip + warn), count semantics (1 per wave pick, unlimited), and field generalization (map) are all decided.
