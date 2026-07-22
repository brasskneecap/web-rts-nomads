# Death, Corpses and Revival

**Status:** IMPLEMENTED (2026-07-22), with placeholder art. Bodies linger for 20
seconds and render as a red blood splatter; real death animations and body
sprites are the only thing still outstanding, and they are confined to one
client method (§11).

---

## 1. The change

Today a unit that reaches 0 HP is queued in `pendingDeaths` and removed from the
field by `drainPendingDeathsLocked` in the same tick. The intent is that a dying
unit switches from an **alive state** to a **dead state** and stays on the field
for a while — a death animation, a body that can be raised, a body that can be
revived.

Two things follow, and they want different shapes.

## 2. A corpse is the same `Unit`, on its own list

The body is the same `*Unit` value it was in life — same ID, same rank, path,
perks, items, XP — so a revive is a move, not a reconstruction. But it lives in
`s.Corpses` / `s.corpsesByID`, **not** in `s.Units`.

The original plan was to leave it in `s.Units` behind a `Dead` flag. That is
wrong for one measurable reason: it puts a corpse in front of ~110 existing
`range s.Units` loops, every one written when a dead unit could not still be
there. Several would be wrong in ways nobody would notice for weeks — a body
granting fog-of-war vision for 20 seconds, a cleric auto-healing it, an aura
counting it as a nearby ally, a spatial index pushing units off it. Auditing 110
call sites is a worse plan than making them structurally unable to see one.

So: `getUnitByIDLocked` deliberately does **not** resolve a corpse; reading one
is an explicit `getCorpseByIDLocked`. `removeUnitLocked` stops being what death
calls and becomes what **decay** calls (`killUnitToCorpseLocked` / `corpses.go`).

## 3. Statuses and buffs do NOT survive death — DECIDED

A status is attached to a living host. When the host dies, the status ends: it
fires `on_status_expire` and is dropped. A revived unit comes back **clean**.

This is already how `tickAbilityStatusesLocked` behaves, and the status-bound
visual half now matches it (`RequiresLiveAnchor`, §5). Nothing needs suspending,
serializing or resuming — which is the main reason to write this rule down
rather than discover it halfway through implementing revive.

What DOES survive is everything that lives on the `Unit` itself: rank, promotion
path, perks, items, XP. Those come back for free precisely because of §2.

## 4. Targeting a corpse is opt-in, and ownership is unchanged — DECIDED

### The query field

`TargetQueryDef` needs a life-state selector whose **zero value means alive
only**:

| value | selects | who authors it |
|---|---|---|
| `""` (default) | living units | every query that exists today, unchanged |
| `"dead"` | corpses only | revive, raise skeleton, devour-a-body |
| `"any"` | both | rare |

It must NOT be expressed as an entry in the existing `Filters` list. A filter is
opt-in, so every already-authored query would silently begin selecting corpses
the day the flag ships — meteor, chain lightning and every zone tick would start
spending damage on bodies. The zero value has to be the safe one. (Same
reasoning as `ExcludeCurrentEvent` being its own field rather than a widening of
`ExcludeSource`.)

### Ownership

**A corpse belongs to the player whose unit it was.** `relations` keeps working
exactly as it does for living units — no special case.

That is what makes the important restriction expressible: **raise skeleton
targets `enemy` corpses only.** A friendly raise would consume a body the owner
may have wanted to revive, and the two abilities would be racing to spend the
same corpse. Revive targets `ally` corpses. The existing relation vocabulary
already separates them.

## 5. What exists today

`(*GameState).unitIsAliveLocked(u *Unit)` in `damage_pipeline.go` is the single
definition of "still a living host": not in the registry, or HP <= 0, or queued
in this tick's `pendingDeathsSet`. **The dead-state flag belongs inside this
function**, not at its call sites.

Adopted by `tickAbilityStatusesLocked` (entry + died-mid-tick),
`tickEffectsLocked` (via `effectInstance.RequiresLiveAnchor`), and
`play_presentation`'s `bindToStatusDuration` path.

Deliberately NOT adopted by the combat AI, which asks a different question —
*targetability*, which also requires `Visible` and an ownership check. Conflating
the two would make corpses untargetable by accident, and §4 needs them
targetable.

## 6. Teardown happens at DEATH, not at decay — DECIDED

Everything `removeUnitLocked` does today is death-time: clear the dying unit's
channel, drop beams aimed at it, cull in-flight projectiles involving it, strip
neutral-camp membership, clear every other unit's attack target pointing at it.

No per-line split. A corpse is an inert husk — nothing is still pointing at it,
nothing is still flying toward it, and a revive does not restore any of it. The
only thing decay does is the final registry removal.

If something later needs to survive death and be restored by a revive, that is
the moment to split it out — not before.

## 7. A corpse does not block movement — DECIDED

No cell occupancy, no pathing cost, no collision. Units walk over bodies.

Free, given §2: `buildBlockedCells` never considered units in the first place,
and `applyUnitSeparationLocked` indexes `s.Units`, which a corpse is no longer
in. Nothing had to be excluded by hand.

## 8. Lifetime is 20s; raise skeleton is the only consumer — DECIDED (provisional)

A corpse decays 20 seconds after death, then `removeUnitLocked`. The number is a
feel-check starting point, not a balance decision — expect it to move.

Raise skeleton is the only thing that consumes a corpse today, and consuming it
removes the body immediately (the raised skeleton is a new unit; its own death
leaves its own corpse like any other unit).

## 9. Every loop over units sees only the living — DECIDED, and free

Given §2 this needed no sweep: a corpse is not in `s.Units`, so regen, auras,
acquisition, threat, fog-of-war and the spatial indexes cannot see one. The full
server suite passed on the first run after the split, which is the evidence that
the separate list was the cheaper design.

**Snapshot building is the exception, and it is opt-in rather than filtered.**
Bodies travel on their own `corpses` wire list, not in `units`. Same argument
one layer up: the client has its own pile of code that walks `units` —
selection, drag-select, click targeting, health bars, minimap blips — and none
of it should have to learn what a corpse is.

## 10. Targeting a corpse: `TargetQueryDef.AliveState`

Already existed as a field, but could not reach anything once bodies left
`s.Units`. Now:

- `""` / `"alive"` — living units. Every query authored before this.
- `"dead"` — corpses only; the pool is `s.Corpses`.
- `"any"` — both.

An **unrecognized** value reads as `alive`: a typo must not silently widen a
query to include bodies.

`consumeCorpseLocked(id)` is how an ability spends a body. It returns false if
the id is not (or is no longer) a corpse, so two abilities racing for the same
body in one tick cannot both get it.

## 11. Placeholder art — the one thing still outstanding

`CanvasRenderer.drawCorpses` draws a procedural dark-red splatter: a main pool
plus four satellite spatters, positioned by a hash of the unit id so a given body
looks the same on every frame and every client (the renderer must stay
deterministic). It fades over its last 1.5 seconds.

Deliberately **not** a faded unit sprite: a body that looks like a unit invites
clicking it, and corpses are not in `units`, so the click would do nothing and
read as a bug.

When real death animations arrive, `drawCorpses` is the only thing that changes.
Nothing else in the client knows what a corpse looks like.

## 12. The preview shows what a match shows

The ability previewer replays `snapshotUnfilteredLocked` frames captured from a
real `GameState`, so a previewed kill leaves a body exactly as a live kill does
(`AbilityPreviewCanvas.applyFrame` assigns `state.corpses` alongside
`state.traps`, for the same reason). Standing rule: **anything visible in a
match must be visible in the previewer**, or an author builds around behaviour
the game does not have.

## 13. Still open

- Raise skeleton does not consume a corpse yet — the mechanism
  (`AliveState: "dead"` + `consumeCorpseLocked`) is in place, but the ability
  still summons from nothing. Rewiring it is a gameplay change, not plumbing.
- Revive does not exist yet. Moving a body back into `s.Units` is the whole of
  it; nothing needs restoring (§3).
- Whether a corpse is selectable by its owner (currently: no).
