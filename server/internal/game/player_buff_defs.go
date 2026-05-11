package game

import (
	"embed"
	"encoding/json"
	"io/fs"
	"sort"
	"strings"
)

//go:embed catalog/player-buffs
var playerBuffDefsFS embed.FS

// PlayerBuffDef is the static definition of a player-level buff loaded from
// catalog/player-buffs/<id>.json. Player buffs apply to all (or a filtered
// subset of) that player's units for an entire match.
type PlayerBuffDef struct {
	ID               string              `json:"id"`
	DisplayName      string              `json:"displayName"`
	Description      string              `json:"description,omitempty"`
	IconKey          string              `json:"iconKey"`
	UnlockCost       int                 `json:"unlockLegendPointCost"`
	// AppliesTo controls which side receives the buff modifiers.
	// "ownedUnits" (default when empty) boosts the player's own units.
	// "enemyUnits" boosts enemy units attacking the player — a deliberate debuff.
	AppliesTo        string              `json:"appliesTo,omitempty"`
	Modifiers        PlayerBuffModifiers `json:"modifiers"`
	AllowedUnitTypes []string            `json:"allowedUnitTypes,omitempty"` // empty = all unit types
}

// PlayerBuffModifiers holds all stat adjustments a player buff can contribute.
// Spawn-time fields are baked into base stats when the unit is created;
// live multiplier fields are summed into the perk hook returns each tick.
type PlayerBuffModifiers struct {
	// Spawn-time flat bonuses applied to Base* stats before applyRankModifiersLocked.
	HPBonus     int `json:"hpBonus,omitempty"`
	DamageBonus int `json:"damageBonus,omitempty"`
	ArmorBonus  int `json:"armorBonus,omitempty"`
	// Per-tick multiplier contributions added into existing perk hook return values.
	AttackSpeedBonus   float64 `json:"attackSpeedBonus,omitempty"`
	MoveSpeedMultBonus float64 `json:"moveSpeedMultBonus,omitempty"`
	BonusDamageMult    float64 `json:"bonusDamageMult,omitempty"`
}

// playerBuffCatalog is the package-level catalog loaded at init time.
// Never mutated after init.
var playerBuffCatalog map[string]*PlayerBuffDef

func init() {
	playerBuffCatalog = make(map[string]*PlayerBuffDef)
	err := fs.WalkDir(playerBuffDefsFS, "catalog/player-buffs", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		data, err := playerBuffDefsFS.ReadFile(path)
		if err != nil {
			panic(path + ": " + err.Error())
		}
		var def PlayerBuffDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic(path + ": " + err.Error())
		}
		if def.ID == "" {
			panic(path + `: missing "id" field`)
		}
		if def.UnlockCost < 0 {
			panic(path + `: unlockLegendPointCost must be >= 0`)
		}
		playerBuffCatalog[def.ID] = &def
		return nil
	})
	if err != nil {
		panic("catalog/player-buffs: " + err.Error())
	}
}

// playerBuffDefByID looks up a PlayerBuffDef by ID. Returns nil if not found.
func playerBuffDefByID(id string) *PlayerBuffDef {
	return playerBuffCatalog[id]
}

// PlayerBuffDefByID is the exported version of playerBuffDefByID for use
// outside the game package (e.g. HTTP handlers).
func PlayerBuffDefByID(id string) *PlayerBuffDef {
	return playerBuffCatalog[id]
}

// ListPlayerBuffDefs returns all player buff definitions sorted by ID.
// Used by the /api/catalog/player-buffs HTTP endpoint.
func ListPlayerBuffDefs() []*PlayerBuffDef {
	defs := make([]*PlayerBuffDef, 0, len(playerBuffCatalog))
	for _, def := range playerBuffCatalog {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}
