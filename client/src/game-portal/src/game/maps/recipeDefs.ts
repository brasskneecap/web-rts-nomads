export type RecipeDef = {
  id: string
  name: string
  /** Input item IDs consumed by the craft (2+). */
  inputs: string[]
  /** Gold cost charged at craft time. */
  costGold: number
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
