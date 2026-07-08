## ADDED Requirements

### Requirement: Spells expose modifier-targeting metadata

An `AbilityDef` SHALL carry a `Tags []string` field (JSON `tags`, omitted ⇒ empty) and its existing `DamageType` SHALL serve as the spell's **school** for modifier targeting. Tags are free-form authored strings (e.g. `"aoe"`, `"projectile"`, `"damage"`). The catalog loader SHALL remain flat and typed — no freeform `config` blob is introduced. Tags and school are read-only inputs to the modifier pipeline; adding a tag SHALL NOT change base cast behavior on its own.

#### Scenario: AbilityDef loads tags

- **WHEN** an ability JSON declares `"tags": ["aoe", "projectile"]`
- **THEN** the loaded `AbilityDef.Tags` equals `["aoe", "projectile"]` and the def otherwise loads and validates unchanged

#### Scenario: Absent tags default to empty

- **WHEN** an ability JSON omits `"tags"`
- **THEN** `AbilityDef.Tags` is empty and the ability is unaffected by any tag-targeted modifier

### Requirement: A modifier targets spells by id, school, or tag

A `SpellModifier` SHALL declare a target with optional `spellId`, `school`, and `tag` fields. A modifier applies to a spell when EVERY specified target field matches (unspecified fields are wildcards): `spellId` matches the ability id, `school` matches the ability's `DamageType`, and `tag` matches when the ability's `Tags` contains that value. A modifier with an empty target SHALL be rejected at load (a modifier that matches every spell is an authoring error).

#### Scenario: School-targeted modifier matches by damage type

- **WHEN** a modifier targets `{ school: "fire" }` and a spell has `DamageType: "fire"`
- **THEN** the modifier applies to that spell and does not apply to a `"lightning"` spell

#### Scenario: Tag-targeted modifier matches by tag membership

- **WHEN** a modifier targets `{ tag: "aoe" }` and a spell's `Tags` contains `"aoe"`
- **THEN** the modifier applies; a spell without the `"aoe"` tag is unaffected

#### Scenario: Multi-field target requires all fields to match

- **WHEN** a modifier targets `{ spellId: "fireball", tag: "aoe" }`
- **THEN** it applies only to a spell whose id is `fireball` AND whose tags include `aoe`

#### Scenario: Empty target is rejected

- **WHEN** a modifier declares no `spellId`, `school`, or `tag`
- **THEN** it is rejected at load time with a message identifying the offending modifier

### Requirement: Modifier fields are a closed typed enum

A `SpellModifier` SHALL name the value it modifies via a typed field enum, not a free-form string path. The recognized fields SHALL include at least: `manaCost`, `cooldown`, `castTime`, `damage`, `radius`, `projectileSpeed`, `duration`, `chainCount`, and `pullStrength`. An unrecognized field value SHALL be rejected at load. Each field maps to a concrete effective-spell value; a field that a given spell does not use is inert for that spell.

#### Scenario: Unknown field is rejected

- **WHEN** a modifier names a field not in the enum
- **THEN** it is rejected at load with a message naming the offending field

#### Scenario: Inert field is a no-op

- **WHEN** a `chainCount` modifier applies to a spell that has no chain behavior
- **THEN** the spell resolves unchanged (the modifier is inert, not an error)

### Requirement: Effective spell values are resolved at cast time without mutating the base def

The system SHALL resolve an `EffectiveSpell` at cast time by folding all applicable modifiers over the immutable base `AbilityDef`. The base def SHALL NOT be mutated. A modifier's `operation` SHALL be either `add` (the default when omitted) or `multiply`. For each field, all `add` modifiers SHALL be applied first, then all `multiply` modifiers. Because add-within-group and multiply-within-group are each commutative, the resolved value SHALL be independent of the order in which modifiers were collected — the pipeline SHALL NOT depend on collection or map-iteration order.

#### Scenario: Additive is the default operation

- **WHEN** a modifier omits `operation` and adds `10` to `damage` on a spell with base `damage` 50
- **THEN** the effective `damage` is 60 and the base def still reads `damage` 50

#### Scenario: Multiplicative operation applies after additive

- **WHEN** a spell has base `damage` 50, one `add 10` modifier and one `multiply 1.2` modifier
- **THEN** the effective `damage` is `(50 + 10) * 1.2 = 72`

#### Scenario: Two additive modifiers stack additively regardless of order

- **WHEN** two modifiers add `+20%` fire damage via `multiply 1.2` each on the same field
- **THEN** the effective value is `base * 1.2 * 1.2` and is identical for either collection order (multiply group is commutative)

#### Scenario: Base definition is never mutated

- **WHEN** the same spell is cast twice, the first cast under an active `+50% damage` modifier and the second with no modifiers
- **THEN** the second cast resolves to the unmodified base `damage` (the first cast left the base def untouched)

### Requirement: Modifiers are collected from source-agnostic providers at cast time

The pipeline SHALL gather modifiers for a `(caster, spell)` from a source-agnostic collector so that perks, buffs, and items feed the same resolution path. The collector SHALL be the single documented plug-in point for future spell-modifying content. Collection SHALL be deterministic and SHALL NOT read wall-clock time or unseeded randomness.

#### Scenario: A perk-provided modifier reaches the effective values

- **WHEN** a caster has an active source that provides a `{ school: "fire", field: "damage", multiply: 1.2 }` modifier and casts a fire spell
- **THEN** the effective `damage` reflects the `1.2` multiplier

#### Scenario: No active modifiers yields the base values

- **WHEN** a caster with no active modifiers casts a spell
- **THEN** every effective field equals the base def value
