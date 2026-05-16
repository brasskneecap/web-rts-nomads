package game

import (
	"embed"
	"encoding/json"
	"io/fs"
	"sort"
)

// Embeds the per-effect catalog tree. Layout mirrors the projectile catalog
// (one directory per id) so an effect can grow sibling files later (sprite
// manifests, particle configs) without reshuffling the tree:
//
//	catalog/effects/<id>/<id>.json   — EffectDef for that effect
//
// Adding a new effect: create catalog/effects/<newid>/<newid>.json. The
// directory name must match the JSON's `id` field; mismatch panics at startup
// (same discipline as projectile_defs.go / unit_defs.go).
//
//go:embed catalog/effects
var effectDefsFS embed.FS

// EffectAnchor is where a played effect renders relative to its bound target.
// It is a small closed set (mirrors the DamageType / TargetClass idiom):
// authoring an unrecognised anchor panics at catalog load so typos are caught
// immediately. The empty value means "unspecified" and resolves to center.
//
// NOTE: the current transient-effect renderer anchors at the unit's origin
// and applies a per-sprite-sheet offsetY (client effects/<id>/sprites.json);
// it does not yet consume this anchor. Anchor is therefore authored asset
// metadata (the asset-type property the spec requires) and the seam for a
// future render-offset mapping. TODO: map EffectAnchor → render offset client
// side.
type EffectAnchor string

const (
	EffectAnchorCenter EffectAnchor = "center"
	EffectAnchorFeet   EffectAnchor = "feet"
	EffectAnchorHead   EffectAnchor = "head"
)

// OrCenter resolves an unspecified (empty) anchor to the default, center.
func (a EffectAnchor) OrCenter() EffectAnchor {
	if a == "" {
		return EffectAnchorCenter
	}
	return a
}

func isValidEffectAnchor(a EffectAnchor) bool {
	switch a {
	case EffectAnchorCenter, EffectAnchorFeet, EffectAnchorHead:
		return true
	default:
		return false
	}
}

// EffectDef is the authoritative, server-owned definition of a visual overlay
// effect (heal glow, impact fizzle, buff aura, …). It is parallel to
// ProjectileDef: the server owns the id / duration / anchor contract; the
// client owns the actual sprite sheet separately under
// assets/effects/<id>/{sprites.json,sheet.png} (same server/client split as
// units and projectiles).
//
// Effects are purely cosmetic — no gameplay logic ever branches on one. They
// are *played* through the shared transient-effect pipeline
// (queueEffectLocked → EffectSnapshot → client effectSprites.ts), the same
// path perks use, so a played effect actually renders. An effect is played:
//   - on a unit (heal target, impact reaction) via playEffectOnUnitLocked,
//   - as a projectile's impact effect (ProjectileDef.ImpactEffect, played on
//     the unit a projectile reaches), or
//   - carried by a projectile in flight (ProjectileDef.FollowEffect, Part 1).
type EffectDef struct {
	// ID is the stable string identifier other code references (e.g.
	// "healing_glow", "fizzle"). Must match the containing directory name and
	// the client assets/effects/<id>/ folder name (the wire Name the client
	// renderer keys on).
	ID string `json:"id"`
	// SpritePath is an optional logical pointer to the art. The client renders
	// from its own assets/effects/<id>/sprites.json manifest, so this is not
	// required and may be empty — kept for parity with the asset-type spec and
	// future server-side tooling.
	SpritePath string `json:"spritePath,omitempty"`
	// Duration is how long the effect plays, in seconds — passed straight to
	// queueEffectLocked, which drives the client animation timeline via
	// Progress (0→1 over this span). 0 currently falls back to the pipeline's
	// 1.0s default (the transient pipeline has no indefinite/looping mode).
	// Negative is invalid and panics at load.
	Duration float64 `json:"duration"`
	// Anchor is authored render placement metadata (see EffectAnchor note).
	// Empty resolves to center; any non-empty value must be a known anchor.
	Anchor EffectAnchor `json:"anchor,omitempty"`

	// TODO(tuning): additional properties as the effect system grows —
	// loop behavior, particle config, color tint, scale, sound hook,
	// indefinite/bound-looping support. Keep new fields `omitempty`.
}

// effectDefsByID is a package-level var so it is initialized before any
// init() that might reference effect defs (same rationale as
// projectileDefsByID / unitDefsByType).
var effectDefsByID = loadEffectDefs()

func loadEffectDefs() map[string]EffectDef {
	idEntries, err := fs.ReadDir(effectDefsFS, "catalog/effects")
	if err != nil {
		panic("catalog/effects: " + err.Error())
	}
	result := make(map[string]EffectDef, len(idEntries))
	for _, entry := range idEntries {
		if !entry.IsDir() {
			panic("catalog/effects: unexpected file " + entry.Name() + " — effects must live at catalog/effects/<id>/<id>.json")
		}
		idKey := entry.Name()
		rel := "catalog/effects/" + idKey + "/" + idKey + ".json"
		data, err := effectDefsFS.ReadFile(rel)
		if err != nil {
			panic(rel + ": " + err.Error())
		}
		var def EffectDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic(rel + ": " + err.Error())
		}
		if def.ID == "" {
			panic(rel + `: missing "id" field`)
		}
		if def.ID != idKey {
			panic(rel + ": def.ID " + def.ID + " does not match directory name " + idKey)
		}
		if def.Duration < 0 {
			panic(rel + `: duration must be >= 0`)
		}
		if def.Anchor != "" && !isValidEffectAnchor(def.Anchor) {
			panic(rel + `: anchor "` + string(def.Anchor) + `" must be one of "center" | "feet" | "head"`)
		}
		if _, dup := result[def.ID]; dup {
			panic(rel + `: duplicate effect id "` + def.ID + `"`)
		}
		result[def.ID] = def
	}
	return result
}

// getEffectDef looks up an effect definition by id. The bool is false when no
// effect with that id is registered (same contract as getProjectileDef).
func getEffectDef(id string) (EffectDef, bool) {
	def, ok := effectDefsByID[id]
	return def, ok
}

// ListEffectDefs returns every registered effect definition sorted by id.
func ListEffectDefs() []EffectDef {
	defs := make([]EffectDef, 0, len(effectDefsByID))
	for _, def := range effectDefsByID {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}

// playEffectOnUnitLocked plays a registered effect on a unit through the
// shared transient-effect pipeline (queueEffectLocked → EffectSnapshot), so
// it renders via the exact same path perks use. Returns false and plays
// nothing for a nil unit or an unregistered effect id — callers that care
// about feedback can react, but it is always safe to ignore the result.
//
// The effect's def Duration is used as the play duration; queueEffectLocked
// treats a non-positive duration as its 1.0s default (the transient pipeline
// has no indefinite/looping mode — see the EffectDef.Duration TODO).
//
// Caller holds s.mu.
func (s *GameState) playEffectOnUnitLocked(unit *Unit, effectID string) bool {
	if unit == nil {
		return false
	}
	def, ok := getEffectDef(effectID)
	if !ok {
		return false
	}
	s.queueEffectLocked(def.ID, unit.ID, unit.X, unit.Y, 1.0 /*sizeScale*/, def.Duration, "" /*variant*/)
	// queueEffectLocked has no anchor parameter (perks don't need one); set it
	// on the instance just queued. Empty/center is the default everywhere
	// else, so this is the only producer of a non-center anchor.
	if n := len(s.activeEffects); n > 0 {
		s.activeEffects[n-1].Anchor = def.Anchor.OrCenter()
	}
	return true
}

// followEffectForProjectileDef returns the validated follow-effect id a
// projectile spawned from def should carry while in flight, or "" when the
// def specifies none or names an unregistered effect. Fail-safe by design: a
// bad id degrades to "no follow effect" rather than crashing. Seam used by
// Part 7's def-based projectile spawning.
func followEffectForProjectileDef(def ProjectileDef) string {
	if def.FollowEffect == "" {
		return ""
	}
	if _, ok := getEffectDef(def.FollowEffect); !ok {
		return ""
	}
	return def.FollowEffect
}

// impactEffectForProjectileDef returns the validated impact-effect id a
// projectile spawned from def plays on the unit it reaches, or "" when the
// def specifies none or names an unregistered effect. Mirrors
// followEffectForProjectileDef (same fail-safe contract). Distinct concept:
// FollowEffect plays on the projectile during flight; ImpactEffect plays on
// the *target* when the projectile lands.
func impactEffectForProjectileDef(def ProjectileDef) string {
	if def.ImpactEffect == "" {
		return ""
	}
	if _, ok := getEffectDef(def.ImpactEffect); !ok {
		return ""
	}
	return def.ImpactEffect
}

// playProjectileImpactLocked plays a projectile's impact effect on the unit
// it reached, through the shared transient-effect pipeline. No-op when the
// projectile carries no impact effect, names an unregistered one, or the
// target is gone. Called from landProjectileLocked; existing procedural
// projectiles carry no ImpactEffect so they are unaffected.
//
// Caller holds s.mu.
func (s *GameState) playProjectileImpactLocked(proj *Projectile, target *Unit) {
	if proj == nil || target == nil || proj.ImpactEffect == "" {
		return
	}
	s.playEffectOnUnitLocked(target, proj.ImpactEffect)
}
