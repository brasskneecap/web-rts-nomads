import { describe, it, expect } from 'vitest'
import { gzipSync } from 'node:zlib'
import { decompressMapGz } from './mapContentCache'

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
