# Multiplayer Recap Roster + Joiner Dominion Points Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix two multiplayer bugs for the remote joiner (Player 2): (1) the end-of-match recap shows only the joiner's own stats row, and (2) the joiner receives no dominion points.

**Architecture:** Multiplayer is host-authoritative P2P over Steam — the joiner's `join_match` and all simulation run on the **host's** server, and the joiner is a thin client of the host's snapshots. The host's per-kill dominion-point commit therefore lands on the **host's** local profile store, never on the joiner's machine. Fix: (a) the server accumulates each player's per-match earned dominion points and delivers each viewer their **own** total inside the game-over snapshot; (b) only a **remote joiner** persists that total to its own local profile (the host keeps the existing, correct server-side commit, so there is no double-count); (c) the client **freezes** the final player roster the moment game-over is first observed, so connection teardown can't clobber the recap's data source.

**Tech Stack:** Go (server, `net/http`, `encoding/json`, standard `testing`); TypeScript / Vue 3 (client, Vitest); newline-delimited JSON snapshots over WebSocket.

**Key design facts (verified):**
- A player's in-match ID, the HTTP `X-Player-ID` header, and the profile key are the **same UUID** ([NetworkClient.ts:110-118](../../../client/src/game-portal/src/game/network/NetworkClient.ts#L110-L118)). So when the joiner persists its own earned DP over HTTP, it lands on the exact profile its menu reads.
- Remote-joiner detection: `sessionStorage.getItem(STEAM_PROXY_FLAG_KEY) === '1'` (Steam) or a direct-connect proxy token ([NetworkClient.ts:84-99](../../../client/src/game-portal/src/game/network/NetworkClient.ts#L84-L99)).
- The host's existing per-kill / match-end commit ([manager.go:66-95](../../../server/internal/game/manager.go#L66-L95)) is correct for any profile that lives on *this* machine (single-player and the host player). It is left **unchanged**.
- **Scope limit (documented):** earned DP rides on `GameOverSnapshot`, which is present whenever a player has lost all townhalls (the PvP / elimination case this bug was reported in). A co-op match won purely by an objective with *no* human eliminated produces no `GameOverSnapshot`; a remote joiner in that narrow case would still not get client-persisted DP. Out of scope here — noted as a follow-up.

---

## File Structure

**Server (Go):**
- `server/internal/game/state.go` — add `MatchDominionPointsEarned` to `Player`; populate the new wire field in the two per-viewer game-over branches.
- `server/internal/game/dominion_points.go` — accumulate `MatchDominionPointsEarned` on every successful drop roll, independent of commit mode.
- `server/pkg/protocol/messages.go` — add `YourDominionPointsEarned` to `GameOverSnapshot`.
- `server/internal/profile/types.go` — add `CreditedMatchIDs` to `PlayerProfile`.
- `server/internal/http/profile_handlers.go` — new `POST /api/profile/match/award-dominion-points` endpoint (idempotent by matchId).

**Client (TS/Vue):**
- `client/src/game-portal/src/game/network/protocol.ts` — add `yourDominionPointsEarned?` to `GameOverSnapshot`.
- `client/src/game-portal/src/services/profileApi.ts` — `awardMatchDominionPoints()` + `isRemoteProxyClient()` helper.
- `client/src/game-portal/src/game/core/GameState.ts` — freeze final roster + earned DP on first game-over.
- `client/src/game-portal/src/game/core/GameClient.ts` — expose frozen roster + earned DP on the UI state.
- `client/src/game-portal/src/views/Match.vue` — capture the frozen roster (not live `playerSnapshots`) into the recap snapshot.
- `client/src/game-portal/src/state/matchEndState.ts` — carry `dominionPointsEarned` + `matchId` + a one-shot `dpPersisted` guard.
- `client/src/game-portal/src/views/MatchEnd.vue` — persist earned DP locally for a remote joiner, once, on mount.

---

## Task 1: Server — accumulate per-match earned dominion points

**Files:**
- Modify: `server/internal/game/state.go:613-615` (Player struct)
- Modify: `server/internal/game/dominion_points.go:47-62`
- Test: `server/internal/game/dominion_points_test.go`

- [ ] **Step 1: Write the failing test**

Add to `server/internal/game/dominion_points_test.go`:

```go
// TestMatchDominionPointsEarned_AccumulatesInImmediateMode verifies the
// always-on per-match earned counter increments on a successful drop even in
// immediate commit mode (where RunDominionPointDrops is intentionally left at
// zero). This counter is what the game-over snapshot reports to each viewer.
func TestMatchDominionPointsEarned_AccumulatesInImmediateMode(t *testing.T) {
	gameplayTuningOnce.Do(func() {})
	prev := gameplayTuningSingleton
	t.Cleanup(func() { gameplayTuningSingleton = prev })
	g := gameplayTuning()
	gameplayTuningSingleton = &g
	gameplayTuningSingleton.DominionPoints.CommitMode = dominionPointCommitModeImmediate

	s := newDPTestState(t)
	const playerID = "p_alpha"
	s.Players[playerID] = &Player{ID: playerID}
	// Wire the immediate hook so the immediate branch does not warn-and-drop.
	s.SetImmediateDominionPointDropHandler(func(string, int) {})

	dead := &Unit{OwnerID: enemyPlayerID, UnitType: "worker"}
	// rngLoot.Float64() < chance must pass; base chance is 1.0 in tuning, but
	// pin a forced-pass by setting the per-unit amount explicitly via def.
	s.rollDominionPointDropLocked(playerID, dead)

	if got := s.Players[playerID].MatchDominionPointsEarned; got <= 0 {
		t.Fatalf("MatchDominionPointsEarned: want > 0 after a successful drop, got %d", got)
	}
	if got := s.Players[playerID].RunDominionPointDrops; got != 0 {
		t.Errorf("RunDominionPointDrops must stay 0 in immediate mode, got %d", got)
	}
}
```

> If `newDPTestState` / `gameplayTuningOnce` / `gameplayTuningSingleton` helper names differ in the existing test file, mirror whatever the sibling tests in `dominion_points_test.go` already use (they set commit mode via `withMatchEndCommitMode(t)` — add a parallel `withImmediateCommitMode(t)` if cleaner). The assertion is what matters: a successful drop increments `MatchDominionPointsEarned` and leaves `RunDominionPointDrops` at 0 in immediate mode.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/game/ -run TestMatchDominionPointsEarned_AccumulatesInImmediateMode -v`
Expected: FAIL — `MatchDominionPointsEarned` is not a field of `Player` (compile error).

- [ ] **Step 3: Add the field**

In `server/internal/game/state.go`, immediately after the `RunDominionPointDrops` field (line 615):

```go
	// RunDominionPointDrops accumulates dominion-point drops during the match.
	// Committed to the profile file at match end.
	RunDominionPointDrops int

	// MatchDominionPointsEarned is the always-on per-match earned total,
	// incremented on every successful drop regardless of commitMode. Unlike
	// RunDominionPointDrops (which stays 0 in immediate mode by design), this
	// is the authoritative per-player total reported to each viewer in the
	// game-over snapshot, for end-of-match display and for a remote joiner to
	// persist into its own local profile (the host commits server-side).
	MatchDominionPointsEarned int
```

- [ ] **Step 4: Accumulate on a successful roll**

In `server/internal/game/dominion_points.go`, change the body of the `if s.rngLoot.Float64() < chance {` block (lines 47-62) so the earned counter increments before the commit-mode branch:

```go
	if s.rngLoot.Float64() < chance {
		// Always-on per-match earned total (independent of commitMode). This is
		// what the game-over snapshot reports to each viewer.
		if player, ok := s.Players[attackerOwnerID]; ok {
			player.MatchDominionPointsEarned += amount
		}

		if tuning.DominionPoints.CommitMode == dominionPointCommitModeImmediate {
			// Fire-and-forget commit; do NOT accumulate RunDominionPointDrops so
			// the match-end commit path sees zero and cannot double-credit.
			if s.onDominionPointDropImmediate != nil {
				s.onDominionPointDropImmediate(attackerOwnerID, amount)
			} else {
				log.Printf("[DP] WARNING: commitMode=immediate but no hook wired; drop is lost (attacker=%s amount=%d)",
					attackerOwnerID, amount)
			}
			return
		}
		if player, ok := s.Players[attackerOwnerID]; ok {
			player.RunDominionPointDrops += amount
		}
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd server && go test ./internal/game/ -run 'TestMatchDominionPointsEarned|Dominion' -v`
Expected: PASS (new test green; all existing dominion tests still green — commit-mode behavior is unchanged).

- [ ] **Step 6: Commit**

```bash
git add server/internal/game/state.go server/internal/game/dominion_points.go server/internal/game/dominion_points_test.go
git commit -m "feat(server): track always-on per-match earned dominion points"
```

---

## Task 2: Server — deliver each viewer their own earned DP in the game-over snapshot

**Files:**
- Modify: `server/pkg/protocol/messages.go:1199-1201`
- Modify: `server/internal/game/state.go:1685-1691` (unfiltered branch) and `:1908-1915` (FOW branch)
- Test: `server/internal/game/snapshot_dominion_test.go` (new)

- [ ] **Step 1: Add the wire field**

In `server/pkg/protocol/messages.go`, extend `GameOverSnapshot`:

```go
type GameOverSnapshot struct {
	LostPlayerIDs []string `json:"lostPlayerIds"`
	// YourDominionPointsEarned is the snapshot viewer's own per-match earned
	// dominion-point total. Per-viewer: each client sees its own number. The
	// host persists this server-side; a remote joiner reads this field to
	// persist into its own local profile (the host can't write the joiner's
	// profile, which lives on a different machine).
	YourDominionPointsEarned int `json:"yourDominionPointsEarned,omitempty"`
}
```

- [ ] **Step 2: Write the failing test**

Create `server/internal/game/snapshot_dominion_test.go`:

```go
package game

import "testing"

// TestSnapshotForPlayer_CarriesViewerEarnedDP verifies that the per-viewer
// snapshot's GameOver block reports the VIEWER's own MatchDominionPointsEarned,
// not another player's.
func TestSnapshotForPlayer_CarriesViewerEarnedDP(t *testing.T) {
	s := newDPTestState(t)
	s.Players["p_host"] = &Player{ID: "p_host", MatchDominionPointsEarned: 9}
	s.Players["p_joiner"] = &Player{ID: "p_joiner", MatchDominionPointsEarned: 4}
	// Force game-over so the GameOver block is populated.
	s.lostPlayerIDs = map[string]bool{"p_host": true}

	s.mu.RLock()
	snap := s.snapshotForPlayerLocked("p_joiner")
	s.mu.RUnlock()

	if snap.GameOver == nil {
		t.Fatal("expected GameOver block to be present")
	}
	if got := snap.GameOver.YourDominionPointsEarned; got != 4 {
		t.Errorf("viewer p_joiner earned DP: want 4, got %d", got)
	}
}
```

> Use whatever minimal state constructor the package's other game tests use (`newDPTestState` is referenced from Task 1; if the real helper is named differently, match it). The viewer must have a FOW entry or none — both branches are exercised in later steps; for this test a nil FOW (unfiltered branch) is fine.

- [ ] **Step 3: Run test to verify it fails**

Run: `cd server && go test ./internal/game/ -run TestSnapshotForPlayer_CarriesViewerEarnedDP -v`
Expected: FAIL — `YourDominionPointsEarned` is 0 (not yet populated).

- [ ] **Step 4: Populate the field in both per-viewer branches**

In `server/internal/game/state.go`, **unfiltered branch** — after line 1690 (`snap.Victory = s.buildVictorySnapshotForViewerLocked(viewerID)`), before `return snap`:

```go
		snap.Victory = s.buildVictorySnapshotForViewerLocked(viewerID)
		if snap.GameOver != nil {
			if p, ok := s.Players[viewerID]; ok {
				snap.GameOver.YourDominionPointsEarned = p.MatchDominionPointsEarned
			}
		}
		return snap
```

In the **FOW branch**, extend the game-over block (lines 1908-1915):

```go
	var gameOver *protocol.GameOverSnapshot
	if len(s.lostPlayerIDs) > 0 {
		ids := make([]string, 0, len(s.lostPlayerIDs))
		for id := range s.lostPlayerIDs {
			ids = append(ids, id)
		}
		gameOver = &protocol.GameOverSnapshot{LostPlayerIDs: ids}
		if p, ok := s.Players[viewerID]; ok {
			gameOver.YourDominionPointsEarned = p.MatchDominionPointsEarned
		}
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd server && go test ./internal/game/ -run 'TestSnapshotForPlayer_CarriesViewerEarnedDP' -v && cd server && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 6: Commit**

```bash
git add server/pkg/protocol/messages.go server/internal/game/state.go server/internal/game/snapshot_dominion_test.go
git commit -m "feat(server): report viewer's own earned dominion points in game-over snapshot"
```

---

## Task 3: Server — idempotent client-driven award endpoint

**Files:**
- Modify: `server/internal/profile/types.go` (PlayerProfile struct)
- Modify: `server/internal/http/profile_handlers.go` (new handler in `registerProfileRoutes`)
- Test: `server/internal/http/profile_award_dominion_test.go` (new)

- [ ] **Step 1: Add the idempotency field to the profile**

In `server/internal/profile/types.go`, add to `PlayerProfile` (next to `CompletedCampaignObjectives`, around line 68):

```go
	// CreditedMatchIDs records match IDs whose end-of-match dominion-point
	// award has already been applied to this profile, so a client retry /
	// recap re-mount cannot double-credit. Bounded to the most recent entries
	// (see award handler). nil/empty for fresh profiles.
	CreditedMatchIDs []string `json:"creditedMatchIds,omitempty"`
```

- [ ] **Step 2: Write the failing test**

Create `server/internal/http/profile_award_dominion_test.go`:

```go
package httpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"webrts/server/internal/profile"
)

func TestAwardDominionPoints_CreditsOnceThenDedups(t *testing.T) {
	mux, pm := newTestMux(t) // existing helper in this package
	const playerID = "00000000-0000-0000-0000-000000000001"

	post := func() *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"matchId": "match-7", "amount": 5})
		req := httptest.NewRequest(http.MethodPost, "/api/profile/match/award-dominion-points", bytes.NewReader(body))
		req.Header.Set("X-Player-ID", playerID)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	if rec := post(); rec.Code != http.StatusOK {
		t.Fatalf("first award status: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	// Second identical call must be a no-op (idempotent by matchId).
	if rec := post(); rec.Code != http.StatusOK {
		t.Fatalf("second award status: want 200, got %d", rec.Code)
	}

	var p *profile.PlayerProfile
	if err := pm.WithLocked(playerID, func(prof *profile.PlayerProfile) error {
		p = prof
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if p.DominionPoints != 5 {
		t.Errorf("DominionPoints: want 5 (credited once), got %d", p.DominionPoints)
	}
	if p.LifetimeDominionPoints != 5 {
		t.Errorf("LifetimeDominionPoints: want 5, got %d", p.LifetimeDominionPoints)
	}
}
```

> `newTestMux(t)` is the existing helper used by the sibling profile tests (see `profile_upgrade_handlers_test.go:19`). If it does not already register all profile routes, use the same registration call those tests use.

- [ ] **Step 3: Run test to verify it fails**

Run: `cd server && go test ./internal/http/ -run TestAwardDominionPoints_CreditsOnceThenDedups -v`
Expected: FAIL — 404 (route not registered) / 405.

- [ ] **Step 4: Add the handler**

In `server/internal/http/profile_handlers.go`, inside `registerProfileRoutes`, add a new route (place it next to the campaign handlers):

```go
	// Client-driven end-of-match dominion-point award. Needed because in
	// host-authoritative multiplayer the joiner's profile lives on the
	// joiner's machine — the host cannot write it. The joiner POSTs its own
	// server-reported earned total here against ITS local server. Idempotent
	// by matchId so a recap re-mount / retry cannot double-credit.
	//
	// Body: {matchId: string, amount: int}  Header: X-Player-ID
	mux.HandleFunc("/api/profile/match/award-dominion-points", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		playerID := extractPlayerID(w, r)
		if playerID == "" {
			return
		}
		var body struct {
			MatchID string `json:"matchId"`
			Amount  int    `json:"amount"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
			return
		}
		if body.MatchID == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_match_id", "matchId is required")
			return
		}
		if body.Amount < 0 {
			writeJSONError(w, http.StatusBadRequest, "invalid_amount", "amount must be >= 0")
			return
		}
		var updated *profile.PlayerProfile
		err := pm.WithLocked(playerID, func(p *profile.PlayerProfile) error {
			for _, id := range p.CreditedMatchIDs {
				if id == body.MatchID {
					updated = p // already credited — no-op
					return nil
				}
			}
			if body.Amount > 0 {
				p.DominionPoints += body.Amount
				p.LifetimeDominionPoints += body.Amount
			}
			p.CreditedMatchIDs = append(p.CreditedMatchIDs, body.MatchID)
			// Bound the ledger so it can't grow without limit; matchId reuse
			// across distinct sessions far enough apart is acceptable risk.
			const maxCredited = 50
			if len(p.CreditedMatchIDs) > maxCredited {
				p.CreditedMatchIDs = p.CreditedMatchIDs[len(p.CreditedMatchIDs)-maxCredited:]
			}
			updated = p
			return nil
		})
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "profile_error", err.Error())
			return
		}
		writeJSON(w, updated)
	})
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd server && go test ./internal/http/ ./internal/profile/ -run 'Award|Profile' -v && cd server && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 6: Commit**

```bash
git add server/internal/profile/types.go server/internal/http/profile_handlers.go server/internal/http/profile_award_dominion_test.go
git commit -m "feat(server): idempotent client-driven match dominion-point award endpoint"
```

---

## Task 4: Client — protocol field + API helpers

**Files:**
- Modify: `client/src/game-portal/src/game/network/protocol.ts:1123-1125`
- Modify: `client/src/game-portal/src/services/profileApi.ts`

- [ ] **Step 1: Extend the client `GameOverSnapshot` type**

In `client/src/game-portal/src/game/network/protocol.ts`:

```ts
export type GameOverSnapshot = {
  lostPlayerIds: string[]
  /** This viewer's own per-match earned dominion points. Present only at
   *  game-over. A remote joiner persists this into its own local profile. */
  yourDominionPointsEarned?: number
}
```

- [ ] **Step 2: Add the API helper + remote-joiner detector**

In `client/src/game-portal/src/services/profileApi.ts`, add:

```ts
/** True when this SPA tab is a remote multiplayer client proxying to a host
 *  (Steam joiner or direct-connect joiner). Mirrors NetworkClient's WS-URL
 *  resolution. The host / single-player return false and rely on the
 *  server-side dominion-point commit. */
export function isRemoteProxyClient(): boolean {
  try {
    if (sessionStorage.getItem('webrts.steam.proxyActive') === '1') return true
    if (sessionStorage.getItem('webrts.directConnect.proxyToken')) return true
  } catch {
    // sessionStorage can throw in sandboxed contexts — treat as non-proxy.
  }
  return false
}

/** Persist end-of-match dominion points to THIS machine's local profile.
 *  Idempotent on the server by matchId. Used by a remote joiner, whose
 *  earned DP the host could only commit to the host's own disk. Returns the
 *  updated profile. */
export async function awardMatchDominionPoints(matchId: string, amount: number): Promise<PlayerProfile> {
  const res = await fetch(`${API_BASE}/api/profile/match/award-dominion-points`, {
    method: 'POST',
    headers: playerHeaders(),
    body: JSON.stringify({ matchId, amount }),
  })
  return handleResponse<PlayerProfile>(res)
}
```

> The two sessionStorage keys are the literal values of `STEAM_PROXY_FLAG_KEY` and `PROXY_TOKEN_STORAGE_KEY` in `NetworkClient.ts`. They are intentionally inlined here to avoid `profileApi.ts` importing the network layer; if the project prefers a shared constants module, export them there instead.

- [ ] **Step 3: Type-check**

Run: `cd client/src/game-portal && npx vue-tsc --noEmit`
Expected: no new type errors.

- [ ] **Step 4: Commit**

```bash
git add client/src/game-portal/src/game/network/protocol.ts client/src/game-portal/src/services/profileApi.ts
git commit -m "feat(client): protocol field + API for client-side match DP award"
```

---

## Task 5: Client — freeze the final roster + earned DP on first game-over

**Files:**
- Modify: `client/src/game-portal/src/game/core/GameState.ts` (fields near :681; freeze in `applySnapshot` near :1408-1417)
- Modify: `client/src/game-portal/src/game/core/GameClient.ts:60-70` (UI type) and `:280-290` (UI build)
- Test: `client/src/game-portal/src/game/core/gameState.endRoster.test.ts` (new)

- [ ] **Step 1: Write the failing test**

Create `client/src/game-portal/src/game/core/gameState.endRoster.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { GameState } from './GameState'
import type { MatchSnapshotMessage, PlayerSnapshot } from '../network/protocol'

function player(id: string): PlayerSnapshot {
  return { playerId: id, color: '#fff', resources: [] } as unknown as PlayerSnapshot
}

function snap(players: PlayerSnapshot[], extra: Partial<MatchSnapshotMessage> = {}): MatchSnapshotMessage {
  return {
    type: 'match_snapshot',
    matchId: 'match-7',
    tick: 1,
    players,
    units: [],
    buildings: [],
    ...extra,
  } as unknown as MatchSnapshotMessage
}

describe('GameState end-of-match roster freeze', () => {
  it('freezes both players + earned DP when game-over first appears, and a later thinner snapshot cannot clobber it', () => {
    const gs = new GameState()
    gs.localPlayerId = 'p_joiner'

    // Game-over snapshot with both players present + the viewer's earned DP.
    gs.applySnapshot(snap([player('p_host'), player('p_joiner')], {
      gameOver: { lostPlayerIds: ['p_host'], yourDominionPointsEarned: 4 },
    }))

    // A later snapshot where the host has dropped out of the roster (teardown).
    gs.applySnapshot(snap([player('p_joiner')], {
      gameOver: { lostPlayerIds: ['p_host'], yourDominionPointsEarned: 4 },
    }))

    expect(gs.frozenEndPlayers?.map((p) => p.playerId).sort()).toEqual(['p_host', 'p_joiner'])
    expect(gs.matchDominionPointsEarned).toBe(4)
  })
})
```

> If `new GameState()` requires constructor args in this codebase, match how the existing GameState tests instantiate it. The behavioral assertions are the contract.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client/src/game-portal && npx vitest run src/game/core/gameState.endRoster.test.ts`
Expected: FAIL — `frozenEndPlayers` / `matchDominionPointsEarned` are undefined.

- [ ] **Step 3: Add the fields**

In `client/src/game-portal/src/game/core/GameState.ts`, near the game-over field (line 681):

```ts
  // Game over state — non-null once any player has lost all townhalls.
  gameOverSnapshot: GameOverSnapshot | null = null

  // Frozen end-of-match roster. Captured from the FIRST snapshot that reports
  // game-over (or victory) so connection teardown — e.g. the host leaving and
  // dropping out of the roster — can't clobber the recap's data source. Null
  // until the match ends. The recap reads this, not the live playerSnapshots.
  frozenEndPlayers: PlayerSnapshot[] | null = null
  // This viewer's own earned dominion points for the match, taken from the
  // game-over snapshot at freeze time. 0 until/unless the server reports it.
  matchDominionPointsEarned = 0
  private endRosterFrozen = false
```

- [ ] **Step 4: Freeze in `applySnapshot`**

In `client/src/game-portal/src/game/core/GameState.ts`, in `applySnapshot`, replace the existing game-over assignment block (around lines 1412-1417):

```ts
    if (message.gameOver) {
      this.gameOverSnapshot = message.gameOver
    }
    if (message.victory) {
      this.victorySnapshot = message.victory
    }

    // Freeze the end-of-match roster + this viewer's earned DP exactly once,
    // from the first snapshot that reports the match is over. playerSnapshots
    // was already updated above (applyPlayerSnapshots ran earlier this call),
    // so it reflects this snapshot's full roster.
    const matchEnded = !!message.gameOver || message.victory?.achieved === true
    if (matchEnded && !this.endRosterFrozen) {
      this.endRosterFrozen = true
      this.frozenEndPlayers = [...this.playerSnapshots]
      this.matchDominionPointsEarned = message.gameOver?.yourDominionPointsEarned ?? 0
    }
```

- [ ] **Step 5: Expose on the UI state**

In `client/src/game-portal/src/game/core/GameClient.ts`, add to the UI state interface (near line 65):

```ts
  players: import('../network/protocol').PlayerSnapshot[]
  frozenEndPlayers: import('../network/protocol').PlayerSnapshot[] | null
  matchDominionPointsEarned: number
```

and to the object built around line 284:

```ts
      players: this.state.playerSnapshots,
      frozenEndPlayers: this.state.frozenEndPlayers,
      matchDominionPointsEarned: this.state.matchDominionPointsEarned,
```

- [ ] **Step 6: Run test + type-check**

Run: `cd client/src/game-portal && npx vitest run src/game/core/gameState.endRoster.test.ts && npx vue-tsc --noEmit`
Expected: PASS + no new type errors.

- [ ] **Step 7: Commit**

```bash
git add client/src/game-portal/src/game/core/GameState.ts client/src/game-portal/src/game/core/GameClient.ts client/src/game-portal/src/game/core/gameState.endRoster.test.ts
git commit -m "fix(client): freeze end-of-match roster + earned DP on first game-over"
```

---

## Task 6: Client — recap uses frozen roster; joiner persists earned DP

**Files:**
- Modify: `client/src/game-portal/src/state/matchEndState.ts`
- Modify: `client/src/game-portal/src/views/Match.vue:394-406`
- Modify: `client/src/game-portal/src/views/MatchEnd.vue`

- [ ] **Step 1: Extend the recap snapshot state**

In `client/src/game-portal/src/state/matchEndState.ts`, add to the `MatchEndSnapshot` interface:

```ts
  /** This viewer's own earned dominion points for the match. A remote joiner
   *  persists this to its local profile; the host ignores it (server already
   *  committed). */
  dominionPointsEarned: number
  /** Match ID — idempotency key for the client-side dominion-point award. */
  matchId: string
```

and add a one-shot guard flag below the ref:

```ts
/** Set once the remote joiner has POSTed its earned DP for the current
 *  recap, so a recap re-mount / route bounce can't double-fire. Reset by
 *  clearMatchEndSnapshot. */
export const matchEndDpPersisted = ref(false)
```

Update `clearMatchEndSnapshot` to also reset it:

```ts
export function clearMatchEndSnapshot(): void {
  matchEndSnapshot.value = null
  matchEndDpPersisted.value = false
}
```

- [ ] **Step 2: Capture frozen roster + earned DP in Match.vue**

In `client/src/game-portal/src/views/Match.vue`, in `transitionToMatchEnd` (lines 395-406), source the roster from the frozen field with a live fallback, and carry the earned DP + matchId:

```ts
  setMatchEndSnapshot({
    outcome,
    // Defensive shallow copies — the underlying arrays are reactive and
    // mutated by the network layer; the recap should see a stable
    // post-match view, not the next snapshot's diff.
    objectives: [...ui.value.objectives],
    // Prefer the roster frozen at game-over (immune to teardown clobber);
    // fall back to the live roster for any end path without a freeze.
    players: ui.value.frozenEndPlayers
      ? [...ui.value.frozenEndPlayers]
      : [...ui.value.players],
    viewerId: ui.value.player.playerId ?? '',
    dominionPointsEarned: ui.value.matchDominionPointsEarned,
    matchId: networkClient.matchId ?? localStorage.getItem('webrts.matchId') ?? '',
    campaignId: campaignSession.value?.campaignId ?? null,
    levelId: campaignSession.value?.levelId ?? null,
    levelDisplayName: campaignSession.value?.levelDisplayName,
  })
```

> Use whatever Match.vue already has in scope for the match ID. If `networkClient` is not directly referenced in this file, read `localStorage.getItem('webrts.matchId')` (the `MATCH_ID_STORAGE_KEY` NetworkClient persists on every snapshot — see [NetworkClient.ts:697-698](../../../client/src/game-portal/src/game/network/NetworkClient.ts#L697-L698)). The fallback already covers this.

- [ ] **Step 3: Persist for the remote joiner on recap mount**

In `client/src/game-portal/src/views/MatchEnd.vue`, extend the script. Add imports:

```ts
import { matchEndSnapshot, clearMatchEndSnapshot, matchEndDpPersisted } from '@/state/matchEndState'
import { awardMatchDominionPoints, isRemoteProxyClient } from '@/services/profileApi'
```

Replace the existing `onMounted` with one that also persists DP for a remote joiner exactly once:

```ts
onMounted(() => {
  const snap = snapshot.value
  if (!snap) {
    // Cold mount with no snapshot — bounce home so the user isn't stranded.
    void router.replace('/')
    return
  }

  // Remote joiner only: the host already committed the host player's DP
  // server-side, and could only write the joiner's DP to the host's disk —
  // so the joiner persists its own earned total to ITS local profile here.
  // Single-player / host take no action (server-side commit is authoritative).
  // Idempotent on the server by matchId; the local guard prevents a re-mount
  // from firing a second request.
  if (
    !matchEndDpPersisted.value &&
    isRemoteProxyClient() &&
    snap.matchId &&
    snap.dominionPointsEarned > 0
  ) {
    matchEndDpPersisted.value = true
    void awardMatchDominionPoints(snap.matchId, snap.dominionPointsEarned)
      .then(() => refreshProfile())
      .catch((err) => console.error('[MatchEnd] failed to persist dominion points:', err))
  }
})
```

> `refreshProfile` is already imported (`const { refresh: refreshProfile } = useProfile()`). Keep the existing `onClose` handler unchanged.

- [ ] **Step 4: Type-check + run the client test suite**

Run: `cd client/src/game-portal && npx vue-tsc --noEmit && npx vitest run`
Expected: no new type errors; existing suite green.

- [ ] **Step 5: Commit**

```bash
git add client/src/game-portal/src/state/matchEndState.ts client/src/game-portal/src/views/Match.vue client/src/game-portal/src/views/MatchEnd.vue
git commit -m "fix(client): recap uses frozen roster; remote joiner persists earned dominion points"
```

---

## Task 7: Full verification

- [ ] **Step 1: Server build + full test pass**

Run: `cd server && go build ./... && go test ./...`
Expected: all green.

- [ ] **Step 2: Client build + full test pass**

Run: `cd client/src/game-portal && npx vue-tsc --noEmit && npx vitest run`
Expected: all green.

- [ ] **Step 3: Two-machine (or two-instance) Steam manual smoke**

With a host (P1) and a joiner (P2) on a Steam multiplayer match on `forest-1`:
1. Play until the match ends (one side eliminated).
2. **Bug 1:** On P2's recap, confirm the Match Statistics table shows **both** P1's and P2's rows with non-zero stats.
3. **Bug 2:** Note P2's dominion-point balance in the menu before and after; confirm it increases by P2's earned drops after the recap appears.
4. Confirm P1's balance still increases correctly (no regression, no double-count).
5. Re-enter and exit the recap / refresh: confirm P2's balance does **not** increase a second time (idempotency).

---

## Self-Review Notes

- **Spec coverage:** Bug 1 → Task 5 (freeze) + Task 6 step 2 (recap reads frozen roster). Bug 2 → Task 1 (accumulate) + Task 2 (deliver per-viewer) + Task 3 (idempotent endpoint) + Task 4/6 (client persists, joiner-only). No double-count: host path untouched; only `isRemoteProxyClient()` persists client-side.
- **Type consistency:** `MatchDominionPointsEarned` (Go) ↔ `YourDominionPointsEarned` wire field ↔ `yourDominionPointsEarned` (TS) ↔ `matchDominionPointsEarned` (GameState/UI) ↔ `dominionPointsEarned` (recap snapshot). Each hop renames intentionally at a serialization boundary; the JSON tag `yourDominionPointsEarned` is the contract on both sides.
- **Known limitation (documented, out of scope):** earned DP rides on `GameOverSnapshot`; a co-op match won by objective with no human eliminated yields no game-over block, so a remote joiner there would not get client-persisted DP. Follow-up if co-op objective wins become common.
- **matchId reuse:** server `nextID` resets on restart, so `match-N` strings recur across sessions. The credited ledger is bounded to 50; a collision only 50+ matches later would drop one legit award. Acceptable for now; a globally-unique match token would remove it.
