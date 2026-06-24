package game

import (
	"embed"
	"encoding/json"
	"io/fs"
	"sort"
)

//go:embed catalog/profile-upgrades/*.json
var profileUpgradeDefsFS embed.FS

// ProfileUpgradeDef is the static definition of a persistent profile upgrade
// loaded from catalog/profile-upgrades/<id>.json. Profile upgrades are
// purchased with Dominion Points between matches and applied at match start.
type ProfileUpgradeDef struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	MaxRanks    int                  `json:"maxRanks"`
	CostPerRank []int                `json:"costPerRank"`
	Effect      ProfileUpgradeEffect `json:"effect"`
}

// ProfileUpgradeEffect is the typed effect payload for a ProfileUpgradeDef.
// The Type field discriminates the active mode; all other fields are
// type-specific and are ignored for types that don't use them.
type ProfileUpgradeEffect struct {
	Type string `json:"type"`
	// extraStartingUnit fields
	UnitType     string `json:"unitType,omitempty"`
	CountPerRank int    `json:"countPerRank,omitempty"`
	// damageMultiplierByType fields
	DamageTypeClass   string  `json:"damageTypeClass,omitempty"`
	MultiplierPerRank float64 `json:"multiplierPerRank,omitempty"`
	// startingResource fields
	ResourceType  string `json:"resourceType,omitempty"`
	AmountPerRank int    `json:"amountPerRank,omitempty"`
}

// profileUpgradeEffectHandler pairs a startup validator with a match-start
// applier for a single effect type. Validators run at init; appliers run once
// per player per match start.
type profileUpgradeEffectHandler struct {
	// validate is called during loadProfileUpgradeDefs for every def whose
	// effect type matches this handler. It should panic with a descriptive
	// message (including the file name) when the effect fields are invalid.
	validate func(filename string, effect ProfileUpgradeEffect)

	// applyAtMatchStart is called once per owned rank when a player joins a
	// match. It updates the precomputed convenience fields on player.
	applyAtMatchStart func(player *Player, rank int, effect ProfileUpgradeEffect)
}

// profileUpgradeEffectRegistry maps effect type strings to their handlers.
// New effect types are registered here; the load function and match-start
// applier call into this map, so no call sites need updating.
var profileUpgradeEffectRegistry = map[string]profileUpgradeEffectHandler{
	"extraStartingUnit": {
		validate: func(filename string, effect ProfileUpgradeEffect) {
			if effect.UnitType == "" {
				panic("catalog/profile-upgrades/" + filename + `: effect "extraStartingUnit" requires non-empty unitType`)
			}
			if _, ok := getUnitDef(effect.UnitType); !ok {
				panic("catalog/profile-upgrades/" + filename + `: effect "extraStartingUnit" unitType "` + effect.UnitType + `" is not in the unit catalog`)
			}
			if effect.CountPerRank <= 0 {
				panic("catalog/profile-upgrades/" + filename + `: effect "extraStartingUnit" requires countPerRank > 0`)
			}
		},
		applyAtMatchStart: func(player *Player, rank int, effect ProfileUpgradeEffect) {
			if player.ExtraStartingUnits == nil {
				player.ExtraStartingUnits = map[string]int{}
			}
			player.ExtraStartingUnits[effect.UnitType] += rank * effect.CountPerRank
		},
	},
	"damageMultiplierByType": {
		validate: func(filename string, effect ProfileUpgradeEffect) {
			switch effect.DamageTypeClass {
			case "physical", "nonPhysical":
				// valid
			default:
				panic(`catalog/profile-upgrades/` + filename + `: effect "damageMultiplierByType" damageTypeClass must be "physical" or "nonPhysical", got "` + effect.DamageTypeClass + `"`)
			}
			if effect.MultiplierPerRank <= 0 {
				panic("catalog/profile-upgrades/" + filename + `: effect "damageMultiplierByType" requires multiplierPerRank > 0`)
			}
		},
		applyAtMatchStart: func(player *Player, rank int, effect ProfileUpgradeEffect) {
			bonus := float64(rank) * effect.MultiplierPerRank
			switch effect.DamageTypeClass {
			case "physical":
				player.PhysicalDamageMultiplier += bonus
			case "nonPhysical":
				player.MagicDamageMultiplier += bonus
			}
		},
	},
	"startingResource": {
		validate: func(filename string, effect ProfileUpgradeEffect) {
			if effect.ResourceType == "" {
				panic("catalog/profile-upgrades/" + filename + `: effect "startingResource" requires non-empty resourceType`)
			}
			if _, ok := playerConfig().StartingResources[effect.ResourceType]; !ok {
				panic("catalog/profile-upgrades/" + filename + `: effect "startingResource" resourceType "` + effect.ResourceType + `" is not a configured starting resource (see catalog/player/player.json)`)
			}
			if effect.AmountPerRank <= 0 {
				panic("catalog/profile-upgrades/" + filename + `: effect "startingResource" requires amountPerRank > 0`)
			}
		},
		applyAtMatchStart: func(player *Player, rank int, effect ProfileUpgradeEffect) {
			if player.Resources == nil {
				player.Resources = map[string]int{}
			}
			player.Resources[effect.ResourceType] += rank * effect.AmountPerRank
		},
	},
}

// profileUpgradeDefsByID is the package-level catalog, loaded once at startup.
// Never mutated after initialization.
var profileUpgradeDefsByID = loadProfileUpgradeDefs()

func loadProfileUpgradeDefs() map[string]ProfileUpgradeDef {
	entries, err := fs.ReadDir(profileUpgradeDefsFS, "catalog/profile-upgrades")
	if err != nil {
		panic("catalog/profile-upgrades: " + err.Error())
	}
	result := make(map[string]ProfileUpgradeDef, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		filename := entry.Name()
		data, err := profileUpgradeDefsFS.ReadFile("catalog/profile-upgrades/" + filename)
		if err != nil {
			panic("catalog/profile-upgrades/" + filename + ": " + err.Error())
		}
		var def ProfileUpgradeDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic("catalog/profile-upgrades/" + filename + ": " + err.Error())
		}
		if def.ID == "" {
			panic("catalog/profile-upgrades/" + filename + `: missing "id"`)
		}
		if def.MaxRanks <= 0 {
			panic("catalog/profile-upgrades/" + filename + `: "maxRanks" must be > 0`)
		}
		if len(def.CostPerRank) != def.MaxRanks {
			panic("catalog/profile-upgrades/" + filename + `: "costPerRank" length ` +
				itoa(len(def.CostPerRank)) + ` does not match "maxRanks" ` + itoa(def.MaxRanks))
		}
		for i, cost := range def.CostPerRank {
			if cost <= 0 {
				panic("catalog/profile-upgrades/" + filename + `: costPerRank[` + itoa(i) + `] must be > 0`)
			}
		}
		handler, ok := profileUpgradeEffectRegistry[def.Effect.Type]
		if !ok {
			panic("catalog/profile-upgrades/" + filename + `: unknown effect type "` + def.Effect.Type + `"`)
		}
		handler.validate(filename, def.Effect)
		if _, dup := result[def.ID]; dup {
			panic(`catalog/profile-upgrades/` + filename + `: duplicate id "` + def.ID + `"`)
		}
		result[def.ID] = def
	}
	return result
}

// getProfileUpgradeDef returns the ProfileUpgradeDef for id and whether it
// was found. Internal use only; HTTP handlers use GetProfileUpgradeDef.
func getProfileUpgradeDef(id string) (ProfileUpgradeDef, bool) {
	def, ok := profileUpgradeDefsByID[id]
	return def, ok
}

// GetProfileUpgradeDef is the exported accessor for the profile upgrade
// catalog, intended for use by the HTTP handlers and other outside packages.
func GetProfileUpgradeDef(id string) (ProfileUpgradeDef, bool) {
	return getProfileUpgradeDef(id)
}

// ListProfileUpgradeDefs returns all registered profile upgrade definitions
// sorted by ID.
func ListProfileUpgradeDefs() []ProfileUpgradeDef {
	defs := make([]ProfileUpgradeDef, 0, len(profileUpgradeDefsByID))
	for _, d := range profileUpgradeDefsByID {
		defs = append(defs, d)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}

// applyProfileUpgradesToPlayerLocked walks the player's owned upgrade ranks
// in sorted ID order and calls each effect's applyAtMatchStart hook to
// populate PhysicalDamageMultiplier, MagicDamageMultiplier, and
// ExtraStartingWorkers. Only upgrades present in player.ActiveUpgradeIDs are
// applied; upgrades owned but inactive are skipped. Must be called under s.mu
// write lock.
func applyProfileUpgradesToPlayerLocked(player *Player) {
	if len(player.ProfileUpgrades) == 0 {
		return
	}
	// Collect and sort IDs for determinism.
	ids := make([]string, 0, len(player.ProfileUpgrades))
	for id := range player.ProfileUpgrades {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		// Skip upgrades that are owned but not active.
		if !player.ActiveUpgradeIDs[id] {
			continue
		}
		rank := player.ProfileUpgrades[id]
		if rank <= 0 {
			continue
		}
		def, ok := getProfileUpgradeDef(id)
		if !ok {
			// Unknown upgrade ID in the profile — skip gracefully. This can
			// happen if a catalog entry is removed after purchase.
			continue
		}
		handler, ok := profileUpgradeEffectRegistry[def.Effect.Type]
		if !ok {
			continue
		}
		handler.applyAtMatchStart(player, rank, def.Effect)
	}
}

// itoa is a small helper to avoid importing strconv just for int formatting.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
