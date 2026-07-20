# Per-Rank Ability-Slot Pools (Editor + Path-Owned) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Make "this rank pulls one ability from a pool" a first-class, path-owned, **editor-editable** concept. Add `abilityPoolsByRank` to the PathDef (mirroring `perksByRank`), migrate the Arch Mage's spell pool into its path file, delete the standalone `spell-pools.json`, and give the Unit Type Editor a per-rank **Perk-slot / Ability-slot** selector whose ability picker **blocks abilities already granted to the unit** (no `chain_lightning, chain_lightning`).

**Architecture:** The runtime already rolls one pool ability per rank and de-dupes against known abilities (`spell_pool_roll.go`); only the DATA source and the EDITOR surface change. `perksByRank` is the exact template — a per-rank `map[string][]string` on `pathCatalogFile`, validated at load/save, registered into a derived map, round-tripped by the path editor. We add `abilityPoolsByRank` alongside it and repoint the pool lookup at it.

**Tech Stack:** Go (server, `internal/game`), embedded catalog, Vue 3 + TS (client path editor). Gate backend on `go build ./... && go vet ./... && go test ./internal/game/`; frontend on `npx vitest run` + `npx vue-tsc -b` from `client/src/game-portal`.

**Behavior invariant:** The Arch Mage must roll the exact same spells at each rank under a given seed. A guard test pins this before the migration.

---

## The `perksByRank` template — mirror these exact sites for `abilityPoolsByRank`

- Struct field: `pathCatalogFile.PerksByRank map[string][]string` — [path_defs.go:99](server/internal/game/path_defs.go#L99).
- Derived map var: `pathPerkRefsByPath` — [path_defs.go:212](server/internal/game/path_defs.go#L212); reader `pathPerkRefsForRank` — [path_defs.go:334](server/internal/game/path_defs.go#L334).
- Live-swap on rebuild: `pathPerkRefsByPath = fresh.perkRefsByPath` — [path_defs.go:126](server/internal/game/path_defs.go#L126).
- Validation: `validatePathFile` PerksByRank block — [path_persistence.go:529-549](server/internal/game/path_persistence.go#L529-L549).
- Registration into derived maps: `registerPathFileInto` — [path_persistence.go:683-691](server/internal/game/path_persistence.go#L683-L691).
- Deep-copy: `clonePathCatalogFile` — [path_persistence.go:784-790](server/internal/game/path_persistence.go#L784-L790).
- Client form: `AuthoredPathDef.perksByRank` + `MODELED_PATH_KEYS` — [pathEditorForm.ts:59,68-71](client/src/game-portal/src/game/units/pathEditorForm.ts#L59).
- Runtime pool lookup to repoint: `spellPoolFor` — [spell_pool_defs.go:97](server/internal/game/spell_pool_defs.go#L97); roll consumer (unchanged): `rollUnitPoolSpellForRankLocked` — [spell_pool_roll.go:44](server/internal/game/spell_pool_roll.go#L44).

Run Go commands from `server/`; client commands from `client/src/game-portal/`. **No `git commit`/`git add`** — the human commits.

---

## PHASE A — Backend: path-owned ability pools (behavior-preserving)

### Task A1: Guard test — pin the Arch Mage's rolled spells

**Files:** Test: `server/internal/game/ability_pool_migration_test.go` (create)

- [ ] **Step 1: Write a test that records each rank's roll candidates for arch_mage**

The stable, seed-independent thing to assert is the **candidate pool** `spellPoolFor` returns per rank (the roll itself is RNG-seeded and covered elsewhere). Pin today's effective candidate sets:

```go
package game

import (
	"sort"
	"testing"
)

// wantArchMagePool is the effective candidate set spellPoolFor returns per rank
// TODAY (spell-pools.json bronze=[5], silver=[] but cumulative → silver=bronze).
// The migration must keep these sets identical (behavior invariant).
var wantArchMagePool = map[string][]string{
	"bronze": {"arcane_orb", "chain_lightning", "fireball", "meteor", "shatter"},
	"silver": {"arcane_orb", "chain_lightning", "fireball", "meteor", "shatter"},
	"gold":   {}, // gold grants no pool ability
}

func TestArchMageAbilityPoolCandidates(t *testing.T) {
	for _, rank := range []string{"bronze", "silver", "gold"} {
		got := append([]string(nil), spellPoolFor("arch_mage", rank)...)
		sort.Strings(got)
		want := wantArchMagePool[rank]
		if len(got) != len(want) {
			t.Errorf("%s: got %v, want %v", rank, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: got %v, want %v", rank, got, want)
				break
			}
		}
	}
}
```

- [ ] **Step 2: Run it — PASS on today's code**

Run: `cd server && go test ./internal/game/ -run TestArchMageAbilityPoolCandidates -v`
Expected: PASS. (If it fails, the `want` table is wrong — capture the actual `got` and reconcile, do NOT change catalog data.)

- [ ] **Step 3: Commit** (human) — mention: `git add server/internal/game/ability_pool_migration_test.go`

### Task A2: Add `AbilityPoolsByRank` to the PathDef (mirror `perksByRank`)

**Files:** `path_defs.go`, `path_persistence.go`

- [ ] **Step 1: Add the struct field**

In `pathCatalogFile` ([path_defs.go:99](server/internal/game/path_defs.go#L99)), right after `PerksByRank`:

```go
	// AbilityPoolsByRank is the per-rank pool of candidate ability ids a unit
	// MAY be granted at that rank (one is rolled at rank-up — see
	// spell_pool_roll.go). Keyed bronze/silver/gold. A rank with a non-empty
	// pool is an "ability slot"; a rank with perksByRank entries is a "perk
	// slot" (the editor presents these as mutually exclusive per rank, but the
	// runtime reads both independently). Replaces the standalone spell-pools.json.
	AbilityPoolsByRank map[string][]string `json:"abilityPoolsByRank,omitempty"`
```

- [ ] **Step 2: Add the derived map + reader + live-swap**

Mirror `pathPerkRefsByPath`: add `var pathAbilityPoolsByPath = map[string]map[string][]string{}` near [path_defs.go:212](server/internal/game/path_defs.go#L212); a reader `pathAbilityPoolsForRank(pathName, rank string) []string` mirroring `pathPerkRefsForRank` (return a copy); and in the rebuild swap add `pathAbilityPoolsByPath = fresh.abilityPoolsByPath` next to [path_defs.go:126](server/internal/game/path_defs.go#L126). Add the `abilityPoolsByPath` field to the `pathDerivedMaps` bundle struct (same struct that holds `perkRefsByPath`).

- [ ] **Step 3: Validation (with the no-dupe rule)**

In `validatePathFile` ([path_persistence.go:529](server/internal/game/path_persistence.go#L529)), after the PerksByRank block, add an AbilityPoolsByRank block: each rank key must be bronze/silver/gold; each ability id must resolve via `getAbilityDef(id)` (not `perkDefLookup`); reject empty ids. THEN enforce the dedup rule:
- **base ∩ pool** — no pool may list an ability that is already in the path's base `Abilities` (a permanently-granted ability in a roll pool is a dead/contradictory entry). Reject, naming the id + rank.
- **within a rank** — a single rank's pool may not list the same id twice. Reject.
- **ALLOWED: same id across different ranks' pools.** This is intentional — shared pools (e.g. arch_mage bronze + silver both list the same 5 spells) are the design; the runtime roll de-dupes the actual grant across ranks via `unitKnownSpellSetLocked`, so a unit never ends up with two copies. Do NOT reject cross-rank sharing.

Sorted iteration for a deterministic first-error (mirror the PerksByRank sort).

- [ ] **Step 4: Registration + clone**

In `registerPathFileInto` ([path_persistence.go:683](server/internal/game/path_persistence.go#L683)) copy `file.AbilityPoolsByRank` into `dst.abilityPoolsByPath[file.Path]` exactly like the PerksByRank block. In `clonePathCatalogFile` ([path_persistence.go:784](server/internal/game/path_persistence.go#L784)) deep-copy `AbilityPoolsByRank` exactly like `PerksByRank`.

- [ ] **Step 5: Build + existing suite**

Run: `cd server && go build ./... && go vet ./... && go test ./internal/game/ -run 'TestPath|TestArchMage'`
Expected: PASS + clean. (No behavior change yet — nothing reads the new field.)

- [ ] **Step 6: Commit** (human)

### Task A3: Migrate the data + repoint the runtime; delete spell-pools.json

**Files:** `catalog/units/human/adept/paths/arch_mage/arch_mage.json`, `spell_pool_defs.go`, `catalog/spell-pools.json` (delete), tests.

- [ ] **Step 1: Author `abilityPoolsByRank` on arch_mage.json (self-contained per rank)**

Add, alongside the existing `perksByRank` (which stays as the gold perks):

```json
"abilityPoolsByRank": {
  "bronze": ["fireball", "chain_lightning", "arcane_orb", "shatter", "meteor"],
  "silver": ["fireball", "chain_lightning", "arcane_orb", "shatter", "meteor"]
}
```

Silver explicitly lists the same 5 (self-contained) — this reproduces today's cumulative behavior WITHOUT the hidden union. Gold has no pool (perk tier).

- [ ] **Step 2: Repoint `spellPoolFor` at the path data; drop the cumulative union + the embed**

Rewrite `spellPoolFor` ([spell_pool_defs.go:97](server/internal/game/spell_pool_defs.go#L97)) to simply return a copy of `pathAbilityPoolsForRank(archetype, rank)` (each rank is now self-contained; no bronze∪silver special-case). Delete `//go:embed catalog/spell-pools.json`, `spellPoolsRaw`, `spellPoolsByArchetype`, `mustLoadSpellPools`, and the `//go:embed`'d file `catalog/spell-pools.json`. Keep `loadSpellPools`/validation logic ONLY if a test still needs it — otherwise delete it too (the path validation in A2 now owns pool validation). Keep `ListSpellPools` working (rebuild it from `pathAbilityPoolsByPath` for diagnostics/tests) or delete if unused — follow the compiler.

- [ ] **Step 3: Update spell-pool tests to the path-based source**

`spell_pool_defs_test.go` (tested the embed loader/cumulative union) and `arch_mage_bronze_pool_test.go` / `spell_pool_roll_test.go`: repoint any assertion about the pool source to the path (`pathAbilityPoolsForRank` / `spellPoolFor` against `arch_mage`). Remove tests that asserted the deleted cumulative-union behavior specifically; keep/convert the roll-determinism and no-duplicate-across-ranks tests (those invariants still hold). Preserve each test's real intent — do not weaken.

- [ ] **Step 4: Verify behavior preserved**

Run: `cd server && go build ./... && go vet ./... && go test ./internal/game/`
Expected: PASS + clean. **`TestArchMageAbilityPoolCandidates` (A1) must still pass** — identical candidate sets, proving the migration is behavior-neutral. Determinism/rank-up tests green.

- [ ] **Step 5: Commit** (human)

---

## PHASE B — Frontend: per-rank Perk/Ability slot selector + dedup picker

### Task B1: Model `abilityPoolsByRank` on the path form

**Files:** `client/src/game-portal/src/game/units/pathEditorForm.ts`

- [ ] **Step 1: Add the field + modeled key**

Add `abilityPoolsByRank?: Record<string, string[]>` to `AuthoredPathDef` (next to `perksByRank`, [pathEditorForm.ts:59](client/src/game-portal/src/game/units/pathEditorForm.ts#L59)) and `'abilityPoolsByRank'` to `MODELED_PATH_KEYS`. It round-trips exactly like `perksByRank` (no other code needed — the modeled/remainder split handles it).

- [ ] **Step 2: Type-check** — `cd client/src/game-portal && npx vue-tsc -b` → clean.

- [ ] **Step 3: Commit** (human)

### Task B2: Per-rank slot selector + ability-pool picker with dedup

**Files:** `client/src/game-portal/src/components/UnitTypeEditorPanel.vue`, `UnitTypeEditorPanel.pathForm.test.ts`

Context: the "Perk References" section renders `PERK_RANK_ORDER` rows, each with a perk picker ([UnitTypeEditorPanel.vue:627-650](client/src/game-portal/src/components/UnitTypeEditorPanel.vue#L627-L650)); `availablePerkOptionsForRank` and `addPerkToRank`/`removePerkFromRank` are the idiom to mirror. `abilityOptions` ([:905](client/src/game-portal/src/components/UnitTypeEditorPanel.vue#L905)) is the list of all authored abilities as `{id,label}`.

- [ ] **Step 1: Failing test**

In `UnitTypeEditorPanel.pathForm.test.ts` add a test: for a path, each rank row shows a slot-type selector; setting `bronze` to "Ability slot" reveals an ability picker; the picker's options EXCLUDE abilities already in the path's base `abilities` array and abilities in another rank's pool (assert a base ability like `arcane_missiles` and a same-path other-rank ability are absent), and adding one writes `pathForm.abilityPoolsByRank.bronze`. Match the existing mount/stub idiom in that file.

- [ ] **Step 2: Run — FAIL.** `cd client/src/game-portal && npx vitest run src/components/UnitTypeEditorPanel.pathForm.test.ts`

- [ ] **Step 3: Implement the per-rank slot UI**

In the "Perk References" `SectionCard` (rename its title to "Rank Slots"), for each `rank in PERK_RANK_ORDER` render a small selector: **Perk slot** vs **Ability slot**. Derive the current type: `abilityPoolsByRank?.[rank]?.length` (or an explicit presence flag) ⇒ Ability, else Perk. Switching a rank to Ability clears `perksByRank[rank]`; switching to Perk clears `abilityPoolsByRank[rank]` (mutually exclusive per rank in the UI). Render the existing perk picker for Perk slots; for Ability slots render an ability picker mirroring the perk one:

```ts
// Abilities eligible for THIS rank's pool: all authored abilities, minus the
// path's base abilities (already always-granted) and the ids already in THIS
// rank's pool. The SAME ability MAY appear in another rank's pool (shared pools
// are intentional — the runtime de-dupes the actual grant), so we do NOT exclude
// other ranks. This matches the backend base∩pool + within-rank dedup rule.
function availableAbilityOptionsForRank(rank: string): FilterableOption[] {
  const taken = new Set<string>([
    ...(pathForm.value?.abilities ?? []),
    ...abilitiesForRank(rank),
  ])
  return abilityDefs.value
    .filter((d) => !taken.has(d.id))
    .map((d) => ({ id: d.id, label: d.displayName || d.id }))
    .sort((a, b) => a.label.localeCompare(b.label))
}
function abilitiesForRank(rank: string): string[] { return pathForm.value?.abilityPoolsByRank?.[rank] ?? [] }
function addAbilityToRank(rank: string, id: string) { /* mirror addPerkToRank on abilityPoolsByRank */ }
function removeAbilityFromRank(rank: string, id: string) { /* mirror removePerkFromRank */ }
```

(Excludes base abilities + this rank's current picks; allows the same id in a different rank's pool — matching the backend rule.)

- [ ] **Step 4: Run tests + type-check + broad sanity**

Run: `cd client/src/game-portal && npx vitest run src/components/UnitTypeEditorPanel src/game/units && npx vue-tsc -b`
Expected: PASS + clean. (Pre-existing unrelated failures `worldEditorToolbar`, `ListEditorPanel` are NOT yours.)

- [ ] **Step 5: Commit** (human)

---

## PHASE C — Rename spell → ability (finish the cleanup)

Now that these pools are generic per-path ability pools (and Trapper will reuse them), rename the legacy "spell" vocabulary to "ability" across server internals, the wire field, and the client. **Pure mechanical rename — ZERO behavior change.** Done LAST so it renames only the symbols that survive Phase A (the embed loader / `spellPoolsByArchetype` are already gone). The compilers are the safety net: `go build` and `vue-tsc -b` flag every straggler.

**Rename map (apply everywhere; grep each, let the compiler find the rest):**

| Old (server) | New |
|---|---|
| file `spell_pool_roll.go` | `ability_pool_roll.go` |
| file `spell_pool_defs.go` | `ability_pool_defs.go` |
| `spellPoolFor` | `abilityPoolFor` |
| `ListSpellPools` / `sortedSpellPoolArchetypes` | `ListAbilityPools` / `sortedAbilityPoolArchetypes` |
| `Unit.PoolSpellsByRank` | `Unit.PoolAbilitiesByRank` |
| `rollUnitPoolSpellsLocked` | `rollUnitPoolAbilitiesLocked` |
| `rollUnitPoolSpellForRankLocked` | `rollUnitPoolAbilityForRankLocked` |
| `spellSlotRankForAbilityLocked` | `abilitySlotRankLocked` |
| `unitKnownSpellSetLocked` | `unitKnownAbilitySetLocked` |
| `randomLearnedSpellLocked` | `randomLearnedAbilityLocked` |
| wire `AbilitySnapshot.SpellSlotRank` (`json:"spellSlotRank"`) | `AbilitySlotRank` (`json:"abilitySlotRank"`) |
| comments "spell pool" / "spell slot" / "pool spell" / "learned spell" | "ability pool" / "ability slot" / "pool ability" / "learned ability" |

| Old (client) | New |
|---|---|
| `AbilitySnapshot.spellSlotRank` (protocol.ts) | `abilitySlotRank` |
| `buildSpellSlotCell` (GameState.ts) | `buildAbilitySlotCell` |
| `spellSlotByRank` (GameState.ts) | `abilitySlotByRank` |
| file `GameState.archMageSpellSlot.test.ts` | `GameState.archMageAbilitySlot.test.ts` |

Leave the broader cast-system "spell" vocabulary alone (`EffectiveSpell`, `effectiveSpellLocked`, spell-charge) — a spell is a kind of ability and that's a separate, larger concern. This rename is scoped to the **pool + slot** concept only. Also leave the Phase-E client symbols (`PERK_RANK_BY_ID_MAP`, `initPerkRanksFromPaths`) — already correctly named.

### Task C1: Server rename (internals + wire field)

**Files:** the server files from the rename map + every consumer (`progression.go`, `state.go`, `state_spawn.go`, `debug_spawn.go`, `ability_autocast.go`, `ability_priority.go`, `path_ability_defs.go`, `perks_arch_mage.go`) + server tests (`arch_mage_bronze_pool_test.go`, `ability_exec_perk_parity_test.go`, `meteor_test.go`, `arcane_missiles_test.go`, `perks_arch_mage_test.go`, the renamed pool test files, and the Phase-A guard test `ability_pool_migration_test.go`).

- [ ] **Step 1: Apply the server rename map.** Rename the two files, then apply each symbol/field/comment rename. Use grep per old symbol to find sites; the field `PoolSpellsByRank` is on the `Unit` struct (state.go) so it ripples widely — rename the struct field and fix every reader. Rename the wire field `SpellSlotRank`→`AbilitySlotRank` and its json tag `spellSlotRank`→`abilitySlotRank` in `messages.go`.
- [ ] **Step 2: Build + vet + full suite.** `cd server && go build ./... && go vet ./... && go test ./internal/game/` → PASS + clean. Grep to confirm no `spell.?[Pp]ool`, `PoolSpells`, or `SpellSlot` symbols remain in non-comment Go (excluding the intentionally-kept `EffectiveSpell`/spell-charge cast vocabulary): `cd server && grep -rn "PoolSpells\|SpellSlotRank\|spellPoolFor\|spellSlotRank" --include=*.go` → empty.
- [ ] **Step 3: Commit** (human). NOTE: the wire json changed `spellSlotRank`→`abilitySlotRank`; the client (C2) must land together or spell-slot cells stop rendering at runtime.

### Task C2: Client rename (wire field + consumers)

**Files:** `protocol.ts`, `GameState.ts`, `GameState.archMageSpellSlot.test.ts` (→ renamed).

- [ ] **Step 1: Apply the client rename map.** `spellSlotRank`→`abilitySlotRank` on the `AbilitySnapshot` type (protocol.ts) and every reader in `GameState.ts` (`.filter((a) => !a.spellSlotRank)`, `spellSlotByRank`, `buildSpellSlotCell`, the `slot` usage in the perk-cell builder). Rename the test file + its refs.
- [ ] **Step 2: Verify.** `cd client/src/game-portal && npx vue-tsc -b && npx vitest run src/game/core` → clean + PASS. Grep: `grep -rn "spellSlotRank\|SpellSlot" src` → empty (except any unrelated comment you choose to keep).
- [ ] **Step 3: Commit** (human) — together with C1 (shared wire contract).

---

## Self-Review

- **Behavior invariant:** A1 guard proves arch_mage's candidate sets are unchanged; the self-contained silver pool reproduces the old cumulative union exactly (same 5 candidates, dedup still blocks repeats).
- **Dedup:** enforced in two places — backend `validatePathFile` (save-time hard error) and the editor picker (`availableAbilityOptionsForRank` excludes base + all pools). No path can grant an ability twice.
- **Mirror fidelity:** every `AbilityPoolsByRank` site has a `PerksByRank` counterpart listed above; the field round-trips through the same path-editor save flow.
- **Data unified:** `spell-pools.json` deleted; each path's pools live in its own file next to `perksByRank`.
- **Slot semantics:** per-rank Perk/Ability is a UI-level mutual exclusion; the backend reads both maps independently (no over-constraint).
