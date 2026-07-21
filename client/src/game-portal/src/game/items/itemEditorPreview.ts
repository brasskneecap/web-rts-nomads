// Live preview for the item editor: turns the in-progress FORM into the same
// ItemDef shape the server would serve, so the editor can render the real
// in-game tooltip card (ItemTooltipCard) instead of a lookalike.
//
// The one piece of real work here is proc resolution. On the wire the server
// marshals each proc with its RESOLVED payload (effect def + the item's
// overrides); an unsaved draft has never been to the server, so we resolve it
// here from the proc-effect catalog the editor already fetched. This mirrors
// resolveProcEffectParams on the server — the rule is "a non-zero override
// wins, otherwise inherit the effect def" — and it is the only place the client
// re-implements a server rule. Keep the two in step.
import type { ItemDef, ItemProcWire } from '../maps/itemDefs'
import type { ItemEditorForm, ProcForm } from './itemEditorForm'

function resolveProc(proc: ProcForm): ItemProcWire | null {
  // A proc casts an ability at what it hits; emit the reference so the
  // tooltip/summary can render "casts <ability>".
  if (!proc.ability) return null
  return { trigger: proc.trigger, chance: proc.chancePct / 100, ability: proc.ability }
}

/**
 * Builds the ItemDef the tooltip preview renders from. Mirrors
 * saveRequestFromForm's omission rules (zero stats and empty elemental rows are
 * dropped) so the preview shows exactly what a save would produce.
 */
export function previewDefFromForm(form: ItemEditorForm): ItemDef {
  const m = form.mods
  const def: ItemDef = {
    id: form.id,
    displayName: form.displayName || form.id || 'Untitled',
    description: form.description || undefined,
    iconKey: form.iconKey || form.id,
    kind: form.kind,
    tier: form.tier as ItemDef['tier'],
    category: form.category,
    costGold: form.costGold,
  }

  if (form.kind === 'consumable') {
    const c = form.consumable
    def.consumable = {
      type: c.type,
      amount: c.amount || undefined,
      range: c.range || undefined,
      split: c.split,
      durationSeconds: c.durationSeconds || undefined,
    }
    def.maxStacks = form.maxStacks || undefined
    return def
  }

  const modifiers: NonNullable<ItemDef['modifiers']> = {}
  if (m.hp) modifiers.hp = m.hp
  if (m.damage) modifiers.damage = m.damage
  if (m.armor) modifiers.armor = m.armor
  if (m.attackSpeed) modifiers.attackSpeed = m.attackSpeed
  if (m.moveSpeed) modifiers.moveSpeed = m.moveSpeed
  if (m.healthRegen) modifiers.healthRegen = m.healthRegen
  if (m.maxShield) modifiers.maxShield = m.maxShield
  if (m.dodgePct) modifiers.dodgeChance = m.dodgePct / 100
  if (m.blockPct) modifiers.blockChance = m.blockPct / 100
  if (Object.keys(modifiers).length > 0) def.modifiers = modifiers

  const elemental = form.elemental.filter((e) => e.amount > 0 && e.type)
  if (elemental.length > 0) def.onHitElemental = elemental.map((e) => ({ ...e }))

  const procs = form.procs
    .map((p) => resolveProc(p))
    .filter((p): p is ItemProcWire => p !== null)
  if (procs.length > 0) def.procs = procs

  return def
}
