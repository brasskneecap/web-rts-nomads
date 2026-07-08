# Arch Mage "Meteor" Spell Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a data-driven Arch Mage point-target spell "Meteor" — a delayed-impact fire AoE that falls from the upper-right sky, deals impact damage, and leaves a lingering burning ground zone that damages enemies over time, with per-animation-frame render layering (frames 1–6 above units, frames 7+ below units).

**Architecture:** The spell's **gameplay** and **visual** are cleanly separated.
- *Gameplay (server):* Meteor is a `targetsPoint` ability. Casting spawns a new, **server-only** `GroundHazard` entity at the impact point. The hazard counts down an impact delay (the "fall time"), applies a one-time AoE at impact, then ticks a lingering burn AoE for a configured duration. No new wire/snapshot type is needed — damage rides the existing authoritative pipeline (popups already serialize; MP joiners run their sim on the host).
- *Visual (client):* A single world-anchored **effect** (`meteor`) plays at the impact point. Its `sprites.json` manifest gains two generic, reusable fields — `impactFrame` (per-frame render-layer split point) and `originOffsetX/Y` (the sky offset the sprite falls *from*). The canvas renderer gains a "below units" effect pass so frames ≥ `impactFrame` render on the ground layer while earlier frames render above units.

**Tech Stack:** Go (authoritative tick sim, `server/internal/game`), TypeScript/Vue 3 canvas renderer (`client/src/game-portal/src/game/rendering`), JSON catalogs for abilities/effects.

**Design intent (locked):** Meteor is heavier and slower than Fireball. Fireball = fast, single-target-seeking, reliable (18 mana, 90 dmg, 0.6s cast, 6s cd). Meteor = slow wind-up + falling telegraph, large impact, lingering burn (higher mana/cooldown, ground-targetable).

---

## Key reference points (verified against current code)

**Server:**
- `AbilityDef` struct + loader/validation: [ability_defs.go:97-368](../../../server/internal/game/ability_defs.go#L97-L368), `loadAbilityDefs` at [ability_defs.go:480](../../../server/internal/game/ability_defs.go#L480).
- `EffectiveSpell` (folded values): [spell_modifier.go:140-151](../../../server/internal/game/spell_modifier.go#L140-L151); `damageAmount`→`Damage`, `radius`→`Radius` already populated.
- Point-cast resolution: `beginAbilityCastAtPointLocked` [ability_cast.go:151](../../../server/internal/game/ability_cast.go#L151), `resolveAbilityCastAtPointLocked` [ability_cast.go:188-215](../../../server/internal/game/ability_cast.go#L188-L215). Existing branches: arcane_orb (projectile+pull) and shatter (instant AoE).
- AoE damage helper (handles mitigation, threat, attributed death/XP internally — does NOT return a dead-unit slice): `applyAbilitySplashDamageLocked(ownerUnitID int, ownerPlayerID string, cx, cy, radius float64, damage int, dmgType DamageType, primaryID int)` — [state_combat.go:428](../../../server/internal/game/state_combat.go#L428).
- Effect queueing at a world point: `playEffectAtPointLocked(effectID, x, y, scale)` [ability_cast.go:224](../../../server/internal/game/ability_cast.go#L224) → `queueEffectLocked` [state_effects.go:53](../../../server/internal/game/state_effects.go#L53). Duration comes from the `EffectDef`.
- `EffectDef` struct + loader: [effect_defs.go](../../../server/internal/game/effect_defs.go); catalog at `catalog/effects/<id>/<id>.json`.
- Persistent-entity precedent (structure to mirror for the hazard tick): trap `fire_pit` zone loop [trap.go:495-624](../../../server/internal/game/trap.go#L495-L624) and lifetime cull [trap.go:269-310](../../../server/internal/game/trap.go#L269-L310); arcane-orb per-tick DoT [projectile.go:543-572](../../../server/internal/game/projectile.go#L543-L572).
- `Unit` cast fields: [state.go:363-366](../../../server/internal/game/state.go#L363-L366) (`CastAbilityID`, `CastTargetID`, `CastTimeRemaining`).
- `GameState` entity slices + ID counters: [state.go:948-978](../../../server/internal/game/state.go#L948-L978) (`Traps`, `nextTrapID` at :957, init at [state.go:1267](../../../server/internal/game/state.go#L1267)).
- Tick loop `Update`: trap/effect ticks at [state.go:2751-2765](../../../server/internal/game/state.go#L2751-L2765). `drainPendingDeathsLocked` at :2765 — the hazard tick must run **before** it.
- Cast lifecycle: `tickUnitCastLocked` [ability_cast.go:314](../../../server/internal/game/ability_cast.go#L314), `clearUnitCastLocked` [ability_cast.go:503](../../../server/internal/game/ability_cast.go#L503).
- Spell registry: [spell-pools.json](../../../server/internal/game/catalog/spell-pools.json); pool test `arch_mage_bronze_pool_test.go`.

**Client:**
- Effect sprite loader + manifest types: `effectSprites.ts` — `EffectManifest` [~:9-26], `EffectSpriteSet` [~:28-37], registration loop [~:51-80], `getEffectSprite` [~:85] (`client/src/game-portal/src/game/rendering/effectSprites.ts`).
- Render loop draw order: `CanvasRenderer.render()` [CanvasRenderer.ts:500-517](../../../client/src/game-portal/src/game/rendering/CanvasRenderer.ts#L500-L517). `drawLootDrops()` :508 → `drawUnits()` :509 (Y-sorted units/buildings/obstacles) → `drawEffects()` :512 → `drawProjectiles()` :514.
- `drawEffects` implementation: [CanvasRenderer.ts:3039](../../../client/src/game-portal/src/game/rendering/CanvasRenderer.ts#L3039) — anchors world/unit, `frameIndex = min(frames-1, floor(effect.progress*frames))`, single row.
- Effect snapshot ingestion: `GameState.applySnapshot` sets `this.effects = message.effects ?? []` (`client/src/game-portal/src/game/core/GameState.ts`); renderer consumes raw snapshot arrays.
- Existing manifest format: `assets/effects/explosion/sprites.json` = `{ "frameWidth":64, "frameHeight":64, "offsetY":-20, "frames":13, "sheet":"sheet.png" }`.

**Asset already present:** `client/src/game-portal/src/assets/abilities/meteor/meteor.png` and `client/src/game-portal/src/assets/projectiles/meteor.png` (identical bytes) — a **1024×64** horizontal strip = **16 frames of 64×64**. The projectile copy is unused by this design (Meteor is an effect, not a projectile) — Task 11 removes it.

---

## File Structure

**Server — created:**
- `server/internal/game/ground_hazard.go` — the `GroundHazard` entity, its tick (`tickGroundHazardsLocked`), impact + burn appliers, spawn helper, ID helper. One responsibility: delayed-impact + lingering-burn ground zones (reusable for future "sky-drop"/hazard spells).
- `server/internal/game/ground_hazard_test.go` — unit tests for the hazard lifecycle.
- `server/internal/game/meteor_test.go` — end-to-end cast tests (point cast → impact → burn).
- `server/internal/game/catalog/abilities/meteor/meteor.json` — the `AbilityDef`.
- `server/internal/game/catalog/effects/meteor/meteor.json` — the `EffectDef` (id + animation duration).

**Server — modified:**
- `server/internal/game/ability_defs.go` — new `AbilityDef` fields + load validation.
- `server/internal/game/ability_cast.go` — new Meteor branch in `resolveAbilityCastAtPointLocked`; point-cast **cast-time** support in `beginAbilityCastAtPointLocked` + `tickUnitCastLocked` + `clearUnitCastLocked`.
- `server/internal/game/state.go` — `GroundHazard` slice + `nextGroundHazardID` on `GameState` (+ init); `CastIsPoint`/`CastPointX`/`CastPointY` on `Unit`; wire `tickGroundHazardsLocked` into `Update`.
- `server/internal/game/catalog/spell-pools.json` — add `meteor` to `arch_mage.silver`.
- `server/internal/game/arch_mage_bronze_pool_test.go` (or the silver-pool test) — update the enumerated pool membership.

**Client — created:**
- `client/src/game-portal/src/assets/effects/meteor/sheet.png` — copy of `meteor.png`.
- `client/src/game-portal/src/assets/effects/meteor/sprites.json` — manifest with new `impactFrame` + `originOffsetX/Y`.

**Client — modified:**
- `client/src/game-portal/src/game/rendering/effectSprites.ts` — manifest/registry fields for `impactFrame`, `originOffsetX/Y`.
- `client/src/game-portal/src/game/rendering/CanvasRenderer.ts` — split `drawEffects` into per-frame-layered passes (below-units + above-units), fall-offset interpolation.

---

## A note on the visual/gameplay timing contract (read before Task 2 & Task 8)

The meteor **effect** plays once over its `EffectDef.Duration` (client `progress` 0→1 maps linearly to frames: `frameIndex = floor(progress*frames)`). The **server** fires impact at `impactDelaySeconds`. To make the sprite visually land exactly when damage lands, author these so:

```
impactDelaySeconds  ≈  EffectDef.Duration × (impactFrame-1) / frames
```

With `frames=16`, `impactFrame=7`, `EffectDef.Duration=1.6`, `impactDelaySeconds=0.6`: `1.6 × 6/16 = 0.6`. ✔ Frames 1–6 (fall) play over 0–0.6s; frames 7–16 (impact + crater) play over 0.6–1.6s.

The **burn** runs server-side for `burnDurationSeconds` (independent of the 1.6s animation). During and after the crater animation, burn damage is readable as fire damage popups ticking off enemies in the zone. (Task 11 optionally adds a looping `burning` ground sprite for the full burn window.)

This one-line relationship is the only coupling between the sprite and the sim. It is documented in `meteor.json` and repeated in the effect manifest.

---

## Task 1: Add Meteor `AbilityDef` fields + validation

Adds the config knobs Meteor needs that no existing field covers: the fall delay and the four burn parameters. Impact damage reuses `damageAmount`, impact radius reuses `radius`, and the visual reuses `effectAtPoint`/`effectScale`.

**Files:**
- Modify: `server/internal/game/ability_defs.go` (struct near :97-368; loader/validation near :480)
- Test: `server/internal/game/meteor_test.go` (create)

- [ ] **Step 1: Write the failing test** — Meteor's catalog def parses with the new fields.

Create `server/internal/game/meteor_test.go`:

```go
package game

import "testing"

// meteorDef returns the catalog-authored Meteor ability. Tests derive expected
// values from this so tuning meteor.json never breaks a behavioral test.
func meteorDef(t *testing.T) AbilityDef {
	t.Helper()
	def, ok := getAbilityDef("meteor")
	if !ok {
		t.Fatal(`getAbilityDef("meteor") = _, false; want the catalog-authored Meteor`)
	}
	return def
}

func TestMeteorDef_ParsesConfigFields(t *testing.T) {
	def := meteorDef(t)
	if !def.TargetsPoint {
		t.Error("meteor must be a point-target spell (targetsPoint:true)")
	}
	if def.ImpactDelaySeconds <= 0 {
		t.Errorf("ImpactDelaySeconds = %v; want > 0", def.ImpactDelaySeconds)
	}
	if def.BurnDurationSeconds <= 0 {
		t.Errorf("BurnDurationSeconds = %v; want > 0", def.BurnDurationSeconds)
	}
	if def.BurnTickIntervalSeconds <= 0 {
		t.Errorf("BurnTickIntervalSeconds = %v; want > 0", def.BurnTickIntervalSeconds)
	}
	if def.BurnDamagePerTick <= 0 {
		t.Errorf("BurnDamagePerTick = %v; want > 0", def.BurnDamagePerTick)
	}
	if def.BurnRadius <= 0 {
		t.Errorf("BurnRadius = %v; want > 0", def.BurnRadius)
	}
	if def.DamageAmount <= 0 || def.Radius <= 0 {
		t.Errorf("impact damage/radius must be set: DamageAmount=%v Radius=%v", def.DamageAmount, def.Radius)
	}
}
```

- [ ] **Step 2: Run test — verify it fails to compile** (fields don't exist yet).

Run: `cd server && go test ./internal/game/ -run TestMeteorDef_ParsesConfigFields`
Expected: build failure — `def.ImpactDelaySeconds undefined` (and the catalog file is also absent until Task 2).

- [ ] **Step 3: Add the fields to `AbilityDef`.** In `ability_defs.go`, inside the `AbilityDef` struct (place after the existing `slowDurationSeconds` field near :348, keeping the "Spell mechanics" grouping):

```go
	// ── Delayed-impact ground hazard (Meteor and future "sky-drop" spells) ──────
	// These drive the generic GroundHazard entity (ground_hazard.go), spawned by
	// resolveAbilityCastAtPointLocked when ImpactDelaySeconds > 0. Impact damage
	// reuses DamageAmount; impact radius reuses Radius. Any future delayed-AoE +
	// lingering-DoT spell reuses these without new code.
	//
	// ImpactDelaySeconds is the "fall time": seconds between cast resolution and
	// the one-time impact AoE. See the visual/gameplay timing contract in the
	// Meteor plan — keep it ≈ effectDuration × (impactFrame-1)/frames so the
	// falling sprite lands when damage lands.
	ImpactDelaySeconds float64 `json:"impactDelaySeconds"`
	// Burn (lingering ground zone) knobs. Active only when BurnDurationSeconds > 0.
	BurnDurationSeconds     float64 `json:"burnDurationSeconds"`
	BurnDamagePerTick       int     `json:"burnDamagePerTick"`
	BurnTickIntervalSeconds float64 `json:"burnTickIntervalSeconds"`
	BurnRadius              float64 `json:"burnRadius"`
```

- [ ] **Step 4: Add load-time validation.** In `loadAbilityDefs` (near :480), after the def is unmarshaled and the existing field validations run, add a guard so a misconfigured delayed-impact spell fails fast at startup (mirrors the existing panic-on-bad-catalog discipline). Find the per-def validation block and append:

```go
		// Delayed-impact spells must declare a positive burn tick interval when a
		// burn is configured, otherwise tickGroundHazardsLocked would advance its
		// tick timer by 0 and loop. Impact delay without a burn is allowed
		// (pure delayed AoE); a burn without an impact delay is not (there is no
		// hazard to carry it).
		if def.BurnDurationSeconds > 0 && def.ImpactDelaySeconds <= 0 {
			panic("ability " + def.ID + ": burnDurationSeconds requires impactDelaySeconds > 0")
		}
		if def.BurnDurationSeconds > 0 && def.BurnTickIntervalSeconds <= 0 {
			panic("ability " + def.ID + ": burnDurationSeconds requires burnTickIntervalSeconds > 0")
		}
```

(Match the exact style of the surrounding validation — if the loader accumulates errors into a returned `error` rather than panicking, return a wrapped error instead of `panic`. Read the adjacent lines and follow the established pattern.)

- [ ] **Step 5: Run test — still red** because `meteor.json` doesn't exist yet. That's expected; Task 2 creates it. Confirm the *compile* error is gone (fields resolve):

Run: `cd server && go build ./internal/game/`
Expected: builds clean.

- [ ] **Step 6: Commit.**

```bash
git add server/internal/game/ability_defs.go server/internal/game/meteor_test.go
git commit -m "feat(meteor): add impact-delay + burn AbilityDef fields and validation"
```

---

## Task 2: Author the Meteor ability catalog def

**Files:**
- Create: `server/internal/game/catalog/abilities/meteor/meteor.json`

- [ ] **Step 1: Write the catalog file.**

```json
{
  "id": "meteor",
  "displayName": "Meteor",
  "type": "spell",
  "category": "offensive",
  "manaCost": 40,
  "cooldown": 12,
  "castTime": 0.8,
  "castRange": 450,
  "damageType": "fire",
  "targetsPoint": true,
  "canTargetSelf": false,
  "canTargetAllies": false,
  "canTargetEnemies": true,
  "damageAmount": 140,
  "radius": 130,
  "impactDelaySeconds": 0.6,
  "burnDurationSeconds": 4.0,
  "burnDamagePerTick": 12,
  "burnTickIntervalSeconds": 0.5,
  "burnRadius": 120,
  "casterAnimation": "Attacking",
  "effectAtPoint": "meteor",
  "effectScale": 1.0,
  "tags": ["aoe", "damage", "dot"],
  "icon": "TODO/abilities/meteor.png"
}
```

Notes for the implementer:
- `castTime: 0.8` requires the point-cast cast-time support in **Task 7**. If Task 7 is deferred, set `castTime` to `0` — Meteor still works (the fall delay provides the telegraph). Do not leave a non-zero point-cast `castTime` without Task 7, or the cast will be rejected/mis-timed.
- `impactDelaySeconds: 0.6` is chosen to align with the effect at `EffectDef.Duration=1.6`, `impactFrame=7`, `frames=16` (see timing contract). Keep these in sync when tuning.

- [ ] **Step 2: Run the Task-1 def test — now green.**

Run: `cd server && go test ./internal/game/ -run TestMeteorDef_ParsesConfigFields -v`
Expected: PASS.

- [ ] **Step 3: Commit.**

```bash
git add server/internal/game/catalog/abilities/meteor/meteor.json
git commit -m "feat(meteor): author Meteor ability catalog def"
```

---

## Task 3: `GroundHazard` entity + tick lifecycle (server-only)

The persistent gameplay entity. No wire/snapshot changes — damage is delivered through `applyAbilitySplashDamageLocked`, whose HP changes and popups already serialize.

**Files:**
- Create: `server/internal/game/ground_hazard.go`
- Create: `server/internal/game/ground_hazard_test.go`
- Modify: `server/internal/game/state.go` (add slice + ID counter + init + tick wiring)

- [ ] **Step 1: Add state to `GameState` and `Unit`.** In `state.go`, next to the `Traps`/`nextTrapID` fields (near :948-957), add:

```go
	// GroundHazards is the set of active delayed-impact / lingering-burn ground
	// zones (Meteor and future sky-drop spells). Server-only: never serialized —
	// the visual is a client effect and damage rides the authoritative pipeline.
	// Spawned by spawnGroundHazardLocked; ticked by tickGroundHazardsLocked in
	// Update (after traps, before drainPendingDeaths).
	GroundHazards      []*GroundHazard
	nextGroundHazardID int
```

In the `GameState` constructor where `nextTrapID: 1` is set (near :1267), add:

```go
		nextGroundHazardID:        1,
```

- [ ] **Step 2: Write the failing test** for the hazard lifecycle.

Create `server/internal/game/ground_hazard_test.go`:

```go
package game

import "testing"

// TestGroundHazard_DelaysImpactThenBurns verifies the two-phase lifecycle:
// no damage during the fall delay, a one-time impact hit at the delay, then
// periodic burn ticks for the burn duration, then removal.
func TestGroundHazard_DelaysImpactThenBurns(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	caster := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 100, Y: 100})
	caster.Visible = true
	enemy := spawnEnemy(t, s, 800, 800) // full HP=500, hostile faction
	enemyID := enemy.ID
	startHP := enemy.HP

	h := &GroundHazard{
		ID:                   groundHazardIDString(1),
		Kind:                 "meteor",
		OwnerUnitID:          caster.ID,
		OwnerPlayerID:        caster.OwnerID,
		X:                    800, Y: 800,
		ImpactDelayRemaining: 0.6,
		ImpactRadius:         130,
		ImpactDamage:         140,
		DamageType:           DamageFire,
		BurnRemaining:        4.0,
		BurnRadius:           120,
		BurnDamagePerTick:    12,
		BurnTickInterval:     0.5,
	}
	s.GroundHazards = append(s.GroundHazards, h)
	s.mu.Unlock()

	// Advance 0.5s (< impact delay): no damage yet.
	advance(s, 10) // 10 × 0.05
	s.mu.RLock()
	if s.unitsByID[enemyID].HP != startHP {
		t.Fatalf("enemy took damage before impact: HP=%d want %d", s.unitsByID[enemyID].HP, startHP)
	}
	s.mu.RUnlock()

	// Cross the impact threshold (total ~0.65s): impact applied exactly once.
	advance(s, 3)
	s.mu.RLock()
	afterImpact := s.unitsByID[enemyID].HP
	s.mu.RUnlock()
	if afterImpact >= startHP {
		t.Fatalf("enemy should have taken impact damage: HP=%d want < %d", afterImpact, startHP)
	}

	// Let the burn run to completion.
	advance(s, 90) // 4.5s
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.unitsByID[enemyID].HP >= afterImpact {
		t.Errorf("enemy should have taken burn damage over time: HP=%d want < %d", s.unitsByID[enemyID].HP, afterImpact)
	}
	if len(s.GroundHazards) != 0 {
		t.Errorf("hazard should be culled after burn ends: %d remaining", len(s.GroundHazards))
	}
}
```

Add the import if the test file needs it: `"webrts/server/pkg/protocol"`.

- [ ] **Step 2b: Run test — verify it fails to compile** (`GroundHazard` undefined).

Run: `cd server && go test ./internal/game/ -run TestGroundHazard_DelaysImpactThenBurns`
Expected: build failure.

- [ ] **Step 3: Implement `ground_hazard.go`.**

```go
package game

import "strconv"

// ═════════════════════════════════════════════════════════════════════════════
// GROUND HAZARD SYSTEM
//
// A GroundHazard is a placeable, server-only zone with a two-phase life:
//
//   1. FALL / DELAY phase: for ImpactDelayRemaining seconds after spawn the
//      hazard does nothing (the projectile is visually "falling"). The client
//      shows the fall via the meteor effect's early frames.
//   2. IMPACT: when the delay elapses, a one-time AoE hit is applied at the
//      hazard center (ImpactRadius / ImpactDamage).
//   3. BURN phase: for BurnRemaining seconds after impact, a periodic AoE
//      (every BurnTickInterval) damages hostile units standing within
//      BurnRadius. Enemies who walk in later are hit; enemies who leave stop
//      taking damage (re-evaluated every tick — a true ground zone, unlike a
//      one-shot burn debuff).
//
// EXTENSION POINT: this is the generic delayed-AoE + lingering-DoT primitive.
// Future "sky-drop" or "ground hazard" spells reuse it by setting the AbilityDef
// impactDelay/burn fields and going through spawnGroundHazardLocked — no new
// per-spell code. Kind is carried for future per-variant branching/telemetry
// but is not switched on today.
//
// Server-only by design: the hazard is never serialized. Its only player-visible
// output is (a) the client meteor EFFECT (queued separately at cast time) and
// (b) damage, which rides the authoritative pipeline and already reaches the
// client as HP deltas + damage popups. MP joiners run their sim on the host, so
// they see identical results with no extra wire state.
//
// Damage is delivered via applyAbilitySplashDamageLocked, which owns mitigation,
// threat, and the attributed death/XP drain — so this file never touches the
// death queue directly (drainPendingDeathsLocked, which runs later in Update,
// cleans up any kills).
// ═════════════════════════════════════════════════════════════════════════════

// GroundHazard is a delayed-impact + lingering-burn ground zone. All fields are
// snapshotted at spawn time so live catalog tuning does not retroactively change
// active hazards (same discipline as Trap).
type GroundHazard struct {
	ID            string
	Kind          string // ability id that spawned it ("meteor"); reserved for future branching
	OwnerUnitID   int
	OwnerPlayerID string
	X, Y          float64

	// Impact (fall) phase.
	ImpactDelayRemaining float64 // counts down; at <= 0 the one-time impact fires
	Impacted             bool
	ImpactRadius         float64
	ImpactDamage         int
	DamageType           DamageType

	// Burn (lingering) phase. Active while BurnRemaining > 0 after impact.
	BurnRemaining     float64
	BurnRadius        float64
	BurnDamagePerTick int
	BurnTickInterval  float64
	burnTickTimer     float64 // counts down to the next burn tick (unexported: runtime-only)
}

func groundHazardIDString(id int) string {
	return "hazard-" + strconv.Itoa(id)
}

// spawnGroundHazardLocked constructs a GroundHazard from an ability def + its
// effective values and appends it to s.GroundHazards. Impact damage/radius come
// from the effective spell (so a mana/damage modifier is honored); burn knobs
// and the fall delay come from the raw def (not modifier-eligible today).
//
// Caller holds s.mu.
func (s *GameState) spawnGroundHazardLocked(caster *Unit, def AbilityDef, eff EffectiveSpell, x, y float64) {
	if caster == nil {
		return
	}
	id := s.nextGroundHazardID
	s.nextGroundHazardID++
	s.GroundHazards = append(s.GroundHazards, &GroundHazard{
		ID:                   groundHazardIDString(id),
		Kind:                 def.ID,
		OwnerUnitID:          caster.ID,
		OwnerPlayerID:        caster.OwnerID,
		X:                    x,
		Y:                    y,
		ImpactDelayRemaining: def.ImpactDelaySeconds,
		ImpactRadius:         eff.Radius,
		ImpactDamage:         eff.Damage,
		DamageType:           def.DamageType.OrPhysical(),
		BurnRemaining:        def.BurnDurationSeconds,
		BurnRadius:           def.BurnRadius,
		BurnDamagePerTick:    def.BurnDamagePerTick,
		BurnTickInterval:     def.BurnTickIntervalSeconds,
	})
}

// tickGroundHazardsLocked advances every hazard by dt: counts down the fall
// delay, fires the one-time impact, then ticks the burn. Culls hazards whose
// burn has ended or whose owning player has left the match. Uses the
// filter-into-front-of-slice pattern (like tickTrapsLocked) to avoid allocation
// in the steady state.
//
// Must run under s.mu, AFTER combat/trap ticks and BEFORE drainPendingDeaths.
func (s *GameState) tickGroundHazardsLocked(dt float64) {
	if len(s.GroundHazards) == 0 {
		return
	}
	kept := s.GroundHazards[:0]
	for _, h := range s.GroundHazards {
		// Drop if the owning player has left the match (mirrors tickTrapsLocked).
		if _, ok := s.Players[h.OwnerPlayerID]; !ok {
			continue
		}

		if !h.Impacted {
			h.ImpactDelayRemaining -= dt
			if h.ImpactDelayRemaining > 0 {
				kept = append(kept, h)
				continue // still falling
			}
			s.applyHazardImpactLocked(h)
			h.Impacted = true
			h.burnTickTimer = 0 // fire the first burn tick promptly on the next branch
		}

		// Burn phase.
		if h.BurnRemaining > 0 {
			h.burnTickTimer -= dt
			if h.burnTickTimer <= 0 {
				h.burnTickTimer += h.BurnTickInterval
				s.applyHazardBurnTickLocked(h)
			}
			h.BurnRemaining -= dt
			if h.BurnRemaining > 0 {
				kept = append(kept, h)
			}
		}
		// else: no burn configured (pure delayed AoE) — drop after impact.
	}
	s.GroundHazards = kept
}

// applyHazardImpactLocked deals the one-time impact AoE at the hazard center.
// Caller holds s.mu.
func (s *GameState) applyHazardImpactLocked(h *GroundHazard) {
	if h.ImpactDamage <= 0 || h.ImpactRadius <= 0 {
		return
	}
	s.applyAbilitySplashDamageLocked(h.OwnerUnitID, h.OwnerPlayerID, h.X, h.Y, h.ImpactRadius, h.ImpactDamage, h.DamageType, 0)
}

// applyHazardBurnTickLocked deals one burn tick to all hostile units currently
// within BurnRadius. Re-evaluated each tick so it damages only who is present
// now (true ground zone). Caller holds s.mu.
func (s *GameState) applyHazardBurnTickLocked(h *GroundHazard) {
	if h.BurnDamagePerTick <= 0 || h.BurnRadius <= 0 {
		return
	}
	s.applyAbilitySplashDamageLocked(h.OwnerUnitID, h.OwnerPlayerID, h.X, h.Y, h.BurnRadius, h.BurnDamagePerTick, h.DamageType, 0)
}
```

Implementer note: confirm `applyAbilitySplashDamageLocked` tolerates an `ownerUnitID` whose unit has died/been removed (the caster can die during the 4s burn). Read [state_combat.go:428](../../../server/internal/game/state_combat.go#L428); if it dereferences the owner without a nil check, guard the lookup there or pass a sentinel exactly as the trap system does for dead trap owners ([trap.go:341-344](../../../server/internal/game/trap.go#L341-L344)). Do not change damage attribution semantics — just avoid a nil-deref.

- [ ] **Step 4: Wire the tick into `Update`.** In `state.go`, in the `Update` profiled section list, immediately after the `traps` line (:2760) and before `drainPendingDeaths` (:2765):

```go
	profileSection("groundHazards", func() { s.tickGroundHazardsLocked(dt) }) // delayed-impact + lingering burn zones
```

- [ ] **Step 5: Run test — verify green.**

Run: `cd server && go test ./internal/game/ -run TestGroundHazard_DelaysImpactThenBurns -v`
Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add server/internal/game/ground_hazard.go server/internal/game/ground_hazard_test.go server/internal/game/state.go
git commit -m "feat(meteor): add server-only GroundHazard entity + tick lifecycle"
```

---

## Task 4: Cast Meteor — point-cast branch that spawns the hazard + plays the effect

**Files:**
- Modify: `server/internal/game/ability_cast.go` (`resolveAbilityCastAtPointLocked`, :188-215)
- Test: `server/internal/game/meteor_test.go` (extend)

- [ ] **Step 1: Write the failing end-to-end test.**

Append to `server/internal/game/meteor_test.go`:

```go
// TestMeteor_PointCastSpawnsHazardAndEffect verifies a ground cast: spends mana,
// queues the world-anchored meteor effect at the clamped point, spawns a
// GroundHazard, deals no damage until the fall delay elapses, then impacts.
func TestMeteor_PointCastSpawnsHazardAndEffect(t *testing.T) {
	def := meteorDef(t)
	s := newProjectileTestState(t)
	s.mu.Lock()
	caster := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
	caster.Visible = true
	caster.CurrentMana = def.ManaCost + 10
	caster.Abilities = append(caster.Abilities, "meteor") // grant it for the test
	enemy := spawnEnemy(t, s, 360, 300)                   // within impact radius of the cast point
	enemyID := enemy.ID
	startHP := enemy.HP
	startMana := caster.CurrentMana

	// With castTime > 0 (Task 7) the cast is timed; without Task 7 set meteor
	// castTime to 0 and this resolves immediately. Either way, advancing past
	// castTime + impactDelay produces the impact.
	ok, reason := s.beginAbilityCastAtPointLocked(caster, "meteor", 360, 300)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("beginAbilityCastAtPointLocked failed: %q", reason)
	}

	// Let the cast resolve (if timed) so the hazard exists and the effect is queued.
	advance(s, int(def.CastTime/0.05)+2)
	s.mu.RLock()
	if len(s.GroundHazards) != 1 {
		t.Fatalf("expected 1 GroundHazard after cast; got %d", len(s.GroundHazards))
	}
	if caster.CurrentMana != startMana-def.ManaCost {
		t.Errorf("mana = %d; want %d (spent on resolution)", caster.CurrentMana, startMana-def.ManaCost)
	}
	if queuedEffectFor(s, "meteor", 0) == nil { // anchorUnitID 0 == world-anchored
		t.Error("meteor effect should have been queued at the cast point")
	}
	preImpactHP := s.unitsByID[enemyID].HP
	s.mu.RUnlock()
	if preImpactHP != startHP {
		t.Fatalf("enemy damaged before impact: HP=%d want %d", preImpactHP, startHP)
	}

	// Past the impact delay: enemy takes impact damage.
	advance(s, int(def.ImpactDelaySeconds/0.05)+3)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.unitsByID[enemyID].HP >= startHP {
		t.Errorf("enemy should have taken meteor impact damage: HP=%d want < %d", s.unitsByID[enemyID].HP, startHP)
	}
}
```

Note: `queuedEffectFor(s, "meteor", 0)` — confirm the helper at [effect_defs_test.go:7](../../../server/internal/game/effect_defs_test.go#L7) matches on `anchorUnitID`; world-anchored effects use anchor `0`. If the helper filters differently, adapt the assertion to iterate `s.activeEffects` for a `meteor`-named entry.

- [ ] **Step 2: Run test — expect failure** (no meteor branch yet: the existing shatter branch would try an *instant* AoE because `def.Projectile == "" && eff.Radius > 0`, so the enemy would be damaged immediately — the `preImpactHP` assertion fails).

Run: `cd server && go test ./internal/game/ -run TestMeteor_PointCastSpawnsHazardAndEffect`
Expected: FAIL (damage applied too early / no hazard).

- [ ] **Step 3: Add the Meteor branch.** In `resolveAbilityCastAtPointLocked` ([ability_cast.go:188](../../../server/internal/game/ability_cast.go#L188)), insert this branch **before** the existing instant-AoE (shatter) branch at :208 — Meteor also has `Projectile == "" && Radius > 0`, so it must be matched first and `return`:

```go
	// Delayed-impact ground hazard (Meteor and future sky-drop spells). A point
	// cast that declares an impact delay does NOT resolve its AoE instantly:
	// instead it spawns a GroundHazard that falls, impacts once after
	// ImpactDelaySeconds, then leaves a lingering burn. Fully data-driven off the
	// def (no per-spell branch) — any ability with impactDelaySeconds > 0 and no
	// projectile reuses this. The visual is a world-anchored effect whose sprite
	// metadata drives the fall animation + per-frame render layering (client).
	//
	// EXTENSION POINT: to add another delayed-AoE spell, author its ability JSON
	// with impactDelay/burn fields + an effectAtPoint effect — nothing here changes.
	if def.ImpactDelaySeconds > 0 && def.Projectile == "" {
		cx, cy := clampPointToRange(caster.X, caster.Y, x, y, def.CastRange.Resolve(caster))
		s.spawnGroundHazardLocked(caster, def, eff, cx, cy)
		// World-anchored VFX at the impact point. Duration comes from the
		// meteor EffectDef; see the timing contract (impactDelay is authored to
		// line up with the sprite's impactFrame). Plays regardless of hits so a
		// whiffed ground cast still reads.
		s.playEffectAtPointLocked(def.EffectAtPoint, cx, cy, def.EffectScale)
		return
	}
```

- [ ] **Step 4: Run test — verify green.**

Run: `cd server && go test ./internal/game/ -run 'TestMeteor_PointCastSpawnsHazardAndEffect|TestMeteorDef_ParsesConfigFields' -v`
Expected: PASS.

- [ ] **Step 5: Run the full game package to check for regressions.**

Run: `cd server && go test ./internal/game/`
Expected: PASS (Task 7 is not yet done; meteor.json's `castTime:0.8` is fine because point casts still resolve immediately today — the cast-time is simply ignored until Task 7. The test advances enough ticks either way. If any pre-existing point-cast test breaks, stop and reconcile before continuing.)

- [ ] **Step 6: Commit.**

```bash
git add server/internal/game/ability_cast.go server/internal/game/meteor_test.go
git commit -m "feat(meteor): spawn GroundHazard + play effect on point cast"
```

---

## Task 5: Meteor server `EffectDef`

Registers the effect id so `playEffectAtPointLocked` accepts it and the client's `meteor` snapshot has a matching server-owned definition. `Duration` sets the animation length (see timing contract).

**Files:**
- Create: `server/internal/game/catalog/effects/meteor/meteor.json`

- [ ] **Step 1: Write the effect def.**

```json
{
  "id": "meteor",
  "spritePath": "",
  "duration": 1.6,
  "anchor": "center"
}
```

`duration: 1.6` → with `frames:16`, `impactFrame:7`, the sprite's impact frame plays at `1.6 × 6/16 = 0.6s`, matching `impactDelaySeconds: 0.6`.

- [ ] **Step 2: Verify the catalog loads** (dir name must equal `id`).

Run: `cd server && go test ./internal/game/ -run 'TestMeteor'`
Expected: PASS (the effect def now resolves in `playEffectAtPointLocked`; `queuedEffectFor` finds it).

- [ ] **Step 3: Commit.**

```bash
git add server/internal/game/catalog/effects/meteor/meteor.json
git commit -m "feat(meteor): author Meteor EffectDef (1.6s animation)"
```

---

## Task 6: One shared Arch Mage spell pool (Bronze + Silver draw from it) + add Meteor

**Design decision (from the user):** there is **one** Arch Mage spell pool; both Bronze and Silver promotions roll from the same pool. Implement this as **cumulative pools**: a rank rolls from its own tier's list *plus every lower tier's list*. So Silver's effective pool = Bronze ∪ Silver (they share), Gold = Bronze ∪ Silver ∪ Gold. The existing no-duplicate logic (`unitKnownSpellSetLocked`, [spell_pool_roll.go:96-105](../../../server/internal/game/spell_pool_roll.go#L96-L105)) already guarantees the Silver roll picks a spell the Bronze roll didn't grant. Meteor is added to the single shared list.

This is minimal: it changes only `spellPoolFor` (the one resolver) and the catalog. Only `arch_mage` has pools today and Silver/Gold were empty, so the sole live effect is: an Arch Mage now also rolls a *second, distinct* spell from the shared pool at Silver promotion.

**Files:**
- Modify: `server/internal/game/spell_pool_defs.go` (`spellPoolFor`)
- Modify: `server/internal/game/catalog/spell-pools.json`
- Modify: `server/internal/game/arch_mage_bronze_pool_test.go`
- Test: `server/internal/game/spell_pool_roll_test.go` (add a shared-pool test)

- [ ] **Step 1: Write the failing test** for cumulative/shared pools.

Add to `server/internal/game/spell_pool_roll_test.go`:

```go
// The Arch Mage pool is shared across ranks: Silver rolls from the same set as
// Bronze (cumulative), so Bronze and Silver together grant two DISTINCT spells
// drawn from one pool.
func TestArchMagePool_SharedAcrossBronzeAndSilver(t *testing.T) {
	bronze := spellPoolFor("arch_mage", "bronze")
	silver := spellPoolFor("arch_mage", "silver")
	if len(silver) < len(bronze) {
		t.Fatalf("silver pool (%v) must include the whole bronze pool (%v)", silver, bronze)
	}
	for _, id := range bronze {
		if !containsStr(silver, id) {
			t.Errorf("shared pool: silver missing bronze spell %q (silver=%v)", id, silver)
		}
	}
	if !containsStr(bronze, "meteor") {
		t.Errorf("meteor should be in the shared pool; bronze=%v", bronze)
	}

	// A unit promoted to silver gets two distinct pool spells (one per rank).
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	u := spawnProjTestUnit(t, s, "p1", 100, 100)
	u.ProgressionPath = "arch_mage"
	u.Rank = "silver"
	u.Abilities = nil
	s.rollUnitPoolSpellsLocked(u)
	b, s2 := u.PoolSpellsByRank["bronze"], u.PoolSpellsByRank["silver"]
	if b == "" || s2 == "" {
		t.Fatalf("expected a bronze AND a silver pool pick; got bronze=%q silver=%q", b, s2)
	}
	if b == s2 {
		t.Errorf("bronze and silver picks must be distinct; both = %q", b)
	}
}
```

- [ ] **Step 2: Run — expect failure** (silver pool is empty today; meteor not present).

Run: `cd server && go test ./internal/game/ -run TestArchMagePool_SharedAcrossBronzeAndSilver`
Expected: FAIL.

- [ ] **Step 3: Add Meteor to the single shared list** in `spell-pools.json`. Author every Arch Mage spell once under `bronze`; leave `silver` empty (it inherits Bronze via the cumulative resolver):

```json
{
  "arch_mage": {
    "bronze": ["fireball", "chain_lightning", "arcane_orb", "shatter", "meteor"],
    "silver": []
  }
}
```

- [ ] **Step 4: Make `spellPoolFor` cumulative.** Replace the body of `spellPoolFor` ([spell_pool_defs.go:87-93](../../../server/internal/game/spell_pool_defs.go#L87-L93)) with a version that unions the requested rank with all lower ranks. Update the doc comment to describe the shared/cumulative model.

```go
// spellPoolFor returns the candidate ability ids an archetype may roll at `rank`.
// Pools are CUMULATIVE: a rank draws from its own tier's list plus every lower
// tier's list, so the whole archetype shares one growing pool (Bronze ⊆ Silver ⊆
// Gold). This is why a single authored list under "bronze" is rolled at both
// Bronze and Silver promotions; the no-duplicate logic in spell_pool_roll.go
// ensures each rank grants a distinct spell. Returns a freshly built, deduped,
// order-stable slice (bronze order, then any new silver ids, then new gold);
// callers may sort it (the roll does). Empty when the archetype has no pool.
func spellPoolFor(archetype, rank string) []string {
	byRank, ok := spellPoolsByArchetype[archetype]
	if !ok {
		return nil
	}
	// Lower-to-requested rank order; stop after the requested rank.
	order := []string{unitRankBronze, unitRankSilver, unitRankGold}
	seen := make(map[string]bool)
	var out []string
	for _, r := range order {
		for _, id := range byRank[r] {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
		if r == rank {
			break
		}
	}
	return out
}
```

(Confirm the `unitRankBronze/Silver/Gold` constants are those used elsewhere, e.g. [spell_pool_roll.go:83](../../../server/internal/game/spell_pool_roll.go#L83). If a requested `rank` isn't one of the three, the loop unions all tiers — acceptable, but the callers only pass valid ranks.)

- [ ] **Step 5: Update the existing bronze-pool test.** In `arch_mage_bronze_pool_test.go`, `TestArchMageBronzePool_Contents` asserts bronze = the old four spells and silver is empty. Update it for the shared pool:

```go
func TestArchMageBronzePool_Contents(t *testing.T) {
	bronze := spellPoolFor("arch_mage", "bronze")
	want := []string{"fireball", "chain_lightning", "arcane_orb", "shatter", "meteor"}
	if len(bronze) != len(want) {
		t.Fatalf("bronze pool = %v; want %v", bronze, want)
	}
	for _, id := range want {
		if !containsStr(bronze, id) {
			t.Errorf("bronze pool missing %q (got %v)", id, bronze)
		}
		if _, ok := getAbilityDef(id); !ok {
			t.Errorf("bronze pool spell %q has no registered AbilityDef", id)
		}
	}
	// Silver is cumulative — it inherits the full bronze list (shared pool).
	if silver := spellPoolFor("arch_mage", "silver"); len(silver) != len(want) {
		t.Errorf("silver pool = %v; want the shared bronze list %v (cumulative)", silver, want)
	}
}
```

(If other tests — e.g. `spell_pool_defs_test.go` or `TestArchMageBronzePool_AssignedSpellIsCastable` — assert the old empty-silver or four-spell bronze behavior, update their expectations the same way. `meteor` is point-targeted, so the castability branch in `TestArchMageBronzePool_AssignedSpellIsCastable` already handles it via `beginAbilityCastAtPointLocked` — but grant the caster enough mana/range: meteor needs manaCost 40 and castRange 450, so set `caster.CurrentMana = 100` (already) and `caster.AttackRange`/cast range accordingly; if the test's fixed cast point is out of the meteor's range it will still succeed because point casts clamp to range.)

- [ ] **Step 6: Run pool tests — verify green.**

Run: `cd server && go test ./internal/game/ -run 'Pool|SpellPool'`
Expected: PASS.

- [ ] **Step 7: Commit.**

```bash
git add server/internal/game/spell_pool_defs.go server/internal/game/catalog/spell-pools.json server/internal/game/*pool*test.go
git commit -m "feat(meteor): single shared Arch Mage pool (cumulative ranks) + add Meteor"
```

---

## Task 7: Point-cast cast-time support (wind-up)

Enables a non-zero `castTime` on point (ground-target) spells. Currently point casts resolve instantly ([ability_cast.go:146-149](../../../server/internal/game/ability_cast.go#L146-L149) documents the gap). This gives Meteor its wind-up and is reusable for any future timed point spell.

**Files:**
- Modify: `server/internal/game/state.go` (`Unit` cast fields, near :363-366)
- Modify: `server/internal/game/ability_cast.go` (`beginAbilityCastAtPointLocked`, `tickUnitCastLocked`, `clearUnitCastLocked`)
- Test: `server/internal/game/meteor_test.go` (extend)

- [ ] **Step 1: Write the failing test.**

Append to `server/internal/game/meteor_test.go`:

```go
// TestMeteor_TimedPointCast verifies castTime > 0 on a point spell: the caster
// is locked casting, mana is spent only on completion, and the hazard is spawned
// when the cast timer elapses (not at initiation).
func TestMeteor_TimedPointCast(t *testing.T) {
	def := meteorDef(t)
	if def.CastTime <= 0 {
		t.Skip("meteor castTime is 0; timed-point-cast path not exercised")
	}
	s := newProjectileTestState(t)
	s.mu.Lock()
	caster := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
	caster.Visible = true
	caster.CurrentMana = def.ManaCost + 10
	caster.Abilities = append(caster.Abilities, "meteor")
	startMana := caster.CurrentMana
	ok, reason := s.beginAbilityCastAtPointLocked(caster, "meteor", 360, 300)
	casting := caster.CastAbilityID
	manaAtStart := caster.CurrentMana
	s.mu.Unlock()
	if !ok {
		t.Fatalf("cast failed: %q", reason)
	}
	if casting != "meteor" {
		t.Errorf("caster should be locked casting meteor mid-cast; CastAbilityID=%q", casting)
	}
	if manaAtStart != startMana {
		t.Errorf("mana must not be spent at cast start: %d want %d", manaAtStart, startMana)
	}

	// Before the cast time elapses: no hazard yet.
	advance(s, int((def.CastTime/0.05))-1)
	s.mu.RLock()
	mid := len(s.GroundHazards)
	s.mu.RUnlock()
	if mid != 0 {
		t.Fatalf("hazard spawned before cast completed: %d", mid)
	}

	// After cast completes: hazard spawned, mana spent, cast cleared.
	advance(s, 3)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.GroundHazards) != 1 {
		t.Errorf("expected hazard after cast completes; got %d", len(s.GroundHazards))
	}
	if caster.CurrentMana != startMana-def.ManaCost {
		t.Errorf("mana = %d; want %d (spent on completion)", caster.CurrentMana, startMana-def.ManaCost)
	}
	if caster.CastAbilityID != "" {
		t.Errorf("cast should be cleared; CastAbilityID=%q", caster.CastAbilityID)
	}
}
```

- [ ] **Step 2: Run — expect failure** (point casts resolve instantly today, so the caster is never "casting" and the hazard exists at initiation).

Run: `cd server && go test ./internal/game/ -run TestMeteor_TimedPointCast`
Expected: FAIL.

- [ ] **Step 3: Add point-cast fields to `Unit`.** In `state.go`, after `CastTimeRemaining` (:365):

```go
	// Point (ground-target) cast in progress. When CastIsPoint is true the cast
	// resolves at a world point (CastPointX/Y) rather than a unit target — see
	// tickUnitCastLocked. Set by beginAbilityCastAtPointLocked for timed point
	// casts; cleared by clearUnitCastLocked.
	CastIsPoint bool
	CastPointX  float64
	CastPointY  float64
```

- [ ] **Step 4: Branch `beginAbilityCastAtPointLocked` on cast time.** In `ability_cast.go`, replace the immediate-resolve tail (currently `s.resolveAbilityCastAtPointLocked(caster, def, eff, x, y); return true, ""` at :179-180) with:

```go
	// Timed point cast: lock the caster and store the target point; resolution
	// happens in tickUnitCastLocked when the timer elapses. Zero cast time keeps
	// the prior behavior (resolve now). Mana is spent on completion, so an
	// interrupted wind-up costs nothing (matches the unit-target path).
	if eff.CastTime > 0 {
		caster.LastCastFailure = ""
		caster.CastAbilityID = abilityID
		caster.CastIsPoint = true
		caster.CastPointX = x
		caster.CastPointY = y
		caster.CastTimeRemaining = eff.CastTime
		s.beginUnitCastingLocked(caster) // lock + "Casting" animation slot
		return true, ""
	}
	s.resolveAbilityCastAtPointLocked(caster, def, eff, x, y)
	return true, ""
```

(The cooldown is already armed above this point, matching the unit-target path.)

- [ ] **Step 5: Resolve timed point casts in `tickUnitCastLocked`.** In `ability_cast.go` ([:314](../../../server/internal/game/ability_cast.go#L314)), right after the `def, ok := getAbilityDef(...)` lookup and the `unit.HP <= 0` guard, add a point-cast branch **before** the unit-target re-resolution (which assumes `CastTargetID`):

```go
	// Point (ground) casts have no target unit to re-resolve — the aim point was
	// fixed at cast start. Count down and resolve at the stored world point.
	if unit.CastIsPoint {
		unit.CastTimeRemaining -= dt
		if unit.CastTimeRemaining > 0 {
			return
		}
		eff := s.effectiveSpellLocked(unit, def)
		s.resolveAbilityCastAtPointLocked(unit, def, eff, unit.CastPointX, unit.CastPointY)
		s.clearUnitCastLocked(unit)
		return
	}
```

- [ ] **Step 6: Clear point-cast fields in `clearUnitCastLocked`.** In `ability_cast.go` ([:503](../../../server/internal/game/ability_cast.go#L503)), add to the field resets:

```go
	unit.CastIsPoint = false
	unit.CastPointX = 0
	unit.CastPointY = 0
```

- [ ] **Step 7: Run tests — verify green.**

Run: `cd server && go test ./internal/game/ -run 'TestMeteor'`
Expected: PASS.

- [ ] **Step 8: Full regression.**

Run: `cd server && go test ./internal/game/`
Expected: PASS (existing point spells arcane_orb/shatter have `castTime:0`, so they still resolve instantly — this branch is inert for them).

- [ ] **Step 9: Commit.**

```bash
git add server/internal/game/state.go server/internal/game/ability_cast.go server/internal/game/meteor_test.go
git commit -m "feat(meteor): support cast time (wind-up) on point-target casts"
```

---

## Task 8: Author the client Meteor effect asset

**Files:**
- Create: `client/src/game-portal/src/assets/effects/meteor/sheet.png` (copy of the strip)
- Create: `client/src/game-portal/src/assets/effects/meteor/sprites.json`

- [ ] **Step 1: Copy the spritesheet into the effects folder** (the effect glob loads `assets/effects/*/sheet.png` by folder name; `assets/abilities/meteor/` and `assets/projectiles/` are not loaded as effects).

Run:
```bash
mkdir -p "client/src/game-portal/src/assets/effects/meteor"
cp "client/src/game-portal/src/assets/abilities/meteor/meteor.png" "client/src/game-portal/src/assets/effects/meteor/sheet.png"
```

- [ ] **Step 2: Write the manifest** with the two new generic fields.

`client/src/game-portal/src/assets/effects/meteor/sprites.json`:

```json
{
  "frameWidth": 64,
  "frameHeight": 64,
  "frames": 16,
  "sheet": "sheet.png",
  "displayScale": 1.6,
  "impactFrame": 7,
  "originOffsetX": 280,
  "originOffsetY": -380
}
```

- `impactFrame: 7` — frames 1–6 render **above** units (falling); frames 7–16 render **below** units (impact + crater on the ground). See timing contract: with server `EffectDef.Duration=1.6` and `impactDelaySeconds=0.6`, frame 7 shows at 0.6s (impact).
- `originOffsetX/Y` — the sky offset (world px) the sprite falls *from*: `+X` = right, `-Y` = up ⇒ upper-right. The sprite interpolates from `(anchor + offset)` to `anchor` across frames 1–6, then stays at `anchor`.
- `displayScale: 1.6` — Meteor reads big; tune to taste.

- [ ] **Step 3: Commit** (client build wiring comes in Tasks 9–10).

```bash
git add client/src/game-portal/src/assets/effects/meteor/
git commit -m "feat(meteor): add client meteor effect spritesheet + manifest"
```

---

## Task 9: Extend the effect manifest/registry types

**Files:**
- Modify: `client/src/game-portal/src/game/rendering/effectSprites.ts`

- [ ] **Step 1: Add fields to `EffectManifest`** (the parsed `sprites.json` shape, ~:9-26). Add:

```ts
  /**
   * Optional per-frame render-layer split. Frames with index < (impactFrame-1)
   * render ABOVE units (e.g. a meteor falling through the air); frames from
   * (impactFrame-1) onward render BELOW units (on the ground layer). 1-based to
   * match how animators count frames. Omit for effects that render entirely on
   * the default (above-units) layer — the existing behavior is unchanged.
   *
   * EXTENSION POINT: any effect can opt into per-frame layering by setting this.
   */
  impactFrame?: number;
  /**
   * Optional origin offset (world px) the sprite visually falls FROM during the
   * pre-impact frames. The effect is anchored at its impact point; during frames
   * 1..(impactFrame-1) it is drawn at (anchor + offset), interpolated to (anchor)
   * by impact. +X = right, -Y = up. Omit for effects that don't travel.
   *
   * EXTENSION POINT: reusable "offset-origin" for any future sky-drop effect.
   */
  originOffsetX?: number;
  originOffsetY?: number;
```

- [ ] **Step 2: Add the same fields to `EffectSpriteSet`** (the runtime registry value, ~:28-37):

```ts
  impactFrame?: number;
  originOffsetX?: number;
  originOffsetY?: number;
```

- [ ] **Step 3: Surface them in the registration loop** (~:68-79) where the registry entry is built from the manifest. Add:

```ts
    impactFrame: manifest.impactFrame,
    originOffsetX: manifest.originOffsetX,
    originOffsetY: manifest.originOffsetY,
```

- [ ] **Step 4: Type-check.**

Run: `cd client/src/game-portal && npx vue-tsc -b`
Expected: no new errors. (Project convention: type-check is `vue-tsc -b`, not `--noEmit`.)

- [ ] **Step 5: Commit.**

```bash
git add client/src/game-portal/src/game/rendering/effectSprites.ts
git commit -m "feat(meteor): add impactFrame + originOffset fields to effect manifest"
```

---

## Task 10: Per-frame render layering + fall-offset in the canvas renderer

Split effect rendering into a **below-units** pass and the existing **above-units** pass, and apply the falling origin-offset. This is the core rendering extension — comment it as the reusable seam.

**Files:**
- Modify: `client/src/game-portal/src/game/rendering/CanvasRenderer.ts` (`render()` :500-517; `drawEffects` :3039)

- [ ] **Step 1: Add the below-units effect pass to the draw order.** In `render()`, between `this.drawLootDrops()` (:508) and `this.drawUnits(units)` (:509), add:

```ts
    // Ground-layer effects: frames that a per-frame manifest marks as
    // "below units" (e.g. a meteor's impact + burning crater) render here,
    // beneath the Y-sorted unit/building band, so they read as on-the-ground.
    // Effects with no per-frame layering are skipped here and drawn in the
    // above-units pass below (unchanged behavior).
    this.drawEffects(this.state.effects, units, 'below');
```

Then change the existing effects call at :512 from `this.drawEffects(this.state.effects, units)` to:

```ts
    this.drawEffects(this.state.effects, units, 'above');
```

- [ ] **Step 2: Make `drawEffects` layer-aware.** Change its signature to accept the pass, defaulting to `'above'` so any other caller is unaffected:

```ts
  private drawEffects(
    effects: EffectSnapshot[],
    units: UnitSnapshot[],
    pass: 'above' | 'below' = 'above',
  ): void {
```

(Use the actual snapshot types already imported in the file for `effects`/`units`.)

- [ ] **Step 3: Inside the per-effect loop, compute the current frame's layer and skip effects that don't belong to this pass.** After the sprite set is resolved and `frameIndex` is computed (near :3078), and before the draw call (:3093), insert:

```ts
      // Per-frame render layer (data-driven via the effect manifest's
      // impactFrame). Frames before the impact frame are "above units"
      // (in the air); the impact frame onward is "below units" (on the ground).
      // Effects without impactFrame are always "above" — legacy behavior.
      // EXTENSION POINT: this is the only place frame→layer is decided; any
      // effect that sets impactFrame gets the split for free.
      const impactIdx =
        spriteSet.impactFrame && spriteSet.impactFrame > 0
          ? spriteSet.impactFrame - 1
          : Infinity;
      const frameLayer = frameIndex >= impactIdx ? 'below' : 'above';
      if (frameLayer !== pass) {
        continue;
      }

      // Offset-origin fall: during the pre-impact frames the sprite is drawn
      // offset toward its spawn point (upper-right sky) and slides to the
      // anchor by impact. Zero when the effect declares no origin offset.
      // EXTENSION POINT: reusable for any "falls from an offset" effect.
      let fallDX = 0;
      let fallDY = 0;
      if (
        impactIdx !== Infinity &&
        (spriteSet.originOffsetX || spriteSet.originOffsetY)
      ) {
        // 0 at spawn → 1 at impact. Uses continuous progress (not the discrete
        // frame index) for smooth motion. impactProgress is the fraction of the
        // animation spent falling.
        const impactProgress = impactIdx / frames;
        const fallT =
          impactProgress > 0
            ? Math.min(1, Math.max(0, effect.progress / impactProgress))
            : 1;
        fallDX = (spriteSet.originOffsetX ?? 0) * (1 - fallT);
        fallDY = (spriteSet.originOffsetY ?? 0) * (1 - fallT);
      }
```

Then apply `fallDX/fallDY` to the draw position. Locate the existing destination coordinates used in the `drawImage` call (the anchor + scaled `offsetX/offsetY`, near :3088-3093) and add `fallDX`/`fallDY` to the x/y respectively. For example, if the code computes `const drawX = anchorX + (spriteSet.offsetX ?? 0) * scale;` add `+ fallDX` (and `+ fallDY` for `drawY`). Keep the existing centering and scale math intact — only add the fall delta.

Implementer note: `frames` and `frameIndex` are already in scope from the existing frame-stepping logic (`frameIndex = Math.min(frames-1, Math.floor(effect.progress * frames))`). Reuse them; do not recompute.

- [ ] **Step 4: Type-check.**

Run: `cd client/src/game-portal && npx vue-tsc -b`
Expected: no new errors.

- [ ] **Step 5: Manual visual verification** (see Task 12 for how to launch). Cast Meteor on a group of units. Confirm:
  - The meteor sprite appears in the upper-right and falls diagonally to the target, rendered **in front of** units during the fall.
  - At impact the explosion/crater renders **behind** units (a unit standing on the crater is drawn over it).
  - Enemies in the zone take an impact hit then tick fire damage for the burn duration.

- [ ] **Step 6: Commit.**

```bash
git add client/src/game-portal/src/game/rendering/CanvasRenderer.ts
git commit -m "feat(meteor): per-frame render layering + fall-offset for effects"
```

---

## Task 11: Clean up the unused projectile asset (optional)

Meteor is an effect, not a projectile. The duplicate `assets/projectiles/meteor.png` would otherwise be auto-registered as a (broken, 16-wide-but-not-square-per-frame → single-static-frame) projectile variant. It's harmless (nothing references variant `meteor`) but misleading.

**Files:**
- Delete: `client/src/game-portal/src/assets/projectiles/meteor.png`

- [ ] **Step 1: Remove the file.**

Run: `rm "client/src/game-portal/src/assets/projectiles/meteor.png"`

- [ ] **Step 2: Type-check + confirm no reference.**

Run: `cd client/src/game-portal && npx vue-tsc -b`
Expected: clean. (Grep first for the literal `"meteor"` projectile variant usage to be safe; there should be none.)

- [ ] **Step 3: Commit.**

```bash
git add -A client/src/game-portal/src/assets/projectiles/
git commit -m "chore(meteor): remove unused projectile spritesheet (meteor is an effect)"
```

---

## Task 12: Full verification

- [ ] **Step 1: Server test suite.**

Run: `cd server && go test ./...`
Expected: PASS.

- [ ] **Step 2: Server vet/build.**

Run: `cd server && go vet ./internal/game/ && go build ./...`
Expected: clean.

- [ ] **Step 3: Client type-check.**

Run: `cd client/src/game-portal && npx vue-tsc -b`
Expected: clean.

- [ ] **Step 4: End-to-end in the running app.** Use the project `run`/`verify` skill (or `start.bat`) to launch server + client. As an Arch Mage with Meteor learned:
  - Cast on **enemy unit** and on **empty ground** — both must fire at that location (point-cast routing).
  - Confirm the wind-up (0.8s cast), the diagonal fall from the upper-right, front-of-units during fall, behind-units at impact, the impact AoE, and ~4s of burn ticks in the zone (walk an enemy in/out to confirm the zone only damages who is currently inside).
  - Confirm mana (40) is spent only on completion and the 12s cooldown wipe shows on the action button.

- [ ] **Step 5: Determinism sanity.** The hazard tick uses only `dt` countdowns and iterates `s.Units` (no wall-clock, no unseeded RNG, no map-iteration-driven outcome). Confirm no `time.Now()`/`math/rand` was introduced in `ground_hazard.go`.

- [ ] **Step 6: Final commit** (if any lint/format fixes were needed).

```bash
git add -A
git commit -m "chore(meteor): verification pass fixes"
```

---

## Self-Review (checked against the spec)

**Spec coverage:**
- Target enemy or ground → `targetsPoint:true` (Task 2), routes both click types (existing client behavior). ✔
- Meteor appears upper-right, falls diagonally, not from caster → effect anchored at impact point + `originOffsetX/Y` fall interpolation (Tasks 8, 10). ✔
- Immediate AoE impact damage → `applyHazardImpactLocked` at `impactDelay` (Task 3). ✔
- Lingering burning area, DoT while enemies remain → `GroundHazard` burn phase re-evaluates who is in `burnRadius` each tick (Task 3). ✔
- Per-frame render layering (frames 1–6 above, 7+ below) → `impactFrame` manifest field + split draw pass (Tasks 9, 10). ✔ Data-driven, generic. ✔
- All values configurable: manaCost, cooldown, castTime, impact delay/travel, impact damage, impact radius, burn duration, burn dmg/tick, burn tick interval, burn radius, spawn offset → all in `meteor.json` except spawn offset (visual-only → effect `sprites.json`). Task 2 + Task 8. ✔
- Registered in Arch Mage registry → Task 6. ✔
- Reuse existing AoE + DoT-style systems → `applyAbilitySplashDamageLocked` reused; hazard modeled on trap/arcane-orb patterns. ✔
- Extend projectile/effect rendering for offset origin → done in the effect system (projectiles can't render below units). ✔
- Avoid hardcoded Meteor rendering → all via generic manifest fields + a generic `ImpactDelaySeconds`-triggered server branch. ✔
- Comment extension points for future spells → done in `ground_hazard.go`, the cast branch, and the renderer/manifest. ✔

**Type consistency:** `impactFrame`/`originOffsetX`/`originOffsetY` names match across `sprites.json`, `EffectManifest`, `EffectSpriteSet`, registry loop, and renderer. Server fields `ImpactDelaySeconds`/`BurnDurationSeconds`/`BurnDamagePerTick`/`BurnTickIntervalSeconds`/`BurnRadius` match across `AbilityDef`, JSON keys, `spawnGroundHazardLocked`, and tests. `GroundHazard` field names consistent between `ground_hazard.go` and `ground_hazard_test.go`.

**Known judgment calls surfaced for the user (see message).**
