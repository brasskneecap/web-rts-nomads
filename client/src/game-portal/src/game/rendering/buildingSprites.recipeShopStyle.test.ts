import { describe, expect, it } from 'vitest'
import { getRecipeShopStyleSprite, listRecipeShopStyles } from './buildingSprites'

describe('recipe-shop style sprites', () => {
  it('returns null for an unset / empty / unknown style', () => {
    expect(getRecipeShopStyleSprite(undefined)).toBeNull()
    expect(getRecipeShopStyleSprite(null)).toBeNull()
    expect(getRecipeShopStyleSprite('')).toBeNull()
    expect(getRecipeShopStyleSprite('no_such_style')).toBeNull()
  })

  it('exposes a (possibly empty) sorted list of style names', () => {
    const styles = listRecipeShopStyles()
    expect(Array.isArray(styles)).toBe(true)
    const sorted = [...styles].sort()
    expect(styles).toEqual(sorted)
  })
})
