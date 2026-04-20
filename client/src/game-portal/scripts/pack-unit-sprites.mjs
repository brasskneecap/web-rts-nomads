#!/usr/bin/env node
// Packs PixelLab per-frame unit sprites into horizontal strips and emits a
// derived sprites.json manifest. Run after dropping a new unit export into
// `src/assets/units/{unit}/`.
//
// Usage:  npm run pack:sprites
//
// Input  : src/assets/units/*/metadata.json  (PixelLab export, unmodified)
// Output : src/assets/units/*/packed/{animation}-{direction}.png  (strips)
//          src/assets/units/*/sprites.json                         (loader input)
//
// The runtime loader consumes sprites.json and the packed strips only; the
// raw animations/ frames can stay on disk (they aren't imported by Vite
// because nothing globs them) or be gitignored if you want the repo leaner.

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

async function packAnimationDirection(unitDir, animSlug, direction, framePaths) {
  if (framePaths.length === 0) return null

  const frames = []
  for (const rel of framePaths) {
    frames.push(await readPng(path.join(unitDir, rel)))
  }

  const { width: frameWidth, height: frameHeight } = frames[0]
  for (const f of frames) {
    if (f.width !== frameWidth || f.height !== frameHeight) {
      throw new Error(
        `frame size mismatch in ${unitDir}/${animSlug}/${direction}: expected ${frameWidth}x${frameHeight}, got ${f.width}x${f.height}`,
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

  const outDir = path.join(unitDir, 'packed')
  await fs.mkdir(outDir, { recursive: true })
  const outName = `${animSlug}-${direction}.png`
  await fs.writeFile(path.join(outDir, outName), PNG.sync.write(strip))

  return {
    relPath: `packed/${outName}`,
    frameWidth,
    frameHeight,
    frameCount: frames.length,
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
    const strips = {}
    let frameCount = 0
    let frameWidth = 0
    let frameHeight = 0

    for (const [dir, rels] of Object.entries(byDir ?? {})) {
      if (!Array.isArray(rels) || rels.length === 0) continue
      const result = await packAnimationDirection(unitDir, slug, dir, rels)
      if (!result) continue
      strips[dir] = result.relPath
      frameCount = Math.max(frameCount, result.frameCount)
      frameWidth = result.frameWidth
      frameHeight = result.frameHeight
    }

    if (Object.keys(strips).length > 0) {
      animations[slug] = { frameCount, frameWidth, frameHeight, strips }
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
