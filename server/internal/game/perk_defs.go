package game

// ═════════════════════════════════════════════════════════════════════════════
// PERK DEFINITIONS — DATA LAYER
//
// This file owns the PerkDef type and the perk catalog loaded from JSON.
// It is intentionally kept free of runtime game logic so it matches the
// same shape as effect_defs.go and projectile_defs.go.
//
// ┌─────────────────────────────────────────────────────────────────────────┐
// │  WHERE THINGS LIVE                                                      │
// │                                                                         │
// │    PERK DEFINITIONS (data, tuning, eligibility)                         │
// │      → catalog/perks/<id>/<id>.json                                     │
// │        One directory per perk id (mirrors catalog/effects). Each file   │
// │        is a single PerkDef carrying its own UnitType / Path / Rank      │
// │        eligibility fields. Adding a perk means adding a new             │
// │        catalog/perks/<newid>/<newid>.json.                              │
// │                                                                         │
// │    PATH STAT MULTIPLIERS (per rank)                                     │
// │      → catalog/units/<faction>/<unit>/paths/<path>/<path>.json          │
// │        Loaded by path_defs.go.                                          │
// │                                                                         │
// │    UNIT BASE STATS                                                      │
// │      → catalog/units/<faction>/<unit>/<unit>.json                       │
// │                                                                         │
// │    PERK RUNTIME BEHAVIOUR (effects, hooks, state)                       │
// │      → perks.go   (assignment + all seven hook functions)               │
// │                                                                         │
// │    PERK ICONS (HUD artwork)                                             │
// │      → catalog/action-icons.json  (id: "perk-<name>")                   │
// └─────────────────────────────────────────────────────────────────────────┘
//
// Eligibility fields (UnitType, Path, Rank) accept "" as a wildcard — a perk
// with an empty Path applies to every path, etc. The assignment system in
// perks.go calls eligiblePerksForUnitAtRank() (via perkPoolForRankLocked) to
// build the pool automatically, so no assignment-side code needs to change
// when new perks are added to the JSON.
// ═════════════════════════════════════════════════════════════════════════════

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"sync"
)

// Embeds the standalone perk catalog tree so this file can load perk JSONs
// from catalog/perks/<id>/<id>.json. Mirrors effect_defs.go's embed of
// catalog/effects; each perk lives in its own id-named directory whose name
// must equal the JSON's "id" field.
//
//go:embed all:catalog/perks
var perkDefsFS embed.FS

// perkIDPattern is the id gate for editor-authored perks (SavePerkDef /
// DeletePerkOverride). The embedded loader gates ids against the directory
// name instead, so this pattern is not applied there.
var perkIDPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

// embeddedPerkDefs is the standalone catalog, id-keyed, loaded once at init.
// It is the immutable baseline that rebuildPerkRegistry (perk_persistence.go)
// merges the writable overlay on top of.
var embeddedPerkDefs = loadPerkDefs()

// loadPerkDefs reads every catalog/perks/<id>/<id>.json into an id-keyed map.
// Mirrors loadEffectDefs: the directory name is authoritative for the id, and
// any structural problem panics at startup (embedded data is a build-time bug
// if malformed).
func loadPerkDefs() map[string]PerkDef {
	entries, err := fs.ReadDir(perkDefsFS, "catalog/perks")
	if err != nil {
		panic("catalog/perks: " + err.Error())
	}
	result := make(map[string]PerkDef, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		rel := "catalog/perks/" + id + "/" + id + ".json"
		data, err := perkDefsFS.ReadFile(rel)
		if err != nil {
			panic(rel + ": " + err.Error())
		}
		var def PerkDef
		if err := json.Unmarshal(data, &def); err != nil {
			panic(rel + ": " + err.Error())
		}
		if def.ID == "" {
			panic(rel + `: missing "id"`)
		}
		if def.ID != id {
			panic(rel + ": id " + def.ID + " != dir " + id)
		}
		if err := validatePerkDef(&def); err != nil {
			panic(rel + ": " + err.Error())
		}
		if _, dup := result[def.ID]; dup {
			panic(rel + ": duplicate perk id " + def.ID)
		}
		result[def.ID] = def
	}
	return result
}

// validatePerkDef is the shared load + save content gate. Does NOT check id
// (loader gates against dir name; editor against perkIDPattern).
func validatePerkDef(def *PerkDef) error {
	switch def.Rank {
	case "", unitRankBronze, unitRankSilver, unitRankGold:
	default:
		return fmt.Errorf("rank %q must be \"\" | bronze | silver | gold", def.Rank)
	}
	if def.Effect != nil {
		switch def.Effect.Target {
		case "", "self", "enemies":
		default:
			return fmt.Errorf("effect.target %q must be \"self\" | \"enemies\"", def.Effect.Target)
		}
	}
	return nil
}

// PerkEffect describes the generalized visual effect a perk triggers on proc.
// It is embedded inside PerkDef.Effect and drives queueEffectLocked via
// applyPerkEffectLocked in perks_attack.go.
//
//   - Name            — wire name matched by the client renderer (e.g. "whirlwind")
//   - Target          — "self" (anchor to attacker) or "enemies" (anchor to primary target)
//   - SizeScale       — visual scale multiplier; <= 0 defaults to 1.0
//   - DurationSeconds — on-screen lifetime; <= 0 defaults to 1.0
//   - Variant         — optional sub-variant for client art selection
type PerkEffect struct {
	Name            string  `json:"name"`
	Target          string  `json:"target"`          // "self" or "enemies"
	SizeScale       float64 `json:"sizeScale,omitempty"`
	DurationSeconds float64 `json:"durationSeconds,omitempty"`
	Variant         string  `json:"variant,omitempty"`
}

// PerkDef is the static definition of a perk loaded from the catalog.
//
// Fields:
//   - ID           — unique string key; used by runtime handlers to dispatch behaviour
//   - DisplayName  — human-readable name shown in UI
//   - Description  — one-line flavour/tooltip text
//   - UnitType     — eligible unit type, e.g. "soldier". Empty = any.
//   - Path         — eligible promotion path, e.g. "berserker". Empty = any.
//   - Rank         — eligible rank tier, e.g. "bronze". Empty = any.
//   - RequiresPerk — (optional) gate: this perk only appears in the pool when
//                    the unit already owns the named perk. Empty = no gate.
//                    Useful for Silver/Gold perks that only make sense alongside
//                    a specific Bronze perk (e.g. explosive_chain requires
//                    explosive_trap). Set in the JSON as "requiresPerk".
//   - Config       — perk-specific tuning values. Keys and their meanings are
//                    documented in the JSON file alongside each perk entry.
//   - Effect       — optional visual effect to queue on perk proc (see PerkEffect).
type PerkDef struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
	// TooltipTemplate is a client-interpolated string for the tooltip. Keys in
	// curly braces are replaced with live values from the perk's config (or
	// effectiveTrap payload for trapper bronze perks). Supported token forms:
	//   {key}      — raw number; integer if whole, else 1 decimal
	//   {key%}     — value×100 as integer percent (0.2 → "20%")
	//   {key+%}    — delta percent: (value−1)×100, signed (1.25 → "+25%")
	//   {key:N}    — force N decimal places
	//   {trap.key} — read from effectiveTrap payload (trapper bronze only)
	// Omitted for perks where description alone is sufficient.
	TooltipTemplate string `json:"tooltipTemplate,omitempty"`
	// TooltipTemplateByTrap lets trapper perks that describe multiple trap
	// variants (e.g. ascendant_infusion, overload_protocol) show only the branch
	// matching the unit's owned Bronze trap perk. Keys are bronze trap perk ids
	// ("caltrops", "fire_pit", "explosive_trap", "marker_trap"); the client
	// picks the entry matching unit.effectiveTrap.perkId. Takes precedence over
	// TooltipTemplate when both are present and the unit has an effective trap.
	TooltipTemplateByTrap map[string]string `json:"tooltipTemplateByTrap,omitempty"`
	// TooltipTemplateByOwnedPerk is the generic equivalent of
	// TooltipTemplateByTrap for adaptive perks whose effect varies with the
	// unit's other perk picks (e.g. Siphoner ascended_corruption, whose
	// behaviour mirrors whichever Silver perk the unit owns). Keys are perk
	// ids; the client iterates unit.PerkIDs in slot order and picks the
	// first key that the unit owns. Takes precedence over TooltipTemplate
	// when a match is found, so the tooltip only shows the relevant
	// branch instead of dumping every variant.
	TooltipTemplateByOwnedPerk map[string]string `json:"tooltipTemplateByOwnedPerk,omitempty"`
	// Icon is the action-icon ID used to render this perk in the HUD.
	// Matches an entry in catalog/action-icons.json ("perk-<name>").
	Icon         string             `json:"icon,omitempty"`
	UnitType     string             `json:"unitType,omitempty"`
	Path         string             `json:"path,omitempty"`
	Rank         string             `json:"rank,omitempty"`
	RequiresPerk string             `json:"requiresPerk,omitempty"`
	Config       map[string]float64 `json:"config"`
	// ConfigByRank holds optional per-rank overrides keyed by the owning
	// unit's CURRENT rank ("bronze" / "silver" / "gold"). When a unit reads
	// this perk's config, values in ConfigByRank[unit.Rank] shadow the
	// matching keys in Config — everything else falls through to the base.
	// Callers must go through ConfigForRank to get a merged view.
	ConfigByRank map[string]map[string]float64 `json:"configByRank,omitempty"`
	// Effect is the optional visual effect triggered on perk proc. Nil when
	// the perk has no generalized visual effect (most perks). Populated from
	// the "effect" key in the catalog JSON.
	Effect *PerkEffect `json:"effect,omitempty"`
	// GrantsAbilities lists ability ids that should be appended to the
	// unit's Abilities slice when this perk is owned. Empty / nil for the
	// vast majority of perks. Used by ability-granting perks (Siphoner
	// bronze: lingering_hex / mark_of_weakness) so a Siphoner with the
	// corresponding Bronze pick gains a new castable on their action bar.
	// The grant is applied in assignUnitPathAbilitiesLocked (step 4) and
	// is idempotent — duplicate ids are filtered. Removing the perk would
	// strip the ability; we don't currently support perk removal, so this
	// is unidirectional.
	GrantsAbilities []string `json:"grantsAbilities,omitempty"`
	// Wired reports whether a Go handler exists for this perk's id (spec
	// §7.3) — see perk_wired.go's wiredPerkIDs for exactly what counts. It
	// is a derived, presentation-only field: it is NEVER set on the
	// registry's own *PerkDef values (perkDefsByID / perkDefLookup /
	// snapshotPerkDefs all leave it at its zero value, false). ListPerkDefs
	// is the ONLY place that populates it, on the per-def COPY it returns —
	// the shape the /catalog/perks HTTP endpoint and the future editor UI
	// consume, so an editor-authored perk with no matching handler can be
	// labeled "inert" instead of silently doing nothing in a match.
	Wired bool `json:"wired"`
}

// ConfigForRank returns the effective config map for a perk at a given rank.
// Base Config is used as the default; any keys present in ConfigByRank[rank]
// overwrite the base. Missing rank (or empty override) returns base verbatim,
// avoiding allocation in the common path.
//
// Safe to call on a nil PerkDef (returns nil). Safe with an empty rank string
// (returns the base Config unchanged).
func (def *PerkDef) ConfigForRank(rank string) map[string]float64 {
	if def == nil {
		return nil
	}
	override, ok := def.ConfigByRank[rank]
	if !ok || len(override) == 0 {
		return def.Config
	}
	merged := make(map[string]float64, len(def.Config)+len(override))
	for k, v := range def.Config {
		merged[k] = v
	}
	for k, v := range override {
		merged[k] = v
	}
	return merged
}

// perkDefsByID is the in-memory index populated from the perk catalog at startup.
// The hierarchy on disk is flattened here so all callers work against a
// simple id→def map.
//
// perkDefsMu guards perkDefsByID. init() (perk_persistence.go) populates it
// single-threaded before any goroutine exists (same exemption documented for
// path_defs.go's pathCatalogMu). Every read — i.e. everything reachable after
// startup, including the tick-loop rank-up path (eligiblePerksForUnitAtRank) —
// MUST go through perkDefLookup / snapshotPerkDefs rather than touching
// perkDefsByID directly. This is what lets a runtime rebuild
// (perk_persistence.go's rebuildPerkRegistry) swap the whole map safely while
// readers are still using it.
//
// Returned *PerkDef pointers are READ-ONLY as far as any caller is
// concerned: a rebuild always builds entirely NEW *PerkDef values into a
// fresh map before swapping, never mutates a def a reader might already be
// holding.
var perkDefsMu sync.RWMutex
var perkDefsByID map[string]*PerkDef

// perkDefLookup is the synchronized read path for perkDefsByID.
func perkDefLookup(id string) (*PerkDef, bool) {
	perkDefsMu.RLock()
	defer perkDefsMu.RUnlock()
	def, ok := perkDefsByID[id]
	return def, ok
}

// snapshotPerkDefs returns a slice copy of every def currently in
// perkDefsByID — for callers that need to iterate the whole catalog
// (eligiblePerksForUnitAtRank, ListPerkDefs). The slice itself is a fresh
// allocation (safe to sort/filter without racing a concurrent rebuild); the
// *PerkDef values it holds are shared, read-only pointers (see the
// perkDefsByID doc comment above).
func snapshotPerkDefs() []*PerkDef {
	perkDefsMu.RLock()
	defer perkDefsMu.RUnlock()
	out := make([]*PerkDef, 0, len(perkDefsByID))
	for _, def := range perkDefsByID {
		out = append(out, def)
	}
	return out
}

// perkDefByID looks up a perk definition by its ID.
// Returns nil if not found.
func perkDefByID(id string) *PerkDef {
	def, _ := perkDefLookup(id)
	return def
}

// ─────────────────────────────────────────────────────────────────────────────
// EXTENSION POINT — PERK POOL FILTER
//
// eligiblePerksForUnitAtRank returns every perk in the catalog whose
// eligibility fields match the unit's UnitType, ProgressionPath and the
// given rank. An empty field in the definition matches any value (wildcard).
//
// This is the sole filter used by the assignment pipeline (via
// perkPoolForRankLocked in perks.go). Adding a new perk to catalog/perks is
// sufficient to include it in the eligible pool — no code changes needed here
// or in the assignment function.
//
// To restrict a perk to multiple paths or ranks, add multiple PerkDef entries
// sharing the same ID — or extend this function with set-based eligibility —
// but keep it as the single place that defines "what qualifies".
// ─────────────────────────────────────────────────────────────────────────────
func eligiblePerksForUnitAtRank(unit *Unit, rank string) []*PerkDef {
	var eligible []*PerkDef
	seen := map[string]struct{}{}
	for _, def := range snapshotPerkDefs() {
		if def.UnitType != "" && def.UnitType != unit.UnitType {
			continue
		}
		if def.Path != "" && def.Path != unit.ProgressionPath {
			continue
		}
		if def.Rank != "" && def.Rank != rank {
			continue
		}
		eligible = append(eligible, def)
		seen[def.ID] = struct{}{}
	}
	// Union in the path's explicit per-rank perk references (SP2). A referenced
	// perk that already matched via eligibility is not added twice (dedup via
	// seen). Unknown ids resolve fail-safe (skipped). The ID-sort below keeps
	// rngPerks.Intn deterministic regardless of insertion order.
	for _, perkID := range pathPerkRefsForRank(unit.ProgressionPath, rank) {
		if _, dup := seen[perkID]; dup {
			continue
		}
		if def, ok := perkDefLookup(perkID); ok {
			eligible = append(eligible, def)
			seen[perkID] = struct{}{}
		}
	}
	// Sort by ID before returning so that rngPerks.Intn picks from a
	// deterministic order regardless of map iteration order. Without this sort,
	// two GameState instances with the same seed can produce different perks
	// because Go randomises map iteration order per process, violating the
	// replay-reproducibility invariant required by AI_RULES.md.
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].ID < eligible[j].ID })
	return eligible
}

// ListPerkDefs returns all perk definitions sorted by ID.
// Used by the /catalog/perks HTTP endpoint (mirrors ListUnitDefs / ListBuildingDefs).
func ListPerkDefs() []PerkDef {
	snapshot := snapshotPerkDefs()
	defs := make([]PerkDef, 0, len(snapshot))
	for _, def := range snapshot {
		cp := *def
		cp.Wired = isWiredPerk(cp.ID)
		defs = append(defs, cp)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}
