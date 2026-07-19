import { describe, expect, it } from 'vitest'
import type { TargetQueryDef } from '@/game/abilities/program/abilityProgram'
import { summarizeTargetQuery } from './summarizeTargetQuery'

describe('summarizeTargetQuery', () => {
  it('describes the default all-in-scene query', () => {
    expect(summarizeTargetQuery(undefined)).toBe('Select all living units in the scene.')
    expect(summarizeTargetQuery({ source: 'all_in_scene' })).toBe('Select all living units in the scene.')
  })

  it('names a single referenced unit without pool modifiers', () => {
    expect(summarizeTargetQuery({ source: 'current_event' })).toBe('Select the triggering unit.')
    expect(summarizeTargetQuery({ source: 'caster' })).toBe('Select the caster.')
    expect(summarizeTargetQuery({ source: 'initial_target' })).toBe('Select the initial target.')
  })

  it('reproduces the Frost Bolt chain-split query in prose', () => {
    const q: TargetQueryDef = {
      source: 'all_in_scene',
      excludeCurrentEvent: true,
      origin: 'current_event_position',
      radius: 200,
      maxCount: 2,
      ordering: 'closest',
      relations: ['enemy'],
    }
    expect(summarizeTargetQuery(q)).toBe(
      'Select the 2 closest living enemy units within 200 units of the triggering unit, excluding the triggering unit.',
    )
  })

  it('describes a resurrection-style query (dead allies near the caster)', () => {
    const q: TargetQueryDef = {
      source: 'all_in_scene',
      relations: ['ally'],
      aliveState: 'dead',
      maxCount: 3,
      radius: 150,
      origin: 'caster',
    }
    expect(summarizeTargetQuery(q)).toBe('Select up to 3 dead allied units within 150 units of the caster.')
  })

  it('uses a singular noun and no count word for a single-target ordered pick', () => {
    const q: TargetQueryDef = { source: 'all_in_scene', maxCount: 1, ordering: 'lowest_health', relations: ['enemy'] }
    expect(summarizeTargetQuery(q)).toBe('Select the lowest-health living enemy unit in the scene.')
  })

  it('omits the state word for "any" alive state', () => {
    const q: TargetQueryDef = { source: 'all_in_scene', aliveState: 'any', relations: ['enemy'] }
    expect(summarizeTargetQuery(q)).toBe('Select all enemy units in the scene.')
  })

  it('describes previous-action-target and saved-selection sources', () => {
    expect(summarizeTargetQuery({ source: 'previous_action_targets' })).toBe(
      "Select all living units among the previous action's targets.",
    )
    expect(summarizeTargetQuery({ source: 'named_context', originRef: { key: 'chainTargets' } })).toBe(
      'Select all living units from the saved selection "chainTargets".',
    )
  })

  it('renders a random pick and joined relations', () => {
    const single: TargetQueryDef = { source: 'all_in_scene', maxCount: 1, ordering: 'random' }
    expect(summarizeTargetQuery(single)).toBe('Select a random living unit in the scene.')

    const multi: TargetQueryDef = { source: 'all_in_scene', relations: ['ally', 'enemy'] }
    expect(summarizeTargetQuery(multi)).toBe('Select all living allied/enemy units in the scene.')
  })

  it('appends exclusion/inclusion clauses', () => {
    const q: TargetQueryDef = { source: 'all_in_scene', excludeSource: true, includeInitialTarget: true }
    expect(summarizeTargetQuery(q)).toBe(
      'Select all living units in the scene, always including the initial target, excluding the caster.',
    )
  })

  it('notes an excludeRef saved-set exclusion (chains)', () => {
    const q: TargetQueryDef = { source: 'all_in_scene', relations: ['enemy'], excludeRef: { key: 'chainHits' } }
    expect(summarizeTargetQuery(q)).toBe(
      'Select all living enemy units in the scene, excluding anyone already in "chainHits".',
    )
  })

  it('degrades unknown enum values to their raw string without throwing', () => {
    const q = { source: 'some_future_source', relations: ['gremlin'] } as unknown as TargetQueryDef
    expect(() => summarizeTargetQuery(q)).not.toThrow()
    expect(summarizeTargetQuery(q)).toBe('Select all living gremlin units in the scene.')
  })
})
