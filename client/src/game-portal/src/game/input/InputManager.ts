// src/game/input/InputManager.ts
import { GameState } from '../core/GameState'
import type { GameClient } from '../core/GameClient'
import { Camera } from '../rendering/Camera'
import { getMinimapBounds } from '../rendering/CanvasRenderer'
import { NetworkClient } from '../network/NetworkClient'

export class InputManager {
  private canvas: HTMLCanvasElement
  private state: GameState
  private client: GameClient
  private camera: Camera
  private network: NetworkClient

  private isLeftMouseDown = false
  private isMiddleMouseDown = false
  private isSpaceHeld = false

  private dragThreshold = 6
  private dragStarted = false
  private isSpacePanning = false
  private isMinimapNavigating = false

  private lastMouseX = 0
  private lastMouseY = 0
  private readonly moveCursor = `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='28' height='28' viewBox='0 0 28 28'%3E%3Cg fill='none' stroke='%230f172a' stroke-width='2.2' stroke-linecap='round' stroke-linejoin='round'%3E%3Ccircle cx='14' cy='14' r='10.5' fill='rgba(125,220,255,0.92)'/%3E%3Cpath d='M14 5v18M5 14h18'/%3E%3Cpath d='M14 5l-3 3M14 5l3 3M23 14l-3-3M23 14l-3 3M14 23l-3-3M14 23l3-3M5 14l3-3M5 14l3 3'/%3E%3C/g%3E%3C/svg%3E") 14 14, pointer`
  private readonly gatherCursor = `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='28' height='28' viewBox='0 0 28 28'%3E%3Cg fill='none' fill-rule='evenodd'%3E%3Ccircle cx='14' cy='14' r='11' fill='rgba(202,138,4,0.95)' stroke='rgba(15,23,42,0.9)' stroke-width='2'/%3E%3Cpath d='M9 16l4.2-4.4 2.6 2.6L12 18z' fill='%23fff7d6'/%3E%3Cpath d='M15.6 9.4l3 3' stroke='%230f172a' stroke-width='2.2' stroke-linecap='round'/%3E%3C/g%3E%3C/svg%3E") 14 14, pointer`
  private readonly repairCursor = `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='28' height='28' viewBox='0 0 28 28'%3E%3Ccircle cx='14' cy='14' r='11' fill='rgba(74,222,128,0.92)' stroke='rgba(15,23,42,0.9)' stroke-width='2'/%3E%3Cpath d='M9 14h10M14 9v10' stroke='%230f172a' stroke-width='2.5' stroke-linecap='round'/%3E%3Cpath d='M10 10l8 8M18 10l-8 8' stroke='rgba(15,23,42,0.25)' stroke-width='1.2' stroke-linecap='round'/%3E%3C/svg%3E") 14 14, pointer`
  private readonly attackCursor = `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='28' height='28' viewBox='0 0 28 28'%3E%3Ccircle cx='14' cy='14' r='11' fill='rgba(239,68,68,0.92)' stroke='rgba(15,23,42,0.9)' stroke-width='2'/%3E%3Cg stroke='%230f172a' stroke-width='2.2' stroke-linecap='round'%3E%3Cpath d='M18 10l-8 8M10 10l2 2-4 4 2 2 4-4 2 2 4-4-2-2-2 2-2-2z'/%3E%3C/g%3E%3C/svg%3E") 14 14, crosshair`

  constructor(
    canvas: HTMLCanvasElement,
    state: GameState,
    client: GameClient,
    camera: Camera,
    network: NetworkClient,
  ) {
    this.canvas = canvas
    this.state = state
    this.client = client
    this.camera = camera
    this.network = network

    canvas.addEventListener('contextmenu', this.onRightClick)
    canvas.addEventListener('mousedown', this.onMouseDown)
    canvas.addEventListener('mousemove', this.onMouseMove)
    canvas.addEventListener('mouseleave', this.onMouseLeave)
    canvas.addEventListener('wheel', this.onWheel, { passive: false })

    window.addEventListener('mouseup', this.onMouseUp)
    window.addEventListener('keydown', this.onKeyDown)
    window.addEventListener('keyup', this.onKeyUp)
  }

  private getScreenPosition(e: MouseEvent | WheelEvent) {
    const rect = this.canvas.getBoundingClientRect()
    return {
      x: e.clientX - rect.left,
      y: e.clientY - rect.top,
    }
  }

  private getWorldPosition(e: MouseEvent | WheelEvent) {
    const screen = this.getScreenPosition(e)
    return this.camera.screenToWorld(screen.x, screen.y)
  }

  private onKeyDown = (e: KeyboardEvent) => {
    if (e.code === 'Space') {
      this.isSpaceHeld = true
      this.canvas.style.cursor = 'grab'
      e.preventDefault()
      return
    }

    if (e.repeat || e.altKey || e.ctrlKey || e.metaKey) {
      return
    }

    if ((e.key.toLowerCase() === 'x' || e.code === 'KeyX') && this.hasVisibleAction('close-build-menu')) {
      this.client.performSelectionAction('close-build-menu')
      e.preventDefault()
      return
    }

    const actionId = this.getHotkeyAction(e)
    if (!actionId) {
      return
    }

    this.client.performSelectionAction(actionId)
    e.preventDefault()
  }

  private onKeyUp = (e: KeyboardEvent) => {
    if (e.code === 'Space') {
      this.isSpaceHeld = false
      this.isSpacePanning = false
      this.updateHoverCursor(this.lastMouseX, this.lastMouseY)
      e.preventDefault()
    }
  }

  private onMouseDown = (e: MouseEvent) => {
    const screen = this.getScreenPosition(e)

    this.lastMouseX = screen.x
    this.lastMouseY = screen.y

    if (e.button === 0) {
      if (this.isInsideMinimap(screen.x, screen.y)) {
        this.isLeftMouseDown = true
        this.isMinimapNavigating = true
        this.dragStarted = false
        this.centerCameraFromMinimap(screen.x, screen.y)
        return
      }

      const world = this.getWorldPosition(e)
      if (!this.isSpaceHeld && this.client.tryHandleWorldClick(world.x, world.y)) {
        this.isLeftMouseDown = false
        this.dragStarted = false
        this.state.endSelectionBox()
        return
      }
      this.isLeftMouseDown = true
      this.dragStarted = false

      if (this.isSpaceHeld) {
        this.isSpacePanning = true
        this.canvas.style.cursor = 'grabbing'
        return
      }

      this.state.beginSelectionBox(world.x, world.y)
      return
    }

    if (e.button === 1) {
      e.preventDefault()
      this.isMiddleMouseDown = true
    }
  }

  private onMouseMove = (e: MouseEvent) => {
    const screen = this.getScreenPosition(e)
    this.updateHoverCursor(screen.x, screen.y)

    if (this.state.isBuildPlacementActive()) {
      const world = this.getWorldPosition(e)
      this.state.updateBuildPlacement(world.x, world.y)
    }

    if (this.isLeftMouseDown && this.isMinimapNavigating) {
      this.centerCameraFromMinimap(screen.x, screen.y)
      this.lastMouseX = screen.x
      this.lastMouseY = screen.y
      return
    }

    if (this.isMiddleMouseDown || (this.isLeftMouseDown && this.isSpacePanning)) {
      const dx = screen.x - this.lastMouseX
      const dy = screen.y - this.lastMouseY

      this.camera.pan(-dx / this.camera.zoom, -dy / this.camera.zoom)
      this.camera.clamp(
        this.canvas.width,
        this.canvas.height,
        this.state.mapWidth,
        this.state.mapHeight,
      )

      this.lastMouseX = screen.x
      this.lastMouseY = screen.y
      return
    }

    if (!this.isLeftMouseDown) return

    const world = this.getWorldPosition(e)
    this.state.updateSelectionBox(world.x, world.y)

    const bounds = this.state.getSelectionBounds()
    const width = bounds.right - bounds.left
    const height = bounds.bottom - bounds.top

    if (width > this.dragThreshold || height > this.dragThreshold) {
      this.dragStarted = true
    }
  }

  private onMouseUp = (e: MouseEvent) => {
    if (e.button === 1) {
      this.isMiddleMouseDown = false
      return
    }

    if (e.button !== 0 || !this.isLeftMouseDown) return

    if (this.isMinimapNavigating) {
      this.isLeftMouseDown = false
      this.isMinimapNavigating = false
      this.state.endSelectionBox()
      return
    }

    if (this.isSpacePanning) {
      this.isLeftMouseDown = false
      this.isSpacePanning = false
      this.canvas.style.cursor = this.isSpaceHeld ? 'grab' : 'default'
      return
    }

    const world = this.getWorldPosition(e)
    const isShiftHeld = e.shiftKey

    if (this.dragStarted) {
      this.state.updateSelectionBox(world.x, world.y)

      if (isShiftHeld) {
        this.state.addUnitsInBox()
      } else {
        this.state.selectUnitsInBox()
      }
    } else {
      const clickedUnit = this.state.getUnitAtPosition(world.x, world.y)
      const clickedBuilding = this.state.getBuildingAtPosition(world.x, world.y)

      if (clickedUnit) {
        if (isShiftHeld) {
          this.state.toggleUnitSelection(clickedUnit.id)
        } else {
          this.state.selectUnit(clickedUnit.id)
        }
      } else {
        const clickedEnemy = this.state.getEnemyUnitAtPosition(world.x, world.y)
        if (clickedEnemy && !isShiftHeld) {
          this.state.inspectEnemyUnit(clickedEnemy.id)
        } else if (clickedBuilding && !isShiftHeld) {
          this.state.selectBuilding(clickedBuilding.id)
        } else if (!isShiftHeld) {
          this.state.clearSelection()
        }
      }
    }

    this.isLeftMouseDown = false
    this.dragStarted = false
    this.state.endSelectionBox()
  }

  private onRightClick = (e: MouseEvent) => {
    e.preventDefault()
    if (this.state.isAnyTargetingActive()) {
      this.client.cancelTargeting()
      this.updateHoverCursor(this.lastMouseX, this.lastMouseY)
      return
    }
    const screen = this.getScreenPosition(e)
    if (this.isInsideMinimap(screen.x, screen.y)) return

    const world = this.getWorldPosition(e)
    const unitIds = this.state.getOrderedSelectedUnitIds()
    const clickedBuilding = this.state.getBuildingAtPosition(world.x, world.y, 16)

    if (unitIds.length === 0) return

    const clickedEnemy = this.state.getEnemyUnitAtPosition(world.x, world.y)
    if (clickedEnemy && this.state.selectedUnitsCanAttack()) {
      this.network.sendAttackCommand(unitIds, clickedEnemy.id)
      return
    }

    if (
      clickedBuilding &&
      this.isRepairableBuilding(clickedBuilding) &&
      this.state.selectedUnitsCanBuild()
    ) {
      const cellSize = this.state.mapConfig.cellSize
      const buildingCenterX = (clickedBuilding.x + clickedBuilding.width / 2) * cellSize
      const buildingCenterY = (clickedBuilding.y + clickedBuilding.height / 2) * cellSize
      this.state.addMoveMarker(buildingCenterX, buildingCenterY, 700)
      this.network.sendRepairCommand(unitIds, clickedBuilding.id)
      return
    }

    if (
      clickedBuilding &&
      clickedBuilding.capabilities.includes('resource-source') &&
      this.state.selectedUnitsCanGather()
    ) {
      const cellSize = this.state.mapConfig.cellSize
      const buildingCenterX = (clickedBuilding.x + clickedBuilding.width / 2) * cellSize
      const buildingCenterY = (clickedBuilding.y + clickedBuilding.height / 2) * cellSize
      this.state.addMoveMarker(buildingCenterX, buildingCenterY, 700)
      this.network.sendGatherCommand(unitIds, clickedBuilding.id)
      return
    }

    this.state.addFormationMoveMarkers(world.x, world.y)
    this.network.sendMoveCommand(unitIds, world.x, world.y)
  }

  private onMouseLeave = () => {
    this.state.setHoveredInteractableBuilding(null)
    this.state.setHoveredEnemyUnit(null)
    if (!this.isSpaceHeld && !this.isSpacePanning && !this.isMiddleMouseDown) {
      this.canvas.style.cursor = 'default'
    }
  }

  private onWheel = (e: WheelEvent) => {
    e.preventDefault()
    const screen = this.getScreenPosition(e)
    this.camera.adjustZoom(e.deltaY, screen.x, screen.y)
    this.camera.clamp(
      this.canvas.width,
      this.canvas.height,
      this.state.mapWidth,
      this.state.mapHeight,
    )
  }

  refreshCursor() {
    this.updateHoverCursor(this.lastMouseX, this.lastMouseY)
  }

  destroy() {
    this.canvas.removeEventListener('contextmenu', this.onRightClick)
    this.canvas.removeEventListener('mousedown', this.onMouseDown)
    this.canvas.removeEventListener('mousemove', this.onMouseMove)
    this.canvas.removeEventListener('mouseleave', this.onMouseLeave)
    this.canvas.removeEventListener('wheel', this.onWheel)

    window.removeEventListener('mouseup', this.onMouseUp)
    window.removeEventListener('keydown', this.onKeyDown)
    window.removeEventListener('keyup', this.onKeyUp)
  }

  private isRepairableBuilding(
    building: ReturnType<GameState['getBuildingAtPosition']>,
  ): boolean {
    return (
      !!building &&
      building.ownerId === this.state.localPlayerId &&
      typeof building.metadata?.['hp'] === 'number' &&
      typeof building.metadata?.['maxHp'] === 'number' &&
      (building.metadata['hp'] as number) < (building.metadata['maxHp'] as number)
    )
  }

  private hasVisibleAction(actionId: string): boolean {
    return this.state
      .getSelectionSummary()
      .actions.some((action) => action.id === actionId && !action.disabled)
  }

  private getHotkeyAction(event: KeyboardEvent): string | null {
    const selection = this.state.getSelectionSummary()

    if (selection.kind !== 'unit' && selection.kind !== 'group') {
      return null
    }

    const normalizedKey = event.key.toLowerCase()

    const hotkeyActionMap: Record<string, string> = {
      m: 'move',
      r: 'repair',
      g: 'gather',
      a: 'attack',
      b: selection.actions.some((action) => action.id === 'build-barracks')
        ? 'build-barracks'
        : 'build',
    }

    const requestedAction = hotkeyActionMap[normalizedKey]
    if (!requestedAction) {
      return null
    }

    const availableAction = selection.actions.find(
      (action) => action.id === requestedAction && !action.disabled,
    )

    return availableAction?.id ?? null
  }

  private updateHoverCursor(screenX: number, screenY: number) {
    if (this.isSpaceHeld || this.isSpacePanning) {
      this.canvas.style.cursor = this.isSpacePanning ? 'grabbing' : 'grab'
      this.state.setHoveredInteractableBuilding(null)
      return
    }

    if (this.isMiddleMouseDown || this.isMinimapNavigating) {
      this.canvas.style.cursor = 'default'
      this.state.setHoveredInteractableBuilding(null)
      return
    }

    if (this.state.isBuildPlacementActive()) {
      this.state.setHoveredInteractableBuilding(null)
      this.canvas.style.cursor = 'crosshair'
      return
    }

    const world = this.camera.screenToWorld(screenX, screenY)
    const hoveredBuilding = this.state.getBuildingAtPosition(world.x, world.y, 16)
    const isGatherableBuilding =
      !!hoveredBuilding &&
      hoveredBuilding.capabilities.includes('resource-source')
    const isRepairableBuilding = this.isRepairableBuilding(hoveredBuilding)

    if (this.state.isUnitTargetingActive('move')) {
      this.state.setHoveredInteractableBuilding(null)
      this.state.setHoveredEnemyUnit(null)
      this.canvas.style.cursor = this.moveCursor
      return
    }

    if (this.state.isUnitTargetingActive('gather')) {
      this.state.setHoveredInteractableBuilding(isGatherableBuilding ? hoveredBuilding.id : null)
      this.state.setHoveredEnemyUnit(null)
      this.canvas.style.cursor = this.gatherCursor
      return
    }

    if (this.state.isUnitTargetingActive('repair')) {
      this.state.setHoveredInteractableBuilding(isRepairableBuilding ? hoveredBuilding!.id : null)
      this.state.setHoveredEnemyUnit(null)
      this.canvas.style.cursor = this.repairCursor
      return
    }

    // Attack cursor when hovering an enemy with attack-capable units selected
    const hoveredEnemy = this.state.getEnemyUnitAtPosition(world.x, world.y)
    if (hoveredEnemy && this.state.selectedUnitsCanAttack()) {
      this.state.setHoveredEnemyUnit(hoveredEnemy.id)
      this.state.setHoveredInteractableBuilding(null)
      this.canvas.style.cursor = this.attackCursor
      return
    }

    this.state.setHoveredEnemyUnit(hoveredEnemy?.id ?? null)

    const canGather =
      isGatherableBuilding &&
      this.state.selectedUnitsCanGather()
    const canRepair =
      isRepairableBuilding &&
      this.state.selectedUnitsCanBuild()

    this.state.setHoveredInteractableBuilding(
      canRepair || canGather ? hoveredBuilding!.id : null,
    )
    this.canvas.style.cursor = canRepair
      ? this.repairCursor
      : canGather
        ? this.gatherCursor
        : 'default'
  }

  private isInsideMinimap(screenX: number, screenY: number) {
    const bounds = getMinimapBounds(
      this.canvas.width,
      this.canvas.height,
      this.state.mapWidth,
      this.state.mapHeight,
    )

    return (
      screenX >= bounds.x &&
      screenX <= bounds.x + bounds.width &&
      screenY >= bounds.y &&
      screenY <= bounds.y + bounds.height
    )
  }

  private centerCameraFromMinimap(screenX: number, screenY: number) {
    const bounds = getMinimapBounds(
      this.canvas.width,
      this.canvas.height,
      this.state.mapWidth,
      this.state.mapHeight,
    )

    const normalizedX = (screenX - bounds.x) / bounds.width
    const normalizedY = (screenY - bounds.y) / bounds.height
    const worldX = normalizedX * this.state.mapWidth
    const worldY = normalizedY * this.state.mapHeight

    this.camera.centerOn(
      worldX,
      worldY,
      this.canvas.width,
      this.canvas.height,
      this.state.mapWidth,
      this.state.mapHeight,
    )
  }
}
