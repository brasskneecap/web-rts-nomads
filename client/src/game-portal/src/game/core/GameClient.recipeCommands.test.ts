import { describe, expect, it } from 'vitest'
import { GameClient } from './GameClient'

// Build a GameClient with its network + state stubbed just enough to exercise
// performSelectionAction's recipe branches.
function mkClient() {
  const sent: any[] = []
  const client = Object.create(GameClient.prototype) as GameClient
  ;(client as any).network = { send: (m: any) => sent.push(m) }
  ;(client as any).state = { getSelectedBuilding: () => ({ id: 'art-1', buildingType: 'artificer' }) }
  return { client, sent }
}

// The wire carries ITEM ids: an item is its own recipe, so a recipe has no
// identity of its own to send.
describe('performSelectionAction — recipe commands', () => {
  it('buy-recipe-<id> sends purchase_recipe with the building id and the item id', () => {
    const { client, sent } = mkClient()
    ;(client as any).state.getSelectedBuilding = () => ({ id: 'rs-1', buildingType: 'recipe-shop' })
    client.performSelectionAction('buy-recipe-fire_sword')
    expect(sent).toEqual([{ type: 'purchase_recipe', buildingId: 'rs-1', itemId: 'fire_sword' }])
  })

  it('craft-<id> sends craft_item with the item id', () => {
    const { client, sent } = mkClient()
    client.performSelectionAction('craft-fire_sword')
    expect(sent).toEqual([{ type: 'craft_item', itemId: 'fire_sword' }])
  })
})
