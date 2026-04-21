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
const unitsRoot = path.resolve(here, '..', 'src', 'assets', 'units')

async function readPng(file) {
  const buf = await fs.readFile(file)
  return PNG.sync.read(buf)
}

const DIRECTION_ORDER = ['north', 'south', 'east', 'west']

// Packs all directions of a single animation into one 2D sheet — columns are
// animation frames, rows are directions (order recorded in rowOrder). Returns
// null when no direction has any frames.
async function packAnimation(unitDir, animSlug, byDir) {
  const dirs = DIRECTION_ORDER.filter((d) => Array.isArray(byDir[d]) && byDir[d].length > 0)
  if (dirs.length === 0) return null

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

  const animations = {}
  for (const [hashedName, byDir] of Object.entries(meta?.frames?.animations ?? {})) {
    const slug = animSlugFromHashedName(hashedName)
    const result = await packAnimation(unitDir, slug, byDir ?? {})
    if (!result) continue
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

async function main() {
  let entries
  try {
    entries = await fs.readdir(unitsRoot, { withFileTypes: true })
  } catch (err) {
    console.error(`[pack:sprites] cannot read ${unitsRoot}:`, err.message)
    process.exit(1)
  }

  let packed = 0
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
    packed += 1
    console.log(
      `[pack:sprites] ${result.unitKey}: ${result.rotations} rotations, ${result.animations} animations`,
    )
  }

  console.log(`[pack:sprites] done — packed ${packed} unit(s)`)
}

main().catch((err) => {
  console.error(err)
  process.exit(1)
})
