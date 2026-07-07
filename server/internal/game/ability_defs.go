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

	// Category classifies what the ability is for (heal / buff_ally / summon /
	// offensive). Optional; empty = unspecified. INERT in Phase 1 — populated
	// from JSON and validated at load (see init below) but read by no runtime
	// code yet; Phase 2 ability-priority scoring will branch on it. Mirrors the
	// DamageType extensible-enum pattern (see ability_category.go).
	Category AbilityCategory `json:"category,omitempty"`

	// HealAmount is the HP an effect-ability restores to its target on
	// resolve (Heal). NOTE: not in the Part 6 attribute list — added with the
	// Heal ability as the ability-specific effect magnitude. 0 ⇒ the ability
	// does not heal. Capped at the target's MaxHP (no overheal) at resolve.
	HealAmount int `json:"healAmount,omitempty"`

	// DamageAmount is the HP an offensive ability deals to its target on
	// resolve. Symmetric to HealAmount: 0 ⇒ the ability does no damage (the
	// field is additive and inert for every ability that omits it). Applied
	// through the shared authoritative damage pipeline
	// (applyUnitDamageWithSourceLocked) so mitigation, the death pipeline,
	// threat, and determinism are inherited — it is NOT a parallel damage
	// path. HealAmount and DamageAmount are independent resolve steps; an
	// ability may declare either, both, or neither.
	DamageAmount int `json:"damageAmount,omitempty"`

	// TargetCount is the number of targets this ability can affect per cast.
	// Default (0 or absent) normalises to 1 (single-target). Values > 1 drive
	// the multi-target path: the resolver applies the effect to up to TargetCount
	// valid candidates ordered by ascending HP percent, tie-broken by unit.ID.
	// Loaded from JSON key "targetCount"; values < 1 are normalised to 1 at load.
	TargetCount int `json:"targetCount,omitempty"`

	// SummonUnitType is the unit-type id (matches a catalog/units/.../<id>.json)
	// that this ability spawns on resolve. Empty ⇒ ability is not a summon
	// (the field is inert for every existing ability, same pattern as
	// HealAmount / DamageAmount). The spawned unit takes the caster's OwnerID
	// and color and appears at a small fixed offset from the caster's
	// position (deterministic — no RNG). An unknown id is logged once at
	// resolve and the call is a no-op (mana was already spent), matching the
	// existing getProjectileDef miss handling.
	SummonUnitType string `json:"summonUnitType,omitempty"`

	// SummonCount is the number of units (of SummonUnitType) this ability
	// spawns per resolve. Default (0 or absent) normalises to 1. Values > 1
	// fan the spawns out in a deterministic horizontal row below the caster
	// so they do not stack at the same coordinate. Inert when
	// SummonUnitType is empty.
	SummonCount int `json:"summonCount,omitempty"`

	// ── Auto-cast ──────────────────────────────────────────────────────────
	// SupportsAutoCast gates whether the action bar exposes an auto-cast
	// toggle for this ability (default false). AutoCastTargetSelector names a
	// targeting strategy in the auto-cast selector registry (built in a later
	// part); it is just a string here — resolution/validation is the
	// registry's job.
	SupportsAutoCast      bool   `json:"supportsAutoCast,omitempty"`
	AutoCastTargetSelector string `json:"autoCastTargetSelector,omitempty"`
	// DefaultAutoCast controls whether the ability's auto-cast toggle starts
	// in the ENABLED state at unit spawn / ability grant. Only applies when
	// SupportsAutoCast is true. Player intent always overrides — once the
	// player toggles the ability (on or off), the explicit value is preserved
	// through promotions and ability replacements (heal → greater_heal). New
	// abilities granted at higher ranks default per this flag at grant time.
	// Player-owned units only — enemy units never get the default seeded.
	DefaultAutoCast bool `json:"defaultAutoCast,omitempty"`

	// ── Channeled-beam extension ───────────────────────────────────────────
	// ChannelType, when non-empty, routes the ability into the channel
	// lifecycle (ability_channel.go) instead of the standard one-shot cast.
	// The only supported value today is "beam" (Siphon Life). Empty or absent
	// = legacy one-shot cast; all existing abilities are unaffected.
	ChannelType string `json:"channelType,omitempty"`

	// TickIntervalSeconds is the cadence at which the channel's per-tick
	// effect fires (e.g. 0.25 = four times per second). Only meaningful when
	// ChannelType != "".
	TickIntervalSeconds float64 `json:"tickIntervalSeconds,omitempty"`

	// ManaCostPerTick is the mana deducted on each channel tick. The channel
	// stops when the caster cannot afford the next tick. Only meaningful when
	// ChannelType != "". The top-level ManaCost field is the per-CAST cost
	// (paid at channel start); these two are independent.
	ManaCostPerTick int `json:"manaCostPerTick,omitempty"`

	// DamagePerTick is the damage dealt to the channel target each tick.
	// Routed through applyUnitDamageWithSourceLocked. Only meaningful when
	// ChannelType != "".
	DamagePerTick int `json:"damagePerTick,omitempty"`

	// HealingMultiplier scales the damage-to-healing conversion for Siphon
	// Life. healAmount = round(DamagePerTick * HealingMultiplier). Default
	// 0.0 (zero) is normalised to 1.0 at load time so omitting the field
	// produces 1× conversion (lossless siphon). Only meaningful when
	// ChannelType != "".
	HealingMultiplier float64 `json:"healingMultiplier,omitempty"`

	// AllyHealRadius is the world-pixel radius within which the channel
	// looks for an ally to route excess healing to when the caster is at full
	// HP. 0 = no ally routing (self-only). Only meaningful when ChannelType != "".
	AllyHealRadius float64 `json:"allyHealRadius,omitempty"`

	// (Channel-pose frames live on the caster's UnitDef / PathDef — see
	// UnitDef.ChannelLoop and pathCatalogFile.ChannelLoop. They are visual
	// data about the caster's sprite, not the ability, so two units sharing
	// one channel ability can pin to different frames on their own sheets.)

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

	// Projectile is the optional projectile-def id (catalog/projectiles/<id>)
	// an offensive ability launches at its target on resolve. When set (and
	// DamageAmount > 0) the ability's damage is DEFERRED and delivered by a
	// homing bolt that travels to the target and applies the damage on impact —
	// the same pipeline basic-attack shots use — instead of being applied
	// instantly. Empty ⇒ instant (hitscan) damage, the prior behaviour. Inert
	// for abilities with no DamageAmount.
	Projectile string `json:"projectile,omitempty"`
}

// CasterAnimationOrCasting returns the caster animation status, defaulting an
// unset value to the canonical "Casting" slot (unit_animation.go).
func (a AbilityDef) CasterAnimationOrCasting() string {
	if a.CasterAnimation == "" {
		return unitStatusCasting
	}
	return a.CasterAnimation
}

// EffectiveCooldown returns the cooldown duration the UI should display and
// the cooldown system should arm on each cast. It is max(Cooldown, CastTime)
// so the action-bar clock-wipe overlay is always visible while the cast is
// in flight — without this clamp, a 1s-cast / 0s-cooldown spell (base Heal)
// would have NO visible wipe even though the unit is locked in place
// casting for a full second, leaving the player wondering whether their
// click registered. When Cooldown >= CastTime (Greater Heal: 3s cd vs 1s
// cast) the cooldown drives the wipe as authored. Cast time alone is never
// shorter than what the unit is committed to anyway, so this lower bound
// adds no false "still on cooldown" gating.
func (a AbilityDef) EffectiveCooldown() float64 {
	if a.CastTime > a.Cooldown {
		return a.CastTime
	}
	return a.Cooldown
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
	return a.WithinCastRangeScaled(caster, target, 1.0)
}

// WithinCastRangeScaled is WithinCastRange with the resolved range
// multiplied by `mult` first. Used by perks that scale a specific channel
// ability's reach (e.g. beam_mastery on Siphon Life). Pass 1.0 for the
// default behaviour. A non-positive `mult` collapses the range to 0,
// matching the "ability can't reach anything" semantic of CastRange == 0.
func (a AbilityDef) WithinCastRangeScaled(caster, target *Unit, mult float64) bool {
	if caster == nil || target == nil {
		return false
	}
	if target.ID == caster.ID {
		return true // distance 0 — self is always in range
	}
	r := a.CastRange.Resolve(caster) * mult
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
		if def.Category != "" && !IsValidAbilityCategory(def.Category) {
			panic(rel + `: category "` + string(def.Category) + `" is not a registered ability category`)
		}
		if _, dup := result[def.ID]; dup {
			panic(rel + `: duplicate ability id "` + def.ID + `"`)
		}
		// Normalise TargetCount: 0 (omitted) and negative values both mean
		// single-target (TargetCount == 1). This ensures the multi-target path
		// is only entered for explicitly-authored values > 1.
		if def.TargetCount < 1 {
			def.TargetCount = 1
		}
		// Normalise SummonCount the same way: 0 / negative ⇒ 1. The summon
		// loop in spawnSummonedUnitLocked is only entered when SummonUnitType
		// is set, so this normalisation is inert for non-summon abilities.
		if def.SummonCount < 1 {
			def.SummonCount = 1
		}
		// Normalise HealingMultiplier: 0 (omitted) → 1.0 so a siphon_life
		// that omits the field gets a lossless 1× conversion. Only applies
		// to channeled abilities; for one-shot casts the field is unused and
		// this normalisation is a harmless no-op.
		if def.ChannelType != "" && def.HealingMultiplier == 0 {
			def.HealingMultiplier = 1.0
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
