import { describe, expect, it } from 'vitest'
import { getRecipeShopStyleSprite, getRecipeShopStyleUrl, listRecipeShopStyles } from './buildingSprites'

describe('recipe-shop style sprites', () => {
  it('returns null for an unset / empty / unknown style', () => {
    expect(getRecipeShopStyleSprite(undefined)).toBeNull()
    expect(getRecipeShopStyleSprite(null)).toBeNull()
    expect(getRecipeShopStyleSprite('')).toBeNull()
    expect(getRecipeShopStyleSprite('no_such_style')).toBeNull()
  })

  it('resolves the Shop-card image URL from the shop style', () => {
    // Every registered style yields its own asset URL — this is what drives the
    // Shop menu card art "based on the style being used." Recipe shops ship no
    // built-in default, so an unset/unknown style resolves to the same fallback
    // (currently null) and the card uses its ActionIcon building icon instead.
    for (const style of listRecipeShopStyles()) {
      expect(typeof getRecipeShopStyleUrl(style)).toBe('string')
    }
    expect(getRecipeShopStyleUrl('no_such_style')).toBe(getRecipeShopStyleUrl(undefined))
  })

  it('exposes a (possibly empty) sorted list of style names', () => {
    const styles = listRecipeShopStyles()
    expect(Array.isArray(styles)).toBe(true)
    const sorted = [...styles].sort()
    expect(styles).toEqual(sorted)
  })
})
