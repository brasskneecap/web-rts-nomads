package game

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
)

// Embeds the per-unit catalog tree so this file can load path JSONs from
// catalog/units/<unit>/paths/*.json. unit_defs.go embeds the same tree for
// unit-def loading; both init functions filter the tree independently.
//
//go:embed catalog/units
var pathDefsFS embed.FS

// pathCatalogFile is the on-disk shape of a single
// catalog/units/<unit>/paths/<path>.json. One file per promotion path for
// that unit; the file carries per-rank stat multipliers in a ranks map so
// editing a single (path, rank) cell is a one-number change with no risk
// of contaminating another path.
type pathCatalogFile struct {
	Path        string                       `json:"path"`
	Description string                       `json:"description,omitempty"`
	Ranks       map[string]pathRankStatsJSON `json:"ranks"`
}

// pathRankStatsJSON mirrors the stat-modifier fields of pathModifierDef (the
// in-memory struct) with json tags. A separate type keeps pathModifierDef
// free of serialization concerns and lets the loader validate rank keys
// before exposing them to the rest of the codebase.
type pathRankStatsJSON struct {
	MaxHPMultiplier       float64 `json:"maxHPMultiplier"`
	DamageMultiplier      float64 `json:"damageMultiplier"`
	AttackSpeedMultiplier float64 `json:"attackSpeedMultiplier"`
	MoveSpeedMultiplier   float64 `json:"moveSpeedMultiplier"`
	Armor                 int     `json:"armor"`
}

// pathModifiersByKey is the loaded lookup map. Key is path + "/" + rank (e.g.
// "vanguard/bronze"). Missing entries resolve to identityPathModifier via
// pathModifierFor — a typo in a path id therefore fails loud in-game (stats
// unchanged from base) rather than silently picking an unrelated row.
var pathModifiersByKey map[string]pathModifierDef

// defaultRankCurve is the rank-progression multiplier for units that earn XP
// without ever being assigned a promotion path — workers, raiders, and any
// future utility units. Used by pathModifierFor when path == unitPathNone.
//
// Not configurable via JSON because "none" is not a player-facing path, it's
// a system fallback. Changing these numbers is a structural-progression
// decision (affects every path-less unit uniformly) and belongs in code.
// If you want the Vanguard or Berserker curve tuned instead, edit the file
// under catalog/units/<unit>/paths/ — those ARE JSON-configurable.
var defaultRankCurve = map[string]pathModifierDef{
	unitRankBronze: {Path: unitPathNone, Rank: unitRankBronze, MaxHPMultiplier: 1.10, DamageMultiplier: 1.10, AttackSpeedMultiplier: 1.00, MoveSpeedMultiplier: 1.00, Armor: 0},
	unitRankSilver: {Path: unitPathNone, Rank: unitRankSilver, MaxHPMultiplier: 1.20, DamageMultiplier: 1.25, AttackSpeedMultiplier: 1.10, MoveSpeedMultiplier: 1.00, Armor: 0},
	unitRankGold:   {Path: unitPathNone, Rank: unitRankGold, MaxHPMultiplier: 1.35, DamageMultiplier: 1.50, AttackSpeedMultiplier: 1.25, MoveSpeedMultiplier: 1.00, Armor: 0},
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
	// Walk each unit directory looking for a paths/ subfolder. Units without
	// promotion paths (worker, raider) simply have no paths/ dir — no error.
	unitEntries, err := fs.ReadDir(pathDefsFS, "catalog/units")
	if err != nil {
		panic("catalog/units: " + err.Error())
	}
	pathModifiersByKey = make(map[string]pathModifierDef, len(unitEntries)*3)

	for _, unitEntry := range unitEntries {
		if !unitEntry.IsDir() {
			continue // unit_defs.go already panics on stray files
		}
		unitKey := unitEntry.Name()
		pathsDir := "catalog/units/" + unitKey + "/paths"

		pathEntries, err := fs.ReadDir(pathDefsFS, pathsDir)
		if err != nil {
			// No paths/ subfolder for this unit — the unit simply doesn't
			// have promotion paths (e.g. worker, raider). Not an error.
			continue
		}

		for _, pathEntry := range pathEntries {
			if pathEntry.IsDir() {
				continue
			}
			rel := pathsDir + "/" + pathEntry.Name()
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
			for rankName, stats := range file.Ranks {
				if _, ok := validRankName[rankName]; !ok {
					panic(fmt.Sprintf("%s: unknown rank %q (want bronze/silver/gold)", rel, rankName))
				}
				key := pathModifierKey(file.Path, rankName)
				if _, exists := pathModifiersByKey[key]; exists {
					// Two files define the same (path, rank) — e.g. if
					// vanguard.json appeared under both soldier/paths and
					// archer/paths. Path ids are globally unique, so this is
					// always a mistake; fail loud at startup.
					panic(fmt.Sprintf("%s: duplicate definition for %s", rel, key))
				}
				pathModifiersByKey[key] = pathModifierDef{
					Path:                  file.Path,
					Rank:                  rankName,
					MaxHPMultiplier:       stats.MaxHPMultiplier,
					DamageMultiplier:      stats.DamageMultiplier,
					AttackSpeedMultiplier: stats.AttackSpeedMultiplier,
					MoveSpeedMultiplier:   stats.MoveSpeedMultiplier,
					Armor:                 stats.Armor,
				}
			}
		}
	}
}
