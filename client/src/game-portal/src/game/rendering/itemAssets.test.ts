import { describe, expect, it } from 'vitest'
import { getItemAssetImage, getItemImageSourceUrl, listIconGroups, listItemAssetKeys } from './itemAssets'

describe('itemAssets gallery + fallback', () => {
  it('lists bundled keys sorted and non-empty', () => {
    const keys = listItemAssetKeys()
    expect(keys.length).toBeGreaterThan(10)
    expect([...keys].sort()).toEqual(keys)
    expect(keys).toContain('fire_sword')
  })
  it('bundled key resolves to a bundled URL, unknown key to the server route', () => {
    expect(getItemImageSourceUrl('fire_sword')).not.toContain('/catalog/items/')
    expect(getItemImageSourceUrl('brand_new_upload')).toBe('/catalog/items/brand_new_upload/image')
  })
  it('includes icon-library assets (selectable in the editor gallery)', () => {
    const keys = listItemAssetKeys()
    expect(keys).toContain('shield_fire_0001') // from assets/icons/shield/
    expect(getItemImageSourceUrl('shield_fire_0001')).not.toContain('/catalog/items/')
  })
  it('groups icon-library assets by subdirectory, sorted, excluding item catalog art', () => {
    const groups = listIconGroups()
    expect(groups.length).toBeGreaterThan(1)
    // Sorted by group name.
    expect(groups.map((g) => g.name)).toEqual([...groups.map((g) => g.name)].sort())
    const shield = groups.find((g) => g.name === 'shield')
    expect(shield?.keys).toContain('shield_fire_0001')
    // Keys sorted within a group.
    expect(shield && [...shield.keys].sort()).toEqual(shield?.keys)
    // Item catalog art (fire_sword) is NOT part of any icon group.
    expect(groups.flatMap((g) => g.keys)).not.toContain('fire_sword')
  })
  it('returns null for keys whose server fetch has failed (placeholder path restored)', () => {
    const img = getItemAssetImage('definitely_missing_everywhere')
    expect(img).not.toBeNull()
    img!.dispatchEvent(new Event('error'))
    expect(getItemAssetImage('definitely_missing_everywhere')).toBeNull()
  })
})
