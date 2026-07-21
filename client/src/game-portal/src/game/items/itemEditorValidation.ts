// Client-side mirror of the server's item validation, used ONLY to drive the
// editor's live checklist. The server is still the authority — SaveEditorItem
// re-validates everything and its message is what surfaces on a failed save.
// This exists so an author sees a problem while typing instead of after
// clicking Save.
//
// Every rule below has a counterpart in validateItemDef / SaveEditorItem
// (server/internal/game/items.go, item_editor.go). If you add a rule there, add
// it here; if the two disagree, the server wins.
import type { ValidationCheck } from '@/components/editor/ValidationChecklist.vue'
import type { ItemEditorForm } from './itemEditorForm'

const ID_PATTERN = /^[a-z0-9_]+$/

export type ValidationContext = {
  /** Every item id in the catalog — recipe ingredients must name one. */
  knownItemIds: Set<string>
}

export function validateItemForm(form: ItemEditorForm, ctx: ValidationContext): ValidationCheck[] {
  const checks: ValidationCheck[] = []

  // A NEW def whose id already exists would silently overwrite that item on
  // save (the server treats a save as an upsert), so refuse it here.
  const collides = form.isNew && ctx.knownItemIds.has(form.id)
  checks.push(
    !form.id
      ? { ok: false, message: 'ID is required.' }
      : !ID_PATTERN.test(form.id)
        ? { ok: false, message: 'ID must be lowercase letters, digits and underscores only.' }
        : collides
          ? { ok: false, message: `An item with the ID "${form.id}" already exists — pick another.` }
          : { ok: true, message: 'ID is valid.' },
  )

  checks.push(form.displayName
    ? { ok: true, message: 'Display name is set.' }
    : { ok: false, message: 'Display name is required.' })

  if (form.kind === 'equipment') {
    const badPct = [
      form.mods.dodgePct >= 100 || form.mods.dodgePct < 0 ? 'Dodge' : '',
      form.mods.blockPct >= 100 || form.mods.blockPct < 0 ? 'Block' : '',
    ].filter(Boolean)
    checks.push(badPct.length > 0
      ? { ok: false, message: `${badPct.join(' and ')} chance must be between 0 and 99%.` }
      : { ok: true, message: 'Stat values are in range.' })

    // A proc with no ability chosen is dropped at save rather than rejected, so
    // flag it as an unfinished row instead of an error the server would return.
    const unfinished = form.procs.filter((p) => !p.ability).length
    const badChance = form.procs.some((p) => p.chancePct <= 0 || p.chancePct > 100)
    checks.push(
      badChance
        ? { ok: false, message: 'Every proc chance must be between 1 and 100%.' }
        : unfinished > 0
          ? { ok: false, message: `${unfinished} proc${unfinished > 1 ? 's have' : ' has'} no ability selected and will not be saved.` }
          : { ok: true, message: form.procs.length > 0 ? 'All procs are valid.' : 'No procs.' },
    )
  }

  if (form.kind === 'consumable') {
    checks.push(form.consumable.type
      ? { ok: true, message: 'Consumable effect is set.' }
      : { ok: false, message: 'A consumable needs an effect type.' })
  }

  if (form.crafting.isRecipe) {
    const inputs = form.crafting.inputs.filter(Boolean)
    const unknown = inputs.filter((i) => !ctx.knownItemIds.has(i))
    const selfRef = inputs.includes(form.id)
    checks.push(
      inputs.length < 2
        ? { ok: false, message: 'A craftable item needs at least 2 ingredients.' }
        : selfRef
          ? { ok: false, message: 'An item cannot be its own ingredient.' }
          : unknown.length > 0
            ? { ok: false, message: `Unknown ingredient: ${unknown.join(', ')}.` }
            : { ok: true, message: 'Crafting requirements met.' },
    )
    const negative = [
      form.crafting.craftCost < 0 ? 'Craft cost' : '',
      form.crafting.recipeCost < 0 ? 'Recipe cost' : '',
    ].filter(Boolean)
    if (negative.length > 0) {
      checks.push({ ok: false, message: `${negative.join(' and ')} must not be negative.` })
    }
  }

  return checks
}

/** True when nothing in the checklist blocks a save. */
export function isFormSaveable(checks: ValidationCheck[]): boolean {
  return checks.every((c) => c.ok)
}
