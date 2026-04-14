// src/game/rendering/CanvasRenderer.ts
import { GameState } from '../core/GameState'
import {
  DEFAULT_GRASS_COLOR,
  getBuildingColor,
  getObstacleColor,
  getTerrainColor,
} from '../maps/mapConfig'
import { Camera } from './Camera'

export type MinimapBounds = {
  x: number
  y: number
  width: number
  height: number
}

export function getMinimapBounds(
  canvasWidth: number,
  _canvasHeight: number,
  mapWidth: number,
  mapHeight: number,
): MinimapBounds {
  const padding = 18
  const maxWidth = 220
  const aspectRatio = mapWidth / mapHeight
  const width = Math.min(maxWidth, canvasWidth * 0.2)
  const height = width / aspectRatio

  return {
    x: canvasWidth - width - padding,
    y: padding,
    width,
    height,
  }
}

export class CanvasRenderer {
  private ctx: CanvasRenderingContext2D
  private canvas: HTMLCanvasElement
  private state: GameState
  private camera: Camera
  private resizeObserver: ResizeObserver | null = null
  private renderTime = 0

  constructor(canvas: HTMLCanvasElement, state: GameState, camera: Camera) {
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('Canvas not supported')

    this.canvas = canvas
    this.ctx = ctx
    this.state = state
    this.camera = camera

    this.resize()
    window.addEventListener('resize', this.resize)

    if (typeof ResizeObserver !== 'undefined') {
      this.resizeObserver = new ResizeObserver(() => {
        this.resize()
      })
      this.resizeObserver.observe(this.canvas)
    }
  }

  private resize = () => {
    this.canvas.width = this.canvas.clientWidth
    this.canvas.height = this.canvas.clientHeight

    this.camera.clamp(
      this.canvas.width,
      this.canvas.height,
      this.state.mapWidth,
      this.state.mapHeight,
    )
  }

  render() {
    const ctx = this.ctx
    const renderTime = performance.now()
    this.renderTime = renderTime
    const units = this.state.getInterpolatedUnits(renderTime)

    ctx.clearRect(0, 0, this.canvas.width, this.canvas.height)

    // Background outside the map
    ctx.fillStyle = '#0a0a0a'
    ctx.fillRect(0, 0, this.canvas.width, this.canvas.height)

    ctx.save()
    ctx.scale(this.camera.zoom, this.camera.zoom)
    ctx.translate(-this.camera.x, -this.camera.y)

    this.drawMapBackground()
    this.drawGrid()
    this.drawMapBounds()
    this.drawMoveMarkers()
    this.drawBuildingSpawnMarkers()
    this.drawUnits(units)
    this.drawBuildPlacementGhost()
    this.drawSelectionBox()

    ctx.restore()

    this.drawMinimap(units)
  }

  destroy() {
    window.removeEventListener('resize', this.resize)
    this.resizeObserver?.disconnect()
    this.resizeObserver = null
  }

  private drawGrid() {
    const ctx = this.ctx
    const gridSize = this.state.mapConfig.cellSize

    const worldWidth = this.canvas.width / this.camera.zoom
    const worldHeight = this.canvas.height / this.camera.zoom

    const viewStartX = this.camera.x
    const viewEndX = this.camera.x + worldWidth
    const viewStartY = this.camera.y
    const viewEndY = this.camera.y + worldHeight

    const startX = Math.max(0, Math.floor(viewStartX / gridSize) * gridSize)
    const endX = Math.min(this.state.mapWidth, viewEndX + gridSize)
    const startY = Math.max(0, Math.floor(viewStartY / gridSize) * gridSize)
    const endY = Math.min(this.state.mapHeight, viewEndY + gridSize)

    ctx.strokeStyle = '#1f1f1f'
    ctx.lineWidth = 1 / this.camera.zoom

    for (let x = startX; x < endX; x += gridSize) {
      ctx.beginPath()
      ctx.moveTo(x, startY)
      ctx.lineTo(x, endY)
      ctx.stroke()
    }

    for (let y = startY; y < endY; y += gridSize) {
      ctx.beginPath()
      ctx.moveTo(startX, y)
      ctx.lineTo(endX, y)
      ctx.stroke()
    }
  }

  private drawMapBackground() {
    const ctx = this.ctx

    ctx.fillStyle = DEFAULT_GRASS_COLOR
    ctx.fillRect(0, 0, this.state.mapWidth, this.state.mapHeight)

    const { cellSize, terrain, obstacles, buildings } = this.state.mapConfig

    for (const tile of terrain) {
      ctx.fillStyle = getTerrainColor(tile.terrain)
      ctx.fillRect(tile.x * cellSize, tile.y * cellSize, cellSize, cellSize)
    }

    for (const tile of obstacles) {
      const worldX = tile.x * cellSize
      const worldY = tile.y * cellSize
      const inset = cellSize * 0.14

      ctx.fillStyle = getObstacleColor(tile.obstacle)
      ctx.fillRect(
        worldX + inset,
        worldY + inset,
        cellSize - inset * 2,
        cellSize - inset * 2,
      )

      ctx.strokeStyle = 'rgba(15, 23, 42, 0.75)'
      ctx.lineWidth = 2 / this.camera.zoom
      ctx.strokeRect(
        worldX + inset,
        worldY + inset,
        cellSize - inset * 2,
        cellSize - inset * 2,
      )
    }

    for (const building of buildings) {
      if (!building.visible) continue
      if (building.buildingType === 'enemy-spawnpoint') continue

      const worldX = building.x * cellSize
      const worldY = building.y * cellSize
      const width = building.width * cellSize
      const height = building.height * cellSize
      const inset = cellSize * 0.18
      const ownerColor =
        building.occupied && building.ownerId
          ? this.state.getPlayerColor(building.ownerId)
          : null

      ctx.fillStyle = getBuildingColor(building.buildingType, building.occupied, ownerColor)
      ctx.fillRect(worldX + inset, worldY + inset, width - inset * 2, height - inset * 2)

      if (building.buildingType === 'barracks') {
        this.drawBarracksCornersAt(worldX, worldY, width, height, inset, cellSize)
      }

      if (building.buildingType === 'farm') {
        this.drawFarmDetailAt(worldX, worldY, inset, cellSize)
      }

      const isUnderConstruction = building.metadata?.['underConstruction'] === true

      if (isUnderConstruction) {
        ctx.save()
        ctx.globalAlpha = 0.45
        ctx.fillStyle = '#0f172a'
        ctx.fillRect(worldX + inset, worldY + inset, width - inset * 2, height - inset * 2)
        ctx.restore()

        ctx.save()
        ctx.strokeStyle = '#fbbf24'
        ctx.lineWidth = 2 / this.camera.zoom
        ctx.setLineDash([8 / this.camera.zoom, 5 / this.camera.zoom])
        ctx.strokeRect(worldX + inset, worldY + inset, width - inset * 2, height - inset * 2)
        ctx.restore()

        // Construction progress bar above the building
        const hp = building.metadata?.['hp'] as number | undefined
        const maxHp = building.metadata?.['maxHp'] as number | undefined
        if (hp !== undefined && maxHp !== undefined && maxHp > 0) {
          const progress = Math.max(0, Math.min(1, hp / maxHp))
          const barH = 6 / this.camera.zoom
          const barX = worldX + inset
          const barY = worldY + inset - barH - 4 / this.camera.zoom
          const barW = width - inset * 2

          ctx.save()
          ctx.fillStyle = '#1e293b'
          ctx.fillRect(barX, barY, barW, barH)
          ctx.fillStyle = '#fbbf24'
          ctx.fillRect(barX, barY, barW * progress, barH)
          ctx.strokeStyle = 'rgba(251,191,36,0.5)'
          ctx.lineWidth = 1 / this.camera.zoom
          ctx.setLineDash([])
          ctx.strokeRect(barX, barY, barW, barH)
          ctx.restore()
        }
      } else {
        ctx.strokeStyle = 'rgba(15, 23, 42, 0.85)'
        ctx.lineWidth = 2 / this.camera.zoom
        ctx.strokeRect(worldX + inset, worldY + inset, width - inset * 2, height - inset * 2)

        const hp = building.metadata?.['hp'] as number | undefined
        const maxHp = building.metadata?.['maxHp'] as number | undefined
        if (hp !== undefined && maxHp !== undefined && maxHp > 0 && hp < maxHp) {
          const healthPercent = Math.max(0, Math.min(1, hp / maxHp))
          const barH = 6 / this.camera.zoom
          const barX = worldX + inset
          const barY = worldY + inset - barH - 4 / this.camera.zoom
          const barW = width - inset * 2

          let fillColor = '#22c55e'
          if (healthPercent <= 0.35) fillColor = '#ef4444'
          else if (healthPercent <= 0.7) fillColor = '#eab308'

          ctx.save()
          ctx.fillStyle = 'rgba(2, 6, 23, 0.85)'
          ctx.fillRect(barX - 1, barY - 1, barW + 2, barH + 2)
          ctx.fillStyle = '#0f172a'
          ctx.fillRect(barX, barY, barW, barH)
          ctx.fillStyle = fillColor
          ctx.fillRect(barX, barY, barW * healthPercent, barH)
          ctx.strokeStyle = 'rgba(248, 250, 252, 0.8)'
          ctx.lineWidth = 1 / this.camera.zoom
          ctx.setLineDash([])
          ctx.strokeRect(barX, barY, barW, barH)
          ctx.restore()
        }
      }

      if (this.state.selectedBuildingId === building.id) {
        ctx.strokeStyle = '#fde68a'
        ctx.lineWidth = 3 / this.camera.zoom
        ctx.strokeRect(
          worldX + inset - 4 / this.camera.zoom,
          worldY + inset - 4 / this.camera.zoom,
          width - inset * 2 + 8 / this.camera.zoom,
          height - inset * 2 + 8 / this.camera.zoom,
        )
      }

      if (this.state.hoveredInteractableBuildingId === building.id) {
        ctx.save()
        ctx.strokeStyle = 'rgba(250, 204, 21, 0.95)'
        ctx.lineWidth = 4 / this.camera.zoom
        ctx.setLineDash([10 / this.camera.zoom, 6 / this.camera.zoom])
        ctx.strokeRect(
          worldX + inset - 8 / this.camera.zoom,
          worldY + inset - 8 / this.camera.zoom,
          width - inset * 2 + 16 / this.camera.zoom,
          height - inset * 2 + 16 / this.camera.zoom,
        )
        ctx.restore()
      }
    }
  }

  private drawMapBounds() {
    const ctx = this.ctx

    ctx.strokeStyle = '#444'
    ctx.lineWidth = 2 / this.camera.zoom
    ctx.strokeRect(0, 0, this.state.mapWidth, this.state.mapHeight)
  }

  private drawMoveMarkers() {
    const ctx = this.ctx
    const now = performance.now()

    for (const marker of this.state.moveMarkers) {
      const elapsed = now - marker.createdAt
      const progress = Math.max(0, Math.min(elapsed / marker.durationMs, 1))
      const alpha = 1 - progress

      const radius = 10 + progress * 16

      ctx.save()
      ctx.globalAlpha = alpha
      ctx.strokeStyle = '#7fdcff'
      ctx.lineWidth = 2 / this.camera.zoom

      ctx.beginPath()
      ctx.arc(marker.x, marker.y, radius, 0, Math.PI * 2)
      ctx.stroke()

      ctx.beginPath()
      ctx.arc(marker.x, marker.y, 4, 0, Math.PI * 2)
      ctx.stroke()

      ctx.restore()
    }
  }

  private drawBuildingSpawnMarkers() {
    const selectedBuilding = this.state.getSelectedBuilding()
    if (!selectedBuilding) return

    const spawnPoint = this.state.getBuildingSpawnPoint(selectedBuilding)
    if (!spawnPoint) return

    this.drawSpawnPointMarker(spawnPoint.x, spawnPoint.y, '#93c5fd')
  }

  private drawSpawnPointMarker(x: number, y: number, color: string) {
    const ctx = this.ctx

    ctx.save()
    ctx.strokeStyle = color
    ctx.fillStyle = 'rgba(15, 23, 42, 0.92)'
    ctx.lineWidth = 2 / this.camera.zoom

    ctx.beginPath()
    ctx.arc(x, y, 9, 0, Math.PI * 2)
    ctx.fill()
    ctx.stroke()

    ctx.beginPath()
    ctx.moveTo(x - 13, y)
    ctx.lineTo(x + 13, y)
    ctx.moveTo(x, y - 13)
    ctx.lineTo(x, y + 13)
    ctx.stroke()
    ctx.restore()
  }

  private drawUnits(
    units: Array<{
      id: number
      unitType?: string
      status?: string
      x: number
      y: number
      hp?: number
      maxHp?: number
      color?: string
      visible?: boolean
      ownerId?: string
    }>,
  ) {
    const ctx = this.ctx

    for (const unit of units) {
      if (unit.visible === false) {
        continue
      }

      const selected = this.state.selectedUnitIds.has(unit.id)
      const isInspected = this.state.inspectedEnemyUnitId === unit.id
      const isHoveredEnemy = this.state.hoveredEnemyUnitId === unit.id

      if (selected) {
        ctx.strokeStyle = 'yellow'
        ctx.lineWidth = 3 / this.camera.zoom
        ctx.beginPath()
        ctx.arc(unit.x, unit.y, 15, 0, Math.PI * 2)
        ctx.stroke()
      }

      // Enemy hover ring (orange dashed)
      if (isHoveredEnemy && !isInspected) {
        ctx.save()
        ctx.strokeStyle = 'rgba(251, 146, 60, 0.9)'
        ctx.lineWidth = 2 / this.camera.zoom
        ctx.setLineDash([5 / this.camera.zoom, 4 / this.camera.zoom])
        ctx.beginPath()
        ctx.arc(unit.x, unit.y, 16, 0, Math.PI * 2)
        ctx.stroke()
        ctx.restore()
      }

      // Inspected enemy ring (red solid)
      if (isInspected) {
        ctx.strokeStyle = '#ef4444'
        ctx.lineWidth = 3 / this.camera.zoom
        ctx.beginPath()
        ctx.arc(unit.x, unit.y, 15, 0, Math.PI * 2)
        ctx.stroke()
      }

      // Health bar always visible for all units
      const isEnemy = unit.ownerId !== this.state.localPlayerId
      this.drawSelectedUnitHealthBar(unit, isEnemy)

      if (unit.status === 'Attacking') {
        this.drawAttackEffect(unit.x, unit.y)
      } else if (unit.status === 'Chopping Wood') {
        this.drawChoppingEffect(unit.x, unit.y)
      }

      ctx.fillStyle = unit.color || 'lime'

      if (unit.unitType === 'soldier' || unit.unitType === 'raider') {
        this.drawSoldierShape(unit.x, unit.y)
      } else {
        ctx.beginPath()
        ctx.arc(unit.x, unit.y, 10, 0, Math.PI * 2)
        ctx.fill()
      }
    }
  }

  private drawAttackEffect(x: number, y: number) {
    const ctx = this.ctx
    // Sharp impact burst: 4 slash lines that shoot outward and fade, cycling at ~4Hz
    const t = (this.renderTime % 250) / 250
    const alpha = 1 - t
    const inner = 6 + t * 4
    const outer = 14 + t * 10

    ctx.save()
    ctx.strokeStyle = `rgba(239, 68, 68, ${alpha})`
    ctx.lineWidth = 2 / this.camera.zoom
    ctx.lineCap = 'round'

    // 4 diagonal slash lines at 45° increments
    const angles = [Math.PI / 4, -Math.PI / 4, (3 * Math.PI) / 4, -(3 * Math.PI) / 4]
    for (const angle of angles) {
      const cos = Math.cos(angle)
      const sin = Math.sin(angle)
      ctx.beginPath()
      ctx.moveTo(x + cos * inner, y + sin * inner)
      ctx.lineTo(x + cos * outer, y + sin * outer)
      ctx.stroke()
    }

    // Bright center flash on impact start
    if (t < 0.25) {
      const flashAlpha = (1 - t / 0.25) * 0.8
      ctx.fillStyle = `rgba(255, 200, 100, ${flashAlpha})`
      ctx.beginPath()
      ctx.arc(x, y, 4 + t * 6, 0, Math.PI * 2)
      ctx.fill()
    }

    ctx.restore()
  }

  private drawChoppingEffect(x: number, y: number) {
    const ctx = this.ctx
    // Axe-swing arc that oscillates left and right above the unit
    const swing = Math.sin(this.renderTime * 0.005) // ±1, ~1Hz
    const angle = swing * (Math.PI / 3)

    ctx.save()
    ctx.translate(x, y - 16)
    ctx.rotate(angle)
    ctx.strokeStyle = '#92400e'
    ctx.lineWidth = 3 / this.camera.zoom
    ctx.lineCap = 'round'
    ctx.beginPath()
    ctx.arc(0, 0, 7, -Math.PI / 3, Math.PI / 3)
    ctx.stroke()
    // Axe head dot at the tip
    ctx.fillStyle = '#d97706'
    ctx.beginPath()
    ctx.arc(0, -7, 3, 0, Math.PI * 2)
    ctx.fill()
    ctx.restore()
  }

  private drawSoldierShape(x: number, y: number) {
    const ctx = this.ctx
    const w = 9
    const h = 10
    const r = 8

    // Top circle
    ctx.beginPath()
    ctx.arc(x, y - r, r, 0, Math.PI * 2)
    ctx.fill()

    // Bottom triangle (tip pointing down from center)
    ctx.beginPath()
    ctx.moveTo(x - w, y)
    ctx.lineTo(x + w, y)
    ctx.lineTo(x, y + h)
    ctx.closePath()
    ctx.fill()
  }

  private drawSelectedUnitHealthBar(unit: {
    x: number
    y: number
    hp?: number
    maxHp?: number
  }, isEnemy = false) {
    const ctx = this.ctx
    const maxHp = Math.max(unit.maxHp ?? unit.hp ?? 100, 1)
    const hp = Math.max(0, Math.min(unit.hp ?? maxHp, maxHp))
    const healthPercent = hp / maxHp
    const barWidth = 26
    const barHeight = 4
    const barX = unit.x - barWidth / 2
    const barY = unit.y - 22

    let fillColor = '#22c55e'
    if (isEnemy) {
      fillColor = '#ef4444'
    } else if (healthPercent <= 0.35) {
      fillColor = '#ef4444'
    } else if (healthPercent <= 0.7) {
      fillColor = '#eab308'
    }

    ctx.save()
    ctx.fillStyle = 'rgba(2, 6, 23, 0.85)'
    ctx.fillRect(barX - 1, barY - 1, barWidth + 2, barHeight + 2)

    ctx.fillStyle = '#0f172a'
    ctx.fillRect(barX, barY, barWidth, barHeight)

    ctx.fillStyle = fillColor
    ctx.fillRect(barX, barY, barWidth * healthPercent, barHeight)

    ctx.strokeStyle = 'rgba(248, 250, 252, 0.8)'
    ctx.lineWidth = 1 / this.camera.zoom
    ctx.strokeRect(barX, barY, barWidth, barHeight)
    ctx.restore()
  }

  private drawBarracksCornersAt(
    worldX: number,
    worldY: number,
    width: number,
    height: number,
    inset: number,
    cellSize: number,
  ) {
    const ctx = this.ctx
    const cornerSize = cellSize * 0.28
    const left = worldX + inset
    const top = worldY + inset
    const right = worldX + width - inset - cornerSize
    const bottom = worldY + height - inset - cornerSize

    ctx.fillStyle = '#94a3b8'
    ctx.fillRect(left, top, cornerSize, cornerSize)
    ctx.fillRect(right, top, cornerSize, cornerSize)
    ctx.fillRect(left, bottom, cornerSize, cornerSize)
    ctx.fillRect(right, bottom, cornerSize, cornerSize)
  }

  private drawFarmDetailAt(worldX: number, worldY: number, inset: number, cellSize: number) {
    const ctx = this.ctx
    // Gold square filling most of the bottom-left cell
    const tileX = worldX + inset
    const tileY = worldY + cellSize + inset
    const tileSize = cellSize - inset * 2
    ctx.fillStyle = '#ca8a04'
    ctx.fillRect(tileX, tileY, tileSize, tileSize)
  }

  private drawBuildPlacementGhost() {
    const placement = this.state.buildPlacement
    if (!placement) return

    const ctx = this.ctx
    const { cellSize } = this.state.mapConfig
    const { cursorGridX, cursorGridY, gridW, gridH, valid } = placement

    const worldX = cursorGridX * cellSize
    const worldY = cursorGridY * cellSize
    const width = gridW * cellSize
    const height = gridH * cellSize
    const inset = cellSize * 0.18

    ctx.save()
    ctx.globalAlpha = 0.6
    ctx.fillStyle = valid ? (placement.buildingType === 'farm' ? '#4a7c3f' : '#1e40af') : '#dc2626'
    ctx.fillRect(worldX + inset, worldY + inset, width - inset * 2, height - inset * 2)

    if (placement.buildingType === 'farm') {
      this.drawFarmDetailAt(worldX, worldY, inset, cellSize)
    } else {
      this.drawBarracksCornersAt(worldX, worldY, width, height, inset, cellSize)
    }

    ctx.globalAlpha = 0.9
    ctx.strokeStyle = valid ? '#93c5fd' : '#fca5a5'
    ctx.lineWidth = 2 / this.camera.zoom
    ctx.setLineDash([8 / this.camera.zoom, 4 / this.camera.zoom])
    ctx.strokeRect(worldX + inset, worldY + inset, width - inset * 2, height - inset * 2)
    ctx.restore()
  }

  private drawSelectionBox() {
    if (!this.state.selectionBox.active) return

    const ctx = this.ctx
    const { left, top, right, bottom } = this.state.getSelectionBounds()
    const width = right - left
    const height = bottom - top

    ctx.fillStyle = 'rgba(80, 160, 255, 0.2)'
    ctx.strokeStyle = 'rgba(80, 160, 255, 0.9)'
    ctx.lineWidth = 1 / this.camera.zoom

    ctx.fillRect(left, top, width, height)
    ctx.strokeRect(left, top, width, height)
  }

  private drawMinimap(
    units: Array<{
      id: number
      x: number
      y: number
      ownerId?: string
      color?: string
      visible?: boolean
    }>,
  ) {
    const ctx = this.ctx
    const bounds = getMinimapBounds(
      this.canvas.width,
      this.canvas.height,
      this.state.mapWidth,
      this.state.mapHeight,
    )
    const { x, y, width: minimapWidth, height: minimapHeight } = bounds

    ctx.save()

    ctx.fillStyle = 'rgba(5, 10, 18, 0.82)'
    ctx.fillRect(x, y, minimapWidth, minimapHeight)

    for (const tile of this.state.mapConfig.terrain) {
      ctx.fillStyle = getTerrainColor(tile.terrain)
      ctx.fillRect(
        x + (tile.x / this.state.mapConfig.gridCols) * minimapWidth,
        y + (tile.y / this.state.mapConfig.gridRows) * minimapHeight,
        minimapWidth / this.state.mapConfig.gridCols,
        minimapHeight / this.state.mapConfig.gridRows,
      )
    }

    for (const tile of this.state.mapConfig.obstacles) {
      ctx.fillStyle = getObstacleColor(tile.obstacle)
      ctx.fillRect(
        x + (tile.x / this.state.mapConfig.gridCols) * minimapWidth,
        y + (tile.y / this.state.mapConfig.gridRows) * minimapHeight,
        minimapWidth / this.state.mapConfig.gridCols,
        minimapHeight / this.state.mapConfig.gridRows,
      )
    }

    for (const building of this.state.mapConfig.buildings) {
      if (!building.visible) continue

      const ownerColor =
        building.occupied && building.ownerId
          ? this.state.getPlayerColor(building.ownerId)
          : null

      ctx.fillStyle = getBuildingColor(building.buildingType, building.occupied, ownerColor)
      ctx.fillRect(
        x + (building.x / this.state.mapConfig.gridCols) * minimapWidth,
        y + (building.y / this.state.mapConfig.gridRows) * minimapHeight,
        (building.width / this.state.mapConfig.gridCols) * minimapWidth,
        (building.height / this.state.mapConfig.gridRows) * minimapHeight,
      )
    }

    ctx.strokeStyle = 'rgba(166, 191, 255, 0.35)'
    ctx.lineWidth = 1
    ctx.strokeRect(x, y, minimapWidth, minimapHeight)

    for (const unit of units) {
      if (unit.visible === false) {
        continue
      }

      const dotX = x + (unit.x / this.state.mapWidth) * minimapWidth
      const dotY = y + (unit.y / this.state.mapHeight) * minimapHeight
      const isLocalPlayerUnit =
        !!this.state.localPlayerId && unit.ownerId === this.state.localPlayerId

      ctx.fillStyle = isLocalPlayerUnit ? '#f8fafc' : (unit.color ?? '#94a3b8')
      ctx.beginPath()
      ctx.arc(dotX, dotY, isLocalPlayerUnit ? 2.8 : 2.2, 0, Math.PI * 2)
      ctx.fill()
    }

    const worldWidth = this.canvas.width / this.camera.zoom
    const worldHeight = this.canvas.height / this.camera.zoom
    const viewX = x + (this.camera.x / this.state.mapWidth) * minimapWidth
    const viewY = y + (this.camera.y / this.state.mapHeight) * minimapHeight
    const viewWidth = (worldWidth / this.state.mapWidth) * minimapWidth
    const viewHeight = (worldHeight / this.state.mapHeight) * minimapHeight

    ctx.strokeStyle = 'rgba(125, 211, 252, 0.95)'
    ctx.lineWidth = 1.5
    ctx.strokeRect(viewX, viewY, viewWidth, viewHeight)

    ctx.restore()
  }
}
