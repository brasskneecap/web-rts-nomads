## Why

The Cleric path's identity is healing/protection. Right now Greater Heal is gated behind a Bronze perk choice — a Cleric that rolls `sanctuary` or `mana_conduit` never gets the AoE heal that defines the fantasy. Moving the heal upgrade onto the path baseline frees the Bronze pool to be four *flavor* choices (none of which feel "must-pick"), and opens a slot for a second buff-on-heal flavor alongside Battle Prayer: an armor-bonus variant for defensive-leaning Clerics.

## What Changes

- **Greater Heal becomes a Cleric path baseline ability via a new `"abilities"` override field on path JSONs.** `cleric.json` declares `"abilities": ["greater_heal"]`, which is loaded into `pathAbilitiesByPath` by `path_defs.go` (symmetric to the existing `projectile` / `damageType` / `visionRange` overrides). On every promotion event, `assignUnitPathAbilitiesLocked` recomputes `unit.Abilities` from `(UnitType, ProgressionPath, Rank)` — base unit abilities → path-level override → additive per-rank grants → position-based state migration. Greater Heal is "what the cleric IS," not "a delta applied to acolyte." (Initial design used a per-(path, rank) grant file; that approach was abandoned mid-implementation in favour of the path-level override because the one-shot mutation was fragile and the path-level pattern is consistent with the existing per-path overrides for other stats.)
- **`greater_heal` is removed from the Cleric Bronze perk pool.** The slot-preserving swap helper (`applyGreaterHealSwapLocked`) is deleted entirely; state migration now happens by position inside `assignUnitPathAbilitiesLocked`.
- **New `bolstering_prayer` Bronze perk.** Mechanically symmetric to `battle_prayer`: a Cleric owning it stamps a timed armor buff onto every target it heals. Uses cross-unit `UnitPerkState` fields (`BolsteringPrayerRemaining`, `BolsteringPrayerArmor`) that mirror `BattlePrayerRemaining` / `BattlePrayerMultiplier`. Decays in the same `state.go Update()` per-unit loop. Picks up the same full-HP focus-target recast-threshold logic, generalised so both perks share the autocast trigger.
- **Bronze pool stays at four entries:** `sanctuary`, `battle_prayer`, `bolstering_prayer`, `mana_conduit`.
- **Focus-active multi-target fill widened.** When a cleric has an active focus target and greater_heal autocasts, the other two slots of the 3-target cast fill with allies in cast range regardless of HP (injured allies still sort first; caster excluded). This makes the buff-refresh path useful even when nothing else is injured — buffs propagate to nearby allies instead of wasting slots on full-HP-but-injured-not-found fallthrough.
- **No change to ability JSON, no change to wire protocol.** `greater_heal.json` is already authored; the change is purely *how the cleric path declares its abilities*. The armor buff piggybacks on existing `effectiveArmorLocked` aggregation; no new damage-pipeline step.

## Capabilities

### Modified Capabilities

- `cleric-bronze-perks` — `greater_heal` requirements are REMOVED (the perk no longer exists). A new `bolstering_prayer` requirement set is ADDED. The four-perk-pool requirement is MODIFIED to swap `greater_heal` → `bolstering_prayer`. The `battle_prayer` focus-recast requirement is MODIFIED to generalise to "any heal-buff perk owned by the caster" so `bolstering_prayer` participates in the recast-threshold autocast logic without duplicating the rule.
- `per-path-ability-kits` — the "no authored grants" requirement is REMOVED. A new requirement is ADDED specifying the Cleric Bronze grant (`greater_heal`) and the heal-replacing swap behaviour that fires inside `assignUnitPathAbilitiesLocked`.

### New Capabilities

<!-- None. Both changes layer onto existing capabilities. -->

## Impact

**Server (Go) — catalog:**
- `server/internal/game/catalog/units/human/acolyte/paths/cleric/cleric.json` — add `"abilities": ["greater_heal"]` to the path JSON.
- `server/internal/game/catalog/units/human/acolyte/paths/cleric/perks/bronze.json` — drop the `greater_heal` perk entry; add the `bolstering_prayer` entry.
- `server/internal/game/catalog/action-icons.json` — add a `perk-bolstering-prayer` icon entry (placeholder asset OK pending art).

**Server (Go) — path-ability resolution:**
- `server/internal/game/path_defs.go` — add `Abilities *[]string` field to `pathCatalogFile` (pointer so the loader distinguishes "absent" from "empty"); add `pathAbilitiesByPath = map[string][]string{}` global; validate each entry against the ability registry at load-time (panic on typo).
- `server/internal/game/path_ability_defs.go` — rewrite `assignUnitPathAbilitiesLocked` as a full recompute of `unit.Abilities` from `(UnitType, ProgressionPath, Rank)`. Composition: base unit abilities → path-level override (from `pathAbilitiesByPath`) → per-rank grants (additive) → state migration of `AutoCastEnabled` / `AbilityCooldowns` by position.
- `server/internal/game/debug_spawn.go` — `DebugSpawnUnit` calls `assignUnitPathAbilitiesLocked` after path/rank assignment so debug-spawned units go through the same recompute as promotion-grown units.
- `server/internal/game/perks.go` — DELETE `applyGreaterHealSwapLocked` (the one-shot mutation helper); the perk-side `applyPerkGrantedHooksLocked` stays as an empty extension seam.

**Server (Go) — Bolstering Prayer + autocast:**
- `server/internal/game/perks.go` — extend `UnitPerkState` with `BolsteringPrayerRemaining` + `BolsteringPrayerArmor`; add a `bolstering_prayer` case to `onPerkAbilityResolvedLocked` parallel to the existing `battle_prayer` case.
- `server/internal/game/perks_defense.go` — new `perkBonusArmorFromBuffsLocked` helper reads `unit.PerkState.BolsteringPrayerArmor` when `BolsteringPrayerRemaining > 0`; `effectiveArmorLocked` adds it to the flat-armor accumulator.
- `server/internal/game/state.go` — extend the cross-unit decay block (currently decays `BattlePrayerRemaining`) to also decay `BolsteringPrayerRemaining`, resetting both fields to 0 on expiry.
- `server/internal/game/autocast_selectors.go` — generalise the `battle_prayer` recast-threshold branch into a `healBuffRecastSpecs` registry so both `battle_prayer` and `bolstering_prayer` participate in the full-HP focus recast logic. ALSO: widen the multi-target candidate pool when a focus target is set so the AoE slots fill with allies near the healer regardless of HP (caster excluded; injured allies still sort first).
- `server/internal/game/perks_icons.go` — emit a `bolstering_prayer` buff icon on any unit with `BolsteringPrayerRemaining > 0` (parallel to the existing `battle_prayer` icon rule).

**Client (TypeScript / Vue 3):**
- No protocol changes. The buff surfaces through the existing active-buffs strip via the new icon-emitter case.

**Tests:**
- Server: Bronze cleric gets `greater_heal` and not plain `heal` immediately on promotion (regardless of which Bronze perk is rolled); the path-level override is idempotent across repeated `assignUnitPathAbilitiesLocked` calls; the recompute composes path override + synthetic rank-grants with no duplicates; `bolstering_prayer` stamps the armor buff on every heal target; refresh-longer / refresh-stronger semantics; cross-unit decay; `effectiveArmorLocked` includes the buff bonus while active and excludes it after expiry; full-HP focus recast fires for `bolstering_prayer` exactly like `battle_prayer`; both buffs stack additively when both Clerics heal the same ally; focus-active multi-target fill includes full-HP allies but excludes the caster and prefers injured; determinism under seed.
- No client tests required (no protocol change, no new UI surface beyond an icon).

**No impact on:** Arch Mage path, Cleric Silver/Gold perks, focus target order/movement, ability multi-target plumbing (TargetCount / force-include unchanged), projectile system, combat AI scoring.

**Migration / save compatibility:** A unit loaded from a save with the now-removed `greater_heal` perk id in `PerkIDs` will retain the id but find no matching `PerkDef` at runtime (perk-def lookup returns nil — already handled defensively at every hook site). The unit's `Abilities` will be recomputed correctly on the next promotion event or `applyRankModifiersLocked` recompute (or simply remains correct if the unit already had `"greater_heal"` from before the change). No data migration script is required; the recompute is idempotent and self-healing.
