## Why

Maps now carry capturable **zones** (`add-map-zones`): a team starts holding a seed zone, captures adjacent zones, and earns vision over a held zone plus the right to build inside it. The next ask is for a held zone to optionally grant **passive bonuses** to its owner — health regen, gather rate, move speed, and so on.

The trap to avoid is building a one-off "capture-point bonus system" with bespoke effect types (`worker_gold_multiplier`, `unit_health_regen`, …). The codebase already grows stat bonuses from several sources — path/rank multipliers, equipment, perks, banners, profile upgrades, advancements — and each currently speaks its own dialect at its own read site. Adding a sixth private dialect for zones makes the next system (equipment v2, campaign modifiers, weather, shrines) the seventh.

What is missing is a **shared modifier vocabulary** every system can emit into: a stat id + an operation + a value, stacked by one documented rule. Zone auras should be the first consumer of that vocabulary, not a parallel mechanism. With it, a zone aura is just data — `{stat: "healthRegen", operation: "add", value: 2}` — that aggregates into the same per-player modifier set a future campaign bonus or equipment piece will use, and the existing stat pipeline reads one extra term at the points where it already reads perk/banner/aura terms.

This change therefore delivers two things:

1. **A reusable stat-modifier vocabulary + per-player aggregation.** A canonical `StatModifier{stat, operation, value}`, a registry of valid stat ids (existing combat stats plus the new economy/worker stats), the `add`/`multiply` stacking rule, and a per-player aggregated modifier set the existing stat read sites consult. This is the extension point all future systems plug into.
2. **The Zone Aura system on top of it.** Optional `auras` on a zone, a per-player Zone Aura Manager that recomputes a player's aggregate when zone ownership changes, multi-zone stacking, ownership-loss teardown, map-editor authoring of auras, and a zone inspection panel that shows owner + granted bonuses.

Scope for v1 is **global** auras (they affect all of the owner's units/buildings/workers regardless of position) — the structure leaves clean seams for local/radius auras, debuffs, and periodic effects later.

## What Changes

### Shared stat-modifier vocabulary (protocol + Go + TS)

- Add a canonical `StatModifier` type: `{stat string, operation string, value float64}`. `operation` is `add` or `multiply`. This is the single shape every modifier source emits.
- Add a **stat id registry**: a sorted, validated set of stat ids with metadata (display label, default value, whether `multiply` is meaningful). Initial ids cover the existing combat stats (`healthRegen`, `manaRegen`, `moveSpeed`, `attackSpeed`, `damage`, `armor`, `maxHealth`, `maxMana`) and the new economy/worker stats (`goldGatherRate`, `woodGatherRate`, `gatherSpeed`, `workerMoveSpeed`, `unitProductionSpeed`, `buildingConstructionSpeed`). Adding a stat later is one registry entry plus one read-site wire-up.
- Add a per-player **aggregated modifier set** on `Player` (a compact `map[stat]{add, mul}` reduced from contributing `StatModifier`s) and an O(1) resolver `playerStatModifierLocked(playerID, stat) (add, mul)`. The documented stacking rule: `effective = (base + Σadd) × Πmultiply`, matching the request's `+2 +3 = +5` and `×1.15` examples.

### Stat pipeline integration (Go)

- At each existing stat read site, add **one** term that reads the per-player aggregate — no new stat math, no duplicated formulas. Existing-stat read sites (armor via `effectiveArmorLocked`, attack speed via `perkAttackSpeedBonusLocked`, move speed via `perkMoveSpeedMultiplierLocked`, damage via the damage multiplier chain, max HP via `applyRankModifiersLocked`, health/mana regen, max mana) consult the aggregate alongside the perk/banner/aura terms they already sum.
- For the **new** stats with no current read site (gather rate/amount, gather speed, worker move speed, unit production speed, building construction speed), add a read site in the worker/production/construction code that applies the aggregate's `(base + add) × mul` rule.

### Zone aura model (protocol + map data)

- Add an optional `auras []ZoneAura` to the `Zone` definition. A `ZoneAura` carries a `type` discriminator (`stat_modifier` for v1), a reserved `scope` (`global` default), and — for `stat_modifier` — an embedded `StatModifier`. The `type` field is the extension seam for future aura kinds (`periodic`, `spawn`, `vision`, …) without touching the v1 path.
- Validate auras at map load (panic naming map + zone): known `type`, known `stat`, `operation` in `{add, multiply}`, finite `value`.

### Player Zone Aura Manager (Go)

- Add a per-player manager responsible for: determining the player's owned zones, collecting their active aura modifiers, aggregating them into the player's modifier set, and recomputing on change. It is **event-driven** — recompute fires when a zone's ownership flips, not every tick — so units never poll zone ownership.
- Hook the existing zone ownership flip (in the capture handlers / `tickZonesLocked`) to notify the manager: on a flip, recompute the **old** owner's aggregate (the zone drops out) and the **new** owner's aggregate (the zone's auras add in). Ownership loss is the old-owner recompute with the zone gone.

### Map editor (TS / Vue 3)

- Extend the zone popup in `MapEditorPanel.vue` with a **Bonuses** section: add aura, remove aura, a stat selector (driven by the stat id registry), an operation selector (`add` / `multiply`), and a value field. Saved into the zone's `auras` array via the existing map-save path.

### Zone inspection UI (TS / Vue 3)

- When a captured zone is selected, show an inspection panel: zone name, **Owner** (player name + color from the snapshot), and a **Bonuses** list formatted from the zone's static `auras` (`+2 Health Regen`, `+15% Gold Gather Rate`, `+10% Move Speed`). No new snapshot fields are required — auras are static (welcome payload) and owner is already in `ZoneSnapshot`.

## Capabilities

### New Capabilities

- `stat-modifiers` — the reusable stat-modifier vocabulary: the canonical `StatModifier{stat, operation, value}`, the validated stat id registry, the `add`/`multiply` stacking rule, the per-player aggregated modifier set with its O(1) resolver, and the integration of that aggregate into the existing per-stat read sites (plus the new economy/worker read sites). The foundation every future bonus source emits into.
- `zone-auras` — the zone aura feature: optional `auras` on a zone, the per-player Zone Aura Manager (owned-zone collection, aggregation, event-driven recompute), ownership-change application (apply to new owner, remove from old, teardown on loss), multi-zone stacking through the shared rule, editor authoring of auras, and the zone inspection panel.

## Impact

- **Protocol:** `Zone` gains `Auras []ZoneAura` (new `ZoneAura`, `StatModifier` types). `MatchSnapshot`/welcome unchanged for auras (static auras already travel with the zone def; owner already in `ZoneSnapshot`). Map JSON gains an optional per-zone `auras` array — additive; old maps load with no auras.
- **Server (Go):**
  - `server/pkg/protocol/messages.go` — `StatModifier`, `ZoneAura`, `Zone.Auras`.
  - `server/internal/game/stat_modifiers.go` (new) — `StatModifier` semantics, stat id registry + validation, per-player `PlayerStatModifierSet`, `playerStatModifierLocked` resolver, aggregation helpers.
  - `server/internal/game/state.go` — `Player` gains the aggregated modifier set field; initialise at player construction.
  - `server/internal/game/zone_auras.go` (new) — the Player Zone Aura Manager: collect owned-zone auras, aggregate into the player set, `recomputeZoneAuraModifiersLocked(playerID)`, `onZoneOwnershipChangedLocked(zoneID, oldOwner, newOwner)`.
  - `server/internal/game/zone_handlers.go` / `zone_runtime.go` — call the ownership-change hook wherever `rt.Owner` flips.
  - `server/internal/game/maps.go` — normalise + validate `Auras` on load.
  - Existing read sites — `perks_defense.go` (`effectiveArmorLocked`), `perks_attack.go` (`perkAttackSpeedBonusLocked`, damage chain), `perks_movement.go` (`perkMoveSpeedMultiplierLocked`), `progression.go` (`applyRankModifiersLocked` for max HP / max mana), the health/mana regen apply path: add one aggregate term each.
  - New read sites — `state_workers.go` (gather rate/amount, gather speed, worker move speed), `state_production.go` (unit production speed), the building construction path (building construction speed).
  - `server/internal/game/catalog/maps/*.json` — extend the demonstration zone map with sample auras.
- **Client (Vue / TS):**
  - `client/src/game-portal/src/game/network/protocol.ts` — mirror `StatModifier`, `ZoneAura`, `Zone.auras`.
  - A shared stat id registry / formatter module (TS mirror) for the editor dropdown and the UI labels.
  - `client/src/game-portal/src/components/MapEditorPanel.vue` — Bonuses authoring section in the zone popup.
  - Zone inspection panel component — owner + formatted bonuses on zone selection.
- **Invariants (AI_RULES):** auras and zones referenced by id, resolved/validated each tick they are read; no persisted `*Unit`/`*BuildingTile`; aura aggregation is deterministic (stable iteration over the authored zone slice and sorted stat ids — no map-iteration-order-driven outcomes) and runs inside the tick loop under `s.mu`; no I/O on the tick path; the client renders snapshot/static fields only and never computes bonus application.

## Out of Scope

- **Local / radius / regional auras.** v1 auras are global to the owning player. The `scope` field is reserved and defaulted to `global`; radius evaluation is a future change.
- **Non-stat aura types.** The `type` discriminator reserves `periodic`, `spawn`, `vision`, `debuff`, etc., but only `stat_modifier` is implemented now.
- **Migrating perks / equipment / advancements onto the shared vocabulary.** This change *introduces* the vocabulary and makes zone auras its first consumer; retrofitting existing systems to emit `StatModifier`s is deliberately deferred so their behaviour is untouched.
- **Enemy debuff auras** (auras that modify enemies of the owner). Reserved by the `scope`/`type` seams; not implemented.
- **Per-aura UI iconography / on-map bonus indicators.** The inspection panel lists bonuses textually; bespoke icons are future polish.
- **New balance tuning.** The sample map demonstrates the mechanism; tuning real campaign/PvP values is separate.
