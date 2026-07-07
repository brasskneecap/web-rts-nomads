package game

import (
	"embed"
	"encoding/json"
	"io/fs"
	"sort"
	"strings"
)

//go:embed all:catalog/buildings
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
	// in state_upgrades.go's handleUpgradeBuildingTierLocked).
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
	// Shadow tunes the ground shadow drawn under the building. Client-only
	// render config; the server never reads it and only passes it through.
	// Omit the block to use the footprint-derived default shadow.
	Shadow *BuildingShadowDef `json:"shadow,omitempty"`
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

// BuildingShadowDef tunes the ground shadow drawn under a building. All
// distance fields are in *cell units*; omitted/zero fields fall back to a
// footprint-derived default (see resolveBuildingShadow on the client):
//
//	radiusX = footprintWidth * 0.42
//	radiusY = radiusX * 0.3
//	center  = horizontally centered, near the footprint's bottom edge
//
// Client-only render config; the server passes it through untouched.
type BuildingShadowDef struct {
	// Enabled defaults to true; set false to draw no shadow for this building.
	Enabled *bool   `json:"enabled,omitempty"`
	OffsetX float64 `json:"offsetX,omitempty"`
	OffsetY float64 `json:"offsetY,omitempty"`
	RadiusX float64 `json:"radiusX,omitempty"`
	RadiusY float64 `json:"radiusY,omitempty"`
	// Opacity is the peak alpha at the shadow center (0..1). Zero/omitted ⇒
	// the client default.
	Opacity float64 `json:"opacity,omitempty"`
}

// BuildingStyleRenderDef is a per-art render override for a building that
// selects its sprite per-instance rather than from a single sprite.png
// (currently only recipe-shop, via the shopStyle metadata set in the map
// editor). It carries only the client-render fields that differ between art
// variants — the gameplay def in <type>/<type>.json remains the single source
// of truth for footprint, capabilities, HP, etc. Authored as one JSON per
// sprite, colocated with the base def and keyed by the file stem (= style
// name = the sprite's file stem under client assets/buildings/recipe-shops/).
// The server never reads these fields; it passes them through to the client,
// which prefers them over the base def's render config when a style is set.
type BuildingStyleRenderDef struct {
	SpriteRender  *BuildingSpriteRenderDef  `json:"spriteRender,omitempty"`
	SelectionRing *BuildingSelectionRingDef `json:"selectionRing,omitempty"`
	Shadow        *BuildingShadowDef        `json:"shadow,omitempty"`
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

// buildingCatalog / buildingDefsByType MUST be var initializers (not init())
// so that Go's package-level dependency analysis can order them before any
// other var initializer that calls getBuildingDef — most importantly
// unitDefsByType's loader, which validates each unit's RequiresBuildings
// against the building catalog. All package-level var initializers run
// before any init() function, so converting this to init() would race
// with unitDefsByType (and break maps.go's placedUnits hydration via
// the `_ = unitDefsByType` dependency-injection marker). Go orders these by
// reference: unitDefsByType → getBuildingDef → buildingDefsByType →
// buildingCatalog.
var buildingCatalog = loadBuildingCatalog()
var buildingDefsByType = buildingCatalog.defs
var buildingStyleRenders = buildingCatalog.styles

type buildingCatalogData struct {
	defs map[string]BuildingDef
	// styles maps buildingType → styleName → per-style render override, for
	// buildings authored as a subdirectory (see loadBuildingCatalog).
	styles map[string]map[string]BuildingStyleRenderDef
}

// loadBuildingCatalog reads catalog/buildings. Two authoring layouts coexist:
//
//   - A flat <type>.json at the top level is a single BuildingDef (the common
//     case — one building type, one shared sprite).
//   - A subdirectory <type>/ groups a building that picks its sprite
//     per-instance: <type>/<type>.json is the gameplay BuildingDef, and every
//     other <type>/<style>.json is a BuildingStyleRenderDef keyed by its file
//     stem (the style name). This lets each sprite variant carry its own
//     selection ring / sprite bounds while sharing one gameplay def.
func loadBuildingCatalog() buildingCatalogData {
	const root = "catalog/buildings"
	entries, err := fs.ReadDir(buildingDefsFS, root)
	if err != nil {
		panic(root + ": " + err.Error())
	}
	defs := make(map[string]BuildingDef, len(entries))
	styles := make(map[string]map[string]BuildingStyleRenderDef)

	loadDef := func(path string) {
		data, err := buildingDefsFS.ReadFile(path)
		if err != nil {
			panic(path + ": " + err.Error())
		}
		var def BuildingDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic(path + ": " + err.Error())
		}
		defs[def.Type] = def
	}

	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() {
			if strings.HasSuffix(name, ".json") {
				loadDef(root + "/" + name)
			}
			continue
		}
		dir := root + "/" + name
		subEntries, err := fs.ReadDir(buildingDefsFS, dir)
		if err != nil {
			panic(dir + ": " + err.Error())
		}
		baseFile := name + ".json"
		for _, sub := range subEntries {
			if sub.IsDir() || !strings.HasSuffix(sub.Name(), ".json") {
				continue
			}
			path := dir + "/" + sub.Name()
			if sub.Name() == baseFile {
				loadDef(path)
				continue
			}
			data, err := buildingDefsFS.ReadFile(path)
			if err != nil {
				panic(path + ": " + err.Error())
			}
			var style BuildingStyleRenderDef
			if err := json.Unmarshal(data, &style); err != nil {
				panic(path + ": " + err.Error())
			}
			if styles[name] == nil {
				styles[name] = make(map[string]BuildingStyleRenderDef)
			}
			styles[name][strings.TrimSuffix(sub.Name(), ".json")] = style
		}
	}
	return buildingCatalogData{defs: defs, styles: styles}
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

// buildingRequirementTier resolves a unit RequiresBuildings entry to the root
// building type and the minimum tier a placed building of that root must reach
// to satisfy it. A tier link (temple, keep, castle) resolves to its chain root
// and 1-based position — "temple" → ("chapel", 2), "keep" → ("townhall", 2). A
// plain type (blacksmith, chapel) or an unknown type resolves to (itself, 1),
// preserving the exact pre-tier behaviour. This exists because a placed
// building keeps its ROOT BuildingType and records progress only in
// metadata["tier"]: a "temple" requirement is really "a chapel at tier ≥ 2".
func buildingRequirementTier(requiredType string) (rootType string, tier int) {
	def, ok := buildingDefsByType[requiredType]
	if !ok {
		return requiredType, 1
	}
	// Walk upgradesFrom back to the chain root (bounded against a malformed cycle).
	root := def
	for i := 0; root.UpgradesFrom != "" && i < len(buildingDefsByType); i++ {
		parent, ok := buildingDefsByType[root.UpgradesFrom]
		if !ok {
			break
		}
		root = parent
	}
	chain := upgradeChainFor(root.Type)
	for i, d := range chain {
		if d.Type == requiredType {
			return root.Type, i + 1
		}
	}
	return requiredType, 1
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

// ListBuildingStyleRenders returns the per-style render overrides keyed by
// buildingType → styleName. Consumed by the /catalog/buildings HTTP endpoint
// and passed through to the client renderer, which prefers a style's override
// over the base building def's render config when the instance has that style
// set. Buildings without a subdirectory layout have no entry.
func ListBuildingStyleRenders() map[string]map[string]BuildingStyleRenderDef {
	return buildingStyleRenders
}
