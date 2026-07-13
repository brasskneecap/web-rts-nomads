# Playtest In-Game HUD — Design Spec

**World editor sub-feature.** When the user hits **Play** in the world editor,
the playtest should present the full in-game player UI (unit selection + stats,
commands, resources, minimap, commander abilities, objectives) so giving orders
and inspecting units feels like a real match — not the current bare canvas +
Pause/Reset bar. Reuse the exact HUD components and client machinery the
`/match` route uses.

**Branch:** `unit-types-editor` (current world-editor work line).

---

## 1. Goal

During an ephemeral playtest, render the **core gameplay HUD** over the play
canvas and wire it to the live match so the player can:

- Select units (click, drag-box) and see selected-unit **stats** in the
  selection panel.
- Issue **commands** — move, attack, abilities, use consumables, equip items.
- See **resources / wave / notifications** and the **minimap**.
- See **objectives / zone-capture** state (the playtest already evaluates
  objectives).

Pause and Reset (the existing `PlaytestBar`) remain.

### Non-goals (v1)

- **Match-flow chrome:** the pause/settings menu (`MatchMenuLauncher` menu +
  `MatchSettingsModal` + `MatchMenu`), the wave-upgrade modal
  (`WaveUpgradeModal`), the victory modal (`CampaignVictoryModal`), and the
  disconnect overlay are NOT shown. They assume a real match lifecycle
  (navigation to `/match-end`, campaign session state, reconnect flow) that has
  no place in an editor sandbox.
- **No `Match.vue` reuse.** Its route bootstrap (status preflight that redirects
  to `/`), its `currentMatchId → router.replace('/match/:id')` watcher, and its
  end-of-match `router.push('/match-end')` must never run in the editor. We
  avoid them by mounting the HUD components directly, not the view.
- **Audio unchanged.** `/world-editor` is not flagged `silenceMusic`, so menu
  music keeps playing under the playtest. Left as-is in v1.
- **Game-over handling.** On an ephemeral game-over the sim halts and the user
  hits Reset; no victory modal, no navigation. Acceptable for v1.

---

## 2. Architecture

### 2.1 The one behavioral change: unify the client

Today `usePlaytest` constructs a **closure-local** `GameClient`
(`new GameClient(canvas, mapId)`), while every HUD component reads the
**module-level singleton** owned by `useGameClient()`
(`composables/useGameClient.ts`). That is the sole blocker: the HUD would render
an empty snapshot because it reads a different client than the playtest created.

**Fix:** route the playtest through the singleton.

- `usePlaytest.start(file)` → `useGameClient().init(playCanvas, mapId, { ephemeral: true })`
  instead of `new GameClient` + `client.start`. `init` already spreads
  `options` into `client.start(options)`, and `GameClient.start` calls
  `network.setEphemeral(!!options.ephemeral)`, so ephemeral flows through. The
  earlier ephemeral-join fix (omit `matchId` when ephemeral) lives in
  `NetworkClient`, so every Play still mints a **fresh** ephemeral match.
- `usePlaytest.stop()` → the composable's `destroy()`.
- `usePlaytest.togglePause()` → the composable's `sendSetPause(...)`; the paused
  state is authoritative from the snapshot (`ui.value.paused`).

Reusing the singleton also reuses, for free: the per-frame `requestAnimationFrame`
loop that refreshes the reactive `ui` snapshot from `client.getUiSnapshot()`, the
push-based connection-state watchers, and the ~30 command forwarders
(`performSelectionAction`, `selectUnitOnly`, `sendEquipItem`,
`beginCommanderAbility`, `setMinimapPanelRect`, …).

### 2.2 Input needs nothing

`InputManager` is constructed inside `GameClient` and binds all canvas listeners
(mousedown/move/up, contextmenu, dblclick, wheel) plus window hotkeys at
construction, to whatever canvas is passed. The play canvas already receives
full RTS input today; unifying the client changes nothing here.

---

## 3. Components

### 3.1 `PlaytestHud.vue` (new — `components/world-editor/`)

A self-contained overlay that calls `useGameClient()` and renders the core HUD
subset, fed by the composable's reactive `ui` and wired to its forwarders. It
mirrors the **subset** of `Match.vue`'s HUD template + emit-mapping — copied for
these components only, with none of `Match.vue`'s route logic.

Renders:

| Component | Data in | Commands out (→ composable forwarder) |
|---|---|---|
| `MatchHud` | `:ui` | — (read-only) |
| `SelectionHud` | `:ui` | `@action → performSelectionAction`, `@select-unit → selectUnitOnly`, `@deselect-unit`, `@use-consumable`, `@equip-item`, `@minimap-rect → setMinimapPanelRect` (exact set copied from `Match.vue`) |
| Minimap | `:ui` | as `Match.vue` wires it |
| Commander ability bar | `:ui.commanderAbilities` | `@cast-ability → beginCommanderAbility` / `cancelCommanderAbility` |
| `MatchObjectivesPanel` / `ZoneCapturePanel` / `ZoneInspectionPanel` | `ui.objectives` / `ui.zoneCaptureCards` / `ui.zoneInspection` | — |

The authoritative list of props/emits per component and their forwarder targets
is taken verbatim from `Match.vue`'s existing wiring for that component (the
plan enumerates them). `PlaytestHud` introduces **no** new client coupling —
like the HUD components themselves, all client access is centralized (here, via
`useGameClient()` forwarders).

### 3.2 `usePlaytest.ts` (modify)

Swap the client ownership as in §2.1. Preserve the reentry guard (`playing` +
synchronous `starting`), the try/catch error surfacing (`saveError`), and the
`onBeforeUnmount` teardown. `saveMapCatalogFile` before `init` is unchanged (the
map must exist server-side for `join_match`).

### 3.3 `WorldEditorPanel.vue` (modify)

When `playtestPlaying`, mount `<PlaytestHud />` over the play-canvas region
alongside the existing `<PlaytestBar>`. The play canvas remains the element
passed to `init`. No other editor behavior changes.

---

## 4. Data flow

1. **Play** → `usePlaytest.start(exportedCatalogFile)` → `saveMapCatalogFile` →
   `useGameClient().init(playCanvas, resolvedMapId, {ephemeral:true})` → singleton
   `GameClient` starts, renders to the play canvas, binds input, joins a fresh
   ephemeral match.
2. The composable's rAF loop refreshes `ui` each frame from
   `client.getUiSnapshot()`.
3. `PlaytestHud` reads `ui` (selection, selected-unit stats, resources,
   objectives, abilities) and renders the HUD.
4. Player input on the canvas (select/drag/right-click) is handled by
   `InputManager`; HUD-issued commands emit up and route through the composable
   forwarders back to the client.
5. **Pause** → `sendSetPause(true/false)` freezes/continues; `ui.paused` drives
   the button label. **Reset** → `destroy()` tears the match down; the editor
   canvas re-shows with placements intact.

---

## 5. Error handling & teardown

- `start` retains its try/catch: a failed `saveMapCatalogFile` or `init` tears
  down any partial client, leaves `playing=false`, and surfaces `saveError`.
- Reset, `onBeforeUnmount`, and any error path call the composable's `destroy()`
  so the module-level singleton is clean before any later navigation to a real
  match (no leaked rAF loop, socket, or stale `ui`).
- Because `PlaytestHud` mounts only pure HUD components (no `Match.vue`), no
  route navigation, preflight, or `/match-end` push can fire from the editor.

---

## 6. Testing

- **`usePlaytest`** (update existing tests): mock `useGameClient` instead of the
  raw `GameClient` ctor. Assert `start` calls `init` with `{ephemeral:true}`,
  `stop` calls `destroy`, `togglePause` calls `sendSetPause`, and the reentry
  guard (no double-init) still holds.
- **`PlaytestHud`**: shallow-mount with a stub `ui`; assert it renders the core
  HUD components and that a representative command emit (e.g. `SelectionHud`'s
  `action`) is forwarded to the matching `useGameClient` forwarder (mock the
  composable).
- **Manual E2E (the proof):** Play → HUD appears → click-select a unit → stats
  show in `SelectionHud` → right-click move/attack → cast a commander ability →
  Pause freezes → Reset returns to the editor with no lingering client (no
  console errors, no leaked socket).

---

## 7. Global constraints

- Branch `unit-types-editor`; do not modify `Match.vue` or the item/map editors.
  Reuse HUD components read-only (props + emits); centralize client access via
  `useGameClient()`.
- No literal `cursor:` declarations in new component CSS except
  `cursor: not-allowed` on forbidden-action states.
- The client is server-authoritative; the HUD only renders `ui` snapshots and
  forwards command intents — no client-side gameplay simulation.
- Ephemeral playtest invariant preserved: fresh match per Play (NetworkClient
  omits `matchId` when ephemeral), no reward persistence.
- Client commands from `client/src/game-portal` (`npm run test`, `npm run build`).
