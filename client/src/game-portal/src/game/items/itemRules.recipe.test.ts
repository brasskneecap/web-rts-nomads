import { describe, expect, it } from 'vitest'
import { buildItemTooltipBody } from './itemRules'
import type { ItemDef } from '../maps/itemDefs'

const fireSword: ItemDef = {
  id: 'fire_sword', displayName: 'Fire Sword', iconKey: 'fire_sword',
  kind: 'equipment', tier: 'rare', costGold: 0,
  modifiers: { damage: 5 },
  onHitElemental: [{ type: 'fire', amount: 5 }],
  procs: [{ trigger: 'onHit', chance: 0.05, ability: 'fire_bolt' }],
}

describe('buildItemTooltipBody — elemental on-hit + proc', () => {
  it('includes the physical damage, the elemental on-hit, and the proc', () => {
    const body = buildItemTooltipBody(fireSword)
    expect(body).toContain('+5 Damage')
    expect(body.toLowerCase()).toContain('fire') // elemental on-hit
    expect(body).toContain('5') // +5 fire
    expect(body).toMatch(/5%/) // proc chance rendered as a percent
    expect(body).toContain('cast fire_bolt') // the proc casts its ability
  })
})
