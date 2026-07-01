# Objective First-Completion Dominion Point Rewards — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **Project rule — NO git commits by the tooling.** The user handles all staging and commits. Do **not** run `git add` / `git commit`. Where this plan says "Checkpoint", stop and let the user review/commit.

**Goal:** Let map authors attach a Dominion Point reward to any objective; grant it the first time (ever, per player) that player completes the objective.

**Architecture:** Add an optional `rewardDominionPoints` field to the objective definition (server + client + map JSON). Surface it on the per-viewer objective snapshot. At match-end the client already POSTs completed objective IDs to `/api/profile/campaign/complete-objectives`; extend that endpoint to atomically credit the reward for objective IDs that are *newly* added to the player's persistent `CompletedCampaignObjectives` set — which is exactly "first time ever". Nothing touches the tick/simulation path.

**Tech Stack:** Go (server, `net/http`, `go test`), TypeScript/Vue 3 (client, `vitest`, `vue-tsc -b`).

**Design doc:** [docs/superpowers/specs/2026-07-01-objective-first-completion-dominion-reward-design.md](../specs/2026-07-01-objective-first-completion-dominion-reward-design.md)

**Commands:**
- Server tests: `cd server && go test ./internal/http/... ./internal/game/... ./pkg/protocol/...`
- Client type-check: `cd client/src/game-portal && npx vue-tsc -b` (per project convention — `--noEmit` false-cleans)
- Client tests: `cd client/src/game-portal && npm run test`

---

## File Structure

**Server (Go):**
- `server/internal/game/objective_defs.go` — add `RewardDominionPoints int` to `ObjectiveDef`; validate non-negative in `parseAndValidateObjectiveDef`.
- `server/pkg/protocol/messages.go` — add `RewardDominionPoints int` to `MapCampaignObjective` (authoring/map wire) and to `ObjectiveSnapshot` (per-tick wire).
- `server/internal/game/campaign_defs.go` — carry the field in the `MapCampaignObjective → ObjectiveDef` conversion.
- `server/internal/game/objective_runtime.go` — populate the field on the per-viewer snapshot.
- `server/internal/http/profile_handlers.go` — extend the complete-objectives endpoint to award DP for newly-added objective IDs.

**Client (TS/Vue):**
- `client/src/game-portal/src/game/network/protocol.ts` — add `rewardDominionPoints?` to `MapCampaignObjective` and `ObjectiveSnapshot`.
- `client/src/game-portal/src/types/campaign.ts` — add `rewardDominionPoints?` to `Objective` and `ObjectiveProgress` (documented server mirrors).
- `client/src/game-portal/src/components/MapEditorPanel.vue` — add the "DP Reward" input + default it in `addObjective()`.
- `client/src/game-portal/src/services/profileApi.ts` — change `markCampaignObjectivesComplete` to send `objectives: {id, rewardDominionPoints}[]`.
- `client/src/game-portal/src/views/MatchEnd.vue` — build the objectives-with-rewards payload from the match-end snapshot.

---

## Task 1: Server — reward field on the objective definition + validation

**Files:**
- Modify: `server/internal/game/objective_defs.go` (struct at 25-37; validator at 130-…)
- Modify: `server/pkg/protocol/messages.go` (`MapCampaignObjective` at 363-370)
- Modify: `server/internal/game/campaign_defs.go` (conversion at 197-204)
- Test: `server/internal/game/objective_reward_def_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `server/internal/game/objective_reward_def_test.go`:

```go
package game

import "testing"

// A valid non-negative reward survives parse+validate and is preserved on the
// returned ObjectiveDef.
func TestObjectiveReward_PositivePreserved(t *testing.T) {
	def := parseAndValidateObjectiveDef("test.json", "test_level", ObjectiveDef{
		ID:                   "clear_camps",
		Type:                 "kill_camps",
		Config:               []byte(`{"count":1}`),
		RewardDominionPoints: 25,
	})
	if def.RewardDominionPoints != 25 {
		t.Fatalf("RewardDominionPoints: want 25, got %d", def.RewardDominionPoints)
	}
}

// A negative reward is rejected at catalog-load time (panics, matching the
// other objective validation guards).
func TestObjectiveReward_NegativeRejected(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic for negative rewardDominionPoints, got none")
		}
	}()
	parseAndValidateObjectiveDef("test.json", "test_level", ObjectiveDef{
		ID:                   "clear_camps",
		Type:                 "kill_camps",
		Config:               []byte(`{"count":1}`),
		RewardDominionPoints: -5,
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd server && go test ./internal/game/ -run TestObjectiveReward -v`
Expected: compile error (`unknown field RewardDominionPoints`) or FAIL.

- [ ] **Step 3: Add the struct field**

In `server/internal/game/objective_defs.go`, inside `ObjectiveDef` (after the `Required` field, before `Config`):

```go
	// RewardDominionPoints is the Dominion Point reward granted the first
	// time (ever, per player) this objective is completed. 0 / omitted = no
	// reward. Metadata only — it does not participate in evaluation.
	RewardDominionPoints int `json:"rewardDominionPoints,omitempty"`
```

- [ ] **Step 4: Add the validation guard**

In `server/internal/game/objective_defs.go`, inside `parseAndValidateObjectiveDef`, immediately after the scope `switch` block (before the `handler, ok := objectiveRegistry[...]` lookup):

```go
	if raw.RewardDominionPoints < 0 {
		panic("catalog\\campaigns\\" + filename + ": level " + levelID +
			": objective " + raw.ID + ": rewardDominionPoints must be >= 0")
	}
```

- [ ] **Step 5: Add the field to the map wire struct**

In `server/pkg/protocol/messages.go`, inside `MapCampaignObjective` (after `Required`, before `Config`):

```go
	RewardDominionPoints int             `json:"rewardDominionPoints,omitempty"`
```

- [ ] **Step 6: Carry the field through the conversion**

In `server/internal/game/campaign_defs.go`, in the `def := ObjectiveDef{...}` literal (197-204), add:

```go
			RewardDominionPoints: raw.RewardDominionPoints,
```

- [ ] **Step 7: Run the test to verify it passes**

Run: `cd server && go test ./internal/game/ -run TestObjectiveReward -v`
Expected: PASS (both cases).

- [ ] **Step 8: Run the surrounding suites to confirm no regression**

Run: `cd server && go test ./internal/game/ ./pkg/protocol/...`
Expected: PASS.

- [ ] **Step 9: Checkpoint** — stop for user review/commit.

---

## Task 2: Server — surface the reward on the per-viewer snapshot

**Files:**
- Modify: `server/pkg/protocol/messages.go` (`ObjectiveSnapshot` at 1340-1362)
- Modify: `server/internal/game/objective_runtime.go` (`buildVictorySnapshotForViewerLocked` append at 141-151)
- Test: `server/internal/game/snapshot_objectives_test.go` (add a test; helper `findObjective` already exists at line 9)

- [ ] **Step 1: Write the failing test**

Append to `server/internal/game/snapshot_objectives_test.go`:

```go
// An objective's authored RewardDominionPoints is carried onto the per-viewer
// ObjectiveSnapshot so the match-end client can send it back with the
// completion POST.
func TestObjectiveSnapshot_CarriesRewardDominionPoints(t *testing.T) {
	s := NewGameState(1, 1, 1) // matches existing constructor arity in this file
	s.Objectives = []objectiveRuntime{
		{
			Def: ObjectiveDef{
				ID:                   "clear_camps",
				Type:                 "kill_camps",
				Scope:                ObjectiveScopeTeam,
				RewardDominionPoints: 40,
			},
			TeamState: ObjectiveState{ObjectiveID: "clear_camps", Scope: ObjectiveScopeTeam},
		},
	}
	snap := s.buildVictorySnapshotForViewerLocked("")
	if snap == nil {
		t.Fatal("expected a victory snapshot, got nil")
	}
	got := findObjective(snap.Objectives, "clear_camps")
	if got.RewardDominionPoints != 40 {
		t.Fatalf("RewardDominionPoints: want 40, got %d", got.RewardDominionPoints)
	}
}
```

> Note for implementer: match `NewGameState`'s real signature — copy the constructor call from an existing test at the top of `snapshot_objectives_test.go` rather than trusting the arity above. The assertion is what matters.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd server && go test ./internal/game/ -run TestObjectiveSnapshot_CarriesRewardDominionPoints -v`
Expected: compile error (`unknown field RewardDominionPoints` on `ObjectiveSnapshot`) or FAIL.

- [ ] **Step 3: Add the field to `ObjectiveSnapshot`**

In `server/pkg/protocol/messages.go`, inside `ObjectiveSnapshot` (after `Failed`):

```go
	// RewardDominionPoints is the DP reward this objective grants the first
	// time (ever, per player) it is completed. The match-end client echoes it
	// back with the completion POST so the server can credit it. 0 = no reward.
	RewardDominionPoints int `json:"rewardDominionPoints,omitempty"`
```

- [ ] **Step 4: Populate it in the snapshot builder**

In `server/internal/game/objective_runtime.go`, in the `protocol.ObjectiveSnapshot{...}` literal (141-151), add:

```go
			RewardDominionPoints: runtime.Def.RewardDominionPoints,
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `cd server && go test ./internal/game/ -run TestObjectiveSnapshot_CarriesRewardDominionPoints -v`
Expected: PASS.

- [ ] **Step 6: Run surrounding suites**

Run: `cd server && go test ./internal/game/ ./pkg/protocol/...`
Expected: PASS.

- [ ] **Step 7: Checkpoint** — stop for user review/commit.

---

## Task 3: Server — award DP for newly-completed objectives in the endpoint

**Files:**
- Modify: `server/internal/http/profile_handlers.go` (handler at 433-483)
- Test: `server/internal/http/objective_reward_award_test.go` (new; reuse helpers `newTestMux`, `seedPlayer`, `postJSON`, `readProfileBody`, `testPlayerID` from `complete_objectives_handler_test.go`)

- [ ] **Step 1: Write the failing tests**

Create `server/internal/http/objective_reward_award_test.go`:

```go
package httpserver

import (
	"net/http"
	"testing"
)

// First-ever completion of a rewarded objective credits its DP to the profile
// (both spendable and lifetime).
func TestObjectiveReward_FirstCompletionAwardsDP(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	rec := postJSON(t, mux, completeObjectivesPath, testPlayerID, map[string]any{
		"campaignId": "forest",
		"levelId":    "forest_01",
		"objectives": []map[string]any{
			{"id": "clear_camps", "rewardDominionPoints": 30},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	p := readProfileBody(t, rec)
	if p.DominionPoints != 30 {
		t.Errorf("DominionPoints: want 30, got %d", p.DominionPoints)
	}
	if p.LifetimeDominionPoints != 30 {
		t.Errorf("LifetimeDominionPoints: want 30, got %d", p.LifetimeDominionPoints)
	}
}

// Re-completing the same objective grants nothing (first-time-ever only).
func TestObjectiveReward_RepeatCompletionAwardsNothing(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	body := map[string]any{
		"campaignId": "forest",
		"levelId":    "forest_01",
		"objectives": []map[string]any{{"id": "clear_camps", "rewardDominionPoints": 30}},
	}
	_ = postJSON(t, mux, completeObjectivesPath, testPlayerID, body)
	second := postJSON(t, mux, completeObjectivesPath, testPlayerID, body)

	p := readProfileBody(t, second)
	if p.DominionPoints != 30 {
		t.Errorf("DominionPoints after repeat: want 30 (awarded once), got %d", p.DominionPoints)
	}
}

// A batch mixing a brand-new objective with an already-completed one credits
// only the new one.
func TestObjectiveReward_OnlyNewObjectivesAwarded(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	_ = postJSON(t, mux, completeObjectivesPath, testPlayerID, map[string]any{
		"campaignId": "forest",
		"levelId":    "forest_01",
		"objectives": []map[string]any{{"id": "clear_camps", "rewardDominionPoints": 30}},
	})
	rec := postJSON(t, mux, completeObjectivesPath, testPlayerID, map[string]any{
		"campaignId": "forest",
		"levelId":    "forest_01",
		"objectives": []map[string]any{
			{"id": "clear_camps", "rewardDominionPoints": 30}, // already done
			{"id": "build_barracks", "rewardDominionPoints": 15}, // new
		},
	})
	p := readProfileBody(t, rec)
	if p.DominionPoints != 45 { // 30 + 15
		t.Errorf("DominionPoints: want 45, got %d", p.DominionPoints)
	}
}

// A zero-reward objective completes normally but grants no DP.
func TestObjectiveReward_ZeroRewardGrantsNoDP(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	rec := postJSON(t, mux, completeObjectivesPath, testPlayerID, map[string]any{
		"campaignId": "forest",
		"levelId":    "forest_01",
		"objectives": []map[string]any{{"id": "clear_camps", "rewardDominionPoints": 0}},
	})
	p := readProfileBody(t, rec)
	if p.DominionPoints != 0 {
		t.Errorf("DominionPoints: want 0, got %d", p.DominionPoints)
	}
	if got := p.CompletedCampaignObjectives["forest/forest_01"]; len(got) != 1 || got[0] != "clear_camps" {
		t.Errorf("objective should still be recorded, got %v", got)
	}
}

// The legacy objectiveIds body shape still records completions (and grants no
// DP, since it carries no reward data).
func TestObjectiveReward_LegacyObjectiveIDsStillWork(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)

	rec := postJSON(t, mux, completeObjectivesPath, testPlayerID, map[string]any{
		"campaignId":   "forest",
		"levelId":      "forest_01",
		"objectiveIds": []string{"clear_camps"},
	})
	p := readProfileBody(t, rec)
	if p.DominionPoints != 0 {
		t.Errorf("legacy shape should grant no DP, got %d", p.DominionPoints)
	}
	if got := p.CompletedCampaignObjectives["forest/forest_01"]; len(got) != 1 {
		t.Errorf("legacy shape should still record completion, got %v", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd server && go test ./internal/http/ -run TestObjectiveReward -v`
Expected: FAIL — DP stays 0 because the handler ignores `objectives`/rewards today.

- [ ] **Step 3: Extend the request body and award logic**

In `server/internal/http/profile_handlers.go`, replace the body struct (442-446) with:

```go
		var body struct {
			CampaignID   string   `json:"campaignId"`
			LevelID      string   `json:"levelId"`
			ObjectiveIDs []string `json:"objectiveIds"` // legacy shape: IDs only, no reward
			Objectives   []struct {
				ID                   string `json:"id"`
				RewardDominionPoints int    `json:"rewardDominionPoints"`
			} `json:"objectives"`
		}
```

Then replace the `WithLocked` mutation body (the block from `set := p.CompletedCampaignObjectives[key]` through `p.CompletedCampaignObjectives[key] = set`) with:

```go
			set := p.CompletedCampaignObjectives[key]
			existing := make(map[string]bool, len(set))
			for _, id := range set {
				existing[id] = true
			}

			// Unify the legacy IDs-only shape with the new reward-carrying
			// shape. Legacy entries carry a zero reward.
			type incoming struct {
				id     string
				reward int
			}
			items := make([]incoming, 0, len(body.ObjectiveIDs)+len(body.Objectives))
			for _, id := range body.ObjectiveIDs {
				items = append(items, incoming{id: id})
			}
			for _, o := range body.Objectives {
				items = append(items, incoming{id: o.ID, reward: o.RewardDominionPoints})
			}

			earned := 0
			for _, it := range items {
				if it.id == "" || existing[it.id] {
					continue // blank, or already completed on a prior play — no re-award
				}
				existing[it.id] = true
				set = addToSortedSet(set, it.id)
				if it.reward > 0 {
					earned += it.reward
				}
			}
			p.CompletedCampaignObjectives[key] = set

			// First-completion Dominion Point reward. Credited in the SAME
			// locked mutation as the set merge so the "is this ID new?"
			// decision and the award are atomic — mirrors CommitDominionPoints.
			if earned > 0 {
				p.DominionPoints += earned
				p.LifetimeDominionPoints += earned
			}
```

- [ ] **Step 4: Run the new tests to verify they pass**

Run: `cd server && go test ./internal/http/ -run TestObjectiveReward -v`
Expected: PASS (all five).

- [ ] **Step 5: Run the existing complete-objectives suite (legacy shape regression)**

Run: `cd server && go test ./internal/http/`
Expected: PASS — the existing `TestCompleteObjectives_*` tests use `objectiveIds` and must still pass unchanged.

- [ ] **Step 6: Checkpoint** — stop for user review/commit.

---

## Task 4: Client — add the reward field to the mirror types

**Files:**
- Modify: `client/src/game-portal/src/game/network/protocol.ts` (`MapCampaignObjective` 376-383; `ObjectiveSnapshot` 1232-1247)
- Modify: `client/src/game-portal/src/types/campaign.ts` (`Objective` 27-43; `ObjectiveProgress` 49-59)

- [ ] **Step 1: Add to `protocol.ts` `MapCampaignObjective`**

After the `required?: boolean` line inside `MapCampaignObjective`:

```ts
  /** DP reward granted the first time (ever, per player) this objective is
   *  completed. Absent/0 = no reward. Mirrors `RewardDominionPoints` on the
   *  server's `protocol.MapCampaignObjective`. */
  rewardDominionPoints?: number
```

- [ ] **Step 2: Add to `protocol.ts` `ObjectiveSnapshot`**

After the `failed?: boolean` line inside `ObjectiveSnapshot` (the per-tick wire type at 1232):

```ts
  /** DP reward this objective grants on first-ever completion. Echoed back to
   *  the server with the match-end completion POST. Absent/0 = no reward. */
  rewardDominionPoints?: number
```

- [ ] **Step 3: Add to `campaign.ts` `Objective` and `ObjectiveProgress`**

In `Objective` (after `required?`):

```ts
  /** DP reward granted the first time (ever, per player) this objective is
   *  completed. Absent/0 = no reward. */
  rewardDominionPoints?: number
```

In `ObjectiveProgress` (after `failed?`):

```ts
  /** DP reward for first-ever completion; mirrors the server snapshot. */
  rewardDominionPoints?: number
```

- [ ] **Step 4: Type-check**

Run: `cd client/src/game-portal && npx vue-tsc -b`
Expected: no errors.

- [ ] **Step 5: Checkpoint** — stop for user review/commit.

---

## Task 5: Client — map editor "DP Reward" input

**Files:**
- Modify: `client/src/game-portal/src/components/MapEditorPanel.vue` (objective meta row ~316-332; `addObjective` 2614-2627)

- [ ] **Step 1: Add the input to the objective card**

In `MapEditorPanel.vue`, inside the `campaign-objective__meta` row (after the closing `</label>` of the Required checkbox at ~331, before the row's closing `</div>` at 332), add:

```html
                  <label class="campaign-objective__reward">
                    <span>DP Reward <span class="field-hint">(first completion)</span></span>
                    <input
                      type="number" min="0"
                      :value="obj.rewardDominionPoints ?? 0"
                      @input="updateObjective(idx, { rewardDominionPoints: Math.max(0, Math.floor(+($event.target as HTMLInputElement).value || 0)) })"
                    />
                  </label>
```

> Note: do NOT add any `cursor:` declaration to the new `.campaign-objective__reward` style — the global cursor rules cover it (project convention). A scoped style block for spacing/label layout is fine if the adjacent objective styles use one; match the existing `.campaign-objective__required` style.

- [ ] **Step 2: Default the field in `addObjective()`**

In `addObjective()` (2618-2625), add `rewardDominionPoints: 0,` to the pushed object literal:

```ts
  objectives.push({
    id: `objective_${objectives.length + 1}`,
    type: defaultType,
    description: '',
    scope: 'team',
    required: false,
    rewardDominionPoints: 0,
    config: emptyObjectiveConfig(defaultType),
  })
```

- [ ] **Step 3: Type-check**

Run: `cd client/src/game-portal && npx vue-tsc -b`
Expected: no errors. (`updateObjective`'s `Partial<MapCampaignObjective>` already accepts the new key from Task 4.)

- [ ] **Step 4: Manual round-trip verification**

Run the editor (`npm run dev`), add an objective, set DP Reward = 25, save the map, reload it, and confirm the value persists to `25`. The field is a plain property on the objective object, so it serializes with the rest of the campaign block — confirm no serializer drops it.

Expected: the reloaded objective shows `25`.

- [ ] **Step 5: Checkpoint** — stop for user review/commit.

---

## Task 6: Client — send reward amounts with the completion POST

**Files:**
- Modify: `client/src/game-portal/src/services/profileApi.ts` (`markCampaignObjectivesComplete` 144-155)
- Modify: `client/src/game-portal/src/views/MatchEnd.vue` (`onClose` 65-91)

- [ ] **Step 1: Change the API function signature + body**

In `profileApi.ts`, replace `markCampaignObjectivesComplete` (144-155) with:

```ts
export async function markCampaignObjectivesComplete(
  campaignId: string,
  levelId: string,
  objectives: { id: string; rewardDominionPoints?: number }[],
): Promise<PlayerProfile> {
  const res = await fetch(`${API_BASE}/api/profile/campaign/complete-objectives`, {
    method: 'POST',
    headers: playerHeaders(),
    body: JSON.stringify({ campaignId, levelId, objectives }),
  })
  return handleResponse<PlayerProfile>(res)
}
```

(Update the doc comment above it to say it now carries per-objective first-completion DP rewards; the server credits DP for objective IDs newly added to the persistent set.)

- [ ] **Step 2: Build the payload from the match-end snapshot**

In `MatchEnd.vue` `onClose` (65-84), replace the `completedIDs` block and the `markCampaignObjectivesComplete(...)` call:

```ts
    const completedObjectives = snap.objectives
      .filter((o) => o.completed && !o.failed)
      .map((o) => ({ id: o.id, rewardDominionPoints: o.rewardDominionPoints ?? 0 }))
    try {
      await markCampaignObjectivesComplete(
        session.campaignId,
        session.levelId,
        completedObjectives,
      )
      // Refresh the profile so the Campaign panel's level-select rows AND the
      // Dominion Point balance pick up the first-completion reward before the
      // user navigates back.
      await refreshProfile()
    } catch (err) {
      console.error('[Campaign] failed to record completed objectives:', err)
    }
```

> Note: no `isRemoteProxyClient()` branch is needed here. This POST already targets the caller's own local server (`API_BASE`), so the DP award lands on the correct per-machine profile for both host and remote joiner — the same reason the existing objective-completion tracking works uniformly.

- [ ] **Step 3: Type-check**

Run: `cd client/src/game-portal && npx vue-tsc -b`
Expected: no errors. Confirm no other caller of `markCampaignObjectivesComplete` exists (grep already shows `MatchEnd.vue` is the sole caller; the reference in `Match.vue:113` is a comment).

- [ ] **Step 4: Run the client test suite**

Run: `cd client/src/game-portal && npm run test`
Expected: PASS (no regressions).

- [ ] **Step 5: Checkpoint** — stop for user review/commit.

---

## Final verification

- [ ] **Full server suite:** `cd server && go test ./...` → PASS
- [ ] **Client type-check:** `cd client/src/game-portal && npx vue-tsc -b` → clean
- [ ] **Client tests:** `cd client/src/game-portal && npm run test` → PASS
- [ ] **Manual end-to-end:** author a map objective with DP Reward = N; play it and complete the objective; confirm the recap → menu shows the DP balance increased by N; replay and complete again; confirm the balance does **not** increase a second time.

---

## Spec coverage check

- First-time-ever (persistent) semantics → Task 3 (award keyed on newly-added-to-set). ✔
- One DP field per objective, all types → Task 1 (field on `ObjectiveDef`, type-agnostic) + Task 5 (editor input on every card). ✔
- Editor configurable → Task 5. ✔
- Reward amount reaches match-end client → Task 2 (snapshot) + Task 4 (client types). ✔
- Award path atomic/idempotent → Task 3 (single locked mutation, membership guard). ✔
- Host + remote joiner uniform (per-machine) → Task 6 note (no proxy branch). ✔
- Team-scope = per-player reward → falls out of per-player persistent set; no code needed (verified by design). ✔
- Non-negative validation → Task 1. ✔
- Existing maps/profiles unaffected; no retroactive grants → Task 3 (legacy shape + only-new awarded) tests. ✔
