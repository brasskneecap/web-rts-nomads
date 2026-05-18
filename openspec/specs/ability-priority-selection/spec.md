# ability-priority-selection Specification

## Purpose

Defines how a multi-ability autocaster selects which ready ability to cast each
tick: a gather → score → pick pipeline that is deterministic, has an activation
floor, preserves single-ability no-regression, and scores candidates per
`AbilityCategory`.

## Requirements

### Requirement: Autocast selects the highest-scored ready ability

`tickUnitAutoCastLocked` SHALL select the ability to autocast by **gather → score → pick**, not by first-ready order. It SHALL first gather every candidate from `unit.Abilities` in slot order that passes the **unchanged** gates — autocast enabled, `SupportsAutoCast`, off cooldown, `unit.CurrentMana >= def.ManaCost`, and a non-nil target from `resolveAutoCastTargetLocked` — then score each `(ability, target)` candidate, then cast the single highest-scored candidate. At most one cast SHALL be initiated per unit per tick, and never while `unit.CastAbilityID != ""`. The pre-flight guards (nil unit, `HP <= 0`, no autocast-enabled abilities, cast in progress) SHALL be unchanged. On the chosen cast the ability cooldown SHALL be armed exactly as before.

#### Scenario: Higher-scored ready ability wins over an earlier slot

- **WHEN** a unit has two autocast-enabled, ready, valid-target abilities and the later-slot ability scores higher for the current world state
- **THEN** the later-slot ability is the one cast that tick

#### Scenario: Gates are applied before scoring

- **WHEN** an ability is autocast-enabled but on cooldown, mana-unaffordable, or has no selector target
- **THEN** it is not a candidate and is never scored or cast that tick

#### Scenario: One cast per unit per tick, not while casting

- **WHEN** a unit already has `CastAbilityID != ""` or has initiated a cast this tick
- **THEN** no further autocast is initiated that tick

### Requirement: Deterministic tiebreak and activation floor

Candidate scoring SHALL break ties deterministically by ascending `unit.Abilities` slot index, then by ability id. If the highest candidate score is below `minActivationScore`, the unit SHALL cast nothing that tick (the basic attack proceeds via the unchanged combat AI). Selection SHALL read only live unit/world state and the ordered `unit.Abilities` slice; it SHALL NOT use map iteration order, wall-clock, or unseeded randomness.

#### Scenario: Equal scores break by slot then id

- **WHEN** two candidates produce an equal score
- **THEN** the one with the lower `unit.Abilities` slot index is chosen; if slot indices were equal, the lexicographically smaller ability id is chosen

#### Scenario: Below the activation floor casts nothing

- **WHEN** every ready candidate scores below `minActivationScore`
- **THEN** no ability is autocast that tick and the unit's basic-attack behaviour is unaffected

#### Scenario: Selection is replay-deterministic

- **WHEN** a seeded match with a multi-ability autocaster is run twice with the same seed and inputs
- **THEN** the set of ticks each ability is cast on is identical between the two runs

### Requirement: No behavioural regression for a single autocast ability

With exactly one autocast-enabled candidate, highest-scored-ready SHALL be behaviourally identical to the prior first-ready logic: the lone candidate SHALL be cast on exactly the ticks it would have been cast before. `minActivationScore` and the per-category formulas (including the empty/unknown-category fallback) SHALL be set so that any currently-castable ability with a valid selector target scores strictly above `minActivationScore`.

#### Scenario: Heal-only Apprentice is byte-identical pre/post

- **WHEN** a seeded match with a heal-only (un-promoted) Apprentice is run before and after this change with the same seed and inputs
- **THEN** the set of ticks heal is auto-cast on is identical between the two runs

#### Scenario: A lone uncategorised ability still fires

- **WHEN** a unit's only autocast candidate has an empty/unregistered `Category` but is ready with a valid target
- **THEN** it scores above `minActivationScore` and is cast, exactly as first-ready would have

### Requirement: Per-category scoring semantics

`ability_priority.go` SHALL score a candidate by `def.Category`: `heal` increases with the target's HP deficit (with a bounded bonus for additional nearby damaged allies); `offensive` increases with target strategic value, clustering, and finishing potential; `buff_ally` is high when the target lacks the buff and is in combat and ~0 otherwise; `summon` derives from local force deficit (target is self); an empty or unregistered category resolves to a conservative fallback that still clears `minActivationScore` for a lone valid-target candidate. Per-category weights SHALL live in a small Go table keyed by `AbilityCategory`, not on `CombatProfile`.

#### Scenario: Heal scores by ally HP deficit

- **WHEN** two heal candidates target allies at different HP percentages
- **THEN** the candidate targeting the lower-HP-percentage ally scores higher

#### Scenario: Heal vs offensive chosen by situation

- **WHEN** a caster has a `heal` and an `offensive` ability both ready, and a nearby ally is critically low
- **THEN** the `heal` candidate outscores the `offensive` candidate and is the one cast; **and WHEN** no ally is damaged but a valid enemy is in range, the `offensive` candidate is the one cast
