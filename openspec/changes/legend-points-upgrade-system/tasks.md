## 1. Tuning: enable the 5% drop rate

- [x] 1.1 Edit `server/internal/game/catalog/tuning/gameplay_tuning.json`: set `legendPoints.perKillBaseDropChance = 0.05` and `legendPoints.perKillBaseAmount = 1`.
- [x] 1.2 Verify `tuning_defs.go` validation still passes (`perKillBaseDropChance` is in `[0,1]`).
- [x] 1.3 Add or extend a test in `server/internal/game/player_buffs_test.go` that confirms a kill with the seeded RNG below 0.05 awards exactly 1 to `Player.RunLegendPointDrops`, and that friendly fire does not.

## 2. Profile schema migration

- [x] 2.1 In `server/internal/profile/types.go`, add `OwnedUpgradeRanks map[string]int `json:"ownedUpgradeRanks"`` to `PlayerProfile`. Bump `CurrentVersion` from 1 to 2.
- [x] 2.2 In the profile store's read path, when loading a profile whose `Version < 2` (or where the map is nil), initialize `OwnedUpgradeRanks = map[string]int{}`. Persist as v2 on the next mutation.
- [x] 2.3 Update `server/internal/profile/store_test.go` (or add a new test) to load a hand-rolled v1 JSON fixture and assert that `OwnedUpgradeRanks` is non-nil and empty after read, and that the file is re-saved with `version: 2` after a subsequent `WithLocked` mutation.

## 3. Profile upgrade catalog (Go)

- [x] 3.1 Create `server/internal/game/catalog/profile-upgrades/additional_worker.json` with `maxRanks: 2`, `costPerRank: [25, 100]`, effect `{ "type": "extraStartingUnit", "unitType": "worker", "countPerRank": 1 }`.
- [x] 3.2 Create `server/internal/game/catalog/profile-upgrades/physical_power.json` with `maxRanks: 10`, `costPerRank: [10,20,30,40,50,60,70,80,90,100]`, effect `{ "type": "damageMultiplierByType", "damageTypeClass": "physical", "multiplierPerRank": 0.10 }`.
- [x] 3.3 Create `server/internal/game/catalog/profile-upgrades/magic_power.json` mirroring `physical_power.json` with `damageTypeClass: "nonPhysical"`.
- [x] 3.4 Create `server/internal/game/profile_upgrade_defs.go` with `ProfileUpgradeDef`, `ProfileUpgradeEffect` (discriminated by `Type`), an `//go:embed catalog/profile-upgrades/*.json` loader following the pattern in `upgrade_defs.go`, validation (panic on duplicate id, mismatched `costPerRank` length, unknown effect type, non-positive `maxRanks` or costs), and `getProfileUpgradeDef(id)` / `ListProfileUpgradeDefs()` accessors.
- [x] 3.5 Define a small `profileUpgradeEffectRegistry` keyed by effect type with two registered handlers: `extraStartingUnit` (validates `unitType` exists in unit defs, `countPerRank > 0`) and `damageMultiplierByType` (validates `damageTypeClass` is `"physical"` or `"nonPhysical"`, `multiplierPerRank > 0`).
- [x] 3.6 Add `server/internal/game/profile_upgrade_defs_test.go`: loads the three real catalog files, asserts IDs and cost arrays match the spec; uses table-driven cases to verify the loader panics on each malformed-catalog variant (duplicate id, bad cost length, unknown effect type).

## 4. HTTP endpoints

- [x] 4.1 In `server/internal/http/profile_handlers.go`, register `GET /api/catalog/profile-upgrades` that returns `{ "upgrades": game.ListProfileUpgradeDefs() }`.
- [x] 4.2 Extend the `GET /api/profile` handler to also return `profileUpgradeCatalog` in its response body.
- [x] 4.3 Add `POST /api/profile/upgrades/purchase`: parses `{ upgradeId }`, looks up the def, validates rank < max and `LegendPoints >= costPerRank[currentRank]`, debits, increments, returns updated profile. Error codes per spec: `unknown_upgrade`, `max_rank_reached`, `insufficient_legend_points`.
- [x] 4.4 Add `POST /api/profile/upgrades/refund`: parses `{ upgradeId }`, validates rank >= 1, refunds `costPerRank[currentRank - 1]` to `LegendPoints` (not `LifetimeLegendPoints`), decrements. Error codes: `unknown_upgrade`, `not_owned`.
- [x] 4.5 Add HTTP tests covering: successful purchase debits exact cost; insufficient-points rejection does not mutate the profile; max-rank rejection; unknown-id rejection; refund returns exact cost of last rank; refund of rank-0 rejected; refund does not change `LifetimeLegendPoints`.

## 5. Match-start application

- [x] 5.1 Add fields to `Player` in `server/internal/game/state.go`: `ProfileUpgrades map[string]int`, `PhysicalDamageMultiplier float64`, `MagicDamageMultiplier float64`, `ExtraStartingWorkers int`. Default the two multipliers to `1.0` in the player-construction path.
- [x] 5.2 When a player joins a match, copy `PlayerProfile.OwnedUpgradeRanks` onto `Player.ProfileUpgrades`. Walk the player's owned ranks in sorted ID order; for each, call the effect handler registered in step 3.5 to update the precomputed fields.
- [x] 5.3 In `state_spawn.go`'s `spawnPlacedUnitsForPlayerLocked` (or a new sibling called immediately after it), after the authored loop completes, spawn `ExtraStartingWorkers` additional `"worker"` units near the player's claimed townhall using `findNearestWalkable` against the current blocked-cell set. Log a warning and skip when no walkable cell is found.
- [x] 5.4 Add tests in `server/internal/game/`: a player with `additional_worker` rank 2 starts with `(authoredCount + 2)` worker units; a player with no ranks starts with `authoredCount`; multipliers default to 1.0.

## 6. Damage pipeline integration

- [x] 6.1 In `server/internal/game/damage_pipeline.go`, at the point where damage type has been resolved via `DamageType.OrPhysical()`, multiply the outgoing damage by the owner's `PhysicalDamageMultiplier` when the resolved type is `DamagePhysical`, and by `MagicDamageMultiplier` otherwise. Resolve the owner via `s.Players[attackerOwnerID]`; if the attacker has no owner entry (neutral / enemy AI), skip the multiplication.
- [x] 6.2 Add a test that constructs two attackers (one player with rank-3 `physical_power`, one enemy AI) and verifies the player's physical attack is multiplied by 1.30 while the enemy attack and the player's magic attack are unmodified. Use the existing damage-event capture helpers in `damage_type_test.go` as a reference.

## 7. Client: types and composable

- [x] 7.1 In `client/src/game-portal/src/types/profile.ts`, add `ownedUpgradeRanks: Record<string, number>` to `PlayerProfile`. Add new types `ProfileUpgradeDef` and `ProfileUpgradeEffect` mirroring the server schema.
- [x] 7.2 Create `client/src/game-portal/src/composables/useProfileUpgrades.ts` exposing `catalog`, `ownedRanks`, `legendPoints`, `purchase(upgradeId)`, `refund(upgradeId)` — backed by the new endpoints and re-binding the `useProfile` profile ref on each response.

## 8. Client: Upgrades panel UI

- [x] 8.1 Create `client/src/game-portal/src/components/profile/ProfileUpgradesPanel.vue` that lists every catalog entry as a card with: name, description, `currentRank / maxRanks`, next-rank cost (or "Maxed"), Buy and Refund buttons. Buttons call the composable's `purchase` / `refund` and disable while the request is in flight.
- [x] 8.2 In `client/src/game-portal/src/views/ProfileView.vue`, remove the "Coming Soon" placeholder under `activeTab === 'upgrades'` and mount `<ProfileUpgradesPanel />` in its place.
- [ ] 8.3 Manually verify against `npm run dev` that all three upgrades render, Buy reduces the Legend Points header value by the exact cost, Refund restores it, the rank counter advances, and the Buy button is hidden / disabled at max rank.

## 9. End-to-end verification

- [ ] 9.1 Run `go test ./...` from the `server/` directory and confirm all new and existing tests pass.
- [x] 9.2 Run `npm run typecheck` (or the equivalent) from the client portal directory and confirm no type errors.
- [ ] 9.3 Play a short match locally and confirm: enemy kills occasionally show as Legend Point gains in the post-match summary; purchasing `additional_worker` rank 1 makes the next match start with one extra worker; rank-3 `physical_power` visibly increases physical-attacker damage; refund restores the displayed Legend Point total exactly.
