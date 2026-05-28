## Why

Legend Points already exist on the player profile and are partially wired (drop rolls on kill via `rollLegendPointDropLocked`, totals carried over via `LegendPointsEarned` in match-end protocol), but the currency has no sink — the Upgrades tab on the profile screen still reads "Coming Soon". Players accumulate a number that does not affect future runs, so the meta-progression loop is incomplete.

This change finishes the loop: enemies drop Legend Points at a defined rate, players spend them on persistent post-match upgrades that affect future runs, and the system is designed so additional upgrades can be added by dropping a new catalog entry without touching the engine.

## What Changes

- Enable kill-drop rate: set `legendPoints.perKillBaseDropChance = 0.05` in `gameplay_tuning.json` so every enemy has a 5% chance to drop 1 Legend Point on kill (the wiring already exists; only the tuning value changes).
- Introduce a generic **profile upgrade** system: catalog-driven definitions describing rank costs, max ranks, and a typed effect that is applied to a player at match start.
- Ship three initial upgrades using the new system:
  - `additional_worker` — +1 starting worker per rank, max 2 ranks, costs `[25, 100]`.
  - `physical_power` — +10% physical damage per rank, max 10 ranks, costs `[10, 20, 30, 40, 50, 60, 70, 80, 90, 100]` (total 550).
  - `magic_power` — +10% non-physical damage per rank, max 10 ranks, same cost curve as `physical_power`.
- Persist owned ranks on `PlayerProfile` and expose them via `GET /api/profile`.
- Add HTTP endpoints to purchase a rank, refund a rank (full cost refund), and list the upgrade catalog.
- Apply purchased ranks server-side at match start: extra workers spawn alongside the authored starting units; damage multipliers feed into the damage pipeline scoped by `DamageType` (physical vs. non-physical).
- Replace the "Coming Soon" Upgrades tab on `ProfileView.vue` with a real catalog UI that lists each upgrade, shows current rank / next-rank cost / refund value, and calls the new endpoints.

## Capabilities

### New Capabilities
- `profile-upgrades`: catalog-driven, generically extensible persistent post-match upgrade purchases (definitions, ranks, costs, effects, purchase / refund, and runtime application at match start).

### Modified Capabilities
<!-- No existing capability specs in openspec/specs/ for legend-point earning or profile state today — the kill-drop wiring lives in code/tests but not in a versioned spec, so no delta is needed. The drop-rate tuning change is captured under `profile-upgrades` as the funding side of the loop. -->

## Impact

- **Server (Go):**
  - `server/internal/game/catalog/tuning/gameplay_tuning.json` — set `perKillBaseDropChance` to `0.05`.
  - `server/internal/profile/types.go` — add `OwnedUpgradeRanks map[string]int` to `PlayerProfile`; bump `CurrentVersion` and add a forward migration that defaults the new field to `{}`.
  - `server/internal/game/catalog/profile-upgrades/*.json` + new loader (`profile_upgrade_defs.go`) — generic catalog of `ProfileUpgradeDef`s with typed effects (`extraStartingUnit`, `damageMultiplierByType`, extensible).
  - `server/internal/http/profile_handlers.go` — three new routes (`POST /api/profile/upgrades/purchase`, `POST /api/profile/upgrades/refund`, `GET /api/catalog/profile-upgrades`); existing `GET /api/profile` payload extended with owned ranks and catalog.
  - `server/internal/game/state.go` / match bootstrap — read profile upgrade ranks on match join and stash them on `Player`; apply at match start (spawn extra workers, store per-player damage-type multipliers).
  - `server/internal/game/damage_pipeline.go` — multiply outgoing damage by the player's per-type multiplier (physical vs. non-physical resolved from `DamageType.OrPhysical()`).
- **Client (Vue/TS):**
  - `client/src/game-portal/src/views/ProfileView.vue` — replace placeholder with a real Upgrades panel.
  - `client/src/game-portal/src/types/profile.ts` — extend `PlayerProfile` type.
  - new composable `useProfileUpgrades.ts` and component `ProfileUpgradesPanel.vue` for catalog rendering + purchase/refund actions.
- **No invariant changes:** all targeting/ID rules in `AI_RULES.md` are untouched; this change adds persistent profile state and a multiplier read in the damage pipeline, not new pointer fields or tick-loop work.
