## ADDED Requirements

### Requirement: A list is either uniform or weighted

A list SHALL take exactly one of two forms:

- **uniform** — a plain set of item IDs. Every member is equally likely when the list is rolled or sampled.
- **weighted** — a `maxRoll` die plus entries, each naming an item and owning a roll range. A member's share of the die is its likelihood.

A list declaring both forms SHALL be rejected at load: a list has one notion of how likely its members are, not two.

A weighted list's entries SHALL tile `1..maxRoll` with no gaps and no overlaps. A weighted list, once rolled, ALWAYS yields an item — the decision of whether anything drops at all belongs to the table, not to the pool.

#### Scenario: A uniform list rolls evenly
- **WHEN** a uniform list of three items is rolled many times
- **THEN** each member is yielded with equal probability

#### Scenario: A weighted list rolls by weight
- **WHEN** a weighted list gives one item 90 of 100 rolls and another 10
- **THEN** the first is yielded roughly nine times as often

#### Scenario: A gap in a weighted list is rejected
- **WHEN** a weighted list's entries leave a roll uncovered
- **THEN** the catalog SHALL reject the list, naming the uncovered range

#### Scenario: Declaring both forms is rejected
- **WHEN** a list declares both `items` and `entries`
- **THEN** the catalog SHALL reject it

### Requirement: Weights apply wherever a list is read

A list's weights SHALL govern every sampling of it, not only loot. A shop that samples a weighted list SHALL sample it by weight, so a rare member is rare on the shelf for the same reason it is rare in a chest.

Consumers that care only about MEMBERSHIP — a crafting building's scope, a Recipe Shop's pool, a shop's verbatim shelf — SHALL read the members without regard to form, and SHALL behave identically for a uniform and a weighted list holding the same items.

#### Scenario: A shop samples a weighted list by weight
- **WHEN** a neutral shop is bound to a weighted list and samples its stock
- **THEN** members with a larger share of the die appear more often

#### Scenario: Membership is form-agnostic
- **WHEN** a crafting building is bound to a weighted list
- **THEN** it offers exactly the items on that list, with the weights playing no part

#### Scenario: Uniform lists are unaffected
- **WHEN** a shop is bound to a uniform list
- **THEN** it samples uniformly, exactly as before weights existed
