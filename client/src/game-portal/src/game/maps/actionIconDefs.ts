export type ActionIconDef = {
  id: string
  path: string
}

export let ACTION_ICON_DEFS: ActionIconDef[] = []

export let ACTION_ICON_MAP = new Map<string, string>()

export function initActionIcons(defs: ActionIconDef[]): void {
  ACTION_ICON_DEFS = defs
  ACTION_ICON_MAP = new Map(defs.map((def) => [def.id, def.path]))
}

// iconKindForId: the overhead-icon channel is encoded in the id's prefix
// (see CanvasRenderer's drawUnitActiveBuffs/drawUnitActiveDebuffs — "buff-*"
// ids resolve via PERK_DEF_MAP, "debuff-*" ids resolve directly, both against
// this same map). Shared here so the Ability Builder's apply_mark picker
// (OverheadIconPicker.vue / InspectorBar.vue) derives iconKind from the
// chosen id instead of asking the author to set a second field that could go
// stale relative to the first. Returns undefined for an id with neither
// prefix (there are none published today, but a future addition shouldn't
// crash — the caller decides the fallback).
export function iconKindForId(id: string): 'buff' | 'debuff' | undefined {
  if (id.startsWith('debuff-')) return 'debuff'
  if (id.startsWith('buff-')) return 'buff'
  return undefined
}
