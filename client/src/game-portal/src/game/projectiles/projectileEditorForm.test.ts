import { describe, expect, it } from 'vitest'
import { createBlankForm, formFromDef, saveRequestFromForm, type AuthoredProjectileDef } from './projectileEditorForm'

describe('projectileEditorForm', () => {
  it('createBlankForm has empty id + remainder', () => {
    const f = createBlankForm()
    expect(f.id).toBe('')
    expect(f.remainder).toEqual({})
  })
  it('round-trips modeled fields and preserves unmodeled keys', () => {
    const def = { id: 'bolt', kind: 'beam', durationMs: 300, speed: 0, followEffect: 'fizzle', futureKnob: 9 } as AuthoredProjectileDef
    const form = formFromDef(def)
    expect(form.kind).toBe('beam')
    expect(form.remainder).toEqual({ futureKnob: 9 })
    const out = saveRequestFromForm(form)
    expect(out.followEffect).toBe('fizzle')
    expect((out as Record<string, unknown>).futureKnob).toBe(9)
  })
  it('saveRequestFromForm drops undefined modeled fields', () => {
    const form = createBlankForm()
    form.id = 'x'
    const out = saveRequestFromForm(form)
    expect('speed' in out).toBe(false)
  })
})
