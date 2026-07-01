export type RecipeDef = {
  id: string
  name: string
  /** Input item IDs consumed by the craft (2+). */
  inputs: string[]
  /** Gold cost charged at craft time. */
  costGold: number
  /** Output item ID produced. */
  output: string
}

export let RECIPE_DEFS: RecipeDef[] = []

export let RECIPE_DEF_MAP = new Map<string, RecipeDef>()

export function initRecipeDefs(defs: RecipeDef[]): void {
  RECIPE_DEFS = defs
  RECIPE_DEF_MAP = new Map(defs.map((def) => [def.id, def]))
}
