# Zone Capture Requirements HUD + Ghost Tower Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show, below the objectives HUD, a card per zone your team occupies but doesn't own yet — with the capture requirement + live status — and draw a translucent "ghost" tower on each un-built claim capture point.

**Architecture:** All client-side, no protocol/server changes. A pure `buildZoneCaptureCards` function derives the card view-model from data the client already has (`zoneSnapshotsById`, `mapConfig.zones`, `units`, `mapConfig.buildings`). `GameState` wraps it and exposes it on the `GameUiSnapshot`; a new `ZoneCapturePanel.vue` renders it below `MatchObjectivesPanel`. The ghost tower is a `CanvasRenderer` change in the existing claim-slot block.

**Tech Stack:** TypeScript, Vue 3, HTML canvas, Vitest (`vitest run`). Verified via `npx vue-tsc -b` (the two errors in `useGameClient.ts:9` and `GameClient.ts:323` are PRE-EXISTING and unrelated — the bar is "no new errors").

**Design doc:** [docs/superpowers/specs/2026-06-22-zone-capture-requirements-hud.md](../specs/2026-06-22-zone-capture-requirements-hud.md)

---

## File Structure

- **Create** `client/src/game-portal/src/game/zones/zoneCaptureCards.ts` — pure `buildZoneCaptureCards` + `ZoneCaptureCard` type. No DOM, no Vue.
- **Create** `client/src/game-portal/src/game/zones/zoneCaptureCards.test.ts` — vitest unit tests for the builder.
- **Modify** `client/src/game-portal/src/game/core/GameState.ts` — `getZoneCaptureCards()` + a friendly-owner helper.
- **Modify** `client/src/game-portal/src/game/core/GameClient.ts` — add `zoneCaptureCards` to `GameUiSnapshot` + `getUiSnapshot()`.
- **Modify** `client/src/game-portal/src/composables/useGameClient.ts` — add `zoneCaptureCards: []` to `emptyUiSnapshot`.
- **Create** `client/src/game-portal/src/components/match/ZoneCapturePanel.vue` — renders the cards.
- **Modify** `client/src/game-portal/src/views/Match.vue` — mount the panel below objectives.
- **Modify** `client/src/game-portal/src/game/rendering/CanvasRenderer.ts` — ghost tower in the claim-slot block.

All commands run from `client/src/game-portal` unless noted.

---

## Task 1: Pure `buildZoneCaptureCards` builder + tests

**Files:**
- Create: `client/src/game-portal/src/game/zones/zoneCaptureCards.ts`
- Test: `client/src/game-portal/src/game/zones/zoneCaptureCards.test.ts`

- [ ] **Step 1: Write the failing test**

Create `zoneCaptureCards.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { buildZoneCaptureCards, type ZoneCaptureCardInput } from './zoneCaptureCards'
import type { Zone, ZoneSnapshot } from '../network/protocol'
import { ZONE_TEAM_OWNER } from '../network/protocol'

const CELL = 64

function zone(partial: Partial<Zone> & { id: string }): Zone {
  return {
    id: partial.id,
    anchor: partial.anchor ?? { x: 0, y: 0 },
    cells: partial.cells ?? [],
    capture: partial.capture ?? { type: 'presence' },
    ...partial,
  } as Zone
}

// One friendly unit centered on cell (cx, cy).
function unitAt(cx: number, cy: number, ownerId = 'p1') {
  return { x: (cx + 0.5) * CELL, y: (cy + 0.5) * CELL, ownerId }
}

function baseInput(over: Partial<ZoneCaptureCardInput>): ZoneCaptureCardInput {
  return {
    zones: [],
    snapshotsById: new Map(),
    units: [],
    buildings: [],
    cellSize: CELL,
    isFriendlyOwner: (o) => o === 'p1',
    isHostileOwner: (o) => o === '__enemy__' || o === '__neutral__',
    ...over,
  }
}

describe('buildZoneCaptureCards', () => {
  it('claim zone: requirement + held/total from snapshot', () => {
    const z = zone({
      id: 'ridge', name: 'North Ridge',
      cells: [[5, 5]], anchor: { x: 5, y: 5 },
      capture: { type: 'claim' }, claimPoints: [[5, 5], [8, 5]],
    })
    const snap: ZoneSnapshot = {
      id: 'ridge', owner: 'neutral', progress: 0.5,
      claimPoints: [{ progress: 1, captured: true }, { progress: 0.5 }],
    }
    const cards = buildZoneCaptureCards(baseInput({
      zones: [z], snapshotsById: new Map([['ridge', snap]]), units: [unitAt(5, 5)],
    }))
    expect(cards).toHaveLength(1)
    expect(cards[0].requirement).toBe('Build & defend 2 towers')
    expect(cards[0].status).toBe('1/2 points held')
    expect(cards[0].progress).toBe(0.5)
  })

  it('presence: capturing / contested / locked states', () => {
    const seed = zone({ id: 'seed', cells: [[0, 0]], capture: { type: 'presence' } })
    const open = zone({ id: 'open', cells: [[5, 5]], capture: { type: 'presence' } })
    const gated = zone({ id: 'gated', cells: [[6, 6]], capture: { type: 'presence' }, adjacent: ['seed'] })
    const snaps = new Map<string, ZoneSnapshot>([
      ['seed', { id: 'seed', owner: 'neutral' }],
      ['open', { id: 'open', owner: 'neutral', progress: 0.5 }],
      ['gated', { id: 'gated', owner: 'neutral', progress: 0 }],
    ])
    const capturing = buildZoneCaptureCards(baseInput({ zones: [open], snapshotsById: snaps, units: [unitAt(5, 5)] }))
    expect(capturing[0].state).toBe('progress')
    expect(capturing[0].status).toBe('Capturing… 50%')

    const contestedSnaps = new Map(snaps)
    contestedSnaps.set('open', { id: 'open', owner: 'neutral', progress: 0.3, contested: true })
    const contested = buildZoneCaptureCards(baseInput({ zones: [open], snapshotsById: contestedSnaps, units: [unitAt(5, 5)] }))
    expect(contested[0].state).toBe('contested')

    // gated's only prereq 'seed' is NOT team-owned → locked.
    const locked = buildZoneCaptureCards(baseInput({ zones: [seed, gated], snapshotsById: snaps, units: [unitAt(6, 6)] }))
    const card = locked.find((c) => c.id === 'gated')!
    expect(card.state).toBe('locked')
  })

  it('clear zone: counts hostile units inside', () => {
    const z = zone({ id: 'camp', cells: [[5, 5], [6, 5]], capture: { type: 'clear' } })
    const snaps = new Map<string, ZoneSnapshot>([['camp', { id: 'camp', owner: 'neutral' }]])
    const cards = buildZoneCaptureCards(baseInput({
      zones: [z], snapshotsById: snaps,
      units: [unitAt(5, 5), unitAt(6, 5, '__enemy__'), unitAt(5, 5, '__neutral__')],
    }))
    expect(cards[0].requirement).toBe('Defeat all enemies in the zone')
    expect(cards[0].status).toBe('2 enemies remain')
  })

  it('skips zones with no friendly units inside, and team-owned zones', () => {
    const z = zone({ id: 'a', cells: [[5, 5]], capture: { type: 'presence' } })
    const owned = zone({ id: 'b', cells: [[7, 7]], capture: { type: 'presence' } })
    const snaps = new Map<string, ZoneSnapshot>([
      ['a', { id: 'a', owner: 'neutral' }],
      ['b', { id: 'b', owner: ZONE_TEAM_OWNER }],
    ])
    // 'a' has no unit inside; 'b' is team-owned even with a unit inside.
    const cards = buildZoneCaptureCards(baseInput({
      zones: [z, owned], snapshotsById: snaps, units: [unitAt(7, 7)],
    }))
    expect(cards).toHaveLength(0)
  })
})
```

- [ ] **Step 2: Run it, verify it FAILS**

Run: `cd "c:/Personal Dev/webrts/client/src/game-portal" && npx vitest run src/game/zones/zoneCaptureCards.test.ts`
Expected: FAIL — cannot resolve `./zoneCaptureCards` (module not created yet).

- [ ] **Step 3: Implement the builder**

Create `zoneCaptureCards.ts`:

```ts
import type { Zone, ZoneSnapshot } from '../network/protocol'
import { ZONE_TEAM_OWNER, ENEMY_PLAYER_ID, NEUTRAL_PLAYER_ID } from '../network/protocol'

/** View-model for one zone-capture card in the HUD. Derived entirely from the
 *  per-tick zone snapshot + static map data; no server text. */
export type ZoneCaptureCard = {
  id: string
  name: string
  type: string
  requirement: string
  status: string
  state: 'progress' | 'contested' | 'locked' | 'idle'
  progress: number // 0..1 for the bar; 0 when not applicable
  ownerColor: string | null
}

type UnitLike = { x: number; y: number; ownerId?: string }
type BuildingLike = { x: number; y: number; width: number; height: number; ownerId?: string }

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

/** Mirror of the server adjacency gate (zoneCapturableByLocked). Empty links ⇒
 *  ungated; requireAllLinks ⇒ all neighbours team-owned; else any one. */
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
    if (ownerIsTeam(snap.owner, isFriendlyOwner)) continue // already ours — no card

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
          status = 'Locked — capture an adjacent zone first'
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
        status = enemies > 0 ? `${enemies} enem${enemies === 1 ? 'y' : 'ies'} remain` : 'Clearing…'
        break
      }
      case 'control_point': {
        requirement = 'Hold a structure on the point'
        const ax = zone.anchor.x
        const ay = zone.anchor.y
        const structure = buildings.find(
          (b) => ax >= b.x && ax < b.x + b.width && ay >= b.y && ay < b.y + b.height,
        )
        const held = !!structure && isFriendlyOwner(structure.ownerId)
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
      ownerColor: snap.ownerColor && snap.ownerColor.length > 0 ? snap.ownerColor : null,
    })
  }

  return out
}
```

- [ ] **Step 4: Run the test, verify PASS**

Run: `cd "c:/Personal Dev/webrts/client/src/game-portal" && npx vitest run src/game/zones/zoneCaptureCards.test.ts`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add client/src/game-portal/src/game/zones/zoneCaptureCards.ts client/src/game-portal/src/game/zones/zoneCaptureCards.test.ts
git commit -m "feat(hud): pure builder for zone-capture requirement cards"
```

---

## Task 2: Expose cards from GameState + GameUiSnapshot

**Files:**
- Modify: `client/src/game-portal/src/game/core/GameState.ts`
- Modify: `client/src/game-portal/src/game/core/GameClient.ts`
- Modify: `client/src/game-portal/src/composables/useGameClient.ts`

- [ ] **Step 1: Add `getZoneCaptureCards()` + friendly-owner helper to GameState**

In `GameState.ts`, add the import near the other `../zones` / protocol imports at the top:

```ts
import { buildZoneCaptureCards, type ZoneCaptureCard } from '../zones/zoneCaptureCards'
```

Then add these two methods to the `GameState` class (place them right after the existing `getObjectives()` method, ~line 2803). `teamOf`, `isHostileToLocalPlayer`, `ENEMY_PLAYER_ID`, `NEUTRAL_PLAYER_ID`, `this.localPlayerId`, `this.units`, `this.zoneSnapshotsById`, and `this.mapConfig` already exist in this file:

```ts
  /** A unit owner counts as "my team" when it's the local player or an allied
   *  player on the same team — never the enemy/neutral AI. Mirrors the server
   *  playersAreFriendly chokepoint for the zone-capture HUD. */
  private isFriendlyOwnerForZone(ownerId: string | undefined): boolean {
    if (!ownerId || ownerId === ENEMY_PLAYER_ID || ownerId === NEUTRAL_PLAYER_ID) return false
    if (!this.localPlayerId) return false
    if (ownerId === this.localPlayerId) return true
    return this.teamOf(ownerId) === this.teamOf(this.localPlayerId)
  }

  /** Zone-capture requirement cards for zones my team currently occupies but
   *  does not yet own. Drives ZoneCapturePanel. Empty when no zones qualify. */
  getZoneCaptureCards(): ZoneCaptureCard[] {
    return buildZoneCaptureCards({
      zones: this.mapConfig.zones ?? [],
      snapshotsById: this.zoneSnapshotsById,
      units: this.units,
      buildings: this.mapConfig.buildings,
      cellSize: this.mapConfig.cellSize,
      isFriendlyOwner: (o) => this.isFriendlyOwnerForZone(o),
      isHostileOwner: (o) => this.isHostileToLocalPlayer(o),
    })
  }
```

Note: confirm `ENEMY_PLAYER_ID` / `NEUTRAL_PLAYER_ID` are imported in `GameState.ts` (they are used by `ownersAreHostile`). If not present in the import list, add them to the existing `from '../network/protocol'` import.

- [ ] **Step 2: Add the field to `GameUiSnapshot` + `getUiSnapshot()`**

In `GameClient.ts`, add to the `GameUiSnapshot` type (after the `objectives:` field, ~line 57):

```ts
  /** Zone-capture requirement cards (zones my team occupies but doesn't own).
   *  Empty when none qualify. Drives ZoneCapturePanel. */
  zoneCaptureCards: import('../zones/zoneCaptureCards').ZoneCaptureCard[]
```

And in `getUiSnapshot()` (after `objectives: this.state.getObjectives(),`, ~line 279):

```ts
      zoneCaptureCards: this.state.getZoneCaptureCards(),
```

- [ ] **Step 3: Add to `emptyUiSnapshot`**

In `useGameClient.ts`, find the `emptyUiSnapshot` object (it has `objectives: [],` ~line 43) and add right after it:

```ts
  zoneCaptureCards: [],
```

- [ ] **Step 4: Typecheck**

Run: `cd "c:/Personal Dev/webrts/client/src/game-portal" && npx vue-tsc -b`
Expected: only the two PRE-EXISTING errors (`useGameClient.ts:9`, `GameClient.ts:323`). No new errors mentioning zoneCaptureCards / GameState / GameClient.

- [ ] **Step 5: Commit**

```bash
git add client/src/game-portal/src/game/core/GameState.ts client/src/game-portal/src/game/core/GameClient.ts client/src/game-portal/src/composables/useGameClient.ts
git commit -m "feat(hud): expose zoneCaptureCards on the UI snapshot"
```

---

## Task 3: `ZoneCapturePanel.vue` component

**Files:**
- Create: `client/src/game-portal/src/components/match/ZoneCapturePanel.vue`

- [ ] **Step 1: Create the component**

Mirror `MatchObjectivesPanel.vue` styling (parchment/sepia). Create `ZoneCapturePanel.vue`:

```vue
<template>
  <div v-if="cards.length" class="zone-capture" role="status" aria-live="polite">
    <div class="zone-capture__header">Capturing</div>
    <ul class="zone-capture__list">
      <li
        v-for="card in cards"
        :key="card.id"
        class="zone-card"
        :class="`zone-card--${card.state}`"
        :style="card.ownerColor ? { borderLeftColor: card.ownerColor } : undefined"
      >
        <div class="zone-card__name">{{ card.name }}</div>
        <div class="zone-card__req">{{ card.requirement }}</div>
        <div class="zone-card__status">{{ card.status }}</div>
        <div v-if="card.progress > 0" class="zone-card__bar">
          <div class="zone-card__bar-fill" :style="{ width: `${Math.round(card.progress * 100)}%` }" />
        </div>
      </li>
    </ul>
  </div>
</template>

<script setup lang="ts">
import type { ZoneCaptureCard } from '@/game/zones/zoneCaptureCards'

const props = defineProps<{ cards: ZoneCaptureCard[] }>()
void props
</script>

<style scoped>
.zone-capture {
  width: 280px;
  max-width: 90vw;
  pointer-events: auto;
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  color: #f4d27a;
  background: rgba(28, 18, 8, 0.78);
  border: 1px solid rgba(212, 168, 71, 0.45);
  border-radius: 4px;
  padding: 10px 12px;
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.55), 0 0 0 1px rgba(0, 0, 0, 0.25) inset;
}

.zone-capture__header {
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: #d7bb84;
  margin-bottom: 6px;
}

.zone-capture__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.zone-card {
  border-left: 3px solid rgba(212, 168, 71, 0.55);
  padding-left: 8px;
  font-family: 'Trebuchet MS', 'Lucida Sans Unicode', system-ui, sans-serif;
  line-height: 1.3;
}

.zone-card__name {
  font-size: 13px;
  font-weight: 700;
  color: #f4d27a;
}

.zone-card__req {
  font-size: 12px;
  color: rgba(244, 210, 122, 0.85);
}

.zone-card__status {
  font-size: 12px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  color: rgba(244, 210, 122, 0.9);
}

.zone-card--contested .zone-card__status {
  color: #f0b070;
}

.zone-card--locked .zone-card__status {
  color: rgba(244, 210, 122, 0.55);
}

.zone-card__bar {
  margin-top: 3px;
  height: 5px;
  background: rgba(0, 0, 0, 0.45);
  border-radius: 3px;
  overflow: hidden;
}

.zone-card__bar-fill {
  height: 100%;
  background: rgba(96, 165, 250, 0.9);
}

.zone-card--contested .zone-card__bar-fill {
  background: rgba(251, 191, 36, 0.95);
}
</style>
```

- [ ] **Step 2: Typecheck**

Run: `cd "c:/Personal Dev/webrts/client/src/game-portal" && npx vue-tsc -b`
Expected: only the two pre-existing errors; nothing in `ZoneCapturePanel.vue`.

- [ ] **Step 3: Commit**

```bash
git add client/src/game-portal/src/components/match/ZoneCapturePanel.vue
git commit -m "feat(hud): ZoneCapturePanel component"
```

---

## Task 4: Mount the panel below objectives in Match.vue

**Files:**
- Modify: `client/src/game-portal/src/views/Match.vue`

- [ ] **Step 1: Import the component**

In `Match.vue`, add next to the existing `import MatchObjectivesPanel ...` (~line 183):

```ts
import ZoneCapturePanel from '@/components/match/ZoneCapturePanel.vue'
```

- [ ] **Step 2: Restructure the anchor to hold both panels**

Replace the existing objectives anchor block (~lines 22-27):

```html
    <div
      v-if="hasStarted && campaignSession && ui.objectives.length"
      class="match-objectives-anchor"
    >
      <MatchObjectivesPanel :objectives="ui.objectives" />
    </div>
```

with (shows the column if EITHER panel has content; zone panel is NOT gated on campaign):

```html
    <div
      v-if="hasStarted && ((campaignSession && ui.objectives.length) || ui.zoneCaptureCards.length)"
      class="match-objectives-anchor"
    >
      <MatchObjectivesPanel v-if="campaignSession && ui.objectives.length" :objectives="ui.objectives" />
      <ZoneCapturePanel v-if="ui.zoneCaptureCards.length" :cards="ui.zoneCaptureCards" />
    </div>
```

- [ ] **Step 3: Make the anchor a stacking column**

Replace the `.match-objectives-anchor` rule (~lines 769-775):

```css
.match-objectives-anchor {
  position: absolute;
  top: 70px;
  right: 16px;
  z-index: 15;
  pointer-events: none;
}
```

with:

```css
.match-objectives-anchor {
  position: absolute;
  top: 70px;
  right: 16px;
  z-index: 15;
  pointer-events: none;
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: flex-end;
}
```

- [ ] **Step 4: Typecheck + build**

Run: `cd "c:/Personal Dev/webrts/client/src/game-portal" && npx vue-tsc -b`
Expected: only the two pre-existing errors.

- [ ] **Step 5: Commit**

```bash
git add client/src/game-portal/src/views/Match.vue
git commit -m "feat(hud): mount ZoneCapturePanel below objectives"
```

---

## Task 5: Ghost tower at claim capture points (CanvasRenderer)

**Files:**
- Modify: `client/src/game-portal/src/game/rendering/CanvasRenderer.ts`

- [ ] **Step 1: Confirm the claim-slot block and sprite import**

Read the claim block in `drawZoneOverlay` (the `if (zone.capture?.type === 'claim' && !isAlly)` loop over `points`, ~lines 1888-1919) and confirm `getBuildingSprite` is imported at the top of the file (it is — used by `drawBuildings`). Confirm `this.state.mapConfig.buildings` is accessible.

- [ ] **Step 2: Add the ghost-tower draw inside the per-point loop**

The loop currently computes `const captured = snap.claimPoints?.[i]?.captured ?? false` and then draws the green (captured) or cyan (outstanding) outline. For the OUTSTANDING (`!captured`) branch, draw the ghost tower FIRST (so the cyan outline overlays it), but only when no building occupies the slot. Add this helper method to the class (near other private draw helpers):

```ts
  // True when any building footprint overlaps the 2x2 slot whose top-left is
  // (px, py). Used to suppress the ghost tower once a real tower is placed.
  private claimSlotHasBuilding(px: number, py: number): boolean {
    for (const b of this.state.mapConfig.buildings) {
      if (px < b.x + b.width && px + 2 > b.x && py < b.y + b.height && py + 2 > b.y) return true
    }
    return false
  }
```

Then, inside the `points.forEach((p, i) => { ... })` loop, in the `else` (outstanding) branch — BEFORE the existing cyan fill/stroke — insert:

```ts
            // Ghost tower: translucent preview of what to build here, until a
            // real building occupies the slot.
            if (!this.claimSlotHasBuilding(p[0], p[1])) {
              const towerType =
                (zone.capture?.config?.['towerType'] as string | undefined) ?? 'Tower'
              const ghost = getBuildingSprite(towerType)
              if (ghost) {
                ctx.save()
                ctx.globalAlpha = 0.35
                ctx.drawImage(ghost, sx, sy, slot, slot)
                ctx.restore()
              }
            }
```

(`sx`, `sy`, `slot`, and `zone` are already in scope in that loop from the existing code: `const sx = p[0] * cellSize`, `const sy = p[1] * cellSize`, `const slot = 2 * cellSize`.)

- [ ] **Step 3: Typecheck + build**

Run: `cd "c:/Personal Dev/webrts/client/src/game-portal" && npx vue-tsc -b`
Expected: only the two pre-existing errors. If `zone.capture?.config` is typed as opaque and the index access errors, cast via `(zone.capture?.config as Record<string, unknown> | undefined)?.['towerType']`.

Run: `cd "c:/Personal Dev/webrts/client/src/game-portal" && npm run build`
Expected: build halts ONLY on the two pre-existing `vue-tsc` errors (same as before this work); no new failures. If it fails on anything in CanvasRenderer/zoneCaptureCards/ZoneCapturePanel, that is in-scope and must be fixed.

- [ ] **Step 4: Commit**

```bash
git add client/src/game-portal/src/game/rendering/CanvasRenderer.ts
git commit -m "feat(render): ghost tower preview on un-built claim capture points"
```

---

## Task 6: Full verification

- [ ] **Step 1: Run the new unit tests + the existing FE suite**

Run: `cd "c:/Personal Dev/webrts/client/src/game-portal" && npx vitest run`
Expected: all tests PASS, including `zoneCaptureCards.test.ts`.

- [ ] **Step 2: Typecheck**

Run: `cd "c:/Personal Dev/webrts/client/src/game-portal" && npx vue-tsc -b`
Expected: only the two pre-existing errors (`useGameClient.ts:9`, `GameClient.ts:323`).

- [ ] **Step 3: Manual smoke (optional, recommended)**

Launch the app, load a map with a claim zone, move a unit into the zone: confirm (a) a "Capturing" card appears below objectives with "Build & defend N towers" + "0/N points held", (b) each un-built capture point shows a translucent tower, (c) building a tower removes that point's ghost, (d) capturing all points removes the card and ghosts.

---

## Notes for the implementer

- **No server/protocol changes.** Everything derives from the existing snapshot + map data. If you find yourself editing Go or `messages.go`, stop — that's out of scope.
- **Builder purity.** `buildZoneCaptureCards` must stay pure (no `GameState`, no Vue, no DOM) so its test stays fast and deterministic. All "is this owner friendly/hostile" logic enters via the two predicates.
- **Pre-existing errors.** `vue-tsc` already reports exactly two unrelated errors (`useGameClient.ts:9`, `GameClient.ts:323`). They are NOT yours; the bar is "no NEW errors." `npm run build` currently halts on them — that's the existing baseline.
- **Field-name consistency:** `ZoneCaptureCard` fields (`requirement`, `status`, `state`, `progress`, `ownerColor`) are used identically in Task 1 (builder), Task 3 (component), and the `GameUiSnapshot` type in Task 2.
