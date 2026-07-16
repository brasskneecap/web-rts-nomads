import { describe, expect, it } from 'vitest'
import { createBlankForm, formFromDef, saveRequestFromForm, type AuthoredPerkDef } from './perkEditorForm'

describe('perkEditorForm', () => {
  it('createBlankForm has empty id + remainder', () => {
    const f = createBlankForm()
    expect(f.id).toBe('')
    expect(f.remainder).toEqual({})
  })
  it('round-trips modeled fields and preserves unmodeled keys', () => {
    const def = {
      id: 'flame-ward',
      displayName: 'Flame Ward',
      unitType: 'mage',
      path: 'fire',
      rank: '3',
      config: { duration: 5 },
      effect: { name: 'burn', target: 'enemies' },
      grantsAbilities: ['fireball'],
      futureKnob: 7,
    } as AuthoredPerkDef
    const form = formFromDef(def)
    expect(form.displayName).toBe('Flame Ward')
    expect(form.unitType).toBe('mage')
    expect(form.config).toEqual({ duration: 5 })
    expect(form.remainder).toEqual({ futureKnob: 7 })
    const out = saveRequestFromForm(form)
    expect(out.path).toBe('fire')
    expect(out.effect).toEqual({ name: 'burn', target: 'enemies' })
    expect((out as Record<string, unknown>).futureKnob).toBe(7)
  })
  it('saveRequestFromForm drops undefined modeled fields', () => {
    const form = createBlankForm()
    form.id = 'x'
    const out = saveRequestFromForm(form)
    expect('displayName' in out).toBe(false)
    expect('config' in out).toBe(false)
  })
  it('saveRequestFromForm drops wired (server-derived)', () => {
    const form = createBlankForm()
    form.id = 'x'
    form.wired = true
    const out = saveRequestFromForm(form)
    expect('wired' in out).toBe(false)
  })
})
