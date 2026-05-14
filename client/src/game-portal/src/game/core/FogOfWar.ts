export type CellState = 0 | 1 | 3

export class FogOfWar {
  cols: number = 0
  rows: number = 0
  cells: Uint8Array = new Uint8Array(0)
  revTick: number = -1

  applySnapshot(snap: { cols: number; rows: number; runs: number[]; revTick: number }): boolean {
    if (snap.revTick === this.revTick) return false
    this.cols = snap.cols
    this.rows = snap.rows
    if (this.cells.length !== snap.cols * snap.rows) {
      this.cells = new Uint8Array(snap.cols * snap.rows)
    }
    let idx = 0
    for (let i = 0; i < snap.runs.length - 1; i += 2) {
      const state = snap.runs[i]
      const count = snap.runs[i + 1]
      this.cells.fill(state, idx, idx + count)
      idx += count
    }
    this.revTick = snap.revTick
    return true
  }

  isClear(worldX: number, worldY: number, cellSize: number): boolean {
    const gx = Math.floor(worldX / cellSize)
    const gy = Math.floor(worldY / cellSize)
    if (gx < 0 || gy < 0 || gx >= this.cols || gy >= this.rows) return false
    return this.cells[gy * this.cols + gx] === 3
  }

  isEverSeen(worldX: number, worldY: number, cellSize: number): boolean {
    const gx = Math.floor(worldX / cellSize)
    const gy = Math.floor(worldY / cellSize)
    if (gx < 0 || gy < 0 || gx >= this.cols || gy >= this.rows) return false
    return this.cells[gy * this.cols + gx] >= 1
  }

  cellAt(gx: number, gy: number): CellState {
    if (gx < 0 || gy < 0 || gx >= this.cols || gy >= this.rows) return 0
    const v = this.cells[gy * this.cols + gx]
    return (v === 3 ? 3 : v === 1 ? 1 : 0) as CellState
  }
}
