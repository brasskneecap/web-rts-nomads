# Unit-Types Editor v2 — Phase 4: Per-Facing Attack Origins

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An author can drag a crosshair on the sprite preview to set, per facing, exactly where a unit's projectiles/spells/beams leave its body — and see the result by firing a test shot — with zero change to any unit that hasn't been authored.

**Architecture:** A new authored `attackOrigin` block (`{ default?, byFacing? }`) rides on `UnitDef` as a pass-through JSON field (like `bounds`/`shadow`/`attackVisual`). A pure resolver `getResolvedUnitAttackOrigin(def, facing)` returns the authored lift or `null`. The renderer consults it *first* in the projectile/beam origin chain, and — because origins are now facing-dependent — **snapshots each projectile's origin lift once at first sight** so a unit turning mid-flight can't drag an in-flight shot sideways. Absent authoring, the resolver returns `null` and every unit renders exactly as it does today.

**Tech Stack:** Go 1.22 (`server/`), Vue 3 + TS SPA (`client/src/game-portal`, vitest).

**Spec:** `docs/superpowers/specs/2026-07-13-unit-types-editor-v2-design.md` §6 (D3).

**Phase 4 of 5.** Phases 1–3 (factions/stats, runtime sprite overlay + preview, browser packer + ingest) are complete. Phase 5 (path entities) follows and will extend attack-origin authoring to promotion paths.

---

## What the code actually does today (verified — this shapes everything)

- **`attackVisual.originX/originY` is dead for any unit with art.** `getProjectileOriginLift` ([CanvasRenderer.ts:3608](../../client/src/game-portal/src/game/rendering/CanvasRenderer.ts#L3608)) returns `spriteBodyCenterLift()` for sprite-backed units, and that helper **always returns `x: 0`** with a geometrically-derived `y` ([CanvasRenderer.ts:3560-3576](../../client/src/game-portal/src/game/rendering/CanvasRenderer.ts#L3560-L3576)). The authored `attackVisual.originX/originY` is only reached for sprite-less procedural units. So today **every sprite unit fires from its horizontal center** — there is no way to author a bow-hand offset. That's the gap Phase 4 fills.
- **The origin lift is facing-independent today.** The per-frame cache is keyed `unitType|path` ([CanvasRenderer.ts:3161](../../client/src/game-portal/src/game/rendering/CanvasRenderer.ts#L3161)), so a unit turning mid-flight does not move its in-flight projectiles' origins. Per-facing origins **introduce** that possibility — hence the per-projectile snapshot (§Task 3) is a *requirement*, not a nicety.
- **`beamOrigin` in `sprites.json` is dead** — declared in the TS types, authored by zero manifests, always resolves to `{0,0}` ([CanvasRenderer.ts:3383](../../client/src/game-portal/src/game/rendering/CanvasRenderer.ts#L3383)). Folding beams into `attackOrigin` is therefore a no-op replacement, not a behavior change.
- **The server drops unknown JSON keys.** `UnitDef` only preserves `attackVisual`/`bounds`/`shadow` because each is an explicit `json.RawMessage` field ([unit_defs.go:89-107](../../server/internal/game/unit_defs.go#L89-L107)). A new `attackOrigin` key would be **silently discarded on save** unless it gets the same treatment. That's Task 1.

## The non-negotiable invariant: ZERO visual diff on day one

`attackOrigin` stays absent until the author drags the crosshair. `getResolvedUnitAttackOrigin` returns `null` for an unauthored unit, and the renderer falls through to **exactly today's chain** (`spriteBodyCenterLift → attackVisual → default const`). No unit's projectiles move until someone authors an origin for it. Every task below preserves this; the E2E proves it.

---

## Global Constraints

- **Do not run `git commit` or `git add`.** Each task ends with a **Checkpoint**.
- Go from `server/`; client from `client/src/game-portal`. Client typecheck is **`npx vue-tsc -b`**.
- `gofmt -l` flags the whole checkout (CRLF) — gates are `go vet` / `go build`.
- **No literal `cursor:` declarations** in component CSS (`cursor: not-allowed` on forbidden states only).
- **No simulation change.** The server's projectile origin stays `attacker.X/attacker.Y`; everything here is a client-side visual lift. Deterministic sim is untouched.
- Do not modify the item editor, the old map editor, or the abilities editor.
- Phase 4 targets **base units only**. Promotion-path attack origins come with Phase 5's path editor.

---

### Task 1: Server — `attackOrigin` pass-through field on `UnitDef`

**Files:**
- Modify: `server/internal/game/unit_defs.go` (add the field beside `AttackVisual`)
- Modify: `server/internal/game/unit_persistence_test.go` (lossless round-trip test)

**Interface:** `UnitDef` gains `AttackOrigin json.RawMessage json:"attackOrigin,omitempty"` — an opaque client-render blob the server never reads, exactly like `Bounds`/`Shadow`/`AttackVisual`.

- [ ] **Step 1: Write the failing test**

Append to `server/internal/game/unit_persistence_test.go`:

```go
func TestSaveUnitDef_LosslessAttackOrigin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_CATALOG_DIR", dir)
	def := UnitDef{
		Type: "origin_unit", Faction: "human", Name: "Origin", HP: 1, Damage: 1,
		AttackRange: 1, AttackSpeed: 1, MoveSpeed: 1,
		AttackOrigin: json.RawMessage(`{"default":{"x":3,"y":-40},"byFacing":{"east":{"x":14,"y":-30}}}`),
	}
	if err := SaveUnitDef(&def); err != nil {
		t.Fatalf("SaveUnitDef: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "human", "origin_unit", "origin_unit.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var round UnitDef
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(round.AttackOrigin) == 0 {
		t.Fatal("attackOrigin lost on round-trip")
	}
	var got struct{ Default struct{ X, Y float64 } }
	if err := json.Unmarshal(round.AttackOrigin, &got); err != nil || got.Default.X != 3 || got.Default.Y != -40 {
		t.Fatalf("attackOrigin not preserved verbatim: %s", round.AttackOrigin)
	}
}
```

- [ ] **Step 2: Run to verify it fails** — `cd server && go test ./internal/game/ -run TestSaveUnitDef_LosslessAttackOrigin -count=1` → FAIL (`unknown field AttackOrigin`).

- [ ] **Step 3: Add the field**

In `unit_defs.go`, immediately after the `AttackVisual` field (~line 89):

```go
	// AttackOrigin is optional per-unit, per-facing tuning of where a
	// projectile / spell / beam visually leaves the unit's body (screen-space
	// offsets from unit.x/unit.y). Shape: {default?:{x,y}, byFacing?:{<dir>:{x,y}}}.
	// Client-only render config, passed through as-is; the server never reads it.
	// Absent ⇒ the client derives the origin geometrically, exactly as before.
	AttackOrigin json.RawMessage `json:"attackOrigin,omitempty"`
```

- [ ] **Step 4: Run to verify it passes + gates**

- `cd server && go test ./internal/game/ -run 'TestSaveUnitDef|TestValidateUnitDef' -count=1` → PASS (existing lossless/round-trip tests still green)
- `cd server && go build ./... && go vet ./internal/game/` → clean

- [ ] **Step 5: Checkpoint (do not commit)**

---

### Task 2: Client — `attackOrigin` model + pure resolver

**Files:**
- Modify: `client/src/game-portal/src/game/maps/unitDefs.ts` (types + `getResolvedUnitAttackOrigin`)
- Create: `client/src/game-portal/src/game/maps/unitAttackOrigin.test.ts`
- Modify: `client/src/game-portal/src/game/units/unitEditorForm.ts` (model `attackOrigin` so the editor can bind it)
- Modify: `client/src/game-portal/src/game/units/unitEditorForm.test.ts`

**Interfaces:**
- `type UnitOriginPoint = { x: number; y: number }`
- `type UnitAttackOrigin = { default?: UnitOriginPoint; byFacing?: Partial<Record<UnitDirection, UnitOriginPoint>> }`
- `function getResolvedUnitAttackOrigin(def: UnitDef | null | undefined, facing: UnitDirection | undefined): UnitOriginPoint | null` — authored lift or `null`.
- `attackOrigin?: UnitAttackOrigin` added to `UnitDef` (render-time type) and to `AuthoredUnitDef` + `MODELED_KEYS` (editor form), so it round-trips as a typed field instead of riding in `remainder`.

**The resolver — precedence, and `null` = "not authored, use today's geometry":**

- [ ] **Step 1: Write the failing test**

Create `unitAttackOrigin.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { getResolvedUnitAttackOrigin } from './unitDefs'

const def = (attackOrigin: any) => ({ type: 't', faction: 'human', attackOrigin }) as any

describe('getResolvedUnitAttackOrigin', () => {
  it('returns null when unauthored (renderer keeps today\'s geometry)', () => {
    expect(getResolvedUnitAttackOrigin(def(undefined), 'east')).toBeNull()
    expect(getResolvedUnitAttackOrigin(def({}), 'east')).toBeNull()
    expect(getResolvedUnitAttackOrigin(null, 'east')).toBeNull()
  })

  it('byFacing wins over default for that facing', () => {
    const d = def({ default: { x: 0, y: -30 }, byFacing: { east: { x: 14, y: -28 } } })
    expect(getResolvedUnitAttackOrigin(d, 'east')).toEqual({ x: 14, y: -28 })
  })

  it('falls back to default for a facing with no override', () => {
    const d = def({ default: { x: 2, y: -30 }, byFacing: { east: { x: 14, y: -28 } } })
    expect(getResolvedUnitAttackOrigin(d, 'north')).toEqual({ x: 2, y: -30 })
  })

  it('uses default when facing is undefined', () => {
    const d = def({ default: { x: 2, y: -30 }, byFacing: { east: { x: 14, y: -28 } } })
    expect(getResolvedUnitAttackOrigin(d, undefined)).toEqual({ x: 2, y: -30 })
  })

  it('rounds to integer pixels', () => {
    expect(getResolvedUnitAttackOrigin(def({ default: { x: 2.6, y: -30.4 } }), undefined)).toEqual({ x: 3, y: -30 })
  })
})
```

- [ ] **Step 2: Run to verify it fails** — `cd client/src/game-portal && npm run test -- unitAttackOrigin` → FAIL (symbol missing).

- [ ] **Step 3: Implement** in `unitDefs.ts` — add the two types, add `attackOrigin?: UnitAttackOrigin` to the `UnitDef` type, and:

```ts
export function getResolvedUnitAttackOrigin(
  def: UnitDef | null | undefined,
  facing: UnitDirection | undefined,
): UnitOriginPoint | null {
  const ao = def?.attackOrigin
  if (!ao) return null
  const pick = (facing && ao.byFacing?.[facing]) || ao.default
  if (!pick) return null
  return { x: Math.round(pick.x), y: Math.round(pick.y) }
}
```

(`UnitDirection` imports from `../rendering/unitSprites`.)

- [ ] **Step 4: Model it in the editor form** — in `unitEditorForm.ts`, add `attackOrigin?: UnitAttackOrigin` to `AuthoredUnitDef` and add `'attackOrigin'` to `MODELED_KEYS` so `formFromDef`→`saveRequestFromForm` round-trips it as a typed field. Extend the existing lossless round-trip test in `unitEditorForm.test.ts` to include an `attackOrigin` in the full-def fixture and assert it survives.

- [ ] **Step 5: Gates**

- `cd client/src/game-portal && npm run test -- unitAttackOrigin unitEditorForm` → PASS
- `cd client/src/game-portal && npx vue-tsc -b` → clean

- [ ] **Step 6: Checkpoint (do not commit)**

---

### Task 3: Renderer — authored origin + per-projectile snapshot + facing getter

**This is the load-bearing renderer change. Two sub-parts, both easy to get subtly wrong.**

**Files:**
- Modify: `client/src/game-portal/src/game/rendering/unitAnimation.ts` (expose `currentDirection`)
- Modify: `client/src/game-portal/src/game/rendering/CanvasRenderer.ts` (origin precedence + per-projectile snapshot; beam origin)
- Create: `client/src/game-portal/src/game/rendering/attackOriginResolve.test.ts` (pure precedence unit)

**3a — `unitAnim.currentDirection(unitId)`**

The controller already stores per-unit facing (`UnitAnimState.direction`, set in `sample()`). Add a read-only getter:

```ts
// The facing last computed for this unit by sample(), or undefined if the unit
// has not been sampled yet this session. Used by the renderer to pick a
// per-facing projectile origin at the moment of firing.
currentDirection(unitId: number): UnitDirection | undefined {
  return this.states.get(unitId)?.direction
}
```

(Match the real field name of the per-unit state map — read the file.)

**3b — extract a pure origin resolver so it's testable**

The renderer can't be unit-tested (canvas), so extract the precedence into a pure module `attackOriginResolve.ts` that CanvasRenderer calls:

```ts
// resolveProjectileOriginLift returns the screen-space origin offset for a
// projectile, in strict precedence:
//   1. authored attackOrigin for the owner's facing (or its default)
//   2. the caller-supplied geometric fallback (spriteBodyCenterLift / attackVisual)
// Authored wins; everything else is exactly today's behavior. Keeping this pure
// lets it be tested without a canvas.
export function resolveProjectileOriginLift(
  authored: { x: number; y: number } | null,
  geometricFallback: { x: number; y: number },
): { x: number; y: number } {
  return authored ?? geometricFallback
}
```

Test `attackOriginResolve.test.ts`: authored wins; null → fallback. (Trivial but pins the contract and documents the precedence.)

**3c — wire it into the projectile draw loop with a per-projectile snapshot**

Add an INSTANCE field (persists across frames — NOT the per-frame cache):

```ts
// Origin lift snapshotted per projectile at first sight. Origins are now
// facing-dependent, so recomputing each frame from the owner's CURRENT facing
// would drag an in-flight shot sideways when the owner turns. Snapshot once,
// keep for the projectile's life, evict when it disappears.
private projectileOriginLiftById = new Map<number, { x: number; y: number }>()
```

In the projectile loop ([CanvasRenderer.ts:3165](../../client/src/game-portal/src/game/rendering/CanvasRenderer.ts#L3165)), replace the per-frame `getProjectileOriginLift` call with a snapshot lookup:

```ts
    for (const proj of projectiles) {
      let originLift = this.projectileOriginLiftById.get(proj.id)
      if (!originLift) {
        const owner = unitsById.get(proj.ownerUnitId)
        const facing = owner ? this.unitAnim.currentDirection(owner.id) : undefined
        const authored = getResolvedUnitAttackOrigin(
          owner?.unitType ? UNIT_DEF_MAP.get(owner.unitType) : undefined,
          facing,
        )
        originLift = resolveProjectileOriginLift(
          authored,
          this.getProjectileOriginLift(owner, originLiftCache), // existing geometric chain
        )
        this.projectileOriginLiftById.set(proj.id, originLift)
      }
      // ... existing targetLift, originX = proj.originX + originLift.x, etc.
```

After the loop, prune dead entries:

```ts
    const liveProjectileIds = new Set(projectiles.map((p) => p.id))
    for (const id of this.projectileOriginLiftById.keys()) {
      if (!liveProjectileIds.has(id)) this.projectileOriginLiftById.delete(id)
    }
```

> **Dead-owner correctness:** once snapshotted, the origin no longer needs the owner — so a projectile whose owner died mid-flight keeps a stable origin instead of snapping to the default. Confirm `proj.id` is stable across frames (it is — the server assigns projectile ids). If a `pierce`/AoE projectile has no owner, `owner` is undefined → `authored` null → geometric fallback → today's behavior.

**3d — beam origin: replace the dead `beamOrigin`**

At [CanvasRenderer.ts:3383-3386](../../client/src/game-portal/src/game/rendering/CanvasRenderer.ts#L3383-L3386), the beam adds `casterSpriteSet.beamOrigin` (always `{0,0}`). Replace it with the authored attackOrigin (facing-aware, via the caster's `currentDirection`), falling back to `{0,0}` when unauthored — so beams gain the same authorable origin and nothing moves until authored:

```ts
        const casterFacing = this.unitAnim.currentDirection(caster.id)
        const authoredBeam = getResolvedUnitAttackOrigin(UNIT_DEF_MAP.get(caster.unitType), casterFacing)
        const beamOrigin = authoredBeam ?? { x: 0, y: 0 }
        originX = caster.x + originLift.x + beamOrigin.x
        originY = caster.y + originLift.y + beamOrigin.y
```

> Note this ADDS the authored origin to the existing `originLift` for beams (matching the old `beamOrigin` semantics), whereas for projectiles the authored origin REPLACES the geometric lift. That asymmetry is intentional and preserves each path's current default; call it out in a comment so a future reader doesn't "unify" them and shift every beam.

- [ ] **Step 1: Write the pure resolver test** (`attackOriginResolve.test.ts`) → run → fail.
- [ ] **Step 2: Implement `attackOriginResolve.ts` + `currentDirection`** → test passes.
- [ ] **Step 3: Wire the projectile snapshot + beam origin into `CanvasRenderer.ts`.**
- [ ] **Step 4: Gates**
  - `cd client/src/game-portal && npm run test` → no new failures
  - `cd client/src/game-portal && npx vue-tsc -b && npm run build` → clean
- [ ] **Step 5: Checkpoint (do not commit)** — note: the *visual* correctness (snapshot, facing) is proven in Task 5's E2E; unit tests cover the pure resolver only.

---

### Task 4: Editor — the crosshair origin editor on the preview

**Files:**
- Modify: `client/src/game-portal/src/components/UnitSpritePreview.vue` (crosshair overlay + origin state) OR a new child component mounted in the Preview section
- Modify: `client/src/game-portal/src/components/UnitTypeEditorPanel.vue` (bind `form.attackOrigin`, test-fire action)

**What it does (spec §6.4):**
- A draggable crosshair overlaid on the main preview canvas, plus numeric x/y inputs, editing the origin for the **currently-selected facing**.
- Switching the facing selector switches which `byFacing` entry is edited; an **"apply to all facings"** action writes `default`.
- The crosshair's initial position for an unauthored unit is the **current derived lift** (call the same geometry the renderer uses, or approximate via the preview's known sprite box) so "nothing moves until you drag" is visibly true — the crosshair starts where the projectile currently leaves.
- A **"fire test projectile"** button: play the attacking animation and animate a ghost dot from the authored origin toward a dummy target, so the author sees the real result, not a static point.
- Writes into `form.attackOrigin` (`{ default?, byFacing? }`), which round-trips via Task 2's modeled field and saves through the existing unit Save.

**Implementation notes:**
- The preview canvas already draws at `UNIT_SPRITE_SCALE` with a known box (`MAIN_BOX`). Map a mouse position on the canvas → a unit-space `{x,y}` offset (invert the same transform the preview uses to place the sprite). Keep this math in one small helper; it's the fiddly part.
- The crosshair edits are per-facing: the preview already has a `direction` ref — reuse it as the facing being authored.
- **No literal `cursor:` declarations.** A draggable handle may use `cursor: grab`/`grabbing`? — NO, those are literal cursors and the project bans them; rely on the global game cursor, or use a visual affordance (a ring) instead of a cursor change.
- Test-fire is a preview-only animation; do NOT touch combat/sim.

- [ ] **Step 1: Build the crosshair overlay + numeric inputs + facing binding.**
- [ ] **Step 2: Add "apply to all facings" and "fire test projectile".**
- [ ] **Step 3: Bind to `form.attackOrigin` in the panel; confirm Save persists it.**
- [ ] **Step 4: Gates** — `npx vue-tsc -b && npm run build && npm run test`; `grep -n "cursor:"` on the touched components → only `not-allowed`.
- [ ] **Step 5: Checkpoint (do not commit)**

---

### Task 5: Verification sweep + END-TO-END proof

**The renderer's visual behavior (snapshot, facing, zero-diff) can only be proven in a browser.**

- [ ] **Step 1: Full gates** — server (`go vet/build/test ./...`) + client (`npm run test && npx vue-tsc -b && npm run build`) all green.

- [ ] **Step 2: Zero-diff proof (the invariant).** With the dev stack, place an existing sprite unit (e.g. archer) and playtest → its projectiles leave from exactly where they do today. Confirm NO authored `attackOrigin` exists on any shipped unit (`git grep -l attackOrigin server/internal/game/catalog/` is empty). This proves the field is inert until used.

- [ ] **Step 3: Author + fire (the feature).** In the editor, select archer → Preview → drag the origin crosshair for the `east` facing to the bow hand → "fire test projectile" shows the ghost leaving the hand → Save → place an archer facing east and playtest → its arrows now leave the authored point. Author only `east`; confirm other facings still use the derived default (drag `default` too and confirm all facings shift).

- [ ] **Step 4: The turning-owner snapshot.** Fire a slow projectile, then turn the shooter while it's in flight → the in-flight arrow's origin does NOT slide (it was snapshotted at fire time). This is the specific bug the snapshot prevents; verify it by eye.

- [ ] **Step 5: Dead-owner.** Kill a shooter while its projectile is mid-flight → the projectile completes without a crash and without its origin snapping.

- [ ] **Step 6: Hygiene + report.** `git status` shows no stray catalog art; only intended files changed. Report gates, the four E2E proofs (esp. zero-diff and the snapshot), and any deviation.
