## MODIFIED Requirements

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
