# Standalone Perks — Sub-project 2: Rank-on-reference opt-in + union selection — Design

**Date:** 2026-07-13
**Branch:** `perks-standalone` (continues on top of SP1; nothing merged to `main` until the whole perks effort is done)
**Program context:** Perk-system redesign, sub-project 2 of 2. SP1 made perks standalone id-addressed catalog defs with their own editor (behavior-identical). SP2 adds the **explicit per-rank unit→perk reference** (opt-in) and makes selection the **union** of eligibility-auto-match + references — delivering the many-to-many "one perk, many units at any rank" goal.

## Problem & goal

After SP1, a unit's perks are still chosen purely by the perk's own eligibility wildcards (`PerkDef.UnitType`/`Path`/`Rank`). To let an author explicitly grant a specific standalone perk to a specific path at a specific rank — independent of the perk's own eligibility, and reusable across units/ranks — add an explicit reference on the path and union it into selection. **Purely additive:** references start empty, so behavior is byte-identical until an author opts a perk in.

## Foundational findings (from recon — bind the design)

- **`pathCatalogFile`** (`path_defs.go:27-93`) is the persisted path def, addressed by `{unit, path}`. It **already carries a rank-keyed map** `Ranks map[string]pathRankStatsJSON` (`:92`) — the exact precedent a `perksByRank map[string][]string` mirrors. Rank keys validate against `validRankName` (bronze/silver/gold; `path_defs.go:412-416`). Path's `abilities` are validated per-id at `path_defs.go:468-477` — the pattern a perk-id validation mirrors.
- The whole path file round-trips through `SavePathDef` (`path_persistence.go:137-177`), `EditorPathEntry{unit,path,def:rawJSON}` (`path_editor.go:98-102`), `POST /paths {unit, path:<file>}` (`editor_handlers.go:457-470`), `GET /catalog/paths` → `ListPathDefsFull` (`path_editor.go:110-145`). **So a new `perksByRank` field persists + reaches the client with no new endpoints.**
- **Selection is computed in ONE place:** `eligiblePerksForUnitAtRank(unit, rank)` (`perk_defs.go:324-345`) — iterates `snapshotPerkDefs()`, filters by the perk's `UnitType`/`Path`/`Rank` wildcards (`""`=any), appends survivors, **sorts by `def.ID`** (determinism, `:343`). The downstream chain (`perkPoolForRankLocked` rank cascade → `eligiblePerksAfterFiltersLocked` already-owned + `RequiresPerk` gate → `rngPerks.Intn` pick, all in `perks.go:937-1059`) reads that pool and needs **no change** — union'd perks flow through the same filters.
- `perkDefLookup(id) (*PerkDef, bool)` (`perk_defs.go:278-283`) resolves a perk id → def. `unit.UnitType`, `unit.ProgressionPath`, `unit.Rank` (`state.go:146-148`) are the selection inputs.
- The path system has a derived-map rebuild (`rebuildDerivedPathMaps`, `path_persistence.go:61-129`; `registerPathFileInto`, `path_defs.go:580-670`; `pathDerivedMaps`, `path_defs.go:500-512`) — the clean place to expose a fast, deterministic `(unitType, path, rank) → []perkId` lookup.
- **Nothing today reads a per-path perk list** — selection is purely eligibility-driven. So empty references break nothing; the union is `wildcard ∪ ∅ = wildcard` (SP1-preserved behavior).

## Decisions (from brainstorming)

1. **Reference lives on `PathDef`** (`pathCatalogFile.perksByRank map[string][]string`), mirroring `Ranks`. NOT on UnitDef (would force nested `map[path][rank]` + re-introduce unit↔path addressing the path already owns).
2. **Hybrid, union selection:** keep the perk's eligibility wildcards (auto-match half); the reference is the opt-in half; a unit's pool at rank R = auto-match(unit,R) ∪ referenced-ids(path,R), **deduped**, **ID-sorted** (determinism).
3. **References start empty** — purely additive, behavior-identical until authored. No migration, no seeding.
4. **Rank is on the reference** — `perksByRank` is keyed by rank, so the same perk id can be referenced at different ranks by different paths.

## Architecture

### §1 Server — the reference field + validation (`path_defs.go`)
- Add `PerksByRank map[string][]string \`json:"perksByRank,omitempty"\`` to `pathCatalogFile`, beside `Ranks`.
- In `validatePathFile` (`path_defs.go:431-492`): validate each key of `PerksByRank` against `validRankName` (bronze/silver/gold), and each perk id against `perkDefLookup` (unknown id → error), mirroring the existing per-ability validation block. Empty/absent map = valid (additive).

### §2 Server — derived lookup + the union (`path_persistence.go` / `path_defs.go` + `perk_defs.go`)
- Expose a package helper `pathPerkRefsForRank(unitType, pathName, rank string) []string` reading a derived map keyed by the path topology (populated in the path derived-map rebuild alongside the existing per-path derived data). Returns the referenced perk ids for that (unit, path, rank), or nil. Reads under the path system's existing synchronization.
- In `eligiblePerksForUnitAtRank` (`perk_defs.go:324-345`): after the existing wildcard loop (which builds the auto-match slice + a `seen` set of ids), append each `pathPerkRefsForRank(unit.UnitType, unit.ProgressionPath, rank)` id that (a) resolves via `perkDefLookup` and (b) isn't already in `seen`. Then keep the existing `sort.Slice(... def.ID ...)`. Dedup + sort preserve replay determinism.
- No change to the downstream selection chain (owned filter, RequiresPerk, cascade, RNG).

### §3 Client — the reference field on the path form
- `game/units/pathEditorForm.ts`: add `perksByRank?: Record<string, string[]>` to `AuthoredPathDef`, and add `'perksByRank'` to `MODELED_PATH_KEYS` (else it only round-trips via `remainder` and isn't first-class editable). `saveRequestFromPathForm` already skips `undefined`, so an unset map isn't written — additive by construction.

### §4 Client — the Perk References editor section (`UnitTypeEditorPanel.vue`)
- Add a **"Perk References"** `SectionCard` in the path form's **Combat** tab (alongside `ranks`; add `'perks'` to that tab's `sections` in `PATH_TABS` and render the card `v-show="activePathTab === pathSectionTab('perks')"`, mirroring the Ranks card at `:613-623`).
- The card binds `pathForm.perksByRank` and offers, per rank (bronze/silver/gold): an add/remove list of perk ids chosen from the standalone perk catalog (`GET /catalog/perks`, fetched on mount into a ref; show id + displayName, an "inert" hint when `!wired`). Updates follow the same `@update:`→assign pattern as the Ranks grid (`onPathRanksUpdate`).
- No save-flow change — the refs persist inside `req.path` via the existing `savePath()`.

## Error handling
- Bad rank key or unknown perk id in `perksByRank` → `validatePathFile` error → HTTP 400 `validation_failed` → editor inline error.
- A referenced perk id that later gets deleted → `perkDefLookup` miss at selection → simply not added to the pool (fail-safe, same discipline as an unknown ability id). The editor's catalog list keeps authors from typoing.
- Empty/absent `perksByRank` → no-op, behavior-identical.

## Testing
- **Go:** `perksByRank` round-trips through `SavePathDef` (save a path with refs, reload, refs present); `validatePathFile` rejects a bad rank key and an unknown perk id; **union+dedup** — `eligiblePerksForUnitAtRank` includes a referenced perk the eligibility wouldn't match, a perk matching BOTH halves appears exactly once, and the result stays ID-sorted; **behavior-identical** — with empty refs, the pool equals the pre-SP2 (SP1) result for representative (unit, path, rank). Determinism replay unchanged.
- **Client (vitest):** path form round-trips `perksByRank`; the Perk References section adds/removes ids per rank and writes them into the form.
- **Build gates:** server `go build`/`vet`/`test`; client `npm run build` + `npm run test` (3 pre-existing `ListEditorPanel.test.ts` failures expected).
- **Manual E2E:** in the Unit Types editor, open a path → Combat → Perk References → add a perk to `bronze` that the perk's own eligibility does NOT match → Play → confirm the unit can now roll it at bronze; remove it → gone; confirm a path with no refs behaves exactly as before.

## Out of scope (SP2)
- Removing/retiring the perk eligibility wildcards (they stay as the auto-match half of the hybrid; a future "seed refs + drop eligibility" pass is a possible SP3, not now).
- Unit-level (non-path) perk references.
- Any change to perk mechanics/hooks or the standalone catalog/editor from SP1.

## Global constraints
- Purely additive; runtime behavior-identical with empty references. Determinism preserved (dedup + ID-sort in the union). No `game`→`profile` write.
- Referenced perk ids resolved fail-safe (unknown id = not added); path rank keys gated by `validRankName`.
- No literal `cursor:` in new component CSS except `cursor: not-allowed` on forbidden states.
- Build gates as above; per-task commits, explicit `git add`, no push, nothing to `main`.
- Mirror the existing path-file field + editor idioms (`Ranks`/`abilities`); the union is a single, dedup+sort-preserving edit.
