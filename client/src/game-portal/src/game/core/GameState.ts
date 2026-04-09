import type { MapConfig, MatchSnapshotMessage } from '../network/protocol'

export type Unit = {
  id: number
  x: number
  y: number
  hp?: number
  maxHp?: number
  ownerId?: string
  color?: string
  targetX?: number
  targetY?: number
  moving?: boolean
}

export type InterpolationFrame = {
  tick: number
  serverNow: number
  receivedAt: number
  units: Unit[]
}

export type SelectionBox = {
  startX: number
  startY: number
  currentX: number
  currentY: number
  active: boolean
}

export type MoveMarker = {
  id: number
  x: number
  y: number
  createdAt: number
  durationMs: number
}

export type Vec2 = {
  x: number
  y: number
}

export type PlayerSummary = {
  playerId: string | null
  color: string | null
  totalUnits: number
  selectedUnits: number
  totalHp: number
  resources: ResourceStock[]
}

export type ResourceStock = {
  id: string
  label: string
  amount: number
  accent: string
}

export class GameState {
  private resourceStocks: ResourceStock[] = [
    { id: 'gold', label: 'Gold', amount: 500, accent: '#d4a84f' },
    { id: 'wood', label: 'Wood', amount: 180, accent: '#7a9a52' },
    { id: 'food', label: 'Food', amount: 24, accent: '#c96e43' },
  ]

  units: Unit[] = []

  snapshotBuffer: InterpolationFrame[] = []
  interpolationDelayMs = 100
  maxBufferedSnapshots = 20

  localPlayerId: string | null = null
  mapWidth = 6144
  mapHeight = 4096
  
  selectedUnitIds = new Set<number>()
  selectedUnitOrder: number[] = []

  selectionBox: SelectionBox = {
    startX: 0,
    startY: 0,
    currentX: 0,
    currentY: 0,
    active: false,
  }

  moveMarkers: MoveMarker[] = []
  private nextMoveMarkerId = 1

  setLocalPlayerId(playerId: string) {
    this.localPlayerId = playerId
  }

  update(_dt: number) {
    const now = performance.now()
    this.moveMarkers = this.moveMarkers.filter(
      (marker) => now - marker.createdAt < marker.durationMs,
    )
  }

  addMoveMarker(x: number, y: number, durationMs = 550) {
    this.moveMarkers.push({
      id: this.nextMoveMarkerId++,
      x,
      y,
      createdAt: performance.now(),
      durationMs,
    })
  }

  addMoveMarkers(points: Vec2[], durationMs = 550) {
    const createdAt = performance.now()

    for (const point of points) {
      this.moveMarkers.push({
        id: this.nextMoveMarkerId++,
        x: point.x,
        y: point.y,
        createdAt,
        durationMs,
      })
    }
  }

  applySnapshot(message: MatchSnapshotMessage) {
    const now = performance.now()

    this.setMapConfig(message.map)
    
    const frame: InterpolationFrame = {
      tick: message.tick,
      serverNow: message.serverNow,
      receivedAt: now,
      units: message.units.map((unit) => ({
        id: unit.id,
        x: unit.x,
        y: unit.y,
        hp: unit.hp,
        maxHp: unit.maxHp,
        ownerId: unit.ownerId,
        color: unit.color,
        targetX: unit.targetX,
        targetY: unit.targetY,
        moving: unit.moving,
      })),
    }

    this.snapshotBuffer.push(frame)

    if (this.snapshotBuffer.length > this.maxBufferedSnapshots) {
      this.snapshotBuffer.shift()
    }

    this.units = frame.units.map((unit) => ({ ...unit }))

    const validIds = new Set(this.units.map((u) => u.id))

    for (const id of Array.from(this.selectedUnitIds)) {
      if (!validIds.has(id)) {
        this.selectedUnitIds.delete(id)
      }
    }

    this.selectedUnitOrder = this.selectedUnitOrder.filter((id) =>
      validIds.has(id),
    )
  }

  getInterpolatedUnits(renderTime: number): Unit[] {
    if (this.snapshotBuffer.length === 0) {
      return this.units
    }

    if (this.snapshotBuffer.length === 1) {
      return this.snapshotBuffer[0].units.map((unit) => ({ ...unit }))
    }

    const targetTime = renderTime - this.interpolationDelayMs

    let fromFrame: InterpolationFrame | null = null
    let toFrame: InterpolationFrame | null = null

    for (let i = 0; i < this.snapshotBuffer.length - 1; i++) {
      const current = this.snapshotBuffer[i]
      const next = this.snapshotBuffer[i + 1]

      if (current.receivedAt <= targetTime && next.receivedAt >= targetTime) {
        fromFrame = current
        toFrame = next
        break
      }
    }

    if (!fromFrame && !toFrame && targetTime < this.snapshotBuffer[0].receivedAt) {
      return this.snapshotBuffer[0].units.map((unit) => ({ ...unit }))
    }

    if (!fromFrame || !toFrame) {
      const latest = this.snapshotBuffer[this.snapshotBuffer.length - 1]
      return latest.units.map((unit) => ({ ...unit }))
    }

    const duration = Math.max(1, toFrame.receivedAt - fromFrame.receivedAt)
    const alphaRaw = (targetTime - fromFrame.receivedAt) / duration
    const alpha = Math.max(0, Math.min(alphaRaw, 1))

    const fromMap = new Map(fromFrame.units.map((u) => [u.id, u]))
    const interpolated: Unit[] = []

    for (const toUnit of toFrame.units) {
      const fromUnit = fromMap.get(toUnit.id)

      if (!fromUnit) {
        interpolated.push({ ...toUnit })
        continue
      }

      interpolated.push({
        ...toUnit,
        x: fromUnit.x + (toUnit.x - fromUnit.x) * alpha,
        y: fromUnit.y + (toUnit.y - fromUnit.y) * alpha,
      })
    }

    return interpolated
  }

  private getInteractionUnits(): Unit[] {
    return this.getInterpolatedUnits(performance.now())
  }

  private isOwnedByLocalPlayer(unit: Unit): boolean {
    return !!this.localPlayerId && unit.ownerId === this.localPlayerId
  }

  clearSelection() {
    this.selectedUnitIds.clear()
    this.selectedUnitOrder = []
  }

  selectUnit(unitId: number) {
    const unit = this.units.find((u) => u.id === unitId)
    if (!unit || !this.isOwnedByLocalPlayer(unit)) return

    this.selectedUnitIds.clear()
    this.selectedUnitIds.add(unitId)
    this.selectedUnitOrder = [unitId]
  }

  setSelection(unitIds: number[]) {
    const ownedIds = unitIds.filter((id) => {
      const unit = this.units.find((u) => u.id === id)
      return !!unit && this.isOwnedByLocalPlayer(unit)
    })

    this.selectedUnitIds.clear()

    for (const id of ownedIds) {
      this.selectedUnitIds.add(id)
    }

    this.selectedUnitOrder = [...ownedIds]
  }

  addFormationMoveMarkers(destX: number, destY: number, durationMs = 550) {
    const points = this.getFormationDestinations(destX, destY)
    this.addMoveMarkers(points, durationMs)
  }

  addUnitToSelection(unitId: number) {
    const unit = this.units.find((u) => u.id === unitId)
    if (!unit || !this.isOwnedByLocalPlayer(unit)) return
    if (this.selectedUnitIds.has(unitId)) return

    this.selectedUnitIds.add(unitId)
    this.selectedUnitOrder.push(unitId)
  }

  removeUnitFromSelection(unitId: number) {
    this.selectedUnitIds.delete(unitId)
    this.selectedUnitOrder = this.selectedUnitOrder.filter((id) => id !== unitId)
  }

  toggleUnitSelection(unitId: number) {
    const unit = this.units.find((u) => u.id === unitId)
    if (!unit || !this.isOwnedByLocalPlayer(unit)) return

    if (this.selectedUnitIds.has(unitId)) {
      this.removeUnitFromSelection(unitId)
    } else {
      this.addUnitToSelection(unitId)
    }
  }

  beginSelectionBox(x: number, y: number) {
    this.selectionBox.startX = x
    this.selectionBox.startY = y
    this.selectionBox.currentX = x
    this.selectionBox.currentY = y
    this.selectionBox.active = true
  }

  updateSelectionBox(x: number, y: number) {
    this.selectionBox.currentX = x
    this.selectionBox.currentY = y
  }

  endSelectionBox() {
    this.selectionBox.active = false
  }

  getSelectionBounds() {
    const { startX, startY, currentX, currentY } = this.selectionBox

    return {
      left: Math.min(startX, currentX),
      right: Math.max(startX, currentX),
      top: Math.min(startY, currentY),
      bottom: Math.max(startY, currentY),
    }
  }

  private getUnitsInSelectionBox(): Unit[] {
    const { left, right, top, bottom } = this.getSelectionBounds()

    return this.getInteractionUnits()
      .filter((unit) => {
        return (
          this.isOwnedByLocalPlayer(unit) &&
          unit.x >= left &&
          unit.x <= right &&
          unit.y >= top &&
          unit.y <= bottom
        )
      })
      .sort((a, b) => {
        const rowTolerance = 12
        const yDiff = a.y - b.y

        if (Math.abs(yDiff) > rowTolerance) {
          return yDiff
        }

        return a.x - b.x
      })
  }

  selectUnitsInBox() {
    const selected = this.getUnitsInSelectionBox().map((unit) => unit.id)
    this.setSelection(selected)
  }

  addUnitsInBox() {
    const unitsInBox = this.getUnitsInSelectionBox()

    for (const unit of unitsInBox) {
      this.addUnitToSelection(unit.id)
    }
  }

  getUnitAtPosition(x: number, y: number, radius = 14): Unit | undefined {
    return this.getInteractionUnits().find((unit) => {
      if (!this.isOwnedByLocalPlayer(unit)) return false

      const dx = unit.x - x
      const dy = unit.y - y
      return Math.sqrt(dx * dx + dy * dy) <= radius
    })
  }

  getOrderedSelectedUnitIds(): number[] {
    return this.selectedUnitOrder.filter((id) => {
      if (!this.selectedUnitIds.has(id)) return false
      const unit = this.units.find((u) => u.id === id)
      return !!unit && this.isOwnedByLocalPlayer(unit)
    })
  }

  getFormationDestinations(destX: number, destY: number): Vec2[] {
    const selectedIds = this.getOrderedSelectedUnitIds()
    const count = selectedIds.length

    if (count === 0) {
      return []
    }

    if (count === 1) {
      return [{ x: destX, y: destY }]
    }

    const spacing = 24
    const cols = Math.ceil(Math.sqrt(count))
    const rows = Math.ceil(count / cols)

    const totalWidth = (cols - 1) * spacing
    const totalHeight = (rows - 1) * spacing

    const startX = destX - totalWidth / 2
    const startY = destY - totalHeight / 2

    const points: Vec2[] = []

    for (let i = 0; i < count; i++) {
      const col = i % cols
      const row = Math.floor(i / cols)

      points.push({
        x: startX + col * spacing,
        y: startY + row * spacing,
      })
    }

    return points
  }

  setMapConfig(map: MapConfig) {
    this.mapWidth = map.width
    this.mapHeight = map.height
  }

  getLocalPlayerUnits(): Unit[] {
    if (!this.localPlayerId) return []
    return this.units.filter((unit) => unit.ownerId === this.localPlayerId)
  }

  getSelectedUnits(): Unit[] {
    const selectedIds = this.getOrderedSelectedUnitIds()

    return selectedIds
      .map((id) => this.units.find((unit) => unit.id === id))
      .filter((unit): unit is Unit => !!unit)
  }

  getLocalPlayerSpawnCenter(): Vec2 | null {
    const localUnits = this.getLocalPlayerUnits()
    if (localUnits.length === 0) return null

    const totals = localUnits.reduce(
      (acc, unit) => {
        acc.x += unit.x
        acc.y += unit.y
        return acc
      },
      { x: 0, y: 0 },
    )

    return {
      x: totals.x / localUnits.length,
      y: totals.y / localUnits.length,
    }
  }

  getPlayerSummary(): PlayerSummary {
    const localUnits = this.getLocalPlayerUnits()

    return {
      playerId: this.localPlayerId,
      color: localUnits[0]?.color ?? null,
      totalUnits: localUnits.length,
      selectedUnits: this.selectedUnitIds.size,
      totalHp: localUnits.reduce((sum, unit) => sum + (unit.hp ?? 0), 0),
      resources: this.resourceStocks.map((resource) => ({ ...resource })),
    }
  }
}
