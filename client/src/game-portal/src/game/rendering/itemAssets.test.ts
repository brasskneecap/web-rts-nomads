import { afterEach, describe, expect, it } from 'vitest'
import { getItemAssetImage, getItemImageSourceUrl, listIconGroups, listItemAssetKeys, registerUploadedIcons } from './itemAssets'

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

// An uploaded icon has to beat the bundled asset, or replacing a SHIPPED item's
// icon silently does nothing: every shipped item ships with bundled art keyed by
// its id, and the upload route forces iconKey to that same id.
describe('author-uploaded icons override bundled art', () => {
  afterEach(() => {
    // Clear the registration so one test's upload can't leak into another.
    registerUploadedIcons([{ id: 'fire_sword' }, { id: 'broad_sword' }])
  })

  it('serves the versioned server URL for an item with an uploaded icon, even when bundled art exists', () => {
    expect(getItemImageSourceUrl('fire_sword')).not.toContain('/catalog/items/')

    registerUploadedIcons([{ id: 'fire_sword', iconUploadedAt: 1700000000 }])

    expect(getItemImageSourceUrl('fire_sword')).toBe('/catalog/items/fire_sword/image?v=1700000000')
  })

  it('busts the cache when the same icon is re-uploaded', () => {
    registerUploadedIcons([{ id: 'broad_sword', iconUploadedAt: 111 }])
    const first = getItemAssetImage('broad_sword')
    expect(first!.src).toContain('v=111')

    registerUploadedIcons([{ id: 'broad_sword', iconUploadedAt: 222 }])
    const second = getItemAssetImage('broad_sword')
    expect(second!.src).toContain('v=222')
    expect(second).not.toBe(first) // a new Image, not the cached one
  })

  it('falls back to the bundled asset once the upload is removed', () => {
    registerUploadedIcons([{ id: 'fire_sword', iconUploadedAt: 1700000000 }])
    expect(getItemImageSourceUrl('fire_sword')).toContain('/catalog/items/')

    registerUploadedIcons([{ id: 'fire_sword' }]) // reset-to-default deletes the icon

    expect(getItemImageSourceUrl('fire_sword')).not.toContain('/catalog/items/')
  })
})
