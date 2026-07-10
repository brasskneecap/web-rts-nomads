import { describe, expect, it } from 'vitest'
import { MENU_ENTRIES } from '@/views/MainMenu.vue'

describe('main menu entries', () => {
  it('has five entries with Item Editor between Map Editor and Settings', () => {
    const labels = MENU_ENTRIES.map((e) => e.label)
    expect(labels).toEqual(['Start Game', 'Profile', 'Map Editor', 'Item Editor', 'Settings'])
    const editor = MENU_ENTRIES.find((e) => e.label === 'Item Editor')
    expect(editor?.to).toBe('/item-editor')
    // tops strictly increasing within the sign span
    const tops = MENU_ENTRIES.map((e) => e.top)
    expect([...tops].sort((a, b) => a - b)).toEqual(tops)
    expect(tops[0]).toBeCloseTo(55.97, 1)
    // Settings sits on the fifth plank (plank pitch ~6.42%: 75.25 + 6.43).
    expect(tops[4]).toBeCloseTo(81.68, 1)
  })
})
