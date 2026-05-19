package game

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
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
	// VisionRange overrides BaseVisionRange for units on this path, in world pixels.
	// When 0 or absent, the unit's BaseVisionRange (from its unit def) is used.
	VisionRange float64 `json:"visionRange,omitempty"`
	// Projectile overrides the unit def's Projectile (the ProjectileDef id the
	// basic ranged attack fires) for units promoted onto this path — e.g. the
	// Cleric firing "holy_bolt" instead of the Apprentice's "fire_bolt". Empty
	// ⇒ keep whatever the unit def set at spawn. Validated at load against the
	// projectile catalog (same fail-loud contract as UnitDef.Projectile).
	Projectile string `json:"projectile,omitempty"`
	// DamageType overrides the unit def's DamageType (the basic attack's
	// element/school tag) for units on this path. Optional flavor/metadata,
	// same as UnitDef.DamageType; empty ⇒ keep the unit def's type. Validated
	// at load when non-empty.
	DamageType DamageType `json:"damageType,omitempty"`
	// ProjectileScale overrides the unit def's ProjectileScale (the per-unit
	// projectile-sprite render multiplier) for units promoted onto this path,
	// so two paths of the same base unit (e.g. Apprentice → Cleric vs Arch
	// Mage) can size their shots independently. Purely visual; > 0 ⇒ override,
	// omitted / 0 ⇒ keep whatever the unit def set at spawn. Validated >= 0.
	ProjectileScale float64 `json:"projectileScale,omitempty"`
	Ranks           map[string]pathRankStatsJSON `json:"ranks"`
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
	MaxHPMultiplier       float64 `json:"maxHPMultiplier"`
	DamageMultiplier      float64 `json:"damageMultiplier"`
	AttackSpeedMultiplier float64 `json:"attackSpeedMultiplier"`
	MoveSpeedMultiplier   float64 `json:"moveSpeedMultiplier"`
	AttackRange           float64 `json:"attackRange"`
	AttackRangeMultiplier float64 `json:"attackRangeMultiplier"`
	Armor                 int     `json:"armor"`
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

// pathProjectileScaleByPath holds the optional per-path projectile-sprite
// render multiplier override, keyed by path id (e.g. "cleric": 1.5). A path
// absent from the map means "no override — keep the unit def's
// ProjectileScale". Only paths declaring a positive value are stored, so an
// omitted / zero field never zeroes the unit-def value. Applied in
// applyRankModifiersLocked.
var pathProjectileScaleByPath = map[string]float64{}

// PathBoundsEntry is the shape served to the client: a path id plus its
// raw bounds blob. Slice form (rather than map) gives stable ordering in
// the JSON response.
type PathBoundsEntry struct {
	Path   string          `json:"path"`
	Bounds json.RawMessage `json:"bounds"`
}

// ListPathBounds returns all paths that declared a bounds override, sorted
// by path id. Mirrors ListUnitDefs / ListBuildingDefs.
func ListPathBounds() []PathBoundsEntry {
	out := make([]PathBoundsEntry, 0, len(pathBoundsByPath))
	for path, bounds := range pathBoundsByPath {
		out = append(out, PathBoundsEntry{Path: path, Bounds: bounds})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
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
var defaultRankCurve = map[string]pathModifierDef{
	unitRankBronze: {Path: unitPathNone, Rank: unitRankBronze, MaxHPMultiplier: 1.10, DamageMultiplier: 1.10, AttackSpeedMultiplier: 1.00, MoveSpeedMultiplier: 1.00, AttackRangeMultiplier: 1.0, Armor: 0},
	unitRankSilver: {Path: unitPathNone, Rank: unitRankSilver, MaxHPMultiplier: 1.20, DamageMultiplier: 1.25, AttackSpeedMultiplier: 1.10, MoveSpeedMultiplier: 1.00, AttackRangeMultiplier: 1.0, Armor: 0},
	unitRankGold:   {Path: unitPathNone, Rank: unitRankGold, MaxHPMultiplier: 1.35, DamageMultiplier: 1.50, AttackSpeedMultiplier: 1.25, MoveSpeedMultiplier: 1.00, AttackRangeMultiplier: 1.0, Armor: 0},
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
				if file.Path == "" {
					panic(rel + `: missing "path" field`)
				}
				if file.Path != pathKey {
					// Directory name is the canonical path id. A mismatch means
					// someone edited one without the other; fail loud so the
					// catalog stays coherent.
					panic(fmt.Sprintf("%s: path %q does not match directory name %q", rel, file.Path, pathKey))
				}
				if len(file.Bounds) > 0 {
					pathBoundsByPath[file.Path] = file.Bounds
				}
				if file.VisionRange > 0 {
					pathVisionRangeByPath[file.Path] = file.VisionRange
				}
				if file.Projectile != "" {
					if _, ok := getProjectileDef(file.Projectile); !ok {
						panic(rel + `: projectile "` + file.Projectile + `" is not a registered projectile def`)
					}
					pathProjectileByPath[file.Path] = file.Projectile
				}
				if file.DamageType != "" {
					if !IsValidDamageType(file.DamageType) {
						panic(rel + `: damageType "` + string(file.DamageType) + `" is not a registered damage type`)
					}
					pathDamageTypeByPath[file.Path] = file.DamageType
				}
				if file.ProjectileScale < 0 {
					panic(rel + `: projectileScale must be >= 0 (0/omitted ⇒ keep the unit def value)`)
				}
				if file.ProjectileScale > 0 {
					pathProjectileScaleByPath[file.Path] = file.ProjectileScale
				}
				for rankName, stats := range file.Ranks {
					if _, ok := validRankName[rankName]; !ok {
						panic(fmt.Sprintf("%s: unknown rank %q (want bronze/silver/gold)", rel, rankName))
					}
					key := pathModifierKey(file.Path, rankName)
					if _, exists := pathModifiersByKey[key]; exists {
						// Two files define the same (path, rank) — e.g. if
						// berserker appeared under both soldier/paths and
						// archer/paths. Path ids are globally unique; fail loud.
						panic(fmt.Sprintf("%s: duplicate definition for %s", rel, key))
					}
					// Attack-range fields are optional in the JSON. AttackRange
					// (flat override, in pixels) is preserved as-is; 0 means
					// "no override". AttackRangeMultiplier defaults to 1.0 when
					// missing / zero so paths that don't tune range continue to
					// work without authoring the field.
					attackRangeMult := stats.AttackRangeMultiplier
					if attackRangeMult <= 0 {
						attackRangeMult = 1.0
					}
					pathModifiersByKey[key] = pathModifierDef{
						Path:                  file.Path,
						Rank:                  rankName,
						MaxHPMultiplier:       stats.MaxHPMultiplier,
						DamageMultiplier:      stats.DamageMultiplier,
						AttackSpeedMultiplier: stats.AttackSpeedMultiplier,
						MoveSpeedMultiplier:   stats.MoveSpeedMultiplier,
						AttackRange:           stats.AttackRange,
						AttackRangeMultiplier: attackRangeMult,
						Armor:                 stats.Armor,
					}
				}
			}
		}
	}
}
