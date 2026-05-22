## ADDED Requirements

### Requirement: A registered `caster` combat profile exists, `support`-derived with two intentional deltas

The `combatProfiles` registry SHALL contain a `"caster"` entry whose every field equals the `"support"` entry's value **except**:

- `Name`, which SHALL be `"caster"`;
- `MaxChaseDistance`, which SHALL be set near the `"archer"` profile's pursuit envelope and SHALL NOT be shrunk below the `"archer"` profile's `MaxChaseDistance` (rationale: leash self-clamps to the unit's `AttackRange` but `MaxChaseDistance` does not, so inheriting `support`'s smaller value would silently reduce caster pursuit range);
- `AoERadius`, which SHALL be `0`;
- the `AoECluster` target weight, which SHALL be `0` (the Acolyte's *current* kit is single-target — its basic attack fires the `fire_bolt` projectile and its only ability is `heal`, neither AoE — so `support`'s AoE tuning is inapplicable today; this is a current-kit decision, not a claim that casters are never AoE: the design intentionally anticipates future AoE caster abilities, at which point this profile is re-tuned).

The existing `"archer"` and `"support"` entries SHALL NOT be modified by this change.

Because `unit_defs.go` validates `UnitDef.CombatProfile` against `combatProfiles` at catalog load and panics on an unknown name, the `"caster"` entry MUST be present before any catalog unit references `combatProfile: "caster"`.

#### Scenario: `caster` profile is registered and resolvable

- **WHEN** the server resolves the combat profile for a unit whose `UnitDef.CombatProfile` is `"caster"`
- **THEN** `resolveCombatProfile` returns the `"caster"` profile (not a fallback) and the catalog loads without panic

#### Scenario: `caster` equals `support` except the four documented fields

- **WHEN** the `"caster"` and `"support"` profiles are compared field by field
- **THEN** every field is equal except `Name` (`"caster"` vs `"support"`), `MaxChaseDistance`, `AoERadius`, and the `AoECluster` weight

#### Scenario: `caster` pursuit range is not shrunk below the archer envelope

- **WHEN** the `"caster"` profile's `MaxChaseDistance` is compared to the `"archer"` profile's `MaxChaseDistance`
- **THEN** the caster value is not less than the archer value, and the caster's `AoERadius` and `AoECluster` weight are both `0`

#### Scenario: `caster` retreats from melee like `support`

- **WHEN** a `caster`-profiled unit has an enemy melee attacker inside its retreat trigger range
- **THEN** the unit kites away (non-zero `RetreatDistance` / `RetreatTriggerMeleeRange`, inherited from `support`) rather than standing still as the `archer` profile does

#### Scenario: `archer` and `support` profiles are unchanged

- **WHEN** the `"archer"` and `"support"` profile entries are inspected after this change
- **THEN** their field values are byte-identical to before the change

### Requirement: The Acolyte unit uses the `caster` profile and archetype

The Acolyte catalog entry (`catalog/units/human/acolyte/acolyte.json`) SHALL set both `"archetype"` and `"combatProfile"` to `"caster"` (previously both `"archer"`). The Acolyte's `UnitType` and all other catalog fields SHALL be unchanged. The Cleric and Arch Mage promotion paths are the same `UnitDef` plus rank stat-multipliers (their JSON files carry no `type`/`archetype`/`combatProfile`); they inherit the `caster` profile because `resolveCombatProfile` resolves through `acolyte.json` for `UnitType == "acolyte"`, with no separate catalog edit and no `combatProfile` field in the path files.

#### Scenario: Acolyte resolves to the caster profile

- **WHEN** an Acolyte unit's combat profile is resolved
- **THEN** the resolved profile is `"caster"`

#### Scenario: Cleric and Arch Mage resolve to the caster profile without a path-file edit

- **WHEN** an Acolyte is promoted to the Cleric or Arch Mage path and its combat profile is resolved
- **THEN** the resolved profile is `"caster"`, resolved via `acolyte.json` (the path files contain no `combatProfile`), with no separate catalog change

#### Scenario: Acolyte kites melee instead of standing and dying

- **WHEN** an enemy melee unit closes into an Acolyte that previously (archer profile) would have stood still
- **THEN** the Acolyte retreats while remaining able to act, instead of standing in place until it dies

#### Scenario: Acolyte fallback archetype is also caster

- **WHEN** the Acolyte's archetype is used as the profile-resolution fallback key (the `combatProfile`-empty path)
- **THEN** the fallback key is `"caster"`, consistent with its explicit `combatProfile`

### Requirement: Archetype scoping excludes the caster line from `archer`-only upgrades (intended role separation)

Because `unit.Archetype` is the match key for archetype-scoped upgrades (`upgradeScopeArchetype` in `upgrade_apply.go`), flipping the Acolyte's `archetype` from `"archer"` to `"caster"` SHALL remove the Acolyte / Cleric / Arch Mage from every `archetype: "archer"` upgrade — specifically the live `swift_strikes_*` attack-speed upgrades. This exclusion is a **design goal** of the archetype, not a tolerated side-effect: a backline caster SHALL NOT inherit archer-only upgrades, and `archetype` is the boundary that enforces this. No caster-scoped upgrade is added in Phase 1 (a caster upgrade line is future content, not part of this change).

#### Scenario: Acolyte no longer matches the archer-scoped Swift Strikes upgrade

- **WHEN** the archetype-scoped upgrade match (`upgradeScopeArchetype`) is evaluated for an Acolyte (now `archetype: "caster"`) against an `archetype: "archer"` upgrade such as `swift_strikes_common`
- **THEN** the upgrade does not match the Acolyte

#### Scenario: No archer-only upgrade leaks onto the caster Acolyte

- **WHEN** every archetype-scoped upgrade in the catalog is evaluated against an Acolyte with `archetype: "caster"`
- **THEN** no `archetype: "archer"` upgrade matches, and no archetype-scoped upgrade matches at all (no caster-scoped upgrade exists yet) — this is the intended role separation, with caster-scoped upgrades left to future content

### Requirement: AI scoring treats `caster` exactly as `support`

`unitStrategicValue` SHALL grant a `caster`-profiled unit the same backline value bonus it grants a `support`-profiled unit. `unitTypePreference` SHALL, for a `caster` attacker, apply the same target preferences it applies for a `support` attacker; and wherever a target being `support`-profiled grants a prioritisation bonus to the attacker, a target being `caster`-profiled SHALL grant the identical bonus. No scoring branch SHALL special-case `caster` differently from `support`. The Decision-1 profile-number deltas (`MaxChaseDistance` / `AoERadius` / `AoECluster`) SHALL NOT affect **strategic-value or type-preference** scoring, because `unitStrategicValue` and `unitTypePreference` do not read those fields. Those deltas **do** influence target-selection scoring via `scoreUnitTargetLocked` / `scoreBuildingTargetLocked` (the `MaxChaseDistance` reach term and the `AoERadius`/`AoECluster` cluster term); that difference is the intended chase-envelope change (proposal Delta 4) plus the deliberate removal of AoE-cluster bias from a unit whose current kit is single-target — it is expected, not a leak, and SHALL NOT be asserted equal between `caster` and `support`.

#### Scenario: Caster strategic value equals support strategic value

- **WHEN** `unitStrategicValue` is computed for an otherwise-identical unit under the `caster` profile and under the `support` profile
- **THEN** the two values are equal (the profile-number deltas do not leak into strategic value)

#### Scenario: Caster attacker uses support target preferences

- **WHEN** `unitTypePreference` is evaluated with a `caster`-profiled attacker against a given target
- **THEN** the returned preference equals what a `support`-profiled attacker would return against the same target

#### Scenario: A caster target is prioritised like a support target

- **WHEN** an archer-, mage-, cavalry-, or skirmisher-profiled unit evaluates `unitTypePreference` against a `caster` target versus a `support` target
- **THEN** the prioritisation bonus is identical for the two target profiles

#### Scenario: Non-caster unit scoring is unaffected in isolation

- **WHEN** no `caster`-profiled unit is present in a scenario
- **THEN** every unit's strategic value and type preference is identical to before this change
