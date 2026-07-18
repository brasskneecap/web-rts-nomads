# Composable Abilities — Phase 2 (Core Data Model) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land the composable ability **data model** — Go types, JSON marshal with remainder round-trip, an action registry, a validation skeleton, `AbilityDef` wiring, and TS mirrors — with **zero runtime behavior change** (programs are parsed/validated but not executed yet; that is Phase 3).

**Architecture:** New `ability_program*.go` files beside `ability_defs.go`. `AbilityDef` gains `SchemaVersion int` + `Program *AbilityProgram`. Legacy abilities (no program) are untouched. The action registry is the single source for decode + validate + (later) execute/describe/schema. See the design doc: `docs/superpowers/specs/2026-07-15-composable-abilities-design.md` §2, §6, §8.

**Tech Stack:** Go (server, `encoding/json`, table tests), TypeScript (Vitest mirrors). `*Locked` convention N/A here (pure data).

**Reference conventions:**
- Run Go tests: `cd server && go test ./internal/game/ -run <Name> -count=1`
- Run all Go game tests: `cd server && go test ./internal/game/`
- Client type-check is `vue-tsc -b` (NOT `--noEmit`); client tests `cd client/src/game-portal && npm run test -- <file>`
- The user handles all git commits/staging — **do not run `git commit`**. Where a task says "Commit", stage-and-describe only if asked; otherwise treat it as a checkpoint boundary for review.

---

### Task 1: Program core types + extensible enums

**Files:**
- Create: `server/internal/game/ability_program.go`
- Test: `server/internal/game/ability_program_test.go`

- [ ] **Step 1: Write the failing test** — construct a program in memory and assert field access.

```go
package game

import "testing"

func TestAbilityProgramConstruct(t *testing.T) {
	p := AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryUnit, Relations: []TargetRelation{RelSelf, RelAlly}, Range: CastRangeMatchAttackRange},
		Triggers: []AbilityTriggerDef{{
			ID:   "t_cast",
			Type: TriggerOnCastComplete,
			Actions: []AbilityActionDef{
				{ID: "a_heal", Type: ActionRestoreHealth, Enabled: true},
			},
		}},
	}
	if p.Entry.Type != EntryUnit {
		t.Fatalf("entry type = %q", p.Entry.Type)
	}
	if got := p.Triggers[0].Actions[0].Type; got != ActionRestoreHealth {
		t.Fatalf("action type = %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails** — `cd server && go test ./internal/game/ -run TestAbilityProgramConstruct -count=1` → FAIL (undefined types).

- [ ] **Step 3: Write the types.** Create `ability_program.go` with the structs and enum consts exactly as in design §2.2–2.5: `AbilityProgram` (with unexported `Remainder map[string]json.RawMessage`), `AbilityEntryDef`, `AbilityTriggerDef`, `TriggerTiming`, `AbilityActionDef`, `TargetQueryDef`, `ContextRef`, `AbilityConditionDef`, `ZoneDef`, `StatusDef`, `ProjectileSpawnDef`, `PresentationInstanceDef`, and the string-enum types with their const sets:

```go
type AbilityEntryType string
const (
	EntrySelf        AbilityEntryType = "self"
	EntryUnit        AbilityEntryType = "unit"
	EntryGroundPoint AbilityEntryType = "ground_point"
	EntryDirection   AbilityEntryType = "direction"
	EntryNoTarget    AbilityEntryType = "no_target"
	EntryPassive     AbilityEntryType = "passive"
)

type TargetRelation string
const (
	RelSelf TargetRelation = "self"; RelAlly TargetRelation = "ally"
	RelEnemy TargetRelation = "enemy"; RelNeutral TargetRelation = "neutral"
)

type TriggerType string
const (
	TriggerOnCastStart       TriggerType = "on_cast_start"
	TriggerOnCastComplete    TriggerType = "on_cast_complete"
	TriggerOnAnimationMarker TriggerType = "on_animation_marker"
	TriggerOnProjectileImpact TriggerType = "on_projectile_impact"
	TriggerOnZoneTick        TriggerType = "on_zone_tick"
	TriggerOnZoneEnter       TriggerType = "on_zone_enter"
	TriggerOnZoneExit        TriggerType = "on_zone_exit"
	TriggerOnStatusTick      TriggerType = "on_status_tick"
	TriggerOnStatusExpire    TriggerType = "on_status_expire"
	TriggerOnTargetHit       TriggerType = "on_target_hit"
	TriggerOnDamageDealt     TriggerType = "on_damage_dealt"
	TriggerOnUnitDeath       TriggerType = "on_unit_death"
	TriggerOnActionComplete  TriggerType = "on_action_complete"
	TriggerOnChargeFull      TriggerType = "on_charge_full"
	TriggerCustom            TriggerType = "custom"
)

type ActionType string
const (
	ActionSelectTargets ActionType = "select_targets"
	ActionStoreTargets  ActionType = "store_targets"
	ActionFilterTargets ActionType = "filter_targets"
	ActionDealDamage    ActionType = "deal_damage"
	ActionRestoreHealth ActionType = "restore_health"
	ActionApplyStatus   ActionType = "apply_status"
	ActionRemoveStatus  ActionType = "remove_status"
	ActionCreateZone    ActionType = "create_zone"
	ActionLaunchProjectile ActionType = "launch_projectile"
	ActionSummonUnit    ActionType = "summon_unit"
	ActionMoveUnit      ActionType = "move_unit"
	ActionApplyForce    ActionType = "apply_force"
	ActionModifyResource ActionType = "modify_resource"
	ActionTriggerEvent  ActionType = "trigger_event"
	ActionPlayPresentation ActionType = "play_presentation"
	ActionPlaySound     ActionType = "play_sound"
	ActionChangeRenderLayer ActionType = "change_render_layer"
	ActionCameraShake   ActionType = "camera_shake"
	ActionWait          ActionType = "wait"
	ActionConditional   ActionType = "conditional"
	ActionRepeat        ActionType = "repeat"
	ActionCustom        ActionType = "custom"
)

type TargetSource string
const (
	SrcCaster TargetSource = "caster"; SrcInitialTarget TargetSource = "initial_target"
	SrcPrevActionTargets TargetSource = "previous_action_targets"; SrcCurrentEvent TargetSource = "current_event"
	SrcNamedContext TargetSource = "named_context"; SrcSourceObject TargetSource = "source_object"
	SrcAllInScene TargetSource = "all_in_scene"
)

type TargetOrigin string
const (
	OriginCaster TargetOrigin = "caster"; OriginInitialTarget TargetOrigin = "initial_target"
	OriginInitialTargetPos TargetOrigin = "initial_target_position"; OriginCastPoint TargetOrigin = "cast_point"
	OriginImpactPosition TargetOrigin = "impact_position"; OriginCurrentEventPos TargetOrigin = "current_event_position"
	OriginProjectilePos TargetOrigin = "projectile_position"; OriginZoneCenter TargetOrigin = "zone_center"
	OriginStatusOwner TargetOrigin = "status_owner"; OriginSummonedUnit TargetOrigin = "summoned_unit"
	OriginNamedContextValue TargetOrigin = "named_context_value"
)

type TargetOrdering string
const (
	OrderClosest TargetOrdering = "closest"; OrderFarthest TargetOrdering = "farthest"
	OrderLowestHealth TargetOrdering = "lowest_health"; OrderLowestHealthPct TargetOrdering = "lowest_health_percentage"
	OrderHighestHealth TargetOrdering = "highest_health"; OrderRandom TargetOrdering = "random"
	OrderUnitID TargetOrdering = "unit_id"
)
```

Struct definitions: copy verbatim from design §2.2–2.4 (use `json.RawMessage` for `AbilityActionDef.Config`; `ZoneDef/StatusDef/ProjectileSpawnDef/PresentationInstanceDef` carry `Triggers []AbilityTriggerDef`). Add `import "encoding/json"`.

- [ ] **Step 4: Run test to verify it passes** — same command → PASS.

- [ ] **Step 5: Commit** — checkpoint: "feat(abilities): composable program core types + enums".

---

### Task 2: JSON round-trip with remainder preservation

**Files:**
- Modify: `server/internal/game/ability_program.go` (add `MarshalJSON`/`UnmarshalJSON` for `AbilityProgram`)
- Test: `server/internal/game/ability_program_marshal_test.go`

- [ ] **Step 1: Write the failing test** — an unknown program-level key and an unknown action Config sub-key must survive a decode→encode round-trip.

```go
package game

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAbilityProgramRoundTripPreservesUnknownKeys(t *testing.T) {
	src := `{
		"entry": {"type":"unit","relations":["self"],"range":"match_attack_range"},
		"futureTopLevelKey": {"x": 1},
		"triggers": [{"id":"t","type":"on_cast_complete","actions":[
			{"id":"a","type":"deal_damage","enabled":true,"config":{"amount":10,"futureCfgKey":"keepme"}}
		]}]
	}`
	var p AbilityProgram
	if err := json.Unmarshal([]byte(src), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "futureTopLevelKey") {
		t.Errorf("lost unknown top-level key: %s", s)
	}
	if !strings.Contains(s, "keepme") {
		t.Errorf("lost unknown action config key: %s", s)
	}
}
```

- [ ] **Step 2: Run test to verify it fails** — `cd server && go test ./internal/game/ -run TestAbilityProgramRoundTrip -count=1` → FAIL (unknown key dropped).

- [ ] **Step 3: Implement.** Add to `ability_program.go`:

```go
// programAlias avoids infinite recursion in the custom (Un)marshalers.
type programAlias AbilityProgram

func (p *AbilityProgram) UnmarshalJSON(b []byte) error {
	var base programAlias
	if err := json.Unmarshal(b, &base); err != nil {
		return err
	}
	*p = AbilityProgram(base)
	// Capture unknown top-level keys into Remainder.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	for _, known := range programKnownKeys {
		delete(raw, known)
	}
	if len(raw) > 0 {
		p.Remainder = raw
	}
	return nil
}

func (p AbilityProgram) MarshalJSON() ([]byte, error) {
	out, err := json.Marshal(programAlias(p))
	if err != nil {
		return nil, err
	}
	if len(p.Remainder) == 0 {
		return out, nil
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(out, &merged); err != nil {
		return nil, err
	}
	for k, v := range p.Remainder {
		if _, exists := merged[k]; !exists {
			merged[k] = v
		}
	}
	return json.Marshal(merged)
}

var programKnownKeys = []string{"entry", "triggers", "namedTriggers", "presentations"}
```

Action `Config` is already `json.RawMessage`, so unknown sub-keys survive automatically (decoders in Task 3 must NOT re-marshal the config back — they read fields, the raw stays authoritative for save). Add a code comment on `AbilityActionDef.Config` stating this.

- [ ] **Step 4: Run test to verify it passes** — same command → PASS.

- [ ] **Step 5: Commit** — checkpoint: "feat(abilities): program JSON round-trip preserves unknown keys".

---

### Task 3: Action registry + first three descriptors (decode + validate + schema)

**Files:**
- Create: `server/internal/game/ability_program_registry.go`
- Test: `server/internal/game/ability_program_registry_test.go`

- [ ] **Step 1: Write the failing test.**

```go
package game

import "testing"

func TestActionRegistryHasCoreActions(t *testing.T) {
	for _, at := range []ActionType{ActionDealDamage, ActionRestoreHealth, ActionSelectTargets} {
		if _, ok := lookupActionDescriptor(at); !ok {
			t.Errorf("missing descriptor for %q", at)
		}
	}
}

func TestDealDamageDecodeAndValidate(t *testing.T) {
	d, _ := lookupActionDescriptor(ActionDealDamage)
	cfg, err := d.Decode([]byte(`{"amount":140,"type":"fire"}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if issues := d.Validate(cfg, ValidationScope{}); len(issues) != 0 {
		t.Fatalf("unexpected issues: %+v", issues)
	}
	// amount <= 0 must produce a validation issue.
	bad, _ := d.Decode([]byte(`{"amount":0,"type":"fire"}`))
	if issues := d.Validate(bad, ValidationScope{}); len(issues) == 0 {
		t.Fatalf("expected issue for zero damage")
	}
}
```

- [ ] **Step 2: Run test to verify it fails** — FAIL (undefined `lookupActionDescriptor`, `ValidationScope`, etc.).

- [ ] **Step 3: Implement.** Create `ability_program_registry.go`:

```go
package game

import "encoding/json"

// ActionConfig is the decoded, typed config for one action. Concrete per type.
type ActionConfig interface{ actionConfig() }

type ValidationScope struct {
	// Populated in Task 4 with available context keys, prior action outputs, etc.
	AvailableContext map[string]bool
	PriorOutputs     map[string]bool
}

type ActionFieldSchema struct {
	Fields []SchemaField `json:"fields"`
}
type SchemaField struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Control string `json:"control"` // number|text|boolean|enum|multiselect|asset|sentinel_number|duration|percentage|target_query|context_ref|animation_marker|nested_triggers
	Options []string `json:"options,omitempty"`
	Section string `json:"section,omitempty"` // Basic|Targeting|Timing|Properties|Presentation|Conditions|Advanced|Notes
}

type ActionDescriptor struct {
	Type     ActionType
	Decode   func(json.RawMessage) (ActionConfig, error)
	Validate func(cfg ActionConfig, scope ValidationScope) []ValidationIssue
	Schema   ActionFieldSchema
	// Execute + Describe added in Phase 3 / Phase 7.
}

var actionRegistry = map[ActionType]ActionDescriptor{}

func registerAction(d ActionDescriptor) { actionRegistry[d.Type] = d }
func lookupActionDescriptor(t ActionType) (ActionDescriptor, bool) { d, ok := actionRegistry[t]; return d, ok }

// ── deal_damage ──
type dealDamageConfig struct {
	Amount int        `json:"amount"`
	Type   DamageType `json:"type"`
	Radius float64    `json:"radius,omitempty"`
}
func (dealDamageConfig) actionConfig() {}

// ── restore_health ──
type restoreHealthConfig struct {
	Amount int        `json:"amount"`
	School DamageType `json:"school,omitempty"`
}
func (restoreHealthConfig) actionConfig() {}

// ── select_targets: config is empty; the TargetQueryDef on the action carries it ──
type selectTargetsConfig struct{}
func (selectTargetsConfig) actionConfig() {}

func init() {
	registerAction(ActionDescriptor{
		Type:   ActionDealDamage,
		Decode: func(b json.RawMessage) (ActionConfig, error) { var c dealDamageConfig; err := json.Unmarshal(b, &c); return c, err },
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(dealDamageConfig)
			var out []ValidationIssue
			if c.Amount <= 0 {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "deal_damage requires amount > 0", Severity: "error"})
			}
			if c.Type != "" && !IsValidDamageType(c.Type) {
				out = append(out, ValidationIssue{Code: "invalid_damage_type", Message: "unknown damage type " + string(c.Type), Severity: "error"})
			}
			return out
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "amount", Label: "Amount", Control: "number", Section: "Properties"},
			{Key: "type", Label: "Damage Type", Control: "enum", Section: "Properties"},
			{Key: "radius", Label: "Radius", Control: "number", Section: "Targeting"},
		}},
	})
	registerAction(ActionDescriptor{
		Type:   ActionRestoreHealth,
		Decode: func(b json.RawMessage) (ActionConfig, error) { var c restoreHealthConfig; err := json.Unmarshal(b, &c); return c, err },
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(restoreHealthConfig)
			if c.Amount <= 0 {
				return []ValidationIssue{{Code: "empty_required_property", Message: "restore_health requires amount > 0", Severity: "error"}}
			}
			return nil
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "amount", Label: "Amount", Control: "number", Section: "Properties"},
			{Key: "school", Label: "School", Control: "enum", Section: "Properties"},
		}},
	})
	registerAction(ActionDescriptor{
		Type:   ActionSelectTargets,
		Decode: func(b json.RawMessage) (ActionConfig, error) { return selectTargetsConfig{}, nil },
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue { return nil },
		Schema:   ActionFieldSchema{Fields: []SchemaField{{Key: "target", Label: "Target Query", Control: "target_query", Section: "Targeting"}}},
	})
}
```

(`ValidationIssue` is defined in Task 4; if Task 4 hasn't run yet, add the struct here temporarily and move it — but the subagent order runs Task 4 before running the full suite, so define `ValidationIssue` in Task 4 and have Task 3 depend on it. To keep Task 3 compiling standalone, define `ValidationIssue` in `ability_program_registry.go` and reference it from Task 4.)

**Decision:** define `ValidationIssue` in this file (Task 3) so the registry compiles independently; Task 4 imports it.

```go
type ValidationIssue struct {
	Path     string `json:"path"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Severity string `json:"severity"` // error | warning
}
```

- [ ] **Step 4: Run test to verify it passes** — `cd server && go test ./internal/game/ -run 'TestActionRegistry|TestDealDamage' -count=1` → PASS.

- [ ] **Step 5: Commit** — checkpoint: "feat(abilities): action registry + deal_damage/restore_health/select_targets descriptors".

---

### Task 4: Program validation skeleton

**Files:**
- Create: `server/internal/game/ability_program_validate.go`
- Test: `server/internal/game/ability_program_validate_test.go`

- [ ] **Step 1: Write the failing test.**

```go
package game

import "testing"

func hasCode(issues []ValidationIssue, code string) bool {
	for _, i := range issues { if i.Code == code { return true } }
	return false
}

func TestValidateProgramStructural(t *testing.T) {
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryUnit, Relations: []TargetRelation{RelSelf}},
		Triggers: []AbilityTriggerDef{
			{ID: "t1", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
				{ID: "a1", Type: "no_such_action", Enabled: true},
				{ID: "a1", Type: ActionDealDamage, Enabled: true, Config: []byte(`{"amount":0}`)},
			}},
		},
	}
	issues := validateAbilityProgram(prog)
	if !hasCode(issues, "unsupported_action_type") { t.Error("want unsupported_action_type") }
	if !hasCode(issues, "duplicate_id") { t.Error("want duplicate_id for repeated a1") }
	if !hasCode(issues, "empty_required_property") { t.Error("want deal_damage amount issue") }
}

func TestValidateProgramTickInterval(t *testing.T) {
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryGroundPoint},
		Triggers: []AbilityTriggerDef{{ID: "t", Type: TriggerOnZoneTick,
			Timing: &TriggerTiming{TickInterval: 0}, Actions: []AbilityActionDef{{ID: "a", Type: ActionSelectTargets, Enabled: true}}}},
	}
	if !hasCode(validateAbilityProgram(prog), "invalid_tick_interval") {
		t.Error("want invalid_tick_interval for tickInterval<=0")
	}
}
```

- [ ] **Step 2: Run test to verify it fails** — FAIL (undefined `validateAbilityProgram`).

- [ ] **Step 3: Implement `validateAbilityProgram(prog *AbilityProgram) []ValidationIssue`.** Walk triggers/actions recursively (into `Children` and object `Triggers`), collecting:
  - `duplicate_id` — any repeated trigger/action id (track a `map[string]bool` with path).
  - `unsupported_action_type` — `lookupActionDescriptor` miss (skip `ActionCustom`).
  - per-action: run the descriptor's `Decode`+`Validate`, prefix each returned issue's `Path` with `triggers[i].actions[j]`.
  - `invalid_tick_interval` — `on_zone_tick`/`on_status_tick` trigger with `Timing == nil || TickInterval <= 0`.
  - `no_behavior` (warning) — program with zero actions across all triggers.

  Build `Path` strings like `triggers[0].actions[1]`. Return a flat `[]ValidationIssue`. Keep the deeper checks from design §8 (context-reference gating, circular named triggers, marker bounds) as `// TODO(phase-2b)` stubs with a comment — the skeleton covers the structural subset the test asserts.

- [ ] **Step 4: Run test to verify it passes** — `cd server && go test ./internal/game/ -run TestValidateProgram -count=1` → PASS.

- [ ] **Step 5: Commit** — checkpoint: "feat(abilities): program validation skeleton (structural checks)".

---

### Task 5: Wire `Program` onto `AbilityDef` + branch the single validation gate

**Files:**
- Modify: `server/internal/game/ability_defs.go` (add fields to `AbilityDef`; branch `validateAbilityDef`)
- Test: `server/internal/game/ability_defs_program_test.go`

- [ ] **Step 1: Write the failing test.**

```go
package game

import (
	"encoding/json"
	"testing"
)

func TestAbilityDefCarriesProgramAndValidates(t *testing.T) {
	src := `{
		"id":"test_ability","displayName":"Test","type":"spell","schemaVersion":2,
		"program":{"entry":{"type":"unit","relations":["ally"]},
			"triggers":[{"id":"t","type":"on_cast_complete","actions":[
				{"id":"a","type":"restore_health","enabled":true,"config":{"amount":15}}]}]}
	}`
	var def AbilityDef
	if err := json.Unmarshal([]byte(src), &def); err != nil { t.Fatalf("unmarshal: %v", err) }
	if def.Program == nil { t.Fatal("program not decoded") }
	if err := validateAbilityDef(&def); err != nil { t.Fatalf("valid program rejected: %v", err) }
}

func TestAbilityDefRejectsInvalidProgram(t *testing.T) {
	def := AbilityDef{ID: "x", SchemaVersion: 2, Program: &AbilityProgram{
		Triggers: []AbilityTriggerDef{{ID: "t", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
			{ID: "a", Type: ActionDealDamage, Enabled: true, Config: json.RawMessage(`{"amount":0}`)}}}}}}
	if err := validateAbilityDef(&def); err == nil { t.Fatal("expected error for amount<=0 program") }
}

func TestLegacyAbilityUnaffected(t *testing.T) {
	def := AbilityDef{ID: "heal", HealAmount: 10, Type: AbilitySpell}
	if err := validateAbilityDef(&def); err != nil { t.Fatalf("legacy def rejected: %v", err) }
	if def.Program != nil { t.Fatal("legacy def must not gain a program") }
}
```

- [ ] **Step 2: Run test to verify it fails** — FAIL (no `SchemaVersion`/`Program` fields).

- [ ] **Step 3: Implement.** In `ability_defs.go`:
  - Add to the `AbilityDef` struct (after the charge-fire block): `SchemaVersion int` json `"schemaVersion,omitempty"` and `Program *AbilityProgram` json `"program,omitempty"`, with doc comments from design §2.1.
  - In `validateAbilityDef`, after the existing checks and before `return nil`, add:

```go
if def.Program != nil {
	if issues := validateAbilityProgram(def.Program); len(issues) > 0 {
		for _, is := range issues {
			if is.Severity == "error" {
				return fmt.Errorf("program invalid at %s: %s", is.Path, is.Message)
			}
		}
	}
}
```

  Legacy defs (`Program == nil`) skip the branch entirely — behavior identical to today.

- [ ] **Step 4: Run test to verify it passes** — `cd server && go test ./internal/game/ -run 'TestAbilityDefCarries|TestAbilityDefRejects|TestLegacyAbility' -count=1` → PASS. Then run the whole game package to confirm no regression: `cd server && go test ./internal/game/` → PASS.

- [ ] **Step 5: Commit** — checkpoint: "feat(abilities): AbilityDef.Program field wired into the shared validation gate".

---

### Task 6: TypeScript mirrors + remainder round-trip

**Files:**
- Create: `client/src/game-portal/src/game/abilities/program/abilityProgram.ts`
- Test: `client/src/game-portal/src/game/abilities/program/abilityProgram.test.ts`
- Modify: `client/src/game-portal/src/game/abilities/abilityEditorForm.ts` (add `schemaVersion?` + `program?` to `AuthoredAbilityDef` + `MODELED_KEYS`)

- [ ] **Step 1: Write the failing test.**

```ts
import { describe, it, expect } from 'vitest'
import { parseProgram, serializeProgram, type AbilityProgram } from './abilityProgram'

describe('abilityProgram round-trip', () => {
  it('preserves unknown top-level and config keys', () => {
    const raw = {
      entry: { type: 'unit', relations: ['self'], range: 'match_attack_range' },
      futureKey: { x: 1 },
      triggers: [{ id: 't', type: 'on_cast_complete', actions: [
        { id: 'a', type: 'deal_damage', enabled: true, config: { amount: 10, futureCfgKey: 'keep' } },
      ] }],
    }
    const prog: AbilityProgram = parseProgram(raw)
    const out = serializeProgram(prog) as Record<string, unknown>
    expect(JSON.stringify(out)).toContain('futureKey')
    expect(JSON.stringify(out)).toContain('keep')
  })
})
```

- [ ] **Step 2: Run test to verify it fails** — `cd client/src/game-portal && npm run test -- abilityProgram` → FAIL (module missing).

- [ ] **Step 3: Implement `abilityProgram.ts`.** Mirror the Go types as TS interfaces (`AbilityProgram`, `AbilityEntryDef`, `AbilityTriggerDef`, `AbilityActionDef`, `TargetQueryDef`, `ContextRef`, `AbilityConditionDef`, `ZoneDef`, `StatusDef`, `ProjectileSpawnDef`, `PresentationInstanceDef`) with string-literal union types matching the enum consts. Each node interface carries an index signature `[key: string]: unknown` OR an explicit `remainder` — use the simplest that preserves round-trip: `parseProgram` splits known keys from a `__remainder` bag at the program level and leaves `action.config` as an opaque `Record<string, unknown>` (never destructured on save). `serializeProgram` merges `__remainder` back. Keep `action.config` verbatim.

Then in `abilityEditorForm.ts`: add `schemaVersion?: number` and `program?: AbilityProgram` to `AuthoredAbilityDef`, add both keys to `MODELED_KEYS`, and import the type. `formFromDef`/`saveRequestFromForm` already handle modeled keys generically — verify `program` survives a form round-trip (it will, since it's in `MODELED_KEYS`).

- [ ] **Step 4: Run test to verify it passes** — `cd client/src/game-portal && npm run test -- abilityProgram` → PASS. Then type-check: `cd client/src/game-portal && npx vue-tsc -b` → clean.

- [ ] **Step 5: Commit** — checkpoint: "feat(abilities): TS program mirrors + remainder round-trip; wire into AuthoredAbilityDef".

---

### Task 7: Fixture parse tests (Meteor + Greater Heal v2 JSON)

**Files:**
- Test: `server/internal/game/ability_program_fixtures_test.go`
- Test: `client/src/game-portal/src/game/abilities/program/fixtures.test.ts`

- [ ] **Step 1: Write the failing tests.** Embed the exact v2 JSON from design §5.1 and §5.2 as string literals. Assert: (a) Go `json.Unmarshal` into `AbilityDef` succeeds, `Program != nil`, `validateAbilityDef` returns nil; (b) re-marshal contains `"burning_crater"` / `"healing_glow"` (round-trip intact); (c) TS `parseProgram(JSON.parse(fixture))` produces a program whose `triggers[0].type === 'on_cast_complete'`.

```go
func TestMeteorV2Fixture(t *testing.T) {
	var def AbilityDef
	if err := json.Unmarshal([]byte(meteorV2JSON), &def); err != nil { t.Fatalf("unmarshal: %v", err) }
	if def.Program == nil || len(def.Program.Presentations) == 0 { t.Fatal("meteor presentation missing") }
	if err := validateAbilityDef(&def); err != nil { t.Fatalf("meteor v2 invalid: %v", err) }
}
```

(Include `greaterHealV2JSON` similarly. Paste the JSON verbatim from the design doc §5.)

- [ ] **Step 2: Run to verify they fail** — before pasting valid JSON, or run against current code → they fail if any field name in the fixture doesn't decode. This is the fixture's real purpose: it locks the JSON shape to the Go/TS types.

- [ ] **Step 3: Reconcile.** If a fixture key doesn't decode onto the types, fix the **types** (not the fixture) so the design-doc JSON is authoritative, OR correct the design doc if the fixture had a typo — and note the correction in the plan. Re-run.

- [ ] **Step 4: Run to verify they pass** — both suites green.

- [ ] **Step 5: Commit** — checkpoint: "test(abilities): Meteor + Greater Heal v2 fixtures parse & validate".

---

## Self-review notes

- **Type consistency:** `ValidationIssue` is defined once (Task 3, `ability_program_registry.go`); Tasks 4–5 reference it. `lookupActionDescriptor`/`registerAction` names are stable across Tasks 3–4. `AbilityProgram.Remainder` is unexported and handled only in Task 2's (un)marshalers.
- **No behavior change:** Nothing in Phase 2 touches the tick loop, cast resolution, or the editor UI. Programs are decoded + validated only. Legacy abilities never gain a `Program`.
- **Spec coverage (Phase 2 slice of design §11.2):** Go types ✓ (T1) · registries ✓ (T3) · JSON marshal ✓ (T2) · remainder round-trip ✓ (T2, T6) · TS mirrors ✓ (T6) · validation skeleton ✓ (T4) · `AbilityDef` wiring ✓ (T5) · fixtures ✓ (T7). Executor, compiler, editor UI, preview, descriptions are Phases 3–7.
- **Deferred within Phase 2** (explicit, not silent): deep validation checks (context-reference gating, circular named-trigger detection, marker-bounds) are `TODO(phase-2b)` stubs in Task 4; the editor schema endpoint is Phase 5; action `Execute`/`Describe` are Phases 3/7.

## Carry-forwards to Phase 3 (from the final holistic review — 2026-07-15)

Phase 2 landed green with zero runtime behavior change (the program path is dormant — no catalog ability sets `schemaVersion`/`program` yet; the only wiring is the two `AbilityDef` fields + the `validateAbilityDef` branch). Two latent traps must be resolved BEFORE the executor lands in Phase 3:

1. **`knownActionTypes` drift is unguarded.** `ability_program_validate.go` hand-duplicates the 22 `ActionType` consts from `ability_program.go`. If someone adds a const but forgets the set, valid authored actions get wrongly flagged `unsupported_action_type` (error → blocks save/load). *Fix:* declare one canonical `[]ActionType` slice of all consts, derive `isKnownActionType` from it (so the map can't drift), and add a guard test asserting completeness. Do this as Phase 3 Task 0.

2. **`AbilityActionDef.Enabled bool` defaults to `false` on decode.** A hand-authored action that omits `"enabled"` is silently inert; fixtures dodge this by always setting `enabled:true`. *Fix:* normalize absent→`true` at the single loader/validate choke point (`validateAbilityProgram`'s walk, or the compile step) so hand-authored JSON isn't silently dead; the editor must also always emit `enabled:true` on action creation; the executor treats `Enabled==false` as skip. Pick the loader as the authoritative normalization site.

Minor (non-blocking): the `abilityEditorForm.ts` comment references `parseProgram`/`serializeProgram` as the round-trip mechanism, but at that layer `program` is actually preserved by verbatim pass-through (it's a modeled key); the typed parse/serialize path only engages once editor code reads the program. Tidy the comment when Phase 5 wires the editor.
