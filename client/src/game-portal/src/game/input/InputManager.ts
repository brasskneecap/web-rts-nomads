// src/game/input/InputManager.ts
import { GameState } from '../core/GameState'
import type { GameClient } from '../core/GameClient'
import { Camera } from '../rendering/Camera'
import { getMinimapBounds } from '../rendering/CanvasRenderer'
import { NetworkClient } from '../network/NetworkClient'
import { BUILDABLE_BUILDING_DEFS } from '../maps/buildingDefs'
import { resolveCursor } from '../rendering/cursors'

export class InputManager {
  private canvas: HTMLCanvasElement
  private state: GameState
  private client: GameClient
  private camera: Camera
  private network: NetworkClient

  private isLeftMouseDown = false
  private isMiddleMouseDown = false
  private isRightMouseDown = false
  private isSpaceHeld = false

  private dragThreshold = 6
  private dragStarted = false
  private isSpacePanning = false
  private isRightPanning = false
  private rightPanEligible = false
  private rightPanDelayTimer: ReturnType<typeof setTimeout> | null = null
  private rightPanStartX = 0
  private rightPanStartY = 0
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
    canvas.addEventListener('dblclick', this.onDoubleClick)
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
    if (e.code === 'Escape') {
      this.state.clearSelection()
      e.preventDefault()
      return
    }

    if (e.code === 'Space') {
      this.isSpaceHeld = true
      this.canvas.style.cursor = resolveCursor('grab', 'grab')
      e.preventDefault()
      return
    }

    // Control groups: Ctrl+1..9/0 binds the current selection to a slot,
    // 1..9/0 recalls it. Handled before the modifier-gate below so the
    // Ctrl-held assign path actually fires. Skip when typing in an input,
    // or when Alt/Meta are held (those are reserved for other shortcuts).
    if (!e.repeat && !e.altKey && !e.metaKey && !this.isFocusInTextInput()) {
      const groupKey = this.controlGroupKeyFromEvent(e)
      if (groupKey !== null) {
        if (e.ctrlKey) {
          this.client.assignControlGroup(groupKey)
          e.preventDefault()
          return
        }
        if (!e.shiftKey) {
          this.client.selectControlGroup(groupKey)
          e.preventDefault()
          return
        }
      }
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
        this.canvas.style.cursor = resolveCursor('grabbing', 'grabbing')
        return
      }

      this.state.beginSelectionBox(world.x, world.y)
      return
    }

    if (e.button === 1) {
      e.preventDefault()
      this.isMiddleMouseDown = true
      return
    }

    if (e.button === 2) {
      // Right-click panning is intentionally disabled while units are selected.
      // Even with the 100ms hold gate below, a player who holds slightly too
      // long while issuing a command would activate pan and onRightClick would
      // suppress the unit command — a confusing, command-eating UX. With units
      // selected, right-click is reserved for commands; pan remains available
      // via middle mouse or space+left mouse.
      if (this.state.getSelectedUnits().length > 0) {
        return
      }
      // No units selected: keep the existing camera-pan-on-hold behaviour so
      // the player can survey the map. Pan is gated behind a 100ms hold so
      // that quick right-clicks (which still produce a contextmenu event with
      // no command target) don't accidentally pan. Once the delay expires,
      // dragging past the threshold activates pan mode and onRightClick
      // suppresses any command that would otherwise fire.
      this.isRightMouseDown = true
      this.isRightPanning = false
      this.rightPanEligible = false
      this.rightPanStartX = screen.x
      this.rightPanStartY = screen.y
      if (this.rightPanDelayTimer !== null) clearTimeout(this.rightPanDelayTimer)
      this.rightPanDelayTimer = setTimeout(() => {
        this.rightPanDelayTimer = null
        if (this.isRightMouseDown) this.rightPanEligible = true
      }, 100)
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

    if (this.isRightMouseDown) {
      if (!this.isRightPanning && this.rightPanEligible) {
        const totalDx = screen.x - this.rightPanStartX
        const totalDy = screen.y - this.rightPanStartY
        if (Math.abs(totalDx) > this.dragThreshold || Math.abs(totalDy) > this.dragThreshold) {
          this.isRightPanning = true
          this.canvas.style.cursor = resolveCursor('grabbing', 'grabbing')
        }
      }

      if (this.isRightPanning) {
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
      }
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

    if (e.button === 2) {
      // Only meaningful when right-pan was armed in onMouseDown — gated on
      // "no units selected" there. When units are selected the right-button
      // arm step is skipped, so all these fields are already false and the
      // body below is a no-op cleanup.
      this.isRightMouseDown = false
      if (this.rightPanDelayTimer !== null) {
        clearTimeout(this.rightPanDelayTimer)
        this.rightPanDelayTimer = null
      }
      this.rightPanEligible = false
      // Leave isRightPanning set so the contextmenu handler can suppress
      // the right-click action that would otherwise fire after the drag.
      if (this.isRightPanning) {
        this.updateHoverCursor(this.lastMouseX, this.lastMouseY)
      }
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
      this.canvas.style.cursor = this.isSpaceHeld
        ? resolveCursor('grab', 'grab')
        : resolveCursor('default', 'default')
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
      const clickedObstacle = clickedBuilding
        ? undefined
        : this.state.getObstacleAtPosition(world.x, world.y)

      if (clickedUnit) {
        if (isShiftHeld) {
          this.state.toggleUnitSelection(clickedUnit.id)
        } else {
          this.state.selectUnit(clickedUnit.id)
        }
      } else {
        const clickedAlly = this.state.getAllyUnitAtPosition(world.x, world.y)
        const clickedEnemy = clickedAlly
          ? undefined
          : this.state.getEnemyUnitAtPosition(world.x, world.y)
        if (clickedAlly && !isShiftHeld) {
          this.state.inspectAllyUnit(clickedAlly.id)
        } else if (clickedEnemy && !isShiftHeld) {
          this.state.inspectEnemyUnit(clickedEnemy.id)
        } else if (clickedBuilding && !isShiftHeld) {
          this.state.selectBuilding(clickedBuilding.id)
        } else if (clickedObstacle && clickedObstacle.id && !isShiftHeld) {
          this.state.selectObstacle(clickedObstacle.id)
        } else if (!isShiftHeld) {
          // Traps have lowest selection priority — a unit or building standing
          // on a trap zone is always selected first.
          const clickedTrap = this.state.getTrapAtPosition(world.x, world.y)
          if (clickedTrap) {
            this.state.selectTrap(clickedTrap.id)
          } else {
            this.state.clearSelection()
          }
        }
      }
    }

    this.isLeftMouseDown = false
    this.dragStarted = false
    this.state.endSelectionBox()
  }

  // Double-clicking an owned unit selects every visible same-type unit
  // currently inside the on-screen viewport. Standard RTS gesture for
  // "select all of these on screen".
  private onDoubleClick = (e: MouseEvent) => {
    if (e.button !== 0) return
    const screen = this.getScreenPosition(e)
    if (this.isInsideMinimap(screen.x, screen.y)) return

    const world = this.getWorldPosition(e)
    const clickedUnit = this.state.getUnitAtPosition(world.x, world.y)
    if (!clickedUnit) return

    const viewBounds = {
      left: this.camera.x,
      top: this.camera.y,
      right: this.camera.x + this.canvas.width / this.camera.zoom,
      bottom: this.camera.y + this.canvas.height / this.camera.zoom,
    }
    this.state.selectVisibleSameTypeUnits(clickedUnit.unitType, viewBounds)
  }

  private onRightClick = (e: MouseEvent) => {
    e.preventDefault()

    if (this.isRightPanning) {
      this.isRightPanning = false
      return
    }

    const screen = this.getScreenPosition(e)
    if (this.isInsideMinimap(screen.x, screen.y)) return

    if (this.state.isAnyTargetingActive()) {
      this.client.cancelTargeting()
      this.updateHoverCursor(this.lastMouseX, this.lastMouseY)
      return
    }

    const world = this.getWorldPosition(e)
    const selectedBuilding = this.state.getSelectedBuilding()
    if (selectedBuilding && this.hasVisibleAction('set-spawn-point')) {
      const spawnPoint = this.state.getSelectedBuildingSpawnPointTarget(world.x, world.y)
      if (spawnPoint) {
        this.network.sendSetBuildingSpawnPointCommand(selectedBuilding.id, spawnPoint.x, spawnPoint.y)
        this.state.addMoveMarker(spawnPoint.x, spawnPoint.y, 800)
      }
      return
    }

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

    const clickedObstacle = this.state.getGatherableObstacleAtPosition(world.x, world.y, 16)
    if (clickedObstacle && clickedObstacle.id && this.state.selectedUnitsCanGather()) {
      const cellSize = this.state.mapConfig.cellSize
      const obstacleCenterX = (clickedObstacle.x + (clickedObstacle.width ?? 1) / 2) * cellSize
      const obstacleCenterY = (clickedObstacle.y + (clickedObstacle.height ?? 1) / 2) * cellSize
      this.state.addMoveMarker(obstacleCenterX, obstacleCenterY, 700)
      this.network.sendGatherCommand(unitIds, clickedObstacle.id)
      return
    }

    this.state.addFormationMoveMarkers(world.x, world.y)
    this.network.sendMoveCommand(unitIds, world.x, world.y)
  }

  private onMouseLeave = () => {
    this.state.setHoveredInteractableBuilding(null)
    this.state.setHoveredInteractableObstacle(null)
    this.state.setHoveredEnemyUnit(null)
    if (!this.isSpaceHeld && !this.isSpacePanning && !this.isMiddleMouseDown) {
      this.canvas.style.cursor = resolveCursor('default', 'default')
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
    if (this.rightPanDelayTimer !== null) {
      clearTimeout(this.rightPanDelayTimer)
      this.rightPanDelayTimer = null
    }
    this.canvas.removeEventListener('contextmenu', this.onRightClick)
    this.canvas.removeEventListener('mousedown', this.onMouseDown)
    this.canvas.removeEventListener('mousemove', this.onMouseMove)
    this.canvas.removeEventListener('mouseleave', this.onMouseLeave)
    this.canvas.removeEventListener('dblclick', this.onDoubleClick)
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
      h: 'hold',
      p: 'patrol',
    }

    for (const def of BUILDABLE_BUILDING_DEFS) {
      const buildActionId = `build-${def.type}`
      const menuIsOpen = selection.actions.some((action) => action.id === buildActionId)
      hotkeyActionMap[def.hotkey.toLowerCase()] = menuIsOpen ? buildActionId : 'build'
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
    // Reset the friendly-hover indicator every frame. It only matters inside
    // the cast-ability / focus-target branches below, which set it back to a
    // non-null value when the cursor is over a valid same-team unit. Doing
    // the reset once up here means every other branch can ignore the field
    // entirely (instead of having to remember to null it).
    this.state.setHoveredFriendlyUnit(null)

    if (this.isSpaceHeld || this.isSpacePanning) {
      this.canvas.style.cursor = this.isSpacePanning
        ? resolveCursor('grabbing', 'grabbing')
        : resolveCursor('grab', 'grab')
      this.state.setHoveredInteractableBuilding(null)
      this.state.setHoveredInteractableObstacle(null)
      return
    }

    if (this.isMiddleMouseDown || this.isMinimapNavigating) {
      this.canvas.style.cursor = resolveCursor('default', 'default')
      this.state.setHoveredInteractableBuilding(null)
      this.state.setHoveredInteractableObstacle(null)
      return
    }

    if (this.isRightPanning) {
      this.canvas.style.cursor = resolveCursor('grabbing', 'grabbing')
      this.state.setHoveredInteractableBuilding(null)
      this.state.setHoveredInteractableObstacle(null)
      this.state.setHoveredEnemyUnit(null)
      return
    }

    if (this.state.isBuildPlacementActive()) {
      this.state.setHoveredInteractableBuilding(null)
      this.state.setHoveredInteractableObstacle(null)
      this.canvas.style.cursor = resolveCursor('crosshair', 'crosshair')
      return
    }

    const world = this.camera.screenToWorld(screenX, screenY)
    const hoveredBuilding = this.state.getBuildingAtPosition(world.x, world.y, 16)
    const hoveredObstacle = hoveredBuilding
      ? undefined
      : this.state.getGatherableObstacleAtPosition(world.x, world.y, 16)
    const isGatherableBuilding =
      !!hoveredBuilding &&
      hoveredBuilding.capabilities.includes('resource-source')
    const isGatherableObstacle = !!hoveredObstacle
    const isRepairableBuilding = this.isRepairableBuilding(hoveredBuilding)

    // Resource kind decides chopwood vs minegold. Buildings and obstacles
    // both carry an optional `resourceType`; fall back to the generic gather
    // SVG when the source doesn't declare one.
    const gatherResource = isGatherableBuilding
      ? hoveredBuilding!.resourceType
      : isGatherableObstacle
        ? hoveredObstacle!.resourceType
        : undefined
    const gatherCursorKey =
      gatherResource === 'wood' ? 'chopwood'
      : gatherResource === 'gold' ? 'minegold'
      : 'gather'

    // Target-selection modes: show the reticle cursor while waiting for the
    // player to click a target, regardless of what's under the pointer. This
    // covers an armed ability (heal, etc.), the attack command (clicking
    // Attack or pressing A → attack-move targeting), and the Focus Target
    // command (player picking an ally for a Cleric to follow / prioritise).
    //
    // Per-mode hover preview: stamp the hover state for whichever class of
    // target the active mode is asking for, so the renderer can draw the
    // corresponding ring under the cursor.
    //   - cast-ability / focus-target: same-team unit  → blue dashed ring
    //   - attack:                      hostile unit     → orange dashed ring
    // Both rings preview "this is the unit your next click will commit to."
    if (
      this.state.isUnitTargetingActive('cast-ability') ||
      this.state.isUnitTargetingActive('attack') ||
      this.state.isUnitTargetingActive('focus-target')
    ) {
      this.state.setHoveredInteractableBuilding(null)
      this.state.setHoveredInteractableObstacle(null)
      if (this.state.isUnitTargetingActive('attack')) {
        const enemy = this.state.getEnemyUnitAtPosition(world.x, world.y)
        this.state.setHoveredEnemyUnit(enemy ? enemy.id : null)
      } else {
        this.state.setHoveredEnemyUnit(null)
        const ally = this.state.getUnitAtPosition(world.x, world.y)
        this.state.setHoveredFriendlyUnit(ally ? ally.id : null)
      }
      this.canvas.style.cursor = resolveCursor('target', 'crosshair')
      return
    }

    if (this.state.isUnitTargetingActive('move') || this.state.isUnitTargetingActive('patrol')) {
      this.state.setHoveredInteractableBuilding(null)
      this.state.setHoveredInteractableObstacle(null)
      this.state.setHoveredEnemyUnit(null)
      this.canvas.style.cursor = resolveCursor('move', this.moveCursor)
      return
    }

    if (this.state.isUnitTargetingActive('gather')) {
      this.state.setHoveredInteractableBuilding(isGatherableBuilding ? hoveredBuilding.id : null)
      this.state.setHoveredInteractableObstacle(
        isGatherableObstacle && hoveredObstacle?.id ? hoveredObstacle.id : null,
      )
      this.state.setHoveredEnemyUnit(null)
      this.canvas.style.cursor = resolveCursor(gatherCursorKey, this.gatherCursor)
      return
    }

    if (this.state.isUnitTargetingActive('repair')) {
      this.state.setHoveredInteractableBuilding(isRepairableBuilding ? hoveredBuilding!.id : null)
      this.state.setHoveredInteractableObstacle(null)
      this.state.setHoveredEnemyUnit(null)
      this.canvas.style.cursor = resolveCursor('repair', this.repairCursor)
      return
    }

    // Attack cursor when hovering an enemy with attack-capable units selected
    const hoveredEnemy = this.state.getEnemyUnitAtPosition(world.x, world.y)
    if (hoveredEnemy && this.state.selectedUnitsCanAttack()) {
      this.state.setHoveredEnemyUnit(hoveredEnemy.id)
      this.state.setHoveredInteractableBuilding(null)
      this.state.setHoveredInteractableObstacle(null)
      this.canvas.style.cursor = resolveCursor('attack', this.attackCursor)
      return
    }

    this.state.setHoveredEnemyUnit(hoveredEnemy?.id ?? null)

    const canGatherBuilding =
      isGatherableBuilding &&
      this.state.selectedUnitsCanGather()
    const canGatherObstacle =
      isGatherableObstacle &&
      this.state.selectedUnitsCanGather()
    const canRepair =
      isRepairableBuilding &&
      this.state.selectedUnitsCanBuild()

    this.state.setHoveredInteractableBuilding(
      canRepair || canGatherBuilding ? hoveredBuilding!.id : null,
    )
    this.state.setHoveredInteractableObstacle(
      canGatherObstacle && hoveredObstacle?.id ? hoveredObstacle.id : null,
    )
    this.canvas.style.cursor = canRepair
      ? resolveCursor('repair', this.repairCursor)
      : canGatherBuilding || canGatherObstacle
        ? resolveCursor(gatherCursorKey, this.gatherCursor)
        : resolveCursor('default', 'default')
  }

  private isInsideMinimap(screenX: number, screenY: number) {
    const bounds = getMinimapBounds(
      this.canvas.width,
      this.canvas.height,
      this.state.mapWidth,
      this.state.mapHeight,
      this.state.minimapPanelRect,
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
      this.state.minimapPanelRect,
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

  // Maps Digit1..Digit9 to control group keys 1..9 and Digit0 to 10
  // (matching the standard RTS convention). Returns null for any other key.
  // Uses event.code so the mapping survives non-US keyboard layouts.
  private controlGroupKeyFromEvent(e: KeyboardEvent): number | null {
    if (e.code === 'Digit0' || e.code === 'Numpad0') return 10
    const m = /^(?:Digit|Numpad)([1-9])$/.exec(e.code)
    if (m) return Number(m[1])
    return null
  }

  // Skip game keybinds when the user is typing into an input. Without this,
  // pressing a digit in a chat box or text field would also recall a group.
  private isFocusInTextInput(): boolean {
    const el = document.activeElement as HTMLElement | null
    if (!el) return false
    if (el.isContentEditable) return true
    const tag = el.tagName
    return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT'
  }
}
