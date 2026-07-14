package game

// ═════════════════════════════════════════════════════════════════════════════
// PERK DEFINITIONS — DATA LAYER
//
// This file owns the PerkDef type and the perk catalog loaded from JSON.
// It is intentionally kept free of runtime game logic so it matches the
// same shape as unit_defs.go and building_defs.go.
//
// ┌─────────────────────────────────────────────────────────────────────────┐
// │  WHERE THINGS LIVE                                                      │
// │                                                                         │
// │    PERK DEFINITIONS (data, tuning, eligibility)                         │
// │      → catalog/units/<faction>/<unit>/paths/<path>/perks/<rank>.json    │
// │        Each file holds the array of perk entries for that slot.         │
// │        Adding a perk means appending to the right file;                 │
// │        UnitType / Path / Rank are inferred from the file path.          │
// │                                                                         │
// │    PATH STAT MULTIPLIERS (per rank)                                     │
// │      → catalog/units/<faction>/<unit>/paths/<path>/<path>.json          │
// │        Sibling of perks/; loaded by path_defs.go.                       │
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
	"path"
	"sort"
	"strings"
	"sync"
)

// Embeds the per-unit catalog tree so this file can load perk JSONs from
// catalog/units/<faction>/<unit>/paths/<path>/perks/*.json. unit_defs.go and
// path_defs.go embed the same tree; each init() filters independently.
//
//go:embed all:catalog/units
var perkDefsFS embed.FS

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
// perkDefsMu guards perkDefsByID. init() populates it single-threaded before
// any goroutine exists (same exemption documented for path_defs.go's
// pathCatalogMu), so init's own write bypasses the lock. Every OTHER read —
// i.e. everything reachable after startup, including the tick-loop rank-up
// path (eligiblePerksForUnitAtRank) — MUST go through perkDefLookup /
// snapshotPerkDefs rather than touching perkDefsByID directly. This is what
// lets a future runtime rebuild (perk_persistence.go's rebuildPerkRegistry)
// swap the whole map safely while readers are still using it.
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

// perkEntryJSON is the shape of a single perk entry in a per-rank JSON file.
// It carries only the perk-specific fields; UnitType, Path, and Rank are
// injected from the file path during parsing.
//
// Config is decoded lazily as RawMessage so the loader can distinguish
// scalar tuning keys (e.g. "radius": 60) from per-rank override blocks
// (e.g. "silver": { "radius": 80 }). See splitRankConfig.
type perkEntryJSON struct {
	ID                    string                     `json:"id"`
	DisplayName           string                     `json:"displayName"`
	Description           string                     `json:"description,omitempty"`
	TooltipTemplate       string                     `json:"tooltipTemplate,omitempty"`
	TooltipTemplateByTrap      map[string]string          `json:"tooltipTemplateByTrap,omitempty"`
	TooltipTemplateByOwnedPerk map[string]string          `json:"tooltipTemplateByOwnedPerk,omitempty"`
	Icon                  string                     `json:"icon,omitempty"`
	RequiresPerk          string                     `json:"requiresPerk,omitempty"`
	Config                map[string]json.RawMessage `json:"config"`
	Effect                *PerkEffect                `json:"effect,omitempty"`
	GrantsAbilities       []string                   `json:"grantsAbilities,omitempty"`
}

// perkRankOverrideKeys enumerates the JSON keys inside `config` that are
// treated as per-rank override blocks rather than tuning scalars. Matches the
// rank constants in progression.go.
var perkRankOverrideKeys = map[string]struct{}{
	unitRankBronze: {},
	unitRankSilver: {},
	unitRankGold:   {},
}

// splitRankConfig partitions the raw config map into (baseConfig, rankOverrides).
// Scalar number keys go into baseConfig. Keys matching a known rank are decoded
// as nested {string: float64} maps. Any other shape is a JSON error and is
// surfaced by the caller (which panics — catalog data is embedded, so malformed
// JSON is a build-time bug).
func splitRankConfig(raw map[string]json.RawMessage) (map[string]float64, map[string]map[string]float64, error) {
	if len(raw) == 0 {
		return nil, nil, nil
	}
	base := make(map[string]float64, len(raw))
	var overrides map[string]map[string]float64
	for k, v := range raw {
		if _, isRank := perkRankOverrideKeys[k]; isRank {
			var nested map[string]float64
			if err := json.Unmarshal(v, &nested); err != nil {
				return nil, nil, err
			}
			if len(nested) == 0 {
				continue
			}
			if overrides == nil {
				overrides = make(map[string]map[string]float64, 3)
			}
			overrides[k] = nested
			continue
		}
		var f float64
		if err := json.Unmarshal(v, &f); err != nil {
			return nil, nil, err
		}
		base[k] = f
	}
	return base, overrides, nil
}

// perkPoolDirName is the fixed subdirectory name holding perk rank files
// under a path directory (catalog/units/<faction>/<unit>/paths/<path>/perks/
// <rank>.json). Named so the init() walk and perk_persistence.go's directory
// matching never drift on the literal.
const perkPoolDirName = "perks"

// perkPoolKey identifies one rank's perk pool — the unit/path granularity a
// perks/<rank>.json file is authored at. Used to key both
// embeddedPerkPools (path_defs.go's init(), below) and runtimePerkPools
// (perk_persistence.go's writable overlay). unitType/pathName/rank are all
// constrained to unitIDPattern (no "/"), so joining with "/" round-trips
// unambiguously via splitPerkPoolKey.
func perkPoolKey(unitType, pathName, rank string) string {
	return unitType + "/" + pathName + "/" + rank
}

// splitPerkPoolKey reverses perkPoolKey. ok is false if key isn't a 3-part
// "/"-joined string (defensive — every key this package itself produces via
// perkPoolKey always round-trips).
func splitPerkPoolKey(key string) (unitType, pathName, rank string, ok bool) {
	parts := strings.Split(key, "/")
	if len(parts) != 3 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// buildPerkDefsFromPool converts one rank's raw perk entries into PerkDef
// values, injecting UnitType/Path/Rank (never present in the source JSON —
// see perkEntryJSON). Shared by init() (which panics on error — embedded
// catalog data is a build-time bug if malformed) and the runtime overlay
// path (perk_persistence.go's rebuildPerkRegistry, which must skip a bad
// pool rather than crash a running server). Returning the error rather than
// panicking here is what makes that reuse possible; init() decides whether
// to panic.
func buildPerkDefsFromPool(unitType, pathName, rank string, entries []perkEntryJSON) ([]*PerkDef, error) {
	out := make([]*PerkDef, 0, len(entries))
	for _, entry := range entries {
		base, overrides, err := splitRankConfig(entry.Config)
		if err != nil {
			return nil, fmt.Errorf("[%s].config: %w", entry.ID, err)
		}
		out = append(out, &PerkDef{
			ID:                         entry.ID,
			DisplayName:                entry.DisplayName,
			Description:                entry.Description,
			TooltipTemplate:            entry.TooltipTemplate,
			TooltipTemplateByTrap:      entry.TooltipTemplateByTrap,
			TooltipTemplateByOwnedPerk: entry.TooltipTemplateByOwnedPerk,
			Icon:                       entry.Icon,
			UnitType:                   unitType,
			Path:                       pathName,
			Rank:                       rank,
			RequiresPerk:               entry.RequiresPerk,
			Config:                     base,
			ConfigByRank:               overrides,
			Effect:                     entry.Effect,
			GrantsAbilities:            entry.GrantsAbilities,
		})
	}
	return out, nil
}

// clonePerkEntries returns a deep-enough copy of a rank's perk entry array:
// every field holding map/slice/pointer state (the tooltip-by-* maps,
// Config's RawMessage values, Effect, GrantsAbilities) gets its own backing
// storage, so neither the embedded baseline snapshot (embeddedPerkPools) nor
// a stored overlay pool (runtimePerkPools) can be mutated by a caller still
// holding the original slice. Mirrors clonePathCatalogFile (path_defs.go).
func clonePerkEntries(entries []perkEntryJSON) []perkEntryJSON {
	if entries == nil {
		return nil
	}
	out := make([]perkEntryJSON, len(entries))
	for i, e := range entries {
		cp := e
		if e.TooltipTemplateByTrap != nil {
			m := make(map[string]string, len(e.TooltipTemplateByTrap))
			for k, v := range e.TooltipTemplateByTrap {
				m[k] = v
			}
			cp.TooltipTemplateByTrap = m
		}
		if e.TooltipTemplateByOwnedPerk != nil {
			m := make(map[string]string, len(e.TooltipTemplateByOwnedPerk))
			for k, v := range e.TooltipTemplateByOwnedPerk {
				m[k] = v
			}
			cp.TooltipTemplateByOwnedPerk = m
		}
		if e.Config != nil {
			m := make(map[string]json.RawMessage, len(e.Config))
			for k, v := range e.Config {
				m[k] = append(json.RawMessage(nil), v...)
			}
			cp.Config = m
		}
		if e.Effect != nil {
			eff := *e.Effect
			cp.Effect = &eff
		}
		if e.GrantsAbilities != nil {
			cp.GrantsAbilities = append([]string(nil), e.GrantsAbilities...)
		}
		out[i] = cp
	}
	return out
}

// embeddedPerkPools snapshots the embedded catalog (perkPoolKey -> the
// rank's raw perk entries) so a runtime rebuild (rebuildPerkRegistry in
// perk_persistence.go) can regenerate perkDefsByID by merging this baseline
// with the writable overlay, without re-walking the embed FS. Populated
// once by init() below via clonePerkEntries (deep copy — nothing can mutate
// the baseline afterward); read-only for the rest of the process's life, so
// reads elsewhere in the package do not take a lock — same convention as
// embeddedPathFiles.
var embeddedPerkPools = map[string][]perkEntryJSON{}

func init() {
	// On-disk layout:
	//   catalog/units/<faction>/<unit>/paths/<path>/perks/<rank>.json
	//     → [perkEntry, perkEntry, ...]
	//
	// The walker accepts only files matching this shape exactly; anything
	// else is a structural mistake and panics so it fails loud at startup.
	// unitType, path, and rank are derived from the file path and written
	// into each PerkDef — no redundancy in the source JSON.
	//
	// perkDefsByID and embeddedPerkPools are written directly here (no
	// perkDefsMu / lock) — init() runs single-threaded before any goroutine
	// exists, the same exemption path_defs.go documents for pathCatalogMu.
	// perkDefsByID[def.ID] = def below has NO duplicate-id check, matching
	// the catalog's pre-existing (accepted) behavior: the shipped catalog is
	// known collision-free, and a later duplicate silently overwriting is
	// exactly the bug Part C closes for the EDITOR path (SavePerkPool/
	// SaveEditorPerkPool reject a new collision before it's ever written);
	// this loader's own embed-time behavior is intentionally left unchanged.
	perkDefsByID = make(map[string]*PerkDef)

	err := fs.WalkDir(perkDefsFS, "catalog/units", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".json") {
			return nil
		}

		rel := strings.TrimPrefix(p, "catalog/units/")
		parts := strings.Split(rel, "/")
		// Only files matching <faction>/<unit>/paths/<path>/perks/<rank>.json
		// are perk definitions. Everything else under catalog/units/ belongs
		// to unit_defs.go or path_defs.go — ignore it here.
		if len(parts) != 6 || parts[2] != unitPathsSubdirName || parts[4] != perkPoolDirName {
			return nil
		}
		unitType := parts[1]
		pathName := parts[3]
		rank := strings.TrimSuffix(parts[5], path.Ext(parts[5]))

		data, err := perkDefsFS.ReadFile(p)
		if err != nil {
			panic(p + ": " + err.Error())
		}
		var entries []perkEntryJSON
		if err := json.Unmarshal(data, &entries); err != nil {
			panic(p + ": " + err.Error())
		}
		defs, err := buildPerkDefsFromPool(unitType, pathName, rank, entries)
		if err != nil {
			panic(p + " " + err.Error())
		}
		for _, def := range defs {
			perkDefsByID[def.ID] = def
		}
		embeddedPerkPools[perkPoolKey(unitType, pathName, rank)] = clonePerkEntries(entries)
		return nil
	})
	if err != nil {
		panic("catalog/units: " + err.Error())
	}
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
// perkPoolForRankLocked in perks.go). Adding a new perk to the correct
// catalog/perks/<unit>/<path>/<rank>.json file is sufficient to include it
// in the eligible pool — no code changes needed here or in the assignment
// function.
//
// To restrict a perk to multiple paths or ranks, add multiple PerkDef entries
// sharing the same ID — or extend this function with set-based eligibility —
// but keep it as the single place that defines "what qualifies".
// ─────────────────────────────────────────────────────────────────────────────
func eligiblePerksForUnitAtRank(unit *Unit, rank string) []*PerkDef {
	var eligible []*PerkDef
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
