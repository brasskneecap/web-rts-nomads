import { describe, expect, it } from 'vitest'
import { buildItemTooltipBody } from './itemRules'
import type { ItemDef } from '../maps/itemDefs'

const fireSword: ItemDef = {
  id: 'fire_sword', displayName: 'Fire Sword', iconKey: 'fire_sword',
  kind: 'equipment', tier: 'rare', slotKind: 'any', costGold: 0,
  modifiers: { damage: 5 },
  onHitElemental: [{ type: 'fire', amount: 5 }],
  onHitProc: { chance: 0.05, damage: 25, damageType: 'fire', projectileID: 'fire_bolt' },
}

describe('buildItemTooltipBody — elemental on-hit + proc', () => {
  it('includes the physical damage, the elemental on-hit, and the proc', () => {
    const body = buildItemTooltipBody(fireSword)
    expect(body).toContain('+5 Damage')
    expect(body.toLowerCase()).toContain('fire') // elemental on-hit
    expect(body).toContain('5') // +5 fire
    expect(body).toMatch(/5%/) // proc chance rendered as a percent
    expect(body).toContain('25') // proc damage
  })
})
