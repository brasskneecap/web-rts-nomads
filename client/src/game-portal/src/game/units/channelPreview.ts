import type { AuthoredAbilityDef } from '@/game/abilities/abilityEditorForm'

// Info the channel-loop preview needs about a unit's channeling ability:
// which one drives the preview (id) and how often it ticks (for the beam
// pulse). tickIntervalSeconds may be undefined when the ability doesn't
// author one — the preview falls back to a default cadence in that case.
export interface ChannelAbilityInfo {
  id: string
  tickIntervalSeconds: number | undefined
}

// A channeling ability is one whose def sets channelType (mirrors
// abilityEditorForm.inferFamily's channel check). Channel-loop frames live on
// the caster (unit/path), not the ability, so any one of the unit's channeling
// abilities is enough to enable the preview — we return the first.
export function pickChannelAbility(
  abilityIds: string[] | undefined,
  defsById: Map<string, AuthoredAbilityDef>,
): ChannelAbilityInfo | null {
  for (const id of abilityIds ?? []) {
    const def = defsById.get(id)
    if (def && def.channelType) {
      return { id, tickIntervalSeconds: def.tickIntervalSeconds }
    }
  }
  return null
}

// Phase in [0, 1) through the current channel tick, given elapsed ms since the
// channel started and the ability's tick interval (seconds). Drives the beam
// pulse that travels from the attack origin outward once per tick. Returns 0
// for a missing / non-positive interval (nothing to pulse) and clamps negative
// elapsed to 0.
export function channelTickPhase(elapsedMs: number, tickIntervalSeconds: number | undefined): number {
  if (!tickIntervalSeconds || tickIntervalSeconds <= 0) return 0
  if (elapsedMs <= 0) return 0
  const intervalMs = tickIntervalSeconds * 1000
  return (elapsedMs % intervalMs) / intervalMs
}
