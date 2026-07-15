import { describe, expect, it } from 'vitest'
import { nonCraftableWarning, validateListForm } from './listEditorValidation'
import type { ListEditorForm, ListValidationContext } from './listEditorValidation'
import type { ItemDef } from '../maps/itemDefs'

const item = (id: string, craftable = false): ItemDef => ({
  id, displayName: id, iconKey: id, kind: 'equipment', tier: 'common', costGold: 10,
  ...(craftable
    ? { crafting: { inputs: ['a', 'b'], craftCostGold: 10, recipeCostGold: 20 } }
    : {}),
})

const ctx = (over: Partial<ListValidationContext> = {}): ListValidationContext => ({
  knownListIds: new Set<string>(),
  itemsById: new Map([
    ['broad_sword', item('broad_sword')],
    ['potion', item('potion')],
    ['fire_sword', item('fire_sword', true)],
  ]),
  ...over,
})

const uniform = (items: string[], over: Partial<ListEditorForm> = {}): ListEditorForm => ({
  id: 'my_list', isNew: true, name: 'My List', weighted: false, maxRoll: 100,
  members: items.map((item) => ({ item, min: 1, max: 1 })),
  ...over,
})

const weighted = (
  members: { item: string; min: number; max: number }[],
  maxRoll = 100,
  over: Partial<ListEditorForm> = {},
): ListEditorForm => ({
  id: 'my_list', isNew: true, name: 'My List', weighted: true, maxRoll, members, ...over,
})

const form = (over: Partial<ListEditorForm> = {}): ListEditorForm => uniform(['broad_sword'], over)

const failures = (f: ListEditorForm, c = ctx()) =>
  validateListForm(f, c).filter((chk) => !chk.ok).map((chk) => chk.message)

describe('validateListForm', () => {
  it('accepts a well-formed list', () => {
    expect(failures(form())).toEqual([])
  })

  it('refuses an empty list', () => {
    expect(failures(uniform([]))).toContain('A list needs at least one item.')
  })

  it('refuses an unknown item', () => {
    expect(failures(uniform(['no_such_item']))).toContain('Unknown item: no_such_item.')
  })

  it('refuses duplicate members', () => {
    expect(failures(uniform(['broad_sword', 'broad_sword'])))
      .toContain('Duplicate item: broad_sword.')
  })

  it('refuses an id that collides with an existing list', () => {
    const c = ctx({ knownListIds: new Set(['my_list']) })
    expect(failures(form(), c).some((m) => m.includes('already exists'))).toBe(true)
  })

  it('refuses a name-less list', () => {
    expect(failures(form({ name: '' }))).toContain('Name is required.')
  })

  // A list of non-craftable items is VALID — that is the whole point of an
  // untyped list. It is meaningless to an Artificer and perfectly right as shop
  // stock or a loot pool, so it warns rather than blocking.
  it('does not block a list of non-craftable items', () => {
    expect(failures(uniform(['broad_sword', 'potion']))).toEqual([])
  })
})

describe('validateListForm — weighted lists', () => {
  it('accepts a weighted list that tiles the die', () => {
    expect(failures(weighted([
      { item: 'broad_sword', min: 1, max: 90 },
      { item: 'potion', min: 91, max: 100 },
    ]))).toEqual([])
  })

  it('rejects a gap in the die', () => {
    const msgs = failures(weighted([
      { item: 'broad_sword', min: 1, max: 50 },
      { item: 'potion', min: 61, max: 100 },
    ]))
    expect(msgs.some((m) => m.includes('51') && m.includes('60'))).toBe(true)
  })

  it('rejects an overlap', () => {
    const msgs = failures(weighted([
      { item: 'broad_sword', min: 1, max: 60 },
      { item: 'potion', min: 50, max: 100 },
    ]))
    expect(msgs.some((m) => m.toLowerCase().includes('claimed twice'))).toBe(true)
  })

  it('rejects a trailing gap', () => {
    expect(failures(weighted([{ item: 'broad_sword', min: 1, max: 50 }])).length).toBeGreaterThan(0)
  })
})

describe('nonCraftableWarning', () => {
  it('warns, in terms of what will happen, when members are not craftable', () => {
    const w = nonCraftableWarning(uniform(['broad_sword', 'potion', 'fire_sword']), ctx())
    expect(w).toContain('2 of 3')
    expect(w).toContain('will ignore them')
  })

  it('says nothing when every member is craftable', () => {
    expect(nonCraftableWarning(uniform(['fire_sword']), ctx())).toBe('')
  })

  it('says nothing for an empty list (the checklist already covers that)', () => {
    expect(nonCraftableWarning(uniform([]), ctx())).toBe('')
  })
})
