# Standalone Perks — Sub-project 1: Standalone Perk Catalog + Editor — Design

**Date:** 2026-07-13
**Branch:** `perks-standalone` (off `reference-def-editors`, base `a53ff60`; nothing merged to `main` until the whole perks effort is done)
**Program context:** Perk-system redesign. Perks become standalone, generic catalog defs (like abilities/projectiles/effects) that units reference — replacing today's pools-nested-under-units authoring. This is **sub-project 1 of 2**: move perks to a standalone catalog + own editor, runtime **behavior-identical**. Sub-project 2 adds the rank-on-reference unit→perk opt-in and union selection.

## Problem & goal

Perks are authored today as **pools** nested at `catalog/units/<faction>/<unit>/paths/<path>/perks/<rank>.json`, with `UnitType`/`Path`/`Rank` **injected from the directory path** (not present in the JSON). This ties perk authoring to a single unit/path/rank and blocks the goal of one perk being reusable across many units. Move perks to a **standalone catalog** `catalog/perks/<id>/<id>.json`, with each def self-contained, edited in a dedicated editor — **without changing any in-match behavior**.

## Foundational findings (from recon — bind the design)

- **The runtime registry is ALREADY global-by-id.** `perkDefsByID map[string]*PerkDef` (`perk_defs.go:205`) is keyed by the bare perk id; every behavior hook, `GrantsAbilities`, `Effect.Name`, `Icon` resolves by global id. So this is an **authoring-format** change, not a runtime-engine change.
- **Units do NOT reference perks by id today.** The only unit→perk linkage is the eligibility wildcards (`PerkDef.UnitType`/`Path`/`Rank`, `""`=any) filtered by `eligiblePerksForUnitAtRank` (`perk_defs.go:503-524`). `Unit.PerkIDs` holds only rolled results. (Adding a unit→id reference is **sub-project 2**, out of scope here.)
- **`PerkDef`** (`perk_defs.go:92-160`): identity (`ID`,`DisplayName`,`Description`,`Icon`); tooltip (`TooltipTemplate`,`TooltipTemplateByTrap`,`TooltipTemplateByOwnedPerk`); **eligibility** (`UnitType`,`Path`,`Rank`,`RequiresPerk`); tuning (`Config map[string]float64`,`ConfigByRank map[string]map[string]float64`); `Effect *PerkEffect`; `GrantsAbilities []string`; derived `Wired bool` (set only by `ListPerkDefs`, never stored).
- **Authoring shape today** (`perkEntryJSON`, `perk_defs.go:238-250`): per-entry JSON WITHOUT `UnitType`/`Path`/`Rank` (injected from path), and `config` decoded as `map[string]json.RawMessage` then split by `splitRankConfig` into base `Config` + per-rank `ConfigByRank`.
- **Existing pool machinery to retire:** loader walk (`perk_defs.go` init `:415-477`), `embeddedPerkPools`/`perkPoolKey`, `perk_persistence.go` (`runtimePerkPools`, `rebuildPerkRegistry`, `SavePerkPool`, `DeletePerkPool`, `validatePerkPoolEntries`, `LoadPersistedPerksIntoOverlay`), `perk_editor.go` (`SaveEditorPerkPool*`, `DeleteEditorPerkPool`), HTTP `POST /perks {unit,path,rank,perks}` + `DELETE /perks/{unit}/{path}/{rank}`, client `PerkPoolEditor.vue`, `UnitTypeEditorPanel.vue` perk section (`:627-633`, `poolsForPath` `:785-794`, per-rank save loop `:1698-1700`), `pathEditorApi.ts` `savePerks`/`deletePerks`.
- **Ship scale:** 21 pool files, **72 distinct perk ids**, 6 unit/path combos. `validatePerkPoolEntries` already enforces **globally-unique perk ids** — so no id collisions when flattening to a standalone per-id catalog.
- **`perk_wired.go`** `wiredPerkIDs` (hand-maintained set of ids with a Go handler) is presentation-only ("inert" badge) and its own comment flags the redesign; keep it as-is in SP1.

## Decisions (from brainstorming)

1. **Standalone catalog** `catalog/perks/<id>/<id>.json`, one full `PerkDef` per file (eligibility fields authored on the def). Mirrors the effect/projectile catalog layout.
2. **Behavior-identical:** selection still uses `eligiblePerksForUnitAtRank`'s wildcard filter; the eligibility values are now read from the def's own fields (migrated from the old pool paths). No in-match change.
3. **Rank stays on the def in SP1** (migrated from the pool path). Moving rank to the reference is sub-project 2.
4. **Retire the pool authoring system** (endpoints, overlay, editor, `PerkPoolEditor`, unit-editor perk section) — replaced by the standalone Perks editor. Perk authoring MOVES from the unit editor to the dedicated editor.
5. **A migration generator** produces the standalone catalog from the current pools, verified to yield a byte-identical registry.

## Architecture

### §1 The standalone catalog format
- `server/internal/game/catalog/perks/<id>/<id>.json` — a marshaled `PerkDef` with ALL fields, including `unitType`/`path`/`rank` (previously injected) and the **already-split** `config` (scalars) + `configByRank` (per-rank overrides). Embed via `//go:embed all:catalog/perks` on a new `perkDefsFS`.
- The `PerkDef` struct is **unchanged**. Only the *source* of `UnitType`/`Path`/`Rank`/`Config`/`ConfigByRank` changes: read from JSON, not injected/split at load.

### §2 Migration generator (one-shot, output committed)
- A Go program `server/cmd/migrate-perks/main.go` (removed after the catalog is committed, or kept as a documented one-shot): walks the current `catalog/units/**/paths/**/perks/*.json` pool tree, and for each entry emits `catalog/perks/<id>/<id>.json` with:
  - `unitType`/`path`/`rank` set from the pool file's directory path;
  - `config`/`configByRank` produced by the existing `splitRankConfig` logic (so the standalone def stores the already-split form);
  - all other entry fields copied verbatim.
- **Equivalence verification (the safety gate):** a Go test builds the perk registry from the OLD pools (via the existing `buildPerkDefsFromPool` path) and from the NEW standalone catalog, and asserts the two `map[string]*PerkDef` are deep-equal (same 72 ids, same field values). This proves the migration is lossless before the old system is deleted.

### §3 Loader (`perk_defs.go`)
- Replace the `init()` unit-tree walk with `loadPerkDefs()` reading `catalog/perks/<id>/<id>.json` (mirror `loadEffectDefs`/`loadProjectileDefs`): unmarshal `PerkDef` directly, validate via a new `validatePerkDef`, panic on embed errors, dup-id panic, build `perkDefsByID`.
- Extract `validatePerkDef(def *PerkDef) error` (id non-empty at load / regex at save; `Rank` in {"",bronze,silver,gold}; `Effect.Target` in the allowed set if `Effect` set; any other load-time checks the pool validator did). Shared by loader + save.
- Delete `embeddedPerkPools`, `perkPoolKey`/`splitPerkPoolKey`, `buildPerkDefsFromPool`, `perkEntryJSON`, `splitRankConfig` (the last two only if unused post-migration; `splitRankConfig` may be reused by the migration generator, then removed).
- `eligiblePerksForUnitAtRank`, `perkDefLookup`, `snapshotPerkDefs`, `ListPerkDefs`, `ConfigForRank`, `PerkEffect` — **unchanged**.

### §4 Persistence (`perk_persistence.go`) — id-addressed overlay
- Replace pool-addressed machinery with the effect/projectile pattern: `perkIDPattern`, `resolvePerksDir()` (env `PERK_CATALOG_DIR`, else dev `internal/game/catalog/perks`), `runtimePerks map[string]PerkDef` + RWMutex, `SavePerkDef`, `PerkIsEmbedded`, `DeletePerkOverride`, `LoadPersistedPerksIntoOverlay` (walk `catalog/perks`).
- Make `perkDefLookup`/`snapshotPerkDefs`/`ListPerkDefs` overlay-aware (overlay-wins over embed), preserving the existing `perkDefsMu`-synchronized read contract and the sorted-by-id determinism the selection relies on.
- Remove `runtimePerkPools`, `rebuildPerkRegistry`, `SavePerkPool`, `DeletePerkPool`, `PerkPoolIsEmbedded`, `validatePerkPoolEntries`, `removePerkPoolFile`, `loadPersistedPerkPoolsFromDir`.
- `resolvePerksDir` no longer aliases `resolveUnitsDir` (perks have their own dir now).
- `cmd/api/main.go`: `LoadPersistedPerksIntoOverlay()` stays wired (its body changes; the call site is unchanged).

### §5 Editor wrapper + HTTP (`perk_editor.go`, `editor_handlers.go`)
- `perk_editor.go`: replace the pool wrappers with `EditorPerkSaveRequest{ Perk PerkDef }`, `SaveEditorPerk` (id-regex + `validatePerkDef` → `editorValidationError`, then `SavePerkDef`), `DeleteEditorPerk(id)` → `DeletePerkOverride`.
- `editor_handlers.go`: replace `POST /perks {unit,path,rank,perks}` + `DELETE /perks/{unit}/{path}/{rank}` with `POST /perks {perk}` + `DELETE /perks/{id}` — the effect/projectile shape (201 saved / 400 validation_failed / invalid_id / 404 / deleted|reset via `PerkIsEmbedded`).
- `GET /catalog/perks` (`router.go`) — unchanged endpoint; now returns the standalone-def list (still `{perks: ListPerkDefs()}` with `wired`).

### §6 Client — standalone editor triad + retiring the pool UI
- New `game/perks/perkEditorForm.ts` (`AuthoredPerkDef` modeling all `PerkDef` fields + `remainder`), `perkEditorApi.ts` (`fetchAuthoredPerkDefs` GET `/catalog/perks` → `{perks}`, `saveEditorPerk` POST `/perks {perk}`, `deleteEditorPerk` DELETE), `components/PerkEditorPanel.vue` (full-coverage form: identity, icon, eligibility `unitType`/`path`/`rank`, `requiresPerk`, tooltip templates, config/configByRank maps, effect, grantsAbilities; a `wired` read-only badge), `views/PerkEditor.vue` + `/perk-editor` route, and a world-editor "Perks" screen (flip `perks` toolbar enabled + the current screen-switch wiring).
- **Retire:** delete `components/PerkPoolEditor.vue` (+ test); remove the perk section from `UnitTypeEditorPanel.vue` (the `<PerkPoolEditor>` host, `poolsForPath`, the per-rank `savePerks` loop, `perkCatalog` if now unused there); remove `savePerks`/`deletePerks` from `pathEditorApi.ts` (keep `fetchPerkCatalog` if still used, else drop). `game/maps/perkDefs.ts` and `game/core/perkTooltip.ts` stay (runtime rendering).
- Field coverage for the panel is **full** (consistent with the other editors). Eligibility `unitType`/`path`/`rank` render as inputs/dropdowns with a blank = "(any)" wildcard option.

## Error handling
- Migration: the equivalence test is a hard gate — if the standalone catalog doesn't reproduce the exact registry, the migration is wrong and must be fixed before deleting the pool system.
- Save with an invalid perk (bad id, bad rank, bad effect target) → `editorValidationError` → HTTP 400 → panel inline error.
- Delete of an embedded perk = reset-to-shipped-default; overlay-only = true delete.
- Overlay dir unwritable / bad startup file → best-effort skip, logged, never fatal.
- An authored perk whose id has no Go handler is **inert** (the existing `wired` badge already surfaces this) — unchanged.

## Testing
- **Migration equivalence (Go):** old-pools registry ≡ new-catalog registry (deep-equal on all 72 defs). THE gate.
- **Loader (Go):** the standalone catalog loads; `validatePerkDef` rejects bad rank/effect-target; every shipped perk id present; `eligiblePerksForUnitAtRank` returns the same set for representative (unit, rank) pairs as before (behavior-identical selection).
- **Persistence (Go):** `SavePerkDef` → overlay → `perkDefLookup` round-trip; overlay-wins; disk round-trip + embed-revert; bad-id reject; `POST /perks`/`DELETE /perks/{id}` 201/400/deleted/reset.
- **Client (vitest):** perk form transforms + remainder; api 400 → `EditorValidationError`; panel mounts + lists; toolbar renders Perks enabled; `UnitTypeEditorPanel` still mounts/saves paths with the perk section removed (no dangling refs).
- **Determinism:** a match seed replay produces identical perk rolls before/after (the selection pool + sort order are preserved).
- **Manual E2E:** author/edit a perk in the standalone editor (set eligibility) → start a match → confirm the unit rolls it at the eligible rank exactly as before; reset an embedded perk; confirm the unit editor no longer shows a perk section and still saves paths.

## Out of scope (SP1)
- The **unit→perk reference / rank-on-reference / union selection** (sub-project 2).
- New perk mechanics or `PerkDef` fields; `perk_wired.go` overhaul (stays as-is).
- Perk **art** upload (icons already resolve via the action-icon catalog by id).

## Global constraints
- Runtime **behavior-identical** — selection, determinism, and every perk hook unchanged. No `game`→`profile` write.
- `perkIDPattern = ^[a-z0-9_]+$` — path-traversal gate on every id entry point.
- The registry stays global-by-id and its reads stay `perkDefsMu`-synchronized and sorted for replay determinism.
- No literal `cursor:` in new component CSS except `cursor: not-allowed` on forbidden states.
- WorldEditorPanel wiring uses the CURRENT screen-switch pattern.
- Build gates: server `go build`/`vet`/`test` (not gofmt); client `npm run build` + `npm run test` (3 pre-existing `ListEditorPanel.test.ts` failures are expected). Per-task commits, explicit `git add`, no push, nothing to `main`.
- Mirror the effect/projectile/ability editor idioms; the migration is the only novel piece.
