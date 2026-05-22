## ADDED Requirements

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

`assignUnitPathAbilitiesLocked(unit)` SHALL be called from `addUnitXPLocked` immediately after `assignUnitPerkLocked`, once per crossed rank. For the unit's resolved `(path, rank)` it SHALL append each granted ability id to `unit.Abilities` **iff not already present**, in catalog list order. It SHALL introduce no RNG (the only progression RNG remains the existing path *choice*). Granted spell abilities SHALL initialise their autocast/cooldown state lazily exactly as base abilities do (no spawn-path change).

#### Scenario: Abilities granted on reaching a rank

- **WHEN** an Acolyte on the Cleric path advances to a rank whose Cleric grant is `["greater_heal"]`
- **THEN** `greater_heal` is appended to `unit.Abilities` after that rank-up

#### Scenario: Multi-rank catch-up grants each crossed rank with no duplicates

- **WHEN** a single XP gain advances a unit past several rank thresholds at once
- **THEN** every crossed rank's grants are applied and no ability id appears more than once in `unit.Abilities`

#### Scenario: Re-invocation is idempotent

- **WHEN** `assignUnitPathAbilitiesLocked` runs again for a unit that already holds its `(path, rank)` grants
- **THEN** `unit.Abilities` is unchanged (no duplicates appended)

#### Scenario: Grants are RNG-free and deterministic

- **WHEN** two seeded runs promote the same unit along the same path with the same inputs
- **THEN** the resulting `unit.Abilities` order and contents are identical between runs

### Requirement: Granted abilities surface with no protocol change

A path-granted ability SHALL appear in the owner-facing `AbilitySnapshot` via the existing `unit.Abilities` → `abilityStatesLocked` path, carrying its display name, icon, mana cost, per-ability autocast toggle, and cooldown. No protocol/wire field SHALL be added or changed for this capability.

#### Scenario: Post-promotion ability appears in the snapshot

- **WHEN** a unit is granted a path ability on promotion
- **THEN** that ability is present in its `AbilitySnapshot` with a working autocast toggle and cooldown fields, and no new protocol field was introduced

### Requirement: Cleric and Arch Mage starter kits are authored

The catalog SHALL define at least one Cleric heal-line ability grant and at least one Arch Mage offensive ability grant. The Arch Mage offensive ability SHALL use the already-registered `closest_enemy_in_range` autocast selector. Each authored ability SHALL have a valid `AbilityDef` (including a registered `Category`).

#### Scenario: Cleric path grants a heal-line ability

- **WHEN** an Acolyte promoted on the Cleric path reaches the granting rank
- **THEN** it holds at least one `heal`-category ability beyond base `heal`

#### Scenario: Arch Mage path grants an offensive ability

- **WHEN** an Acolyte promoted on the Arch Mage path reaches the granting rank
- **THEN** it holds at least one `offensive`-category ability whose `AutoCastTargetSelector` is `closest_enemy_in_range`

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
