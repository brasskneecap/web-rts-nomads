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
      path: 'fire',
      config: { duration: 5 },
      effect: { name: 'burn', target: 'enemies' },
      grantsAbilities: ['fireball'],
      futureKnob: 7,
    } as AuthoredPerkDef
    const form = formFromDef(def)
    expect(form.displayName).toBe('Flame Ward')
    expect(form.path).toBe('fire')
    expect(form.config).toEqual({ duration: 5 })
    expect(form.remainder).toEqual({ futureKnob: 7 })
    const out = saveRequestFromForm(form)
    expect(out.path).toBe('fire')
    expect(out.effect).toEqual({ name: 'burn', target: 'enemies' })
    expect((out as Record<string, unknown>).futureKnob).toBe(7)
  })
  it('does not surface unitType/rank as modeled form fields', () => {
    // unitType/rank no longer exist server-side; a real fetched def will
    // never carry them, so the realistic save request never emits them.
    const def = {
      id: 'flame-ward',
      path: 'fire',
    } as AuthoredPerkDef
    const form = formFromDef(def)
    expect(form.unitType).toBeUndefined()
    expect(form.rank).toBeUndefined()
    const out = saveRequestFromForm(form)
    expect(out.path).toBe('fire')
    expect('unitType' in out).toBe(false)
    expect('rank' in out).toBe(false)
  })
  it('a stray unitType/rank on an input def is not exposed as a modeled field (falls to unmodeled remainder, like any unrecognized key)', () => {
    const def = {
      id: 'flame-ward',
      path: 'fire',
      unitType: 'mage',
      rank: '3',
    } as AuthoredPerkDef
    const form = formFromDef(def)
    // not surfaced as first-class modeled properties for the panel to bind to
    expect(form.unitType).toBeUndefined()
    expect(form.rank).toBeUndefined()
    expect(form.remainder.unitType).toBe('mage')
    expect(form.remainder.rank).toBe('3')
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
