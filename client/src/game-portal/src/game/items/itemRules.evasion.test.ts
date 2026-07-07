import { describe, expect, it } from 'vitest'
import { buildItemTooltipBody } from './itemRules'
import type { ItemDef } from '../maps/itemDefs'

const fireShield: ItemDef = {
  id: 'fire_shield', displayName: 'Fire Shield', iconKey: 'fire_shield',
  kind: 'equipment', tier: 'rare', slotKind: 'any', costGold: 0,
  modifiers: { armor: 35, blockChance: 0.15 },
  onStruckProc: { chance: 0.1, effect: 'fire_bolt_ignite', damage: 25, damageType: 'fire', projectileID: 'fire_bolt' },
}

const elvenCloak: ItemDef = {
  id: 'elven_cloak', displayName: 'Elven Cloak', iconKey: 'elven_cloak',
  kind: 'equipment', tier: 'uncommon', slotKind: 'any', costGold: 150,
  modifiers: { armor: 15, dodgeChance: 0.15 },
}

describe('buildItemTooltipBody — dodge/block + struck procs', () => {
  it('renders block chance and the when-hit proc line', () => {
    const body = buildItemTooltipBody(fireShield)
    expect(body).toContain('+35 Armor')
    expect(body).toMatch(/\+15% Block Chance/i)
    expect(body).toMatch(/10% when hit/i)
    expect(body).toContain('25') // proc damage
    expect(body.toLowerCase()).toContain('fire')
  })
  it('renders dodge chance', () => {
    const body = buildItemTooltipBody(elvenCloak)
    expect(body).toMatch(/\+15% Dodge Chance/i)
  })
})
