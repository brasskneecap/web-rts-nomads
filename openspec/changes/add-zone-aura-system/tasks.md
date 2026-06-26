## 1. Protocol types

- [x] 1.1 In `server/pkg/protocol/messages.go`, add a `StatModifier` struct: `Stat string \`json:"stat"\``, `Operation string \`json:"operation"\`` (`add` | `multiply`), `Value float64 \`json:"value"\``.
- [x] 1.2 Add a `ZoneAura` struct: `Type string \`json:"type"\`` (`stat_modifier` for v1), `Scope string \`json:"scope,omitempty"\`` (default `global`), `Modifier StatModifier \`json:"modifier"\``.
- [x] 1.3 Add `Auras []ZoneAura \`json:"auras,omitempty"\`` to the `Zone` struct. Confirm it round-trips through the catalog renderer (`RenderCatalogEntryJSON`) and `cloneMapConfig` (deep-copy the slice).
- [x] 1.4 No new snapshot/welcome fields: confirm `Zone.Auras` already travels in the welcome payload with the rest of the static zone def, and that `ZoneSnapshot` already carries `owner` + `ownerColor` for the inspection UI.

## 2. Server: stat-modifier vocabulary

- [x] 2.1 Create `server/internal/game/stat_modifiers.go`. Define operation constants (`statOpAdd = "add"`, `statOpMultiply = "multiply"`).
- [x] 2.2 Define the stat registry: an ordered slice of `{ID, Label, DefaultValue float64, AllowMultiply bool}` seeded with the combat stats (`healthRegen`, `manaRegen`, `moveSpeed`, `attackSpeed`, `damage`, `armor`, `maxHealth`, `maxMana`) and economy/worker stats (`goldGatherRate`, `woodGatherRate`, `gatherSpeed`, `workerMoveSpeed`, `unitProductionSpeed`, `buildingConstructionSpeed`). Add `isKnownStat(id) bool`, `statLabel(id) string`, and a sorted `ListStatIDs()` for editor schema discovery.
- [x] 2.3 Add `validateStatModifier(filename, ctx string, m protocol.StatModifier) error` (or panic helper consistent with zone validation): `Stat` is known, `Operation` ∈ {add, multiply}, `Value` is finite, and `Value` is not the identity-breaking `0` for a `multiply`.
- [x] 2.4 Define `PlayerStatModifierSet` (e.g. `map[string]statAccum` where `statAccum{Add float64; Mul float64}`, identity `{0, 1}`). Add `(set) fold(m StatModifier)` applying the stacking rule, and `(set) resolve(stat) (add, mul float64)` returning `(0, 1)` when absent.
- [x] 2.5 Add `GameState.playerStatModifierLocked(playerID, stat string) (add, mul float64)` reading `Player.ZoneStatModifiers`; O(1), returns `(0, 1)` for unknown player/stat.
- [x] 2.6 Tests: adds sum; multiplies multiply; `(base+add)×mul` ordering; order-independence; empty set resolves `(0,1)`; unknown stat / bad operation rejected by validation.

## 3. Server: per-player aggregate field

- [x] 3.1 In `server/internal/game/state.go`, add `ZoneStatModifiers PlayerStatModifierSet` to `Player` (documented alongside `PhysicalDamageMultiplier`). Initialise to an empty (non-nil) set at every player-construction site.
- [x] 3.2 Confirm `clonePlayer`/snapshot paths do not need the aggregate (it is server-only, derived from zones); leave it off the wire.

## 4. Server: Player Zone Aura Manager

- [x] 4.1 Create `server/internal/game/zone_auras.go`. Implement `recomputeZoneAuraModifiersLocked(playerID string)`: clear `player.ZoneStatModifiers`; iterate `s.Zones` in authored order; for each zone whose `Owner` is allied with `playerID` (`zonesAlliedLocked`), iterate `zone.Def.Auras`; for `Type == "stat_modifier"` fold `Modifier` into the set. Skip non-player owners (`neutral`, team sentinel handled via the ally check). Deterministic (stable slice order, commutative fold).
- [x] 4.2 Implement `setZoneOwnerLocked(rt *zoneRuntime, newOwner string)`: capture `old := rt.Owner`; assign `rt.Owner = newOwner`; if `old != newOwner` call `onZoneOwnershipChangedLocked(rt.Def.ID, old, newOwner)`.
- [x] 4.3 Implement `onZoneOwnershipChangedLocked(zoneID, oldOwner, newOwner string)`: recompute the aggregate for `oldOwner` (and its allies) and `newOwner` (and its allies); skip non-player sentinels; after recompute, re-apply cached stats for affected players' units (task 6.5).
- [x] 4.4 Replace every direct `rt.Owner = …` assignment in `zone_handlers.go` / `zone_runtime.go` (the `presence`, `control_point`, `clear`, `claim` mechanics, and install) with `setZoneOwnerLocked`. Install-time owners (`startingOwner`) flow through it too so initial aggregates are correct (or recompute all players once after `installZonesLocked`).
- [x] 4.5 Tests: capture moves auras A→B; loss removes A's bonuses; multi-zone additive stack (+2,+3 ⇒ +5); multi-zone multiplicative stack (×1.15,×1.15 ⇒ ×1.3225); team-owned zone feeds both allied players; neutral zone grants nothing.

## 5. Server: map load validation

- [x] 5.1 In `server/internal/game/maps.go` (zone load/normalise path added by `add-map-zones`), normalise `Auras`: nil → empty slice; default `Scope` to `global`; default `Type` to `stat_modifier` when empty.
- [x] 5.2 Validate each aura at load (panic naming map file + zone id): known `Type`; for `stat_modifier`, `validateStatModifier` passes. Carry `Auras` through `cloneMapConfig` and `hydrateEntryInPlace`.
- [x] 5.3 Tests: valid auras load; unknown stat panics naming map+zone; bad operation panics; map with no auras loads empty.

## 6. Server: stat pipeline integration (existing read sites)

- [x] 6.1 `armor` — in `perks_defense.go` `effectiveArmorLocked`, add `add, mul := s.playerStatModifierLocked(unit.OwnerID, "armor")`: fold `add` into the flat-bonus sum and compose `mul` into the existing percent term (do not create a second percent path).
- [x] 6.2 `attackSpeed` — in `perks_attack.go` `perkAttackSpeedBonusLocked` (and/or the effective-speed site in `state_combat.go`), add the aggregate's `add`; apply `mul` to effective attack speed.
- [x] 6.3 `moveSpeed` — in `perks_movement.go` `perkMoveSpeedMultiplierLocked`, fold `add` into the `1 + Σbonus` and multiply by `mul`.
- [x] 6.4 `damage` — in the damage chain (`state_combat.go` raw-damage / `damage_pipeline.go`), fold `mul` next to `PhysicalDamageMultiplier`/`MagicDamageMultiplier` and apply `add` as a flat pre-mitigation term.
- [x] 6.5 `maxHealth` / `maxMana` — in `progression.go` `applyRankModifiersLocked`, after the equipment fold apply `(base+add)×mul` from the aggregate. Ensure `onZoneOwnershipChangedLocked` re-runs `applyRankModifiersLocked(unit, preserveHealthPercent=true)` for affected players' units so cached max stats track ownership.
- [x] 6.6 `healthRegen` / `manaRegen` — at the per-tick regen apply path, fold `(base+add)×mul` into the per-second regen used that tick.
- [x] 6.7 Tests (per stat): an active aura changes the effective value by the expected `(base+add)×mul`; no active aura leaves the value byte-identical to pre-change; max-HP aura changes `unit.MaxHP` on capture and reverts on loss preserving HP fraction.

## 7. Server: stat pipeline integration (new economy/worker read sites)

- [x] 7.1 `goldGatherRate` / `woodGatherRate` — in `state_workers.go` gather/deposit path, apply the resolved `(base+add)×mul` to the amount gained per gather, keyed by resource (gold vs wood).
- [x] 7.2 `gatherSpeed` — apply `mul` to gather cadence/interval (faster gathering) at the worker gather-timer site.
- [x] 7.3 `workerMoveSpeed` — at the worker movement step, apply the aggregate gated to worker unit types, composing with the global `moveSpeed` term.
- [x] 7.4 `unitProductionSpeed` — in `state_production.go` train-time computation, fold `mul` alongside `GlobalUnitSpawnTimeMultiplier`/`UnitSpawnTimeMultipliers`.
- [x] 7.5 `buildingConstructionSpeed` — at the building construction progress path, apply `mul` to construction rate.
- [x] 7.6 Tests: each new stat's aura changes the corresponding economy/production/construction outcome by the expected factor; absence leaves it unchanged.

## 8. Server: sample map

- [x] 8.1 Extend the demonstration zone map (`server/internal/game/catalog/maps/zone-demo.json` or equivalent) so at least two zones declare auras (mix of `add` and `multiply`, at least one combat and one economy stat) to exercise capture/transfer/stacking end to end.
- [x] 8.2 Determinism test: a two-run replay with the same seed/commands produces identical per-player aggregates and identical affected stat values tick-for-tick.

## 9. Client: protocol mirror + shared registry

- [x] 9.1 In `client/src/game-portal/src/game/network/protocol.ts`, mirror `StatModifier`, `ZoneAura`, and add `auras?: ZoneAura[]` to `Zone`.
- [x] 9.2 Add a shared TS stat module (mirrors the Go registry): the stat id list with display labels and a `formatModifier(m)` helper that renders `+2 Health Regen`, `+15% Gold Gather Rate`, `+10% Move Speed` (multiply → percent delta, add → signed flat). Single source for both the editor dropdown and the inspection UI.

## 10. Client: editor aura authoring

- [x] 10.1 In `client/src/game-portal/src/components/MapEditorPanel.vue` zone popup, add a **Bonuses** section listing the selected zone's `auras` as rows.
- [x] 10.2 Per row: a stat `<select>` driven by the shared registry, an operation `<select>` (`add` / `multiply`), a numeric value input, and a **Remove** button. An **Add aura** button appends a default `{type: "stat_modifier", scope: "global", modifier: {stat: <first>, operation: "add", value: 0}}`.
- [x] 10.3 Edits mutate `zone.auras` in place and persist through the existing `SaveMapCatalogEntry` path (no new save plumbing). Verify a round-trip: author → save → reload shows the same auras.

## 11. Client: zone inspection UI

- [x] 11.1 On zone selection, render an inspection panel: zone name; **Owner** (player display name + color, resolved from the live `ZoneSnapshot.owner`/`ownerColor`); **Bonuses** list formatted from the zone's static `auras` via `formatModifier`.
- [x] 11.2 Hide the Bonuses section when the zone has no auras. Read auras from static zone data and owner from the snapshot — never compute application client-side.
- [x] 11.3 Verify against the design example: capturing the demo zone shows the owner and the expected formatted bonus lines.

## 12. Extension-point verification

- [~] 12.1 Confirm a new stat id can be added with one registry entry (Go + TS) plus one read-site wire-up, with no change to `StatModifier`, the aggregator, or the aura code. (Verified by construction: the `statRegistry` table + `isKnownStat`/`statLabel` indirection means adding a stat is a single table row in `stat_modifiers.go` (and the TS mirror) plus one read-site `playerStatModifierLocked` call; the aggregator switches on aura `Type`, never on stat id. Not exercised with a throwaway-stat test.)
- [x] 12.2 Confirm the aggregator switches on aura `Type` and ignores/handles unknown types without breaking `stat_modifier` aggregation; confirm `Scope` defaults to `global` and the global path does not read any radius field. Document these seams in `zone_auras.go` for the future radius/debuff/periodic work.
