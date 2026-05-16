package game

import (
	"embed"
	"encoding/json"
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

// ProjectileDef is the authoritative, server-owned definition of a projectile
// type. The server uses it for flight speed and (future) flight behavior; the
// client owns the visuals separately via a mirrored sprites.json under
// client/.../assets/projectiles/<id>/ (same split as units, whose UnitDef
// carries no sprite paths).
//
// Hit/miss and damage rules for projectiles are NOT data on this struct —
// they are fixed behavior (see projectileHitsLocked / applyProjectileDamageLocked):
//   - A projectile hits or misses based on the *target's* dodge/block stats
//     if it has any; a target with no evasion stats is always hit.
//   - On hit it damages only the resolved target — no AoE, no pass-through.
type ProjectileDef struct {
	// ID is the stable string identifier other code references (e.g.
	// "fire_bolt"). Must match the containing directory name.
	ID string `json:"id"`
	// Speed is world-space travel speed in pixels/second. When omitted or
	// <= 0 the loader defaults it to defaultProjectileSpeed so a def can opt
	// into "the standard speed" by simply leaving the field out.
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
		if def.Speed <= 0 {
			def.Speed = defaultProjectileSpeed
		}
		if _, dup := result[def.ID]; dup {
			panic(rel + `: duplicate projectile id "` + def.ID + `"`)
		}
		result[def.ID] = def
	}
	return result
}

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

// evasionForUnit returns the evasion profile for a unit. No unit type defines
// dodge or block today, so this is always the zero value (always-hit). It is
// the single place a real evasion system would be wired in.
//
// TODO: source real dodge/block from UnitDef / perks / equipment when an
// evasion system is added. Keep the always-hit default for units that opt
// out so existing combat math is unchanged.
func evasionForUnit(u *Unit) TargetEvasion {
	_ = u
	return TargetEvasion{}
}

// projectileHitsLocked decides whether a projectile that reaches its target
// connects. Hit/miss is driven entirely by the *target's* evasion: a target
// with no dodge/block always gets hit; otherwise a single deterministic roll
// is taken against s.rngCombat (seeded per-match, isolated from every other
// RNG stream so this never perturbs perk/loot/cosmetic determinism). RNG is
// only consumed when the target actually has evasion, so today — when no
// unit defines dodge/block — no combat roll is ever taken.
//
// Caller holds s.mu.
func (s *GameState) projectileHitsLocked(ev TargetEvasion) bool {
	if !ev.HasEvasion() {
		return true
	}
	avoid := ev.DodgeChance + ev.BlockChance
	if avoid >= 1.0 {
		return false
	}
	return s.rngCombat.Float64() >= avoid
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
