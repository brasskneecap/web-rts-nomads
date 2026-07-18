import { describe, expect, it } from 'vitest'
import { createBlankForm, formFromDef, saveRequestFromForm, type AuthoredEffectDef } from './effectEditorForm'

describe('effectEditorForm', () => {
  it('createBlankForm has empty id + remainder', () => {
    const f = createBlankForm()
    expect(f.id).toBe('')
    expect(f.remainder).toEqual({})
  })
  it('round-trips modeled fields and preserves unmodeled keys', () => {
    const def = { id: 'glow', duration: 0.5, anchor: 'head', futureKnob: 7 } as AuthoredEffectDef
    const form = formFromDef(def)
    expect(form.duration).toBe(0.5)
    expect(form.remainder).toEqual({ futureKnob: 7 })
    const out = saveRequestFromForm(form)
    expect(out.anchor).toBe('head')
    expect((out as Record<string, unknown>).futureKnob).toBe(7)
  })
  it('saveRequestFromForm drops undefined modeled fields', () => {
    const form = createBlankForm()
    form.id = 'x'
    const out = saveRequestFromForm(form)
    expect('duration' in out).toBe(false)
  })
})
