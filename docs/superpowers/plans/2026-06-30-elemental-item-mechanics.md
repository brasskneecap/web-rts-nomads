# Elemental Item Mechanics Implementation Plan (Plan 1 of 2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add data-driven elemental on-hit mechanics to the item system — a separate typed damage instance per element and a % chance proc that fires an elemental bolt — plus the new base content (elemental rings, the three crafted elemental swords as item defs, and frost/lightning bolt projectiles).

**Architecture:** Two optional properties on `ItemDef` (`onHitElemental`, `onHitProc`) are aggregated per-unit into the existing cached `UnitEquipmentBonus` whenever a loadout changes, then applied at the single existing on-hit hub `resolveAttackHitLocked` — which fires for both melee swings (called directly) and ranged shots (called from `landProjectileLocked`). Elemental damage is applied as its own `applyUnitDamageWithSourceLocked` call tagged with the element, distinct from the physical hit. Procs spawn a dedicated recursion-safe projectile that bypasses the on-hit hub so it cannot re-trigger procs (the same non-recursion discipline base-stat splash already uses).

**Tech Stack:** Go (server, `server/internal/game`), JSON catalog files embedded via `//go:embed`. Deterministic tick simulation under a seeded RNG (`s.rngPerks`).

**Scope note:** This is Plan 1 of 2. It delivers working, equippable elemental items with full combat behavior, testable end-to-end in Go with no UI. Plan 2 (recipes, Recipe Shop, Artificer, crafting commands, profile persistence, Vue UI) builds on the items this plan produces. Design spec: `docs/superpowers/specs/2026-06-30-equipment-recipe-crafting-design.md`.

## Global Constraints

These apply to every task (from `CLAUDE.md` / `.claude/rules/AI_RULES.md`):

- **Determinism.** Simulation must be deterministic under a seed. Use the existing seeded `s.rngPerks` stream for every random roll. No `math/rand`, no wall-clock, no map-iteration order driving outcomes.
- **Lock discipline.** Functions ending in `Locked` assume `s.mu` is already held. Do not acquire the lock inside them; do not call them without holding it.
- **ID-based targeting.** Reference other units/buildings by ID, not by long-lived pointer. Within-tick `*Unit` parameters and local variables are fine and preferred. Never persist a resolved `*Unit` on a struct that outlives the tick. (Procs spawn projectiles that already store `TargetUnitID int` — keep that pattern.)
- **Resolve-and-validate.** Any `getUnitByIDLocked` result used must be nil/HP/Visible/ownership checked before use.
- **No client-side simulation.** This plan is server-only. The client receives the new `ItemDef` fields automatically via the existing `/catalog/items` JSON route and ignores fields it doesn't read yet.
- **Damage types are registered.** `fire`/`frost`/`lightning` already exist in `damage_type.go`'s registry. New typed content must use a registered `DamageType`.

---

### Task 1: Add `onHitElemental` and `onHitProc` fields to `ItemDef` + validation

**Files:**
- Modify: `server/internal/game/items.go` (add types + fields near `ItemModifiers` at lines 45-92; add validation in `loadItemCatalog` at lines 101-128)
- Test: `server/internal/game/item_onhit_def_test.go` (create)

**Interfaces:**
- Produces:
  - `type ItemElementalDamage struct { Type DamageType; Amount int }` (JSON: `type`, `amount`)
  - `type ItemOnHitProc struct { Chance float64; Damage int; DamageType DamageType; ProjectileID string }` (JSON: `chance`, `damage`, `damageType`, `projectileID`)
  - `ItemDef.OnHitElemental []ItemElementalDamage` (JSON `onHitElemental,omitempty`)
  - `ItemDef.OnHitProc *ItemOnHitProc` (JSON `onHitProc,omitempty`)
  - `func validateItemDef(def *ItemDef) error` — returns an error when any `OnHitElemental[i].Type` or `OnHitProc.DamageType` is non-empty but not a registered damage type, or `OnHitProc.Chance` is outside `[0,1]`.

- [ ] **Step 1: Write the failing test**

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

	goodProc := &ItemDef{
		ID:        "fire_sword",
		Kind:      ItemKindEquipment,
		OnHitProc: &ItemOnHitProc{Chance: 0.05, Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"},
	}
	if err := validateItemDef(goodProc); err != nil {
		t.Fatalf("valid proc def rejected: %v", err)
	}

	badType := &ItemDef{ID: "bad", OnHitElemental: []ItemElementalDamage{{Type: DamageType("plasma"), Amount: 5}}}
	if err := validateItemDef(badType); err == nil {
		t.Fatalf("expected error for unregistered elemental damage type, got nil")
	}

	badChance := &ItemDef{ID: "bad2", OnHitProc: &ItemOnHitProc{Chance: 1.5, Damage: 10, DamageType: DamageFire}}
	if err := validateItemDef(badChance); err == nil {
		t.Fatalf("expected error for proc chance > 1, got nil")
	}

	badProcType := &ItemDef{ID: "bad3", OnHitProc: &ItemOnHitProc{Chance: 0.1, Damage: 10, DamageType: DamageType("void")}}
	if err := validateItemDef(badProcType); err == nil {
		t.Fatalf("expected error for unregistered proc damage type, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/internal/game/ -run TestValidateItemDef_OnHitFields -v`
Expected: FAIL — `undefined: ItemElementalDamage` / `validateItemDef` (compile error).

- [ ] **Step 3: Add the types, fields, and validation**

In `server/internal/game/items.go`, add after the `ItemModifiers` struct (after line 55):

```go
// ItemElementalDamage is a flat typed damage amount an equipment item adds as
// its OWN damage instance on each landed basic attack — separate from physical
// modifiers.Damage so future resistance/weakness logic can treat it
// independently. Type must be a registered DamageType.
type ItemElementalDamage struct {
	Type   DamageType `json:"type"`
	Amount int        `json:"amount"`
}

// ItemOnHitProc is a percent-chance on-hit effect: on each landed basic attack
// the wielder rolls Chance against the seeded perk RNG and, on success, fires a
// homing projectile (ProjectileID) dealing Damage of DamageType to the current
// target. Damage is applied as its own instance and does NOT re-trigger on-hit
// effects (no recursion).
type ItemOnHitProc struct {
	Chance       float64    `json:"chance"`
	Damage       int        `json:"damage"`
	DamageType   DamageType `json:"damageType"`
	ProjectileID string     `json:"projectileID"`
}
```

Add two fields to `ItemDef` (after the `Effects` field, line 89):

```go
	OnHitElemental   []ItemElementalDamage `json:"onHitElemental,omitempty"`
	OnHitProc        *ItemOnHitProc        `json:"onHitProc,omitempty"`
```

Add the validator function (anywhere in `items.go`, e.g. after `getItemDef`):

```go
// validateItemDef checks the on-hit fields of an item def. Empty DamageType is
// rejected here (unlike combat code that resolves it to physical) because a
// typed elemental bonus with no explicit element is a content authoring error.
func validateItemDef(def *ItemDef) error {
	for i, e := range def.OnHitElemental {
		if !IsValidDamageType(e.Type) {
			return fmt.Errorf("item %q onHitElemental[%d]: unregistered damage type %q", def.ID, i, e.Type)
		}
	}
	if p := def.OnHitProc; p != nil {
		if p.Chance < 0 || p.Chance > 1 {
			return fmt.Errorf("item %q onHitProc.chance %v out of range [0,1]", def.ID, p.Chance)
		}
		if !IsValidDamageType(p.DamageType) {
			return fmt.Errorf("item %q onHitProc.damageType: unregistered damage type %q", def.ID, p.DamageType)
		}
	}
	return nil
}
```

Add `"fmt"` to the imports in `items.go` (the import block at lines 3-9 does not currently include it).

Wire validation into `loadItemCatalog` — after the `if def.ID == ""` check (line 118-120) and before `catalog[def.ID] = &def`:

```go
		if err := validateItemDef(&def); err != nil {
			panic(path + ": " + err.Error())
		}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/internal/game/ -run TestValidateItemDef_OnHitFields -v`
Expected: PASS.

- [ ] **Step 5: Run the full package build to confirm the embedded catalog still loads**

Run: `go test ./server/internal/game/ -run TestValidateItemDef_OnHitFields -count=1`
Expected: PASS (package init loads the real catalog through the new validator without panicking).

- [ ] **Step 6: Commit**

```bash
git add server/internal/game/items.go server/internal/game/item_onhit_def_test.go
git commit -m "feat(items): add onHitElemental and onHitProc fields to ItemDef with validation"
```

---

### Task 2: Aggregate on-hit bonuses into `UnitEquipmentBonus`

**Files:**
- Modify: `server/internal/game/state_items.go` (extend `UnitEquipmentBonus` at lines 28-36; rework the loop in `recomputeUnitEquipmentBonusLocked` at lines 205-239)
- Test: `server/internal/game/equipment_onhit_bonus_test.go` (create)

**Interfaces:**
- Consumes: `ItemDef.OnHitElemental`, `ItemDef.OnHitProc` (Task 1).
- Produces:
  - `type EquipmentProc struct { Chance float64; Damage int; DamageType DamageType; ProjectileID string }`
  - `UnitEquipmentBonus.OnHitElemental map[DamageType]int` — summed amount per element across all equipped items.
  - `UnitEquipmentBonus.OnHitProcs []EquipmentProc` — one entry per equipped item carrying a proc.

- [ ] **Step 1: Write the failing test**

```go
package game

import "testing"

// equip directly sets a unit's slot and recomputes — bypasses the vault/equip
// handlers since this test only exercises bonus aggregation.
func equipForTest(s *GameState, u *Unit, itemID string) {
	u.InventorySize++
	u.Equipped = append(u.Equipped, &EquippedItem{InstanceID: s.allocItemInstanceIDLocked(), ItemID: itemID, Stacks: 1})
	s.recomputeUnitEquipmentBonusLocked(u)
}

func TestEquipmentBonus_AggregatesElementalAndProc(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x0E1)
	s.mu.Lock()
	defer s.mu.Unlock()

	u := &Unit{ID: s.nextUnitID, OwnerID: "p1", UnitType: "soldier", Visible: true, HP: 100, MaxHP: 100}
	s.nextUnitID++
	s.addUnitLocked(u)

	// One fire ring → +5 fire on-hit, no proc.
	equipForTest(s, u, "fire_ring")
	if got := u.EquipmentBonus.OnHitElemental[DamageFire]; got != 5 {
		t.Fatalf("after fire_ring: fire on-hit = %d, want 5", got)
	}
	if len(u.EquipmentBonus.OnHitProcs) != 0 {
		t.Fatalf("fire_ring should carry no proc, got %d", len(u.EquipmentBonus.OnHitProcs))
	}

	// Add a fire sword → fire on-hit stacks to 10 (ring 5 + sword 5) and one proc appears.
	equipForTest(s, u, "fire_sword")
	if got := u.EquipmentBonus.OnHitElemental[DamageFire]; got != 10 {
		t.Fatalf("after fire_ring+fire_sword: fire on-hit = %d, want 10", got)
	}
	if got := u.EquipmentBonus.Damage; got != 5 {
		t.Fatalf("fire_sword physical modifier should add 5 damage, got %d", got)
	}
	if len(u.EquipmentBonus.OnHitProcs) != 1 {
		t.Fatalf("fire_sword should carry exactly one proc, got %d", len(u.EquipmentBonus.OnHitProcs))
	}
	p := u.EquipmentBonus.OnHitProcs[0]
	if p.Damage != 25 || p.DamageType != DamageFire || p.ProjectileID != "fire_bolt" || p.Chance <= 0 {
		t.Fatalf("fire_sword proc unexpected: %+v", p)
	}
}
```

> This test depends on the `fire_ring` and `fire_sword` catalog files created in Tasks 6 and 7. When running this task in isolation before those exist, it will fail at the catalog lookup. Implement Tasks 6 and 7's JSON files first if you are executing strictly task-by-task, OR temporarily assert against inline defs. The recommended execution order is 1 → 2 → 6 → 7 → 3 → 4 → 5 → 8; see "Execution order" at the bottom. For this task, write the code in Steps 3, then create the two JSON files (copy from Tasks 6/7) so the test can pass here.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/internal/game/ -run TestEquipmentBonus_AggregatesElementalAndProc -v`
Expected: FAIL — `u.EquipmentBonus.OnHitElemental` undefined (compile error).

- [ ] **Step 3: Extend the bonus struct and aggregation loop**

In `server/internal/game/state_items.go`, add the proc type above `UnitEquipmentBonus` (before line 28):

```go
// EquipmentProc is a runtime copy of an item's ItemOnHitProc, aggregated onto a
// unit's EquipmentBonus so the combat hook can roll it without re-reading the
// catalog every hit.
type EquipmentProc struct {
	Chance       float64
	Damage       int
	DamageType   DamageType
	ProjectileID string
}
```

Add two fields to `UnitEquipmentBonus` (after `MaxShield int`, line 35):

```go
	// OnHitElemental sums per-element flat damage applied as a SEPARATE typed
	// instance on each landed basic attack. nil when no equipped item grants any.
	OnHitElemental map[DamageType]int
	// OnHitProcs is one entry per equipped item that defines an onHitProc.
	OnHitProcs     []EquipmentProc
```

Rework the loop in `recomputeUnitEquipmentBonusLocked` (lines 209-224). The current loop does `if !ok || def.Modifiers == nil { continue }`, which would skip rings (they have no `modifiers`). Replace the loop body so modifiers and on-hit data are handled independently:

```go
	for _, slot := range unit.Equipped {
		if slot == nil {
			continue
		}
		def, ok := s.itemCatalog[slot.ItemID]
		if !ok {
			continue
		}
		if def.Modifiers != nil {
			unit.EquipmentBonus.Damage += def.Modifiers.Damage
			unit.EquipmentBonus.HP += def.Modifiers.HP
			unit.EquipmentBonus.Armor += def.Modifiers.Armor
			unit.EquipmentBonus.AttackSpeed += def.Modifiers.AttackSpeed
			unit.EquipmentBonus.MoveSpeed += def.Modifiers.MoveSpeed
			unit.EquipmentBonus.HealthRegen += def.Modifiers.HealthRegen
			unit.EquipmentBonus.MaxShield += def.Modifiers.MaxShield
		}
		for _, e := range def.OnHitElemental {
			if e.Amount == 0 {
				continue
			}
			if unit.EquipmentBonus.OnHitElemental == nil {
				unit.EquipmentBonus.OnHitElemental = make(map[DamageType]int)
			}
			unit.EquipmentBonus.OnHitElemental[e.Type.OrPhysical()] += e.Amount
		}
		if p := def.OnHitProc; p != nil {
			unit.EquipmentBonus.OnHitProcs = append(unit.EquipmentBonus.OnHitProcs, EquipmentProc{
				Chance:       p.Chance,
				Damage:       p.Damage,
				DamageType:   p.DamageType.OrPhysical(),
				ProjectileID: p.ProjectileID,
			})
		}
	}
```

(The line `unit.EquipmentBonus = UnitEquipmentBonus{}` at line 208 already resets the new fields to nil each recompute — leave it.)

- [ ] **Step 4: Create the catalog JSON this test needs**

If executing strictly in order, create `fire_ring.json` (Task 6) and `fire_sword.json` (Task 7) now by copying their contents from those tasks. Otherwise this step is satisfied once Tasks 6–7 run.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./server/internal/game/ -run TestEquipmentBonus_AggregatesElementalAndProc -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add server/internal/game/state_items.go server/internal/game/equipment_onhit_bonus_test.go
git commit -m "feat(items): aggregate onHitElemental and onHitProc into UnitEquipmentBonus"
```

---

### Task 3: Apply elemental on-hit damage as a separate typed instance

**Files:**
- Modify: `server/internal/game/state_combat.go` (add a helper; call it from `resolveAttackHitLocked` after the perk attack hooks at line 322, before the target-death block at line 324)
- Test: `server/internal/game/equipment_onhit_combat_test.go` (create)

**Interfaces:**
- Consumes: `UnitEquipmentBonus.OnHitElemental` (Task 2).
- Produces: `func (s *GameState) applyEquipmentOnHitElementalLocked(attacker, target *Unit)` — applies one `applyUnitDamageWithSourceLocked` call per element with a positive aggregated amount, tagged `Kind: "item-elemental"` and the element's `DamageType`. Deterministic iteration (sorted by `DamageTypes()` order) so determinism holds even though it reads a map.

- [ ] **Step 1: Write the failing test**

```go
package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

func TestOnHitElemental_AppliesSeparateTypedInstance(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xE1E)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	attacker.AttackDamageType = "" // physical basic attack
	// Give the attacker a +5 fire on-hit bonus directly.
	attacker.EquipmentBonus.OnHitElemental = map[DamageType]int{DamageFire: 5}

	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 100, MaxHP: 100}
	s.nextUnitID++
	s.addUnitLocked(target)

	s.resetDamageTypeHintsThisTickLocked()
	deadUnitIDs := []int{}
	// Physical hit of 8; armor 0 so the physical lands as 8 and fire as 5 → HP 87.
	s.resolveAttackHitLocked(attacker, target, 8, &deadUnitIDs)

	if target.HP != 87 {
		t.Fatalf("expected HP 87 (100 - 8 physical - 5 fire), got %d", target.HP)
	}
	// The fire instance must emit a "fire" colored hint (proof it was a typed,
	// separate damage event — physical emits none).
	if hint := findHint(s, target.ID, "fire"); hint == nil {
		t.Fatalf("expected a fire damage-type hint from the elemental on-hit instance; queue: %+v", s.damageTypeHintsThisTick)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/internal/game/ -run TestOnHitElemental_AppliesSeparateTypedInstance -v`
Expected: FAIL — `target.HP` is 92 (only the physical 8 applied) and no fire hint, because the helper does not exist yet.

- [ ] **Step 3: Implement the helper and wire it in**

Add to `server/internal/game/state_combat.go`:

```go
// applyEquipmentOnHitElementalLocked applies the attacker's aggregated
// per-element on-hit damage as SEPARATE typed damage instances against the
// primary target, distinct from the physical hit that resolveAttackHitLocked
// already landed. Iterates DamageTypes() (sorted) rather than ranging the map
// directly so the order of damage events is deterministic. No-op when the
// attacker has no elemental bonus. Must be called under s.mu.
func (s *GameState) applyEquipmentOnHitElementalLocked(attacker, target *Unit) {
	if attacker == nil || target == nil || len(attacker.EquipmentBonus.OnHitElemental) == 0 {
		return
	}
	for _, dt := range DamageTypes() {
		amt := attacker.EquipmentBonus.OnHitElemental[dt]
		if amt <= 0 {
			continue
		}
		s.applyUnitDamageWithSourceLocked(target, amt, DamageSource{
			AttackerUnitID: attacker.ID,
			Kind:           "item-elemental",
			DamageType:     dt,
		})
	}
}
```

In `resolveAttackHitLocked`, insert the call immediately after the perk attack hooks (after line 322 `s.onPerkAttackDamageAppliedLocked(attacker, target, damage)` and before the `if target.HP <= 0` block at line 324):

```go
	s.applyEquipmentOnHitElementalLocked(attacker, target)
```

This placement means: the existing `if target.HP <= 0` death block (lines 324-341) catches a kill landed by the elemental instance, so no separate death bookkeeping is needed here. (Elemental damage is intentionally NOT applied to splash victims — `applySplashDamageLocked` calls `applyUnitDamageWithSourceLocked` directly and never enters this hub, matching the design's "primary target only" rule.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/internal/game/ -run TestOnHitElemental_AppliesSeparateTypedInstance -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/game/state_combat.go server/internal/game/equipment_onhit_combat_test.go
git commit -m "feat(combat): apply equipment onHitElemental as a separate typed damage instance"
```

---

### Task 4: Roll the on-hit proc and fire a recursion-safe elemental bolt

**Files:**
- Modify: `server/internal/game/projectile.go` (add `SkipOnHitEffects bool` to `Projectile` at lines 33-120; add a proc-spawn helper; add a direct-damage branch at the top of `landProjectileLocked` at line 489)
- Modify: `server/internal/game/state_combat.go` (call the proc roller from `resolveAttackHitLocked` right after the elemental call from Task 3)
- Test: `server/internal/game/equipment_onhit_proc_test.go` (create)

**Interfaces:**
- Consumes: `UnitEquipmentBonus.OnHitProcs` (Task 2), `s.rngPerks` (existing seeded RNG).
- Produces:
  - `Projectile.SkipOnHitEffects bool` — when true, `landProjectileLocked` applies damage directly and does NOT call `resolveAttackHitLocked` (prevents proc→on-hit→proc recursion).
  - `func (s *GameState) fireOnHitProcProjectileLocked(attacker, target *Unit, proc EquipmentProc)` — spawns a homing projectile carrying `proc.Damage`/`proc.DamageType`, `Variant`/sprite from `proc.ProjectileID`, `SkipOnHitEffects: true`.
  - `func (s *GameState) rollEquipmentProcsLocked(attacker, target *Unit)` — for each proc on the attacker, rolls `s.rngPerks.Float64() < proc.Chance` and fires the bolt on success.

- [ ] **Step 1: Write the failing test**

```go
package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

func TestOnHitProc_FiresBoltDeterministically(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x9C0)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 500, MaxHP: 500}
	s.nextUnitID++
	s.addUnitLocked(target)

	// chance 1.0 → a proc projectile must spawn on every hit.
	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 1.0, Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}}
	before := len(s.Projectiles)
	deadUnitIDs := []int{}
	s.resolveAttackHitLocked(attacker, target, 1, &deadUnitIDs)
	if len(s.Projectiles) != before+1 {
		t.Fatalf("chance 1.0 should spawn exactly one proc projectile, got %d new", len(s.Projectiles)-before)
	}
	proc := s.Projectiles[len(s.Projectiles)-1]
	if !proc.SkipOnHitEffects || proc.Damage != 25 || proc.DamageType != DamageFire {
		t.Fatalf("proc projectile fields unexpected: %+v", proc)
	}

	// chance 0.0 → never spawns.
	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 0.0, Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}}
	before = len(s.Projectiles)
	s.resolveAttackHitLocked(attacker, target, 1, &deadUnitIDs)
	if len(s.Projectiles) != before {
		t.Fatalf("chance 0.0 should spawn no proc projectile, got %d new", len(s.Projectiles)-before)
	}
}

func TestOnHitProc_ProjectileDoesNotReProc(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x9C1)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 1.0, Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}}
	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 500, MaxHP: 500}
	s.nextUnitID++
	s.addUnitLocked(target)

	// Fire one proc, then land it manually. Landing must apply 25 fire damage and
	// must NOT spawn another proc projectile (SkipOnHitEffects bypasses the hub).
	deadUnitIDs := []int{}
	s.rollEquipmentProcsLocked(attacker, target)
	if len(s.Projectiles) != 1 {
		t.Fatalf("expected 1 proc projectile, got %d", len(s.Projectiles))
	}
	proc := s.Projectiles[0]
	hpBefore := target.HP
	s.landProjectileLocked(proc, target, &deadUnitIDs)
	if target.HP != hpBefore-25 {
		t.Fatalf("proc landing should deal 25, HP went %d→%d", hpBefore, target.HP)
	}
	if len(s.Projectiles) != 1 {
		t.Fatalf("landing a proc projectile must not spawn another projectile, have %d", len(s.Projectiles))
	}
}

func TestOnHitProc_Deterministic(t *testing.T) {
	run := func() int {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x5EED)
		s.mu.Lock()
		defer s.mu.Unlock()
		attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
		attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{{Chance: 0.5, Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}}
		target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 1_000_000, MaxHP: 1_000_000}
		s.nextUnitID++
		s.addUnitLocked(target)
		count := 0
		for i := 0; i < 200; i++ {
			before := len(s.Projectiles)
			s.rollEquipmentProcsLocked(attacker, target)
			if len(s.Projectiles) > before {
				count++
			}
		}
		return count
	}
	a, b := run(), run()
	if a != b {
		t.Fatalf("proc rolls not deterministic under fixed seed: %d vs %d", a, b)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/internal/game/ -run TestOnHitProc -v`
Expected: FAIL — `rollEquipmentProcsLocked` / `SkipOnHitEffects` undefined (compile error).

- [ ] **Step 3: Add the projectile field, spawn helper, and land branch**

In `server/internal/game/projectile.go`, add to the `Projectile` struct (after the `IsCrit` field, line 119):

```go
	// SkipOnHitEffects marks a projectile that must NOT run the on-hit reaction
	// hub when it lands — its damage is applied directly via
	// applyUnitDamageWithSourceLocked. Set on equipment-proc bolts so a proc
	// cannot trigger another proc (mirrors the non-recursion discipline of
	// base-stat splash, which also bypasses resolveAttackHitLocked).
	SkipOnHitEffects bool
```

Add the spawn helper (near `fireHomingProjectileLocked`):

```go
// fireOnHitProcProjectileLocked spawns a homing projectile for an equipment
// on-hit proc. Unlike fireHomingProjectileLocked it does not derive its damage
// type from the attacker — it carries the proc's own Damage/DamageType — and it
// sets SkipOnHitEffects so landing applies damage directly without re-entering
// the on-hit hub. Must be called under s.mu.
func (s *GameState) fireOnHitProcProjectileLocked(attacker, target *Unit, proc EquipmentProc) {
	speed := defaultProjectileSpeed
	var followEffect, impactEffect string
	if def, ok := getProjectileDef(proc.ProjectileID); ok {
		speed = def.Speed
		followEffect = followEffectForProjectileDef(def)
		impactEffect = impactEffectForProjectileDef(def)
	}

	dx := target.X - attacker.X
	dy := target.Y - attacker.Y
	travelTime := math.Sqrt(dx*dx+dy*dy) / speed
	if travelTime < minProjectileFlightSeconds {
		travelTime = minProjectileFlightSeconds
	}

	id := fmt.Sprintf("proj_%d", s.nextProjectileID)
	s.nextProjectileID++

	variant := proc.ProjectileID
	if variant == "" {
		variant = attacker.UnitType
	}
	s.Projectiles = append(s.Projectiles, &Projectile{
		ID:               id,
		OwnerUnitID:      attacker.ID,
		OwnerPlayerID:    attacker.OwnerID,
		TargetUnitID:     target.ID,
		OriginX:          attacker.X,
		OriginY:          attacker.Y,
		TargetX:          target.X,
		TargetY:          target.Y,
		TotalSeconds:     travelTime,
		RemainingSeconds: travelTime,
		Damage:           proc.Damage,
		Variant:          variant,
		FollowEffect:     followEffect,
		ImpactEffect:     impactEffect,
		DamageType:       proc.DamageType,
		Scale:            attacker.ProjectileScale,
		SkipOnHitEffects: true,
	})
}
```

Add the direct-damage branch at the top of `landProjectileLocked`, immediately after `s.playProjectileImpactLocked(proj, target)` (line 494) and before the `attacker := s.getUnitByIDLocked(...)` line:

```go
	if proj.SkipOnHitEffects {
		// Equipment-proc bolt: apply its typed damage directly. Bypasses the
		// on-hit hub so it cannot trigger another proc or elemental instance.
		s.applyUnitDamageWithSourceLocked(target, proj.Damage, DamageSource{
			AttackerUnitID: proj.OwnerUnitID,
			Kind:           "item-proc",
			DamageType:     proj.DamageType,
		})
		if target.HP <= 0 {
			target.HP = 0
			*deadUnitIDs = append(*deadUnitIDs, target.ID)
		}
		return
	}
```

- [ ] **Step 4: Add the proc roller and wire it into the hub**

In `server/internal/game/state_combat.go`:

```go
// rollEquipmentProcsLocked rolls each of the attacker's equipped on-hit procs
// against the seeded perk RNG and fires an elemental bolt for each success at
// the primary target. Must be called under s.mu. Determinism: rngPerks is the
// shared seeded stream; OnHitProcs order is fixed by equip order.
func (s *GameState) rollEquipmentProcsLocked(attacker, target *Unit) {
	if attacker == nil || target == nil || len(attacker.EquipmentBonus.OnHitProcs) == 0 {
		return
	}
	for _, proc := range attacker.EquipmentBonus.OnHitProcs {
		if proc.Chance <= 0 || proc.Damage <= 0 {
			continue
		}
		if s.rngPerks.Float64() < proc.Chance {
			s.fireOnHitProcProjectileLocked(attacker, target, proc)
		}
	}
}
```

In `resolveAttackHitLocked`, add the call right after the elemental call inserted in Task 3 (so both run before the `if target.HP <= 0` block):

```go
	s.applyEquipmentOnHitElementalLocked(attacker, target)
	s.rollEquipmentProcsLocked(attacker, target)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./server/internal/game/ -run TestOnHitProc -v`
Expected: PASS (all three).

- [ ] **Step 6: Commit**

```bash
git add server/internal/game/projectile.go server/internal/game/state_combat.go server/internal/game/equipment_onhit_proc_test.go
git commit -m "feat(combat): roll equipment on-hit procs and fire recursion-safe elemental bolts"
```

---

### Task 5: Add `frost_bolt` and `lightning_bolt` projectile defs

**Files:**
- Create: `server/internal/game/catalog/projectiles/frost_bolt/frost_bolt.json`
- Create: `server/internal/game/catalog/projectiles/lightning_bolt/lightning_bolt.json`
- Test: `server/internal/game/elemental_projectile_defs_test.go` (create)

**Interfaces:**
- Consumes: `getProjectileDef` (existing), `ProjectileDef` shape (`id`, `speed`, `followEffect`, `impactEffect`).
- Produces: two new projectile ids resolvable via `getProjectileDef`.

- [ ] **Step 1: Write the failing test**

```go
package game

import "testing"

func TestElementalBoltProjectileDefs_Load(t *testing.T) {
	for _, id := range []string{"fire_bolt", "frost_bolt", "lightning_bolt"} {
		def, ok := getProjectileDef(id)
		if !ok {
			t.Fatalf("projectile def %q not found in catalog", id)
		}
		if def.Speed <= 0 {
			t.Fatalf("projectile def %q has non-positive speed %v", id, def.Speed)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/internal/game/ -run TestElementalBoltProjectileDefs_Load -v`
Expected: FAIL — `frost_bolt` not found.

- [ ] **Step 3: Create the two JSON files**

`server/internal/game/catalog/projectiles/frost_bolt/frost_bolt.json`:

```json
{
  "id": "frost_bolt",
  "speed": 500,
  "followEffect": "",
  "impactEffect": "fizzle"
}
```

`server/internal/game/catalog/projectiles/lightning_bolt/lightning_bolt.json`:

```json
{
  "id": "lightning_bolt",
  "speed": 500,
  "followEffect": "",
  "impactEffect": "fizzle"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/internal/game/ -run TestElementalBoltProjectileDefs_Load -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/game/catalog/projectiles/frost_bolt server/internal/game/catalog/projectiles/lightning_bolt server/internal/game/elemental_projectile_defs_test.go
git commit -m "feat(content): add frost_bolt and lightning_bolt projectile defs"
```

---

### Task 6: Add the elemental ring base items

**Files:**
- Create: `server/internal/game/catalog/items/accessories/common/fire_ring.json`
- Create: `server/internal/game/catalog/items/accessories/common/ice_ring.json`
- Create: `server/internal/game/catalog/items/accessories/common/lightning_ring.json`
- Test: `server/internal/game/elemental_rings_test.go` (create)

**Interfaces:**
- Consumes: `getItemDef` (existing), the `OnHitElemental` field (Task 1).
- Produces: three item ids — `fire_ring`, `ice_ring`, `lightning_ring` — each `kind: equipment`, `slotKind: "any"` (the game has no dedicated accessory slot — all slots are universal), `onHitElemental` +5 of its element.

> Note: `ice_ring` uses damage type `frost` (the registered cold element in `damage_type.go`); "ice" is only its display name. The directory name `accessories/` is just catalog organization — `slotKind` is `"any"`.

- [ ] **Step 1: Write the failing test**

```go
package game

import "testing"

func TestElementalRings_Load(t *testing.T) {
	cases := []struct {
		id   string
		elem DamageType
	}{
		{"fire_ring", DamageFire},
		{"ice_ring", DamageFrost},
		{"lightning_ring", DamageLightning},
	}
	for _, tc := range cases {
		def, ok := getItemDef(tc.id)
		if !ok {
			t.Fatalf("item def %q not found", tc.id)
		}
		if def.Kind != ItemKindEquipment {
			t.Errorf("%s: kind = %q, want equipment", tc.id, def.Kind)
		}
		if def.SlotKind != ItemSlotKindAny {
			t.Errorf("%s: slotKind = %q, want any", tc.id, def.SlotKind)
		}
		var fire int
		for _, e := range def.OnHitElemental {
			if e.Type == tc.elem {
				fire = e.Amount
			}
		}
		if fire != 5 {
			t.Errorf("%s: onHitElemental %v = %d, want 5", tc.id, tc.elem, fire)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/internal/game/ -run TestElementalRings_Load -v`
Expected: FAIL — `fire_ring` not found.

- [ ] **Step 3: Create the three JSON files**

`fire_ring.json`:

```json
{
  "id": "fire_ring",
  "displayName": "Fire Ring",
  "description": "+5 fire damage on hit.",
  "iconKey": "fire_ring",
  "kind": "equipment",
  "tier": "common",
  "category": "Accessory",
  "slotKind": "any",
  "allowedUnitTypes": [],
  "costGold": 75,
  "onHitElemental": [ { "type": "fire", "amount": 5 } ]
}
```

`ice_ring.json` (note `type: "frost"`):

```json
{
  "id": "ice_ring",
  "displayName": "Ice Ring",
  "description": "+5 frost damage on hit.",
  "iconKey": "ice_ring",
  "kind": "equipment",
  "tier": "common",
  "category": "Accessory",
  "slotKind": "any",
  "allowedUnitTypes": [],
  "costGold": 75,
  "onHitElemental": [ { "type": "frost", "amount": 5 } ]
}
```

`lightning_ring.json`:

```json
{
  "id": "lightning_ring",
  "displayName": "Lightning Ring",
  "description": "+5 lightning damage on hit.",
  "iconKey": "lightning_ring",
  "kind": "equipment",
  "tier": "common",
  "category": "Accessory",
  "slotKind": "any",
  "allowedUnitTypes": [],
  "costGold": 75,
  "onHitElemental": [ { "type": "lightning", "amount": 5 } ]
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/internal/game/ -run TestElementalRings_Load -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/game/catalog/items/accessories server/internal/game/elemental_rings_test.go
git commit -m "feat(content): add fire/ice/lightning elemental ring base items"
```

---

### Task 7: Add the crafted elemental sword item defs + end-to-end combat test

**Files:**
- Create: `server/internal/game/catalog/items/weapons/rare/fire_sword.json`
- Create: `server/internal/game/catalog/items/weapons/rare/ice_sword.json`
- Create: `server/internal/game/catalog/items/weapons/rare/lightning_sword.json`
- Test: `server/internal/game/elemental_swords_test.go` (create)

> Note: a `flame_sword.json` and `ice_sword.json` already exist under `catalog/items/weapons/{rare,epic}/` as flat-damage themed weapons (per the item-system survey). To avoid an ID collision, the crafted output IDs are `fire_sword`, `ice_sword`, `lightning_sword`. Before creating files, confirm no existing file already uses the `id` value `ice_sword`: run `grep -rl '"id": "ice_sword"' server/internal/game/catalog/items`. If one exists, rename the crafted output to `frost_sword` and update the recipe in Plan 2 accordingly. (Catalog IDs are globally unique — two files with the same `id` would clobber each other in the map.)

**Interfaces:**
- Consumes: Tasks 1-6 (fields, aggregation, combat application, proc, projectile defs).
- Produces: three crafted item ids combining broad-sword physical (`modifiers.damage: 5`) + ring elemental (`onHitElemental: 5`) + a 5% / 25-damage proc bolt.

- [ ] **Step 1: Write the failing test**

```go
package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

func TestFireSword_EndToEnd(t *testing.T) {
	def, ok := getItemDef("fire_sword")
	if !ok {
		t.Fatalf("fire_sword not found")
	}
	if def.Modifiers == nil || def.Modifiers.Damage != 5 {
		t.Fatalf("fire_sword should grant +5 physical damage, got %+v", def.Modifiers)
	}
	if def.OnHitProc == nil || def.OnHitProc.Damage != 25 || def.OnHitProc.DamageType != DamageFire || def.OnHitProc.ProjectileID != "fire_bolt" {
		t.Fatalf("fire_sword proc unexpected: %+v", def.OnHitProc)
	}
	if def.OnHitProc.Chance < 0.049 || def.OnHitProc.Chance > 0.051 {
		t.Fatalf("fire_sword proc chance should be ~0.05, got %v", def.OnHitProc.Chance)
	}

	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xF12E)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	attacker.AttackDamageType = ""
	// Equip the fire sword via direct slot set + recompute.
	attacker.InventorySize++
	attacker.Equipped = append(attacker.Equipped, &EquippedItem{InstanceID: s.allocItemInstanceIDLocked(), ItemID: "fire_sword", Stacks: 1})
	s.recomputeUnitEquipmentBonusLocked(attacker)

	if attacker.EquipmentBonus.OnHitElemental[DamageFire] != 5 {
		t.Fatalf("equipped fire_sword: fire on-hit = %d, want 5", attacker.EquipmentBonus.OnHitElemental[DamageFire])
	}

	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 100, MaxHP: 100}
	s.nextUnitID++
	s.addUnitLocked(target)

	s.resetDamageTypeHintsThisTickLocked()
	deadUnitIDs := []int{}
	// Physical 10 + 5 fire separate instance → HP 85.
	s.resolveAttackHitLocked(attacker, target, 10, &deadUnitIDs)
	if target.HP != 85 {
		t.Fatalf("expected HP 85 (100 - 10 physical - 5 fire), got %d", target.HP)
	}
	if hint := findHint(s, target.ID, "fire"); hint == nil {
		t.Fatalf("expected a fire damage-type hint from the sword's elemental instance")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/internal/game/ -run TestFireSword_EndToEnd -v`
Expected: FAIL — `fire_sword` not found.

- [ ] **Step 3: Create the three JSON files**

`fire_sword.json`:

```json
{
  "id": "fire_sword",
  "displayName": "Fire Sword",
  "description": "+5 damage. +5 fire damage on hit. 5% chance on hit to launch a firebolt (25 fire damage).",
  "iconKey": "fire_sword",
  "kind": "equipment",
  "tier": "rare",
  "category": "Weapon",
  "slotKind": "any",
  "allowedUnitTypes": [],
  "costGold": 0,
  "modifiers": { "damage": 5 },
  "onHitElemental": [ { "type": "fire", "amount": 5 } ],
  "onHitProc": { "chance": 0.05, "damage": 25, "damageType": "fire", "projectileID": "fire_bolt" }
}
```

`ice_sword.json`:

```json
{
  "id": "ice_sword",
  "displayName": "Ice Sword",
  "description": "+5 damage. +5 frost damage on hit. 5% chance on hit to launch a frostbolt (25 frost damage).",
  "iconKey": "ice_sword",
  "kind": "equipment",
  "tier": "rare",
  "category": "Weapon",
  "slotKind": "any",
  "allowedUnitTypes": [],
  "costGold": 0,
  "modifiers": { "damage": 5 },
  "onHitElemental": [ { "type": "frost", "amount": 5 } ],
  "onHitProc": { "chance": 0.05, "damage": 25, "damageType": "frost", "projectileID": "frost_bolt" }
}
```

`lightning_sword.json`:

```json
{
  "id": "lightning_sword",
  "displayName": "Lightning Sword",
  "description": "+5 damage. +5 lightning damage on hit. 5% chance on hit to launch a lightning bolt (25 lightning damage).",
  "iconKey": "lightning_sword",
  "kind": "equipment",
  "tier": "rare",
  "category": "Weapon",
  "slotKind": "any",
  "allowedUnitTypes": [],
  "costGold": 0,
  "modifiers": { "damage": 5 },
  "onHitElemental": [ { "type": "lightning", "amount": 5 } ],
  "onHitProc": { "chance": 0.05, "damage": 25, "damageType": "lightning", "projectileID": "lightning_bolt" }
}
```

`costGold: 0` — these are crafted, not bought; the value is unused until/unless they appear in a shop. The recipe's gold cost lives on the `RecipeDef` in Plan 2.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/internal/game/ -run TestFireSword_EndToEnd -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/game/catalog/items/weapons/rare server/internal/game/elemental_swords_test.go
git commit -m "feat(content): add crafted fire/ice/lightning sword item defs"
```

---

### Task 8: Make elemental rings obtainable (neutral marketplace stock + enemy drops)

**Files:**
- Modify: the neutral merchant loot table and an enemy loot table under `server/internal/game/` (likely `loot_table_defs.go` and/or `catalog/.../loot-tables/*.json` — confirm the actual location first; see Step 1)
- Test: `server/internal/game/elemental_ring_drops_test.go` (create)

**Interfaces:**
- Consumes: the loot-table loader and the rings from Task 6.
- Produces: `fire_ring`/`ice_ring`/`lightning_ring` present in at least one neutral-shop loot table and one enemy loot table.

- [ ] **Step 1: Locate the loot tables (read before writing)**

Run: `grep -rln 'broad_sword' server/internal/game/catalog` and `grep -rln 'merchant_basic' server/internal/game`
Read the file that defines the `merchant_basic` neutral-shop loot table and the enemy loot table(s) referenced by neutral camps / enemy units. The shop survey identified `loot_table_defs.go` and `loot_table` catalog entries as the relevant spots; confirm whether tables are Go literals or JSON before editing. Note the exact table IDs and entry shape (item id + weight/quantity) used by neighbors.

- [ ] **Step 2: Write the failing test**

Adapt the table IDs to what Step 1 found. This test asserts the rings are reachable from a known table by ID. Replace `lootTableByID` / `merchant_basic` / `enemyTableID` with the real accessors/ids discovered in Step 1.

```go
package game

import "testing"

func TestElementalRings_InLootTables(t *testing.T) {
	want := map[string]bool{"fire_ring": false, "ice_ring": false, "lightning_ring": false}

	// Replace getLootTableByID + table ids with the real ones found in Step 1.
	for _, tableID := range []string{"merchant_basic", "neutral_camp_basic"} {
		table, ok := getLootTableByID(tableID)
		if !ok {
			t.Fatalf("loot table %q not found", tableID)
		}
		for _, entry := range table.Entries {
			if _, tracked := want[entry.ItemID]; tracked {
				want[entry.ItemID] = true
			}
		}
	}
	for id, found := range want {
		if !found {
			t.Errorf("ring %q is not present in any checked loot table", id)
		}
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./server/internal/game/ -run TestElementalRings_InLootTables -v`
Expected: FAIL — rings absent from the tables (or the test's accessor names need to match Step 1; fix names first, then it should fail on the missing entries).

- [ ] **Step 4: Add the rings to the tables**

Add `fire_ring`, `ice_ring`, `lightning_ring` entries (with weights consistent with neighboring common items) to the neutral merchant loot table and one enemy/neutral-camp loot table, following the exact entry shape observed in Step 1. (Do not invent a new table — extend existing ones so the rings flow through the established shop-stock and drop paths with no new wiring.)

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./server/internal/game/ -run TestElementalRings_InLootTables -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add -A server/internal/game
git commit -m "feat(content): add elemental rings to neutral merchant and enemy loot tables"
```

---

### Task 9: Full-package verification

**Files:** none (verification only).

- [ ] **Step 1: Run the whole game package test suite**

Run: `go test ./server/internal/game/ -count=1`
Expected: PASS — no existing combat/equipment/projectile/damage-type test regressed.

- [ ] **Step 2: Build the server**

Run: `go build ./server/...`
Expected: clean build.

- [ ] **Step 3: Run the broader suite if quick**

Run: `go test ./server/... -count=1`
Expected: PASS (or only pre-existing unrelated failures — note any in the handoff).

- [ ] **Step 4: Commit (only if any incidental fix was needed)**

```bash
git add -A
git commit -m "test: full-package verification for elemental item mechanics"
```

---

## Execution order

Strict task-by-task TDD has one ordering wrinkle: Task 2's aggregation test and Task 7's end-to-end test reference catalog files created in Tasks 6 and 7. Two options:

1. **Recommended:** Implement code Tasks 1 → 2 → 3 → 4 → 5, creating the `fire_ring.json` and `fire_sword.json` files early (their content is fixed in Tasks 6/7) so Task 2/end-to-end tests can pass, then formalize the remaining content in Tasks 6 → 7 → 8.
2. **Alternative:** Reorder to 1 → 5 → 6 → 7 (content first) → 2 → 3 → 4 → 8, so every catalog file exists before the aggregation/combat tests run. This keeps each test green at the moment it's written at the cost of writing JSON before the code that reads it.

Either way, Task 9 is last.

## Self-review checklist (completed during authoring)

- **Spec coverage:** onHitElemental separate instance (Tasks 1-3), 5% proc firebolt (Tasks 1,2,4), frost/lightning bolts (Task 5), rings as base items +5 elemental (Task 6), crafted swords combining broad-sword + ring + proc (Task 7), rings from neutral marketplace + enemy drops (Task 8). Recipe/shop/Artificer/persistence/UI are explicitly Plan 2.
- **Placeholders:** none — every code step shows complete code; Task 8 intentionally requires a read-first step because loot-table storage (Go vs JSON) must be confirmed against the repo, with the exact grep commands provided.
- **Type consistency:** `ItemElementalDamage`/`ItemOnHitProc` (catalog) vs `EquipmentProc` (runtime) used consistently; `UnitEquipmentBonus.OnHitElemental`/`OnHitProcs`; `Projectile.SkipOnHitEffects`; `applyEquipmentOnHitElementalLocked` / `rollEquipmentProcsLocked` / `fireOnHitProcProjectileLocked` names match between definition and call sites.
- **Determinism:** all rolls use `s.rngPerks`; the elemental application iterates `DamageTypes()` (sorted) instead of ranging a map.
