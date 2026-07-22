import { describe, expect, it } from 'vitest'
import type { AbilityProgram } from '@/game/abilities/program/abilityProgram'
import { refFromPath } from './refFromPath'

function program(): AbilityProgram {
  return {
    entry: { type: 'unit', range: 300 },
    triggers: [
      { id: 't1', type: 'on_cast_complete', actions: [{ id: 'a1', type: 'select_targets' }] },
      {
        id: 't2',
        type: 'on_zone_enter',
        actions: [
          { id: 'a2', type: 'deal_damage' },
          { id: 'a3', type: 'restore_health' },
        ],
      },
    ],
  }
}

// meteorProgram: a nested-3-level fixture mirroring programTree.test.ts's
// shared meteorProgram() fixture (cast -> play_presentation -> presentation
// trigger -> create_zone -> config.triggers -> zone-tick trigger), but with
// ONE deliberate difference: the presentation trigger's id ("t_hit") is
// DIFFERENT from its marker string ("impact"). Meteor's own real fixture
// uses "impact" for BOTH, which would let a marker[X] lookup pass by
// accident even if it were implemented as a same-string id search instead of
// a genuine `timing.marker` search. Keeping them distinct here means the
// "marker[impact].actions[sel]" test below can only pass if the marker[X]
// branch actually searches `timing.marker`, not `id`.
function meteorProgram(): AbilityProgram {
  return {
    entry: { type: 'ground_point', range: 600 },
    triggers: [
      {
        id: 'cast',
        type: 'on_cast_complete',
        actions: [{ id: 'meteor', type: 'play_presentation', config: { presentationId: 'p_meteor' } }],
      },
    ],
    presentations: [
      {
        id: 'p_meteor',
        asset: 'fx/meteor',
        position: { key: 'impactPosition' },
        triggers: [
          {
            id: 't_hit',
            type: 'on_animation_marker',
            timing: { marker: 'impact' },
            actions: [
              { id: 'sel', type: 'select_targets' },
              { id: 'dmg', type: 'deal_damage', config: { amount: 50 } },
              {
                id: 'zone',
                type: 'create_zone',
                config: {
                  radius: 200,
                  duration: 5,
                  triggers: [
                    {
                      id: 'burn',
                      type: 'on_tick',
                      timing: { tickInterval: 1000 },
                      actions: [
                        { id: 'bsel', type: 'select_targets' },
                        { id: 'bdmg', type: 'deal_damage', config: { amount: 5 } },
                      ],
                    },
                  ],
                },
              },
            ],
          },
        ],
      },
    ],
  }
}

describe('refFromPath', () => {
  describe('index grammar (validateAbilityProgram / flow view paths)', () => {
    it('resolves a trigger-only path to the trigger at that index', () => {
      expect(refFromPath(program(), 'triggers[1]')).toEqual({
        kind: 'trigger',
        path: [{ kind: 'trigger', id: 't2' }],
      })
    })

    it('resolves a trigger.action path to the action at that index', () => {
      expect(refFromPath(program(), 'triggers[1].actions[1]')).toEqual({
        kind: 'action',
        path: [{ kind: 'trigger', id: 't2' }, { kind: 'action', id: 'a3' }],
      })
    })

    it('returns null for an out-of-range trigger index', () => {
      expect(refFromPath(program(), 'triggers[5]')).toBeNull()
    })

    it('returns null for an out-of-range action index', () => {
      expect(refFromPath(program(), 'triggers[0].actions[9]')).toBeNull()
    })

    it('resolves the root trigger.action path on the meteor fixture', () => {
      expect(refFromPath(meteorProgram(), 'triggers[0].actions[0]')).toEqual({
        kind: 'action',
        path: [{ kind: 'trigger', id: 'cast' }, { kind: 'action', id: 'meteor' }],
      })
    })

    it('resolves presentations[i].triggers[j]', () => {
      expect(refFromPath(meteorProgram(), 'presentations[0].triggers[0]')).toEqual({
        kind: 'trigger',
        path: [{ kind: 'presentation', id: 'p_meteor' }, { kind: 'trigger', id: 't_hit' }],
      })
    })

    it('resolves presentations[0].triggers[0].actions[2] (create_zone action)', () => {
      expect(refFromPath(meteorProgram(), 'presentations[0].triggers[0].actions[2]')).toEqual({
        kind: 'action',
        path: [
          { kind: 'presentation', id: 'p_meteor' },
          { kind: 'trigger', id: 't_hit' },
          { kind: 'action', id: 'zone' },
        ],
      })
    })

    it('resolves a nested trigger under .config.triggers[k]', () => {
      expect(refFromPath(meteorProgram(), 'presentations[0].triggers[0].actions[2].config.triggers[0]')).toEqual({
        kind: 'trigger',
        path: [
          { kind: 'presentation', id: 'p_meteor' },
          { kind: 'trigger', id: 't_hit' },
          { kind: 'action', id: 'zone' },
          { kind: 'trigger', id: 'burn' },
        ],
      })
    })

    it('resolves a nested action under .config.triggers[k].actions[l]', () => {
      expect(
        refFromPath(meteorProgram(), 'presentations[0].triggers[0].actions[2].config.triggers[0].actions[1]'),
      ).toEqual({
        kind: 'action',
        path: [
          { kind: 'presentation', id: 'p_meteor' },
          { kind: 'trigger', id: 't_hit' },
          { kind: 'action', id: 'zone' },
          { kind: 'trigger', id: 'burn' },
          { kind: 'action', id: 'bdmg' },
        ],
      })
    })

    it('resolves a nested trigger under .children[k] and its nested action', () => {
      const prog: AbilityProgram = {
        entry: { type: 'self', range: 0 },
        triggers: [
          {
            id: 'r',
            type: 'on_cast_complete',
            actions: [
              {
                id: 'act',
                type: 'play_sound',
                children: [
                  {
                    id: 'onDone',
                    type: 'on_action_complete',
                    actions: [{ id: 'donefx', type: 'play_sound' }],
                  },
                ],
              },
            ],
          },
        ],
      }
      expect(refFromPath(prog, 'triggers[0].actions[0].children[0]')).toEqual({
        kind: 'trigger',
        path: [{ kind: 'trigger', id: 'r' }, { kind: 'action', id: 'act' }, { kind: 'trigger', id: 'onDone' }],
      })
      expect(refFromPath(prog, 'triggers[0].actions[0].children[0].actions[0]')).toEqual({
        kind: 'action',
        path: [
          { kind: 'trigger', id: 'r' },
          { kind: 'action', id: 'act' },
          { kind: 'trigger', id: 'onDone' },
          { kind: 'action', id: 'donefx' },
        ],
      })
    })

    it('returns null for a trigger directly followed by .children (no .actions in between)', () => {
      expect(refFromPath(program(), 'triggers[0].children[0]')).toBeNull()
    })

    // .body[k] (a loop's nested action list) / .then[k] / .else[k] (a
    // conditional's two nested action lists) all resolve to a nested ACTION,
    // not a trigger — so, unlike .children[k]/.config.triggers[k], the walk
    // can continue with another ".actions[..]" or nested-list step straight
    // after one of these.
    it('resolves a loop action under .body[k]', () => {
      const prog: AbilityProgram = {
        entry: { type: 'self', range: 0 },
        triggers: [
          {
            id: 'r',
            type: 'on_cast_complete',
            actions: [{ id: 'lp', type: 'loop', config: { iterations: 3, body: [{ id: 'b1', type: 'wait' }] } }],
          },
        ],
      }
      expect(refFromPath(prog, 'triggers[0].actions[0].body[0]')).toEqual({
        kind: 'action',
        path: [{ kind: 'trigger', id: 'r' }, { kind: 'action', id: 'lp' }, { kind: 'action', id: 'b1' }],
      })
    })

    it('resolves a conditional action under .then[k] and .else[k]', () => {
      const prog: AbilityProgram = {
        entry: { type: 'self', range: 0 },
        triggers: [
          {
            id: 'r',
            type: 'on_cast_complete',
            actions: [
              {
                id: 'cond',
                type: 'conditional',
                config: {
                  conditions: [{ op: 'has_perk', right: 'lasting_flames' }],
                  then: [{ id: 'burn', type: 'apply_status_duration' }],
                  else: [{ id: 'dmg', type: 'deal_damage' }],
                },
              },
            ],
          },
        ],
      }
      expect(refFromPath(prog, 'triggers[0].actions[0].then[0]')).toEqual({
        kind: 'action',
        path: [{ kind: 'trigger', id: 'r' }, { kind: 'action', id: 'cond' }, { kind: 'action', id: 'burn' }],
      })
      expect(refFromPath(prog, 'triggers[0].actions[0].else[0]')).toEqual({
        kind: 'action',
        path: [{ kind: 'trigger', id: 'r' }, { kind: 'action', id: 'cond' }, { kind: 'action', id: 'dmg' }],
      })
    })
  })

  describe('id grammar (RunAbilityPreview execution trace paths)', () => {
    it('resolves a bare trigger id to the trigger with that id', () => {
      expect(refFromPath(program(), 't1')).toEqual({
        kind: 'trigger',
        path: [{ kind: 'trigger', id: 't1' }],
      })
    })

    it('resolves a "<triggerId>.actions[<actionId>]" path to the action with that id', () => {
      expect(refFromPath(program(), 't1.actions[a1]')).toEqual({
        kind: 'action',
        path: [{ kind: 'trigger', id: 't1' }, { kind: 'action', id: 'a1' }],
      })
    })

    it('returns null for a trigger id that does not exist in the program', () => {
      expect(refFromPath(program(), 't9')).toBeNull()
    })

    it('returns null for an action id that does not exist under an otherwise-valid trigger', () => {
      expect(refFromPath(program(), 't1.actions[zzz]')).toBeNull()
    })

    it('resolves "burn.actions[bdmg]" (a zone-tick root with no ancestry) to the full nested path', () => {
      expect(refFromPath(meteorProgram(), 'burn.actions[bdmg]')).toEqual({
        kind: 'action',
        path: [
          { kind: 'presentation', id: 'p_meteor' },
          { kind: 'trigger', id: 't_hit' },
          { kind: 'action', id: 'zone' },
          { kind: 'trigger', id: 'burn' },
          { kind: 'action', id: 'bdmg' },
        ],
      })
    })

    it('resolves a bare nested trigger id root ("burn") to its full nested path', () => {
      expect(refFromPath(meteorProgram(), 'burn')).toEqual({
        kind: 'trigger',
        path: [
          { kind: 'presentation', id: 'p_meteor' },
          { kind: 'trigger', id: 't_hit' },
          { kind: 'action', id: 'zone' },
          { kind: 'trigger', id: 'burn' },
        ],
      })
    })

    it('resolves "marker[impact].actions[sel]" by matching timing.marker, not the trigger id', () => {
      expect(refFromPath(meteorProgram(), 'marker[impact].actions[sel]')).toEqual({
        kind: 'action',
        path: [
          { kind: 'presentation', id: 'p_meteor' },
          { kind: 'trigger', id: 't_hit' },
          { kind: 'action', id: 'sel' },
        ],
      })
    })

    it('resolves a bare "marker[impact]" root to the trigger whose timing.marker matches', () => {
      expect(refFromPath(meteorProgram(), 'marker[impact]')).toEqual({
        kind: 'trigger',
        path: [{ kind: 'presentation', id: 'p_meteor' }, { kind: 'trigger', id: 't_hit' }],
      })
    })

    it('returns null for a marker string with no matching trigger', () => {
      expect(refFromPath(meteorProgram(), 'marker[nope]')).toBeNull()
      expect(refFromPath(meteorProgram(), 'marker[nope].actions[sel]')).toBeNull()
    })

    it('returns null for a marker match with no such action id', () => {
      expect(refFromPath(meteorProgram(), 'marker[impact].actions[zzz]')).toBeNull()
    })

    it('returns null for the literal conditional.then root (unaddressable, not a bug)', () => {
      expect(refFromPath(program(), 'conditional.then')).toBeNull()
      expect(refFromPath(program(), 'conditional.then.actions[x]')).toBeNull()
    })

    it('returns null for the literal repeat root (unaddressable, not a bug)', () => {
      expect(refFromPath(program(), 'repeat')).toBeNull()
      expect(refFromPath(program(), 'repeat.actions[x]')).toBeNull()
    })

    it('returns null for the literal namedTrigger[<id>] root (namedTriggers authoring is out of scope)', () => {
      expect(refFromPath(program(), 'namedTrigger[foo]')).toBeNull()
      expect(refFromPath(program(), 'namedTrigger[foo].actions[x]')).toBeNull()
    })
  })

  it('returns null for an unrecognized path shape', () => {
    expect(refFromPath(program(), 'identity.category')).toBeNull()
    expect(refFromPath(program(), 'namedTriggers[foo]')).toBeNull()
    expect(refFromPath(program(), 'triggers[0].children[0]')).toBeNull()
  })

  it('returns null for an empty path', () => {
    expect(refFromPath(program(), '')).toBeNull()
  })

  it('never throws on garbage input', () => {
    expect(() => refFromPath(program(), '][invalid')).not.toThrow()
    expect(refFromPath(program(), '][invalid')).toBeNull()
    expect(() => refFromPath(program(), 'triggers[abc]')).not.toThrow()
    expect(refFromPath(program(), 'triggers[abc]')).toBeNull()
    expect(() => refFromPath(program(), '...')).not.toThrow()
    expect(refFromPath(program(), '...')).toBeNull()
  })
})
