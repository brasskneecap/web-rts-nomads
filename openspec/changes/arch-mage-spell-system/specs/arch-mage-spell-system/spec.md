## ADDED Requirements

### Requirement: The Arch Mage bronze pool defines fireball, chain_lightning, and arcane_orb

The Arch Mage bronze spell pool SHALL contain three authored spells — `fireball`, `chain_lightning`, and `arcane_orb` — each a registered `AbilityDef` with its own catalog entry, damage type (school), tags, mana cost, cast time, and cooldown. The silver and gold Arch Mage pools SHALL be empty in this change (higher-tier content is deferred). Each bronze spell SHALL be castable by an Arch Mage that was assigned it, resolving through the existing ability-cast lifecycle (mana spend, cast time, cooldown, autocast).

#### Scenario: Bronze pool contains the three spells

- **WHEN** the catalog loads
- **THEN** the `(arch_mage, bronze)` pool resolves to `fireball`, `chain_lightning`, and `arcane_orb`, and the silver/gold pools are empty

#### Scenario: An assigned bronze spell is castable

- **WHEN** an Arch Mage assigned a bronze spell casts it on a valid target
- **THEN** the cast spends mana, honors cast time and cooldown, and resolves its effect through the shared ability pipeline

### Requirement: Fireball deals area splash damage on resolve

`fireball` SHALL launch an ability projectile at its target and, on impact, deal area damage to hostiles within its radius by reusing the existing splash-damage helper (`applySplashDamageLocked`) — not a hand-rolled damage loop. Damage SHALL route through the authoritative damage pipeline (mitigation, death, threat, determinism inherited). Its `radius` and `damage` SHALL be modifier-eligible fields.

#### Scenario: Fireball damages clustered enemies

- **WHEN** `fireball` resolves on a target with other hostiles within its radius
- **THEN** the primary target and the nearby hostiles take damage via the shared splash pipeline, attributed to the caster and typed by the spell's school

#### Scenario: Fireball kills route through the normal death pipeline

- **WHEN** a `fireball` splash hit kills a unit
- **THEN** the standard death/threat pipeline runs, with no parallel death path introduced

### Requirement: Chain_lightning bounces between enemies on resolve

`chain_lightning` SHALL deliver a bouncing chain that reuses the existing bounce mechanic (the `lightning_chain` proc / beam-bounce path with `bounceCount` / `bounceRange` / `bounceDamageFalloff`) rather than a new chaining implementation. Bounce target selection SHALL be deterministic (no map-iteration-order dependence). Its `chainCount` and `damage` SHALL be modifier-eligible fields.

#### Scenario: Chain lightning arcs to nearby enemies

- **WHEN** `chain_lightning` resolves on a target with additional hostiles within bounce range
- **THEN** the effect arcs to up to `bounceCount` further enemies, each hop losing `bounceDamageFalloff` damage, via the existing bounce mechanic

#### Scenario: Bounce selection is deterministic

- **WHEN** two seeded runs cast `chain_lightning` in the same configuration
- **THEN** the same units are hit in the same order across runs

### Requirement: Arcane_orb pulls enemies toward its center on resolve

`arcane_orb` SHALL apply the forced-displacement (pull) effect to hostiles near its target/center on resolve, pulling them toward the center over a duration via the forced-displacement subsystem. Its `pullStrength`, `radius`, and `duration` SHALL be modifier-eligible fields. `arcane_orb` SHALL only pull hostiles relative to the caster.

#### Scenario: Arcane orb pulls nearby enemies inward

- **WHEN** `arcane_orb` resolves near a cluster of hostiles
- **THEN** those hostiles are displaced toward the orb's center over the effect's duration, and allies/caster are unaffected

### Requirement: Spell definitions carry modifier-targeting metadata

Each Arch Mage bronze spell SHALL declare a `DamageType` (its school) and `Tags` describing its behavior (e.g. `fireball` tagged `aoe`/`projectile`, `chain_lightning` tagged `chain`, `arcane_orb` tagged `cc`/`aoe`) so that future school- and tag-targeted modifiers resolve against them through the spell-modifier pipeline.

#### Scenario: A school-targeted modifier reaches a bronze spell

- **WHEN** a `{ school: <fireball's damage type>, field: "damage", multiply: 1.2 }` modifier is active and `fireball` is cast
- **THEN** the effective damage reflects the multiplier while the base def is unchanged

#### Scenario: A tag-targeted modifier reaches the tagged spells

- **WHEN** a `{ tag: "aoe" }` modifier is active
- **THEN** it applies to `fireball` and `arcane_orb` (tagged `aoe`) and not to `chain_lightning` (not tagged `aoe`)
