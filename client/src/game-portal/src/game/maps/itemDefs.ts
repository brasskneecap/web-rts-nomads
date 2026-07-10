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
  /** Slot type the item occupies: 'weapon' | 'armor' | 'accessory' | 'any'. */
  slotKind: string
  /** Unit types allowed to equip this item. Absent = all unit types. */
  allowedUnitTypes?: string[]
  /** Gold cost to purchase from a shop building. */
  costGold: number
  /** True when the item is craftable at the Artificer (a recipe unlocks it). */
  isRecipe?: boolean
  /** Gold cost to craft, when isRecipe. */
  recipeCost?: number
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
   */
  overridden?: boolean
  /** Stat changes applied while held. Absent = no stat changes. */
  modifiers?: ItemModifiers
  /** Named effect tags granted while held. Absent = no effects. */
  effects?: ItemEffect[]
  /** Flat elemental damage applied as a separate typed instance on each hit. */
  onHitElemental?: { type: string; amount: number }[]
  /** Percent-chance on-hit proc: fires an elemental bolt for `damage`.
   *  Server-side this references a catalog proc effect (+ optional
   *  overrides); the wire always carries the RESOLVED payload below
   *  (`effect` is the reference id, included for display/debugging only —
   *  the client never needs proc-catalog knowledge of its own). */
  onHitProc?: { chance: number; effect?: string; damage: number; damageType: string; projectileID: string }
  /** Percent-chance proc when the holder is struck: fires an elemental bolt
   *  at the attacker for `damage`. Same wire shape as `onHitProc` — the
   *  server always marshals the resolved payload. */
  onStruckProc?: { chance: number; effect?: string; damage: number; damageType: string; projectileID: string }
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
