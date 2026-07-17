# Composable, Event-Driven Ability Authoring — Design & Phase 1 Plan

> **Status:** Phase 1 (investigation + technical plan) — *for review before broad implementation.*
> **Scope:** the ability system only (Go runtime, catalog JSON, TS editor, in-editor preview). No speculative rewrites outside abilities.

**Goal:** Replace the growing set of optional mechanic-specific ability fields with a composable **trigger → action** model (with target queries, persistent objects, presentation, and a deterministic trace-driven preview), while preserving legacy JSON, the remainder round-trip, the single validation gate, and the authoritative runtime seams.

**Catalog-wide scope (not just the two fixtures).** Meteor and Greater Heal are *acceptance anchors* named by the spec, but the model, executor, legacy compiler, and editor must cover **every ability in the catalog**. All 11 current abilities are enumerated with their compile targets in §4.1; the compiler must load and the editor must edit all of them, with none left un-loadable at any migration step. Three (`arcane_orb` moving zone, `siphon_life` channel, `arcane_missiles` charge-fire) keep their existing runtime paths initially and are modeled as `custom`/compile-to-existing until composable equivalents are proven.

---

## 1. Current architecture & affected files

### 1.1 The three existing layers (unchanged conceptually)

| Layer | File | Role |
|---|---|---|
| Go runtime def | `server/internal/game/ability_defs.go` — `AbilityDef` (`:108`) | Authoritative struct; loaded, validated, drives sim. |
| Catalog JSON | `server/internal/game/catalog/abilities/<id>/<id>.json` | One dir per id; overlay-writable copy at `$ABILITY_CATALOG_DIR`. |
| TS editor model | `client/.../game/abilities/abilityEditorForm.ts` — `AuthoredAbilityDef` / `AbilityEditorForm` | Superset with `remainder` bag + index signature. |

### 1.2 Persistence & catalog (reuse as-is)

- Embed base: `//go:embed all:catalog/abilities` → `abilityDefsByID` (`ability_defs.go:559`).
- Overlay: `runtimeAbilities` map (`ability_persistence.go:22`); `getAbilityDef` / `ListAbilityDefs` consult overlay first, embed second.
- Save: `SaveAbilityDef` (`ability_persistence.go:45`) → `validateAbilityDef` → write `<dir>/<id>/<id>.json` → `runtimeAbilities[id]=def`.
- Startup rehydrate: `LoadPersistedAbilitiesIntoOverlay` (`main.go:60`).
- Delete/reset: `DeleteAbilityOverride` (`ability_persistence.go:136`); embed-backed → reset, overlay-only → vanish.
- `CastRange.MarshalJSON` round-trips the `"match_attack_range"` sentinel (`ability_defs.go:76`).

### 1.3 Validation (single gate — preserve the principle)

`validateAbilityDef(*AbilityDef) error` (`ability_defs.go:523`) — shared by loader, `SaveAbilityDef`, startup rehydrate, and `SaveEditorAbility`. Validates + normalizes numeric defaults in place.

### 1.4 HTTP + editor API (extend, don't replace)

- Read GETs: `/catalog/abilities` (returns `abilityCatalogEntry{AbilityDef; GeneratedDescription}`, `router.go:41`), `/catalog/projectiles|effects|autocast-selectors|ability-categories|damage-types` (`router.go:46-103`), `/catalog/units`.
- Mutations: `POST /abilities`, `POST /abilities/{id}/image`, `DELETE /abilities/{id}` (`editor_handlers.go:292-353`).
- TS API: `abilityEditorApi.ts` (`fetchAuthoredAbilityDefs`, `saveEditorAbility`, `deleteEditorAbility`, `uploadAbilityIcon`, `EditorValidationError`, catalog fetchers).

### 1.5 Runtime execution seams (the executor will CALL these, not reimplement)

| Concern | Function | File |
|---|---|---|
| Cast lifecycle | `beginAbilityCastLocked` / `beginAbilityCastAtPointLocked` / `tickUnitCastLocked` / `resolveAbilityCastLocked` / `resolveAbilityCastAtPointLocked` | `ability_cast.go` |
| Per-target dispatch | `resolveAbilityCastOnTargetLocked` | `ability_cast.go:497` |
| Single-target damage | `applyUnitDamageWithSourceLocked(target,dmg,DamageSource) int` | `perks_defense.go:49` |
| AoE damage | `applyAbilitySplashDamageLocked(ownerUnitID,ownerPlayerID,cx,cy,radius,dmg,type,primaryID)` | `state_combat.go:428` |
| Heal | `applyClericHealLocked` (overheal-aware) | `ability_cast.go:515` |
| Summon | `spawnSummonedUnitLocked(caster,def)` | `ability_summon.go:38` |
| Projectile | `fireAbilityProjectileLocked(caster,target,def,eff)` | `projectile.go:341` |
| Ground hazard/zone | `spawnGroundHazardLocked` / `tickGroundHazardsLocked` (`GroundHazard` struct `:35`) | `ground_hazard.go` |
| Slow/CC | `applyProcSlowLocked(targetID,mult,dur,type)` (routes cold→chill) | `combat_ai_cc.go:113` |
| Stun / burn DoT | `ApplyStunLocked`, `applyProcBurnLocked` | `combat_ai_cc.go` |
| Pull/force | `applyPullInRadiusLocked(caster,cx,cy,radius,strength,dur) int` | `spell_pull.go:43` |
| Multi-target select | `buildCastTargetSetLocked(caster,def,primary) []*Unit` (asc HP%, cap TargetCount) | `autocast_selectors.go:236` |
| Selectors | `resolveAutoCastTargetLocked`, `selectLowestHPPercentageAllyInRange`, `selectClosestEnemyInRange`, … | `autocast_selectors.go` |
| Resolve helpers | `getUnitByIDLocked`, `distanceSquared`, `unitsHostileLocked`, `unitsFriendlyLocked` | `state_helpers.go` |
| Effective magnitudes | `effectiveSpellLocked` (folds perks/buffs/items) | `spell_modifier.go:252` |

**Determinism:** ability/proc/hazard/pull/summon paths contain **no RNG**; five seeded streams exist (`s.rngCombat` etc.) via `NewGameStateWithSeed(cfg,seed)` (`state.go:1301`). `*Locked` = caller holds `s.mu`. Targets stored as **IDs**, re-resolved + re-validated every tick.

### 1.6 Status/timed-effect systems (what "apply_status" maps onto)

There is **no unified Buff registry**. Timed effects are individual fields on `Unit` (`StunnedRemaining`, `SlowedRemaining/Multiplier`, `ColdSlowed*`, `PullRemaining/*`) and on `Unit.PerkState` (`UnitPerkState` in `perks.go`: `BurnStacks`, `MarkStacks`, `ShieldPools`, `AmplifyDamageRemaining`, prayers, etc.), each decayed in `Update()`. **Implication:** `apply_status` in Phase 3 maps to the *existing named* CC/DoT primitives (slow/stun/burn). A general author-defined status object with arbitrary nested triggers is a **later** capability (see §7 risk R3); the model supports it but Phase 3 only wires the primitives that already exist.

### 1.7 Animation / presentation / preview (the net-new surface)

- Animation: freeform server `unit.Status` string → client slot via `pickAnimation` (`unitAnimation.ts:343`); frame index = `floor(elapsed/frameDurationMs)`. **No named markers exist.**
- Effects: `EffectDef{ID,SpritePath,Duration,Anchor}` (`effect_defs.go:74`) + client manifest `EffectManifest{frameWidth,frameHeight,frames,impactFrame,originOffsetX/Y,loop,displayScale}` (`effectSprites.ts:9`). `impactFrame` is the only frame→semantics coupling (above/below-units render split).
- Renderer: `CanvasRenderer(canvas,state,camera)`; `render()` takes **no time arg**, samples `performance.now()` internally; z-order is `anchorY`-derived (`drawUnits` `:2493`); effects have a per-frame `above/below` split. **No general render-layer property.**
- Preview: `AbilityAnimationViewer.vue` builds a synthetic `GameState`, fakes a **seconds-based** timeline, drives `requestAnimationFrame`. Quirks: "casting-fallback" (`ANIMATION_FALLBACK.casting='attacking'`) and "facing-prime" (double-render nudge to lock facing).

**Net-new work:** (N1) named animation-marker model, (N2) `change_render_layer` action + a render-layer field the renderer honors, (N3) deterministic/injectable clock threaded through `render()` + effect/projectile/overlay frame math.

---

## 2. Proposed Go data model

New file cluster under `server/internal/game/`: `ability_program.go` (types), `ability_program_registry.go` (action/trigger/condition registries), `ability_program_exec.go` (executor), `ability_program_compile.go` (legacy→program compiler), `ability_program_validate.go`, `ability_program_describe.go`, `ability_program_schema.go` (editor schema emitter).

### 2.1 Ability-level additions to `AbilityDef`

```go
// AbilityDef gains ONE new optional field. schemaVersion selects authority:
//   absent/1 → legacy flat fields are authoritative; Program is compiled on load (transient).
//   2        → Program is authoritative; legacy mechanic fields are dropped on convert.
type AbilityDef struct {
    // ... all existing fields unchanged (identity, cast-setup, legacy mechanic knobs) ...
    SchemaVersion int             `json:"schemaVersion,omitempty"`
    Program       *AbilityProgram `json:"program,omitempty"`
}
```

Identity + cast-setup fields (`ID`, `DisplayName`, `Type`, `Category`, `DamageType`, `Tags`, `Icon`, `Description`, `ManaCost`, `Cooldown`, `CastTime`, `CanTarget*`, `TargetsPoint`, `CastRange`, `SupportsAutoCast`, `AutoCastTargetSelector`, `DefaultAutoCast`, `CasterAnimation`) **stay top-level** — they are ability-level, not mechanic-level. The composable model replaces only the *mechanic* fields (`radius`, `impactDelaySeconds`, `burn*`, `chainCount`, `slow*`, `pull*`, `channel*`, `charge*`, `summon*`, `healAmount`, `damageAmount`, `damagePerSecond`, `targetCount`, effect refs).

### 2.2 Program + entry + trigger + action envelopes

```go
type AbilityProgram struct {
    Entry         AbilityEntryDef                 `json:"entry"`
    Triggers      []AbilityTriggerDef             `json:"triggers"`
    NamedTriggers map[string]AbilityTriggerDef    `json:"namedTriggers,omitempty"` // reusable, invoked by trigger_event
    Presentations []PresentationInstanceDef       `json:"presentations,omitempty"` // objects w/ marker triggers
    Remainder     map[string]json.RawMessage      `json:"-"`                       // round-trip unknown keys (see §6.3)
}

type AbilityEntryType string // self | unit | ground_point | direction | no_target | passive
type TargetRelation string   // self | ally | enemy | neutral

type AbilityEntryDef struct {
    Type      AbilityEntryType  `json:"type"`
    Relations []TargetRelation  `json:"relations,omitempty"` // only for Type==unit
    Range     CastRange         `json:"range"`               // reuse existing polymorphic type
}

type AbilityTriggerDef struct {
    ID         string                 `json:"id"`
    Name       string                 `json:"name,omitempty"`
    Type       TriggerType            `json:"type"`               // extensible enum (§2.4)
    Source     *ContextRef            `json:"source,omitempty"`   // which object owns/emits (zone/status/projectile/presentation)
    Timing     *TriggerTiming         `json:"timing,omitempty"`   // marker name, tick interval, frame, etc.
    Conditions []AbilityConditionDef  `json:"conditions,omitempty"`
    Actions    []AbilityActionDef     `json:"actions"`
}

type TriggerTiming struct {
    Marker       string  `json:"marker,omitempty"`       // on_animation_marker
    Frame        *int    `json:"frame,omitempty"`        // direct frame fallback
    TickInterval float64 `json:"tickInterval,omitempty"` // on_zone_tick / on_status_tick
    DelaySeconds float64 `json:"delaySeconds,omitempty"`
}

type AbilityActionDef struct {
    ID          string                     `json:"id"`
    Type        ActionType                 `json:"type"`                  // extensible enum (§2.5)
    DisplayName string                     `json:"displayName,omitempty"`
    Disabled    bool                       `json:"disabled,omitempty"`    // default enabled; only "disabled": true turns it off (IsEnabled())
    Conditions  []AbilityConditionDef      `json:"conditions,omitempty"`
    Target      *TargetQueryDef            `json:"target,omitempty"`      // OR a TargetRef via Input
    Input       map[string]ContextRef      `json:"input,omitempty"`       // named context inputs
    Outputs     map[string]string          `json:"outputs,omitempty"`     // action output -> named context key
    Config      json.RawMessage            `json:"config,omitempty"`      // action-specific; typed per registry (§3.2)
    Children    []AbilityTriggerDef        `json:"children,omitempty"`    // follow-up / on_action_complete etc.
}
```

### 2.3 Target query, condition, context reference

```go
type TargetSource string // caster | initial_target | previous_action_targets | current_event | named_context | source_object | all_in_scene
type TargetOrigin string // caster | initial_target | initial_target_position | cast_point | impact_position |
                         // current_event_position | projectile_position | zone_center | status_owner | summoned_unit | named_context_value
type TargetOrdering string // closest | farthest | lowest_health | lowest_health_percentage | highest_health | random | unit_id

type TargetQueryDef struct {
    Source               TargetSource   `json:"source"`
    Origin               TargetOrigin   `json:"origin,omitempty"`
    OriginRef            *ContextRef    `json:"originRef,omitempty"` // when Origin==named_context_value
    Relations            []TargetRelation `json:"relations,omitempty"`
    Filters              []TargetFilter `json:"filters,omitempty"`
    Radius               float64        `json:"radius,omitempty"`
    MinCount             int            `json:"minCount,omitempty"`
    MaxCount             int            `json:"maxCount,omitempty"` // 0 = unbounded
    Ordering             TargetOrdering `json:"ordering,omitempty"`
    IncludeInitialTarget bool           `json:"includeInitialTarget,omitempty"`
    ExcludeSource        bool           `json:"excludeSource,omitempty"`
    RequireLineOfSight   bool           `json:"requireLineOfSight,omitempty"`
    AliveState           string         `json:"aliveState,omitempty"` // alive | dead | any (default alive)
}

type ContextRef struct {
    Key  string `json:"key"`            // e.g. "impactPosition", "caster", "selectedTargets", or a named binding
}

type AbilityConditionDef struct {
    Type  ConditionType   `json:"type"`  // extensible enum
    Left  ContextRef      `json:"left"`
    Op    string          `json:"op"`    // eq|ne|lt|lte|gt|gte|has|not
    Right json.RawMessage `json:"right"`
}
```

### 2.4 Persistent objects (zone / status / projectile / presentation)

These are **configs carried in the action's `Config`** (e.g. `create_zone`'s config is a `ZoneDef`), each able to carry **nested triggers**. At runtime the executor spawns a live entity that holds the *compiled* trigger list.

```go
type ZoneAnchor string // ground | unit | object
type ZoneDef struct {
    Name          string              `json:"name,omitempty"`
    PositionRef   ContextRef          `json:"position"`     // origin of the zone
    Anchor        ZoneAnchor          `json:"anchor"`
    FollowsAnchor bool                `json:"followsAnchor,omitempty"`
    Radius        float64             `json:"radius"`
    Duration      float64             `json:"duration"`
    TickInterval  float64             `json:"tickInterval,omitempty"`
    OwnerRef      ContextRef          `json:"owner"`
    Presentation  string              `json:"presentation,omitempty"` // effect id
    Triggers      []AbilityTriggerDef `json:"triggers,omitempty"`     // on_created/on_tick/on_enter/on_exit/on_expire
}

type StatusDef struct {
    Name         string              `json:"name,omitempty"`
    TargetRef    ContextRef          `json:"target"`
    Duration     float64             `json:"duration"`
    TickInterval float64             `json:"tickInterval,omitempty"`
    Stacking     string              `json:"stacking,omitempty"` // none|refresh|stack
    MaxStacks    int                 `json:"maxStacks,omitempty"`
    SourceRef    ContextRef          `json:"source"`
    Presentation string              `json:"presentation,omitempty"`
    Triggers     []AbilityTriggerDef `json:"triggers,omitempty"` // on_applied/on_tick/on_damage_*/on_expire/on_removed
}

type ProjectileSpawnDef struct {
    SourceRef    ContextRef          `json:"source"`
    DestRef      ContextRef          `json:"destination"`
    ProjectileID string              `json:"projectile,omitempty"` // catalog projectile def
    Speed        float64             `json:"speed,omitempty"`
    Piercing     bool                `json:"piercing,omitempty"`
    Presentation string              `json:"presentation,omitempty"`
    Triggers     []AbilityTriggerDef `json:"triggers,omitempty"` // on_projectile_impact
}

type PresentationInstanceDef struct {
    ID          string              `json:"id"`
    Asset       string              `json:"asset"`          // effect id
    PositionRef ContextRef          `json:"position"`
    AttachRef   *ContextRef         `json:"attach,omitempty"`
    Scale       float64             `json:"scale,omitempty"`
    RenderLayer string              `json:"renderLayer,omitempty"` // in_front_of_units | behind_units | ground
    Animation   string              `json:"animation,omitempty"`
    Triggers    []AbilityTriggerDef `json:"triggers,omitempty"`    // on_animation_marker / on_action_complete
}
```

### 2.5 Extensible enums (registry-backed, not hardcoded switches)

```go
type TriggerType string  // on_cast_start on_cast_complete on_animation_marker on_projectile_impact
                         // on_zone_tick on_zone_enter on_zone_exit on_status_tick on_status_expire
                         // on_target_hit on_damage_dealt on_unit_death on_action_complete custom
type ActionType string   // select_targets store_targets filter_targets deal_damage restore_health
                         // apply_status remove_status create_zone launch_projectile summon_unit move_unit
                         // apply_force modify_resource trigger_event play_presentation play_sound
                         // change_render_layer camera_shake wait conditional repeat
type ConditionType string
```

Each `ActionType` registers a descriptor (§3.2). Adding an action = adding one registry entry (decode + execute + validate + describe + editor-schema), **no editor rewrite**.

---

## 3. Runtime execution plan

### 3.1 Compile-then-execute

```
AbilityDef (legacy flat OR schemaVersion:2 program)
        │  loadAbilityDefs / SaveAbilityDef
        ▼
compileAbilityProgram(def) → *compiledProgram   (in-memory; legacy synthesized, v2 parsed)
        ▼  cached on the def / overlay entry
runAbilityProgramLocked(s, ctx, compiled)        (executor; calls existing seams)
```

- `compiledProgram` holds resolved trigger tables keyed by `TriggerType` + object bindings, with `Config` decoded into concrete Go structs via the registry. Compilation happens once at load/save, not per cast.
- **Legacy abilities** (no program) are compiled by `ability_program_compile.go` (§4) into the *same* `compiledProgram` shape, so the executor has one code path.

### 3.2 Action registry (the composable core)

```go
type ActionDescriptor struct {
    Type     ActionType
    Decode   func(json.RawMessage) (ActionConfig, error)
    Execute  func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) ([]int, error) // returns output target IDs
    Validate func(cfg ActionConfig, scope ValidationScope) []ValidationIssue
    Describe func(cfg ActionConfig, ctx DescribeCtx) string
    Schema   ActionFieldSchema // drives the editor inspector (§9)
}
var actionRegistry = map[ActionType]ActionDescriptor{}
```

Executors are thin adapters over §1.5 seams. Examples:
- `deal_damage` → resolve targets → per target `applyUnitDamageWithSourceLocked(t, cfg.Amount, DamageSource{AttackerUnitID: ctx.CasterID, Kind:"ability", DamageType: cfg.Type})`; or `applyAbilitySplashDamageLocked` when the target query is a radius origin.
- `restore_health` → `applyClericHealLocked`.
- `create_zone` → generalize `spawnGroundHazardLocked` into `spawnZoneLocked` carrying a compiled `on_tick` trigger; `tickGroundHazardsLocked` → `tickZonesLocked` fires the nested trigger each interval (reuses `applyAbilitySplashDamageLocked` when the nested action is `deal_damage`, but is generic).
- `summon_unit` → `spawnSummonedUnitLocked`. `launch_projectile` → `fireAbilityProjectileLocked` (impact fires the projectile's nested `on_projectile_impact` trigger). `apply_force` → `applyPullInRadiusLocked`. `apply_status` → `applyProcSlowLocked` / `ApplyStunLocked` / `applyProcBurnLocked` (Phase 3 primitives).

### 3.3 RuntimeAbilityContext (typed, ID-based)

```go
type RuntimeAbilityContext struct {
    CasterID      int
    AbilityID     string
    CastID        int64
    InitialTarget int          // 0 = none
    CastPoint     Vec2
    EventPosition Vec2
    ImpactPosition Vec2
    ProjectileID  int
    ZoneID        string
    StatusKey     string
    StatusOwner   int
    SummonedUnit  int
    SourceObject  ContextValue
    HitTargets    []int
    Selected      []int
    Named         map[string]ContextValue // output bindings
    ElapsedTime   float64
    TickIndex     int
    Trace         *AbilityExecutionTrace  // nil in production, set in preview
}
```

All entity refs are **IDs**, re-resolved via `getUnitByIDLocked` + validated at point of use (per the ID-targeting invariant). The context surface exposed by each trigger type is gated (§8 invalid-context validation).

### 3.4 Where the executor hooks in

- `resolveAbilityCastLocked` / `resolveAbilityCastAtPointLocked` → build `RuntimeAbilityContext`, fire the `on_cast_complete` trigger set via `runAbilityProgramLocked`.
- Cast-time markers (`on_cast_start`) fire from `beginUnitCastingLocked`; animation markers fire from a new marker-tick check in `tickUnitCastLocked` (server maps marker→cast-time fraction; see N1/§7 R2).
- Zones/statuses/projectiles: their nested triggers fire from `tickZonesLocked` / status decay / `landProjectileLocked`.
- **Autocast + `EffectiveSpell`**: unchanged. `select_targets` reuses `buildCastTargetSetLocked`/selectors; magnitudes still resolved via `effectiveSpellLocked` where a legacy modifier field maps to an action config value.

### 3.5 Execution tracing

```go
type AbilityExecutionTrace struct{ Events []AbilityExecutionTraceEvent }
type AbilityExecutionTraceEvent struct {
    Time    float64      `json:"t"`
    Type    string       `json:"type"` // cast_started..validation_error (spec list)
    Path    string       `json:"path,omitempty"` // triggers[i].actions[j] — maps event→flow card
    Payload map[string]any `json:"payload,omitempty"`
}
```

The executor records events when `ctx.Trace != nil`. **Production runs pass `nil`** (zero overhead). The preview harness (§10) passes a real trace; it is the single source for the preview log + timeline markers.

---

## 4. Legacy compiler (Phase 4)

`compileLegacyAbility(def AbilityDef) *AbilityProgram` synthesizes a program from flat fields. It must handle **the whole catalog** — the compiler is the guarantee that no existing ability breaks.

### 4.1 Full catalog coverage (all 11 abilities)

| Ability | Mechanic | Compiled program | Reused seam(s) |
|---|---|---|---|
| `heal` | single heal | `on_cast_complete` → `select_targets(self+ally, max 1)` → `restore_health` → `play_presentation(healing_glow)` | `applyClericHealLocked` |
| `greater_heal` | multi heal | same, `select_targets` maxCount=`targetCount`, order lowest_hp_pct, includeInitialTarget | `buildCastTargetSetLocked`, `applyClericHealLocked` |
| `arcane_bolt` | single-target projectile | `on_cast_complete` → `launch_projectile(arcane_bolt→initial_target)`; proj `on_projectile_impact` → `deal_damage` | `fireAbilityProjectileLocked` |
| `fireball` | projectile + splash | `launch_projectile(fire_bolt)`; `on_projectile_impact` → `deal_damage(radius=100)` | `fireAbilityProjectileLocked`, `applyAbilitySplashDamageLocked` |
| `chain_lightning` | projectile + chain | `launch_projectile(lightning_bolt)`; `on_projectile_impact` → `deal_damage` + chain cfg (chainCount/bounceRange/falloff) | `fireAbilityChainLocked` |
| `shatter` | instant point-AoE + slow | `on_cast_complete` → `select_targets(origin=cast_point, radius=110, enemies)` → `deal_damage` + `apply_status(slow 0.5/3s)` + `play_presentation(shatter)` | `applyAbilitySplashDamageLocked`, `applyProcSlowLocked` |
| `meteor` | delayed impact + burn zone | `play_presentation(meteor@cast_point, scale 3)` w/ markers `cross_unit_plane`→`change_render_layer`, `impact`→ `select_targets(impact_position, r230)` + `deal_damage(140)` + `create_zone(Burning Crater, r120, 4s, 0.5s, burning_crater)` w/ `on_zone_tick`→select+`deal_damage(12)` | `spawnGroundHazardLocked`→`spawnZoneLocked`, `applyAbilitySplashDamageLocked` |
| `raise_skeleton` | summon | `on_cast_complete` → `summon_unit(skeleton_soldier ×3)` | `spawnSummonedUnitLocked` |
| `arcane_orb` ⚠ | moving pull+DoT zone (point) | `on_cast_complete` → `launch_projectile(arcane_orb, flypast)` carrying a **moving zone**: proj `on_projectile_tick` → `select_targets(origin=projectile_position, r130, enemies)` → `apply_force(pull 160)` + `deal_damage(dps 16)` | `spawnArcaneOrbLocked`, `applyPullInRadiusLocked` — **kept as-is; modeled `custom` until moving-zone proven** |
| `siphon_life` ⚠ | channeled beam drain+heal | channel entry; `on_status_tick`(0.25s) → `deal_damage(6)` + `restore_health(caster/ally, ×1.0)` | `beginAbilityChannelLocked`/`tickUnitChannelLocked` — **existing channel path kept; modeled `custom`** |
| `arcane_missiles` ⚠ | charge-fire passive | passive entry; `on_charge_full`(charge 30, ratio 1) → `repeat(3, stagger 100ms)` `launch_projectile(random_enemy_in_range, minor dmg 25)` | existing `spell_charge.go` path — **kept; modeled `custom` until composable repeat/charge proven** |

⚠ = the three flagged complex abilities: they load and edit through the new model from day one (identity + cast-setup + a `custom`-typed program node preserving their legacy fields), but their **runtime keeps the existing path** in Phase 4. Composable moving-zone / channel / charge-fire land in a later phase; until then the compiler emits a `custom` trigger whose executor delegates to the legacy resolve function, so behavior is byte-identical.

### 4.2 Compiler patterns (mechanic → program fragment)

Single-target damage · projectile-carried damage · splash · chain · instant point-AoE · delayed impact · burning ground zone · summon · slow/CC · pull/force · DoT · channel (custom) · charge-fire (custom). Each maps to a fragment builder in `ability_program_compile.go`.

**Acceptance:** every ability in §4.1 loads via the compiler and, when run through the executor, produces a trace **behavior-identical to the legacy path** under a fixed seed (golden tests compare damage/heal/zone/summon/projectile events tick-for-tick). `meteor` and `greater_heal` are the primary golden fixtures; the other 9 get at least a "compiles + resolves without divergence" golden test.

---

## 5. Example version-2 JSON

### 5.1 Greater Heal (`schemaVersion: 2`)

```json
{
  "id": "greater_heal",
  "displayName": "Greater Heal",
  "type": "spell",
  "category": "heal",
  "damageType": "holy",
  "icon": "TODO/abilities/greater_heal.png",
  "manaCost": 10,
  "cooldown": 3,
  "castTime": 1.0,
  "casterAnimation": "Casting",
  "supportsAutoCast": true,
  "autoCastTargetSelector": "lowest_hp_percentage_ally_in_range",
  "defaultAutoCast": true,
  "schemaVersion": 2,
  "program": {
    "entry": { "type": "unit", "relations": ["self", "ally"], "range": "match_attack_range" },
    "triggers": [
      {
        "id": "t_cast", "type": "on_cast_complete",
        "actions": [
          {
            "id": "a_select", "type": "select_targets",
            "target": {
              "source": "all_in_scene", "origin": "caster", "relations": ["self", "ally"],
              "radius": -1, "ordering": "lowest_health_percentage",
              "maxCount": 3, "includeInitialTarget": true
            },
            "outputs": { "targets": "healTargets" }
          },
          {
            "id": "a_heal", "type": "restore_health",
            "input": { "targets": { "key": "healTargets" } },
            "config": { "amount": 15, "school": "holy" }
          },
          {
            "id": "a_vfx", "type": "play_presentation",
            "input": { "attach": { "key": "healTargets" } },
            "config": { "asset": "healing_glow", "oncePerTarget": true }
          }
        ]
      }
    ]
  }
}
```

`radius: -1` reuses the match-attack-range sentinel semantics for the query origin (resolved to caster attack range). `targetCount` has moved out of cost/timing into `select_targets.maxCount`.

### 5.2 Meteor (`schemaVersion: 2`)

```json
{
  "id": "meteor",
  "displayName": "Meteor",
  "type": "spell",
  "category": "offensive",
  "damageType": "fire",
  "tags": ["aoe", "damage", "dot"],
  "icon": "TODO/abilities/meteor.png",
  "manaCost": 40,
  "cooldown": 12,
  "castTime": 0.8,
  "supportsAutoCast": true,
  "autoCastTargetSelector": "closest_enemy_in_range",
  "defaultAutoCast": true,
  "schemaVersion": 2,
  "program": {
    "entry": { "type": "ground_point", "relations": ["enemy"], "range": 400 },
    "triggers": [
      {
        "id": "t_cast", "type": "on_cast_complete",
        "actions": [
          {
            "id": "a_meteor", "type": "play_presentation",
            "config": {
              "asset": "meteor", "position": { "key": "castPoint" },
              "scale": 3, "renderLayer": "in_front_of_units",
              "presentationId": "p_meteor"
            }
          }
        ]
      }
    ],
    "presentations": [
      {
        "id": "p_meteor", "asset": "meteor",
        "position": { "key": "castPoint" }, "scale": 3, "renderLayer": "in_front_of_units",
        "triggers": [
          {
            "id": "t_cross", "type": "on_animation_marker", "timing": { "marker": "cross_unit_plane" },
            "actions": [
              { "id": "a_layer", "type": "change_render_layer", "config": { "layer": "behind_units" } }
            ]
          },
          {
            "id": "t_impact", "type": "on_animation_marker", "timing": { "marker": "impact" },
            "actions": [
              {
                "id": "a_sel", "type": "select_targets",
                "target": { "source": "all_in_scene", "origin": "impact_position", "radius": 230, "relations": ["enemy"] },
                "outputs": { "targets": "hitEnemies" }
              },
              {
                "id": "a_dmg", "type": "deal_damage",
                "input": { "targets": { "key": "hitEnemies" } },
                "config": { "amount": 140, "type": "fire" }
              },
              {
                "id": "a_zone", "type": "create_zone",
                "config": {
                  "name": "Burning Crater", "position": { "key": "impactPosition" },
                  "anchor": "ground", "radius": 120, "duration": 4, "tickInterval": 0.5,
                  "owner": { "key": "caster" }, "presentation": "burning_crater",
                  "triggers": [
                    {
                      "id": "t_burn", "type": "on_zone_tick", "timing": { "tickInterval": 0.5 },
                      "actions": [
                        {
                          "id": "a_bsel", "type": "select_targets",
                          "target": { "source": "all_in_scene", "origin": "zone_center", "radius": 120, "relations": ["enemy"] },
                          "outputs": { "targets": "burnHits" }
                        },
                        {
                          "id": "a_bdmg", "type": "deal_damage",
                          "input": { "targets": { "key": "burnHits" } },
                          "config": { "amount": 12, "type": "fire" }
                        }
                      ]
                    }
                  ]
                }
              }
            ]
          }
        ]
      }
    ]
  }
}
```

---

## 6. Compatibility & migration plan

### 6.1 Authority rule (no double source of truth)

- **`schemaVersion` absent / `1`** → legacy flat mechanic fields are authoritative; `Program` is `nil` on disk and **compiled transiently** at load. Editor shows a read-only "Legacy-backed" badge + **"Convert to Composable Ability"** button.
- **`schemaVersion: 2`** → `Program` authoritative; legacy mechanic fields are **removed on convert** (identity/cast-setup stay). The two never coexist as competing values.

### 6.2 Convert flow

`POST /abilities/{id}/convert` → server runs `compileLegacyAbility`, returns the v2 def for the editor to review/save. Convert is **explicit, per-ability**; no bulk catalog rewrite in this project.

### 6.3 Remainder preservation (both layers)

- Go: `AbilityProgram.Remainder map[string]json.RawMessage` captures unknown program-level keys via custom `UnmarshalJSON`/`MarshalJSON`; likewise each action's unknown `Config` sub-keys are preserved by round-tripping `Config` as `json.RawMessage` and re-emitting untouched fields.
- TS: existing `remainder` bag in `AbilityEditorForm` continues to catch unmodeled top-level keys; a parallel `programRemainder` handles unknown program nodes so a newer server schema round-trips through an older editor unharmed.

### 6.4 Validation parity

`validateAbilityDef` gains a branch: if `Program != nil`, run `validateAbilityProgram` (§8). Loader, save, rehydrate, and editor-save all inherit it — the single-gate principle holds.

---

## 7. Migration risks

- **R1 — Zone generalization.** `GroundHazard` is meteor-shaped (impact+burn). Generalizing to a `Zone` with an arbitrary compiled `on_tick`/`on_enter`/`on_exit` trigger is the biggest runtime change. *Mitigation:* Phase 3 keeps `GroundHazard` for the compiled-legacy burn and introduces `Zone` alongside; migrate meteor's compile onto `Zone` only once golden tests pass.
- **R2 — Animation markers on the server.** The sim has no frames; markers must map to **cast-time fractions / effect durations** server-side (deterministic tick time) while the editor maps them to sprite frames for scrubbing. *Mitigation:* markers are authored as `{name, atSeconds}` on presentation instances; the editor's frame view is a convenience mapping (`marker.frame = round(atSeconds/effectDuration*frames)`). Direct-frame triggers remain a fallback.
- **R3 — General author-defined statuses.** No unified buff registry exists. *Mitigation:* Phase 3 `apply_status` targets only existing primitives (slow/stun/burn); arbitrary nested-trigger statuses land in a later phase behind a new `UnitStatusInstance` container.
- **R4 — Preview authority.** A TS reimplementation of resolution would violate "don't duplicate gameplay logic." *Mitigation:* the preview runs the **real Go executor** in a headless preview harness (§10), never re-deriving damage/heal/targeting in Vue.
- **R5 — `change_render_layer`.** No render-layer concept exists. *Mitigation:* add a `renderLayer` field to the effect/presentation snapshot the renderer already draws, honored by a small change to `drawEffects` pass selection; scope it to presentation instances (not arbitrary units) in v1.

---

## 8. Validation plan

`validateAbilityProgram(prog, def) []ValidationIssue` — structural issues mapped to UI paths:

```go
type ValidationIssue struct {
    Path    string `json:"path"`    // e.g. "triggers[2].actions[1].target"
    Code    string `json:"code"`    // machine code (invalid_context_reference, ...)
    Message string `json:"message"` // human text
    Severity string `json:"severity"` // error | warning
}
```

Checks: duplicate ids (ability/trigger/action) · missing named-trigger reference · circular named-trigger invocation · direct recursive action chains · invalid context reference for trigger type (gated by a `triggerContextTable`) · missing target source · missing position source · invalid/out-of-bounds animation marker · tick interval ≤ 0 · duration < tick interval · persistent object without termination (zone/status with no duration) · action referencing an output produced later · unsupported action type (not in registry) · empty required action props (per registry schema) · invalid target relation for entry type · zone/status trigger referencing unavailable context · presentation asset not found · ability with no reachable gameplay-or-presentation behavior · named trigger never invoked (warning) · unreachable trigger (warning) · infinite repeat/recursion risk.

Errors block save (400 `validation_failed`, reusing `EditorValidationError`); warnings surface on cards. `POST /abilities/validate` (dry-run) powers live editor feedback without saving.

---

## 9. Vue UI component breakdown

Preserve Lords of Conquest styling (dark wood panels, dark metal frames, gold headings, compact density, existing typography, `EditorShell`/`SectionCard`/`EditorField`/`UiButton`/`GameScrollArea`). Save Ability control top-right (existing `EditorHeader`). Desktop-first, collapsible regions, no SaaS-workflow look.

New files under `client/src/game-portal/src/components/ability-builder/`:

- `AbilityBuilderPanel.vue` — replaces `AbilityEditorPanel.vue` shell; owns selection state, undo/redo stack, unsaved-changes guard, four-region layout.
- **Left — `AbilityOverviewCard.vue`**: icon, name, category, mana/cd/cast, generated description, entry+cast summary, tags, notes. Click a group → opens it in the inspector. Summary/navigation only.
- **Center-left — `AbilityFlow.vue`** (primary authoring): vertical `FlowTriggerCard.vue` + `FlowActionCard.vue`, nested for child triggers/zones/statuses/presentations. Add/reorder/drag/duplicate/disable/delete/collapse/copy-paste/nest/convert-inline-to-named. Compact per-card summary (from the describe registry) + inline validation badges. View toggle: Flow | Timeline | Compact list.
- **Center-right — `AbilityPreview.vue`** (§10): renderer canvas + `PreviewControls.vue` (play/pause/restart/frame-step/scrub/speed/loop/repeat), scene controls (`PreviewScene.vue`: move caster/targets, spawn/remove units, team, pre-damage), overlay toggles (grid/ranges/hitboxes/selection radii/render layers/target queries/damage numbers/markers), `PreviewTimeline.vue` (markers for cast start/complete/animation markers/impact/damage/heal/zone/tick/status/summon/expire; click marker → seek), and `PreviewEventLog.vue` (tabs: Overview/Targets/Damage/Healing/Status/Context/Validation; click row → select flow card).
- **Right — `ItemInspector.vue`**: edits the selected node (ability/trigger/action/target query/zone/status/projectile/presentation/condition). Sections Basic/Targeting/Timing/Properties/Presentation/Conditions/Advanced/Notes. **Schema-driven** via `SchemaField.vue` fed by the registry's `ActionFieldSchema` (control types: number/text/boolean/enum/multiselect/asset-picker/sentinel-or-number/duration/percentage/target-query/context-ref/animation-marker/nested-trigger-list). No per-mechanic hardcoded markup.
- **Bottom — `ActionPalette.vue`**: searchable, categorized (Targets/Combat/World/Resources/Flow/Presentation). Click → add to selected trigger. Drag-drop optional (not first pass).

TS mirrors in `client/.../game/abilities/program/`: `abilityProgram.ts` (types mirroring §2), `programSchema.ts` (fetched control schema), `programTrace.ts` (`AbilityExecutionTrace(Event)`), `programValidation.ts`, plus `remainder` round-trip helpers extended in `abilityEditorForm.ts`.

New endpoints for the schema-driven editor: `GET /catalog/action-schema` (registry → field schemas + context tables), `POST /abilities/validate`, `POST /abilities/{id}/convert`, `POST /abilities/preview` (§10).

---

## 10. In-game preview integration plan

**Approach: authoritative headless preview harness (server-run executor) + real client renderer.** Avoids duplicating resolution in Vue (R4) and reuses the existing snapshot wire types the renderer already consumes.

1. **Server preview harness** (`server/internal/game/ability_preview.go`): `RunAbilityPreview(req PreviewRequest) PreviewResult`. Builds an isolated `GameState` via `NewGameStateWithSeed(cfg, req.Seed)`, spawns caster + test units per the scene, issues the cast, and **steps the tick loop deterministically** for N ticks with `ctx.Trace` attached. Records, per tick: a lightweight `protocol` snapshot (reuse `AbilitySnapshot`/unit/effect/projectile/zone wire types) **plus** the `AbilityExecutionTrace`.
2. **Endpoint** `POST /abilities/preview` → `{ program|abilityId, scene, seed }` → `{ frames: SnapshotFrame[], trace }`. Deterministic; same input → same output.
3. **Client replay** (`AbilityPreview.vue`): feed recorded snapshot frames into the **real `CanvasRenderer`** by seeking the frame buffer to the scrub time — no live sim in the browser. Scrubbing/frame-step = index into `frames`; play = advance by playback speed.
4. **Deterministic clock (N3):** thread an explicit `renderTime` into `CanvasRenderer.render(timeMs)` and through effect/projectile/overlay frame math (currently internal `performance.now()`), so the preview clock is the trace clock, enabling scrub/step. Production callers pass `performance.now()` (no behavior change).
5. **Markers/overlays:** timeline markers and the event log come **from the trace** (`AbilityExecutionTraceEvent`), not inferred from visuals. Overlay toggles (ranges/hitboxes/target-queries/render-layers) draw from the same frames + trace.
6. **Marker authoring:** the editor scrubs the *presentation asset's* frames (client-side, from the effect manifest) to place/move named markers; the server maps `atSeconds` deterministically (R2). `change_render_layer` (R5) flips the presentation snapshot's `renderLayer`, honored in `drawEffects`.

Fallback if the harness proves too heavy for Phase 6: a **client sandbox that calls the same compiled program via a thin Go→WASM executor build** — deferred, listed only as alternative.

---

## 11. Phased implementation order

1. **Phase 1 — this document.** Investigation + plan. *(review gate)*
2. **Phase 2 — core data model.** Go types (§2) + registries + JSON (un)marshal + remainder round-trip + TS mirrors + `validateAbilityProgram` skeleton. Unit tests. No behavior change (programs unused at runtime yet).
3. **Phase 3 — runtime executor.** `runAbilityProgramLocked` + action registry executors over existing seams (§3), `RuntimeAbilityContext`, tracing, `Zone` generalization (R1). Golden tests vs legacy behavior.
4. **Phase 4 — legacy compiler.** `compileLegacyAbility` for the §4 patterns; assert `meteor` + `greater_heal` compile behavior-identical; route resolve through the executor.
5. **Phase 5 — editor flow UI.** Four-region layout, flow/inspector/palette, schema-driven controls, undo/redo, unsaved-changes guard, LoC styling. Legacy badge + convert button.
6. **Phase 6 — interactive preview.** Preview harness + endpoint + deterministic clock + renderer replay + timeline/overlays/log.
7. **Phase 7 — descriptions + migration tools.** `describeAbilityProgram` walking triggers/actions (compositional; prioritizes gameplay over presentation); convert flow polish. Preserve authored override + never persist generated text.

**Each phase produces working, tested software and gets a per-phase bite-sized task plan before its code is written.**

---

## 12. Generated description (new model)

`describeAbilityProgram(prog)` walks the primary gameplay trigger, composing clauses from `deal_damage`/`restore_health`/`create_zone`/`summon_unit` actions and their target queries; skips pure presentation. Targets:

- Greater Heal → *"Restores 15 health to up to 3 allies with the lowest health percentage."*
- Meteor → *"Calls down a meteor that deals 140 fire damage to enemies within 230 units. It leaves a burning crater that deals 12 fire damage every 0.5 seconds for 4 seconds."*

Override + non-persistence rules unchanged (`EffectiveDescription`, strip `generatedDescription` on save).

---

## 13. Deliverables checklist (Phase 1)

- [x] Current architecture + affected files (§1) · Go data model (§2) · TS types (§2/§9) · v2 Meteor JSON (§5.2) · v2 Greater Heal JSON (§5.1) · migration plan (§6) · runtime execution plan (§3) · validation plan (§8) · Vue component breakdown (§9) · preview integration (§10) · phased order (§11).
