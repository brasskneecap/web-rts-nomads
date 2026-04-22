# AI Rules for webrts

These rules apply to all AI-assisted code changes in this repository. Follow them unless a specific task explicitly overrides them.

## Project Context

Authoritative Go backend server + TypeScript/Vue 3 frontend client. The server owns all game state and simulation; the client sends command intents and renders server-provided state. Simulation is tick-based and runs under a single lock (`*Locked` method suffix indicates "caller holds the state lock").

## Target References

Combat, projectiles, threat tables, and AI state all reference other units/buildings by **ID**, never by long-lived pointer. This invariant already holds across the codebase and must be preserved.

Concrete identifier types:
- Units: `unit.ID` is `int` (stored on other structs as `AttackTargetID int`, `TauntedByUnitID int`, `TargetUnitID int`, `OwnerUnitID int`, etc.).
- Buildings: `building.ID` is `string` (stored as `AttackBuildingTargetID string`).

### The rules

1. **Store targets by ID, not by pointer.** Fields on `Unit`, `Projectile`, `ThreatEntry`, AI scoring contexts, perk state, and any other struct that outlives a single tick store the ID, not a `*Unit` / `*BuildingTile`.

2. **Resolve at point-of-use, every tick.** Call `s.getUnitByIDLocked(id)` (or the building equivalent) each tick the target is needed. Do not cache a resolved pointer in a field that survives past the current tick.

3. **Every resolution must be validated before use.** A lookup can return `nil` (unit was removed) or return a unit that is dead / invisible / on the wrong team. Callers must handle this explicitly. The canonical guard is:
   ```go
   target := s.getUnitByIDLocked(unit.AttackTargetID)
   if target == nil || !target.Visible || target.HP <= 0 || target.OwnerID == unit.OwnerID {
       // drop the target, fall back to resume/idle behavior
   }
   ```
   See [combat_ai_scoring.go:9-13](../../server/internal/game/combat_ai_scoring.go#L9-L13), [combat_ai.go:142-152](../../server/internal/game/combat_ai.go#L142-L152), [combat_ai_retreat.go:59-67](../../server/internal/game/combat_ai_retreat.go#L59-L67).

4. **Within-tick `*Unit` parameters are fine and preferred.** Once a target has been resolved and validated at the top of a tick-local code path, passing the `*Unit` down into helper functions (`scoreUnitTargetLocked(unit, target *Unit, ...)`, `refreshUnitAttackApproachLocked(unit, target *Unit, ...)`, etc.) is the correct pattern. Do **not** force helpers to re-resolve — that adds redundant map lookups and masks which call sites are responsible for validation.

5. **Never persist a resolved `*Unit` across tick boundaries.** The test is "does this struct live into the next tick?" If yes → store the ID. If no (it's a local variable, a parameter, a return value consumed in the same tick) → `*Unit` is fine.

6. **Sticky player orders: `ManualAttackTarget`.** Player-issued attack commands bypass the AI's retarget/leash/retreat logic as long as the target is still valid. See [combat_ai.go:150](../../server/internal/game/combat_ai.go#L150) and [combat_ai_scoring.go:17-19](../../server/internal/game/combat_ai_scoring.go#L17-L19). When the target becomes invalid, `shouldDropCurrentTargetLocked` clears both the ID and the sticky flag via `clearCombatTargetLocked`. New code that introduces player-directed targeting must follow the same pattern.

7. **The registry is the single source of truth.** If the same target needs to be known in two places (e.g., on the unit and in an AI scoring context), both places store the ID and resolve independently. Do not hand a pointer from one owner to another for storage.

### Red flags in code review

- A new field with type `*Unit` or `*BuildingTile` on any struct that is not a tick-local working value (anything persisted on `Unit`, `Projectile`, `PerkState`, `ThreatEntry`, etc.).
- A `getUnitByIDLocked(...)` call whose result is used without a `nil` / `HP` / `Visible` / ownership check.
- A function that receives an ID, resolves it, and stores the resulting pointer somewhere that survives the function call.
- Code that assumes "the target was valid last tick, so it's valid now."
- Introducing a parallel `*Unit` cache alongside an existing ID field (double-source-of-truth).

### Not a red flag

- Helpers taking `target *Unit` as a parameter. These are within-tick working values and match the existing idiom. Do not rewrite them to take IDs unless there is a concrete reason (e.g., the helper is being moved to a context that outlives the tick).

## Simulation & Concurrency

- Functions ending in `Locked` assume `s.mu` (or the relevant state lock) is already held. Do not acquire the lock inside them and do not call them without holding it.
- Tick simulation is deterministic under a seed. Do not introduce nondeterministic sources (wall-clock time, `math/rand` without the seeded RNG, map iteration order used to drive outcomes) into simulation code.
- Mutations to `GameState` happen inside the tick loop. Network/IO handlers enqueue intents, they do not mutate game state directly.

## Frontend

- Client is **TypeScript / Vue 3** (see [client/src/game-portal/src/](../../client/src/game-portal/src/)). Prefer editing existing components and Pinia stores over introducing new abstractions.
- The client is a view of server state. Never simulate gameplay logic client-side that the server is authoritative over — render what the server sends.

## When in doubt

- Read the adjacent code before introducing a new pattern. The existing combat AI, projectile, threat, and CC systems are the reference implementations for ID-based targeting.
- If a rule in this doc appears to conflict with what the code actually does, trust the code and flag the doc for correction rather than "migrating" working code to match a misread rule.
