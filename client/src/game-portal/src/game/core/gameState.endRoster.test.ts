// Regression test: frozenEndPlayers is captured from the FIRST game-over
// snapshot and must not be clobbered by subsequent snapshots that have a
// smaller roster (e.g. the host dropped out of the server's player list
// during connection teardown).

import { describe, expect, it } from 'vitest'
import { GameState } from './GameState'
import type { MatchSnapshotMessage, PlayerSnapshot } from '../network/protocol'

// Minimal PlayerSnapshot stub — only fields applyPlayerSnapshots reads
// unconditionally (playerId, color, teamId, resources).
function makePlayer(id: string): PlayerSnapshot {
  return {
    playerId: id,
    color: '#ffffff',
    teamId: 0,
    resources: [],
  }
}

// Minimal MatchSnapshotMessage. The required non-optional fields on the wire
// type are: type, tick, serverNow, matchId, buildings, players, units, wave.
// Everything else is optional and can be omitted.
function makeSnapshot(
  players: PlayerSnapshot[],
  overrides: Partial<MatchSnapshotMessage> = {},
): MatchSnapshotMessage {
  return {
    type: 'match_snapshot',
    tick: 1,
    serverNow: 0,
    matchId: 'test-match',
    buildings: [],
    players,
    units: [],
    wave: {
      enabled: false,
      currentWave: 0,
      totalWaves: 0,
      state: '',
      timer: 0,
      waveDuration: 0,
    },
    ...overrides,
  }
}

describe('GameState — frozenEndPlayers', () => {
  it('freezes both players from the first game-over snapshot and ignores subsequent roster shrinkage', () => {
    const state = new GameState()
    // Set localPlayerId so applyPlayerSnapshots can resolve the local player
    // without returning early on the resource/upgrade copy paths. The local
    // player row just needs playerId to match.
    state.localPlayerId = 'p_joiner'

    const pHost = makePlayer('p_host')
    const pJoiner = makePlayer('p_joiner')

    // First snapshot: game over, both players still present.
    const snap1 = makeSnapshot([pHost, pJoiner], {
      gameOver: { lostPlayerIds: ['p_host'], yourDominionPointsEarned: 4 },
    })
    state.applySnapshot(snap1)

    // Second snapshot: host has dropped from the roster (connection teardown),
    // but the same gameOver payload is still present.
    const snap2 = makeSnapshot([pJoiner], {
      gameOver: { lostPlayerIds: ['p_host'], yourDominionPointsEarned: 4 },
    })
    state.applySnapshot(snap2)

    // The frozen roster must reflect the FIRST snapshot — both players.
    expect(state.frozenEndPlayers).not.toBeNull()
    expect(state.frozenEndPlayers).toHaveLength(2)
    const frozenIds = state.frozenEndPlayers!.map((p) => p.playerId).sort()
    expect(frozenIds).toEqual(['p_host', 'p_joiner'])

    // Earned DP comes from the first game-over snapshot.
    expect(state.matchDominionPointsEarned).toBe(4)
  })

  it('freezes via victory snapshot when gameOver is absent', () => {
    const state = new GameState()
    state.localPlayerId = 'p_joiner'

    const pHost = makePlayer('p_host')
    const pJoiner = makePlayer('p_joiner')

    const snap = makeSnapshot([pHost, pJoiner], {
      victory: {
        achieved: true,
        objectives: [],
      },
    })
    state.applySnapshot(snap)

    expect(state.frozenEndPlayers).not.toBeNull()
    expect(state.frozenEndPlayers).toHaveLength(2)
    // No gameOver payload → earned DP defaults to 0.
    expect(state.matchDominionPointsEarned).toBe(0)
  })

  it('does not freeze while the match is still in progress', () => {
    const state = new GameState()
    state.localPlayerId = 'p_joiner'

    const snap = makeSnapshot([makePlayer('p_host'), makePlayer('p_joiner')])
    // No gameOver or victory — mid-game snapshot.
    state.applySnapshot(snap)

    expect(state.frozenEndPlayers).toBeNull()
    expect(state.matchDominionPointsEarned).toBe(0)
  })
})
