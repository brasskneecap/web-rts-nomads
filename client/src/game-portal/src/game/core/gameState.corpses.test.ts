import { describe, it, expect } from 'vitest'
import { GameState } from './GameState'
import type { CorpseSnapshot } from '../network/protocol'

function corpse(over: Partial<CorpseSnapshot> = {}): CorpseSnapshot {
  return {
    id: 7,
    ownerId: 'p1',
    unitType: 'archer',
    name: 'Archer',
    rank: 'gold',
    progressionPath: 'trapper',
    x: 100,
    y: 100,
    remaining: 12.5,
    ...over,
  }
}

function stateWithCorpses(corpses: CorpseSnapshot[]): GameState {
  const state = new GameState()
  state.localPlayerId = 'p1'
  state.corpses = corpses
  return state
}

describe('corpse selection', () => {
  it('hits a body clicked near its centre and misses one clicked past it', () => {
    const state = stateWithCorpses([corpse()])
    expect(state.getCorpseAtPosition(103, 98)?.id).toBe(7)
    expect(state.getCorpseAtPosition(160, 100)).toBeUndefined()
  })

  it('picks the closest body when two overlap', () => {
    const state = stateWithCorpses([corpse({ id: 1, x: 100 }), corpse({ id: 2, x: 108 })])
    expect(state.getCorpseAtPosition(107, 100)?.id).toBe(2)
  })

  it('selecting a body clears every other selection', () => {
    const state = stateWithCorpses([corpse()])
    state.selectedBuildingId = 'b1'
    state.selectedTrapId = 't1'
    state.selectedZoneId = 'z1'

    state.selectCorpse(7)

    expect(state.selectedCorpseId).toBe(7)
    expect(state.selectedBuildingId).toBeNull()
    expect(state.selectedTrapId).toBeNull()
    expect(state.selectedZoneId).toBeNull()
  })

  // The shared reset inside selectCorpse clears selectedCorpseId too, so the
  // assignment has to come last. Ordering bug, not a hypothetical.
  it('survives its own shared reset', () => {
    const state = stateWithCorpses([corpse()])
    state.selectCorpse(7)
    expect(state.getSelectedCorpse()?.id).toBe(7)
  })

  it('selecting something else drops the body selection', () => {
    const state = stateWithCorpses([corpse()])
    state.selectCorpse(7)
    state.selectTrap('t1')
    expect(state.selectedCorpseId).toBeNull()
  })

  // The panel names WHO died — that is the entire reason a corpse is clickable.
  it('reports the dead unit it represented', () => {
    const state = stateWithCorpses([corpse()])
    state.selectCorpse(7)

    const summary = state.getSelectionSummary()
    expect(summary.kind).toBe('unit')
    if (summary.kind !== 'unit') throw new Error('unreachable')
    expect(summary.title).toBe('Archer')
    expect(summary.pathLabel?.toLowerCase()).toContain('trapper')
    expect(summary.rankLabel?.toLowerCase()).toContain('gold')
    // No orders: a corpse takes none, and it is not in `units` for anything to
    // act on.
    expect(summary.actions).toEqual([])
    expect(summary.details.map((d) => d.label)).toContain('Decays in')
  })

  it('names an enemy body by its owner rather than as your own', () => {
    const state = stateWithCorpses([corpse({ ownerId: 'p2' })])
    state.selectCorpse(7)
    expect(state.getSelectionSummary().subtitle).toContain('p2')
  })

  it('falls back to the unit type when a body carries no name', () => {
    const state = stateWithCorpses([corpse({ name: undefined })])
    state.selectCorpse(7)
    expect(state.getSelectionSummary().title).toBe('archer')
  })
})
