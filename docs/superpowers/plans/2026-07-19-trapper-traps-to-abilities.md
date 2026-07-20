# Trapper Traps → Abilities Migration Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`).

**Goal:** Turn the Trapper's four traps from auto-firing bronze PERKS into pool-acquired ABILITIES (like the Arch Mage's spells), placed on the nearest enemy via autocast (today's behavior), reusing the entire existing trap runtime. Silver/Gold perks stay perks and keep modifying the trap.

**Architecture:** A new `place_trap` ability action (following the `summon_unit` precedent) calls the shipped `plantOneTrapLocked`, so all Silver/Gold upgrades + client rendering are preserved. Acquisition uses the `abilityPoolsByRank` mechanism already on the PathDef. Trap stats move into each trap's AbilityDef. The 4 bronze trap perks are deleted; the 4 trap-specific Silver perks switch from `requiresPerk` to a new `requiresAbility` gate.

**Tech Stack:** Go (server, `internal/game`), Vue 3 + TS (client tooltips). Gate backend on `go build ./... && go vet ./... && go test ./internal/game/`.

**Behavior invariant:** A Trapper with a given trap + rank + Silver/Gold perks must place an identical trap (same stats, same placement, same upgrades) after the migration. Characterization tests pin this.

---

## Reference facts (verified)

- `plantTrapLocked(unit, def *PerkDef)` → `plantOneTrapLocked(unit, def, bonusIndex)` — [trap.go:943,969](server/internal/game/trap.go#L969). Reads `trapType := def.ID` and `cfg := def.ConfigForRank(unit.Rank)`, then a `switch trapType` snapshots per-type fields (`radius`/`explosionRadius`/`triggerRadius`/`damagePerSecond`/`slowMultiplier`/`durationSeconds`/`burstDamage`/`markMultiplier`/`markDuration`). Position = `unit.X/Y + trapPlacementOffsetLocked(...)` (thrown toward nearest enemy; falls back to unit pos).
- Placement driver today: `tickTrapPlacementLocked(unit, def, dt)` — [trap.go:1887](server/internal/game/trap.go#L1887), dispatched from the perk tick at [perks.go:1225](server/internal/game/perks.go#L1225) (`case "caltrops","fire_pit","explosive_trap","marker_trap"`). Gate = `trapperHasHostileInRangeLocked`; cooldown = `placeIntervalSeconds * mods.CooldownMultiplier`. **No mana.**
- Effective-trap stats (drives client tooltips + the wire `EffectiveTrapSnapshot.perkId`): `DebugEffectiveTrapStats` / `EffectiveTrapSnapshotLocked` — [perks_trapper.go:383,353](server/internal/game/perks_trapper.go#L383). Resolves the trap by scanning `unit.PerkIDs` for a bronze trap id, reads `perkDefByID(id).ConfigForRank(unit.Rank)`.
- Silver/Gold modifiers: `trapModifiersForUnitLocked` + `trapSpecificModifiersForUnitLocked(unit, trapType)` — scan `unit.PerkIDs`, gate on `trapType` string. **These keep working unchanged** (ability id == trapType).
- `requiresPerk` gate (acquisition pool filter): `eligiblePerksAfterFiltersLocked` — [perks.go:1053](server/internal/game/perks.go#L1053). The 4 trap-specific Silver perks (`barbed_field`→caltrops, `explosive_chain`→explosive_trap, `exposed_weakness`→marker_trap, `lasting_flames`→fire_pit) use it.
- Only `fire_pit` has `configByRank` (dps 16/28/45, radius 55/75/95). The other three are flat.
- `place_trap` precedent: `summon_unit` action — [ability_exec_actions.go:185](server/internal/game/ability_exec_actions.go#L185) (decodes typed config, calls an existing Go seam). Autocast selector `closest_enemy_in_range` exists.
- Ability pool acquisition: `abilityPoolsByRank` on the PathDef (built this session) + `abilityPoolFor`/`rollUnitPoolAbilityForRankLocked`.

Run Go commands from `server/`. **No `git commit`/`add`/`rm`/`mv`** — the human stages.

---

## PHASE 1 — Engine: decouple the plant primitive + add `place_trap` (behavior-preserving)

### Task 1.1: Characterization test — pin a placed trap's stats for each type

**Files:** Test: `server/internal/game/trap_migration_test.go` (create)

- [ ] **Step 1:** Write a test that, for each of the 4 trap types (as bronze perks, current path), spawns a Trapper at bronze/silver/gold, forces a plant, and records the resulting `*Trap`'s key fields (TrapType, Radius, TriggerRadius, DamagePerSecond, SlowMultiplier, BurstDamage, MarkMultiplier, MarkDuration, RemainingSeconds). Assert against the known values (derive from the trap JSON configs — e.g. explosive_trap: Radius=100, TriggerRadius=50, BurstDamage=75; fire_pit silver: DamagePerSecond=28, Radius=75). Use the existing `trap_test.go` helpers for spawning a Trapper + planting. This is the invariant the whole migration must preserve.
- [ ] **Step 2:** Run → PASS on current code. `cd server && go test ./internal/game/ -run TestTrapCharacterization -v`. (If a value differs, capture actual + reconcile — don't change catalog.)
- [ ] **Step 3:** Commit (human).

### Task 1.2: Introduce `TrapConfig` and refactor the plant primitive to take it

**Files:** `trap.go`

- [ ] **Step 1:** Define a `TrapConfig` struct holding everything `plantOneTrapLocked` reads from `cfg`: `TrapType string`, `DurationSeconds`, `PlaceIntervalSeconds`, `Radius`, `ExplosionRadius`, `TriggerRadius`, `DamagePerSecond`, `SlowMultiplier`, `BurstDamage`, `MarkMultiplier`, `MarkDuration float64` (int for BurstDamage where the code ints it). Add a builder `trapConfigFromPerkLocked(def *PerkDef, rank string) TrapConfig` that reads `def.ID` + `def.ConfigForRank(rank)` into the struct (exactly the fields the current switch reads).
- [ ] **Step 2:** Change `plantOneTrapLocked` (and `plantTrapLocked`) to take `cfg TrapConfig` instead of `def *PerkDef`. Replace `trapType := def.ID` → `cfg.TrapType`, and every `cfg["..."]` (the map) → the struct field. The Silver/Gold modifier calls (`trapModifiersForUnitLocked(unit)`, `trapSpecificModifiersForUnitLocked(unit, trapType)`) still take `unit` + the trapType string — unchanged. `increased_deployment` check (`containsString(unit.PerkIDs, ...)`) unchanged.
- [ ] **Step 3:** Update the caller `tickTrapPlacementLocked` ([trap.go:1913](server/internal/game/trap.go#L1913)) to build `TrapConfig` via `trapConfigFromPerkLocked(def, unit.Rank)` and pass it. Behavior identical.
- [ ] **Step 4:** `cd server && go build ./... && go test ./internal/game/ -run 'TestTrap' -v` → PASS. The characterization test (1.1) MUST stay green (proves the refactor is behavior-neutral). Then full `go test ./internal/game/` → PASS.
- [ ] **Step 5:** Commit (human).

### Task 1.3: Add the `place_trap` action

**Files:** `ability_program.go` (enum), a new `ability_exec_place_trap.go` (or add to `ability_exec_actions.go`), test.

- [ ] **Step 1:** Add `ActionPlaceTrap ActionType = "place_trap"` to the action enum + `allActionTypes`. Define a `placeTrapConfig` decoded from the action `config`: the trap params (a `TrapConfig` minus TrapType-from-elsewhere, PLUS `trapType string` and an optional `configByRank map[string]<partial>` for fire_pit-style rank scaling). Follow the `summon_unit` config-decode pattern exactly.
- [ ] **Step 2:** Register the action (`registerAction`) with an `Execute` that: resolves the caster + the cast target position (the autocast/selected target — reuse `resolveContextPositionLocked` or the ability ctx target), builds a `TrapConfig` from the action config (applying the caster's rank via the configByRank override), and calls `plantTrapLocked(caster, cfg)` — the primitive refactored in 1.2. Position: the trap should land on/toward the target, matching today; if the plant primitive still computes its own throw-offset from nearest enemy, keep that (the autocast target IS the nearest enemy, so it's consistent) — OR pass the resolved target position into the primitive. Decide during impl to match the characterization of placement; document the choice.
- [ ] **Step 3:** Unit-test the action in isolation: a synthetic ability with a `place_trap` action, cast by a unit, produces a `*Trap` on `s.Traps` with the configured stats. `cd server && go test ./internal/game/ -run TestPlaceTrapAction -v` → PASS.
- [ ] **Step 4:** `go build ./... && go vet ./... && go test ./internal/game/` → PASS + clean.
- [ ] **Step 5:** Commit (human).

**End of Phase 1: the trap runtime is drivable by an ability, with zero change to the existing perk-driven behavior. Traps are still perks. Phase 2 flips acquisition.**

---

## PHASE 2 — Cutover: traps become pool abilities, perks deleted (OUTLINE — detail after Phase 1 lands, since the `place_trap` config shape informs it)

Tasks (each with tests, behavior pinned by the 1.1 characterization test adapted to the ability path):

- **2.1 — `requiresAbility` gate.** Add `PerkDef.RequiresAbility string` (json `requiresAbility`); enforce in `eligiblePerksAfterFiltersLocked` alongside `requiresPerk` (perk offered only if `containsString(unit.Abilities, requiresAbility)`). Convert the 4 trap-specific Silver perks: replace `requiresPerk: "<trap>"` → `requiresAbility: "<trap>"`.
- **2.2 — Author the 4 trap abilities** in `catalog/abilities/<trap>/<trap>.json`: `type: spell`, `no_target`/self entry or point entry, `castTime: 0`, `manaCost: 0`, `cooldown: <placeInterval>`, `supportsAutoCast: true`, `defaultAutoCast: true`, `autoCastTargetSelector: closest_enemy_in_range`, `icon: <existing perk icon>`, program `on_cast_complete → place_trap{ trapType, <stats>, configByRank(fire_pit) }`. Move the stats out of the (soon-deleted) perk JSONs into these.
- **2.3 — Trapper pool + kit.** Add `abilityPoolsByRank: { bronze: ["caltrops","fire_pit","explosive_trap","marker_trap"] }` to the trapper path JSON. (Bronze is now an Ability slot.)
- **2.4 — Repoint effective-trap resolution.** `DebugEffectiveTrapStats` resolves the trap from the unit's owned trap ABILITY (scan `unit.Abilities` for one of the 4 ids) and reads the trap config from the ability def's `place_trap` config (not `perkDefByID`). `effectiveTrap.perkId` becomes the ability id (or rename the wire field to `trapId` in Phase 3).
- **2.5 — Delete the 4 bronze trap perks** (`catalog/perks/trapper/{caltrops,fire_pit,explosive_trap,marker_trap}/`) and remove the perk-tick placement dispatch ([perks.go:1225](server/internal/game/perks.go#L1225)) + the now-unused `tickTrapPlacementLocked` / `trapConfigFromPerkLocked` perk path. `DebugEffectiveTrapStats`'s bronze-perk scan is replaced by 2.4.
- **2.6 — Update tests** (~80 sites across `trap_test.go`, `trap_silver_test.go`, `perk_tooltip_test.go`, `perksbyrank_migration_test.go`, `advancement_archer_master_huntsman_test.go`): a Trapper now gets its trap via the ability pool roll, not a bronze perk. Repoint spawn helpers; keep the characterization + Silver/Gold upgrade invariants.

## PHASE 3 — Client / tooltips (OUTLINE)

- Repoint the client trap tooltip keying (`effectiveTrap.perkId` / the `{trap.*}` template tokens) to the ability. Optionally rename the wire field `perkId` → `trapId`. Verify `vue-tsc -b` + trap-related vitest.

---

## Self-Review (Phase 1 focus)

- The 1.1 characterization test pins placed-trap stats for all 4 types at all ranks BEFORE any refactor, and stays green through 1.2 (plant refactor) and after 1.3.
- `TrapConfig` carries every field the plant switch reads; `trapConfigFromPerkLocked` reproduces `ConfigForRank` reads exactly (fire_pit rank scaling preserved).
- Silver/Gold modifier calls + `increased_deployment` are untouched in Phase 1 (still key off `unit` + trapType).
- `place_trap` follows the `summon_unit` seam; the action is unit-tested producing a real `*Trap`.
- Phases 2–3 are outlined (not fully granular) because the `place_trap` config shape from 1.3 determines their exact form — they get detailed once Phase 1 lands.
