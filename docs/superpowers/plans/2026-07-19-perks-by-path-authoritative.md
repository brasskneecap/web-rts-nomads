# Perks-by-Path Authoritative Catalog — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a promotion path's authored `perksByRank` list the *single source of truth* for which perks a unit can roll, organize the perk catalog on disk by path (+ a `generic` bucket), derive a perk's association from its folder, and filter the editor's perk picker to associated + generic perks.

**Architecture:** Today a perk self-declares `unitType`/`path`/`rank` and `eligiblePerksForUnitAtRank` scans the whole catalog to build a rank-up pool, then *unions* the path's optional `perksByRank` refs on top. This plan inverts that: the path's `perksByRank` becomes authoritative (the catalog scan is removed), a perk's owning path is derived from its directory (`catalog/perks/<path>/<id>/<id>.json`, or `catalog/perks/generic/<id>/…`), and the now-dead `unitType`/`rank` eligibility fields are retired. Rank is decided purely by which bucket a path drops a perk into — the same generic perk can be Bronze on one path and Silver on another.

**Tech Stack:** Go 1.x (server, `internal/game`), embedded catalog via `//go:embed`, Vue 3 + TypeScript (client editor). Tests are Go table tests + Vitest.

**Execution ordering (dependency-safe — every commit stays green):**
1. **Phase A** — Populate `perksByRank` on all 7 paths *while auto-match still runs* (union ⇒ zero behavior change).
2. **Phase B** — Flip `eligiblePerksForUnitAtRank` to authoritative (refs-only). Behavior identical because refs mirror the old auto-match.
3. **Phase C** — Physical folder reorg + loader/persistence/`maps.go` derive-from-folder + strip dead fields.
4. **Phase D** — Frontend picker filter + standalone perk-editor cleanup.

---

## File Structure

**Backend (`server/internal/game/`):**
- `perk_defs.go` — `PerkDef` struct (remove `UnitType`/`Rank`; `Path` becomes folder-derived association), `validatePerkDef`, `loadPerkDefs` (two-level folder walk), `eligiblePerksForUnitAtRank` (refs-only).
- `perk_persistence.go` — `SavePerkDef` / `DeletePerkOverride` / `LoadPersistedPerksIntoOverlay` become association(folder)-aware.
- `path_defs.go` — add `unitTypeForPath(path)` reverse lookup helper (consumed by `maps.go`).
- `maps.go` — `filterKnownPerkIDsForUnit` validates via path→unit instead of `def.UnitType`.
- `catalog/perks/<path>/<id>/<id>.json` — relocated 72 perk files, with `unitType`/`path`/`rank` keys stripped.
- `catalog/perks/generic/.gitkeep` — empty association bucket for future path-agnostic perks.
- `catalog/units/human/<unit>/paths/<path>/<path>.json` — 7 path files gain a `perksByRank` block.

**Frontend (`client/src/game-portal/src/`):**
- `components/UnitTypeEditorPanel.vue` — `availablePerkOptionsForRank` filters by association + generic.
- `game/perks/perkEditorForm.ts` — drop `unitType`/`rank` from the modeled/persisted keys; keep `path` as read-only association.
- `components/PerkEditorPanel.vue` — remove the `unitType`/`rank` authoring controls; show folder-derived association read-only.

---

## Reference data — the exact "mirror today's pools" mapping

These are the perks each path auto-matches today (verified from the catalog). Phase A writes exactly these into each path's `perksByRank`. **`arch_mage` deliberately has only Gold** — it authors no Bronze/Silver perks today and must keep granting none.

```
acolyte / cleric
  bronze: battle_prayer, bolstering_prayer, mana_conduit, sanctuary
  silver: divine_aegis, divine_healer, restoration_aura, zealous_march
  gold:   beacon_of_life, divine_intervention, divine_judgement

acolyte / siphoner
  bronze: lingering_hex, mark_of_weakness, soul_leech, withering_beam
  silver: amplify_damage, chain_siphon, dark_renewal, shared_suffering
  gold:   ascended_corruption, beam_mastery, repurposed_life

adept / arch_mage
  gold:   arcane_conduit, arcane_feedback, unstable_magic

archer / marksman
  bronze: eagle_spirit, hawk_spirit, vulture_spirit
  silver: hunters_mark, pierce, split_shot
  gold:   bullseye, double_shot, explosive_tips

archer / trapper
  bronze: caltrops, explosive_trap, fire_pit, marker_trap
  silver: amplified_effects, barbed_field, explosive_chain, exposed_weakness,
          extended_setup, lasting_flames, rapid_deployment, wider_nets
  gold:   ascendant_infusion, increased_deployment, overload_protocol

soldier / berserker
  bronze: bloodlust, cleaving_rage, frenzy_core, relentless, savage_strikes
  silver: blood_sustain, executioner, momentum
  gold:   berserk_state, blood_engine, whirlwind_core

soldier / vanguard
  bronze: hold_the_line, interlock, reinforced_armor, retaliation, shield_bash
  silver: brace, challengers_mark, last_stand, punishing_guard
  gold:   guardian_aura, pain_share, rallying_banner
```

Path file locations:
```
server/internal/game/catalog/units/human/acolyte/paths/cleric/cleric.json
server/internal/game/catalog/units/human/acolyte/paths/siphoner/siphoner.json
server/internal/game/catalog/units/human/adept/paths/arch_mage/arch_mage.json
server/internal/game/catalog/units/human/archer/paths/marksman/marksman.json
server/internal/game/catalog/units/human/archer/paths/trapper/trapper.json
server/internal/game/catalog/units/human/soldier/paths/berserker/berserker.json
server/internal/game/catalog/units/human/soldier/paths/vanguard/vanguard.json
```

Run all Go commands from `server/`. Build/test gate: `go build ./... && go vet ./... && go test ./internal/game/`.

---

## PHASE A — Populate `perksByRank` on every path (behavior-neutral union)

### Task A1: Add a determinism regression test that pins each path's rank-up pool

**Files:**
- Test: `server/internal/game/perksbyrank_migration_test.go` (create)

- [ ] **Step 1: Write the test that asserts each path's pool equals the mirror mapping**

```go
package game

import (
	"sort"
	"testing"
)

// wantPathPools is the authoritative "mirror today's pools" mapping. Phase A
// makes perksByRank produce exactly these sets; Phase B makes them the SOLE
// source. Keeping this table in the test (not derived from the JSON) is
// intentional: it is the human-authored contract the catalog must satisfy, so
// a stray edit to a path file fails here instead of silently changing rolls.
var wantPathPools = map[string]map[string][]string{
	"cleric": {
		"bronze": {"battle_prayer", "bolstering_prayer", "mana_conduit", "sanctuary"},
		"silver": {"divine_aegis", "divine_healer", "restoration_aura", "zealous_march"},
		"gold":   {"beacon_of_life", "divine_intervention", "divine_judgement"},
	},
	"siphoner": {
		"bronze": {"lingering_hex", "mark_of_weakness", "soul_leech", "withering_beam"},
		"silver": {"amplify_damage", "chain_siphon", "dark_renewal", "shared_suffering"},
		"gold":   {"ascended_corruption", "beam_mastery", "repurposed_life"},
	},
	"arch_mage": {
		"gold": {"arcane_conduit", "arcane_feedback", "unstable_magic"},
	},
	"marksman": {
		"bronze": {"eagle_spirit", "hawk_spirit", "vulture_spirit"},
		"silver": {"hunters_mark", "pierce", "split_shot"},
		"gold":   {"bullseye", "double_shot", "explosive_tips"},
	},
	"trapper": {
		"bronze": {"caltrops", "explosive_trap", "fire_pit", "marker_trap"},
		"silver": {"amplified_effects", "barbed_field", "explosive_chain", "exposed_weakness", "extended_setup", "lasting_flames", "rapid_deployment", "wider_nets"},
		"gold":   {"ascendant_infusion", "increased_deployment", "overload_protocol"},
	},
	"berserker": {
		"bronze": {"bloodlust", "cleaving_rage", "frenzy_core", "relentless", "savage_strikes"},
		"silver": {"blood_sustain", "executioner", "momentum"},
		"gold":   {"berserk_state", "blood_engine", "whirlwind_core"},
	},
	"vanguard": {
		"bronze": {"hold_the_line", "interlock", "reinforced_armor", "retaliation", "shield_bash"},
		"silver": {"brace", "challengers_mark", "last_stand", "punishing_guard"},
		"gold":   {"guardian_aura", "pain_share", "rallying_banner"},
	},
}

// pathUnitType maps each path to the unit type a probe Unit needs so
// eligiblePerksForUnitAtRank resolves the right pool.
var pathUnitType = map[string]string{
	"cleric": "acolyte", "siphoner": "acolyte", "arch_mage": "adept",
	"marksman": "archer", "trapper": "archer", "berserker": "soldier", "vanguard": "soldier",
}

func poolIDsAtRank(path, rank string) []string {
	u := &Unit{UnitType: pathUnitType[path], ProgressionPath: path}
	defs := eligiblePerksForUnitAtRank(u, rank)
	ids := make([]string, 0, len(defs))
	for _, d := range defs {
		ids = append(ids, d.ID)
	}
	sort.Strings(ids)
	return ids
}

func TestPathPoolsMatchMirror(t *testing.T) {
	for path, byRank := range wantPathPools {
		for rank, want := range byRank {
			sort.Strings(want)
			got := poolIDsAtRank(path, rank)
			if len(got) != len(want) {
				t.Errorf("%s/%s: got %v, want %v", path, rank, got, want)
				continue
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("%s/%s: got %v, want %v", path, rank, got, want)
					break
				}
			}
		}
	}
}
```

- [ ] **Step 2: Run it to confirm it PASSES on the current (auto-match) catalog**

Run: `go test ./internal/game/ -run TestPathPoolsMatchMirror -v`
Expected: PASS. (Auto-match already yields these sets; this test now guards them through the migration. If it FAILS, the mirror table is wrong — fix the table to match reality before proceeding, do not change the catalog.)

- [ ] **Step 3: Commit**

```bash
git add server/internal/game/perksbyrank_migration_test.go
git commit -m "test(perks): pin each path's rank-up pool before perksByRank migration"
```

### Task A2: Write `perksByRank` into all 7 path files

**Files:**
- Modify: the 7 path JSONs listed in Reference data.

- [ ] **Step 1: Add the `perksByRank` block to `cleric.json`**

Insert a top-level `"perksByRank"` key (place it right after `"abilities"`). Example for cleric:

```json
"perksByRank": {
  "bronze": ["battle_prayer", "bolstering_prayer", "mana_conduit", "sanctuary"],
  "silver": ["divine_aegis", "divine_healer", "restoration_aura", "zealous_march"],
  "gold": ["beacon_of_life", "divine_intervention", "divine_judgement"]
}
```

- [ ] **Step 2: Repeat for the other 6 paths** using the exact lists in Reference data. For `arch_mage.json`, write only the gold key:

```json
"perksByRank": {
  "gold": ["arcane_conduit", "arcane_feedback", "unstable_magic"]
}
```

- [ ] **Step 3: Run the guard test — it must still pass (union is a no-op because refs duplicate auto-match)**

Run: `go test ./internal/game/ -run TestPathPoolsMatchMirror -v`
Expected: PASS (unchanged sets — refs are deduped against auto-matches in `eligiblePerksForUnitAtRank`).

- [ ] **Step 4: Full determinism + suite check**

Run: `go test ./internal/game/`
Expected: PASS. In particular `advancement_qa_test.go` (seeded rank-up determinism) must stay green — the dedup + ID-sort in `eligiblePerksForUnitAtRank` keeps ordering stable.

- [ ] **Step 5: Commit**

```bash
git add server/internal/game/catalog/units/human/*/paths/*/*.json
git commit -m "feat(perks): author perksByRank on all 7 paths (mirrors current pools)"
```

---

## PHASE B — Make `perksByRank` authoritative (remove the catalog scan)

### Task B1: Rewrite `eligiblePerksForUnitAtRank` to resolve refs only

**Files:**
- Modify: `server/internal/game/perk_defs.go:324-360` (`eligiblePerksForUnitAtRank`)

- [ ] **Step 1: Replace the function body with a refs-only resolver**

```go
// eligiblePerksForUnitAtRank returns the perks a unit may roll at the given
// rank. AUTHORITATIVE MODEL: the ONLY source is the unit's promotion path's
// explicit perksByRank list (pathPerkRefsForRank). A perk's own folder-derived
// association (PerkDef.Path) is used only for editor filtering and display — it
// no longer participates in rank-up selection. Unknown ids resolve fail-safe
// (skipped). The ID-sort keeps rngPerks.Intn deterministic regardless of the
// authored list order, preserving replay reproducibility (AI_RULES.md).
func eligiblePerksForUnitAtRank(unit *Unit, rank string) []*PerkDef {
	if unit == nil {
		return nil
	}
	refs := pathPerkRefsForRank(unit.ProgressionPath, rank)
	if len(refs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(refs))
	eligible := make([]*PerkDef, 0, len(refs))
	for _, perkID := range refs {
		if _, dup := seen[perkID]; dup {
			continue
		}
		if def, ok := perkDefLookup(perkID); ok {
			eligible = append(eligible, def)
			seen[perkID] = struct{}{}
		}
	}
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].ID < eligible[j].ID })
	return eligible
}
```

- [ ] **Step 2: Run the guard + determinism tests**

Run: `go test ./internal/game/ -run 'TestPathPoolsMatchMirror|TestAdvancement' -v`
Expected: PASS — the resolved sets equal the old auto-matched sets, so pools and seeded rolls are unchanged.

- [ ] **Step 3: Fix tests that asserted on the retired eligibility fields**

These tests reference `def.UnitType`/`def.Rank`/`def.Path` as the *matching* mechanism and must be updated to assert against the path's authored refs instead. For each, replace the field-based assertion with a pool-membership assertion via `eligiblePerksForUnitAtRank` (or `perkPoolForRankLocked`):

- `server/internal/game/perks_arch_mage_test.go:32-70` — instead of `def.UnitType=="adept" && def.Path=="arch_mage" && def.Rank=="gold"`, assert the perk id is present in `eligiblePerksForUnitAtRank(&Unit{UnitType:"adept",ProgressionPath:"arch_mage"}, "gold")`.
- `server/internal/game/perk_empty_pool_test.go:84` — same substitution (drop the `def.UnitType==… && def.Path==… && def.Rank==…` guard; assert pool membership).
- `server/internal/game/trap_test.go:1519-1526,1704-1711` — these assert returned perks have `def.Rank == bronze/gold`. Rank is no longer on the def; assert instead that every returned id is a member of the corresponding `wantPathPools["trapper"][rank]` set.
- `server/internal/game/greater_heal_swap_test.go:125` — `def.Path=="cleric" && def.Rank==bronze` becomes membership in `wantPathPools["cleric"]["bronze"]`.
- `server/internal/game/perk_persistence_test.go:132-214` — the SP2 union tests; update the ones that assert auto-match-by-field to assert refs-resolution. Keep the ref-union test (it now describes the sole path, not an add-on).

Run each after editing: `go test ./internal/game/ -run <TestName> -v` → PASS.

- [ ] **Step 4: Full suite**

Run: `go build ./... && go vet ./... && go test ./internal/game/`
Expected: PASS + clean.

- [ ] **Step 5: Commit**

```bash
git add server/internal/game/perk_defs.go server/internal/game/*_test.go
git commit -m "feat(perks): perksByRank is now the sole rank-up source (remove catalog auto-match)"
```

---

## PHASE C — Folder reorg, derive-from-folder loader, field strip

### Task C1: Add the `unitTypeForPath` reverse lookup

**Files:**
- Modify: `server/internal/game/path_defs.go` (near `pathsForUnitType`, ~line 362)
- Test: `server/internal/game/path_defs_perkassoc_test.go` (create)

- [ ] **Step 1: Write the failing test**

```go
package game

import "testing"

func TestUnitTypeForPath(t *testing.T) {
	cases := map[string]string{
		"siphoner": "acolyte", "cleric": "acolyte", "arch_mage": "adept",
		"marksman": "archer", "trapper": "archer",
		"berserker": "soldier", "vanguard": "soldier",
		"does_not_exist": "",
	}
	for path, want := range cases {
		if got := unitTypeForPath(path); got != want {
			t.Errorf("unitTypeForPath(%q) = %q, want %q", path, got, want)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/game/ -run TestUnitTypeForPath -v`
Expected: FAIL (`unitTypeForPath` undefined).

- [ ] **Step 3: Implement the helper**

```go
// unitTypeForPath returns the unit type that owns a promotion path, or "" if
// the path is unknown. Reverse of pathsByUnitType; used by placed-unit perk
// validation (maps.go) now that a perk's owning unit is derived from its path
// association rather than a stored PerkDef.UnitType field. Linear scan over a
// tiny catalog (a handful of unit types) — no index needed.
func unitTypeForPath(path string) string {
	if path == "" {
		return ""
	}
	pathCatalogMu.RLock()
	defer pathCatalogMu.RUnlock()
	for unitType, paths := range pathsByUnitType {
		for _, p := range paths {
			if p == path {
				return unitType
			}
		}
	}
	return ""
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/game/ -run TestUnitTypeForPath -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/game/path_defs.go server/internal/game/path_defs_perkassoc_test.go
git commit -m "feat(perks): add unitTypeForPath reverse lookup for association validation"
```

### Task C2: Physically relocate the 72 perk files and strip dead fields

**Files:**
- Move+edit: every `server/internal/game/catalog/perks/<id>/<id>.json` → `catalog/perks/<path>/<id>/<id>.json`
- Create: `server/internal/game/catalog/perks/generic/.gitkeep`

- [ ] **Step 1: Run the relocation script** (from repo root; uses `git mv` so history follows the file)

```bash
cd "server/internal/game/catalog/perks"
node -e '
const fs=require("fs"), cp=require("child_process");
for (const id of fs.readdirSync(".")) {
  const src = id+"/"+id+".json";
  if (!fs.existsSync(src)) continue;              // skip non-perk dirs
  const j = JSON.parse(fs.readFileSync(src,"utf8"));
  const assoc = (j.path && j.path.trim()) ? j.path.trim() : "generic";
  // strip retired eligibility keys; folder is now the source of truth
  delete j.unitType; delete j.path; delete j.rank;
  const destDir = assoc+"/"+id;
  fs.mkdirSync(destDir,{recursive:true});
  // rewrite content first (into the OLD path), then git mv preserves history
  fs.writeFileSync(src, JSON.stringify(j,null,2)+"\n");
  cp.execSync(`git mv "${src}" "${destDir}/${id}.json"`);
  cp.execSync(`git rm -r --quiet "${id}" 2>/dev/null || true`); // drop now-empty old dir
}
fs.mkdirSync("generic",{recursive:true});
fs.writeFileSync("generic/.gitkeep","");
console.log("relocated");
'
```

- [ ] **Step 2: Verify the new layout**

Run: `ls server/internal/game/catalog/perks/` (expect: `arch_mage cleric marksman siphoner soldier… trapper vanguard berserker generic`) and spot-check `cat server/internal/game/catalog/perks/siphoner/lingering_hex/lingering_hex.json` — it must have NO `unitType`/`path`/`rank` keys.

- [ ] **Step 3: Do NOT build yet** — the loader still expects the flat layout and will panic. Proceed to C3 before compiling. (This task and C3 land in one commit.)

### Task C3: Rewrite the embed loader for the two-level layout

**Files:**
- Modify: `server/internal/game/perk_defs.go:73-108` (`loadPerkDefs`), `:99-126` (`validatePerkDef`), and the `PerkDef` struct `:162-230`.

- [ ] **Step 1: Remove `UnitType` and `Rank` from the `PerkDef` struct; document `Path` as folder-derived**

In the struct, delete the `UnitType` and `Rank` fields. Change the `Path` doc + tag to:

```go
	// Path is the perk's association: the promotion path whose folder it lives
	// in under catalog/perks/<path>/<id>/. Empty means it lives in
	// catalog/perks/generic/ (usable by any path). DERIVED FROM THE FOLDER at
	// load — never read from the JSON body — and used only for editor picker
	// filtering + display, NOT for rank-up selection (that is perksByRank).
	Path string `json:"path,omitempty"`
```

Leave `ConfigByRank` untouched (it keys off the *unit's* current rank, unrelated to the retired perk `Rank` field).

- [ ] **Step 2: Rewrite `loadPerkDefs` to walk `<assoc>/<id>/<id>.json`**

```go
func loadPerkDefs() map[string]PerkDef {
	assocDirs, err := fs.ReadDir(perkDefsFS, "catalog/perks")
	if err != nil {
		panic("catalog/perks: " + err.Error())
	}
	result := make(map[string]PerkDef)
	for _, assocEntry := range assocDirs {
		if !assocEntry.IsDir() {
			continue
		}
		assoc := assocEntry.Name() // "siphoner", "trapper", …, or "generic"
		// "generic" is the wildcard bucket → empty association.
		assocPath := assoc
		if assoc == "generic" {
			assocPath = ""
		}
		perkDirs, err := fs.ReadDir(perkDefsFS, "catalog/perks/"+assoc)
		if err != nil {
			panic("catalog/perks/" + assoc + ": " + err.Error())
		}
		for _, entry := range perkDirs {
			if !entry.IsDir() {
				continue // skips .gitkeep etc.
			}
			id := entry.Name()
			rel := "catalog/perks/" + assoc + "/" + id + "/" + id + ".json"
			data, err := perkDefsFS.ReadFile(rel)
			if err != nil {
				panic(rel + ": " + err.Error())
			}
			var def PerkDef
			if err := json.Unmarshal(data, &def); err != nil {
				panic(rel + ": " + err.Error())
			}
			if def.ID == "" {
				panic(rel + `: missing "id"`)
			}
			if def.ID != id {
				panic(rel + ": id " + def.ID + " != dir " + id)
			}
			def.Path = assocPath // folder is authoritative for association
			if err := validatePerkDef(&def); err != nil {
				panic(rel + ": " + err.Error())
			}
			if _, dup := result[def.ID]; dup {
				panic(rel + ": duplicate perk id " + def.ID)
			}
			result[def.ID] = def
		}
	}
	return result
}
```

- [ ] **Step 3: Drop the retired `Rank`/`Effect.Target` gate from `validatePerkDef`** (keep the Effect.Target check; remove the Rank switch since the field is gone):

```go
func validatePerkDef(def *PerkDef) error {
	if def.Effect != nil {
		switch def.Effect.Target {
		case "", "self", "enemies":
		default:
			return fmt.Errorf("effect.target %q must be \"self\" | \"enemies\"", def.Effect.Target)
		}
	}
	return nil
}
```

- [ ] **Step 4: Fix any now-broken references to the removed fields**

Compile and follow the errors. Known sites to update (they were test-only assertions handled in Phase B, but re-grep to be safe):

Run: `cd server && grep -rn "\.Rank\b" internal/game/*.go | grep -i perk` and `grep -rn "PerkDef{" internal/game` — remove `UnitType:`/`Rank:` struct-literal fields from any test fixtures.

- [ ] **Step 5: Build + full suite**

Run: `cd server && go build ./... && go vet ./... && go test ./internal/game/`
Expected: PASS + clean. `TestPathPoolsMatchMirror` still green (pools now come purely from perksByRank against the relocated catalog).

- [ ] **Step 6: Commit C2+C3 together**

```bash
git add server/internal/game/catalog/perks server/internal/game/perk_defs.go server/internal/game/*_test.go
git commit -m "refactor(perks): organize catalog by path/generic folders; derive association from folder"
```

### Task C4: Make persistence association(folder)-aware

**Files:**
- Modify: `server/internal/game/perk_persistence.go` (`SavePerkDef`, `DeletePerkOverride`, `LoadPersistedPerksIntoOverlay`)
- Test: `server/internal/game/perk_persistence_test.go` (add a round-trip case)

- [ ] **Step 1: Add a helper that maps an association to its folder segment**

```go
// perkAssocDir returns the on-disk folder segment for a perk association.
// Empty association (generic perk) lives under "generic".
func perkAssocDir(assocPath string) string {
	if assocPath == "" {
		return "generic"
	}
	return assocPath
}
```

- [ ] **Step 2: Rewrite `SavePerkDef` to write `<dir>/<assoc>/<id>/<id>.json` and never persist the derived `Path`**

```go
func SavePerkDef(def *PerkDef) error {
	if !perkIDPattern.MatchString(def.ID) {
		return fmt.Errorf("perk id %q must match %s", def.ID, perkIDPattern)
	}
	if err := validatePerkDef(def); err != nil {
		return err
	}
	dir, err := resolvePerksDir()
	if err != nil {
		return err
	}
	assoc := perkAssocDir(def.Path)
	outDir := filepath.Join(dir, assoc, def.ID)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	// The folder encodes association; do not also write path/unitType/rank into
	// the file body. Clear Path on the copy we marshal (it is re-derived on load).
	toWrite := *def
	toWrite.Path = ""
	raw, err := json.MarshalIndent(&toWrite, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, def.ID+".json"), raw, 0o644); err != nil {
		return err
	}
	runtimePerksMu.Lock()
	runtimePerks[def.ID] = *def // overlay keeps the association in memory
	runtimePerksMu.Unlock()
	rebuildPerkRegistry()
	return nil
}
```

- [ ] **Step 3: Rewrite `DeletePerkOverride` to resolve the folder from the known def**

```go
func DeletePerkOverride(id string) (existed bool, err error) {
	if !perkIDPattern.MatchString(id) {
		return false, nil
	}
	dir, derr := resolvePerksDir()
	if derr != nil {
		return false, derr
	}
	// Resolve association from whatever we currently know about the id
	// (embedded baseline or live overlay) so we target the right folder.
	assocPath := ""
	if d, ok := embeddedPerkDefs[id]; ok {
		assocPath = d.Path
	}
	runtimePerksMu.RLock()
	if d, ok := runtimePerks[id]; ok {
		assocPath = d.Path
	}
	runtimePerksMu.RUnlock()
	assoc := perkAssocDir(assocPath)

	removed := false
	if rerr := os.Remove(filepath.Join(dir, assoc, id, id+".json")); rerr == nil {
		removed = true
		_ = os.Remove(filepath.Join(dir, assoc, id)) // best-effort empty-dir cleanup
	}
	runtimePerksMu.Lock()
	_, inOverlay := runtimePerks[id]
	delete(runtimePerks, id)
	runtimePerksMu.Unlock()
	rebuildPerkRegistry()
	return removed || inOverlay, nil
}
```

- [ ] **Step 4: Update `LoadPersistedPerksIntoOverlay` to derive association from the parent-of-parent folder**

The existing `filepath.WalkDir` already recurses, so it will reach `<assoc>/<id>/<id>.json`. Set `def.Path` from the association folder before overlaying:

```go
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		var def PerkDef
		if json.Unmarshal(raw, &def) != nil || def.ID == "" || validatePerkDef(&def) != nil {
			slog.Warn("persisted perks: skipped file", "file", d.Name())
			return nil
		}
		// Association = the folder two levels up: <dir>/<assoc>/<id>/<id>.json.
		assoc := filepath.Base(filepath.Dir(filepath.Dir(path)))
		if assoc == "generic" {
			def.Path = ""
		} else {
			def.Path = assoc
		}
		runtimePerksMu.Lock()
		runtimePerks[def.ID] = def
		runtimePerksMu.Unlock()
		loaded++
		return nil
	})
```

- [ ] **Step 5: Add a persistence round-trip test**

```go
func TestSavePerkDefWritesAssociationFolder(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PERK_CATALOG_DIR", tmp)
	def := &PerkDef{ID: "test_generic_perk", DisplayName: "Test", Path: ""}
	if err := SavePerkDef(def); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "generic", "test_generic_perk", "test_generic_perk.json")); err != nil {
		t.Fatalf("expected file under generic/: %v", err)
	}
	assoc := &PerkDef{ID: "test_trapper_perk", DisplayName: "Test2", Path: "trapper"}
	if err := SavePerkDef(assoc); err != nil {
		t.Fatalf("save assoc: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(tmp, "trapper", "test_trapper_perk", "test_trapper_perk.json"))
	if strings.Contains(string(raw), "\"path\"") {
		t.Errorf("path key must not be persisted in the file body: %s", raw)
	}
}
```

- [ ] **Step 6: Run + full suite**

Run: `cd server && go test ./internal/game/ -run 'TestSavePerkDef|TestPathPools' -v && go build ./... && go vet ./...`
Expected: PASS + clean.

- [ ] **Step 7: Commit**

```bash
git add server/internal/game/perk_persistence.go server/internal/game/perk_persistence_test.go
git commit -m "feat(perks): association-aware persistence (write under <assoc>/, derive on load)"
```

### Task C5: Fix `filterKnownPerkIDsForUnit` to validate via path→unit

**Files:**
- Modify: `server/internal/game/maps.go:191-213`
- Test: `server/internal/game/maps.go`'s existing perk tests, or add to `maps_test.go` if present.

- [ ] **Step 1: Replace the `def.UnitType` check with an association→unit check**

```go
// filterKnownPerkIDsForUnit drops any perk id absent from the catalog, or whose
// folder association belongs to a DIFFERENT unit type. A generic perk
// (empty association) is a wildcard and always kept. Rank/path-order is
// deliberately NOT enforced here — placed-unit authoring may grant perks out of
// normal progression order, same freedom as debug-spawn. Never fatal.
func filterKnownPerkIDsForUnit(unitType string, ids []string) []string {
	out := ids[:0:0]
	for _, id := range ids {
		def := perkDefByID(id)
		if def == nil {
			slog.Warn("hydratePlacedUnits: dropping unknown perk", "unitType", unitType, "perk", id)
			continue
		}
		if def.Path != "" && unitTypeForPath(def.Path) != unitType {
			slog.Warn("hydratePlacedUnits: dropping perk not valid for unitType",
				"unitType", unitType, "perk", id, "perkPath", def.Path)
			continue
		}
		out = append(out, id)
	}
	return out
}
```

- [ ] **Step 2: Build + full suite**

Run: `cd server && go build ./... && go vet ./... && go test ./internal/game/`
Expected: PASS + clean.

- [ ] **Step 3: Commit**

```bash
git add server/internal/game/maps.go
git commit -m "refactor(perks): validate placed-unit perks by path association, not UnitType field"
```

---

## PHASE D — Frontend: filtered picker + perk-editor cleanup

### Task D1: Filter the path perk picker to associated + generic perks

**Files:**
- Modify: `client/src/game-portal/src/components/UnitTypeEditorPanel.vue:840-846` (`availablePerkOptionsForRank`)
- Test: `client/src/game-portal/src/components/UnitTypeEditorPanel.pathForm.test.ts` (add a case)

- [ ] **Step 1: Write the failing Vitest case**

Add a test asserting the picker for a `siphoner` path only offers perks whose `path` is `"siphoner"` or `""` (generic), excluding e.g. a `trapper` perk. Model it on the existing tests in that file (they already mount the panel with a `perkDefs` fixture). Assert `availablePerkOptionsForRank('bronze')` excludes a fixture perk with `path:'trapper'` and includes one with `path:''`.

- [ ] **Step 2: Run to verify it fails**

Run: `cd client/src/game-portal && npx vitest run src/components/UnitTypeEditorPanel.pathForm.test.ts`
Expected: FAIL (picker currently returns all perks).

- [ ] **Step 3: Implement the association filter**

```ts
// Catalog perks eligible for THIS path's picker: association matches the path
// being edited, or the perk is generic (empty association). Already-referenced
// perks at this rank are excluded. Sorted alphabetically — mirrors abilityOptions.
function availablePerkOptionsForRank(rank: string): FilterableOption[] {
  const taken = new Set(perksForRank(rank))
  const activePath = pathForm.value?.path ?? ''
  return perkDefs.value
    .filter((d) => !taken.has(d.id))
    .filter((d) => !d.path || d.path === activePath) // generic OR associated
    .map((d) => ({ id: d.id, label: d.displayName || d.id }))
    .sort((a, b) => a.label.localeCompare(b.label))
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd client/src/game-portal && npx vitest run src/components/UnitTypeEditorPanel.pathForm.test.ts`
Expected: PASS.

- [ ] **Step 5: Type-check (build mode — `--noEmit` false-cleans in this repo)**

Run: `cd client/src/game-portal && npx vue-tsc -b`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add client/src/game-portal/src/components/UnitTypeEditorPanel.vue client/src/game-portal/src/components/UnitTypeEditorPanel.pathForm.test.ts
git commit -m "feat(editor): filter path perk picker to associated + generic perks"
```

### Task D2: Retire `unitType`/`rank` from the standalone perk editor form

**Files:**
- Modify: `client/src/game-portal/src/game/perks/perkEditorForm.ts:17-33`
- Modify: `client/src/game-portal/src/components/PerkEditorPanel.vue` (remove unitType/rank inputs; show `path` read-only)
- Test: `client/src/game-portal/src/game/perks/perkEditorForm.test.ts` (if present) — update expected modeled keys.

- [ ] **Step 1: Drop `unitType` and `rank` from the interface + modeled keys**

In `perkEditorForm.ts`, remove the `unitType?: string` and `rank?: string` fields from `AuthoredPerkDef`, and remove `'unitType'` and `'rank'` from the `MODELED_PERK_KEYS` array (keep `'path'`). Keep `path` read-only on the form (it is folder-derived and returned by the API; the editor displays it but the save request should not move files — path changes are out of scope for this panel and handled by the catalog folder).

- [ ] **Step 2: Remove the unitType/rank controls from `PerkEditorPanel.vue`**

Delete the `<select>`/`<input>` bindings for `form.unitType` and `form.rank`. Add a read-only line showing the association: `Association: {{ form.path || 'generic' }}`.

- [ ] **Step 3: Update / add the form round-trip test**

Assert `perkFormFromDef` no longer surfaces `unitType`/`rank` as modeled keys and that `saveRequestFromPerkForm` omits them.

- [ ] **Step 4: Run tests + type-check**

Run: `cd client/src/game-portal && npx vitest run src/game/perks && npx vue-tsc -b`
Expected: PASS + clean.

- [ ] **Step 5: Commit**

```bash
git add client/src/game-portal/src/game/perks/perkEditorForm.ts client/src/game-portal/src/components/PerkEditorPanel.vue client/src/game-portal/src/game/perks/perkEditorForm.test.ts
git commit -m "feat(editor): perk editor drops unitType/rank; shows folder association read-only"
```

---

## Final verification

- [ ] **Backend:** `cd server && go build ./... && go vet ./... && go test ./internal/game/` → PASS + clean.
- [ ] **Frontend:** `cd client/src/game-portal && npx vitest run && npx vue-tsc -b` → PASS + clean.
- [ ] **Behavior sanity (manual, via /run or a match):** promote a Siphoner Bronze→Silver→Gold and confirm it still rolls from the same pools it did pre-refactor (the `TestPathPoolsMatchMirror` guard covers this deterministically, but a live spot-check confirms end-to-end).
- [ ] **Editor sanity:** open the Unit Type Editor → a path's Perk References → the Add-perk dropdown now lists only that path's perks + generic; the standalone Perk editor no longer shows unitType/rank.

---

## Self-Review notes (author checklist, done)

- **Spec coverage:** (1) organize by path folders + generic → Phase C2; (2) perks defined to a specific unit/path as needed → folder association + `unitTypeForPath`; (3) editor brings in associated/generic perks and assigns to bronze/silver/gold → existing `perksByRank` UI + D1 filter; (4) user determines what's pulled at rank-up → Phase B authoritative. ✔
- **Behavior preservation:** Phase A/B guard test `TestPathPoolsMatchMirror` proves zero pool change; `arch_mage` gold-only preserved. ✔
- **Determinism:** `eligiblePerksForUnitAtRank` keeps the ID-sort so seeded rolls are unchanged (AI_RULES.md). ✔
- **Type consistency:** `PerkDef.Path` is the single association field end to end (Go struct, API JSON `path`, client `AuthoredPerkDef.path`); `UnitType`/`Rank` removed in both Go and TS. ✔
- **Open micro-decision flagged for executor:** whether the standalone Perk editor should allow *moving* a perk between association folders (a file move) — deferred out of scope in D2; today association is set by where the file is created. Revisit only if the user wants in-editor re-association.
