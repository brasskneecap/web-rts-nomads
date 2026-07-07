# Proc Effects System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract weapon on-hit proc effects into a standalone, catalog-driven, parameterized system (`executeProcEffectLocked`) fireable from any source — units, and later perks/abilities/traps/buildings — then migrate the three elemental swords onto it with byte-identical in-game behavior.

**Architecture:** A new `catalog/procs/` directory of named `ProcEffectDef`s; a pure `resolveProcEffectParams(def, overrides)` precedence function; a `ProcSource` value struct (IDs, not pointers) so non-unit sources work; `executeProcEffectLocked(src, target, params)` as the single RNG-free execution entry point. Equipment keeps its chance roll and resolves item→params at equip time so the per-hit path stays lookup-free.

**Tech Stack:** Go 1.22 (server module `webrts/server`, root `server/`), embedded JSON catalogs, table-driven Go tests.

**Spec:** `docs/superpowers/specs/2026-07-06-proc-effects-design.md`

## Global Constraints

- Branch: all work happens on `proc-effects` (already created).
- AI_RULES apply: store target/owner references as IDs never pointers on structs that outlive a tick; `*Unit` params are fine within a tick; `Locked` suffix means caller holds `s.mu`; no new RNG sources — `executeProcEffectLocked` must contain NO RNG.
- Determinism: the chance roll stays in `rollEquipmentProcsLocked` against `s.rngPerks`, consumed at exactly the same points as today.
- Catalog discipline: filename must match JSON `id`; mismatch panics at startup.
- In-game behavior of fire_sword / frost_sword / lightning_sword must be identical before and after.
- No client changes: `UnitEquipmentBonus`/`EquipmentProc` are not in the wire protocol (verified — no matches in `server/pkg/protocol/messages.go`).
- All test commands run from `server/`: `cd server` first (module root).
- Only `damageType` and `projectileID` are fixed per effect def; every other knob is overridable (dynamic-tunability is a design goal for future ability upgrades).
- Commit messages follow existing repo style (short imperative summary).

---

### Task 1: Proc effect catalog — `ProcEffectParams`, `ProcEffectDef`, loader, three shipped defs

**Files:**
- Create: `server/internal/game/proc_effect_defs.go`
- Create: `server/internal/game/catalog/procs/fire_bolt_ignite.json`
- Create: `server/internal/game/catalog/procs/frost_bolt_chill.json`
- Create: `server/internal/game/catalog/procs/lightning_chain.json`
- Test: `server/internal/game/proc_effect_defs_test.go`

**Interfaces:**
- Consumes: `DamageType`, `IsValidDamageType` (existing, `damage_pipeline.go`).
- Produces: `type ProcEffectParams struct` (full payload value struct), `type ProcEffectDef struct {ID string; ProcEffectParams}`, `getProcEffectDef(id string) (ProcEffectDef, bool)`, `validateProcEffectDef(def *ProcEffectDef) error`. Later tasks call `getProcEffectDef` from item validation and equip-time resolution.

- [ ] **Step 1: Write the three catalog JSON files** (extracted verbatim from the shipped swords)

`server/internal/game/catalog/procs/fire_bolt_ignite.json`:
```json
{
  "id": "fire_bolt_ignite",
  "damage": 25,
  "damageType": "fire",
  "projectileID": "fire_bolt",
  "burnDamagePerSecond": 8,
  "burnDurationSeconds": 3
}
```

`server/internal/game/catalog/procs/frost_bolt_chill.json`:
```json
{
  "id": "frost_bolt_chill",
  "damage": 25,
  "damageType": "cold",
  "projectileID": "frost_bolt",
  "projectileScale": 2,
  "slowMultiplier": 0.75,
  "slowDurationSeconds": 2
}
```

`server/internal/game/catalog/procs/lightning_chain.json`:
```json
{
  "id": "lightning_chain",
  "damage": 25,
  "damageType": "lightning",
  "projectileID": "lightning_bolt",
  "bounceCount": 2,
  "bounceRange": 200,
  "bounceDamageFalloff": 5
}
```

- [ ] **Step 2: Write the failing test**

`server/internal/game/proc_effect_defs_test.go`:
```go
package game

import "testing"

// TestProcEffectCatalog_ShippedDefsLoad guards the shipped catalog: the three
// effects extracted from the elemental swords load with their identity fields
// intact. Payload numbers are asserted as invariants (positive, in-range),
// not pinned values, so a balance tweak doesn't break the test.
func TestProcEffectCatalog_ShippedDefsLoad(t *testing.T) {
	cases := []struct {
		id             string
		wantElement    DamageType
		wantProjectile string
	}{
		{"fire_bolt_ignite", DamageFire, "fire_bolt"},
		{"frost_bolt_chill", DamageCold, "frost_bolt"},
		{"lightning_chain", DamageLightning, "lightning_bolt"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			def, ok := getProcEffectDef(tc.id)
			if !ok {
				t.Fatalf("%s not in proc catalog", tc.id)
			}
			if def.ID != tc.id {
				t.Errorf("ID = %q, want %q", def.ID, tc.id)
			}
			if def.Damage <= 0 {
				t.Errorf("Damage want > 0, got %d", def.Damage)
			}
			if def.DamageType != tc.wantElement {
				t.Errorf("DamageType = %s, want %s", def.DamageType, tc.wantElement)
			}
			if def.ProjectileID != tc.wantProjectile {
				t.Errorf("ProjectileID = %q, want %q", def.ProjectileID, tc.wantProjectile)
			}
		})
	}

	// Per-effect payload wiring (invariants, not numbers).
	fire, _ := getProcEffectDef("fire_bolt_ignite")
	if fire.BurnDamagePerSecond <= 0 || fire.BurnDurationSeconds <= 0 {
		t.Errorf("fire_bolt_ignite needs a positive burn, got %v dps / %v s", fire.BurnDamagePerSecond, fire.BurnDurationSeconds)
	}
	frost, _ := getProcEffectDef("frost_bolt_chill")
	if !(frost.SlowMultiplier > 0 && frost.SlowMultiplier < 1) || frost.SlowDurationSeconds <= 0 {
		t.Errorf("frost_bolt_chill needs a chill in (0,1) with positive duration, got %v / %v s", frost.SlowMultiplier, frost.SlowDurationSeconds)
	}
	chain, _ := getProcEffectDef("lightning_chain")
	if chain.BounceCount <= 0 || chain.BounceRange <= 0 {
		t.Errorf("lightning_chain needs a real chain, got count=%d range=%v", chain.BounceCount, chain.BounceRange)
	}
}

// TestValidateProcEffectDef exercises the load-time validation rules.
func TestValidateProcEffectDef(t *testing.T) {
	good := &ProcEffectDef{ID: "ok", ProcEffectParams: ProcEffectParams{Damage: 10, DamageType: DamageFire, ProjectileID: "fire_bolt"}}
	if err := validateProcEffectDef(good); err != nil {
		t.Fatalf("valid def rejected: %v", err)
	}
	noDamage := &ProcEffectDef{ID: "bad1", ProcEffectParams: ProcEffectParams{Damage: 0, DamageType: DamageFire, ProjectileID: "fire_bolt"}}
	if err := validateProcEffectDef(noDamage); err == nil {
		t.Error("expected error for damage <= 0, got nil")
	}
	badType := &ProcEffectDef{ID: "bad2", ProcEffectParams: ProcEffectParams{Damage: 10, DamageType: DamageType("plasma"), ProjectileID: "fire_bolt"}}
	if err := validateProcEffectDef(badType); err == nil {
		t.Error("expected error for unregistered damage type, got nil")
	}
	noProjectile := &ProcEffectDef{ID: "bad3", ProcEffectParams: ProcEffectParams{Damage: 10, DamageType: DamageFire}}
	if err := validateProcEffectDef(noProjectile); err == nil {
		t.Error("expected error for empty projectileID, got nil")
	}
	negScale := &ProcEffectDef{ID: "bad4", ProcEffectParams: ProcEffectParams{Damage: 10, DamageType: DamageFire, ProjectileID: "fire_bolt", ProjectileScale: -1}}
	if err := validateProcEffectDef(negScale); err == nil {
		t.Error("expected error for negative projectileScale, got nil")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd server && go test ./internal/game/ -run "TestProcEffectCatalog_ShippedDefsLoad|TestValidateProcEffectDef" -v`
Expected: FAIL to compile — `undefined: getProcEffectDef`, `undefined: ProcEffectDef`.

- [ ] **Step 4: Write the implementation**

`server/internal/game/proc_effect_defs.go`:
```go
package game

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
)

// Embeds the proc-effect catalog. Flat layout — proc effects carry no client
// assets of their own (visuals come from the projectile/beam def they name),
// so unlike projectiles there is no per-id directory:
//
//	catalog/procs/<id>.json — ProcEffectDef for that effect
//
// The filename (minus .json) must match the JSON's `id` field; mismatch panics
// at startup so the catalog stays coherent (same discipline as
// projectile_defs.go / unit_defs.go).
//
//go:embed catalog/procs
var procEffectDefsFS embed.FS

// ProcEffectParams is the full runtime payload of a proc effect — everything
// executeProcEffectLocked needs to fire it at a target. Authored on a
// ProcEffectDef (with per-reference overrides applied by
// resolveProcEffectParams), but a plain value struct so future systems
// (ability upgrades, traps) can construct or transform one programmatically.
type ProcEffectParams struct {
	// Damage / DamageType: the typed damage instance the effect lands.
	Damage     int        `json:"damage"`
	DamageType DamageType `json:"damageType"`
	// ProjectileID names the emitter def (catalog/projectiles) that carries
	// the effect: a projectile-kind def flies a homing bolt, a beam-kind def
	// zaps instantly with deferred damage.
	ProjectileID string `json:"projectileID"`
	// ProjectileScale is a render-size multiplier for the fired bolt's
	// sprite. 0 ⇒ fall back to the firing unit's ProjectileScale (non-unit
	// sources render at client default 1×).
	ProjectileScale float64 `json:"projectileScale,omitempty"`
	// Bounce / chain (beam emitters only): the effect arcs to up to
	// BounceCount further enemies, each hop leaping off the PREVIOUS victim
	// to the nearest not-yet-hit hostile within BounceRange, losing
	// BounceDamageFalloff damage per hop.
	BounceCount         int     `json:"bounceCount,omitempty"`
	BounceRange         float64 `json:"bounceRange,omitempty"`
	BounceDamageFalloff int     `json:"bounceDamageFalloff,omitempty"`
	// On-hit slow (chill): scales the hit unit's attack + move speed by
	// SlowMultiplier for SlowDurationSeconds via the shared slow system.
	// Zero ⇒ no slow.
	SlowMultiplier      float64 `json:"slowMultiplier,omitempty"`
	SlowDurationSeconds float64 `json:"slowDurationSeconds,omitempty"`
	// On-hit burn (fire DoT): ignites the hit unit for BurnDamagePerSecond
	// over BurnDurationSeconds via the shared burn system. Zero ⇒ no burn.
	BurnDamagePerSecond float64 `json:"burnDamagePerSecond,omitempty"`
	BurnDurationSeconds float64 `json:"burnDurationSeconds,omitempty"`
}

// ProcEffectDef is one named, reusable proc effect in catalog/procs. The ID
// is what items (and future perks/abilities/traps) reference; the embedded
// params are the effect's authored payload. DamageType and ProjectileID are
// the effect's IDENTITY — references may override the other knobs (see
// ProcEffectOverrides) but never these two.
type ProcEffectDef struct {
	ID string `json:"id"`
	ProcEffectParams
}

// procEffectDefsByID is a package-level var so Go's dependency-ordered var
// initialization guarantees it is ready before any other var initializer that
// references it — specifically loadItemCatalog, whose validation resolves
// item onHitProc.effect references via getProcEffectDef (same trick
// itemCatalogSingleton uses for the loot-table loader).
var procEffectDefsByID = loadProcEffectDefs()

func loadProcEffectDefs() map[string]ProcEffectDef {
	entries, err := fs.ReadDir(procEffectDefsFS, "catalog/procs")
	if err != nil {
		panic("catalog/procs: " + err.Error())
	}
	result := make(map[string]ProcEffectDef, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			panic("catalog/procs: unexpected entry " + entry.Name() + " — proc effects must live at catalog/procs/<id>.json")
		}
		idKey := strings.TrimSuffix(entry.Name(), ".json")
		rel := "catalog/procs/" + entry.Name()
		data, err := procEffectDefsFS.ReadFile(rel)
		if err != nil {
			panic(rel + ": " + err.Error())
		}
		var def ProcEffectDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic(rel + ": " + err.Error())
		}
		if def.ID == "" {
			panic(rel + `: missing "id" field`)
		}
		if def.ID != idKey {
			panic(rel + ": def.ID " + def.ID + " does not match filename " + idKey)
		}
		if err := validateProcEffectDef(&def); err != nil {
			panic(rel + ": " + err.Error())
		}
		if _, dup := result[def.ID]; dup {
			panic(rel + `: duplicate proc effect id "` + def.ID + `"`)
		}
		result[def.ID] = def
	}
	return result
}

// validateProcEffectDef checks a proc effect's authored payload. An effect
// with no damage, an unregistered element, or no emitter is a content
// authoring error caught at startup.
func validateProcEffectDef(def *ProcEffectDef) error {
	if def.Damage <= 0 {
		return fmt.Errorf("proc effect %q damage %d must be > 0", def.ID, def.Damage)
	}
	if !IsValidDamageType(def.DamageType) {
		return fmt.Errorf("proc effect %q damageType: unregistered damage type %q", def.ID, def.DamageType)
	}
	if def.ProjectileID == "" {
		return fmt.Errorf("proc effect %q projectileID is required (names the emitter def)", def.ID)
	}
	if def.ProjectileScale < 0 {
		return fmt.Errorf("proc effect %q projectileScale %v must be >= 0 (0/omitted ⇒ fall back to the firing unit's scale)", def.ID, def.ProjectileScale)
	}
	return nil
}

// getProcEffectDef looks up a proc effect definition by id. The bool is false
// when no effect with that id is registered — callers must handle it (same
// contract as getProjectileDef / getItemDef).
func getProcEffectDef(id string) (ProcEffectDef, bool) {
	def, ok := procEffectDefsByID[id]
	return def, ok
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd server && go test ./internal/game/ -run "TestProcEffectCatalog_ShippedDefsLoad|TestValidateProcEffectDef" -v`
Expected: PASS (both tests, all subtests).

- [ ] **Step 6: Run the full game package to catch startup-panic regressions**

Run: `cd server && go test ./internal/game/`
Expected: PASS (the new embed/loader must not panic any existing test's `NewGameStateWithSeed`).

- [ ] **Step 7: Commit**

```bash
git add server/internal/game/proc_effect_defs.go server/internal/game/proc_effect_defs_test.go server/internal/game/catalog/procs/
git commit -m "Add proc effect catalog: ProcEffectDef loader + three shipped effect defs"
```

---

### Task 2: `ProcEffectOverrides` + `resolveProcEffectParams`

**Files:**
- Create: `server/internal/game/proc_effects.go`
- Test: `server/internal/game/proc_effects_test.go`

**Interfaces:**
- Consumes: `ProcEffectDef`, `ProcEffectParams` (Task 1).
- Produces: `type ProcEffectOverrides struct` (JSON-tagged override bag, embedded by `ItemOnHitProc` in Task 5 and by future perk/ability/trap triggers), `resolveProcEffectParams(def ProcEffectDef, o ProcEffectOverrides) ProcEffectParams` (the single precedence implementation for ALL consumers).

- [ ] **Step 1: Write the failing test**

`server/internal/game/proc_effects_test.go`:
```go
package game

import "testing"

// TestResolveProcEffectParams_OverridePrecedence: a non-zero override field
// replaces the def's value; a zero field keeps the def's. Covers every
// overridable knob so a new field can't silently skip precedence.
func TestResolveProcEffectParams_OverridePrecedence(t *testing.T) {
	def := ProcEffectDef{
		ID: "test_effect",
		ProcEffectParams: ProcEffectParams{
			Damage: 25, DamageType: DamageLightning, ProjectileID: "lightning_bolt",
			ProjectileScale: 1, BounceCount: 2, BounceRange: 200, BounceDamageFalloff: 5,
			SlowMultiplier: 0.75, SlowDurationSeconds: 2,
			BurnDamagePerSecond: 8, BurnDurationSeconds: 3,
		},
	}

	// Zero overrides ⇒ params identical to the def's payload.
	if got := resolveProcEffectParams(def, ProcEffectOverrides{}); got != def.ProcEffectParams {
		t.Errorf("zero overrides must return the def's params verbatim:\n got %+v\nwant %+v", got, def.ProcEffectParams)
	}

	// Every knob overridden ⇒ every knob replaced; identity fields untouched.
	o := ProcEffectOverrides{
		Damage: 40, ProjectileScale: 3, BounceCount: 4, BounceRange: 300, BounceDamageFalloff: 10,
		SlowMultiplier: 0.5, SlowDurationSeconds: 4, BurnDamagePerSecond: 12, BurnDurationSeconds: 6,
	}
	got := resolveProcEffectParams(def, o)
	want := ProcEffectParams{
		Damage: 40, DamageType: DamageLightning, ProjectileID: "lightning_bolt",
		ProjectileScale: 3, BounceCount: 4, BounceRange: 300, BounceDamageFalloff: 10,
		SlowMultiplier: 0.5, SlowDurationSeconds: 4, BurnDamagePerSecond: 12, BurnDurationSeconds: 6,
	}
	if got != want {
		t.Errorf("full overrides:\n got %+v\nwant %+v", got, want)
	}

	// Partial override: one field replaced, the rest keep the def's values.
	partial := resolveProcEffectParams(def, ProcEffectOverrides{BounceCount: 4})
	if partial.BounceCount != 4 {
		t.Errorf("BounceCount override lost: got %d, want 4", partial.BounceCount)
	}
	if partial.Damage != 25 || partial.BounceRange != 200 || partial.SlowMultiplier != 0.75 {
		t.Errorf("non-overridden fields must keep def values, got %+v", partial)
	}
	// Identity fields can never change through overrides.
	if partial.DamageType != DamageLightning || partial.ProjectileID != "lightning_bolt" {
		t.Errorf("identity fields mutated: %+v", partial)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/game/ -run TestResolveProcEffectParams_OverridePrecedence -v`
Expected: FAIL to compile — `undefined: resolveProcEffectParams`, `undefined: ProcEffectOverrides`.

- [ ] **Step 3: Write the implementation**

`server/internal/game/proc_effects.go`:
```go
package game

// ProcEffectOverrides is the shared override bag every trigger that
// references a proc effect embeds (ItemOnHitProc today; perk/ability/trap
// triggers later), so all consumers share one override vocabulary and one
// precedence implementation. Non-zero fields replace the referenced def's
// value; zero means "use the def's". DamageType and ProjectileID are
// deliberately NOT here — they are the effect's identity (element, visuals,
// CC payload); a different element is a different effect def.
//
// Known limitation of zero-means-inherit: an override cannot disable a def's
// non-zero field (e.g. bounce a chaining effect down to 0 hops). Author a
// separate def for that instead of a sentinel.
type ProcEffectOverrides struct {
	Damage              int     `json:"damage,omitempty"`
	ProjectileScale     float64 `json:"projectileScale,omitempty"`
	BounceCount         int     `json:"bounceCount,omitempty"`
	BounceRange         float64 `json:"bounceRange,omitempty"`
	BounceDamageFalloff int     `json:"bounceDamageFalloff,omitempty"`
	SlowMultiplier      float64 `json:"slowMultiplier,omitempty"`
	SlowDurationSeconds float64 `json:"slowDurationSeconds,omitempty"`
	BurnDamagePerSecond float64 `json:"burnDamagePerSecond,omitempty"`
	BurnDurationSeconds float64 `json:"burnDurationSeconds,omitempty"`
}

// resolveProcEffectParams applies o's non-zero fields onto a copy of def's
// params. This is the SINGLE precedence implementation for all consumers —
// future systems (ability upgrades) call this rather than reimplementing
// override rules.
func resolveProcEffectParams(def ProcEffectDef, o ProcEffectOverrides) ProcEffectParams {
	p := def.ProcEffectParams
	if o.Damage > 0 {
		p.Damage = o.Damage
	}
	if o.ProjectileScale > 0 {
		p.ProjectileScale = o.ProjectileScale
	}
	if o.BounceCount > 0 {
		p.BounceCount = o.BounceCount
	}
	if o.BounceRange > 0 {
		p.BounceRange = o.BounceRange
	}
	if o.BounceDamageFalloff > 0 {
		p.BounceDamageFalloff = o.BounceDamageFalloff
	}
	if o.SlowMultiplier > 0 {
		p.SlowMultiplier = o.SlowMultiplier
	}
	if o.SlowDurationSeconds > 0 {
		p.SlowDurationSeconds = o.SlowDurationSeconds
	}
	if o.BurnDamagePerSecond > 0 {
		p.BurnDamagePerSecond = o.BurnDamagePerSecond
	}
	if o.BurnDurationSeconds > 0 {
		p.BurnDurationSeconds = o.BurnDurationSeconds
	}
	return p
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./internal/game/ -run TestResolveProcEffectParams_OverridePrecedence -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/game/proc_effects.go server/internal/game/proc_effects_test.go
git commit -m "Add ProcEffectOverrides + resolveProcEffectParams precedence helper"
```

---

### Task 3: `ProcSource` + `executeProcEffectLocked` + source-agnostic fire/spawn refactor

**Files:**
- Modify: `server/internal/game/proc_effects.go` (add ProcSource + execute entry point)
- Modify: `server/internal/game/projectile.go:216-335` (`fireOnHitProcProjectileLocked` → `fireProcProjectileLocked`, `fireOnHitProcBeamLocked` → `fireProcBeamLocked`)
- Modify: `server/internal/game/beam.go:141-158` (`spawnMomentaryDamageBeamLocked` takes ProcSource + explicit origin)
- Modify: `server/internal/game/perks_siphoner.go:634,657` (`nearestChainBounceTargetLocked` takes owner ID string, not `*Unit`)
- Modify: `server/internal/game/state_combat.go:458-483` (`rollEquipmentProcsLocked` routes through `executeProcEffectLocked` via a temporary params shim)
- Test: `server/internal/game/proc_effects_test.go` (add non-unit-source tests)

**Interfaces:**
- Consumes: `ProcEffectParams` (Task 1), `getProjectileDef`, `spawnMomentaryBeamLocked` (unchanged), `playersAreHostileLocked`, `applyBeamPendingDamageLocked`/`tickBeamsLocked` (unchanged), `landProjectileLocked` (unchanged).
- Produces: `type ProcSource struct {OwnerUnitID int; OwnerPlayerID string; OriginX, OriginY float64}`, `procSourceFromUnit(u *Unit) ProcSource`, `(s *GameState) executeProcEffectLocked(src ProcSource, target *Unit, p ProcEffectParams)`. Task 4 calls `executeProcEffectLocked` from the final `rollEquipmentProcsLocked`.

- [ ] **Step 1: Write the failing tests** (append to `server/internal/game/proc_effects_test.go`)

```go
// TestExecuteProcEffect_NonUnitSource_Projectile: a proc effect fired from a
// sourceless origin (OwnerUnitID == 0 — a future trap/building) spawns a bolt
// from the source coordinates, lands its damage, and never panics — even when
// the hit kills the target (no kill credit / XP to award).
func TestExecuteProcEffect_NonUnitSource_Projectile(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xA110)
	s.mu.Lock()
	defer s.mu.Unlock()

	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 20, MaxHP: 20, X: 50, Y: 60}
	s.nextUnitID++
	s.addUnitLocked(target)

	src := ProcSource{OwnerUnitID: 0, OwnerPlayerID: "p1", OriginX: 10, OriginY: 20}
	p := ProcEffectParams{Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}
	s.executeProcEffectLocked(src, target, p)

	if len(s.Projectiles) != 1 {
		t.Fatalf("expected 1 projectile, got %d", len(s.Projectiles))
	}
	proj := s.Projectiles[0]
	if proj.OwnerUnitID != 0 || proj.OwnerPlayerID != "p1" {
		t.Errorf("owner fields: unit=%d player=%q, want 0 / p1", proj.OwnerUnitID, proj.OwnerPlayerID)
	}
	if proj.OriginX != 10 || proj.OriginY != 20 {
		t.Errorf("origin = (%v,%v), want (10,20) — the source coords, not a unit", proj.OriginX, proj.OriginY)
	}
	if !proj.SkipOnHitEffects {
		t.Error("proc bolt must skip the on-hit hub")
	}

	// Landing a killing blow with no owner unit must not panic and must apply.
	dead := []int{}
	s.landProjectileLocked(proj, target, &dead)
	if target.HP != 0 {
		t.Errorf("target HP = %d, want 0 (25 damage vs 20 HP)", target.HP)
	}
	if len(dead) != 1 || dead[0] != target.ID {
		t.Errorf("dead list = %v, want [%d]", dead, target.ID)
	}
}

// TestExecuteProcEffect_NonUnitSource_BeamChain: a beam-kind effect from a
// non-unit source zaps from the source coords, defers its damage, and chains
// using the source's OWNER PLAYER for hostility. No unit anywhere on the
// firing side.
func TestExecuteProcEffect_NonUnitSource_BeamChain(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xA111)
	s.mu.Lock()
	defer s.mu.Unlock()

	t0 := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 500, MaxHP: 500, X: 100, Y: 0}
	s.nextUnitID++
	s.addUnitLocked(t0)
	t1 := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 500, MaxHP: 500, X: 150, Y: 0}
	s.nextUnitID++
	s.addUnitLocked(t1)

	src := ProcSource{OwnerUnitID: 0, OwnerPlayerID: "p1", OriginX: 0, OriginY: 0}
	p := ProcEffectParams{
		Damage: 25, DamageType: DamageLightning, ProjectileID: "lightning_bolt",
		BounceCount: 1, BounceRange: 200, BounceDamageFalloff: 5,
	}
	s.executeProcEffectLocked(src, t0, p)

	// Primary + one bounce = two momentary beams; primary leaves the source
	// coords with no caster unit.
	if len(s.Beams) != 2 {
		t.Fatalf("expected 2 beams (primary + bounce), got %d", len(s.Beams))
	}
	primary := s.Beams[0]
	if primary.CasterUnitID != 0 || primary.AttackerUnitID != 0 {
		t.Errorf("non-unit primary beam caster/attacker = %d/%d, want 0/0", primary.CasterUnitID, primary.AttackerUnitID)
	}
	if primary.OriginX != 0 || primary.OriginY != 0 {
		t.Errorf("primary origin = (%v,%v), want source coords (0,0)", primary.OriginX, primary.OriginY)
	}
	bounce := s.Beams[1]
	if bounce.TargetUnitID != t1.ID {
		t.Errorf("bounce target = %d, want %d", bounce.TargetUnitID, t1.ID)
	}
	if bounce.CasterUnitID != t0.ID {
		t.Errorf("bounce visually leaves the previous victim: caster = %d, want %d", bounce.CasterUnitID, t0.ID)
	}

	// Deferred damage lands without an owner unit and without panicking.
	hp0, hp1 := t0.HP, t1.HP
	s.tickBeamsLocked(beamProcDamageDelaySeconds + 0.01)
	if t0.HP != hp0-25 {
		t.Errorf("primary damage: HP %d→%d, want -25", hp0, t0.HP)
	}
	if t1.HP != hp1-20 {
		t.Errorf("bounce damage (falloff 5): HP %d→%d, want -20", hp1, t1.HP)
	}
}

// TestExecuteProcEffect_UnitSourceParity: procSourceFromUnit + execute spawns
// a projectile identical (owner, origin, scale fallback) to what the old
// equipment path produced — the migration must be behavior-preserving.
func TestExecuteProcEffect_UnitSourceParity(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xA112)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 5, Y: 7})
	attacker.ProjectileScale = 1.5
	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 500, MaxHP: 500, X: 50, Y: 0}
	s.nextUnitID++
	s.addUnitLocked(target)

	// Params with no scale of their own inherit the firing UNIT's scale.
	p := ProcEffectParams{Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}
	s.executeProcEffectLocked(procSourceFromUnit(attacker), target, p)
	if len(s.Projectiles) != 1 {
		t.Fatalf("expected 1 projectile, got %d", len(s.Projectiles))
	}
	proj := s.Projectiles[0]
	if proj.OwnerUnitID != attacker.ID || proj.Scale != 1.5 {
		t.Errorf("owner=%d scale=%v, want %d / 1.5 (unit-scale fallback)", proj.OwnerUnitID, proj.Scale, attacker.ID)
	}
}
```

Note: this test file already imports `testing`; add `"webrts/server/pkg/protocol"` to its imports for `protocol.Vec2`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd server && go test ./internal/game/ -run "TestExecuteProcEffect" -v`
Expected: FAIL to compile — `undefined: ProcSource`, `undefined: executeProcEffectLocked` (method), `undefined: procSourceFromUnit`.

- [ ] **Step 3: Add ProcSource + executeProcEffectLocked to `proc_effects.go`**

Append:
```go
// ProcSource identifies who/where a proc effect fires from. IDs, never
// pointers (AI_RULES §Target References). Non-unit sources — traps,
// buildings, world events — leave OwnerUnitID 0: no kill credit or XP is
// attributed and the effect originates at OriginX/Y.
type ProcSource struct {
	OwnerUnitID   int
	OwnerPlayerID string
	OriginX       float64
	OriginY       float64
}

// procSourceFromUnit is the common-case constructor: the effect fires from
// the unit's current position with kill credit to that unit.
func procSourceFromUnit(u *Unit) ProcSource {
	return ProcSource{OwnerUnitID: u.ID, OwnerPlayerID: u.OwnerID, OriginX: u.X, OriginY: u.Y}
}

// executeProcEffectLocked fires one proc effect from src at target. Routes by
// the emitted effect's declared kind: a beam-kind def zaps the target
// instantly (damage deferred a beat so it pops as its own number), a
// projectile-kind def (the default, incl. unknown ids) fires a flying bolt
// that lands later. Contains NO RNG — whether an effect fires is the
// trigger's business (equipment rolls its chance against rngPerks; an ability
// or trap calls this directly). Caller holds s.mu write lock.
func (s *GameState) executeProcEffectLocked(src ProcSource, target *Unit, p ProcEffectParams) {
	if target == nil || p.Damage <= 0 {
		return
	}
	if def, ok := getProjectileDef(p.ProjectileID); ok && def.IsBeam() {
		s.fireProcBeamLocked(src, target, p, def)
	} else {
		s.fireProcProjectileLocked(src, target, p)
	}
}
```

- [ ] **Step 4: Refactor the projectile fire path** (`server/internal/game/projectile.go:216-273`)

Replace `fireOnHitProcProjectileLocked` entirely with:
```go
// fireProcProjectileLocked spawns a homing projectile for a proc effect fired
// from src. It carries the effect's own Damage/DamageType (not a unit's
// attack type) and sets SkipOnHitEffects so landing applies damage directly
// without re-entering the on-hit hub. Non-unit sources (src.OwnerUnitID == 0)
// launch from src.OriginX/Y with no kill credit. Must be called under s.mu.
func (s *GameState) fireProcProjectileLocked(src ProcSource, target *Unit, p ProcEffectParams) {
	speed := defaultProjectileSpeed
	var followEffect, impactEffect string
	if def, ok := getProjectileDef(p.ProjectileID); ok {
		speed = def.Speed
		followEffect = followEffectForProjectileDef(def)
		impactEffect = impactEffectForProjectileDef(def)
	}

	dx := target.X - src.OriginX
	dy := target.Y - src.OriginY
	travelTime := math.Sqrt(dx*dx+dy*dy) / speed
	if travelTime < minProjectileFlightSeconds {
		travelTime = minProjectileFlightSeconds
	}

	id := fmt.Sprintf("proj_%d", s.nextProjectileID)
	s.nextProjectileID++

	// Params-authored scale wins when set; otherwise inherit the firing
	// unit's scale (the prior behavior — resolved at point of use, within
	// this tick). Both are "0 ⇒ client default 1×". The variant falls back to
	// the firing unit's type only for hand-built params with no ProjectileID;
	// catalog-loaded effects always name one (validated at load).
	variant := p.ProjectileID
	scale := p.ProjectileScale
	if scale <= 0 || variant == "" {
		if owner := s.getUnitByIDLocked(src.OwnerUnitID); owner != nil {
			if scale <= 0 {
				scale = owner.ProjectileScale
			}
			if variant == "" {
				variant = owner.UnitType
			}
		}
	}
	s.Projectiles = append(s.Projectiles, &Projectile{
		ID:                  id,
		OwnerUnitID:         src.OwnerUnitID,
		OwnerPlayerID:       src.OwnerPlayerID,
		TargetUnitID:        target.ID,
		OriginX:             src.OriginX,
		OriginY:             src.OriginY,
		TargetX:             target.X,
		TargetY:             target.Y,
		TotalSeconds:        travelTime,
		RemainingSeconds:    travelTime,
		Damage:              p.Damage,
		Variant:             variant,
		FollowEffect:        followEffect,
		ImpactEffect:        impactEffect,
		DamageType:          p.DamageType,
		Scale:               scale,
		SkipOnHitEffects:    true,
		SlowMultiplier:      p.SlowMultiplier,
		SlowDurationSeconds: p.SlowDurationSeconds,
		BurnDamagePerSecond: p.BurnDamagePerSecond,
		BurnDurationSeconds: p.BurnDurationSeconds,
	})
}
```

- [ ] **Step 5: Refactor the beam fire path** (`server/internal/game/projectile.go:275-335`)

Replace `fireOnHitProcBeamLocked` entirely with:
```go
// fireProcBeamLocked handles a proc effect whose emitter def is
// EmitterKindBeam (e.g. lightning_chain's "lightning_bolt"). It spawns the
// momentary beam flash NOW (frozen endpoints let it render even if the target
// later dies) but DEFERS the damage by beamProcDamageDelaySeconds — a beam is
// otherwise instantaneous, so applying damage this tick would merge its
// number into the triggering hit's number. tickBeamsLocked lands the damage a
// beat later, bypassing the on-hit hub, so a proc can't trigger another proc.
//
// Non-unit sources: the primary flash leaves src.OriginX/Y with no caster
// unit; hostility for chain hops keys off src.OwnerPlayerID.
//
// Caller holds s.mu write lock.
func (s *GameState) fireProcBeamLocked(src ProcSource, target *Unit, p ProcEffectParams, def ProjectileDef) {
	variant := p.ProjectileID
	if variant == "" {
		variant = def.ID
	}
	impact := impactEffectForProjectileDef(def)

	// Primary hit: source → target. Damage is deferred (see the helper) so it
	// pops as its own number instead of merging into the triggering attack.
	primary := s.spawnMomentaryDamageBeamLocked(src, src.OwnerUnitID, src.OriginX, src.OriginY, target, variant, p.Damage, p.DamageType, impact, def.DurationMs, beamProcDamageDelaySeconds)
	primary.SlowMultiplier = p.SlowMultiplier
	primary.SlowDurationSeconds = p.SlowDurationSeconds
	primary.BurnDamagePerSecond = p.BurnDamagePerSecond
	primary.BurnDurationSeconds = p.BurnDurationSeconds

	// Optional chain: the bolt arcs to up to BounceCount further enemies.
	// Each hop leaps off the PREVIOUS victim to the nearest not-yet-hit
	// hostile within BounceRange, losing BounceDamageFalloff damage per hop
	// (25 → 20 → 15 with count=2, falloff=5). Kill credit always stays with
	// the source. Reuses the generic bounce picker shared with chain_siphon.
	if p.BounceCount <= 0 || p.BounceRange <= 0 {
		return
	}
	rangeSq := p.BounceRange * p.BounceRange
	// Exclude the primary target and the source unit from every hop so the
	// chain can't oscillate back onto an already-hit unit or the wielder.
	// A non-unit source (OwnerUnitID 0) matches no unit, so nothing extra is
	// excluded for it.
	excluded := make(map[int]struct{}, p.BounceCount+2)
	excluded[target.ID] = struct{}{}
	if src.OwnerUnitID != 0 {
		excluded[src.OwnerUnitID] = struct{}{}
	}
	cursor := target
	for hop := 1; hop <= p.BounceCount; hop++ {
		next := s.nearestChainBounceTargetLocked(src.OwnerPlayerID, cursor, rangeSq, excluded)
		if next == nil {
			break // chain fizzles: nothing eligible within range of the last victim
		}
		dmg := p.Damage - p.BounceDamageFalloff*hop
		if dmg <= 0 {
			break // fully attenuated — stop arcing
		}
		// Beam leaves the previous victim (cursor) but the hit still credits
		// the original source. The chill/burn rides each hop too.
		bounce := s.spawnMomentaryDamageBeamLocked(src, cursor.ID, cursor.X, cursor.Y, next, variant, dmg, p.DamageType, impact, def.DurationMs, beamProcDamageDelaySeconds)
		bounce.SlowMultiplier = p.SlowMultiplier
		bounce.SlowDurationSeconds = p.SlowDurationSeconds
		bounce.BurnDamagePerSecond = p.BurnDamagePerSecond
		bounce.BurnDurationSeconds = p.BurnDurationSeconds
		excluded[next.ID] = struct{}{}
		cursor = next
	}
}
```

- [ ] **Step 6: Refactor `spawnMomentaryDamageBeamLocked`** (`server/internal/game/beam.go:141-158`)

Replace with (note: `spawnMomentaryBeamLocked` at beam.go:118 stays UNCHANGED — tests call it directly for pure visual flashes):
```go
// spawnMomentaryDamageBeamLocked spawns a one-shot beam flash from a frozen
// origin point to `to` and schedules `damage` (typed) to land on `to` after
// `delaySec`, credited to src.OwnerUnitID. fromUnitID is the VISUAL origin
// unit (drives the client's origin-lift sprite lookup; 0 when the beam leaves
// a non-unit source) and fromX/Y freeze the beam's start — the visual origin
// and the kill credit differ on a bounce hop, where the beam leaps off a
// victim but the original source still gets the kill.
//
// Caller holds s.mu write lock.
func (s *GameState) spawnMomentaryDamageBeamLocked(src ProcSource, fromUnitID int, fromX, fromY float64, to *Unit, variant string, damage int, dmgType DamageType, impactEffect string, durationMs int, delaySec float64) *Beam {
	if durationMs <= 0 {
		durationMs = defaultBeamDurationMs
	}
	b := &Beam{
		ID:                   fmt.Sprintf("beam-%d", s.nextBeamID),
		CasterUnitID:         fromUnitID,
		AttackerUnitID:       src.OwnerUnitID,
		TargetUnitID:         to.ID,
		OwnerPlayerID:        src.OwnerPlayerID,
		Variant:              variant,
		Momentary:            true,
		RemainingSeconds:     float64(durationMs) / 1000.0,
		OriginX:              fromX,
		OriginY:              fromY,
		TargetX:              to.X,
		TargetY:              to.Y,
		PendingDamage:        damage,
		DamageType:           dmgType,
		DamageDelayRemaining: delaySec,
		ImpactEffect:         impactEffect,
	}
	s.nextBeamID++
	s.Beams = append(s.Beams, b)
	return b
}
```

- [ ] **Step 7: Refactor the bounce picker to take an owner ID** (`server/internal/game/perks_siphoner.go:648-682`)

Change the signature and hostility check (body otherwise unchanged):
```go
// nearestChainBounceTargetLocked returns the nearest hostile (to casterOwnerID)
// ...existing comment, with `caster` wording updated to the owner id...
func (s *GameState) nearestChainBounceTargetLocked(casterOwnerID string, from *Unit, rangeSq float64, excluded map[int]struct{}) *Unit {
	...
	if !s.playersAreHostileLocked(casterOwnerID, u.OwnerID) {
		continue
	}
	...
}
```
Update the chain_siphon call site at `perks_siphoner.go:634`:
```go
next := s.nearestChainBounceTargetLocked(caster.OwnerID, cursor, rangeSq, excluded)
```

- [ ] **Step 8: Shim `rollEquipmentProcsLocked`** (`server/internal/game/state_combat.go:462-483`)

Replace the roll body so all firing routes through the new entry point (the inline params literal is TEMPORARY — Task 4 replaces `EquipmentProc` with a resolved `Params` field and deletes it):
```go
func (s *GameState) rollEquipmentProcsLocked(attacker, target *Unit) {
	if attacker == nil || target == nil || len(attacker.EquipmentBonus.OnHitProcs) == 0 {
		return
	}
	for _, proc := range attacker.EquipmentBonus.OnHitProcs {
		if proc.Chance <= 0 || proc.Damage <= 0 {
			continue
		}
		if s.rngPerks.Float64() < proc.Chance {
			// TEMPORARY shim (removed when EquipmentProc carries resolved
			// ProcEffectParams): copy the legacy flat fields into params.
			s.executeProcEffectLocked(procSourceFromUnit(attacker), target, ProcEffectParams{
				Damage:              proc.Damage,
				DamageType:          proc.DamageType,
				ProjectileID:        proc.ProjectileID,
				ProjectileScale:     proc.ProjectileScale,
				BounceCount:         proc.BounceCount,
				BounceRange:         proc.BounceRange,
				BounceDamageFalloff: proc.BounceDamageFalloff,
				SlowMultiplier:      proc.SlowMultiplier,
				SlowDurationSeconds: proc.SlowDurationSeconds,
				BurnDamagePerSecond: proc.BurnDamagePerSecond,
				BurnDurationSeconds: proc.BurnDurationSeconds,
			})
		}
	}
}
```
Keep the function's existing doc comment; append one sentence: "Firing routes through executeProcEffectLocked — this function owns ONLY the chance roll."

- [ ] **Step 9: Run the new tests, then the whole package**

Run: `cd server && go test ./internal/game/ -run "TestExecuteProcEffect" -v`
Expected: PASS (3 tests).
Run: `cd server && go test ./internal/game/`
Expected: PASS — every existing proc/beam/burn/slow test must still pass unchanged; the RNG consumption pattern is identical (one `rngPerks.Float64()` per proc entry per hit).

- [ ] **Step 10: Commit**

```bash
git add server/internal/game/proc_effects.go server/internal/game/proc_effects_test.go server/internal/game/projectile.go server/internal/game/beam.go server/internal/game/perks_siphoner.go server/internal/game/state_combat.go
git commit -m "Add ProcSource + executeProcEffectLocked; make proc firing source-agnostic"
```

---

### Task 4: `EquipmentProc` carries resolved `ProcEffectParams`

**Files:**
- Modify: `server/internal/game/state_items.go:25-70` (EquipmentProc struct) and `:270-285` (recompute copy)
- Modify: `server/internal/game/state_combat.go:462-483` (drop the Task-3 shim)
- Modify (mechanical test migration): `server/internal/game/equipment_onhit_proc_test.go`, `server/internal/game/burn_proc_test.go`, `server/internal/game/proc_slow_test.go`, `server/internal/game/beam_proc_test.go`

**Interfaces:**
- Consumes: `ProcEffectParams` (Task 1), `executeProcEffectLocked` (Task 3).
- Produces: `type EquipmentProc struct {Chance float64; Params ProcEffectParams}`. Task 5's recompute keeps this exact shape (it only changes how Params is derived).

- [ ] **Step 1: Restructure `EquipmentProc`** (`server/internal/game/state_items.go:25-52`)

Replace the struct and its comment with:
```go
// EquipmentProc is one equipped on-hit proc, resolved at equip time so the
// per-hit path never re-reads catalogs: Chance is the trigger's roll (against
// the seeded perk RNG) and Params is the fully-resolved effect payload.
type EquipmentProc struct {
	Chance float64
	Params ProcEffectParams
}
```

- [ ] **Step 2: Update the equip-time copy** (`server/internal/game/state_items.go:270-285`, inside `recomputeUnitEquipmentBonusLocked`)

Replace the `if p := def.OnHitProc; p != nil {...}` block with (still reading the LEGACY flat item schema — Task 5 swaps this to catalog resolution):
```go
		if p := def.OnHitProc; p != nil {
			unit.EquipmentBonus.OnHitProcs = append(unit.EquipmentBonus.OnHitProcs, EquipmentProc{
				Chance: p.Chance,
				Params: ProcEffectParams{
					Damage:              p.Damage,
					DamageType:          p.DamageType.OrPhysical(),
					ProjectileID:        p.ProjectileID,
					ProjectileScale:     p.ProjectileScale,
					BounceCount:         p.BounceCount,
					BounceRange:         p.BounceRange,
					BounceDamageFalloff: p.BounceDamageFalloff,
					SlowMultiplier:      p.SlowMultiplier,
					SlowDurationSeconds: p.SlowDurationSeconds,
					BurnDamagePerSecond: p.BurnDamagePerSecond,
					BurnDurationSeconds: p.BurnDurationSeconds,
				},
			})
		}
```

- [ ] **Step 3: Drop the shim in `rollEquipmentProcsLocked`** (`server/internal/game/state_combat.go`)

The loop body becomes:
```go
	for _, proc := range attacker.EquipmentBonus.OnHitProcs {
		if proc.Chance <= 0 || proc.Params.Damage <= 0 {
			continue
		}
		if s.rngPerks.Float64() < proc.Chance {
			s.executeProcEffectLocked(procSourceFromUnit(attacker), target, proc.Params)
		}
	}
```
Delete the "TEMPORARY shim" comment.

- [ ] **Step 4: Migrate every test that hand-builds `EquipmentProc`**

Find them all: `cd server && grep -rn "EquipmentProc{" internal/game/*_test.go`
Expected sites: `equipment_onhit_proc_test.go` (~6), `burn_proc_test.go` (1), `proc_slow_test.go` (2), `beam_proc_test.go` (~5).

The rewrite is mechanical — the flat payload fields move inside `Params: ProcEffectParams{...}`, `Chance` stays at the top level. Two concrete examples that cover both shapes; apply the identical transformation at every site:

Before (`equipment_onhit_proc_test.go:20`):
```go
attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 1.0, Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}}
```
After:
```go
attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 1.0, Params: ProcEffectParams{Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}}}
```

Before (`burn_proc_test.go:23-26`):
```go
proc := EquipmentProc{
	Chance: 1.0, Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt",
	BurnDamagePerSecond: 8, BurnDurationSeconds: 3,
}
```
After:
```go
proc := EquipmentProc{Chance: 1.0, Params: ProcEffectParams{
	Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt",
	BurnDamagePerSecond: 8, BurnDurationSeconds: 3,
}}
```
Where a test later reads payload fields off the local `proc` variable (e.g. `proc.BurnDamagePerSecond`, `proc.SlowMultiplier`, `proc.Damage` in `burn_proc_test.go`, `proc_slow_test.go`, `beam_proc_test.go`), update the selector to `proc.Params.<Field>`.

- [ ] **Step 5: Compile-check, run the affected tests, then the whole package**

Run: `cd server && go build ./... && go test ./internal/game/ -run "TestOnHitProc|TestExecuteProcEffect|TestFireSword|TestFrostSword|TestLightningSword" -v`
Expected: PASS.
Run: `cd server && go test ./internal/game/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add server/internal/game/state_items.go server/internal/game/state_combat.go server/internal/game/equipment_onhit_proc_test.go server/internal/game/burn_proc_test.go server/internal/game/proc_slow_test.go server/internal/game/beam_proc_test.go
git commit -m "EquipmentProc carries resolved ProcEffectParams; chance roll is trigger-only"
```

---

### Task 5: Item schema → effect reference + overrides; migrate the three swords

**Files:**
- Modify: `server/internal/game/items.go:67-121` (ItemOnHitProc), `:250-268` (validateItemDef)
- Modify: `server/internal/game/state_items.go` (recompute uses ResolveParams)
- Modify: `server/internal/game/catalog/items/weapons/rare/fire_sword.json`
- Modify: `server/internal/game/catalog/items/weapons/rare/frost_sword.json`
- Modify: `server/internal/game/catalog/items/weapons/rare/lightning_sword.json`
- Modify (tests): `server/internal/game/item_onhit_def_test.go`, `server/internal/game/elemental_swords_test.go`, `server/internal/game/burn_proc_test.go` (TestFireSword_ProcIsWiredToBurn), `server/internal/game/proc_slow_test.go` (TestFrostSword_ProcIsWiredToChill), `server/internal/game/beam_proc_test.go` (TestLightningSword_ProcIsWiredToChain)

**Interfaces:**
- Consumes: `ProcEffectOverrides`, `resolveProcEffectParams` (Task 2), `getProcEffectDef` (Task 1), `EquipmentProc{Chance, Params}` (Task 4).
- Produces: `type ItemOnHitProc struct {Chance float64; Effect string; ProcEffectOverrides}` and `(p *ItemOnHitProc) ResolveParams() (ProcEffectParams, bool)` — used by recompute and by catalog-wiring tests.

- [ ] **Step 1: Rewrite the failing validation test first**

Replace `server/internal/game/item_onhit_def_test.go` entirely:
```go
package game

import "testing"

func TestValidateItemDef_OnHitFields(t *testing.T) {
	good := &ItemDef{
		ID:   "fire_ring",
		Kind: ItemKindEquipment,
		OnHitElemental: []ItemElementalDamage{{Type: DamageFire, Amount: 5}},
	}
	if err := validateItemDef(good); err != nil {
		t.Fatalf("valid item def rejected: %v", err)
	}

	// A proc is now a REFERENCE to a catalog effect (+ optional overrides).
	goodProc := &ItemDef{
		ID:        "fire_sword",
		Kind:      ItemKindEquipment,
		OnHitProc: &ItemOnHitProc{Chance: 0.05, Effect: "fire_bolt_ignite"},
	}
	if err := validateItemDef(goodProc); err != nil {
		t.Fatalf("valid proc def rejected: %v", err)
	}

	goodOverride := &ItemDef{
		ID:        "heavy_fire_sword",
		Kind:      ItemKindEquipment,
		OnHitProc: &ItemOnHitProc{Chance: 0.05, Effect: "fire_bolt_ignite", ProcEffectOverrides: ProcEffectOverrides{Damage: 40}},
	}
	if err := validateItemDef(goodOverride); err != nil {
		t.Fatalf("valid proc override rejected: %v", err)
	}

	badType := &ItemDef{ID: "bad", OnHitElemental: []ItemElementalDamage{{Type: DamageType("plasma"), Amount: 5}}}
	if err := validateItemDef(badType); err == nil {
		t.Fatalf("expected error for unregistered elemental damage type, got nil")
	}

	badChance := &ItemDef{ID: "bad2", OnHitProc: &ItemOnHitProc{Chance: 1.5, Effect: "fire_bolt_ignite"}}
	if err := validateItemDef(badChance); err == nil {
		t.Fatalf("expected error for proc chance > 1, got nil")
	}

	noEffect := &ItemDef{ID: "bad3", OnHitProc: &ItemOnHitProc{Chance: 0.1}}
	if err := validateItemDef(noEffect); err == nil {
		t.Fatalf("expected error for missing onHitProc.effect, got nil")
	}

	unknownEffect := &ItemDef{ID: "bad4", OnHitProc: &ItemOnHitProc{Chance: 0.1, Effect: "no_such_effect"}}
	if err := validateItemDef(unknownEffect); err == nil {
		t.Fatalf("expected error for unregistered onHitProc.effect, got nil")
	}
}

// TestItemOnHitProc_ResolveParams: an item reference resolves to the catalog
// def's payload with the item's non-zero overrides applied.
func TestItemOnHitProc_ResolveParams(t *testing.T) {
	plain := &ItemOnHitProc{Chance: 0.1, Effect: "lightning_chain"}
	p, ok := plain.ResolveParams()
	if !ok {
		t.Fatal("lightning_chain should resolve")
	}
	def, _ := getProcEffectDef("lightning_chain")
	if p != def.ProcEffectParams {
		t.Errorf("no overrides ⇒ def payload verbatim:\n got %+v\nwant %+v", p, def.ProcEffectParams)
	}

	tuned := &ItemOnHitProc{Chance: 0.1, Effect: "lightning_chain", ProcEffectOverrides: ProcEffectOverrides{Damage: 40, BounceCount: 4}}
	p2, _ := tuned.ResolveParams()
	if p2.Damage != 40 || p2.BounceCount != 4 {
		t.Errorf("overrides not applied: %+v", p2)
	}
	if p2.DamageType != def.DamageType || p2.ProjectileID != def.ProjectileID || p2.BounceRange != def.BounceRange {
		t.Errorf("non-overridden/identity fields must keep def values: %+v", p2)
	}

	missing := &ItemOnHitProc{Chance: 0.1, Effect: "no_such_effect"}
	if _, ok := missing.ResolveParams(); ok {
		t.Error("unknown effect must resolve ok=false")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/game/ -run "TestValidateItemDef_OnHitFields|TestItemOnHitProc_ResolveParams" -v`
Expected: FAIL to compile — `ItemOnHitProc` has no field `Effect`, no method `ResolveParams`.

- [ ] **Step 3: Rewrite `ItemOnHitProc` + validation** (`server/internal/game/items.go`)

Replace the `ItemOnHitProc` struct (items.go:67-121) with:
```go
// ItemOnHitProc is a percent-chance on-hit trigger: on each landed basic
// attack the wielder rolls Chance against the seeded perk RNG and, on
// success, fires the referenced proc effect (catalog/procs) at the current
// target. Effect names the ProcEffectDef (required); the embedded
// ProcEffectOverrides let this item re-tune the effect's numbers (damage,
// scale, bounce, slow, burn) without authoring a new def — the effect's
// element and emitter are fixed by the def. Damage is applied as its own
// instance and does NOT re-trigger on-hit effects (no recursion).
type ItemOnHitProc struct {
	Chance float64 `json:"chance"`
	Effect string  `json:"effect"`
	ProcEffectOverrides
}

// ResolveParams returns the proc's effective payload: the referenced effect
// def with this item's non-zero overrides applied. ok is false when Effect
// names no registered proc effect — validateItemDef rejects that at load, so
// a false here can only come from a hand-built def in tests.
func (p *ItemOnHitProc) ResolveParams() (ProcEffectParams, bool) {
	def, ok := getProcEffectDef(p.Effect)
	if !ok {
		return ProcEffectParams{}, false
	}
	return resolveProcEffectParams(def, p.ProcEffectOverrides), true
}
```
Replace the proc branch of `validateItemDef` (items.go:256-266) with:
```go
	if p := def.OnHitProc; p != nil {
		if p.Chance < 0 || p.Chance > 1 {
			return fmt.Errorf("item %q onHitProc.chance %v out of range [0,1]", def.ID, p.Chance)
		}
		if p.Effect == "" {
			return fmt.Errorf("item %q onHitProc.effect is required (a catalog/procs id)", def.ID)
		}
		if _, ok := getProcEffectDef(p.Effect); !ok {
			return fmt.Errorf("item %q onHitProc.effect %q is not a registered proc effect", def.ID, p.Effect)
		}
		if p.Damage < 0 {
			return fmt.Errorf("item %q onHitProc.damage override %v must be >= 0", def.ID, p.Damage)
		}
		if p.ProjectileScale < 0 {
			return fmt.Errorf("item %q onHitProc.projectileScale override %v must be >= 0", def.ID, p.ProjectileScale)
		}
	}
```
(Var-init ordering: `itemCatalogSingleton`'s initializer now transitively references `procEffectDefsByID` via `getProcEffectDef` — Go's dependency-ordered var initialization guarantees the proc catalog loads first, same mechanism the loot-table loader relies on for items.)

- [ ] **Step 4: Point recompute at the resolver** (`server/internal/game/state_items.go`, the block from Task 4 Step 2)

```go
		if p := def.OnHitProc; p != nil {
			if params, ok := p.ResolveParams(); ok {
				unit.EquipmentBonus.OnHitProcs = append(unit.EquipmentBonus.OnHitProcs, EquipmentProc{Chance: p.Chance, Params: params})
			}
		}
```
(No `.OrPhysical()` needed anymore: `validateProcEffectDef` guarantees a registered damage type on every catalog effect.)

- [ ] **Step 5: Migrate the three sword JSONs** (line 14 of each; rest of each file unchanged)

`fire_sword.json`:
```json
  "onHitProc": { "chance": 0.1, "effect": "fire_bolt_ignite" }
```
`frost_sword.json`:
```json
  "onHitProc": { "chance": 0.1, "effect": "frost_bolt_chill" }
```
`lightning_sword.json`:
```json
  "onHitProc": { "chance": 0.1, "effect": "lightning_chain" }
```

- [ ] **Step 6: Update the catalog-wiring tests to resolve through the effect**

Every test reading payload fields off `def.OnHitProc` switches to `ResolveParams()`. The uniform pattern — shown fully for the burn guard; apply identically to the others:

`burn_proc_test.go` — `TestFireSword_ProcIsWiredToBurn` body becomes:
```go
	def, ok := getItemDef("fire_sword")
	if !ok {
		t.Fatal("fire_sword not in catalog")
	}
	p := def.OnHitProc
	if p == nil {
		t.Fatal("fire_sword has no onHitProc")
	}
	params, ok := p.ResolveParams()
	if !ok {
		t.Fatalf("fire_sword onHitProc.effect %q is not a registered proc effect", p.Effect)
	}
	if params.BurnDamagePerSecond <= 0 {
		t.Errorf("fire_sword burn needs a positive DPS, got %v", params.BurnDamagePerSecond)
	}
	if params.BurnDurationSeconds <= 0 {
		t.Errorf("fire_sword burn needs a positive duration, got %v", params.BurnDurationSeconds)
	}
```
Apply the same `p := def.OnHitProc` → `params, ok := p.ResolveParams()` transformation to:
- `proc_slow_test.go` `TestFrostSword_ProcIsWiredToChill` (reads `SlowMultiplier`, `SlowDurationSeconds` → `params.*`)
- `beam_proc_test.go` `TestLightningSword_ProcIsWiredToChain` (reads `BounceCount`, `BounceRange`, `BounceDamageFalloff`, `ProjectileID`, `Damage` → `params.*`; `Chance` stays `p.Chance`)
- `elemental_swords_test.go` — both the table-driven structural test (lines ~49-66) and `TestFireSword_EndToEnd` (lines ~79-84): `def.OnHitProc.Damage/DamageType/ProjectileID` → `params.Damage/DamageType/ProjectileID`; `def.OnHitProc.Chance` stays as-is. Find every remaining payload read with `grep -n "OnHitProc\." internal/game/*_test.go` and convert each.

- [ ] **Step 7: Run the migrated tests, then the whole package, then everything**

Run: `cd server && go test ./internal/game/ -run "TestValidateItemDef|TestItemOnHitProc_ResolveParams|TestFireSword|TestFrostSword|TestLightningSword|TestElemental" -v`
Expected: PASS.
Run: `cd server && go test ./...`
Expected: PASS — the whole server module, proving the schema change breaks no loader, shop, loot, or crafting path.

- [ ] **Step 8: Commit**

```bash
git add server/internal/game/items.go server/internal/game/state_items.go server/internal/game/catalog/items/weapons/rare/ server/internal/game/item_onhit_def_test.go server/internal/game/elemental_swords_test.go server/internal/game/burn_proc_test.go server/internal/game/proc_slow_test.go server/internal/game/beam_proc_test.go
git commit -m "Items reference proc effects by id with overrides; migrate elemental swords"
```

---

### Task 6: Full verification sweep

**Files:**
- No new files; fixes only if verification finds drift.

**Interfaces:**
- Consumes: everything above.
- Produces: a green build — the done-signal for the branch.

- [ ] **Step 1: Vet + format + full test run**

Run: `cd server && gofmt -l ./internal/game/ && go vet ./... && go test ./... -count=1`
Expected: `gofmt -l` prints nothing; vet clean; all tests PASS.

- [ ] **Step 2: Grep for leftovers of the old system**

Run: `cd server && grep -rn "fireOnHitProc\|proc.Damage\b" internal/game/ --include="*.go"`
Expected: no matches (old function names gone; no stale flat-field reads).

- [ ] **Step 3: Determinism spot-check**

Run: `cd server && go test ./internal/game/ -run "TestOnHitProc_Deterministic" -count=5 -v`
Expected: PASS ×5 (seeded RNG stream consumption unchanged by the refactor).

- [ ] **Step 4: Commit any verification fixes**

```bash
git add -A server/internal/game/
git commit -m "Proc effects: verification fixes"
```
(Skip the commit if Step 1-3 found nothing to fix.)
