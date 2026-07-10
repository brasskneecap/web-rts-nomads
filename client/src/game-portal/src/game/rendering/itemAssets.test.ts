import { describe, expect, it } from 'vitest'
import { getItemAssetImage, getItemImageSourceUrl, listItemAssetKeys } from './itemAssets'

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
  it('returns null for keys whose server fetch has failed (placeholder path restored)', () => {
    const img = getItemAssetImage('definitely_missing_everywhere')
    expect(img).not.toBeNull()
    img!.dispatchEvent(new Event('error'))
    expect(getItemAssetImage('definitely_missing_everywhere')).toBeNull()
  })
})
