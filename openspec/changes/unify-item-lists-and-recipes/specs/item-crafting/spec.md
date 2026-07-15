## ADDED Requirements

### Requirement: Craftability is a property of an item

An item def SHALL carry an optional crafting block declaring its ingredients and its two crafting prices. An item SHALL be craftable if and only if it declares a crafting block. There SHALL be no separate recipe entity: an item is its own recipe.

The crafting block SHALL declare:
- `inputs` — the item IDs consumed by one craft. MUST contain at least 2 entries, each resolving to a known item, none of which is the item itself.
- `craftCostGold` — gold charged per craft at a crafting building. MUST NOT be negative.
- `recipeCostGold` — gold charged once to learn the recipe. MUST NOT be negative.
- `starter` — when true, every player has already learned this item's recipe at match start.

#### Scenario: An item with ingredients is craftable
- **WHEN** an item def declares a crafting block with 2 or more inputs
- **THEN** the item is craftable, and its recipe is offered by crafting buildings and Recipe Shops

#### Scenario: An item with no crafting block is not craftable
- **WHEN** an item def declares no crafting block
- **THEN** the item is not craftable, no Recipe Shop offers its recipe, and no crafting building can produce it

#### Scenario: Fewer than two ingredients is rejected
- **WHEN** an item def declares a crafting block with fewer than 2 inputs
- **THEN** the catalog SHALL reject the def at load, and the editor SHALL refuse to save it

#### Scenario: An item cannot be its own ingredient
- **WHEN** an item def declares a crafting block whose inputs include the item's own ID
- **THEN** the def SHALL be rejected

### Requirement: The three item prices are independent

Every price in the item economy SHALL be a distinct, separately tunable field. Changing one MUST NOT change another.

- **Item cost** (`costGold`, on the item) — buying the finished item outright from a shop.
- **Craft cost** (`crafting.craftCostGold`) — charged at a crafting building on every craft, in addition to consuming the inputs.
- **Recipe cost** (`crafting.recipeCostGold`) — charged once at a Recipe Shop to learn the recipe.

#### Scenario: Learning charges the recipe cost, not the craft cost
- **WHEN** a player buys an item's recipe at a Recipe Shop
- **THEN** the player is charged `crafting.recipeCostGold` and no other price

#### Scenario: Crafting charges the craft cost, not the recipe cost
- **WHEN** a player crafts an item at a crafting building
- **THEN** the player is charged `crafting.craftCostGold` and the inputs are consumed, and the recipe cost is not charged again

#### Scenario: Buying the finished item charges the item cost
- **WHEN** a player buys a craftable item outright from a shop that stocks it
- **THEN** the player is charged the item's `costGold`, no ingredients are consumed, and no recipe is learned

### Requirement: Recipes are learned, and learning persists

A player SHALL be able to craft an item only after learning its recipe. Learned recipes SHALL be identified by the item ID they produce.

A player's learned set at match start SHALL be the union of the recipes saved on their profile and every item whose crafting block is flagged `starter`.

#### Scenario: Crafting an unlearned recipe is refused
- **WHEN** a player attempts to craft an item whose recipe they have not learned
- **THEN** the craft is refused, no gold is spent, and no ingredients are consumed

#### Scenario: A starter recipe needs no learning
- **WHEN** a match starts
- **THEN** every item whose crafting block is flagged `starter` is already in every player's learned set, with no purchase required

#### Scenario: Learning survives the match
- **WHEN** a player learns a recipe during a match
- **THEN** that item ID is recorded on the player's profile and is in their learned set at the start of their next match

#### Scenario: Learning a known recipe is a no-op
- **WHEN** a player buys a recipe they have already learned
- **THEN** no gold is spent and no shop stock is consumed

### Requirement: A crafting building produces items the player has learned

A building with the crafting capability SHALL offer the items the player has learned, and SHALL charge the craft cost while consuming the inputs.

A craft SHALL be refused unless the player owns at least one fully-built crafting building, has learned the recipe, can afford the craft cost, and holds every input in their vault (counting duplicates).

#### Scenario: A craft consumes inputs and charges the craft cost
- **WHEN** a player crafts an item they have learned, holding all inputs and enough gold
- **THEN** one of each input is consumed from the vault, the craft cost is deducted, and the output item is added to the vault

#### Scenario: Missing an ingredient refuses the craft
- **WHEN** a player attempts a craft while missing one of the inputs
- **THEN** the craft is refused, no gold is spent, and no items are consumed

#### Scenario: No crafting building refuses the craft
- **WHEN** a player attempts a craft while owning no fully-built crafting building
- **THEN** the craft is refused

### Requirement: A Recipe Shop sells recipes at the recipe cost

A building with the recipe-purchase capability SHALL stock the recipes of craftable items and sell them at each item's recipe cost. A purchase SHALL add the item to the buyer's learned set.

A purchase SHALL be refused unless the shop is discovered, not guard-locked, not under construction, has the recipe in stock, and the buyer can afford the recipe cost and has not already learned it.

#### Scenario: Buying a recipe learns it and consumes stock
- **WHEN** a player buys a stocked recipe they can afford and have not learned
- **THEN** the recipe cost is deducted, the item joins the player's learned set, and the shop's stock for that recipe decrements

#### Scenario: A guard-locked shop sells nothing
- **WHEN** a player attempts to buy a recipe from a Recipe Shop whose guards are still alive
- **THEN** the purchase is refused and no gold is spent

#### Scenario: The displayed price is the price charged
- **WHEN** a Recipe Shop displays a recipe for sale
- **THEN** the price shown is the item's recipe cost, which is the price the server charges

## REMOVED Requirements

### Requirement: Recipes are a separate catalog entity

**Reason**: A recipe def was a strict 1:1 shadow of the item it produced — same ID, same name, and a rarity derived from the output item's tier. It carried no information the item could not hold, and the only production writer of recipes could only ever emit `id == output == item.id`. Two entities keyed by the same ID is a standing invitation to drift.

**Migration**: Each recipe's `inputs`, `costGold` (→ `craftCostGold`), `unlockCostGold` (→ `recipeCostGold`), and `starter` fold into the `crafting` block of the item named by its `output`. Recipe IDs on the wire and in player profiles are already item IDs, so stored values carry over unchanged. `catalog/recipes/` is removed.
