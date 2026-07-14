import type { ItemTier, ItemDef } from '../maps/itemDefs'

/** Consistent tier border/accent colors used in vault and inventory UIs. */
export const TIER_COLORS: Record<ItemTier, string> = {
  common:    '#9ca3af',
  uncommon:  '#4ade80',
  rare:      '#60a5fa',
  epic:      '#c084fc',
  legendary: '#fb923c',
}

/**
 * Builds a short human-readable stat description for an item tooltip.
 * E.g. "+5 Damage, +2 Armor" for equipment; "Heals 50 HP" for a potion.
 */
export function buildItemTooltipBody(def: ItemDef): string {
  const parts: string[] = []

  if (def.kind === 'consumable' && def.consumable) {
    const c = def.consumable
    if (c.type === 'heal' && c.amount !== undefined) {
      parts.push(`Heals ${c.amount} HP`)
    } else if (c.type === 'grant_xp' && c.amount !== undefined) {
      parts.push(`Grants ${c.amount} XP`)
    } else if (c.type === 'shield' && c.amount !== undefined) {
      parts.push(`Grants ${c.amount} Shield`)
    } else if (c.type === 'buff' && c.durationSeconds !== undefined) {
      parts.push(`Buff for ${c.durationSeconds}s`)
    } else {
      parts.push(c.type.charAt(0).toUpperCase() + c.type.slice(1))
    }
    // Consumables are used as a ground-targeted AoE; say how the amount is
    // distributed so "Heals 100 HP" isn't read as per-unit when it splits.
    parts.push(c.split !== false ? 'split between units in the area' : 'to every unit in the area')
  }

  const m = def.modifiers
  if (m) {
    if (m.hp)          parts.push(`+${m.hp} HP`)
    if (m.damage)      parts.push(`+${m.damage} Damage`)
    if (m.armor)       parts.push(`+${m.armor} Armor`)
    if (m.attackSpeed) parts.push(`+${m.attackSpeed.toFixed(2)} Attack Speed`)
    if (m.moveSpeed)   parts.push(`+${m.moveSpeed} Move Speed`)
    if (m.healthRegen) parts.push(`+${m.healthRegen} HP/s`)
    if (m.maxShield)   parts.push(`+${m.maxShield} Max Shield`)
    if (m.dodgeChance) parts.push(`+${Math.round(m.dodgeChance * 100)}% Dodge Chance`)
    if (m.blockChance) parts.push(`+${Math.round(m.blockChance * 100)}% Block Chance`)
  }

  if (def.effects && def.effects.length > 0) {
    for (const fx of def.effects) {
      switch (fx) {
        case 'lifesteal':    parts.push('Lifesteal'); break
        case 'regenerate':   parts.push('Regenerate'); break
        case 'aura-buff':    parts.push('Aura Buff'); break
        case 'reveal-fog':   parts.push('Reveal Fog'); break
        case 'damage-reflect': parts.push('Damage Reflect'); break
      }
    }
  }

  if (def.onHitElemental?.length) {
    for (const e of def.onHitElemental) {
      const elem = e.type.charAt(0).toUpperCase() + e.type.slice(1)
      parts.push(`+${e.amount} ${elem} damage on hit`)
    }
  }
  // One line per proc, in catalog order — an item may carry several, including
  // more than one on the same trigger.
  for (const proc of def.procs ?? []) {
    const pct = Math.round(proc.chance * 100)
    const elem = proc.damageType.charAt(0).toUpperCase() + proc.damageType.slice(1)
    parts.push(proc.trigger === 'onStruck'
      ? `${pct}% when hit: ${proc.damage} ${elem} bolt at the attacker`
      : `${pct}% on hit: ${proc.damage} ${elem} bolt`)
  }

  return parts.join(', ')
}
