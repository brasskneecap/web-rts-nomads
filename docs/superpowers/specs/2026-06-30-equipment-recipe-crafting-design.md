# Equipment Recipe & Crafting System — Design

**Date:** 2026-06-30
**Status:** Approved (brainstorming) — ready for implementation plan

## Summary

Add an equipment crafting system. Players discover **recipes** at neutral **Recipe
Shops**, combine **2+ ingredient items + gold** at a new player building (the
**Artificer**) to produce a stronger item, and — once a recipe is crafted for the
first time — keep that recipe **permanently unlocked on their account** for all
future matches.

The first content set is three crafted elemental swords (`fire_sword`,
`ice_sword`, `lightning_sword`), each made from a `broad_sword` plus the matching
new **elemental ring** (`fire_ring`, `ice_ring`, `lightning_ring`).

This change also introduces the first two genuinely **typed, mechanically-distinct
elemental effects** in the game:

1. Elemental rings/swords add **+5 elemental damage as a separate damage instance**
   (not folded into physical damage), so future resist/weakness systems can treat
   it independently.
2. Crafted swords carry a **5% on-hit proc** that fires an elemental bolt projectile
   dealing 25 damage of that element.

## Goals

- Recipes: 2+ input items + gold → one stronger output item.
- Recipes purchased at neutral Recipe Shops; crafted at a player-built Artificer.
- First successful craft permanently unlocks that recipe **per-recipe, account-wide**.
- New elemental rings (+5 elemental damage each) sold at neutral marketplaces and
  droppable from enemies.
- Three starter crafted swords combining broad sword + matching ring stats + a 5%
  elemental proc (25 damage).

## Non-Goals (v1)

- Elemental resistances/weaknesses on units (the typed damage instance is the seam;
  the actual mitigation math is future work — already a documented TODO in
  `damage_type.go`).
- Elemental on-hit and the proc are NOT restricted to a single primary target.
  They apply to every target a landed basic attack reaches — so a piercing
  attacker (Marksman `pierce`) applies the elemental instance and rolls the proc
  on each corridor victim. This is intentional: a proc may become an AoE effect
  or spell in the future, so the system must not assume primary-only. (Base-stat
  *splash* still bypasses the on-hit hub and therefore does not carry elemental/
  proc — splash is a separate, pre-existing payload.) Decided 2026-06-30 during
  Plan 1 review.
- Crafting from a unit's equipped slots, or any UI to "uncraft".
- More than the three starter recipes.

## Key Decisions (from brainstorming)

| Decision | Choice |
|---|---|
| Unlock scope | Per-recipe, **account-wide**. |
| Unlock trigger | **Buy recipe → craft once → permanent.** Buying only unlocks for the current match; the first successful craft writes it to the profile. |
| Ingredient source | Consumed from the player **Vault**, plus gold. |
| Ingredient sourcing | Rings from **neutral** marketplaces + **enemy drops**; broad sword from existing shops. |
| Elemental depth | +5 elemental is a **separate typed damage instance**, distinct from physical. Not flavor. |
| Item slot | Universal item slot (`slotKind: "any"`). The game has no dedicated "accessory" slot. |
| Recipe Shop stock | **Random subset per match** via a recipe loot table (mirrors neutral merchant). |
| Proc/elemental targets | Primary target only (v1). Proc reuses the basic-attack hit hook for melee + ranged. |

---

## A. Item mechanics (foundation)

Two new **data-driven** optional properties on `ItemDef`
(`server/internal/game/items.go`). New items are pure JSON — no per-item Go code.

### A1. `onHitElemental` — separate typed damage instance

```jsonc
"onHitElemental": [ { "type": "fire", "amount": 5 } ]
```

- Aggregated per damage-type across all equipped items into the unit's cached
  `EquipmentBonus` (extend `UnitEquipmentBonus` in `state_items.go` with a
  per-type map, e.g. `OnHitElemental map[DamageType]int`), recomputed in
  `recomputeUnitEquipmentBonusLocked`. Two fire items stack to +10 fire.
- Applied on each **landed basic attack** as its own call through
  `applyUnitDamageWithSourceLocked` with
  `DamageSource{Kind: "item-elemental", DamageType: <type>}`, at the single
  on-hit hub `resolveAttackHitLocked` — which fires for melee (called directly)
  and ranged (called from `landProjectileLocked`), and for every pierce-corridor
  victim. (As-built, the bonus is read from the wielder's live `EquipmentBonus`
  at hit time rather than snapshotted onto the projectile at fire time; the
  practical difference is only for a ranged attacker who dies mid-flight, where
  the orphaned-arrow path applies physical damage only — matching how that path
  already skips all attacker-side perks.)
- `type` must be a registered `DamageType` (validated at catalog load, mirroring
  ability damage-type validation).

### A2. `onHitProc` — % chance elemental bolt

```jsonc
"onHitProc": { "chance": 0.05, "damage": 25, "damageType": "fire", "projectileID": "fire_bolt" }
```

- On each landed basic attack, roll `s.rngPerks.Float64() < chance` (existing
  deterministic, replay-safe RNG stream — same one crits/perk procs use).
- On success, spawn a homing projectile at the unit's current attack target dealing
  `damage` of `damageType` as its own instance. Reuse the projectile spawn path; the
  proc projectile carries its own `Damage`/`DamageType` and does **not** re-trigger
  on-hit elemental/proc effects (no recursion).
- Aggregated onto `EquipmentBonus` as a list of procs (a unit could in principle hold
  multiple proc items; each rolls independently).

### A3. New catalog content

- **Rings** (`kind: equipment`, `slotKind: "any"`), each `onHitElemental: 5` of its
  element, modest `costGold`:
  `catalog/items/.../fire_ring.json`, `ice_ring.json`, `lightning_ring.json`.
- **Crafted swords** (`kind: equipment`, `slotKind: "any"`):
  - `modifiers.damage: 5` (broad sword's physical)
  - `onHitElemental: 5` of the element (the ring's contribution)
  - `onHitProc: { chance: 0.05, damage: 25, damageType: <element>, projectileID: <bolt> }`
  - `fire_sword.json`, `ice_sword.json`, `lightning_sword.json`.
- **Projectiles**: `fire_bolt` already exists. Add `frost_bolt` and `lightning_bolt`
  defs under `catalog/projectiles/` (recolored bolts; follow/impact effects).
- **Acquisition wiring**: add the rings to the marketplace/merchant loot tables and to
  enemy loot tables (`loot_table_defs.go` / the relevant catalog loot tables).

---

## B. Recipe data model

New `RecipeDef` catalog, embedded loader mirroring `items.go`
(`//go:embed catalog/recipes`), served over HTTP at `GET /catalog/recipes`.

```jsonc
// catalog/recipes/fire_sword.json
{
  "id": "fire_sword",
  "name": "Fire Sword",
  "inputs": ["broad_sword", "fire_ring"],
  "costGold": 150,
  "output": "fire_sword"
}
```

- Three starter recipes: fire / ice / lightning sword.
- Loader validates that every `input` and the `output` resolve to a real `ItemDef`
  at startup (fail-fast, like the existing catalog loaders).
- A **recipe loot table** lists the recipes a Recipe Shop may stock.

---

## C. Buildings & commands

### C1. Recipe Shop (neutral building)

- `catalog/buildings/recipe-shop.json`, `class: "neutral"`, `buildable: false`,
  new capability `recipe-purchase`. Placed/initialized like `neutral-shop` at match
  start; **rolls a random recipe subset** at match start via a `ShopRecipeLootTableID`
  (mirroring the merchant's `ShopLootTableID` flow in `state_shop.go`).
- New WS command `purchase_recipe` → `handlePurchaseRecipeLocked`:
  - Validate: building exists, visible/known in buyer FOW, has `recipe-purchase`
    capability, not guard-locked, recipe present in this shop's recipe inventory.
  - Gold-gate against `player.Resources["gold"]`; on success deduct gold and add the
    recipe ID to the player's in-match `UnlockedRecipeIDs` set. Decrement stock.
  - **Buying does not write the profile** — only crafting does (decision above).

### C2. Artificer (player building)

- `catalog/buildings/artificer.json`, `class: "player"`, `buildable: true`,
  gold+wood cost (sized like `marketplace.json`), new capability `crafting`, a
  build hotkey.
- New WS command `craft_item` → `handleCraftItemLocked`:
  - Validate: player owns at least one **fully-built, visible** Artificer; recipe is
    in `UnlockedRecipeIDs`; the **Vault contains every input item**; player can afford
    `costGold`; Vault has room for the output.
  - On success: remove one of each input item from the Vault, deduct gold, add the
    output item to the Vault (reuse `addItemToVaultLocked`).
  - **First craft persistence:** if the recipe is not yet in the profile's
    `KnownRecipeIDs`, record it via the profile manager (account-wide unlock).

---

## D. Account-wide persistence

- `PlayerProfile` (`server/internal/profile/types.go`) gains
  `KnownRecipeIDs []string`. Bump `CurrentVersion` **7 → 8** with a forward
  migration in `migrateProfile` that initializes it to an empty slice.
- New profile-manager mutation to append a recipe ID idempotently (mirrors existing
  `WithLocked` / commit patterns), written atomically like other profile mutations.
- At match join, seed `Player.UnlockedRecipeIDs` from the profile's `KnownRecipeIDs`
  (snapshot pattern, like `ProfileUpgrades`). `purchase_recipe` adds to the same set
  for the current match.
- **Craftable this match** = `recipeID ∈ Player.UnlockedRecipeIDs` AND the player owns
  a built Artificer.

---

## E. Frontend (TypeScript / Vue 3)

- **Recipe Shop panel:** selecting a neutral Recipe Shop opens a purchase panel
  mirroring the existing shop UI (`VaultPanel`/shop components) — lists stocked
  recipes with gold cost and a Buy button (greyed when unaffordable/out of stock).
- **Artificer craft panel:** selecting an owned Artificer opens a craft panel listing
  unlocked recipes; each shows its ingredients as **have / need** (read from the
  Vault snapshot), the gold cost, and a Craft button enabled only when all ingredients
  + gold are present.
- **Catalogs:** fetch `GET /catalog/recipes` at startup (alongside item/building
  catalogs); mirror `RecipeDef`, the new `ItemDef` fields, and the two new building
  defs client-side (display only — server stays authoritative).
- **Tooltips:** extend item tooltip body (`itemRules.ts`) to show elemental on-hit
  damage and the proc.
- **Wire commands:** `purchase_recipe` and `craft_item` through the existing network
  client.
- **Conventions (CLAUDE.md):** the client only renders server state — no client-side
  combat math. No literal `cursor:` declarations in new component CSS (global rules
  handle it).

---

## F. Testing

Deterministic Go tests (seeded sim) covering:

1. **Elemental on-hit:** equipping a fire ring applies +5 fire as a **separate**
   `DamageType: fire` instance on a basic attack; physical damage is unchanged; two
   fire items stack to +10.
2. **Proc:** with the seeded RNG, the 5% proc fires at the expected rate and spawns a
   bolt dealing 25 typed damage; the proc projectile does not re-trigger procs.
3. **Recipe purchase:** gold-gated; success adds to `UnlockedRecipeIDs` and decrements
   stock; insufficient gold is a no-op.
4. **Craft:** requires a built Artificer + unlocked recipe + all Vault ingredients +
   gold; success consumes exactly one of each input and the gold and outputs the item;
   missing any precondition is a no-op.
5. **Persistence:** first successful craft writes `KnownRecipeIDs`; subsequent matches
   seed it into `UnlockedRecipeIDs` so the recipe is craftable without re-buying.
6. **Migration:** a v7 profile round-trips to v8 with an empty `KnownRecipeIDs`.

---

## Touched files (orientation, not exhaustive)

**Server**
- `items.go` — `ItemDef.OnHitElemental`, `ItemDef.OnHitProc`, validation.
- `state_items.go` — `UnitEquipmentBonus` per-type elemental + proc aggregation;
  `recomputeUnitEquipmentBonusLocked`.
- `state_combat.go` / `projectile.go` — apply elemental instance + proc on landed
  hits; snapshot elemental amounts onto projectiles.
- `recipes.go` (new) — `RecipeDef` + embedded loader; `ListRecipeDefs`.
- `state_shop.go` — recipe-shop init/inventory roll; `handlePurchaseRecipeLocked`.
- `state_crafting.go` (new) — `handleCraftItemLocked`.
- `state_buildings.go` / building catalog — Artificer build path honoring `crafting`.
- `building_defs.go` — recognise new capabilities (data only).
- `profile/types.go`, `profile/store.go`, `profile/manager.go` — `KnownRecipeIDs`,
  v7→v8 migration, append mutation.
- `state.go` — `Player.UnlockedRecipeIDs`; seed at join.
- `ws/handlers.go` + `pkg/protocol/messages.go` — `purchase_recipe`, `craft_item`
  messages and recipe/item snapshot fields.
- `http/router.go` — `GET /catalog/recipes`.
- `catalog/` — new items (rings, swords), recipes, projectiles (frost/lightning bolt),
  buildings (recipe-shop, artificer), loot-table edits.

**Client**
- `game/maps/itemDefs.ts`, `recipeDefs.ts` (new), `buildingDefs.ts`, `catalog.ts` —
  mirror new defs + fetch `/catalog/recipes`.
- Recipe Shop + Artificer panels (new Vue components alongside `VaultPanel.vue`).
- `items/itemRules.ts` — tooltip extensions.
- `game/core/GameState.ts` / `GameClient.ts` / `network/NetworkClient.ts` — wire the
  two new commands and surface unlocked recipes.
