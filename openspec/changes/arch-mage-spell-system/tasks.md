## 1. Spell registry extension (§9)

- [x] 1.1 Add `Tags []string` (JSON `tags`) to `AbilityDef` in `ability_defs.go`; default empty when absent.
- [x] 1.2 Confirm `DamageType` is usable as the school-targeting key (no new field); add a doc comment noting it doubles as "school".
- [x] 1.3 Test: an ability def with/without `tags` loads and validates; tags round-trip; absent tags ⇒ empty slice.

## 2. Spell modifier pipeline (§10)

- [x] 2.1 Define `SpellModField` as a typed, load-validated enum (manaCost, cooldown, castTime, damage, radius, projectileSpeed, duration, chainCount, pullStrength). Follow the `DamageType`/`AbilityCategory` extensible-enum idiom.
- [x] 2.2 Define `SpellModifier{ Target{SpellID, School, Tag}, Field SpellModField, Operation (add|multiply, default add), Value float64 }` with a matcher (`appliesTo(def)`): all specified target fields must match; empty target is invalid.
- [x] 2.3 Define `EffectiveSpell` value struct carrying every modifier-eligible field, plus `resolveEffectiveSpell(def, modifiers)` that folds all `add` then all `multiply` per field over the immutable base def.
- [x] 2.4 Add `collectSpellModifiersLocked(caster, def)` with source hooks for perks/buffs/items (documented plug-in point); return concatenated modifiers.
- [x] 2.5 Test: additive default; multiply-after-add ordering; shuffled collection order yields identical result (order-independence); base def unmutated across two casts; inert field is a no-op; empty target rejected; unknown field rejected.

## 3. Cast path reads effective values

- [x] 3.1 Route `resolveAbilityCastLocked` (and mana/cooldown/cast-time gating) to read from `EffectiveSpell` instead of raw def fields; base def stays the source for identity/targeting.
- [x] 3.2 Test: a spell cast under an active modifier spends/deals the modified value; with no modifiers it matches base (regression guard for existing abilities heal/greater_heal/arcane_bolt).

## 4. Spell pools catalog + loader (§11)

- [x] 4.1 Add the pool catalog (`catalog/spell-pools.json` embed) shaped `{ archetype: { rank: [ids] } }`; loader validates rank keys ∈ bronze/silver/gold and every id is a registered `AbilityDef` (panic on miss via `mustLoadSpellPools`, naming archetype/rank/id).
- [x] 4.2 Add `spellPoolFor(archetype, rank) []string` lookup; missing archetype/rank ⇒ empty pool.
- [x] 4.3 Test: valid pool loads; unknown id rejected; unknown rank rejected; missing cell ⇒ empty (parse/validate split so panics are testable as errors).

## 5. Random per-unit assignment (§11)

- [x] 5.1 Add persistent `PoolSpellsByRank map[string]string` to `Unit` (records the pick; id string, never a pointer).
- [x] 5.2 Add `rollUnitPoolSpellsLocked`/`rollUnitPoolSpellForRankLocked`: candidates = pool − already-known, sort candidates, draw from `rngPerks`, record on `PoolSpellsByRank`. Exhausted/empty ⇒ record nothing, draw no RNG.
- [x] 5.3 Call the roll at rank-up after path is recorded and before the ability recompute (progression.go + debug_spawn.go).
- [x] 5.4 Extend `assignUnitPathAbilitiesLocked` composition step (3b): after path override + rank grants, append recorded `PoolSpellsByRank[R]` picks additively (skip duplicates). No RNG in the recompute.
- [x] 5.5 Test: promotion assigns exactly one bronze spell; roll excludes known; per-unit heterogeneity across a run; seed-determinism across runs; recompute reads recorded pick, is idempotent, draws zero RNG; exhausted pool assigns nothing.

## 6. Bronze spell: fireball (reuse splash)

- [x] 6.1 Author `catalog/abilities/fireball/fireball.json` (school `fire`, tags `aoe`/`projectile`/`damage`, projectile `fire_bolt`, radius, damage, mana/cast/cooldown, autocast). Placeholder icon (TODO marker).
- [x] 6.2 On impact, deal effective damage/radius splash via `applyAbilitySplashDamageLocked` (routes through the shared authoritative pipeline, defers death/XP to the pending-death drain like the single-target ability bolt — no hand-rolled loop). Projectile carries `AbilitySplashRadius = eff.Radius`.
- [x] 6.3 Test: fireball damages clustered hostiles via splash and spares out-of-radius units; a low-HP splash victim is killed through the shared pipeline; `radius`/`damage` are in the modifier field enum.

## 7. Bronze spell: chain_lightning (reuse bounce)

- [x] 7.1 Author `catalog/abilities/chain_lightning/chain_lightning.json` (school lightning, tag `chain`, mana/cast/cooldown, autocast, bounce params, emitter `lightning_bolt`).
- [x] 7.2 On resolve, fire the existing beam-bounce mechanic via `fireAbilityChainLocked` → `executeProcEffectLocked`/`fireProcBeamLocked` seeded from the target; `eff.ChainCount` → `bounceCount`; deterministic nearest-hop selection.
- [x] 7.3 Test: chain arcs to `chainCount` enemies with per-hop falloff (primary>b1>b2), spares out-of-range units; outcome identical across seeded runs; `chainCount`/`damage` are in the modifier field enum.

## 8. Forced-displacement CC subsystem

- [x] 8.1 Per-unit pull state on `Unit` (`PullRemaining`/`PullCenterX`/`PullCenterY`/`PullStrength`), mirroring the `StunnedRemaining` CC-field pattern; `applyPullLocked`/`applyPullInRadiusLocked` set it.
- [x] 8.2 `tickUnitPullLocked`: deterministic per-tick delta toward center (no overshoot / snap on reach); expire on duration end via `endUnitPullLocked`. Dead/removed units are skipped by the area applier.
- [x] 8.3 Integrate with the per-unit loop in `state.go`: pull gate `continue`s before stun/movement so displacement wins the tick; `endUnitPullLocked` drops the stale path (no snap-back). Clip-through + rest-position reconciliation via existing repath recovery (design D5).
- [x] 8.4 `pullStrength` is a `SpellModField` (enum member; arcane_orb reads `eff.PullStrength`).
- [x] 8.5 Test: pulled unit closes distance each Update tick; no overshoot; seed-determinism; dead target skipped; allies/caster never pulled; pulled-then-released drops stale path (no snap-back).

## 9. Bronze spell: arcane_orb (uses pull)

- [x] 9.1 Author `catalog/abilities/arcane_orb/arcane_orb.json` (school arcane, tags `cc`/`aoe`, radius, pullStrength, duration, small damage, mana/cast/cooldown, autocast). Instant (no projectile) — center = target position (design open-Q resolved: instant-center, unit-targeted).
- [x] 9.2 On resolve, `applyPullInRadiusLocked` drags hostiles within effective `radius` of the center at effective `pullStrength` for effective `duration` (independent of the damage step).
- [x] 9.3 Test: arcane_orb pulls nearby hostiles inward over duration; allies/caster/out-of-radius unaffected; a `pullStrength` modifier scales the applied rate without mutating the base def.

## 10. Bronze pool wiring + integration

- [x] 10.1 Populate the `arch_mage` bronze pool with `["fireball", "chain_lightning", "arcane_orb"]`; silver/gold empty.
- [x] 10.2 Test: `(arch_mage, bronze)` pool resolves to the three; silver/gold empty; a promoted Arch Mage is assigned exactly one and can cast it end-to-end.
- [x] 10.3 Assigned spell surfaces in `AbilitySnapshot` (via existing `unit.Abilities` path) with display name/cooldown/autocast; no new protocol field.

## 11. Verification

- [x] 11.1 `go test ./...` in `server/` passes (all packages ok); `go build ./...` and `go vet` clean. No regression in caster/ability/progression/movement suites (the new pool RNG draw only fires for non-empty pools, so non-caster progression is byte-for-byte unchanged).
- [x] 11.2 No client changes required — no protocol/`AbilitySnapshot` field added (assigned spell rides the existing `unit.Abilities` path; pull is visible via existing unit-position snapshots). fireball→`fire_bolt` and chain_lightning→`lightning_bolt` reuse existing projectile/beam rendering; arcane_orb is instant (position-driven). Icons are TODO placeholders (dormant-ability convention). Live client render not exercised (headless).
- [x] 11.3 Determinism covered by tests: `TestSpellPoolRoll_DeterministicAndPerUnit` (identical assignments across two seeded runs), `TestPull_Deterministic` (identical pull outcomes), `TestChainLightning_DeterministicSelection`.

## 12. Arch Mage identity redesign (post-review scope)

### 12a. Arcane Missiles passive + Arcane Charge
- [x] 12.1 Add a passive `AbilityType`/marker so an ability is never manually cast or auto-cast and never occupies the castable action row.
- [x] 12.2 Author `catalog/abilities/arcane_missiles/arcane_missiles.json` (type passive; `chargeRequired`, `manaToChargeRatio`, `missileCount`, `damagePerMissile`, `projectile`, `targeting`, `allowDuplicateTargets` — all configurable).
- [x] 12.3 Add `ArcaneCharge float64` to `Unit`; hook `spendUnitManaLocked` so every mana point spent by a unit with the arcane_missiles passive adds `cost * manaToChargeRatio` charge.
- [x] 12.4 When charge ≥ `chargeRequired`, auto-fire `missileCount` missiles at random in-range enemies (deterministic `rngCombat`; duplicates allowed), each dealing `damagePerMissile` via the projectile system; subtract `chargeRequired` (carry overflow). Reject manual/auto cast of passive abilities.
- [x] 12.5 Test: mana spend accrues charge only for owners of the passive; threshold fires exactly `missileCount` missiles and resets; targeting stays in range and is seed-deterministic; passive is not manually castable.

### 12b. Learnable spell-slot system (generic) + arch_mage override
- [x] 12.6 `arch_mage.json` declares `"abilities": ["arcane_missiles"]` (path override REMOVES arcane_bolt from the Arch Mage; base Adept keeps arcane_bolt).
- [x] 12.7 Surface each pool-rolled slot spell with its learned rank in `AbilitySnapshot` (e.g. `spellSlotRank`), and mark passives, so the client can place slot spells in perk cells and hide the passive from the castable row. Slot = a rank for which the archetype has a pool (bronze slot 1, silver slot 2) — reuses `PoolSpellsByRank`.
- [x] 12.8 Test: an Arch Mage has arcane_missiles (passive) + its bronze slot spell (castable), NOT arcane_bolt; the slot spell carries its rank in the snapshot.

### 12c. Client rendering
- [x] 12.9 Client: render a spell-slot ability in its rank's perk cell (bronze/silver) as a CASTABLE cell (clickable, autocast, cooldown) with the rank border — not a display-only perk; exclude slot spells from the normal ability row.
- [x] 12.10 Client: hide passive abilities from the castable ability row (optionally show Arcane Missiles as a passive/charge indicator).
- [x] 12.11 Client type-check (`vue-tsc -b`) clean; verify Arch Mage shows Arcane Missiles (passive) + learned spell in the bronze slot, no Arcane Bolt.

### 12d. Spell action-icon asset resolution (follow-up)
- [x] 12.12 Server: `AbilitySnapshot.Projectile` carries the ability's projectile id.
- [x] 12.13 Client: `abilityAssets.ts` globs `assets/abilities/<id>/*.png` + `assets/projectiles/*.png`; spell action cells (row + slot) set `iconDef:{kind:'ability',type,projectile}`; `ActionIcon.vue` resolves ability art → projectile image → bundled action sprite → SVG.

### 12e. Arcane Orb → traveling vortex (post-review redesign)
- [x] 12.14 arcane_orb no longer instant-pulls: unit-targeted, but on resolve spawns a slow straight-line orb projectile (`Projectile.ArcaneOrb`) traveling caster→target direction up to full cast range (like a slow Marksman pierce). arcane_orb.json → projectile `arcane_orb`, `projectileSpeed` slow, fixed `castRange`, no damage.
- [x] 12.15 `tickArcaneOrbProjectileLocked`: each tick the orb re-applies the radius pull toward its CURRENT position (moving vortex) via `applyPullInRadiusLocked`; despawns at path end (no impact damage). `pullStrength`/`radius` modifier-eligible.
- [x] 12.16 Client: orb renders via existing projectile system (Origin→Target + progress, no homing/arc since targetUnitId=0); uses the bundled `assets/projectiles/arcane_orb.png` sprite via the auto-registration path (no hand-registered override shadowing it).

### 12f. Arcane Orb → ground/point cast + damage (post-review)
- [x] 12.17 `AbilityDef.TargetsPoint`; arcane_orb.json → `targetsPoint`, no unit-target flags, `castTime 0`, `damageAmount` as vortex DPS.
- [x] 12.18 Point-cast lifecycle: `beginAbilityCastAtPointLocked`/`resolveAbilityCastAtPointLocked` fire the orb toward a world point; `beginAbilityCastLocked` rejects point abilities; `RequestAbilityCast` gains `targetX/targetY` and routes by `TargetsPoint`; auto-cast aims a point ability at the chosen enemy's position; `canAbilityTargetUnitLocked` lets point abilities "see" enemies for auto-cast selection only.
- [x] 12.19 Orb deals vortex damage: `Projectile.ArcaneOrbDamagePerSecond`; each tick `tickArcaneOrbProjectileLocked` damages in-radius hostiles (dt-scaled) via `applyAbilitySplashDamageLocked` (shared pipeline).
- [x] 12.20 Protocol/client: `CastAbilityCommandMessage.targetX/targetY`, `AbilitySnapshot.targetsPoint`; WS handler forwards the point; client cast-ability click sends the clicked world point (ground or unit position) for point abilities.
