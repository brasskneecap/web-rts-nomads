import type {
  BuildingTile,
  MapConfig,
  MatchSnapshotMessage,
  PlayerSnapshot,
  ResourceType,
  UnitCapability,
  UnitType,
} from '../network/protocol'
import { createEditorMapConfig, sanitizeMapConfig } from '../maps/mapConfig'

export type Unit = {
  id: number
  unitType: UnitType
  name: string
  capabilities: UnitCapability[]
  visible: boolean
  status?: string
  x: number
  y: number
  hp?: number
  maxHp?: number
  ownerId?: string
  color?: string
  carriedResourceType?: ResourceType
  carriedAmount?: number
  targetX?: number
  targetY?: number
  moving?: boolean
}

export type ActionItem = {
  id: string
  label: string
  disabled?: boolean
}

export type DetailItem = {
  id: string
  label: string
  value?: string
}

export type ProductionSummary = {
  unitType: string
  remainingSeconds: number
  totalSeconds: number
  queueLength: number
  queuedUnitTypes: string[]
  progress: number
  timeLabel: string
}

export type SelectionSummary =
  | {
      kind: 'none'
      title: string
      subtitle: string
      details: DetailItem[]
      actions: ActionItem[]
      production?: undefined
    }
  | {
      kind: 'unit'
      title: string
      subtitle: string
      details: DetailItem[]
      actions: ActionItem[]
      production?: undefined
    }
  | {
      kind: 'building'
      title: string
      subtitle: string
      details: DetailItem[]
      actions: ActionItem[]
      production?: ProductionSummary
    }
  | {
      kind: 'group'
      title: string
      subtitle: string
      details: DetailItem[]
      actions: ActionItem[]
      production?: undefined
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

export type BuildingTargetingMode = 'set-spawn-point'

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
  private playerColors = new Map<string, string>()

  units: Unit[] = []

  snapshotBuffer: InterpolationFrame[] = []
  interpolationDelayMs = 100
  maxBufferedSnapshots = 20

  localPlayerId: string | null = null
  mapWidth = 6144
  mapHeight = 4096
  mapConfig: MapConfig = createEditorMapConfig(96, 64, {
    id: 'loading-map',
    name: 'Loading Map',
    description: '',
  })
  
  selectedUnitIds = new Set<number>()
  selectedUnitOrder: number[] = []
  selectedBuildingId: string | null = null
  hoveredInteractableBuildingId: string | null = null
  buildingTargetingMode: BuildingTargetingMode | null = null

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
        unitType: unit.unitType,
        name: unit.name,
        capabilities: unit.capabilities ?? [],
        visible: unit.visible,
        status: unit.status,
        x: unit.x,
        y: unit.y,
        hp: unit.hp,
        maxHp: unit.maxHp,
        ownerId: unit.ownerId,
        color: unit.color,
        carriedResourceType: unit.carriedResourceType,
        carriedAmount: unit.carriedAmount,
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
    this.applyPlayerSnapshots(message.players)

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
    this.selectedBuildingId = null
    this.buildingTargetingMode = null
  }

  selectUnit(unitId: number) {
    const unit = this.units.find((u) => u.id === unitId)
    if (!unit || !this.isOwnedByLocalPlayer(unit)) return

    this.selectedUnitIds.clear()
    this.selectedUnitIds.add(unitId)
    this.selectedUnitOrder = [unitId]
    this.selectedBuildingId = null
    this.buildingTargetingMode = null
  }

  setSelection(unitIds: number[]) {
    const ownedIds = unitIds.filter((id) => {
      const unit = this.units.find((u) => u.id === id)
      return !!unit && this.isOwnedByLocalPlayer(unit)
    })

    this.selectedUnitIds.clear()
    this.selectedBuildingId = null
    this.buildingTargetingMode = null

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
      if (!this.isOwnedByLocalPlayer(unit) || !unit.visible) return false

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
    const selectedUnits = this.getSelectedUnits()
    const count = selectedUnits.length

    if (count === 0) {
      return []
    }

    if (count === 1) {
      return [{ x: destX, y: destY }]
    }

    return buildFormationDestinations(selectedUnits, { x: destX, y: destY }, 28)
  }

  getBuildingAtPosition(x: number, y: number, padding = 0): BuildingTile | undefined {
    const { cellSize, buildings } = this.mapConfig

    return buildings.find((building) => {
      if (!building.visible) return false

      const left = building.x * cellSize - padding
      const top = building.y * cellSize - padding
      const right = left + building.width * cellSize + padding * 2
      const bottom = top + building.height * cellSize + padding * 2

      return x >= left && x <= right && y >= top && y <= bottom
    })
  }

  selectBuilding(buildingId: string) {
    this.selectedUnitIds.clear()
    this.selectedUnitOrder = []
    this.selectedBuildingId = buildingId
    this.buildingTargetingMode = null
  }

  getSelectedBuilding(): BuildingTile | null {
    if (!this.selectedBuildingId) return null
    return this.mapConfig.buildings.find((building) => building.id === this.selectedBuildingId) ?? null
  }

  beginBuildingTargeting(mode: BuildingTargetingMode) {
    if (!this.getSelectedBuilding()) return
    this.buildingTargetingMode = mode
  }

  cancelBuildingTargeting() {
    this.buildingTargetingMode = null
  }

  isBuildingTargetingActive(mode?: BuildingTargetingMode) {
    if (!this.buildingTargetingMode) return false
    return mode ? this.buildingTargetingMode === mode : true
  }

  getTargetedBuildingSpawnPoint(worldX: number, worldY: number): Vec2 | null {
    const building = this.getSelectedBuilding()
    if (!building || this.buildingTargetingMode !== 'set-spawn-point') return null
    return clampBuildingSpawnPoint(this.mapConfig, building, { x: worldX, y: worldY })
  }

  getBuildingSpawnPoint(building: BuildingTile): Vec2 | null {
    const x = getBuildingMetadataNumber(building, 'spawnPointX')
    const y = getBuildingMetadataNumber(building, 'spawnPointY')
    if (x === undefined || y === undefined) return null
    return { x, y }
  }

  setHoveredInteractableBuilding(buildingId: string | null) {
    this.hoveredInteractableBuildingId = buildingId
  }

  setMapConfig(map: MapConfig) {
    this.mapConfig = sanitizeMapConfig(map)
    this.mapWidth = this.mapConfig.width
    this.mapHeight = this.mapConfig.height

    if (
      this.selectedBuildingId &&
      !this.mapConfig.buildings.some((building) => building.id === this.selectedBuildingId && building.visible)
    ) {
      this.selectedBuildingId = null
    }
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

  selectedUnitsCanGather(): boolean {
    const units = this.getSelectedUnits()
    return units.length > 0 && units.every((unit) => unit.capabilities.includes('gather'))
  }

  getSelectionSummary(): SelectionSummary {
    const selectedBuilding = this.getSelectedBuilding()
    if (selectedBuilding) {
      const title = formatBuildingName(selectedBuilding.buildingType)
      const activeProduction = getBuildingProductionState(selectedBuilding)
      const defaultSubtitle = selectedBuilding.ownerId
        ? `Owned by ${selectedBuilding.ownerId}`
        : selectedBuilding.occupied
          ? 'Occupied'
          : 'Neutral'
      const subtitle = this.buildingTargetingMode === 'set-spawn-point'
        ? 'Click anywhere on the map to set the spawn point target.'
        : activeProduction
          ? `Training ${formatSpawnUnitType(activeProduction.unitType)}`
        : defaultSubtitle

      return {
        kind: 'building',
        title,
        subtitle,
        details: getBuildingDetails(selectedBuilding),
        actions: getBuildingActions(selectedBuilding),
        production: activeProduction ? toProductionSummary(activeProduction) : undefined,
      }
    }

    const selectedUnits = this.getSelectedUnits()
    if (selectedUnits.length === 0) {
      return {
        kind: 'none',
        title: 'No Selection',
        subtitle: 'Select a unit or building to inspect details and actions.',
        details: [],
        actions: [],
      }
    }

    if (selectedUnits.length === 1) {
      const unit = selectedUnits[0]
      return {
        kind: 'unit',
        title: unit.name,
        subtitle: unit.status || formatUnitType(unit.unitType),
        details: getUnitDetails(unit),
        actions: getUnitActions(unit),
      }
    }

    const totalHp = selectedUnits.reduce((sum, unit) => sum + (unit.hp ?? 0), 0)
    const totalMaxHp = selectedUnits.reduce((sum, unit) => sum + (unit.maxHp ?? unit.hp ?? 0), 0)

    return {
      kind: 'group',
      title: `${selectedUnits.length} Units Selected`,
      subtitle: selectedUnits.every((unit) => unit.unitType === 'worker')
        ? summarizeWorkerGroupStatus(selectedUnits)
        : 'Mixed Detachment',
      details: getGroupDetails(selectedUnits, totalHp, totalMaxHp),
      actions: getGroupActions(selectedUnits),
    }
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
      color:
        (this.localPlayerId ? this.playerColors.get(this.localPlayerId) : null) ??
        localUnits[0]?.color ??
        null,
      totalUnits: localUnits.length,
      selectedUnits: this.selectedUnitIds.size,
      totalHp: localUnits.reduce((sum, unit) => sum + (unit.hp ?? 0), 0),
      resources: this.resourceStocks.map((resource) => ({ ...resource })),
    }
  }

  getPlayerColor(playerId: string | null | undefined): string | null {
    if (!playerId) return null
    return this.playerColors.get(playerId) ?? null
  }

  private applyPlayerSnapshots(players: PlayerSnapshot[]) {
    this.playerColors = new Map(players.map((player) => [player.playerId, player.color]))

    if (!this.localPlayerId) return

    const localPlayer = players.find((player) => player.playerId === this.localPlayerId)
    if (!localPlayer) return

    this.resourceStocks = localPlayer.resources.map((resource) => ({
      id: resource.id,
      label: resource.label,
      amount: resource.amount,
      accent: resource.accent,
    }))
  }
}

function buildFormationDestinations(units: Unit[], anchor: Vec2, spacing: number): Vec2[] {
  if (units.length === 0) return []
  if (units.length === 1) return [anchor]

  const center = getUnitCenter(units)
  let forwardX = anchor.x - center.x
  let forwardY = anchor.y - center.y
  let forwardLength = Math.hypot(forwardX, forwardY)

  if (forwardLength < 0.001) {
    forwardX = 0
    forwardY = 1
    forwardLength = 1
  }

  forwardX /= forwardLength
  forwardY /= forwardLength

  const rightX = forwardY
  const rightY = -forwardX
  const cols = Math.ceil(Math.sqrt(units.length))
  const rows = Math.ceil(units.length / cols)
  const totalWidth = (cols - 1) * spacing
  const totalHeight = (rows - 1) * spacing
  const slots = units.map((_, index) => {
    const col = index % cols
    const row = Math.floor(index / cols)
    const rightOffset = col * spacing - totalWidth / 2
    const forwardOffset = row * spacing - totalHeight / 2

    return {
      x: anchor.x + rightX * rightOffset + forwardX * forwardOffset,
      y: anchor.y + rightY * rightOffset + forwardY * forwardOffset,
    }
  })

  const unitOrder = units
    .map((unit, index) => {
      const relativeX = unit.x - center.x
      const relativeY = unit.y - center.y

      return {
        index,
        right: relativeX * rightX + relativeY * rightY,
        forward: relativeX * forwardX + relativeY * forwardY,
      }
    })
    .sort((a, b) => {
      if (Math.abs(a.forward - b.forward) > 8) {
        return a.forward - b.forward
      }

      return a.right - b.right
    })

  const slotOrder = slots
    .map((slot, index) => {
      const relativeX = slot.x - anchor.x
      const relativeY = slot.y - anchor.y

      return {
        index,
        right: relativeX * rightX + relativeY * rightY,
        forward: relativeX * forwardX + relativeY * forwardY,
      }
    })
    .sort((a, b) => {
      if (Math.abs(a.forward - b.forward) > 8) {
        return a.forward - b.forward
      }

      return a.right - b.right
    })

  const targets = new Array<Vec2>(units.length)
  for (let i = 0; i < units.length; i++) {
    targets[unitOrder[i].index] = slots[slotOrder[i].index]
  }

  return targets
}

function getUnitCenter(units: Unit[]): Vec2 {
  const totals = units.reduce(
    (acc, unit) => {
      acc.x += unit.x
      acc.y += unit.y
      return acc
    },
    { x: 0, y: 0 },
  )

  return {
    x: totals.x / units.length,
    y: totals.y / units.length,
  }
}

function formatUnitType(unitType: UnitType) {
  switch (unitType) {
    case 'worker':
      return 'Worker Unit'
  }
}

function formatBuildingName(buildingType: BuildingTile['buildingType']) {
  switch (buildingType) {
    case 'goldmine':
      return 'Goldmine'
    case 'townhall':
      return 'Townhall'
    case 'tree':
      return 'Tree'
  }
}

function formatResourceLabel(resourceType: ResourceType) {
  switch (resourceType) {
    case 'gold':
      return 'Gold'
    case 'wood':
      return 'Wood'
  }
}

function getUnitActions(unit: Unit): ActionItem[] {
  return unit.capabilities.map((capability) => {
    switch (capability) {
      case 'build':
        return { id: 'build', label: 'Build' }
      case 'gather':
        return { id: 'gather', label: 'Gather' }
      default:
        return { id: capability, label: 'Move' }
    }
  })
}

function getGroupActions(units: Unit[]): ActionItem[] {
  const capabilities = new Set<UnitCapability>()

  for (const unit of units) {
    for (const capability of unit.capabilities) {
      capabilities.add(capability)
    }
  }

  return Array.from(capabilities).map((capability) => {
    switch (capability) {
      case 'build':
        return { id: 'build', label: 'Build' }
      case 'gather':
        return { id: 'gather', label: 'Gather' }
      default:
        return { id: capability, label: 'Move' }
    }
  })
}

function getBuildingActions(building: BuildingTile): ActionItem[] {
  const actions: ActionItem[] = []

  if (building.capabilities.includes('resource-source')) {
    const label = building.buildingType === 'tree' ? 'Chop Wood' : 'Harvest Gold'
    actions.push({ id: 'harvest', label })
  }
  if (
    building.capabilities.includes('unit-spawner') &&
    building.spawnUnitTypes?.includes('worker')
  ) {
    actions.push({ id: 'train-worker', label: 'Train Worker' })
    actions.push({ id: 'set-spawn-point', label: 'Set Spawn Point' })
  }

  return actions
}

function getBuildingDetails(building: BuildingTile): DetailItem[] {
  const activeProduction = getBuildingProductionState(building)
  if (activeProduction) {
    const nextQueuedUnit = activeProduction.queuedUnitTypes[1]
    const hiddenQueueCount = Math.max(activeProduction.queueLength - 2, 0)
    return [
      {
        id: 'current-training-unit',
        label: 'Training',
        value: formatSpawnUnitType(activeProduction.unitType),
      },
      {
        id: 'next-queued-unit',
        label: 'Next In Queue',
        value: nextQueuedUnit
          ? `${formatSpawnUnitType(nextQueuedUnit)}${hiddenQueueCount > 0 ? ` (${hiddenQueueCount})` : ''}`
          : 'None',
      },
    ]
  }

  const details: DetailItem[] = []

  const hp = getBuildingMetadataNumber(building, 'hp')
  const maxHp = getBuildingMetadataNumber(building, 'maxHp')
  if (hp !== undefined) {
    details.push({
      id: 'durability',
      label: 'Durability',
      value: `${hp} / ${maxHp ?? hp}`,
    })
  }

  const workerLabel = getBuildingResourceLabel(building)
  const workerAmount = getBuildingResourceAmount(building)
  if (workerLabel && workerAmount !== undefined) {
    details.push({
      id: 'workers-inside',
      label: workerLabel,
      value: String(workerAmount),
    })
  }

  const stockLabel = getBuildingStockLabel(building)
  const stockAmount = getBuildingStockAmount(building)
  if (stockLabel && stockAmount !== undefined) {
    details.push({
      id: 'resource-stock',
      label: stockLabel,
      value: String(stockAmount),
    })
  }

  if (building.capabilities.includes('deposit-point')) {
    details.push({ id: 'deposit-point', label: 'Deposit Point' })
  }
  if (building.capabilities.includes('occupiable')) {
    details.push({ id: 'occupiable', label: 'Occupiable' })
  }
  if (building.capabilities.includes('unit-spawner') && building.spawnUnitTypes?.length) {
    details.push({
      id: 'trains-units',
      label: 'Trains',
      value: building.spawnUnitTypes.map(formatSpawnUnitType).join(', '),
    })
  }

  const spawnPointLabel = getBuildingSpawnPointLabel(building)
  if (spawnPointLabel) {
    details.push({
      id: 'spawn-point',
      label: 'Spawn Point',
      value: spawnPointLabel,
    })
  }

  return details
}

function getUnitDetails(unit: Unit): DetailItem[] {
  const details: DetailItem[] = [
    {
      id: 'durability',
      label: 'Durability',
      value: `${unit.hp ?? 0} / ${unit.maxHp ?? unit.hp ?? 0}`,
    },
  ]

  if (unit.carriedResourceType && unit.carriedAmount !== undefined) {
    details.push({
      id: 'carried-resource',
      label: `${formatResourceLabel(unit.carriedResourceType)} Carried`,
      value: String(unit.carriedAmount),
    })
  }

  return details
}

function getGroupDetails(units: Unit[], totalHp: number, totalMaxHp: number): DetailItem[] {
  const details: DetailItem[] = [
    {
      id: 'durability',
      label: 'Durability',
      value: `${totalHp} / ${totalMaxHp}`,
    },
  ]

  const carryingGold = units.reduce(
    (sum, unit) => sum + (unit.carriedResourceType === 'gold' ? unit.carriedAmount ?? 0 : 0),
    0,
  )
  const carryingWood = units.reduce(
    (sum, unit) => sum + (unit.carriedResourceType === 'wood' ? unit.carriedAmount ?? 0 : 0),
    0,
  )

  if (carryingGold > 0) {
    details.push({ id: 'group-gold', label: 'Gold Carried', value: String(carryingGold) })
  }
  if (carryingWood > 0) {
    details.push({ id: 'group-wood', label: 'Wood Carried', value: String(carryingWood) })
  }

  return details
}

function summarizeWorkerGroupStatus(units: Unit[]) {
  const gathering = units.filter(
    (unit) => unit.status === 'Mining Gold' || unit.status === 'Chopping Wood',
  ).length
  const returning = units.filter(
    (unit) => unit.status === 'Returning Gold' || unit.status === 'Returning Wood',
  ).length
  const heading = units.filter(
    (unit) => unit.status === 'Heading To Mine' || unit.status === 'Heading To Tree',
  ).length

  if (gathering > 0) return `${gathering} Gathering, ${returning} Returning`
  if (heading > 0) return `${heading} Heading Out`
  if (returning > 0) return `${returning} Returning Resources`
  return 'Worker Crew'
}

function getBuildingResourceLabel(building: BuildingTile) {
  const currentWorkers = getBuildingMetadataNumber(building, 'currentWorkers')
  const maxWorkers = getBuildingMetadataNumber(building, 'maxWorkers')
  if (currentWorkers !== undefined && maxWorkers !== undefined) {
    const workerLabel = building.buildingType === 'tree' ? 'Chopping' : 'Workers Inside'
    return `${workerLabel} / ${maxWorkers} Max`
  }

  return building.resourceType ? formatResourceLabel(building.resourceType) : undefined
}

function getBuildingResourceAmount(building: BuildingTile) {
  const currentWorkers = getBuildingMetadataNumber(building, 'currentWorkers')
  if (currentWorkers !== undefined) {
    return currentWorkers
  }

  return building.resourceAmount
}

function getBuildingMetadataNumber(building: BuildingTile, key: string) {
  const value = building.metadata?.[key]
  return typeof value === 'number' ? value : undefined
}

function getBuildingMetadataString(building: BuildingTile, key: string) {
  const value = building.metadata?.[key]
  return typeof value === 'string' ? value : undefined
}

function getBuildingProductionState(building: BuildingTile) {
  const unitType = getBuildingMetadataString(building, 'producingUnitType')
  const remainingSeconds = getBuildingMetadataNumber(building, 'productionRemainingSeconds')
  const totalSeconds = getBuildingMetadataNumber(building, 'productionTotalSeconds')
  const queueLength = getBuildingMetadataNumber(building, 'productionQueueLength')
  const queuedUnitTypesRaw = getBuildingMetadataString(building, 'queuedUnitTypes')

  if (!unitType || remainingSeconds === undefined || totalSeconds === undefined) {
    return null
  }

  return {
    unitType,
    remainingSeconds,
    totalSeconds,
    queueLength: Math.max(1, Math.round(queueLength ?? 1)),
    queuedUnitTypes: queuedUnitTypesRaw
      ? queuedUnitTypesRaw.split(',').map((item) => item.trim()).filter(Boolean)
      : [unitType],
  }
}

function toProductionSummary(production: NonNullable<ReturnType<typeof getBuildingProductionState>>): ProductionSummary {
  const progress = production.totalSeconds > 0
    ? 1 - Math.max(0, Math.min(production.remainingSeconds / production.totalSeconds, 1))
    : 1

  return {
    ...production,
    progress,
    timeLabel: formatRemainingSeconds(production.remainingSeconds),
  }
}

function getBuildingSpawnPointLabel(building: BuildingTile) {
  const x = getBuildingMetadataNumber(building, 'spawnPointX')
  const y = getBuildingMetadataNumber(building, 'spawnPointY')
  if (x === undefined || y === undefined) return undefined
  return `${Math.round(x)}, ${Math.round(y)}`
}

function getBuildingStockLabel(building: BuildingTile): string | undefined {
  if (!building.resourceType) return undefined
  return formatResourceLabel(building.resourceType as ResourceType) + ' Remaining'
}

function getBuildingStockAmount(building: BuildingTile): number | undefined {
  if (!building.resourceType) return undefined
  return building.resourceAmount ?? 0
}

function formatSpawnUnitType(unitType: string) {
  switch (unitType) {
    case 'worker':
      return 'Worker'
    default:
      return unitType
  }
}

function formatRemainingSeconds(seconds: number) {
  if (seconds >= 10) {
    return `${seconds.toFixed(0)}s`
  }

  return `${seconds.toFixed(1)}s`
}

function clampBuildingSpawnPoint(map: MapConfig, _building: BuildingTile, point: Vec2): Vec2 {
  return {
    x: clamp(point.x, 10, map.width - 10),
    y: clamp(point.y, 10, map.height - 10),
  }
}

function clamp(value: number, min: number, max: number) {
  return Math.max(min, Math.min(max, value))
}
