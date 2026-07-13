// Browser-side companion to `spritePacking.ts`. That module is the pure
// layout core (blit-plan math, no pixels, no DOM); this module is the I/O
// shell around it — read a dropped PixelLab export folder, decode its frame
// PNGs via `createImageBitmap`, hand the decoded dims to `planSpriteSheets`,
// then rasterize each returned `SheetPlan` to a PNG `Blob` via canvas.
//
// Layout (sheet dimensions, blit coordinates, manifest shape) is NEVER
// recomputed here — it is delegated entirely to `planSpriteSheets`. This
// module only decodes pixels in and re-encodes pixels out.

import {
  normalizeExportShape,
  planSpriteSheets,
  type PixelLabMeta,
  type SheetPlan,
  type SpriteManifestJSON,
} from './spritePacking'

/** One file from a dropped folder. `path` is relative to the folder root —
 * i.e. `file.webkitRelativePath` with its first path segment (the dropped
 * wrapper folder's own name) stripped. */
export interface DroppedFile {
  path: string
  blob: Blob
}

/** Result of ingesting one PixelLab export folder: the derived manifest, the
 * rasterized sheet PNGs (keyed by their manifest-relative sheet name, e.g.
 * `packed/walking.png`), an optional portrait, and any non-fatal warnings. */
export interface IngestResult {
  manifest: SpriteManifestJSON
  sheets: { name: string; blob: Blob }[]
  portrait?: Blob
  warnings: string[]
}

// Collects every frame path referenced by (already state-normalized)
// metadata — every rotations value plus every animation frame array. This is
// the pre-decode inventory step: we need the full path list before we can
// check the drop for missing frames or decode anything.
function collectFramePaths(meta: PixelLabMeta): string[] {
  const out: string[] = []
  for (const rel of Object.values(meta.frames?.rotations ?? {})) {
    if (typeof rel === 'string') out.push(rel)
  }
  for (const byDir of Object.values(meta.frames?.animations ?? {})) {
    for (const rels of Object.values(byDir ?? {})) {
      if (Array.isArray(rels)) out.push(...rels)
    }
  }
  return out
}

// Prefers OffscreenCanvas (workers, modern browsers) and falls back to a
// detached <canvas> element — the Tauri webview and older engines may lack
// OffscreenCanvas entirely.
function makeCanvas(width: number, height: number): OffscreenCanvas | HTMLCanvasElement {
  if (typeof OffscreenCanvas !== 'undefined') {
    return new OffscreenCanvas(width, height)
  }
  const canvas = document.createElement('canvas')
  canvas.width = width
  canvas.height = height
  return canvas
}

function isOffscreenCanvas(canvas: OffscreenCanvas | HTMLCanvasElement): canvas is OffscreenCanvas {
  return typeof OffscreenCanvas !== 'undefined' && canvas instanceof OffscreenCanvas
}

async function canvasToPngBlob(canvas: OffscreenCanvas | HTMLCanvasElement): Promise<Blob> {
  if (isOffscreenCanvas(canvas)) {
    return canvas.convertToBlob({ type: 'image/png' })
  }
  return new Promise<Blob>((resolve, reject) => {
    canvas.toBlob((blob) => {
      if (blob) resolve(blob)
      else reject(new Error('spriteIngest: canvas.toBlob produced no blob'))
    }, 'image/png')
  })
}

// Rasterizes one SheetPlan to a PNG Blob. Layout is entirely pre-computed by
// `planSpriteSheets` — this only executes `drawImage` per blit at the
// coordinates it was given. Not unit-tested for pixel correctness (happy-dom
// has no working canvas); covered by the Task 5 E2E flow instead.
async function rasterizeSheet(plan: SheetPlan, bitmaps: Record<string, ImageBitmap>): Promise<Blob> {
  const canvas = makeCanvas(plan.width, plan.height)
  const ctx = canvas.getContext('2d') as CanvasRenderingContext2D | OffscreenCanvasRenderingContext2D | null
  if (!ctx) {
    throw new Error('spriteIngest: 2D canvas context unavailable')
  }
  ctx.imageSmoothingEnabled = false
  // Zero-init + explicit clear — matches the CLI's zero-init PNG buffer, so
  // trailing columns left unblitted by an uneven-frame-count direction (see
  // spritePacking.ts's planAnimation) stay transparent, not garbage.
  ctx.clearRect(0, 0, plan.width, plan.height)
  for (const b of plan.blits) {
    ctx.drawImage(bitmaps[b.srcKey], b.dstX, b.dstY)
  }
  return canvasToPngBlob(canvas)
}

/** Reads a dropped PixelLab export folder, decodes every referenced frame,
 * plans the sheet layout (via `planSpriteSheets`), and rasterizes each
 * planned sheet to a PNG Blob. Throws with an actionable message when the
 * drop is missing `metadata.json`, missing a frame the metadata references,
 * or has nothing to pack. Multi-state exports are packed from `states[0]`
 * (matching the CLI) and produce a warning rather than an error. */
export async function ingestExportFolder(files: DroppedFile[]): Promise<IngestResult> {
  const warnings: string[] = []

  const metaFile = files.find((f) => f.path === 'metadata.json')
  if (!metaFile) {
    throw new Error('No metadata.json in the dropped folder')
  }

  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- metadata.json is untyped JSON at the drop boundary
  const rawMeta = JSON.parse(await metaFile.blob.text()) as any
  if (Array.isArray(rawMeta?.states) && rawMeta.states.length > 1) {
    warnings.push('Multi-state export: only the first state was packed.')
  }
  const meta = normalizeExportShape(rawMeta as PixelLabMeta)

  const byPath = new Map(files.map((f) => [f.path, f]))
  const framePaths = collectFramePaths(meta)

  const frameDims: Record<string, { w: number; h: number }> = {}
  // Declared before the try so the finally below can close whatever got
  // decoded even if the decode loop itself throws partway through (missing
  // frame, or createImageBitmap rejecting) — not just failures in
  // planSpriteSheets/rasterizeSheet after decoding finished.
  const bitmaps: Record<string, ImageBitmap> = {}

  try {
    for (const framePath of framePaths) {
      const file = byPath.get(framePath)
      if (!file) {
        throw new Error(`Missing frame referenced by metadata: ${framePath}`)
      }
      const bitmap = await createImageBitmap(file.blob)
      bitmaps[framePath] = bitmap
      frameDims[framePath] = { w: bitmap.width, h: bitmap.height }
    }

    const plan = planSpriteSheets(meta, frameDims)
    if (plan.sheets.length === 0) {
      throw new Error('Nothing to pack: the export has no animations or rotations')
    }

    const sheets: { name: string; blob: Blob }[] = []
    for (const sheetPlan of plan.sheets) {
      const blob = await rasterizeSheet(sheetPlan, bitmaps)
      sheets.push({ name: sheetPlan.name, blob })
    }

    const portrait = byPath.get('portrait.png')?.blob

    return { manifest: plan.manifest, sheets, portrait, warnings }
  } finally {
    for (const bitmap of Object.values(bitmaps)) bitmap.close()
  }
}

/** Base64-encodes a Blob's raw bytes (no `data:...;base64,` prefix) — used to
 * build the `UnitArtUploadFile[]` for `saveUnitArt`. Goes through
 * `FileReader.readAsDataURL` rather than `btoa(String.fromCharCode(...))`:
 * spreading a large Uint8Array as call arguments risks a stack overflow on
 * big sheets, and FileReader's own base64 encoding avoids that entirely. */
export function blobToBase64(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(reader.error ?? new Error('blobToBase64: FileReader error'))
    reader.onload = () => {
      const result = reader.result as string
      // readAsDataURL yields "data:<mime>;base64,<payload>" — keep only the payload.
      const commaIndex = result.indexOf(',')
      resolve(commaIndex >= 0 ? result.slice(commaIndex + 1) : result)
    }
    reader.readAsDataURL(blob)
  })
}

/** Maps each rasterized sheet blob to an object URL keyed by its
 * manifest-relative name (e.g. `packed/walking.png`). Call `revoke()` on
 * save, discard, or unmount to free every URL — object URLs otherwise leak
 * for the page's lifetime. */
export function packedSheetToObjectUrls(result: IngestResult): { urls: Record<string, string>; revoke: () => void } {
  const urls: Record<string, string> = {}
  for (const sheet of result.sheets) {
    urls[sheet.name] = URL.createObjectURL(sheet.blob)
  }
  return {
    urls,
    revoke: () => {
      for (const url of Object.values(urls)) URL.revokeObjectURL(url)
    },
  }
}
