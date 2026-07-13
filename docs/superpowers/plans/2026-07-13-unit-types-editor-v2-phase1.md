# Unit-Types Editor v2 — Phase 1: Factions, Archetypes & Stat Floors

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The unit editor gains a faction filter and faction creation, an archetype dropdown backed by the real combat-profile registry, real pickers for every cross-referenced field, and blank units that actually function instead of spawning as 0-HP statues.

**Architecture:** A faction becomes a persisted record (`catalog/units/<faction>/faction.json`) with an embed loader + writable overlay, mirroring the existing unit/item editor pattern exactly. Five already-implemented-but-unrouted `List*` functions get catalog endpoints so the client's free-text fields become dropdowns. `validateUnitDef` gains stat floors, guarded by a test that runs it over the entire embedded catalog.

**Tech Stack:** Go 1.22 (server, module `webrts/server`, root `server/`), Vue 3 + TypeScript SPA (`client/src/game-portal`, vitest).

**Spec:** `docs/superpowers/specs/2026-07-13-unit-types-editor-v2-design.md` (§3, and the `/catalog/*` rows of §8)

**Phase 1 of 5.** Phases 2–5 (runtime sprite overlay, browser packer + art ingest, per-facing attack origins, path entities) get their own plans. This phase is independently shippable: it touches no renderer and no simulation code.

---

## Global Constraints

- **Do not run `git commit`.** The user handles all staging and commits. Each task ends with a **Checkpoint** listing the files touched and the gates that must be green — stop there and report.
- Follow the item/unit-editor overlay + disk template (`item_persistence.go`, `unit_persistence.go`). Do not invent a new persistence pattern.
- Reuse the existing in-package `editorValidationError{}` / `IsEditorValidationError` (defined in `item_editor.go`). Do NOT redefine.
- `unitIDPattern = ^[a-z0-9_]+$` guards every path segment we write, enforced at BOTH the handler and persistence layers.
- Overlay is registered only AFTER a successful disk write.
- `game/` must NOT import `profile`. `Locked` suffix = caller holds `s.mu`. Deterministic sim — this phase touches no sim code, keep it that way.
- No literal `cursor:` declarations in component CSS (global rules cover it); `cursor: not-allowed` on forbidden-action states only.
- Never modify the item editor (`ItemEditorPanel.vue`, `game/items/*`, `item_editor.go`, `item_persistence.go`) or the old map editor (`MapEditorPanel.vue`, `views/Editor.vue`).
- Go commands run from `server/`; client commands from `client/src/game-portal`.
- Client typecheck is **`npx vue-tsc -b`** (build mode). `--noEmit` false-cleans because the root tsconfig is solution-style.
- `gofmt -l` flags the whole checkout (CRLF) — use `go vet` / `go build` as the gates.
- Known pre-existing failure, unrelated to this work: `cmd/api` `TestServerReadyLineAndStdinShutdown`. Introduce no NEW failures.

---

### Task 1: Server — expose the five unrouted catalogs

`ListProjectileDefs()`, `ListAbilityDefs()`, `ListEffectDefs()`, and `DamageTypes()` are all implemented with **zero callers**. Routing them is what turns the editor's free-text fields into pickers. `ListArchetypes()` is new — the archetype set is the key set of `combatProfiles`, which is also the valid set for `combatProfile`, so one endpoint serves both dropdowns.

**Files:**
- Modify: `server/internal/game/combat_ai_profiles.go` (add `ListArchetypes` at the end of the file)
- Modify: `server/internal/http/router.go` (add routes after the `/catalog/perks` block, ~line 65)
- Test: `server/internal/game/archetype_list_test.go` (create)

**Interfaces:**
- Produces: `func ListArchetypes() []string` — every key of `combatProfiles`, sorted. Consumed by the client's Archetype and Combat Profile selects (Task 7).
- Produces routes: `GET /catalog/archetypes`, `/catalog/projectiles`, `/catalog/abilities`, `/catalog/effects`, `/catalog/damage-types`.

- [ ] **Step 1: Write the failing test**

Create `server/internal/game/archetype_list_test.go`. Note it derives its expectations from `combatProfiles` itself rather than pinning a hardcoded list — the profile set is content that will grow.

```go
package game

import (
	"sort"
	"testing"
)

func TestListArchetypes_ReturnsEverySortedProfileKey(t *testing.T) {
	got := ListArchetypes()
	if len(got) != len(combatProfiles) {
		t.Fatalf("ListArchetypes returned %d entries, want %d (one per combat profile)", len(got), len(combatProfiles))
	}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("ListArchetypes must be sorted for stable catalog output, got %v", got)
	}
	for _, key := range got {
		if _, ok := combatProfiles[key]; !ok {
			t.Fatalf("ListArchetypes returned %q, which is not a combat profile", key)
		}
	}
	// Every profile must be reachable — a missing one silently hides an
	// archetype from the editor dropdown.
	seen := make(map[string]bool, len(got))
	for _, key := range got {
		seen[key] = true
	}
	for key := range combatProfiles {
		if !seen[key] {
			t.Fatalf("combat profile %q is missing from ListArchetypes", key)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd server && go test ./internal/game/ -run TestListArchetypes -count=1`
Expected: FAIL — `undefined: ListArchetypes`.

- [ ] **Step 3: Add `ListArchetypes`**

Append to `server/internal/game/combat_ai_profiles.go`:

```go
// ListArchetypes returns every registered combat-profile key, sorted.
//
// This is the valid set for BOTH UnitDef.Archetype and UnitDef.CombatProfile:
// resolveCombatProfile tries CombatProfile first, then falls back to Archetype
// as a profile key, then to an inferred default. An archetype outside this set
// is not rejected (it silently degrades to the soldier profile), which is
// exactly why the editor needs to show the author the real list.
func ListArchetypes() []string {
	out := make([]string, 0, len(combatProfiles))
	for key := range combatProfiles {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
```

If `combat_ai_profiles.go` does not already import `sort`, add it.

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd server && go test ./internal/game/ -run TestListArchetypes -count=1`
Expected: PASS.

- [ ] **Step 5: Add the five catalog routes**

In `server/internal/http/router.go`, immediately after the `/catalog/perks` handler block (ends ~line 65), add:

```go
	mux.HandleFunc("/catalog/archetypes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"archetypes": game.ListArchetypes(),
		})
	})

	mux.HandleFunc("/catalog/projectiles", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"projectiles": game.ListProjectileDefs(),
		})
	})

	mux.HandleFunc("/catalog/abilities", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"abilities": game.ListAbilityDefs(),
		})
	})

	mux.HandleFunc("/catalog/effects", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"effects": game.ListEffectDefs(),
		})
	})

	mux.HandleFunc("/catalog/damage-types", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"damageTypes": game.DamageTypes(),
		})
	})
```

- [ ] **Step 6: Gates**

Run: `cd server && go build ./... && go vet ./internal/game/ ./internal/http/`
Expected: clean.
Run: `cd server && go test ./internal/game/ -run TestListArchetypes -count=1`
Expected: PASS.

- [ ] **Step 7: Checkpoint (do not commit)**

Files touched: `server/internal/game/combat_ai_profiles.go`, `server/internal/game/archetype_list_test.go`, `server/internal/http/router.go`. Report gates green and stop.

---

### Task 2: Server — the faction registry (`faction_defs.go`)

**This task contains the one change that would otherwise crash the server.** `loadUnitDefsByType` panics on *any* non-directory entry inside a faction folder ([unit_defs.go:260-262](../../server/internal/game/unit_defs.go#L260-L262)), so dropping a `faction.json` in there — the whole point of this task — is a startup panic until the loader learns to skip it. The loader change and the first real `faction.json` land together, so the panic path is proven closed by a test that could not otherwise exist.

**Files:**
- Create: `server/internal/game/faction_defs.go`
- Create: `server/internal/game/catalog/units/human/faction.json`
- Modify: `server/internal/game/unit_defs.go` (the `!entry.IsDir()` panic at lines 260-262)
- Test: `server/internal/game/faction_defs_test.go` (create)

**Interfaces:**
- Produces: `type FactionDef struct { ID, DisplayName string; Order int }`
- Produces: `const factionMetaFileName = "faction.json"`
- Produces: `func ListFactions() []FactionDef` — embedded records, plus a synthesized default for any faction a unit references that has no record. Made overlay-aware in Task 3.
- Produces: `func defaultFactionDef(id string) FactionDef`, `func normalizeFactionDef(id string, def FactionDef) FactionDef` — consumed by `faction_persistence.go` (Task 3).
- Consumes: `unitDefsFS` (the existing `//go:embed catalog/units`), `ListUnitDefs()`.

**Deliberate asymmetry:** only `human` gets a `faction.json` in this task. The other three factions (`raider`, `wildborne`, `witherborne`) stay record-less on purpose, so the test proves *both* branches against real catalog data — the parsed-record path and the synthesized-default path.

- [ ] **Step 1: Write the failing test**

Create `server/internal/game/faction_defs_test.go`:

```go
package game

import "testing"

func factionByID(t *testing.T, id string) FactionDef {
	t.Helper()
	for _, f := range ListFactions() {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("faction %q not found in ListFactions()", id)
	return FactionDef{}
}

// human ships a faction.json — its displayName must come from the file, and the
// loader must not panic on the file's presence inside a faction directory.
func TestListFactions_ReadsAuthoredRecord(t *testing.T) {
	human := factionByID(t, "human")
	if human.DisplayName != "Human" {
		t.Fatalf("human displayName = %q, want %q (from faction.json)", human.DisplayName, "Human")
	}
}

// The other faction dirs have no faction.json and must still be real factions
// with a readable label — that is the "no new files needed" guarantee.
func TestListFactions_SynthesizesMissingRecord(t *testing.T) {
	wither := factionByID(t, "witherborne")
	if wither.DisplayName != "Witherborne" {
		t.Fatalf("witherborne displayName = %q, want titleized %q", wither.DisplayName, "Witherborne")
	}
}

// Every faction referenced by a unit must appear — otherwise the editor's
// filter would hide units that exist.
func TestListFactions_CoversEveryFactionAUnitReferences(t *testing.T) {
	present := map[string]bool{}
	for _, f := range ListFactions() {
		present[f.ID] = true
	}
	for _, def := range ListUnitDefs() {
		if def.Faction == "" {
			continue
		}
		if !present[def.Faction] {
			t.Fatalf("unit %q has faction %q, which ListFactions() omits", def.Type, def.Faction)
		}
	}
}

func TestTitleizeFactionID(t *testing.T) {
	cases := map[string]string{
		"human":       "Human",
		"witherborne": "Witherborne",
		"night_elf":   "Night Elf",
	}
	for in, want := range cases {
		if got := titleizeFactionID(in); got != want {
			t.Fatalf("titleizeFactionID(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd server && go test ./internal/game/ -run TestListFactions -count=1`
Expected: FAIL — `undefined: FactionDef` / `undefined: ListFactions`.

- [ ] **Step 3: Create `faction_defs.go`**

```go
package game

import (
	"encoding/json"
	"io/fs"
	"sort"
	"strings"
)

// factionMetaFileName is the per-faction metadata record, living beside the
// unit directories at catalog/units/<faction>/faction.json. It is NOT a unit —
// every catalog walk that expects unit directories must skip it by name.
const factionMetaFileName = "faction.json"

// FactionDef is a faction's presentation record.
//
// The DIRECTORY is the source of truth for a faction's existence; faction.json
// only adds metadata. A faction directory without one is still a perfectly
// valid faction (its record is synthesized), which is why the factions that
// predate this file need no new JSON. The record exists so that (a) a faction
// can be created in the editor before it owns any units, and (b) the editor can
// show "Witherborne" instead of "witherborne".
type FactionDef struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	// Order sorts the editor's faction filter. Ties fall back to ID.
	Order int `json:"order,omitempty"`
}

var embeddedFactions = loadEmbeddedFactions()

func loadEmbeddedFactions() map[string]FactionDef {
	entries, err := fs.ReadDir(unitDefsFS, "catalog/units")
	if err != nil {
		panic("catalog/units: " + err.Error())
	}
	result := make(map[string]FactionDef, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		def := defaultFactionDef(id)
		if raw, rerr := unitDefsFS.ReadFile("catalog/units/" + id + "/" + factionMetaFileName); rerr == nil {
			var parsed FactionDef
			if uerr := json.Unmarshal(raw, &parsed); uerr != nil {
				panic("catalog/units/" + id + "/" + factionMetaFileName + ": " + uerr.Error())
			}
			if parsed.ID != "" && parsed.ID != id {
				panic("catalog/units/" + id + "/" + factionMetaFileName + `: id "` + parsed.ID + `" does not match directory "` + id + `"`)
			}
			def = normalizeFactionDef(id, parsed)
		}
		result[id] = def
	}
	return result
}

// defaultFactionDef is the record a faction directory gets when it has no
// faction.json of its own.
func defaultFactionDef(id string) FactionDef {
	return FactionDef{ID: id, DisplayName: titleizeFactionID(id)}
}

// normalizeFactionDef forces the record's id to match its directory and fills a
// blank display name, so a hand-written or editor-posted record is always
// coherent with where it lives.
func normalizeFactionDef(id string, def FactionDef) FactionDef {
	def.ID = id
	if strings.TrimSpace(def.DisplayName) == "" {
		def.DisplayName = titleizeFactionID(id)
	}
	return def
}

// titleizeFactionID turns "witherborne" into "Witherborne" and "night_elf" into
// "Night Elf" — a readable fallback label for a faction with no record.
func titleizeFactionID(id string) string {
	words := strings.Split(id, "_")
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

// ListFactions returns every known faction, sorted by Order then ID.
//
// A faction is "known" if it has an embedded directory OR any unit claims it.
// The second clause matters: an editor-created unit can be saved into a brand
// new faction, and the filter must never hide a unit that exists.
func ListFactions() []FactionDef {
	merged := make(map[string]FactionDef, len(embeddedFactions))
	for id, def := range embeddedFactions {
		merged[id] = def
	}
	for _, unit := range ListUnitDefs() {
		if unit.Faction == "" {
			continue
		}
		if _, ok := merged[unit.Faction]; !ok {
			merged[unit.Faction] = defaultFactionDef(unit.Faction)
		}
	}
	out := make([]FactionDef, 0, len(merged))
	for _, def := range merged {
		out = append(out, def)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].ID < out[j].ID
	})
	return out
}
```

- [ ] **Step 4: Teach the unit loader to skip `faction.json`**

In `server/internal/game/unit_defs.go`, replace the unit-entry loop's non-directory panic (lines 259-262):

```go
		for _, entry := range unitEntries {
			if !entry.IsDir() {
				panic("catalog/units/" + factionKey + ": unexpected file " + entry.Name() + " — units must live at catalog/units/<faction>/<unit>/<unit>.json")
			}
```

with:

```go
		for _, entry := range unitEntries {
			if !entry.IsDir() {
				// faction.json is the faction's own metadata record, owned by
				// faction_defs.go — not a unit. Every other loose file here is
				// a mistake and still panics.
				if entry.Name() == factionMetaFileName {
					continue
				}
				panic("catalog/units/" + factionKey + ": unexpected file " + entry.Name() + " — units must live at catalog/units/<faction>/<unit>/<unit>.json")
			}
```

- [ ] **Step 5: Add the first real faction record**

Create `server/internal/game/catalog/units/human/faction.json`:

```json
{
  "id": "human",
  "displayName": "Human",
  "order": 1
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd server && go test ./internal/game/ -run 'TestListFactions|TestTitleize' -count=1`
Expected: PASS, all four.

If instead the package **panics at init** with `unexpected file faction.json`, Step 4 was not applied — the loader is still rejecting the record. Fix Step 4 before continuing; every other test in the package will be failing too.

- [ ] **Step 7: Gates**

Run: `cd server && go build ./... && go vet ./internal/game/`
Expected: clean.
Run: `cd server && go test ./internal/game/ -count=1 2>&1 | grep -E "^(--- FAIL|FAIL|ok)"`
Expected: `ok` — the whole game package still loads its catalog. A panic here means a faction directory broke the embed loader.

- [ ] **Step 8: Checkpoint (do not commit)**

Files touched: `server/internal/game/faction_defs.go`, `server/internal/game/faction_defs_test.go`, `server/internal/game/unit_defs.go`, `server/internal/game/catalog/units/human/faction.json`.

---

### Task 3: Server — faction persistence, editor orchestrator, and routes

**Files:**
- Create: `server/internal/game/faction_persistence.go`
- Create: `server/internal/game/faction_editor.go`
- Modify: `server/internal/game/faction_defs.go` (`ListFactions` → overlay-aware)
- Modify: `server/internal/game/unit_persistence.go` (skip `faction.json`; tighten `removeUnitOverrideFiles`)
- Modify: `server/internal/http/router.go` (add `GET /catalog/factions`)
- Modify: `server/internal/http/editor_handlers.go` (add `POST /factions`, `DELETE /factions/{id}`)
- Modify: `server/cmd/api/main.go` (call `LoadPersistedFactionsIntoOverlay`, line ~57)
- Test: `server/internal/game/faction_persistence_test.go` (**already exists** — Task 2 created it during code review with `TestDeleteUnitOverride_DoesNotEatFactionRecords`. EXTEND it; do not overwrite it, and do not re-add that test.)

**Interfaces:**
- Produces: `func SaveFactionDef(def *FactionDef) error`, `func DeleteFactionOverride(id string) (existed bool, err error)`, `func FactionUnitTypes(id string) []string`, `func FactionIsEmbedded(id string) bool`, `func LoadPersistedFactionsIntoOverlay()`.
- Produces: `type EditorFactionSaveRequest struct { Faction FactionDef }`, `func SaveEditorFaction(req EditorFactionSaveRequest) error`, `func DeleteEditorFaction(id string) (bool, error)`.
- Consumes: `resolveUnitsDir()`, `unitIDPattern`, `editorValidationError{}` (all existing), `normalizeFactionDef` / `defaultFactionDef` / `factionMetaFileName` / `embeddedFactions` (Task 2).

**Two hazards this task closes:**

1. `loadPersistedUnitsFromDir` walks every `.json` in the writable tree and parses it as a `UnitDef`. It would try to parse `faction.json` and log a spurious "skipped file" warning on every boot. Skip it by name.
2. `removeUnitOverrideFiles` deletes *any* file named `<unitType>.json` anywhere under the units dir. A unit legally named `faction` (it matches `unitIDPattern`) would make deleting that unit **delete every faction record in the catalog.** Constrain the match to files whose parent directory is the unit type — which is the only legal location for an override anyway.

- [ ] **Step 1: Write the failing test**

Create `server/internal/game/faction_persistence_test.go`:

```go
package game

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveFactionDef_WritesRecordAndOverlays(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	t.Cleanup(func() {
		runtimeFactionsMu.Lock()
		delete(runtimeFactions, "night_elf")
		runtimeFactionsMu.Unlock()
	})

	def := FactionDef{ID: "night_elf", DisplayName: "Night Elf", Order: 9}
	if err := SaveFactionDef(&def); err != nil {
		t.Fatalf("SaveFactionDef: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "night_elf", "faction.json")); err != nil {
		t.Fatalf("expected faction record on disk: %v", err)
	}

	// An editor-created faction exists immediately, with zero units — that is
	// the whole point of the registry.
	var found bool
	for _, f := range ListFactions() {
		if f.ID == "night_elf" {
			found = true
			if f.DisplayName != "Night Elf" {
				t.Fatalf("displayName = %q, want %q", f.DisplayName, "Night Elf")
			}
		}
	}
	if !found {
		t.Fatal("saved faction is missing from ListFactions()")
	}
}

func TestSaveFactionDef_BlankDisplayNameIsTitleized(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	t.Cleanup(func() {
		runtimeFactionsMu.Lock()
		delete(runtimeFactions, "sun_kin")
		runtimeFactionsMu.Unlock()
	})

	def := FactionDef{ID: "sun_kin"}
	if err := SaveFactionDef(&def); err != nil {
		t.Fatalf("SaveFactionDef: %v", err)
	}
	for _, f := range ListFactions() {
		if f.ID == "sun_kin" && f.DisplayName != "Sun Kin" {
			t.Fatalf("displayName = %q, want titleized fallback %q", f.DisplayName, "Sun Kin")
		}
	}
}

func TestSaveFactionDef_RejectsBadID(t *testing.T) {
	t.Setenv("UNIT_CATALOG_DIR", t.TempDir())
	def := FactionDef{ID: "../evil", DisplayName: "Evil"}
	if err := SaveFactionDef(&def); err == nil {
		t.Fatal("expected bad-id rejection")
	}
}

// Deleting a faction that still owns units would orphan them out of every
// filter — and in the dev tree, where the writable dir IS the source tree,
// it would be deleting real catalog content.
func TestDeleteFaction_RefusesWhileUnitsRemain(t *testing.T) {
	t.Setenv("UNIT_CATALOG_DIR", t.TempDir())
	if len(FactionUnitTypes("human")) == 0 {
		t.Fatal("precondition: human must own units in the embedded catalog")
	}
	_, err := DeleteFactionOverride("human")
	if err == nil {
		t.Fatal("expected deletion of a populated faction to be refused")
	}
}

func TestDeleteFaction_RemovesEmptyFaction(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	def := FactionDef{ID: "doomed", DisplayName: "Doomed"}
	if err := SaveFactionDef(&def); err != nil {
		t.Fatalf("SaveFactionDef: %v", err)
	}
	existed, err := DeleteFactionOverride("doomed")
	if err != nil || !existed {
		t.Fatalf("DeleteFactionOverride existed=%v err=%v", existed, err)
	}
	if _, serr := os.Stat(filepath.Join(dir, "doomed")); !os.IsNotExist(serr) {
		t.Fatal("expected the empty faction directory to be removed")
	}
	for _, f := range ListFactions() {
		if f.ID == "doomed" {
			t.Fatal("deleted faction still listed")
		}
	}
}

// A unit legally named "faction" must not take the faction records down with
// it when deleted — removeUnitOverrideFiles must only match <type>/<type>.json.
func TestDeleteUnitOverride_DoesNotEatFactionRecords(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	t.Cleanup(func() {
		runtimeUnitsMu.Lock()
		delete(runtimeUnits, "faction")
		runtimeUnitsMu.Unlock()
		runtimeFactionsMu.Lock()
		delete(runtimeFactions, "test_faction")
		runtimeFactionsMu.Unlock()
	})

	fac := FactionDef{ID: "test_faction", DisplayName: "Test Faction"}
	if err := SaveFactionDef(&fac); err != nil {
		t.Fatalf("SaveFactionDef: %v", err)
	}
	unit := UnitDef{
		Type: "faction", Faction: "test_faction", Name: "Faction",
		HP: 10, MoveSpeed: 1, Damage: 1, AttackRange: 1, AttackSpeed: 1,
	}
	if err := SaveUnitDef(&unit); err != nil {
		t.Fatalf("SaveUnitDef: %v", err)
	}
	if _, err := DeleteUnitOverride("faction"); err != nil {
		t.Fatalf("DeleteUnitOverride: %v", err)
	}
	recordPath := filepath.Join(dir, "test_faction", "faction.json")
	if _, err := os.Stat(recordPath); err != nil {
		t.Fatalf("deleting a unit named \"faction\" destroyed the faction record: %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd server && go test ./internal/game/ -run 'TestSaveFactionDef|TestDeleteFaction|TestDeleteUnitOverride_DoesNot' -count=1`
Expected: FAIL — `undefined: SaveFactionDef` / `undefined: runtimeFactions`.

- [ ] **Step 3: Create `faction_persistence.go`**

```go
package game

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

var (
	runtimeFactionsMu sync.RWMutex
	runtimeFactions   = map[string]FactionDef{}
)

// SaveFactionDef validates and writes <dir>/<id>/faction.json, then registers
// the record in the overlay. MkdirAll is what lets a faction exist before it
// owns any units — the directory is the faction.
func SaveFactionDef(def *FactionDef) error {
	if !unitIDPattern.MatchString(def.ID) {
		return fmt.Errorf("faction id %q must match %s", def.ID, unitIDPattern)
	}
	normalized := normalizeFactionDef(def.ID, *def)
	dir, err := resolveUnitsDir()
	if err != nil {
		return err
	}
	outDir := filepath.Join(dir, normalized.ID)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, factionMetaFileName), raw, 0o644); err != nil {
		return err
	}
	runtimeFactionsMu.Lock()
	runtimeFactions[normalized.ID] = normalized
	runtimeFactionsMu.Unlock()
	return nil
}

// FactionUnitTypes returns the unit types currently claiming a faction, sorted.
// Empty ⇒ the faction is safe to delete.
func FactionUnitTypes(id string) []string {
	var types []string
	for _, def := range ListUnitDefs() {
		if def.Faction == id {
			types = append(types, def.Type)
		}
	}
	sort.Strings(types)
	return types
}

// DeleteFactionOverride removes a faction's record, and its directory if that
// leaves it empty.
//
// It refuses while any unit still claims the faction. Two reasons: those units
// would vanish from every faction filter, and in the dev tree the writable dir
// IS the source tree — removing a populated faction directory would delete real
// catalog content. The directory is only ever removed when empty, never
// recursively.
func DeleteFactionOverride(id string) (existed bool, err error) {
	if !unitIDPattern.MatchString(id) {
		return false, nil // never a valid faction id; also blocks path traversal
	}
	if owned := FactionUnitTypes(id); len(owned) > 0 {
		return false, fmt.Errorf(
			"faction %q still has %d unit(s): %s — move or delete them before deleting the faction",
			id, len(owned), strings.Join(owned, ", "))
	}
	dir, derr := resolveUnitsDir()
	if derr != nil {
		return false, derr
	}
	factionDir := filepath.Join(dir, id)
	removed := false
	if rerr := os.Remove(filepath.Join(factionDir, factionMetaFileName)); rerr == nil {
		removed = true
	}
	if entries, rerr := os.ReadDir(factionDir); rerr == nil && len(entries) == 0 {
		_ = os.Remove(factionDir)
	}
	runtimeFactionsMu.Lock()
	_, inOverlay := runtimeFactions[id]
	delete(runtimeFactions, id)
	runtimeFactionsMu.Unlock()
	return removed || inOverlay, nil
}

// FactionIsEmbedded reports whether a faction directory exists in the embed.
func FactionIsEmbedded(id string) bool {
	_, ok := embeddedFactions[id]
	return ok
}

// LoadPersistedFactionsIntoOverlay overlays writable faction records onto the
// embed at startup.
func LoadPersistedFactionsIntoOverlay() {
	dir, err := resolveUnitsDir()
	if err != nil {
		slog.Info("persisted factions: no writable units dir; using embedded factions only", "err", err)
		return
	}
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		return
	}
	loaded := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		raw, ferr := os.ReadFile(filepath.Join(dir, entry.Name(), factionMetaFileName))
		if ferr != nil {
			continue // a faction directory with no record is still a valid faction
		}
		var def FactionDef
		if uerr := json.Unmarshal(raw, &def); uerr != nil {
			slog.Warn("persisted factions: skipped record", "faction", entry.Name(), "err", uerr)
			continue
		}
		normalized := normalizeFactionDef(entry.Name(), def)
		runtimeFactionsMu.Lock()
		runtimeFactions[normalized.ID] = normalized
		runtimeFactionsMu.Unlock()
		loaded++
	}
	if loaded > 0 {
		slog.Info("persisted factions: overlaid on embedded catalog", "count", loaded, "dir", dir)
	}
}
```

- [ ] **Step 4: Make `ListFactions` overlay-aware**

In `server/internal/game/faction_defs.go`, in `ListFactions`, insert the overlay merge between the embedded loop and the unit-derived loop (overlay wins over embed; a unit-derived synthesis only fills genuine gaps):

```go
	merged := make(map[string]FactionDef, len(embeddedFactions))
	for id, def := range embeddedFactions {
		merged[id] = def
	}
	runtimeFactionsMu.RLock()
	for id, def := range runtimeFactions {
		merged[id] = def
	}
	runtimeFactionsMu.RUnlock()
	for _, unit := range ListUnitDefs() {
```

- [x] **Step 5: Close the two `unit_persistence.go` hazards** — **DONE IN TASK 2.** Pulled forward during code review, because Task 2 introduced `human/faction.json` and leaving these open meant shipping an intermediate state that logged a spurious warn on every boot. Both fixes and their regression tests are already in the tree. Verify they are present, then move to Step 6. The original instructions are retained below for reference only.

In `server/internal/game/unit_persistence.go`, in `loadPersistedUnitsFromDir`, add the faction-record skip immediately before the `.json` suffix check:

```go
		if d.Name() == factionMetaFileName {
			return nil // owned by faction_persistence.go, not a unit
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}
```

In the same file, in `removeUnitOverrideFiles`, constrain the match to the only legal override location — `<faction>/<type>/<type>.json`. Replace:

```go
		if d.Name() == target {
			if rerr := os.Remove(path); rerr == nil {
				removed = true
			}
		}
```

with:

```go
		// Match ONLY <faction>/<type>/<type>.json. Without the parent-dir check,
		// deleting a unit legally named "faction" would remove every
		// faction.json record in the catalog.
		if d.Name() == target && filepath.Base(filepath.Dir(path)) == unitType {
			if rerr := os.Remove(path); rerr == nil {
				removed = true
			}
		}
```

- [ ] **Step 6: Create `faction_editor.go`**

```go
package game

import "fmt"

// EditorFactionSaveRequest is the body of POST /factions.
type EditorFactionSaveRequest struct {
	Faction FactionDef `json:"faction"`
}

// SaveEditorFaction validates then persists a faction record. Validation
// failures are wrapped as editorValidationError so the handler returns 400.
func SaveEditorFaction(req EditorFactionSaveRequest) error {
	faction := req.Faction
	if !unitIDPattern.MatchString(faction.ID) {
		return editorValidationError{fmt.Errorf("faction id %q must match %s", faction.ID, unitIDPattern)}
	}
	return SaveFactionDef(&faction)
}

// DeleteEditorFaction removes a faction record. "Still has units" is a
// validation error, not a 500 — it is a state the author can fix, and the
// message names the units so they can fix it.
func DeleteEditorFaction(id string) (existed bool, err error) {
	existed, err = DeleteFactionOverride(id)
	if err != nil {
		return false, editorValidationError{err}
	}
	return existed, nil
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `cd server && go test ./internal/game/ -run 'TestSaveFactionDef|TestDeleteFaction|TestDeleteUnitOverride_DoesNot|TestListFactions' -count=1`
Expected: PASS.

- [ ] **Step 8: Add the HTTP routes**

In `server/internal/http/router.go`, after the `/catalog/damage-types` block from Task 1:

```go
	mux.HandleFunc("/catalog/factions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"factions": game.ListFactions(),
		})
	})
```

In `server/internal/http/editor_handlers.go`, inside `registerEditorRoutes`, after the `/units/` block (ends ~line 135):

```go
	mux.HandleFunc("/factions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		var req game.EditorFactionSaveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		if err := game.SaveEditorFaction(req); err != nil {
			if game.IsEditorValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": req.Faction.ID, "status": "saved"})
	})

	mux.HandleFunc("/factions/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/factions/")
		if r.Method != http.MethodDelete {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "DELETE only")
			return
		}
		if id == "" || strings.Contains(id, "/") {
			writeJSONError(w, http.StatusBadRequest, "invalid_id", "expected /factions/{id}")
			return
		}
		existed, err := game.DeleteEditorFaction(id)
		if err != nil {
			if game.IsEditorValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error())
			return
		}
		if !existed {
			writeJSONError(w, http.StatusNotFound, "not_found", "no faction record for "+id)
			return
		}
		writeJSON(w, map[string]string{"id": id, "status": "deleted"})
	})
```

- [ ] **Step 9: Wire the startup loader**

In `server/cmd/api/main.go`, immediately after `game.LoadPersistedUnitsIntoOverlay()` (line ~57):

```go
	game.LoadPersistedFactionsIntoOverlay()
```

- [ ] **Step 10: Gates**

Run: `cd server && go build ./... && go vet ./internal/game/ ./internal/http/ ./cmd/api/`
Expected: clean.
Run: `cd server && go test ./internal/game/ -count=1 2>&1 | grep -E "^(--- FAIL|FAIL|ok)"`
Expected: `ok`.

- [ ] **Step 11: Checkpoint (do not commit)**

Files touched: `faction_persistence.go`, `faction_editor.go`, `faction_persistence_test.go`, `faction_defs.go`, `unit_persistence.go`, `router.go`, `editor_handlers.go`, `main.go`.

---

### Task 4: Server — stat floors on `validateUnitDef`

Today a blank-created unit is all zeros: 0 HP, 0 move speed. It spawns, does nothing, and cannot die. The floors make a new unit functional at minimum.

**Files:**
- Modify: `server/internal/game/unit_defs.go` (`validateUnitDef`, after the `RequiresBuildings` loop, ~line 354)
- Test: `server/internal/game/unit_defs_stat_floors_test.go` (create)

**Interfaces:**
- Consumes/extends: `validateUnitDef` (existing). Called by the embed loader (panics), `parsePersistedUnitFile`, `SaveUnitDef`, and `SaveEditorUnit` — so one change covers every write path.

**The rules, and why they are exactly these:** verified against the catalog — no unit has a zero `hp`, `moveSpeed`, or `attackSpeed`, so those floors are safe.

**Correction (found during Task 4 review — the original rationale here was wrong).** I claimed `worker` "omits its attack fields." It does not: `worker.json` has `nonCombat: true` **and** `damage: 3, attackRange: 60, attackSpeed: 1`. In this codebase `nonCombat` means *"not counted as an army unit"*, NOT *"cannot attack"*. In fact **all 14 shipped units have `damage > 0`**, so an unconditional attack floor would pass the entire catalog identically.

The attack fields are still **conditional on `damage > 0`** — but for a forward-looking reason, not a catalog one: a unit authored in the editor with no attack (`damage` is `omitempty`, so absent == 0) must remain legal. The branch is exercised only by editor-authored defs today.

- [ ] **Step 1: Write the failing test**

Create `server/internal/game/unit_defs_stat_floors_test.go`. The last test is the important one — it is the guard that proves the rules are not stricter than the content they must accept.

```go
package game

import "testing"

func floorValidUnit() UnitDef {
	return UnitDef{
		Type: "floor_test", Faction: "human", Name: "Floor Test",
		HP: 100, MoveSpeed: 60, Damage: 10, AttackRange: 32, AttackSpeed: 1,
	}
}

func TestValidateUnitDef_StatFloors(t *testing.T) {
	cases := map[string]func(*UnitDef){
		"zero hp":                       func(d *UnitDef) { d.HP = 0 },
		"negative hp":                   func(d *UnitDef) { d.HP = -1 },
		"zero move speed":               func(d *UnitDef) { d.MoveSpeed = 0 },
		"attacker with no range":        func(d *UnitDef) { d.AttackRange = 0 },
		"attacker with no attack speed": func(d *UnitDef) { d.AttackSpeed = 0 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			def := floorValidUnit()
			mutate(&def)
			if err := validateUnitDef(&def); err == nil {
				t.Fatalf("expected %s to be rejected", name)
			}
		})
	}
}

// A non-combat unit (worker) deals no damage and authors no attack fields. The
// attack floors must not apply to it.
func TestValidateUnitDef_NonCombatNeedsNoAttackFields(t *testing.T) {
	def := UnitDef{
		Type: "gatherer", Faction: "human", Name: "Gatherer",
		HP: 60, MoveSpeed: 55, NonCombat: true,
	}
	if err := validateUnitDef(&def); err != nil {
		t.Fatalf("a damage-less unit must pass the attack floors, got %v", err)
	}
}

// THE GUARD. Every def the game actually ships must satisfy the new rules. A
// failure here means a floor is stricter than real content — relax the floor,
// do not "fix" the catalog.
func TestValidateUnitDef_EveryEmbeddedUnitPasses(t *testing.T) {
	for _, def := range ListUnitDefs() {
		def := def
		t.Run(def.Type, func(t *testing.T) {
			if err := validateUnitDef(&def); err != nil {
				t.Fatalf("shipped unit %q fails validation: %v — the new floor is too strict for real content", def.Type, err)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd server && go test ./internal/game/ -run 'TestValidateUnitDef_StatFloors' -count=1`
Expected: FAIL — every subtest, because no floors exist yet and the mutated defs still validate.

- [ ] **Step 3: Add the floors**

In `server/internal/game/unit_defs.go`, in `validateUnitDef`, insert immediately before the closing `return nil` (after the `RequiresBuildings` loop):

```go
	// Stat floors. A unit that violates these spawns but cannot function — 0 HP
	// cannot die, 0 moveSpeed cannot walk. Damage is deliberately NOT floored:
	// non-combat units (worker) author no attack at all. The attack fields are
	// therefore conditional on the unit actually having an attack.
	if def.HP <= 0 {
		return fmt.Errorf("unit %q: hp must be > 0", def.Type)
	}
	if def.MoveSpeed <= 0 {
		return fmt.Errorf("unit %q: moveSpeed must be > 0", def.Type)
	}
	if def.Damage > 0 {
		if def.AttackRange <= 0 {
			return fmt.Errorf("unit %q: attackRange must be > 0 when damage > 0", def.Type)
		}
		if def.AttackSpeed <= 0 {
			return fmt.Errorf("unit %q: attackSpeed must be > 0 when damage > 0", def.Type)
		}
	}
	return nil
```

- [ ] **Step 4: Run the tests — including the guard**

Run: `cd server && go test ./internal/game/ -run TestValidateUnitDef -count=1`
Expected: PASS, including every `TestValidateUnitDef_EveryEmbeddedUnitPasses/<unit>` subtest.

**If the package panics at init** with a `unit "x": ... must be > 0` message, a real shipped unit violates a floor. That floor is wrong — the catalog is the authority. Relax the offending rule (e.g. make it conditional like the attack fields are), note which unit forced it, and re-run. Do not edit the catalog to satisfy the validator.

- [ ] **Step 5: Gates**

Run: `cd server && go build ./... && go vet ./internal/game/`
Expected: clean.
Run: `cd server && go test ./internal/game/ -count=1 2>&1 | grep -E "^(--- FAIL|FAIL|ok)"`
Expected: `ok`.

- [ ] **Step 6: Checkpoint (do not commit)**

Files touched: `server/internal/game/unit_defs.go`, `server/internal/game/unit_defs_stat_floors_test.go`.

---

### Task 5: Client — the editor catalog API

**Files:**
- Create: `client/src/game-portal/src/game/units/editorCatalogApi.ts`

**Interfaces:**
- Produces: `interface FactionDef { id, displayName, order? }`
- Produces: `fetchFactions()`, `saveFaction(faction)`, `deleteFaction(id)`, `fetchArchetypes()`, `fetchProjectileIds()`, `fetchAbilityIds()`, `fetchDamageTypes()`, `fetchBuildingIds()`.
- Consumes: the routes from Tasks 1 and 3, plus the existing `GET /catalog/buildings`.
- Consumed by: `UnitTypeEditorPanel.vue` (Task 7).

Mirrors `unitEditorApi.ts` idioms exactly: same `API_BASE`, same `EditorValidationError` (re-exported from there, not redefined), same 400-body shape (`{error, message}`).

- [ ] **Step 1: Create the module**

```ts
import { EditorValidationError } from './unitEditorApi'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

// FactionDef mirrors the Go FactionDef (server/internal/game/faction_defs.go).
// A faction directory without a faction.json still yields a record — the server
// synthesizes one — so every faction the editor sees has a display name.
export interface FactionDef {
  id: string
  displayName: string
  order?: number
}

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`)
  if (!res.ok) throw new Error(`Failed to load ${path}: ${res.status}`)
  return (await res.json()) as T
}

export async function fetchFactions(): Promise<FactionDef[]> {
  const data = await getJSON<{ factions: FactionDef[] }>('/catalog/factions')
  return data.factions ?? []
}

export async function saveFaction(faction: FactionDef): Promise<void> {
  const res = await fetch(`${API_BASE}/factions`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ faction }),
  })
  if (res.status === 400) {
    const body = (await res.json()) as { error?: string; message?: string }
    if (body.error === 'validation_failed') {
      throw new EditorValidationError(body.message ?? 'validation failed')
    }
  }
  if (!res.ok) throw new Error(`Failed to save faction: ${res.status}`)
}

// A faction that still owns units is refused by the server with a
// validation_failed naming those units — surface it, don't swallow it.
export async function deleteFaction(id: string): Promise<void> {
  const res = await fetch(`${API_BASE}/factions/${encodeURIComponent(id)}`, { method: 'DELETE' })
  if (res.status === 400) {
    const body = (await res.json()) as { error?: string; message?: string }
    if (body.error === 'validation_failed') {
      throw new EditorValidationError(body.message ?? 'validation failed')
    }
  }
  if (!res.ok) throw new Error(`Failed to delete faction: ${res.status}`)
}

// The archetype set is the combat-profile key set, and it is the valid set for
// BOTH the Archetype and Combat Profile fields.
export async function fetchArchetypes(): Promise<string[]> {
  const data = await getJSON<{ archetypes: string[] }>('/catalog/archetypes')
  return data.archetypes ?? []
}

export async function fetchProjectileIds(): Promise<string[]> {
  const data = await getJSON<{ projectiles: Array<{ id?: string; type?: string }> }>('/catalog/projectiles')
  return (data.projectiles ?? []).map((p) => p.id ?? p.type ?? '').filter(Boolean).sort()
}

export async function fetchAbilityIds(): Promise<string[]> {
  const data = await getJSON<{ abilities: Array<{ id?: string }> }>('/catalog/abilities')
  return (data.abilities ?? []).map((a) => a.id ?? '').filter(Boolean).sort()
}

export async function fetchDamageTypes(): Promise<string[]> {
  const data = await getJSON<{ damageTypes: string[] }>('/catalog/damage-types')
  return data.damageTypes ?? []
}

export async function fetchBuildingIds(): Promise<string[]> {
  const data = await getJSON<{ buildings: Array<{ type?: string }> }>('/catalog/buildings')
  return (data.buildings ?? []).map((b) => b.type ?? '').filter(Boolean).sort()
}
```

- [ ] **Step 2: Verify the projectile and ability id field names**

`fetchProjectileIds` and `fetchAbilityIds` above guess between `id` and `type`. Confirm the real JSON key by reading the Go structs:

Run: `cd server && grep -n "json:\"id\"\|json:\"type\"" internal/game/projectile_defs.go internal/game/ability_defs.go | head -20`

Fix the mapper to use the actual key and delete the fallback — a `?? ''` that never fires is noise, and one that silently fires hides a schema mismatch behind an empty dropdown.

- [ ] **Step 3: Typecheck**

Run: `cd client/src/game-portal && npx vue-tsc -b`
Expected: clean.

- [ ] **Step 4: Checkpoint (do not commit)**

Files touched: `client/src/game-portal/src/game/units/editorCatalogApi.ts`.

---

### Task 6: Client — blank units get working defaults

**Files:**
- Modify: `client/src/game-portal/src/game/units/unitEditorForm.ts` (`createBlankForm`, line 62)
- Test: `client/src/game-portal/src/game/units/unitEditorForm.test.ts` (extend the existing file)

**Interfaces:**
- Produces: `TEMPLATE_UNIT_TYPE`, `FALLBACK_STATS`, `pickTemplateStats(defs)`, and `createBlankForm(defaults?)` — the panel (Task 7) calls `createBlankForm(pickTemplateStats(units.value))`.
- The existing `createBlankForm()` call sites keep working: the parameter is optional and defaults to `FALLBACK_STATS`.

Defaults are **cloned from a real catalog unit**, not hardcoded, so they track balance changes instead of rotting. The constants exist only for the offline/empty-catalog case, and the test asserts they satisfy the server's floors rather than pinning their values.

- [ ] **Step 1: Write the failing test**

Append to `client/src/game-portal/src/game/units/unitEditorForm.test.ts`:

```ts
import {
  createBlankForm, pickTemplateStats, saveRequestFromForm,
  TEMPLATE_UNIT_TYPE, type AuthoredUnitDef,
} from './unitEditorForm'

describe('blank unit defaults', () => {
  const template: AuthoredUnitDef = {
    type: TEMPLATE_UNIT_TYPE, faction: 'human', name: 'Soldier',
    hp: 220, armor: 33, damage: 24, attackRange: 40, attackSpeed: 0.9,
    moveSpeed: 58, visionRange: 400, meatCost: 2,
  }

  it('clones the stat block from the template unit', () => {
    const form = createBlankForm(pickTemplateStats([template]))
    expect(form.hp).toBe(template.hp)
    expect(form.moveSpeed).toBe(template.moveSpeed)
    expect(form.attackRange).toBe(template.attackRange)
    expect(form.visionRange).toBe(template.visionRange)
  })

  it('does not clone identity or cost from the template', () => {
    const form = createBlankForm(pickTemplateStats([template]))
    expect(form.type).toBe('')
    expect(form.faction).toBe('')
    expect(form.name).toBeUndefined()
    expect(form.meatCost).toBeUndefined()
  })

  // The whole point: a blank unit must clear the server's stat floors, or the
  // author's first Save is a validation error they did nothing to cause.
  it('produces a def that satisfies the server stat floors, template or not', () => {
    for (const defaults of [pickTemplateStats([template]), pickTemplateStats([])]) {
      const def = saveRequestFromForm(createBlankForm(defaults))
      expect(def.hp!).toBeGreaterThan(0)
      expect(def.moveSpeed!).toBeGreaterThan(0)
      if ((def.damage ?? 0) > 0) {
        expect(def.attackRange!).toBeGreaterThan(0)
        expect(def.attackSpeed!).toBeGreaterThan(0)
      }
    }
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd client/src/game-portal && npm run test -- unitEditorForm`
Expected: FAIL — `pickTemplateStats` / `TEMPLATE_UNIT_TYPE` are not exported.

- [ ] **Step 3: Implement**

In `client/src/game-portal/src/game/units/unitEditorForm.ts`, replace `createBlankForm` (lines 62-64) with:

```ts
// The unit a brand-new unit copies its stat block from. Defaults are cloned
// from real catalog content rather than hardcoded, so they follow balance
// changes instead of rotting.
export const TEMPLATE_UNIT_TYPE = 'soldier'

// Only the stat block is inherited. Identity, cost, and gating are per-unit
// design decisions and are deliberately left blank.
const TEMPLATE_STAT_KEYS = [
  'hp', 'armor', 'damage', 'attackRange', 'attackSpeed', 'moveSpeed', 'visionRange',
] as const

// Used only when the template unit is unavailable (offline, empty catalog).
// These are "functional", not "balanced" — the template is the real source.
// They must clear the server's stat floors (hp > 0, moveSpeed > 0, and an
// attacker needs range + speed), or a blank unit cannot be saved at all.
export const FALLBACK_STATS: Partial<AuthoredUnitDef> = {
  hp: 100, armor: 0, damage: 10, attackRange: 32, attackSpeed: 1, moveSpeed: 60, visionRange: 300,
}

// pickTemplateStats pulls the stat block out of the template unit in a fetched
// catalog, falling back per-key so a template missing one field still yields a
// complete, saveable block.
export function pickTemplateStats(defs: AuthoredUnitDef[]): Partial<AuthoredUnitDef> {
  const template = defs.find((d) => d.type === TEMPLATE_UNIT_TYPE)
  if (!template) return { ...FALLBACK_STATS }
  const out: Record<string, unknown> = {}
  for (const key of TEMPLATE_STAT_KEYS) {
    out[key] = template[key] ?? FALLBACK_STATS[key]
  }
  return out as Partial<AuthoredUnitDef>
}

export function createBlankForm(defaults: Partial<AuthoredUnitDef> = FALLBACK_STATS): UnitEditorForm {
  return { ...defaults, type: '', faction: '', remainder: {} }
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd client/src/game-portal && npm run test -- unitEditorForm`
Expected: PASS — the new cases AND the two pre-existing round-trip cases. The existing `createBlankForm produces a settable shell with no art` case must still pass: `attackVisual` is still absent, since only stat keys are seeded.

- [ ] **Step 5: Typecheck**

Run: `cd client/src/game-portal && npx vue-tsc -b`
Expected: clean.

- [ ] **Step 6: Checkpoint (do not commit)**

Files touched: `client/src/game-portal/src/game/units/unitEditorForm.ts`, `.../unitEditorForm.test.ts`.

---

### Task 7: Client — faction filter, faction creation, and real dropdowns

**Files:**
- Modify: `client/src/game-portal/src/components/UnitTypeEditorPanel.vue`

**Interfaces:**
- Consumes: `fetchFactions`, `saveFaction`, `deleteFaction`, `fetchArchetypes`, `fetchProjectileIds`, `fetchAbilityIds`, `fetchDamageTypes`, `fetchBuildingIds`, `FactionDef` (Task 5); `createBlankForm`, `pickTemplateStats` (Task 6).
- Produces: no new exports. Self-contained panel, unchanged mount contract (still no required props, still mountable bare in the world-editor popup).

Deletes the hardcoded `const FACTIONS = [...]` at line 227 — the bug that made a new server faction invisible to the editor.

- [ ] **Step 1a: Fix the imports**

The panel already imports from `'vue'` (line 211) and from `unitEditorForm` (lines 212-215). **Extend those existing import statements — do not add a second `import ... from 'vue'`.**

Line 211 becomes:
```ts
import { computed, onMounted, reactive, ref, watch } from 'vue'
```

The `unitEditorForm` import (lines 212-215) gains `pickTemplateStats`:
```ts
import {
  createBlankForm, formFromDef, saveRequestFromForm, pickTemplateStats,
  type AuthoredUnitDef, type UnitEditorForm,
} from '@/game/units/unitEditorForm'
```

Then add one new import below the existing `unitEditorApi` import (line 218). `EditorValidationError` is already imported there — do not re-import it.
```ts
import {
  fetchFactions, saveFaction, deleteFaction, fetchArchetypes,
  fetchProjectileIds, fetchAbilityIds, fetchDamageTypes, fetchBuildingIds,
  type FactionDef,
} from '@/game/units/editorCatalogApi'
```

- [ ] **Step 1b: Replace the hardcoded faction list with server-sourced state**

Delete line 227 (`const FACTIONS = ['human', 'raider', 'wildborne', 'witherborne'] as const`) — this is the bug that made a new server faction invisible to the editor. In its place:

```ts
// Server-sourced catalogs backing the selects. Free-text fallbacks stay
// available (see the datalist inputs below) because the server, not the UI, is
// the validator — a stale dropdown must never block a legal value.
const factions = ref<FactionDef[]>([])
const archetypes = ref<string[]>([])
const projectileIds = ref<string[]>([])
const abilityIds = ref<string[]>([])
const damageTypes = ref<string[]>([])
const buildingIds = ref<string[]>([])

const factionFilter = ref<string>('')   // '' = All
const newFactionId = ref('')
const newFactionName = ref('')
const showNewFaction = ref(false)
const factionError = ref('')

const visibleUnits = computed(() =>
  factionFilter.value
    ? units.value.filter((u) => u.faction === factionFilter.value)
    : units.value,
)

// An archetype outside the combat-profile set is not rejected by the server —
// it silently falls back to the soldier profile. Warn rather than block.
const archetypeWarning = computed(() => {
  const value = form.value.archetype
  if (!value || archetypes.value.length === 0) return ''
  if (archetypes.value.includes(value)) return ''
  return `"${value}" is not a known combat profile — this unit will fall back to the soldier profile.`
})

async function reloadCatalogs() {
  const [f, a, p, ab, dt, b] = await Promise.all([
    fetchFactions(), fetchArchetypes(), fetchProjectileIds(),
    fetchAbilityIds(), fetchDamageTypes(), fetchBuildingIds(),
  ])
  factions.value = f
  archetypes.value = a
  projectileIds.value = p
  abilityIds.value = ab
  damageTypes.value = dt
  buildingIds.value = b
}

async function createFaction() {
  factionError.value = ''
  busy.value = true
  try {
    await saveFaction({ id: newFactionId.value.trim(), displayName: newFactionName.value.trim() })
    await reloadCatalogs()
    factionFilter.value = newFactionId.value.trim()
    form.value.faction = factionFilter.value
    newFactionId.value = ''
    newFactionName.value = ''
    showNewFaction.value = false
  } catch (e) {
    factionError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

// The server refuses to delete a faction that still owns units, and its message
// names them. Surface it verbatim — it tells the author exactly what to fix.
async function removeFaction(id: string) {
  factionError.value = ''
  busy.value = true
  try {
    await deleteFaction(id)
    if (factionFilter.value === id) factionFilter.value = ''
    await reloadCatalogs()
  } catch (e) {
    factionError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}
```

- [ ] **Step 2: Seed new units from the template, and load the catalogs on mount**

Replace `newUnit()` (lines 297-305) so a blank unit starts functional, and pre-select the active faction filter so creating a unit inside a filtered faction does the obvious thing:

```ts
function newUnit() {
  form.value = createBlankForm(pickTemplateStats(units.value))
  form.value.faction = factionFilter.value || ''
  selectedType.value = null
  saveError.value = ''
  resourceCostRows.value = []
  pathChancesRows.value = []
  channelLoopStart.value = undefined
  channelLoopEnd.value = undefined
}
```

Replace `onMounted(reload)` (line 334) with:

```ts
onMounted(async () => {
  await reload()
  try {
    await reloadCatalogs()
  } catch (e) {
    // A catalog fetch failure degrades the selects to free text — it must not
    // take the whole panel down, because unit editing still works without them.
    loadError.value = e instanceof Error ? e.message : String(e)
  }
})
```

- [ ] **Step 3: Add the filter bar + new-faction form to the list column**

In `<template>`, replace the `<aside class="unit-editor__list">` block (lines 3-17) with:

```html
    <aside class="unit-editor__list">
      <div class="unit-editor__filter">
        <label class="unit-editor__filter-label">Faction</label>
        <select v-model="factionFilter">
          <option value="">All ({{ units.length }})</option>
          <option v-for="f in factions" :key="f.id" :value="f.id">{{ f.displayName }}</option>
        </select>
        <div class="unit-editor__filter-actions">
          <button type="button" :disabled="busy" @click="showNewFaction = !showNewFaction">+ Faction</button>
          <button
            type="button"
            :disabled="busy || !factionFilter"
            @click="removeFaction(factionFilter)"
          >
            Delete Faction
          </button>
        </div>
        <div v-if="showNewFaction" class="unit-editor__new-faction">
          <input v-model="newFactionId" placeholder="id (a-z0-9_)" />
          <input v-model="newFactionName" placeholder="Display Name" />
          <button type="button" :disabled="busy || !newFactionId.trim()" @click="createFaction">Create</button>
        </div>
        <p v-if="factionError" class="unit-editor__error">{{ factionError }}</p>
      </div>

      <button type="button" class="unit-editor__new" :disabled="busy" @click="newUnit">+ New Unit</button>
      <p v-if="loadError" class="unit-editor__error">{{ loadError }}</p>
      <ul>
        <li v-for="u in visibleUnits" :key="u.type">
          <button
            type="button"
            :class="{ 'is-selected': u.type === selectedType }"
            @click="selectUnit(u)"
          >
            {{ u.type }}
          </button>
        </li>
      </ul>
    </aside>
```

- [ ] **Step 4: Turn the free-text fields into pickers**

In the Identity section, replace the Faction `<select>` (lines 26-31) and the Archetype `<input>` (line 32):

```html
          <label>
            Faction
            <select v-model="form.faction">
              <option value="" disabled>— select a faction —</option>
              <option v-for="f in factions" :key="f.id" :value="f.id">{{ f.displayName }}</option>
            </select>
          </label>
          <label>
            Archetype
            <input v-model="form.archetype" list="unit-editor-archetypes" placeholder="(defaults to the unit type)" />
            <datalist id="unit-editor-archetypes">
              <option v-for="a in archetypes" :key="a" :value="a" />
            </datalist>
          </label>
          <p v-if="archetypeWarning" class="unit-editor__warning">{{ archetypeWarning }}</p>
```

`<input list=...>` + `<datalist>` is used rather than `<select>` on purpose: it offers the real set while still accepting a value outside it. The server does not reject an unknown archetype, so the editor must not either — it warns instead.

In the Combat section, replace the Combat Profile (line 73), Damage Type (line 75), and Projectile (line 83) inputs:

```html
          <label>
            Combat Profile
            <input v-model="form.combatProfile" list="unit-editor-archetypes" placeholder="(inferred from archetype)" />
          </label>
          <label>Attack Type <input v-model="form.attackType" /></label>
          <label>
            Damage Type
            <input v-model="form.damageType" list="unit-editor-damage-types" placeholder="(unspecified = physical)" />
            <datalist id="unit-editor-damage-types">
              <option v-for="d in damageTypes" :key="d" :value="d" />
            </datalist>
          </label>
```

and the Projectile input:

```html
          <label>
            Projectile
            <input v-model="form.projectile" list="unit-editor-projectiles" />
            <datalist id="unit-editor-projectiles">
              <option v-for="p in projectileIds" :key="p" :value="p" />
            </datalist>
          </label>
```

In the Abilities section, add a datalist to the Abilities input (line 142-145) — it is comma-separated, so the datalist assists rather than constrains; leave the binding untouched:

```html
          <label>
            Abilities (comma-separated)
            <input
              :value="(form.abilities ?? []).join(',')"
              @input="updateStringList('abilities', ($event.target as HTMLInputElement).value)"
            />
            <span class="unit-editor__hint">Known: {{ abilityIds.join(', ') || '—' }}</span>
          </label>
```

In the Gating section, do the same for Requires Buildings (line 163-166):

```html
          <label>
            Requires Buildings (comma-separated)
            <input
              :value="(form.requiresBuildings ?? []).join(',')"
              @input="updateStringList('requiresBuildings', ($event.target as HTMLInputElement).value)"
            />
            <span class="unit-editor__hint">Known: {{ buildingIds.join(', ') || '—' }}</span>
          </label>
```

- [ ] **Step 5: Styles**

Append to the `<style scoped>` block. No literal `cursor:` declarations — the global rules cover buttons, enabled and disabled.

```css
.unit-editor__filter {
  display: grid;
  gap: 6px;
  padding-bottom: 8px;
  border-bottom: 1px solid rgba(148, 163, 184, 0.18);
}

.unit-editor__filter-label {
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.unit-editor__filter select,
.unit-editor__new-faction input {
  width: 100%;
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
}

.unit-editor__filter-actions {
  display: flex;
  gap: 6px;
}

.unit-editor__filter-actions button {
  flex: 1;
  font-size: 0.72rem;
}

.unit-editor__new-faction {
  display: grid;
  gap: 6px;
  padding: 8px;
  border: 1px solid rgba(215, 187, 132, 0.35);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.6);
}

.unit-editor__warning {
  color: #fcd34d;
  font-size: 0.72rem;
  margin: 0;
}
</style>
```

- [ ] **Step 6: Build + typecheck**

Run: `cd client/src/game-portal && npx vue-tsc -b && npm run build`
Expected: clean.

- [ ] **Step 7: Manual verification**

Start the server and the client, open `/unit-type-editor`, and confirm:
1. The faction dropdown shows **Human** (from `faction.json`) and **Raider / Wildborne / Witherborne** (synthesized) — not a hardcoded list.
2. Selecting a faction filters the unit list; "All" restores it.
3. `+ Faction` → create `night_elf` / "Night Elf" → it appears in the filter immediately and a `catalog/units/night_elf/faction.json` appears on disk.
4. `Delete Faction` on `night_elf` (which has no units) succeeds. On `human`, it fails with an inline message naming the human units.
5. `+ New Unit` → the stat fields are **pre-filled from soldier**, not zeros. Set a type and faction, Save — it succeeds with no validation error.
6. Archetype: typing `archer` shows no warning; typing `wizard` shows the soldier-fallback warning but still saves.
7. Clear HP to 0 and Save → the server rejects it inline: `hp must be > 0`.

- [ ] **Step 8: Checkpoint (do not commit)**

Files touched: `client/src/game-portal/src/components/UnitTypeEditorPanel.vue`.

---

### Task 8: Verification sweep

**Files:** fixes only.

- [ ] **Step 1: Full gates**

Run: `cd server && go vet ./... && go build ./... && go test ./... -count=1 2>&1 | grep -E "^(--- FAIL|FAIL)"`
Expected: only the known pre-existing `cmd/api` `TestServerReadyLineAndStdinShutdown` failure. Anything else is new and must be fixed.

Run: `cd client/src/game-portal && npm run test && npx vue-tsc -b && npm run build`
Expected: green + clean.

- [ ] **Step 2: Untouched-editor check**

Run:
```bash
cd "$(git rev-parse --show-toplevel)" && git diff --stat -- \
  client/src/game-portal/src/components/MapEditorPanel.vue \
  client/src/game-portal/src/views/Editor.vue \
  client/src/game-portal/src/components/ItemEditorPanel.vue \
  server/internal/game/item_editor.go \
  server/internal/game/item_persistence.go
```
Expected: empty. The item editor and old map editor are untouched.

- [ ] **Step 3: Catalog hygiene**

Manual testing writes real files under `server/internal/game/catalog/units/`. Run `git status` and confirm the ONLY intended new catalog file is `catalog/units/human/faction.json`. Revert any test factions or units created during Step 7 of Task 7:

```bash
git status --short server/internal/game/catalog/units/
```

Delete any stray `night_elf/`, `doomed/`, or test-unit directories before handing off.

- [ ] **Step 4: Report**

Summarize: gates green, the one known pre-existing failure, files added/modified, and anything a floor had to be relaxed for (Task 4 Step 4).
