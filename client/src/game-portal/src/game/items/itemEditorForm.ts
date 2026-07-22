// Item editor form module — pure transforms between the wire ItemDef shape
// (fractions, resolved proc payloads) and the editor form shape (percents,
// nullable proc overrides = "inherit"). No fetch/IO here; see itemEditorApi.ts
// for the service layer. Percent<->fraction conversion happens ONLY in this
// file per project convention.
import type { ItemDef, ItemProcTrigger } from '../maps/itemDefs'
import type { EditorSaveRequest } from './itemEditorApi'

/** One row of the editor's proc list. Existence IS enablement — a proc the
 *  user removes is spliced out of the list, not flagged off. A proc CASTS a
 *  composable ability at what it hits (the bespoke proc-effect path was
 *  removed). */
export type ProcForm = {
  trigger: ItemProcTrigger
  /** The composable ability this proc casts at its target. */
  ability: string
  chancePct: number
}

export type ConsumableForm = {
  type: string
  amount: number
  range: number
  split: boolean
  durationSeconds: number
}

export type ItemEditorForm = {
  id: string
  isNew: boolean
  /** 'equipment' (stat/proc gear) or 'consumable' (used-item effect). */
  kind: 'equipment' | 'consumable'
  displayName: string
  description: string
  iconKey: string
  tier: string
  /** Organizational only — groups items in the editor and decides which catalog
   *  subdirectory the def is written to. Never an equip restriction. */
  category: string
  costGold: number
  /** Consumable effect + stack count. Emitted only when kind === 'consumable'. */
  consumable: ConsumableForm
  maxStacks: number
  mods: {
    hp: number
    damage: number
    armor: number
    attackSpeed: number
    moveSpeed: number
    healthRegen: number
    maxShield: number
    dodgePct: number
    blockPct: number
  }
  /** BROAD ability modifiers this item grants its holder — "+15% radius" to
   *  every ability they cast. This is the addressing an item NEEDS: unlike a
   *  perk it cannot name an ability, because it does not know who equipped it.
   *  Keyed by stat id from /catalog/ability-stats. See ability_stats.go. */
  abilityStats: Record<string, { flat?: number; pct?: number }>
  elemental: { type: string; amount: number }[]
  /** Any number of procs, in the order they will be saved. Several may share a
   *  trigger — each rolls independently server-side. */
  procs: ProcForm[]
  /**
   * Crafting: an item is craftable (isRecipe) when it carries a recipe — an item
   * IS its own recipe. The two costs are DIFFERENT prices, tuned separately:
   * craftCost is paid at a crafting building on every craft; recipeCost is paid
   * once at a Recipe Shop to learn the recipe at all. (form.costGold, outside
   * this block, is the third: buying the finished item off a shop shelf.)
   * Availability — which shops stock it, what drops it — is LIST membership,
   * edited in the Lists tab, not here.
   */
  crafting: {
    isRecipe: boolean
    /** Gold per craft at the Artificer, alongside the consumed ingredients. */
    craftCost: number
    /** Gold to learn the recipe at a Recipe Shop. Moot when starter is true. */
    recipeCost: number
    inputs: string[]
    starter: boolean
  }
  /**
   * Fields the editor does not model but must survive an edit round-trip.
   * Explicit allowlist — never spread the raw def (its proc fields carry
   * RESOLVED wire values that would re-save as frozen overrides).
   */
  unmodeled: Record<string, unknown>
}

// Fields the editor does not model but must survive an edit round-trip.
// Explicit allowlist — never spread the raw def (its proc fields carry
// RESOLVED wire values that would re-save as frozen overrides). maxStacks and
// consumable are now MODELED (see ConsumableForm), so they are not here.
const UNMODELED_KEYS = ['requiredBuilding', 'effects'] as const

const blankConsumable = (): ConsumableForm => ({
  type: 'heal', amount: 50, range: 0, split: true, durationSeconds: 0,
})

/** A fresh proc row for the editor's "+ Add Proc". Overrides start null
 *  ("inherit from the effect"); the user picks the effect. */
export const blankProc = (trigger: ItemProcTrigger = 'onHit'): ProcForm => ({
  trigger, ability: '', chancePct: 10,
})

export function createBlankForm(): ItemEditorForm {
  return {
    id: '', isNew: true, kind: 'equipment', displayName: '', description: '', iconKey: '', tier: 'common',
    category: 'Weapon', costGold: 0,
    consumable: blankConsumable(), maxStacks: 0,
    mods: { hp: 0, damage: 0, armor: 0, attackSpeed: 0, moveSpeed: 0, healthRegen: 0, maxShield: 0, dodgePct: 0, blockPct: 0 },
    abilityStats: {},
    elemental: [],
    procs: [],
    crafting: { isRecipe: false, craftCost: 150, recipeCost: 150, inputs: ['', ''], starter: false },
    unmodeled: {},
  }
}

function procFormFromWire(p: NonNullable<ItemDef['procs']>[number]): ProcForm {
  return { trigger: p.trigger, ability: p.ability ?? '', chancePct: Math.round(p.chance * 100) }
}

export function formFromDef(def: ItemDef): ItemEditorForm {
  const m = def.modifiers ?? {}
  const c = def.consumable
  // The crafting block IS craftability — an item is its own recipe. Present
  // means craftable; absent means not.
  const craft = def.crafting
  return {
    id: def.id, isNew: false, kind: def.kind === 'consumable' ? 'consumable' : 'equipment',
    displayName: def.displayName, description: def.description ?? '',
    iconKey: def.iconKey, tier: def.tier, category: def.category ?? 'Weapon', costGold: def.costGold,
    abilityStats: def.abilityStats ?? {},
    consumable: c
      ? { type: c.type, amount: c.amount ?? 0, range: c.range ?? 0, split: c.split ?? true, durationSeconds: c.durationSeconds ?? 0 }
      : blankConsumable(),
    maxStacks: def.maxStacks ?? 0,
    mods: {
      hp: m.hp ?? 0, damage: m.damage ?? 0, armor: m.armor ?? 0, attackSpeed: m.attackSpeed ?? 0,
      moveSpeed: m.moveSpeed ?? 0, healthRegen: m.healthRegen ?? 0, maxShield: m.maxShield ?? 0,
      dodgePct: Math.round((m.dodgeChance ?? 0) * 100), blockPct: Math.round((m.blockChance ?? 0) * 100),
    },
    elemental: (def.onHitElemental ?? []).map((e) => ({ ...e })),
    procs: (def.procs ?? []).map(procFormFromWire),
    crafting: {
      isRecipe: craft !== undefined,
      craftCost: craft?.craftCostGold ?? 150,
      recipeCost: craft?.recipeCostGold ?? 150,
      inputs: craft ? [...craft.inputs] : ['', ''],
      starter: craft?.starter ?? false,
    },
    unmodeled: pickUnmodeled(def),
  }
}

// Picks the ALLOWLISTED unmodeled keys present on the def. Never a blind
// spread — see UNMODELED_KEYS comment.
function pickUnmodeled(def: ItemDef): Record<string, unknown> {
  const picked: Record<string, unknown> = {}
  for (const key of UNMODELED_KEYS) {
    if (def[key] !== undefined) picked[key] = def[key]
  }
  return picked
}

// A proc row with no ability chosen is dropped rather than saved — the server
// rejects an empty proc, and a half-filled row is not an intent to author one.
function procWireFromForm(p: ProcForm): Record<string, unknown> | undefined {
  if (!p.ability) return undefined
  return { trigger: p.trigger, chance: p.chancePct / 100, ability: p.ability }
}

export function saveRequestFromForm(form: ItemEditorForm): EditorSaveRequest {
  // Fields common to both kinds. `unmodeled` (requiredBuilding/effects) is
  // preserved for either kind; equipment- and consumable-specific blocks are
  // added below so a consumable never carries stat/proc data and vice versa.
  const item: Record<string, unknown> = {
    ...form.unmodeled,
    id: form.id, displayName: form.displayName, iconKey: form.iconKey || form.id,
    kind: form.kind, tier: form.tier, category: form.category,
    costGold: form.costGold,
  }
  if (form.description) item.description = form.description

  // Crafting rides ON the item — an item is its own recipe. Absent = not
  // craftable, and there is no second file to fall out of step with.
  if (form.crafting.isRecipe) {
    item.crafting = {
      inputs: form.crafting.inputs.filter(Boolean),
      craftCostGold: form.crafting.craftCost,
      recipeCostGold: form.crafting.recipeCost,
      ...(form.crafting.starter ? { starter: true } : {}),
    }
  }

  if (form.kind === 'consumable') {
    const c = form.consumable
    const consumable: Record<string, unknown> = { type: c.type }
    if (c.amount) consumable.amount = c.amount
    if (c.range) consumable.range = c.range
    // split is always sent: absent defaults to true server-side, so an
    // unchecked (false) box would be indistinguishable from "not set".
    consumable.split = c.split
    if (c.durationSeconds) consumable.durationSeconds = c.durationSeconds
    item.consumable = consumable
    if (form.maxStacks > 0) item.maxStacks = form.maxStacks
    return { item }
  }

  // Equipment: stat modifiers, ability stats, on-hit elemental, and procs.
  const m = form.mods
  const modifiers: Record<string, number> = {}
  if (m.hp) modifiers.hp = m.hp
  if (m.damage) modifiers.damage = m.damage
  if (m.armor) modifiers.armor = m.armor
  if (m.attackSpeed) modifiers.attackSpeed = m.attackSpeed
  if (m.moveSpeed) modifiers.moveSpeed = m.moveSpeed
  if (m.healthRegen) modifiers.healthRegen = m.healthRegen
  if (m.maxShield) modifiers.maxShield = m.maxShield
  if (m.dodgePct) modifiers.dodgeChance = m.dodgePct / 100
  if (m.blockPct) modifiers.blockChance = m.blockPct / 100

  const elemental = form.elemental.filter((e) => e.amount > 0 && e.type)

  if (Object.keys(modifiers).length > 0) item.modifiers = modifiers
  // Absent and empty mean the same thing server-side, so a bare {} is noise.
  if (Object.keys(form.abilityStats).length > 0) item.abilityStats = form.abilityStats
  if (elemental.length > 0) item.onHitElemental = elemental

  const procs = form.procs.map(procWireFromForm).filter((p) => p !== undefined)
  if (procs.length > 0) item.procs = procs

  return { item }
}
