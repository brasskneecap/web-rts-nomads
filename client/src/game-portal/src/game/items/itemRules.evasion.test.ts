import { describe, expect, it } from 'vitest'
import { buildItemTooltipBody } from './itemRules'
import type { ItemDef } from '../maps/itemDefs'

const fireShield: ItemDef = {
  id: 'fire_shield', displayName: 'Fire Shield', iconKey: 'fire_shield',
  kind: 'equipment', tier: 'rare', costGold: 0,
  modifiers: { armor: 35, blockChance: 0.15 },
  procs: [{ trigger: 'onStruck', chance: 0.1, ability: 'frost_bolt' }],
}

const elvenCloak: ItemDef = {
  id: 'elven_cloak', displayName: 'Elven Cloak', iconKey: 'elven_cloak',
  kind: 'equipment', tier: 'uncommon', costGold: 150,
  modifiers: { armor: 15, dodgeChance: 0.15 },
}

describe('buildItemTooltipBody — dodge/block + struck procs', () => {
  it('renders block chance and the when-hit proc line', () => {
    const body = buildItemTooltipBody(fireShield)
    expect(body).toContain('+35 Armor')
    expect(body).toMatch(/\+15% Block Chance/i)
    expect(body).toMatch(/10% when hit/i)
    expect(body).toContain('cast frost_bolt') // the struck proc casts its ability
  })
  it('renders dodge chance', () => {
    const body = buildItemTooltipBody(elvenCloak)
    expect(body).toMatch(/\+15% Dodge Chance/i)
  })

  it('renders one line per proc when an item carries several', () => {
    const stormBrand: ItemDef = {
      id: 'storm_brand', displayName: 'Storm Brand', iconKey: 'storm_brand',
      kind: 'equipment', tier: 'epic', costGold: 0,
      procs: [
        { trigger: 'onHit', chance: 0.1, ability: 'fire_bolt' },
        { trigger: 'onHit', chance: 0.25, ability: 'chain_lightning' },
        { trigger: 'onStruck', chance: 0.5, ability: 'frost_bolt' },
      ],
    }
    const body = buildItemTooltipBody(stormBrand)
    expect(body).toContain('10% on hit: cast fire_bolt')
    expect(body).toContain('25% on hit: cast chain_lightning')
    expect(body).toContain('50% when hit: cast frost_bolt at the attacker')
  })
})
