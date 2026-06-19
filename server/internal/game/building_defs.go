package game

import (
	"embed"
	"encoding/json"
	"io/fs"
	"sort"
)

//go:embed catalog/buildings/*.json
var buildingDefsFS embed.FS

// BuildingDef holds the configuration for a buildable building type.
// Client-only fields (Color, Label, Hotkey) are passed through to the
// API as-is; the server game logic never reads them. Procedural render
// fallbacks are no longer carried in the catalog — they live frontend-side
// (see client buildingFallbackRender.ts).
type BuildingDef struct {
	Type         string  `json:"type"`
	Class        string  `json:"class,omitempty"`
	Buildable    *bool   `json:"buildable,omitempty"`
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	MaxHp        float64 `json:"maxHp"`
	BuildSeconds float64 `json:"buildSeconds"`
	Damage       int     `json:"damage,omitempty"`
	AttackRange  float64 `json:"attackRange,omitempty"`
	AttackSpeed  float64 `json:"attackSpeed,omitempty"`
	VisionRange  float64 `json:"visionRange,omitempty"`
	// UnobstructedVision, when true, makes this building's FOW vision ignore
	// line-of-sight blockers (trees/obstacles and terrain cliffs) within its
	// range — it sees over them, the way flyer units do. Omitted ⇒ normal,
	// occluded vision. Set on Tower so a tower placed in a forest still reveals
	// its full radius.
	UnobstructedVision bool            `json:"unobstructedVision,omitempty"`
	AttackVisual       json.RawMessage `json:"attackVisual,omitempty"`
	ResourceType       string          `json:"resourceType,omitempty"`
	ResourceAmount     int             `json:"resourceAmount,omitempty"`
	ResourceCost       map[string]int  `json:"resourceCost"`
	Capabilities       []string        `json:"capabilities"`
	SpawnUnitTypes     []string        `json:"spawnUnitTypes"`
	// RequiresTownhallTier gates construction: the owning player must control
	// at least one fully-built townhall whose tier is ≥ this value before
	// BuildBuilding accepts the placement. Zero/omitted ⇒ no requirement.
	// Tiers: 1 = Town Hall, 2 = Keep, 3 = Castle (mirrors the upgrade chain
	// in state_upgrades.go's handleUpgradeTownHallLocked).
	RequiresTownhallTier int `json:"requiresTownhallTier,omitempty"`
	// UpgradesFrom names the building type this one is a tier-up of (e.g. keep
	// upgradesFrom townhall, castle upgradesFrom keep). Empty for base/standalone
	// buildings. The defs form an ordered chain that the tier-up logic walks
	// instead of hardcoding tiers. NOTE: a placed building keeps its base type
	// plus a numeric `tier` in metadata — it does not change BuildingType on
	// upgrade. These tier defs supply the per-step cost, duration, and display
	// name; their other stats currently mirror the base (tiers are stat-identical
	// today) and are the hook for future per-tier balancing.
	UpgradesFrom string `json:"upgradesFrom,omitempty"`
	// UpgradeCost is the resource cost to upgrade INTO this tier (paired with
	// UpgradesFrom). Ignored on base buildings.
	UpgradeCost map[string]int `json:"upgradeCost,omitempty"`
	// UpgradeSeconds is how long the tier-up into this building takes.
	UpgradeSeconds float64        `json:"upgradeSeconds,omitempty"`
	Metadata       map[string]any `json:"metadata"`
	Color          string         `json:"color,omitempty"`
	Label          string         `json:"label,omitempty"`
	Hotkey         string         `json:"hotkey,omitempty"`
	// SpriteRender lets a building's sprite extend beyond its grid footprint
	// without affecting pathing, selection hit-testing, or the grid cells
	// the building occupies. Mirrors ObstacleRenderDef semantics (cell units,
	// omitted fields use footprint defaults). Used for e.g. a barracks with
	// a flag pole taller than its footprint, or a wider sprite that spills
	// half a cell over each side.
	SpriteRender *BuildingSpriteRenderDef `json:"spriteRender,omitempty"`
	// SelectionRing nudges the size/placement of the selection & hover ring
	// drawn around the building. Client-only (the server never reads it).
	// Mirrors ObstacleSelectionRingDef semantics. Omit the block to keep the
	// default ring derived from the footprint width.
	SelectionRing *BuildingSelectionRingDef `json:"selectionRing,omitempty"`
}

// BuildingSelectionRingDef sizes/places the selection & hover ellipse for a
// building. All fields are in *cell units*. Defaults (when a field is zero /
// omitted) reproduce the legacy footprint-derived ring:
//
//	radiusX = footprintWidth * 0.55
//	radiusY = radiusX * 0.34
//	center  = horizontally centered, ~0.62 down the footprint
//
// offsetX/offsetY (when set) are measured from the footprint's top-left
// corner. Setting only radiusX/radiusY enlarges the ring while keeping the
// default centering.
type BuildingSelectionRingDef struct {
	OffsetX float64 `json:"offsetX,omitempty"`
	OffsetY float64 `json:"offsetY,omitempty"`
	RadiusX float64 `json:"radiusX,omitempty"`
	RadiusY float64 `json:"radiusY,omitempty"`
}

// BuildingSpriteRenderDef is the sprite-overflow config for a building.
// Shape parallels ObstacleRenderDef. All fields are in cell units; zero
// means "use default" (offset=0, width/height = footprint).
type BuildingSpriteRenderDef struct {
	OffsetX float64 `json:"offsetX,omitempty"`
	OffsetY float64 `json:"offsetY,omitempty"`
	Width   float64 `json:"width,omitempty"`
	Height  float64 `json:"height,omitempty"`
}

// Class returns the building's class, defaulting to "player" when unspecified.
func (d BuildingDef) ClassOrDefault() string {
	if d.Class == "" {
		return "player"
	}
	return d.Class
}

// HpPerSecond returns the build/repair rate for this building type.
func (d BuildingDef) HpPerSecond() float64 {
	if d.BuildSeconds <= 0 {
		return d.MaxHp
	}
	return d.MaxHp / d.BuildSeconds
}

// buildingDefsByType MUST be a var initializer (not init()) so that
// Go's package-level dependency analysis can order it before any other
// var initializer that calls getBuildingDef — most importantly
// unitDefsByType's loader, which validates each unit's RequiresBuildings
// against the building catalog. All package-level var initializers run
// before any init() function, so converting this to init() would race
// with unitDefsByType (and break maps.go's placedUnits hydration via
// the `_ = unitDefsByType` dependency-injection marker).
var buildingDefsByType = loadBuildingDefsByType()

func loadBuildingDefsByType() map[string]BuildingDef {
	// Each file under catalog/buildings/ is a single BuildingDef object.
	entries, err := fs.ReadDir(buildingDefsFS, "catalog/buildings")
	if err != nil {
		panic("catalog/buildings: " + err.Error())
	}
	result := make(map[string]BuildingDef, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := buildingDefsFS.ReadFile("catalog/buildings/" + entry.Name())
		if err != nil {
			panic("catalog/buildings/" + entry.Name() + ": " + err.Error())
		}
		var def BuildingDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic("catalog/buildings/" + entry.Name() + ": " + err.Error())
		}
		result[def.Type] = def
	}
	return result
}

func getBuildingDef(buildingType string) (BuildingDef, bool) {
	def, ok := buildingDefsByType[buildingType]
	return def, ok
}

// upgradeChainFor returns the ordered tier chain rooted at rootType: the base
// def first, then each def whose UpgradesFrom points at the previous link.
// e.g. upgradeChainFor("townhall") → [townhall, keep, castle]. Returns nil if
// rootType is unknown. The catalog is authored as a straight chain; the scan is
// deterministic (sorted by type) and capped at the catalog size so a malformed
// cycle can't loop forever.
func upgradeChainFor(rootType string) []BuildingDef {
	root, ok := buildingDefsByType[rootType]
	if !ok {
		return nil
	}

	types := make([]string, 0, len(buildingDefsByType))
	for t := range buildingDefsByType {
		types = append(types, t)
	}
	sort.Strings(types)

	chain := []BuildingDef{root}
	for len(chain) <= len(buildingDefsByType) {
		current := chain[len(chain)-1]
		var next BuildingDef
		found := false
		for _, t := range types {
			if d := buildingDefsByType[t]; d.UpgradesFrom == current.Type {
				next, found = d, true
				break
			}
		}
		if !found {
			break
		}
		chain = append(chain, next)
	}
	return chain
}

func (d BuildingDef) IsBuildable() bool {
	return d.Buildable == nil || *d.Buildable
}

func ListBuildingDefs() []BuildingDef {
	defs := make([]BuildingDef, 0, len(buildingDefsByType))
	for _, def := range buildingDefsByType {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Type < defs[j].Type })
	return defs
}
