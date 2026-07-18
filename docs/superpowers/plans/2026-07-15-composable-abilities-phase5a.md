# Composable Abilities — Phase 5a (Editor Backend + TS Enablers) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Build the backend endpoints + TypeScript API/types the new composable ability editor (Phase 5b UI) will consume: a registry-driven **action schema** endpoint (so the inspector is schema-driven, not hardcoded), a **structured validation** endpoint (per-card issue paths), a **convert** endpoint (legacy → composable, with honest runnable/degradation warnings), and a **compiled-program** field on the catalog GET (so the editor can display a legacy ability's flow). All testable (Go httptest + Vitest); no UI yet.

**Architecture:** Extend the existing `registerAbilityCatalogRoutes` / `editor_handlers.go` patterns and `abilityEditorApi.ts`. The action registry (`ActionDescriptor.Schema`, Phase 3) is the single source for editor controls — exposed via a new exported `game.ActionSchemas()`. Validation reuses the authoritative `validateAbilityProgram`. Convert reuses `compileLegacyAbility` + the runnable classification from Phase 4.

**Why 5a before the UI:** these are the data contract the four-region editor renders against, each independently testable — building them first de-risks the (visually-reviewed) UI slices. This is NOT premature: the design (§9) mandates schema-driven controls from the registry and authoritative validation.

**Tech Stack:** Go (`server/internal/http`, `server/internal/game`) + TS (`client/src/game-portal/src/game/abilities/`). `cd server && go test ./internal/http/ ./internal/game/`; `cd client/src/game-portal && npm run test -- <file>` + `npx vue-tsc -b`. Do NOT run `git commit`.

**Existing shapes (verified):**
- `abilityCatalogEntry{ game.AbilityDef; GeneratedDescription string }` (`router.go:41`) served by `GET /catalog/abilities` (`:47`). `AbilityDef` already json-marshals `program`/`schemaVersion` (omitempty).
- Editor mutations in `editor_handlers.go`: `POST /abilities` (`:292`, decodes `game.EditorAbilitySaveRequest{Ability AbilityDef}`, calls `game.SaveEditorAbility` → 400 `validation_failed` / 201 `saved`), `POST /abilities/{id}/image`, `DELETE /abilities/{id}`. `writeJSON`/`writeJSONError` helpers exist.
- `game.SaveEditorAbility(req)` (`ability_editor.go:12`) validates id + `validateAbilityDef` (wraps as `editorValidationError`).
- `game.compileLegacyAbility(def) *AbilityProgram` (pure), `validateAbilityProgram(prog) []ValidationIssue` (`ValidationIssue{Path,Code,Message,Severity}`), `actionRegistry` (unexported map of `ActionDescriptor{Type,Decode,Validate,Schema,Execute}`), `allActionTypes []ActionType`, and the Phase-4 runnable classification (`programIsExecutorRunnable` was a TEST helper — a package-level equivalent must be added here).
- TS: `abilityEditorApi.ts` (`fetchAuthoredAbilityDefs`, `saveEditorAbility`, `EditorValidationError`, catalog fetchers using `getJson`/`API_BASE`); `program/abilityProgram.ts` (`AbilityProgram` + `parseProgram`/`serializeProgram`); `abilityEditorForm.ts` (`AuthoredAbilityDef` with `program?`/`schemaVersion?`).

---

### Task 1: Action-schema endpoint (`GET /catalog/action-schema`)

**Files:**
- Modify: `server/internal/game/ability_program_registry.go` (export `ActionSchemas()` + a runnable helper)
- Create: `server/internal/game/ability_program_enums.go` (`ProgramEnums()`)
- Modify: `server/internal/http/router.go` (route in `registerAbilityCatalogRoutes`)
- Test: `server/internal/game/ability_program_schema_test.go`, `server/internal/http/router_ability_schema_test.go`

- [ ] **Step 1: Write failing Go tests.**
  - `game`: `TestActionSchemasCoversRegistry` — `ActionSchemas()` returns one entry per registered action type; each has the `Type`, its `Schema.Fields`, and a `Runnable bool` (== descriptor `Execute != nil`); assert `deal_damage` is Runnable with an `amount` field, `play_presentation` is absent (no descriptor) or present-but-not-runnable per the chosen contract. `TestProgramEnumsNonEmpty` — `ProgramEnums()` returns non-empty slices for entryTypes, relations, triggerTypes, actionTypes, targetSources, targetOrigins, targetOrderings.
  - `http`: `TestActionSchemaEndpoint` — httptest GET `/catalog/action-schema`, 200, JSON has `actions` (array with `type`/`fields`/`runnable`) and `enums` (object). (Use the existing router test harness — read an existing `router_*_test.go` for how it builds the mux + does an httptest request.)

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement.**
  - In `ability_program_registry.go`: add exported types + func:
    ```go
    // ActionSchema is one action's editor metadata: its type, the inspector
    // controls, and whether the executor can run it today (Execute != nil) — the
    // editor surfaces non-runnable actions as "display-only" (deferred mechanics).
    type ActionSchema struct {
        Type     ActionType        `json:"type"`
        Fields   []SchemaField     `json:"fields"`
        Runnable bool              `json:"runnable"`
    }
    // ActionSchemas returns the editor schema for every action type in allActionTypes,
    // sorted by type for determinism. Types with a registered descriptor use its
    // Schema + Execute!=nil; types without a descriptor (deferred) get empty Fields
    // + Runnable=false so the editor can still list them.
    func ActionSchemas() []ActionSchema { ... iterate allActionTypes, lookupActionDescriptor, sort by Type ... }
    // actionTypeIsRunnable reports whether an action type has a registered non-nil Execute.
    func actionTypeIsRunnable(t ActionType) bool { d, ok := lookupActionDescriptor(t); return ok && d.Execute != nil }
    ```
  - New `ability_program_enums.go`: `func ProgramEnums() map[string][]string` returning the string values for `entryTypes` (EntrySelf..EntryPassive), `relations` (Rel*), `triggerTypes` (all TriggerType consts), `actionTypes` (allActionTypes as strings), `targetSources`, `targetOrigins`, `targetOrderings`, `zoneAnchors`, `conditionOps` (["eq","ne","lt","lte","gt","gte","has","not"]). Build each from the const set (a small hand-listed slice per enum — keep it beside the enum or here; add a drift-guard test like `TestAllActionTypesMatchesSourceConsts` if the actionTypes list duplicates `allActionTypes` — actually reuse `allActionTypes` directly for actionTypes to avoid drift).
  - Route in `registerAbilityCatalogRoutes`:
    ```go
    mux.HandleFunc("/catalog/action-schema", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{"actions": game.ActionSchemas(), "enums": game.ProgramEnums()})
    })
    ```

- [ ] **Step 4: Run** all new + `go test ./internal/game/ ./internal/http/ -count=1` + `go build ./...` + `go vet` + `gofmt -l`. Green.

- [ ] **Step 5: Commit** — "feat(abilities): action-schema endpoint for the composable editor".

---

### Task 2: Structured validation endpoint (`POST /abilities/validate`)

**Files:**
- Modify: `server/internal/game/ability_editor.go` (add `EditorAbilityIssues`)
- Modify: `server/internal/http/editor_handlers.go` (route)
- Test: `server/internal/game/ability_editor_test.go`, `server/internal/http/editor_validate_test.go`

- [ ] **Step 1: Write failing tests.**
  - `game`: `TestEditorAbilityIssues` — for a def with a `Program` containing a `deal_damage{amount:0}` action, `EditorAbilityIssues(def)` returns a `[]ValidationIssue` including the `empty_required_property` error with its `Path` (`triggers[..].actions[..]`). For a clean legacy heal (no program), returns empty. For an invalid id / bad damageType, returns a def-level issue.
  - `http`: `TestValidateEndpoint` — httptest `POST /abilities/validate` with `{ability: <def with a bad program>}` → 200 with `{issues: [...]}` (NOT a 400 — validate is a dry-run that always 200s and returns the issue list; only malformed JSON is 400). Confirm the issues carry `path`/`code`/`message`/`severity`.

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement.**
  - `EditorAbilityIssues(def AbilityDef) []ValidationIssue`: collect (a) def-level issues — mirror `validateAbilityDef`'s checks but as issues rather than a single error (id pattern via `abilityIDPattern`, damageType/category registered, the burn/tick guards), and (b) if `def.Program != nil`, append `validateAbilityProgram(def.Program)`. Each def-level issue gets a `Path` like `"identity.id"` / `"identity.damageType"` / a mechanic path so the editor can map it. Return the combined slice (empty = valid). Keep `SaveEditorAbility`/`validateAbilityDef` unchanged (save still hard-fails on the first error — this endpoint is the richer dry-run).
  - Route `POST /abilities/validate` in `editor_handlers.go` (register it BEFORE the `/abilities/` catch-all or as `/abilities/validate` exact — mind ServeMux longest-prefix; `/abilities/validate` is more specific than `/abilities/` so register both, exact wins). Decode `EditorAbilitySaveRequest`, return `writeJSON(w, map[string]any{"issues": game.EditorAbilityIssues(req.Ability)})`. Malformed JSON → 400 `invalid_json`.

- [ ] **Step 4: Run** all + full `go test ./internal/game/ ./internal/http/` + build/vet/gofmt. Green. Confirm existing `/abilities` save + delete tests still pass (route precedence intact).

- [ ] **Step 5: Commit** — "feat(abilities): structured dry-run validation endpoint".

---

### Task 3: Convert endpoint (`POST /abilities/{id}/convert`)

**Files:**
- Modify: `server/internal/game/ability_editor.go` (add `ConvertLegacyAbility`)
- Modify: `server/internal/http/editor_handlers.go` (route inside the `/abilities/` handler)
- Test: `server/internal/game/ability_convert_test.go`, `server/internal/http/editor_convert_test.go`

- [ ] **Step 1: Write failing tests.**
  - `game`: `TestConvertLegacyAbility` — `ConvertLegacyAbility("greater_heal")` returns `(def, warnings, err)` where the returned def has `SchemaVersion==2`, a non-nil `Program` (== `compileLegacyAbility`), the top-level CAST-SETUP + targeting fields PRESERVED (`ManaCost`/`Cooldown`/`CastTime`/`CanTargetAllies`/`CastRange`/`DamageType`/`SupportsAutoCast` unchanged), and the MECHANIC fields CLEARED (`HealAmount==0`, `TargetCount` normalized, `Radius==0`, etc.). `ConvertLegacyAbility("fireball")` returns a non-empty `warnings` slice (executor can't run projectiles → conversion degrades it) — the runnable classification drives this. `ConvertLegacyAbility` on an already-v2 ability returns an error/warning ("already composable"). The converted def must pass `validateAbilityDef`.
  - `http`: `TestConvertEndpoint` — `POST /abilities/greater_heal/convert` → 200 `{ability: <converted def>, warnings: [...], runnable: true}`; `POST /abilities/fireball/convert` → 200 with non-empty warnings + `runnable:false`; unknown id → 404.

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement.**
  - `ConvertLegacyAbility(id string) (AbilityDef, []string, error)`: look up `getAbilityDef(id)` (404 if missing); if `def.SchemaVersion >= 2` return an error/"already composable". `prog := compileLegacyAbility(def)`. Build the converted def: COPY the identity + cast-setup + targeting fields (`ID, DisplayName, Type, Category, DamageType, Tags, Icon, Description, CanTargetSelf/Allies/Enemies, TargetsPoint, CastRange, ManaCost, Cooldown, CastTime, SupportsAutoCast, AutoCastTargetSelector, DefaultAutoCast, CasterAnimation`); set `SchemaVersion=2`, `Program=prog`; leave every MECHANIC field at zero (do NOT copy `HealAmount/DamageAmount/DamagePerSecond/TargetCount/SummonUnitType/SummonCount/Radius/ImpactDelaySeconds/Burn*/Slow*/PullStrength/Duration/Chain*/Bounce*/Projectile*/EffectOnTarget/EffectAtPoint/BurnEffectAtPoint/EffectScale/Channel*/Charge*/Missile*` etc.). Warnings: if the compiled program is NOT fully executor-runnable (add a package-level `AbilityProgramRunnable(prog) bool` walking the structurally-visible tree like the Phase-4 test's `programIsExecutorRunnable`), append a warning naming the deferred mechanic (e.g. "Projectile delivery is not yet executed by the composable runtime — converting will make this ability inert in-game until a later phase."). Return `(converted, warnings, nil)`. (Convert does NOT save — the editor reviews + saves via the existing `POST /abilities`.)
  - Route: in the `/abilities/` handler in `editor_handlers.go`, add a `POST .../convert` branch (mirror the `/image` `CutSuffix` pattern): `if rest, isConvert := strings.CutSuffix(id, "/convert"); isConvert && r.Method == http.MethodPost { ... }`. Call `ConvertLegacyAbility(rest)`, 404 on not-found error, else `writeJSON(w, map[string]any{"ability": conv, "warnings": warnings, "runnable": game.AbilityProgramRunnable(conv.Program)})`.

- [ ] **Step 4: Run** all + full package + build/vet/gofmt. Green. Confirm the converted def round-trips through `SaveEditorAbility` (validates clean).

- [ ] **Step 5: Commit** — "feat(abilities): legacy→composable convert endpoint with degradation warnings".

---

### Task 4: Compiled program + runnable flag on the catalog GET

**Files:**
- Modify: `server/internal/http/router.go` (`abilityCatalogEntry` + the handler)
- Test: `server/internal/http/router_ability_catalog_test.go`

- [ ] **Step 1: Write the failing test.** httptest `GET /catalog/abilities` → for a LEGACY ability (e.g. `fireball`, Program nil), the entry has a non-null `compiledProgram` (the display flow) and `runnable:false`; for `raise_skeleton`, `runnable:true`; a `SchemaVersion>=2` ability (none shipped — skip or inject) would have its own `program` and null `compiledProgram`. Confirm `generatedDescription` still present (no regression).

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement.** Extend `abilityCatalogEntry`:
  ```go
  type abilityCatalogEntry struct {
      game.AbilityDef
      GeneratedDescription string              `json:"generatedDescription"`
      // CompiledProgram is the composable view of a LEGACY ability (nil Program) so
      // the editor can display its flow without the author converting. Null for
      // abilities that already have an authored Program. Display-only — never saved.
      CompiledProgram *game.AbilityProgram `json:"compiledProgram,omitempty"`
      // Runnable: whether the composable runtime can fully execute this ability's
      // (authored or compiled) program today — the editor labels non-runnable ones
      // "display-only".
      Runnable bool `json:"runnable"`
  }
  ```
  In the handler loop: `prog := d.Program; if prog == nil { prog = game.CompileLegacyAbilityForEditor(d) }` (add an EXPORTED wrapper `game.CompileLegacyAbilityForEditor(def) *AbilityProgram = compileLegacyAbility(def)` since `compileLegacyAbility` is unexported and `router.go` is package `http`). Set `CompiledProgram` only when `d.Program == nil`. Set `Runnable = game.AbilityProgramRunnable(prog)`.

- [ ] **Step 4: Run** + full `go test ./internal/http/` + build/vet/gofmt. Green.

- [ ] **Step 5: Commit** — "feat(abilities): expose compiled program + runnable flag on catalog GET".

---

### Task 5: TypeScript API + types

**Files:**
- Create: `client/src/game-portal/src/game/abilities/program/programSchema.ts`
- Create: `client/src/game-portal/src/game/abilities/program/programValidation.ts`
- Modify: `client/src/game-portal/src/game/abilities/abilityEditorApi.ts`
- Modify: `client/src/game-portal/src/game/abilities/abilityEditorForm.ts` (add `compiledProgram?`/`runnable?` to `AuthoredAbilityDef` + `MODELED_KEYS` — they're read-only display fields, stripped on save like `generatedDescription`)
- Test: `client/.../program/programSchema.test.ts`, `client/.../abilities/abilityEditorApi.test.ts` (extend)

- [ ] **Step 1: Write failing Vitest tests.**
  - `programSchema`: `parseActionSchemaResponse(raw)` yields `{ actions: ActionSchema[]; enums: Record<string,string[]> }` with `ActionSchema{type,fields,runnable}` and `SchemaField{key,label,control,options?,section?}`. A `schemaForAction(schemas, 'deal_damage')` lookup returns its fields.
  - `abilityEditorApi`: mock `fetch`; `fetchActionSchema()` GETs `/catalog/action-schema`; `validateAbilityProgram(def)` POSTs `/abilities/validate` returns `issues`; `convertAbility(id)` POSTs `/abilities/{id}/convert` returns `{ability,warnings,runnable}`. (Follow the existing `getJson`/`API_BASE` + test-mock pattern in the file.)

- [ ] **Step 2: Run** `npm run test -- programSchema abilityEditorApi` → FAIL.

- [ ] **Step 3: Implement.**
  - `programSchema.ts`: TS types `ActionSchema`, `SchemaField` (control union: `'number'|'text'|'boolean'|'enum'|'multiselect'|'asset'|'sentinel_number'|'duration'|'percentage'|'target_query'|'context_ref'|'animation_marker'|'nested_triggers' | (string & {})`), `ProgramEnums = Record<string,string[]>`, `ActionSchemaBundle{actions,enums}`; `parseActionSchemaResponse`, `schemaForAction(bundle, type)`.
  - `programValidation.ts`: `ValidationIssue{path,code,message,severity:'error'|'warning'}`; helpers `issuesForPath(issues, pathPrefix)` (map an issue to a flow card by path prefix), `hasBlockingError(issues)`.
  - `abilityEditorApi.ts`: `fetchActionSchema(): Promise<ActionSchemaBundle>`, `validateAbilityProgram(ability): Promise<ValidationIssue[]>`, `convertAbility(id): Promise<{ability: AuthoredAbilityDef; warnings: string[]; runnable: boolean}>`.
  - `abilityEditorForm.ts`: add `compiledProgram?: AbilityProgram` + `runnable?: boolean` to `AuthoredAbilityDef` + `MODELED_KEYS`; ensure `saveRequestFromForm` strips them (like `generatedDescription` — add to the strip list).

- [ ] **Step 4: Run** `npm run test -- programSchema abilityEditorApi abilityEditorForm` → PASS; `npx vue-tsc -b` → clean. (Ignore the known-unrelated `ListEditorPanel.test.ts` failure.)

- [ ] **Step 5: Commit** — "feat(abilities): TS action-schema/validate/convert API + types".

---

## Self-review notes

- **No live behavior change / no UI yet:** these are additive read/dry-run/convert endpoints + TS plumbing. Convert does NOT save (editor saves via existing `POST /abilities`). `SaveEditorAbility`/`validateAbilityDef` unchanged. The catalog GET gains display-only fields (stripped on save).
- **Single-source discipline preserved:** action schema comes from the registry (`ActionSchemas()`), validation from `validateAbilityProgram`, compile from `compileLegacyAbility` — no re-implementation. Enums reuse `allActionTypes` to avoid drift.
- **Honesty about runnability:** convert + catalog GET both surface `runnable` (via `AbilityProgramRunnable`) so the editor can warn that converting a projectile/chain/channel/charge/meteor ability degrades it until later phases (carry-forward #4).
- **Type consistency:** `ActionSchema`/`SchemaField`/`ValidationIssue`/`AbilityProgramRunnable`/`CompileLegacyAbilityForEditor` used consistently Go↔TS. `SchemaField.control` union mirrors the Go `SchemaField.Control` strings.
- **Deferred to 5b (UI):** the four-region flow editor, schema-driven inspector rendering, action palette, undo/redo, drag-and-drop, unsaved-changes guard, the "Convert" button + warning modal — all consume these endpoints. Phase 6 preview endpoint (`POST /abilities/preview`) is NOT in 5a.
