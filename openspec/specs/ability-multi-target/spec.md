# ability-multi-target Specification

## Purpose

Extends `AbilityDef` with an optional `TargetCount int` field (default `1`). When `TargetCount > 1`, the ability resolver applies its primary effect to up to N lowest-HP-percent valid allies within cast range, with explicit prioritization for a caller-provided "force-include" target (used by `cleric-focus-target`). Single-target abilities are byte-identical to the pre-change behavior. The `TargetCount` value is surfaced on `AbilitySnapshot` so the client can render multi-target cursors and predicted-target indicators.

## Requirements

### Requirement: `AbilityDef` carries an optional `TargetCount` field

`AbilityDef` SHALL have an optional integer field `TargetCount`, serialised as the JSON key `"targetCount"`. When the JSON key is omitted or set to a value `< 1`, `TargetCount` SHALL resolve to `1` (single-target). When `TargetCount > 1`, the ability is multi-target and the resolver SHALL apply its primary effect (heal, damage, etc.) to up to `TargetCount` valid targets selected per the ability's selector rules.

`TargetCount == 1` behavior SHALL be byte-identical to the pre-change behavior for every existing ability — no existing ability JSON file is required to add the key.

#### Scenario: Ability without `targetCount` defaults to single-target

- **WHEN** an ability JSON file omits the `"targetCount"` key
- **THEN** the definition loads with `TargetCount == 1` and the resolver applies the effect to exactly one target

#### Scenario: Ability with `targetCount: 3` resolves to multi-target

- **WHEN** an ability JSON file declares `"targetCount": 3`
- **THEN** the definition loads with `TargetCount == 3` and the resolver invokes the multi-target path

#### Scenario: Out-of-range targetCount clamps to single-target

- **WHEN** an ability JSON file declares `"targetCount": 0` or a negative value
- **THEN** the definition loads with `TargetCount == 1` (defensive normalization)

### Requirement: Multi-target selector returns lowest-HP-percent valid allies

When the resolver is invoked for an ability with `TargetCount > 1`, `CanTargetAllies == true`, and `Category == "heal"`, the selector SHALL produce up to `TargetCount` allied units that satisfy: same team as caster, `Visible == true`, `HP > 0`, and within `def.Resolve(caster).CastRange` of the caster. Selection SHALL order candidates by ascending HP percent (`HP / MaxHP`) and break ties by ascending `unit.ID`. The selector SHALL exclude candidates that are at full HP unless the caster's perks explicitly opt them in (see `cleric-bronze-perks` Battle Prayer recast-threshold rule).

#### Scenario: Three injured allies in range all receive the effect

- **WHEN** a caster with `TargetCount: 3` Heal stands within range of three injured allies (HP < MaxHP)
- **THEN** the resolver applies heal to all three, ordered by ascending HP percent

#### Scenario: Only one ally in range with three-target ability

- **WHEN** a caster with `TargetCount: 3` Heal stands within range of only one injured ally
- **THEN** the resolver applies heal to that single ally; the cast does not fail

#### Scenario: Full-HP allies are excluded by default

- **WHEN** a caster with `TargetCount: 3` Heal stands near two injured allies and one full-HP ally
- **THEN** only the two injured allies receive heal; the full-HP ally is not a target

#### Scenario: Tied HP percent breaks by unit ID

- **WHEN** two allies have the same HP percent and only one slot remains
- **THEN** the ally with the lower `unit.ID` is chosen

### Requirement: Caller-provided force-include target is added to the multi-target set

The multi-target selector SHALL accept an optional `forceIncludeUnitID int` argument. When `forceIncludeUnitID != 0` and resolves to a valid same-team ally in range, that unit SHALL be present in the returned target set even if it is at full HP or would otherwise be ranked below the cutoff. If the natural selector already produces `TargetCount` targets and the force-include unit is not among them, the force-include unit SHALL replace the highest-HP-percent natural pick. Force-include SHALL NOT exceed `TargetCount` (the set size is hard-capped).

#### Scenario: Force-include displaces highest-HP-percent natural pick

- **WHEN** the natural selector picks allies A (10% HP), B (40% HP), C (60% HP) for a `TargetCount: 3` cast, and the caller force-includes ally D at 100% HP
- **THEN** the returned set is {A, B, D}; C is dropped because D displaced the highest-HP natural pick

#### Scenario: Force-include already present is a no-op

- **WHEN** the force-include unit is already among the natural picks
- **THEN** the returned set is unchanged; the unit appears exactly once

#### Scenario: Force-include of invalid unit is ignored

- **WHEN** `forceIncludeUnitID` resolves to a dead, invisible, out-of-range, or wrong-team unit
- **THEN** the force-include is silently ignored and the natural selector result is returned

### Requirement: Multi-target resolution applies the post-cast hook per target

`resolveAbilityCastLocked` SHALL invoke the per-target effect (heal, damage, status) once per selected target. After each target's effect resolves, the resolver SHALL call `onPerkAbilityResolvedLocked(caster, def, target)` so perk-conditioned post-effects can fire per target (e.g., Battle Prayer applies its buff to every healed target). The hook SHALL be called exactly once per `(target, cast)` pair, never with `nil` target or with a target that died from a prior effect in the same cast.

#### Scenario: Greater Heal hits three allies and fires three post-cast hooks

- **WHEN** a `TargetCount: 3` Heal resolves on three allies
- **THEN** `onPerkAbilityResolvedLocked` is called three times, once per target, after that target's heal lands

#### Scenario: Target dying mid-resolve is skipped for subsequent hooks

- **WHEN** the per-target effects are applied in selector order and the second target dies from an unrelated tick event before the resolver reaches the third
- **THEN** the third target still receives its effect and its hook fires; the dead second target's hook does not re-fire

### Requirement: `TargetCount` is surfaced on ability snapshots

`AbilitySnapshot` SHALL include a `TargetCount` integer field, copied from `AbilityDef.TargetCount` at snapshot time, so the client can render multi-target cursors, tooltips, and predicted-target indicators. A snapshot for a single-target ability SHALL report `TargetCount: 1`. This adds a field to the wire shape but does not affect any other snapshot semantics.

#### Scenario: Single-target ability snapshot reports TargetCount 1

- **WHEN** a unit owns `heal` (default `TargetCount`)
- **THEN** its ability snapshot for `heal` has `TargetCount == 1`

#### Scenario: Greater Heal snapshot reports TargetCount 3

- **WHEN** a unit owns `greater_heal` with `"targetCount": 3` in the catalog
- **THEN** its ability snapshot for `greater_heal` has `TargetCount == 3`
