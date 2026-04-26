// ─────────────────────────────────────────────────────────────────────────────
// Item definitions — client-side type layer
//
// Mirrors the (eventual) ItemDef struct on the server. Each item carries:
//   - identity (id + display copy)
//   - an icon key matching a PNG in src/assets/actions/ (loaded by
//     actionIconSprites — same loader the perk/action HUD uses)
//   - optional stat modifiers applied to the holder
//   - optional effect tags that flag systems (e.g. "regenerate", "aura")
//
// TO ADD / EDIT AN ITEM DEFINITION (when the catalog is wired):
//   edit  server/internal/game/catalog/item-defs.json
//   (this file's types update via fetchItemDefs — no manual sync needed)
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Stat modifiers an item applies to its holder while equipped. All fields are
 * additive and optional — omit a key to leave that stat untouched. The server
 * is the source of truth for combat math; these values are used client-side
 * for tooltip previews ("+5 damage", "+2 armor") and UI affordances.
 *
 * Add new keys here as the item system grows; keep them aligned with the
 * matching field on Unit / UnitSnapshot so resolution stays trivial.
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
 * to icon overlays where applicable. Use a string literal union as known
 * effects accumulate so authors get autocomplete + typo protection.
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
   * `getActionIconImage(iconKey)` against `src/assets/actions/<iconKey>.png`.
   * Falls back to the SVG path map when no PNG is present.
   */
  iconKey: string
  /** Stat changes applied while held. Absent = no stat changes. */
  modifiers?: ItemModifiers
  /** Named effect tags granted while held. Absent = no effects. */
  effects?: ItemEffect[]
  /** Display-only category label ("Weapon", "Trinket", "Consumable"). */
  category?: string
  /** Stack ceiling — items above 1 are stackable. Defaults to 1 when absent. */
  maxStacks?: number
}

export let ITEM_DEFS: ItemDef[] = []

export let ITEM_DEF_MAP = new Map<string, ItemDef>()

export function initItemDefs(defs: ItemDef[]): void {
  ITEM_DEFS = defs
  ITEM_DEF_MAP = new Map(defs.map((def) => [def.id, def]))
}
