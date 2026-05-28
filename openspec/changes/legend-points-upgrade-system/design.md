## Context

Legend Points already exist as a persistent currency on `profile.PlayerProfile` (`LegendPoints`, `LifetimeLegendPoints`) and are already partially wired:

- `rollLegendPointDropLocked` in `server/internal/game/player_buffs.go` reads `tuning.LegendPoints.PerKillBaseDropChance` / `PerKillBaseAmount` (plus per-unit overrides) and accumulates drops into `Player.RunLegendPointDrops`.
- Match-end protocol (`pkg/protocol/messages.go`) carries `LegendPointsEarned` and the profile manager persists it.
- `profile.PlayerProfile` already carries `MaxRerolls` and `MaxUpgradeStacks` as legend-incrementable caps, but no UI or HTTP endpoint exposes purchases for them.

The Upgrades tab on `views/ProfileView.vue` renders a static "Coming Soon" placeholder. There is no catalog of buyable upgrades, no purchase/refund endpoint, and no spec describing how legend purchases should apply to a match.

Constraints:

- **Server is authoritative.** The client cannot decide what an upgrade does; effects must be applied server-side at match start.
- **Tick determinism (`AI_RULES.md`).** Anything plumbed into the damage pipeline must read a precomputed value, not branch on profile-fetch I/O during a tick.
- **No new pointer-target fields on tick-loop structs.** Per-player multipliers live on `Player` as primitives, populated once at match-start.
- **Catalog-driven extensibility.** New upgrades must be addable by dropping a JSON file plus, at most, registering a new effect handler — never by special-casing the engine.

## Goals / Non-Goals

**Goals:**

- Enable the 5% per-kill drop rate by tuning, not code.
- Define a generic `ProfileUpgradeDef` schema and loader that supports the three initial upgrades and is straightforwardly extensible (new effect types are added by registering a new effect handler).
- Persist owned ranks per player and migrate existing profiles forward without data loss.
- Provide REST endpoints for purchase, refund, and catalog read.
- Apply owned ranks deterministically at match start, before the tick loop starts producing events.
- Keep refunds simple: full cost refund of any single rank, last-rank-first.
- Ship a Profile → Upgrades panel that renders the catalog and drives purchase/refund.

**Non-Goals:**

- No partial / discounted / time-limited refunds; refunds always return exactly the cost paid for that rank.
- No upgrade prerequisites or unlock conditions (other than rank cost and max rank). Future work, not in this change.
- No re-balancing of existing wave upgrades or buffs.
- No new damage school. "Magic" is defined here as "any `DamageType` that is not `DamagePhysical` after `OrPhysical()` resolution" — this is the existing classification, not a new one.
- No in-match purchasing. Purchases happen only outside of a match, against the profile.
- No legend-point earning changes beyond enabling the existing kill-drop rate via tuning.

## Decisions

### Decision: One generic catalog + typed effects, not three bespoke upgrades

`ProfileUpgradeDef` is loaded from `server/internal/game/catalog/profile-upgrades/<id>.json`. Each def has an `id`, `name`, `description`, `maxRanks`, an ordered `costPerRank []int` of length `maxRanks`, and an `effect` discriminated by `effect.type`. The initial types:

- `extraStartingUnit` — fields: `unitType string`, `countPerRank int`. Applied at match start: after authored placed units spawn, spawn `rank * countPerRank` additional `unitType` units near the player's townhall.
- `damageMultiplierByType` — fields: `damageTypeClass string` (`"physical"` or `"nonPhysical"`), `multiplierPerRank float64`. Applied as `1 + rank * multiplierPerRank` on outgoing damage that matches the class.

A small `profileUpgradeEffectRegistry map[string]profileUpgradeEffectApplier` lets future contributors register a new effect type alongside its JSON-validation and "apply-to-match" hook without editing existing call sites.

**Alternatives considered:** (a) Hardcoded `if upgradeID == "additional_worker"` branches in match bootstrap — rejected, every new upgrade would need an engine change. (b) Sharing the existing `UpgradeDef` (wave upgrades) by adding a new scope — rejected, wave upgrades are run-transient and pick from a deck offered between waves; profile upgrades are persistent purchases. Mixing them would conflate two very different lifecycles.

### Decision: Store owned ranks as `map[string]int` on the profile, with a v2 migration

Add `OwnedUpgradeRanks map[string]int `json:"ownedUpgradeRanks"`` to `PlayerProfile`. Bump `CurrentVersion` from 1 to 2 and add a migration in the profile store that initializes the map to `{}` for v1 profiles.

This avoids carrying a parallel "owned" list separate from ranks (single source of truth) and serializes cleanly to JSON. `MaxRerolls` and `MaxUpgradeStacks` stay where they are — they remain valid "legend-incrementable caps" applied by the wave-upgrade system; this change does not touch them.

**Alternatives considered:** (a) A flat `OwnedUpgrades []string` with duplicates for stacks — rejected, awkward to query and to refund cleanly. (b) A `[]ProfileUpgradeRank{ID, Rank}` slice — rejected, two fields where one map works.

### Decision: Full-cost refund of the topmost rank only

Refunding `additional_worker` at rank 2 returns 100 LP (the cost of rank 2) and drops rank to 1. To refund the first rank as well, the player issues a second refund call. This keeps the cost table the single source of truth for both purchase and refund prices; no separate "refund value" column.

**Alternatives considered:** (a) Refund-all in one call — fine, can be a UI convenience; the endpoint stays per-rank for atomicity. (b) Tax / partial refund — rejected by the user's spec ("refund these powers to give players back the cost of the upgrade").

### Decision: Snapshot ranks onto `Player` at match start

When a player joins a match, the server reads their `PlayerProfile.OwnedUpgradeRanks` once and writes a `Player.ProfileUpgrades map[string]int` plus precomputed convenience fields:

- `Player.PhysicalDamageMultiplier float64` (default `1.0`)
- `Player.MagicDamageMultiplier float64` (default `1.0`)
- `Player.ExtraStartingWorkers int`

This satisfies the "no I/O on the tick path" invariant: the damage pipeline reads `s.Players[ownerID].PhysicalDamageMultiplier`, never the profile store. Mid-match profile mutations have no effect on the running match.

### Decision: Apply `damageMultiplierByType` in the damage pipeline, after the existing physical/magic classification

Outgoing damage is multiplied in `damage_pipeline.go` after the existing source/type resolution:

```
if resolvedType == DamagePhysical { dmg *= attacker.Owner.PhysicalDamageMultiplier }
else                              { dmg *= attacker.Owner.MagicDamageMultiplier }
```

Stacking order is "additive ranks, multiplicative across systems": ranks within an upgrade add (`1 + rank * 0.10`); separate systems (wave upgrades, items, buffs, profile upgrades) multiply. This matches how existing wave-upgrade damage multipliers stack with item damage.

### Decision: Spawn extra workers as part of `spawnPlacedUnitsForPlayerLocked`

After the authored placed-unit loop runs for a player, iterate the player's `ProfileUpgrades`, look up each def in the registry, and call `applier.applyAtMatchStart(s, player)`. For `extraStartingUnit`, this calls the existing `spawnPlayerUnitLocked("worker", ...)` near the player's townhall using `findNearestWalkable` so the new workers never spawn in a blocked cell. Determinism is preserved because the loop is ordered (sorted by upgrade ID, then by rank).

### Decision: 5% drop rate via tuning only

Set `legendPoints.perKillBaseDropChance = 0.05` and `legendPoints.perKillBaseAmount = 1` in `gameplay_tuning.json`. No code change in `rollLegendPointDropLocked`; it already does the right thing under the existing seed (`s.rngLoot`).

## Risks / Trade-offs

- **Risk:** Migrating profiles in place could corrupt files mid-write on crash. → **Mitigation:** The profile store already writes via a temp-file + rename pattern (`store_test.go` references "backup LegendPoints" — backup support exists). The v1→v2 migration adds a single optional field and falls back to `{}` if absent; on read of an unmigrated v1 profile, we lazily populate `OwnedUpgradeRanks = {}` and write back on the next `WithLocked` mutation. No destructive change to existing fields.
- **Risk:** Damage multipliers stacking out of control if future code adds another global multiplier. → **Mitigation:** Multipliers are applied at one well-known location in `damage_pipeline.go`; the design intentionally collapses both physical and magic into a single `if/else` against `DamagePhysical`. Adding a third class would require an explicit code change, not a silent stacking.
- **Risk:** Extra workers spawning on top of authored units or in unreachable terrain. → **Mitigation:** Reuse `findNearestWalkable` and the existing spawn-blocked logic from `spawnPlacedUnitsForPlayerLocked`. If no walkable cell is found within the search radius, log a warning and skip — this matches the existing behavior for authored placed units.
- **Trade-off:** Refunds only restore the top rank, not arbitrary ranks. This means re-spec'ing a maxed `physical_power` (rank 10) into `magic_power` requires ten refund calls (cheap on the UI side — a "Refund All" button issues them in a loop). Worth the simplicity of "cost table is the only source of truth".
- **Trade-off:** Profile upgrades apply *only* at match start. A player who buys a rank during an active match does not see it that match. This matches the determinism / "no profile I/O on tick path" invariant and is the same lifecycle as buffs.

## Migration Plan

1. Land the proposal/specs/tasks together with the tuning change so kill drops start the moment the binary ships, even before the UI exists.
2. Ship the schema bump (`CurrentVersion = 2`) and migration in the profile store; verify in `store_test.go` that a v1 fixture loads cleanly with `OwnedUpgradeRanks = {}`.
3. Ship the catalog + HTTP endpoints behind no flag — they read empty maps for existing profiles and behave correctly (no purchases yet).
4. Ship the match-start applier; for players with no purchased ranks, this is a no-op.
5. Ship the UI. Players can begin purchasing immediately.

Rollback: each layer is additive and rolling back the UI alone (or the applier alone) leaves the data on disk intact. The schema bump is the only step that requires a forward migration; reverting to v1 would require either rewriting profiles or keeping the v2 field unread by older binaries (Go's JSON unmarshal ignores unknown fields, so v1 binaries will read v2 profiles cleanly — they just won't use `OwnedUpgradeRanks`).
