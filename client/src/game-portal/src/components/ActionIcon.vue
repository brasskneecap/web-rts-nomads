<template>
  <canvas v-if="action.iconDef" ref="canvasEl" width="64" height="64" class="action-icon" />
  <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" class="action-icon">
    <path :d="getActionIcon(action.id)" />
  </svg>
</template>

<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import type { ActionItem } from '@/game/core/GameState'
import { BUILDING_DEF_MAP } from '@/game/maps/buildingDefs'
import { UNIT_DEF_MAP } from '@/game/maps/unitDefs'
import { ACTION_ICON_MAP } from '@/game/maps/actionIconDefs'

const props = defineProps<{
  action: ActionItem
}>()

const canvasEl = ref<HTMLCanvasElement | null>(null)

const CANVAS_SIZE = 64
const PADDING = 5
const DRAW_SIZE = CANVAS_SIZE - PADDING * 2
// Default color used for 'player'-tinted layers in icons
const ICON_PLAYER_COLOR = '#3b82f6'

function drawBuilding(ctx: CanvasRenderingContext2D, type: string) {
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

function drawUnit(ctx: CanvasRenderingContext2D, type: string) {
  const def = UNIT_DEF_MAP.get(type)
  if (!def?.render) return

  const { render } = def

  // Compute bounding box of the unit's pixel-space layers
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity
  for (const layer of render.layers) {
    if (layer.kind === 'circle') {
      minX = Math.min(minX, layer.cx - layer.r)
      minY = Math.min(minY, layer.cy - layer.r)
      maxX = Math.max(maxX, layer.cx + layer.r)
      maxY = Math.max(maxY, layer.cy + layer.r)
    } else if (layer.kind === 'poly') {
      for (const [px, py] of layer.points) {
        minX = Math.min(minX, px)
        minY = Math.min(minY, py)
        maxX = Math.max(maxX, px)
        maxY = Math.max(maxY, py)
      }
    }
  }
  if (!isFinite(minX)) return

  const unitW = maxX - minX
  const unitH = maxY - minY
  const scale = DRAW_SIZE / Math.max(unitW, unitH, 1)
  const cx = PADDING + DRAW_SIZE / 2 - ((minX + maxX) / 2) * scale
  const cy = PADDING + DRAW_SIZE / 2 - ((minY + maxY) / 2) * scale

  for (const layer of render.layers) {
    ctx.fillStyle = layer.color === 'player' ? ICON_PLAYER_COLOR : layer.color
    if (layer.kind === 'circle') {
      ctx.beginPath()
      ctx.arc(cx + layer.cx * scale, cy + layer.cy * scale, layer.r * scale, 0, Math.PI * 2)
      ctx.fill()
    } else if (layer.kind === 'poly') {
      ctx.beginPath()
      ctx.moveTo(cx + layer.points[0][0] * scale, cy + layer.points[0][1] * scale)
      for (let i = 1; i < layer.points.length; i++) {
        ctx.lineTo(cx + layer.points[i][0] * scale, cy + layer.points[i][1] * scale)
      }
      ctx.closePath()
      ctx.fill()
    }
  }
}

function draw() {
  const canvas = canvasEl.value
  if (!canvas) return
  const ctx = canvas.getContext('2d')
  if (!ctx) return

  ctx.clearRect(0, 0, CANVAS_SIZE, CANVAS_SIZE)

  const { iconDef } = props.action
  if (!iconDef) return

  if (iconDef.kind === 'building') {
    drawBuilding(ctx, iconDef.type)
  } else if (iconDef.kind === 'unit') {
    drawUnit(ctx, iconDef.type)
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
  width: 58%;
  height: 58%;
}
</style>
