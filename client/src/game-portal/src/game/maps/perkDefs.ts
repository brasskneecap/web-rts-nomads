// ─────────────────────────────────────────────────────────────────────────────
// Perk definitions — client-side type layer
//
// Mirrors the PerkDef struct in server/internal/game/perk_defs.go and the
// data in server/internal/game/catalog/perk-defs.json.
//
// TO ADD / EDIT A PERK DEFINITION:
//   edit  server/internal/game/catalog/perk-defs.json
//   (types here update automatically — no manual sync needed)
// ─────────────────────────────────────────────────────────────────────────────

export type PerkDef = {
  id: string
  displayName: string
  description?: string
  /**
   * Client-interpolated tooltip template. Tokens in curly braces are replaced
   * with live values from the perk's config (or the unit's effectiveTrap payload
   * for trapper bronze perks). Supported token forms:
   *   {key}       — raw number; integer if whole, else 1 decimal
   *   {key%}      — value×100 as integer percent (0.2 → "20%")
   *   {key+%}     — delta percent: (value−1)×100, signed (1.25 → "+25%")
   *   {key:N}     — force N decimal places
   *   {trap.key}  — read from unit.effectiveTrap (trapper bronze only)
   * When absent, the formatter falls back to description.
   */
  tooltipTemplate?: string
  /**
   * Per-trap-type templates for trapper perks whose effect depends on which
   * Bronze trap perk the unit owns (e.g. ascendant_infusion, overload_protocol).
   * Keys are Bronze trap perk ids ("caltrops", "fire_pit", "explosive_trap",
   * "marker_trap"); the formatter picks the entry matching
   * unit.effectiveTrap?.perkId. When present it takes precedence over
   * tooltipTemplate for the matching trap; falls back to tooltipTemplate (or
   * description) when no match.
   */
  tooltipTemplateByTrap?: Record<string, string>
  /**
   * Action-icon ID used to render this perk's icon in the HUD.
   * Matches an entry in action-icons.json (e.g. "perk-bloodlust").
   * Edit the SVG path for this ID in the action-icon editor to customise the artwork.
   */
  icon?: string
  /** Eligible unit type, e.g. "soldier". Absent = any. */
  unitType?: string
  /** Eligible promotion path, e.g. "berserker". Absent = any. */
  path?: string
  /** Eligible rank tier, e.g. "bronze". Absent = any. */
  rank?: string
  /** Perk-specific tuning values. Keys are documented in perk-defs.json. */
  config: Record<string, number>
  /**
   * Per-rank config overrides keyed by rank name ("bronze" / "silver" / "gold").
   * Values shadow the matching key in config; all other keys fall through to base.
   * Mirrors PerkDef.ConfigByRank on the server.
   */
  configByRank?: Record<string, Record<string, number>>
}

export let PERK_DEFS: PerkDef[] = []
export let PERK_DEF_MAP = new Map<string, PerkDef>()

export function initPerkDefs(defs: PerkDef[]): void {
  PERK_DEFS = defs
  PERK_DEF_MAP = new Map(defs.map((def) => [def.id, def]))
}

// ─────────────────────────────────────────────────────────────────────────────
// Unit-centered aura radii — read by the renderer to draw a dashed range ring
// around units that carry the perk. Keys are perk ids; values are the config
// field holding the radius (in world-space pixels).
//
// Only include perks whose area of effect is ALWAYS centered on this unit
// while it owns the perk. Conditionally-triggered auras (e.g. Last Stand's
// taunt) or detached objects (banners, traps) should NOT go here — render
// those off their own entity instead.
// ─────────────────────────────────────────────────────────────────────────────
type AuraRadiusSource = {
  /** Config key on the perk def that holds the radius (in world pixels). */
  key: string
  /**
   * When true, the ring only renders while the perk is "live" — present in
   * the unit's `activeBuffs`, OR (if `activeEffectName` is set) present in
   * the unit's anchored effects. Use for triggered effects so the
   * visualization appears only during the window it's real. Absent / false =
   * always render while the unit owns the perk.
   */
  onlyWhenActive?: boolean
  /**
   * When set, the perk is also considered "live" while an EffectSnapshot
   * with this name is anchored to the unit. Required for perks like
   * whirlwind_core that drive their visualization through the effect system
   * rather than the buff/icon system.
   */
  activeEffectName?: string
}

const AURA_RADIUS_SOURCES: Record<string, AuraRadiusSource> = {
  // Vanguard — always-on defensive/support auras.
  guardian_aura: { key: 'radius' },
  pain_share: { key: 'radius' },
  interlock: { key: 'radius' },
  brace: { key: 'radius' },
  // Berserker — whirlwind is now an RNG proc per attack. Ring flashes only
  // while the spin effect is anchored to the unit (an EffectSnapshot with
  // name "whirlwind"), mirroring the proc moment rather than implying an
  // always-on aura.
  whirlwind_core: { key: 'radius', onlyWhenActive: true, activeEffectName: 'whirlwind' },
  // Vanguard — triggers once when HP drops below threshold, runs for
  // tauntDurationSeconds. Gated on activeBuffs so the ring only appears
  // while the taunt is actually live.
  last_stand: { key: 'tauntRadius', onlyWhenActive: true },
  // NOT listed — draw off their own entities:
  //   rallying_banner (bannerRadius): banner is a detached map entity.
  //   All trapper perks (caltrops / fire_pit / explosive_trap / marker_trap):
  //     traps live as their own map entities once placed.
}

export function getPerkAuraRadius(
  perkId: string,
  activeBuffIds?: Set<string>,
  activeEffectNames?: Set<string>,
): number | null {
  const source = AURA_RADIUS_SOURCES[perkId]
  if (!source) return null
  if (source.onlyWhenActive) {
    const buffActive = activeBuffIds?.has(perkId) ?? false
    const effectActive = source.activeEffectName
      ? (activeEffectNames?.has(source.activeEffectName) ?? false)
      : false
    if (!buffActive && !effectActive) return null
  }
  const value = PERK_DEF_MAP.get(perkId)?.config?.[source.key]
  return typeof value === 'number' && value > 0 ? value : null
}
