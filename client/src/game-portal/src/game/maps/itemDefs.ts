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
  /**
   * Building type that must be built and owned for this item to be purchasable.
   * Empty/undefined means no building gate. Drives Shop UI availability and
   * locked-state tooltips.
   */
  requiredBuilding?: string
  /** Display-only category label ("Weapon", "Trinket", "Consumable"). */
  category?: string
  /** Stat changes applied while held. Absent = no stat changes. */
  modifiers?: ItemModifiers
  /** Named effect tags granted while held. Absent = no effects. */
  effects?: ItemEffect[]
  /** Stack ceiling — items above 1 are stackable. Defaults to 1 when absent. */
  maxStacks?: number
  /** Consumable-specific config. Only set when kind === 'consumable'. */
  consumable?: {
    type: string
    amount?: number
    durationSeconds?: number
  }
}

export let ITEM_DEFS: ItemDef[] = []

export let ITEM_DEF_MAP = new Map<string, ItemDef>()

export function initItemDefs(defs: ItemDef[]): void {
  ITEM_DEFS = defs
  ITEM_DEF_MAP = new Map(defs.map((def) => [def.id, def]))
}
