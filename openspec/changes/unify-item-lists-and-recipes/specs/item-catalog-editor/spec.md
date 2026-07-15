## ADDED Requirements

### Requirement: The item editor is tabbed, with Items and Lists

The item editor SHALL present two tabs: **Items** and **Lists**. The Items tab SHALL behave as the item editor does today. The Lists tab SHALL provide full create, edit, and delete of lists.

Tabs SHALL be keyboard-navigable and expose the standard tablist semantics to assistive technology.

#### Scenario: Switching tabs
- **WHEN** an author selects the Lists tab
- **THEN** the list authoring surface replaces the item form, and the sidebar lists the catalog's lists

#### Scenario: Item authoring is unchanged
- **WHEN** an author selects the Items tab
- **THEN** every item authoring capability that existed before this change is present and behaves identically

### Requirement: Lists are authorable end to end

An author SHALL be able to create a list, name it, add and remove item members, save it, and delete it — without editing JSON. A saved list SHALL be immediately bindable to a building or camp in the map editor.

A list SHALL be rejected on save if it has no members, or if a member names no known item.

#### Scenario: Creating a list
- **WHEN** an author creates a list, names it, adds two items, and saves
- **THEN** the list is persisted, appears in the catalog, and is selectable in the map editor's list dropdowns

#### Scenario: Deleting a list
- **WHEN** an author deletes a list
- **THEN** the list is removed from the catalog and no longer offered in the map editor

#### Scenario: An empty list is refused
- **WHEN** an author saves a list with no members
- **THEN** the save is refused with a message saying a list needs at least one item

### Requirement: The editor warns when a list's members will be ignored

Because a list is untyped, the editor SHALL surface a non-blocking warning when a list contains items that a crafting consumer would skip — stated as a consequence, not a rule.

The warning SHALL NOT block the save, because the same list may be entirely correct as shop stock or a loot pool.

#### Scenario: A mixed list warns about its non-craftable members
- **WHEN** an author edits a list where some members are not craftable
- **THEN** the editor shows a warning naming how many members a Recipe Shop or crafting building would ignore

#### Scenario: The warning never blocks a save
- **WHEN** an author saves a list that carries a non-craftable-members warning
- **THEN** the list saves successfully

### Requirement: An item's ID is derived from its display name

For a new item, the ID SHALL be auto-filled as a slug of the display name, and SHALL keep tracking the display name until the author edits the ID by hand. A hand-typed ID SHALL itself be slugged, so an invalid ID cannot be submitted.

A saved item's ID SHALL be immutable — it is the item's primary key, and every list, shop, recipe, and loot reference points at it.

#### Scenario: The ID follows the name
- **WHEN** an author types "Fire Sword" as the display name of a new item
- **THEN** the ID is filled in as `fire_sword`

#### Scenario: A hand-edited ID stops following the name
- **WHEN** an author edits the ID by hand and then changes the display name
- **THEN** the ID keeps the author's value

#### Scenario: A saved item's ID is locked
- **WHEN** an author opens an already-saved item
- **THEN** the ID is not editable

### Requirement: Buildings bind a list through one editor control and one metadata key

The map editor SHALL bind a list to a building through a single list selector, regardless of the building's capability. The binding SHALL be stored under exactly one metadata key, `list`, which SHALL be the only key any consumer reads.

The superseded keys `itemList` and `recipeList` SHALL NOT be aliased or silently ignored. A map that still carries either key SHALL fail to load with an error naming the map, the building, and the key to rename — silently dropping the key would erase a shop's stock configuration with no signal.

#### Scenario: Binding a list to a shop
- **WHEN** an author selects a list on a shop building in the map editor
- **THEN** that shop stocks the list's items in the next match, bound under the `list` key

#### Scenario: A stale metadata key is refused, not ignored
- **WHEN** a map is loaded whose building still carries the old `itemList` or `recipeList` key
- **THEN** the map fails to load with an error naming the map, the building, and the key to rename

#### Scenario: One selector regardless of building type
- **WHEN** an author binds a list to a Marketplace, a Recipe Shop, and an Artificer
- **THEN** the same control is used for all three, and all three store the binding under `list`
