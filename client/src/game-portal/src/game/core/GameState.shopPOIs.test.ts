// Neutral shop POIs must survive fog of war. The welcome message carries the
// full authored map (all buildings, scouted or not), but every subsequent
// match_snapshot replaces mapConfig.buildings with a FOW-filtered list that
// simply omits unscouted shops. The minimap wants shop POIs always visible
// (same treatment as neutral-camp dots), so GameState captures them once in
// setMapConfig and never lets snapshot merges clobber them.

import { describe, expect, it } from 'vitest'
import { GameState } from './GameState'
import { createEditorMapConfig } from '../maps/mapConfig'
import type { BuildingCapability, BuildingTile, MatchSnapshotMessage } from '../network/protocol'

function makeShop(
  buildingType: string,
  x: number,
  y: number,
  capability: BuildingCapability,
): BuildingTile {
  return {
    id: `${buildingType}-${x}-${y}`,
    buildingType,
    x,
    y,
    width: 3,
    height: 3,
    occupied: true,
    visible: true,
    capabilities: [capability],
  }
}

function makeSnapshot(buildings: BuildingTile[]): MatchSnapshotMessage {
  return {
    type: 'match_snapshot',
    tick: 1,
    serverNow: 0,
    matchId: 'test-match',
    buildings,
    players: [],
    units: [],
    wave: { enabled: false, currentWave: 0, totalWaves: 0, state: '', timer: 0, waveDuration: 0 },
  }
}

describe('GameState — neutral shop POIs', () => {
  it('captures shop POIs from the welcome map config', () => {
    const state = new GameState()
    state.setMapConfig(
      createEditorMapConfig(32, 32, {
        buildings: [
          makeShop('neutral-shop', 10, 5, 'item-purchase'),
          makeShop('recipe-shop', 20, 8, 'recipe-purchase'),
        ],
      }),
    )

    expect(state.neutralShopPOIs.map((p) => p.buildingType).sort()).toEqual([
      'neutral-shop',
      'recipe-shop',
    ])
  })

  it('keeps the POIs when a FOW-filtered snapshot omits the shops', () => {
    const state = new GameState()
    state.setMapConfig(
      createEditorMapConfig(32, 32, {
        buildings: [makeShop('neutral-shop', 10, 5, 'item-purchase')],
      }),
    )

    // Server dropped the unscouted shop from the snapshot's building list.
    state.applySnapshot(makeSnapshot([]))

    expect(state.neutralShopPOIs).toHaveLength(1)
    expect(state.neutralShopPOIs[0]).toMatchObject({ buildingType: 'neutral-shop', x: 10, y: 5 })
  })
})
