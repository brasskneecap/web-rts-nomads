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
	// DodgeChance / BlockChance are additive probability contributions to the
	// wearer's evasion stats (0.15 = +15%). Validated to [0,1) at load; the
	// combined dodge+block total is capped at evasionCapTotal at roll time,
	// not here, so stacked items display honestly.
	DodgeChance float64 `json:"dodgeChance,omitempty"`
	BlockChance float64 `json:"blockChance,omitempty"`
}

// ItemElementalDamage is a flat typed damage amount an equipment item adds as
// its OWN damage instance on each landed basic attack — separate from physical
// modifiers.Damage so future resistance/weakness logic can treat it
// independently. Type must be a registered DamageType.
type ItemElementalDamage struct {
	Type   DamageType `json:"type"`
	Amount int        `json:"amount"`
}

// ItemProcTrigger names the combat event that rolls a proc.
type ItemProcTrigger string

const (
	// ProcOnHit rolls on each basic attack the wielder LANDS, firing the effect
	// at the target.
	ProcOnHit ItemProcTrigger = "onHit"
	// ProcOnStruck is the on-BEING-hit mirror: when a basic attack LANDS on the
	// wearer (post-evasion — dodged/blocked hits never trigger it), the wearer
	// rolls and, on success, fires the effect back at the attacker.
	ProcOnStruck ItemProcTrigger = "onStruck"
)

// IsValidProcTrigger reports whether t is a trigger the combat code handles.
func IsValidProcTrigger(t ItemProcTrigger) bool {
	return t == ProcOnHit || t == ProcOnStruck
}

// ItemProc is one percent-chance proc on an item: on each Trigger event the
// wielder rolls Chance against the seeded perk RNG and, on success, CASTS the
// referenced composable ability (Ability) at what it hit, via
// castAbilityAsProcLocked — free of mana/cooldown, so the ability is the single
// source of truth for what the proc does (a frost/lightning weapon proc is
// literally "cast Frost Bolt / Chain Lightning"). The bespoke ProcEffectDef
// path this used to support was removed once every catalog item moved to
// abilities. An item may carry any number of procs, including several on the
// same trigger; each rolls independently (see ItemDef.Procs).
type ItemProc struct {
	Trigger ItemProcTrigger `json:"trigger"`
	Chance  float64         `json:"chance"`
	Ability string          `json:"ability"`
}

// defaultConsumableRangeUnits is the AoE radius (world units) a consumable
// covers when its def doesn't author an explicit "range".
const defaultConsumableRangeUnits = 100.0

// ConsumableEffect describes the effect applied when a consumable is used.
// Consumables are used as a ground-targeted AoE: every friendly unit within
// Range of the click point is affected. Implemented types: "heal" (restore
// Amount HP, capped at MaxHP) and "grant_xp" (award Amount XP through the
// normal rank-up pipeline). Future types (buffs, mana, etc.) add cases to
// applyConsumableToUnitLocked.
type ConsumableEffect struct {
	Type   string `json:"type"` // "heal" | "grant_xp" | future types
	Amount int    `json:"amount,omitempty"`
	// Range is the AoE radius in world units around the click point. 0/absent
	// falls back to defaultConsumableRangeUnits.
	Range float64 `json:"range,omitempty"`
	// Split controls how Amount is distributed across the units hit: true
	// (the default when absent) divides Amount evenly between them; false
	// gives the full Amount to every unit hit.
	Split           *bool   `json:"split,omitempty"`
	DurationSeconds float64 `json:"durationSeconds,omitempty"` // future: timed buffs
}

// EffectiveRange returns the authored AoE radius, falling back to the default.
func (c *ConsumableEffect) EffectiveRange() float64 {
	if c.Range > 0 {
		return c.Range
	}
	return defaultConsumableRangeUnits
}

// SplitEnabled reports whether Amount is divided across targets (default true).
func (c *ConsumableEffect) SplitEnabled() bool {
	return c.Split == nil || *c.Split
}

// ItemCrafting is an item's recipe: what it consumes, and the two prices that
// gate it. Its presence on an ItemDef IS that item's craftability — there is no
// separate recipe entity, and an item is its own recipe.
//
// The two gold fields buy different things and are tuned independently:
//
//   - CraftCostGold  — charged at a crafting building on EVERY craft, on top of
//     consuming Inputs. See handleCraftItemLocked.
//   - RecipeCostGold — charged ONCE at a Recipe Shop to learn the recipe. See
//     handlePurchaseRecipeLocked.
//
// The third price in the item economy — buying the finished item off a shop
// shelf — is ItemDef.CostGold, not here.
type ItemCrafting struct {
	// Inputs are the item IDs consumed by one craft (2+, duplicates allowed).
	Inputs []string `json:"inputs"`
	// CraftCostGold is charged per craft at a crafting building.
	CraftCostGold int `json:"craftCostGold"`
	// RecipeCostGold is charged once at a Recipe Shop to learn this recipe. Zero
	// means free to learn (still gated on shop stock). Moot when Starter is set.
	RecipeCostGold int `json:"recipeCostGold"`
	// Starter, when true, marks this recipe as one every player has already
	// learned at match start — no Recipe Shop purchase required.
	Starter bool `json:"starter,omitempty"`
}

// ItemDef is the catalog definition for one item type, loaded from
// catalog/items/<id>.json. All game-logic reads go through this struct; client
// display fields (DisplayName, Description, IconKey) are passed through
// unchanged via the /catalog/items HTTP route.
type ItemDef struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName"`
	Description string   `json:"description,omitempty"`
	IconKey     string   `json:"iconKey"`
	Kind        ItemKind `json:"kind"`
	Tier        ItemTier `json:"tier"`
	// Category is organizational only — it groups items in the editor and
	// decides which catalog subdirectory the def is written to. It is NOT an
	// equip restriction; nothing in combat or equipping reads it.
	Category string `json:"category,omitempty"`
	// CostGold is what a shop charges to buy this item outright, finished. The
	// two crafting prices live on Crafting, which is also what makes the item
	// craftable at all.
	CostGold int `json:"costGold"`
	// Crafting is this item's recipe, or nil when the item cannot be crafted.
	// Its presence is the single craftability predicate — see IsCraftable.
	Crafting *ItemCrafting `json:"crafting,omitempty"`
	// RequiredBuilding was historically the building type that gated an
	// item's purchase. As of per-building-shop-inventories it is preserved
	// for backward display only and no longer participates in purchase
	// validation — the authoritative inventory is BuildingTile.ShopInventory,
	// populated per-building from shopFixedInventory / shopLootTableId, or
	// from the small defaultMarketplaceStarterInventory fallback.
	RequiredBuilding string                `json:"requiredBuilding,omitempty"`
	Modifiers        *ItemModifiers        `json:"modifiers,omitempty"`
	// AbilityParams are this item's contributions to target abilities' declared
	// PARAMETERS (AbilityDef.Params) — identical shape to PerkDef.AbilityParams,
	// because ability parameters are deliberately source-agnostic: an item tunes
	// an ability's numbers exactly the way a perk does. Target may be an ability
	// id or "tag:<name>". See ability_params.go.
	// AbilityStats are this item's BROAD, kind-targeted ability contributions —
	// "+15% radius to your abilities". This is the addressing mode an item NEEDS:
	// unlike a perk, an item cannot name an ability, because it does not know who
	// equipped it or what they cast. Same id vocabulary as UnitDef.AbilityStats.
	AbilityStats map[string]AbilityStatMod `json:"abilityStats,omitempty"`
	// AbilityFields are this item's PRECISE, per-action contributions, for an item
	// that names a specific ability ("+30% Fire Pit radius") rather than buffing
	// every ability broadly. See ability_field_mods.go.
	AbilityFields []AbilityFieldModifier `json:"abilityFields,omitempty"`
	Effects          []string              `json:"effects,omitempty"` // future: "lifesteal", "splash", etc.
	Consumable       *ConsumableEffect     `json:"consumable,omitempty"`
	MaxStacks        int                   `json:"maxStacks,omitempty"` // consumables only; 0 treated as 1
	OnHitElemental   []ItemElementalDamage `json:"onHitElemental,omitempty"`
	// Procs are the item's proc triggers, each naming its own combat event
	// (Trigger), effect, chance and overrides. An item may define any number of
	// them, including several on the same trigger — every proc rolls
	// independently, so two onHit procs on one weapon can both fire on the same
	// attack. Legacy defs that authored a single "onHitProc"/"onStruckProc"
	// object are folded into this list at load (see UnmarshalJSON).
	Procs []ItemProc `json:"procs,omitempty"`

	// Overridden marks a def sourced from the writable editor overlay rather
	// than the embedded catalog. Runtime-only provenance for the editor UI —
	// never authored in embed files (the disk writer always strips it).
	Overridden bool `json:"overridden,omitempty"`
	// Custom marks a def the author CREATED (it does not ship in the embedded
	// catalog), as opposed to an edited copy of a shipped def. The editor uses
	// it to decide whether deleting removes the item or resets it to its
	// shipped default. Stamped by ListItemDefs on the way out; never authored,
	// never persisted (the disk writer strips it with Overridden).
	Custom bool `json:"custom,omitempty"`
	// IconUploadedAt is the mtime (unix seconds) of the author's uploaded icon,
	// or 0 when there is none. Non-zero means the client must serve the icon
	// from /catalog/items/{id}/image rather than its bundled art — without this
	// a shipped item's icon could never be replaced, because the bundled asset
	// always wins on key. Doubles as a cache-buster. Stamped by ListItemDefs;
	// never authored, never persisted.
	IconUploadedAt int64 `json:"iconUploadedAt,omitempty"`
}

// itemDefFields is ItemDef without its methods, so UnmarshalJSON can decode
// into it without recursing into itself.
type itemDefFields ItemDef

// UnmarshalJSON accepts the pre-list proc schema — a single "onHitProc" and/or
// "onStruckProc" object, whose KEY carried the trigger — and folds it into
// Procs. Kept so item JSON authored before the list schema (shipped catalog
// files, anything a user saved from the old editor) still loads; nothing
// writes those keys any more. Marshaling is unaffected: ItemDef emits only
// "procs".
func (d *ItemDef) UnmarshalJSON(data []byte) error {
	var raw struct {
		itemDefFields
		OnHitProc    *ItemProc `json:"onHitProc,omitempty"`
		OnStruckProc *ItemProc `json:"onStruckProc,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*d = ItemDef(raw.itemDefFields)
	if p := raw.OnHitProc; p != nil {
		p.Trigger = ProcOnHit
		d.Procs = append(d.Procs, *p)
	}
	if p := raw.OnStruckProc; p != nil {
		p.Trigger = ProcOnStruck
		d.Procs = append(d.Procs, *p)
	}
	return nil
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
		if d.IsDir() {
			if d.Name() == itemIconsSubdirName {
				return fs.SkipDir // _icons holds uploaded PNGs, not defs
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".json") {
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
	// Second pass: crafting inputs point at OTHER items, so they can only be
	// resolved once every def has been read. Deterministic order (sorted ids) so
	// a broken catalog always names the same offender first.
	ids := make([]string, 0, len(catalog))
	for id := range catalog {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	inCatalog := func(id string) bool { _, ok := catalog[id]; return ok }
	for _, id := range ids {
		if err := validateItemCraftingRefs(catalog[id], inCatalog); err != nil {
			panic("catalog/items: " + err.Error())
		}
	}
	return catalog
}

// getItemDef returns the item definition for the given id, or (nil, false)
// when the id is not in the catalog. Safe to call after package init. The
// writable editor overlay (runtimeItems) wins over the embedded catalog.
func getItemDef(id string) (*ItemDef, bool) {
	runtimeItemsMu.RLock()
	if def, ok := runtimeItems[id]; ok {
		runtimeItemsMu.RUnlock()
		return def, true
	}
	runtimeItemsMu.RUnlock()
	def, ok := itemCatalogSingleton[id]
	return def, ok
}

// validateItemDef checks the on-hit fields of an item def. Empty DamageType is
// rejected here (unlike combat code that resolves it to physical) because a
// typed elemental bonus with no explicit element is a content authoring error.
func validateItemDef(def *ItemDef) error {
	if err := validateAbilityFieldModifiers(fmt.Sprintf("item %q", def.ID), def.AbilityFields); err != nil {
		return err
	}
	if err := validateAbilityStats(fmt.Sprintf("item %q", def.ID), def.AbilityStats); err != nil {
		return err
	}
	if m := def.Modifiers; m != nil {
		if m.DodgeChance < 0 || m.DodgeChance >= 1 {
			return fmt.Errorf("item %q modifiers.dodgeChance %v out of range [0,1)", def.ID, m.DodgeChance)
		}
		if m.BlockChance < 0 || m.BlockChance >= 1 {
			return fmt.Errorf("item %q modifiers.blockChance %v out of range [0,1)", def.ID, m.BlockChance)
		}
	}
	for i, e := range def.OnHitElemental {
		if !IsValidDamageType(e.Type) {
			return fmt.Errorf("item %q onHitElemental[%d]: unregistered damage type %q", def.ID, i, e.Type)
		}
	}
	for i := range def.Procs {
		if err := validateItemProc(def.ID, i, &def.Procs[i]); err != nil {
			return err
		}
	}
	// A consumable's effect must name a type; an empty type is a silent no-op
	// in applyConsumableToUnitLocked. Unknown types are left to that switch —
	// validating them here would couple this loader to the effect list.
	if def.Consumable != nil && def.Consumable.Type == "" {
		return fmt.Errorf("item %q consumable.type is required", def.ID)
	}
	if err := validateItemCrafting(def); err != nil {
		return err
	}
	return nil
}

// validateItemCrafting checks the SELF-CONTAINED crafting rules — the ones that
// need nothing but this def. Whether each input names a real item is checked
// separately (validateItemCraftingRefs), because at catalog-load time the item
// map is still being filled and an input may not have been read yet.
func validateItemCrafting(def *ItemDef) error {
	c := def.Crafting
	if c == nil {
		return nil
	}
	// Two inputs is what makes a craft a combination rather than a purchase.
	if len(c.Inputs) < 2 {
		return fmt.Errorf("item %q: a craftable item needs at least 2 inputs, has %d", def.ID, len(c.Inputs))
	}
	for i, in := range c.Inputs {
		if in == def.ID {
			return fmt.Errorf("item %q: cannot be its own crafting input (inputs[%d])", def.ID, i)
		}
	}
	// Negative gold would PAY the player to craft or to learn (an exploit). Zero
	// is legal: a recipe whose only cost is its ingredients, or a free-to-learn one.
	if c.CraftCostGold < 0 {
		return fmt.Errorf("item %q: crafting.craftCostGold must not be negative, got %d", def.ID, c.CraftCostGold)
	}
	if c.RecipeCostGold < 0 {
		return fmt.Errorf("item %q: crafting.recipeCostGold must not be negative, got %d", def.ID, c.RecipeCostGold)
	}
	return nil
}

// validateItemCraftingRefs checks that every crafting input names a real item.
// Runs as a second pass once the whole catalog is loaded — see validateItemCrafting.
//
// `known` is passed in rather than resolved through getItemDef because the
// embed loader calls this while it is still BUILDING the catalog singleton, so
// getItemDef would see a nil map. Overlay writers pass the live lookup.
func validateItemCraftingRefs(def *ItemDef, known func(string) bool) error {
	if def.Crafting == nil {
		return nil
	}
	for i, in := range def.Crafting.Inputs {
		if !known(in) {
			return fmt.Errorf("item %q: crafting.inputs[%d] %q is not a known item", def.ID, i, in)
		}
	}
	return nil
}

// itemKnownInCatalog is the `known` lookup for a fully-loaded catalog.
func itemKnownInCatalog(id string) bool {
	_, ok := getItemDef(id)
	return ok
}

// IsCraftable reports whether this item can be produced at a crafting building.
// The presence of the crafting block is the single source of truth — there is no
// separate recipe entity to consult.
func (d *ItemDef) IsCraftable() bool { return d != nil && d.Crafting != nil }

// validateItemProc checks one entry of ItemDef.Procs — its trigger, its
// reference into the proc catalog, and the override sanity rules. Index i is
// carried into the error so the message points at the offending entry.
func validateItemProc(itemID string, i int, p *ItemProc) error {
	field := fmt.Sprintf("procs[%d]", i)
	if !IsValidProcTrigger(p.Trigger) {
		return fmt.Errorf("item %q %s.trigger %q must be %q or %q", itemID, field, p.Trigger, ProcOnHit, ProcOnStruck)
	}
	if p.Chance < 0 || p.Chance > 1 {
		return fmt.Errorf("item %q %s.chance %v out of range [0,1]", itemID, field, p.Chance)
	}
	// A proc casts a composable ability at what it hit; the ability id is
	// required and must resolve.
	if p.Ability == "" {
		return fmt.Errorf("item %q %s.ability is required (an ability id to cast)", itemID, field)
	}
	if _, ok := getAbilityDef(p.Ability); !ok {
		return fmt.Errorf("item %q %s.ability %q is not a registered ability", itemID, field, p.Ability)
	}
	return nil
}

// ListItemDefs returns all item definitions as a deterministically sorted
// slice (alphabetical by ID), merging the writable editor overlay
// (runtimeItems) on top of the embedded catalog. Used by the /catalog/items
// HTTP route.
//
// Each returned def carries Custom: true when the item does NOT ship in the
// embedded catalog, i.e. the author created it. This is the signal the editor
// needs to know whether deleting means "remove it" or "reset it to the shipped
// default" (see DeleteEditorItem, which branches on the same ItemIsEmbedded
// check). Overridden cannot answer that — in a dev build the writable dir IS
// the embed source, so every item loads through the overlay and reports
// Overridden: true.
func ListItemDefs() []*ItemDef {
	merged := make(map[string]*ItemDef, len(itemCatalogSingleton))
	for id, def := range itemCatalogSingleton {
		merged[id] = def
	}
	runtimeItemsMu.RLock()
	for id, def := range runtimeItems {
		merged[id] = def
	}
	runtimeItemsMu.RUnlock()
	defs := make([]*ItemDef, 0, len(merged))
	for _, def := range merged {
		// Copy before stamping: the map values are the live catalog / overlay
		// defs and must not be mutated by a read.
		out := *def
		out.Custom = !ItemIsEmbedded(out.ID)
		out.IconUploadedAt = ItemIconUploadedAt(out.ID)
		defs = append(defs, &out)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}

