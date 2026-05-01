#!/usr/bin/env node
// Packs PixelLab per-frame unit sprites into a single 2D sheet per animation
// (columns = frames, rows = directions) and emits a derived sprites.json
// manifest. Run after dropping a new unit export into `src/assets/units/{unit}/`.
//
// Usage:  npm run pack:sprites
//
// Input  : src/assets/units/*/metadata.json  (PixelLab export, unmodified)
// Output : src/assets/units/*/packed/{animation}.png  (one sheet per animation)
//          src/assets/units/*/sprites.json            (loader input)
//
// The runtime loader consumes sprites.json and the packed sheets only; the
// raw animations/ frames can stay on disk (they aren't imported by Vite
// because nothing globs them) or be gitignored if you want the repo leaner.
// The loader still understands the legacy per-direction strip layout, so
// older packed units keep working without re-baking.

import { promises as fs } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { PNG } from 'pngjs'

const here = path.dirname(fileURLToPath(import.meta.url))
const scriptPath = fileURLToPath(import.meta.url)
const unitsRoot = path.resolve(here, '..', 'src', 'assets', 'units')
const objectsRoot = path.resolve(here, '..', 'src', 'assets', 'objects')

// --force bypasses the incremental change-detection and re-packs every folder.
// Use it after editing this script's output format or when re-packing an
// external toolchain's output that shares mtimes.
const FORCE = process.argv.slice(2).some((a) => a === '--force' || a === '-f')

// When this script itself is newer than a folder's packed manifest, re-pack —
// a change here (output format, new fields, bug fix) must flow through to
// every manifest, not just folders with touched source files.
let scriptMtimeMs = 0

// Canonical compass order for object idle frames. PixelLab exports an 8-way
// "rotations" set; treating them as a clockwise cycle gives a smooth in-place
// rotation/animation loop for static placeables like traps and barrels.
const OBJECT_IDLE_ORDER = [
  'north',
  'north-east',
  'east',
  'south-east',
  'south',
  'south-west',
  'west',
  'north-west',
]

async function readPng(file) {
  const buf = await fs.readFile(file)
  return PNG.sync.read(buf)
}

// Reads hand-editable override fields from a previously-generated sprites.json.
// Returns only the fields present in the prior file; callers spread the result
// into the new manifest so user overrides (scale / offsets / future tweaks)
// survive a re-pack without needing a separate config file.
async function readPreservedOverrides(dir) {
  const out = {}
  try {
    const raw = await fs.readFile(path.join(dir, 'sprites.json'), 'utf8')
    const parsed = JSON.parse(raw)
    if (typeof parsed.scale === 'number' && parsed.scale > 0) out.scale = parsed.scale
    if (typeof parsed.offsetX === 'number') out.offsetX = parsed.offsetX
    if (typeof parsed.offsetY === 'number') out.offsetY = parsed.offsetY
  } catch {
    /* no prior manifest or invalid JSON — nothing to preserve */
  }
  return out
}

// Reads the packedAt ISO timestamp from a folder's existing sprites.json.
// Returns 0 when the manifest is missing or unreadable, which forces a re-pack.
// Using packedAt (vs. sprites.json's fs mtime) is stable across `git checkout`
// — cloning the repo or switching branches resets file mtimes, but the
// embedded ISO string survives, so CI and fresh clones don't redundantly
// re-pack every folder on the first run.
async function readPackedAt(dir) {
  try {
    const raw = await fs.readFile(path.join(dir, 'sprites.json'), 'utf8')
    const parsed = JSON.parse(raw)
    const t = Date.parse(parsed?.packedAt ?? '')
    return Number.isFinite(t) ? t : 0
  } catch {
    return 0
  }
}

// Newest mtime (ms) across a list of candidate input files. Missing files are
// silently ignored — the existing pack flow already tolerates pruned frames.
async function newestMtimeMs(files) {
  let newest = 0
  for (const f of files) {
    try {
      const stat = await fs.stat(f)
      if (stat.mtimeMs > newest) newest = stat.mtimeMs
    } catch {
      /* missing — skip */
    }
  }
  return newest
}

// Decide whether a folder's sprites.json is current given a list of input
// files. Returns true when every input is older than (or equal to) the last
// packedAt timestamp AND the pack script itself hasn't changed since.
async function isUpToDate(dir, inputFiles) {
  if (FORCE) return false
  const packedAt = await readPackedAt(dir)
  if (packedAt === 0) return false
  if (scriptMtimeMs > packedAt) return false
  const newest = await newestMtimeMs(inputFiles)
  return newest <= packedAt
}

// Cardinals listed first so 4-direction units pack to a row layout that's
// bit-identical to the pre-8-way pipeline; diagonals (when present in the
// PixelLab export) tack onto the bottom rows. The runtime reads rowOrder
// from sprites.json, so the loader doesn't care about ordering — but
// keeping cardinals first minimizes git churn for existing units.
const DIRECTION_ORDER = [
  'north',
  'south',
  'east',
  'west',
  'north-east',
  'south-east',
  'south-west',
  'north-west',
]

// Packs all directions of a single animation into one 2D sheet — columns are
// animation frames, rows are directions (order recorded in rowOrder). Returns
// null when no direction has any frames, or `{ pruned: true }` when the
// animation's raw frames have been deleted since the last pack (caller is
// expected to carry the prior sprites.json entry over in that case).
async function packAnimation(unitDir, animSlug, byDir) {
  const dirs = DIRECTION_ORDER.filter((d) => Array.isArray(byDir[d]) && byDir[d].length > 0)
  if (dirs.length === 0) return null

  // Per-animation prune detection: if the first referenced frame of the first
  // direction is missing on disk, treat the whole animation as pruned. Avoids
  // bringing the whole run down when only some animations of a unit have had
  // their raw frames cleaned up.
  const probe = path.join(unitDir, byDir[dirs[0]][0])
  try {
    await fs.access(probe)
  } catch {
    return { pruned: true }
  }

  const framesByDir = {}
  let frameWidth = 0
  let frameHeight = 0
  let frameCount = 0

  for (const dir of dirs) {
    const pngs = []
    for (const rel of byDir[dir]) {
      pngs.push(await readPng(path.join(unitDir, rel)))
    }
    framesByDir[dir] = pngs
    frameCount = Math.max(frameCount, pngs.length)
    if (!frameWidth) {
      frameWidth = pngs[0].width
      frameHeight = pngs[0].height
    }
  }

  for (const dir of dirs) {
    for (const f of framesByDir[dir]) {
      if (f.width !== frameWidth || f.height !== frameHeight) {
        throw new Error(
          `frame size mismatch in ${unitDir}/${animSlug}/${dir}: expected ${frameWidth}x${frameHeight}, got ${f.width}x${f.height}`,
        )
      }
    }
  }

  const sheetW = frameWidth * frameCount
  const sheetH = frameHeight * dirs.length
  const sheet = new PNG({ width: sheetW, height: sheetH })

  for (let r = 0; r < dirs.length; r++) {
    const pngs = framesByDir[dirs[r]]
    for (let f = 0; f < pngs.length; f++) {
      const src = pngs[f]
      for (let y = 0; y < frameHeight; y++) {
        const srcStart = y * src.width * 4
        const dstStart = (r * frameHeight + y) * sheetW * 4 + f * frameWidth * 4
        src.data.copy(sheet.data, dstStart, srcStart, srcStart + frameWidth * 4)
      }
    }
  }

  const outDir = path.join(unitDir, 'packed')
  await fs.mkdir(outDir, { recursive: true })
  const outName = `${animSlug}.png`
  await fs.writeFile(path.join(outDir, outName), PNG.sync.write(sheet))

  // Remove legacy per-direction strips for this animation — they've been
  // superseded by the 2D sheet and would otherwise clutter the repo.
  for (const dir of DIRECTION_ORDER) {
    const legacy = path.join(outDir, `${animSlug}-${dir}.png`)
    try {
      await fs.unlink(legacy)
    } catch {
      /* not present — nothing to clean up */
    }
  }

  return {
    relPath: `packed/${outName}`,
    frameWidth,
    frameHeight,
    frameCount,
    rowOrder: dirs,
  }
}

function animSlugFromHashedName(name) {
  return name.split('-')[0].toLowerCase()
}

async function packUnit(unitDir) {
  const metaPath = path.join(unitDir, 'metadata.json')
  let meta
  try {
    meta = JSON.parse(await fs.readFile(metaPath, 'utf8'))
  } catch {
    return { skipped: true }
  }

  const unitKey = path.basename(unitDir)

  // Raw PixelLab frames may have been pruned after a prior pack (see header
  // comment — they're optional on disk). If the first referenced frame is
  // gone, assume this unit is already packed and leave its sprites.json alone.
  const firstFrame = Object.values(meta?.frames?.animations ?? {})
    .flatMap((byDir) => Object.values(byDir ?? {}))
    .find((rels) => Array.isArray(rels) && rels.length > 0)?.[0]
  if (firstFrame) {
    try {
      await fs.access(path.join(unitDir, firstFrame))
    } catch {
      return { alreadyPacked: true, unitKey }
    }
  }

  // Skip folders whose inputs haven't changed since the last pack. Includes
  // metadata.json, every rotation PNG, and every animation frame PNG — any of
  // them being touched (new animation, edited frame, PixelLab re-export)
  // invalidates the cached output. The script's own mtime is factored in so
  // a change to pack logic re-packs everything.
  const inputs = [metaPath]
  for (const rel of Object.values(meta?.frames?.rotations ?? {})) {
    if (typeof rel === 'string') inputs.push(path.join(unitDir, rel))
  }
  for (const byDir of Object.values(meta?.frames?.animations ?? {})) {
    for (const rels of Object.values(byDir ?? {})) {
      if (Array.isArray(rels)) {
        for (const rel of rels) inputs.push(path.join(unitDir, rel))
      }
    }
  }
  if (await isUpToDate(unitDir, inputs)) {
    return { upToDate: true, unitKey }
  }
  const size = {
    width: meta?.character?.size?.width ?? 64,
    height: meta?.character?.size?.height ?? 64,
  }

  const rotations = {}
  for (const [dir, rel] of Object.entries(meta?.frames?.rotations ?? {})) {
    if (typeof rel !== 'string') continue
    const source = path.join(unitDir, rel)
    try {
      await fs.access(source)
      rotations[dir] = rel
    } catch {
      console.warn(`[pack:sprites] ${unitKey}: missing rotation '${rel}' — skipping`)
    }
  }

  // Prior manifest — used to carry forward animation entries for animations
  // whose raw frames have been pruned since the last pack.
  let priorAnimations = {}
  try {
    const prior = JSON.parse(await fs.readFile(path.join(unitDir, 'sprites.json'), 'utf8'))
    if (prior && typeof prior.animations === 'object' && prior.animations) {
      priorAnimations = prior.animations
    }
  } catch {
    /* no prior manifest — nothing to carry forward */
  }

  const animations = {}
  for (const [hashedName, byDir] of Object.entries(meta?.frames?.animations ?? {})) {
    const slug = animSlugFromHashedName(hashedName)
    const result = await packAnimation(unitDir, slug, byDir ?? {})
    if (!result) continue
    if (result.pruned) {
      if (priorAnimations[slug]) {
        animations[slug] = priorAnimations[slug]
      } else {
        console.warn(`[pack:sprites] ${unitKey}: animation '${slug}' frames pruned and no prior manifest entry — dropping`)
      }
      continue
    }
    animations[slug] = {
      frameCount: result.frameCount,
      frameWidth: result.frameWidth,
      frameHeight: result.frameHeight,
      sheet: result.relPath,
      rowOrder: result.rowOrder,
    }
  }

  const manifest = {
    key: unitKey,
    size,
    rotations,
    animations,
    packedAt: new Date().toISOString(),
  }

  await fs.writeFile(
    path.join(unitDir, 'sprites.json'),
    JSON.stringify(manifest, null, 2) + '\n',
  )

  return {
    unitKey,
    rotations: Object.keys(rotations).length,
    animations: Object.keys(animations).length,
  }
}

// Packs a single horizontal strip from an ordered list of frame PNGs.
// Used by object packing (idle rotation strip + per-animation strips).
async function packFrameStrip(objectDir, outName, framePaths) {
  if (framePaths.length === 0) return null

  const frames = []
  for (const rel of framePaths) {
    frames.push(await readPng(path.join(objectDir, rel)))
  }

  const { width: frameWidth, height: frameHeight } = frames[0]
  for (const f of frames) {
    if (f.width !== frameWidth || f.height !== frameHeight) {
      throw new Error(
        `frame size mismatch in ${objectDir}/${outName}: expected ${frameWidth}x${frameHeight}, got ${f.width}x${f.height}`,
      )
    }
  }

  const strip = new PNG({ width: frameWidth * frames.length, height: frameHeight })
  for (let i = 0; i < frames.length; i++) {
    const src = frames[i]
    for (let y = 0; y < frameHeight; y++) {
      const srcStart = y * src.width * 4
      const dstStart = y * strip.width * 4 + i * frameWidth * 4
      src.data.copy(strip.data, dstStart, srcStart, srcStart + frameWidth * 4)
    }
  }

  const outDir = path.join(objectDir, 'packed')
  await fs.mkdir(outDir, { recursive: true })
  await fs.writeFile(path.join(outDir, outName), PNG.sync.write(strip))

  return {
    relPath: `packed/${outName}`,
    frameWidth,
    frameHeight,
    frameCount: frames.length,
  }
}

// Reassembles a 2D grid of frames into a single horizontal strip — the format
// the runtime loader expects. Used by packSimpleObject when a source PNG has
// more than one row of frames (common for tools that export 4×4 grids).
function unrollGridToStrip(grid, frameW, frameH, cols, rows) {
  const totalFrames = cols * rows
  const strip = new PNG({ width: frameW * totalFrames, height: frameH })
  for (let i = 0; i < totalFrames; i++) {
    const gridRow = Math.floor(i / cols)
    const gridCol = i % cols
    for (let y = 0; y < frameH; y++) {
      const srcY = gridRow * frameH + y
      const srcStart = (srcY * grid.width + gridCol * frameW) * 4
      const dstStart = (y * strip.width + i * frameW) * 4
      grid.data.copy(strip.data, dstStart, srcStart, srcStart + frameW * 4)
    }
  }
  return strip
}

// Strips an object-key prefix from a filename stem so animation keys stay
// short: "caltrops-electrified" in the caltrops/ folder becomes "electrified".
function deriveAnimKey(stem, objectKey) {
  const prefix = objectKey + '-'
  if (stem.startsWith(prefix)) return stem.slice(prefix.length)
  return stem
}

// Packs the pre-made sheets at an object root into per-animation horizontal
// strips. Used for simple objects that don't need the full PixelLab
// rotations/animations tree.
//
// Convention:
//   - sprite.png (required) — idle animation; its height defines the frame
//     size used for every other sheet in the folder. Typically a horizontal
//     strip (N × H), but a single H × H frame is fine (frameCount=1).
//   - <name>.png            — additional animation keyed by <name>; stripped
//     of the "<objectKey>-" prefix when present. Can be a horizontal strip
//     OR a 2D grid (any cols × rows of the canonical frame size); grids are
//     unrolled at pack time to a uniform horizontal strip so the runtime
//     loader never has to know the difference.
async function packSimpleObject(objectDir) {
  const objectKey = path.basename(objectDir)
  const spritePath = path.join(objectDir, 'sprite.png')
  try {
    await fs.access(spritePath)
  } catch {
    return null
  }

  // Skip when every PNG at the object root is older than the last pack.
  // Includes sprite.png plus any sibling animation sheets — renaming or
  // adding a new sheet bumps the directory entry mtime, but we stat the
  // individual files directly so orphaned packed/ PNGs from a prior run
  // don't get counted as inputs.
  const rootEntries = await fs.readdir(objectDir, { withFileTypes: true })
  const inputPngs = rootEntries
    .filter((e) => e.isFile() && e.name.toLowerCase().endsWith('.png'))
    .map((e) => path.join(objectDir, e.name))
  if (await isUpToDate(objectDir, inputPngs)) {
    return { upToDate: true }
  }

  // sprite.png's height is the canonical frame size. All other sheets in
  // this folder must be an integer multiple of it on both axes.
  const spritePng = await readPng(spritePath)
  const frameSize = spritePng.height

  const outDir = path.join(objectDir, 'packed')
  await fs.mkdir(outDir, { recursive: true })

  // Clear out stale packed outputs so renamed/removed source PNGs don't
  // leave orphans behind.
  try {
    for (const entry of await fs.readdir(outDir)) {
      if (entry.endsWith('.png')) {
        await fs.unlink(path.join(outDir, entry))
      }
    }
  } catch { /* outDir just created — nothing to clean */ }

  const animations = {}

  const entries = await fs.readdir(objectDir, { withFileTypes: true })
  for (const entry of entries) {
    if (!entry.isFile() || !entry.name.toLowerCase().endsWith('.png')) continue

    const stem = entry.name.slice(0, -4)
    const animKey = entry.name === 'sprite.png'
      ? 'idle'
      : deriveAnimKey(stem, objectKey)

    const png = entry.name === 'sprite.png'
      ? spritePng
      : await readPng(path.join(objectDir, entry.name))

    const cols = Math.max(1, Math.floor(png.width / frameSize))
    const rows = Math.max(1, Math.floor(png.height / frameSize))
    const frameCount = cols * rows

    const strip = rows > 1
      ? unrollGridToStrip(png, frameSize, frameSize, cols, rows)
      : png

    const outName = `${animKey}.png`
    await fs.writeFile(path.join(outDir, outName), PNG.sync.write(strip))

    animations[animKey] = {
      frameCount,
      frameWidth: frameSize,
      frameHeight: frameSize,
      sheet: `packed/${outName}`,
      loop: true,
    }
  }

  if (!animations.idle) return null

  return {
    size: { width: frameSize, height: frameSize },
    animations,
  }
}

// Packs a PixelLab object export. Unlike units, objects don't have per-direction
// strips — PixelLab's `rotations` entries are treated as ordered idle-animation
// frames (barrel rotating in place, lantern flickering, etc.), and each named
// animation is expected to have a single 'south' direction since objects face a
// single canonical pose.
//
// Falls through to packSimpleObject when there's no metadata.json but a
// sprite.png strip exists — covers the "single sheet, single looping
// animation" case for lightweight placeables.
async function packObject(objectDir) {
  const objectKey = path.basename(objectDir)
  const metaPath = path.join(objectDir, 'metadata.json')
  // Preserve hand-edited override fields (scale, offsetX, offsetY) from a
  // prior sprites.json so re-packing doesn't wipe intentional tweaks.
  const preserved = await readPreservedOverrides(objectDir)
  let meta
  try {
    meta = JSON.parse(await fs.readFile(metaPath, 'utf8'))
  } catch {
    const simple = await packSimpleObject(objectDir)
    if (!simple) return { skipped: true }
    if (simple.upToDate) return { upToDate: true, objectKey }
    const manifest = {
      key: objectKey,
      size: simple.size,
      animations: simple.animations,
      ...preserved,
      packedAt: new Date().toISOString(),
    }
    await fs.writeFile(
      path.join(objectDir, 'sprites.json'),
      JSON.stringify(manifest, null, 2) + '\n',
    )
    return {
      objectKey,
      animations: Object.keys(simple.animations).length,
    }
  }

  const firstFrame = meta?.frames?.rotations?.south
    ?? Object.values(meta?.frames?.rotations ?? {})[0]
    ?? Object.values(meta?.frames?.animations ?? {})
      .flatMap((byDir) => byDir?.south ?? [])[0]
  if (firstFrame) {
    try {
      await fs.access(path.join(objectDir, firstFrame))
    } catch {
      return { alreadyPacked: true, objectKey }
    }
  }

  // Same incremental check as packUnit — metadata + every referenced raw frame.
  const objInputs = [metaPath]
  for (const rel of Object.values(meta?.frames?.rotations ?? {})) {
    if (typeof rel === 'string') objInputs.push(path.join(objectDir, rel))
  }
  for (const byDir of Object.values(meta?.frames?.animations ?? {})) {
    for (const rels of Object.values(byDir ?? {})) {
      if (Array.isArray(rels)) {
        for (const rel of rels) objInputs.push(path.join(objectDir, rel))
      }
    }
  }
  if (await isUpToDate(objectDir, objInputs)) {
    return { upToDate: true, objectKey }
  }

  const size = {
    width: meta?.character?.size?.width ?? 32,
    height: meta?.character?.size?.height ?? 32,
  }

  // Prior manifest — used to carry forward animation entries for animations
  // whose raw frames have been pruned since the last pack.
  let priorAnimations = {}
  try {
    const prior = JSON.parse(await fs.readFile(path.join(objectDir, 'sprites.json'), 'utf8'))
    if (prior && typeof prior.animations === 'object' && prior.animations) {
      priorAnimations = prior.animations
    }
  } catch {
    /* no prior manifest — nothing to carry forward */
  }

  const animations = {}

  // Idle — one strip made from the 8 rotation images, in canonical compass
  // order, skipping any direction the export omitted.
  const rotations = meta?.frames?.rotations ?? {}
  const idleFrames = OBJECT_IDLE_ORDER
    .map((d) => (typeof rotations[d] === 'string' ? rotations[d] : null))
    .filter((rel) => rel != null)
  if (idleFrames.length > 0) {
    const idle = await packFrameStrip(objectDir, 'idle.png', idleFrames)
    if (idle) {
      animations.idle = {
        frameCount: idle.frameCount,
        frameWidth: idle.frameWidth,
        frameHeight: idle.frameHeight,
        sheet: idle.relPath,
        loop: true,
      }
    }
  }

  // Named animations — each animation has a single 'south' direction. When an
  // animation's raw frames have been pruned since the last pack, carry the
  // prior manifest entry forward instead of erroring out.
  for (const [hashedName, byDir] of Object.entries(meta?.frames?.animations ?? {})) {
    const slug = animSlugFromHashedName(hashedName)
    const frames = Array.isArray(byDir?.south) ? byDir.south : null
    if (!frames || frames.length === 0) continue
    try {
      await fs.access(path.join(objectDir, frames[0]))
    } catch {
      if (priorAnimations[slug]) {
        animations[slug] = priorAnimations[slug]
      } else {
        console.warn(`[pack:sprites] object ${objectKey}: animation '${slug}' frames pruned and no prior manifest entry — dropping`)
      }
      continue
    }
    const strip = await packFrameStrip(objectDir, `${slug}.png`, frames)
    if (!strip) continue
    animations[slug] = {
      frameCount: strip.frameCount,
      frameWidth: strip.frameWidth,
      frameHeight: strip.frameHeight,
      sheet: strip.relPath,
      loop: false,
    }
  }

  const manifest = {
    key: objectKey,
    size,
    animations,
    ...preserved,
    packedAt: new Date().toISOString(),
  }

  await fs.writeFile(
    path.join(objectDir, 'sprites.json'),
    JSON.stringify(manifest, null, 2) + '\n',
  )

  return {
    objectKey,
    animations: Object.keys(animations).length,
  }
}

async function main() {
  try {
    scriptMtimeMs = (await fs.stat(scriptPath)).mtimeMs
  } catch {
    scriptMtimeMs = 0
  }

  let entries
  try {
    entries = await fs.readdir(unitsRoot, { withFileTypes: true })
  } catch (err) {
    console.error(`[pack:sprites] cannot read ${unitsRoot}:`, err.message)
    process.exit(1)
  }

  let packed = 0
  let upToDate = 0
  for (const entry of entries) {
    if (!entry.isDirectory()) continue
    const unitDir = path.join(unitsRoot, entry.name)
    const result = await packUnit(unitDir)
    if (result.skipped) {
      console.log(`[pack:sprites] ${entry.name}: no metadata.json — skipped`)
      continue
    }
    if (result.alreadyPacked) {
      console.log(`[pack:sprites] ${result.unitKey}: raw frames pruned — leaving existing sprites.json`)
      continue
    }
    if (result.upToDate) {
      upToDate += 1
      continue
    }
    packed += 1
    console.log(
      `[pack:sprites] ${result.unitKey}: ${result.rotations} rotations, ${result.animations} animations`,
    )
  }

  // ── Objects (explosive_trap, future placeables) ────────────────────────────
  let objectEntries = []
  try {
    objectEntries = await fs.readdir(objectsRoot, { withFileTypes: true })
  } catch {
    /* objects root missing — no objects to pack yet */
  }

  let packedObjects = 0
  let upToDateObjects = 0
  for (const entry of objectEntries) {
    if (!entry.isDirectory()) continue
    const objectDir = path.join(objectsRoot, entry.name)
    const result = await packObject(objectDir)
    if (result.skipped) {
      console.log(`[pack:sprites] object ${entry.name}: no metadata.json — skipped`)
      continue
    }
    if (result.alreadyPacked) {
      console.log(`[pack:sprites] object ${result.objectKey}: raw frames pruned — leaving existing sprites.json`)
      continue
    }
    if (result.upToDate) {
      upToDateObjects += 1
      continue
    }
    packedObjects += 1
    console.log(`[pack:sprites] object ${result.objectKey}: ${result.animations} animations`)
  }

  const cacheSummary = upToDate + upToDateObjects > 0
    ? ` (${upToDate} unit(s), ${upToDateObjects} object(s) up-to-date — pass --force to re-pack)`
    : ''
  console.log(
    `[pack:sprites] done — packed ${packed} unit(s), ${packedObjects} object(s)${cacheSummary}`,
  )
}

main().catch((err) => {
  console.error(err)
  process.exit(1)
})
