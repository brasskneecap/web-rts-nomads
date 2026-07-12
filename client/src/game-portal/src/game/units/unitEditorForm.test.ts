import { describe, expect, it } from 'vitest'
import { createBlankForm, formFromDef, saveRequestFromForm, type AuthoredUnitDef } from './unitEditorForm'

const fullDef: AuthoredUnitDef = {
  type: 'archer', faction: 'human', name: 'Archer', archetype: 'ranged',
  hp: 120, armor: 2, damage: 18, attackRange: 5, attackSpeed: 1.2, splashRadius: 0,
  moveSpeed: 2.4, resourceCost: { gold: 40 }, meatCost: 1, spawnSeconds: 8,
  capabilities: ['attack'], combatProfile: 'ranged_basic', attackType: 'bow',
  damageType: 'physical', targetableTypes: ['ground', 'flyer'], projectile: 'arrow',
  projectileScale: 1, goldGatherAmount: 0, woodGatherAmount: 0, maxMana: 0,
  manaRegenRate: 0, visionRange: 6, flyer: false, abilities: [],
  requiresBuildings: [], pathChances: {}, dominionPointDropChance: 0.1,
  dominionPointAmount: 1, spawnExp: 0, nonCombat: false,
  // art blobs the form does NOT model — must survive untouched:
  attackVisual: { anchor: 'hand' }, bounds: { w: 20, h: 40 }, shadow: { scale: 0.6 },
}

describe('unitEditorForm round-trip', () => {
  it('formFromDef -> saveRequestFromForm is lossless, incl. art blobs', () => {
    const out = saveRequestFromForm(formFromDef(fullDef))
    expect(out).toEqual(fullDef)
  })

  it('createBlankForm produces a settable shell with no art', () => {
    const form = createBlankForm()
    form.type = 'my_unit'
    form.faction = 'human'
    const def = saveRequestFromForm(form)
    expect(def.type).toBe('my_unit')
    expect(def.faction).toBe('human')
    expect(def.attackVisual).toBeUndefined()
  })
})
