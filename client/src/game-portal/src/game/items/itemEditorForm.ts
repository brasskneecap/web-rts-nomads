// Item editor form module — pure transforms between the wire ItemDef shape
// (fractions, resolved proc payloads) and the editor form shape (percents,
// nullable proc overrides = "inherit"). No fetch/IO here; see itemEditorApi.ts
// for the service layer. Percent<->fraction conversion happens ONLY in this
// file per project convention.
import type { ItemDef } from '../maps/itemDefs'
import type { EditorSaveRequest, ItemAvailability } from './itemEditorApi'

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

export type ItemEditorForm = {
  id: string
  isNew: boolean
  displayName: string
  description: string
  iconKey: string
  tier: string
  category: string
  slotKind: string
  costGold: number
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
  crafting: { enabled: boolean; inputA: string; inputB: string; costGold: number }
  availability: ItemAvailability
}

const blankProc = (): ProcForm => ({
  enabled: false, effect: '', chancePct: 10,
  damage: null, projectileScale: null, bounceCount: null, bounceRange: null, bounceDamageFalloff: null,
  slowMultiplier: null, slowDurationSeconds: null, burnDamagePerSecond: null, burnDurationSeconds: null,
})

export function createBlankForm(): ItemEditorForm {
  return {
    id: '', isNew: true, displayName: '', description: '', iconKey: '', tier: 'common',
    category: 'Weapon', slotKind: 'any', costGold: 0,
    mods: { hp: 0, damage: 0, armor: 0, attackSpeed: 0, moveSpeed: 0, healthRegen: 0, maxShield: 0, dodgePct: 0, blockPct: 0 },
    elemental: [],
    onHit: blankProc(), onStruck: blankProc(),
    crafting: { enabled: false, inputA: '', inputB: '', costGold: 150 },
    availability: { marketplace: false, wanderingMerchant: false, lootTable: { enabled: false, weight: 10 }, recipeList: false },
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
  availability: ItemAvailability,
  recipe: { inputs: string[]; costGold: number } | null,
): ItemEditorForm {
  const m = def.modifiers ?? {}
  return {
    id: def.id, isNew: false, displayName: def.displayName, description: def.description ?? '',
    iconKey: def.iconKey, tier: def.tier, category: def.category ?? 'Weapon', slotKind: def.slotKind, costGold: def.costGold,
    mods: {
      hp: m.hp ?? 0, damage: m.damage ?? 0, armor: m.armor ?? 0, attackSpeed: m.attackSpeed ?? 0,
      moveSpeed: m.moveSpeed ?? 0, healthRegen: m.healthRegen ?? 0, maxShield: m.maxShield ?? 0,
      dodgePct: Math.round((m.dodgeChance ?? 0) * 100), blockPct: Math.round((m.blockChance ?? 0) * 100),
    },
    elemental: (def.onHitElemental ?? []).map((e) => ({ ...e })),
    onHit: procFormFromWire(def.onHitProc), onStruck: procFormFromWire(def.onStruckProc),
    crafting: recipe
      ? { enabled: true, inputA: recipe.inputs[0] ?? '', inputB: recipe.inputs[1] ?? '', costGold: recipe.costGold }
      : { enabled: false, inputA: '', inputB: '', costGold: 150 },
    availability: { ...availability, lootTable: { ...availability.lootTable, weight: availability.lootTable.weight || 10 } },
  }
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

  const item: Record<string, unknown> = {
    id: form.id, displayName: form.displayName, iconKey: form.iconKey || form.id,
    kind: 'equipment', tier: form.tier, category: form.category, slotKind: form.slotKind,
    costGold: form.costGold,
  }
  if (form.description) item.description = form.description
  if (Object.keys(modifiers).length > 0) item.modifiers = modifiers
  if (elemental.length > 0) item.onHitElemental = elemental
  const onHit = procWireFromForm(form.onHit)
  if (onHit) item.onHitProc = onHit
  const onStruck = procWireFromForm(form.onStruck)
  if (onStruck) item.onStruckProc = onStruck

  return {
    item,
    recipe: form.crafting.enabled
      ? { inputs: [form.crafting.inputA, form.crafting.inputB], costGold: form.crafting.costGold }
      : null,
    availability: {
      ...form.availability,
      recipeList: form.availability.recipeList && form.crafting.enabled,
    },
  }
}
