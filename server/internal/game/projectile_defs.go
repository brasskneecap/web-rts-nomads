package game

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
)

// Embeds the per-projectile catalog tree. Layout mirrors the unit catalog
// (one directory per id) so a projectile can grow sibling files later
// (sprites manifests, tuning overrides) without reshuffling the tree:
//
//	catalog/projectiles/<id>/<id>.json   — ProjectileDef for that projectile
//
// Adding a new projectile: create catalog/projectiles/<newid>/<newid>.json.
// The directory name must match the JSON's `id` field; mismatch panics at
// startup so the catalog stays coherent (same discipline as unit_defs.go).
//
//go:embed catalog/projectiles
var projectileDefsFS embed.FS

// EmitterKind discriminates the two ways a source can throw an effect at a
// target. It is the single declarative switch the spawn code branches on so a
// content author states "beam" or "projectile" once, on the def, and every
// reference (unit shots, item on-hit procs) routes to the right entity and the
// right client asset folder automatically.
type EmitterKind string

const (
	// EmitterKindProjectile is a flying, homing entity that travels from the
	// source to the target and lands damage on impact. Client art lives under
	// client/.../assets/projectiles/<id>/. This is the default when a def omits
	// "kind", so every pre-existing projectile def stays valid untouched.
	EmitterKindProjectile EmitterKind = "projectile"
	// EmitterKindBeam is an instantaneous line from source to target: no flight,
	// damage applied the moment it fires, rendered as a short-lived beam flash.
	// Client art lives under client/.../assets/beams/<id>/.
	EmitterKindBeam EmitterKind = "beam"
)

// defaultBeamDurationMs is how long a momentary (proc-fired) beam flash stays
// on screen when a beam def omits "durationMs". ~260ms reads as a snappy zap
// while still showing a few frames of the beam's animation.
const defaultBeamDurationMs = 260

// beamProcDamageDelaySeconds is how long a momentary proc beam waits before it
// lands its damage. A beam is instantaneous, so applying damage immediately
// would merge its number into the triggering hit's number (same tick). A short
// delay (well under the flash's lifetime) drops the damage onto a LATER tick so
// it reads as its own floating number — the same separation the old projectile
// version got for free by traveling. Kept < defaultBeamDurationMs so the number
// pops while the zap is still visible.
const beamProcDamageDelaySeconds = 0.12

// ProjectileDef is the authoritative, server-owned definition of an *emitted
// effect* — a projectile OR a beam (see Kind). The server uses it for flight
// speed / beam duration and (future) behavior; the client owns the visuals
// separately via a mirrored sprites.json under client/.../assets/<projectiles
// or beams>/<id>/ (same split as units, whose UnitDef carries no sprite paths).
//
// The type is still named ProjectileDef (and lives under catalog/projectiles)
// for continuity; "beam" is an opt-in Kind rather than a separate catalog so a
// single reference by id resolves to whichever the author declared.
//
// Hit/miss and damage rules for PROJECTILES are NOT data on this struct —
// they are fixed behavior (see attackHitsLocked / applyProjectileDamageLocked):
//   - A projectile hits or misses based on the *target's* dodge/block stats
//     if it has any; a target with no evasion stats is always hit.
//   - On hit it damages only the resolved target — no AoE, no pass-through.
//
// Beams are instantaneous and always connect (no evasion roll) — their damage
// is applied by the spawn site at fire time, not by a flight/land pipeline.
type ProjectileDef struct {
	// ID is the stable string identifier other code references (e.g.
	// "fire_bolt"). Must match the containing directory name.
	ID string `json:"id"`
	// Kind declares whether this effect is a flying projectile or an
	// instantaneous beam. Empty ⇒ EmitterKindProjectile (back-compat: existing
	// defs that predate this field are projectiles). Validated at load.
	Kind EmitterKind `json:"kind,omitempty"`
	// DurationMs is the on-screen lifetime of a beam flash in milliseconds.
	// Ignored for projectiles. When omitted or <= 0 on a beam def the loader
	// defaults it to defaultBeamDurationMs.
	DurationMs int `json:"durationMs,omitempty"`
	// Speed is world-space travel speed in pixels/second. When omitted or
	// <= 0 the loader defaults it to defaultProjectileSpeed so a def can opt
	// into "the standard speed" by simply leaving the field out. Ignored for
	// beams (they don't travel).
	Speed float64 `json:"speed"`
	// FollowEffect is the optional id of an effect (see the effect asset
	// system) that plays continuously on the projectile while it travels.
	// Empty string = no follow effect (the projectile sprite is the visual).
	FollowEffect string `json:"followEffect,omitempty"`
	// ImpactEffect is the optional id of an effect played on the *target* unit
	// when the projectile reaches it (e.g. fire_bolt → "fizzle"). Distinct
	// from FollowEffect: follow plays on the projectile during flight, impact
	// plays on the unit it hits, at landing. Empty = no impact effect.
	ImpactEffect string `json:"impactEffect,omitempty"`

	// TODO(tuning): future per-projectile properties go here as the
	// projectile system grows — e.g. Lifetime (despawn after N seconds),
	// Piercing / PierceMaxHits, AoeRadius, gravity/arc shaping. Keep new
	// fields `omitempty` so existing defs stay valid without edits.
}

// projectileDefsByID is a package-level var so it is initialized before any
// init() that might reference projectile defs (same rationale as
// obstacleDefsByType / unitDefsByType).
var projectileDefsByID = loadProjectileDefs()

// validateProjectileDef normalizes an emitter def's defaultable fields IN PLACE
// (Kind, beam DurationMs, Speed) and returns an error for an invalid Kind. It is
// the single gate shared by the catalog loader and the editor save path, so a
// def that loads cleanly is exactly a def that saves cleanly. It does NOT check
// the id (the loader gates that against the dir name, the editor against
// projectileIDPattern).
func validateProjectileDef(def *ProjectileDef) error {
	if def.Kind == "" {
		def.Kind = EmitterKindProjectile
	}
	if def.Kind != EmitterKindProjectile && def.Kind != EmitterKindBeam {
		return fmt.Errorf("invalid kind %q — must be \"projectile\" or \"beam\"", def.Kind)
	}
	if def.Kind == EmitterKindBeam && def.DurationMs <= 0 {
		def.DurationMs = defaultBeamDurationMs
	}
	if def.Speed <= 0 {
		def.Speed = defaultProjectileSpeed
	}
	return nil
}

func loadProjectileDefs() map[string]ProjectileDef {
	// Single-level directory layout: catalog/projectiles/<id>/<id>.json.
	// The id directory name must match the JSON's "id" field; any drift
	// panics at startup so the catalog stays coherent.
	idEntries, err := fs.ReadDir(projectileDefsFS, "catalog/projectiles")
	if err != nil {
		panic("catalog/projectiles: " + err.Error())
	}
	result := make(map[string]ProjectileDef, len(idEntries))
	for _, entry := range idEntries {
		if !entry.IsDir() {
			panic("catalog/projectiles: unexpected file " + entry.Name() + " — projectiles must live at catalog/projectiles/<id>/<id>.json")
		}
		idKey := entry.Name()
		rel := "catalog/projectiles/" + idKey + "/" + idKey + ".json"
		data, err := projectileDefsFS.ReadFile(rel)
		if err != nil {
			panic(rel + ": " + err.Error())
		}
		var def ProjectileDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic(rel + ": " + err.Error())
		}
		if def.ID == "" {
			panic(rel + `: missing "id" field`)
		}
		if def.ID != idKey {
			panic(rel + ": def.ID " + def.ID + " does not match directory name " + idKey)
		}
		if err := validateProjectileDef(&def); err != nil {
			panic(rel + ": " + err.Error())
		}
		if _, dup := result[def.ID]; dup {
			panic(rel + `: duplicate projectile id "` + def.ID + `"`)
		}
		result[def.ID] = def
	}
	return result
}

// IsBeam reports whether this def is emitted as an instantaneous beam rather
// than a flying projectile. The single point spawn code branches on.
func (d ProjectileDef) IsBeam() bool { return d.Kind == EmitterKindBeam }

// getProjectileDef looks up a projectile definition by id. The bool is false
// when no projectile with that id is registered. Callers that resolve a
// projectile by id must handle the not-found case (same contract as
// getUnitDef / getObstacleDef).
func getProjectileDef(id string) (ProjectileDef, bool) {
	def, ok := projectileDefsByID[id]
	return def, ok
}

// ListProjectileDefs returns every registered projectile definition sorted by
// id (stable order for catalog APIs and tests).
func ListProjectileDefs() []ProjectileDef {
	defs := make([]ProjectileDef, 0, len(projectileDefsByID))
	for _, def := range projectileDefsByID {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}

// TargetEvasion is a target's chance to avoid a projectile entirely. Both
// fields are probabilities in [0,1]; 0 means "cannot dodge/block". This is
// the seam through which a future evasion system feeds the hit roll without
// every call site needing to know where the numbers come from.
type TargetEvasion struct {
	// DodgeChance is the probability the target sidesteps the projectile.
	DodgeChance float64
	// BlockChance is the probability the target blocks the projectile.
	BlockChance float64
}

// HasEvasion reports whether the target has any non-zero evasion. When false
// the projectile is guaranteed to hit and no RNG is consumed.
func (e TargetEvasion) HasEvasion() bool {
	return e.DodgeChance > 0 || e.BlockChance > 0
}

// baseUnitDodgeChance is the game-wide innate dodge probability every unit
// has before path and equipment contributions. Base block is 0 — block comes
// only from paths (Vanguard) and items (shields).
const baseUnitDodgeChance = 0.05

// evasionForUnit returns a unit's live evasion totals: game-wide base +
// progression-path rank contribution + equipped-item bonus, each additive.
// The combined cap is NOT applied here (attackHitsLocked clamps at roll
// time) so displayed stats stay honest.
func evasionForUnit(u *Unit) TargetEvasion {
	return TargetEvasion{
		DodgeChance: baseUnitDodgeChance + u.PathDodgeChance + u.EquipmentBonus.DodgeChance,
		BlockChance: u.PathBlockChance + u.EquipmentBonus.BlockChance,
	}
}

// evasionCapTotal is the ceiling on a unit's combined dodge+block avoidance:
// however stacked its gear, every unit is hittable at least 1-in-4. Applied
// at roll time only — stored/displayed stats stay uncapped and honest.
const evasionCapTotal = 0.75

// attackHitsLocked decides whether a basic attack connecting with a target
// lands. Hit/miss is driven entirely by the *target's* evasion: a single
// deterministic roll is taken against s.rngCombat (seeded per-match,
// isolated from every other RNG stream so this never perturbs
// perk/loot/cosmetic determinism). The roll space is partitioned block-first
// — an avoided roll below the block portion reports "block", the remainder
// "dodge" — so shields read as active in the popup. RNG is only consumed
// when the target actually has evasion; every real unit does (base dodge),
// but zero-evasion profiles (tests, effects) skip the draw.
//
// Returns (true, "") on a landed hit, (false, "block"|"dodge") on an avoid.
// Caller holds s.mu.
func (s *GameState) attackHitsLocked(ev TargetEvasion) (bool, string) {
	if !ev.HasEvasion() {
		return true, ""
	}
	avoid := ev.DodgeChance + ev.BlockChance
	if avoid > evasionCapTotal {
		avoid = evasionCapTotal
	}
	roll := s.rngCombat.Float64()
	if roll >= avoid {
		return true, ""
	}
	blockPortion := ev.BlockChance
	if blockPortion > avoid {
		blockPortion = avoid
	}
	if roll < blockPortion {
		return false, "block"
	}
	return false, "dodge"
}

// applyProjectileDamageLocked applies a projectile's damage to exactly one
// unit — the resolved target — and nothing else. It deliberately routes
// through applyUnitDamageWithSourceLocked (the core HP pipeline) rather than
// resolveAttackHitLocked, so it never triggers splash, pierce, or
// pass-through. This is the single-target contract of the projectile asset
// type. damage <= 0 or a nil target is a no-op.
//
// TODO: Future support for AoE projectiles and pierce mechanics — those will
// be opt-in via ProjectileDef fields (see the TODO on ProjectileDef) and must
// NOT change this single-target default path.
//
// Caller holds s.mu.
func (s *GameState) applyProjectileDamageLocked(attacker, target *Unit, damage int) {
	if target == nil || damage <= 0 {
		return
	}
	src := DamageSource{Kind: "projectile"}
	if attacker != nil {
		src.AttackerUnitID = attacker.ID
	}
	s.applyUnitDamageWithSourceLocked(target, damage, src)
}
