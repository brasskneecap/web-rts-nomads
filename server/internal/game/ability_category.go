package game

import "sort"

// AbilityCategory classifies what an ability is *for* (heal an ally, buff an
// ally, summon, deal damage). It is an *extensible* enum modelled 1:1 on
// DamageType (see damage_type.go): the recognised set lives in
// abilityCategoryRegistry and new categories are added either by appending a
// const + registry entry here or, for content modules, by calling
// RegisterAbilityCategory at init time — existing call sites never change.
//
// The empty value ("") is the reserved "unspecified" default and is NOT a
// registerable / valid category, identical to DamageType semantics. An ability
// JSON that omits "category" loads with Category == "".
//
// Phase 1 status: this enum is *inert*. AbilityDef.Category is populated from
// JSON and validated at load, but no runtime code reads it yet. Phase 2's
// ability-priority scoring is what will branch on Category; landing the type +
// field + load validation now gives that work a ready, validated seam without
// any Phase-1 behaviour change.
type AbilityCategory string

const (
	// AbilityCategoryHeal restores ally HP (e.g. heal, greater_heal).
	AbilityCategoryHeal AbilityCategory = "heal"
	// AbilityCategoryBuffAlly applies a beneficial effect to an ally
	// (armor / attack-damage / attack-speed buffs, etc.).
	AbilityCategoryBuffAlly AbilityCategory = "buff_ally"
	// AbilityCategorySummon spawns one or more units; the cast target is self.
	AbilityCategorySummon AbilityCategory = "summon"
	// AbilityCategoryOffensive deals damage / debuffs an enemy.
	AbilityCategoryOffensive AbilityCategory = "offensive"
)

// abilityCategoryRegistry is the recognised ability-category set. Populated at
// package init and extended only via RegisterAbilityCategory (intended for
// init-time use by future content). It is never mutated from the tick loop, so
// it adds no determinism or concurrency concerns.
var abilityCategoryRegistry = map[AbilityCategory]struct{}{
	AbilityCategoryHeal:      {},
	AbilityCategoryBuffAlly:  {},
	AbilityCategorySummon:    {},
	AbilityCategoryOffensive: {},
}

// RegisterAbilityCategory adds c to the recognised set so more categories can
// be introduced without editing this file. Idempotent. Panics on an empty id —
// the empty AbilityCategory is reserved to mean "unspecified" and must not be a
// registerable value.
func RegisterAbilityCategory(c AbilityCategory) {
	if c == "" {
		panic("RegisterAbilityCategory: ability category id must be non-empty")
	}
	abilityCategoryRegistry[c] = struct{}{}
}

// IsValidAbilityCategory reports whether c is a registered ability category.
// The empty (unspecified) value is NOT valid here by design: an ability with
// no category simply leaves Category == "" and is not validated against the
// registry.
func IsValidAbilityCategory(c AbilityCategory) bool {
	_, ok := abilityCategoryRegistry[c]
	return ok
}

// AbilityCategories returns every registered ability category, sorted, for
// stable output in catalog APIs, telemetry, and tests.
func AbilityCategories() []AbilityCategory {
	out := make([]AbilityCategory, 0, len(abilityCategoryRegistry))
	for c := range abilityCategoryRegistry {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
