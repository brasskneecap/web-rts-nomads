## ADDED Requirements

### Requirement: A list is a named, untyped set of item IDs

The catalog SHALL provide a single list entity: `{id, name, items[]}`, where every member resolves to a known item. A list SHALL NOT declare what it is for. There SHALL be exactly one list type — separate item-list and recipe-list entities SHALL NOT exist.

A list SHALL contain at least one member. A member that names no known item SHALL be rejected at load.

#### Scenario: A list resolves its members
- **WHEN** a list names item IDs that all exist in the catalog
- **THEN** the list loads and can be bound to any building or camp

#### Scenario: A list naming an unknown item is rejected
- **WHEN** a list names an item ID that resolves to no item def
- **THEN** the catalog SHALL reject the list at load

#### Scenario: The same list serves several roles
- **WHEN** one list is bound to a Marketplace, an Artificer, and a camp
- **THEN** the Marketplace sells its members, the Artificer crafts those that are craftable and learned, and the camp drops one of its members — with no change to the list

### Requirement: The consuming building decides what a list means

A list SHALL be interpreted by whatever consumes it, according to that consumer's capability:

| Consumer | Interprets the list as | Charges |
|---|---|---|
| Shop (`item-purchase`) | items on the shelf | the item's item cost |
| Recipe Shop (`recipe-purchase`) | recipes for sale | the item's recipe cost |
| Crafting building (`crafting`) | the craftable scope, intersected with what the player has learned | the item's craft cost |
| Camp | a uniform drop pool | nothing |

#### Scenario: A list on a shop sells the items
- **WHEN** a list is bound to a building with the item-purchase capability
- **THEN** the shop stocks the list's members and sells each at its item cost

#### Scenario: A list on a Recipe Shop sells the recipes
- **WHEN** a list is bound to a building with the recipe-purchase capability
- **THEN** the shop stocks recipes drawn from the list's craftable members and sells each at its recipe cost

#### Scenario: A list on a crafting building scopes what it can make
- **WHEN** a list is bound to a building with the crafting capability
- **THEN** that building offers only the items that are both on the list and in the player's learned set, charging each item's craft cost

### Requirement: Crafting consumers ignore non-craftable members

A Recipe Shop or crafting building given a list that contains non-craftable items SHALL silently skip those members rather than fail. A list SHALL always be bindable to any consumer; a list with no members meaningful to that consumer SHALL simply resolve to an empty offering.

#### Scenario: A mixed list on a Recipe Shop offers only the craftable members
- **WHEN** a list containing both craftable and non-craftable items is bound to a Recipe Shop
- **THEN** the shop offers recipes only for the craftable members, and does not error

#### Scenario: A list with no craftable members offers nothing
- **WHEN** a list containing no craftable items is bound to a crafting building
- **THEN** the building offers nothing to craft, and the match is unaffected

### Requirement: A crafting building with no list offers everything the player knows

Binding a list to a crafting building SHALL be optional, and SHALL narrow rather than grant. A crafting building with no list bound SHALL offer every item the player has learned.

A list SHALL NOT grant the ability to craft an unlearned item.

#### Scenario: An unbound crafting building is unrestricted
- **WHEN** a player uses a crafting building with no list bound
- **THEN** every item in their learned set is offered

#### Scenario: A list does not bypass learning
- **WHEN** an item is on a crafting building's list but the player has not learned its recipe
- **THEN** the building does not offer it, and attempting to craft it is refused

### Requirement: A camp drops from either a weighted loot table or a list, never both

A neutral camp SHALL name at most one loot source: either a weighted loot table or a list. Naming both SHALL be a load-time validation error.

A list used as a loot source SHALL be a uniform pool: one member is chosen with equal probability, and a drop always occurs. Resource (gold/wood) drops and drop-chance gaps SHALL remain the exclusive province of weighted loot tables.

The choice SHALL be made from the seeded loot RNG, preserving deterministic replay.

#### Scenario: A camp with a list drops one member
- **WHEN** a camp whose loot source is a list is cleared by a player
- **THEN** exactly one member of the list is chosen with uniform probability and dropped as a chest

#### Scenario: A list loot source always drops
- **WHEN** a camp whose loot source is a list is cleared
- **THEN** a chest is always produced — a list cannot express a "no drop" outcome

#### Scenario: Naming two loot sources is rejected
- **WHEN** a neutral group names both a loot table and a list
- **THEN** the catalog SHALL reject it at load rather than silently picking one

#### Scenario: Weighted loot tables are unaffected
- **WHEN** a camp names a weighted loot table
- **THEN** it drops exactly as before, including resource bundles and no-drop outcomes

### Requirement: Authored lists survive a restart

A list SHALL be persistable at runtime and SHALL be reloaded into the catalog overlay on startup, so a list authored in the editor is still there after the server restarts.

#### Scenario: An authored list outlives the process
- **WHEN** a list is created in the editor and the server is restarted
- **THEN** the list is present in the catalog and any building bound to it still resolves it
