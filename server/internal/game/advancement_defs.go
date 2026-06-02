package game

import (
	"encoding/json"
	"io/fs"
	"math"
	"sort"
)

// UnitAdvancementTrack is the catalog shape for a single unit type's
// advancement tree. It is what the API serves under advancementCatalog and
// what advancements.json files decode into (after unwrapping the track object).
type UnitAdvancementTrack struct {
	// UnitType is the unit type these nodes apply to (e.g. "soldier").
	UnitType string                `json:"unitType"`
	// Nodes is the ordered list of advancement nodes for the unit type. Purchase
	// order is left-to-right: node at index N requires node at index N-1.
	Nodes []UnitAdvancementNode `json:"nodes"`
}

// UnitAdvancementNode is the static definition of a single advancement node
// within a track. Loaded from catalog/units/<faction>/<unit>/advancements.json.
type UnitAdvancementNode struct {
	// ID is the globally unique advancement identifier. Must be non-empty.
	ID string `json:"id"`
	// Name is the display name shown in the UI.
	Name string `json:"name"`
	// Description explains the effect to the player.
	Description string `json:"description"`
	// Kind discriminates minor vs major nodes. The frontend uses this to pick
	// the seal vs medal-slot icon. Valid values: "minor", "major".
	Kind string `json:"kind"`
	// Cost is the Legend Point cost to purchase this node (one-time).
	Cost int `json:"cost"`
	// Effects is the slice of typed effects applied at match start. Multiple
	// effects may appear (e.g. a future node that grants both +HP and an extra
	// perk slot). MVP nodes carry exactly one effect.
	Effects []UnitAdvancementEffect `json:"effects"`
	// UnitType is the parent track's unit type, copied into the node at load
	// time so the flat advancementNodesByID lookup carries the parent ref.
	// json:"-" because the wire shape exposes UnitType on the track wrapper.
	UnitType string `json:"-"`
}

// UnitAdvancementEffect is the typed effect payload for a UnitAdvancementNode.
// The Kind field discriminates the active mode.
type UnitAdvancementEffect struct {
	// Kind is the effect discriminator. Registered kinds: "unitStatAdd", "unitStatMul", "unitSpawnExp", "unitExtraPerkSlot", "unitBonusArrows", "unitTrapEffectMul", "unitTrapRadiusMul".
	Kind string `json:"kind"`
	// unitStatAdd / unitStatMul fields:
	// Stat names the unit stat to modify. Recognised values: "maxHp", "damage",
	// "attackRange", "attackSpeed", "moveSpeed", "armor".
	Stat string `json:"stat,omitempty"`
	// Amount is the integer value added to the named stat (unitStatAdd). Also
	// used by unitSpawnExp as the amount of XP to pre-load at spawn.
	Amount int `json:"amount,omitempty"`
	// unitStatMul fields:
	// Percent is the percentage by which to scale the named stat (unitStatMul):
	// 10 means +10% (multiplier 1.10). Stacks multiplicatively across nodes.
	// Integer-typed stats (maxHp, damage, armor) are rounded to nearest after
	// scaling; float stats (attackSpeed, attackRange, moveSpeed) keep precision.
	Percent float64 `json:"percent,omitempty"`
	// unitExtraPerkSlot fields:
	// Tier is the perk tier the second slot draws from: "bronze" | "silver" | "gold".
	Tier string `json:"tier,omitempty"`
	// Rank is reserved for future "two silvers / two golds" expansion; the MVP
	// handler validates Rank == 1 (only single extra slot is supported today).
	Rank int `json:"rank,omitempty"`
}

// advancementEffectHandler pairs a startup validator with a match-start
// applier for a single effect kind. Validators run at init; appliers run once
// per player per match start for each advancement the player owns.
type advancementEffectHandler struct {
	// validate is called during loadAdvancementDefs for every node effect whose
	// kind matches this handler. It should panic with a descriptive message
	// (including the source path) when the effect fields are invalid.
	validate func(src string, effect UnitAdvancementEffect)

	// applyAtMatchStart receives the effective unit def (a value copy of the
	// catalog def, safe to mutate) and the advancement effect. It mutates the
	// def in place; the modified def is stored in the player's
	// EffectiveUnitDefs map and consulted at spawn time.
	applyAtMatchStart func(def *UnitDef, effect UnitAdvancementEffect)
}

// advancementEffectRegistry maps effect kind strings to their handlers.
// New kinds are registered here; the load function and match-start applier
// call into this map, so no call sites need updating.
var advancementEffectRegistry = map[string]advancementEffectHandler{
	"unitStatAdd": {
		validate: func(src string, effect UnitAdvancementEffect) {
			switch effect.Stat {
			case "maxHp", "damage", "attackRange", "attackSpeed", "moveSpeed", "armor":
				// valid
			default:
				panic(src + `: effect "unitStatAdd" stat must be one of "maxHp", "damage", "attackRange", "attackSpeed", "moveSpeed", "armor", got "` + effect.Stat + `"`)
			}
			if effect.Amount == 0 {
				panic(src + `: effect "unitStatAdd" requires non-zero amount`)
			}
		},
		applyAtMatchStart: func(def *UnitDef, effect UnitAdvancementEffect) {
			switch effect.Stat {
			case "maxHp":
				def.HP += effect.Amount
			case "damage":
				def.Damage += effect.Amount
			case "attackRange":
				def.AttackRange += float64(effect.Amount)
			case "attackSpeed":
				def.AttackSpeed += float64(effect.Amount)
			case "moveSpeed":
				def.MoveSpeed += float64(effect.Amount)
			case "armor":
				def.Armor += effect.Amount
			}
		},
	},
	// unitStatMul scales a unit stat by a percentage (multiplicative). Percent
	// is the percentage delta: 10 → ×1.10. Multiple owned nodes that scale the
	// same stat stack multiplicatively (two +10% nodes → ×1.21). Used for
	// "+X% attack speed" style advancements that a flat unitStatAdd cannot
	// express on a float stat. Integer stats are rounded to nearest after
	// scaling so the spawn pipeline still sees clean integers.
	"unitStatMul": {
		validate: func(src string, effect UnitAdvancementEffect) {
			switch effect.Stat {
			case "maxHp", "damage", "attackRange", "attackSpeed", "moveSpeed", "armor":
				// valid
			default:
				panic(src + `: effect "unitStatMul" stat must be one of "maxHp", "damage", "attackRange", "attackSpeed", "moveSpeed", "armor", got "` + effect.Stat + `"`)
			}
			if effect.Percent == 0 {
				panic(src + `: effect "unitStatMul" requires non-zero percent`)
			}
		},
		applyAtMatchStart: func(def *UnitDef, effect UnitAdvancementEffect) {
			mul := 1 + effect.Percent/100
			switch effect.Stat {
			case "maxHp":
				def.HP = int(math.Round(float64(def.HP) * mul))
			case "damage":
				def.Damage = int(math.Round(float64(def.Damage) * mul))
			case "attackRange":
				def.AttackRange *= mul
			case "attackSpeed":
				def.AttackSpeed *= mul
			case "moveSpeed":
				def.MoveSpeed *= mul
			case "armor":
				def.Armor = int(math.Round(float64(def.Armor) * mul))
			}
		},
	},
	// unitSpawnExp pre-loads XP onto a unit at spawn. Units that start with
	// enough XP to rank up will trigger the rank-up pipeline immediately after
	// spawn via addUnitXPLocked (called indirectly through the normal XP path
	// once the first tick runs). Effect amount must be > 0.
	"unitSpawnExp": {
		validate: func(src string, effect UnitAdvancementEffect) {
			if effect.Amount <= 0 {
				panic(src + `: effect "unitSpawnExp" requires amount > 0`)
			}
		},
		applyAtMatchStart: func(def *UnitDef, effect UnitAdvancementEffect) {
			def.SpawnExp += effect.Amount
		},
	},
	// unitExtraPerkSlot grants a second perk of the named tier at rank-up. The
	// match-start applier flips a flag on the Player rather than mutating the
	// UnitDef, because the effect fires at rank-up time (perks.go), not spawn
	// time. Tier is one of "bronze" / "silver" / "gold"; Rank is reserved for
	// future "two silvers / two golds" expansion and must be 1 today.
	"unitExtraPerkSlot": {
		validate: func(src string, effect UnitAdvancementEffect) {
			switch effect.Tier {
			case "bronze", "silver", "gold":
				// valid
			default:
				panic(src + `: effect "unitExtraPerkSlot" tier must be "bronze", "silver", or "gold", got "` + effect.Tier + `"`)
			}
			if effect.Rank != 1 {
				panic(src + `: effect "unitExtraPerkSlot" rank must be 1 (only single extra slot is supported today)`)
			}
		},
		applyAtMatchStart: func(def *UnitDef, effect UnitAdvancementEffect) {
			// No-op on UnitDef — this handler signals via Player.ExtraPerkSlots,
			// which is populated by a sibling pass in applyAdvancementsToEffectiveDefsLocked.
		},
	},
	// unitBonusArrows grants extra arrows per attack, fired through the
	// split-shot fan-out (see onMarksmanProjectileFiredLocked). Stacks on top
	// of the split_shot perk's own extra-shot count. Amount is the number of
	// extra arrows to add; must be > 0.
	"unitBonusArrows": {
		validate: func(src string, effect UnitAdvancementEffect) {
			if effect.Amount <= 0 {
				panic(src + `: effect "unitBonusArrows" requires amount > 0`)
			}
		},
		applyAtMatchStart: func(def *UnitDef, effect UnitAdvancementEffect) {
			def.BonusArrows += effect.Amount
		},
	},
	// unitTrapEffectMul scales the strength of every trap this unit plants
	// (damage, slow, mark) by a percentage. Percent is the percentage delta:
	// 100 → ×2 trap effect. Stored as an additive fraction on the def and
	// folded into the trap pipeline's EffectMultiplier as (1 + fraction) at
	// plant time (see trapModifiersForUnitLocked). Non-zero percent required.
	"unitTrapEffectMul": {
		validate: func(src string, effect UnitAdvancementEffect) {
			if effect.Percent == 0 {
				panic(src + `: effect "unitTrapEffectMul" requires non-zero percent`)
			}
		},
		applyAtMatchStart: func(def *UnitDef, effect UnitAdvancementEffect) {
			def.TrapEffectBonus += effect.Percent / 100
		},
	},
	// unitTrapRadiusMul scales the radius of every trap this unit plants by a
	// percentage. Percent is the percentage delta: 100 → ×2 radius. Mirrors
	// unitTrapEffectMul, folded into the trap pipeline's RadiusMultiplier.
	"unitTrapRadiusMul": {
		validate: func(src string, effect UnitAdvancementEffect) {
			if effect.Percent == 0 {
				panic(src + `: effect "unitTrapRadiusMul" requires non-zero percent`)
			}
		},
		applyAtMatchStart: func(def *UnitDef, effect UnitAdvancementEffect) {
			def.TrapRadiusBonus += effect.Percent / 100
		},
	},
}

// advancementNodesByID is the package-level flat catalog for O(1) lookup by ID.
// advancementTracks is the ordered list of tracks used by ListUnitAdvancementTracks.
// Both are loaded once at startup and never mutated afterward.
//
// The var body references unitDefsByType (same technique as mapCatalog in
// maps.go) to inform Go's init-order analysis that unitDefsByType must be
// initialized before this var, so getUnitDef calls inside loadAdvancementDefs
// see a populated map.
var advancementNodesByID, advancementTracks = func() (map[string]UnitAdvancementNode, []UnitAdvancementTrack) {
	_ = unitDefsByType // dependency ordering guard; see unit_defs.go
	return loadAdvancementDefs()
}()

// advancementTrackFile is the on-disk shape of a single advancements.json file.
// Nodes do not carry a UnitType field — that comes from the track wrapper.
type advancementTrackFile struct {
	UnitType string                `json:"unitType"`
	Nodes    []UnitAdvancementNode `json:"nodes"`
}

// loadAdvancementDefs walks catalog/units/**/<unit>/advancements.json files
// and builds the global advancement catalog. The embed FS for unit defs
// (unitDefsFS) already covers the entire catalog/units tree, so we reuse it.
//
// Files are optional: a unit directory without advancements.json simply has
// no advancements. The loader panics only on malformed JSON or violated
// invariants (duplicate IDs, unknown effect kinds, etc.).
func loadAdvancementDefs() (map[string]UnitAdvancementNode, []UnitAdvancementTrack) {
	byID := make(map[string]UnitAdvancementNode, 8)
	var tracks []UnitAdvancementTrack

	factionEntries, err := fs.ReadDir(unitDefsFS, "catalog/units")
	if err != nil {
		panic("catalog/units (advancements): " + err.Error())
	}

	for _, factionEntry := range factionEntries {
		if !factionEntry.IsDir() {
			continue
		}
		factionKey := factionEntry.Name()
		unitEntries, err := fs.ReadDir(unitDefsFS, "catalog/units/"+factionKey)
		if err != nil {
			panic("catalog/units/" + factionKey + " (advancements): " + err.Error())
		}
		for _, unitEntry := range unitEntries {
			if !unitEntry.IsDir() {
				continue
			}
			unitKey := unitEntry.Name()
			rel := "catalog/units/" + factionKey + "/" + unitKey + "/advancements.json"
			data, readErr := unitDefsFS.ReadFile(rel)
			if readErr != nil {
				// No advancements.json for this unit — skip silently.
				continue
			}

			var trackFile advancementTrackFile
			if err := json.Unmarshal(data, &trackFile); err != nil {
				panic(rel + ": " + err.Error())
			}
			if trackFile.UnitType == "" {
				panic(rel + `: track missing "unitType"`)
			}
			if _, ok := getUnitDef(trackFile.UnitType); !ok {
				panic(rel + `: track unitType "` + trackFile.UnitType + `" is not in the unit catalog`)
			}

			for i, node := range trackFile.Nodes {
				if node.ID == "" {
					panic(rel + `: node at index ` + itoa(i) + ` missing "id"`)
				}
				switch node.Kind {
				case "minor", "major":
					// valid
				default:
					panic(rel + `: node "` + node.ID + `" kind must be "minor" or "major", got "` + node.Kind + `"`)
				}
				if node.Cost <= 0 {
					panic(rel + `: node "` + node.ID + `" cost must be > 0`)
				}
				if len(node.Effects) == 0 {
					panic(rel + `: node "` + node.ID + `" has no effects`)
				}
				for ei, eff := range node.Effects {
					handler, ok := advancementEffectRegistry[eff.Kind]
					if !ok {
						panic(rel + `: node "` + node.ID + `" effect[` + itoa(ei) + `] unknown kind "` + eff.Kind + `"`)
					}
					handler.validate(rel+` node "`+node.ID+`" effect[`+itoa(ei)+`]`, eff)
				}
				if _, dup := byID[node.ID]; dup {
					panic(rel + `: duplicate advancement id "` + node.ID + `"`)
				}
				node.UnitType = trackFile.UnitType
				byID[node.ID] = node
				// Mirror into the track's slice so anything iterating the
				// track-side copy also sees the back-reference.
				trackFile.Nodes[i].UnitType = trackFile.UnitType
			}

			tracks = append(tracks, UnitAdvancementTrack{
				UnitType: trackFile.UnitType,
				Nodes:    trackFile.Nodes,
			})
		}
	}

	// Sort tracks by UnitType for deterministic API output. Node order within
	// each track is preserved from file order (left-to-right purchase chain).
	sort.Slice(tracks, func(i, j int) bool {
		return tracks[i].UnitType < tracks[j].UnitType
	})

	return byID, tracks
}

// GetAdvancementDef returns the UnitAdvancementNode for id and whether it was found.
// The name is kept for backward-compatibility with existing call sites.
func GetAdvancementDef(id string) (UnitAdvancementNode, bool) {
	node, ok := advancementNodesByID[id]
	return node, ok
}

// GetAdvancementPrerequisiteID returns the ID of the node that must be acquired
// before the node with the given id can be purchased. Returns "" when the node
// is the first in its track (no prerequisite) or when id is not found.
//
// Prerequisite order is left-to-right within a track: node at index N requires
// the node at index N-1. Index 0 has no prerequisite.
func GetAdvancementPrerequisiteID(id string) string {
	for _, track := range advancementTracks {
		for i, node := range track.Nodes {
			if node.ID == id {
				if i == 0 {
					return ""
				}
				return track.Nodes[i-1].ID
			}
		}
	}
	return ""
}

// ListUnitAdvancementTracks returns all registered advancement tracks sorted by
// UnitType. Node order within each track reflects the file order (left-to-right
// purchase chain).
func ListUnitAdvancementTracks() []UnitAdvancementTrack {
	// Return a copy so callers cannot mutate the package-level slice.
	out := make([]UnitAdvancementTrack, len(advancementTracks))
	copy(out, advancementTracks)
	return out
}

// applyAdvancementsToEffectiveDefsLocked computes the per-player effective
// UnitDef overrides for every advancement the player owns, and stores them in
// player.EffectiveUnitDefs. Called once per player at match start (inside
// EnsurePlayerWithUpgrades) after the player struct is created. Must be called
// under s.mu write lock.
//
// The algorithm:
//  1. Start with a copy of the catalog UnitDef for each touched unit type.
//  2. Apply each owned advancement node's effects via applyAtMatchStart hooks.
//  3. Store the modified copy in player.EffectiveUnitDefs[unitType].
//
// Advancements not present in the catalog are skipped gracefully (catalog
// entry removed after purchase).
func applyAdvancementsToEffectiveDefsLocked(player *Player) {
	if len(player.AcquiredAdvancements) == 0 {
		return
	}

	// Collect sorted advancement IDs for determinism. The slice is already
	// stored as []string on Player, copied from profile.AcquiredAdvancements
	// IDs at join time.
	ids := make([]string, len(player.AcquiredAdvancements))
	copy(ids, player.AcquiredAdvancements)
	sort.Strings(ids)

	// Build a working map of unitType -> *UnitDef copy.
	// We lazily clone from the catalog on first touch per unit type.
	working := make(map[string]*UnitDef, 4)

	for _, id := range ids {
		node, ok := advancementNodesByID[id]
		if !ok {
			// Catalog entry removed after purchase — skip gracefully.
			continue
		}
		unitDef, alreadyCloned := working[node.UnitType]
		if !alreadyCloned {
			catalogDef, catalogOK := getUnitDef(node.UnitType)
			if !catalogOK {
				continue
			}
			clone := catalogDef // value copy of the struct
			unitDef = &clone
			working[node.UnitType] = unitDef
		}
		for _, eff := range node.Effects {
			handler, ok := advancementEffectRegistry[eff.Kind]
			if !ok {
				continue
			}
			handler.applyAtMatchStart(unitDef, eff)
		}
	}

	// Second pass: populate Player.ExtraPerkSlots for any unitExtraPerkSlot
	// effects. This must be a separate pass because the applyAtMatchStart
	// signature is (*UnitDef, UnitAdvancementEffect) and cannot touch Player.
	for _, id := range ids {
		node, ok := advancementNodesByID[id]
		if !ok {
			continue
		}
		for _, eff := range node.Effects {
			if eff.Kind != "unitExtraPerkSlot" {
				continue
			}
			if player.ExtraPerkSlots == nil {
				player.ExtraPerkSlots = make(map[string]map[string]bool, 1)
			}
			tiers, hasUnit := player.ExtraPerkSlots[node.UnitType]
			if !hasUnit {
				tiers = make(map[string]bool, 1)
				player.ExtraPerkSlots[node.UnitType] = tiers
			}
			tiers[eff.Tier] = true
		}
	}

	if len(working) == 0 {
		return
	}

	if player.EffectiveUnitDefs == nil {
		player.EffectiveUnitDefs = make(map[string]UnitDef, len(working))
	}
	for unitType, def := range working {
		player.EffectiveUnitDefs[unitType] = *def
	}
}
