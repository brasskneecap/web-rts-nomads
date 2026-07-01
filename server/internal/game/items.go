package game

import (
	"embed"
	"encoding/json"
	"fmt"
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

// ItemElementalDamage is a flat typed damage amount an equipment item adds as
// its OWN damage instance on each landed basic attack — separate from physical
// modifiers.Damage so future resistance/weakness logic can treat it
// independently. Type must be a registered DamageType.
type ItemElementalDamage struct {
	Type   DamageType `json:"type"`
	Amount int        `json:"amount"`
}

// ItemOnHitProc is a percent-chance on-hit effect: on each landed basic attack
// the wielder rolls Chance against the seeded perk RNG and, on success, fires a
// homing projectile (ProjectileID) dealing Damage of DamageType to the current
// target. Damage is applied as its own instance and does NOT re-trigger on-hit
// effects (no recursion).
type ItemOnHitProc struct {
	Chance       float64    `json:"chance"`
	Damage       int        `json:"damage"`
	DamageType   DamageType `json:"damageType"`
	ProjectileID string     `json:"projectileID"`
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
	// RequiredBuilding was historically the building type that gated an
	// item's purchase. As of per-building-shop-inventories it is preserved
	// for backward display only and no longer participates in purchase
	// validation — the authoritative inventory is BuildingTile.ShopInventory,
	// populated per-building from shopFixedInventory / shopLootTableId, or
	// from the small defaultMarketplaceStarterInventory fallback.
	RequiredBuilding string            `json:"requiredBuilding,omitempty"`
	Modifiers        *ItemModifiers    `json:"modifiers,omitempty"`
	Effects          []string          `json:"effects,omitempty"`    // future: "lifesteal", "splash", etc.
	Consumable       *ConsumableEffect `json:"consumable,omitempty"`
	MaxStacks        int               `json:"maxStacks,omitempty"`  // consumables only; 0 treated as 1
	OnHitElemental   []ItemElementalDamage `json:"onHitElemental,omitempty"`
	OnHitProc        *ItemOnHitProc        `json:"onHitProc,omitempty"`
}

// itemCatalogSingleton is the package-level item catalog. Populated by a var
// initializer (not init()) so that other var initializers — specifically the
// loot-table loader in loot_table_defs.go — can reference it via getItemDef
// and have Go's dependency-graph-based var ordering guarantee it is ready
// before they run.
var itemCatalogSingleton = loadItemCatalog()

func loadItemCatalog() map[string]*ItemDef {
	catalog := make(map[string]*ItemDef)
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
		if err := validateItemDef(&def); err != nil {
			panic(path + ": " + err.Error())
		}
		catalog[def.ID] = &def
		return nil
	})
	if err != nil {
		panic("catalog/items: " + err.Error())
	}
	return catalog
}

// getItemDef returns the item definition for the given id, or (nil, false)
// when the id is not in the catalog. Safe to call after package init.
func getItemDef(id string) (*ItemDef, bool) {
	def, ok := itemCatalogSingleton[id]
	return def, ok
}

// validateItemDef checks the on-hit fields of an item def. Empty DamageType is
// rejected here (unlike combat code that resolves it to physical) because a
// typed elemental bonus with no explicit element is a content authoring error.
func validateItemDef(def *ItemDef) error {
	for i, e := range def.OnHitElemental {
		if !IsValidDamageType(e.Type) {
			return fmt.Errorf("item %q onHitElemental[%d]: unregistered damage type %q", def.ID, i, e.Type)
		}
	}
	if p := def.OnHitProc; p != nil {
		if p.Chance < 0 || p.Chance > 1 {
			return fmt.Errorf("item %q onHitProc.chance %v out of range [0,1]", def.ID, p.Chance)
		}
		if !IsValidDamageType(p.DamageType) {
			return fmt.Errorf("item %q onHitProc.damageType: unregistered damage type %q", def.ID, p.DamageType)
		}
	}
	return nil
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
