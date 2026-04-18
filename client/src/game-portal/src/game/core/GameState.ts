import type {
  BuildingTile,
  MapConfig,
  MatchSnapshotMessage,
  PlayerSnapshot,
  ResourceType,
  UnitCapability,
  UnitType,
  WaveSnapshot,
} from '../network/protocol'
import { createEditorMapConfig, sanitizeMapConfig } from '../maps/mapConfig'
import { BUILDABLE_BUILDING_DEFS, BUILDING_DEF_MAP } from '../maps/buildingDefs'
import { UNIT_DEF_MAP } from '../maps/unitDefs'
import { PERK_DEF_MAP } from '../maps/perkDefs'

export type Unit = {
  id: number
  unitType: UnitType
  archetype?: string
  name: string
  capabilities: UnitCapability[]
  visible: boolean
  status?: string
  x: number
  y: number
  hp?: number
  maxHp?: number
  damage?: number
  attackSpeed?: number
  moveSpeed?: number
  armor?: number
  xp?: number
  rank?: string
  xpToNextRank?: number
  xpIntoCurrentRank?: number
  recentRankUpSeconds?: number
  path?: string
  perkIds?: string[]
  shield?: number
  maxShield?: number
  /** Perk IDs whose timed or conditional buff is currently active. */
  activeBuffs?: string[]
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
  /**
   * 'perk' marks a display-only perk slot in the bottom row of the action grid.
   * Absent means a regular interactive action button.
   */
  kind?: 'perk'
  /** Rank tier for perk slots — drives the rank-colored border in SelectionHud. */
  perkRank?: 'bronze' | 'silver' | 'gold'
  /** Tooltip header shown on hover for perk slots. */
  tooltipTitle?: string
  /** Tooltip body shown on hover for perk slots. */
  tooltipBody?: string
  disabled?: boolean
  active?: boolean
  iconDef?: { kind: 'building'; type: string } | { kind: 'unit'; type: string }
}

export type DetailItem = {
  id: string
  label: string
  value?: string
  tooltip?: string
  // SVG path (24×24 viewBox, stroke-style). When set, SelectionHud renders this
  // entry as an icon+value row instead of inline "Label: Value" text.
  icon?: string
}

// Stat icons used by unit detail rows. Stroke-style paths on a 24×24 viewBox,
// matching the existing action-icons.json conventions.
const STAT_ICON_HEART = 'M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z'
const STAT_ICON_SWORD = 'M14.5 17.5 L3 6 L3 3 L6 3 L17.5 14.5 M20 12 L12 20.5 M16.5 17.5 L20.5 21.5 L21.5 20.5 L17.5 16.5'
const STAT_ICON_BOOT = 'M6 3v11 M6 13h9v5 M3 18h18 M6 7h2 M6 10h2'
const STAT_ICON_SHIELD = 'M12 2L4 5v6c0 5.5 3.5 10 8 11 4.5-1 8-5.5 8-11V5z'

export type ProductionSummary = {
  unitType: string
  remainingSeconds: number
  totalSeconds: number
  queueLength: number
  queuedUnitTypes: string[]
  progress: number
  timeLabel: string
}

export type RepairSummary = {
  progress: number
  timeLabel: string
  builderCount: number
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
      /** Promotion-path label shown under the unit name (e.g. "Vanguard"). */
      pathLabel?: string
      /** Rank tier label shown in the primary panel (e.g. "Bronze"). */
      rankLabel?: string
      /** XP progress string shown in the primary panel (e.g. "120 / 250 XP"). */
      xpLabel?: string
    }
  | {
      kind: 'building'
      title: string
      subtitle: string
      details: DetailItem[]
      actions: ActionItem[]
      production?: ProductionSummary
      construction?: RepairSummary
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
export type UnitTargetingMode = 'move' | 'gather' | 'repair' | 'attack'

export type BuildPlacement = {
  buildingType: string
  gridW: number
  gridH: number
  cursorGridX: number
  cursorGridY: number
  valid: boolean
  builderUnitIds: number[]
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
  max?: number
  accent: string
}

export type Notification = {
  id: number
  message: string
  remaining: number
}

export class GameState {
  private resourceStocks: ResourceStock[] = [
    { id: 'gold', label: 'Gold', amount: 500, accent: '#d4a84f' },
    { id: 'wood', label: 'Wood', amount: 180, accent: '#7a9a52' },
    { id: 'food', label: 'Food', amount: 0, max: 0, accent: '#c96e43' },
  ]
  private playerColors = new Map<string, string>()
  private nextNotificationId = 0
  notifications: Notification[] = []

  units: Unit[] = []

  snapshotBuffer: InterpolationFrame[] = []
  interpolationDelayMs = 100
  maxBufferedSnapshots = 20

  localPlayerId: string | null = null

  // Current wave state, mirrored from the server snapshot every tick.
  waveSnapshot: WaveSnapshot = {
    enabled: false,
    currentWave: 0,
    totalWaves: 0,
    state: '',
    timer: 0,
    waveDuration: 0,
  }

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
  inspectedEnemyUnitId: number | null = null
  hoveredEnemyUnitId: number | null = null
  hoveredInteractableBuildingId: string | null = null
  buildingTargetingMode: BuildingTargetingMode | null = null
  unitTargetingMode: UnitTargetingMode | null = null
  workerBuildMenuOpen = false
  buildPlacement: BuildPlacement | null = null

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

  update(dt: number) {
    const now = performance.now()
    this.moveMarkers = this.moveMarkers.filter(
      (marker) => now - marker.createdAt < marker.durationMs,
    )
    this.notifications = this.notifications
      .map((n) => ({ ...n, remaining: n.remaining - dt }))
      .filter((n) => n.remaining > 0)
  }

  addNotification(message: string) {
    this.notifications.push({ id: this.nextNotificationId++, message, remaining: 2.5 })
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
        archetype: unit.archetype,
        name: unit.name,
        capabilities: unit.capabilities ?? [],
        visible: unit.visible,
        status: unit.status,
        x: unit.x,
        y: unit.y,
        hp: unit.hp,
        maxHp: unit.maxHp,
        damage: unit.damage,
        attackSpeed: unit.attackSpeed,
        moveSpeed: unit.moveSpeed,
        armor: unit.armor,
        xp: unit.xp,
        rank: unit.rank,
        xpToNextRank: unit.xpToNextRank,
        xpIntoCurrentRank: unit.xpIntoCurrentRank,
        recentRankUpSeconds: unit.recentRankUpSeconds,
        path: unit.progressionPath,
        perkIds: unit.perkIds,
        shield: unit.shield,
        maxShield: unit.maxShield,
        activeBuffs: unit.activeBuffs,
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
    if (message.wave) {
      this.waveSnapshot = message.wave
    }

    const validIds = new Set(this.units.map((u) => u.id))

    for (const id of Array.from(this.selectedUnitIds)) {
      if (!validIds.has(id)) {
        this.selectedUnitIds.delete(id)
      }
    }

    this.selectedUnitOrder = this.selectedUnitOrder.filter((id) =>
      validIds.has(id),
    )

    if (this.selectedUnitOrder.length === 0) {
      this.unitTargetingMode = null
    }

    if (this.inspectedEnemyUnitId !== null && !validIds.has(this.inspectedEnemyUnitId)) {
      this.inspectedEnemyUnitId = null
    }
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
    this.inspectedEnemyUnitId = null
    this.buildingTargetingMode = null
    this.unitTargetingMode = null
    this.workerBuildMenuOpen = false
    this.buildPlacement = null
  }

  selectUnit(unitId: number) {
    const unit = this.units.find((u) => u.id === unitId)
    if (!unit || !this.isOwnedByLocalPlayer(unit)) return

    this.selectedUnitIds.clear()
    this.selectedUnitIds.add(unitId)
    this.selectedUnitOrder = [unitId]
    this.selectedBuildingId = null
    this.inspectedEnemyUnitId = null
    this.buildingTargetingMode = null
    this.unitTargetingMode = null
    this.workerBuildMenuOpen = false
    this.buildPlacement = null
  }

  setSelection(unitIds: number[]) {
    const ownedIds = unitIds.filter((id) => {
      const unit = this.units.find((u) => u.id === id)
      return !!unit && this.isOwnedByLocalPlayer(unit)
    })

    this.selectedUnitIds.clear()
    this.selectedBuildingId = null
    this.buildingTargetingMode = null
    this.unitTargetingMode = null
    this.workerBuildMenuOpen = false
    this.buildPlacement = null

    for (const id of ownedIds) {
      this.selectedUnitIds.add(id)
    }

    this.selectedUnitOrder = [...ownedIds]
  }

  openWorkerBuildMenu() {
    const selectedUnits = this.getSelectedUnits()
    if (!selectedUnits.some((u) => u.capabilities.includes('build'))) return
    this.workerBuildMenuOpen = true
  }

  closeWorkerBuildMenu() {
    this.workerBuildMenuOpen = false
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

  getEnemyUnitAtPosition(x: number, y: number, radius = 14): Unit | undefined {
    return this.getInteractionUnits().find((unit) => {
      if (this.isOwnedByLocalPlayer(unit) || !unit.visible) return false

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
    this.inspectedEnemyUnitId = null
    this.buildingTargetingMode = null
    this.unitTargetingMode = null
    this.workerBuildMenuOpen = false
    this.buildPlacement = null
  }

  getSelectedBuilding(): BuildingTile | null {
    if (!this.selectedBuildingId) return null
    return this.mapConfig.buildings.find((building) => building.id === this.selectedBuildingId) ?? null
  }

  beginBuildingTargeting(mode: BuildingTargetingMode) {
    if (!this.getSelectedBuilding()) return
    this.unitTargetingMode = null
    this.buildingTargetingMode = mode
  }

  cancelBuildingTargeting() {
    this.buildingTargetingMode = null
  }

  beginUnitTargeting(mode: UnitTargetingMode) {
    if (this.getSelectedUnits().length === 0) return
    if (mode === 'gather' && !this.selectedUnitsCanGather()) return
    if (mode === 'repair' && !this.selectedUnitsCanBuild()) return

    this.selectedBuildingId = null
    this.buildingTargetingMode = null
    this.unitTargetingMode = mode
  }

  cancelUnitTargeting() {
    this.unitTargetingMode = null
  }

  isUnitTargetingActive(mode?: UnitTargetingMode) {
    if (!this.unitTargetingMode) return false
    return mode ? this.unitTargetingMode === mode : true
  }

  isAnyTargetingActive() {
    return this.isBuildingTargetingActive() || this.isUnitTargetingActive() || this.buildPlacement !== null
  }

  isBuildPlacementActive(): boolean {
    return this.buildPlacement !== null
  }

  beginBuildPlacement(buildingType: string, builderUnitIds: number[]) {
    const def = BUILDING_DEF_MAP.get(buildingType)
    const gridW = def?.width ?? 2
    const gridH = def?.height ?? 2
    const { gridCols, gridRows } = this.mapConfig
    const startX = Math.max(0, Math.floor(gridCols / 2) - 1)
    const startY = Math.max(0, Math.floor(gridRows / 2) - 1)

    this.buildPlacement = {
      buildingType,
      gridW,
      gridH,
      cursorGridX: startX,
      cursorGridY: startY,
      valid: this.isBuildPlacementCellsValid(startX, startY, gridW, gridH),
      builderUnitIds,
    }
    this.workerBuildMenuOpen = false
  }

  cancelBuildPlacement() {
    this.buildPlacement = null
  }

  updateBuildPlacement(worldX: number, worldY: number) {
    if (!this.buildPlacement) return

    const { cellSize, gridCols, gridRows } = this.mapConfig
    const { gridW, gridH } = this.buildPlacement

    const rawGridX = Math.round(worldX / cellSize - gridW / 2)
    const rawGridY = Math.round(worldY / cellSize - gridH / 2)
    const gridX = Math.max(0, Math.min(rawGridX, gridCols - gridW))
    const gridY = Math.max(0, Math.min(rawGridY, gridRows - gridH))

    this.buildPlacement = {
      ...this.buildPlacement,
      cursorGridX: gridX,
      cursorGridY: gridY,
      valid: this.isBuildPlacementCellsValid(gridX, gridY, gridW, gridH),
    }
  }

  private isBuildPlacementCellsValid(gridX: number, gridY: number, gridW: number, gridH: number): boolean {
    const { gridCols, gridRows, buildings, obstacles, cellSize } = this.mapConfig

    if (gridX < 0 || gridY < 0 || gridX + gridW > gridCols || gridY + gridH > gridRows) {
      return false
    }

    for (const obs of obstacles) {
      if (obs.x >= gridX && obs.x < gridX + gridW && obs.y >= gridY && obs.y < gridY + gridH) {
        return false
      }
    }

    for (const building of buildings) {
      if (!building.visible) continue

      const bRight = building.x + building.width
      const bBottom = building.y + building.height
      const pRight = gridX + gridW
      const pBottom = gridY + gridH

      if (gridX < bRight && pRight > building.x && gridY < bBottom && pBottom > building.y) {
        return false
      }
    }

    for (const unit of this.units) {
      if (!unit.visible) continue
      const ux = Math.floor(unit.x / cellSize)
      const uy = Math.floor(unit.y / cellSize)
      if (ux >= gridX && ux < gridX + gridW && uy >= gridY && uy < gridY + gridH) {
        return false
      }
    }

    return true
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

  getSelectedBuildingSpawnPointTarget(worldX: number, worldY: number): Vec2 | null {
    const building = this.getSelectedBuilding()
    if (!building) return null
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

  setHoveredEnemyUnit(unitId: number | null) {
    this.hoveredEnemyUnitId = unitId
  }

  inspectEnemyUnit(unitId: number) {
    const unit = this.units.find((u) => u.id === unitId)
    if (!unit || this.isOwnedByLocalPlayer(unit)) return

    this.selectedUnitIds.clear()
    this.selectedUnitOrder = []
    this.selectedBuildingId = null
    this.inspectedEnemyUnitId = unitId
    this.buildingTargetingMode = null
    this.unitTargetingMode = null
    this.workerBuildMenuOpen = false
    this.buildPlacement = null
  }

  selectedUnitsCanAttack(): boolean {
    const units = this.getSelectedUnits()
    return units.length > 0 && units.some((unit) => unit.capabilities.includes('attack'))
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
      this.buildingTargetingMode = null
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

  selectedUnitsCanBuild(): boolean {
    const units = this.getSelectedUnits()
    return units.length > 0 && units.some((unit) => unit.capabilities.includes('build'))
  }

  hasUnderConstructionBuildings(): boolean {
    return this.mapConfig.buildings.some(
      (b) => b.ownerId === this.localPlayerId && b.metadata?.['underConstruction'] === true,
    )
  }

  getSelectionSummary(): SelectionSummary {
    if (this.inspectedEnemyUnitId !== null && this.selectedUnitIds.size === 0) {
      const unit = this.units.find((u) => u.id === this.inspectedEnemyUnitId)
      if (unit) {
        return {
          kind: 'unit',
          title: `Enemy ${unit.name}`,
          subtitle: unit.status ?? 'Hostile',
          details: getUnitDetails(unit),
          actions: [],
          pathLabel: unit.path && unit.path !== 'none' ? formatUnitPath(unit.path) : undefined,
          rankLabel: formatUnitRank(unit.rank),
          xpLabel: getUnitXpLabel(unit),
        }
      }
      this.inspectedEnemyUnitId = null
    }

    const selectedBuilding = this.getSelectedBuilding()
    if (selectedBuilding) {
      const title = formatBuildingName(selectedBuilding.buildingType)
      const activeProduction = getBuildingProductionState(selectedBuilding)
      const isUnderConstruction = selectedBuilding.metadata?.['underConstruction'] === true
      const defaultSubtitle = selectedBuilding.ownerId
        ? `Owned by ${selectedBuilding.ownerId}`
        : selectedBuilding.occupied
          ? 'Occupied'
          : 'Neutral'
        const subtitle = isUnderConstruction
          ? 'Under Construction'
          : this.buildingTargetingMode === 'set-spawn-point'
            ? 'Click anywhere on the map to set the rally point target.'
            : activeProduction
              ? `Training ${formatSpawnUnitType(activeProduction.unitType)}`
              : defaultSubtitle

      return {
        kind: 'building',
        title,
        subtitle,
        details: getBuildingDetails(selectedBuilding),
        actions: isUnderConstruction ? [] : getBuildingActions(selectedBuilding),
        production: activeProduction ? toProductionSummary(activeProduction) : undefined,
        construction: isUnderConstruction
          ? getBuildingConstructionSummary(selectedBuilding)
          : undefined,
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
      const buildMenuOpen = this.workerBuildMenuOpen && unit.capabilities.includes('build')
      const placementActive = this.buildPlacement !== null

      // Regular actions occupy slots 1–6 (top two rows of the 3×3 grid).
      // Perk items always occupy slots 7–9 (bottom row): bronze, silver, gold.
      // When the build menu is open the full 9 slots are used for building
      // choices, so we skip the perk row in that state.
      const regularActions = getUnitActions(unit, this.unitTargetingMode, buildMenuOpen)
      const actions = buildMenuOpen
        ? regularActions
        : [
            ...regularActions,
            // Pad to 6 so perks always land in the bottom row regardless of
            // how many regular action slots are filled.
            ...Array<ActionItem>(Math.max(0, 6 - regularActions.length)).fill({
              id: '',
              label: '',
              disabled: true,
            }),
            ...getPerkActionItems(unit),
          ]

      return {
        kind: 'unit',
        title: unit.name,
        subtitle: placementActive
          ? 'Click to place the Barracks. Right-click to cancel.'
          : buildMenuOpen
            ? 'Choose a structure to build.'
            : getSelectionUnitSubtitle(
                unit.status || formatUnitType(unit.unitType),
                this.unitTargetingMode,
              ),
        details: getUnitDetails(unit),
        actions,
        pathLabel: unit.path && unit.path !== 'none' ? formatUnitPath(unit.path) : undefined,
        rankLabel: formatUnitRank(unit.rank),
        xpLabel: getUnitXpLabel(unit),
      }
    }

    const totalHp = selectedUnits.reduce((sum, unit) => sum + (unit.hp ?? 0), 0)
    const totalMaxHp = selectedUnits.reduce((sum, unit) => sum + (unit.maxHp ?? unit.hp ?? 0), 0)
    const groupBuildMenuOpen =
      this.workerBuildMenuOpen && selectedUnits.every((u) => u.capabilities.includes('build'))

    return {
      kind: 'group',
      title: `${selectedUnits.length} Units Selected`,
      subtitle: groupBuildMenuOpen
        ? 'Choose a structure to build.'
        : getSelectionUnitSubtitle(
            selectedUnits.every((unit) => unit.unitType === 'worker')
              ? summarizeWorkerGroupStatus(selectedUnits)
              : 'Mixed Detachment',
            this.unitTargetingMode,
          ),
      details: getGroupDetails(selectedUnits, totalHp, totalMaxHp),
      actions: getGroupActions(selectedUnits, this.unitTargetingMode, groupBuildMenuOpen),
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

  getWaveSnapshot(): WaveSnapshot {
    return this.waveSnapshot
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
      max: resource.max,
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

function formatUnitType(unitType: UnitType): string {
  const def = UNIT_DEF_MAP.get(unitType)
  return def ? `${def.name} Unit` : unitType
}

function formatBuildingName(buildingType: BuildingTile['buildingType']): string {
  switch (buildingType) {
    case 'goldmine':
      return 'Goldmine'
    case 'townhall':
      return 'Townhall'
    case 'tree':
      return 'Tree'
    case 'barracks':
      return 'Barracks'
    case 'farm':
      return 'Farm'
    case 'enemy-spawnpoint':
      return 'Enemy Spawnpoint'
    case 'spawn-point':
      return 'Rally Point'
    default:
      return buildingType
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

function getBuildingConstructionSummary(building: BuildingTile): RepairSummary | undefined {
  const hp = getBuildingMetadataNumber(building, 'hp')
  const maxHp = getBuildingMetadataNumber(building, 'maxHp')
  if (hp === undefined || maxHp === undefined || maxHp <= 0) return undefined

  const progress = Math.max(0, Math.min(1, hp / maxHp))
  const builderCount = (building.metadata?.['builderCount'] as number | undefined) ?? 0
  const remainingHp = Math.max(0, maxHp - hp)
  const secondsPerHpPerBuilder = 15 / 500
  const remainingSeconds =
    builderCount > 0 ? remainingHp * secondsPerHpPerBuilder / builderCount : undefined

  return {
    progress,
    timeLabel: remainingSeconds !== undefined ? formatRemainingSeconds(remainingSeconds) : 'Paused',
    builderCount,
  }
}

// ─────────────────────────────────────────────────────────────────────────────
// Perk action items
//
// Returns exactly 3 ActionItems — one per rank tier (bronze / silver / gold) —
// that always occupy the bottom row of the 3×3 action grid.
//
// Each slot shows the assigned perk icon for that rank, or a generic locked
// icon if no perk has been assigned yet. Slots are display-only (kind: 'perk');
// the SelectionHud renders them with rank-tinted borders and no click handler.
//
// TO ADD A NEW RANK TIER: append its name to the `ranks` array below and add
// a matching CSS class in SelectionHud.vue.
// ─────────────────────────────────────────────────────────────────────────────
const PERK_RANKS: Array<'bronze' | 'silver' | 'gold'> = ['bronze', 'silver', 'gold']

function getPerkActionItems(unit: Unit): ActionItem[] {
  // Perks are appended in rank-up order — index 0 = Bronze grant, 1 = Silver, 2 = Gold.
  return PERK_RANKS.map((rank, i) => {
    const perkId = unit.perkIds?.[i]
    const def = perkId ? PERK_DEF_MAP.get(perkId) : undefined

    const rankLabel = rank.charAt(0).toUpperCase() + rank.slice(1)

    if (def) {
      return {
        id: def.icon ?? 'perk-locked',
        label: def.displayName,
        kind: 'perk' as const,
        perkRank: rank,
        tooltipTitle: `${def.displayName} (${rankLabel})`,
        tooltipBody: def.description,
        disabled: true,
      }
    }

    // Locked / empty slot for ranks the unit hasn't reached yet.
    return {
      id: 'perk-locked',
      label: `${rankLabel} Perk (locked)`,
      kind: 'perk' as const,
      perkRank: rank,
      tooltipTitle: `${rankLabel} Perk`,
      tooltipBody: 'Locked — earn this rank to unlock.',
      disabled: true,
    }
  })
}

function getUnitActions(
  unit: Unit,
  activeMode: UnitTargetingMode | null,
  buildMenuOpen: boolean,
): ActionItem[] {
  if (buildMenuOpen) {
    const actions: ActionItem[] = []
    BUILDABLE_BUILDING_DEFS.forEach((def, i) => {
      actions[i] = { id: `build-${def.type}`, label: def.label, iconDef: { kind: 'building', type: def.type } }
    })
    actions[6] = { id: 'close-build-menu', label: 'E(x)it' }
    return actions
  }
  const actions = unit.capabilities.map((capability) => {
    switch (capability) {
      case 'build':
        return { id: 'build', label: '(B)uild' }
      case 'gather':
        return { id: 'gather', label: '(G)ather', active: activeMode === 'gather' }
      case 'attack':
        return { id: 'attack', label: '(A)ttack', active: activeMode === 'attack' }
      default:
        return { id: capability, label: '(M)ove', active: activeMode === 'move' }
    }
  })
  if (unit.capabilities.includes('build')) {
    actions.push({ id: 'repair', label: '(R)epair', active: activeMode === 'repair' })
  }
  return actions
}

function getGroupActions(
  units: Unit[],
  activeMode: UnitTargetingMode | null,
  buildMenuOpen: boolean,
): ActionItem[] {
  if (buildMenuOpen) {
    const actions: ActionItem[] = []
    BUILDABLE_BUILDING_DEFS.forEach((def, i) => {
      actions[i] = { id: `build-${def.type}`, label: def.label, iconDef: { kind: 'building', type: def.type } }
    })
    actions[6] = { id: 'close-build-menu', label: 'E(x)it' }
    return actions
  }

  const capabilities = new Set<UnitCapability>()

  for (const unit of units) {
    for (const capability of unit.capabilities) {
      capabilities.add(capability)
    }
  }

  const actions = Array.from(capabilities).map((capability) => {
    switch (capability) {
      case 'build':
        return { id: 'build', label: '(B)uild' }
      case 'gather':
        return { id: 'gather', label: '(G)ather', active: activeMode === 'gather' }
      case 'attack':
        return { id: 'attack', label: '(A)ttack', active: activeMode === 'attack' }
      default:
        return { id: capability, label: '(M)ove', active: activeMode === 'move' }
    }
  })
  if (capabilities.has('build')) {
    actions.push({ id: 'repair', label: '(R)epair', active: activeMode === 'repair' })
  }
  return actions
}

function getBuildingActions(building: BuildingTile): ActionItem[] {
  const actions: ActionItem[] = []

  if (building.capabilities.includes('unit-spawner')) {
    let hasTrainable = false
    for (const unitType of building.spawnUnitTypes ?? []) {
      const def = UNIT_DEF_MAP.get(unitType)
      if (def) {
        actions.push({ id: `train-${unitType}`, label: def.trainLabel, iconDef: { kind: 'unit', type: unitType } })
        hasTrainable = true
      }
    }
    if (hasTrainable) {
      actions.push({ id: 'set-spawn-point', label: 'Set Rally Point' })
    }
  }

  return actions
}

function getBuildingDetails(building: BuildingTile): DetailItem[] {
  const hp = getBuildingMetadataNumber(building, 'hp')
  const maxHp = getBuildingMetadataNumber(building, 'maxHp')
  const isUnderConstruction = building.metadata?.['underConstruction'] === true
  if (isUnderConstruction) {
    const details: DetailItem[] = []
    if (hp !== undefined && maxHp !== undefined && maxHp > 0) {
      const pct = Math.round((hp / maxHp) * 100)
      const builderCount = getBuildingMetadataNumber(building, 'builderCount') ?? 0
      const remainingHp = Math.max(0, maxHp - hp)
      const secondsPerHpPerBuilder = 15 / 500
      const remainingSeconds =
        builderCount > 0 ? remainingHp * secondsPerHpPerBuilder / builderCount : undefined

      details.push({
        id: 'construction-health',
        label: 'Durability',
        value: formatDurability(hp, maxHp),
      })
      details.push({ id: 'construction-progress', label: 'Progress', value: `${pct}%` })
      details.push({
        id: 'construction-time',
        label: 'Build Time',
        value: remainingSeconds !== undefined ? formatRemainingSeconds(remainingSeconds) : 'Paused',
      })
      details.push({
        id: 'construction-builders',
        label: 'Builders',
        value: `${builderCount} / 3`,
      })
    }
    return details
  }

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

  if (hp !== undefined) {
    details.push({
      id: 'durability',
      label: 'Durability',
      value: formatDurability(hp, maxHp ?? hp),
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
  const buildingDef = BUILDING_DEF_MAP.get(building.buildingType)
  if ((buildingDef?.damage ?? 0) > 0) {
    details.push({
      id: 'damage',
      label: 'Damage',
      value: String(buildingDef?.damage ?? 0),
    })
  }
  if ((buildingDef?.attackRange ?? 0) > 0) {
    details.push({
      id: 'attack-range',
      label: 'Range',
      value: String(Math.round(buildingDef?.attackRange ?? 0)),
    })
  }
  if ((buildingDef?.attackSpeed ?? 0) > 0) {
    details.push({
      id: 'attack-speed',
      label: 'Attack Speed',
      value: `${buildingDef?.attackSpeed ?? 0}/s`,
    })
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
        label: 'Rally Point',
        value: spawnPointLabel,
      })
  }

  return details
}

// Mirrors server/internal/game/progression.go armorDamageReduction — keep in sync.
// reduction = armor / (armor + K) where K = 100.
const ARMOR_MITIGATION_K = 100

function armorDamageReductionFraction(armor: number): number {
  if (armor <= 0) return 0
  return armor / (armor + ARMOR_MITIGATION_K)
}

function getUnitDetails(unit: Unit): DetailItem[] {
  const details: DetailItem[] = [
    {
      id: 'durability',
      label: 'Durability',
      value: `${unit.hp ?? 0} / ${unit.maxHp ?? unit.hp ?? 0}`,
      icon: STAT_ICON_HEART,
    },
  ]

  // Damage and attack speed share one row — the sword icon covers both.
  const hasDamage = (unit.damage ?? 0) > 0
  const hasAttackSpeed = (unit.attackSpeed ?? 0) > 0
  if (hasDamage || hasAttackSpeed) {
    const parts: string[] = []
    if (hasDamage) parts.push(String(unit.damage))
    if (hasAttackSpeed) parts.push(`${unit.attackSpeed!.toFixed(2)}/s`)
    details.push({
      id: 'attack',
      label: 'Damage / Attack Speed',
      value: parts.join(' · '),
      icon: STAT_ICON_SWORD,
    })
  }

  if (unit.moveSpeed !== undefined && unit.moveSpeed > 0) {
    details.push({
      id: 'move-speed',
      label: 'Move Speed',
      value: String(Math.round(unit.moveSpeed)),
      icon: STAT_ICON_BOOT,
    })
  }

  if (unit.armor !== undefined && unit.armor > 0) {
    const reductionPct = Math.round(armorDamageReductionFraction(unit.armor) * 100)
    details.push({
      id: 'armor',
      label: 'Armor',
      value: String(unit.armor),
      tooltip: `${reductionPct}% damage reduction`,
      icon: STAT_ICON_SHIELD,
    })
  }

  // Non-stat details without icons — rendered inline below the stat grid.
  // Shield is only granted by blood_engine; only show when the unit can
  // actually carry shield so stateless units never show "Shield 0/0".
  if ((unit.maxShield ?? 0) > 0) {
    details.push({
      id: 'shield',
      label: 'Shield',
      value: `${unit.shield ?? 0} / ${unit.maxShield ?? 0}`,
    })
  }

  if (unit.carriedResourceType && unit.carriedAmount !== undefined) {
    details.push({
      id: 'carried-resource',
      label: `${formatResourceLabel(unit.carriedResourceType)} Carried`,
      value: String(unit.carriedAmount),
    })
  }

  return details
}

function getUnitXpLabel(unit: Unit): string {
  const xp = unit.xp ?? 0
  if (unit.xpToNextRank && unit.xpToNextRank > 0) {
    const intoCurrent = unit.xpIntoCurrentRank ?? 0
    const rankTotal = intoCurrent + unit.xpToNextRank
    return `${intoCurrent} / ${rankTotal} XP`
  }
  return `${xp} XP (max)`
}

function getGroupDetails(units: Unit[], totalHp: number, totalMaxHp: number): DetailItem[] {
  const details: DetailItem[] = [
    {
      id: 'durability',
      label: 'Durability',
      value: `${totalHp} / ${totalMaxHp}`,
    },
  ]

  const ranks = new Map<string, number>()
  for (const unit of units) {
    const rank = formatUnitRank(unit.rank)
    ranks.set(rank, (ranks.get(rank) ?? 0) + 1)
  }
  if (ranks.size > 0) {
    details.push({
      id: 'group-ranks',
      label: 'Ranks',
      value: [...ranks.entries()].map(([rank, count]) => `${rank} x${count}`).join(', '),
    })
  }

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

function formatUnitPath(path?: string) {
  switch (path) {
    case 'vanguard':
      return 'Vanguard'
    case 'berserker':
      return 'Berserker'
    default:
      return ''
  }
}

function formatUnitRank(rank?: string) {
  switch (rank) {
    case 'bronze':
      return 'Bronze'
    case 'silver':
      return 'Silver'
    case 'gold':
      return 'Gold'
    default:
      return 'Base'
  }
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

function formatSpawnUnitType(unitType: string): string {
  return UNIT_DEF_MAP.get(unitType)?.name ?? unitType
}

function formatRemainingSeconds(seconds: number) {
  if (seconds >= 10) {
    return `${seconds.toFixed(0)}s`
  }

  return `${seconds.toFixed(1)}s`
}

function formatDurability(current: number, max: number) {
  return `${Math.round(current)} / ${Math.round(max)}`
}

function getSelectionUnitSubtitle(baseSubtitle: string, unitTargetingMode: UnitTargetingMode | null) {
  switch (unitTargetingMode) {
    case 'move':
      return 'Move order ready. Left-click a destination.'
    case 'gather':
      return 'Gather order ready. Left-click a goldmine or tree.'
    case 'repair':
      return 'Repair/build ready. Left-click a building under construction.'
    default:
      return baseSubtitle
  }
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
