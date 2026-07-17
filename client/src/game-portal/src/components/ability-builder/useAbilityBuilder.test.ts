import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import type { AuthoredAbilityDef } from '@/game/abilities/abilityEditorForm'
import type { ValidationIssue } from '@/game/abilities/program/programValidation'
import { useAbilityBuilder } from './useAbilityBuilder'
import { resolveNode, type NodePath } from './programTree'

// A legacy (pre-schemaVersion-2) ability: no authored `program`, only a
// server-computed `compiledProgram` view — this is the shape
// GET /catalog/abilities returns for an ability nobody has converted yet.
const legacyAbility: AuthoredAbilityDef = {
  id: 'heal',
  displayName: 'Heal',
  schemaVersion: 1,
  runnable: true,
  compiledProgram: {
    entry: { type: 'unit', range: 300 },
    triggers: [
      {
        id: 't1',
        type: 'on_cast_complete',
        actions: [{ id: 'a1', type: 'restore_health' }],
      },
    ],
  },
}

// A composable (schemaVersion 2) ability: carries its own authored
// `program` directly, and is savable without going through convert().
const composableAbility: AuthoredAbilityDef = {
  id: 'fireball',
  displayName: 'Fireball',
  schemaVersion: 2,
  runnable: true,
  program: {
    entry: { type: 'unit', range: 400 },
    triggers: [
      {
        id: 't1',
        type: 'on_cast_complete',
        actions: [{ id: 'a1', type: 'deal_damage', config: { amount: 10, radius: 2 } }],
      },
    ],
  },
}

function jsonResponse(body: unknown, status = 200) {
  return { ok: status < 400, status, json: async () => body }
}

interface FetchMockOptions {
  abilities: AuthoredAbilityDef[]
  validateIssues: ValidationIssue[]
  convertResult?: { ability: AuthoredAbilityDef; warnings: string[]; runnable: boolean }
}

// makeFetchMock stubs every /catalog + /abilities endpoint useAbilityBuilder
// touches, keyed by URL suffix + method (mirrors the stubCatalogFetch pattern
// in AbilityEditorPanel.test.ts). `saveCalls` records POST /abilities bodies
// so save() can be asserted against without reaching into the module
// internals.
function makeFetchMock(opts: FetchMockOptions) {
  const saveCalls: { ability: AuthoredAbilityDef }[] = []
  const fn = vi.fn(async (url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'
    if (method === 'GET' && u.endsWith('/catalog/abilities')) return jsonResponse({ abilities: opts.abilities })
    if (method === 'GET' && u.endsWith('/catalog/action-schema')) return jsonResponse({ actions: [], enums: {} })
    if (method === 'GET' && u.endsWith('/catalog/effects')) return jsonResponse({ effects: [] })
    if (method === 'GET' && u.endsWith('/catalog/projectiles')) return jsonResponse({ projectiles: [] })
    if (method === 'GET' && u.endsWith('/catalog/damage-types')) return jsonResponse({ damageTypes: [] })
    if (method === 'GET' && u.endsWith('/catalog/ability-categories')) return jsonResponse({ abilityCategories: [] })
    if (method === 'GET' && u.endsWith('/catalog/autocast-selectors')) return jsonResponse({ autoCastSelectors: [] })
    if (method === 'GET' && u.endsWith('/catalog/units')) return jsonResponse({ units: [] })
    if (method === 'POST' && u.endsWith('/abilities/validate')) return jsonResponse({ issues: opts.validateIssues })
    if (method === 'POST' && u.includes('/convert')) {
      if (!opts.convertResult) throw new Error('convertResult not configured for this test')
      return jsonResponse({
        ability: opts.convertResult.ability,
        warnings: opts.convertResult.warnings,
        runnable: opts.convertResult.runnable,
      })
    }
    if (method === 'POST' && u.endsWith('/abilities')) {
      saveCalls.push(JSON.parse(String(init?.body)))
      return jsonResponse({})
    }
    return jsonResponse({})
  })
  return { fn, saveCalls }
}

afterEach(() => vi.restoreAllMocks())

describe('useAbilityBuilder load + selection', () => {
  it('load() + selectAbility() populates form/program/isLegacy/runnable from a legacy ability', async () => {
    const { fn } = makeFetchMock({ abilities: [legacyAbility], validateIssues: [] })
    vi.stubGlobal('fetch', fn)

    const { load, selectAbility, form, program, isLegacy, runnable, editing } = useAbilityBuilder()
    await load()
    selectAbility('heal')
    await flushPromises()

    expect(editing.value).toBe(true)
    expect(form.value.id).toBe('heal')
    expect(form.value.displayName).toBe('Heal')
    expect(isLegacy.value).toBe(true)
    expect(runnable.value).toBe(true)
    expect(program.value).toEqual(legacyAbility.compiledProgram)
  })
})

describe('useAbilityBuilder mutations + undo/redo', () => {
  it('a mutation marks dirty + canUndo, and undo() restores the prior program', async () => {
    const { fn } = makeFetchMock({ abilities: [legacyAbility], validateIssues: [] })
    vi.stubGlobal('fetch', fn)

    const b = useAbilityBuilder()
    await b.load()
    b.selectAbility('heal')
    await flushPromises()

    expect(b.dirty.value).toBe(false)
    expect(b.canUndo.value).toBe(false)
    const programBefore = structuredClone(b.program.value)

    const newId = b.addAction([{ kind: 'trigger', id: 't1' }], 'deal_damage')

    expect(newId).not.toBe('')
    expect(b.dirty.value).toBe(true)
    expect(b.canUndo.value).toBe(true)
    expect(b.program.value.triggers[0].actions).toHaveLength(2)
    expect(b.program.value.triggers[0].actions.at(-1)?.id).toBe(newId)
    expect(b.selected.value).toEqual({
      kind: 'action',
      path: [{ kind: 'trigger', id: 't1' }, { kind: 'action', id: newId }],
    })

    b.undo()

    expect(b.program.value).toEqual(programBefore)
    expect(b.canUndo.value).toBe(false)
    expect(b.canRedo.value).toBe(true)

    b.redo()
    expect(b.program.value.triggers[0].actions).toHaveLength(2)
  })
})

describe('useAbilityBuilder updateActionConfig', () => {
  it('merges one config key without dropping other config keys, and creates an undo entry', async () => {
    const { fn } = makeFetchMock({ abilities: [composableAbility], validateIssues: [] })
    vi.stubGlobal('fetch', fn)

    const b = useAbilityBuilder()
    await b.load()
    b.selectAbility('fireball')
    await flushPromises()

    expect(b.canUndo.value).toBe(false)
    const action = () => b.program.value.triggers[0].actions[0]
    expect(action().config).toEqual({ amount: 10, radius: 2 })

    b.updateActionConfig([{ kind: 'trigger', id: 't1' }, { kind: 'action', id: 'a1' }], { amount: 25 })

    expect(action().config).toEqual({ amount: 25, radius: 2 })
    expect(b.dirty.value).toBe(true)
    expect(b.canUndo.value).toBe(true)

    b.undo()
    expect(action().config).toEqual({ amount: 10, radius: 2 })
  })
})

describe('useAbilityBuilder legacy save guard', () => {
  it('refuses to save a legacy ability directly, without calling the API', async () => {
    const { fn, saveCalls } = makeFetchMock({ abilities: [legacyAbility], validateIssues: [] })
    vi.stubGlobal('fetch', fn)

    const b = useAbilityBuilder()
    await b.load()
    b.selectAbility('heal')
    await flushPromises()
    expect(b.isLegacy.value).toBe(true)

    await b.save()

    expect(saveCalls).toHaveLength(0)
    expect(b.saveError.value).toBe('Convert this ability to composable before saving.')
  })
})

describe('useAbilityBuilder convert', () => {
  it('resets the undo baseline after converting, keeping dirty=true and surfacing warnings', async () => {
    const convertedAbility: AuthoredAbilityDef = {
      id: 'heal',
      displayName: 'Heal',
      schemaVersion: 2,
      runnable: true,
      program: {
        entry: { type: 'unit', range: 300 },
        triggers: [{ id: 't1', type: 'on_cast_complete', actions: [{ id: 'a1', type: 'restore_health' }] }],
      },
    }
    const { fn } = makeFetchMock({
      abilities: [legacyAbility],
      validateIssues: [],
      convertResult: { ability: convertedAbility, warnings: ['lossy: minorDamage flag dropped'], runnable: true },
    })
    vi.stubGlobal('fetch', fn)

    const b = useAbilityBuilder()
    await b.load()
    b.selectAbility('heal')
    await flushPromises()

    // Build up undo history BEFORE converting, to prove convert() clears it
    // rather than leaving a path back to the pre-conversion legacy form.
    b.addAction([{ kind: 'trigger', id: 't1' }], 'deal_damage')
    expect(b.canUndo.value).toBe(true)

    await b.convert()

    expect(b.isLegacy.value).toBe(false)
    expect(b.canUndo.value).toBe(false)
    expect(b.canRedo.value).toBe(false)
    expect(b.dirty.value).toBe(true)
    expect(b.warnings.value).toEqual(['lossy: minorDamage flag dropped'])
  })
})

describe('useAbilityBuilder save', () => {
  it('does not call the API and sets saveError when issues has a blocking error', async () => {
    const { fn, saveCalls } = makeFetchMock({ abilities: [composableAbility], validateIssues: [] })
    vi.stubGlobal('fetch', fn)

    const b = useAbilityBuilder()
    await b.load()
    b.selectAbility('fireball')
    await flushPromises()

    b.issues.value = [{ path: 'triggers[0]', code: 'bad_target', message: 'broken', severity: 'error' }]
    await b.save()

    expect(saveCalls).toHaveLength(0)
    expect(b.saveError.value).not.toBe('')
  })

  it('calls saveEditorAbility with schemaVersion 2 + a program when clean', async () => {
    const { fn, saveCalls } = makeFetchMock({ abilities: [composableAbility], validateIssues: [] })
    vi.stubGlobal('fetch', fn)

    const b = useAbilityBuilder()
    await b.load()
    b.selectAbility('fireball')
    await flushPromises()
    expect(b.issues.value).toEqual([])

    await b.save()

    expect(saveCalls).toHaveLength(1)
    const sent = saveCalls[0].ability
    expect(sent.schemaVersion).toBe(2)
    expect(sent.program).toBeTruthy()
    expect((sent.program as { triggers: { id: string }[] }).triggers[0].id).toBe('t1')
    expect(b.saveError.value).toBe('')
    expect(b.dirty.value).toBe(false)
  })
})

describe('useAbilityBuilder nested selection (depth 3)', () => {
  // meteor's crater DoT shape: cast -> zone (create_zone) -> config.triggers
  // -> burn -> bdmg. Exercises select()/updateAction() against a NodePath
  // nested 3 levels deep, and proves undo/redo's structuredClone snapshot
  // (which now clones `selected: NodeRef` carrying a NodePath, not a flat
  // {triggerId, actionId}) keeps working against plain-data path arrays.
  const meteorAbility: AuthoredAbilityDef = {
    id: 'meteor',
    displayName: 'Meteor',
    schemaVersion: 2,
    runnable: true,
    program: {
      entry: { type: 'ground_point', range: 600 },
      triggers: [
        {
          id: 'cast',
          type: 'on_cast_complete',
          actions: [
            {
              id: 'zone',
              type: 'create_zone',
              config: {
                radius: 200,
                triggers: [
                  {
                    id: 'burn',
                    type: 'on_zone_tick',
                    timing: { tickInterval: 1000 },
                    actions: [{ id: 'bdmg', type: 'deal_damage', config: { amount: 5 } }],
                  },
                ],
              },
            },
          ],
        },
      ],
    },
  }

  const bdmgPath: NodePath = [
    { kind: 'trigger', id: 'cast' },
    { kind: 'action', id: 'zone' },
    { kind: 'trigger', id: 'burn' },
    { kind: 'action', id: 'bdmg' },
  ]

  it('selects and updates the crater DoT nested 3 levels deep, and undo restores both program and selection', async () => {
    const { fn } = makeFetchMock({ abilities: [meteorAbility], validateIssues: [] })
    vi.stubGlobal('fetch', fn)

    const b = useAbilityBuilder()
    await b.load()
    b.selectAbility('meteor')
    await flushPromises()

    b.select({ kind: 'action', path: bdmgPath })
    expect(b.selected.value).toEqual({ kind: 'action', path: bdmgPath })

    // resolveNode hits the MEANT node, not just any node at that depth —
    // guards against the WATCH OUT on resolveNode (a wrong programmatically
    // built path resolving to the wrong node instead of failing).
    const before = resolveNode(b.program.value, bdmgPath)
    expect(before?.kind).toBe('action')
    expect(before?.kind === 'action' && before.node.id).toBe('bdmg')
    expect(before?.kind === 'action' && before.node.config).toEqual({ amount: 5 })

    b.updateAction(bdmgPath, { config: { amount: 25 } })

    const afterUpdate = resolveNode(b.program.value, bdmgPath)
    expect(afterUpdate?.kind === 'action' && afterUpdate.node.config).toEqual({ amount: 25 })
    expect(b.canUndo.value).toBe(true)

    b.undo()

    const afterUndo = resolveNode(b.program.value, bdmgPath)
    expect(afterUndo?.kind === 'action' && afterUndo.node.config).toEqual({ amount: 5 })
    // Selection itself survived the structuredClone-based undo snapshot.
    expect(b.selected.value).toEqual({ kind: 'action', path: bdmgPath })

    b.redo()
    const afterRedo = resolveNode(b.program.value, bdmgPath)
    expect(afterRedo?.kind === 'action' && afterRedo.node.config).toEqual({ amount: 25 })
  })
})
