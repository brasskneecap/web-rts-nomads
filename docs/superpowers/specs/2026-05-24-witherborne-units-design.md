# Witherborne Units (Necromancer + Skeleton Soldier) — Design

**Date:** 2026-05-24
**Status:** Approved, ready for implementation planning
**Scope:** Add two new enemy units (`necromancer`, `skeleton_soldier`) under a new `witherborne` faction, plus a new `raise_skeleton` ability that summons a `skeleton_soldier`. Map-editor paintability is a side effect of the catalog layout — no editor code changes.

## 1. Purpose

Introduce the first two members of the `witherborne` faction so playtest maps can include undead enemies, and ship the first **summon** ability so future witherborne units have a precedent for it.

Goals:
1. Necromancer is a caster modelled on the existing `acolyte` stat block.
2. Skeleton soldier is a basic melee enemy modelled on the existing `raider` stat block.
3. Both units appear in the map editor's brush picker automatically (no UI registration code).
4. `raise_skeleton` lets a necromancer spawn one `skeleton_soldier` on cast.

## 2. Non-goals

- **No path/perk progression** for either unit (raiders have none — these are pure enemies).
- **No new projectile or VFX assets** — the necromancer reuses the existing `shadow_bolt` projectile and `shadow` damage type (already wired for Arch Mage).
- **No summon cap or summon-despawn-on-caster-death** — a necromancer can keep summoning while it has mana. Adding a cap requires new caster→summons tracking on `Unit`; that's a follow-up if playtest shows it matters.
- **No selection-HUD portrait** for either unit (no `portrait.png` in the existing asset directories). Selection HUD renders its standard missing-art fallback. Tracked as a follow-up.
- **No expansion of the ability target model** to support target-location ("cast on a tile"). `raise_skeleton` is self-targeted; the skeleton spawns next to the caster.
- **No client code changes** beyond what `catalog/units/` auto-discovery already provides.

## 3. High-level architecture

The change is mostly catalog data. The one load-bearing code addition is a new resolve step in the ability system to actually spawn a unit when `def.SummonUnitType` is set.

```
catalog data (JSON, no code):
  catalog/units/witherborne/necromancer/necromancer.json
  catalog/units/witherborne/skeleton_soldier/skeleton_soldier.json
  catalog/abilities/raise_skeleton/raise_skeleton.json

new code (Go, small):
  AbilityDef gains a `SummonUnitType string` field
  resolveAbilityCastOnTargetLocked gains a new branch: if def.SummonUnitType != "",
    call spawnPlayerUnitLocked with the caster's OwnerID + color, at a small
    offset from the caster's position
```

The client picks the new units up automatically via the existing `catalog/units/<faction>/` auto-discovery (see [`client/src/game-portal/src/game/maps/unitDefs.ts:32-40`](../../../client/src/game-portal/src/game/maps/unitDefs.ts#L32-L40)).

## 4. Components

### 4.1 `catalog/units/witherborne/necromancer/necromancer.json`

Combat stats copied from `acolyte.json`. Economic fields (resourceCost / requiresBuildings / spawnSeconds / trainLabel) follow the **raider** pattern because this is an enemy unit, not a trainable friendly. Faction `"witherborne"`, archetype `"caster"`, combatProfile `"caster"`.

Concrete values:

```json
{
  "type": "necromancer",
  "name": "Necromancer",
  "faction": "witherborne",
  "archetype": "caster",
  "combatProfile": "caster",
  "visionRange": 384,
  "hp": 75,
  "damage": 8,
  "attackRange": 220,
  "attackSpeed": 1.5,
  "moveSpeed": 100,
  "resourceCost": {},
  "meatCost": 0,
  "spawnSeconds": 0,
  "capabilities": ["move", "attack"],
  "trainLabel": "",
  "maxMana": 50,
  "manaRegenRate": 1.0,
  "projectile": "shadow_bolt",
  "damageType": "shadow",
  "projectileScale": 1.5,
  "abilities": ["raise_skeleton"],
  "attackVisual": {
    "kind": "projectile",
    "originX": 24,
    "originY": 5,
    "effectLength": 22
  },
  "bounds": {
    "halfWidth": 20,
    "top": -72,
    "bottom": 36,
    "ringOffsetX": 0,
    "ringOffsetY": -15
  }
}
```

### 4.2 `catalog/units/witherborne/skeleton_soldier/skeleton_soldier.json`

Direct copy of `raider.json` with name/type/faction renamed.

```json
{
  "type": "skeleton_soldier",
  "name": "Skeleton Soldier",
  "faction": "witherborne",
  "archetype": "raider",
  "visionRange": 256,
  "hp": 75,
  "damage": 5,
  "attackRange": 60,
  "attackSpeed": 1,
  "moveSpeed": 100,
  "resourceCost": {},
  "meatCost": 0,
  "spawnSeconds": 0,
  "capabilities": ["move", "attack"],
  "trainLabel": "",
  "bounds": {
    "halfWidth": 14,
    "top": -60,
    "bottom": 10,
    "ringOffsetX": 0,
    "ringOffsetY": -8
  }
}
```

### 4.3 `catalog/abilities/raise_skeleton/raise_skeleton.json`

Self-targeted summon. Mana cost and cooldown chosen so a fresh-mana caster can summon at most ~2 in quick succession; sustained rate is gated by `manaRegenRate: 1.0` (~30 seconds per summon).

```json
{
  "id": "raise_skeleton",
  "displayName": "Raise Skeleton",
  "type": "spell",
  "canTargetSelf": true,
  "canTargetAllies": false,
  "canTargetEnemies": false,
  "castRange": 0,
  "castTime": 1.5,
  "manaCost": 30,
  "cooldown": 10,
  "damageType": "shadow",
  "category": "summon",
  "summonUnitType": "skeleton_soldier"
}
```

### 4.4 `AbilityDef` — new field `SummonUnitType string`

Add one optional field. Empty = no summon (every existing ability is inert).

```go
// SummonUnitType is the unit-type id (matches a catalog/units/.../<id>.json)
// that this ability spawns on resolve. Empty ⇒ ability is not a summon.
// Spawned unit takes the caster's OwnerID and color and appears at a small
// offset from the caster's position. The summoned unit type must exist in
// the unit catalog; an unknown id is a no-op at resolve with a logged warning
// (consistent with how getProjectileDef misses are handled today).
SummonUnitType string `json:"summonUnitType,omitempty"`
```

### 4.5 `resolveAbilityCastOnTargetLocked` — new summon branch

The existing function (in `server/internal/game/ability_cast.go`) handles `HealAmount`, `DamageAmount`, `EffectOnTarget`, and a perk hook. Add a `SummonUnitType` branch after the existing resolve steps:

```go
if def.SummonUnitType != "" {
    s.spawnSummonedUnitLocked(caster, def)
}
```

Where `spawnSummonedUnitLocked` is a new helper that:
1. Looks up the unit def via `getUnitDef(def.SummonUnitType)`. If missing, log once and return — consistent with the catalog miss handling elsewhere.
2. Computes a spawn position: caster position + a small deterministic offset (e.g., `(0, +48)` — directly "below" the caster from the camera's view). Determinism is preserved because the offset is a constant, not RNG-derived.
3. Calls `spawnPlayerUnitLocked(def.SummonUnitType, caster.OwnerID, caster.Color, spawnPos)`.
4. Returns. The summoned unit enters the existing combat/AI pipelines via `spawnUnitFromDefLocked`'s normal initialisation.

The summon happens AFTER the existing resolve steps in `resolveAbilityCastOnTargetLocked`, so if a future summon ability also has `EffectOnTarget` (e.g., a self-buff VFX on cast), both resolve in deterministic order.

## 5. Data flow & lifecycle

### 5.1 Map editor paints a necromancer

```
1. User opens the map editor (Editor.vue).
2. The editor reads the unit catalog from the server (existing /catalog/units
   endpoint).
3. The catalog now contains a "witherborne" faction bucket with two entries.
4. The editor renders a "witherborne" tab in the brush picker — populated
   automatically by the existing faction-bucket logic (unitDefs.ts:32-40).
5. User clicks a tile to paint a necromancer; the map JSON gains a unit entry
   with `unitType: "necromancer"` and an enemy `playerSlot`.
6. On match start, spawnPlayerUnitLocked instantiates the necromancer from the
   stat block.
```

### 5.2 Necromancer casts Raise Skeleton

```
1. AI / autocast triggers raise_skeleton on the necromancer (target = self).
2. beginAbilityCastLocked validates targeting (canTargetSelf), range (0 is
   irrelevant for self), and mana (30). Cooldown is armed at cast start.
3. Casting animation plays for 1.5 s (CastTime).
4. resolveAbilityCastLocked spends mana (30), then resolveAbilityCastOnTargetLocked
   runs:
   - HealAmount == 0  → no heal.
   - DamageAmount == 0 → no damage.
   - EffectOnTarget == "" → no VFX.
   - SummonUnitType == "skeleton_soldier" → spawnSummonedUnitLocked fires.
5. spawnPlayerUnitLocked instantiates the skeleton with OwnerID = enemy player,
   color = necromancer's color, position = necromancer + (0, +48).
6. The skeleton enters combat normally on its next tick.
7. Cooldown clears after 10 s; mana regenerates at 1.0/s.
```

### 5.3 Failure modes

| Trigger | Behaviour |
|---|---|
| Necromancer dies mid-cast | Existing `tickUnitCastLocked` path: cast is cancelled, no mana spent, no skeleton spawned. |
| Necromancer dies during cooldown | No-op — the cooldown is on a dead unit and never matters. The summoned skeletons (if any) survive independently — there is no "despawn on caster death" link (non-goal). |
| `skeleton_soldier` somehow missing from unit catalog at resolve time | `spawnSummonedUnitLocked` logs once and returns; mana was already spent. This is a build-time bug since both ship together, so the log is for catch-fire awareness, not a recoverable runtime error. |
| Two casts complete in the same tick on the same necromancer | Impossible — the cast lock and the cooldown together prevent overlap. |
| The summon's spawn position is occluded by terrain / another unit | Out of scope. The existing `spawnPlayerUnitLocked` does not collision-check, and neither do the other spawn paths (waves, training). If this becomes a real complaint, follow up by giving `spawnSummonedUnitLocked` access to the same nudge-to-free-tile logic the wave spawner uses. |

## 6. Testing strategy

### 6.1 Catalog load test

The existing catalog loader unit tests will detect:
- Missing required fields on the two new unit JSONs.
- Invalid faction / archetype / damage-type identifiers.
- Mismatched `type` field vs. directory name.

Add a new unit test (or extend an existing one) asserting that:
- `getUnitDef("necromancer")` returns a def with `Abilities == ["raise_skeleton"]`.
- `getUnitDef("skeleton_soldier")` returns a def with the raider stat block values.
- `getAbilityDef("raise_skeleton")` returns a def with `SummonUnitType == "skeleton_soldier"`.

### 6.2 Summon resolve test

New unit test (`server/internal/game/ability_summon_test.go`) covering:

- **Happy path:** seed a necromancer at a known position, fire the cast, advance time past `CastTime`, assert a `skeleton_soldier` exists owned by the same `OwnerID` at the expected offset position.
- **Mana cost:** caster mana drops by 30 on resolve, NOT on cast start.
- **Cooldown:** second cast fails until cooldown clears.
- **Cancel on caster death:** kill caster mid-cast → no skeleton spawned, no mana spent.
- **Unknown summon type:** seed an `AbilityDef` with `SummonUnitType: "nonexistent"`, fire the cast, assert the cast completes (mana spent) but no unit is spawned and a log message is emitted.

### 6.3 Determinism

`spawnSummonedUnitLocked` uses no RNG (fixed offset constant). The summoned unit's stat block, OwnerID, and color are deterministic. No new sources of nondeterminism are introduced.

### 6.4 Map-editor smoke

Manual: open the map editor in dev, confirm a "witherborne" brush tab appears with necromancer + skeleton_soldier entries, paint each on a test map, start a match against that map, confirm both spawn and behave normally.

## 7. Risks / trade-offs

- **No summon cap means runaway skeleton armies in long fights.** Mana regen (~30s per cast) is the only natural throttle. If a single necromancer left alive for 5+ minutes produces 10+ skeletons, the AI / combat system may chug. Mitigation: playtest data first; if it bites, add an `s.casterSummons` map keyed by caster ID with a configurable cap.
- **No portrait art** means the selection HUD looks unfinished for both units. Mitigation: art follow-up; not blocking.
- **Necromancer reuses Arch Mage's `shadow_bolt` projectile.** If `shadow_bolt` is later rebalanced for Arch Mage gameplay, the necromancer inherits the change. Acceptable — they're thematically aligned (both shadow casters).
- **Spawn position is a fixed offset, not collision-checked.** If a necromancer is wedged against a wall, the skeleton may spawn inside terrain or on top of another unit. The existing spawn pipeline tolerates this; visual stack-overlap is the worst case. Tracked as a follow-up if it surfaces in playtest.
- **`AbilityDef.SummonUnitType` is the first new field added since the heal/damage refactor.** Adding it is a one-line schema change; existing abilities omit it and remain inert (consistent with how `HealAmount` and `DamageAmount` work).

## 8. Migration

No live data to migrate. All changes are additive. Existing maps without witherborne units are unaffected. Existing abilities are unaffected because `SummonUnitType` is optional and defaults to empty.

## 9. Open questions deferred to the implementation plan

1. **Spawn offset direction.** `(0, +48)` (below caster) is the placeholder. The implementation plan can pick a different fixed direction if combat-side scripting benefits from a specific one (e.g., front of caster facing).
2. **Should `raise_skeleton` support auto-cast?** Default no. Easy to flip later by setting `supportsAutoCast: true` + an `autoCastTargetSelector`. Out of scope for v1.
3. **Necromancer melee attack visual.** Currently inherits acolyte's projectile attack (shadow_bolt). If the necromancer should also have a melee fallback when out of mana, that's a different design.

## 10. Cross-references

- Acolyte stat block: [`server/internal/game/catalog/units/human/acolyte/acolyte.json`](../../../server/internal/game/catalog/units/human/acolyte/acolyte.json).
- Raider stat block: [`server/internal/game/catalog/units/raider/raider/raider.json`](../../../server/internal/game/catalog/units/raider/raider/raider.json).
- Existing shadow_bolt projectile: [`server/internal/game/catalog/projectiles/dark_bolt/dark_bolt.json`](../../../server/internal/game/catalog/projectiles/dark_bolt/dark_bolt.json) (id: `shadow_bolt`).
- Ability cast lifecycle: [`server/internal/game/ability_cast.go`](../../../server/internal/game/ability_cast.go).
- Ability def + catalog loader: [`server/internal/game/ability_defs.go`](../../../server/internal/game/ability_defs.go).
- Unit spawn pipeline: [`server/internal/game/state_spawn.go`](../../../server/internal/game/state_spawn.go).
- Map-editor faction auto-discovery: [`client/src/game-portal/src/game/maps/unitDefs.ts`](../../../client/src/game-portal/src/game/maps/unitDefs.ts) (`UnitFaction` doc comment, lines 32-40).
- AI / target-by-ID invariants: [`.claude/rules/AI_RULES.md`](../../../.claude/rules/AI_RULES.md). Nothing in this spec changes those rules — the summoned skeleton is spawned by ID and immediately registered in the unit map; no new pointer-stored target fields are introduced.
