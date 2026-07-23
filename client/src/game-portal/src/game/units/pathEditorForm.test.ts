import { describe, expect, it } from 'vitest'
import {
  createBlankPathForm,
  pathFormFromDef,
  saveRequestFromPathForm,
  type AuthoredPathDef,
} from './pathEditorForm'

const fullDef: AuthoredPathDef = {
  path: 'gold',
  description: 'The gold promotion path.',
  visionRange: 6,
  projectile: 'arrow',
  damageType: 'physical',
  attackType: 'bow',
  projectileScale: 1,
  abilities: ['piercing_shot', 'volley'],
  channelLoop: { start: 12, end: 30 },
  bounds: { halfWidth: 20, top: -40, bottom: 2 },
  ranks: {
    bronze: {
      maxHPMultiplier: 1.1, maxMPMultiplier: 1, healthRegenMultiplier: 1,
      damageMultiplier: 1.1, attackSpeedMultiplier: 1, moveSpeedMultiplier: 1,
      attackRange: 0, attackRangeMultiplier: 1, armor: 0, dodgeChance: 0, blockChance: 0,
    },
    silver: {
      maxHPMultiplier: 1.25, maxMPMultiplier: 1, healthRegenMultiplier: 1.1,
      damageMultiplier: 1.25, attackSpeedMultiplier: 1.05, moveSpeedMultiplier: 1,
      attackRange: 20, attackRangeMultiplier: 1, armor: 1, dodgeChance: 0.02, blockChance: 0,
    },
    gold: {
      maxHPMultiplier: 1.5, maxMPMultiplier: 1, healthRegenMultiplier: 1.2,
      damageMultiplier: 1.5, attackSpeedMultiplier: 1.1, moveSpeedMultiplier: 1.05,
      attackRange: 40, attackRangeMultiplier: 1.1, armor: 2, dodgeChance: 0.05, blockChance: 0.05,
    },
  },
  // unmodeled key the form does NOT know about — must survive untouched:
  futureExoticField: { some: 'blob' },
}

describe('pathEditorForm round-trip', () => {
  it('pathFormFromDef -> saveRequestFromPathForm is lossless, incl. unknown keys and full ranks grid', () => {
    const form = pathFormFromDef(fullDef, 'archer')
    const out = saveRequestFromPathForm(form)
    expect(out).toEqual({ unit: 'archer', path: fullDef })
  })

  it('createBlankPathForm produces a settable shell', () => {
    const form = createBlankPathForm('archer')
    form.path = 'bronze'
    const req = saveRequestFromPathForm(form)
    expect(req.unit).toBe('archer')
    expect(req.path.path).toBe('bronze')
  })
})

describe('parentUnit is client-only routing state', () => {
  it('does not leak into the persisted path object', () => {
    const form = pathFormFromDef(fullDef, 'archer')
    const req = saveRequestFromPathForm(form)
    expect('parentUnit' in req.path).toBe(false)
  })

  it('does not leak into remainder', () => {
    const form = pathFormFromDef(fullDef, 'archer')
    expect('parentUnit' in form.remainder).toBe(false)
  })

  it('is not read from the def even if present there (routing is caller-supplied)', () => {
    const defWithStrayParentUnit = { ...fullDef, parentUnit: 'wrong_unit' } as AuthoredPathDef
    const form = pathFormFromDef(defWithStrayParentUnit, 'archer')
    expect(form.parentUnit).toBe('archer')
    const req = saveRequestFromPathForm(form)
    expect(req.unit).toBe('archer')
    // the stray value from the def must not have been silently persisted either
    expect(req.path.parentUnit).toBeUndefined()
  })
})

describe('undefined-vs-0 preserved on rank multipliers', () => {
  it('omits an unset attackRangeMultiplier rather than coercing to 0', () => {
    const def: AuthoredPathDef = {
      path: 'bronze',
      ranks: { bronze: { maxHPMultiplier: 1 } },
    }
    const form = pathFormFromDef(def, 'archer')
    const req = saveRequestFromPathForm(form)
    expect('attackRangeMultiplier' in req.path.ranks!.bronze).toBe(false)
  })

  it('round-trips an explicit 0 on a rank multiplier without dropping it', () => {
    const def: AuthoredPathDef = {
      path: 'bronze',
      ranks: { bronze: { armor: 0, dodgeChance: 0 } },
    }
    const form = pathFormFromDef(def, 'archer')
    const req = saveRequestFromPathForm(form)
    expect(req.path.ranks!.bronze.armor).toBe(0)
    expect(req.path.ranks!.bronze.dodgeChance).toBe(0)
  })
})

describe('blank path form serialization', () => {
  it('serializes to a minimal request without spurious keys', () => {
    const form = createBlankPathForm('archer')
    const req = saveRequestFromPathForm(form)
    expect(req).toEqual({ unit: 'archer', path: { path: '', ranks: {} } })
  })
})

describe('perksByRank as a modeled field', () => {
  it('round-trips perksByRank as a modeled field', () => {
    const form = pathFormFromDef({ path: 'berserker', perksByRank: { bronze: ['tough'] } } as AuthoredPathDef, 'warrior')
    expect(form.perksByRank).toEqual({ bronze: ['tough'] })
    expect(form.remainder?.perksByRank).toBeUndefined()
    const out = saveRequestFromPathForm(form)
    expect(out.path.perksByRank).toEqual({ bronze: ['tough'] })
  })

  it('omits an unset perksByRank from the save output', () => {
    const form = pathFormFromDef({ path: 'berserker' } as AuthoredPathDef, 'warrior')
    expect(form.perksByRank).toBeUndefined()
    const out = saveRequestFromPathForm(form)
    expect('perksByRank' in out.path).toBe(false)
  })
})
