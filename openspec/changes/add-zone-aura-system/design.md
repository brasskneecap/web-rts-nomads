## Context

The authoritative simulation lives in the Go server (`server/internal/game/`); all state mutates inside `GameState.Update(dt)` under a single lock (`s.mu`), and a per-viewer `MatchSnapshotMessage` is streamed to the Vue client each tick. `add-map-zones` already shipped the zone runtime: `GameState.Zones []zoneRuntime` (`{Def protocol.Zone, Owner string, Progress, Contested, …}`), a `zoneCaptureRegistry` of capture mechanics whose `evaluate` hooks flip `rt.Owner`, an `installZonesLocked` at match start, a per-tick `tickZonesLocked`, owned-zone vision in `fow_recompute.go`, and an ownership-gated `BuildBuilding`. Zone geometry + the static `Zone` def travel once in the welcome payload; a per-tick `ZoneSnapshot{id, owner, contested, progress, ownerColor}` carries mutable control state.

The stat system is **layered and read-at-point-of-use**, not a single modifier engine:

- **Base + path/rank multipliers + equipment** are folded into `unit.MaxHP/Damage/AttackSpeed/MoveSpeed/Armor` by `applyRankModifiersLocked` (`progression.go`), recomputed on rank-up / equip / upgrade.
- **Perks, banners, and existing radius auras** are read **on demand in the hot loop**: `effectiveArmorLocked` sums `perkBonusArmorLocked + perkBonusArmorFromBannersLocked + perkBonusArmorFromAurasLocked + perkBonusArmorFromBuffsLocked` and applies a percent term; `perkAttackSpeedBonusLocked` sums perk + banner + buff attack-speed bonuses; `perkMoveSpeedMultiplierLocked` returns `1 + Σbonuses`; damage runs `unit.Damage × (1 + perkBonusDamageMultiplierLocked) × player.PhysicalDamageMultiplier`. The pattern for "a new bonus source" is already established: add one term at the read site (see `perks_auras.go` — `guardian_aura`, `rallying_banner`, `sanctuary`).
- **Per-player aggregates already exist** on `Player`: `PhysicalDamageMultiplier`, `MagicDamageMultiplier`, `GlobalUnitSpawnTimeMultiplier`, `UnitSpawnTimeMultipliers`. Zone auras add one more aggregate of the same kind.

There is **no canonical `Modifier{stat, operation, value}` type today** and no shared stat id enum — `attackSpeed`/`damage`/`hp`/`moveSpeed` appear as ad-hoc strings in `upgrade_defs.go` and `advancement_defs.go`. This change introduces that canonical vocabulary and makes zone auras its first consumer, **without** rewriting the existing read sites' math.

Constraints (from `AI_RULES.md`):

- **Server authoritative.** Bonus application is server-side; the client renders static aura defs + snapshot owner only.
- **Targets by id, resolved + validated each tick.** The aura manager references zones by id; it never persists a `*Unit`/`*BuildingTile`.
- **Determinism.** Aggregation iterates the stable authored `s.Zones` slice and sorted stat ids; no wall-clock, no unseeded RNG, no map-iteration-order-driven outcomes.
- **`*Locked` discipline; no tick-path I/O.** All aura mutation is in `…Locked` methods under `s.mu`.
- **Reuse, don't duplicate.** No second armor/damage/speed formula. The aura aggregate is one additive/multiplicative term consumed by the *existing* read sites.

## Goals / Non-Goals

**Goals:**

- A single stat-modifier vocabulary (`StatModifier{stat, operation, value}` + stat id registry + `add`/`multiply` stacking) that every present and future bonus source can speak.
- Zone auras expressed purely as data in that vocabulary — no zone-specific effect types.
- A per-player Zone Aura Manager that aggregates owned-zone auras once per ownership change, so units never poll zones.
- Global v1 application that reuses every existing stat read site; new economy/worker stats get new read sites following the same pattern.
- Editor authoring of auras and an inspection panel showing owner + bonuses.
- Clean seams for radius scope, debuffs, periodic effects, and retrofitting other systems onto the vocabulary.

**Non-Goals:**

- Rewriting `applyRankModifiersLocked` or the perk read sites into a unified modifier engine. The vocabulary is layered over them; migration of existing sources is future work.
- Radius/local auras, enemy debuffs, periodic/spawn/vision aura types — reserved via `scope`/`type`, not implemented.
- Per-unit aura state. v1 is per-player and global; nothing is stamped on the unit.

## Decisions

### Decision: One canonical `StatModifier{stat, operation, value}`, layered over existing read sites — not a rewrite

Introduce `StatModifier struct { Stat string; Operation string; Value float64 }` in protocol, with `operation ∈ {add, multiply}`. This is the lingua franca: zone auras, and later campaign/equipment/events, all emit `StatModifier`s. The stacking rule is fixed and documented:

```
effective(stat) = (base + Σ value where op==add) × Π value where op==multiply
```

This reproduces the request's examples exactly: `+2` then `+3` → `+5` (sum of adds); `×1.15` (product of multiplies). It also matches how the codebase already combines bonuses — additive perk/banner terms summed, then percent/multiplier terms applied — so wiring the aggregate into a read site is a one-line addition next to the term that is already there.

Crucially we do **not** migrate perks/equipment/path multipliers into `StatModifier`s in this change. Those keep their current code paths untouched. The aggregate is an *additional* contributor read at the same point, exactly like `guardian_aura` was added next to `rallying_banner` in `perks_auras.go`. This keeps blast radius small and behaviour of shipped systems unchanged, while still giving the user the "all systems speak the same language" property for *new* systems.

**Alternatives considered:**

- (a) **Full unified modifier engine** — rip out the layered read sites and recompute every stat from a single per-unit modifier list. Rejected: enormous blast radius across combat, movement, armor, regen, equipment, and ~hundreds of perk fields; contradicts "match existing architecture" and "trust the code over a misread rule." The user asked to *reuse* the system, not replace it.
- (b) **Zone-specific effect types** (`worker_gold_multiplier`, …) — explicitly rejected by the request, and exactly the duplication this change exists to avoid.

### Decision: A validated stat id registry is the single source of truth for "what is a stat"

Add `statRegistry`: an ordered list of stat ids, each with `{id, label, defaultValue, allowMultiply}`. Validation (`isKnownStat`, `validateStatModifier`) rejects unknown ids and bad operations at map load (panic naming map + zone), consistent with how zone capture configs validate. The registry seeds:

- **Combat (existing read sites):** `healthRegen`, `manaRegen`, `moveSpeed`, `attackSpeed`, `damage`, `armor`, `maxHealth`, `maxMana`.
- **Economy / workers (new read sites):** `goldGatherRate`, `woodGatherRate`, `gatherSpeed`, `workerMoveSpeed`, `unitProductionSpeed`, `buildingConstructionSpeed`.

The registry is mirrored in TS (one small generated/maintained module) so the editor dropdown and the UI label formatter share the same ids and display strings. **Adding a stat is one registry entry + one read-site wire-up** — the property the request asks for ("straightforward to add stats without aura-specific code").

A stat id is just a string key, not a hard-typed field. The registry decides validity and presentation; the read site decides semantics. This is what lets a future system add a stat without the aura code knowing about it.

### Decision: Aggregate per player into a `PlayerStatModifierSet`, recomputed on ownership change (event-driven, not per tick)

Add to `Player`:

```
ZoneStatModifiers PlayerStatModifierSet   // map[stat]{Add float64; Mul float64}
```

reduced from all auras of all zones the player currently owns. v1 keeps this *specifically* the zone-aura aggregate (a named field, parallel to `PhysicalDamageMultiplier`), so it is obvious where the contribution comes from; a future change can generalise the field to "all `StatModifier` sources" without changing the resolver signature.

The resolver `playerStatModifierLocked(playerID, stat) (add float64, mul float64)` is O(1): a map lookup returning `(0, 1)` for an absent stat. Read sites call `add, mul := s.playerStatModifierLocked(unit.OwnerID, statX)` and fold it into their existing expression.

**Recompute is event-driven.** Because v1 scope is global and ownership flips are rare, the aggregate is rebuilt only when a zone's owner changes — never per tick, never per unit. This directly satisfies requirement #7 ("avoid every unit repeatedly checking zone ownership"): the per-tick hot path is an O(1) map read, and the O(zones × auras) reduction runs only on a flip.

**Alternatives considered:** recompute every tick (simpler, but wasteful and needless given global scope) — rejected; stamp aura bonuses on each unit like cross-unit perk buffs — rejected because global scope makes a per-player aggregate strictly smaller and avoids touching unit spawn/despawn.

### Decision: The Player Zone Aura Manager owns collection + aggregation + recompute; ownership flips notify it

`zone_auras.go` provides:

- `recomputeZoneAuraModifiersLocked(playerID string)` — clear the player's `ZoneStatModifiers`, iterate `s.Zones` in authored order, and for each zone whose `Owner` is allied with `playerID` (reusing `zonesAlliedLocked`), fold each `stat_modifier` aura into the set per the stacking rule. Deterministic: stable slice order, and adds/muls combine commutatively.
- `onZoneOwnershipChangedLocked(zoneID, oldOwner, newOwner string)` — the single hook the zone runtime calls **wherever `rt.Owner` is assigned a new value** (the `presence`, `control_point`, `clear`, and `claim` handlers, plus any future mechanic). It recomputes the old owner's set (the zone drops out → bonuses removed) and the new owner's set (the zone's auras add in). A flip to/from `neutral` is handled the same way (neutral is not a real player, so its recompute is a no-op/skip).

Because ownership-loss is just "old owner recomputes without this zone," requirement #2 (remove bonuses immediately on loss, transfer on change) falls out of the same code path — there is no separate teardown path to keep in sync.

The capture handlers today assign `rt.Owner = …` directly. To make the hook unmissable, funnel those assignments through a small `setZoneOwnerLocked(rt, newOwner)` helper that captures `old := rt.Owner`, assigns, and calls `onZoneOwnershipChangedLocked` when `old != newOwner`. This is the same "single chokepoint" discipline `BuildBuilding` uses for the build-gate.

### Decision: Global v1 application reuses existing read sites; new stats get new read sites in the same shape

For each stat, application happens at the point the stat is consumed, folding in `(add, mul)` from the aggregate:

| Stat(s) | Read site (existing) | Integration |
|---|---|---|
| `armor` | `effectiveArmorLocked` (`perks_defense.go`) | add to the flat-bonus sum; `multiply` folds into the existing percent term |
| `attackSpeed` | `perkAttackSpeedBonusLocked` (`perks_attack.go`) | add the aggregate's add; apply mul to effective speed |
| `damage` | damage chain (`state_combat.go` / `damage_pipeline.go`) | fold mul next to `PhysicalDamageMultiplier`; add as flat pre-mitigation |
| `moveSpeed` | `perkMoveSpeedMultiplierLocked` (`perks_movement.go`) | add into the `1 + Σbonus` and multiply the result |
| `maxHealth` / `maxMana` | `applyRankModifiersLocked` (`progression.go`) | apply `(base+add)×mul` after equipment fold; trigger `applyRankModifiersLocked` on aura recompute so max HP tracks ownership |
| `healthRegen` / `manaRegen` | per-tick regen apply path | fold into the per-second regen used that tick |

New stats with **no** current read site get one, applying the same `(base + add) × mul`:

| Stat | New read site |
|---|---|
| `goldGatherRate` / `woodGatherRate` / `gatherSpeed` | `state_workers.go` gather/deposit path (amount per gather and/or gather cadence) |
| `workerMoveSpeed` | worker movement step (gated to worker unit types, composing with `moveSpeed`) |
| `unitProductionSpeed` | `state_production.go` train-time computation (alongside `GlobalUnitSpawnTimeMultiplier`) |
| `buildingConstructionSpeed` | building construction progress path |

`maxHealth`/`maxMana` are the one subtlety: they are *folded* values (cached on the unit), not read-on-demand, so the aura recompute must re-run `applyRankModifiersLocked` for the affected player's units (preserving current HP %) when the aggregate changes. This is the same call equip/upgrade already make; ownership flips are rare so the cost is negligible. The on-demand stats (armor, speeds, damage, regen, gather, production) need no such trigger — they pick up the new aggregate on their next read.

### Decision: `ZoneAura` is a typed envelope around a `StatModifier`; `type` and `scope` are the extension seams

```
type ZoneAura struct {
    Type     string       `json:"type"`            // "stat_modifier" (v1)
    Scope    string       `json:"scope,omitempty"` // "global" (default); reserved: "radius"/"regional"
    Modifier StatModifier `json:"modifier"`        // present when Type == "stat_modifier"
}
```

Zone gains `Auras []ZoneAura`. The aggregator switches on `Type`: `stat_modifier` folds `Modifier`; unknown/future types are ignored by v1 aggregation (and rejected by the loader unless registered). New aura kinds (`periodic`, `spawn`, `vision`, `debuff`) are added by handling a new `Type` in the aggregator/manager — the zone schema, editor save path, and ownership hook are untouched. `Scope` defaults to `global`; a future radius implementation reads `Scope == "radius"` and a radius field without changing the global path.

This mirrors how `ZoneCapture{Type, Config}` already discriminates capture mechanics — designers and the code already understand that pattern.

### Decision: No new wire fields — auras are static, owner is already snapshotted

Auras live on the `Zone` def, which already travels once in the welcome payload. Current owner + owner color are already in `ZoneSnapshot`. So the inspection UI needs **zero** new network surface: it reads the selected zone's static `auras` and formats them with the shared label registry, and reads the owner from the live `ZoneSnapshot`. This keeps the per-tick wire unchanged and means bonuses are visible even before capture (you can inspect what a zone *would* grant). Server-side effective application is authoritative and independent of what the UI shows.

### Decision: Editor authoring writes the `auras` array through the existing save path

The zone popup in `MapEditorPanel.vue` gains a **Bonuses** section: a list of aura rows (stat select from the registry, operation select `add`/`multiply`, numeric value), an **Add aura** button, and per-row **Remove**. Edits mutate the in-memory `Zone.auras` and persist through the same `SaveMapCatalogEntry` path zones already use — no new save plumbing. The stat dropdown is driven by the shared TS registry so it cannot author an unknown stat.

## Risks / Trade-offs

- **Two stat dialects during the transition.** This change adds the canonical vocabulary but leaves perks/equipment/upgrades on their existing strings. Until those are retrofitted, "all systems speak the same language" is true for *new* sources only. Accepted deliberately (see Non-Goals) to keep blast radius small; the registry and resolver are the migration target when each old system is touched next.
- **`maxHealth`/`maxMana` recompute coupling.** Because these are folded onto the unit, an aura change must re-run `applyRankModifiersLocked` for the player's units. Mis-wiring this (forgetting the trigger) would make a max-HP aura silently no-op until the next rank-up. Mitigated by routing every ownership flip through `setZoneOwnerLocked → onZoneOwnershipChangedLocked`, which performs the recompute-and-reapply in one place, and by a test asserting a max-HP aura changes unit `MaxHP` on capture.
- **Multiply on additive-natured stats.** `multiply` on a stat like `armor` (which the codebase treats as flat) must compose with the existing percent term, not create a second independent percent path. The integration table pins each stat's fold explicitly; the per-stat scenarios lock the expected result so a wrong fold is caught.
- **Determinism of aggregation.** Folding auras in map order would be non-deterministic. Mitigated: iterate `s.Zones` (authored slice) and combine via sum/product (commutative), so the result is order-independent anyway — belt and suspenders.
- **Neutral / team-sentinel owners.** `Owner` can be `neutral` or a team sentinel. The recompute skips non-player owners and uses `zonesAlliedLocked` for membership, so a team-owned zone correctly feeds every allied player's aggregate (matching co-op shared-team reality).

## Migration

`Zone.Auras` is additive; every existing map loads with no auras and unchanged behaviour (no aura ⇒ empty aggregate ⇒ resolver returns `(0,1)` ⇒ every read site behaves exactly as today). The stat registry and per-player aggregate are new and dormant until a zone with auras is captured. No schema version bump, no data migration, no change to any existing stat's value when no auras are present. The only authored data is sample auras on the demonstration zone map.

## Open Questions

- Should `gatherSpeed` and the per-gather `goldGatherRate`/`woodGatherRate` be distinct stats (cadence vs. amount), or should one "gather rate" multiplier cover both? Shipping with amount-multiplier stats (`goldGatherRate`, `woodGatherRate`) plus a separate `gatherSpeed` cadence stat, since the existing system exposes gather *amount* and gather *interval* separately; collapsible later if designers never use both.
- Should the inspection panel show the **effective** aggregated player bonus (summed across all owned zones) in addition to the selected zone's own auras? Shipping with per-zone auras in the panel (answers "what does this zone grant"); a player-wide "active territory bonuses" summary is a small follow-up that reads the same aggregate.
- When `maxHealth` is reduced by losing a zone, should current HP clamp down immediately or only cap future healing? Shipping with `applyRankModifiersLocked(preserveHealthPercent=true)` (the existing equip/upgrade behaviour) for consistency; revisit if it feels punishing.
