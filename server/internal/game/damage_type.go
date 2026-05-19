package game

import "sort"

// DamageType classifies the element / school of a damage event or ability
// cast. It is an *extensible* enum: the recognised set lives in
// damageTypeRegistry and new schools are added either by appending a const +
// registry entry here or, for content modules, by calling RegisterDamageType
// at init time — existing call sites never need to change.
//
// The damage type of an attack is determined by the attacker's attack or the
// ability definition, NOT by the projectile that carries it (a fire-themed
// archer firing a fire_bolt does fire damage; the bolt sprite is just the
// visual). It is plumbed onto DamageSource so it travels with the damage
// event through the pipeline.
//
// Today damage type is primarily flavor / metadata: it is recorded on the
// damage event but does not change any damage numbers.
//
// TODO: Future support for resistances, weaknesses, and damage type modifiers
// per unit (e.g. a frost-resistant unit takes reduced DamageFrost). When that
// lands, mitigation in the damage pipeline should branch on
// DamageSource.ResolvedDamageType(); no call site that merely *tags* damage
// should need to change.
type DamageType string

const (
	// DamagePhysical is the default for non-magical attacks. It is also what
	// ResolvedDamageType() / OrPhysical() return when a damage event leaves
	// DamageType unset, so the many existing DamageSource{} call sites behave
	// as physical with zero edits.
	DamagePhysical  DamageType = "physical"
	DamageFire      DamageType = "fire"
	DamageFrost     DamageType = "frost"
	DamageLightning DamageType = "lightning"
	DamageArcane    DamageType = "arcane"
	DamageHoly      DamageType = "holy"
	// DamageShadow is the dark/necrotic school. Today, like every other
	// non-physical type, it is flavor/metadata only and does not change damage
	// numbers — it is the tag the Arch Mage's dark_bolt rides on and the seam
	// future shadow-resistance logic / client tinting will read.
	DamageShadow    DamageType = "shadow"
)

// damageTypeRegistry is the recognised damage-type set. Populated at package
// init and extended only via RegisterDamageType (intended for init-time use
// by future content). It is never mutated from the tick loop, so it adds no
// determinism or concurrency concerns.
var damageTypeRegistry = map[DamageType]struct{}{
	DamagePhysical:  {},
	DamageFire:      {},
	DamageFrost:     {},
	DamageLightning: {},
	DamageArcane:    {},
	DamageHoly:      {},
	DamageShadow:    {},
}

// RegisterDamageType adds dt to the recognised set so more schools can be
// introduced without editing this file. Idempotent. Panics on an empty id —
// the empty DamageType is reserved to mean "unspecified → physical" and must
// not be a registerable value.
func RegisterDamageType(dt DamageType) {
	if dt == "" {
		panic("RegisterDamageType: damage type id must be non-empty")
	}
	damageTypeRegistry[dt] = struct{}{}
}

// IsValidDamageType reports whether dt is a registered damage type. The empty
// (unspecified) value is NOT valid here by design: callers that accept
// "unspecified" should resolve it with OrPhysical() first.
func IsValidDamageType(dt DamageType) bool {
	_, ok := damageTypeRegistry[dt]
	return ok
}

// DamageTypes returns every registered damage type, sorted, for stable output
// in catalog APIs, telemetry, and tests.
func DamageTypes() []DamageType {
	out := make([]DamageType, 0, len(damageTypeRegistry))
	for dt := range damageTypeRegistry {
		out = append(out, dt)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// OrPhysical resolves an unspecified (empty) damage type to the default,
// DamagePhysical. Resolve at read time so call sites may leave DamageType
// zero and still get correct, explicit behavior.
func (d DamageType) OrPhysical() DamageType {
	if d == "" {
		return DamagePhysical
	}
	return d
}
