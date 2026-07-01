# Conquest Badges Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

> **Project rule — NO git commits by the tooling.** The user stages/commits. Do NOT run `git add`/`git commit`. "Checkpoint" = stop for the user.

**Goal:** Add a new persistent currency, **Conquest Badges**, earned by completing objectives and consumed (1 each) when purchasing "big" (`kind: "major"`) advancements.

**Design decisions (locked with user):**
- "Big" advancement = the existing `kind: "major"` node. No new per-node field, no catalog JSON edits.
- Buying a major advancement **consumes 1 badge** in addition to its DP cost, and is **gated** (can't buy a major node with 0 badges). Resetting advancements **refunds** the badge.
- Badges are earned **only** via objectives — a per-objective `rewardConquestBadges` reward (default 0, editable in the map editor), riding the existing `/api/profile/campaign/complete-objectives` match-end endpoint (the same path built for `rewardDominionPoints`).
- Balance only (no `LifetimeConquestBadges`).
- A dev-grant endpoint/button is included because objectives are otherwise the sole source (needed to test the advancement gate).

**Architecture:** Mirror the `rewardDominionPoints` objective-reward path for earning; extend the advancement purchase/reset handlers for spending. This reuses the atomic, idempotent complete-objectives award logic already in place.

**Commands:**
- Server tests: `cd server && go test ./internal/http/... ./internal/game/... ./pkg/protocol/...`
- Client type-check: `cd "client/src/game-portal" && npx vue-tsc -b`
- Client tests: `cd "client/src/game-portal" && npm run test`

---

## File Structure

**Server:**
- `server/internal/profile/types.go` — `ConquestBadges int` on `PlayerProfile`; `BadgesPaid int` on `AcquiredAdvancement`.
- `server/internal/game/objective_defs.go` — `RewardConquestBadges int` on `ObjectiveDef` + non-negative validation.
- `server/pkg/protocol/messages.go` — `RewardConquestBadges` on `MapCampaignObjective` and `ObjectiveSnapshot`.
- `server/internal/game/campaign_defs.go` — carry through conversion.
- `server/internal/game/objective_runtime.go` — populate on snapshot.
- `server/internal/http/profile_handlers.go` — complete-objectives awards badges; add dev grant-conquest-badges endpoint.
- `server/internal/http/advancement_handlers.go` — purchase gate/consume + reset/stale refund of badges; responses carry `conquestBadges`.

**Client:**
- `client/src/game-portal/src/types/profile.ts` — `conquestBadges` on `PlayerProfile`; `badgesPaid?` on `AcquiredAdvancement`; `conquestBadges` on `PurchaseAdvancementResponse` (in profileApi.ts).
- `client/src/game-portal/src/game/network/protocol.ts` — `rewardConquestBadges?` on `MapCampaignObjective` + `ObjectiveSnapshot`.
- `client/src/game-portal/src/types/campaign.ts` — `rewardConquestBadges?` on `Objective` + `ObjectiveProgress`.
- `client/src/game-portal/src/components/MapEditorPanel.vue` — "Conquest Badge Reward" input.
- `client/src/game-portal/src/services/profileApi.ts` — send `rewardConquestBadges`; `PurchaseAdvancementResponse.conquestBadges`; `devGrantConquestBadges`.
- `client/src/game-portal/src/views/MatchEnd.vue` — include badge reward in payload.
- `client/src/game-portal/src/composables/useAdvancements.ts` — badge balance + `canAcquire` gate + sync on purchase/reset.
- `client/src/game-portal/src/views/Advancements.vue` — gate major nodes on badge; tooltip/label.
- `client/src/game-portal/src/components/menu/MenuDominionPanel.vue` + `views/ProfileView.vue` — display badge balance (+ dev-grant button).

---

## Task S1: Server — profile data model

**Files:** Modify `server/internal/profile/types.go`. No standalone test (behavior is covered by S3/S4 handler tests).

- [ ] **Step 1:** In `PlayerProfile` (after `LifetimeDominionPoints`, line 19) add:

```go
	// ConquestBadges is a persistent spendable currency earned by completing
	// map objectives and consumed (1 each) when purchasing "major" unit
	// advancements. Balance-only (no lifetime counter). A new int field
	// defaults to 0 on load for existing profiles, which is the correct
	// starting balance — no migration/version bump required (same reasoning
	// as CreditedMatchIDs).
	ConquestBadges int `json:"conquestBadges"`
```

- [ ] **Step 2:** In `AcquiredAdvancement` (after `CostPaid`, line 87) add:

```go
	// BadgesPaid is the number of Conquest Badges consumed at purchase time
	// (1 for major advancements, 0 for minor). Stored so reset / removal
	// refunds return the correct badge count. omitempty keeps existing
	// records unchanged on re-serialize.
	BadgesPaid int `json:"badgesPaid,omitempty"`
```

- [ ] **Step 3:** Build to confirm it compiles: `cd server && go build ./...` → clean.
- [ ] **Step 4: Checkpoint.**

---

## Task S2: Server — objective badge-reward field (mirror of RewardDominionPoints)

**Files:**
- Modify: `server/internal/game/objective_defs.go`, `server/pkg/protocol/messages.go`, `server/internal/game/campaign_defs.go`, `server/internal/game/objective_runtime.go`
- Test: `server/internal/game/conquest_badge_objective_test.go` (new)

- [ ] **Step 1: Write failing tests.** Create `server/internal/game/conquest_badge_objective_test.go`:

```go
package game

import "testing"

func TestObjectiveBadgeReward_PositivePreserved(t *testing.T) {
	def := parseAndValidateObjectiveDef("test.json", "test_level", ObjectiveDef{
		ID:                   "clear_camps",
		Type:                 "kill_camps",
		Config:               []byte(`{"count":1}`),
		RewardConquestBadges: 2,
	})
	if def.RewardConquestBadges != 2 {
		t.Fatalf("RewardConquestBadges: want 2, got %d", def.RewardConquestBadges)
	}
}

func TestObjectiveBadgeReward_NegativeRejected(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic for negative rewardConquestBadges, got none")
		}
	}()
	parseAndValidateObjectiveDef("test.json", "test_level", ObjectiveDef{
		ID:                   "clear_camps",
		Type:                 "kill_camps",
		Config:               []byte(`{"count":1}`),
		RewardConquestBadges: -1,
	})
}

func TestObjectiveSnapshot_CarriesRewardConquestBadges(t *testing.T) {
	// Construct GameState the SAME way TestObjectiveSnapshot_CarriesRewardDominionPoints
	// does in snapshot_objectives_test.go — copy that setup. Then:
	s.Objectives = []objectiveRuntime{
		{
			Def:       ObjectiveDef{ID: "clear_camps", Type: "kill_camps", Scope: ObjectiveScopeTeam, RewardConquestBadges: 3},
			TeamState: ObjectiveState{ObjectiveID: "clear_camps", Scope: ObjectiveScopeTeam},
		},
	}
	snap := s.buildVictorySnapshotForViewerLocked("")
	if snap == nil {
		t.Fatal("expected snapshot, got nil")
	}
	got := findObjective(snap.Objectives, "clear_camps")
	if got.RewardConquestBadges != 3 {
		t.Fatalf("RewardConquestBadges: want 3, got %d", got.RewardConquestBadges)
	}
}
```

> Implementer: read snapshot_objectives_test.go and reuse its exact GameState constructor + `findObjective` helper for the third test (do not invent an API).

- [ ] **Step 2:** Run, confirm fail: `cd server && go test ./internal/game/ -run 'TestObjectiveBadgeReward|TestObjectiveSnapshot_CarriesRewardConquestBadges' -v`

- [ ] **Step 3:** `objective_defs.go` — add to `ObjectiveDef` (after `RewardDominionPoints`):

```go
	// RewardConquestBadges is the Conquest Badge reward granted the first time
	// (ever, per player) this objective is completed. 0 / omitted = no reward.
	RewardConquestBadges int `json:"rewardConquestBadges,omitempty"`
```

And in `parseAndValidateObjectiveDef`, next to the existing `RewardDominionPoints < 0` guard:

```go
	if raw.RewardConquestBadges < 0 {
		panic("catalog\\campaigns\\" + filename + ": level " + levelID +
			": objective " + raw.ID + ": rewardConquestBadges must be >= 0")
	}
```

- [ ] **Step 4:** `messages.go` — add `RewardConquestBadges int \`json:"rewardConquestBadges,omitempty"\`` to BOTH `MapCampaignObjective` (after its `RewardDominionPoints`) and `ObjectiveSnapshot` (after its `RewardDominionPoints`). Keep gofmt tag alignment.

- [ ] **Step 5:** `campaign_defs.go` — in the `def := ObjectiveDef{...}` literal add `RewardConquestBadges: raw.RewardConquestBadges,`.

- [ ] **Step 6:** `objective_runtime.go` — in the `protocol.ObjectiveSnapshot{...}` literal add `RewardConquestBadges: runtime.Def.RewardConquestBadges,`.

- [ ] **Step 7:** Run, confirm pass: same command as Step 2, then `cd server && go test ./internal/game/ ./pkg/protocol/...`.

- [ ] **Step 8: Checkpoint.**

---

## Task S3: Server — award badges in complete-objectives endpoint

**Files:**
- Modify: `server/internal/http/profile_handlers.go` (complete-objectives handler; body struct + the WithLocked mutation with the existing DP `earned` logic)
- Test: `server/internal/http/conquest_badge_award_test.go` (new; reuse helpers `newTestMux`, `seedPlayer`, `postJSON`, `readProfileBody`, `completeObjectivesPath`, `testPlayerID`)

- [ ] **Step 1: Write failing tests.** Create `server/internal/http/conquest_badge_award_test.go`:

```go
package httpserver

import (
	"net/http"
	"testing"
)

func TestConquestBadge_FirstCompletionAwards(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)
	rec := postJSON(t, mux, completeObjectivesPath, testPlayerID, map[string]any{
		"campaignId": "forest", "levelId": "forest_01",
		"objectives": []map[string]any{
			{"id": "clear_camps", "rewardConquestBadges": 2},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if p := readProfileBody(t, rec); p.ConquestBadges != 2 {
		t.Errorf("ConquestBadges: want 2, got %d", p.ConquestBadges)
	}
}

func TestConquestBadge_RepeatAwardsNothing(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)
	body := map[string]any{
		"campaignId": "forest", "levelId": "forest_01",
		"objectives": []map[string]any{{"id": "clear_camps", "rewardConquestBadges": 2}},
	}
	_ = postJSON(t, mux, completeObjectivesPath, testPlayerID, body)
	second := postJSON(t, mux, completeObjectivesPath, testPlayerID, body)
	if p := readProfileBody(t, second); p.ConquestBadges != 2 {
		t.Errorf("ConquestBadges after repeat: want 2, got %d", p.ConquestBadges)
	}
}

// DP and badge rewards on the same objective batch both credit.
func TestConquestBadge_CoexistsWithDominionReward(t *testing.T) {
	mux, pm := newTestMux(t)
	seedPlayer(t, pm, 0, nil)
	rec := postJSON(t, mux, completeObjectivesPath, testPlayerID, map[string]any{
		"campaignId": "forest", "levelId": "forest_01",
		"objectives": []map[string]any{
			{"id": "clear_camps", "rewardDominionPoints": 30, "rewardConquestBadges": 1},
		},
	})
	p := readProfileBody(t, rec)
	if p.DominionPoints != 30 || p.ConquestBadges != 1 {
		t.Errorf("want DP=30 badges=1, got DP=%d badges=%d", p.DominionPoints, p.ConquestBadges)
	}
}
```

- [ ] **Step 2:** Run, confirm fail: `cd server && go test ./internal/http/ -run TestConquestBadge -v`

- [ ] **Step 3:** In the complete-objectives handler, extend the `Objectives` body struct entry to also decode the badge reward:

```go
			Objectives   []struct {
				ID                   string `json:"id"`
				RewardDominionPoints int    `json:"rewardDominionPoints"`
				RewardConquestBadges int    `json:"rewardConquestBadges"`
			} `json:"objectives"`
```

Extend the `incoming` unify-loop to carry a badge field, accumulate `earnedBadges` alongside the existing `earned`, and credit inside the same lock. Concretely: add `badges int` to the `incoming` struct; when building items from `body.Objectives`, set `badges: o.RewardConquestBadges` (legacy `objectiveIds` entries get 0); inside the per-item loop (only for newly-added ids) add `if it.badges > 0 { earnedBadges += it.badges }`; after the loop:

```go
			if earnedBadges > 0 {
				p.ConquestBadges += earnedBadges
			}
```

(Place `earnedBadges := 0` next to the existing `earned := 0`. Do NOT re-award for ids already in `existing` — the same membership guard covers badges.)

- [ ] **Step 4:** Run, confirm pass: `cd server && go test ./internal/http/ -run TestConquestBadge -v`
- [ ] **Step 5:** Full suite (DP + legacy tests still green): `cd server && go test ./internal/http/`
- [ ] **Step 6: Checkpoint.**

---

## Task S4: Server — advancement badge gate, consume, and refund

**Files:**
- Modify: `server/internal/http/advancement_handlers.go` (purchase handler, reset handler, `refundStaleAdvancementCosts`)
- Test: `server/internal/http/advancement_badge_test.go` (new)

Behavior:
- Purchase of a `node.Kind == "major"` advancement requires `p.ConquestBadges >= 1`; else reject `insufficient_conquest_badges` (400). On success, deduct 1 badge and record `BadgesPaid: 1`. Minor nodes: no badge cost, `BadgesPaid: 0`.
- Both `purchaseResponse` and `resetResponse` gain `ConquestBadges int \`json:"conquestBadges"\``.
- Reset refunds `aa.BadgesPaid` to `p.ConquestBadges` for each acquired advancement.
- `refundStaleAdvancementCosts`: when an advancement is removed from the catalog, also refund `aa.BadgesPaid` (badges are only "lost" if the record is dropped).

- [ ] **Step 1: Write failing tests.** Create `server/internal/http/advancement_badge_test.go`. First READ an existing advancement handler test (e.g. `advancement_handlers_test.go`) to learn the exact helper names for seeding a profile with a chosen DP/badge balance and for finding a real "major" vs "minor" advancement id in the loaded catalog. Then write tests asserting:
  1. Buying a **major** node with 0 badges → 400 `insufficient_conquest_badges`, no DP/badge change.
  2. Buying a **major** node with 1 badge + enough DP → 200, `conquestBadges` in response is 0, DP debited, advancement acquired with `BadgesPaid == 1`.
  3. Buying a **minor** node with 0 badges + enough DP → 200 (no badge needed).
  4. Reset after buying a major node → the badge is refunded (`conquestBadges` back to its pre-purchase value).

Derive DP costs and the major/minor ids from the loaded catalog (do NOT hardcode balance numbers — per project test rules, read `game.GetAdvancementDef`/catalog or pick ids by `Kind`). If the test helpers can't set a starting badge balance directly, grant via `pm.WithLocked` in the test setup.

- [ ] **Step 2:** Run, confirm fail: `cd server && go test ./internal/http/ -run TestAdvancementBadge -v`

- [ ] **Step 3:** In the purchase handler: add `ConquestBadges int \`json:"conquestBadges"\`` to `purchaseResponse`. After the DP-sufficiency check (line 86-89), add:

```go
			if node.Kind == "major" && p.ConquestBadges < 1 {
				writeJSONError(w, http.StatusBadRequest, "insufficient_conquest_badges", "a Conquest Badge is required to purchase this advancement")
				return errAbort
			}
```

Then in the success path, after `p.DominionPoints -= node.Cost`:

```go
			badgesPaid := 0
			if node.Kind == "major" {
				p.ConquestBadges -= 1
				badgesPaid = 1
			}
```

Set `BadgesPaid: badgesPaid` in the appended `profile.AcquiredAdvancement{...}`, and set `ConquestBadges: p.ConquestBadges` in `resp`.

- [ ] **Step 4:** In the reset handler: add `ConquestBadges int \`json:"conquestBadges"\`` to `resetResponse`; in the refund loop add `p.ConquestBadges += aa.BadgesPaid`; set `ConquestBadges: p.ConquestBadges` in `resp`.

- [ ] **Step 5:** In `refundStaleAdvancementCosts`, in the "advancement removed from catalog" branch (after `p.DominionPoints += aa.CostPaid`), add `p.ConquestBadges += aa.BadgesPaid`.

- [ ] **Step 6:** Run, confirm pass: `cd server && go test ./internal/http/ -run TestAdvancementBadge -v`
- [ ] **Step 7:** Full suite: `cd server && go test ./internal/http/`
- [ ] **Step 8: Checkpoint.**

---

## Task S5: Server — dev grant endpoint for badges

**Files:** Modify `server/internal/http/profile_handlers.go` (mirror the existing `/api/profile/dev/grant-dominion-points`).
Test: add one case to `server/internal/http/profile_award_dominion_test.go` or a new small test.

- [ ] **Step 1:** Read the existing `/api/profile/dev/grant-dominion-points` handler in profile_handlers.go. Add a sibling `POST /api/profile/dev/grant-conquest-badges` with body `{ "amount": int }` that does `p.ConquestBadges += amount` (guard amount > 0) inside `pm.WithLocked` and returns the updated profile (same response shape as the DP dev grant).
- [ ] **Step 2:** Write a test: POST amount 3 → profile `ConquestBadges == 3`. Run and confirm pass.
- [ ] **Step 3:** `cd server && go test ./internal/http/` → PASS.
- [ ] **Step 4: Checkpoint.**

---

## Task C1: Client — types

**Files:** Modify `client/src/game-portal/src/types/profile.ts`, `client/src/game-portal/src/game/network/protocol.ts`, `client/src/game-portal/src/types/campaign.ts`, `client/src/game-portal/src/services/profileApi.ts`.

- [ ] **Step 1:** `types/profile.ts`:
  - `PlayerProfile`: after `lifetimeDominionPoints`, add `conquestBadges: number`.
  - `AcquiredAdvancement`: add `badgesPaid?: number`.
- [ ] **Step 2:** `profileApi.ts` `PurchaseAdvancementResponse` (line 93-96): add `conquestBadges: number`.
- [ ] **Step 3:** `protocol.ts`: add `rewardConquestBadges?: number` to `MapCampaignObjective` (after `rewardDominionPoints?`) and to `ObjectiveSnapshot` (after `rewardDominionPoints?`).
- [ ] **Step 4:** `campaign.ts`: add `rewardConquestBadges?: number` to `Objective` and `ObjectiveProgress` (after each `rewardDominionPoints?`).
- [ ] **Step 5:** Type-check: `cd "client/src/game-portal" && npx vue-tsc -b` → clean.
- [ ] **Step 6: Checkpoint.**

---

## Task C2: Client — objective badge reward wiring (mirror of DP reward)

**Files:** Modify `MapEditorPanel.vue`, `services/profileApi.ts`, `views/MatchEnd.vue`.

- [ ] **Step 1:** `MapEditorPanel.vue`: next to the existing "DP Reward" input in the `.campaign-objective__meta` row, add a "Badge Reward" number input (clamped non-negative int) bound to `obj.rewardConquestBadges`:

```html
                    <label class="campaign-objective__reward">
                      <span>Badge Reward <span class="field-hint">(first completion)</span></span>
                      <input
                        type="number" min="0"
                        :value="obj.rewardConquestBadges ?? 0"
                        @input="updateObjective(idx, { rewardConquestBadges: Math.max(0, Math.floor(+($event.target as HTMLInputElement).value || 0)) })"
                      />
                    </label>
```

In `addObjective()`, add `rewardConquestBadges: 0,` to the pushed literal. No `cursor:` CSS.

- [ ] **Step 2:** `profileApi.ts` `markCampaignObjectivesComplete`: widen the `objectives` param type to `{ id: string; rewardDominionPoints?: number; rewardConquestBadges?: number }[]` (body already sends `objectives` verbatim, no other change).

- [ ] **Step 3:** `MatchEnd.vue` `onClose`: extend the mapped payload to include the badge reward:

```ts
      const completedObjectives = snap.objectives
        .filter((o) => o.completed && !o.failed)
        .map((o) => ({
          id: o.id,
          rewardDominionPoints: o.rewardDominionPoints ?? 0,
          rewardConquestBadges: o.rewardConquestBadges ?? 0,
        }))
```

(The comment about the refresh already covers picking up new balances.)

- [ ] **Step 4:** Type-check: `cd "client/src/game-portal" && npx vue-tsc -b` → clean.
- [ ] **Step 5:** Editor round-trip manual check: add objective, set Badge Reward = 1, save, reload, value persists.
- [ ] **Step 6: Checkpoint.**

---

## Task C3: Client — advancement badge gate + balance display + dev grant

**Files:** Modify `composables/useAdvancements.ts`, `views/Advancements.vue`, `components/menu/MenuDominionPanel.vue`, `views/ProfileView.vue`, `services/profileApi.ts`.

- [ ] **Step 1:** `useAdvancements.ts`:
  - Add `const conquestBadges = computed<number>(() => profile.value?.conquestBadges ?? 0)`.
  - Add a gate that also checks the badge for major nodes:
    ```ts
    function canAcquire(node: UnitAdvancementTrack['nodes'][number]): boolean {
      if (!canAfford(node.cost)) return false
      if (node.kind === 'major' && conquestBadges.value < 1) return false
      return true
    }
    ```
  - In `purchase()` and `reset()` success blocks, also sync the badge balance: `p.conquestBadges = updated.conquestBadges`.
  - Export `conquestBadges` and `canAcquire`.

- [ ] **Step 2:** `Advancements.vue`:
  - Pull `canAcquire` (and `conquestBadges` if needed) from `useAdvancements()`.
  - Button `:disabled` (line 50): replace `!canAfford(node.cost)` with `!canAcquire(node)`.
  - `nodeStateClass` (231-240): when available, use `canAcquire(node)` instead of `canAfford(node.cost)` to decide `available` vs `unaffordable`.
  - `nodeStateLabel` (242-247): when available and not acquirable, if it's a major node lacking a badge, say `'requires a Conquest Badge'`; otherwise keep `'not enough Dominion Points'`.
  - `tooltipBody` (249-256): for `node.kind === 'major'`, append a line `Requires: 1 Conquest Badge`.

- [ ] **Step 3:** `profileApi.ts`: add `devGrantConquestBadges(amount: number): Promise<PlayerProfile>` mirroring `devGrantDominionPoints`, POSTing to `/api/profile/dev/grant-conquest-badges`.

- [ ] **Step 4:** Display balance. In `MenuDominionPanel.vue`, add a second readout under the Dominion Points value for Conquest Badges (`profile.value?.conquestBadges ?? 0`), reusing the existing styles (add a small `menu-dominion__badges` line or a second header/value pair). In `ProfileView.vue`, add a Conquest Badges card/line next to the Dominion Points card showing `profile.conquestBadges.toLocaleString()`, plus a `+1 Badge (dev)` button calling `devGrantConquestBadges(1)` then refreshing the profile (mirror `grantDevDominionPoints`).

- [ ] **Step 5:** Type-check + tests: `cd "client/src/game-portal" && npx vue-tsc -b` (clean) and `npm run test` (pass).
- [ ] **Step 6: Checkpoint.**

---

## Final verification

- [ ] `cd server && go test ./internal/http/... ./internal/game/... ./pkg/protocol/...` → PASS
- [ ] `cd "client/src/game-portal" && npx vue-tsc -b` → clean
- [ ] `cd "client/src/game-portal" && npm run test` → PASS
- [ ] Manual E2E: dev-grant a badge; open Advancements — a major node is now purchasable and consumes the badge; with 0 badges a major node is gated (locked/labelled), minor nodes unaffected; reset returns the badge. Author an objective with Badge Reward = 1, complete it, confirm the badge balance rises (and not again on replay).

## Spec coverage

- New currency earned by objectives → S2 (reward field) + S3 (award). ✔
- Default 0 → omitempty + zero-value default; editor defaults 0 (C2). ✔
- Big (major) advancements require 1 badge, consumed, gated → S4 + C3. ✔
- Refund symmetry (reset / catalog removal) → S4. ✔
- Configurable in map editor → C2. ✔
- Balance visible + testable → C3 display + S5/C3 dev grant. ✔
