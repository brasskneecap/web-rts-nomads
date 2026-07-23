package game

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"sort"
	"sync"
)

// Embeds the per-unit catalog tree so this file can load path JSONs from
// catalog/units/<faction>/<unit>/paths/*.json. unit_defs.go embeds the same
// tree for unit-def loading; both init functions filter the tree independently.
//
//go:embed catalog/units
var pathDefsFS embed.FS

// pathCatalogFile is the on-disk shape of a single
// catalog/units/<faction>/<unit>/paths/<path>/<path>.json. Each promotion path
// owns its own directory under its unit; the JSON inside carries the per-rank
// stat multipliers in a ranks map so editing a single (path, rank) cell is
// a one-number change with no risk of contaminating another path. Perks
// for the same path live alongside it at .../<path>/perks/*.json and are
// loaded by perk_defs.go.
type pathCatalogFile struct {
	Path        string `json:"path"`
	Description string `json:"description,omitempty"`
	// Bounds is an optional per-path override of the unit's visual footprint
	// (halfWidth/top/bottom/ringOffsetX/ringOffsetY). Path variants often ship
	// their own sprites at different pixel sizes than the base unit, so the
	// selection ring and hit-test rect need their own values. Passed through
	// as-is; client uses path-keyed bounds before falling back to the base
	// unit's bounds. Server game logic never reads it.
	Bounds json.RawMessage `json:"bounds,omitempty"`
	// AttackOrigin is an optional per-path override of where projectiles/
	// beams visually originate (screen-space offsets from unit.x/unit.y),
	// shape {default?:{x,y}, byFacing?:{<dir>:{x,y}}} — mirrors
	// UnitDef.AttackOrigin (unit_defs.go). Path variants often ship their
	// own sprites at different sizes/proportions than the base unit, so a
	// path may need its own origin rather than inheriting the unit's.
	// Opaque client-render data: the server never reads it, only persists
	// and serves it back, same as Bounds.
	AttackOrigin json.RawMessage `json:"attackOrigin,omitempty"`
	// Shadow is an optional per-path override of the unit's ground-shadow
	// tuning (enabled/radiusX/radiusY/opacity/offsetX/offsetY) — mirrors
	// UnitDef.Shadow. A path variant whose sprite sits differently on the
	// ground (or is simply bigger) may want its own blob shadow rather than
	// inheriting the base unit's. Opaque client-render passthrough, exactly
	// like Bounds/AttackOrigin: the server never reads it, only persists and
	// serves it back.
	Shadow json.RawMessage `json:"shadow,omitempty"`
	// VisionRange overrides BaseVisionRange for units on this path, in world pixels.
	// When 0 or absent, the unit's BaseVisionRange (from its unit def) is used.
	VisionRange float64 `json:"visionRange,omitempty"`
	// Projectile overrides the unit def's Projectile (the ProjectileDef id the
	// basic ranged attack fires) for units promoted onto this path — e.g. the
	// Cleric firing "holy_bolt" instead of the Acolyte's "fire_bolt". Empty
	// ⇒ keep whatever the unit def set at spawn. Validated at load against the
	// projectile catalog (same fail-loud contract as UnitDef.Projectile).
	Projectile string `json:"projectile,omitempty"`
	// DamageType overrides the unit def's DamageType (the basic attack's
	// element/school tag) for units on this path. Optional flavor/metadata,
	// same as UnitDef.DamageType; empty ⇒ keep the unit def's type. Validated
	// at load when non-empty.
	DamageType DamageType `json:"damageType,omitempty"`
	// AttackType overrides the unit def's AttackType (the melee attack-sound
	// key) for units promoted onto this path — e.g. a soldier ("swing")
	// becoming a vanguard ("stab"). Empty ⇒ keep whatever the unit def set at
	// spawn (so the berserker path, which also swings, simply omits it and
	// inherits the soldier's "swing"). Purely presentational.
	AttackType string `json:"attackType,omitempty"`
	// ProjectileScale overrides the unit def's ProjectileScale (the per-unit
	// projectile-sprite render multiplier) for units promoted onto this path,
	// so two paths of the same base unit (e.g. Acolyte → Cleric vs Arch
	// Mage) can size their shots independently. Purely visual; > 0 ⇒ override,
	// omitted / 0 ⇒ keep whatever the unit def set at spawn. Validated >= 0.
	ProjectileScale float64 `json:"projectileScale,omitempty"`
	// Abilities, when present, REPLACES the base unit def's Abilities list for
	// units on this path. The mechanism is symmetric to Projectile / DamageType
	// / VisionRange above: the path JSON gets to declare what its units have
	// instead of layering a "swap" mutation on top of the base. Each entry
	// MUST be a registered AbilityDef id (load-time panic on typo).
	//
	// A pointer-to-slice is used so the loader can distinguish "field absent"
	// (no override, keep base) from "field present but empty" (this path has
	// no abilities — strips the base list). The common case (cleric) is the
	// "1-for-1 swap" pattern: acolyte ["heal"] → cleric ["greater_heal"].
	// Per-instance AutoCastEnabled / AbilityCooldowns are migrated by position
	// in assignUnitPathAbilitiesLocked so a heal-autocasted acolyte keeps
	// autocast on greater_heal after promotion.
	Abilities *[]string `json:"abilities,omitempty"`
	// ChannelLoop overrides the base unit def's ChannelLoop for units on
	// this path (e.g. the Siphoner pinning its channel pose to a different
	// pair of frames than a hypothetical other Acolyte channel-path). Same
	// pointer-to-struct semantics as UnitDef.ChannelLoop: absent ⇒ inherit
	// from the unit def. Validated at load (start >= 0, end >= start).
	ChannelLoop *ChannelLoopRange            `json:"channelLoop,omitempty"`
	Ranks       map[string]pathRankStatsJSON `json:"ranks"`
	// PerksByRank is the SOLE source of a path's rank-up perk pool, keyed by
	// rank (bronze/silver/gold). Each value is a list of standalone perk ids
	// (catalog/perks) — see eligiblePerksForUnitAtRank, which resolves ONLY
	// this list (a PerkDef's own UnitType/Path/Rank fields no longer
	// participate in selection; they are editor filtering/display only).
	// Absent/empty for a (path, rank) ⇒ that rank rolls no perks. Perk ids
	// resolve fail-safe at selection; validated at save via perkDefLookup.
	PerksByRank map[string][]string `json:"perksByRank,omitempty"`
	// AbilityPoolsByRank is the per-rank pool of candidate ability ids a unit
	// MAY be granted at that rank (one is rolled at rank-up — see the ability
	// pool roll). Keyed bronze/silver/gold. Independent of PerksByRank (a rank
	// may author either). Editor-authored; replaces the former standalone
	// spell-pool catalog file (removed).
	AbilityPoolsByRank map[string][]string `json:"abilityPoolsByRank,omitempty"`
	// AbilityStatsByRank are the BROAD ability modifiers a unit on this path
	// carries at a given rank — "+25 ability power at gold", "+15% radius".
	// Keyed bronze/silver/gold, then by ability-stat id (AbilityStatDefs).
	//
	// ABSOLUTE per rank, not cumulative: a rank's block is the unit's total
	// contribution AT that rank, exactly like Ranks' multipliers above (gold's
	// x1.35 is off the base unit, not off silver's x1.2). The editor floors each
	// rank at the previous one so the totals can never regress, and
	// validatePathAbilityStatsByRank enforces the same rule for hand-edited
	// files — otherwise a gold unit could silently be WEAKER than a silver one.
	AbilityStatsByRank map[string]map[string]AbilityStatMod `json:"abilityStatsByRank,omitempty"`
}

// pathRankStatsJSON mirrors the stat-modifier fields of pathModifierDef (the
// in-memory struct) with json tags. A separate type keeps pathModifierDef
// free of serialization concerns and lets the loader validate rank keys
// before exposing them to the rest of the codebase.
//
// AttackRange (flat override, in world pixels) and AttackRangeMultiplier
// (multiplier on top of unit.BaseAttackRange) are both optional. Omitted /
// zero values are no-ops at load time so paths that don't tune attack range
// can continue to omit them. When both are present in a rank row, the flat
// override wins — see applyRankModifiersLocked for the resolution order.
type pathRankStatsJSON struct {
	// BaseStats sets per-rank values for the REGISTERED stats that have no
	// typed field above — abilityPower, critChance, lifesteal, and anything else
	// statBaseAuthorable allows (the same vocabulary UnitDef.BaseStats uses).
	//
	// The 12 typed fields below are MULTIPLIERS off the unit's base, which works
	// for hp/damage but is meaningless for a stat whose base is 0 — no multiple
	// of zero ability power is ever more than zero. So these are ABSOLUTE
	// values: the stat IS this at that rank. The editor seeds each rank from the
	// unit's own base and floors it at the previous rank, and
	// validatePathRankBaseStats enforces the same for hand-edited files.
	BaseStats map[string]float64 `json:"baseStats,omitempty"`

	MaxHPMultiplier float64 `json:"maxHPMultiplier"`
	// MaxMPMultiplier scales the unit def's catalog MaxMana for this (path, rank).
	// Optional: omitted / zero defaults to 1.0 at load (so non-caster paths that
	// never author it don't zero a caster's pool). Applied in applyRankModifiersLocked.
	MaxMPMultiplier float64 `json:"maxMPMultiplier"`
	// HealthRegenMultiplier scales the unit's base passive HP regen
	// (UnitDef.healthRegenRate, else the global default) for this (path, rank).
	// Optional: omitted / zero defaults to 1.0 at load, so a path that does not
	// author it leaves regen exactly as it was — adding this field rebalances
	// nothing until a path opts in.
	//
	// Note it scales the unit's BASE regen, so a unit authored with
	// healthRegenRate: 0 ("never regenerates") stays at 0 at every rank. A
	// multiplier cannot resurrect regen from zero — that is deliberate.
	HealthRegenMultiplier float64 `json:"healthRegenMultiplier"`
	DamageMultiplier      float64 `json:"damageMultiplier"`
	AttackSpeedMultiplier float64 `json:"attackSpeedMultiplier"`
	MoveSpeedMultiplier   float64 `json:"moveSpeedMultiplier"`
	AttackRange           float64 `json:"attackRange"`
	AttackRangeMultiplier float64 `json:"attackRangeMultiplier"`
	Armor                 int     `json:"armor"`
	// DodgeChance / BlockChance are per-rank ADDITIVE evasion contributions
	// (0.10 = +10%). Absent (0) means the path contributes nothing; the
	// game-wide base (baseUnitDodgeChance) always applies on top.
	DodgeChance float64 `json:"dodgeChance"`
	BlockChance float64 `json:"blockChance"`
	// VisionRange is a per-rank FLAT override (world pixels). When > 0 it
	// replaces the resolved base/path vision for this (path, rank), before the
	// perk vision multiplier. Absent (0) leaves vision at the path-level /
	// unit-def value. Applied in applyRankModifiersLocked.
	VisionRange float64 `json:"visionRange"`
}

// pathModifiersByKey is the loaded lookup map. Key is path + "/" + rank (e.g.
// "vanguard/bronze"). Missing entries resolve to identityPathModifier via
// pathModifierFor — a typo in a path id therefore fails loud in-game (stats
// unchanged from base) rather than silently picking an unrelated row.
var pathModifiersByKey map[string]pathModifierDef

// pathBoundsByPath holds the optional per-path visual-bounds override, keyed
// by path id (e.g. "marksman"). Empty when a path JSON omits the field. Used
// by the /catalog/units endpoint so the client can render path-promoted units
// with sprite-appropriate selection rings.
var pathBoundsByPath = map[string]json.RawMessage{}

// pathAttackOriginByPath holds the optional per-path attack-origin override
// (screen-space projectile/beam launch offsets, {default?, byFacing?}),
// keyed by path id. Empty when a path JSON omits the field. Mirrors
// pathBoundsByPath exactly — same opaque client-render passthrough
// contract, served via the /catalog/units endpoint's "paths" entries
// (see PathBoundsEntry / ListPathBounds) so the game client's existing
// fetch is sufficient; the server never reads it.
var pathAttackOriginByPath = map[string]json.RawMessage{}

// pathShadowByPath holds the optional per-path ground-shadow override, keyed by
// path id. Empty when a path JSON omits the field. Mirrors pathBoundsByPath /
// pathAttackOriginByPath exactly — same opaque client-render passthrough,
// served via the /catalog/units "paths" entries (PathBoundsEntry.Shadow).
var pathShadowByPath = map[string]json.RawMessage{}

// pathVisionRangeByPath stores the optional per-path base vision range in world
// pixels, keyed by path id (e.g. "marksman": 448). Zero means "use the unit
// def's visionRange". Applied in applyRankModifiersLocked.
var pathVisionRangeByPath = map[string]float64{}

// pathProjectileByPath / pathDamageTypeByPath hold the optional per-path
// overrides of the unit def's basic-attack projectile and damage-type tag,
// keyed by path id (e.g. "cleric": "holy_bolt", "cleric": "holy"). A path
// absent from a map means "no override — keep the unit def value set at
// spawn". Applied in applyRankModifiersLocked once the unit's ProgressionPath
// is assigned. Validated at load (path_defs init) so a typo'd projectile or
// unknown damage type fails loud at startup, same as UnitDef.
var pathProjectileByPath = map[string]string{}
var pathDamageTypeByPath = map[string]DamageType{}

// pathAttackTypeByPath holds the optional per-path override of the unit def's
// melee attack-sound key, keyed by path id (e.g. "vanguard": "stab"). A path
// absent from the map means "no override — keep the unit def's AttackType set
// at spawn". Applied in applyRankModifiersLocked once ProgressionPath is set.
var pathAttackTypeByPath = map[string]string{}

// pathProjectileScaleByPath holds the optional per-path projectile-sprite
// render multiplier override, keyed by path id (e.g. "cleric": 1.5). A path
// absent from the map means "no override — keep the unit def's
// ProjectileScale". Only paths declaring a positive value are stored, so an
// omitted / zero field never zeroes the unit-def value. Applied in
// applyRankModifiersLocked.
var pathProjectileScaleByPath = map[string]float64{}

// pathAbilitiesByPath stores the optional per-path ability list override,
// keyed by path id (e.g. "cleric": ["greater_heal"]). A path absent from the
// map means "no override — keep the base unit def's Abilities". A path
// present with an empty slice means "explicit empty list" (strips the base).
// Applied in assignUnitPathAbilitiesLocked, which composes path override +
// rank-grants (path_ability_defs.go) + state migration in one resolution
// pass.
var pathAbilitiesByPath = map[string][]string{}

// pathAbilityStatsByPath stores the per-(path, rank) BROAD ability modifiers a
// unit carries at that rank — path -> rank -> stat id -> {flat, pct}. Read via
// pathAbilityStatsFor, folded into a caster through
// collectAbilityStatSourcesLocked's path source (ability_stats.go).
var pathAbilityStatsByPath = map[string]map[string]map[string]AbilityStatMod{}

// pathPerkRefsByPath stores the per-path, per-rank explicit perk-id
// references (PathDef.PerksByRank), keyed by path id then rank (e.g.
// "berserker" -> "bronze" -> ["frenzy"]). This is the SOLE source of a
// (path, rank)'s eligible perk pool (see eligiblePerksForUnitAtRank /
// pathPerkRefsForRank). A path absent from the map, or a rank absent from a
// path's inner map, means that (path, rank) rolls no perks. Populated by
// registerPathFileInto, mirroring pathAbilitiesByPath's contract exactly.
var pathPerkRefsByPath = map[string]map[string][]string{}

// pathAbilityPoolsByPath stores the per-path, per-rank ability-pool
// references (PathDef.AbilityPoolsByRank), keyed by path id then rank (e.g.
// "arch_mage" -> "silver" -> ["fireball", "chain_lightning"]). Mirrors
// pathPerkRefsByPath's contract exactly: a path absent from the map, or a
// rank absent from a path's inner map, means that (path, rank) has no
// authored ability pool. Populated by registerPathFileInto. Nothing reads
// this map yet — a later task wires it into the rank-up ability roll.
var pathAbilityPoolsByPath = map[string]map[string][]string{}

// pathChannelLoopByPath stores the optional per-path channel-pose frame
// override, keyed by path id (e.g. "siphoner": {Start: 3, End: 4}). A path
// absent from the map means "no override — fall back to the base unit
// def's ChannelLoop, or (0, 0) if that is also absent". Read by
// channelLoopRangeForUnitLocked at snapshot time.
var pathChannelLoopByPath = map[string]ChannelLoopRange{}

// pathsByUnitType records the catalog topology: which unit directory owns
// each path directory. Keyed by unit type, value is the sorted list of path
// ids that live under catalog/units/<faction>/<unit>/paths/. Populated by
// the init walk in this file. Exposed via ListPathsByUnitType so clients
// (e.g. DebugSpawnPanel) can derive their unit→path UI from the directory
// layout instead of duplicating it.
var pathsByUnitType = map[string][]string{}

// pathCatalogMu guards all 13 package-global path-catalog maps above
// (pathModifiersByKey, pathBoundsByPath, pathAttackOriginByPath,
// pathVisionRangeByPath, pathProjectileByPath, pathDamageTypeByPath,
// pathAttackTypeByPath, pathProjectileScaleByPath, pathAbilitiesByPath,
// pathPerkRefsByPath, pathAbilityPoolsByPath, pathChannelLoopByPath,
// pathsByUnitType).
//
// init() populates these maps single-threaded at startup before any
// goroutine exists, so init's own reads/writes bypass the lock (see the
// PathChances cross-validation block at the end of init, and the writes
// inside the catalog walk). Every OTHER read — i.e. everything that runs
// during the simulation tick loop, HTTP handlers, or any code path reachable
// after startup — MUST go through the accessor functions below rather than
// touching a map directly. This is what makes it safe for a future task to
// add a writable editor overlay that rebuilds these maps at runtime while
// the tick loop keeps reading them.
var pathCatalogMu sync.RWMutex

// pathModifierLookup is the synchronized read path for pathModifiersByKey.
// Mirrors a direct `def, ok := pathModifiersByKey[key]` map read.
func pathModifierLookup(key string) (pathModifierDef, bool) {
	pathCatalogMu.RLock()
	defer pathCatalogMu.RUnlock()
	def, ok := pathModifiersByKey[key]
	return def, ok
}

// pathBoundsFor is the synchronized read path for pathBoundsByPath.
func pathBoundsFor(path string) (json.RawMessage, bool) {
	pathCatalogMu.RLock()
	defer pathCatalogMu.RUnlock()
	bounds, ok := pathBoundsByPath[path]
	return bounds, ok
}

// pathAttackOriginFor is the synchronized read path for
// pathAttackOriginByPath.
func pathAttackOriginFor(path string) (json.RawMessage, bool) {
	pathCatalogMu.RLock()
	defer pathCatalogMu.RUnlock()
	origin, ok := pathAttackOriginByPath[path]
	return origin, ok
}

// pathVisionRangeFor is the synchronized read path for pathVisionRangeByPath.
func pathVisionRangeFor(path string) (float64, bool) {
	pathCatalogMu.RLock()
	defer pathCatalogMu.RUnlock()
	vision, ok := pathVisionRangeByPath[path]
	return vision, ok
}

// pathProjectileFor is the synchronized read path for pathProjectileByPath.
func pathProjectileFor(path string) (string, bool) {
	pathCatalogMu.RLock()
	defer pathCatalogMu.RUnlock()
	projectile, ok := pathProjectileByPath[path]
	return projectile, ok
}

// pathDamageTypeFor is the synchronized read path for pathDamageTypeByPath.
func pathDamageTypeFor(path string) (DamageType, bool) {
	pathCatalogMu.RLock()
	defer pathCatalogMu.RUnlock()
	damageType, ok := pathDamageTypeByPath[path]
	return damageType, ok
}

// pathAttackTypeFor is the synchronized read path for pathAttackTypeByPath.
func pathAttackTypeFor(path string) (string, bool) {
	pathCatalogMu.RLock()
	defer pathCatalogMu.RUnlock()
	attackType, ok := pathAttackTypeByPath[path]
	return attackType, ok
}

// pathProjectileScaleFor is the synchronized read path for
// pathProjectileScaleByPath.
func pathProjectileScaleFor(path string) (float64, bool) {
	pathCatalogMu.RLock()
	defer pathCatalogMu.RUnlock()
	scale, ok := pathProjectileScaleByPath[path]
	return scale, ok
}

// pathAbilitiesFor is the synchronized read path for pathAbilitiesByPath. The
// returned slice is a COPY — callers must not be able to mutate the shared
// catalog state through it.
func pathAbilitiesFor(path string) ([]string, bool) {
	pathCatalogMu.RLock()
	defer pathCatalogMu.RUnlock()
	abilities, ok := pathAbilitiesByPath[path]
	if !ok {
		return nil, false
	}
	cp := make([]string, len(abilities))
	copy(cp, abilities)
	return cp, true
}

// pathPerkRefsForRank returns the explicit perk-id references authored on the
// given path for the given rank, or nil. Path ids are globally unique so the
// unit type is not part of the key. Caller must NOT hold pathCatalogMu. The
// returned slice is a COPY (mirrors pathAbilitiesFor / pathsForUnitType) —
// callers (e.g. eligiblePerksForUnitAtRank, the sole consumer) must not be
// able to mutate the shared catalog state through it.
func pathPerkRefsForRank(pathName, rank string) []string {
	pathCatalogMu.RLock()
	defer pathCatalogMu.RUnlock()
	byRank := pathPerkRefsByPath[pathName]
	if byRank == nil {
		return nil
	}
	refs := byRank[rank]
	if refs == nil {
		return nil
	}
	cp := make([]string, len(refs))
	copy(cp, refs)
	return cp
}

// pathAbilityPoolsForRank returns the ability-pool references authored on the
// given path for the given rank, or nil. Mirrors pathPerkRefsForRank exactly
// (same locking, returns a COPY, nil when absent). Caller must NOT hold
// pathCatalogMu.
func pathAbilityPoolsForRank(pathName, rank string) []string {
	pathCatalogMu.RLock()
	defer pathCatalogMu.RUnlock()
	byRank := pathAbilityPoolsByPath[pathName]
	if byRank == nil {
		return nil
	}
	pool := byRank[rank]
	if pool == nil {
		return nil
	}
	cp := make([]string, len(pool))
	copy(cp, pool)
	return cp
}

// pathChannelLoopFor is the synchronized read path for pathChannelLoopByPath.
func pathChannelLoopFor(path string) (ChannelLoopRange, bool) {
	pathCatalogMu.RLock()
	defer pathCatalogMu.RUnlock()
	loop, ok := pathChannelLoopByPath[path]
	return loop, ok
}

// pathsForUnitType is the synchronized read path for pathsByUnitType. The
// returned slice is a COPY — callers must not be able to mutate the shared
// catalog state through it. Returns nil (not an error) for an unknown unit
// type, matching a direct map-miss read.
func pathsForUnitType(unitType string) []string {
	pathCatalogMu.RLock()
	defer pathCatalogMu.RUnlock()
	paths := pathsByUnitType[unitType]
	cp := make([]string, len(paths))
	copy(cp, paths)
	return cp
}

// unitTypeForPath returns the unit type that owns a promotion path, or "" if
// the path is unknown. Reverse of pathsByUnitType; used by placed-unit perk
// validation (maps.go) now that a perk's owning unit is derived from its path
// association rather than a stored PerkDef.UnitType field. Linear scan over a
// tiny catalog (a handful of unit types) — no index needed.
func unitTypeForPath(path string) string {
	if path == "" {
		return ""
	}
	pathCatalogMu.RLock()
	defer pathCatalogMu.RUnlock()
	for unitType, paths := range pathsByUnitType {
		for _, p := range paths {
			if p == path {
				return unitType
			}
		}
	}
	return ""
}

// PathBoundsEntry is the shape served to the client: a path id plus its raw
// bounds blob and (if declared) its raw attack-origin blob. Slice form
// (rather than map) gives stable ordering in the JSON response.
type PathBoundsEntry struct {
	Path         string          `json:"path"`
	Bounds       json.RawMessage `json:"bounds"`
	AttackOrigin json.RawMessage `json:"attackOrigin,omitempty"`
	Shadow       json.RawMessage `json:"shadow,omitempty"`
}

// ListPathBounds returns every path that declared a bounds override and/or
// an attackOrigin override — the union of both maps, so a path authoring
// ONLY an attackOrigin (no bounds) still appears — sorted by path id.
// Mirrors ListUnitDefs / ListBuildingDefs. This is the single "paths" field
// the game client already fetches via GET /catalog/units, so serving
// attackOrigin here (rather than adding a new endpoint) keeps that fetch
// sufficient for path-level attack-origin overrides too.
func ListPathBounds() []PathBoundsEntry {
	pathCatalogMu.RLock()
	defer pathCatalogMu.RUnlock()
	seen := make(map[string]struct{}, len(pathBoundsByPath)+len(pathAttackOriginByPath)+len(pathShadowByPath))
	for path := range pathBoundsByPath {
		seen[path] = struct{}{}
	}
	for path := range pathAttackOriginByPath {
		seen[path] = struct{}{}
	}
	for path := range pathShadowByPath {
		seen[path] = struct{}{}
	}
	out := make([]PathBoundsEntry, 0, len(seen))
	for path := range seen {
		out = append(out, PathBoundsEntry{
			Path:         path,
			Bounds:       pathBoundsByPath[path],
			AttackOrigin: pathAttackOriginByPath[path],
			Shadow:       pathShadowByPath[path],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// ListPathsByUnitType returns a copy of the unit→paths catalog topology
// (which path directories live under each unit directory). The returned
// map is freshly allocated so callers can mutate it without affecting the
// package state; path-id slices are stable-sorted.
func ListPathsByUnitType() map[string][]string {
	pathCatalogMu.RLock()
	defer pathCatalogMu.RUnlock()
	out := make(map[string][]string, len(pathsByUnitType))
	for unitType, paths := range pathsByUnitType {
		cp := make([]string, len(paths))
		copy(cp, paths)
		out[unitType] = cp
	}
	return out
}

// defaultRankCurve is the rank-progression multiplier for units that earn XP
// without ever being assigned a promotion path — workers, raiders, and any
// future utility units. Used by pathModifierFor when path == unitPathNone.
//
// Not configurable via JSON because "none" is not a player-facing path, it's
// a system fallback. Changing these numbers is a structural-progression
// decision (affects every path-less unit uniformly) and belongs in code.
// If you want the Vanguard or Berserker curve tuned instead, edit the file
// under catalog/units/<faction>/<unit>/paths/ — those ARE JSON-configurable.
// HealthRegenMultiplier is 1.0 on every row on purpose: a path-less unit's regen
// does NOT scale with rank by default. Adding the field must rebalance nothing —
// regen scaling is opt-in, authored per (path, rank) in the path JSON.
var defaultRankCurve = map[string]pathModifierDef{
	unitRankBronze: {Path: unitPathNone, Rank: unitRankBronze, MaxHPMultiplier: 1.10, MaxMPMultiplier: 1.0, HealthRegenMultiplier: 1.0, DamageMultiplier: 1.10, AttackSpeedMultiplier: 1.00, MoveSpeedMultiplier: 1.00, AttackRangeMultiplier: 1.0, Armor: 0},
	unitRankSilver: {Path: unitPathNone, Rank: unitRankSilver, MaxHPMultiplier: 1.20, MaxMPMultiplier: 1.0, HealthRegenMultiplier: 1.0, DamageMultiplier: 1.25, AttackSpeedMultiplier: 1.10, MoveSpeedMultiplier: 1.00, AttackRangeMultiplier: 1.0, Armor: 0},
	unitRankGold:   {Path: unitPathNone, Rank: unitRankGold, MaxHPMultiplier: 1.35, MaxMPMultiplier: 1.0, HealthRegenMultiplier: 1.0, DamageMultiplier: 1.50, AttackSpeedMultiplier: 1.25, MoveSpeedMultiplier: 1.00, AttackRangeMultiplier: 1.0, Armor: 0},
}

func pathModifierKey(path, rank string) string {
	return path + "/" + rank
}

// validRankName is the set of rank names allowed in a path catalog file. Base
// rank is intentionally excluded — pre-promotion stats are always identity,
// driven by the pathModifierFor base-rank short-circuit.
var validRankName = map[string]struct{}{
	unitRankBronze: {},
	unitRankSilver: {},
	unitRankGold:   {},
}

// validatePathFile checks a single decoded pathCatalogFile for validity in
// isolation — no map reads or writes, so it is safe to call before any
// catalog state exists (a future path-editor task will run this on
// user-submitted JSON before it ever touches the runtime catalog). It
// intentionally does NOT catch cross-file collisions (the same path/rank
// defined twice, or two files each claiming the same path-level channelLoop
// / abilities override) — those require comparing against already-
// registered state and live in registerPathFileLocked instead.
//
// Error messages match the panic messages the walk in init() used to
// produce, minus the "<rel>: " file-path prefix — callers add their own
// context (init prepends the embed path; the editor will prepend whatever
// identifies the in-flight edit).
func validatePathFile(file *pathCatalogFile, pathKey string) error {
	if file.Path == "" {
		return errors.New(`missing "path" field`)
	}
	if file.Path != pathKey {
		// Directory name is the canonical path id. A mismatch means someone
		// edited one without the other; fail loud so the catalog stays
		// coherent.
		return fmt.Errorf("path %q does not match directory name %q", file.Path, pathKey)
	}
	if err := validatePathAbilityStatsByRank(file.Path, file.AbilityStatsByRank); err != nil {
		return err
	}
	if err := validatePathRankBaseStats(file.Path, file.Ranks); err != nil {
		return err
	}
	if file.Projectile != "" {
		if _, ok := getProjectileDef(file.Projectile); !ok {
			return fmt.Errorf(`projectile %q is not a registered projectile def`, file.Projectile)
		}
	}
	if file.DamageType != "" {
		if !IsValidDamageType(file.DamageType) {
			return fmt.Errorf(`damageType %q is not a registered damage type`, string(file.DamageType))
		}
	}
	if file.ProjectileScale < 0 {
		return errors.New(`projectileScale must be >= 0 (0/omitted ⇒ keep the unit def value)`)
	}
	// Frame indices must be non-negative and end >= start. Out-of-range
	// positive values modulo on the client at draw time, so we don't
	// bound-check against an expected sheet frame count here — the sprite
	// sheet is client-side data the server doesn't know about.
	if file.ChannelLoop != nil {
		if file.ChannelLoop.Start < 0 {
			return errors.New(`channelLoop.start must be >= 0`)
		}
		if file.ChannelLoop.End < file.ChannelLoop.Start {
			return errors.New(`channelLoop.end must be >= channelLoop.start`)
		}
	}
	// Each ability id must be a registered AbilityDef so a typo fails loud
	// at startup. Mirrors the projectile/damage-type validation.
	if file.Abilities != nil {
		for _, abilityID := range *file.Abilities {
			if abilityID == "" {
				return errors.New(`empty ability id in "abilities"`)
			}
			if _, ok := getAbilityDef(abilityID); !ok {
				return fmt.Errorf("ability %q in \"abilities\" has no registered AbilityDef", abilityID)
			}
		}
	}
	// Rank keys are sorted before validation so a file with multiple bad
	// rank names always reports the same one first, regardless of Go's
	// randomized map iteration order (determinism invariant).
	rankNames := make([]string, 0, len(file.Ranks))
	for rankName := range file.Ranks {
		rankNames = append(rankNames, rankName)
	}
	sort.Strings(rankNames)
	for _, rankName := range rankNames {
		if _, ok := validRankName[rankName]; !ok {
			return fmt.Errorf("unknown rank %q (want bronze/silver/gold)", rankName)
		}
	}
	// PerksByRank: rank keys must be valid ranks; each perk id must resolve to a
	// registered standalone PerkDef so a typo fails loud at save. Sorted for a
	// deterministic first-error.
	perkRefRanks := make([]string, 0, len(file.PerksByRank))
	for rankName := range file.PerksByRank {
		perkRefRanks = append(perkRefRanks, rankName)
	}
	sort.Strings(perkRefRanks)
	for _, rankName := range perkRefRanks {
		if _, ok := validRankName[rankName]; !ok {
			return fmt.Errorf("unknown rank %q in \"perksByRank\" (want bronze/silver/gold)", rankName)
		}
		for _, perkID := range file.PerksByRank[rankName] {
			if perkID == "" {
				return fmt.Errorf("empty perk id in perksByRank[%q]", rankName)
			}
			if _, ok := perkDefLookup(perkID); !ok {
				return fmt.Errorf("perk %q in perksByRank[%q] has no registered PerkDef", perkID, rankName)
			}
		}
	}
	// AbilityPoolsByRank: rank keys must be valid ranks; each ability id must
	// resolve to a registered AbilityDef (not a perk) so a typo fails loud at
	// save. Sorted for a deterministic first-error, mirroring PerksByRank.
	abilityPoolRanks := make([]string, 0, len(file.AbilityPoolsByRank))
	for rankName := range file.AbilityPoolsByRank {
		abilityPoolRanks = append(abilityPoolRanks, rankName)
	}
	sort.Strings(abilityPoolRanks)
	for _, rankName := range abilityPoolRanks {
		if _, ok := validRankName[rankName]; !ok {
			return fmt.Errorf("unknown rank %q in \"abilityPoolsByRank\" (want bronze/silver/gold)", rankName)
		}
		for _, abilityID := range file.AbilityPoolsByRank[rankName] {
			if abilityID == "" {
				return fmt.Errorf("empty ability id in abilityPoolsByRank[%q]", rankName)
			}
			if _, ok := getAbilityDef(abilityID); !ok {
				return fmt.Errorf("ability %q in abilityPoolsByRank[%q] has no registered AbilityDef", abilityID, rankName)
			}
		}
	}
	// An ability id may NOT appear both in the base Abilities override and
	// in any rank's AbilityPoolsByRank list (a permanently-granted ability
	// listed in a roll pool is a dead/contradictory entry). An ability id
	// MAY appear in more than one rank's pool -- this is a valid, designed
	// configuration (e.g. bronze and silver sharing the same roll pool);
	// unitKnownAbilitySetLocked de-dupes the actual grant across ranks at
	// roll time, so a unit never ends up with two copies. Duplicates
	// WITHIN a single rank's pool are still rejected. Sorted rank
	// iteration (abilityPoolRanks, built above) keeps the first-reported
	// error deterministic regardless of Go's map iteration order.
	if file.Abilities != nil || len(file.AbilityPoolsByRank) > 0 {
		baseAbilities := make(map[string]bool)
		if file.Abilities != nil {
			for _, abilityID := range *file.Abilities {
				baseAbilities[abilityID] = true
			}
		}
		for _, rankName := range abilityPoolRanks {
			seenInRank := make(map[string]bool)
			for _, abilityID := range file.AbilityPoolsByRank[rankName] {
				if baseAbilities[abilityID] {
					return fmt.Errorf("ability %q appears in both the base abilities and abilityPoolsByRank[%q]", abilityID, rankName)
				}
				if seenInRank[abilityID] {
					return fmt.Errorf("ability %q is listed twice in abilityPoolsByRank[%q]", abilityID, rankName)
				}
				seenInRank[abilityID] = true
			}
		}
	}
	return nil
}

// pathDerivedMaps bundles the 13 derived path-catalog maps so
// registerPathFileInto can populate either the live package-global maps
// (registerPathFileLocked's use, unchanged from Task 2) or a fresh throwaway
// set (path_persistence.go's rebuildDerivedPathMaps, which builds an entire
// merged catalog from scratch before ever touching what readers see). Field
// names mirror the global variable names 1:1.
type pathDerivedMaps struct {
	modifiersByKey        map[string]pathModifierDef
	boundsByPath          map[string]json.RawMessage
	attackOriginByPath    map[string]json.RawMessage
	shadowByPath          map[string]json.RawMessage
	visionRangeByPath     map[string]float64
	projectileByPath      map[string]string
	damageTypeByPath      map[string]DamageType
	attackTypeByPath      map[string]string
	projectileScaleByPath map[string]float64
	abilitiesByPath       map[string][]string
	perkRefsByPath        map[string]map[string][]string
	abilityPoolsByPath    map[string]map[string][]string
	abilityStatsByPath    map[string]map[string]map[string]AbilityStatMod // path -> rank -> stat id
	channelLoopByPath     map[string]ChannelLoopRange
	pathsByUnitType       map[string][]string
}

// newPathDerivedMaps returns a pathDerivedMaps wrapping 13 brand-new, empty
// maps — the "fresh" side of the build-then-swap rebuild.
func newPathDerivedMaps() *pathDerivedMaps {
	return &pathDerivedMaps{
		modifiersByKey:        make(map[string]pathModifierDef, 16),
		boundsByPath:          map[string]json.RawMessage{},
		attackOriginByPath:    map[string]json.RawMessage{},
		shadowByPath:          map[string]json.RawMessage{},
		visionRangeByPath:     map[string]float64{},
		projectileByPath:      map[string]string{},
		damageTypeByPath:      map[string]DamageType{},
		attackTypeByPath:      map[string]string{},
		projectileScaleByPath: map[string]float64{},
		abilitiesByPath:       map[string][]string{},
		perkRefsByPath:        map[string]map[string][]string{},
		abilityPoolsByPath:    map[string]map[string][]string{},
		abilityStatsByPath:    map[string]map[string]map[string]AbilityStatMod{},
		channelLoopByPath:     map[string]ChannelLoopRange{},
		pathsByUnitType:       map[string][]string{},
	}
}

// livePathDerivedMaps returns a pathDerivedMaps view over the package-global
// map variables — the SAME underlying maps, not copies (maps are reference
// types, so writes through the returned struct mutate the globals directly).
// This is what lets registerPathFileLocked keep its original single-call
// contract unchanged after the pathDerivedMaps generalization.
func livePathDerivedMaps() *pathDerivedMaps {
	return &pathDerivedMaps{
		modifiersByKey:        pathModifiersByKey,
		boundsByPath:          pathBoundsByPath,
		attackOriginByPath:    pathAttackOriginByPath,
		shadowByPath:          pathShadowByPath,
		visionRangeByPath:     pathVisionRangeByPath,
		projectileByPath:      pathProjectileByPath,
		damageTypeByPath:      pathDamageTypeByPath,
		attackTypeByPath:      pathAttackTypeByPath,
		projectileScaleByPath: pathProjectileScaleByPath,
		abilitiesByPath:       pathAbilitiesByPath,
		perkRefsByPath:        pathPerkRefsByPath,
		abilityPoolsByPath:    pathAbilityPoolsByPath,
		abilityStatsByPath:    pathAbilityStatsByPath,
		channelLoopByPath:     pathChannelLoopByPath,
		pathsByUnitType:       pathsByUnitType,
	}
}

// registerPathFileLocked writes an already-validated pathCatalogFile's data
// into the 12 package-global path-catalog maps (topology + per-path
// overrides + per-rank stat modifiers). file MUST have already passed
// validatePathFile — this function only guards against CROSS-file
// collisions (the same path/rank defined twice, or the same path getting
// two channelLoop/abilities overrides from different files); it does not
// re-check per-file validity.
//
// Caller holds pathCatalogMu.Lock(). Today's only caller is init(), which
// runs single-threaded before any goroutine exists and therefore calls this
// WITHOUT actually taking pathCatalogMu (see pathCatalogMu's doc comment
// above — same exemption Task 1 documented for init's direct map writes).
// The "Locked" suffix describes the contract for every OTHER caller: a
// future editor overlay reusing this function to register user-submitted
// path JSON at runtime MUST hold pathCatalogMu.Lock() first.
func registerPathFileLocked(unitKey string, file *pathCatalogFile) error {
	return registerPathFileInto(livePathDerivedMaps(), unitKey, file)
}

// registerPathFileInto is registerPathFileLocked's logic generalized to
// target any pathDerivedMaps instance — the live globals (via
// registerPathFileLocked) or a fresh scratch set (via
// rebuildDerivedPathMaps in path_persistence.go). See registerPathFileLocked
// for the locking contract; this function itself takes no lock and assumes
// the caller has already ensured dst is safe to mutate (either it wraps the
// globals and the caller holds pathCatalogMu, or it's a private fresh
// struct no other goroutine can see yet).
func registerPathFileInto(dst *pathDerivedMaps, unitKey string, file *pathCatalogFile) error {
	dst.pathsByUnitType[unitKey] = append(dst.pathsByUnitType[unitKey], file.Path)

	if len(file.Bounds) > 0 {
		dst.boundsByPath[file.Path] = file.Bounds
	}
	if len(file.AttackOrigin) > 0 {
		dst.attackOriginByPath[file.Path] = file.AttackOrigin
	}
	if len(file.Shadow) > 0 {
		dst.shadowByPath[file.Path] = file.Shadow
	}
	if file.VisionRange > 0 {
		dst.visionRangeByPath[file.Path] = file.VisionRange
	}
	if file.Projectile != "" {
		dst.projectileByPath[file.Path] = file.Projectile
	}
	if file.DamageType != "" {
		dst.damageTypeByPath[file.Path] = file.DamageType
	}
	if file.AttackType != "" {
		dst.attackTypeByPath[file.Path] = file.AttackType
	}
	if file.ProjectileScale > 0 {
		dst.projectileScaleByPath[file.Path] = file.ProjectileScale
	}
	if file.ChannelLoop != nil {
		if _, dup := dst.channelLoopByPath[file.Path]; dup {
			// Two files define a channelLoop for the same path id. Path ids
			// are globally unique; fail loud.
			return fmt.Errorf("duplicate path-level channelLoop override for %q", file.Path)
		}
		dst.channelLoopByPath[file.Path] = *file.ChannelLoop
	}
	if file.Abilities != nil {
		if _, dup := dst.abilitiesByPath[file.Path]; dup {
			return fmt.Errorf("duplicate path-level abilities override for %q", file.Path)
		}
		// Copy so the stored slice is independent of the caller's buffer.
		cp := make([]string, len(*file.Abilities))
		copy(cp, *file.Abilities)
		dst.abilitiesByPath[file.Path] = cp
	}
	if len(file.PerksByRank) > 0 {
		refs := make(map[string][]string, len(file.PerksByRank))
		for rankName, ids := range file.PerksByRank {
			cp := make([]string, len(ids))
			copy(cp, ids)
			refs[rankName] = cp
		}
		dst.perkRefsByPath[file.Path] = refs
	}
	if len(file.AbilityPoolsByRank) > 0 {
		pools := make(map[string][]string, len(file.AbilityPoolsByRank))
		for rankName, ids := range file.AbilityPoolsByRank {
			cp := make([]string, len(ids))
			copy(cp, ids)
			pools[rankName] = cp
		}
		dst.abilityPoolsByPath[file.Path] = pools
	}
	if len(file.AbilityStatsByRank) > 0 {
		byRank := make(map[string]map[string]AbilityStatMod, len(file.AbilityStatsByRank))
		for rankName, stats := range file.AbilityStatsByRank {
			cp := make(map[string]AbilityStatMod, len(stats))
			for id, mod := range stats {
				cp[id] = mod
			}
			byRank[rankName] = cp
		}
		dst.abilityStatsByPath[file.Path] = byRank
	}
	for rankName, stats := range file.Ranks {
		key := pathModifierKey(file.Path, rankName)
		if _, exists := dst.modifiersByKey[key]; exists {
			// Two files define the same (path, rank) — e.g. if berserker
			// appeared under both soldier/paths and archer/paths. Path ids
			// are globally unique; fail loud.
			return fmt.Errorf("duplicate definition for %s", key)
		}
		// Attack-range fields are optional in the JSON. AttackRange (flat
		// override, in pixels) is preserved as-is; 0 means "no override".
		// AttackRangeMultiplier defaults to 1.0 when missing / zero so paths
		// that don't tune range continue to work without authoring the
		// field.
		attackRangeMult := stats.AttackRangeMultiplier
		if attackRangeMult <= 0 {
			attackRangeMult = 1.0
		}
		// Max-mana multiplier is optional: omitted / zero ⇒ 1.0, so a
		// non-caster path that never authors it does not zero a caster's
		// pool (and casters that don't tune mana keep the catalog value).
		maxMPMult := stats.MaxMPMultiplier
		if maxMPMult <= 0 {
			maxMPMult = 1.0
		}
		// Same optional-multiplier convention as maxMP: omitted / zero ⇒
		// 1.0, so a path that does not tune regen leaves it alone rather
		// than zeroing it.
		healthRegenMult := stats.HealthRegenMultiplier
		if healthRegenMult <= 0 {
			healthRegenMult = 1.0
		}
		dst.modifiersByKey[key] = pathModifierDef{
			Path:                  file.Path,
			Rank:                  rankName,
			MaxHPMultiplier:       stats.MaxHPMultiplier,
			MaxMPMultiplier:       maxMPMult,
			HealthRegenMultiplier: healthRegenMult,
			DamageMultiplier:      stats.DamageMultiplier,
			AttackSpeedMultiplier: stats.AttackSpeedMultiplier,
			MoveSpeedMultiplier:   stats.MoveSpeedMultiplier,
			AttackRange:           stats.AttackRange,
			AttackRangeMultiplier: attackRangeMult,
			Armor:                 stats.Armor,
			DodgeChance:           stats.DodgeChance,
			BlockChance:           stats.BlockChance,
			VisionRange:           stats.VisionRange,
			BaseStats:             copyBaseStats(stats.BaseStats),
		}
	}
	return nil
}

// embeddedPathFiles / embeddedPathUnit snapshot the embedded catalog (path
// id -> parsed file / owning unit type) so a runtime rebuild
// (rebuildDerivedPathMaps in path_persistence.go) can regenerate the 10
// derived maps by merging this baseline with the writable overlay, without
// re-walking the embed FS. Populated once by init() below via
// clonePathCatalogFile (a deep-enough copy, so nothing can mutate the
// baseline afterward); read-only for the rest of the process's life, so
// reads elsewhere in the package do not take a lock — same convention as
// unitDefsByType.
var embeddedPathFiles = map[string]*pathCatalogFile{}
var embeddedPathUnit = map[string]string{}

// clonePathCatalogFile returns a deep-enough copy of file: every field that
// holds map/slice/pointer state (Bounds, AttackOrigin, Abilities,
// ChannelLoop, Ranks, PerksByRank, AbilityPoolsByRank) gets its own backing storage so a later mutation to
// the original — or to a copy taken from a live overlay entry — can never
// reach back into the embedded baseline snapshot. Scalar fields copy by
// value via the struct assignment.
func clonePathCatalogFile(file *pathCatalogFile) *pathCatalogFile {
	cp := *file
	if file.Bounds != nil {
		cp.Bounds = append(json.RawMessage(nil), file.Bounds...)
	}
	if file.AttackOrigin != nil {
		cp.AttackOrigin = append(json.RawMessage(nil), file.AttackOrigin...)
	}
	if file.Shadow != nil {
		cp.Shadow = append(json.RawMessage(nil), file.Shadow...)
	}
	if file.Abilities != nil {
		abilities := append([]string(nil), (*file.Abilities)...)
		cp.Abilities = &abilities
	}
	if file.ChannelLoop != nil {
		loop := *file.ChannelLoop
		cp.ChannelLoop = &loop
	}
	if file.Ranks != nil {
		ranks := make(map[string]pathRankStatsJSON, len(file.Ranks))
		for k, v := range file.Ranks {
			ranks[k] = v
		}
		cp.Ranks = ranks
	}
	if file.PerksByRank != nil {
		refs := make(map[string][]string, len(file.PerksByRank))
		for rankName, ids := range file.PerksByRank {
			refs[rankName] = append([]string(nil), ids...)
		}
		cp.PerksByRank = refs
	}
	if file.AbilityPoolsByRank != nil {
		pools := make(map[string][]string, len(file.AbilityPoolsByRank))
		for rankName, ids := range file.AbilityPoolsByRank {
			pools[rankName] = append([]string(nil), ids...)
		}
		cp.AbilityPoolsByRank = pools
	}
	return &cp
}

func init() {
	// Layout:
	//   catalog/units/<faction>/<unit>/paths/<path>/<path>.json  — per-path stat curve
	//   catalog/units/<faction>/<unit>/paths/<path>/perks/*.json — per-rank perk pool
	//                                                              (loaded in perk_defs.go)
	//
	// Walk each unit's paths/ subfolder under each faction directory; each
	// entry is a path directory containing the JSON at <path>/<path>.json.
	// Units without promotion paths (worker, raider) simply have no paths/ dir.
	factionEntries, err := fs.ReadDir(pathDefsFS, "catalog/units")
	if err != nil {
		panic("catalog/units: " + err.Error())
	}
	pathModifiersByKey = make(map[string]pathModifierDef, 16)

	for _, factionEntry := range factionEntries {
		if !factionEntry.IsDir() {
			continue // unit_defs.go already panics on stray files
		}
		factionKey := factionEntry.Name()
		unitEntries, err := fs.ReadDir(pathDefsFS, "catalog/units/"+factionKey)
		if err != nil {
			continue
		}
		for _, unitEntry := range unitEntries {
			if !unitEntry.IsDir() {
				continue
			}
			unitKey := unitEntry.Name()
			pathsDir := "catalog/units/" + factionKey + "/" + unitKey + "/paths"

			pathEntries, err := fs.ReadDir(pathDefsFS, pathsDir)
			if err != nil {
				continue // no paths/ — this unit has no promotion paths
			}

			for _, pathEntry := range pathEntries {
				if !pathEntry.IsDir() {
					// Each entry under paths/ is now a directory (<path>/). A
					// loose file here is a structural mistake — panic so the
					// mismatch is caught at startup.
					panic(fmt.Sprintf("%s: unexpected file %q — paths/ must contain path directories, not loose files",
						pathsDir, pathEntry.Name()))
				}
				pathKey := pathEntry.Name()
				rel := pathsDir + "/" + pathKey + "/" + pathKey + ".json"
				data, err := pathDefsFS.ReadFile(rel)
				if err != nil {
					panic(rel + ": " + err.Error())
				}
				var file pathCatalogFile
				if err := json.Unmarshal(data, &file); err != nil {
					panic(rel + ": " + err.Error())
				}
				if err := validatePathFile(&file, pathKey); err != nil {
					panic(rel + ": " + err.Error())
				}
				// init runs single-threaded before any goroutine exists (see
				// pathCatalogMu's doc comment above), so this call to
				// registerPathFileLocked is made WITHOUT holding
				// pathCatalogMu — safe here only because of that guarantee.
				// Any other caller (e.g. a future editor overlay) MUST hold
				// pathCatalogMu.Lock() first.
				if err := registerPathFileLocked(unitKey, &file); err != nil {
					panic(rel + ": " + err.Error())
				}
				// Snapshot the embedded baseline (deep copy — see
				// clonePathCatalogFile) so a future runtime rebuild
				// (path_persistence.go) can regenerate the derived maps by
				// merging this with the writable overlay, without
				// re-walking the embed FS.
				embeddedPathFiles[file.Path] = clonePathCatalogFile(&file)
				embeddedPathUnit[file.Path] = unitKey
			}
		}
	}

	// Cross-validate UnitDef.PathChances now that pathsByUnitType is populated.
	// Unit defs load via a var initializer (runs before this init), so this is
	// the earliest point both catalogs are available. Every weighted path id a
	// unit can roll must be a real path directory under that unit — a typo here
	// would otherwise silently leave the unit on unitPathNone at runtime.
	for _, unitType := range sortedUnitTypesForPathValidation() {
		def, _ := getUnitDef(unitType)
		if len(def.PathChances) == 0 {
			continue
		}
		known := make(map[string]struct{}, len(pathsByUnitType[unitType]))
		for _, p := range pathsByUnitType[unitType] {
			known[p] = struct{}{}
		}
		for path := range def.PathChances {
			if _, ok := known[path]; !ok {
				panic(fmt.Sprintf("unit %q: pathChances references %q, which is not a path directory under catalog/units/<faction>/%s/paths/",
					unitType, path, unitType))
			}
		}
	}
}

// sortedUnitTypesForPathValidation returns the unit-def keys in a stable order
// so the PathChances cross-validation panic (if any) is deterministic rather
// than depending on map iteration order.
func sortedUnitTypesForPathValidation() []string {
	types := make([]string, 0, len(unitDefsByType))
	for unitType := range unitDefsByType {
		types = append(types, unitType)
	}
	sort.Strings(types)
	return types
}

// pathAbilityStatsFor returns the ability stats a unit on `path` carries at
// `rank`, or nil.
//
// Values are ABSOLUTE per rank (see PathDef.AbilityStatsByRank) — gold's number
// for a stat replaces silver's rather than adding to it — but a rank that does
// not mention a stat INHERITS it, so resolution folds bronze up to `rank` with
// the highest authored rank winning. Reading only the current rank's block
// dropped a bronze-authored stat the moment the unit promoted, which is the
// opposite of the floor the editor and validatePathAbilityStatsByRank promise.
func pathAbilityStatsFor(path, rank string) map[string]AbilityStatMod {
	top := pathRankIndex(rank)
	if path == "" || path == unitPathNone || top < 0 {
		return nil
	}
	pathCatalogMu.RLock()
	defer pathCatalogMu.RUnlock()
	byRank, ok := pathAbilityStatsByPath[path]
	if !ok {
		return nil
	}
	var out map[string]AbilityStatMod
	for _, r := range pathRankOrder[:top+1] {
		if stats := byRank[r]; len(stats) > 0 {
			if out == nil {
				out = make(map[string]AbilityStatMod, len(stats))
			}
			for id, mod := range stats {
				out[id] = mod
			}
		}
	}
	return out
}

// pathRankOrder is bronze -> silver -> gold, the order a unit promotes through.
// Used to check that a per-rank block never regresses.
var pathRankOrder = []string{unitRankBronze, unitRankSilver, unitRankGold}

// pathRankIndex is rank's position in pathRankOrder, or -1 for anything that is
// not a promotion rank (base, or a typo). Callers that fold "every rank up to
// this one" MUST reject -1 rather than treat it as "fold them all" — base rank
// would then inherit gold's numbers.
func pathRankIndex(rank string) int {
	for i, r := range pathRankOrder {
		if r == rank {
			return i
		}
	}
	return -1
}

// validatePathAbilityStatsByRank checks a path's abilityStatsByRank: real rank
// keys, real stat ids, finite values, the same flat-only rule the unit/item
// blocks follow — and that no stat DECREASES as the unit promotes.
//
// The monotonic check is the load-time twin of the editor's hard floor. The
// blocks are ABSOLUTE per rank, so a gold value below silver's would silently
// make a promoted unit WEAKER — a regression no rank-up should ever produce,
// and one nothing else in the game would surface.
func validatePathAbilityStatsByRank(pathID string, byRank map[string]map[string]AbilityStatMod) error {
	if len(byRank) == 0 {
		return nil
	}
	for rank := range byRank {
		switch rank {
		case unitRankBronze, unitRankSilver, unitRankGold:
		default:
			return fmt.Errorf("path %q: abilityStatsByRank has unknown rank %q (want %q, %q or %q)",
				pathID, rank, unitRankBronze, unitRankSilver, unitRankGold)
		}
		if err := validateAbilityStats(fmt.Sprintf("path %q rank %q", pathID, rank), byRank[rank]); err != nil {
			return err
		}
	}

	// Walk bronze -> silver -> gold, carrying the highest value seen for each
	// stat. A rank that omits a stat inherits the previous rank's value rather
	// than dropping to zero, which is what "absolute per rank" means in practice.
	best := map[string]AbilityStatMod{}
	for _, rank := range pathRankOrder {
		stats := byRank[rank]
		for id, mod := range stats {
			prev, seen := best[id]
			if seen {
				if mod.Flat < prev.Flat {
					return fmt.Errorf("path %q: abilityStatsByRank[%q][%q].flat is %v, below the %v carried from an earlier rank — a promotion must never weaken a unit",
						pathID, rank, id, mod.Flat, prev.Flat)
				}
				if mod.Pct < prev.Pct {
					return fmt.Errorf("path %q: abilityStatsByRank[%q][%q].pct is %v, below the %v carried from an earlier rank — a promotion must never weaken a unit",
						pathID, rank, id, mod.Pct, prev.Pct)
				}
			}
			best[id] = mod
		}
	}
	return nil
}

// validatePathRankBaseStats checks the per-rank baseStats blocks: every key must
// be a stat a unit may carry a base for (statBaseAuthorable — the same set
// UnitDef.BaseStats accepts, which excludes anything with a typed field so there
// is never a double source), values finite, and no stat decreasing as the unit
// promotes.
//
// The monotonic rule is the load-time twin of the editor's floor. These are
// ABSOLUTE totals, so a gold value below silver's would make a promoted unit
// weaker at a stat — a regression nothing else would surface.
func validatePathRankBaseStats(pathID string, ranks map[string]pathRankStatsJSON) error {
	best := map[string]float64{}
	for _, rank := range pathRankOrder {
		stats := ranks[rank].BaseStats
		if len(stats) == 0 {
			continue
		}
		ids := make([]string, 0, len(stats))
		for id := range stats {
			ids = append(ids, id)
		}
		sort.Strings(ids) // deterministic error messages
		for _, id := range ids {
			if !isBaseAuthorableStat(id) {
				return fmt.Errorf("path %q: ranks[%q].baseStats[%q] is not a stat a unit can carry a base for (allowed: %v)",
					pathID, rank, id, baseAuthorableStatIDs())
			}
			v := stats[id]
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("path %q: ranks[%q].baseStats[%q] must be finite, got %v", pathID, rank, id, v)
			}
			if prev, seen := best[id]; seen && v < prev {
				return fmt.Errorf("path %q: ranks[%q].baseStats[%q] is %v, below the %v carried from an earlier rank — a promotion must never weaken a unit",
					pathID, rank, id, v, prev)
			}
			best[id] = v
		}
	}
	return nil
}
