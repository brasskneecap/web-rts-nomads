# Standalone Perks SP2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an explicit per-rank `perksByRank` reference to `PathDef`, and make perk selection the UNION of eligibility-auto-match + those references — purely additive (behavior-identical until an author opts a perk in).

**Architecture:** `perksByRank map[string][]string` is added to `pathCatalogFile` beside the existing rank-keyed `Ranks` map, so it rides the whole existing path persistence/overlay/editor/HTTP/client plumbing for free. It's exposed to the runtime via a new per-path derived map (mirroring `abilitiesByPath`), and unioned into the single selection function `eligiblePerksForUnitAtRank` with dedup + the existing ID-sort (determinism preserved). The client path editor gains a per-rank Perk References section.

**Tech Stack:** Go (`internal/game`), TypeScript / Vue 3 (`game-portal`), Vitest, `go test`.

## Global Constraints

- Branch: `perks-standalone` (continues on top of SP1). Nothing to `main`. No push.
- **Purely additive; runtime behavior-identical with empty references.** `auto-match ∪ ∅ = auto-match` = today's SP1-preserved behavior. Determinism preserved: dedup + keep the existing `sort.Slice(... def.ID ...)` in the union. No `game`→`profile` write.
- Reference lives on `pathCatalogFile.PerksByRank map[string][]string` (JSON `perksByRank,omitempty`), keyed by rank (bronze/silver/gold). Rank keys validated via `validRankName`; each perk id validated via `perkDefLookup`.
- Referenced perk ids resolve FAIL-SAFE at selection (unknown id = not added, same discipline as an unknown ability id).
- No literal `cursor:` in new component CSS except `cursor: not-allowed` on forbidden states.
- Build gates: server `go build ./...` + `go vet ./...` + `go test ./...` (NOT gofmt). Client `npm run build` (`vue-tsc -b`) + `npm run test`. 3 pre-existing `ListEditorPanel.test.ts` failures expected; confirm no NEW failures.
- Per-task commits, explicit `git add <files>` (NEVER `-A`/`.`).

## File Structure

**Server:** `path_defs.go` (MODIFY: `PerksByRank` field + validation + derived-map struct/populate + reader), `path_persistence.go` (MODIFY: new global map + rebuild swap), `perk_defs.go` (MODIFY: the union in `eligiblePerksForUnitAtRank`).
**Client:** `game/units/pathEditorForm.ts` (MODIFY: `perksByRank` field + `MODELED_PATH_KEYS`), `components/UnitTypeEditorPanel.vue` (MODIFY: Perk References section).

---

## Task 1: `perksByRank` field + validation + derived-map plumbing + reader (server data layer)

**Files:**
- Modify: `server/internal/game/path_defs.go`, `server/internal/game/path_persistence.go`
- Test: `server/internal/game/path_persistence_test.go` (append) or a new `path_perkrefs_test.go`

**Interfaces:**
- Consumes: `validRankName`, `perkDefLookup`, the path derived-map plumbing (`pathDerivedMaps`, `newPathDerivedMaps`, `livePathDerivedMaps`, `registerPathFileInto`, the rebuild swap).
- Produces: `pathCatalogFile.PerksByRank`, `pathPerkRefsForRank(pathName, rank string) []string`.

- [ ] **Step 1: Write the failing test**

Append a test (mirror how existing path-file tests save + read; use `PATH_CATALOG_DIR`/`UNIT_CATALOG_DIR` temp dir isolation like the other path/perk tests). It must: save a path file with `PerksByRank`, and assert `pathPerkRefsForRank(path, "bronze")` returns the ids; assert `validatePathFile` rejects an unknown rank key and an unknown perk id.

```go
func TestPathPerksByRankRoundTripAndValidate(t *testing.T) {
	t.Setenv("UNIT_CATALOG_DIR", t.TempDir())
	// pick a real embedded perk id + a real unit/path to attach to
	perkID := ListPerkDefs()[0].ID
	// build a minimal path file with a perk ref (adapt the constructor to the
	// real pathCatalogFile builder/JSON the other path tests use)
	file := &pathCatalogFile{Path: "berserker", PerksByRank: map[string][]string{"bronze": {perkID}}}
	if err := validatePathFile(file, "berserker"); err != nil {
		t.Fatalf("valid file rejected: %v", err)
	}
	if err := SavePathDef("soldier", file); err != nil {
		t.Fatalf("SavePathDef: %v", err)
	}
	got := pathPerkRefsForRank("berserker", "bronze")
	if len(got) != 1 || got[0] != perkID {
		t.Fatalf("pathPerkRefsForRank = %v, want [%s]", got, perkID)
	}
	// bad rank key
	if err := validatePathFile(&pathCatalogFile{Path: "berserker", PerksByRank: map[string][]string{"platinum": {perkID}}}, "berserker"); err == nil {
		t.Fatal("expected unknown-rank rejection")
	}
	// unknown perk id
	if err := validatePathFile(&pathCatalogFile{Path: "berserker", PerksByRank: map[string][]string{"bronze": {"no_such_perk"}}}, "berserker"); err == nil {
		t.Fatal("expected unknown-perk rejection")
	}
}
```

(Adjust the `pathCatalogFile` construction + `SavePathDef` signature to match the real ones — read `path_defs.go`/`path_persistence.go` and the existing path tests first; the fields shown are the minimum this test needs.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/game/ -run TestPathPerksByRankRoundTripAndValidate`
Expected: FAIL — `PerksByRank`/`pathPerkRefsForRank` undefined.

- [ ] **Step 3: Add the `PerksByRank` field**

In `path_defs.go`, in `pathCatalogFile` (near `Ranks map[string]pathRankStatsJSON` at ~line 92), add:

```go
	// PerksByRank is the explicit opt-in perk references for this path, keyed by
	// rank (bronze/silver/gold). Each value is a list of standalone perk ids
	// (catalog/perks). At rank-up, these are UNION'd with the perks that match
	// via their own eligibility wildcards (see eligiblePerksForUnitAtRank).
	// Absent/empty ⇒ no explicit refs (auto-match only). Perk ids resolve
	// fail-safe at selection; validated at save via perkDefLookup.
	PerksByRank map[string][]string `json:"perksByRank,omitempty"`
```

- [ ] **Step 4: Validate `PerksByRank`**

In `validatePathFile` (`path_defs.go:431-492`), after the `Ranks` rank-key validation loop (before `return nil`), add a block that validates the perk-ref rank keys AND perk ids (sorted for deterministic error order, mirroring the existing rank + ability validation):

```go
	// PerksByRank: rank keys must be valid ranks; each perk id must resolve to a
	// registered standalone PerkDef so a typo fails loud at save. Sorted for a
	// deterministic first-error.
	perkRefRanks := make([]string, 0, len(file.PerksByRank))
	for rankName := range file.PerksByRank {
		perkRefRanks = append(perkRefRanks, rankName)
	}
	sort.Strings(perkRefRanks)
	for _, rankName := range perkRefRanks {
		if _, ok := validRankName[rankName]; !ok {
			return fmt.Errorf("unknown rank %q in \"perksByRank\" (want bronze/silver/gold)", rankName)
		}
		for _, perkID := range file.PerksByRank[rankName] {
			if perkID == "" {
				return fmt.Errorf("empty perk id in perksByRank[%q]", rankName)
			}
			if _, ok := perkDefLookup(perkID); !ok {
				return fmt.Errorf("perk %q in perksByRank[%q] has no registered PerkDef", perkID, rankName)
			}
		}
	}
```

- [ ] **Step 5: Add the derived map (mirror `abilitiesByPath` at every touch point)**

`perksByRank` needs a runtime lookup by path. Mirror the EXISTING per-path derived map `abilitiesByPath` (a `map[string][]string`), but nested by rank. Use Grep for `abilitiesByPath` and `pathAbilitiesByPath` to find EVERY touch point and add the analogue:
1. In the `pathDerivedMaps` struct (`path_defs.go:500-512`), add: `perkRefsByPath map[string]map[string][]string`.
2. In `newPathDerivedMaps` (`:516-530`), add: `perkRefsByPath: map[string]map[string][]string{},`.
3. In `livePathDerivedMaps` (`:537-551`), add: `perkRefsByPath: pathPerkRefsByPath,`.
4. Declare the package-global `var pathPerkRefsByPath = map[string]map[string][]string{}` next to the other `pathXByPath` globals (Grep `var pathAbilitiesByPath` to find the block).
5. In the rebuild swap in `path_persistence.go` (`rebuildDerivedPathMaps`, `:61-129` — Grep `pathAbilitiesByPath =` to find where the fresh maps are swapped into the globals), add `pathPerkRefsByPath = fresh.perkRefsByPath` alongside the other swaps.
6. In `registerPathFileInto` (`path_defs.go:580-670`), after the `file.Abilities` block, populate the derived map (deep-copy so the stored maps are independent of the caller's buffers):

```go
	if len(file.PerksByRank) > 0 {
		refs := make(map[string][]string, len(file.PerksByRank))
		for rankName, ids := range file.PerksByRank {
			cp := make([]string, len(ids))
			copy(cp, ids)
			refs[rankName] = cp
		}
		dst.perkRefsByPath[file.Path] = refs
	}
```

- [ ] **Step 6: Add the reader `pathPerkRefsForRank`**

Add a reader that mirrors the locking of the existing per-path readers (Grep for how `pathAbilitiesByPath` is read at runtime — e.g. a `pathAbilitiesFor...` helper taking `pathCatalogMu.RLock()`; match that lock discipline exactly):

```go
// pathPerkRefsForRank returns the explicit perk-id references authored on the
// given path for the given rank, or nil. Path ids are globally unique so the
// unit type is not part of the key. Caller must NOT hold pathCatalogMu.
func pathPerkRefsForRank(pathName, rank string) []string {
	pathCatalogMu.RLock()
	defer pathCatalogMu.RUnlock()
	byRank := pathPerkRefsByPath[pathName]
	if byRank == nil {
		return nil
	}
	return byRank[rank]
}
```

(Match the actual mutex name/read idiom of the sibling per-path readers; if reads happen through a different accessor, mirror it.)

- [ ] **Step 7: Run test + build + vet**

Run: `cd server && go test ./internal/game/ -run TestPathPerksByRank && go build ./... && go vet ./...`
Expected: PASS + clean.

- [ ] **Step 8: Commit**

```bash
git add server/internal/game/path_defs.go server/internal/game/path_persistence.go server/internal/game/path_persistence_test.go
git commit -m "feat(perks): PathDef.perksByRank reference field + validation + derived lookup"
```

---

## Task 2: The union in `eligiblePerksForUnitAtRank`

**Files:**
- Modify: `server/internal/game/perk_defs.go` (`eligiblePerksForUnitAtRank`)
- Test: `server/internal/game/perk_defs_test.go` (or a perks selection test file)

**Interfaces:**
- Consumes: `pathPerkRefsForRank` (Task 1), `perkDefLookup`, `snapshotPerkDefs`.

- [ ] **Step 1: Write the failing test**

Add a test that saves a path `perksByRank` ref for a perk whose OWN eligibility would NOT match the unit (e.g. a perk with `UnitType: "someotherunit"`), then asserts `eligiblePerksForUnitAtRank` includes it via the reference; asserts a perk matching BOTH halves appears exactly once; asserts the result is ID-sorted; and asserts that with NO refs the result equals the pre-union set. (Construct a `*Unit` with `UnitType`/`ProgressionPath`/`Rank` set; use `PERK_CATALOG_DIR`/`UNIT_CATALOG_DIR` temp dirs; author a synthetic perk + path ref via SavePerkDef/SavePathDef.)

```go
func TestEligiblePerksUnionWithReferences(t *testing.T) {
	t.Setenv("PERK_CATALOG_DIR", t.TempDir())
	t.Setenv("UNIT_CATALOG_DIR", t.TempDir())
	// A perk whose eligibility does NOT match unit soldier/berserker/bronze:
	if err := SavePerkDef(&PerkDef{ID: "ref_only_perk", UnitType: "nobody"}); err != nil {
		t.Fatal(err)
	}
	// Reference it explicitly on the path at bronze:
	if err := SavePathDef("soldier", &pathCatalogFile{Path: "berserker", PerksByRank: map[string][]string{"bronze": {"ref_only_perk"}}}); err != nil {
		t.Fatal(err)
	}
	unit := &Unit{UnitType: "soldier", ProgressionPath: "berserker", Rank: "bronze"}
	pool := eligiblePerksForUnitAtRank(unit, "bronze")
	// referenced perk present exactly once
	count := 0
	for _, d := range pool {
		if d.ID == "ref_only_perk" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("ref_only_perk count = %d, want 1", count)
	}
	// ID-sorted
	for i := 1; i < len(pool); i++ {
		if pool[i-1].ID > pool[i].ID {
			t.Fatal("pool not ID-sorted")
		}
	}
}
```

(Adapt `SavePathDef`/`pathCatalogFile`/`Unit` construction to the real signatures.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/game/ -run TestEligiblePerksUnion`
Expected: FAIL — the reference is not yet unioned in.

- [ ] **Step 3: Add the union**

In `eligiblePerksForUnitAtRank` (`perk_defs.go:324-345`), REPLACE the body so it tracks seen ids and unions the references before the sort:

```go
func eligiblePerksForUnitAtRank(unit *Unit, rank string) []*PerkDef {
	var eligible []*PerkDef
	seen := map[string]struct{}{}
	for _, def := range snapshotPerkDefs() {
		if def.UnitType != "" && def.UnitType != unit.UnitType {
			continue
		}
		if def.Path != "" && def.Path != unit.ProgressionPath {
			continue
		}
		if def.Rank != "" && def.Rank != rank {
			continue
		}
		eligible = append(eligible, def)
		seen[def.ID] = struct{}{}
	}
	// Union in the path's explicit per-rank perk references (SP2). A referenced
	// perk that already matched via eligibility is not added twice (dedup via
	// seen). Unknown ids resolve fail-safe (skipped). The ID-sort below keeps
	// rngPerks.Intn deterministic regardless of insertion order.
	for _, perkID := range pathPerkRefsForRank(unit.ProgressionPath, rank) {
		if _, dup := seen[perkID]; dup {
			continue
		}
		if def, ok := perkDefLookup(perkID); ok {
			eligible = append(eligible, def)
			seen[perkID] = struct{}{}
		}
	}
	// Sort by ID so rngPerks.Intn picks from a deterministic order regardless of
	// map iteration / insertion order (replay-reproducibility invariant).
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].ID < eligible[j].ID })
	return eligible
}
```

- [ ] **Step 4: Run test + build + vet + the perk/advancement suites**

Run: `cd server && go test ./internal/game/ -run 'TestEligiblePerksUnion|Perk|Advancement|Bronze' && go build ./... && go vet ./...`
Expected: PASS — the union test passes AND the existing perk/advancement/bronze-perk tests still pass (empty-refs behavior-identical).

- [ ] **Step 5: Commit**

```bash
git add server/internal/game/perk_defs.go server/internal/game/perk_defs_test.go
git commit -m "feat(perks): union path perk references into eligible-perk selection"
```

---

## Task 3: Client — `perksByRank` on the path form

**Files:**
- Modify: `client/src/game-portal/src/game/units/pathEditorForm.ts`
- Test: `client/src/game-portal/src/game/units/pathEditorForm.test.ts` (append, if it exists; else add a focused test)

**Interfaces:**
- Produces: `AuthoredPathDef.perksByRank?: Record<string, string[]>`, `'perksByRank'` in `MODELED_PATH_KEYS`.

- [ ] **Step 1: Write the failing test**

Append a test asserting `pathFormFromDef({..., perksByRank: {bronze: ['x']}})` puts `perksByRank` on the form (NOT in `remainder`), and `saveRequestFromPathForm` round-trips it; and that an unset `perksByRank` is omitted from the save request.

```ts
it('round-trips perksByRank as a modeled field', () => {
  const form = pathFormFromDef({ path: 'berserker', perksByRank: { bronze: ['tough'] } } as AuthoredPathDef)
  expect(form.perksByRank).toEqual({ bronze: ['tough'] })
  expect(form.remainder?.perksByRank).toBeUndefined()
  const out = saveRequestFromPathForm(form)
  expect(out.perksByRank).toEqual({ bronze: ['tough'] })
})
```

(Match the real import names in `pathEditorForm.ts`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client/src/game-portal && npx vitest run src/game/units/pathEditorForm.test.ts`
Expected: FAIL — `perksByRank` lands in `remainder` (not modeled).

- [ ] **Step 3: Add the field + modeled key**

In `pathEditorForm.ts`: add `perksByRank?: Record<string, string[]>` to the `AuthoredPathDef` interface, and add `'perksByRank'` to the `MODELED_PATH_KEYS` array (`:67-70`).

- [ ] **Step 4: Run test + build**

Run: `cd client/src/game-portal && npx vitest run src/game/units/pathEditorForm.test.ts && npm run build`
Expected: PASS + clean.

- [ ] **Step 5: Commit**

```bash
git add client/src/game-portal/src/game/units/pathEditorForm.ts client/src/game-portal/src/game/units/pathEditorForm.test.ts
git commit -m "feat(perks): perksByRank as a modeled path-form field"
```

---

## Task 4: Client — Perk References editor section

**Files:**
- Modify: `client/src/game-portal/src/components/UnitTypeEditorPanel.vue`
- Test: an existing `UnitTypeEditorPanel.*.test.ts` (extend) or a focused new test
- Reference (read first): the Ranks `SectionCard` (`UnitTypeEditorPanel.vue:613-623`) + `PATH_TABS` (`:1061-1064`) + the perk catalog fetch pattern used by the standalone `PerkEditorPanel.vue` (`fetchAuthoredPerkDefs`).

**Section structure (build to this):**
- Add `'perks'` to the **Combat** tab's `sections` array in `PATH_TABS` (alongside `'ranks'`).
- On mount (where the panel already fetches its catalogs), fetch the perk catalog (`fetchAuthoredPerkDefs()` from `@/game/perks/perkEditorApi`, or `GET /catalog/perks`) into a ref for the picker options.
- Add a **"Perk References"** `SectionCard` `v-show="activePathTab === pathSectionTab('perks')"` `:index="pathSectionIndex('perks')"` that binds `pathForm.perksByRank`. For each rank (`bronze`/`silver`/`gold`): show the current referenced perk ids (id + displayName, an "inert" hint when `!wired`) with remove buttons, and an add control (a `<select>`/datalist of catalog perk ids not already referenced at that rank). Adding/removing writes back into `pathForm.value.perksByRank` (create the map / rank array as needed; keep it a plain `Record<string, string[]>`).
- No save-flow change — `perksByRank` persists inside `req.path` via the existing `savePath()`.
- No literal `cursor:`.

- [ ] **Step 1: Write the failing test**

Extend a `UnitTypeEditorPanel` test (or add one) that mounts the panel with a path selected, stubs `/catalog/perks`, navigates to the Combat tab's Perk References section, and asserts adding a perk id writes it into the form's `perksByRank[bronze]`. If the existing test harness makes deep interaction hard, a lighter assertion (the section renders + lists a stubbed perk) is acceptable — but at minimum assert the section exists and reads the catalog.

- [ ] **Step 2: Run test to verify it fails** — `npx vitest run <the test>` → FAIL.

- [ ] **Step 3: Read the references, then build the section** per the structure above.

- [ ] **Step 4: Gates**

Run: `cd client/src/game-portal && npm run build && npm run test`
Expected: build clean; suite green except the 3 known `ListEditorPanel` failures; `UnitTypeEditorPanel.*` tests pass.

- [ ] **Step 5: Commit**

```bash
git add client/src/game-portal/src/components/UnitTypeEditorPanel.vue <the test file>
git commit -m "feat(perks): per-rank Perk References editor section on the path form"
```

---

## Task 5: Final verification

- [ ] **Step 1: Full server suite** — `cd server && go build ./... && go vet ./... && go test ./...` (known pre-existing `internal/ws`/`cmd/api` failures excepted; `internal/game`+`internal/http` MUST pass). Confirm the perk/advancement/bronze-perk selection tests pass (empty-refs behavior-identical).
- [ ] **Step 2: Determinism** — run the game determinism/seed tests to confirm perk rolls are byte-identical with empty refs (the dedup+sort preserves order).
- [ ] **Step 3: Full client suite** — `cd client/src/game-portal && npm run build && npm run test` (only the 3 known `ListEditorPanel` failures).
- [ ] **Step 4: Manual E2E (hard gate — running server):** Unit Types editor → open a path → Combat → **Perk References** → add a perk to `bronze` whose own eligibility does NOT match the unit → Save → Play → confirm the unit can now roll it at bronze; remove it → gone; confirm a path with no refs behaves exactly as before.
- [ ] **Step 5: Confirm clean tree** — `git status` + `git log --oneline` for the task commits.

---

## Self-Review Notes (for the executor)

- **Spec coverage:** §1 field+validation → Task 1; §2 derived lookup + union → Tasks 1-2; §3 form field → Task 3; §4 editor section → Task 4; testing/determinism → per-task + Task 5.
- **Behavior-identical guarantee:** empty `perksByRank` ⇒ `pathPerkRefsForRank` returns nil ⇒ the union loop is a no-op ⇒ the pool == the pre-SP2 eligibility set; the ID-sort is unchanged. Task 2 Step 4 re-runs the perk/advancement/bronze suites to confirm.
- **Determinism:** the union dedups (via `seen`) and keeps the existing `sort.Slice(... def.ID ...)`, so `rngPerks.Intn` still picks from a deterministic, dup-free, ID-sorted order.
- **Watch items:** (1) Task 1 Step 5 — mirror `abilitiesByPath` at EVERY derived-map touch point (struct, new/live, global var, rebuild swap, registerPathFileInto); a missed swap-site means overlay edits wouldn't take effect. Grep `abilitiesByPath`/`pathAbilitiesByPath` to enumerate. (2) `pathPerkRefsForRank` must use the SAME `pathCatalogMu` read discipline as the sibling per-path readers (don't double-lock if the caller already holds it — confirm the tick-time call path; the existing per-path runtime readers are the reference for lock-safety). (3) The test constructors for `pathCatalogFile`/`Unit`/`SavePathDef` must match the REAL signatures — read the existing path tests first.
