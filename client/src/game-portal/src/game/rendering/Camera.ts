// src/game/rendering/Camera.ts

// Per-edge pan overscan in world units. Lets the camera move past the map
// boundary so HUD chrome (in-game) or floating UI like the editor minimap
// doesn't permanently cover map content. Defaults match the original
// hard-coded values used by the in-game view.
export type CameraOverscan = {
  left: number
  right: number
  top: number
  bottom: number
}

export const DEFAULT_OVERSCAN: CameraOverscan = {
  left: 80,
  right: 80,
  top: 100,
  bottom: 260,
}

export class Camera {
  x = 0
  y = 0
  zoom = 1
  overscan: CameraOverscan = { ...DEFAULT_OVERSCAN }

  screenToWorld(screenX: number, screenY: number) {
    return {
      x: screenX / this.zoom + this.x,
      y: screenY / this.zoom + this.y,
    }
  }

  worldToScreen(worldX: number, worldY: number) {
    return {
      x: (worldX - this.x) * this.zoom,
      y: (worldY - this.y) * this.zoom,
    }
  }

  pan(dx: number, dy: number) {
    this.x += dx
    this.y += dy
  }

  centerOn(
    worldX: number,
    worldY: number,
    viewportWidth: number,
    viewportHeight: number,
    mapWidth: number,
    mapHeight: number,
  ) {
    this.x = worldX - viewportWidth / this.zoom / 2
    this.y = worldY - viewportHeight / this.zoom / 2
    this.clamp(viewportWidth, viewportHeight, mapWidth, mapHeight)
  }

  adjustZoom(delta: number, mouseX: number, mouseY: number) {
    const oldZoom = this.zoom
    const zoomFactor = delta > 0 ? 0.9 : 1.1

    const newZoom = Math.max(0.5, Math.min(2.5, this.zoom * zoomFactor))
    if (newZoom === oldZoom) return

    const worldBefore = this.screenToWorld(mouseX, mouseY)
    this.zoom = newZoom
    const worldAfter = this.screenToWorld(mouseX, mouseY)

    this.x += worldBefore.x - worldAfter.x
    this.y += worldBefore.y - worldAfter.y
  }

  clamp(
    viewportWidth: number,
    viewportHeight: number,
    mapWidth: number,
    mapHeight: number,
  ) {
    const visibleWorldWidth = viewportWidth / this.zoom
    const visibleWorldHeight = viewportHeight / this.zoom

    // Per-edge overscan lives on the instance so individual views (in-game
    // vs. editor) can opt into different chrome-clearing budgets without
    // forking this class.
    const { left, right, top, bottom } = this.overscan

    const minX = -left
    const minY = -top
    const maxX = mapWidth - visibleWorldWidth + right
    const maxY = mapHeight - visibleWorldHeight + bottom

    if (visibleWorldWidth >= mapWidth + left + right) {
      this.x = (mapWidth - visibleWorldWidth) / 2
    } else {
      this.x = Math.max(minX, Math.min(this.x, maxX))
    }

    if (visibleWorldHeight >= mapHeight + top + bottom) {
      this.y = (mapHeight - visibleWorldHeight) / 2
    } else {
      this.y = Math.max(minY, Math.min(this.y, maxY))
    }
  }
}
