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
