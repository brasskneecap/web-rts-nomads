// Covers the READER + VALIDATION contract of `spriteIngest.ts` — path
// matching against the dropped folder, missing-frame detection, multi-state
// warning, and error propagation from `planSpriteSheets`. happy-dom has no
// working canvas / createImageBitmap, so:
//   - `createImageBitmap` is stubbed to report dims read off the blob's own
//     text content (`{w,h}` JSON) — the test controls dims per-frame by
//     writing them into that frame's blob, no identity maps needed.
//   - The canvas path is stubbed at the DOM API boundary
//     (`HTMLCanvasElement.prototype.getContext` / `.toBlob`), not by
//     restructuring `spriteIngest.ts` — so the real reader/validation code
//     runs unmodified and `rasterizeSheet`'s blit loop genuinely executes
//     (against a no-op 2D context), it just never touches real pixels.
// Rasterized pixel *output* is intentionally NOT asserted here — that's
// E2E-only (Task 5).
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { blobToBase64, ingestExportFolder, packedSheetToObjectUrls, type DroppedFile } from './spriteIngest'

// A "frame" blob whose only content is its own dims, read back by the
// createImageBitmap stub below. Content is otherwise meaningless.
function frameBlob(w: number, h: number): Blob {
  return new Blob([JSON.stringify({ w, h })], { type: 'text/plain' })
}

function jsonBlob(value: unknown): Blob {
  return new Blob([JSON.stringify(value)], { type: 'application/json' })
}

function metaFile(meta: unknown): DroppedFile {
  return { path: 'metadata.json', blob: jsonBlob(meta) }
}

beforeEach(() => {
  vi.stubGlobal(
    'createImageBitmap',
    vi.fn(async (blob: Blob) => {
      const { w, h } = JSON.parse(await blob.text()) as { w: number; h: number }
      return { width: w, height: h, close(): void {} } as unknown as ImageBitmap
    }),
  )

  // Stub the canvas 2D path so rasterizeSheet's blit loop runs for real
  // (proving it doesn't throw / mis-sequence) without needing a working
  // canvas backend. happy-dom lacks OffscreenCanvas, so spriteIngest's
  // makeCanvas() falls back to document.createElement('canvas') — stub that
  // element's prototype methods directly.
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({
    imageSmoothingEnabled: true,
    clearRect: vi.fn(),
    drawImage: vi.fn(),
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- minimal stub context, not a real CanvasRenderingContext2D
  } as any)
  vi.spyOn(HTMLCanvasElement.prototype, 'toBlob').mockImplementation((cb) => {
    cb(new Blob(['dummy-png-bytes'], { type: 'image/png' }))
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('ingestExportFolder', () => {
  it('throws when metadata.json is missing', async () => {
    const files: DroppedFile[] = [{ path: 'rotations/south.png', blob: frameBlob(4, 4) }]
    await expect(ingestExportFolder(files)).rejects.toThrow('No metadata.json in the dropped folder')
  })

  it('throws naming a frame referenced by metadata but absent from the drop', async () => {
    const meta = {
      character: { size: { width: 4, height: 4 } },
      frames: { rotations: { south: 'rotations/south.png' }, animations: {} },
    }
    const files: DroppedFile[] = [metaFile(meta)] // south.png never provided
    await expect(ingestExportFolder(files)).rejects.toThrow(/rotations\/south\.png/)
  })

  it('closes bitmaps decoded before a later missing-frame throw (no leak on the decode-error path)', async () => {
    // north decodes successfully; south is referenced but absent from the
    // drop, so the decode loop throws partway through. The already-decoded
    // north ImageBitmap must still be closed by the outer finally.
    const closeMock = vi.fn()
    vi.stubGlobal(
      'createImageBitmap',
      vi.fn(async (blob: Blob) => {
        const { w, h } = JSON.parse(await blob.text()) as { w: number; h: number }
        return { width: w, height: h, close: closeMock } as unknown as ImageBitmap
      }),
    )

    const meta = {
      character: { size: { width: 4, height: 4 } },
      frames: { rotations: { north: 'rotations/north.png', south: 'rotations/south.png' }, animations: {} },
    }
    const files: DroppedFile[] = [metaFile(meta), { path: 'rotations/north.png', blob: frameBlob(4, 4) }]

    await expect(ingestExportFolder(files)).rejects.toThrow(/rotations\/south\.png/)
    expect(closeMock).toHaveBeenCalledTimes(1)
  })

  it('warns (does not throw) on a multi-state export and packs states[0]', async () => {
    const state0 = {
      character: { size: { width: 4, height: 4 } },
      frames: { rotations: { north: 'rotations/north.png', south: 'rotations/south.png' }, animations: {} },
    }
    const state1 = { character: { size: { width: 4, height: 4 } }, frames: { rotations: {}, animations: {} } }
    const files: DroppedFile[] = [
      metaFile({ states: [state0, state1] }),
      { path: 'rotations/north.png', blob: frameBlob(4, 4) },
      { path: 'rotations/south.png', blob: frameBlob(4, 4) },
    ]
    const result = await ingestExportFolder(files)
    expect(result.warnings).toContain('Multi-state export: only the first state was packed.')
    expect(result.sheets.map((s) => s.name)).toContain('packed/rotations.png')
  })

  it('throws when there is nothing to pack (no animations, no rotations)', async () => {
    const meta = { character: { size: { width: 4, height: 4 } }, frames: { rotations: {}, animations: {} } }
    const files: DroppedFile[] = [metaFile(meta)]
    await expect(ingestExportFolder(files)).rejects.toThrow(/nothing to pack/i)
  })

  it('produces one sheet per animation plus rotations, keyed by manifest path', async () => {
    const meta = {
      character: { size: { width: 4, height: 4 } },
      frames: {
        rotations: { north: 'rotations/north.png', south: 'rotations/south.png' },
        animations: { 'Walking-abc': { north: ['anim/walk/n0.png'], south: ['anim/walk/s0.png'] } },
      },
    }
    const files: DroppedFile[] = [
      metaFile(meta),
      { path: 'rotations/north.png', blob: frameBlob(4, 4) },
      { path: 'rotations/south.png', blob: frameBlob(4, 4) },
      { path: 'anim/walk/n0.png', blob: frameBlob(4, 4) },
      { path: 'anim/walk/s0.png', blob: frameBlob(4, 4) },
    ]
    const result = await ingestExportFolder(files)
    const names = result.sheets.map((s) => s.name).sort()
    expect(names).toEqual(['packed/rotations.png', 'packed/walking.png'])
    expect(result.manifest.animations.walking).toBeDefined()
    expect(result.manifest.rotations).toBeDefined()
    for (const sheet of result.sheets) expect(sheet.blob).toBeInstanceOf(Blob)
  })

  it('propagates a frame-size mismatch from the core', async () => {
    const meta = {
      character: { size: { width: 4, height: 4 } },
      frames: {
        rotations: {},
        animations: { 'Walk-x': { north: ['anim/n0.png'], south: ['anim/s0.png'] } },
      },
    }
    const files: DroppedFile[] = [
      metaFile(meta),
      { path: 'anim/n0.png', blob: frameBlob(4, 4) },
      { path: 'anim/s0.png', blob: frameBlob(4, 5) }, // mismatched height
    ]
    await expect(ingestExportFolder(files)).rejects.toThrow(/mismatch/i)
  })

  it('keeps an optional portrait.png blob when present, and omits it when absent', async () => {
    const meta = {
      character: { size: { width: 4, height: 4 } },
      frames: { rotations: { north: 'rotations/north.png' }, animations: {} },
    }
    const withPortrait = await ingestExportFolder([
      metaFile(meta),
      { path: 'rotations/north.png', blob: frameBlob(4, 4) },
      { path: 'portrait.png', blob: new Blob(['portrait-bytes'], { type: 'image/png' }) },
    ])
    expect(withPortrait.portrait).toBeInstanceOf(Blob)

    const withoutPortrait = await ingestExportFolder([
      metaFile(meta),
      { path: 'rotations/north.png', blob: frameBlob(4, 4) },
    ])
    expect(withoutPortrait.portrait).toBeUndefined()
  })
})

describe('blobToBase64', () => {
  it('round-trips binary bytes (including a null byte and high bytes) without corruption', async () => {
    const bytes = new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x00, 0xff, 0x10])
    const encoded = await blobToBase64(new Blob([bytes]))
    expect(Buffer.from(encoded, 'base64')).toEqual(Buffer.from(bytes))
  })

  it('strips the data:...;base64, prefix rather than including it', async () => {
    const encoded = await blobToBase64(new Blob(['hello'], { type: 'text/plain' }))
    expect(encoded).not.toContain('data:')
    expect(encoded).not.toContain(',')
    expect(Buffer.from(encoded, 'base64').toString()).toBe('hello')
  })
})

describe('packedSheetToObjectUrls', () => {
  it('creates one object URL per sheet, keyed by sheet name, and revoke() frees every URL', () => {
    const createSpy = vi.spyOn(URL, 'createObjectURL').mockImplementation(
      (obj: Blob | MediaSource) => `blob:mock/${(obj as Blob).size}-${Math.random()}`,
    )
    const revokeSpy = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})

    const result = {
      manifest: { size: { width: 4, height: 4 }, animations: {} },
      sheets: [
        { name: 'packed/walking.png', blob: new Blob(['a']) },
        { name: 'packed/rotations.png', blob: new Blob(['b']) },
      ],
      warnings: [],
    }
    const { urls, revoke } = packedSheetToObjectUrls(result)
    expect(Object.keys(urls).sort()).toEqual(['packed/rotations.png', 'packed/walking.png'])
    expect(createSpy).toHaveBeenCalledTimes(2)

    revoke()
    expect(revokeSpy).toHaveBeenCalledTimes(2)
    expect(revokeSpy).toHaveBeenCalledWith(urls['packed/walking.png'])
    expect(revokeSpy).toHaveBeenCalledWith(urls['packed/rotations.png'])
  })
})
