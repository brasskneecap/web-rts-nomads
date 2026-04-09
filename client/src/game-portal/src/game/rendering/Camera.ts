// src/game/rendering/Camera.ts
export class Camera {
  x = 0
  y = 0
  zoom = 1

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

    // Allow the camera to pan a bit outside the map.
    const overscan = 80

    const minX = -overscan
    const minY = -overscan
    const maxX = mapWidth - visibleWorldWidth + overscan
    const maxY = mapHeight - visibleWorldHeight + overscan

    if (visibleWorldWidth >= mapWidth + overscan * 2) {
      this.x = (mapWidth - visibleWorldWidth) / 2
    } else {
      this.x = Math.max(minX, Math.min(this.x, maxX))
    }

    if (visibleWorldHeight >= mapHeight + overscan * 2) {
      this.y = (mapHeight - visibleWorldHeight) / 2
    } else {
      this.y = Math.max(minY, Math.min(this.y, maxY))
    }
  }
}
