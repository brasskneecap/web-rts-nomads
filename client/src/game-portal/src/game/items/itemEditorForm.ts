// Item editor form module — pure transforms between the wire ItemDef shape
// (fractions, resolved proc payloads) and the editor form shape (percents,
// nullable proc overrides = "inherit"). No fetch/IO here; see itemEditorApi.ts
// for the service layer. Percent<->fraction conversion happens ONLY in this
// file per project convention.
import type { ItemDef } from '../maps/itemDefs'
import type { EditorSaveRequest } from './itemEditorApi'

export type ProcForm = {
  enabled: boolean
  effect: string
  chancePct: number
  damage: number | null
  projectileScale: number | null
  bounceCount: number | null
  bounceRange: number | null
  bounceDamageFalloff: number | null
  slowMultiplier: number | null
  slowDurationSeconds: number | null
  burnDamagePerSecond: number | null
  burnDurationSeconds: number | null
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
  category: string
  slotKind: string
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
  elemental: { type: string; amount: number }[]
  onHit: ProcForm
  onStruck: ProcForm
  /**
   * Crafting: an item is craftable (isRecipe) when a recipe unlocks it at the
   * Artificer. recipeCost is the gold to craft; inputs are the ingredients.
   * Availability (which shops stock it, loot tables) is NOT modeled here — a
   * shop-level concern edited elsewhere; the item only owns its own costs.
   */
  crafting: { isRecipe: boolean; recipeCost: number; inputs: string[]; starter: boolean }
  /** Unit types allowed to equip this item. Empty = all unit types. */
  allowedUnitTypes: string[]
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

const blankProc = (): ProcForm => ({
  enabled: false, effect: '', chancePct: 10,
  damage: null, projectileScale: null, bounceCount: null, bounceRange: null, bounceDamageFalloff: null,
  slowMultiplier: null, slowDurationSeconds: null, burnDamagePerSecond: null, burnDurationSeconds: null,
})

export function createBlankForm(): ItemEditorForm {
  return {
    id: '', isNew: true, kind: 'equipment', displayName: '', description: '', iconKey: '', tier: 'common',
    category: 'Weapon', slotKind: 'any', costGold: 0,
    consumable: blankConsumable(), maxStacks: 0,
    mods: { hp: 0, damage: 0, armor: 0, attackSpeed: 0, moveSpeed: 0, healthRegen: 0, maxShield: 0, dodgePct: 0, blockPct: 0 },
    elemental: [],
    onHit: blankProc(), onStruck: blankProc(),
    crafting: { isRecipe: false, recipeCost: 150, inputs: ['', ''], starter: false },
    allowedUnitTypes: [],
    unmodeled: {},
  }
}

function procFormFromWire(p: ItemDef['onHitProc']): ProcForm {
  if (!p) return blankProc()
  return {
    enabled: true, effect: p.effect ?? '', chancePct: Math.round(p.chance * 100),
    // Wire carries RESOLVED values; overrides are not distinguishable from
    // base values on the wire, so loading an item shows resolved numbers as
    // placeholders and leaves overrides null (= inherit). Editing a field
    // sets an override.
    damage: null, projectileScale: null, bounceCount: null, bounceRange: null, bounceDamageFalloff: null,
    slowMultiplier: null, slowDurationSeconds: null, burnDamagePerSecond: null, burnDurationSeconds: null,
  }
}

export function formFromDef(
  def: ItemDef,
  recipe: { inputs: string[]; costGold: number; starter?: boolean } | null,
): ItemEditorForm {
  const m = def.modifiers ?? {}
  const c = def.consumable
  // Craftability is authoritative from the existing recipe when one is present
  // (shipped swords/shields predate the item.isRecipe flag), falling back to
  // the item's own flag/cost for editor-created items.
  const craftable = recipe !== null || (def.isRecipe ?? false)
  return {
    id: def.id, isNew: false, kind: def.kind === 'consumable' ? 'consumable' : 'equipment',
    displayName: def.displayName, description: def.description ?? '',
    iconKey: def.iconKey, tier: def.tier, category: def.category ?? 'Weapon', slotKind: def.slotKind, costGold: def.costGold,
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
    onHit: procFormFromWire(def.onHitProc), onStruck: procFormFromWire(def.onStruckProc),
    crafting: {
      isRecipe: craftable,
      recipeCost: recipe?.costGold ?? def.recipeCost ?? 150,
      inputs: recipe ? [...recipe.inputs] : ['', ''],
      starter: recipe?.starter ?? def.recipeStarter ?? false,
    },
    allowedUnitTypes: [...(def.allowedUnitTypes ?? [])],
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

function procWireFromForm(p: ProcForm): Record<string, unknown> | undefined {
  if (!p.enabled || !p.effect) return undefined
  const wire: Record<string, unknown> = { chance: p.chancePct / 100, effect: p.effect }
  const overrides: [string, number | null][] = [
    ['damage', p.damage], ['projectileScale', p.projectileScale], ['bounceCount', p.bounceCount],
    ['bounceRange', p.bounceRange], ['bounceDamageFalloff', p.bounceDamageFalloff],
    ['slowMultiplier', p.slowMultiplier], ['slowDurationSeconds', p.slowDurationSeconds],
    ['burnDamagePerSecond', p.burnDamagePerSecond], ['burnDurationSeconds', p.burnDurationSeconds],
  ]
  for (const [key, v] of overrides) {
    if (v !== null && v !== 0) wire[key] = v
  }
  return wire
}

export function saveRequestFromForm(form: ItemEditorForm): EditorSaveRequest {
  // Fields common to both kinds. `unmodeled` (requiredBuilding/effects) is
  // preserved for either kind; equipment- and consumable-specific blocks are
  // added below so a consumable never carries stat/proc data and vice versa.
  const item: Record<string, unknown> = {
    ...form.unmodeled,
    id: form.id, displayName: form.displayName, iconKey: form.iconKey || form.id,
    kind: form.kind, tier: form.tier, category: form.category, slotKind: form.slotKind,
    costGold: form.costGold,
  }
  if (form.description) item.description = form.description
  if (form.allowedUnitTypes.length > 0) item.allowedUnitTypes = form.allowedUnitTypes

  // Craftability lives on the item (isRecipe + recipeCost); the ingredient
  // list rides at the request top level and the server syncs the recipe def.
  const inputs = form.crafting.isRecipe ? form.crafting.inputs.filter(Boolean) : []
  if (form.crafting.isRecipe) {
    item.isRecipe = true
    if (form.crafting.recipeCost) item.recipeCost = form.crafting.recipeCost
    if (form.crafting.starter) item.recipeStarter = true
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
    return { item, inputs }
  }

  // Equipment: stat modifiers, on-hit elemental, and procs.
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
  if (elemental.length > 0) item.onHitElemental = elemental
  const onHit = procWireFromForm(form.onHit)
  if (onHit) item.onHitProc = onHit
  const onStruck = procWireFromForm(form.onStruck)
  if (onStruck) item.onStruckProc = onStruck

  return { item, inputs }
}
