# Objective First-Completion Dominion Point Rewards

**Date:** 2026-07-01
**Status:** Approved design, pending implementation plan

## Summary

Let map authors attach a Dominion Point (DP) reward to any objective. When a
player completes that objective for the **first time ever** (across all plays of
the map, tracked persistently), the reward DP is credited to their profile.
Completing the same objective again in a later match awards nothing. The reward
amount is configured per-objective in the map editor.

## Goals

- Map authors can set an optional DP reward on each objective, regardless of
  objective type, from the map editor.
- The reward is granted exactly once per player per objective — the first time
  that player ever completes it.
- Works identically for the local/host player and for remote multiplayer
  joiners (whose profiles persist on their own machine).
- No new writes on the tick/simulation path; no new nondeterminism.

## Non-Goals

- Rewarding resources (gold/wood) or anything other than Dominion Points.
- Re-awarding on repeat completion, or "per-match" rewards. (Explicitly ruled
  out: first-time-ever only.)
- Changing how objectives are evaluated, gated (`required`), or how victory is
  decided. Only the reward-on-first-completion is added.
- Wiring the currently-unused `DominionPointsTuning.PerObjective` global tuning
  field. This feature is per-objective config in the map, not a global constant.
  (Left as-is; a future change may retire it.)

## Background / Current State

**Objectives.** Defined in map JSON as `ObjectiveDef`
([objective_defs.go:25-37](../../../server/internal/game/objective_defs.go#L25-L37)):
`ID`, `Type`, `Description`, `Scope` (team|player), `Required`, `Config`. Evaluated
per-tick; `ObjectiveState.Completed` is sticky within a match
([objective_defs.go:50-57](../../../server/internal/game/objective_defs.go#L50-L57)).
Client mirror types: `Objective`
([campaign.ts:27-43](../../../client/src/game-portal/src/types/campaign.ts#L27-L43))
and the wire type `ObjectiveSnapshot`/`ObjectiveProgress` carried in
`VictorySnapshot`.

**Persistent "ever completed" tracking already exists.** At match end the client
POSTs the set of completed objective IDs to
`/api/profile/campaign/complete-objectives`
([profile_handlers.go:433-483](../../../server/internal/http/profile_handlers.go#L433-L483)),
which merges them into `PlayerProfile.CompletedCampaignObjectives`
(`map[key][]string`, key = `"<campaignId>/<levelId>"`) via `addToSortedSet`. This
is idempotent and is exactly the "first time ever" signal we need — an ID that is
**not already in the set** when this POST runs is a first-time completion. The
client caller is `markCampaignObjectivesComplete`
([profileApi.ts:144-155](../../../client/src/game-portal/src/services/profileApi.ts#L144-L155)).

**Dominion points.** Persist on the profile as `DominionPoints` (spendable) and
`LifetimeDominionPoints` (all-time). Canonical award helper:
`profile.Manager.CommitDominionPoints(playerID string, earned int) error`
([profile/manager.go:59-67](../../../server/internal/profile/manager.go#L59-L67)) —
atomic, no-op when `earned <= 0`, increments both fields.

**Per-machine persistence.** Profile HTTP calls (including
complete-objectives) target the caller's own local server — the host writes its
own profile, a remote joiner writes its own. `isRemoteProxyClient()`
([profileApi.ts:189-197](../../../client/src/game-portal/src/services/profileApi.ts#L189-L197))
gates the host-vs-joiner DP-commit split for the *kill-drop* flow, but the
objective-completion POST is already per-machine for everyone. This is why
attaching the DP award to that POST covers both host and joiner with no
branching.

## Approach

**Piggyback the award on the existing objective-persistence POST.** The
complete-objectives endpoint already performs the set-merge that determines which
objective IDs are new. Extend it to also credit each newly-added objective's DP
reward in the same locked profile mutation. This is atomic, idempotent, uniform
across host/joiner, and touches nothing on the tick path.

Rejected alternatives:

- **Award during tick simulation** when `ObjectiveState.Completed` flips: forbidden —
  the tick loop must not do profile IO (determinism + no-IO-on-tick rules), and a
  remote joiner's sim runs on the host, so a server-side profile write would never
  reach the joiner's machine.
- **Separate client-side DP award call** (reusing `/award-dominion-points`):
  splits the "is this ID new?" decision from the set-merge that decides it,
  creating a double-award race between two POSTs. The single-endpoint approach
  keeps the decision and the award in one lock.

## Design

### 1. Data model — reward field on the objective definition

**Server** ([objective_defs.go](../../../server/internal/game/objective_defs.go)):
add to `ObjectiveDef`:

```go
// RewardDominionPoints is the Dominion Point reward granted the first time
// (ever, per player) this objective is completed. 0 / omitted = no reward.
RewardDominionPoints int `json:"rewardDominionPoints,omitempty"`
```

This is metadata only — it does not participate in evaluation, so no handler
changes. Add a validation guard alongside the existing objective validation:
reject negative values at catalog load (consistent with how other objective
fields panic on bad data).

**Client** ([campaign.ts](../../../client/src/game-portal/src/types/campaign.ts)):
add to `Objective`:

```ts
/** DP reward granted the first time (ever, per player) this objective is
 *  completed. Absent/0 = no reward. */
rewardDominionPoints?: number
```

Map JSON with no `rewardDominionPoints` behaves exactly as today (0 = no reward),
so all existing maps are untouched.

### 2. Surface the reward amount to the match-end client

Add `rewardDominionPoints` to the objective wire snapshot so the recap has each
completed objective's reward in hand without re-loading the map:

- Server `ObjectiveSnapshot` (protocol messages) gains
  `RewardDominionPoints int \`json:"rewardDominionPoints,omitempty"\``, populated
  from the `ObjectiveDef` when the per-viewer snapshot is built
  (`buildVictorySnapshotForViewerLocked`).
- Client `ObjectiveProgress` gains `rewardDominionPoints?: number`.

### 3. Map editor UI

In [MapEditorPanel.vue](../../../client/src/game-portal/src/components/MapEditorPanel.vue),
add one "Dominion Point Reward" number input to each objective card (rendered for
every objective type, next to the existing id/type/description/scope/required
controls). Wire it through the existing objective mutation helpers:

- `addObjective()` — default `rewardDominionPoints: 0` (or omit; treat blank as 0).
- `updateObjective(index, patch)` — handle the new field.
- Map serialization/deserialization — persist `rewardDominionPoints` in the map
  JSON round-trip. Blank input serializes as omitted/0.

Validation: clamp to a non-negative integer in the UI (mirror the server guard).

### 4. Award path — extend the complete-objectives endpoint

**Request body** ([profileApi.ts](../../../client/src/game-portal/src/services/profileApi.ts)
`markCampaignObjectivesComplete`): replace the flat `objectiveIds: string[]` with
entries that carry the reward, e.g.:

```ts
objectives: { id: string; rewardDominionPoints?: number }[]
```

The caller builds this from the match-end objective snapshots: for each objective
with `completed === true`, send `{ id, rewardDominionPoints }`. (Failed / incomplete
objectives are still excluded, matching today.)

**Server** ([profile_handlers.go:433-483](../../../server/internal/http/profile_handlers.go#L433-L483)):
- Accept the new body shape. (Keep tolerant decoding of the old `objectiveIds`
  shape if any caller lags, or update both together — see Migration.)
- Inside the existing `WithLocked` mutation, for each incoming objective:
  determine whether its ID is **already present** in the level's set *before*
  merging. Accumulate `earned += rewardDominionPoints` only for IDs that are newly
  added (not previously present) and whose reward is `> 0`.
- After the set merge, apply the reward in the same lock. Because
  `CommitDominionPoints` acquires its own profile lock, either (a) inline the DP
  increment directly on `p` here (add to `p.DominionPoints` and
  `p.LifetimeDominionPoints`, mirroring `CommitDominionPoints`), or (b) compute
  `earned` inside the lock and call `CommitDominionPoints` after it returns.
  **Prefer (a)** so the set-merge and the DP credit are one atomic profile write —
  this is what makes the operation race-free and idempotent.
- Return the updated profile (already the current behavior). The client's DP
  balance updates reactively from the returned profile, so the recap/menu shows
  the new total with no extra fetch.

**Idempotency / first-time-ever guarantee:** the award is keyed off "ID was not in
the persistent set at merge time." Replaying the map re-sends the same IDs, they
are already in the set, so `earned` is 0 and no DP is granted. This holds per
player per level, exactly matching the approved semantics.

### 5. Team-scope objectives

For a `team`-scope objective, every player who completes the match sends their own
completed-objectives POST against their own profile. Each player is therefore
rewarded based on **their own** persistent set: a teammate who has completed this
objective before gets nothing, a first-timer gets the reward. No special-casing —
this falls out of the per-player, per-machine persistence already in place.

## Data Flow (award, end to end)

1. Match ends. Client reads the final objective snapshots (now carrying
   `rewardDominionPoints`).
2. Client builds `objectives: [{id, rewardDominionPoints}]` from those with
   `completed === true` and POSTs to `/api/profile/campaign/complete-objectives`
   against its **own** local server.
3. Server locks the caller's profile, computes which IDs are newly added to
   `CompletedCampaignObjectives["<campaignId>/<levelId>"]`, sums their rewards,
   adds that sum to `DominionPoints` + `LifetimeDominionPoints`, merges the IDs,
   and returns the updated profile — all in one lock.
4. Client updates its reactive profile from the response; the DP balance in the
   menu reflects the reward.

## Error Handling / Edge Cases

- **Reward = 0 / omitted:** no DP credited; behaves exactly as today.
- **Negative reward:** rejected at catalog load (server) and clamped in the editor
  (client). Should never reach the award path.
- **Repeat completion:** ID already in the set → `earned` unchanged → no award.
- **Defeat with zero completions:** empty `objectives` array is still valid (as
  today); no award.
- **Old-shape request (`objectiveIds`)**: handle during migration — either keep a
  tolerant decoder that accepts both, or ship client+server together. No reward is
  granted for the legacy shape (no reward data present), which is safe.
- **Missing `campaignId`/`levelId`:** endpoint already 400s; unchanged. Maps must
  supply a stable level key for persistence — the same precondition the existing
  objective-completion tracking already requires.

## Testing

- **Server unit test** on the endpoint handler: first POST with a rewarded
  objective credits DP; second identical POST credits nothing (idempotent);
  mixed batch (one new + one already-completed) credits only the new one; reward=0
  credits nothing; negative reward never reaches here (guarded at load).
- **Server catalog-load test:** an objective with a negative `rewardDominionPoints`
  is rejected; a valid one loads and round-trips through the snapshot.
- **Client:** map editor round-trip test — set a reward, save, reload, value
  persists; blank stays blank/0. (Follow existing MapEditorPanel test patterns.)
- **No hardcoded tunables in tests** — derive expected DP from the objective's
  configured reward value in the test fixture, not a pinned literal (per project
  test rules).

## Migration

- Existing maps: no `rewardDominionPoints` → 0 → no behavior change.
- Existing profiles: `CompletedCampaignObjectives` already populated. **Note:**
  objectives a player completed *before* this feature shipped are already in their
  set, so they will **not** retroactively award DP (they are not "newly added").
  This is the intended, safe behavior — no retroactive DP grants.
- Endpoint body shape change: update client `markCampaignObjectivesComplete` and
  the server handler together in the same change; optionally keep the server
  tolerant of the legacy `objectiveIds` field for one release.

## Open Questions

None outstanding. Decisions locked with the user:
- First-time-ever (persistent), not per-match. ✔
- One DP field per objective (all types). ✔
- Team objectives reward each qualifying player via their own persistent set. ✔
- Client sends reward amounts in the request (consistent with the existing
  client-trusted DP award model in this codebase). ✔
