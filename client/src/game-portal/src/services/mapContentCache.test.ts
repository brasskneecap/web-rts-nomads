import { describe, it, expect } from 'vitest'
import { gzipSync } from 'node:zlib'
import { decompressMapGz, withTimeout } from './mapContentCache'

// withTimeout is the guard that keeps a stalled IndexedDB op (or a hung
// decompress) from holding the match-entry gate open forever — the bug behind
// the host's permanent grey screen. It must never hang and never reject.
describe('withTimeout', () => {
  it('falls back when the inner promise never settles', async () => {
    const never = new Promise<number>(() => {})
    expect(await withTimeout(never, 20, -1)).toBe(-1)
  })

  it('passes through a value that settles in time', async () => {
    expect(await withTimeout(Promise.resolve(42), 1000, -1)).toBe(42)
  })

  it('falls back (never rejects) when the inner promise rejects', async () => {
    expect(await withTimeout(Promise.reject(new Error('x')), 1000, -1)).toBe(-1)
  })
})

// decompressMapGz is the client inverse of the server's gzip+base64 map
// encoding (gzipMapConfig). This round-trips a server-shaped payload through it.
describe('decompressMapGz', () => {
  it('round-trips a gzipped+base64 MapConfig payload', async () => {
    const map = {
      id: 'smoke',
      name: 'Smoke',
      contentHash: 'sha256:abc',
      width: 64,
      height: 64,
      gridCols: 1,
      gridRows: 1,
      cellSize: 64,
      terrain: [],
      obstacles: [],
      buildings: [],
    }
    const mapGz = gzipSync(Buffer.from(JSON.stringify(map))).toString('base64')

    const out = await decompressMapGz(mapGz)

    expect(out.id).toBe('smoke')
    expect(out.contentHash).toBe('sha256:abc')
    expect(out.gridCols).toBe(1)
  })

  it('rejects malformed (non-gzip) input so callers can fall back', async () => {
    const garbage = Buffer.from('not gzip').toString('base64')
    await expect(decompressMapGz(garbage)).rejects.toBeDefined()
  })
})
