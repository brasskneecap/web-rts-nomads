# Follow-up: Player-vs-Player Alliance Spec

## Context

As of 2026-04-23, all newly joined players are **allied by default**. The hostility predicate is centralized in [state_combat.go:8-24](../../server/internal/game/state_combat.go#L8-L24):

```go
func playersAreHostile(a, b string) bool {
    if a == b {
        return false
    }
    return a == enemyPlayerID || b == enemyPlayerID
}
```

Every combat / target-acquisition / threat / trap / AoE site in the codebase already routes through this function. The wave-enemy faction (`enemyPlayerID == "__enemy__"`) is the only thing currently hostile to real players.

The original change set this up so that introducing a real PvP spec later requires editing **one function**, not chasing call sites. This doc describes that follow-up work.

## Trigger

Open this doc when any of the following lands:
- A new field on the `Player` struct in [state.go:192-199](../../server/internal/game/state.go#L192-L199) such as `TeamID`, `Alliance`, `Faction`, or similar.
- Any per-match hostility config in lobby / match-creation code (look around [handlers.go](../../server/internal/ws/handlers.go) and anywhere `EnsurePlayer` is called).
- Any new types or functions that look like alliance/team configuration.

If none of these have shipped, **do nothing** — the current "all real players are allies" default is the intended behavior until the spec exists.

## Task 1 — Wire the spec through `playersAreHostile`

Update the helper in [state_combat.go:8-24](../../server/internal/game/state_combat.go#L8-L24) to consult the new spec. Keep the same signature so call sites stay untouched. Pseudocode:

```go
func (s *GameState) playersAreHostile(a, b string) bool {
    if a == b {
        return false
    }
    if a == enemyPlayerID || b == enemyPlayerID {
        return true
    }
    // New: per-match team / alliance lookup
    return s.teamsAreHostileLocked(a, b)
}
```

If the helper needs `*GameState`, promote it from a free function to a method. All call sites currently invoke it as `playersAreHostile(...)` — a method call requires updating those, but it's a mechanical sweep (~30 sites, all already grep-able by the function name).

**Do not introduce a parallel predicate.** The whole point of the central helper is that it's the single source of truth.

## Task 2 — Audit same-OwnerID buff/aura/threat sites

The original change deliberately left "own units only" semantics intact for buffs and threat-sharing. Under PvP, some of these may need to extend to teammates. Decide each on its own merits — extending isn't automatic.

| File | Site | Current behavior | PvP question |
|---|---|---|---|
| [combat_ai_scoring.go](../../server/internal/game/combat_ai_scoring.go) | `backlineProtectionScoreLocked` | Score boost only when target is attacking *your own* backline unit | Should an ally's backline count? |
| [combat_ai_scoring.go](../../server/internal/game/combat_ai_scoring.go) | `structureDefenseScoreLocked` | Bonus for defending *your own* buildings | Should ally buildings count? |
| [combat_ai_scoring.go](../../server/internal/game/combat_ai_scoring.go) | `isEngagedByFriendlyFrontlineLocked` | "Friendly" = same `OwnerID` only | Should allied frontlines count as friendly engagement? |
| [combat_ai_scoring.go](../../server/internal/game/combat_ai_scoring.go) | `estimateDangerScoreLocked` (frontline support loop, ~line 397) | Allied support penalty only counts your own frontline units | Should allied frontline support reduce your danger score? |
| [combat_ai_threat.go](../../server/internal/game/combat_ai_threat.go) | `onUnitDamagedLocked` ally threat sharing (~line 81) | Damage to your unit raises threat on *your other units* nearby | Should it raise threat on allies' units too? |
| [combat_ai_threat.go](../../server/internal/game/combat_ai_threat.go) | `onBuildingDamagedLocked` (~line 100) | Same — your-units-only threat propagation around damaged building | Same question. |
| [perks_auras.go](../../server/internal/game/perks_auras.go) | Aura targeting (~lines 157, 246) | Auras only buff units of the aura-source's owner | Should allied units pick up aura buffs? Big gameplay impact. |
| [perks_icons.go](../../server/internal/game/perks_icons.go) | Perk icon visibility | Same-owner only | Cosmetic — probably fine. |
| [perks.go:716](../../server/internal/game/perks.go#L716) | Perk-driven scan | Same-owner only | Depends on the perk; likely keep own-only. |
| [trap.go:1132](../../server/internal/game/trap.go#L1132) | Final exposure AoE | Hits other units sharing victim's exact `OwnerID` | Under PvP, "victim's faction" needs a clearer definition — see note below. |

### Notes on the trap.go:1132 site

The current check filters AoE to `u.OwnerID == victim.OwnerID`. Comment in code: *"same-team-as-victim only; other teams aren't this trapper's concern"*. Under wave-only hostility this works because the victim is always `__enemy__` and AoE hits other `__enemy__` units. Under PvP with allied teams, "victim's team" needs to mean the *team*, not the *single OwnerID*. The simpler reframing: `playersAreHostile(u.OwnerID, trap.OwnerPlayerID)` (i.e., AoE hits units hostile to the trap owner). Verify with a unit test before changing.

## Task 3 — Tests

The existing test fixtures use `enemyPlayerID` as the hostile party. That continues to work. When PvP spec lands, add new tests that:
- Register two real players and a hostility relationship between them via the new spec.
- Assert that `playersAreHostile("p1", "p2")` returns `true` only when the spec says so.
- Re-run the existing combat/trap tests to confirm they still pass with the new helper.

Run `go test ./...` from `server/` before opening any PR.

## Out of scope

- Don't redesign the lobby flow — that's the spec's job.
- Don't preemptively extend auras/buffs to allies "because PvP eventually." Make that call when there's a concrete spec and a gameplay decision behind it.
- Don't add a `TeamID` field speculatively. Wait for the spec to define the data model.
