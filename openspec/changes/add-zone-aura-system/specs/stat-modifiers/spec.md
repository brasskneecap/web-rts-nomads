## ADDED Requirements

### Requirement: Canonical stat-modifier shape

The system SHALL define one canonical stat-modifier shape, `StatModifier`, carrying a `stat` identifier (string), an `operation` (one of `add` or `multiply`), and a numeric `value`. Every gameplay system that contributes a stat bonus — beginning with zone auras and extensible to campaign, equipment, and event sources — SHALL express that bonus as a `StatModifier`, rather than introducing a system-specific bonus type. The shape SHALL be mirrored on the client for authoring and display.

#### Scenario: Additive modifier carries an add operation
- **WHEN** a source contributes `+2 healthRegen`
- **THEN** it is expressed as `{stat: "healthRegen", operation: "add", value: 2}`

#### Scenario: Multiplicative modifier carries a multiply operation
- **WHEN** a source contributes `+15% goldGatherRate`
- **THEN** it is expressed as `{stat: "goldGatherRate", operation: "multiply", value: 1.15}`

#### Scenario: Unknown operation rejected
- **WHEN** a `StatModifier` declares an `operation` other than `add` or `multiply`
- **THEN** it is rejected by validation rather than silently ignored at application time

### Requirement: Stat identifier registry

The system SHALL maintain a single registry of valid stat identifiers as the source of truth for which stats exist, their display labels, and whether a `multiply` operation is meaningful for each. The registry SHALL be enumerable in a stable (sorted) order for deterministic iteration and for driving editor/UI lists, and SHALL be shared between server validation and the client authoring/display surfaces. Adding a new stat SHALL require only a new registry entry plus wiring at that stat's read site, with no changes to the modifier shape, the aggregation logic, or the aura system.

#### Scenario: Initial combat stats are registered
- **WHEN** the registry is enumerated
- **THEN** it contains at least `healthRegen`, `manaRegen`, `moveSpeed`, `attackSpeed`, `damage`, `armor`, `maxHealth`, and `maxMana`

#### Scenario: Initial economy and worker stats are registered
- **WHEN** the registry is enumerated
- **THEN** it contains at least `goldGatherRate`, `woodGatherRate`, `gatherSpeed`, `workerMoveSpeed`, `unitProductionSpeed`, and `buildingConstructionSpeed`

#### Scenario: Unknown stat id rejected at load
- **WHEN** a `StatModifier` references a `stat` id absent from the registry
- **THEN** validation fails (at map load, naming the offending source) rather than the modifier being applied to nothing

#### Scenario: Adding a stat requires no aura-specific code
- **WHEN** a new stat id is added to the registry and wired at its read site
- **THEN** zone auras and any other modifier source can reference it with no change to the modifier shape, the aggregation, or the aura code

### Requirement: Modifier stacking rule

The system SHALL combine multiple `StatModifier`s targeting the same stat by a single documented rule: the effective stat value is the base value plus the sum of all `add` values, multiplied by the product of all `multiply` values — `effective = (base + Σ add) × Π multiply`. Combination SHALL be order-independent (commutative and associative across the contributing modifiers).

#### Scenario: Additive modifiers sum
- **WHEN** two sources contribute `+2 healthRegen` and `+3 healthRegen`
- **THEN** the combined additive contribution is `+5 healthRegen`

#### Scenario: Multiplicative modifiers multiply
- **WHEN** two sources contribute `×1.15` and `×1.10` to the same stat
- **THEN** the combined multiplier is `×1.265` (the product), not `×1.25` (a sum)

#### Scenario: Adds apply before multiplies
- **WHEN** a stat with base `10` receives `+2 add` and `×1.5 multiply`
- **THEN** the effective value is `(10 + 2) × 1.5 = 18`

#### Scenario: Combination is order-independent
- **WHEN** the same set of modifiers for a stat is combined in any order
- **THEN** the effective value is identical

### Requirement: Per-player aggregated modifier set

The system SHALL maintain, per player, an aggregated modifier set reduced from that player's currently active `StatModifier` sources, exposing per stat the accumulated additive total and accumulated multiplicative factor. The system SHALL provide an O(1) resolver that returns `(add, multiply)` for a `(playerID, stat)` pair, returning the identity `(0, 1)` for a stat with no active modifiers. The aggregated set SHALL be stored on the player alongside the existing per-player aggregates (e.g. damage multipliers) and initialised to the identity at player construction.

#### Scenario: Empty aggregate returns identity
- **WHEN** a player has no active stat modifiers and a read site resolves any stat
- **THEN** the resolver returns add `0` and multiply `1`, leaving the base value unchanged

#### Scenario: Aggregate reflects contributing sources
- **WHEN** a player has active sources contributing `+5 healthRegen` and `×1.15 goldGatherRate`
- **THEN** resolving `healthRegen` returns add `5`, and resolving `goldGatherRate` returns multiply `1.15`

#### Scenario: Resolver is a constant-time lookup
- **WHEN** a hot-path read site resolves a stat for a unit's owner each tick
- **THEN** the resolution is a constant-time lookup that does not iterate zones, units, or modifier sources

### Requirement: Stat pipeline integration without duplicated formulas

The system SHALL apply the per-player aggregated modifiers at each stat's existing point of use by folding the resolved `(add, multiply)` into the value already computed there, and SHALL NOT introduce a parallel or duplicate computation of any stat. For stats that have no current read site (the economy and worker stats), the system SHALL add a read site that applies the same `(base + add) × multiply` rule. Stats that are cached on the unit rather than read on demand (maximum health and maximum mana) SHALL be recomputed when the player's aggregate changes, preserving the current health/mana fraction.

#### Scenario: Existing stat reuses its read site
- **WHEN** an `armor` modifier is active for a unit's owner
- **THEN** the unit's effective armor reflects it through the existing armor computation, with no second armor formula introduced

#### Scenario: New economy stat applied at its read site
- **WHEN** a `goldGatherRate` multiplier is active for a worker's owner
- **THEN** the gold gained per gather reflects `(base + add) × multiply` at the worker gather read site

#### Scenario: Cached max stat recomputed on change
- **WHEN** a `maxHealth` modifier becomes active or inactive for a player
- **THEN** that player's units' maximum health is recomputed, preserving each unit's current health fraction

#### Scenario: No modifiers leaves all stats unchanged
- **WHEN** no stat modifiers are active anywhere in a match
- **THEN** every unit, worker, and building stat is identical to its value before this capability existed

### Requirement: Deterministic, tick-path-safe modifier evaluation

Stat-modifier aggregation and resolution SHALL be deterministic under a fixed seed and input sequence (no wall-clock time, no unseeded RNG, no reliance on map iteration order to drive values), SHALL run inside the tick loop under the state lock when invoked from simulation, and SHALL perform no I/O. Aggregation that iterates contributing sources SHALL use a stable order.

#### Scenario: Identical inputs produce identical aggregates
- **WHEN** two runs use the same seed, map, and command sequence
- **THEN** every player's aggregated modifier set is identical tick-for-tick across the runs

#### Scenario: Aggregation performs no I/O
- **WHEN** the aggregate is recomputed during simulation
- **THEN** no filesystem, network, or profile-store access occurs on that code path
