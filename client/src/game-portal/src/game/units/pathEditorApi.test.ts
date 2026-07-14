import { afterEach, describe, expect, it, vi } from 'vitest'
import { EditorValidationError } from './unitEditorApi'
import {
  deletePath,
  deletePerks,
  fetchPaths,
  fetchPerkCatalog,
  savePath,
  savePerks,
} from './pathEditorApi'

function jsonResponse(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  } as Response
}

function mockFetchOnce(body: unknown, status = 200): ReturnType<typeof vi.fn> {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse(body, status))
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('fetchPaths', () => {
  it('GETs /catalog/paths and unwraps the {paths: [...]} envelope', async () => {
    const paths = [
      { unit: 'archer', path: 'gold', def: { path: 'gold', ranks: {} } },
      { unit: 'archer', path: 'silver', def: { path: 'silver', ranks: {} } },
    ]
    const fetchMock = mockFetchOnce({ paths })
    const result = await fetchPaths()
    expect(result).toEqual(paths)
    const [url] = fetchMock.mock.calls[0]
    expect(url).toContain('/catalog/paths')
  })
})

describe('savePath', () => {
  it('POSTs /paths with the exact {unit, path} body', async () => {
    const req = { unit: 'archer', path: { path: 'gold', ranks: {} } }
    const fetchMock = mockFetchOnce({ id: 'gold', status: 'saved' }, 201)
    await savePath(req)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toContain('/paths')
    expect(init?.method).toBe('POST')
    expect(JSON.parse(init?.body as string)).toEqual(req)
  })

  it('throws EditorValidationError with .serverMessage on a 400 validation_failed', async () => {
    const message = 'path id "gold" must match ^[a-z0-9_]+$'
    mockFetchOnce({ error: 'validation_failed', message }, 400)
    const req = { unit: 'archer', path: { path: 'GOLD!', ranks: {} } }
    await expect(savePath(req)).rejects.toThrow(EditorValidationError)
    mockFetchOnce({ error: 'validation_failed', message }, 400)
    try {
      await savePath(req)
      throw new Error('expected savePath to throw')
    } catch (err) {
      expect(err).toBeInstanceOf(EditorValidationError)
      expect((err as EditorValidationError).serverMessage).toBe(message)
    }
  })
})

describe('deletePath', () => {
  it('DELETEs /paths/{encoded id}', async () => {
    const fetchMock = mockFetchOnce({ id: 'gold rank', status: 'deleted' })
    const result = await deletePath('gold rank')
    expect(result.status).toBe('deleted')
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toContain('/paths/gold%20rank')
    expect(init?.method).toBe('DELETE')
  })

  it('throws EditorValidationError when the path is still referenced by pathChances', async () => {
    const message = 'path "gold" is still referenced by pathChances on: archer. Remove those rows first.'
    mockFetchOnce({ error: 'validation_failed', message }, 400)
    await expect(deletePath('gold')).rejects.toThrow(EditorValidationError)
  })
})

describe('savePerks', () => {
  it('POSTs /perks with the full {unit, path, rank, perks} body', async () => {
    const req = {
      unit: 'archer', path: 'gold', rank: 'bronze',
      perks: [{ id: 'piercing_shot', displayName: 'Piercing Shot', wired: true }],
    }
    const fetchMock = mockFetchOnce({ unit: 'archer', path: 'gold', rank: 'bronze', status: 'saved' }, 201)
    await savePerks(req)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toContain('/perks')
    expect(init?.method).toBe('POST')
    expect(JSON.parse(init?.body as string)).toEqual(req)
  })

  it('throws EditorValidationError on a duplicate perk id', async () => {
    const message = 'perk id "piercing_shot" is already used'
    mockFetchOnce({ error: 'validation_failed', message }, 400)
    const req = { unit: 'archer', path: 'gold', rank: 'bronze', perks: [{ id: 'piercing_shot', wired: false }] }
    await expect(savePerks(req)).rejects.toThrow(EditorValidationError)
  })
})

describe('deletePerks', () => {
  it('DELETEs the 3-segment /perks/{unit}/{path}/{rank} URL, encoding each segment', async () => {
    const fetchMock = mockFetchOnce({ unit: 'archer', path: 'gold', rank: 'bronze', status: 'deleted' })
    const result = await deletePerks('archer', 'gold', 'bronze')
    expect(result.status).toBe('deleted')
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toContain('/perks/archer/gold/bronze')
    expect(init?.method).toBe('DELETE')
  })
})

describe('fetchPerkCatalog', () => {
  it('GETs /catalog/perks, unwraps the envelope, and surfaces `wired`', async () => {
    const perks = [
      { id: 'piercing_shot', displayName: 'Piercing Shot', wired: true, unitType: 'archer', path: 'gold', rank: 'bronze' },
      { id: 'ghost_arrow', displayName: 'Ghost Arrow', wired: false, unitType: 'archer', path: 'silver', rank: 'silver' },
    ]
    const fetchMock = mockFetchOnce({ perks })
    const result = await fetchPerkCatalog()
    expect(result).toEqual(perks)
    expect(result[0].wired).toBe(true)
    expect(result[1].wired).toBe(false)
    const [url] = fetchMock.mock.calls[0]
    expect(url).toContain('/catalog/perks')
  })
})
