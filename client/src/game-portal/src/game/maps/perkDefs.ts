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
   * When true, the ring only renders while the perk id is present in the
   * unit's `activeBuffs`. Use for triggered effects (e.g. Last Stand's taunt
   * pulse) so the visualization appears only during the window it's real.
   * Absent / false = always render while the unit owns the perk.
   */
  onlyWhenActive?: boolean
}

const AURA_RADIUS_SOURCES: Record<string, AuraRadiusSource> = {
  // Vanguard — always-on defensive/support auras.
  guardian_aura: { key: 'radius' },
  pain_share: { key: 'radius' },
  interlock: { key: 'radius' },
  brace: { key: 'radius' },
  // Berserker — whirlwind AoE that pulses on/off; ring shows the area it
  // will hit while pulsing so the player can position accordingly.
  whirlwind_core: { key: 'radius' },
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
): number | null {
  const source = AURA_RADIUS_SOURCES[perkId]
  if (!source) return null
  if (source.onlyWhenActive && !activeBuffIds?.has(perkId)) return null
  const value = PERK_DEF_MAP.get(perkId)?.config?.[source.key]
  return typeof value === 'number' && value > 0 ? value : null
}
