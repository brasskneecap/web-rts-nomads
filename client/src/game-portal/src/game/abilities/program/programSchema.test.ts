import { describe, it, expect } from 'vitest'
import { parseActionSchemaResponse, schemaForAction, type ActionSchemaBundle } from './programSchema'

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
