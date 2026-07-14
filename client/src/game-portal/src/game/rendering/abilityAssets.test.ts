import { describe, expect, it } from 'vitest'
import {
  listAbilityIconKeys,
  getAbilityIconImageByKey,
  getAbilityIconSourceUrl,
  getAbilityPreviewUrl,
} from './abilityAssets'

describe('abilityAssets icon keys', () => {
  it('listAbilityIconKeys returns bundled ability folder names, sorted', () => {
    const keys = listAbilityIconKeys()
    expect(keys).toEqual([...keys].sort())
    // fireball ships as a bundled ability icon folder
    expect(keys).toContain('fireball')
  })

  it('ignores a non-id-pattern key (placeholder path) rather than fetching it', () => {
    // A placeholder path must NOT resolve as a key (no bundled, no server fetch).
    expect(getAbilityIconImageByKey('TODO/abilities/fireball.png')).toBeNull()
    expect(getAbilityIconImageByKey(undefined)).toBeNull()
    expect(getAbilityIconImageByKey('')).toBeNull()
  })

  it('resolves a bundled key to its image', () => {
    expect(getAbilityIconImageByKey('fireball')).not.toBeNull()
  })

  it('getAbilityIconSourceUrl returns the server route for an unbundled key', () => {
    expect(getAbilityIconSourceUrl('uploaded_only')).toContain('/catalog/abilities/uploaded_only/image')
  })

  it('getAbilityIconSourceUrl returns empty string for a non-id-pattern key (placeholder path)', () => {
    expect(getAbilityIconSourceUrl('TODO/abilities/fireball.png')).toBe('')
  })

  it('getAbilityPreviewUrl falls back to the ability id for a placeholder icon path', () => {
    const url = getAbilityPreviewUrl('TODO/abilities/fireball.png', 'fireball')
    expect(url.toLowerCase()).not.toContain('todo')
    expect(url).not.toContain('/catalog/abilities/')
  })

  it('getAbilityPreviewUrl uses a valid key with no bundled asset via the server route', () => {
    const url = getAbilityPreviewUrl('uploaded_id', 'uploaded_id')
    expect(url).toContain('/catalog/abilities/uploaded_id/image')
  })
})
