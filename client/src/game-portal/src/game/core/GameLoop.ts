// src/game/core/GameLoop.ts
type LoopConfig = {
  update: (dt: number) => void
  render: (alpha: number) => void
}

export class GameLoop {
  private updateFn: (dt: number) => void
  private renderFn: (alpha: number) => void

  private running = false
  private lastTime = 0
  private accumulator = 0

  private readonly fixedStep = 1000 / 20 // 20 ticks/sec

  constructor(config: LoopConfig) {
    this.updateFn = config.update
    this.renderFn = config.render
  }

  start() {
    if (this.running) return
    this.running = true
    this.lastTime = performance.now()
    requestAnimationFrame(this.frame)
  }

  stop() {
    this.running = false
  }

  private frame = (now: number) => {
    if (!this.running) return

    let delta = now - this.lastTime
    this.lastTime = now

    if (delta > 250) delta = 250

    this.accumulator += delta

    while (this.accumulator >= this.fixedStep) {
      this.updateFn(this.fixedStep / 1000)
      this.accumulator -= this.fixedStep
    }

    const alpha = this.accumulator / this.fixedStep
    this.renderFn(alpha)

    requestAnimationFrame(this.frame)
  }
}