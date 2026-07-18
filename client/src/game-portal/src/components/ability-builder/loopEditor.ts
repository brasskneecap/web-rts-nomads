// Helpers for editing a `loop` action's config (iterations + variables + body).
// The loop is a normal, addressable action in a trigger's action list (server:
// ability_exec_loop.go), so its header is edited through the builder's
// updateActionConfig by NodePath — these helpers only read the opaque config and
// compute the next variable name; the component does the write.

import type { AbilityActionDef, LoopVar } from '@/game/abilities/program/abilityProgram'

// LoopView is the flattened, read-only view of a loop action's config.
export interface LoopView {
  iterations: number
  vars: LoopVar[]
  body: AbilityActionDef[]
  // stepFirst applies each variable's step to the first iteration too (see Go
  // loopConfig.StepFirst).
  stepFirst: boolean
}

// readLoop returns a loop action's iterations/vars/body, or null when the action
// isn't a loop. Reads the opaque config defensively (never destructures it).
export function readLoop(action: AbilityActionDef): LoopView | null {
  if (action.type !== 'loop') return null
  const c = action.config ?? {}
  return {
    iterations: typeof c.iterations === 'number' ? c.iterations : 0,
    vars: Array.isArray(c.vars) ? (c.vars as LoopVar[]) : [],
    body: Array.isArray(c.body) ? (c.body as AbilityActionDef[]) : [],
    stepFirst: c.stepFirst === true,
  }
}

// nextVarName returns the next unused single letter a..z, or null once all 26
// are taken.
export function nextVarName(vars: LoopVar[]): string | null {
  const used = new Set(vars.map((v) => v.name))
  for (let c = 97; c <= 122; c++) {
    const name = String.fromCharCode(c)
    if (!used.has(name)) return name
  }
  return null
}

// withVarAdded / withVarRemoved / withVarField return a NEW vars array with the
// edit applied — handed to updateActionConfig({ vars }). Immutable; never mutate
// the input.
export function withVarAdded(vars: LoopVar[]): LoopVar[] {
  const name = nextVarName(vars)
  return name ? [...vars, { name, start: 0, step: 0 }] : vars
}

export function withVarRemoved(vars: LoopVar[], name: string): LoopVar[] {
  return vars.filter((v) => v.name !== name)
}

export function withVarField(vars: LoopVar[], name: string, field: 'start' | 'step', value: number): LoopVar[] {
  return vars.map((v) => (v.name === name ? { ...v, [field]: value } : v))
}

// withVarStepMode sets a variable's step mode ('number' additive or 'percent'
// multiplicative). Returns a new vars array.
export function withVarStepMode(vars: LoopVar[], name: string, mode: 'number' | 'percent'): LoopVar[] {
  return vars.map((v) => (v.name === name ? { ...v, stepMode: mode } : v))
}
