import { describe, expect, it } from 'vitest'
import type { AbilityActionDef } from '@/game/abilities/program/abilityProgram'
import { humanizeActionType, summarizeAction } from './summarizeAction'

function action(partial: Partial<AbilityActionDef> & Pick<AbilityActionDef, 'id' | 'type'>): AbilityActionDef {
  return partial
}

describe('humanizeActionType', () => {
  it('title-cases a snake_case action type', () => {
    expect(humanizeActionType('deal_damage')).toBe('Deal Damage')
    expect(humanizeActionType('select_targets')).toBe('Select Targets')
  })

  it('returns an empty string for an empty type', () => {
    expect(humanizeActionType('')).toBe('')
  })
})

describe('summarizeAction', () => {
  it('summarizes deal_damage with amount + type', () => {
    const a = action({ id: 'a1', type: 'deal_damage', config: { amount: 25, type: 'fire' } })
    expect(summarizeAction(a, null)).toBe('Deal Damage — 25 fire')
  })

  it('summarizes restore_health with amount', () => {
    const a = action({ id: 'a1', type: 'restore_health', config: { amount: 40 } })
    expect(summarizeAction(a, null)).toBe('Restore Health — 40')
  })

  it('summarizes select_targets with relations + radius', () => {
    const a = action({
      id: 'a1',
      type: 'select_targets',
      target: { source: 'caster', relations: ['enemy', 'neutral'], radius: 200 },
    })
    expect(summarizeAction(a, null)).toBe('Select Targets — enemy/neutral within 200')
  })

  it('summarizes create_zone with config.name', () => {
    const a = action({ id: 'a1', type: 'create_zone', config: { name: 'Firestorm' } })
    expect(summarizeAction(a, null)).toBe('Create Zone — Firestorm')
  })

  it('summarizes summon_unit with count + unitType', () => {
    const a = action({ id: 'a1', type: 'summon_unit', config: { count: 3, unitType: 'skeleton' } })
    expect(summarizeAction(a, null)).toBe('Summon Unit — 3× skeleton')
  })

  it('summarizes apply_status with config.status', () => {
    const a = action({ id: 'a1', type: 'apply_status', config: { status: 'slow' } })
    expect(summarizeAction(a, null)).toBe('Apply Status — slow')
  })

  it('falls back to just the label for an unknown type', () => {
    const a = action({ id: 'a1', type: 'camera_shake', config: { intensity: 5 } })
    expect(summarizeAction(a, null)).toBe('Camera Shake')
  })

  it('falls back to just the label when config is missing', () => {
    const a = action({ id: 'a1', type: 'deal_damage' })
    expect(summarizeAction(a, null)).toBe('Deal Damage')
  })

  it('never throws on missing/malformed config fields', () => {
    const a = action({ id: 'a1', type: 'deal_damage', config: { amount: 'not-a-number' as unknown as number } })
    expect(() => summarizeAction(a, null)).not.toThrow()
    const b = action({ id: 'b1', type: 'summon_unit', config: {} })
    expect(() => summarizeAction(b, null)).not.toThrow()
    expect(summarizeAction(b, null)).toBe('Summon Unit')
  })
})
