# Design

> **Design-evolution note.** This document was originally written around a
> per-`(path, rank)` ability-grant file plus a one-shot `applyGreaterHealSwapLocked`
> mutation helper. During implementation that approach proved fragile — the swap
> depended on the exact pre-state of `unit.Abilities` at one specific call site,
> and a debug-spawn path was missing it entirely. The design was reworked mid-
> implementation into a **path-level `"abilities"` override on `cleric.json`**,
> symmetric to the existing per-path overrides for `projectile` / `damageType` /
> `visionRange`. The sections below describe the final design as built. The
> per-(path, rank) grant mechanism remains in place as the additive
> "silver cleric also gains X" composable layer, but no rank-grant file is
> authored for the cleric line — greater_heal lives in the path-level override.

## Context

Two related changes that move Cleric power around inside existing systems without
introducing new mechanisms:

1. **Heal upgrade becomes baseline.** Greater Heal stops being a perk choice and
   becomes a Cleric path baseline declared in `cleric.json`'s new `"abilities"`
   field. The Bronze perk pool was already four entries because of this perk;
   making it baseline opens one slot for the new perk and leaves the pool size
   unchanged.
2. **New armor variant of Battle Prayer.** Bolstering Prayer mirrors Battle
   Prayer exactly — the same trigger (heal cast resolves), the same target set
   (every heal target), the same refresh semantics, the same decay loop, the
   same recast-threshold autocast logic — but applies a flat-armor bonus
   instead of an attack-speed bonus.

The change deliberately uses two pre-existing extension seams plus one new
generalised mechanism:

- `assignUnitPathAbilitiesLocked` is rewritten from "append rank-grants" into
  a full recompute of `unit.Abilities` from `(UnitType, ProgressionPath, Rank)`.
  It is called from `addUnitXPLocked` (after `assignUnitPerkLocked`) and from
  `DebugSpawnUnit` (after path/rank assignment).
- `onPerkAbilityResolvedLocked` is the per-target post-cast hook. Battle Prayer
  is currently its only consumer; Bolstering Prayer becomes its second.
- `pathAbilitiesByPath` is the new global, populated by `path_defs.go`'s loader
  from each path JSON's optional `"abilities"` field. Symmetric to
  `pathProjectileByPath` / `pathDamageTypeByPath` / `pathVisionRangeByPath`.

## Goals

- Every Cleric, starting from the moment they promote into the path, has
  Greater Heal as their heal ability. Choice of Bronze perk no longer gates
  the AoE heal.
- Add a Bronze perk that is mechanically familiar (same trigger, same target
  set, same UI surface as Battle Prayer) but rewards positioning healed units
  *defensively* rather than offensively.
- Preserve every invariant that the existing cleric-bronze-perks and
  per-path-ability-kits specs already guarantee — determinism, slot-preserving
  ability swap, refresh-longer / refresh-stronger semantics, cross-unit decay
  in `state.go Update()`, ID-not-pointer combat references.

## Non-goals

- Re-tuning `battle_prayer` itself, or any other Bronze perk.
- Adding Silver or Gold cleric perks.
- Changing Greater Heal's own JSON (TargetCount, manaCost, healAmount, cast
  time, cooldown all stay at their current authored values).
- Changing the heal-replace mechanic itself — only the call site that triggers
  it.
- Save compatibility for matches mid-flight at the time of the change. A unit
  in a serialised save with the obsolete `greater_heal` perk id keeps the id in
  `PerkIDs` (perk-def lookup returns nil, every existing hook handles that
  defensively) but its `Abilities` already contains `"greater_heal"` from the
  earlier swap, so the unit is functionally identical to a Cleric promoted
  post-change.

## Architecture

### A. Greater Heal as a path baseline

#### A.1. Catalog

- `catalog/units/human/apprentice/paths/cleric/cleric.json`: add
  `"abilities": ["greater_heal"]` to the path JSON, alongside the existing
  `projectile`/`damageType`/`projectileScale` overrides.
- `catalog/units/human/apprentice/paths/cleric/perks/bronze.json`: remove the
  `greater_heal` entry. Add `bolstering_prayer` (see Section B). Pool stays at
  four entries.
- `catalog/abilities/greater_heal/greater_heal.json`: unchanged.

#### A.2. Path-level override loader

In `path_defs.go`:

- `pathCatalogFile` gains an optional `Abilities *[]string` field (pointer so
  the loader distinguishes "field absent" from "field present but empty").
- `pathAbilitiesByPath = map[string][]string{}` is the new global storing
  loaded overrides per path id.
- The init loop validates each entry against the ability registry; an unknown
  id or empty string panics at load with a file/id message (mirrors the
  projectile / damage-type validators).
- A path can declare an explicit empty array `"abilities": []` to strip base
  abilities entirely. Field absent ⇒ no override; base unit abilities apply.

#### A.3. The recompute (replaces the swap helper)

`applyGreaterHealSwapLocked` is **deleted**. The previous one-shot mutation
helper had two problems: it depended on the exact pre-state of `unit.Abilities`
at one specific call site, and a debug-spawn path was missing it entirely. The
fix is to make `assignUnitPathAbilitiesLocked` a pure recompute from
`(UnitType, ProgressionPath, Rank)`. Composition:

1. Start with the unit def's `Abilities` (e.g., apprentice → `["heal"]`).
2. If `pathAbilitiesByPath[unit.ProgressionPath]` exists, REPLACE the list with
   the override (cleric → `["greater_heal"]`).
3. For each rank R ≤ current rank, append `(path, R)` rank-grants additively,
   skipping ids already present. No rank-grant files are authored for the
   apprentice line today; this step is the future-extension seam.
4. Migrate `AutoCastEnabled` / `AbilityCooldowns` by position: when the new
   list at index `i` differs from the old `unit.Abilities[i]`, move the
   entry under the new key and delete the old key. Indices beyond the old
   length are fresh slots with no migration source.

Idempotency, RNG-freeness, and determinism are properties of the function
itself: a pure recompute over the same inputs yields the same output. Re-running
the function on an already-promoted cleric produces the same `unit.Abilities`
with no drift.

The `applyPerkGrantedHooksLocked` perk-side hook stays as an empty extension
seam for future ability-replacing perks. `DebugSpawnUnit` is wired to call
`assignUnitPathAbilitiesLocked` after path/rank assignment so debug-spawned
units go through the same recompute as promotion-grown units — the original
debug-spawn-regresses-to-plain-heal bug is closed by construction.

#### A.4. Ordering inside the rank-up loop

Within `addUnitXPLocked`'s per-crossed-rank loop, the order is:

1. `assignUnitPathOnRankUpLocked` — assigns `ProgressionPath` on first rank-up.
2. `assignUnitPerkLocked` — rolls a perk from the eligible pool. With
   `greater_heal` removed from the cleric Bronze pool, the roll picks one of
   `sanctuary`/`battle_prayer`/`bolstering_prayer`/`mana_conduit`. None of
   those touch `Abilities`.
3. `assignUnitPathAbilitiesLocked` — recomputes `unit.Abilities` from
   `(UnitType, ProgressionPath, Rank)`. This is the function that picks up
   the path-level override and lays in the final ability list.
4. `applyRankModifiersLocked` — applies path/rank stat multipliers, projectile
   override, vision range, etc. (unchanged).

This ordering means the recompute always runs AFTER any perk roll and BEFORE
stat modifiers, so the unit's ability list is the canonical
`(UnitType, ProgressionPath, Rank)` resolution by the time the rest of the
rank-up settles.

### B. Bolstering Prayer

#### B.1. Catalog entry

```json
{
  "id": "bolstering_prayer",
  "displayName": "Bolstering Prayer",
  "icon": "perk-bolstering-prayer",
  "description": "Heal also grants temporary armor to every target.",
  "tooltipTemplate": "Healed allies gain +{armorBonus:0} armor for {buffDurationSeconds:1}s. Recasts on the focus target when the buff is below {recastThresholdPercent%} of full duration.",
  "config": {
    "buffDurationSeconds": 5.0,
    "armorBonus": 50,
    "recastThresholdPercent": 0.30
  }
}
```

`buffDurationSeconds` and `recastThresholdPercent` match Battle Prayer
intentionally — the two perks share the autocast-refresh trigger, and same
duration keeps the comparison simple at runtime.

#### B.2. Cross-unit perk state

```go
// ── bolstering_prayer (cleric bronze) ────────────────────────────────────
// Cross-unit buff applied to every target a Bolstering-Prayer-Cleric heals.
// Mirrors the BattlePrayer pair structurally: stored on the HEALED TARGET's
// PerkState (not the Cleric's), decays in state.go Update(), refresh-max
// semantics on re-application.
//
// BolsteringPrayerRemaining: seconds left on the buff. 0 = inactive.
// BolsteringPrayerArmor: flat armor bonus while the buff is active.
//   Set to Config["armorBonus"] on application; carried on the buff so the
//   value travels with it independent of later perk re-tuning.
BolsteringPrayerRemaining float64
BolsteringPrayerArmor     float64
```

The `float64` for armor is deliberate (matches `BattlePrayerMultiplier`'s
type). At the consumer site `effectiveArmorLocked` adds an `int(armor)` after
clamping non-negative — the int conversion is fine because the catalog value
is integer-valued and the buff never accumulates fractional armor.

#### B.3. On-resolve hook

`onPerkAbilityResolvedLocked` gains a second case alongside `battle_prayer`:

```go
case "bolstering_prayer":
    pDef := perkDefByID("bolstering_prayer")
    if pDef == nil { continue }
    cfg := pDef.ConfigForRank(caster.Rank)
    duration := cfg["buffDurationSeconds"]
    armor    := cfg["armorBonus"]
    if duration <= 0 || armor <= 0 { continue }
    if duration > target.PerkState.BolsteringPrayerRemaining {
        target.PerkState.BolsteringPrayerRemaining = duration
    }
    if armor > target.PerkState.BolsteringPrayerArmor {
        target.PerkState.BolsteringPrayerArmor = armor
    }
```

Gated identically on `def.Category == AbilityCategoryHeal` so only heal-class
abilities trigger the buff. Fires once per resolved target (the hook already
runs per-target inside `resolveAbilityCastLocked`).

#### B.4. Armor aggregation

`effectiveArmorLocked` aggregates `unit.Armor`, plus perk armor bonuses, plus
banner/aura bonuses, plus percent-armor multipliers. Bolstering Prayer adds a
new contribution: a flat int equal to `unit.PerkState.BolsteringPrayerArmor`
when `unit.PerkState.BolsteringPrayerRemaining > 0`. The cleanest insertion
point is a new helper `perkBonusArmorFromBuffsLocked(unit) int` that the
existing `effectiveArmorLocked` body adds to `flatBonus`. The helper returns
0 for non-buffed units.

Stacking rules:

- **Same buff, two casters**: refresh-max semantics handle this — the
  stronger of (existing, incoming) wins, never additive across casters.
  Same as Battle Prayer.
- **Different buffs (Battle Prayer + Bolstering Prayer on the same unit)**:
  independent fields, both apply. The healed unit gains both +AS *and* +armor
  if two Clerics — one of each flavor — heal them.
- **Other armor sources (`interlock`, `guardian_aura`, banners)**: additive
  with the buff, per existing flat-armor aggregation. The percent-armor
  multiplier in `effectiveArmorLocked` is applied to `unit.Armor` only, not
  to flat bonuses — Bolstering Prayer is a flat bonus and so is not scaled.

#### B.5. Cross-unit decay

`state.go Update()` currently decays `BattlePrayerRemaining` in the per-unit
loop:

```go
if unit.PerkState.BattlePrayerRemaining > 0 {
    unit.PerkState.BattlePrayerRemaining = math.Max(0, unit.PerkState.BattlePrayerRemaining-dt)
    if unit.PerkState.BattlePrayerRemaining == 0 {
        unit.PerkState.BattlePrayerMultiplier = 0
    }
}
```

A symmetric block is added immediately below it for
`BolsteringPrayerRemaining` / `BolsteringPrayerArmor`. The decay must live
here (not in `tickUnitPerkStateLocked`) because the buffed unit may not own
the `bolstering_prayer` perk.

#### B.6. Autocast — generalised recast-threshold

`autocast_selectors.go` currently has two `battle_prayer`-specific branches:

- `containsString(caster.PerkIDs, "battle_prayer")` gate in the full-HP focus
  selector.
- `Force-include focus when caster owns battle_prayer` gate in the
  multi-target builder.

Both branches generalise to "the caster owns at least one heal-buff perk
that wants to refresh on the focus." Concretely:

```go
type healBuffRecastSpec struct {
    perkID         string
    remainingField func(*UnitPerkState) float64
    cfgKey         string // "buffDurationSeconds" — both perks use the same key
}
var healBuffRecastSpecs = []healBuffRecastSpec{
    {"battle_prayer",     func(ps *UnitPerkState) float64 { return ps.BattlePrayerRemaining     }, "buffDurationSeconds"},
    {"bolstering_prayer", func(ps *UnitPerkState) float64 { return ps.BolsteringPrayerRemaining }, "buffDurationSeconds"},
}
```

The "owns any heal-buff perk" check loops over `healBuffRecastSpecs` and
returns true if any spec's `perkID` is in `caster.PerkIDs`. The "any stale
buff on the focus" check loops the same specs, evaluates
`remaining < recastThresholdPercent * buffDurationSeconds` for each owned
perk, and returns true if any is stale.

This keeps the existing "no recast for casters without a heal-buff perk"
default intact — a Cleric who rolls `sanctuary` or `mana_conduit` continues
to skip full-HP focus heals.

#### B.7. HUD icon

`perks_icons.go` currently emits the `battle_prayer` icon on any unit with
`BattlePrayerRemaining > 0`. A symmetric block emits `bolstering_prayer` on
any unit with `BolsteringPrayerRemaining > 0`. Both icons can co-exist on the
same unit; the existing buff strip already handles multiple icons.

## Risks

- **Power level of +50 armor.** Battle Prayer's +25% attack speed is a known
  quantity. +50 flat armor in a system where base unit armor is in the 0–10
  range is a *much* larger swing. The buff duration (5s) limits exposure,
  but the perk may need to be tuned down to e.g. +20 armor after playtesting.
  The catalog config makes this a one-line change with no spec movement.
- **Two heal-buff perks both wanting the focus recast slot.** Generalised
  recast-threshold logic means a Cleric who somehow ends up with both perks
  (debug spawn, future re-roll mechanic) will recast on a stale-either buff —
  that's a feature, not a bug, because both buffs are independently useful.
- **Save backward compat.** A live save with the old `greater_heal` perk id
  in `PerkIDs` is preserved (no migration), and the unit's `Abilities`
  already has `"greater_heal"` from the original swap. The orphaned perk id
  is harmless: every perk hook iterates `PerkIDs` and looks up
  `perkDefByID`, which returns nil for the removed id, and every hook is
  already defensively nil-checked. Acceptable per the project's iterative
  in-development phase.

## Open questions

None. All three open design choices were resolved during brainstorming:

- Swap timing → Bronze (every Cleric, day one).
- Pool composition → `bolstering_prayer` fills the freed slot. Pool stays at 4.
- Buff mechanics → mirror Battle Prayer exactly.
