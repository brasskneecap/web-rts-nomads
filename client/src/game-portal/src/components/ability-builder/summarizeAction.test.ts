import { afterEach, describe, expect, it } from 'vitest'
import type { AbilityActionDef } from '@/game/abilities/program/abilityProgram'
import { PERK_DEF_MAP, type PerkDef } from '@/game/maps/perkDefs'
import { humanizeActionType, summarizeAction } from './summarizeAction'

function perkDef(overrides: Partial<PerkDef> & { id: string }): PerkDef {
  return { displayName: overrides.id, config: {}, ...overrides }
}

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

  it('applies the display override for store_targets', () => {
    expect(humanizeActionType('store_targets')).toBe('Save Targets')
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

  it('summarizes store_targets ("Save Targets") with its saved name, noting merge', () => {
    const a = action({ id: 'a1', type: 'store_targets', config: { as: 'chainHits' } })
    expect(summarizeAction(a, null)).toBe('Save Targets — chainHits')
    const merged = action({ id: 'a2', type: 'store_targets', config: { as: 'chainHits', merge: true } })
    expect(summarizeAction(merged, null)).toBe('Save Targets — chainHits (merge)')
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


  describe('conditional', () => {
    afterEach(() => {
      // Module-level singleton (populated by GameClient at match start) —
      // reset between tests so one test's perk fixtures never leak into
      // another (same pattern as activeBuffIconResolve.test.ts).
      PERK_DEF_MAP.clear()
    })

    it('summarizes has_perk with the perk\'s catalog display name when the perk catalog is loaded', () => {
      PERK_DEF_MAP.set('lasting_flames', perkDef({ id: 'lasting_flames', displayName: 'Lasting Flames' }))
      const a = action({
        id: 'a1',
        type: 'conditional',
        config: { conditions: [{ op: 'has_perk', right: 'lasting_flames' }] },
      })
      expect(summarizeAction(a, null)).toBe('Conditional — has perk: Lasting Flames')
    })

    it('falls back to a humanized id when the perk catalog has not been loaded (no fetch of its own)', () => {
      const a = action({
        id: 'a1',
        type: 'conditional',
        config: { conditions: [{ op: 'has_perk', right: 'lasting_flames' }] },
      })
      expect(summarizeAction(a, null)).toBe('Conditional — has perk: Lasting Flames')
    })

    it('summarizes not_perk as "missing perk: <name>"', () => {
      const a = action({
        id: 'a1',
        type: 'conditional',
        config: { conditions: [{ op: 'not_perk', right: 'lasting_flames' }] },
      })
      expect(summarizeAction(a, null)).toBe('Conditional — missing perk: Lasting Flames')
    })

    it('summarizes a scalar comparison as "<key> <symbol> <right>"', () => {
      const a = action({
        id: 'a1',
        type: 'conditional',
        config: { conditions: [{ op: 'gte', left: { key: 'selected_count' }, right: 2 }] },
      })
      expect(summarizeAction(a, null)).toBe('Conditional — selected_count >= 2')
    })

    it('joins multiple conditions with " & "', () => {
      const a = action({
        id: 'a1',
        type: 'conditional',
        config: {
          conditions: [
            { op: 'has_perk', right: 'lasting_flames' },
            { op: 'gte', left: { key: 'selected_count' }, right: 2 },
          ],
        },
      })
      expect(summarizeAction(a, null)).toBe('Conditional — has perk: Lasting Flames & selected_count >= 2')
    })

    it('falls back to just the label when there are no conditions', () => {
      const a = action({ id: 'a1', type: 'conditional', config: {} })
      expect(summarizeAction(a, null)).toBe('Conditional')
    })
  })
})
