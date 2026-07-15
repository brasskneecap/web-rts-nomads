/**
 * A list is the catalog's single grouping primitive: a named set of item IDs.
 *
 * It is UNTYPED — it does not declare what it is for. The building that consumes
 * it decides what it means:
 *
 *   Shop         → sells the members            (item.costGold)
 *   Recipe Shop  → sells their RECIPES          (item.crafting.recipeCostGold)
 *   Artificer    → crafts them, ∩ what you know (item.crafting.craftCostGold)
 *   Camp         → drops one, uniform odds      (free)
 *
 * So one list can serve several roles at once. Consumers that only care about
 * craftable items skip the members that are not craftable rather than erroring.
 *
 * This replaces the old ItemListDef / RecipeListDef pair, which were the same
 * shape under two names.
 */
/** One member of a weighted list: an item and the rolls it owns on the die. */
export type ListEntry = {
  item: string
  min: number
  max: number
}

/**
 * A list takes exactly ONE form:
 *  - UNIFORM  — `items` only. Every member equally likely.
 *  - WEIGHTED — `maxRoll` + `entries`, each owning a slice of the die. A
 *    member's share of the die IS its likelihood, and weights apply wherever the
 *    list is read (loot AND shop stock).
 * The weighted form is what loot subtables used to be.
 */
export type ListDef = {
  id: string
  name: string
  /** UNIFORM form. Every member resolves to a real item (validated server-side). */
  items?: string[]
  /** WEIGHTED form: the die. Present iff `entries` is. */
  maxRoll?: number
  /** WEIGHTED form: entries tiling 1..maxRoll, no gaps, no overlaps. */
  entries?: ListEntry[]
}

/** True when the list rolls a die rather than picking evenly. */
export function isWeightedList(l: ListDef | undefined): boolean {
  return (l?.entries?.length ?? 0) > 0
}

/** The list's members regardless of form, in authored order. */
export function listItemIds(l: ListDef | undefined): string[] {
  if (!l) return []
  if (isWeightedList(l)) return l.entries!.map((e) => e.item)
  return l.items ?? []
}

export let LIST_DEFS: ListDef[] = []

export let LIST_DEF_MAP = new Map<string, ListDef>()

export function initListDefs(defs: ListDef[]): void {
  LIST_DEFS = defs
  LIST_DEF_MAP = new Map(defs.map((def) => [def.id, def]))
}
