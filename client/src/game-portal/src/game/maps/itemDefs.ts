// ─────────────────────────────────────────────────────────────────────────────
// Item definitions — client-side type layer
//
// Mirrors the ItemDef struct on the server. Each item carries:
//   - identity (id + display copy)
//   - an icon key matching a PNG in src/assets/ui/actions/ (loaded by
//     actionIconSprites — same loader the perk/action HUD uses)
//   - optional stat modifiers applied to the holder
//   - optional effect tags that flag systems (e.g. "regenerate", "aura")
//
// TO ADD / EDIT AN ITEM DEFINITION (when the catalog is wired):
//   edit  server/internal/game/catalog/item-defs.json
//   (this file's types update via fetchItemDefs — no manual sync needed)
// ─────────────────────────────────────────────────────────────────────────────

export type ItemTier = 'common' | 'uncommon' | 'rare' | 'epic' | 'legendary'
export type ItemKind = 'equipment' | 'consumable'

/**
 * Stat modifiers an item applies to its holder while equipped. All fields are
 * additive and optional — omit a key to leave that stat untouched. The server
 * is the source of truth for combat math; these values are used client-side
 * for tooltip previews ("+5 damage", "+2 armor") and UI affordances.
 */
export type ItemModifiers = {
  hp?: number
  damage?: number
  attackSpeed?: number
  moveSpeed?: number
  armor?: number
  healthRegen?: number
  shield?: number
  maxShield?: number
  /** Additive dodge/block probability (0.15 = +15%). */
  dodgeChance?: number
  blockChance?: number
}

/**
 * Named effect tags an item can grant. These are referenced by the server's
 * effect system; the client only displays them in tooltips and routes them
 * to icon overlays where applicable.
 */
export type ItemEffect =
  | 'regenerate'
  | 'aura-buff'
  | 'reveal-fog'
  | 'damage-reflect'
  | 'lifesteal'

/** The combat event that rolls a proc. */
export type ItemProcTrigger = 'onHit' | 'onStruck'

/** One proc as served on the wire: the trigger + chance, and the ability it
 *  casts at what it hits (the bespoke proc-effect path was removed). */
export type ItemProcWire = {
  trigger: ItemProcTrigger
  chance: number
  ability: string
}

/**
 * An item's recipe: what it consumes, and the two prices that gate it.
 *
 * The two gold fields buy DIFFERENT things and are tuned independently:
 *  - `craftCostGold`  — paid at a crafting building on EVERY craft, on top of
 *    consuming the inputs.
 *  - `recipeCostGold` — paid ONCE at a Recipe Shop to learn the recipe.
 * The third price in the item economy — buying the finished item off a shop
 * shelf — is `ItemDef.costGold`, not here.
 */
export type ItemCrafting = {
  /** Item IDs consumed by one craft (2+, duplicates allowed). */
  inputs: string[]
  /** Gold charged per craft at a crafting building. */
  craftCostGold: number
  /** Gold charged once at a Recipe Shop to learn this recipe. Moot when starter. */
  recipeCostGold: number
  /** When true, every player has already learned this recipe at match start. */
  starter?: boolean
}

/** True when this item can be crafted — i.e. it carries a recipe. */
export function isCraftable(def: ItemDef | undefined): boolean {
  return def?.crafting !== undefined
}

export type ItemDef = {
  id: string
  displayName: string
  description?: string
  /**
   * Action-icon ID used to render this item's icon. Resolved via
   * `getActionIconImage(iconKey)` against `src/assets/ui/actions/<iconKey>.png`.
   * Falls back to the SVG path map when no PNG is present.
   */
  iconKey: string
  /** Whether this is permanent equipment or a single-use consumable. */
  kind: ItemKind
  /** Rarity tier — drives border color in the vault and inventory UIs. */
  tier: ItemTier
  /** Gold to buy this item outright, finished, from a shop building. */
  costGold: number
  /**
   * This item's recipe, or absent when it cannot be crafted. Its presence IS the
   * item's craftability — there is no separate recipe entity, an item is its own
   * recipe.
   */
  crafting?: ItemCrafting
  /**
   * Building type that must be built and owned for this item to be purchasable.
   * Empty/undefined means no building gate. Drives Shop UI availability and
   * locked-state tooltips.
   */
  requiredBuilding?: string
  /** Display-only category label ("Weapon", "Trinket", "Consumable"). */
  category?: string
  /**
   * Editor-only flag: true when this item's catalog entry lives in the
   * writable overrides directory rather than the embedded default catalog.
   * Dev-build quirk: in local dev the writable dir mirrors the embedded
   * source, so every item reports `overridden: true` — expected, not a bug.
   * Because of that quirk it is NOT a usable "did the author make this"
   * signal; use `custom` for that.
   */
  overridden?: boolean
  /**
   * Editor-only flag: true when the author CREATED this item (it does not ship
   * in the embedded catalog). Drives whether deleting removes the item or
   * resets it to its shipped default. Unlike `overridden`, it is accurate in
   * dev builds.
   */
  custom?: boolean
  /**
   * mtime (unix seconds) of the author's uploaded icon, or absent when there is
   * none. Non-zero means the icon must be served from the server rather than
   * from bundled art — see registerUploadedIcons in rendering/itemAssets.ts.
   */
  iconUploadedAt?: number
  /** Stat changes applied while held. Absent = no stat changes. */
  modifiers?: ItemModifiers
  /** BROAD ability modifiers granted to the holder — "+15% radius" to every
   *  ability they cast. Keyed by ability-stat id (a bare kind like "duration",
   *  or an action-scoped "create_zone.duration"); see server ability_stats.go.
   *  Unlike a perk, an item cannot name an ability — it does not know who
   *  equipped it — so it targets a KIND instead. */
  abilityStats?: Record<string, { flat?: number; pct?: number }>
  /** Named effect tags granted while held. Absent = no effects. */
  effects?: ItemEffect[]
  /** Flat elemental damage applied as a separate typed instance on each hit. */
  onHitElemental?: { type: string; amount: number }[]
  /** Percent-chance procs. An item may carry any number of them, including
   *  several on the same trigger — each rolls independently, so two `onHit`
   *  procs can both fire on one attack. `trigger` is the combat event
   *  ('onHit' fires at the target; 'onStruck' fires back at the attacker when
   *  the holder is hit). Server-side each proc references a catalog proc
   *  effect (+ optional overrides); the wire always carries the RESOLVED
   *  payload below (`effect` is the reference id, included for
   *  display/debugging only — the client never needs proc-catalog knowledge of
   *  its own). */
  procs?: ItemProcWire[]
  /** Stack ceiling — items above 1 are stackable. Defaults to 1 when absent. */
  maxStacks?: number
  /** Consumable-specific config. Only set when kind === 'consumable'.
   *  Consumables are used as a ground-targeted AoE: `range` is the radius in
   *  world units (default 100 when absent); `split` (default true) divides
   *  `amount` evenly across the units hit instead of giving each the full
   *  amount. */
  consumable?: {
    type: string
    amount?: number
    range?: number
    split?: boolean
    durationSeconds?: number
  }
}

/** Fallback AoE radius (world units) when a consumable def authors no range.
 *  Mirrors the server's defaultConsumableRangeUnits. */
export const DEFAULT_CONSUMABLE_RANGE = 100

export let ITEM_DEFS: ItemDef[] = []

export let ITEM_DEF_MAP = new Map<string, ItemDef>()

export function initItemDefs(defs: ItemDef[]): void {
  ITEM_DEFS = defs
  ITEM_DEF_MAP = new Map(defs.map((def) => [def.id, def]))
}
