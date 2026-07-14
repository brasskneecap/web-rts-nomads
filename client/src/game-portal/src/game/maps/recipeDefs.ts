/**
 * A recipe carries TWO independent prices — do not use one where the other is
 * meant:
 *  - `costGold`       — the CRAFT cost, paid at the Artificer on every craft.
 *  - `unlockCostGold` — the RECIPE cost, paid once at a Recipe Shop to learn it.
 * The third price in the item economy — buying a finished item outright — is
 * `ItemDef.costGold`, on the item, not here.
 */
export type RecipeDef = {
  id: string
  name: string
  /** Input item IDs consumed by the craft (2+). */
  inputs: string[]
  /** Gold charged per craft at the Artificer, on top of the consumed inputs. */
  costGold: number
  /** Gold charged once at a Recipe Shop to learn this recipe. Moot for starter
   *  recipes (never purchased). Optional so hand-built defs (tests, fetch
   *  fallback) still type; absent reads as free. */
  unlockCostGold?: number
  /** Output item ID produced. */
  output: string
  /** When true, every player has already learned this recipe at match start. */
  starter?: boolean
  /**
   * Quality tier, derived server-side from the recipe's catalog subdirectory
   * (common/uncommon/rare/epic/legendary). Drives the Recipe Shop icon —
   * `${rarity}_recipe`, falling back to `rare_recipe` when no tier-specific
   * asset exists. Optional so hand-built defs (tests, fetch fallback) still type.
   */
  rarity?: string
}

export let RECIPE_DEFS: RecipeDef[] = []

export let RECIPE_DEF_MAP = new Map<string, RecipeDef>()

export function initRecipeDefs(defs: RecipeDef[]): void {
  RECIPE_DEFS = defs
  RECIPE_DEF_MAP = new Map(defs.map((def) => [def.id, def]))
}
