import { describe, expect, it } from 'vitest'
import {
  createBlankForm,
  formFromDef,
  saveRequestFromForm,
  inferFamily,
  type AuthoredAbilityDef,
} from './abilityEditorForm'

describe('abilityEditorForm', () => {
  it('createBlankForm returns an empty-id form with a remainder bag', () => {
    const f = createBlankForm()
    expect(f.id).toBe('')
    expect(f.remainder).toEqual({})
  })

  it('formFromDef splits modeled keys from the remainder and round-trips', () => {
    const def: AuthoredAbilityDef = {
      id: 'heal',
      displayName: 'Heal',
      healAmount: 40,
      castRange: 'match_attack_range',
      // an unmodeled/future key must survive verbatim:
      futureKnob: { nested: true },
    } as AuthoredAbilityDef
    const form = formFromDef(def)
    expect(form.id).toBe('heal')
    expect(form.healAmount).toBe(40)
    expect(form.remainder).toEqual({ futureKnob: { nested: true } })

    const out = saveRequestFromForm(form)
    expect(out.castRange).toBe('match_attack_range')
    expect((out as Record<string, unknown>).futureKnob).toEqual({ nested: true })
    expect(out.healAmount).toBe(40)
  })

  it('saveRequestFromForm drops undefined modeled fields', () => {
    const form = createBlankForm()
    form.id = 'x'
    const out = saveRequestFromForm(form)
    expect('displayName' in out).toBe(false)
  })

  it('inferFamily picks the family from non-zero fields', () => {
    expect(inferFamily({ id: 'a', channelType: 'beam' } as AuthoredAbilityDef)).toBe('channel')
    expect(inferFamily({ id: 'a', chargeRequired: 5 } as AuthoredAbilityDef)).toBe('charge')
    expect(inferFamily({ id: 'a', impactDelaySeconds: 1.2 } as AuthoredAbilityDef)).toBe('meteor')
    expect(inferFamily({ id: 'a', chainCount: 3 } as AuthoredAbilityDef)).toBe('archmage')
    expect(inferFamily({ id: 'a', healAmount: 10 } as AuthoredAbilityDef)).toBe('basic')
  })
})
