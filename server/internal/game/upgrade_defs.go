package game

import (
	"embed"
	"encoding/json"
	"io/fs"
	"sort"
)

//go:embed catalog/upgrades/*.json
var upgradeDefsFS embed.FS

// UpgradeDef is the static definition of a wave upgrade loaded from
// catalog/upgrades/<id>.json. Upgrades are offered between waves in the
// roguelike loop; the player chooses one per wave-end screen.
type UpgradeDef struct {
	ID          string        `json:"id"`
	Group       string        `json:"group"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Rarity      string        `json:"rarity"`
	Scope       string        `json:"scope"`              // "army"|"archetype"|"unitType"|"xp"|"equipment"
	Archetype   string        `json:"archetype,omitempty"`
	UnitType    string        `json:"unitType,omitempty"`
	Effect      UpgradeEffect `json:"effect"`
	MaxStacks   int           `json:"maxStacks"`
}

// UpgradeEffect describes what an UpgradeDef does when applied. Only one mode
// is active per def: stat multiplier (Stat+Multiplier), XP grant (Type="xp",
// Amount), or equipment drop (Type="equipment", ItemID).
type UpgradeEffect struct {
	Type       string  `json:"type,omitempty"`       // "xp"|"equipment"; absent = stat multiplier
	Stat       string  `json:"stat,omitempty"`       // "attackSpeed"|"damage"|"hp"|"moveSpeed"|"attackRange"
	Multiplier float64 `json:"multiplier,omitempty"`
	Amount     int     `json:"amount,omitempty"` // xp grant amount
	ItemID     string  `json:"itemID,omitempty"` // equipment drop item id
}

const (
	upgradeRarityCommon    = "common"
	upgradeRarityRare      = "rare"
	upgradeRarityEpic      = "epic"
	upgradeRarityLegendary = "legendary"
)

const (
	upgradeScopeArmy      = "army"
	upgradeScopeArchetype = "archetype"
	upgradeScopeUnitType  = "unitType"
	upgradeScopeXP        = "xp"
	upgradeScopeEquipment = "equipment"
)

// upgradeRarityOrder maps rarity names to a comparable integer so callers can
// determine which rarity is "higher" without string comparison.
var upgradeRarityOrder = map[string]int{
	upgradeRarityCommon:    0,
	upgradeRarityRare:      1,
	upgradeRarityEpic:      2,
	upgradeRarityLegendary: 3,
}

// upgradeDefsByID is the package-level catalog, loaded once at startup.
// Never mutated after initialization.
var upgradeDefsByID = loadUpgradeDefs()

func loadUpgradeDefs() map[string]UpgradeDef {
	entries, err := fs.ReadDir(upgradeDefsFS, "catalog/upgrades")
	if err != nil {
		panic("catalog/upgrades: " + err.Error())
	}
	result := make(map[string]UpgradeDef, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := upgradeDefsFS.ReadFile("catalog/upgrades/" + entry.Name())
		if err != nil {
			panic("catalog/upgrades/" + entry.Name() + ": " + err.Error())
		}
		var def UpgradeDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic("catalog/upgrades/" + entry.Name() + ": " + err.Error())
		}
		if def.ID == "" {
			panic("catalog/upgrades/" + entry.Name() + `: missing "id"`)
		}
		if def.Group == "" {
			panic("catalog/upgrades/" + entry.Name() + `: missing "group"`)
		}
		if _, valid := upgradeRarityOrder[def.Rarity]; !valid {
			panic("catalog/upgrades/" + entry.Name() + `: invalid rarity "` + def.Rarity + `"`)
		}
		switch def.Scope {
		case upgradeScopeArmy, upgradeScopeArchetype, upgradeScopeUnitType, upgradeScopeXP, upgradeScopeEquipment:
			// valid
		default:
			panic("catalog/upgrades/" + entry.Name() + `: invalid scope "` + def.Scope + `"`)
		}
		if def.Scope == upgradeScopeArchetype && def.Archetype == "" {
			panic("catalog/upgrades/" + entry.Name() + `: scope "archetype" requires non-empty archetype field`)
		}
		if def.Scope == upgradeScopeUnitType && def.UnitType == "" {
			panic("catalog/upgrades/" + entry.Name() + `: scope "unitType" requires non-empty unitType field`)
		}
		switch def.Effect.Type {
		case "":
			// stat-multiplier mode: Stat must be one of the known values
			switch def.Effect.Stat {
			case "attackSpeed", "damage", "hp", "moveSpeed", "attackRange":
				// valid
			default:
				panic("catalog/upgrades/" + entry.Name() + `: stat-mode effect has invalid stat "` + def.Effect.Stat + `"`)
			}
		case "xp":
			if def.Effect.Amount <= 0 {
				panic("catalog/upgrades/" + entry.Name() + `: effect type "xp" requires amount > 0`)
			}
		case "equipment":
			if def.Effect.ItemID == "" {
				panic("catalog/upgrades/" + entry.Name() + `: effect type "equipment" requires non-empty itemID`)
			}
		default:
			panic("catalog/upgrades/" + entry.Name() + `: unknown effect type "` + def.Effect.Type + `"`)
		}
		if def.MaxStacks <= 0 {
			def.MaxStacks = 3
		}
		if _, dup := result[def.ID]; dup {
			panic("catalog/upgrades/" + entry.Name() + `: duplicate id "` + def.ID + `"`)
		}
		result[def.ID] = def
	}
	return result
}

// getUpgradeDef returns the UpgradeDef for id and whether it was found.
func getUpgradeDef(id string) (UpgradeDef, bool) {
	def, ok := upgradeDefsByID[id]
	return def, ok
}

// listUpgradeDefs returns all registered upgrade definitions sorted by ID.
func listUpgradeDefs() []UpgradeDef {
	defs := make([]UpgradeDef, 0, len(upgradeDefsByID))
	for _, d := range upgradeDefsByID {
		defs = append(defs, d)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}
