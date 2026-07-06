package game

import (
	"encoding/json"
	"io/fs"
	"sort"
	"strconv"
)

// UnitUpgradeTrack is the catalog shape for a single unit type's building-driven
// upgrade line, loaded from catalog/units/<faction>/<unit>/upgrades.json. A
// track is purchased at any building that holds the track's Capability (e.g.
// "blacksmith-upgrade") and advances through Tiers in order. This replaces the
// former hardcoded upgradeTrackDefs slice; adding a track (or a new building
// that offers upgrades, like a chapel for acolytes) is now a pure JSON change.
//
// Not to be confused with UpgradeDef in upgrade_defs.go, which is the separate
// between-waves roguelike upgrade system.
type UnitUpgradeTrack struct {
	// UnitType is the unit these upgrades apply to. Equals the UpgradeTrack key
	// and the unit's UnitType — the lookup contract applyPlayerUpgradesAtSpawnLocked
	// relies on.
	UnitType string `json:"unitType"`
	// Capability is the building capability that offers this track. A building
	// whose Capabilities include this string can research the track.
	Capability string `json:"capability"`
	// DisplayName is the label shown in the upgrade UI.
	DisplayName string `json:"displayName"`
	// ResearchSeconds is how long each level of this track takes to research.
	// Defaults to blacksmithUpgradeResearchSeconds when omitted/zero.
	ResearchSeconds float64 `json:"researchSeconds"`
	// Tiers is the ordered list of levels, validated contiguous from level 1.
	Tiers []UnitUpgradeTier `json:"tiers"`
}

// UnitUpgradeTier is one level within a track: its cost, an optional building
// prerequisite, and the stat effects granted when the level is reached.
type UnitUpgradeTier struct {
	// Level is the 1-based level this tier grants. Validated to equal its index+1.
	Level int `json:"level"`
	// Cost is the resource cost to purchase this level (e.g. {"gold":150,"wood":150}).
	Cost map[string]int `json:"cost"`
	// RequiresBuilding gates this tier. Empty = no gate. A value naming a building
	// in the townhall upgrade chain (townhall/keep/castle) maps to a townhall-tier
	// requirement; any other value requires owning a fully-built building of that
	// type. See upgradeRequirementMetLocked.
	RequiresBuilding string `json:"requiresBuilding,omitempty"`
	// Effects are the stat bonuses applied when this level completes (retro to
	// live units) and summed 1..level at spawn for new units.
	Effects []UnitUpgradeEffect `json:"effects"`
}

// UnitUpgradeEffect mirrors the advancement effect authoring shape but uses a
// float Amount so fractional additive bonuses (e.g. +0.05 attack speed) are
// expressible. Only "unitStatAdd" is supported today.
type UnitUpgradeEffect struct {
	Kind   string  `json:"kind"`
	Stat   string  `json:"stat"`
	Amount float64 `json:"amount"`
}

// upgradeStatNames is the set of unit stats an upgrade effect may target. These
// all have a Base* field on Unit that applyUpgradeStatDeltaToUnit writes and
// applyRankModifiersLocked rebakes from.
var upgradeStatNames = map[string]bool{
	"maxHp":       true,
	"damage":      true,
	"armor":       true,
	"attackSpeed": true,
	"moveSpeed":   true,
}

// tierByLevel returns the tier granting the given 1-based level (Tiers are
// validated contiguous from 1 in file order), and whether it exists.
func (t UnitUpgradeTrack) tierByLevel(level int) (UnitUpgradeTier, bool) {
	if level < 1 || level > len(t.Tiers) {
		return UnitUpgradeTier{}, false
	}
	return t.Tiers[level-1], true
}

// resolvedResearchSeconds returns the track's per-level research duration,
// falling back to the package default when the JSON omits it.
func (t UnitUpgradeTrack) resolvedResearchSeconds() float64 {
	if t.ResearchSeconds > 0 {
		return t.ResearchSeconds
	}
	return blacksmithUpgradeResearchSeconds
}

// unitUpgradeTracksByUnitType is the flat catalog keyed by unit type;
// unitUpgradeTracks is the same set sorted by unit type for deterministic
// iteration (snapshots). Both are loaded once at startup and never mutated. The
// closure references unitDefsByType and buildingDefsByType so Go's init-order
// analysis loads the unit and building catalogs first — loadUnitUpgradeDefs
// calls getUnitDef and validates requiresBuilding against the building catalog.
var unitUpgradeTracksByUnitType, unitUpgradeTracks = func() (map[string]UnitUpgradeTrack, []UnitUpgradeTrack) {
	_ = unitDefsByType
	_ = buildingDefsByType
	return loadUnitUpgradeDefs()
}()

// loadUnitUpgradeDefs walks catalog/units/**/upgrades.json and builds the unit
// upgrade catalog. Files are optional (a unit without one simply has no
// upgrades). The loader panics on malformed JSON or violated invariants
// (non-contiguous levels, unknown stat, unknown requiresBuilding type, etc.) so
// misconfiguration fails fast at startup rather than mid-match.
func loadUnitUpgradeDefs() (map[string]UnitUpgradeTrack, []UnitUpgradeTrack) {
	byType := make(map[string]UnitUpgradeTrack, 4)
	var tracks []UnitUpgradeTrack

	factionEntries, err := fs.ReadDir(unitDefsFS, "catalog/units")
	if err != nil {
		panic("catalog/units (upgrades): " + err.Error())
	}

	for _, factionEntry := range factionEntries {
		if !factionEntry.IsDir() {
			continue
		}
		factionKey := factionEntry.Name()
		unitEntries, err := fs.ReadDir(unitDefsFS, "catalog/units/"+factionKey)
		if err != nil {
			panic("catalog/units/" + factionKey + " (upgrades): " + err.Error())
		}
		for _, unitEntry := range unitEntries {
			if !unitEntry.IsDir() {
				continue
			}
			unitKey := unitEntry.Name()
			rel := "catalog/units/" + factionKey + "/" + unitKey + "/upgrades.json"
			data, readErr := unitDefsFS.ReadFile(rel)
			if readErr != nil {
				// No upgrades.json for this unit — skip silently.
				continue
			}

			var track UnitUpgradeTrack
			if err := json.Unmarshal(data, &track); err != nil {
				panic(rel + ": " + err.Error())
			}
			validateUnitUpgradeTrack(rel, track)
			if _, dup := byType[track.UnitType]; dup {
				panic(rel + `: duplicate upgrade track for unit type "` + track.UnitType + `"`)
			}
			byType[track.UnitType] = track
			tracks = append(tracks, track)
		}
	}

	// Deterministic order for snapshot emission and any range-over iteration.
	sort.Slice(tracks, func(i, j int) bool { return tracks[i].UnitType < tracks[j].UnitType })
	return byType, tracks
}

// validateUnitUpgradeTrack panics with a descriptive, path-qualified message if
// the track violates an invariant. Called once per file at load.
func validateUnitUpgradeTrack(rel string, track UnitUpgradeTrack) {
	if track.UnitType == "" {
		panic(rel + `: track missing "unitType"`)
	}
	if _, ok := getUnitDef(track.UnitType); !ok {
		panic(rel + `: unitType "` + track.UnitType + `" is not in the unit catalog`)
	}
	if track.Capability == "" {
		panic(rel + `: track missing "capability"`)
	}
	if len(track.Tiers) == 0 {
		panic(rel + `: track has no tiers`)
	}
	for i, tier := range track.Tiers {
		lvl := strconv.Itoa(tier.Level)
		if tier.Level != i+1 {
			panic(rel + `: tier at index ` + strconv.Itoa(i) + ` must have level ` + strconv.Itoa(i+1) + `, got ` + lvl)
		}
		if len(tier.Cost) == 0 {
			panic(rel + `: tier level ` + lvl + ` has no cost`)
		}
		for res, amt := range tier.Cost {
			if amt <= 0 {
				panic(rel + `: tier level ` + lvl + ` cost "` + res + `" must be > 0`)
			}
		}
		if tier.RequiresBuilding != "" {
			if _, ok := getBuildingDef(tier.RequiresBuilding); !ok {
				panic(rel + `: tier level ` + lvl + ` requiresBuilding "` + tier.RequiresBuilding + `" is not a known building type`)
			}
		}
		if len(tier.Effects) == 0 {
			panic(rel + `: tier level ` + lvl + ` has no effects`)
		}
		for ei, eff := range tier.Effects {
			where := rel + ` tier level ` + lvl + ` effect[` + strconv.Itoa(ei) + `]`
			if eff.Kind != "unitStatAdd" {
				panic(where + `: unknown kind "` + eff.Kind + `" (only "unitStatAdd" is supported)`)
			}
			if !upgradeStatNames[eff.Stat] {
				panic(where + `: unknown stat "` + eff.Stat + `"`)
			}
			if eff.Amount == 0 {
				panic(where + `: requires non-zero amount`)
			}
		}
	}
}

// upgradeTrackDefByID returns the upgrade track for the given track key (== unit
// type) and whether it was found.
func upgradeTrackDefByID(track UpgradeTrack) (UnitUpgradeTrack, bool) {
	def, ok := unitUpgradeTracksByUnitType[string(track)]
	return def, ok
}

// upgradeCostForLevel returns the gold cost to purchase the given 1-based level
// of a track. Returns 0 when the level is out of range.
func upgradeCostForLevel(track UnitUpgradeTrack, level int) int {
	gold, _ := upgradeTierCost(track, level)
	return gold
}

// upgradeTierCost returns the (gold, wood) cost to purchase the given 1-based
// level. Missing resource keys read as 0. Returns (0, 0) when out of range.
func upgradeTierCost(track UnitUpgradeTrack, level int) (gold, wood int) {
	tier, ok := track.tierByLevel(level)
	if !ok {
		return 0, 0
	}
	return tier.Cost["gold"], tier.Cost["wood"]
}
