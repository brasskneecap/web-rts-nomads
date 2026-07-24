// perkModifierModel.test.ts
import { describe, expect, it } from 'vitest'
import { buildModifierList, KIND_META, KIND_ORDER, type ModifierLabels } from './perkModifierModel'
import type { AuthoredPerkDef } from '@/game/perks/perkEditorForm'

const labels: ModifierLabels = {
  statLabel: (id) => ({ abilityDamage: 'Ability Damage', maxHp: 'Max Health' }[id] ?? id),
  abilityStatLabel: (id) => ({ damageTaken: 'Vulnerable (Damage Taken)', moveSpeed: 'Move Speed' }[id] ?? id),
  abilityLabel: (id) => ({ marker_trap: 'Marker Trap' }[id] ?? id),
}

const amplified: AuthoredPerkDef = {
  id: 'amplified_effects',
  statModifiers: [{ stat: 'abilityDamage', op: 'multiply', value: 1.35 }],
  abilityStats: [
    { stat: 'damageTaken', flat: 0.15 },
    { stat: 'moveSpeed', flat: -0.15 },
  ],
  abilityFields: [{ target: 'marker_trap', action: 'mark', field: 'duration', op: 'multiply', value: 1.35 }],
}

describe('buildModifierList', () => {
  it('projects every modifier array into ordered, addressable entries', () => {
    const list = buildModifierList(amplified, labels)
    expect(list.map((e) => e.kind)).toEqual(['unitStat', 'abilityStat', 'abilityStat', 'abilityField'])
    expect(list.map((e) => [e.arrayKey, e.index])).toEqual([
      ['statModifiers', 0], ['abilityStats', 0], ['abilityStats', 1], ['abilityFields', 0],
    ])
  })

  it('builds human summaries using the injected label lookups', () => {
    const [unit, vuln, slow, field] = buildModifierList(amplified, labels)
    expect(unit.summary).toBe('Ability Damage ×1.35')
    expect(vuln.summary).toBe('Vulnerable (Damage Taken) +15%')
    expect(slow.summary).toBe('Move Speed −15%')
    expect(field.summary).toBe('Marker Trap ▸ mark ▸ duration ×1.35')
  })

  it('tags each entry with its kind meta (accent + label)', () => {
    const [unit] = buildModifierList(amplified, labels)
    expect(unit.meta).toBe(KIND_META.unitStat)
    expect(KIND_META.unitStat.label).toBe('Unit Stat Modifier')
    expect(KIND_META.abilityStat.accent).not.toBe(KIND_META.unitStat.accent)
  })

  it('emits nothing for a perk with no modifiers', () => {
    expect(buildModifierList({ id: 'empty' }, labels)).toEqual([])
  })

  it('renders a grant-ability entry per granted id and a config entry per key', () => {
    const list = buildModifierList(
      { id: 'p', grantsAbilities: ['dash', 'blink'], config: { radius: 120 } },
      labels,
    )
    expect(list.filter((e) => e.kind === 'grantAbility')).toHaveLength(2)
    expect(list.filter((e) => e.kind === 'configValue')).toHaveLength(1)
  })

  it('keeps KIND_ORDER and KIND_META in sync (no kind silently dropped)', () => {
    expect([...KIND_ORDER].sort()).toEqual(Object.keys(KIND_META).sort())
  })
})
