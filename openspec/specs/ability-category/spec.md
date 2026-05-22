# ability-category Specification

## Purpose

Defines the optional `AbilityCategory` taxonomy for abilities: an extensible
registered string enum, an optional `Category` field on `AbilityDef` validated
at catalog load, and the tagging of the `heal` ability. The `Category` field is
read by the autocast priority scorer (`ability-priority-selection`) to drive
multi-ability autocast selection; it does not affect autocast gating or any
wire/snapshot shape.

## Requirements

### Requirement: `AbilityCategory` is an extensible, registered string enum

The system SHALL define an `AbilityCategory` string type with registered values `heal` (`AbilityCategoryHeal`), `buff_ally` (`AbilityCategoryBuffAlly`), `summon` (`AbilityCategorySummon`), and `offensive` (`AbilityCategoryOffensive`). It SHALL follow the existing `DamageType` registry pattern: a registry map, a `RegisterAbilityCategory` function that panics on an empty id, an `IsValidAbilityCategory` predicate, and an `AbilityCategories()` accessor returning the registered values in a stable sorted order.

The empty value (`""`) SHALL be the reserved "unspecified" default: it SHALL NOT be registerable and `IsValidAbilityCategory("")` SHALL return false, identical to `DamageType` semantics.

#### Scenario: Registered categories validate

- **WHEN** `IsValidAbilityCategory` is called with `heal`, `buff_ally`, `summon`, or `offensive`
- **THEN** it returns true for each

#### Scenario: Empty and unknown values do not validate

- **WHEN** `IsValidAbilityCategory` is called with `""` or an unregistered string
- **THEN** it returns false

#### Scenario: Registering an empty category panics

- **WHEN** `RegisterAbilityCategory("")` is called
- **THEN** it panics, reserving the empty value as "unspecified"

#### Scenario: Accessor returns a stable sorted list

- **WHEN** `AbilityCategories()` is called
- **THEN** it returns all registered categories in deterministic sorted order

### Requirement: `AbilityDef` carries an optional `Category` field validated at load

`AbilityDef` SHALL have a `Category AbilityCategory` field serialised as the optional JSON key `"category"` (omitted when empty). At ability catalog load, the loader SHALL panic if a definition's `Category` is non-empty and not a registered category, mirroring the existing `damageType` load validation.

A definition with no `"category"` key SHALL load successfully with `Category == ""`. `Category` is now **read by the autocast priority scorer** (`ability-priority-selection`): it influences which ready ability a multi-ability autocaster selects each tick. It SHALL still NOT affect autocast *gating* (mana, cooldown, `SupportsAutoCast`, selector predicate) nor any wire/snapshot shape, and an empty/unregistered `Category` SHALL resolve to the scorer's conservative fallback so a lone valid-target candidate still fires (preserving the single-ability no-regression invariant).

#### Scenario: Ability without a category loads with empty default

- **WHEN** an ability JSON file omits the `"category"` key
- **THEN** the definition loads successfully and its `Category` is `""`

#### Scenario: Ability with a valid category loads

- **WHEN** an ability JSON file declares `"category": "heal"`
- **THEN** the definition loads successfully with `Category == AbilityCategoryHeal`

#### Scenario: Ability with an invalid category panics at load

- **WHEN** an ability JSON file declares a non-empty `"category"` that is not registered
- **THEN** the catalog load panics with a message naming the offending file and category

#### Scenario: Category drives autocast priority selection

- **WHEN** a multi-ability autocaster has two ready, valid-target candidates with different `Category` values
- **THEN** the priority scorer uses each candidate's `Category` to score it, and the highest-scored candidate is the one cast — `Category` is no longer inert

#### Scenario: Autocast gating and wire shape are still unaffected by Category

- **WHEN** an ability is gated by mana, cooldown, `SupportsAutoCast`, or selector-target availability, or its `AbilitySnapshot` is built
- **THEN** the gating decision and the snapshot shape are independent of `Category` (it changes only which *eligible* candidate is selected)

### Requirement: The `heal` ability is tagged `category: heal`

`catalog/abilities/heal/heal.json` SHALL carry the additive `"category": "heal"` field. All other fields of `heal.json` SHALL be unchanged. The `Category` tag now **participates in autocast priority scoring** (heal candidates are scored under the `heal` category); it SHALL NOT alter heal's autocast gating or any wire/snapshot shape.

#### Scenario: Heal definition exposes the heal category

- **WHEN** the `heal` ability definition is loaded
- **THEN** its `Category` is `AbilityCategoryHeal` and every other field matches its pre-change value

#### Scenario: Heal is scored under the heal category

- **WHEN** a multi-ability caster evaluates `heal` as an autocast candidate
- **THEN** it is scored by the `heal`-category formula (ally HP deficit), not treated as uncategorised

### Requirement: Heal-autocast guarantee is gating-equivalence plus a no-melee tripwire

The heal autocast selector (`lowest_hp_percentage_ally_in_range`, `castRange: match_attack_range`) is position-gated, and the `caster` combat profile deliberately moves the unit (retreat). A blanket "byte-identical heal autocast under any fixed seed" claim is therefore false and SHALL NOT be asserted: once a `caster`-profiled Acolyte retreats, its position changes, which legitimately changes which allies are in heal range and thus which ticks heal fires on. That divergence is correct behaviour, not a regression.

The system SHALL instead guarantee: (a) the heal-autocast **gating logic** (mana availability, cooldown, and the `SupportsAutoCast` / selector predicate) is unchanged by the `caster` profile flip and the `Category` tag; and (b) in a scenario with **no melee threat** (the Acolyte never retreats, so position is held constant), the set of ticks heal is auto-cast on is identical pre/post change for the same seed and inputs.

#### Scenario: Heal-autocast gating is unchanged by the profile flip and category tag

- **WHEN** the heal-autocast gate (mana / cooldown / selector predicate) is evaluated for a fixed unit and world state before and after this change
- **THEN** the gate's decision (cast / do not cast, and the selected target) is identical between the two

#### Scenario: No profile↔cadence coupling in a no-melee seeded replay

- **WHEN** a seeded match with an autocasting Acolyte and **no melee threat to it** is run before and after this change with the same seed and inputs
- **THEN** the set of ticks on which heal is cast is identical between the two runs (a tripwire for unintended profile↔cadence coupling)

#### Scenario: Cadence divergence under retreat is expected, not asserted against

- **WHEN** a `caster`-profiled Acolyte retreats from a melee attacker and its heal-target-in-range set changes as a result
- **THEN** the resulting change in heal cast ticks is treated as correct intended behaviour and is explicitly outside the byte-identical guarantee
