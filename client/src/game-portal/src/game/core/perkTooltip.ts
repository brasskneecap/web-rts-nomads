// ─────────────────────────────────────────────────────────────────────────────
// Perk tooltip formatter
//
// Converts a PerkDef's tooltipTemplate into a human-readable string by
// substituting {token} placeholders with live values sourced from the perk's
// effective config and (for trapper perks) the unit's effectiveTrap snapshot.
//
// Supported token forms — see PerkDef.tooltipTemplate JSDoc for the full spec.
// ─────────────────────────────────────────────────────────────────────────────

import type { PerkDef } from '../maps/perkDefs'
import type { Unit } from './GameState'

// ─────────────────────────────────────────────────────────────────────────────
// Config resolution
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Returns the effective config for the perk at its own rank, mirroring the
 * server's ConfigForRank semantics:
 *   base config  ← def.config
 *   merged with  ← def.configByRank[def.rank]  (when present)
 *
 * The rank used here is the perk's own tier (e.g. "bronze"), NOT the unit's
 * current rank — this matches the server-side ConfigForRank call.
 */
function resolveConfig(def: PerkDef): Record<string, number> {
  const rank = def.rank
  if (!rank || !def.configByRank) {
    return def.config
  }
  const override = def.configByRank[rank]
  if (!override || Object.keys(override).length === 0) {
    return def.config
  }
  return { ...def.config, ...override }
}

// ─────────────────────────────────────────────────────────────────────────────
// Number formatting helpers
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Half-away-from-zero rounding to N decimal places.
 * Matches the spec's "round to 1 decimal with half-away-from-zero" requirement.
 */
function roundHalfAwayFromZero(value: number, decimals: number): number {
  const factor = Math.pow(10, decimals)
  return Math.sign(value) * Math.round(Math.abs(value) * factor) / factor
}

/**
 * Format a raw number value:
 *   - Whole integers → no decimal point
 *   - Otherwise → 1 decimal place (half-away-from-zero)
 */
function formatRaw(value: number): string {
  const rounded = roundHalfAwayFromZero(value, 1)
  return Number.isInteger(rounded) ? String(Math.round(rounded)) : rounded.toFixed(1)
}

/**
 * Format value as a forced-N-decimal string.
 */
function formatFixed(value: number, decimals: number): string {
  return roundHalfAwayFromZero(value, decimals).toFixed(decimals)
}

/**
 * Format value as a percent: value × 100, rounded to integer, append "%".
 * E.g. 0.2 → "20%"
 */
function formatPercent(value: number): string {
  return `${Math.round(value * 100)}%`
}

/**
 * Format value as a signed delta percent: (value − 1) × 100, rounded, signed.
 * E.g. 1.25 → "+25%", 0.7 → "-30%"
 */
function formatDeltaPercent(value: number): string {
  const delta = Math.round((value - 1) * 100)
  return delta >= 0 ? `+${delta}%` : `${delta}%`
}

// ─────────────────────────────────────────────────────────────────────────────
// Token regex
//
// Matches tokens of the forms:
//   {key}        {key%}      {key+%}      {key:N}
//   {trap.key}   {trap.key%} {trap.key+%} {trap.key:N}
//
// Named capture groups:
//   trap     — present when the "trap." prefix is used
//   key      — the config key name
//   modifier — "%", "+%", ":N", or "" (raw)
// ─────────────────────────────────────────────────────────────────────────────
const TOKEN_RE = /\{(?<trap>trap\.)?(?<key>[a-zA-Z_][a-zA-Z0-9_]*)(?<modifier>[+]?%|:\d+)?\}/g

// ─────────────────────────────────────────────────────────────────────────────
// Missing-key handling
// ─────────────────────────────────────────────────────────────────────────────

function handleMissingKey(perkId: string, token: string): string {
  if (import.meta.env.DEV) {
    console.error(
      `[perkTooltip] Perk "${perkId}": missing config key in token "${token}". ` +
      'Check the perk JSON and tooltipTemplate.',
    )
  }
  // Return the literal token so the HUD shows the issue without blanking.
  return token
}

// ─────────────────────────────────────────────────────────────────────────────
// Public API
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Formats a perk's tooltip body for display in the HUD.
 *
 * - When `def.tooltipTemplate` is absent, returns `def.description ?? ''` as
 *   a safe rollout path — no template means fall through to the static string.
 * - When the template is present, each `{token}` is replaced with the resolved
 *   numeric value from the perk's effective config or the unit's effectiveTrap.
 * - Missing keys in dev: console.error + render the literal token.
 * - Missing keys in prod: render the literal token silently.
 */
export function formatPerkTooltip(def: PerkDef, unit: Unit): string {
  const trap = unit.effectiveTrap
  // Trap-branched templates: pick the entry matching the unit's owned Bronze
  // trap perk. Prevents multi-variant perks (ascendant_infusion, overload_protocol)
  // from dumping all four trap descriptions into the tooltip.
  const branched = trap?.perkId ? def.tooltipTemplateByTrap?.[trap.perkId] : undefined
  const template = branched ?? def.tooltipTemplate
  if (!template) {
    return def.description ?? ''
  }

  const config = resolveConfig(def)

  return template.replace(TOKEN_RE, (fullMatch, trapPrefix, key, modifier) => {
    let value: number | undefined

    if (trapPrefix) {
      // {trap.key} — read from the unit's live effectiveTrap snapshot.
      if (!trap) {
        return handleMissingKey(def.id, fullMatch)
      }
      // EffectiveTrapSnapshot keys are typed but we need dynamic access.
      // The cast to Record<string, number | undefined> is safe here because
      // all fields of EffectiveTrapSnapshot are optional numbers (or string for
      // perkId, which we never template). If the key doesn't exist on the type
      // at runtime, we catch it via the undefined check below.
      const trapRecord = trap as Record<string, number | string | undefined>
      const raw = trapRecord[key]
      if (typeof raw !== 'number') {
        return handleMissingKey(def.id, fullMatch)
      }
      value = raw
    } else {
      // {key} — read from the perk's resolved config.
      const raw = config[key]
      if (typeof raw !== 'number') {
        return handleMissingKey(def.id, fullMatch)
      }
      value = raw
    }

    // Apply the modifier suffix.
    if (!modifier) {
      // {key} — raw
      return formatRaw(value)
    }
    if (modifier === '%') {
      // {key%} — value × 100 as integer percent
      return formatPercent(value)
    }
    if (modifier === '+%') {
      // {key+%} — delta percent: (value − 1) × 100, signed
      return formatDeltaPercent(value)
    }
    if (modifier.startsWith(':')) {
      // {key:N} — force N decimal places
      const decimals = parseInt(modifier.slice(1), 10)
      return formatFixed(value, decimals)
    }

    // Unreachable given the regex, but satisfies the compiler.
    return formatRaw(value)
  })
}
