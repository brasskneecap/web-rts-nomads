package game

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
)

// Embeds the per-ability catalog tree. Layout mirrors the projectile/effect
// catalogs (one directory per id):
//
//	catalog/abilities/<id>/<id>.json   — AbilityDef for that ability
//
// `all:` is used so the directory can carry a `.keep` placeholder and embed
// cleanly while the catalog is still empty (the first real ability, Heal, is
// authored in a later part). The loader skips non-directory entries, so the
// placeholder is harmless.
//
//go:embed all:catalog/abilities
var abilityDefsFS embed.FS

// AbilityType classifies an ability. Extensible enum (mirrors DamageType):
// new types are added with a const here; "spell" is the one the animation
// system keys on (a "spell" cast plays the unit's casting animation — see
// Part 5 / unit_animation.go).
type AbilityType string

const (
	// AbilitySpell is a magical ability. Casting one drives the unit's
	// "Casting" animation slot (distinct from a basic attack).
	AbilitySpell AbilityType = "spell"
)

// CastRangeMatchAttackRange is the sentinel value of a CastRange that means
// "mirror the caster's current attack range". Authored in JSON either as the
// number -1 or the string "match_attack_range".
const CastRangeMatchAttackRange = -1.0

// CastRange is an ability's targeting range. It unmarshals from JSON as
// EITHER a number (world-pixel radius) OR the string "match_attack_range"
// (resolves to the caster's AttackRange at use time). Any negative number is
// treated as the match sentinel so authoring -1 also works, per the spec.
type CastRange float64

func (c *CastRange) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if s == "match_attack_range" {
			*c = CastRangeMatchAttackRange
			return nil
		}
		return fmt.Errorf("invalid cast_range %q: only the string \"match_attack_range\" is recognised", s)
	}
	var f float64
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	*c = CastRange(f)
	return nil
}

// MatchesAttackRange reports whether this cast range mirrors the caster's
// attack range (the -1 / "match_attack_range" sentinel).
func (c CastRange) MatchesAttackRange() bool { return float64(c) < 0 }

// Resolve returns the concrete world-pixel range for a caster: the caster's
// AttackRange when this is the match sentinel, otherwise the literal value.
// A nil caster resolves the match sentinel to 0 (cannot reach anything).
func (c CastRange) Resolve(caster *Unit) float64 {
	if c.MatchesAttackRange() {
		if caster == nil {
			return 0
		}
		return caster.AttackRange
	}
	return float64(c)
}

// AbilityDef is the authoritative, server-owned definition of an activatable
// ability (spell, etc.). New framework (there is no prior ability system);
// it reuses the existing catalog / DamageType / effect-id conventions rather
// than the passive perk system.
//
// Cast/auto-cast *behavior* (cast-time lifecycle, mana spend, the auto-cast
// loop) is built on this def in later parts; this part owns the data model
// plus targeting and cast-range validation.
type AbilityDef struct {
	// ID is the stable identifier (e.g. "heal"); must match the containing
	// directory name when loaded from the catalog.
	ID string `json:"id"`
	// DisplayName is the player-facing name (e.g. "Heal").
	DisplayName string `json:"displayName"`
	// Type classifies the ability. "spell" drives the casting animation.
	// NOTE: `type` is not in the Part 6 attribute bullet list but is required
	// by the animation (Part 5) and Heal (Part 8) specs — included here so the
	// def is the single source of truth for it.
	Type AbilityType `json:"type,omitempty"`

	// ── Targeting ──────────────────────────────────────────────────────────
	// Whether the ability may target the caster itself / friendly units /
	// hostile units. Defaults are false; an ability authors the ones it needs.
	CanTargetSelf    bool `json:"canTargetSelf,omitempty"`
	CanTargetAllies  bool `json:"canTargetAllies,omitempty"`
	CanTargetEnemies bool `json:"canTargetEnemies,omitempty"`

	// CastRange: number (world px) or "match_attack_range". See CastRange.
	CastRange CastRange `json:"castRange"`

	// ── Cost / timing ──────────────────────────────────────────────────────
	CastTime float64 `json:"castTime,omitempty"` // seconds the cast takes
	ManaCost int     `json:"manaCost,omitempty"` // default 0 (free)
	Cooldown float64 `json:"cooldown,omitempty"` // seconds, default 0

	// DamageType is the optional element/school of the ability (Part 2).
	// Empty = unspecified (resolves to physical via DamageType.OrPhysical()).
	DamageType DamageType `json:"damageType,omitempty"`

	// HealAmount is the HP an effect-ability restores to its target on
	// resolve (Heal). NOTE: not in the Part 6 attribute list — added with the
	// Heal ability as the ability-specific effect magnitude. 0 ⇒ the ability
	// does not heal. Capped at the target's MaxHP (no overheal) at resolve.
	HealAmount int `json:"healAmount,omitempty"`

	// ── Auto-cast ──────────────────────────────────────────────────────────
	// SupportsAutoCast gates whether the action bar exposes an auto-cast
	// toggle for this ability (default false). AutoCastTargetSelector names a
	// targeting strategy in the auto-cast selector registry (built in a later
	// part); it is just a string here — resolution/validation is the
	// registry's job.
	SupportsAutoCast      bool   `json:"supportsAutoCast,omitempty"`
	AutoCastTargetSelector string `json:"autoCastTargetSelector,omitempty"`

	// ── Presentation / resolution hooks ────────────────────────────────────
	// Icon is the action-bar icon path. TODO(asset): real icon art.
	Icon string `json:"icon,omitempty"`
	// CasterAnimation is the animation status the caster plays while casting
	// (e.g. "Casting"). CasterAnimationOrCasting() defaults it.
	CasterAnimation string `json:"casterAnimation,omitempty"`
	// EffectOnTarget is the effect id (Part 4) played on the target when the
	// ability resolves (e.g. "healing_glow"). Empty = none. Resolved
	// fail-safe at use time (an unknown id just means "no effect").
	EffectOnTarget string `json:"effectOnTarget,omitempty"`
}

// CasterAnimationOrCasting returns the caster animation status, defaulting an
// unset value to the canonical "Casting" slot (unit_animation.go).
func (a AbilityDef) CasterAnimationOrCasting() string {
	if a.CasterAnimation == "" {
		return unitStatusCasting
	}
	return a.CasterAnimation
}

// canAbilityTargetUnitLocked reports whether target is a legal target for
// ability `a` cast by caster, per the self / ally / enemy permission flags.
// A nil or dead target is never valid. Classification: same unit = self;
// same TEAM = ally (via unitsFriendlyLocked — alliance is data, Player.TeamID,
// not ownership); otherwise = enemy. At the default single team this is
// identical to the old same-OwnerID rule. (Range is a separate check —
// see WithinCastRange.)
//
// Caller holds s.mu.
func (s *GameState) canAbilityTargetUnitLocked(a AbilityDef, caster, target *Unit) bool {
	if caster == nil || target == nil || target.HP <= 0 {
		return false
	}
	switch {
	case target.ID == caster.ID:
		return a.CanTargetSelf
	case s.unitsFriendlyLocked(caster, target):
		return a.CanTargetAllies
	default:
		return a.CanTargetEnemies
	}
}

// WithinCastRange reports whether target is within this ability's cast range
// of caster (cast range resolved against the caster for the
// match_attack_range case). A non-positive resolved range can never reach a
// distinct target; a unit targeting itself is always within range.
func (a AbilityDef) WithinCastRange(caster, target *Unit) bool {
	if caster == nil || target == nil {
		return false
	}
	if target.ID == caster.ID {
		return true // distance 0 — self is always in range
	}
	r := a.CastRange.Resolve(caster)
	if r <= 0 {
		return false
	}
	return distanceSquared(caster.X, caster.Y, target.X, target.Y) <= r*r
}

// abilityDefsByID is a package-level var, initialized before any init() that
// might reference ability defs (same rationale as the other catalogs).
var abilityDefsByID = loadAbilityDefs()

func loadAbilityDefs() map[string]AbilityDef {
	entries, err := fs.ReadDir(abilityDefsFS, "catalog/abilities")
	if err != nil {
		panic("catalog/abilities: " + err.Error())
	}
	result := make(map[string]AbilityDef, len(entries))
	for _, entry := range entries {
		// Skip non-directory entries (e.g. the .keep placeholder). Abilities
		// must live at catalog/abilities/<id>/<id>.json.
		if !entry.IsDir() {
			continue
		}
		idKey := entry.Name()
		rel := "catalog/abilities/" + idKey + "/" + idKey + ".json"
		data, err := abilityDefsFS.ReadFile(rel)
		if err != nil {
			panic(rel + ": " + err.Error())
		}
		var def AbilityDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic(rel + ": " + err.Error())
		}
		if def.ID == "" {
			panic(rel + `: missing "id" field`)
		}
		if def.ID != idKey {
			panic(rel + ": def.ID " + def.ID + " does not match directory name " + idKey)
		}
		if def.DamageType != "" && !IsValidDamageType(def.DamageType) {
			panic(rel + `: damageType "` + string(def.DamageType) + `" is not a registered damage type`)
		}
		if _, dup := result[def.ID]; dup {
			panic(rel + `: duplicate ability id "` + def.ID + `"`)
		}
		result[def.ID] = def
	}
	return result
}

// getAbilityDef looks up an ability definition by id. The bool is false when
// no ability with that id is registered (same contract as getProjectileDef).
func getAbilityDef(id string) (AbilityDef, bool) {
	def, ok := abilityDefsByID[id]
	return def, ok
}

// ListAbilityDefs returns every registered ability definition sorted by id.
func ListAbilityDefs() []AbilityDef {
	defs := make([]AbilityDef, 0, len(abilityDefsByID))
	for _, def := range abilityDefsByID {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}
