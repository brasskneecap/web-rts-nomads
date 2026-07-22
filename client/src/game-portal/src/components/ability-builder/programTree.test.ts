import { describe, expect, it } from 'vitest'
import type { AbilityActionDef, AbilityProgram, AbilityTriggerDef } from '@/game/abilities/program/abilityProgram'
import {
  collectConditionals,
  addAction,
  addTrigger,
  collectReadContextNames,
  collectSavedContextNames,
  duplicateAction,
  emptyProgram,
  namesSavedByAction,
  findAction,
  findNodePathById,
  findTrigger,
  indexPathFor,
  loopScopeFor,
  moveAction,
  nestedTriggersFor,
  removeAction,
  removeTrigger,
  resolveNode,
  setActionDisabled,
  slotOfNestedTrigger,
  updateAction,
  updateNodeAt,
  updateTrigger,
  type NodePath,
} from './programTree'

// triggerPath / actionPath: NodePath builders for baseProgram()'s root-level
// triggers/actions — every op below takes a NodePath now (the old flat
// triggerId/actionId-string overloads were removed once every caller
// migrated; see the phase-7 plan's Task 5), so a root lookup is a
// single/two-segment path rather than a bare string.
function triggerPath(id: string): NodePath {
  return [{ kind: 'trigger', id }]
}

function actionPath(triggerId: string, actionId: string): NodePath {
  return [{ kind: 'trigger', id: triggerId }, { kind: 'action', id: actionId }]
}

// baseProgram: two triggers, the first with two actions, to exercise
// multi-trigger / multi-action id uniqueness and ordering.
function baseProgram(): AbilityProgram {
  return {
    entry: { type: 'unit', range: 400 },
    triggers: [
      {
        id: 't1',
        type: 'on_cast_complete',
        actions: [
          { id: 'a1', type: 'deal_damage', config: { amount: 10 } },
          { id: 'a2', type: 'apply_status' },
        ],
      },
      {
        id: 't2',
        type: 'on_zone_enter',
        actions: [{ id: 'a3', type: 'play_sound' }],
      },
    ],
  }
}

// meteorProgram: the phase-7 plan's acceptance fixture, nested 3 levels
// deep, used as the shared fixture for every depth-aware resolver test
// below instead of hand-rolling ad-hoc objects per test.
//
//   root trigger `cast` -> action `meteor` (play_presentation,
//     config.presentationId="p_meteor")
//   presentation `p_meteor` -> trigger `impact` (on_animation_marker) ->
//     actions `sel`, `dmg`, `zone`
//   action `zone` (create_zone) -> config.triggers -> trigger `burn`
//     (on_zone_tick) -> actions `bsel`, `bdmg`
//
// `zone` ALSO carries a `children` trigger (`onZoneDone`) alongside its
// `config.triggers` (`burn`) — structurally legal (nothing forbids an action
// from populating both nesting slots at once) and deliberately exercises
// the union-read requirement: a first-match ("children, else config") read
// would silently hide `burn` here.
function meteorProgram(): AbilityProgram {
  return {
    entry: { type: 'ground_point', range: 600 },
    triggers: [
      {
        id: 'cast',
        type: 'on_cast_complete',
        actions: [
          {
            id: 'meteor',
            type: 'play_presentation',
            config: { presentationId: 'p_meteor' },
          },
        ],
      },
    ],
    presentations: [
      {
        id: 'p_meteor',
        asset: 'fx/meteor',
        position: { key: 'impactPosition' },
        triggers: [
          {
            id: 'impact',
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
                children: [
                  {
                    id: 'onZoneDone',
                    type: 'on_action_complete',
                    actions: [{ id: 'zdone1', type: 'play_sound' }],
                  },
                ],
              },
            ],
          },
        ],
      },
    ],
  }
}

describe('emptyProgram', () => {
  it('returns a minimal valid program', () => {
    const prog = emptyProgram()
    expect(prog.entry.type).toBe('no_target')
    expect(prog.triggers).toEqual([])
  })
})

describe('collectSavedContextNames', () => {
  it('collects outputs + store_targets names across every nesting slot, deduped and sorted', () => {
    const prog: AbilityProgram = {
      entry: { type: 'no_target', range: 0 },
      triggers: [
        {
          id: 't1',
          type: 'on_cast_complete',
          actions: [
            // outputs binding
            { id: 'a1', type: 'select_targets', outputs: { targets: 'hit' } },
            // store_targets config.as
            { id: 'a2', type: 'store_targets', config: { as: 'chainHits' } },
            // nested config.triggers (create_zone) with a deeper output
            {
              id: 'a3',
              type: 'create_zone',
              config: {
                triggers: [
                  {
                    id: 'zt',
                    type: 'on_tick',
                    actions: [{ id: 'za', type: 'select_targets', outputs: { targets: 'zoneHit' } }],
                  },
                ],
              },
            },
            // loop body store_targets + a duplicate name (dedup)
            {
              id: 'a4',
              type: 'loop',
              config: { body: [{ id: 'la', type: 'store_targets', config: { as: 'hit' } }] },
            },
          ],
        },
      ],
    }
    expect(collectSavedContextNames(prog)).toEqual(['chainHits', 'hit', 'zoneHit'])
  })

  it('returns an empty list when nothing is saved', () => {
    expect(collectSavedContextNames(baseProgram())).toEqual([])
  })
})

describe('collectReadContextNames', () => {
  it('collects originRef, excludeRef, and input keys across every nesting slot', () => {
    const prog: AbilityProgram = {
      entry: { type: 'no_target', range: 0 },
      triggers: [
        {
          id: 't1',
          type: 'on_cast_complete',
          actions: [
            // source named_context -> originRef, plus excludeRef
            {
              id: 'a1',
              type: 'select_targets',
              target: { source: 'named_context', originRef: { key: 'saved' }, excludeRef: { key: 'struck' } },
            },
            // input ContextRef
            { id: 'a2', type: 'deal_damage', input: { targets: { key: 'hit' } } },
            // nested config.triggers read
            {
              id: 'a3',
              type: 'create_zone',
              config: {
                triggers: [
                  {
                    id: 'zt',
                    type: 'on_tick',
                    actions: [
                      { id: 'za', type: 'select_targets', target: { source: 'named_context', originRef: { key: 'zoneRef' } } },
                    ],
                  },
                ],
              },
            },
          ],
        },
      ],
    }
    expect(collectReadContextNames(prog)).toEqual(['hit', 'saved', 'struck', 'zoneRef'])
  })

  it('returns an empty list when nothing is read by name', () => {
    expect(collectReadContextNames(baseProgram())).toEqual([])
  })
})

describe('namesSavedByAction', () => {
  it('returns outputs destinations and a store_targets as-name', () => {
    expect(namesSavedByAction({ id: 'a', type: 'select_targets', outputs: { targets: 'hit' } })).toEqual(['hit'])
    expect(namesSavedByAction({ id: 'b', type: 'store_targets', config: { as: 'chainHits' } })).toEqual(['chainHits'])
    expect(namesSavedByAction({ id: 'c', type: 'deal_damage' })).toEqual([])
  })
})

describe('addTrigger', () => {
  it('appends a trigger with a unique id and empty actions, without mutating the input', () => {
    const before = baseProgram()
    const snapshot = structuredClone(before)
    const after = addTrigger(before, 'on_cast_start')

    expect(before).toEqual(snapshot)
    expect(after.triggers).toHaveLength(3)
    const added = after.triggers[2]
    expect(added.id).toBe('t3')
    expect(added.type).toBe('on_cast_start')
    expect(added.actions).toEqual([])
  })

  it('keeps trigger ids unique across repeated adds', () => {
    let prog = emptyProgram()
    prog = addTrigger(prog, 'on_cast_start')
    prog = addTrigger(prog, 'on_cast_complete')
    prog = addTrigger(prog, 'on_zone_enter')
    const ids = prog.triggers.map((t) => t.id)
    expect(new Set(ids).size).toBe(ids.length)
    expect(ids).toEqual(['t1', 't2', 't3'])
  })
})

describe('removeTrigger', () => {
  it('removes the matching trigger without mutating the input', () => {
    const before = baseProgram()
    const snapshot = structuredClone(before)
    const after = removeTrigger(before, triggerPath('t1'))

    expect(before).toEqual(snapshot)
    expect(after.triggers.map((t) => t.id)).toEqual(['t2'])
  })

  it('no-ops when the trigger id is not found', () => {
    const before = baseProgram()
    const after = removeTrigger(before, triggerPath('does-not-exist'))
    expect(after.triggers).toHaveLength(2)
  })
})

describe('findTrigger', () => {
  it('finds an existing trigger and returns undefined otherwise', () => {
    const prog = baseProgram()
    expect(findTrigger(prog, 't2')?.type).toBe('on_zone_enter')
    expect(findTrigger(prog, 'nope')).toBeUndefined()
  })
})

describe('addAction', () => {
  it('appends an action to the target trigger with a unique id, without mutating the input', () => {
    const before = baseProgram()
    const snapshot = structuredClone(before)
    const after = addAction(before, triggerPath('t2'), 'restore_health')

    expect(before).toEqual(snapshot)
    const t2 = findTrigger(after, 't2')!
    expect(t2.actions).toHaveLength(2)
    expect(t2.actions[1]).toEqual({ id: 'a4', type: 'restore_health', disabled: false })
    // untouched trigger stays identical
    expect(findTrigger(after, 't1')).toEqual(findTrigger(before, 't1'))
  })

  it('keeps action ids unique across triggers', () => {
    let prog = baseProgram()
    prog = addAction(prog, triggerPath('t1'), 'wait')
    prog = addAction(prog, triggerPath('t2'), 'wait')
    const allIds = prog.triggers.flatMap((t) => t.actions.map((a) => a.id))
    expect(new Set(allIds).size).toBe(allIds.length)
  })

  it('no-ops when the trigger id is not found', () => {
    const before = baseProgram()
    const after = addAction(before, triggerPath('missing'), 'wait')
    expect(after).toEqual(before)
  })
})

describe('removeAction / findAction', () => {
  it('removes the matching action without mutating the input', () => {
    const before = baseProgram()
    const snapshot = structuredClone(before)
    const after = removeAction(before, actionPath('t1', 'a1'))

    expect(before).toEqual(snapshot)
    expect(findTrigger(after, 't1')!.actions.map((a) => a.id)).toEqual(['a2'])
  })

  it('findAction finds an existing action and returns undefined otherwise', () => {
    const prog = baseProgram()
    expect(findAction(prog, 't1', 'a2')?.type).toBe('apply_status')
    expect(findAction(prog, 't1', 'nope')).toBeUndefined()
    expect(findAction(prog, 'missing-trigger', 'a1')).toBeUndefined()
  })
})

describe('moveAction', () => {
  it('swaps with the previous action on "up"', () => {
    const before = baseProgram()
    const snapshot = structuredClone(before)
    const after = moveAction(before, actionPath('t1', 'a2'), 'up')

    expect(before).toEqual(snapshot)
    expect(findTrigger(after, 't1')!.actions.map((a) => a.id)).toEqual(['a2', 'a1'])
  })

  it('swaps with the next action on "down"', () => {
    const after = moveAction(baseProgram(), actionPath('t1', 'a1'), 'down')
    expect(findTrigger(after, 't1')!.actions.map((a) => a.id)).toEqual(['a2', 'a1'])
  })

  it('no-ops at the top bound', () => {
    const before = baseProgram()
    const after = moveAction(before, actionPath('t1', 'a1'), 'up')
    expect(after).toEqual(before)
  })

  it('no-ops at the bottom bound', () => {
    const before = baseProgram()
    const after = moveAction(before, actionPath('t1', 'a2'), 'down')
    expect(after).toEqual(before)
  })
})

describe('duplicateAction', () => {
  it('inserts a deep copy with a new unique id right after the original, without mutating the input', () => {
    const before = baseProgram()
    const snapshot = structuredClone(before)
    const after = duplicateAction(before, actionPath('t1', 'a1'))

    expect(before).toEqual(snapshot)
    const actions = findTrigger(after, 't1')!.actions
    expect(actions.map((a) => a.id)).toEqual(['a1', 'a4', 'a2'])
    expect(actions[1].type).toBe('deal_damage')
    expect(actions[1].config).toEqual({ amount: 10 })

    // deep copy: mutating the duplicate's config must not affect the original
    ;(actions[1].config as { amount: number }).amount = 999
    expect(findAction(before, 't1', 'a1')!.config).toEqual({ amount: 10 })
  })

  it('keeps ids unique across multiple duplicates', () => {
    let prog = baseProgram()
    prog = duplicateAction(prog, actionPath('t1', 'a1'))
    prog = duplicateAction(prog, actionPath('t1', 'a1'))
    const allIds = prog.triggers.flatMap((t) => t.actions.map((a) => a.id))
    expect(new Set(allIds).size).toBe(allIds.length)
  })
})

describe('setActionDisabled', () => {
  it('sets the disabled flag on the matching action without mutating the input', () => {
    const before = baseProgram()
    const snapshot = structuredClone(before)
    const after = setActionDisabled(before, actionPath('t1', 'a1'), true)

    expect(before).toEqual(snapshot)
    expect(findAction(after, 't1', 'a1')!.disabled).toBe(true)
    expect(findAction(after, 't1', 'a2')!.disabled).toBeUndefined()
  })
})

describe('updateAction', () => {
  it('shallow-merges the patch, preserving id/type and replacing config wholesale', () => {
    const before = baseProgram()
    const snapshot = structuredClone(before)
    const after = updateAction(before, actionPath('t1', 'a1'), { config: { amount: 25 }, displayName: 'Big Hit' })

    expect(before).toEqual(snapshot)
    const action = findAction(after, 't1', 'a1')!
    expect(action.id).toBe('a1')
    expect(action.type).toBe('deal_damage')
    expect(action.config).toEqual({ amount: 25 })
    expect(action.displayName).toBe('Big Hit')
  })

  it('sets a target query via patch', () => {
    const after = updateAction(baseProgram(), actionPath('t2', 'a3'), {
      target: { source: 'caster' },
    })
    expect(findAction(after, 't2', 'a3')!.target).toEqual({ source: 'caster' })
  })
})

describe('updateTrigger', () => {
  it('shallow-merges the patch onto the matching trigger without mutating the input', () => {
    const before = baseProgram()
    const snapshot = structuredClone(before)
    const after = updateTrigger(before, triggerPath('t1'), { name: 'On Cast', timing: { delaySeconds: 1 } })

    expect(before).toEqual(snapshot)
    const t1 = findTrigger(after, 't1')!
    expect(t1.name).toBe('On Cast')
    expect(t1.timing).toEqual({ delaySeconds: 1 })
    expect(t1.type).toBe('on_cast_complete')
    expect(t1.actions).toHaveLength(2)
  })
})

// --- Depth-aware path model ------------------------------------------

// Paths into the shared meteorProgram() fixture, one per depth exercised.
const castPath: NodePath = [{ kind: 'trigger', id: 'cast' }]
const meteorActionPath: NodePath = [...castPath, { kind: 'action', id: 'meteor' }]
const impactPath: NodePath = [
  { kind: 'presentation', id: 'p_meteor' },
  { kind: 'trigger', id: 'impact' },
]
const zoneActionPath: NodePath = [...impactPath, { kind: 'action', id: 'zone' }]
const burnPath: NodePath = [...zoneActionPath, { kind: 'trigger', id: 'burn' }]
const bdmgActionPath: NodePath = [...burnPath, { kind: 'action', id: 'bdmg' }]
const onZoneDonePath: NodePath = [...zoneActionPath, { kind: 'trigger', id: 'onZoneDone' }]
const selActionPath: NodePath = [...impactPath, { kind: 'action', id: 'sel' }]
const bselActionPath: NodePath = [...burnPath, { kind: 'action', id: 'bsel' }]
const zdone1ActionPath: NodePath = [...onZoneDonePath, { kind: 'action', id: 'zdone1' }]

// allIds walks the whole program the same way programTree's (unexported)
// collectAllIds does, so tests can assert global id uniqueness without
// reaching into the module's internals.
function allIds(prog: AbilityProgram): string[] {
  const ids: string[] = []
  const walkTrig = (t: AbilityTriggerDef) => {
    ids.push(t.id)
    for (const a of t.actions) walkAct(a)
  }
  const walkAct = (a: AbilityActionDef) => {
    ids.push(a.id)
    for (const child of a.children ?? []) walkTrig(child)
    const raw = (a.config as { triggers?: AbilityTriggerDef[] } | undefined)?.triggers
    if (Array.isArray(raw)) for (const t of raw) walkTrig(t)
  }
  for (const t of prog.triggers) walkTrig(t)
  for (const p of prog.presentations ?? []) {
    ids.push(p.id)
    for (const t of p.triggers ?? []) walkTrig(t)
  }
  return ids
}

// actionAt / triggerAt narrow resolveNode's result to the expected variant
// (throwing if it resolved to the wrong kind or didn't resolve at all), so
// the tests below can assert on the node's own fields without repeating the
// `resolved?.kind === '...'` narrowing dance in every case.
function actionAt(prog: AbilityProgram, path: NodePath): AbilityActionDef {
  const resolved = resolveNode(prog, path)
  if (resolved?.kind !== 'action') throw new Error(`expected an action at ${JSON.stringify(path)}`)
  return resolved.node
}

function triggerAt(prog: AbilityProgram, path: NodePath): AbilityTriggerDef {
  const resolved = resolveNode(prog, path)
  if (resolved?.kind !== 'trigger') throw new Error(`expected a trigger at ${JSON.stringify(path)}`)
  return resolved.node
}

describe('resolveNode', () => {
  it('resolves a root trigger (depth 1)', () => {
    expect(triggerAt(meteorProgram(), castPath)).toEqual(findTrigger(meteorProgram(), 'cast'))
  })

  it('resolves a root action (depth 1 trigger + action)', () => {
    const action = actionAt(meteorProgram(), meteorActionPath)
    expect(action.id).toBe('meteor')
    expect(action.type).toBe('play_presentation')
  })

  it('resolves a presentation-nested trigger (depth 2)', () => {
    const trigger = triggerAt(meteorProgram(), impactPath)
    expect(trigger.id).toBe('impact')
    expect(trigger.type).toBe('on_animation_marker')
  })

  it('resolves an action inside a presentation-nested trigger (depth 2 trigger + action)', () => {
    expect(actionAt(meteorProgram(), zoneActionPath).id).toBe('zone')
  })

  it('resolves a config.triggers-nested trigger three levels deep (depth 3)', () => {
    const trigger = triggerAt(meteorProgram(), burnPath)
    expect(trigger.id).toBe('burn')
    expect(trigger.type).toBe('on_tick')
  })

  it('resolves an action inside the deepest nested trigger (depth 3 trigger + action)', () => {
    const action = actionAt(meteorProgram(), bdmgActionPath)
    expect(action.id).toBe('bdmg')
    expect(action.config).toEqual({ amount: 5 })
  })

  it('resolves a children-nested trigger sitting alongside config.triggers on the same action', () => {
    expect(triggerAt(meteorProgram(), onZoneDonePath).id).toBe('onZoneDone')
  })

  it('returns undefined for an empty path (ability-level has no resolvable node)', () => {
    expect(resolveNode(meteorProgram(), [])).toBeUndefined()
  })

  it('returns undefined, never throws, for a path with an unresolvable id', () => {
    const garbage: NodePath = [{ kind: 'trigger', id: 'does-not-exist' }]
    expect(() => resolveNode(meteorProgram(), garbage)).not.toThrow()
    expect(resolveNode(meteorProgram(), garbage)).toBeUndefined()
  })

  it('returns undefined for a structurally impossible path (action segment at root)', () => {
    const garbage: NodePath = [{ kind: 'action', id: 'meteor' }]
    expect(resolveNode(meteorProgram(), garbage)).toBeUndefined()
  })

  it('returns undefined for a path that dead-ends on a bare presentation segment', () => {
    const garbage: NodePath = [{ kind: 'presentation', id: 'p_meteor' }]
    expect(resolveNode(meteorProgram(), garbage)).toBeUndefined()
  })

  it('returns undefined when a later segment does not belong to its parent', () => {
    // 'burn' lives under 'zone', not directly under 'cast'.
    const garbage: NodePath = [...castPath, { kind: 'trigger', id: 'burn' }]
    expect(resolveNode(meteorProgram(), garbage)).toBeUndefined()
  })
})

describe('nestedTriggersFor', () => {
  it('returns the union of children and config.triggers, not a first-match read', () => {
    const zone = actionAt(meteorProgram(), zoneActionPath)
    expect(nestedTriggersFor(zone).map((t) => t.id)).toEqual(['onZoneDone', 'burn'])
  })

  it('returns an empty array for an action with neither slot populated', () => {
    const plain = findAction(baseProgram(), 't1', 'a1')!
    expect(nestedTriggersFor(plain)).toEqual([])
  })
})

describe('slotOfNestedTrigger', () => {
  it('reports "children" for a trigger nested via action.children', () => {
    const zone = actionAt(meteorProgram(), zoneActionPath)
    expect(slotOfNestedTrigger(zone, 'onZoneDone')).toBe('children')
  })

  it('reports "config" for a trigger nested via config.triggers', () => {
    const zone = actionAt(meteorProgram(), zoneActionPath)
    expect(slotOfNestedTrigger(zone, 'burn')).toBe('config')
  })

  it('reports undefined for an id not nested in either slot', () => {
    const zone = actionAt(meteorProgram(), zoneActionPath)
    expect(slotOfNestedTrigger(zone, 'does-not-exist')).toBeUndefined()
  })
})

describe('indexPathFor', () => {
  it('emits the root trigger grammar', () => {
    expect(indexPathFor(meteorProgram(), castPath)).toBe('triggers[0]')
  })

  it('emits the root action grammar', () => {
    expect(indexPathFor(meteorProgram(), meteorActionPath)).toBe('triggers[0].actions[0]')
  })

  it('emits the presentation-nested trigger grammar', () => {
    expect(indexPathFor(meteorProgram(), impactPath)).toBe('presentations[0].triggers[0]')
  })

  it('emits the grammar for an action nested inside a presentation trigger', () => {
    // sel=0, dmg=1, zone=2
    expect(indexPathFor(meteorProgram(), zoneActionPath)).toBe('presentations[0].triggers[0].actions[2]')
  })

  it('emits the children-nested trigger grammar', () => {
    expect(indexPathFor(meteorProgram(), onZoneDonePath)).toBe(
      'presentations[0].triggers[0].actions[2].children[0]',
    )
  })

  it('emits the config.triggers-nested trigger grammar (matches the Go validator exactly)', () => {
    expect(indexPathFor(meteorProgram(), burnPath)).toBe(
      'presentations[0].triggers[0].actions[2].config.triggers[0]',
    )
  })

  it('emits the grammar for an action nested inside a config.triggers-nested trigger', () => {
    // bsel=0, bdmg=1
    expect(indexPathFor(meteorProgram(), bdmgActionPath)).toBe(
      'presentations[0].triggers[0].actions[2].config.triggers[0].actions[1]',
    )
  })

  it('returns undefined for an unresolvable path, never throws', () => {
    const garbage: NodePath = [{ kind: 'trigger', id: 'does-not-exist' }]
    expect(() => indexPathFor(meteorProgram(), garbage)).not.toThrow()
    expect(indexPathFor(meteorProgram(), garbage)).toBeUndefined()
  })

  it('returns undefined for an empty path', () => {
    expect(indexPathFor(meteorProgram(), [])).toBeUndefined()
  })
})

describe('findNodePathById', () => {
  it('finds a deeply nested action id (config.triggers slot) and returns its full path', () => {
    expect(findNodePathById(meteorProgram(), 'bdmg')).toEqual(bdmgActionPath)
  })

  it('finds a deeply nested trigger id (config.triggers slot) and returns its full path', () => {
    expect(findNodePathById(meteorProgram(), 'burn')).toEqual(burnPath)
  })

  it('finds a children-nested trigger id and returns its full path', () => {
    expect(findNodePathById(meteorProgram(), 'onZoneDone')).toEqual(onZoneDonePath)
  })

  it('finds a root trigger id', () => {
    expect(findNodePathById(meteorProgram(), 'cast')).toEqual(castPath)
  })

  it('finds a root action id', () => {
    expect(findNodePathById(meteorProgram(), 'meteor')).toEqual(meteorActionPath)
  })

  it('returns undefined for an id that does not exist anywhere in the program', () => {
    expect(findNodePathById(meteorProgram(), 'does-not-exist')).toBeUndefined()
  })
})

// --- updateNodeAt: the generic immutable spine rebuild -------------------

describe('updateNodeAt', () => {
  it('replaces a root trigger; the sibling root trigger is passed through by reference', () => {
    const before = baseProgram()
    const snapshot = structuredClone(before)
    const after = updateNodeAt(before, [{ kind: 'trigger', id: 't1' }], (t: AbilityTriggerDef): AbilityTriggerDef => ({
      ...t,
      name: 'Renamed',
    }))

    expect(before).toEqual(snapshot)
    expect(findTrigger(after, 't1')!.name).toBe('Renamed')
    expect(after.triggers[1]).toBe(before.triggers[1]) // t2: untouched branch, same reference
  })

  it('replaces an action nested 3 levels deep in config.triggers, preserving unrelated config keys', () => {
    const before = meteorProgram()
    const after = updateNodeAt(before, bdmgActionPath, (a: AbilityActionDef): AbilityActionDef => ({
      ...a,
      config: { amount: 42 },
    }))

    expect(actionAt(after, bdmgActionPath).config).toEqual({ amount: 42 })
    // sibling under the same nested trigger is untouched, same reference
    expect(actionAt(after, bselActionPath)).toBe(actionAt(before, bselActionPath))
    // the unrelated root branch (cast -> meteor) is untouched, same reference
    expect(after.triggers[0]).toBe(before.triggers[0])
    // input never mutated
    expect(actionAt(before, bdmgActionPath).config).toEqual({ amount: 5 })
  })

  it('replaces a children-nested trigger, leaving the config.triggers sibling slot on the same action untouched', () => {
    const before = meteorProgram()
    const after = updateNodeAt(before, onZoneDonePath, (t: AbilityTriggerDef): AbilityTriggerDef => ({
      ...t,
      name: 'Done',
    }))

    expect(triggerAt(after, onZoneDonePath).name).toBe('Done')
    expect(triggerAt(after, burnPath)).toBe(triggerAt(before, burnPath))
  })

  it('returns the program unchanged, never throwing, for an unresolvable path', () => {
    const before = meteorProgram()
    const garbage: NodePath = [{ kind: 'trigger', id: 'does-not-exist' }]
    expect(() => updateNodeAt(before, garbage, (t: AbilityTriggerDef): AbilityTriggerDef => t)).not.toThrow()
    expect(updateNodeAt(before, garbage, (t: AbilityTriggerDef): AbilityTriggerDef => t)).toBe(before)
  })

  it('returns the program unchanged for an empty path', () => {
    const before = meteorProgram()
    expect(updateNodeAt(before, [], (t: AbilityTriggerDef): AbilityTriggerDef => t)).toBe(before)
  })
})

// --- Depth-aware mutation ops ---------------------------------------------

describe('addTrigger (depth-aware)', () => {
  it('nests under config.triggers for a create_zone parent action, appended after the existing config trigger', () => {
    const before = meteorProgram()
    const snapshot = structuredClone(before)
    const after = addTrigger(before, zoneActionPath, 'on_tick')

    expect(before).toEqual(snapshot)
    const zone = actionAt(after, zoneActionPath)
    expect(nestedTriggersFor(zone).map((t) => t.id)).toEqual(['onZoneDone', 'burn', 't1'])
    expect(slotOfNestedTrigger(zone, 't1')).toBe('config')
    // unrelated config keys on the opaque bag survive
    expect((zone.config as { radius?: number }).radius).toBe(200)
    expect((zone.config as { duration?: number }).duration).toBe(5)
  })

  it('nests under children for a non-create_zone parent action', () => {
    const after = addTrigger(meteorProgram(), meteorActionPath, 'on_action_complete')
    const meteor = actionAt(after, meteorActionPath)
    expect(nestedTriggersFor(meteor).map((t) => t.id)).toEqual(['t1'])
    expect(slotOfNestedTrigger(meteor, 't1')).toBe('children')
  })

  it('is a no-op for an unresolvable parent action path', () => {
    const before = meteorProgram()
    const garbage: NodePath = [...impactPath, { kind: 'action', id: 'does-not-exist' }]
    expect(addTrigger(before, garbage, 'on_tick')).toEqual(before)
  })
})

describe('removeTrigger (depth-aware)', () => {
  it('removes a config.triggers-nested trigger without mutating the input; unrelated config keys survive', () => {
    const before = meteorProgram()
    const snapshot = structuredClone(before)
    const after = removeTrigger(before, burnPath)

    expect(before).toEqual(snapshot)
    const zone = actionAt(after, zoneActionPath)
    expect(nestedTriggersFor(zone).map((t) => t.id)).toEqual(['onZoneDone'])
    expect((zone.config as { radius?: number }).radius).toBe(200)
    expect((zone.config as { duration?: number }).duration).toBe(5)
  })

  it('removes a children-nested trigger without mutating the input', () => {
    const before = meteorProgram()
    const snapshot = structuredClone(before)
    const after = removeTrigger(before, onZoneDonePath)

    expect(before).toEqual(snapshot)
    const zone = actionAt(after, zoneActionPath)
    expect(nestedTriggersFor(zone).map((t) => t.id)).toEqual(['burn'])
  })

  it('removes a presentation-nested root trigger', () => {
    const after = removeTrigger(meteorProgram(), impactPath)
    expect(after.presentations![0].triggers).toEqual([])
  })

  it('leaves untouched branches referenced, not cloned', () => {
    const before = meteorProgram()
    const after = removeTrigger(before, burnPath)
    expect(after.triggers[0]).toBe(before.triggers[0]) // cast -> meteor branch untouched
    expect(actionAt(after, selActionPath)).toBe(actionAt(before, selActionPath))
    // onZoneDone (children slot, sibling of the removed config.triggers slot) untouched
    expect(actionAt(after, zdone1ActionPath)).toBe(actionAt(before, zdone1ActionPath))
  })

  it('is a no-op, never throwing, for an unresolvable path', () => {
    const before = meteorProgram()
    const garbage: NodePath = [{ kind: 'trigger', id: 'does-not-exist' }]
    expect(() => removeTrigger(before, garbage)).not.toThrow()
    expect(removeTrigger(before, garbage)).toEqual(before)
  })
})

describe('addAction (depth-aware)', () => {
  it('appends to a trigger nested 3 levels deep (config.triggers) without mutating the input', () => {
    const before = meteorProgram()
    const snapshot = structuredClone(before)
    const after = addAction(before, burnPath, 'wait')

    expect(before).toEqual(snapshot)
    expect(triggerAt(after, burnPath).actions.map((a) => a.id)).toEqual(['bsel', 'bdmg', 'a1'])
  })

  it('is a no-op for an unresolvable trigger path', () => {
    const before = meteorProgram()
    const garbage: NodePath = [{ kind: 'trigger', id: 'does-not-exist' }]
    expect(addAction(before, garbage, 'wait')).toEqual(before)
  })

  it('is a no-op when the path resolves to an action rather than a trigger', () => {
    const before = meteorProgram()
    expect(addAction(before, zoneActionPath, 'wait')).toEqual(before)
  })
})

describe('removeAction (depth-aware)', () => {
  it('removes an action nested 3 levels deep without mutating the input', () => {
    const before = meteorProgram()
    const snapshot = structuredClone(before)
    const after = removeAction(before, bselActionPath)

    expect(before).toEqual(snapshot)
    expect(triggerAt(after, burnPath).actions.map((a) => a.id)).toEqual(['bdmg'])
  })

  it('is a no-op for an unresolvable action path', () => {
    const before = meteorProgram()
    const garbage: NodePath = [...burnPath, { kind: 'action', id: 'does-not-exist' }]
    expect(removeAction(before, garbage)).toEqual(before)
  })
})

describe('moveAction (depth-aware)', () => {
  it('swaps two actions nested 3 levels deep without mutating the input', () => {
    const before = meteorProgram()
    const snapshot = structuredClone(before)
    const after = moveAction(before, bdmgActionPath, 'up')

    expect(before).toEqual(snapshot)
    expect(triggerAt(after, burnPath).actions.map((a) => a.id)).toEqual(['bdmg', 'bsel'])
  })

  it('no-ops at the top bound at depth 3', () => {
    const before = meteorProgram()
    expect(moveAction(before, bselActionPath, 'up')).toEqual(before)
  })
})

describe('duplicateAction (depth-aware)', () => {
  it('duplicates a leaf action nested 3 levels deep, minting one fresh id, leaving its sibling untouched', () => {
    const before = meteorProgram()
    const snapshot = structuredClone(before)
    const after = duplicateAction(before, bdmgActionPath)

    expect(before).toEqual(snapshot)
    const burn = triggerAt(after, burnPath)
    expect(burn.actions.map((a) => a.id)).toEqual(['bsel', 'bdmg', 'a1'])
    expect(burn.actions[0]).toBe(triggerAt(before, burnPath).actions[0]) // bsel untouched, same reference
  })

  it('duplicating a create_zone action mints FRESH ids for its entire nested subtree, in both slots', () => {
    const before = meteorProgram()
    const snapshot = structuredClone(before)
    const after = duplicateAction(before, zoneActionPath)

    expect(before).toEqual(snapshot)

    const impact = triggerAt(after, impactPath)
    expect(impact.actions).toHaveLength(4)
    const clone = impact.actions[3]
    expect(clone.id).not.toBe('zone')
    expect(clone.type).toBe('create_zone')

    const cloneChildrenIds = (clone.children ?? []).map((t) => t.id)
    const cloneConfigTriggerIds = ((clone.config as { triggers?: AbilityTriggerDef[] } | undefined)?.triggers ?? []).map(
      (t) => t.id,
    )
    // the clone's nested ids must NOT reuse the original subtree's ids
    expect(cloneChildrenIds).not.toContain('onZoneDone')
    expect(cloneConfigTriggerIds).not.toContain('burn')
    expect(cloneChildrenIds[0]).not.toBe(cloneConfigTriggerIds[0])

    // no id anywhere in the resulting program is duplicated
    const ids = allIds(after)
    expect(new Set(ids).size).toBe(ids.length)

    // the original zone action's own nested subtree is untouched
    const originalZone = actionAt(after, zoneActionPath)
    expect(originalZone.children).toEqual(actionAt(before, zoneActionPath).children)
  })

  it('is a no-op for an unresolvable action path', () => {
    const before = meteorProgram()
    const garbage: NodePath = [...burnPath, { kind: 'action', id: 'does-not-exist' }]
    expect(duplicateAction(before, garbage)).toEqual(before)
  })
})

describe('setActionDisabled (depth-aware)', () => {
  it('disables an action nested 3 levels deep without mutating the input, leaving its sibling enabled', () => {
    const before = meteorProgram()
    const snapshot = structuredClone(before)
    const after = setActionDisabled(before, bdmgActionPath, true)

    expect(before).toEqual(snapshot)
    expect(actionAt(after, bdmgActionPath).disabled).toBe(true)
    expect(actionAt(after, bselActionPath).disabled).toBeUndefined()
  })

  it('is a no-op when the path resolves to a trigger rather than an action', () => {
    const before = meteorProgram()
    expect(setActionDisabled(before, burnPath, true)).toEqual(before)
  })
})

describe('updateAction (depth-aware)', () => {
  it("edits the crater DoT's damage 3 levels deep without mutating the input", () => {
    const before = meteorProgram()
    const snapshot = structuredClone(before)
    const after = updateAction(before, bdmgActionPath, { config: { amount: 25 } })

    expect(before).toEqual(snapshot)
    expect(actionAt(after, bdmgActionPath).config).toEqual({ amount: 25 })
    expect(actionAt(after, bselActionPath)).toBe(actionAt(before, bselActionPath))
    expect(after.triggers[0]).toBe(before.triggers[0])
  })

  it('is a no-op when the path resolves to a trigger rather than an action', () => {
    const before = meteorProgram()
    expect(updateAction(before, burnPath, { displayName: 'x' })).toEqual(before)
  })
})

describe('updateTrigger (depth-aware)', () => {
  it('edits the crater DoT trigger 3 levels deep without mutating the input', () => {
    const before = meteorProgram()
    const snapshot = structuredClone(before)
    const after = updateTrigger(before, burnPath, { name: 'Crater Burn' })

    expect(before).toEqual(snapshot)
    expect(triggerAt(after, burnPath).name).toBe('Crater Burn')
    expect(triggerAt(after, burnPath).actions).toHaveLength(2)
  })

  it('is a no-op when the path resolves to an action rather than a trigger', () => {
    const before = meteorProgram()
    expect(updateTrigger(before, bdmgActionPath, { name: 'x' })).toEqual(before)
  })
})

describe('id minting scans config.triggers, not just children', () => {
  it('never mints an id already used by a config.triggers-nested node', () => {
    // Deliberately shaped so the OLD (children-only) id scan would compute
    // "a2" as the next free id — a1 is the only id it would see — even
    // though a2 already names a nested action inside a1's config.triggers.
    const prog: AbilityProgram = {
      entry: { type: 'no_target', range: 0 },
      triggers: [
        {
          id: 't1',
          type: 'on_cast_complete',
          actions: [
            {
              id: 'a1',
              type: 'create_zone',
              config: {
                triggers: [{ id: 't9', type: 'on_tick', actions: [{ id: 'a2', type: 'select_targets' }] }],
              },
            },
          ],
        },
      ],
    }

    const after = addAction(prog, triggerPath('t1'), 'wait')
    const newAction = findTrigger(after, 't1')!.actions[1]
    expect(newAction.id).not.toBe('a2')
    expect(newAction.id).toBe('a3')
  })
})

describe('addTrigger — nested slot rule', () => {
  // The slot decides WHEN a trigger fires, and the wrong one fails SILENTLY:
  // `children` fires as on_action_complete (immediately after the action's
  // Execute returns), while `config.triggers` is decoded by the action's own
  // Go descriptor and fired by the object it creates. Put an
  // on_projectile_impact trigger in `children` and it fires when the bolt is
  // LAUNCHED, not when it lands — and the Go decoder never sees it at all.
  // Mirrors the four Go configs that decode a Triggers field (beam covers both
  // its momentary on_beam_impact and channeled on_beam_tick shapes).
  const CONFIG_SLOT: [string, string][] = [
    ['create_zone', 'on_tick'],
    ['launch_projectile', 'on_projectile_impact'],
    ['beam', 'on_beam_impact'],
    ['beam', 'on_tick'],
    ['apply_status_duration', 'on_action_complete'],
  ]

  it.each(CONFIG_SLOT)('%s nests a new trigger into config.triggers, not children', (actionType, triggerType) => {
    let prog = emptyProgram()
    prog = addTrigger(prog, 'on_cast_complete')
    const tPath: NodePath = [{ kind: 'trigger', id: 't1' }]
    prog = addAction(prog, tPath, actionType)
    const aPath: NodePath = [...tPath, { kind: 'action', id: 'a1' }]

    prog = addTrigger(prog, aPath, triggerType)

    const action = resolveNode(prog, aPath)
    if (!action || action.kind !== 'action') throw new Error('action did not resolve')
    const cfgTriggers = (action.node.config?.triggers ?? []) as { type: string }[]
    expect(cfgTriggers.map((t) => t.type)).toEqual([triggerType])
    expect(action.node.children ?? []).toEqual([])
  })

  it('an action with no config-trigger slot nests into children', () => {
    let prog = emptyProgram()
    prog = addTrigger(prog, 'on_cast_complete')
    const tPath: NodePath = [{ kind: 'trigger', id: 't1' }]
    prog = addAction(prog, tPath, 'deal_damage')
    const aPath: NodePath = [...tPath, { kind: 'action', id: 'a1' }]

    prog = addTrigger(prog, aPath, 'on_action_complete')

    const action = resolveNode(prog, aPath)
    if (!action || action.kind !== 'action') throw new Error('action did not resolve')
    expect((action.node.children ?? []).map((t) => t.type)).toEqual(['on_action_complete'])
    expect(action.node.config?.triggers).toBeUndefined()
  })
})

// ── loop body: actions nested in a `loop` action's config.body ──────────────
//
// A loop action owns a list of ACTIONS (config.body), addressed by extending
// its path with an `{kind:'action', id}` segment. These verify the traversal
// and every list-mutating op works into that body exactly like a trigger's
// own actions.
function loopProgram(): AbilityProgram {
  return {
    entry: { type: 'unit', range: 300 },
    triggers: [
      {
        id: 'cast',
        type: 'on_cast_complete',
        actions: [
          {
            id: 'lp',
            type: 'loop',
            config: {
              iterations: 3,
              vars: [{ name: 'a', start: 60, step: -5 }],
              body: [
                { id: 'b1', type: 'select_targets', target: { source: 'all_in_scene' } },
                { id: 'b2', type: 'deal_damage', config: { amount: 'a' } },
                { id: 'b3', type: 'wait', config: { seconds: 0.1 } },
              ],
            },
          },
        ],
      },
    ],
  }
}

function loopBodyPath(bodyId: string): NodePath {
  return [{ kind: 'trigger', id: 'cast' }, { kind: 'action', id: 'lp' }, { kind: 'action', id: bodyId }]
}

describe('loop body traversal', () => {
  it('resolveNode reaches an action inside config.body', () => {
    const node = resolveNode(loopProgram(), loopBodyPath('b2'))
    expect(node?.kind).toBe('action')
    expect(node?.kind === 'action' && node.node.type).toBe('deal_damage')
  })

  it('indexPathFor derives the validator grammar `...body[k]`', () => {
    expect(indexPathFor(loopProgram(), loopBodyPath('b2'))).toBe('triggers[0].actions[0].body[1]')
  })

  it('findNodePathById locates a body action anywhere in the loop', () => {
    expect(findNodePathById(loopProgram(), 'b3')).toEqual(loopBodyPath('b3'))
  })
})

describe('loop body mutation ops', () => {
  it('addAction appends to the loop body (container path = the loop action)', () => {
    const loopPath: NodePath = [{ kind: 'trigger', id: 'cast' }, { kind: 'action', id: 'lp' }]
    const next = addAction(loopProgram(), loopPath, 'deal_damage')
    const loop = resolveNode(next, loopPath)
    const body = (loop?.kind === 'action' && loop.node.config?.body) as AbilityActionDef[]
    expect(body.map((b) => b.id)).toEqual(['b1', 'b2', 'b3', 'a1'])
  })

  it('removeAction drops a body action', () => {
    const next = removeAction(loopProgram(), loopBodyPath('b2'))
    expect(resolveNode(next, loopBodyPath('b2'))).toBeUndefined()
    expect(resolveNode(next, loopBodyPath('b1'))?.kind).toBe('action')
  })

  it('moveAction reorders within the body', () => {
    const next = moveAction(loopProgram(), loopBodyPath('b1'), 'down')
    const loop = resolveNode(next, [{ kind: 'trigger', id: 'cast' }, { kind: 'action', id: 'lp' }])
    const body = (loop?.kind === 'action' && loop.node.config?.body) as AbilityActionDef[]
    expect(body.map((b) => b.id)).toEqual(['b2', 'b1', 'b3'])
  })

  it('updateAction patches a body action in place', () => {
    const next = updateAction(loopProgram(), loopBodyPath('b2'), { config: { amount: 99 } })
    const node = resolveNode(next, loopBodyPath('b2'))
    expect(node?.kind === 'action' && node.node.config?.amount).toBe(99)
  })

  it('setActionDisabled toggles a body action', () => {
    const next = setActionDisabled(loopProgram(), loopBodyPath('b3'), true)
    const node = resolveNode(next, loopBodyPath('b3'))
    expect(node?.kind === 'action' && node.node.disabled).toBe(true)
  })

  it('duplicateAction inserts a re-id\'d copy after the body action', () => {
    const next = duplicateAction(loopProgram(), loopBodyPath('b2'))
    const loop = resolveNode(next, [{ kind: 'trigger', id: 'cast' }, { kind: 'action', id: 'lp' }])
    const body = (loop?.kind === 'action' && loop.node.config?.body) as AbilityActionDef[]
    expect(body).toHaveLength(4)
    expect(body[2].type).toBe('deal_damage') // the copy sits right after b2
    expect(body[2].id).not.toBe('b2') // …with a fresh id
  })

  it('the original program is never mutated by a body op', () => {
    const p = loopProgram()
    removeAction(p, loopBodyPath('b2'))
    const loop = resolveNode(p, [{ kind: 'trigger', id: 'cast' }, { kind: 'action', id: 'lp' }])
    const body = (loop?.kind === 'action' && loop.node.config?.body) as AbilityActionDef[]
    expect(body.map((b) => b.id)).toEqual(['b1', 'b2', 'b3'])
  })
})

describe('loopScopeFor', () => {
  it('reports the loop variables in scope for a body action', () => {
    const scope = loopScopeFor(loopProgram(), loopBodyPath('b2'))
    expect(scope.inLoop).toBe(true)
    expect(scope.vars).toEqual(['a'])
  })

  it('reports not-in-loop for an action directly under a trigger', () => {
    const p = baseProgram()
    expect(loopScopeFor(p, actionPath('t1', 'a1'))).toEqual({ inLoop: false, vars: [] })
  })

  it('does NOT put a loop in its own variable scope (its iterations field is a plain number)', () => {
    const scope = loopScopeFor(loopProgram(), [{ kind: 'trigger', id: 'cast' }, { kind: 'action', id: 'lp' }])
    expect(scope.inLoop).toBe(false)
  })
})

// ── conditional branches: actions nested in config.then / config.else ──────
//
// A conditional owns TWO nested action lists (unlike a loop's single
// config.body), so these prove the generalization actually disambiguates
// between them by SEARCHING for the id rather than assuming a slot from the
// path shape alone — a `then` action and an `else` action share the exact
// same NodePath shape (their parent's path + one more `{kind:'action', id}`
// segment).
function conditionalProgram(): AbilityProgram {
  return {
    entry: { type: 'unit', range: 300 },
    triggers: [
      {
        id: 'tick',
        type: 'on_tick',
        actions: [
          {
            id: 'deliver',
            type: 'conditional',
            config: {
              conditions: [{ op: 'has_perk', right: 'lasting_flames' }],
              then: [
                { id: 'burn', type: 'apply_status_duration', config: { name: 'Burning' } },
              ],
              else: [
                { id: 'direct_dmg', type: 'deal_damage', config: { amount: 16, type: 'fire' } },
              ],
            },
          },
        ],
      },
    ],
  }
}

function conditionalPath(): NodePath {
  return [{ kind: 'trigger', id: 'tick' }, { kind: 'action', id: 'deliver' }]
}

function branchActionPath(actionId: string): NodePath {
  return [...conditionalPath(), { kind: 'action', id: actionId }]
}

describe('conditional branch traversal', () => {
  it('resolveNode reaches an action inside config.then and one inside config.else', () => {
    const thenNode = resolveNode(conditionalProgram(), branchActionPath('burn'))
    expect(thenNode?.kind).toBe('action')
    expect(thenNode?.kind === 'action' && thenNode.node.type).toBe('apply_status_duration')

    const elseNode = resolveNode(conditionalProgram(), branchActionPath('direct_dmg'))
    expect(elseNode?.kind).toBe('action')
    expect(elseNode?.kind === 'action' && elseNode.node.type).toBe('deal_damage')
  })

  it('indexPathFor derives the validator-style grammar `...then[k]` / `...else[k]`', () => {
    expect(indexPathFor(conditionalProgram(), branchActionPath('burn'))).toBe('triggers[0].actions[0].then[0]')
    expect(indexPathFor(conditionalProgram(), branchActionPath('direct_dmg'))).toBe(
      'triggers[0].actions[0].else[0]',
    )
  })

  it('findNodePathById locates an action in either branch', () => {
    expect(findNodePathById(conditionalProgram(), 'burn')).toEqual(branchActionPath('burn'))
    expect(findNodePathById(conditionalProgram(), 'direct_dmg')).toEqual(branchActionPath('direct_dmg'))
  })
})

describe('conditional branch mutation ops', () => {
  it('addAction with branch "then" appends to config.then only', () => {
    const next = addAction(conditionalProgram(), conditionalPath(), 'play_sound', 'then')
    const cond = resolveNode(next, conditionalPath())
    const then = (cond?.kind === 'action' && cond.node.config?.then) as AbilityActionDef[]
    const els = (cond?.kind === 'action' && cond.node.config?.else) as AbilityActionDef[]
    expect(then.map((a) => a.type)).toEqual(['apply_status_duration', 'play_sound'])
    expect(els).toHaveLength(1) // else untouched
  })

  it('addAction with branch "else" appends to config.else only', () => {
    const next = addAction(conditionalProgram(), conditionalPath(), 'play_sound', 'else')
    const cond = resolveNode(next, conditionalPath())
    const then = (cond?.kind === 'action' && cond.node.config?.then) as AbilityActionDef[]
    const els = (cond?.kind === 'action' && cond.node.config?.else) as AbilityActionDef[]
    expect(els.map((a) => a.type)).toEqual(['deal_damage', 'play_sound'])
    expect(then).toHaveLength(1) // then untouched
  })

  it('removeAction drops a then action without touching else', () => {
    const next = removeAction(conditionalProgram(), branchActionPath('burn'))
    expect(resolveNode(next, branchActionPath('burn'))).toBeUndefined()
    expect(resolveNode(next, branchActionPath('direct_dmg'))?.kind).toBe('action')
  })

  it('removeAction drops an else action without touching then', () => {
    const next = removeAction(conditionalProgram(), branchActionPath('direct_dmg'))
    expect(resolveNode(next, branchActionPath('direct_dmg'))).toBeUndefined()
    expect(resolveNode(next, branchActionPath('burn'))?.kind).toBe('action')
  })

  it('updateAction patches a then action in place', () => {
    const next = updateAction(conditionalProgram(), branchActionPath('burn'), {
      config: { name: 'Burning', duration: 8 },
    })
    const node = resolveNode(next, branchActionPath('burn'))
    expect(node?.kind === 'action' && node.node.config?.duration).toBe(8)
  })

  it('setActionDisabled toggles an else action', () => {
    const next = setActionDisabled(conditionalProgram(), branchActionPath('direct_dmg'), true)
    const node = resolveNode(next, branchActionPath('direct_dmg'))
    expect(node?.kind === 'action' && node.node.disabled).toBe(true)
  })

  it('moveAction reorders within a single branch (then, with a second step added first)', () => {
    const withExtra = addAction(conditionalProgram(), conditionalPath(), 'wait', 'then')
    const moved = moveAction(withExtra, branchActionPath('burn'), 'down')
    const cond = resolveNode(moved, conditionalPath())
    const then = (cond?.kind === 'action' && cond.node.config?.then) as AbilityActionDef[]
    expect(then.map((a) => a.id)).toEqual(['a1', 'burn'])
  })

  it('duplicateAction re-ids a copy and inserts it right after the original, in the same branch', () => {
    const next = duplicateAction(conditionalProgram(), branchActionPath('direct_dmg'))
    const cond = resolveNode(next, conditionalPath())
    const els = (cond?.kind === 'action' && cond.node.config?.else) as AbilityActionDef[]
    expect(els).toHaveLength(2)
    expect(els[1].type).toBe('deal_damage')
    expect(els[1].id).not.toBe('direct_dmg')
  })

  it('duplicating the conditional ACTION ITSELF re-ids every action in BOTH branches (not just then)', () => {
    const next = duplicateAction(conditionalProgram(), conditionalPath())
    const trigger = findTrigger(next, 'tick')!
    expect(trigger.actions).toHaveLength(2)
    const copy = trigger.actions[1]
    const then = copy.config?.then as AbilityActionDef[]
    const els = copy.config?.else as AbilityActionDef[]
    // Fresh ids on both branches' actions — none collide with the original's.
    expect(then[0].id).not.toBe('burn')
    expect(els[0].id).not.toBe('direct_dmg')
    // ...and the copy's own branch action ids don't collide with each other either.
    expect(then[0].id).not.toBe(els[0].id)
  })

  it('the original program is never mutated by a branch op', () => {
    const p = conditionalProgram()
    removeAction(p, branchActionPath('burn'))
    const cond = resolveNode(p, conditionalPath())
    const then = (cond?.kind === 'action' && cond.node.config?.then) as AbilityActionDef[]
    expect(then.map((a) => a.id)).toEqual(['burn'])
  })
})

describe('collectConditionals', () => {
  it('returns nothing for a program with no conditionals', () => {
    expect(collectConditionals(emptyProgram())).toEqual([])
  })

  // Depth is the whole point: fire_pit's conditional lives three levels down
  // (cast trigger -> create_zone -> on_tick -> conditional), so a collector
  // that only scanned root actions would offer no toggle for the one branch
  // the author actually needs to force.
  it('finds conditionals nested inside a zone trigger, and summarizes the branch', () => {
    const prog: AbilityProgram = {
      entry: { type: 'ground_point', range: 220 },
      triggers: [
        {
          id: 'cast',
          type: 'on_cast_complete',
          actions: [
            {
              id: 'pit',
              type: 'create_zone',
              config: {
                triggers: [
                  {
                    id: 'tick',
                    type: 'on_tick',
                    actions: [
                      {
                        id: 'deliver',
                        type: 'conditional',
                        config: { conditions: [{ op: 'has_perk', right: 'lasting_flames' }] },
                      },
                    ],
                  },
                ],
              },
            },
          ],
        },
      ],
    }
    const found = collectConditionals(prog)
    expect(found).toHaveLength(1)
    expect(found[0].id).toBe('deliver')
    expect(found[0].summary).toContain('has perk')
  })

  it('finds a conditional nested inside another conditional\'s branch, in document order', () => {
    const prog: AbilityProgram = {
      entry: { type: 'no_target', range: 0 },
      triggers: [
        {
          id: 'cast',
          type: 'on_cast_complete',
          actions: [
            {
              id: 'outer',
              type: 'conditional',
              config: {
                conditions: [],
                then: [{ id: 'inner_then', type: 'conditional', config: { conditions: [] } }],
                else: [{ id: 'inner_else', type: 'conditional', config: { conditions: [] } }],
              },
            },
          ],
        },
      ],
    }
    expect(collectConditionals(prog).map((c) => c.id)).toEqual(['outer', 'inner_then', 'inner_else'])
  })
})

