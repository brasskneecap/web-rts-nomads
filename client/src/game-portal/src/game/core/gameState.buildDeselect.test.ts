// Regression test: a worker that enters build mode must be dropped from the
// player's selection.
//
// Bug: a selected worker that started constructing stayed selected. Because it
// is hidden inside the building footprint (Status "Building"), the player could
// still issue it a move order, which pulled it back out and wedged the build.
//
// Fix: applySnapshot removes any selected unit whose status is "Building" /
// "Building (Paused)" from the selection (and selection order).

import { describe, expect, it } from 'vitest'
import { GameState } from './GameState'
import type { MatchSnapshotMessage, PlayerSnapshot, UnitSnapshot } from '../network/protocol'

function makePlayer(id: string): PlayerSnapshot {
  return { playerId: id, color: '#ffffff', teamId: 0, resources: [] }
}

function makeWorker(over: Partial<UnitSnapshot> = {}): UnitSnapshot {
  return {
    id: 1,
    ownerId: 'p1',
    color: '#ffffff',
    unitType: 'worker',
    name: 'Worker',
    visible: true,
    x: 0,
    y: 0,
    hp: 100,
    maxHp: 100,
    moving: false,
    ...over,
  }
}

function makeSnapshot(units: UnitSnapshot[]): MatchSnapshotMessage {
  return {
    type: 'match_snapshot',
    tick: 1,
    serverNow: 0,
    matchId: 'test-match',
    buildings: [],
    players: [makePlayer('p1')],
    units,
    wave: { enabled: false, currentWave: 0, totalWaves: 0, state: '', timer: 0, waveDuration: 0 },
  }
}

describe('GameState — deselect on entering build mode', () => {
  it('removes a worker from the selection once it starts building', () => {
    const state = new GameState()
    state.localPlayerId = 'p1'
    state.selectedUnitIds = new Set([1])
    state.selectedUnitOrder = [1]

    state.applySnapshot(makeSnapshot([makeWorker({ status: 'Building', visible: false })]))

    expect(state.selectedUnitIds.has(1)).toBe(false)
    expect(state.selectedUnitOrder).toEqual([])
  })

  it('also deselects while the build is paused', () => {
    const state = new GameState()
    state.localPlayerId = 'p1'
    state.selectedUnitIds = new Set([1])
    state.selectedUnitOrder = [1]

    state.applySnapshot(makeSnapshot([makeWorker({ status: 'Building (Paused)', visible: false })]))

    expect(state.selectedUnitIds.has(1)).toBe(false)
  })

  it('keeps a worker selected while it is merely moving to the site', () => {
    const state = new GameState()
    state.localPlayerId = 'p1'
    state.selectedUnitIds = new Set([1])
    state.selectedUnitOrder = [1]

    state.applySnapshot(makeSnapshot([makeWorker({ status: 'Moving' })]))

    expect(state.selectedUnitIds.has(1)).toBe(true)
    expect(state.selectedUnitOrder).toEqual([1])
  })
})
