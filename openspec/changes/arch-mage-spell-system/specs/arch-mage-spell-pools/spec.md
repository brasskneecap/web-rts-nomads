## ADDED Requirements

### Requirement: Spell pools are data-driven per (archetype, rank)

A spell-pool catalog file SHALL define, for each archetype and rank, a list of eligible spell ids, shaped `{ "<archetype>": { "bronze": [ids...], "silver": [ids...], "gold": [ids...] } }`. The loader SHALL validate that every listed id resolves to a registered `AbilityDef`, panicking at load with the offending archetype/rank/id on a miss (mirroring the catalog-strictness convention). Rank keys SHALL be validated against `bronze`/`silver`/`gold`. A missing archetype or a missing rank list SHALL resolve to an empty pool, not an error. Adding a spell to a pool SHALL require only editing this file — no assignment-logic change.

#### Scenario: Pool file is loaded into the keyed map

- **WHEN** the pool file declares `{ "arch_mage": { "bronze": ["fireball", "chain_lightning", "arcane_orb"] } }`
- **THEN** the `(arch_mage, bronze)` pool resolves to those three ids and `(arch_mage, silver)` resolves to an empty pool

#### Scenario: Unknown spell id in a pool panics at load

- **WHEN** a pool lists an id with no registered `AbilityDef`
- **THEN** catalog load panics naming the archetype, rank, and offending id

#### Scenario: Unknown rank key panics at load

- **WHEN** a pool declares a rank key other than `bronze`/`silver`/`gold`
- **THEN** catalog load panics naming the offending archetype and rank

### Requirement: One spell is randomly assigned per unit at promotion

When a unit is promoted to a rank whose archetype has a non-empty pool, the system SHALL roll exactly one spell id from that rank's pool, EXCLUDING spells the unit already knows, using the seeded progression RNG stream (`rngPerks`) — the same stream that drives path choice. The roll SHALL be per-unit: two units promoted on the same path MAY receive different spells. Map/pool iteration order SHALL NOT drive the outcome (candidate ids sorted before the roll). When every pool candidate is already known (or the pool is empty), the roll SHALL assign nothing and raise no error.

#### Scenario: Promotion assigns one pool spell

- **WHEN** an Adept is promoted onto the Arch Mage path at bronze and the bronze pool has three spells
- **THEN** exactly one of those three is assigned to the unit and becomes one of its known abilities

#### Scenario: Roll excludes already-known spells

- **WHEN** a unit already knows one spell from a rank's pool and is (re)rolled for that rank
- **THEN** the assigned spell is drawn only from the pool candidates the unit does not already know

#### Scenario: Assignment is per-unit and seed-deterministic

- **WHEN** two seeded runs promote the same sequence of units on the Arch Mage path
- **THEN** each unit receives the same assigned spell across runs, and different units within a run may receive different spells

#### Scenario: Exhausted pool assigns nothing

- **WHEN** a unit is rolled for a rank whose entire pool it already knows
- **THEN** no new spell is assigned and no error is raised

### Requirement: The random pick is recorded once and replayed deterministically

The random roll SHALL occur exactly once, at rank-up, and SHALL record its chosen spell id on a persistent per-unit, per-rank field. The RNG-free, idempotent ability recompute SHALL read the recorded pick rather than re-rolling. Re-running the recompute SHALL produce the same abilities with no additional RNG draw and no duplicate.

#### Scenario: Recompute reads the recorded pick without re-rolling

- **WHEN** the ability recompute runs repeatedly on a unit that has a recorded bronze pool pick
- **THEN** each run yields the same known-ability list including that pick, drawing no new RNG

#### Scenario: The roll draws RNG exactly once per rank

- **WHEN** a unit crosses a rank boundary once
- **THEN** exactly one pool RNG draw occurs for that rank and subsequent recomputes draw none

### Requirement: Assigned spells surface through the existing ability snapshot

A pool-assigned spell SHALL appear in the owner-facing ability snapshot through the existing `unit.Abilities` path, carrying its display name, icon, mana cost, autocast toggle, and cooldown. No new protocol/wire field SHALL be required for pool assignment.

#### Scenario: Assigned spell appears in the snapshot

- **WHEN** a unit is assigned a bronze pool spell on promotion
- **THEN** that spell appears in its ability snapshot with a working autocast toggle and cooldown, and no new protocol field was introduced
