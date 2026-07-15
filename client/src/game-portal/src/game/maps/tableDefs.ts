/**
 * A table is a weighted roll over lists, resource grants, and no-drop outcomes —
 * what a camp rolls when cleared, and what a shop rolls to stock its shelves.
 *
 * A table is a `maxRoll` die and rows that TILE it: every roll from 1 to maxRoll
 * lands on exactly one row (no gaps, no overlaps). That totality is the point —
 * "nothing happens" is a row you can see and read a percentage off, not a hole
 * in the ranges.
 */
export type TableDef = {
  id: string
  name: string
  maxRoll: number
  rows: TableRow[]
}

/** A row owns a slice of the die and exactly ONE outcome. */
export type TableRow = {
  min: number
  max: number
  /** Roll this list and grant the item it yields. */
  list?: string
  /** Grant these resources (gold / wood). */
  resources?: Record<string, number>
  /** Grant nothing. */
  nothing?: boolean
}

export let TABLE_DEFS: TableDef[] = []
export let TABLE_DEF_MAP = new Map<string, TableDef>()

export function initTableDefs(defs: TableDef[]): void {
  TABLE_DEFS = defs
  TABLE_DEF_MAP = new Map(defs.map((def) => [def.id, def]))
}

/** Which outcome a row declares, for display. */
export type RowOutcome = 'list' | 'resources' | 'nothing' | 'none'

export function rowOutcome(row: TableRow): RowOutcome {
  const n = (row.list ? 1 : 0) + (row.resources && Object.keys(row.resources).length ? 1 : 0) + (row.nothing ? 1 : 0)
  if (n !== 1) return 'none'
  if (row.list) return 'list'
  if (row.resources && Object.keys(row.resources).length) return 'resources'
  return 'nothing'
}
