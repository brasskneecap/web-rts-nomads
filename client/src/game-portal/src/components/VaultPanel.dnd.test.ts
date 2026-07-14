// Mechanical reproduction of the vault drag-and-drop chain against the REAL
// VaultPanel component: dragstart on a storage cell → drop on a base-rank
// unit's first inventory slot → onEquipItem must fire with (unitId, 0,
// instanceId). Exists to pin the storage→unit-slot equip flow after the
// base-units-get-a-slot change put base-rank units in the vault list.

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createApp, nextTick } from 'vue'
import VaultPanel from './VaultPanel.vue'
import { initItemDefs, type ItemDef } from '@/game/maps/itemDefs'
import type { Unit } from '@/game/core/GameState'

// ActionIcon draws sprite canvases from the icon atlas, which isn't loaded in
// happy-dom — stub it; the drag logic never touches it (icon canvases are
// pointer-events: none).
vi.mock('@/components/ActionIcon.vue', () => ({
  default: { name: 'ActionIcon', props: ['action'], template: '<canvas />' },
}))

function mkItem(id: string): ItemDef {
  return {
    id,
    displayName: id,
    iconKey: id,
    kind: 'equipment',
    tier: 'common',
   
    costGold: 10,
  }
}

function mkConsumable(id: string): ItemDef {
  return {
    id,
    displayName: id,
    iconKey: id,
    kind: 'consumable',
    tier: 'common',
   
    costGold: 10,
    maxStacks: 5,
    consumable: { type: 'heal', amount: 50 },
  }
}

function mkBaseSoldier(id: number): Unit {
  return {
    id,
    unitType: 'soldier',
    name: 'Soldier',
    rank: 'base',
    xp: 0,
    xpIntoCurrentRank: 0,
    xpToNextRank: 100,
    perkIds: [],
    path: 'none',
    hp: 100,
    maxHp: 100,
    inventory: { size: 1, slots: [null, null, null] },
  } as unknown as Unit
}

function mountVault(units: Unit[]) {
  const onEquipItem = vi.fn()
  const onSelectVaultItem = vi.fn()
  const onUseItemOnUnit = vi.fn()
  const props = {
    vault: [
      { instanceId: 7, itemId: 'broad_sword', stacks: 1 },
      { instanceId: 9, itemId: 'potion', stacks: 3 },
    ],
    vaultSelectedInstanceId: null,
    units,
    onSelectVaultItem,
    onUnequipItem: vi.fn(),
    onEquipItem,
    onUseConsumable: vi.fn(),
    onTransferItem: vi.fn(),
    onUseItemOnUnit,
    onFocusUnit: vi.fn(),
    embedded: true,
  }
  const host = document.createElement('div')
  document.body.appendChild(host)
  const app = createApp(VaultPanel, props)
  app.config.warnHandler = () => {} // silence prop-shape warnings from the cast Unit
  app.mount(host)
  return { onEquipItem, onUseItemOnUnit, host, app }
}

beforeEach(() => {
  document.body.innerHTML = ''
  initItemDefs([mkItem('broad_sword'), mkConsumable('potion')])
})

describe('VaultPanel drag-and-drop', () => {
  it('lists a base-rank unit with 1 unlocked + 2 locked slots', async () => {
    mountVault([mkBaseSoldier(42)])
    await nextTick()

    const slots = document.querySelectorAll('.inv__slot')
    expect(slots.length).toBe(3)
    expect(slots[0].classList.contains('inv__slot--locked')).toBe(false)
    expect(slots[1].classList.contains('inv__slot--locked')).toBe(true)
    expect(slots[2].classList.contains('inv__slot--locked')).toBe(true)
  })

  it('equips a storage item dropped on a base-rank unit slot', async () => {
    const { onEquipItem } = mountVault([mkBaseSoldier(42)])
    await nextTick()

    const cell = document.querySelector('.storage__cell:not(.storage__cell--empty)')
    expect(cell, 'storage grid must render the vault item').not.toBeNull()
    cell!.dispatchEvent(new Event('dragstart', { bubbles: true }))
    await nextTick()

    const slot = document.querySelector('.inv__slot')
    expect(slot, 'unit card must render inventory slots').not.toBeNull()

    // dragover must be preventDefault-ed or the browser never allows a drop.
    const over = new Event('dragover', { bubbles: true, cancelable: true })
    slot!.dispatchEvent(over)
    expect(over.defaultPrevented, 'dragover must be accepted on an unlocked empty slot').toBe(true)

    const drop = new Event('drop', { bubbles: true, cancelable: true })
    slot!.dispatchEvent(drop)
    await nextTick()

    expect(onEquipItem).toHaveBeenCalledTimes(1)
    expect(onEquipItem).toHaveBeenCalledWith(42, 0, 7)
  })

  it('applies a bag consumable dragged onto a unit card', async () => {
    const { onUseItemOnUnit, onEquipItem } = mountVault([mkBaseSoldier(42)])
    await nextTick()

    // The consumable renders in the "Items" bag section, not the equipment grid.
    const bagCell = document.querySelector('.bag__cell')
    expect(bagCell, 'bag section must render the consumable').not.toBeNull()
    bagCell!.dispatchEvent(new Event('dragstart', { bubbles: true }))
    await nextTick()

    const card = document.querySelector('.ucard')
    expect(card, 'unit card must render').not.toBeNull()

    // The whole card is a drop target for consumables.
    const over = new Event('dragover', { bubbles: true, cancelable: true })
    card!.dispatchEvent(over)
    expect(over.defaultPrevented, 'dragover must be accepted on the card').toBe(true)

    card!.dispatchEvent(new Event('drop', { bubbles: true, cancelable: true }))
    await nextTick()

    expect(onUseItemOnUnit).toHaveBeenCalledTimes(1)
    expect(onUseItemOnUnit).toHaveBeenCalledWith(42, 9)
    // A consumable drop must never be mistaken for an equip.
    expect(onEquipItem).not.toHaveBeenCalled()
  })

  it('rejects drops on locked slots', async () => {
    const { onEquipItem } = mountVault([mkBaseSoldier(42)])
    await nextTick()

    const cell = document.querySelector('.storage__cell:not(.storage__cell--empty)')
    cell!.dispatchEvent(new Event('dragstart', { bubbles: true }))
    await nextTick()

    const locked = document.querySelectorAll('.inv__slot')[1]
    locked.dispatchEvent(new Event('drop', { bubbles: true, cancelable: true }))
    await nextTick()

    expect(onEquipItem).not.toHaveBeenCalled()
  })
})
