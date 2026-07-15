// Client mirror of the server's roll-coverage rule (roll_coverage.go).
//
// A weighted list's entries and a table's rows must TILE the die — cover
// 1..maxRoll with no gaps and no overlaps — so every roll lands on exactly one
// outcome. The server re-validates; this exists so an author sees the hole while
// they are making it rather than after clicking Save.
//
// Gaps are an ERROR, not an implicit "nothing happens": a hole in the ranges is
// indistinguishable from a typo, which is exactly how the one gap that ever
// shipped (a deliberate 10% no-drop) read.

/** One claimed slice of the die, labelled by what it yields. */
export type CoverageRange = {
  min: number
  max: number
  label: string
}

/** A contiguous slice of the die, resolved: what it yields and how likely. */
export type CoverageBand = {
  min: number
  max: number
  label: string
  /** Rolls this band covers. */
  rolls: number
  /** Share of the die, 0-100, rounded to one decimal. */
  percent: number
  /** True for a band nothing claims — always an error. */
  uncovered: boolean
}

export type CoverageReport = {
  bands: CoverageBand[]
  errors: string[]
  /** True when the die is completely and uniquely covered. */
  complete: boolean
}

/**
 * Resolves the die into bands and reports what is wrong with it.
 *
 * Returns bands for the WHOLE die, including the uncovered stretches — so the
 * editor can show the hole rather than just say there is one.
 */
export function analyzeCoverage(maxRoll: number, ranges: CoverageRange[]): CoverageReport {
  const errors: string[] = []

  if (!Number.isFinite(maxRoll) || maxRoll < 1) {
    return { bands: [], errors: ['Max roll must be at least 1.'], complete: false }
  }
  if (ranges.length === 0) {
    return { bands: [], errors: ['Needs at least one entry.'], complete: false }
  }

  for (const r of ranges) {
    if (r.min < 1 || r.max > maxRoll) {
      errors.push(`${r.label} claims rolls ${r.min}–${r.max}, outside the die 1–${maxRoll}.`)
    }
    if (r.min > r.max) {
      errors.push(`${r.label} claims rolls ${r.min}–${r.max}, which is backwards.`)
    }
  }

  const sorted = [...ranges].sort((a, b) => a.min - b.min)
  const bands: CoverageBand[] = []
  const band = (min: number, max: number, label: string, uncovered: boolean): CoverageBand => {
    const rolls = max - min + 1
    return { min, max, label, rolls, percent: Math.round((rolls / maxRoll) * 1000) / 10, uncovered }
  }

  let next = 1
  for (const r of sorted) {
    if (r.min > next) {
      bands.push(band(next, r.min - 1, 'nothing — uncovered', true))
      errors.push(`Rolls ${next}–${r.min - 1} land on nothing. Every roll must be covered.`)
    } else if (r.min < next) {
      errors.push(`Rolls ${r.min}–${Math.min(r.max, next - 1)} are claimed twice (${r.label} overlaps the range before it).`)
    }
    if (r.min <= r.max) {
      bands.push(band(Math.max(r.min, 1), Math.min(r.max, maxRoll), r.label, false))
    }
    next = Math.max(next, r.max + 1)
  }
  if (next <= maxRoll) {
    bands.push(band(next, maxRoll, 'nothing — uncovered', true))
    errors.push(`Rolls ${next}–${maxRoll} land on nothing. Every roll must be covered.`)
  }

  return { bands, errors, complete: errors.length === 0 }
}
