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
   * Per-owned-perk templates for adaptive perks whose effect varies with the
   * unit's other picks (e.g. Siphoner ascended_corruption, which mirrors
   * whichever Silver perk the unit owns). Keys are perk ids; the formatter
   * iterates unit.perkIds in slot order and picks the first key the unit
   * owns. Takes precedence over tooltipTemplate; the tooltip then shows
   * only the branch relevant to this unit's loadout.
   */
  tooltipTemplateByOwnedPerk?: Record<string, string>
  /**
   * Server-computed, presentation-only prose derived from this perk's typed
   * data (statModifiers / abilityModifiers / abilityRiders). Sent by
   * GET /catalog/perks alongside `wired`. Used by formatPerkTooltip as the
   * fallback when tooltipTemplate is absent — see perkTooltip.ts. Never
   * authored client-side.
   */
  generatedDescription?: string
  /**
   * Action-icon ID used to render this perk's icon in the HUD.
   * Matches an entry in action-icons.json (e.g. "perk-bloodlust").
   * Edit the SVG path for this ID in the action-icon editor to customise the artwork.
   */
  icon?: string
  /** Eligible promotion path, e.g. "berserker". Absent = any. */
  path?: string
  /** Perk-specific tuning values. Keys are documented in perk-defs.json. */
  config: Record<string, number>
  /**
   * Per-rank config overrides keyed by rank name ("bronze" / "silver" / "gold").
   * Values shadow the matching key in config; all other keys fall through to base.
   * Mirrors PerkDef.ConfigByRank on the server.
   */
  configByRank?: Record<string, Record<string, number>>
  /**
   * Declarative unit-centered auras this perk grants. Mirrors PerkDef.Auras
   * on the server (server/internal/game/perk_defs.go). This is the source of
   * truth for aura radius/targeting on migrated perks (e.g. zealous_march);
   * legacy perks still express their radius via a `config` key instead (see
   * AURA_RADIUS_SOURCES below) until they're migrated too.
   */
  auras?: PerkAura[]
}

/**
 * PerkAura mirrors one entry of the Go PerkDef.Auras (server/internal/game/
 * perk_defs.go): a unit-centered, always-on aura granted by a perk.
 */
export type PerkAura = {
  /** Aura reach in world-space pixels. */
  radius: number
  targets: 'allies' | 'enemies'
  includeSelf?: boolean
  stacking?: 'max'
  perAdditionalSource?: number
  statModifiers: PerkAuraStatModifier[]
}

/**
 * PerkAuraStatModifier mirrors the Go AuraDef.StatModifiers entry. This is a
 * near-duplicate of PerkStatModifier declared in game/perks/perkEditorForm.ts
 * (the perk-editor module) — deliberately NOT reused here. perkDefs.ts is a
 * core gameplay/runtime module (imported by GameState, GameClient,
 * CanvasRenderer); perkEditorForm.ts is editor-only and does not currently
 * depend on perkDefs.ts. Importing an editor-module type into the runtime
 * module would invert that dependency direction and risks a future circular
 * import if perkEditorForm.ts ever needs a runtime type back. Keep the two
 * shapes in sync if the server schema changes.
 */
export type PerkAuraStatModifier = {
  stat: string
  op: 'add'
  value: number
  stage?: 'intrinsic' | 'base' | 'final'
}

export let PERK_DEFS: PerkDef[] = []
export let PERK_DEF_MAP = new Map<string, PerkDef>()

export function initPerkDefs(defs: PerkDef[]): void {
  PERK_DEFS = defs
  PERK_DEF_MAP = new Map(defs.map((def) => [def.id, def]))
}

// PERK_RANK_BY_ID_MAP: perk id -> the rank bucket ("bronze"/"silver"/"gold")
// a promotion path's perksByRank assigns it to. Perks no longer carry an
// innate rank on their own def (PerkDef.rank was removed — see AI_RULES /
// the perk-system association refactor), so consumers that need to place a
// unit's owned perk in its correct HUD cell (GameState.ts's
// getPerkActionItems) resolve the rank via this map instead, built from
// every promotion path's perksByRank (see initPerkRanksFromPaths). This
// matters for paths with a "dead" rank that grants neither a spell nor a
// perk (e.g. Arch Mage's silver, whose spell pool AND perk pool are both
// empty) — a rank's absence from unit.abilities is not sufficient on its own
// to prove it is the *next* perk-granting rank, so positional inference
// alone is not reliable and this explicit lookup is required.
export let PERK_RANK_BY_ID_MAP = new Map<string, string>()

// initPerkRanksFromPaths rebuilds PERK_RANK_BY_ID_MAP from every promotion
// path's perksByRank bucket (the /catalog/paths `def.perksByRank` field — see
// PathDef.PerksByRank on the server, path_defs.go). A perk id is expected to
// appear in exactly one (path, rank) bucket across the whole catalog; if the
// same id appeared in more than one, the last one processed wins — acceptable
// since this map only drives cosmetic HUD cell placement, never simulation.
export function initPerkRanksFromPaths(perksByRankPerPath: Array<Record<string, string[]> | undefined>): void {
  const map = new Map<string, string>()
  for (const perksByRank of perksByRankPerPath) {
    if (!perksByRank) continue
    for (const [rank, ids] of Object.entries(perksByRank)) {
      for (const id of ids) map.set(id, rank)
    }
  }
  PERK_RANK_BY_ID_MAP = map
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
  // Cleric Silver — always-on support auras. All three read from the
  // "radiusPixels" config key (the bronze-cleric convention) rather than
  // the vanguard "radius" key, so the ring source must point at the right
  // field. The rings render whenever the owning unit is alive — no extra
  // gating required since these auras have no on/off state of their own.
  divine_aegis: { key: 'radiusPixels' },
  restoration_aura: { key: 'radiusPixels' },
  // zealous_march is migrated to the declarative `auras` schema (see
  // PerkDef.auras / getPerkAuraRadius) and is resolved from there before
  // this map is ever consulted. This entry is kept only as a dead-path
  // safety net in case the aura data is ever missing from the catalog
  // response; remove it once server deletes zealous_march's legacy
  // config.radiusPixels key.
  zealous_march: { key: 'radiusPixels' },
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
  const def = PERK_DEF_MAP.get(perkId)

  // Prefer the declarative aura schema (PerkDef.auras) when present — it is
  // the single source of truth for migrated perks (e.g. zealous_march) and
  // takes priority even if a legacy config key is also still present, so the
  // server can delete the legacy key without the ring going stale. If a perk
  // ever carries multiple auras, use the largest radius since the ring is
  // meant to represent the perk's overall reach.
  if (def?.auras && def.auras.length > 0) {
    let largest = 0
    for (const aura of def.auras) {
      if (aura.radius > largest) largest = aura.radius
    }
    if (largest > 0) return largest
  }

  // Fall back to the legacy config-key lookup for perks not yet migrated to
  // the `auras` schema.
  const source = AURA_RADIUS_SOURCES[perkId]
  if (!source) return null
  if (source.onlyWhenActive) {
    const buffActive = activeBuffIds?.has(perkId) ?? false
    const effectActive = source.activeEffectName
      ? (activeEffectNames?.has(source.activeEffectName) ?? false)
      : false
    if (!buffActive && !effectActive) return null
  }
  const value = def?.config?.[source.key]
  return typeof value === 'number' && value > 0 ? value : null
}
