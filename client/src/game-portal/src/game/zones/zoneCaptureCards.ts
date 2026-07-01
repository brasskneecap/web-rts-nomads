import type { Zone, ZoneSnapshot } from '../network/protocol'
import {
  ZONE_TEAM_OWNER,
  ENEMY_PLAYER_ID,
  NEUTRAL_PLAYER_ID,
  ZONE_AURA_TYPE_STAT_MODIFIER,
} from '../network/protocol'
import { formatModifier } from '../stats/statRegistry'

/** View-model for one zone-capture card in the HUD. Derived entirely from the
 *  per-tick zone snapshot + static map data; no server text. */
export type ZoneCaptureCard = {
  id: string
  name: string
  type: string
  requirement: string
  status: string
  state: 'progress' | 'contested' | 'locked' | 'idle' | 'captured'
  progress: number // 0..1 for the bar; 0 when not applicable
  /** Formatted passive bonuses the zone grants its owner. Shown both before
   *  capture (as a reward preview) and after. Empty when the zone grants
   *  nothing. */
  bonuses: string[]
  ownerColor: string | null
}

/** A zone the team began the match already owning (its spawn/home zone). These
 *  weren't captured, so they're excluded from the captured-card display. */
function isStartingZone(zone: Zone): boolean {
  if (zone.lockedSpawnLabel) return true
  const s = zone.startingOwner
  return !!s && s !== 'neutral' && s !== NEUTRAL_PLAYER_ID
}

/** Format a zone's passive auras into display strings (e.g. "+15% Gold Gather
 *  Rate"). Skips non-stat aura kinds the HUD can't render yet. */
function zoneBonuses(zone: Zone): string[] {
  return (zone.auras ?? [])
    .filter((a) => a.type === ZONE_AURA_TYPE_STAT_MODIFIER && a.modifier)
    .map((a) => formatModifier(a.modifier))
}

type UnitLike = { x: number; y: number; ownerId?: string }
type BuildingLike = { x: number; y: number; width: number; height: number; ownerId?: string | null }

export type ZoneCaptureCardInput = {
  zones: Zone[]
  snapshotsById: Map<string, ZoneSnapshot>
  units: UnitLike[]
  buildings: BuildingLike[]
  cellSize: number
  isFriendlyOwner: (ownerId: string | undefined) => boolean
  isHostileOwner: (ownerId: string | undefined) => boolean
}

function cellKey(x: number, y: number): string {
  return `${x},${y}`
}

/** True when a zone owner string represents the player's team. */
function ownerIsTeam(owner: string | undefined, isFriendlyOwner: (o: string | undefined) => boolean): boolean {
  if (!owner) return false
  if (owner === ZONE_TEAM_OWNER) return true
  if (owner === 'neutral' || owner === ENEMY_PLAYER_ID || owner === NEUTRAL_PLAYER_ID) return false
  return isFriendlyOwner(owner)
}

/** Mirror of the server adjacency gate (zoneCapturableByLocked). Empty links =>
 *  ungated; requireAllLinks => all neighbours team-owned; else any one. */
function zoneCapturable(
  zone: Zone,
  snapshotsById: Map<string, ZoneSnapshot>,
  isFriendlyOwner: (o: string | undefined) => boolean,
): boolean {
  const adj = zone.adjacent ?? []
  if (adj.length === 0) return true
  const owned = adj.filter((id) => ownerIsTeam(snapshotsById.get(id)?.owner, isFriendlyOwner))
  return zone.requireAllLinks ? owned.length === adj.length : owned.length > 0
}

export function buildZoneCaptureCards(input: ZoneCaptureCardInput): ZoneCaptureCard[] {
  const { zones, snapshotsById, units, buildings, cellSize, isFriendlyOwner, isHostileOwner } = input
  const out: ZoneCaptureCard[] = []

  for (const zone of zones) {
    const snap = snapshotsById.get(zone.id)
    if (!snap) continue

    // Captured (team-owned) zones are always shown, surfacing the bonuses they
    // grant. No friendly-unit-inside requirement — ownership alone qualifies.
    // Starting/home zones are excluded: they were owned from the start, not
    // captured.
    if (ownerIsTeam(snap.owner, isFriendlyOwner)) {
      if (isStartingZone(zone)) continue
      out.push({
        id: zone.id,
        name: zone.name || zone.id,
        type: zone.capture.type,
        requirement: '',
        status: 'Captured',
        state: 'captured',
        progress: 0,
        bonuses: zoneBonuses(zone),
        ownerColor: snap.ownerColor && snap.ownerColor.length > 0 ? snap.ownerColor : null,
      })
      continue
    }

    const cellSet = new Set(zone.cells.map(([x, y]) => cellKey(x, y)))
    const inZone = (u: UnitLike) =>
      cellSet.has(cellKey(Math.floor(u.x / cellSize), Math.floor(u.y / cellSize)))

    const friendlyInside = units.some((u) => isFriendlyOwner(u.ownerId) && inZone(u))
    if (!friendlyInside) continue // panel only shows zones we're contesting

    const progress = snap.progress ?? 0
    let requirement = ''
    let status = ''
    let state: ZoneCaptureCard['state'] = 'idle'

    switch (zone.capture.type) {
      case 'claim': {
        const n = (zone.claimPoints?.length ?? 0) > 0 ? zone.claimPoints!.length : 1
        const held = snap.claimPoints ? snap.claimPoints.filter((p) => p.captured).length : 0
        requirement = `Build & defend ${n} tower${n === 1 ? '' : 's'}`
        status = `${held}/${n} points held`
        state = progress > 0 && progress < 1 ? 'progress' : 'idle'
        break
      }
      case 'presence': {
        requirement = 'Hold the zone'
        if (!zoneCapturable(zone, snapshotsById, isFriendlyOwner)) {
          state = 'locked'
          status = 'Locked -- capture an adjacent zone first'
        } else if (snap.contested) {
          state = 'contested'
          status = 'Contested!'
        } else {
          state = progress > 0 ? 'progress' : 'idle'
          status = `Capturing… ${Math.round(progress * 100)}%`
        }
        break
      }
      case 'clear': {
        requirement = 'Defeat all enemies in the zone'
        const enemies = units.filter((u) => isHostileOwner(u.ownerId) && inZone(u)).length
        status = enemies > 0 ? `${enemies} enem${enemies === 1 ? 'y' : 'ies'} remain` : 'Clearing...'
        break
      }
      case 'control_point': {
        requirement = 'Hold a structure on the point'
        const ax = zone.anchor.x
        const ay = zone.anchor.y
        const structure = buildings.find(
          (b) => ax >= b.x && ax < b.x + b.width && ay >= b.y && ay < b.y + b.height,
        )
        const held = !!structure && isFriendlyOwner(structure.ownerId ?? undefined)
        status = held ? 'Structure held' : 'No structure yet'
        break
      }
      default:
        requirement = 'Capture the zone'
        status = ''
    }

    out.push({
      id: zone.id,
      name: zone.name || zone.id,
      type: zone.capture.type,
      requirement,
      status,
      state,
      progress, // 0..1 from the snapshot; the component shows a bar when > 0
      bonuses: zoneBonuses(zone), // shown before capture too, as the reward preview
      ownerColor: snap.ownerColor && snap.ownerColor.length > 0 ? snap.ownerColor : null,
    })
  }

  // Keep actionable (in-progress / contested) cards on top; captured zones
  // settle to the bottom. Array.sort is stable, so each group keeps its order.
  out.sort((a, b) => Number(a.state === 'captured') - Number(b.state === 'captured'))

  return out
}
