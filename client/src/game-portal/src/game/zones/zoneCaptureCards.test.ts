import { describe, it, expect } from 'vitest'
import { buildZoneCaptureCards, type ZoneCaptureCardInput } from './zoneCaptureCards'
import type { Zone, ZoneSnapshot } from '../network/protocol'
import { ZONE_TEAM_OWNER } from '../network/protocol'

const CELL = 64

function zone(partial: Partial<Zone> & { id: string }): Zone {
  return {
    anchor: { x: 0, y: 0 },
    cells: [],
    capture: { type: 'presence' },
    ...partial,
  } as Zone
}

function unitAt(cx: number, cy: number, ownerId = 'p1') {
  return { x: (cx + 0.5) * CELL, y: (cy + 0.5) * CELL, ownerId }
}

function baseInput(over: Partial<ZoneCaptureCardInput>): ZoneCaptureCardInput {
  return {
    zones: [],
    snapshotsById: new Map(),
    units: [],
    buildings: [],
    cellSize: CELL,
    isFriendlyOwner: (o) => o === 'p1',
    isHostileOwner: (o) => o === '__enemy__' || o === '__neutral__',
    ...over,
  }
}

describe('buildZoneCaptureCards', () => {
  it('claim zone: requirement + held/total from snapshot', () => {
    const z = zone({
      id: 'ridge', name: 'North Ridge',
      cells: [[5, 5]], anchor: { x: 5, y: 5 },
      capture: { type: 'claim' }, claimPoints: [[5, 5], [8, 5]],
    })
    const snap: ZoneSnapshot = {
      id: 'ridge', owner: 'neutral', progress: 0.5,
      claimPoints: [{ progress: 1, captured: true }, { progress: 0.5 }],
    }
    const cards = buildZoneCaptureCards(baseInput({
      zones: [z], snapshotsById: new Map([['ridge', snap]]), units: [unitAt(5, 5)],
    }))
    expect(cards).toHaveLength(1)
    expect(cards[0].requirement).toBe('Build & defend 2 towers')
    expect(cards[0].status).toBe('1/2 points held')
    expect(cards[0].progress).toBe(0.5)
  })

  it('presence: capturing / contested / locked states', () => {
    const seed = zone({ id: 'seed', cells: [[0, 0]], capture: { type: 'presence' } })
    const open = zone({ id: 'open', cells: [[5, 5]], capture: { type: 'presence' } })
    const gated = zone({ id: 'gated', cells: [[6, 6]], capture: { type: 'presence' }, adjacent: ['seed'] })
    const snaps = new Map<string, ZoneSnapshot>([
      ['seed', { id: 'seed', owner: 'neutral' }],
      ['open', { id: 'open', owner: 'neutral', progress: 0.5 }],
      ['gated', { id: 'gated', owner: 'neutral', progress: 0 }],
    ])
    const capturing = buildZoneCaptureCards(baseInput({ zones: [open], snapshotsById: snaps, units: [unitAt(5, 5)] }))
    expect(capturing[0].state).toBe('progress')
    expect(capturing[0].status).toBe('Capturing… 50%')

    const contestedSnaps = new Map(snaps)
    contestedSnaps.set('open', { id: 'open', owner: 'neutral', progress: 0.3, contested: true })
    const contested = buildZoneCaptureCards(baseInput({ zones: [open], snapshotsById: contestedSnaps, units: [unitAt(5, 5)] }))
    expect(contested[0].state).toBe('contested')

    const locked = buildZoneCaptureCards(baseInput({ zones: [seed, gated], snapshotsById: snaps, units: [unitAt(6, 6)] }))
    const card = locked.find((c) => c.id === 'gated')!
    expect(card.state).toBe('locked')
  })

  it('clear zone: counts hostile units inside', () => {
    const z = zone({ id: 'camp', cells: [[5, 5], [6, 5]], capture: { type: 'clear' } })
    const snaps = new Map<string, ZoneSnapshot>([['camp', { id: 'camp', owner: 'neutral' }]])
    const cards = buildZoneCaptureCards(baseInput({
      zones: [z], snapshotsById: snaps,
      units: [unitAt(5, 5), unitAt(6, 5, '__enemy__'), unitAt(5, 5, '__neutral__')],
    }))
    expect(cards[0].requirement).toBe('Defeat all enemies in the zone')
    expect(cards[0].status).toBe('2 enemies remain')
  })

  it('skips zones with no friendly units inside, and team-owned zones', () => {
    const z = zone({ id: 'a', cells: [[5, 5]], capture: { type: 'presence' } })
    const owned = zone({ id: 'b', cells: [[7, 7]], capture: { type: 'presence' } })
    const snaps = new Map<string, ZoneSnapshot>([
      ['a', { id: 'a', owner: 'neutral' }],
      ['b', { id: 'b', owner: ZONE_TEAM_OWNER }],
    ])
    const cards = buildZoneCaptureCards(baseInput({
      zones: [z, owned], snapshotsById: snaps, units: [unitAt(7, 7)],
    }))
    expect(cards).toHaveLength(0)
  })
})
