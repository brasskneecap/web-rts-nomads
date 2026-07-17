import { describe, it, expect } from 'vitest'
import {
  fieldConditionMatches,
  fieldVisible,
  parseActionSchemaResponse,
  schemaForAction,
  type ActionSchemaBundle,
  type FieldCondition,
  type SchemaField,
} from './programSchema'

describe('programSchema', () => {
  it('parses action schema response', () => {
    const raw = { actions: [
      { type: 'deal_damage', fields: [{ key: 'amount', label: 'Amount', control: 'number', section: 'Properties' }], runnable: true },
      { type: 'play_presentation', fields: [], runnable: false },
    ], enums: { actionTypes: ['deal_damage','play_presentation'], relations: ['self','ally','enemy','neutral'] } }
    const bundle: ActionSchemaBundle = parseActionSchemaResponse(raw)
    expect(bundle.actions).toHaveLength(2)
    expect(bundle.enums.relations).toContain('enemy')
    const dd = schemaForAction(bundle, 'deal_damage')
    expect(dd?.runnable).toBe(true)
    expect(dd?.fields[0].key).toBe('amount')
    expect(schemaForAction(bundle, 'play_presentation')?.runnable).toBe(false)
  })
  it('treats missing fields as empty array', () => {
    const bundle = parseActionSchemaResponse({ actions: [{ type: 'x', runnable: false }], enums: {} })
    expect(schemaForAction(bundle, 'x')?.fields).toEqual([])
  })
})

describe('fieldConditionMatches', () => {
  // Matches the Go reference impl's own doc example: launch_projectile's
  // `target`/`distance` gate on travelMode (string eq/ne), and its 4
  // chain-only fields gate on chainCount (numeric gt).
  it('eq matches a string field explicitly set to the compared value', () => {
    const cond: FieldCondition = { key: 'travelMode', op: 'eq', value: 'direction' }
    expect(fieldConditionMatches(cond, { travelMode: 'direction' })).toBe(true)
    expect(fieldConditionMatches(cond, { travelMode: 'to_target' })).toBe(false)
  })

  it('ne is the inverse of eq for the same key/value', () => {
    const cond: FieldCondition = { key: 'travelMode', op: 'ne', value: 'direction' }
    expect(fieldConditionMatches(cond, { travelMode: 'direction' })).toBe(false)
    expect(fieldConditionMatches(cond, { travelMode: 'to_target' })).toBe(true)
  })

  it('a missing key resolves to the zero value of value\'s own type: string', () => {
    // travelMode never authored -> "" != "direction" -> ne matches (distance
    // is hidden by default, matching the "to_target" default behavior).
    expect(fieldConditionMatches({ key: 'travelMode', op: 'ne', value: 'direction' }, {})).toBe(true)
    expect(fieldConditionMatches({ key: 'travelMode', op: 'eq', value: 'direction' }, {})).toBe(false)
  })

  it('a missing key resolves to the zero value of value\'s own type: number', () => {
    // chainCount never authored -> 0, so "> 0" is false (chain fields hidden).
    expect(fieldConditionMatches({ key: 'chainCount', op: 'gt', value: 0 }, {})).toBe(false)
    expect(fieldConditionMatches({ key: 'chainCount', op: 'gt', value: 0 }, { chainCount: 2 })).toBe(true)
  })

  it('a missing key resolves to the zero value of value\'s own type: boolean', () => {
    expect(fieldConditionMatches({ key: 'flag', op: 'eq', value: false }, {})).toBe(true)
    expect(fieldConditionMatches({ key: 'flag', op: 'eq', value: true }, {})).toBe(false)
  })

  it('numeric ops: lt/lte/gt/gte', () => {
    expect(fieldConditionMatches({ key: 'n', op: 'lt', value: 5 }, { n: 4 })).toBe(true)
    expect(fieldConditionMatches({ key: 'n', op: 'lt', value: 5 }, { n: 5 })).toBe(false)
    expect(fieldConditionMatches({ key: 'n', op: 'lte', value: 5 }, { n: 5 })).toBe(true)
    expect(fieldConditionMatches({ key: 'n', op: 'gt', value: 5 }, { n: 6 })).toBe(true)
    expect(fieldConditionMatches({ key: 'n', op: 'gte', value: 5 }, { n: 5 })).toBe(true)
    expect(fieldConditionMatches({ key: 'n', op: 'gte', value: 5 }, { n: 4 })).toBe(false)
  })

  it('numeric ops on a non-numeric value conservatively resolve to false, not a throw', () => {
    expect(fieldConditionMatches({ key: 'n', op: 'gt', value: 5 }, { n: 'oops' })).toBe(false)
    expect(fieldConditionMatches({ key: 'n', op: 'gt', value: 'oops' }, { n: 5 })).toBe(false)
  })

  it('an unrecognized op conservatively resolves to false', () => {
    expect(fieldConditionMatches({ key: 'n', op: 'contains', value: 5 }, { n: 5 })).toBe(false)
  })

  it('a non-scalar config value or condition value never throws and resolves to false', () => {
    expect(() => fieldConditionMatches({ key: 'n', op: 'eq', value: [1, 2] }, { n: [1, 2] })).not.toThrow()
    expect(fieldConditionMatches({ key: 'n', op: 'eq', value: [1, 2] }, { n: [1, 2] })).toBe(false)
    expect(() => fieldConditionMatches({ key: 'n', op: 'ne', value: { a: 1 } }, { n: { a: 1 } })).not.toThrow()
    expect(fieldConditionMatches({ key: 'n', op: 'ne', value: { a: 1 } }, { n: { a: 1 } })).toBe(false)
  })

  it('eq treats null and a missing-then-defaulted key as distinct (null is not a JSON scalar default)', () => {
    // config[key] explicitly null is a JSON scalar (comparable); it just
    // isn't equal to a string/number/boolean `value`.
    expect(fieldConditionMatches({ key: 'n', op: 'eq', value: 'x' }, { n: null })).toBe(false)
    expect(fieldConditionMatches({ key: 'n', op: 'ne', value: 'x' }, { n: null })).toBe(true)
  })
})

describe('fieldVisible', () => {
  it('is always true when the field declares no showWhen', () => {
    const field: SchemaField = { key: 'amount', label: 'Amount', control: 'number' }
    expect(fieldVisible(field, {})).toBe(true)
    expect(fieldVisible(field, undefined)).toBe(true)
  })

  it('gates a field per its showWhen against the action config', () => {
    const field: SchemaField = {
      key: 'distance',
      label: 'Distance',
      control: 'number',
      showWhen: { key: 'travelMode', op: 'eq', value: 'direction' },
    }
    expect(fieldVisible(field, { travelMode: 'direction' })).toBe(true)
    expect(fieldVisible(field, { travelMode: 'to_target' })).toBe(false)
    expect(fieldVisible(field, undefined)).toBe(false) // missing key -> "" != "direction"
  })
})
