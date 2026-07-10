import { describe, expect, it } from 'vitest'
import { createBlankForm, formFromDef, saveRequestFromForm } from './itemEditorForm'
import type { ItemDef } from '../maps/itemDefs'

const fireShield: ItemDef = {
  id: 'fire_shield', displayName: 'Fire Shield', iconKey: 'fire_shield',
  kind: 'equipment', tier: 'rare', slotKind: 'any', costGold: 0, category: 'Shield',
  modifiers: { armor: 35, blockChance: 0.15 },
  onStruckProc: { chance: 0.1, effect: 'fire_bolt_ignite', damage: 25, damageType: 'fire', projectileID: 'fire_bolt' },
}
describe('formFromDef / saveRequestFromForm round-trip', () => {
  it('converts fractions to percents and back, and carries craft fields', () => {
    const form = formFromDef(fireShield, { inputs: ['steel_shield', 'fire_ring'], costGold: 150, starter: true })
    expect(form.mods.blockPct).toBe(15)
    expect(form.onStruck.enabled).toBe(true)
    expect(form.onStruck.chancePct).toBe(10)
    expect(form.onStruck.effect).toBe('fire_bolt_ignite')
    expect(form.crafting.isRecipe).toBe(true)
    expect(form.crafting.recipeCost).toBe(150)
    expect(form.crafting.starter).toBe(true)
    expect(form.crafting.inputs).toEqual(['steel_shield', 'fire_ring'])
    expect(form.isNew).toBe(false)

    const req = saveRequestFromForm(form)
    const item = req.item as Record<string, any>
    expect(item.modifiers.blockChance).toBeCloseTo(0.15)
    expect(item.modifiers.armor).toBe(35)
    expect(item.modifiers.dodgeChance).toBeUndefined() // zero mods omitted
    expect(item.onStruckProc).toEqual({ chance: 0.1, effect: 'fire_bolt_ignite' }) // overrides all null → omitted
    expect(item.onHitProc).toBeUndefined() // disabled proc omitted
    expect(item.isRecipe).toBe(true)
    expect(item.recipeCost).toBe(150)
    expect(item.recipeStarter).toBe(true)
    expect(req.inputs).toEqual(['steel_shield', 'fire_ring'])
  })

  it('includes only non-null proc overrides', () => {
    const form = createBlankForm()
    form.id = 'x'
    form.onHit.enabled = true
    form.onHit.effect = 'lightning_chain'
    form.onHit.chancePct = 25
    form.onHit.bounceCount = 4
    const item = saveRequestFromForm(form).item as Record<string, any>
    expect(item.onHitProc).toEqual({ chance: 0.25, effect: 'lightning_chain', bounceCount: 4 })
  })

  it('blank form: not craftable, everything off, empty elemental', () => {
    const form = createBlankForm()
    expect(form.isNew).toBe(true)
    const req = saveRequestFromForm(form)
    expect(req.inputs).toEqual([])
    const item = req.item as Record<string, any>
    expect(item.isRecipe).toBeUndefined()
    expect(item.recipeCost).toBeUndefined()
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
      slotKind: 'any', costGold: 0, requiredBuilding: 'marketplace', allowedUnitTypes: ['soldier'],
    }
    const form = formFromDef(scimitarish, { inputs: ['broad_sword', 'broad_sword', 'broad_sword'], costGold: 100 })
    expect(form.crafting.inputs).toEqual(['broad_sword', 'broad_sword', 'broad_sword'])
    expect(form.allowedUnitTypes).toEqual(['soldier'])
    const req = saveRequestFromForm(form)
    const item = req.item as Record<string, any>
    expect(item.requiredBuilding).toBe('marketplace')
    expect(item.allowedUnitTypes).toEqual(['soldier'])
    expect(req.inputs).toEqual(['broad_sword', 'broad_sword', 'broad_sword'])
  })

  it('never leaks resolved proc wire fields through preservation', () => {
    const withProc: ItemDef = {
      id: 'p', displayName: 'P', iconKey: 'p', kind: 'equipment', tier: 'rare', slotKind: 'any', costGold: 0,
      onHitProc: { chance: 0.1, effect: 'fire_bolt_ignite', damage: 25, damageType: 'fire', projectileID: 'fire_bolt' },
    }
    const form = formFromDef(withProc, null)
    const item = saveRequestFromForm(form).item as Record<string, any>
    expect(item.onHitProc).toEqual({ chance: 0.1, effect: 'fire_bolt_ignite' })
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
      tier: 'rare', slotKind: 'any', costGold: 40, category: 'Consumable',
      consumable: { type: 'heal', amount: 120, range: 150, split: false, durationSeconds: 0 },
      maxStacks: 5,
    }
    const form = formFromDef(potion, null)
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
    expect(item.onHitProc).toBeUndefined()
    expect(item.onStruckProc).toBeUndefined()
    expect(item.onHitElemental).toBeUndefined()
    expect(req.inputs).toEqual([])
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
