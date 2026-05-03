<template>
  <canvas v-if="useCanvas" ref="canvasEl" width="64" height="64" class="action-icon" />
  <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" class="action-icon">
    <path :d="getActionIcon(action.iconId ?? action.id)" />
  </svg>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import type { ActionItem } from '@/game/core/GameState'
import { BUILDING_DEF_MAP } from '@/game/maps/buildingDefs'
import { ITEM_DEF_MAP } from '@/game/maps/itemDefs'
import { ACTION_ICON_MAP } from '@/game/maps/actionIconDefs'
import { getBuildingSpriteImage } from '@/game/rendering/buildingSprites'
import { getUnitSpriteSet } from '@/game/rendering/unitSprites'
import { getActionIconImage } from '@/game/rendering/actionIconSprites'
import { getItemAssetImage } from '@/game/rendering/itemAssets'
import { getItemCatalogImage } from '@/game/rendering/itemCatalogImages'

const props = defineProps<{
  action: ActionItem
}>()

const canvasEl = ref<HTMLCanvasElement | null>(null)

const CANVAS_SIZE = 64
const PADDING = 5
const DRAW_SIZE = CANVAS_SIZE - PADDING * 2

function drawBuildingSprite(ctx: CanvasRenderingContext2D, img: HTMLImageElement) {
  ctx.imageSmoothingEnabled = false
  const spritePadding = 1
  const boxSize = CANVAS_SIZE - spritePadding * 2
  const scale = boxSize / Math.max(img.naturalWidth, img.naturalHeight)
  const w = img.naturalWidth * scale
  const h = img.naturalHeight * scale
  const x = spritePadding + (boxSize - w) / 2
  const y = spritePadding + (boxSize - h) / 2
  ctx.drawImage(img, x, y, w, h)
}

function drawBuilding(ctx: CanvasRenderingContext2D, type: string) {
  const sprite = getBuildingSpriteImage(type)
  if (sprite) {
    if (sprite.complete && sprite.naturalWidth > 0) {
      drawBuildingSprite(ctx, sprite)
      return
    }
    sprite.addEventListener('load', () => draw(), { once: true })
    // Fall through to procedural while the image loads.
  }

  const def = BUILDING_DEF_MAP.get(type)
  if (!def?.render) return

  const { render, width: bW, height: bH, color: playerColor } = def
  const cellSize = DRAW_SIZE / Math.max(bW, bH)
  const offsetX = PADDING + (DRAW_SIZE - bW * cellSize) / 2
  const offsetY = PADDING + (DRAW_SIZE - bH * cellSize) / 2

  for (const layer of render.layers) {
    ctx.fillStyle = layer.color === 'player' ? playerColor : layer.color
    if (!('kind' in layer) || layer.kind === 'rect') {
      ctx.fillRect(
        offsetX + layer.x * cellSize,
        offsetY + layer.y * cellSize,
        layer.w * cellSize,
        layer.h * cellSize,
      )
    } else if (layer.kind === 'tri') {
      const s = cellSize / 6
      const tlX = offsetX + layer.cx * cellSize + layer.sc * s
      const tlY = offsetY + layer.cy * cellSize + layer.sr * s
      const bslash = (layer.sc + layer.sr) % 2 === 1
      ctx.beginPath()
      if (!bslash) {
        if (layer.h === 0) { ctx.moveTo(tlX,     tlY); ctx.lineTo(tlX + s, tlY); ctx.lineTo(tlX,     tlY + s) }
        else               { ctx.moveTo(tlX + s, tlY); ctx.lineTo(tlX + s, tlY + s); ctx.lineTo(tlX, tlY + s) }
      } else {
        if (layer.h === 0) { ctx.moveTo(tlX,     tlY); ctx.lineTo(tlX + s, tlY); ctx.lineTo(tlX + s, tlY + s) }
        else               { ctx.moveTo(tlX,     tlY); ctx.lineTo(tlX,     tlY + s); ctx.lineTo(tlX + s, tlY + s) }
      }
      ctx.closePath()
      ctx.fill()
      ctx.strokeStyle = ctx.fillStyle as string
      ctx.lineWidth = 0.5
      ctx.stroke()
    }
  }
}

function drawUnitSprite(ctx: CanvasRenderingContext2D, img: HTMLImageElement) {
  ctx.imageSmoothingEnabled = false
  const spritePadding = 1
  const boxSize = CANVAS_SIZE - spritePadding * 2
  const scale = boxSize / Math.max(img.naturalWidth, img.naturalHeight)
  const w = img.naturalWidth * scale
  const h = img.naturalHeight * scale
  const x = spritePadding + (boxSize - w) / 2
  const y = spritePadding + (boxSize - h) / 2
  ctx.drawImage(img, x, y, w, h)
}

function drawUnit(ctx: CanvasRenderingContext2D, type: string) {
  const spriteSet = getUnitSpriteSet(type)
  const portrait = spriteSet?.rotations.south ?? spriteSet?.rotations.north
    ?? spriteSet?.rotations.east ?? spriteSet?.rotations.west
  if (!portrait) return
  if (portrait.complete && portrait.naturalWidth > 0) {
    drawUnitSprite(ctx, portrait)
    return
  }
  portrait.addEventListener('load', () => draw(), { once: true })
}

function drawActionSprite(ctx: CanvasRenderingContext2D, img: HTMLImageElement) {
  ctx.imageSmoothingEnabled = false
  const spritePadding = 1
  const boxSize = CANVAS_SIZE - spritePadding * 2
  const scale = boxSize / Math.max(img.naturalWidth, img.naturalHeight)
  const w = img.naturalWidth * scale
  const h = img.naturalHeight * scale
  const x = spritePadding + (boxSize - w) / 2
  const y = spritePadding + (boxSize - h) / 2
  ctx.drawImage(img, x, y, w, h)
}

const useCanvas = computed(() => {
  if (props.action.iconDef) return true
  const lookupId = props.action.iconId ?? props.action.id
  return !!getActionIconImage(lookupId)
})

function draw() {
  const canvas = canvasEl.value
  if (!canvas) return
  const ctx = canvas.getContext('2d')
  if (!ctx) return

  ctx.clearRect(0, 0, CANVAS_SIZE, CANVAS_SIZE)

  const lookupId = props.action.iconId ?? props.action.id
  const sprite = getActionIconImage(lookupId)
  if (sprite) {
    if (sprite.complete && sprite.naturalWidth > 0) {
      drawActionSprite(ctx, sprite)
      return
    }
    sprite.addEventListener('load', () => draw(), { once: true })
    return
  }

  const { iconDef } = props.action
  if (!iconDef) return

  if (iconDef.kind === 'building') {
    drawBuilding(ctx, iconDef.type)
  } else if (iconDef.kind === 'unit') {
    drawUnit(ctx, iconDef.type)
  } else if (iconDef.kind === 'item') {
    const def = ITEM_DEF_MAP.get(iconDef.type)
    const iconKey = def?.iconKey ?? iconDef.type
    // 1. Bundled actions sprite (not currently used for items, reserved for future)
    const localImg = getActionIconImage(iconKey)
    if (localImg) {
      if (localImg.complete && localImg.naturalWidth > 0) {
        drawActionSprite(ctx, localImg)
        return
      }
      localImg.addEventListener('load', () => draw(), { once: true })
      return
    }
    // 2. Client-side bundled asset from assets/items/**/<iconKey>.png
    const assetImg = getItemAssetImage(iconKey)
    if (assetImg) {
      if (assetImg.complete && assetImg.naturalWidth > 0) {
        drawActionSprite(ctx, assetImg)
        return
      }
      assetImg.addEventListener('load', () => draw(), { once: true })
      return
    }
    // 3. Server catalog HTTP endpoint (embedded PNG next to the JSON).
    // Returns null while loading; draw() is called again when the image is ready.
    const catalogImg = getItemCatalogImage(iconDef.type, draw)
    if (catalogImg) {
      drawActionSprite(ctx, catalogImg)
      return
    }
    // Show placeholder while catalog image is loading or unavailable.
    const fallbackKey = def?.kind === 'consumable' ? 'set-spawn-point' : 'attack'
    const fallback = getActionIconImage(fallbackKey)
    if (fallback) {
      if (fallback.complete && fallback.naturalWidth > 0) {
        drawActionSprite(ctx, fallback)
        return
      }
      fallback.addEventListener('load', () => draw(), { once: true })
    }
  }
}

onMounted(draw)
watch(() => props.action, draw, { deep: false })

function getActionIcon(id: string): string {
  return ACTION_ICON_MAP.get(id) ?? 'M12 5v14 M5 12h14'
}
</script>

<style scoped>
.action-icon {
  width: 90%;
  height: 90%;
}
</style>
