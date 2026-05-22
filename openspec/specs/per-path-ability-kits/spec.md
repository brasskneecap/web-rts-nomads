# per-path-ability-kits Specification

## Purpose

Defines catalog-driven, per-(path, rank) ability grants for the Acolyte unit
line: a strict loader, deterministic idempotent promotion grants, snapshot
surfacing with no protocol change, and an additive offensive `DamageAmount`
resolve step. The grant mechanism ships and is test-covered, but no authored
Cleric/Arch Mage starter kits exist — ability acquisition is deferred and every
`(path, rank)` resolves to an empty grant.
## Requirements
### Requirement: Path ability grants are catalog-defined per (path, rank)

A loader (`path_ability_defs.go`), structurally mirroring `path_defs.go`, SHALL read `catalog/units/human/acolyte/paths/<path>/abilities/<rank>.json` of shape `{ "grant": ["<abilityID>", …] }` into a `(path, rank) → []string` map. Rank names SHALL be validated against `bronze`/`silver`/`gold` with a load-time panic on an unknown rank, and a granted ability id that has no registered `AbilityDef` SHALL panic at load (mirroring `path_defs.go` strictness and the Phase-1 category-validation panic). A missing file for a `(path, rank)` cell SHALL resolve to an empty grant, not an error.

#### Scenario: Grant file is loaded into the keyed map

- **WHEN** `paths/cleric/abilities/silver.json` contains `{ "grant": ["greater_heal"] }`
- **THEN** the loader maps `(cleric, silver)` to `["greater_heal"]`

#### Scenario: Unknown ability id panics at load

- **WHEN** a grant file lists an ability id with no registered `AbilityDef`
- **THEN** catalog load panics with a message naming the offending file and id

#### Scenario: Unknown rank panics at load

- **WHEN** a grant file is placed under a rank directory other than `bronze`/`silver`/`gold`
- **THEN** catalog load panics naming the offending file and rank

#### Scenario: Missing cell is an empty grant

- **WHEN** no abilities file exists for a `(path, rank)` cell
- **THEN** that cell resolves to an empty grant and no error is raised

### Requirement: Promotion grants path abilities deterministically and idempotently

`assignUnitPathAbilitiesLocked(unit)` SHALL be the canonical recompute of `unit.Abilities` from its `(UnitType, ProgressionPath, Rank)`. It is called from `addUnitXPLocked` immediately after `assignUnitPerkLocked` (once per crossed rank) and from `DebugSpawnUnit` after path/rank assignment. The function SHALL produce identical results on repeated invocation (idempotent) and SHALL introduce no RNG — the only progression RNG remains the path *choice* in `assignUnitPathOnRankUpLocked`.

Composition (in order):

1. Start with the unit def's `Abilities` (e.g., acolyte → `["heal"]`).
2. If `pathAbilitiesByPath[unit.ProgressionPath]` is set (declared via the path JSON's `"abilities"` field — see the new requirement below), REPLACE the list with the path-level override. This covers the "cleric is a unit with greater_heal" baseline.
3. For each rank `R` the unit has reached (bronze → silver → gold up to its current rank), append any `(path, R)` rank-grants from `pathAbilityGrantsByKey` ADDITIVELY, in catalog list order, skipping any id already present. Rank-grants compose on top of the path-level override — they remain the right tool for "silver cleric also gains X" composable content.
4. Migrate `AutoCastEnabled` / `AbilityCooldowns` entries by position: when the new list at index `i` differs from the existing `unit.Abilities[i]`, migrate the old entry's value to the new key and delete the old key. Indices that don't change are skipped; indices beyond the old length are fresh slots with default state.

Granted/overridden ability entries SHALL initialise their autocast/cooldown maps lazily exactly as base abilities do — no spawn-path change is required.

#### Scenario: Path-level override fires on promotion

- **WHEN** an acolyte with `Abilities: ["heal"]` is promoted to the cleric path (any rank)
- **THEN** after `assignUnitPathAbilitiesLocked`, `unit.Abilities == ["greater_heal"]`

#### Scenario: Multi-rank catch-up composes overrides and grants without duplicates

- **WHEN** a single XP gain advances a unit on the cleric path from base to gold, with synthetic rank-grants for (cleric, silver) = `["synth_silver"]` and (cleric, gold) = `["synth_gold"]`
- **THEN** the final `unit.Abilities` is `["greater_heal", "synth_silver", "synth_gold"]` (override first, grants appended in rank order), with no duplicates

#### Scenario: Re-invocation is idempotent

- **WHEN** `assignUnitPathAbilitiesLocked` runs a second time on a unit whose abilities are already in the resolved state
- **THEN** `unit.Abilities` is unchanged (recompute produces the same list, no duplicates appended)

#### Scenario: Recompute is RNG-free and deterministic

- **WHEN** two seeded runs promote the same unit along the same path with the same inputs
- **THEN** the resulting `unit.Abilities` order and contents are identical between runs

#### Scenario: AutoCast and Cooldown state migrate across same-index swaps

- **WHEN** an acolyte with `AutoCastEnabled["heal"] = true` and `AbilityCooldowns["heal"] = 1.5` is promoted to (cleric, bronze)
- **THEN** after the recompute `AutoCastEnabled["greater_heal"] = true`, `AbilityCooldowns["greater_heal"] = 1.5`, and the `"heal"` keys are absent from both maps

### Requirement: Granted abilities surface with no protocol change

A path-granted ability SHALL appear in the owner-facing `AbilitySnapshot` via the existing `unit.Abilities` → `abilityStatesLocked` path, carrying its display name, icon, mana cost, per-ability autocast toggle, and cooldown. No protocol/wire field SHALL be added or changed for this capability.

#### Scenario: Post-promotion ability appears in the snapshot

- **WHEN** a unit is granted a path ability on promotion
- **THEN** that ability is present in its `AbilitySnapshot` with a working autocast toggle and cooldown fields, and no new protocol field was introduced

### Requirement: Path ability grants are deferred; only the mechanism ships

The per-(path, rank) ability-grant **mechanism** (the loader in `path_ability_defs.go`, the `(path, rank) → []string` lookup via `pathAbilityGrantsFor`, and the additive append step inside `assignUnitPathAbilitiesLocked`) SHALL remain present and behaviourally covered by tests, but no `paths/<path>/abilities/<rank>.json` rank-grant files SHALL exist for the Acolyte line; every `(path, rank)` cell SHALL resolve to an empty grant.

Greater Heal acquisition does NOT live in this rank-grant system — it is the cleric path's *path-level* baseline declared in `cleric.json`'s `"abilities"` override (see the new requirement below). The rank-grant system is reserved for future composable per-rank content like "silver cleric also gains X."

Acquisition of dormant offensive content (`arcane_bolt`) remains deferred. The dormant `arcane_bolt` `AbilityDef` SHALL remain valid (load + validate + resolvable by id) so the engine and dormant-def tests keep working. `greater_heal`'s `AbilityDef` is no longer dormant — it is granted to every cleric via the path-level override.

#### Scenario: No rank-grant file authored anywhere

- **WHEN** the catalog loads
- **THEN** `ListPathAbilityGrants()` returns an empty map (no `(path, rank)` cell has an authored grant file)

#### Scenario: Cleric and Arch Mage promotions don't append anything via the rank-grant system

- **WHEN** an Acolyte is promoted on the Cleric or Arch Mage path to any rank
- **THEN** the per-rank grant step inside `assignUnitPathAbilitiesLocked` appends nothing (no grant files exist); the cleric's `greater_heal` comes from the path-level override, not this step

#### Scenario: Rank-grant mechanism stays covered via a synthetic fixture

- **WHEN** a synthetic `(path, rank)` grant is injected at test time
- **THEN** `assignUnitPathAbilitiesLocked` appends the granted ids in catalog order on top of any path-level override, is idempotent across multi-rank catch-up and re-invocation, and is RNG-free — proving the mechanism without any authored catalog content

#### Scenario: Dormant ability defs remain valid

- **WHEN** the ability catalog loads
- **THEN** `arcane_bolt` loads and validates (registered `Category`/`DamageType`) and is resolvable by id, even though nothing grants it

### Requirement: Offensive abilities deal their `DamageAmount` on resolve

`AbilityDef` SHALL carry an optional `DamageAmount int` (JSON `damageAmount`, omitted/zero ⇒ no damage), symmetric to the existing `HealAmount`. On cast resolution (`resolveAbilityCastLocked`), an ability with `DamageAmount > 0` and a living target SHALL deal that damage through the existing authoritative damage pipeline (`applyUnitDamageWithSourceLocked`) attributed to the caster, with the ability's `DamageType` (resolved via `OrPhysical()` when unset). It SHALL NOT hand-roll damage: mitigation, the death pipeline, threat, and determinism are inherited from the shared pipeline. `HealAmount` and `DamageAmount` are independent resolve steps; an ability may declare either, both, or neither.

#### Scenario: Offensive ability damages its target on resolve

- **WHEN** an ability with `DamageAmount > 0` resolves on a living hostile target
- **THEN** the target loses HP via `applyUnitDamageWithSourceLocked`, the damage is attributed to the caster, and it is typed by the ability's `DamageType` (physical when unset)

#### Scenario: Absent or zero DamageAmount deals no damage

- **WHEN** an ability with no `damageAmount` (or `0`) resolves
- **THEN** no damage is applied — the field is additive and inert for every existing ability (e.g. `heal`)

#### Scenario: Damage routes through the shared pipeline

- **WHEN** an offensive ability kills its target
- **THEN** the normal death pipeline runs (same as a melee/splash kill) — no parallel death/threat path is introduced

### Requirement: Path JSON `"abilities"` field declares a path-level ability list override

A promotion path's JSON (`catalog/units/<faction>/<unit>/paths/<p>/<p>.json`) MAY declare an optional `"abilities"` field. When present, the field SHALL be loaded by `path_defs.go` into `pathAbilitiesByPath[<path>]` as the canonical ability list for units on that path — REPLACING the base unit def's `Abilities` rather than extending it.

The loader SHALL distinguish "field absent" (no override; the base unit's abilities are kept) from "field present but empty" (override active; the path strips base abilities). The catalog struct uses `*[]string` to preserve this distinction. Every entry MUST be a registered `AbilityDef` id; an empty string or an unregistered id SHALL panic at load (mirroring the projectile / damage-type validators in the same file).

The path-level override is symmetric with the existing per-path overrides for `projectile`, `damageType`, `projectileScale`, and `visionRange` (also in `path_defs.go`): a path declares what its units have, and the unit-side recompute reads the declaration. The semantic is "the cleric IS this unit," not "the cleric gets a delta applied to acolyte."

The cleric path SHALL declare `"abilities": ["greater_heal"]` in `cleric.json`. No other Acolyte-line path declares an override at this time.

#### Scenario: Cleric path override is loaded

- **WHEN** the catalog loads
- **THEN** `pathAbilitiesByPath["cleric"] == ["greater_heal"]`

#### Scenario: Paths without an override retain base abilities

- **WHEN** the catalog loads
- **THEN** `pathAbilitiesByPath` does NOT contain entries for any path that did not declare `"abilities"` in its JSON (e.g., `arch_mage`, `vanguard`, `berserker`, `trapper`, `marksman`)

#### Scenario: Unknown ability id in override panics at load

- **WHEN** a path JSON's `"abilities"` array lists an id with no registered `AbilityDef`
- **THEN** catalog load panics naming the offending file and id

#### Scenario: Empty string in override panics at load

- **WHEN** a path JSON's `"abilities"` array contains an empty string
- **THEN** catalog load panics naming the offending file

#### Scenario: Base acolyte without a path keeps base abilities

- **WHEN** an acolyte is at `ProgressionPath == "none"` and `assignUnitPathAbilitiesLocked` runs
- **THEN** `unit.Abilities` resolves to the unit def's base list (e.g., `["heal"]`) — no path-level override applies

