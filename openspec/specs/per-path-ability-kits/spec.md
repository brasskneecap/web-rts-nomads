# per-path-ability-kits Specification

## Purpose

Defines catalog-driven, per-(path, rank) ability grants for the Apprentice unit
line: a strict loader, deterministic idempotent promotion grants, snapshot
surfacing with no protocol change, and an additive offensive `DamageAmount`
resolve step. The grant mechanism ships and is test-covered, but no authored
Cleric/Arch Mage starter kits exist — ability acquisition is deferred and every
`(path, rank)` resolves to an empty grant.

## Requirements

### Requirement: Path ability grants are catalog-defined per (path, rank)

A loader (`path_ability_defs.go`), structurally mirroring `path_defs.go`, SHALL read `catalog/units/human/apprentice/paths/<path>/abilities/<rank>.json` of shape `{ "grant": ["<abilityID>", …] }` into a `(path, rank) → []string` map. Rank names SHALL be validated against `bronze`/`silver`/`gold` with a load-time panic on an unknown rank, and a granted ability id that has no registered `AbilityDef` SHALL panic at load (mirroring `path_defs.go` strictness and the Phase-1 category-validation panic). A missing file for a `(path, rank)` cell SHALL resolve to an empty grant, not an error.

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

- **WHEN** an Apprentice on the Cleric path advances to a rank whose Cleric grant is `["greater_heal"]`
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

### Requirement: Path ability grants are deferred; only the mechanism ships

The per-path ability-grant **mechanism** (the loader, `assignUnitPathAbilitiesLocked`, the `(path,rank)` grant lookup) SHALL remain present and behaviourally covered by tests, but the catalog SHALL NOT grant any real ability on promotion. No `paths/<path>/abilities/<rank>.json` grant files SHALL exist for the Apprentice line; every `(path,rank)` SHALL resolve to an empty grant. Acquisition of the dormant abilities is explicitly deferred: `greater_heal` is intended as a future perk-gated *replacement* of base `heal` (the gating perk is undesigned), and `arcane_bolt`'s acquisition is TBD. The dormant `greater_heal` / `arcane_bolt` `AbilityDef`s SHALL remain valid (load + validate + resolvable by id) so the engine and dormant-def tests keep working.

#### Scenario: No promotion grants any ability by default

- **WHEN** an Apprentice is promoted on the Cleric or Arch Mage path to any rank
- **THEN** `assignUnitPathAbilitiesLocked` appends nothing (no grant files exist) and the unit's `Abilities` is unchanged by the grant step

#### Scenario: Grant mechanism stays covered via a synthetic fixture

- **WHEN** a synthetic `(path,rank)` grant is injected at test time
- **THEN** `assignUnitPathAbilitiesLocked` appends the granted ids in catalog order, is idempotent (append-iff-absent across multi-rank catch-up and re-invocation), and is RNG-free — proving the mechanism without any authored catalog content

#### Scenario: Dormant ability defs remain valid

- **WHEN** the ability catalog loads
- **THEN** `greater_heal` and `arcane_bolt` load and validate (registered `Category`/`DamageType`) and are resolvable by id, even though nothing grants them

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
