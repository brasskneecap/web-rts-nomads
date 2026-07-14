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
// wielder rolls Chance against the seeded perk RNG and, on success, fires the
// referenced proc effect (catalog/procs). Effect names the ProcEffectDef
// (required); the embedded ProcEffectOverrides let this item re-tune the
// effect's numbers (damage, scale, bounce, slow, burn) without authoring a new
// def — the effect's element and emitter are fixed by the def. Damage is
// applied as its own instance and does NOT re-trigger on-hit effects (no
// recursion).
//
// An item may carry any number of procs, including several on the same
// trigger; each rolls independently (see ItemDef.Procs).
type ItemProc struct {
	Trigger ItemProcTrigger `json:"trigger"`
	Chance  float64         `json:"chance"`
	Effect  string          `json:"effect"`
	ProcEffectOverrides
}

// ResolveParams returns the proc's effective payload: the referenced effect
// def with this item's non-zero overrides applied. ok is false when Effect
// names no registered proc effect — validateItemDef rejects that at load, so
// a false here can only come from a hand-built def in tests.
func (p *ItemProc) ResolveParams() (ProcEffectParams, bool) {
	def, ok := getProcEffectDef(p.Effect)
	if !ok {
		return ProcEffectParams{}, false
	}
	return resolveProcEffectParams(def, p.ProcEffectOverrides), true
}

// itemProcWire is the JSON shape emitted for ItemProc. See MarshalJSON for why
// this exists.
type itemProcWire struct {
	Trigger ItemProcTrigger `json:"trigger"`
	Chance  float64         `json:"chance"`
	Effect  string          `json:"effect"`

	Damage              int        `json:"damage,omitempty"`
	DamageType          DamageType `json:"damageType,omitempty"`
	ProjectileID        string     `json:"projectileID,omitempty"`
	ProjectileScale     float64    `json:"projectileScale,omitempty"`
	BounceCount         int        `json:"bounceCount,omitempty"`
	BounceRange         float64    `json:"bounceRange,omitempty"`
	BounceDamageFalloff int        `json:"bounceDamageFalloff,omitempty"`
	SlowMultiplier      float64    `json:"slowMultiplier,omitempty"`
	SlowDurationSeconds float64    `json:"slowDurationSeconds,omitempty"`
	BurnDamagePerSecond float64    `json:"burnDamagePerSecond,omitempty"`
	BurnDurationSeconds float64    `json:"burnDurationSeconds,omitempty"`
}

// MarshalJSON exists for ONE reason: the /catalog/items route serves ItemDef
// to the SPA, and the client tooltip contract predates the effect-reference
// schema — it reads resolved payload fields (damage, damageType,
// projectileID) directly off the proc. Marshal therefore emits the RESOLVED
// params (def + overrides, via ResolveParams) alongside the effect
// reference, so the client stays a dumb view with no proc-catalog knowledge
// of its own. There is deliberately no UnmarshalJSON on ItemProc: catalog
// files keep unmarshaling into Effect + the embedded ProcEffectOverrides
// untouched.
func (p ItemProc) MarshalJSON() ([]byte, error) {
	wire := itemProcWire{Trigger: p.Trigger, Chance: p.Chance, Effect: p.Effect}
	if params, ok := p.ResolveParams(); ok {
		wire.Damage = params.Damage
		wire.DamageType = params.DamageType
		wire.ProjectileID = params.ProjectileID
		wire.ProjectileScale = params.ProjectileScale
		wire.BounceCount = params.BounceCount
		wire.BounceRange = params.BounceRange
		wire.BounceDamageFalloff = params.BounceDamageFalloff
		wire.SlowMultiplier = params.SlowMultiplier
		wire.SlowDurationSeconds = params.SlowDurationSeconds
		wire.BurnDamagePerSecond = params.BurnDamagePerSecond
		wire.BurnDurationSeconds = params.BurnDurationSeconds
	}
	return json.Marshal(wire)
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
	CostGold int    `json:"costGold"`
	// IsRecipe marks an item as craftable at the Artificer: a recipe (whose
	// output is this item) exists to unlock it. Availability elsewhere (which
	// shops stock it, loot tables) is decided at the shop/loot level, not
	// here — the item only declares its own purchase cost (CostGold) and, when
	// craftable, its craft cost (RecipeCost). The item editor keeps the paired
	// recipe def in sync with this flag.
	IsRecipe   bool `json:"isRecipe,omitempty"`
	RecipeCost int  `json:"recipeCost,omitempty"`
	// RecipeStarter, when the item is craftable, marks its recipe as one every
	// player has already learned at match start (no shop/unlock needed). Synced
	// to RecipeDef.Starter by the item editor.
	RecipeStarter bool `json:"recipeStarter,omitempty"`
	// RequiredBuilding was historically the building type that gated an
	// item's purchase. As of per-building-shop-inventories it is preserved
	// for backward display only and no longer participates in purchase
	// validation — the authoritative inventory is BuildingTile.ShopInventory,
	// populated per-building from shopFixedInventory / shopLootTableId, or
	// from the small defaultMarketplaceStarterInventory fallback.
	RequiredBuilding string                `json:"requiredBuilding,omitempty"`
	Modifiers        *ItemModifiers        `json:"modifiers,omitempty"`
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

// itemListsSubdir is the catalog/items subdirectory that holds named item
// lists (a different schema — see ItemListDef). It is skipped by the item
// def walk so list files are never parsed as item defs. Mirrors
// recipeListsSubdir.
const itemListsSubdir = "catalog/items/lists"

func loadItemCatalog() map[string]*ItemDef {
	catalog := make(map[string]*ItemDef)
	err := fs.WalkDir(itemDefsFS, "catalog/items", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == itemListsSubdir || d.Name() == itemIconsSubdirName {
				return fs.SkipDir // lists load separately; _icons holds uploaded PNGs
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
	return nil
}

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
	if p.Effect == "" {
		return fmt.Errorf("item %q %s.effect is required (a catalog/procs id)", itemID, field)
	}
	if _, ok := getProcEffectDef(p.Effect); !ok {
		return fmt.Errorf("item %q %s.effect %q is not a registered proc effect", itemID, field, p.Effect)
	}
	if p.Damage < 0 {
		return fmt.Errorf("item %q %s.damage override %v must be >= 0", itemID, field, p.Damage)
	}
	if p.ProjectileScale < 0 {
		return fmt.Errorf("item %q %s.projectileScale override %v must be >= 0", itemID, field, p.ProjectileScale)
	}
	return nil
}

// ListItemDefs returns all item definitions as a deterministically sorted
// slice (alphabetical by ID), merging the writable editor overlay
// (runtimeItems) on top of the embedded catalog. Used by the /catalog/items
// HTTP route.
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
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}

// ItemListDef is a named, curated set of item IDs, authored under
// catalog/items/lists/<id>.json. Mirrors RecipeListDef: shops resolve a list
// by ID to stock its items instead of hardcoding item IDs in Go (the
// player-built marketplace stocks the "marketplace" list).
type ItemListDef struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Items []string `json:"items"`
}

var itemListCatalogSingleton = loadItemListCatalog()

func loadItemListCatalog() map[string]*ItemListDef {
	catalog := make(map[string]*ItemListDef)
	entries, err := fs.ReadDir(itemDefsFS, itemListsSubdir)
	if err != nil {
		// No lists/ directory is valid — item lists are optional.
		return catalog
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := itemListsSubdir + "/" + e.Name()
		data, err := itemDefsFS.ReadFile(path)
		if err != nil {
			panic(path + ": " + err.Error())
		}
		var def ItemListDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic(path + ": " + err.Error())
		}
		if def.ID == "" {
			panic(path + `: missing "id" field`)
		}
		if err := validateItemListDef(&def); err != nil {
			panic(path + ": " + err.Error())
		}
		catalog[def.ID] = &def
	}
	return catalog
}

// validateItemListDef enforces: at least one item, and every referenced item
// ID resolves to a real item def. Called at catalog load (fail-fast).
func validateItemListDef(def *ItemListDef) error {
	if len(def.Items) == 0 {
		return fmt.Errorf("item list %q: needs at least 1 item", def.ID)
	}
	for i, id := range def.Items {
		if _, ok := getItemDef(id); !ok {
			return fmt.Errorf("item list %q: items[%d] %q is not a known item", def.ID, i, id)
		}
	}
	return nil
}

func getItemListDef(id string) (*ItemListDef, bool) {
	runtimeItemListsMu.RLock()
	if def, ok := runtimeItemLists[id]; ok {
		runtimeItemListsMu.RUnlock()
		return def, true
	}
	runtimeItemListsMu.RUnlock()
	def, ok := itemListCatalogSingleton[id]
	return def, ok
}

// ListItemListDefs returns all item-list defs sorted by ID (for the HTTP
// route and deterministic iteration).
func ListItemListDefs() []*ItemListDef {
	merged := make(map[string]*ItemListDef, len(itemListCatalogSingleton))
	for id, def := range itemListCatalogSingleton {
		merged[id] = def
	}
	runtimeItemListsMu.RLock()
	for id, def := range runtimeItemLists {
		merged[id] = def
	}
	runtimeItemListsMu.RUnlock()
	defs := make([]*ItemListDef, 0, len(merged))
	for _, def := range merged {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}
