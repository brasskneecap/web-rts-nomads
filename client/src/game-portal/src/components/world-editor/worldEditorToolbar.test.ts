import { describe, expect, it } from 'vitest'
import { WORLD_EDITOR_CATEGORIES } from './WorldEditorToolbar.vue'

describe('world editor toolbar categories', () => {
  it('lists the full vision with the milestone-1 ones enabled', () => {
    const byId = Object.fromEntries(WORLD_EDITOR_CATEGORIES.map((c) => [c.id, c]))
    // Full future set is present (visible), enabled flags reflect this milestone.
    // Terrain/obstacles/buildings/units are brush modes, not categories — they
    // live behind the single "Map" entry.
    expect(byId.map.enabled).toBe(true)
    expect(byId.terrain).toBeUndefined()
    expect(byId.obstacles).toBeUndefined()
    expect(byId.buildings).toBeUndefined()
    expect(byId.units).toBeUndefined()
    expect(byId.items.enabled).toBe(true)
    expect(byId.play.enabled).toBe(true)
    expect(byId.abilities.enabled).toBe(true)
    expect(byId.exit.enabled).toBe(true)
    // Later sub-projects: visible but disabled ("coming soon").
    expect(byId.effects.enabled).toBe(true)
    expect(byId.projectiles.enabled).toBe(true)
    expect(byId.perks.enabled).toBe(false)
    expect(byId.campaigns.enabled).toBe(false)
  })
})
