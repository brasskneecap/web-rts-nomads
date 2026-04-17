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
