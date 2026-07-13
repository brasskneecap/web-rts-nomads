# Shared In-Game HUD — Design Spec

**World editor + match convergence.** Give the world editor's playtest FULL
parity with a real match's player UI (shop, vault, upgrades, settings, match
menu, items bar, wave-upgrade modal, debug/battle panels, plus the core HUD),
by extracting `Match.vue`'s in-match HUD into a single shared component that
BOTH `Match.vue` and the playtest render. Parity becomes guaranteed by
construction, and future HUD changes update both places automatically. The
playtest stays ephemeral — no progress/rewards persist.

**Branch:** `unit-types-editor` (current world-editor work line).

**Supersedes:** the "core gameplay HUD" scope of
`2026-07-12-playtest-ingame-hud-design.md`. `PlaytestHud.vue` (the partial copy)
is removed and replaced by the shared component.

---

## 1. Goal

The playtest presents every option a real player has, and the map runs as a real
match:

- Selection panel (unit stats, commands, items/consumables) + resource/wave HUD
  + minimap frame + commander abilities (already present).
- **New:** match menu (shop / upgrades / vault / craft), items bar, settings
  modal, wave-upgrade modal, battle-tracker, debug-spawn, debug HUD, loot-drop
  tooltip, pause banner.
- Waves run (the ephemeral match simulates normally).
- **No progress persists** — the ephemeral match already suppresses the three
  profile-writing hooks; every new surface is match-local (verified §5).

### Non-goals

- **No route/lifecycle in the playtest.** Resume prompt, disconnect overlay,
  campaign-victory → `/match-end`, the status preflight/redirect, and the
  `currentMatchId → router.replace` watcher remain in `Match.vue` and never run
  in the editor.
- **No gameplay behavior change** to the real match. The extraction is
  presentation-only; the live game must behave identically.

---

## 2. Architecture

### 2.1 The decision: extract, don't copy

Two options were weighed:
- **(A) Keep copying** Match.vue's wiring into `PlaytestHud`. Rejected — it
  duplicates ~13 components' props/emits + local state and drifts silently
  (the copy already re-implements `onCommanderCast` verbatim; missing shop/
  vault/settings is the drift that motivated this spec).
- **(B) Extract a shared `InGameHud.vue`** that both `Match.vue` and the
  playtest render. **Chosen.** Parity is guaranteed by construction; one future
  HUD edit updates both. Cost: it touches `Match.vue`, so the live match is
  re-verified.

### 2.2 `components/InGameHud.vue` (new)

Owns the in-match HUD and nothing route-coupled.

- **Prop:** `hud: ReturnType<typeof import('@/composables/useGameClient').useGameClient>`
  (the `useGameClient` handle). Reads `hud.ui.value` (`GameUiSnapshot`) and calls
  its command forwarders.
- **Renders** (the full set, moved verbatim from `Match.vue`'s template):
  `MatchHud`, `SelectionHud`, `MatchMenuLauncher`, `MatchMenu`,
  `MatchSettingsModal`, `ItemsBar`, `WaveUpgradeModal`, `BattleTrackerPanel`,
  `DebugSpawnPanel`, `DebugHud`, `LootDropTooltip`, the pause banner, and the
  objective/zone panels (`MatchObjectivesPanel`, `ZoneCapturePanel`,
  `ZoneInspectionPanel`).
- **Owns local UI state:** `matchMenuOpen`, `matchMenuTab`, `itemsBarVisible`,
  `matchSettingsOpen`, `debugHudVisible`, and the ui-derived computeds
  (`debugSpawnTargetingActive`, `pausedByLabel`). Owns the menu/ESC/F3 keydown
  listeners (they only touch this local UI state) — registered on mount, removed
  on unmount.
- **Owns pure handlers:** `onCommanderCast` (toggle over
  `beginCommanderAbility`/`cancelCommanderAbility`), `onItemUse` (toggle over
  `beginItemUse`/`cancelItemUse`), `openMenuTab`, and inline toggles. All command
  emits map to `hud.*` forwarders exactly as `Match.vue` maps them today.
- **Emits:** exactly one — **`exit`**. Raised by the settings modal's
  `exit-game`. It is the only HUD action that leaves the match; the parent
  decides what "leave" means.
- **Objectives gate simplifies** to `ui.objectives.length` (drop the
  `campaignSession` gate). In a normal non-campaign match `ui.objectives` is
  empty so nothing shows; in a campaign it shows; in the playtest it shows if
  the map defines objectives. One rule, correct for all three.
- **No literal `cursor:`** declarations; **no** route/router/campaignSession/
  matchEndState imports (those stay in `Match.vue`).

### 2.2a Layout / stacking (critical — must not change real-match layout)

The HUD child components position themselves with `position: absolute/fixed`
relative to their host container (`.match-stage`, `position: relative`). To keep
the real match's layout **byte-identical**, `InGameHud` must NOT introduce a
positioned/stacking wrapper that becomes a new containing block. Use a fragment
root (multiple root nodes / no wrapper `div`) — Vue 3 supports this — OR a
wrapper with `display: contents` so the children participate in the host's
layout exactly as they do inline in `Match.vue` today.

Consequently, the **host** provides any needed stacking context:
- `Match.vue`: `.match-stage` is the context, unchanged.
- Playtest: the editor's play area must place `InGameHud` above the opaque
  `.we-play-canvas` (`z-index: 25`). Preserve the fix already shipped for
  `PlaytestHud` — wrap `<InGameHud>` in the editor in a positioned container with
  `z-index: 26` (and `pointer-events: none` + children `auto`, so canvas input
  still passes through gaps). This wrapper lives in `WorldEditorPanel.vue`, not
  in the shared component.

### 2.3 `views/Match.vue` (modified — keeps the shell)

Removes the inline HUD block (moved to `InGameHud`) and its now-internal local
UI state/handlers/listeners. Keeps and continues to own:

- `.match-view`/`.match-stage` wrapper + `<canvas ref="canvas">`.
- `startGame`/`init`/`destroy`/`leaveStoredMatch` bootstrap + localStorage
  session keys.
- Resume prompt (template + `showResumePrompt`/`resumeMapName`/
  `returnToPreviousGame`/`startNewGame`).
- Disconnect overlay (reads composable `connectionState` etc.; `retryReconnect`
  via `hud`, `requestForfeit` local).
- Campaign-victory modal + `onCampaignVictoryExit` → `transitionToMatchEnd`.
- `onMounted` preflight + redirects; `currentMatchId → router.replace` watcher;
  `endOfMatchOutcome` → `transitionToMatchEnd` → `router.push('/match-end')`;
  campaign watchers + `markCampaignLevelComplete`.

Renders: `<InGameHud :hud="gameClientApi" @exit="requestForfeit" />` inside
`.match-stage` beside the canvas, where the inline HUD used to be. (`gameClientApi`
is Match.vue's existing `useGameClient()` handle — Match.vue destructures
individual members today; it must pass the whole handle object to `InGameHud`.
Match.vue keeps its own destructured members for the shell logic it retains.)

### 2.4 Playtest (modified)

- `components/world-editor/WorldEditorPanel.vue`: replace
  `<PlaytestHud :hud="playtestGameClient" />` with
  `<InGameHud :hud="playtestGameClient" @exit="stopPlaytest" />`.
- **Delete** `components/world-editor/PlaytestHud.vue` + `PlaytestHud.test.ts`
  (superseded).
- Keep the slim `PlaytestBar` (Pause/Reset) as the editor's quick control. `Reset`
  and the in-HUD settings "exit" both route to `stopPlaytest` — consistent.
- `usePlaytest` is unchanged (still returns `gameClient`; still ephemeral).

---

## 3. Emit surface (the entire cross-boundary contract)

`InGameHud` exposes one emit:

| Emit | Raised when | `Match.vue` handler | Playtest handler |
|---|---|---|---|
| `exit` | settings modal "exit game" | `requestForfeit()` (→ `/match-end`) | `stopPlaytest()` (→ editor) |

Everything else is internal: `retryReconnect` (disconnect overlay) stays in the
Match.vue shell; campaign-victory `exit` stays in the shell; the victory
`continue` is pure local UI in the shell. `InGameHud` never imports the router.

---

## 4. Data flow

Identical to today's match, now via the shared component:
1. Parent owns a `useGameClient()` handle (`Match.vue`'s real match, or
   `usePlaytest`'s ephemeral one) and passes it as `:hud`.
2. The composable's rAF loop refreshes `ui`; `InGameHud` reads it and renders.
3. HUD command emits route through `hud.*` forwarders to the server.
4. `exit` is the only emit to the parent; the parent navigates (match) or tears
   down (playtest).

---

## 5. Ephemeral reward-suppression (unchanged, verified for new surfaces)

Adding shop/vault/upgrades/craft/wave-upgrades introduces **no** new persistence
risk:

- The `internal/game` package does not import `internal/profile`; it exposes
  exactly three profile-writing hooks, all gated on `match.State.Ephemeral`:
  immediate DP commit, `OnGameOver` DP commit, and `RecordKnownRecipe`.
- Shop purchase, reroll, recipe purchase, vault equip/unequip/transfer/use,
  in-match blacksmith upgrades, and wave upgrades are all pure `GameState`
  mutation — no profile write.
- The only new surface that touches a hook is **crafting** (→ `RecordKnownRecipe`),
  which is already suppressed by the ephemeral gate.

The ephemeral flag reaches the server end-to-end (`init({ephemeral:true})` →
`setEphemeral` → `join_match{ephemeral:true}` → `NewEphemeralMatch` →
`State.Ephemeral`). No change needed here; this section is the safety argument,
not new work.

---

## 6. Error handling & teardown

- `InGameHud` mounts/unmounts its keydown listeners cleanly (`onMounted` /
  `onBeforeUnmount`).
- In the playtest, Reset / editor-unmount call `usePlaytest.stop()` →
  `gameClient.destroy()` (unchanged), which tears down the client; `InGameHud`
  unmounts with `playtestPlaying=false`.
- `exit` from the playtest's settings modal calls `stopPlaytest` — same teardown
  path as Reset.

---

## 7. Testing

- **`InGameHud` unit test:** mount with a stub `hud` (ref `ui` + forwarder
  spies); assert the full component set renders and representative emits forward
  — shop `@purchase → sendPurchaseItem`, `@craft → craftItem`, ability
  `@cast → beginCommanderAbility`, settings `@exit` bubbles as the component's
  `exit` emit.
- **`usePlaytest` tests:** unchanged, stay green.
- **Full client suite green + `npm run build` clean** (the build gate is
  `vue-tsc -b`, which enforces `noUnusedLocals`).
- **Manual E2E — BOTH required before done:**
  1. **Real match** (`/match`): start a game, open shop/vault/upgrades, buy,
     craft, equip, settings, exit to results. Proves the extraction didn't break
     the live game.
  2. **Playtest**: Play in the editor → the full HUD works, waves run, exit via
     settings or Reset → editor; verify (server logs / profile) NO Dominion
     Points / recipes persisted.

The real-match E2E is a hard gate: not "done" until the live game is confirmed
working.

---

## 8. Global constraints

- Branch `unit-types-editor`.
- `InGameHud` is presentation + command-forwarding only; server-authoritative
  sim unchanged; no client-side gameplay logic.
- `InGameHud` imports NO router/route/campaignSession/matchEndState — the shell
  (`Match.vue`) owns all navigation/lifecycle; the sole cross-boundary contract
  is the `exit` emit.
- No literal `cursor:` declarations in new/changed component CSS except
  `cursor: not-allowed` on forbidden-action states.
- Ephemeral playtest invariant preserved: fresh match per Play, no reward
  persistence.
- The real match must behave identically post-refactor (presentation-only move).
- Client commands from `client/src/game-portal` (`npm run test`, `npm run build`).
