import { describe, expect, it } from 'vitest'
import { createBlankForm, formFromDef, saveRequestFromForm } from './itemEditorForm'
import type { ItemDef } from '../maps/itemDefs'

const fireShield: ItemDef = {
  id: 'fire_shield', displayName: 'Fire Shield', iconKey: 'fire_shield',
  kind: 'equipment', tier: 'rare', slotKind: 'any', costGold: 0, category: 'Shield',
  modifiers: { armor: 35, blockChance: 0.15 },
  onStruckProc: { chance: 0.1, effect: 'fire_bolt_ignite', damage: 25, damageType: 'fire', projectileID: 'fire_bolt' },
}
const avail = { marketplace: false, wanderingMerchant: false, lootTable: { enabled: false, weight: 0 }, recipeList: true }

describe('formFromDef / saveRequestFromForm round-trip', () => {
  it('converts fractions to percents and back', () => {
    const form = formFromDef(fireShield, avail, { inputs: ['steel_shield', 'fire_ring'], costGold: 150 })
    expect(form.mods.blockPct).toBe(15)
    expect(form.onStruck.enabled).toBe(true)
    expect(form.onStruck.chancePct).toBe(10)
    expect(form.onStruck.effect).toBe('fire_bolt_ignite')
    expect(form.crafting.enabled).toBe(true)
    expect(form.crafting.inputs).toEqual(['steel_shield', 'fire_ring'])
    expect(form.isNew).toBe(false)

    const req = saveRequestFromForm(form)
    const item = req.item as Record<string, any>
    expect(item.modifiers.blockChance).toBeCloseTo(0.15)
    expect(item.modifiers.armor).toBe(35)
    expect(item.modifiers.dodgeChance).toBeUndefined() // zero mods omitted
    expect(item.onStruckProc).toEqual({ chance: 0.1, effect: 'fire_bolt_ignite' }) // overrides all null → omitted
    expect(item.onHitProc).toBeUndefined() // disabled proc omitted
    expect(req.recipe).toEqual({ inputs: ['steel_shield', 'fire_ring'], costGold: 150 })
    expect(req.availability.recipeList).toBe(true)
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

  it('blank form: no recipe, everything off, empty elemental', () => {
    const form = createBlankForm()
    expect(form.isNew).toBe(true)
    const req = saveRequestFromForm(form)
    expect(req.recipe).toBeNull()
    const item = req.item as Record<string, any>
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
    const form = formFromDef(scimitarish, { marketplace: false, wanderingMerchant: false, lootTable: { enabled: false, weight: 0 }, recipeList: false },
      { inputs: ['broad_sword', 'broad_sword', 'broad_sword'], costGold: 100 })
    expect(form.crafting.inputs).toEqual(['broad_sword', 'broad_sword', 'broad_sword'])
    expect(form.allowedUnitTypes).toEqual(['soldier'])
    const req = saveRequestFromForm(form)
    const item = req.item as Record<string, any>
    expect(item.requiredBuilding).toBe('marketplace')
    expect(item.allowedUnitTypes).toEqual(['soldier'])
    expect(req.recipe?.inputs).toEqual(['broad_sword', 'broad_sword', 'broad_sword'])
  })

  it('never leaks resolved proc wire fields through preservation', () => {
    const withProc: ItemDef = {
      id: 'p', displayName: 'P', iconKey: 'p', kind: 'equipment', tier: 'rare', slotKind: 'any', costGold: 0,
      onHitProc: { chance: 0.1, effect: 'fire_bolt_ignite', damage: 25, damageType: 'fire', projectileID: 'fire_bolt' },
    }
    const form = formFromDef(withProc, { marketplace: false, wanderingMerchant: false, lootTable: { enabled: false, weight: 0 }, recipeList: false }, null)
    const item = saveRequestFromForm(form).item as Record<string, any>
    expect(item.onHitProc).toEqual({ chance: 0.1, effect: 'fire_bolt_ignite' })
    expect(item.overridden).toBeUndefined()
  })
})
