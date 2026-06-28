// src/game/input/InputManager.ts
import { GameState } from '../core/GameState'
import type { GameClient } from '../core/GameClient'
import type { LootDropSnapshot } from '../network/protocol'
import { Camera } from '../rendering/Camera'
import { getMinimapBounds } from '../rendering/CanvasRenderer'
import { NetworkClient } from '../network/NetworkClient'
import { BUILDABLE_BUILDING_DEFS } from '../maps/buildingDefs'
import { resolveCursor } from '../rendering/cursors'
import { playBuildingSelectSound, playSelectSound } from '../../composables/useSfx'

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

  // Tracks the most recent successful control-group recall so a second tap
  // of the same key inside the double-tap window centers the camera on the
  // group's centroid. Reset to null after centering, after a different group
  // key is pressed, or after the window elapses on the next tap.
  private lastGroupRecallKey: number | null = null
  private lastGroupRecallTimeMs = 0
  private readonly groupRecallDoubleTapMs = 400

  private lastMouseX = 0
  private lastMouseY = 0
  // Edge-pan: when the mouse hovers within `edgePanThresholdPx` of the canvas
  // edge, a rAF loop pans the camera at a speed proportional to how deep
  // into the edge zone the cursor sits. Stops itself when the cursor leaves
  // the zone, leaves the canvas, or another pan gesture takes over.
  private edgePanRafId = 0
  private lastEdgePanTime = 0
  // Latest cursor position relative to the canvas; updated on every
  // mousemove. Tracked separately from lastMouseX/Y because the existing
  // drag-pan code computes a delta against lastMouseX/Y and depends on it
  // not being overwritten mid-frame.
  private edgePanMouseX = 0
  private edgePanMouseY = 0
  // Distance from the window top to the canvas top — i.e., the height of
  // the MatchHud sitting above the canvas. Sampled each mousemove so layout
  // changes (resize, fullscreen toggle) are picked up automatically. Used
  // by the tick to compute the "upper half of the top bar" pan zone.
  private canvasTopOffset = 0
  private readonly edgePanThresholdPx = 24
  // Screen-space pixels per second at the very edge. Divided by zoom so the
  // perceived screen-pan speed feels constant across zoom levels.
  private readonly edgePanMaxSpeed = 2200
  // 8 chevron cursors, indexed by direction: 0=E, 1=SE, 2=S, 3=SW, 4=W,
  // 5=NW, 6=N, 7=NE. Built lazily in the constructor so the data URIs are
  // computed once. -1 means "not currently showing an edge-pan cursor."
  private edgePanCursors: string[] = []
  private currentEdgePanDir = -1
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

    // Pre-build the 8 directional cursors. Each is a black-outlined white
    // chevron pointing in the indicated direction, with the hotspot at the
    // SVG center (16,16) so the cursor anchors to the actual mouse point.
    const angles = [0, 45, 90, 135, 180, 225, 270, 315]
    this.edgePanCursors = angles.map((a) => this.buildEdgePanCursor(a))

    canvas.addEventListener('contextmenu', this.onRightClick)
    canvas.addEventListener('mousedown', this.onMouseDown)
    canvas.addEventListener('mousemove', this.onMouseMove)
    canvas.addEventListener('mouseleave', this.onMouseLeave)
    canvas.addEventListener('dblclick', this.onDoubleClick)
    canvas.addEventListener('wheel', this.onWheel, { passive: false })

    window.addEventListener('mouseup', this.onMouseUp)
    window.addEventListener('keydown', this.onKeyDown)
    window.addEventListener('keyup', this.onKeyUp)
    // Window-level mousemove drives edge-pan. We need cursor position even
    // when the pointer sits over the footer HUD or other overlays — those
    // elements consume the canvas-level mousemove, but the window listener
    // still fires. The handler converts to canvas-relative coords itself.
    window.addEventListener('mousemove', this.onWindowMouseMove)
    // Stop edge-pan when the cursor leaves the browser viewport entirely
    // (e.g., into the OS taskbar) or the tab loses focus.
    document.documentElement.addEventListener('mouseleave', this.onDocumentLeave)
    window.addEventListener('blur', this.onWindowBlur)
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
          const recalled = this.client.selectControlGroup(groupKey)
          if (recalled) {
            const nowMs = performance.now()
            const isDoubleTap =
              this.lastGroupRecallKey === groupKey &&
              nowMs - this.lastGroupRecallTimeMs <= this.groupRecallDoubleTapMs
            if (isDoubleTap) {
              this.centerCameraOnSelection()
              this.lastGroupRecallKey = null
              this.lastGroupRecallTimeMs = 0
            } else {
              this.lastGroupRecallKey = groupKey
              this.lastGroupRecallTimeMs = nowMs
            }
          } else {
            this.lastGroupRecallKey = null
            this.lastGroupRecallTimeMs = 0
          }
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
      // While ANY targeting mode is active (commander ability, build
      // placement, etc.) right-click must reliably cancel — so suppress
      // the pan-on-hold path that would otherwise eat the cancel.
      if (this.state.isAnyTargetingActive()) {
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
    // Push raw viewport coords so DOM-overlay tooltips (LootDropTooltip)
    // can render at the cursor without depending on a cached canvas rect.
    this.state.setCursorClient(e.clientX, e.clientY)
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
          // A building still under construction plays the shared construction
          // sound instead of its normal select sound. Both share the select
          // channel, so either one stops on deselect.
          if (clickedBuilding.metadata?.['underConstruction'] === true) {
            playSelectSound('building_construction.mp3')
          } else {
            playBuildingSelectSound(clickedBuilding.buildingType)
          }
        } else if (clickedObstacle && clickedObstacle.id && !isShiftHeld) {
          this.state.selectObstacle(clickedObstacle.id)
        } else if (!isShiftHeld) {
          // Traps have second-lowest selection priority — a unit or building
          // standing on a trap zone is always selected first.
          const clickedTrap = this.state.getTrapAtPosition(world.x, world.y)
          if (clickedTrap) {
            this.state.selectTrap(clickedTrap.id)
          } else {
            // Zones have the absolute lowest priority: only selected when
            // nothing else was hit (no unit/building/obstacle/trap).
            const clickedZoneId = this.state.getZoneIdAtWorld(world.x, world.y)
            if (clickedZoneId) {
              this.state.selectZone(clickedZoneId)
            } else {
              this.state.clearSelection()
            }
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
    this.state.selectVisibleSameTypeUnits(clickedUnit.unitType, clickedUnit.path, viewBounds)
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

    // Player-directed deposit: a right-click on an owned deposit-point building
    // (e.g. a townhall) with at least one carrying worker selected routes the
    // carriers there to drop off their resource. Non-carrying selection members
    // fall through to a normal move so they don't ignore the order. Checked
    // before repair so a carrier+damaged-townhall combo deposits first rather
    // than queuing up a repair task while still holding gold.
    if (
      clickedBuilding &&
      clickedBuilding.capabilities.includes('deposit-point') &&
      clickedBuilding.ownerId === this.state.localPlayerId &&
      this.state.selectedUnitsHaveCarriedResource()
    ) {
      const carrierIds = this.state.getSelectedCarrierUnitIds()
      const otherIds = unitIds.filter((id) => !carrierIds.includes(id))
      const cellSize = this.state.mapConfig.cellSize
      const buildingCenterX = (clickedBuilding.x + clickedBuilding.width / 2) * cellSize
      const buildingCenterY = (clickedBuilding.y + clickedBuilding.height / 2) * cellSize
      this.state.addMoveMarker(buildingCenterX, buildingCenterY, 700)
      if (carrierIds.length > 0) {
        this.network.sendDepositCommand(carrierIds, clickedBuilding.id)
      }
      if (otherIds.length > 0) {
        this.network.sendMoveCommand(otherIds, buildingCenterX, buildingCenterY)
      }
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

    // Loot drop chest — any selected unit can pick up a chest.
    const clickedLootDrop = findLootDropAtWorldPos(this.state, world.x, world.y)
    if (clickedLootDrop) {
      this.state.addMoveMarker(clickedLootDrop.x, clickedLootDrop.y, 700)
      this.network.sendPickupLootCommand(unitIds, clickedLootDrop.id)
      return
    }

    this.state.addFormationMoveMarkers(world.x, world.y)
    this.network.sendMoveCommand(unitIds, world.x, world.y)
  }

  private onMouseLeave = () => {
    this.state.setHoveredInteractableBuilding(null)
    this.state.setHoveredInteractableObstacle(null)
    this.state.setHoveredEnemyUnit(null)
    this.state.setHoveredLootDrop(null)
    if (!this.isSpaceHeld && !this.isSpacePanning && !this.isMiddleMouseDown) {
      this.canvas.style.cursor = resolveCursor('default', 'default')
    }
    // Don't stop edge-pan here — the cursor leaving the canvas (e.g., onto
    // the footer HUD) is exactly when we want to keep panning. The window-
    // level handlers take responsibility for stopping the loop.
  }

  // Window-level mousemove: converts the global cursor position into
  // canvas-relative coords and arms the edge-pan loop. Fires whether the
  // cursor is over the canvas or over a HUD overlay sitting on top of it.
  private onWindowMouseMove = (e: MouseEvent) => {
    const rect = this.canvas.getBoundingClientRect()
    const canvasX = e.clientX - rect.left
    const canvasY = e.clientY - rect.top
    this.edgePanMouseX = canvasX
    this.edgePanMouseY = canvasY
    this.canvasTopOffset = rect.top
    this.startEdgePanLoop()

    // Drag-select continuation while the cursor is over the MatchHud header
    // or any pointer-events: auto region of the SelectionHud footer (which
    // overlays the canvas). The canvas mousemove listener doesn't fire when
    // a DOM overlay swallows the event, so without this the selection box
    // freezes mid-drag. Skip when the canvas itself was the hit target
    // (onMouseMove already handled it) or when a non-select gesture owns
    // the left button. The cursor's world position is computed from the
    // raw screen coords — when the footer covers part of the canvas, this
    // projects "beneath" the footer in world space, which is what we want.
    if (!this.isLeftMouseDown || this.isMinimapNavigating || this.isSpacePanning) return
    if (e.target === this.canvas) return

    const world = this.camera.screenToWorld(canvasX, canvasY)
    this.state.updateSelectionBox(world.x, world.y)
    const bounds = this.state.getSelectionBounds()
    const width = bounds.right - bounds.left
    const height = bounds.bottom - bounds.top
    if (width > this.dragThreshold || height > this.dragThreshold) {
      this.dragStarted = true
    }
  }

  private onDocumentLeave = () => {
    this.stopEdgePanLoop()
  }

  private onWindowBlur = () => {
    this.stopEdgePanLoop()
  }

  // Edge-pan rAF loop. Idempotent — calling startEdgePanLoop while it's
  // already running is a no-op. The tick stops itself when the cursor is
  // no longer in an edge zone or another pan gesture is active.
  private startEdgePanLoop() {
    if (this.edgePanRafId !== 0) return
    this.lastEdgePanTime = performance.now()
    this.edgePanRafId = requestAnimationFrame(this.edgePanTick)
  }

  private stopEdgePanLoop() {
    if (this.edgePanRafId !== 0) {
      cancelAnimationFrame(this.edgePanRafId)
      this.edgePanRafId = 0
    }
    this.clearEdgePanCursor()
  }

  private edgePanTick = (now: number) => {
    this.edgePanRafId = 0

    // Bail out while another pan gesture owns the camera so we don't fight
    // it. The next mousemove will re-arm the loop if the cursor is still in
    // an edge zone.
    if (
      this.isMiddleMouseDown ||
      this.isSpacePanning ||
      this.isRightPanning ||
      this.isMinimapNavigating
    ) {
      this.clearEdgePanCursor()
      return
    }

    const t = this.edgePanThresholdPx
    const w = this.canvas.width
    const h = this.canvas.height
    const x = this.edgePanMouseX
    const y = this.edgePanMouseY

    // Pan velocity ramps linearly from 0 at the threshold boundary up to
    // edgePanMaxSpeed at the very edge. Negative = pan left/up, positive =
    // pan right/down. Outside the canvas, the depth ratio is clamped to 1.
    // Top-edge pan only triggers in the UPPER HALF of the MatchHud — that
    // is, when the cursor is closer to the window top than to the canvas
    // top. The lower half of the top bar is a dead zone so the cursor can
    // drift near the HUD without launching the camera upward
    // unintentionally; that dead zone also naturally protects the menu
    // (crest) button, which sits in the lower half of the bar — no extra
    // x-gate needed. y < 0 means "above the canvas"; topBarHalfBoundary
    // is the y-coord (negative) where the dead zone ends and the pan zone
    // begins. Falls back to the canvas top edge if no top bar is detected.
    const topBar = this.canvasTopOffset
    const topBarHalfBoundary = topBar > 0 ? -topBar / 2 : 0

    let vx = 0
    let vy = 0
    if (x < t) vx = -Math.min(1, (t - x) / t) * this.edgePanMaxSpeed
    else if (x > w - t) vx = Math.min(1, (x - (w - t)) / t) * this.edgePanMaxSpeed

    if (y < topBarHalfBoundary) {
      // Ramp from 0 at the boundary up to max speed at the window top.
      const zoneDepth = Math.max(1, topBar / 2)
      const ratio = Math.min(1, (topBarHalfBoundary - y) / zoneDepth)
      vy = -ratio * this.edgePanMaxSpeed
    } else if (y > h - t) {
      vy = Math.min(1, (y - (h - t)) / t) * this.edgePanMaxSpeed
    }

    if (vx === 0 && vy === 0) {
      // Cursor moved out of the edge zone; let the loop sleep until the
      // next mousemove brings it back.
      this.clearEdgePanCursor()
      return
    }

    // dt clamped to 100ms to keep the camera from teleporting after a long
    // tab-out / hitch. Screen velocity → world delta via the zoom factor,
    // matching the convention used by the drag-pan path above.
    const dt = Math.min(0.1, (now - this.lastEdgePanTime) / 1000)
    this.lastEdgePanTime = now
    this.camera.pan((vx * dt) / this.camera.zoom, (vy * dt) / this.camera.zoom)
    this.camera.clamp(w, h, this.state.mapWidth, this.state.mapHeight)

    this.applyEdgePanCursor(vx, vy)

    this.edgePanRafId = requestAnimationFrame(this.edgePanTick)
  }

  // Direction index from a pan velocity, mapped onto the 8-way cursor
  // array. Returns -1 when there is no pan (defensive — caller should
  // already have early-exited).
  private edgePanDirection(vx: number, vy: number): number {
    if (vx === 0 && vy === 0) return -1
    if (vx > 0 && vy === 0) return 0 // E
    if (vx > 0 && vy > 0) return 1 // SE
    if (vx === 0 && vy > 0) return 2 // S
    if (vx < 0 && vy > 0) return 3 // SW
    if (vx < 0 && vy === 0) return 4 // W
    if (vx < 0 && vy < 0) return 5 // NW
    if (vx === 0 && vy < 0) return 6 // N
    return 7 // NE: vx > 0 && vy < 0
  }

  // Apply the directional cursor to both the canvas and document.body so
  // the indicator is visible whether the actual pointer sits over the
  // canvas (top/left/right edges) or over the SelectionHud footer at the
  // bottom. Only re-applies when the direction has actually changed to
  // avoid thrashing style on every tick.
  private applyEdgePanCursor(vx: number, vy: number) {
    const dir = this.edgePanDirection(vx, vy)
    if (dir === -1 || dir === this.currentEdgePanDir) return
    this.currentEdgePanDir = dir
    const cursor = this.edgePanCursors[dir]
    this.canvas.style.cursor = cursor
    document.body.style.cursor = cursor
  }

  private clearEdgePanCursor() {
    if (this.currentEdgePanDir === -1) return
    this.currentEdgePanDir = -1
    document.body.style.cursor = ''
    // The canvas cursor is re-derived from hover state on the next
    // mousemove; clear here so the panning chevron doesn't linger if the
    // cursor parks outside the canvas after edge-pan stops.
    this.updateHoverCursor(this.lastMouseX, this.lastMouseY)
  }

  // Build a chevron-arrow cursor data URI rotated to point in the given
  // direction (degrees, 0 = right/east). Black outline with a white inner
  // stroke for legibility on light and dark backgrounds.
  private buildEdgePanCursor(angle: number): string {
    const svg =
      `<svg xmlns='http://www.w3.org/2000/svg' width='32' height='32' viewBox='0 0 32 32'>` +
      `<g transform='rotate(${angle} 16 16)'>` +
      `<path d='M11 7 L23 16 L11 25' stroke='black' stroke-width='6' fill='none' ` +
      `stroke-linejoin='round' stroke-linecap='round'/>` +
      `<path d='M11 7 L23 16 L11 25' stroke='white' stroke-width='3' fill='none' ` +
      `stroke-linejoin='round' stroke-linecap='round'/>` +
      `</g></svg>`
    const encoded = encodeURIComponent(svg)
    return `url("data:image/svg+xml,${encoded}") 16 16, default`
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
    this.stopEdgePanLoop()
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
    window.removeEventListener('mousemove', this.onWindowMouseMove)
    document.documentElement.removeEventListener('mouseleave', this.onDocumentLeave)
    window.removeEventListener('blur', this.onWindowBlur)
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

    // Each key maps to an ordered list of candidate action ids; the first one
    // present and enabled on the current selection wins. `g` serves Gather and
    // Guard: the two never coexist on one unit (Guard is offered only to
    // non-gathering combat units), so for a worker `g` resolves to Gather and
    // for a soldier it resolves to Guard.
    const hotkeyActionMap: Record<string, string[]> = {
      m: ['move'],
      r: ['repair'],
      g: ['gather', 'guard'],
      a: ['attack'],
      h: ['hold'],
      p: ['patrol'],
    }

    // Building hotkeys are only live while the build menu is open — i.e.,
    // when the per-building `build-<type>` actions are present on the
    // selection. When the menu is closed only B opens it; pressing W/C/F/K
    // etc. on a worker is intentionally inert so it doesn't dump the player
    // into the build menu through a key they meant for another purpose
    // (e.g. S → Shop). Once the menu opens, each building's hotkey routes
    // to its specific placement action as before.
    for (const def of BUILDABLE_BUILDING_DEFS) {
      const buildActionId = `build-${def.type}`
      if (selection.actions.some((action) => action.id === buildActionId)) {
        hotkeyActionMap[def.hotkey.toLowerCase()] = [buildActionId]
      }
    }
    // B opens the build menu when the menu-open action is available. Kept
    // outside the loop so it doesn't collide with barracks (also `b`) when
    // the menu is already open — the loop above wins in that case and
    // routes B straight to build-barracks.
    if (selection.actions.some((action) => action.id === 'build' && !action.disabled)) {
      hotkeyActionMap['b'] = hotkeyActionMap['b'] ?? ['build']
    }

    const candidates = hotkeyActionMap[normalizedKey]
    if (!candidates) {
      return null
    }

    for (const candidate of candidates) {
      const availableAction = selection.actions.find(
        (action) => action.id === candidate && !action.disabled,
      )
      if (availableAction) {
        return availableAction.id
      }
    }

    return null
  }

  private updateHoverCursor(screenX: number, screenY: number) {
    // Reset the friendly-hover indicator every frame. It only matters inside
    // the cast-ability / focus-target branches below, which set it back to a
    // non-null value when the cursor is over a valid same-team unit. Doing
    // the reset once up here means every other branch can ignore the field
    // entirely (instead of having to remember to null it).
    this.state.setHoveredFriendlyUnit(null)

    // Push the current cursor position into state every frame — world space
    // for renderer previews, screen space for DOM-overlay tooltips.
    {
      const w = this.camera.screenToWorld(screenX, screenY)
      this.state.setCursorWorld(w.x, w.y)
      this.state.setCursorScreen(screenX, screenY)
    }

    // Edge-pan owns the cursor while active. Skipping the rest of this
    // function prevents the canvas mousemove handler from flickering the
    // chevron back to the hover cursor between rAF ticks.
    if (this.currentEdgePanDir !== -1) {
      return
    }

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

    if (this.state.isCommanderTargetingActive()) {
      this.state.setHoveredInteractableBuilding(null)
      this.state.setHoveredInteractableObstacle(null)
      this.state.setHoveredEnemyUnit(null)
      this.state.setHoveredFriendlyUnit(null)
      this.canvas.style.cursor = resolveCursor('target', 'crosshair')
      return
    }

    const world = this.camera.screenToWorld(screenX, screenY)

    // Always clear and re-evaluate the hovered loot drop so it resets when
    // the cursor moves away from a chest.
    const hoveredLootDrop = findLootDropAtWorldPos(this.state, world.x, world.y)
    this.state.setHoveredLootDrop(hoveredLootDrop ? hoveredLootDrop.id : null)

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

    // Deposit hover: owned deposit-point building + at least one carrying worker
    // in the selection. Matches the right-click branch in onRightClick and takes
    // precedence over repair so the cursor advertises the dominant action.
    const canDepositHere =
      !!hoveredBuilding &&
      hoveredBuilding.capabilities.includes('deposit-point') &&
      hoveredBuilding.ownerId === this.state.localPlayerId &&
      this.state.selectedUnitsHaveCarriedResource()
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
      canDepositHere || canRepair || canGatherBuilding ? hoveredBuilding!.id : null,
    )
    this.state.setHoveredInteractableObstacle(
      canGatherObstacle && hoveredObstacle?.id ? hoveredObstacle.id : null,
    )
    this.canvas.style.cursor = canDepositHere
      ? resolveCursor('minegold', this.gatherCursor)
      : canRepair
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

  // Snaps the camera to the centroid of the currently-selected units. Used
  // by the double-tap control-group-key gesture (e.g. 2-then-2) to jump to
  // a group after recalling it. Falls through silently when nothing is
  // selected so a double-tap on an emptied slot doesn't move the camera.
  private centerCameraOnSelection() {
    const units = this.state.getSelectedUnits()
    if (units.length === 0) return
    let sumX = 0
    let sumY = 0
    for (const u of units) {
      sumX += u.x
      sumY += u.y
    }
    this.camera.centerOn(
      sumX / units.length,
      sumY / units.length,
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

// ─── Module-level helpers ─────────────────────────────────────────────────────

// Hit-test `state.lootDropsById` against a world-space position. The pick
// radius (16 px) is generous enough for a comfortable click target.
// Returns the first matching drop, or null if no chest is within range.
function findLootDropAtWorldPos(
  state: GameState,
  worldX: number,
  worldY: number,
): LootDropSnapshot | null {
  const pickRadius = 16
  const pickRadiusSq = pickRadius * pickRadius
  for (const drop of state.lootDropsById.values()) {
    const dx = drop.x - worldX
    const dy = drop.y - worldY
    if (dx * dx + dy * dy <= pickRadiusSq) {
      return drop
    }
  }
  return null
}
