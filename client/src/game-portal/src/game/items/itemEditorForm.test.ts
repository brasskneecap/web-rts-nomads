import { describe, expect, it } from 'vitest'
import { blankProc, createBlankForm, formFromDef, saveRequestFromForm } from './itemEditorForm'
import type { ItemDef } from '../maps/itemDefs'

const fireShield: ItemDef = {
  id: 'fire_shield', displayName: 'Fire Shield', iconKey: 'fire_shield',
  kind: 'equipment', tier: 'rare', costGold: 0, category: 'Shield',
  modifiers: { armor: 35, blockChance: 0.15 },
  procs: [{ trigger: 'onStruck', chance: 0.1, ability: 'frost_bolt' }],
}
describe('formFromDef / saveRequestFromForm round-trip', () => {
  it('converts fractions to percents and back, and carries craft fields', () => {
    const form = formFromDef({
      ...fireShield,
      crafting: {
        inputs: ['steel_shield', 'fire_ring'], craftCostGold: 150, recipeCostGold: 300, starter: true,
      },
    })
    expect(form.mods.blockPct).toBe(15)
    expect(form.procs).toHaveLength(1)
    expect(form.procs[0].trigger).toBe('onStruck')
    expect(form.procs[0].chancePct).toBe(10)
    expect(form.procs[0].ability).toBe('frost_bolt')
    expect(form.crafting.isRecipe).toBe(true)
    expect(form.crafting.craftCost).toBe(150)
    expect(form.crafting.recipeCost).toBe(300)
    expect(form.crafting.starter).toBe(true)
    expect(form.crafting.inputs).toEqual(['steel_shield', 'fire_ring'])
    expect(form.isNew).toBe(false)

    const req = saveRequestFromForm(form)
    const item = req.item as Record<string, any>
    expect(item.modifiers.blockChance).toBeCloseTo(0.15)
    expect(item.modifiers.armor).toBe(35)
    expect(item.modifiers.dodgeChance).toBeUndefined() // zero mods omitted
    // overrides all null → omitted
    expect(item.procs).toEqual([{ trigger: 'onStruck', chance: 0.1, ability: 'frost_bolt' }])
    // Crafting rides ON the item — an item IS its own recipe — with the craft
    // cost and the recipe cost kept distinct.
    expect(item.crafting).toEqual({
      inputs: ['steel_shield', 'fire_ring'],
      craftCostGold: 150,
      recipeCostGold: 300,
      starter: true,
    })
  })

  it('keeps the three costs independent: purchase, craft, and recipe', () => {
    // The item's own price, the per-craft price, and the price of learning the
    // recipe are three separate numbers that must not bleed into each other.
    const form = formFromDef({
      ...fireShield,
      costGold: 40,
      crafting: { inputs: ['steel_shield', 'fire_ring'], craftCostGold: 150, recipeCostGold: 300 },
    })
    expect(form.costGold).toBe(40)
    expect(form.crafting.craftCost).toBe(150)
    expect(form.crafting.recipeCost).toBe(300)

    const item = saveRequestFromForm(form).item as Record<string, any>
    expect(item.costGold).toBe(40)
    expect(item.crafting.craftCostGold).toBe(150)
    expect(item.crafting.recipeCostGold).toBe(300)
  })

  it('saves every proc, including two on the same trigger', () => {
    const form = createBlankForm()
    form.id = 'storm_brand'
    form.procs = [
      { ...blankProc('onHit'), ability: 'fire_bolt', chancePct: 10 },
      { ...blankProc('onHit'), ability: 'chain_lightning', chancePct: 25 },
      { ...blankProc('onStruck'), ability: 'frost_bolt', chancePct: 50 },
    ]
    const item = saveRequestFromForm(form).item as Record<string, any>
    expect(item.procs).toEqual([
      { trigger: 'onHit', chance: 0.1, ability: 'fire_bolt' },
      { trigger: 'onHit', chance: 0.25, ability: 'chain_lightning' },
      { trigger: 'onStruck', chance: 0.5, ability: 'frost_bolt' },
    ])
  })

  it('saves an ability proc (casts an ability, no effect/overrides)', () => {
    const form = createBlankForm()
    form.id = 'frosty_blade'
    form.procs = [{ ...blankProc('onHit'), ability: 'frost_bolt', chancePct: 10 }]
    const item = saveRequestFromForm(form).item as Record<string, any>
    expect(item.procs).toEqual([{ trigger: 'onHit', chance: 0.1, ability: 'frost_bolt' }])
  })

  it('round-trips an ability proc through formFromDef', () => {
    const form = formFromDef({
      id: 'frosty_blade', displayName: 'Frosty Blade', iconKey: 'frosty_blade', kind: 'equipment',
      tier: 'rare', category: 'Weapon', costGold: 0,
      procs: [{ trigger: 'onHit', chance: 0.1, ability: 'frost_bolt' }],
    } as any)
    expect(form.procs[0].ability).toBe('frost_bolt')
    const item = saveRequestFromForm(form).item as Record<string, any>
    expect(item.procs).toEqual([{ trigger: 'onHit', chance: 0.1, ability: 'frost_bolt' }])
  })

  it('drops a proc row with no ability chosen', () => {
    const form = createBlankForm()
    form.id = 'x'
    form.procs = [blankProc('onHit'), { ...blankProc('onStruck'), ability: 'fire_bolt' }]
    const item = saveRequestFromForm(form).item as Record<string, any>
    expect(item.procs).toEqual([{ trigger: 'onStruck', chance: 0.1, ability: 'fire_bolt' }])
  })

  it('blank form: not craftable, everything off, empty elemental', () => {
    const form = createBlankForm()
    expect(form.isNew).toBe(true)
    const item = saveRequestFromForm(form).item as Record<string, any>
    // No crafting block at all — which is exactly what "not craftable" means.
    expect(item.crafting).toBeUndefined()
    expect(item.modifiers).toBeUndefined()
    expect(item.onHitElemental).toBeUndefined()
  })

  it('elemental rows with zero amounts are dropped', () => {
    const form = createBlankForm()
    form.id = 'x'
    form.elemental = [{ type: 'fire', amount: 5 }, { type: 'cold', amount: 0 }]
    const item = saveRequestFromForm(form).item as Record<string, any>
    expect(item.onHitElemental).toEqual([{ type: 'fire', amount: 5 }])
  })

  it('preserves unmodeled fields and 3-input recipes through an edit round-trip', () => {
    const scimitarish: ItemDef = {
      id: 'scim', displayName: 'Scim', iconKey: 'scim', kind: 'equipment', tier: 'uncommon',
      costGold: 0, requiredBuilding: 'marketplace',
    }
    const form = formFromDef({
      ...scimitarish,
      crafting: {
        inputs: ['broad_sword', 'broad_sword', 'broad_sword'], craftCostGold: 100, recipeCostGold: 100,
      },
    })
    expect(form.crafting.inputs).toEqual(['broad_sword', 'broad_sword', 'broad_sword'])
    const item = saveRequestFromForm(form).item as Record<string, any>
    expect(item.requiredBuilding).toBe('marketplace')
    expect(item.crafting.inputs).toEqual(['broad_sword', 'broad_sword', 'broad_sword'])
  })

  it('saves a proc as just trigger/chance/ability, no extra fields', () => {
    const withProc: ItemDef = {
      id: 'p', displayName: 'P', iconKey: 'p', kind: 'equipment', tier: 'rare', costGold: 0,
      procs: [{ trigger: 'onHit', chance: 0.1, ability: 'fire_bolt' }],
    }
    const form = formFromDef(withProc)
    const item = saveRequestFromForm(form).item as Record<string, any>
    expect(item.procs).toEqual([{ trigger: 'onHit', chance: 0.1, ability: 'fire_bolt' }])
    expect(item.overridden).toBeUndefined()
  })

  it('equipment save still emits kind:equipment and no consumable block', () => {
    const form = createBlankForm()
    form.id = 'x'
    const item = saveRequestFromForm(form).item as Record<string, any>
    expect(item.kind).toBe('equipment')
    expect(item.consumable).toBeUndefined()
    expect(item.maxStacks).toBeUndefined()
  })
})

describe('consumable items', () => {
  it('round-trips a consumable def and omits every equipment-only field', () => {
    const potion: ItemDef = {
      id: 'big_heal', displayName: 'Big Heal', iconKey: 'potion_0001', kind: 'consumable',
      tier: 'rare', costGold: 40, category: 'Consumable',
      consumable: { type: 'heal', amount: 120, range: 150, split: false, durationSeconds: 0 },
      maxStacks: 5,
    }
    const form = formFromDef(potion)
    expect(form.kind).toBe('consumable')
    expect(form.consumable.type).toBe('heal')
    expect(form.consumable.amount).toBe(120)
    expect(form.consumable.split).toBe(false)
    expect(form.maxStacks).toBe(5)

    const req = saveRequestFromForm(form)
    const item = req.item as Record<string, any>
    expect(item.kind).toBe('consumable')
    // durationSeconds 0 dropped; split always sent (default-true differs from false)
    expect(item.consumable).toEqual({ type: 'heal', amount: 120, range: 150, split: false })
    expect(item.maxStacks).toBe(5)
    expect(item.modifiers).toBeUndefined()
    expect(item.procs).toBeUndefined()
    expect(item.onHitElemental).toBeUndefined()
    expect(item.crafting).toBeUndefined()
  })

  it('a new consumable sends split:true by default and omits maxStacks when 0', () => {
    const form = createBlankForm()
    form.id = 'quick_heal'
    form.kind = 'consumable'
    form.consumable.type = 'heal'
    form.consumable.amount = 50
    const item = saveRequestFromForm(form).item as Record<string, any>
    expect(item.consumable).toEqual({ type: 'heal', amount: 50, split: true })
    expect(item.maxStacks).toBeUndefined()
  })
})
