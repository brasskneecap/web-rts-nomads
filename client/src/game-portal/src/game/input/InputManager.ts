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
  private readonly gatherCursor = `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='28' height='28' viewBox='0 0 28 28'%3E%3Cg fill='none' fill-rule='evenodd'%3E%3Ccircle cx='14' cy='14' r='11' fill='rgba(202,138,4,0.95)' stroke='rgba(15,23,42,0.9)' stroke-width='2'/%3E%3Cpath d='M9 16l4.2-4.4 2.6 2.6L12 18z' fill='%23fff7d6'/%3E%3Cpath d='M15.6 9.4l3 3' stroke='%230f172a' stroke-width='2.2' stroke-linecap='round'/%3E%3C/g%3E%3C/svg%3E") 14 14, pointer`

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
    }
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
      if (this.client.tryHandleWorldClick(world.x, world.y)) {
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
      } else if (clickedBuilding && !isShiftHeld) {
        this.state.selectBuilding(clickedBuilding.id)
      } else if (!isShiftHeld) {
        this.state.clearSelection()
      }
    }

    this.isLeftMouseDown = false
    this.dragStarted = false
    this.state.endSelectionBox()
  }

  private onRightClick = (e: MouseEvent) => {
    e.preventDefault()
    if (this.state.isBuildingTargetingActive()) {
      this.client.cancelTargeting()
      return
    }
    const screen = this.getScreenPosition(e)
    if (this.isInsideMinimap(screen.x, screen.y)) return

    const world = this.getWorldPosition(e)
    const unitIds = this.state.getOrderedSelectedUnitIds()
    const clickedBuilding = this.state.getBuildingAtPosition(world.x, world.y, 16)

    if (unitIds.length === 0) return

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

    const world = this.camera.screenToWorld(screenX, screenY)
    const hoveredBuilding = this.state.getBuildingAtPosition(world.x, world.y, 16)
    const canGather =
      !!hoveredBuilding &&
      hoveredBuilding.capabilities.includes('resource-source') &&
      this.state.selectedUnitsCanGather()

    this.state.setHoveredInteractableBuilding(canGather ? hoveredBuilding.id : null)
    this.canvas.style.cursor = canGather ? this.gatherCursor : 'default'
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
