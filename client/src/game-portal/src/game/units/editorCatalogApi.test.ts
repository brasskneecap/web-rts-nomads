import { afterEach, describe, expect, it, vi } from 'vitest'
import { EditorValidationError } from './unitEditorApi'
import {
  deleteFaction,
  fetchAbilityIds,
  fetchArchetypes,
  fetchBuildingIds,
  fetchDamageTypes,
  fetchFactions,
  fetchProjectileIds,
  saveFaction,
  saveUnitArt,
} from './editorCatalogApi'

function jsonResponse(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  } as Response
}

// mockFetchOnce stubs global fetch to resolve once with the given JSON body,
// and hands back the mock so callers can assert on how fetch was invoked
// (url/method/body) when that matters.
function mockFetchOnce(body: unknown, status = 200): ReturnType<typeof vi.fn> {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse(body, status))
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('fetchFactions', () => {
  it('maps the {factions: [...]} envelope', async () => {
    const factions = [
      { id: 'human', displayName: 'Human', order: 1 },
      { id: 'orc', displayName: 'Orc' },
    ]
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ factions })))
    const result = await fetchFactions()
    expect(result).toEqual(factions)
  })
})

describe('deleteFaction', () => {
  it('throws EditorValidationError carrying the server message when the faction still owns units', async () => {
    const message = 'faction "human" still has 5 unit(s): acolyte, adept, archer, footman, knight'
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(jsonResponse({ error: 'validation_failed', message }, 400)),
    )
    await expect(deleteFaction('human')).rejects.toThrow(EditorValidationError)
    await expect(deleteFaction('human')).rejects.toThrow(/acolyte, adept, archer, footman, knight/)
  })
})

describe('fetchProjectileIds', () => {
  it('extracts the id field and returns it sorted', async () => {
    const projectiles = [{ id: 'fire_bolt' }, { id: 'arrow' }, { id: 'meteor' }]
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ projectiles })))
    const result = await fetchProjectileIds()
    expect(result).toEqual(['arrow', 'fire_bolt', 'meteor'])
  })
})

describe('fetchAbilityIds', () => {
  it('extracts the id field and sorts', async () => {
    mockFetchOnce({ abilities: [{ id: 'siphon_life' }, { id: 'greater_heal' }] })
    expect(await fetchAbilityIds()).toEqual(['greater_heal', 'siphon_life'])
  })
})

describe('fetchBuildingIds', () => {
  it('extracts the `type` field (NOT `id`) and sorts', async () => {
    // BuildingDef serializes its identifier as `type`, unlike ProjectileDef/AbilityDef
    // which use `id`. If this mapper is ever "corrected" to `.id`, the dropdown goes
    // silently empty — this test is the only thing that catches it.
    mockFetchOnce({ buildings: [{ type: 'barracks' }, { type: 'altar' }] })
    expect(await fetchBuildingIds()).toEqual(['altar', 'barracks'])
  })
})

describe('saveFaction', () => {
  it('POSTs the {faction} envelope', async () => {
    const faction = { id: 'human', displayName: 'Human', order: 1 }
    const fetchMock = mockFetchOnce({ id: 'human', status: 'saved' }, 201)
    await saveFaction(faction)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toContain('/factions')
    expect(init?.method).toBe('POST')
    expect(JSON.parse(init?.body as string)).toEqual({ faction })
  })

  it('surfaces a 400 validation_failed message', async () => {
    const message = 'faction id must be a valid slug'
    mockFetchOnce({ error: 'validation_failed', message }, 400)
    await expect(saveFaction({ id: '', displayName: 'Bad' })).rejects.toThrow(EditorValidationError)
    await expect(saveFaction({ id: '', displayName: 'Bad' })).rejects.toThrow(message)
  })
})

describe('saveUnitArt', () => {
  it('POSTs the {faction, unit, files} envelope', async () => {
    const payload = {
      faction: 'human',
      unit: 'moon_dancer',
      files: [{ name: 'sprites.json', contentBase64: 'eyJrZXkiOiJ4In0=' }],
    }
    const fetchMock = mockFetchOnce({ unit: 'moon_dancer', status: 'saved' }, 201)
    await saveUnitArt(payload)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toContain('/unit-art')
    expect(init?.method).toBe('POST')
    expect(JSON.parse(init?.body as string)).toEqual(payload)
  })

  it('surfaces a 400 art_rejected message as an EditorValidationError', async () => {
    const message = 'file "packed/../evil.png" is not an allowed art file'
    mockFetchOnce({ error: 'art_rejected', message }, 400)
    const payload = { faction: 'human', unit: 'u', files: [{ name: 'sprites.json', contentBase64: 'e30=' }] }
    await expect(saveUnitArt(payload)).rejects.toThrow(EditorValidationError)
    await expect(saveUnitArt(payload)).rejects.toThrow(message)
  })
})

describe('missing-key responses', () => {
  it('yields [] rather than throwing when a list endpoint response omits its key', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({})))
    await expect(fetchArchetypes()).resolves.toEqual([])
    await expect(fetchAbilityIds()).resolves.toEqual([])
    await expect(fetchDamageTypes()).resolves.toEqual([])
    await expect(fetchBuildingIds()).resolves.toEqual([])
  })
})
