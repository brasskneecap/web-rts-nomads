package game

import (
	"embed"
	"encoding/json"
	"io/fs"
	"sort"
	"strings"
)

//go:embed catalog/items
var itemDefsFS embed.FS

// ItemKind distinguishes equipment (persistent stat modifiers) from consumables
// (single-use / stacked effects applied on demand).
type ItemKind string

const (
	ItemKindEquipment  ItemKind = "equipment"
	ItemKindConsumable ItemKind = "consumable"
)

// ItemTier is the quality tier of an item, affecting cost and power.
type ItemTier string

const (
	ItemTierCommon    ItemTier = "common"
	ItemTierUncommon  ItemTier = "uncommon"
	ItemTierRare      ItemTier = "rare"
	ItemTierEpic      ItemTier = "epic"
	ItemTierLegendary ItemTier = "legendary"
)

// ItemSlotKind describes which equipment slot an item occupies. Consumables
// use ItemSlotKindAny because they are not restricted to a particular slot.
type ItemSlotKind string

const (
	ItemSlotKindWeapon    ItemSlotKind = "weapon"
	ItemSlotKindArmor     ItemSlotKind = "armor"
	ItemSlotKindAccessory ItemSlotKind = "accessory"
	ItemSlotKindAny       ItemSlotKind = "any" // consumables fit any slot
)

// ItemModifiers holds the flat stat bonuses granted by an equipment item.
// Zero values are omitted from JSON via omitempty.
type ItemModifiers struct {
	HP          int     `json:"hp,omitempty"`
	Damage      int     `json:"damage,omitempty"`
	Armor       int     `json:"armor,omitempty"`
	AttackSpeed float64 `json:"attackSpeed,omitempty"`
	MoveSpeed   float64 `json:"moveSpeed,omitempty"`
	HealthRegen float64 `json:"healthRegen,omitempty"`
	MaxShield   int     `json:"maxShield,omitempty"`
}

// ConsumableEffect describes the instant or timed effect applied when a
// consumable is used. Only "heal" is implemented in v1; future types (buffs,
// mana, etc.) add cases to applyConsumableEffectLocked.
type ConsumableEffect struct {
	Type            string  `json:"type"`                      // "heal" | future types
	Amount          int     `json:"amount,omitempty"`
	DurationSeconds float64 `json:"durationSeconds,omitempty"` // future: timed buffs
}

// ItemDef is the catalog definition for one item type, loaded from
// catalog/items/<id>.json. All game-logic reads go through this struct; client
// display fields (DisplayName, Description, IconKey) are passed through
// unchanged via the /catalog/items HTTP route.
type ItemDef struct {
	ID               string            `json:"id"`
	DisplayName      string            `json:"displayName"`
	Description      string            `json:"description,omitempty"`
	IconKey          string            `json:"iconKey"`
	Kind             ItemKind          `json:"kind"`
	Tier             ItemTier          `json:"tier"`
	Category         string            `json:"category,omitempty"`
	SlotKind         ItemSlotKind      `json:"slotKind"`
	AllowedUnitTypes []string          `json:"allowedUnitTypes,omitempty"`
	CostGold         int               `json:"costGold"`
	Modifiers        *ItemModifiers    `json:"modifiers,omitempty"`
	Effects          []string          `json:"effects,omitempty"`    // future: "lifesteal", "splash", etc.
	Consumable       *ConsumableEffect `json:"consumable,omitempty"`
	MaxStacks        int               `json:"maxStacks,omitempty"`  // consumables only; 0 treated as 1
}

// itemCatalogSingleton is the package-level item catalog loaded at init time.
// GameState.itemCatalog points at this map; it is never mutated after init.
var itemCatalogSingleton map[string]*ItemDef

func init() {
	itemCatalogSingleton = make(map[string]*ItemDef)
	err := fs.WalkDir(itemDefsFS, "catalog/items", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		data, err := itemDefsFS.ReadFile(path)
		if err != nil {
			panic(path + ": " + err.Error())
		}
		var def ItemDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic(path + ": " + err.Error())
		}
		if def.ID == "" {
			panic(path + `: missing "id" field`)
		}
		itemCatalogSingleton[def.ID] = &def
		return nil
	})
	if err != nil {
		panic("catalog/items: " + err.Error())
	}
}

// ListItemDefs returns all item definitions as a deterministically sorted
// slice (alphabetical by ID). Used by the /catalog/items HTTP route.
func ListItemDefs() []*ItemDef {
	defs := make([]*ItemDef, 0, len(itemCatalogSingleton))
	for _, def := range itemCatalogSingleton {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}
