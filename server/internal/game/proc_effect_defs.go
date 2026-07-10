package game

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// Embeds the proc-effect catalog. Flat layout — proc effects carry no client
// assets of their own (visuals come from the projectile/beam def they name),
// so unlike projectiles there is no per-id directory:
//
//	catalog/procs/<id>.json — ProcEffectDef for that effect
//
// The filename (minus .json) must match the JSON's `id` field; mismatch panics
// at startup so the catalog stays coherent (same discipline as
// projectile_defs.go / unit_defs.go).
//
//go:embed catalog/procs
var procEffectDefsFS embed.FS

// ProcEffectParams is the full runtime payload of a proc effect — everything
// executeProcEffectLocked needs to fire it at a target. Authored on a
// ProcEffectDef (with per-reference overrides applied by
// resolveProcEffectParams), but a plain value struct so future systems
// (ability upgrades, traps) can construct or transform one programmatically.
type ProcEffectParams struct {
	// Damage / DamageType: the typed damage instance the effect lands.
	Damage     int        `json:"damage"`
	DamageType DamageType `json:"damageType"`
	// ProjectileID names the emitter def (catalog/projectiles) that carries
	// the effect: a projectile-kind def flies a homing bolt, a beam-kind def
	// zaps instantly with deferred damage.
	ProjectileID string `json:"projectileID"`
	// ProjectileScale is a render-size multiplier for the fired bolt's
	// sprite. 0 ⇒ fall back to the firing unit's ProjectileScale (non-unit
	// sources render at client default 1×).
	ProjectileScale float64 `json:"projectileScale,omitempty"`
	// Bounce / chain (beam emitters only): the effect arcs to up to
	// BounceCount further enemies, each hop leaping off the PREVIOUS victim
	// to the nearest not-yet-hit hostile within BounceRange, losing
	// BounceDamageFalloff damage per hop.
	BounceCount         int     `json:"bounceCount,omitempty"`
	BounceRange         float64 `json:"bounceRange,omitempty"`
	BounceDamageFalloff int     `json:"bounceDamageFalloff,omitempty"`
	// On-hit slow (chill): scales the hit unit's attack + move speed by
	// SlowMultiplier for SlowDurationSeconds via the shared slow system.
	// Zero ⇒ no slow.
	SlowMultiplier      float64 `json:"slowMultiplier,omitempty"`
	SlowDurationSeconds float64 `json:"slowDurationSeconds,omitempty"`
	// On-hit burn (fire DoT): ignites the hit unit for BurnDamagePerSecond
	// over BurnDurationSeconds via the shared burn system. Zero ⇒ no burn.
	BurnDamagePerSecond float64 `json:"burnDamagePerSecond,omitempty"`
	BurnDurationSeconds float64 `json:"burnDurationSeconds,omitempty"`
}

// ProcEffectDef is one named, reusable proc effect in catalog/procs. The ID
// is what items (and future perks/abilities/traps) reference; the embedded
// params are the effect's authored payload. DamageType and ProjectileID are
// the effect's IDENTITY — references may override the other knobs (see
// ProcEffectOverrides) but never these two.
type ProcEffectDef struct {
	ID string `json:"id"`
	ProcEffectParams
}

// procEffectDefsByID is a package-level var so Go's dependency-ordered var
// initialization guarantees it is ready before any other var initializer that
// references it — specifically loadItemCatalog, whose validation resolves
// item onHitProc.effect references via getProcEffectDef (same trick
// itemCatalogSingleton uses for the loot-table loader).
var procEffectDefsByID = loadProcEffectDefs()

func loadProcEffectDefs() map[string]ProcEffectDef {
	entries, err := fs.ReadDir(procEffectDefsFS, "catalog/procs")
	if err != nil {
		panic("catalog/procs: " + err.Error())
	}
	result := make(map[string]ProcEffectDef, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			panic("catalog/procs: unexpected entry " + entry.Name() + " — proc effects must live at catalog/procs/<id>.json")
		}
		idKey := strings.TrimSuffix(entry.Name(), ".json")
		rel := "catalog/procs/" + entry.Name()
		data, err := procEffectDefsFS.ReadFile(rel)
		if err != nil {
			panic(rel + ": " + err.Error())
		}
		var def ProcEffectDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic(rel + ": " + err.Error())
		}
		if def.ID == "" {
			panic(rel + `: missing "id" field`)
		}
		if def.ID != idKey {
			panic(rel + ": def.ID " + def.ID + " does not match filename " + idKey)
		}
		if err := validateProcEffectDef(&def); err != nil {
			panic(rel + ": " + err.Error())
		}
		if _, dup := result[def.ID]; dup {
			panic(rel + `: duplicate proc effect id "` + def.ID + `"`)
		}
		result[def.ID] = def
	}
	return result
}

// validateProcEffectDef checks a proc effect's authored payload. An effect
// with no damage, an unregistered element, or no emitter is a content
// authoring error caught at startup.
func validateProcEffectDef(def *ProcEffectDef) error {
	if def.Damage <= 0 {
		return fmt.Errorf("proc effect %q damage %d must be > 0", def.ID, def.Damage)
	}
	if !IsValidDamageType(def.DamageType) {
		return fmt.Errorf("proc effect %q damageType: unregistered damage type %q", def.ID, def.DamageType)
	}
	if def.ProjectileID == "" {
		return fmt.Errorf("proc effect %q projectileID is required (names the emitter def)", def.ID)
	}
	if def.ProjectileScale < 0 {
		return fmt.Errorf("proc effect %q projectileScale %v must be >= 0 (0/omitted ⇒ fall back to the firing unit's scale)", def.ID, def.ProjectileScale)
	}
	return nil
}

// getProcEffectDef looks up a proc effect definition by id. The bool is false
// when no effect with that id is registered — callers must handle it (same
// contract as getProjectileDef / getItemDef).
func getProcEffectDef(id string) (ProcEffectDef, bool) {
	def, ok := procEffectDefsByID[id]
	return def, ok
}

// ListProcEffectDefs returns every registered proc effect sorted by id —
// consumed by the /catalog/procs route for the item editor's effect picker.
func ListProcEffectDefs() []ProcEffectDef {
	defs := make([]ProcEffectDef, 0, len(procEffectDefsByID))
	for _, def := range procEffectDefsByID {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}
