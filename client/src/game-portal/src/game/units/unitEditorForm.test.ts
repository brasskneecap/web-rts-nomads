import { describe, expect, it } from 'vitest'
import {
  createBlankForm,
  formFromDef,
  pickTemplateStats,
  saveRequestFromForm,
  TEMPLATE_UNIT_TYPE,
  type AuthoredUnitDef,
} from './unitEditorForm'

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
  attackOrigin: { default: { x: 3, y: -40 }, byFacing: { east: { x: 14, y: -30 } } },
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

describe('blank unit defaults', () => {
  const template: AuthoredUnitDef = {
    type: TEMPLATE_UNIT_TYPE, faction: 'human', name: 'Soldier',
    hp: 220, armor: 33, damage: 24, attackRange: 40, attackSpeed: 0.9,
    moveSpeed: 58, visionRange: 400, meatCost: 2,
  }

  it('clones the stat block from the template unit', () => {
    const form = createBlankForm(pickTemplateStats([template]))
    expect(form.hp).toBe(template.hp)
    expect(form.moveSpeed).toBe(template.moveSpeed)
    expect(form.attackRange).toBe(template.attackRange)
    expect(form.visionRange).toBe(template.visionRange)
  })

  it('does not clone identity or cost from the template', () => {
    const form = createBlankForm(pickTemplateStats([template]))
    expect(form.type).toBe('')
    expect(form.faction).toBe('')
    expect(form.name).toBeUndefined()
    expect(form.meatCost).toBeUndefined()
  })

  // The whole point: a blank unit must clear the server's stat floors, or the
  // author's first Save is a validation error they did nothing to cause.
  // Asserts the INVARIANTS the server enforces, not pinned numbers.
  it('produces a def that satisfies the server stat floors, template or not', () => {
    for (const defaults of [
      pickTemplateStats([template]),
      pickTemplateStats([{ ...template, visionRange: undefined }]), // per-key hole-filler
      pickTemplateStats([]),
    ]) {
      const def = saveRequestFromForm(createBlankForm(defaults))
      expect(def.hp!).toBeGreaterThan(0)
      expect(def.moveSpeed!).toBeGreaterThan(0)
      if ((def.damage ?? 0) > 0) {
        expect(def.attackRange!).toBeGreaterThan(0)
        expect(def.attackSpeed!).toBeGreaterThan(0)
      }
    }
  })

  // `??` not `||`: armor 0 is a legitimate authored value, not "missing".
  it('preserves a legitimate zero rather than falling back', () => {
    expect(pickTemplateStats([{ ...template, armor: 0 }]).armor).toBe(0)
  })
})

describe('healthRegenRate blank-vs-zero', () => {
  // The server models healthRegenRate as a POINTER so that "absent" (inherit
  // the global default) is distinguishable from an authored 0 (never
  // regenerates). The form must preserve that distinction end to end, or a
  // cleared field silently switches a unit's regen off.
  it('omits an unset healthRegenRate rather than sending 0', () => {
    const def = saveRequestFromForm(formFromDef({ type: 'a', faction: 'human' }))
    expect('healthRegenRate' in def).toBe(false)
  })

  it('round-trips an authored 0 (never regenerates) without dropping it', () => {
    const def = saveRequestFromForm(formFromDef({ type: 'construct', faction: 'human', healthRegenRate: 0 }))
    expect(def.healthRegenRate).toBe(0)
  })

  it('round-trips an authored positive rate', () => {
    const def = saveRequestFromForm(formFromDef({ type: 'troll', faction: 'human', healthRegenRate: 2.5 }))
    expect(def.healthRegenRate).toBe(2.5)
  })
})
