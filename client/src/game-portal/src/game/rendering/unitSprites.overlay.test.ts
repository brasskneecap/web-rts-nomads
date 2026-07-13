import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  buildSpriteSet,
  clearRuntimeSpriteSets,
  getUnitSpriteSet,
  loadRuntimeSpriteSets,
  registerRuntimeSpriteSet,
  type SpriteManifest,
  type UnitSpriteSet,
} from './unitSprites'

afterEach(() => {
  clearRuntimeSpriteSets()
  vi.restoreAllMocks()
})

const MANIFEST: SpriteManifest = {
  key: 'overlay_unit',
  size: { width: 104, height: 104 },
  animations: {
    walking: {
      frameCount: 4,
      frameWidth: 104,
      frameHeight: 104,
      sheet: 'packed/walking.png',
      rowOrder: ['north', 'south', 'east', 'west'],
    },
  },
}

describe('buildSpriteSet', () => {
  it('resolves sheet urls through the injected resolver', () => {
    const seen: string[] = []
    const set = buildSpriteSet('overlay_unit', MANIFEST, (rel) => {
      seen.push(rel)
      return `https://example.test/${rel}`
    })
    expect(seen).toContain('packed/walking.png')
    expect(set).not.toBeNull()
    expect(set!.size).toEqual({ width: 104, height: 104 })

    const walking = set!.animations.get('walking')
    expect(walking?.frameCount).toBe(4)
    // rowOrder[i] becomes the row index → getUnitFrame uses it as srcY. An
    // off-by-one here draws the wrong facing.
    expect(walking?.directions.north?.row).toBe(0)
    expect(walking?.directions.west?.row).toBe(3)
  })

  it('returns a set with no animations when no sheet resolves', () => {
    const set = buildSpriteSet('missing', MANIFEST, () => undefined)
    expect(set!.animations.size).toBe(0)
  })
})

describe('runtime overlay', () => {
  it('a runtime set shadows a bundled set with the same key', () => {
    const bundled = getUnitSpriteSet('archer')
    expect(bundled).not.toBeNull()

    const fake: UnitSpriteSet = {
      key: 'archer',
      size: { width: 1, height: 1 },
      rotations: {},
      animations: new Map(),
      beamOrigin: { x: 0, y: 0 },
    }
    registerRuntimeSpriteSet(fake)
    expect(getUnitSpriteSet('archer')!.size).toEqual({ width: 1, height: 1 })

    clearRuntimeSpriteSets()
    expect(getUnitSpriteSet('archer')!.size).not.toEqual({ width: 1, height: 1 })
  })

  it('still prefers the path key over the unit type', () => {
    const fake: UnitSpriteSet = {
      key: 'marksman', size: { width: 7, height: 7 },
      rotations: {}, animations: new Map(), beamOrigin: { x: 0, y: 0 },
    }
    registerRuntimeSpriteSet(fake)
    expect(getUnitSpriteSet('marksman', 'archer')!.size).toEqual({ width: 7, height: 7 })
  })
})

describe('loadRuntimeSpriteSets', () => {
  it('maps the {art:[...]} envelope and registers each entry by key', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({
        art: [{ key: 'overlay_unit', baseUrl: '/assets/units/human/overlay_unit', manifest: MANIFEST }],
      }),
    })) as unknown as typeof fetch)

    const count = await loadRuntimeSpriteSets()
    expect(count).toBe(1)
    expect(getUnitSpriteSet('overlay_unit')).not.toBeNull()
  })

  it('a failed fetch registers nothing and does not throw', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 500 })) as unknown as typeof fetch)
    await expect(loadRuntimeSpriteSets()).resolves.toBe(0)
  })

  it('a key that disappears from the catalog on reload stops shadowing', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({
        art: [{ key: 'a_unit', baseUrl: '/assets/units/human/a_unit', manifest: MANIFEST }],
      }),
    })) as unknown as typeof fetch)
    await loadRuntimeSpriteSets()
    expect(getUnitSpriteSet('a_unit')).not.toBeNull()

    // Second catalog no longer contains 'a_unit' — e.g. the editor reverted
    // its override back to bundled art.
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({
        art: [{ key: 'b_unit', baseUrl: '/assets/units/human/b_unit', manifest: MANIFEST }],
      }),
    })) as unknown as typeof fetch)
    const count = await loadRuntimeSpriteSets()
    expect(count).toBe(1)
    expect(getUnitSpriteSet('b_unit')).not.toBeNull()
    // 'a_unit' has no bundled counterpart, so if it were still shadowing, this
    // would still resolve to the stale runtime set instead of falling through.
    expect(getUnitSpriteSet('a_unit')).toBeNull()
  })
})
