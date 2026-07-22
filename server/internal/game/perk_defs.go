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
// │      → catalog/perks/<path>/<id>/<id>.json                              │
// │        One directory per perk id, nested under its owning promotion     │
// │        path's folder (or catalog/perks/generic/ for path-agnostic       │
// │        perks). The folder name is authoritative for PerkDef.Path — it   │
// │        is DERIVED at load time, never read from the JSON body. Adding   │
// │        a perk means adding a new                                       │
// │        catalog/perks/<path-or-generic>/<newid>/<newid>.json.            │
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
// A perk's association (PerkDef.Path) is derived from its folder under
// catalog/perks/ at load time — "" for perks living in catalog/perks/generic/,
// otherwise the folder name. It is editor filtering/display metadata only and
// does NOT drive rank-up selection. A path's authored PerksByRank
// (path_defs.go) is the SOLE source of its rank-up pool. The assignment
// system in perks.go calls eligiblePerksForUnitAtRank() (via
// perkPoolForRankLocked), which resolves only that list; a new perk JSON is
// NOT automatically eligible until its id is added to the relevant path's
// PerksByRank[rank].
// ═════════════════════════════════════════════════════════════════════════════

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Embeds the standalone perk catalog tree so this file can load perk JSONs
// from catalog/perks/<path>/<id>/<id>.json (or catalog/perks/generic/<id>/
// <id>.json for path-agnostic perks). Mirrors effect_defs.go's embed of
// catalog/effects; each perk lives in its own id-named leaf directory,
// nested under its association folder, whose name must equal the JSON's
// "id" field.
//
//go:embed all:catalog/perks
var perkDefsFS embed.FS

// perkIDPattern is the id gate for editor-authored perks (SavePerkDef /
// DeletePerkOverride). The embedded loader gates ids against the directory
// name instead, so this pattern is not applied there.
var perkIDPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

// hexColorPattern gates PerkAura.RingColor. Accepts CSS hex color shorthand
// (#rgb), full (#rrggbb), and full-with-alpha (#rrggbbaa) — the three forms
// a native <input type="color"> (RGB via the picker) or a hand-typed value
// (any of the three) can produce. Case-insensitive.
var hexColorPattern = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)

// embeddedPerkDefs is the standalone catalog, id-keyed, loaded once at init.
// It is the immutable baseline that rebuildPerkRegistry (perk_persistence.go)
// merges the writable overlay on top of.
var embeddedPerkDefs = loadPerkDefs()

// loadPerkDefs reads every catalog/perks/<path>/<id>/<id>.json into an
// id-keyed map. Mirrors loadEffectDefs: the leaf directory name is
// authoritative for the id, and the parent directory name is authoritative
// for the perk's association (PerkDef.Path) — "generic" maps to "" (any
// path). Any structural problem panics at startup (embedded data is a
// build-time bug if malformed).
func loadPerkDefs() map[string]PerkDef {
	assocDirs, err := fs.ReadDir(perkDefsFS, "catalog/perks")
	if err != nil {
		panic("catalog/perks: " + err.Error())
	}
	result := make(map[string]PerkDef)
	for _, assocEntry := range assocDirs {
		if !assocEntry.IsDir() {
			continue
		}
		assoc := assocEntry.Name() // "siphoner", "trapper", …, or "generic"
		// "generic" is the wildcard bucket → empty association.
		assocPath := assoc
		if assoc == "generic" {
			assocPath = ""
		}
		perkDirs, err := fs.ReadDir(perkDefsFS, "catalog/perks/"+assoc)
		if err != nil {
			panic("catalog/perks/" + assoc + ": " + err.Error())
		}
		for _, entry := range perkDirs {
			if !entry.IsDir() {
				continue // skips .gitkeep and other loose files
			}
			id := entry.Name()
			rel := "catalog/perks/" + assoc + "/" + id + "/" + id + ".json"
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
			def.Path = assocPath // folder is authoritative for association
			if err := validatePerkDef(&def); err != nil {
				panic(rel + ": " + err.Error())
			}
			if _, dup := result[def.ID]; dup {
				panic(rel + ": duplicate perk id " + def.ID)
			}
			result[def.ID] = def
		}
	}
	return result
}

// validatePerkDef is the shared load + save content gate. Does NOT check id
// (loader gates against dir name; editor against perkIDPattern).
func validatePerkDef(def *PerkDef) error {
	if def.Effect != nil {
		switch def.Effect.Target {
		case "", "self", "enemies":
		default:
			return fmt.Errorf("effect.target %q must be \"self\" | \"enemies\"", def.Effect.Target)
		}
	}
	for _, m := range def.AbilityModifiers {
		if m.Target == "" {
			return fmt.Errorf("abilityModifiers entry has empty target")
		}
	}
	if err := validateAbilityFieldModifiers(fmt.Sprintf("perk %q", def.ID), def.AbilityFields); err != nil {
		return err
	}
	for _, sm := range def.StatModifiers {
		if err := validatePerkStatModifier(sm); err != nil {
			return fmt.Errorf("statModifiers: %w", err)
		}
		// PerCompanion only means something inside a PerkAura.StatModifiers
		// entry (see PerkStatModifier.PerCompanion's doc comment) — a
		// top-level entry only ever affects the owning unit, so nothing
		// anywhere ever reads PerCompanion off it. Reject rather than let it
		// silently no-op (standing "no inert authorable fields" rule).
		if sm.PerCompanion != 0 {
			return fmt.Errorf("statModifiers: stat %q sets perCompanion but perCompanion is only meaningful inside auras[].statModifiers — it is never read on a top-level statModifiers entry", sm.Stat)
		}
		// AuraOnly stats (statDef.AuraOnly, stat_modifiers.go — e.g.
		// armorPercent, projectileDamageReduction) have NO top-level fold
		// site: unitPerkStatModifiersLocked's entire caller list resolves
		// only stats with a real per-unit read site, and these two are
		// consumed exclusively via the aura cache
		// (unitAuraStatContributionLocked). A top-level entry naming one
		// would compute and store a per-stage value that nothing ever reads
		// — the identical silent-no-op shape PerCompanion is rejected for
		// above. Reject here too; the SAME stat inside auras[].statModifiers
		// is unaffected by this check (see the loop below) and remains its
		// valid, intended home.
		if isAuraOnlyStat(sm.Stat) {
			return fmt.Errorf("statModifiers: stat %q can only be used inside an auras[] entry (auras[].statModifiers) — it has no effect on the owning unit's own top-level statModifiers because no top-level fold site reads it (see statDef.AuraOnly, stat_modifiers.go)", sm.Stat)
		}
	}
	for i, aura := range def.Auras {
		if aura.Radius <= 0 {
			return fmt.Errorf("auras[%d]: radius must be > 0", i)
		}
		switch aura.Targets {
		case "allies", "enemies", "sameOwner":
		default:
			return fmt.Errorf("auras[%d]: targets %q must be \"allies\", \"enemies\", or \"sameOwner\"", i, aura.Targets)
		}
		switch aura.Stacking {
		case "", auraStackingMax:
		default:
			return fmt.Errorf("auras[%d]: stacking %q must be %q (or omit)", i, aura.Stacking, auraStackingMax)
		}
		for j, sm := range aura.StatModifiers {
			if err := validatePerkStatModifier(sm); err != nil {
				return fmt.Errorf("auras[%d].statModifiers[%d]: %w", i, j, err)
			}
			// Auras currently support only additive, base-stage stat
			// contributions — see PerkAura.StatModifiers' doc comment and
			// perk_aura_stat_cache.go's rebuildAuraStatCacheLocked, which
			// reads sm.Value straight into an additive accumulator and never
			// looks at sm.Op or sm.Stage at all. Authoring "multiply" or a
			// non-base stage here would silently fall back to add-at-base
			// behavior with no error anywhere — reject it at load time
			// instead, per the standing "no inert authorable fields" rule
			// (the same reasoning that removed cooldownMult/radiusMult/
			// durationMult from AbilityModifier). Lift this once a real aura
			// fold site consumes Op/Stage (see the doc comment for what that
			// would take).
			if sm.Op != statOpAdd {
				return fmt.Errorf("auras[%d].statModifiers[%d]: op %q not supported for auras — auras currently support only additive (%q) stat contributions; %q and other ops are not implemented by any aura fold site yet", i, j, sm.Op, statOpAdd, sm.Op)
			}
			stage := sm.Stage
			if stage == "" {
				stage = statStageBase
			}
			if stage != statStageBase {
				return fmt.Errorf("auras[%d].statModifiers[%d]: stage %q not supported for auras — auras currently support only base-stage stat contributions; %q and other stages are not implemented by any aura fold site yet", i, j, sm.Stage, statStageBase)
			}
		}
		if aura.RingColor != "" && !hexColorPattern.MatchString(aura.RingColor) {
			return fmt.Errorf("auras[%d]: ringColor %q must be a valid CSS hex color (#rgb, #rrggbb, or #rrggbbaa)", i, aura.RingColor)
		}
	}
	for _, r := range def.AbilityRiders {
		if r.Target == "" {
			return fmt.Errorf("abilityRiders entry has empty target")
		}
		if !isKnownTriggerType(r.Trigger) {
			return fmt.Errorf("abilityRiders entry %q has unknown trigger %q", r.Target, r.Trigger)
		}
		// Route each rider action through the SAME structural validator the
		// ability program itself uses (ability_program_validate.go), so a
		// rider's actions are held to the identical bar as an authored
		// ability's actions — one validator, not a parallel hand-rolled copy.
		w := &validationWalker{seenIDs: map[string]bool{}}
		for i, action := range r.Actions {
			w.walkAction(action, fmt.Sprintf("abilityRiders[target=%s].actions[%d]", r.Target, i), false, false, nil)
		}
		for _, issue := range w.issues {
			if issue.Severity == "error" {
				return fmt.Errorf("abilityRiders %q: %s: %s", r.Target, issue.Path, issue.Message)
			}
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
	Target          string  `json:"target"` // "self" or "enemies"
	SizeScale       float64 `json:"sizeScale,omitempty"`
	DurationSeconds float64 `json:"durationSeconds,omitempty"`
	Variant         string  `json:"variant,omitempty"`
}

// AbilityModifier is a scalar modification a perk applies to a target ability
// (by id today; ability tags are the planned extension). A zero-valued mult
// means "unset" (identity 1.0) — perks only set the fields they change.
type AbilityModifier struct {
	Target       string  `json:"target"` // ability id
	DamageMult   float64 `json:"damageMult,omitempty"`
	HealMult     float64 `json:"healMult,omitempty"`
	ManaCostMult float64 `json:"manaCostMult,omitempty"`
	RangeMult    float64 `json:"rangeMult,omitempty"`
	// CooldownMult scales the target ability's effective cooldown. Folded into
	// the cast-cooldown arm site via effectiveSpellLocked (spell_modifier.go),
	// so a perk can make an ability come off cooldown faster (mult < 1) or
	// slower (mult > 1). Re-added when rapid_deployment — the Trapper's "place
	// traps 30% more often" perk — became a data perk modifying the four trap
	// abilities' cooldowns, the concrete consumer that justifies the field (it
	// had been removed earlier as inert under the "no unused authorable fields"
	// rule). A zero/negative value is treated as unset (identity 1.0).
	CooldownMult float64 `json:"cooldownMult,omitempty"`
}

// AbilityRider is a fragment of actions a perk grafts onto a target
// ability's existing trigger — the second of two data-driven perk→ability
// influence mechanisms alongside AbilityModifier (which only scales
// existing numbers). Where AbilityModifier multiplies a scalar on the
// target ability, a rider ADDS behavior: its Actions run whenever the
// target ability's Trigger fires, as if authored directly into that
// ability's program.
//
// This is schema + validation only (T1): nothing executes riders yet. The
// runtime that grafts Actions onto the live ability program at Trigger and
// runs them is a later task.
type AbilityRider struct {
	// Target is the id of the ability this rider attaches to.
	Target string `json:"target"`
	// Trigger is the TriggerType on the target ability's program that this
	// rider's Actions run alongside. Must be one of the TriggerType consts
	// (ability_program.go) — see isKnownTriggerType.
	Trigger TriggerType `json:"trigger"`
	// Actions are appended to whatever runs when Trigger fires. Validated
	// with the same structural validator (walkAction,
	// ability_program_validate.go) an authored ability's own actions go
	// through, so a rider action is held to the identical bar.
	Actions []AbilityActionDef `json:"actions,omitempty"`
}

// PerkStatModifier is one typed, validated unit-stat change contributed by a
// perk — the data-driven replacement for the old freeform Config-map
// convention, under which a perk's Go handler had to know the exact key
// string to read; a typo there silently read 0 forever with no error
// anywhere. Stat is checked against the registered stat vocabulary
// (stat_modifiers.go's statRegistry / isKnownStat) at catalog load
// (validatePerkDef), so an unknown stat is a build-time error instead of a
// silent no-op.
//
//   - Stat  — a statRegistry id (e.g. "moveSpeed", "armor"). MUST satisfy
//     isKnownStat; validatePerkDef rejects unknown ids with a designer-facing
//     message listing the valid ones.
//   - Op    — statOpAdd ("add") or statOpMultiply ("multiply").
//   - Value — the operand for Op.
//   - Stage — statStageIntrinsic ("intrinsic"), statStageBase ("base", the
//     default when omitted), or statStageFinal ("final"). Base-stage
//     modifiers fold into the same (base + Σadd) × Πmul pool zone auras
//     already use; intrinsic-stage modifiers apply strictly BEFORE that pool
//     (for scaling the unit's own base stat without also scaling an external
//     additive bonus); final-stage modifiers apply strictly AFTER every
//     base-stage contribution. See applyStatStages (stat_modifiers.go) for
//     the exact fold order.
//
// Resolved per-unit by unitPerkStatModifiersLocked (perk_stat_modifiers.go)
// and folded in at each stat's existing zone-aura fold site.
type PerkStatModifier struct {
	Stat  string  `json:"stat"`
	Op    string  `json:"op"`
	Value float64 `json:"value"`
	Stage string  `json:"stage,omitempty"`
	// PerCompanion is the EMITTER-side synergy bonus this modifier's Value
	// gains per companion emitter of the SAME PerkAura — see
	// PerkAura.SynergyRadiusPerCompanion for the full companion-counting
	// algorithm (rebuildAuraStatCacheLocked, perk_aura_stat_cache.go).
	// Guardian Vanguards standing near each other is the pilot: each
	// companion within the aura's BASE radius adds PerCompanion to the
	// effective Value fanned out to recipients, e.g. bonusArmor: 15,
	// perCompanion: 5 with 2 companions emits effective value 25.
	//
	// ONLY meaningful inside a PerkAura.StatModifiers entry — a
	// top-level PerkDef.StatModifiers entry (this same type, reused) only
	// ever affects the OWNING unit and has no notion of "nearby companion
	// emitters" at all, so validatePerkDef REJECTS a non-zero PerCompanion
	// there (no inert authorable fields — see the standing rule that
	// removed cooldownMult/radiusMult/durationMult from AbilityModifier).
	PerCompanion float64 `json:"perCompanion,omitempty"`
}

// validatePerkStatModifier checks one PerkStatModifier's fields against the
// shared stat vocabulary (stat_modifiers.go). Shared by both validation call
// sites — a top-level PerkDef.StatModifiers entry and a nested
// PerkAura.StatModifiers entry — so a designer authoring either one gets the
// identical bar and the identical error message shape. Returns an unwrapped
// error; callers add their own positional context (e.g. "statModifiers:" or
// "auras[0].statModifiers[1]:") via fmt.Errorf("%w", ...).
func validatePerkStatModifier(sm PerkStatModifier) error {
	if !isKnownStat(sm.Stat) {
		return fmt.Errorf("unknown stat %q — valid stats are: %s", sm.Stat, strings.Join(ListStatIDs(), ", "))
	}
	if sm.Op != statOpAdd && sm.Op != statOpMultiply {
		return fmt.Errorf("stat %q has invalid op %q (want %q or %q)", sm.Stat, sm.Op, statOpAdd, statOpMultiply)
	}
	switch sm.Stage {
	case "", statStageIntrinsic, statStageBase, statStageFinal:
	default:
		return fmt.Errorf("stat %q has unknown stage %q (want %q, %q, %q, or omit for base)", sm.Stat, sm.Stage, statStageIntrinsic, statStageBase, statStageFinal)
	}
	if sm.Op == statOpMultiply {
		if d, ok := statRegistryByID[sm.Stat]; ok && !d.AllowMultiply {
			return fmt.Errorf("stat %q does not support multiply (see statRegistry AllowMultiply)", sm.Stat)
		}
	}
	return nil
}

// auraStackingMax is the only implemented PerkAura.Stacking mode today: take
// the max Value and max PerAdditionalSource independently across covering
// sources, then effectiveValue = maxValue + (count-1) * maxPerAdditionalSource.
// See PerkAura's doc comment and perk_aura_stat_cache.go's
// rebuildAuraStatCacheLocked for the full algorithm.
const auraStackingMax = "max"

// PerkAura describes a radius-based stat effect a perk EMITS to OTHER units
// — the aura sibling of PerkStatModifier, which only ever affects the OWNING
// unit. Pilot: zealous_march (Silver Cleric) grants nearby allies bonus move
// speed. Resolved per-tick by the generic cache in perk_aura_stat_cache.go
// (rebuildAuraStatCacheLocked / unitAuraStatContributionLocked), never by a
// perk-specific Go handler.
//
//   - Radius              — pixel radius the aura covers. Must be > 0
//     (validatePerkDef rejects <= 0). This is the BASE radius — see
//     SynergyRadiusPerCompanion for how it can grow at runtime.
//   - Targets             — "allies", "enemies", or "sameOwner": which units
//     relative to the emitter are eligible recipients.
//     "allies" means playersAreFriendlyLocked(emitter, candidate) — same
//     TEAM, which can include units belonging to a DIFFERENT player on a
//     multiplayer team. "sameOwner" is the strictly narrower relation
//     candidate.OwnerID == emitter.OwnerID — the SAME player's units only.
//     This distinction is invisible in 1-owner-per-team matches but matters
//     the moment two players share a team: an "allies"-targeted aura covers
//     a teammate's units too, a "sameOwner"-targeted aura does not.
//     guardian_aura uses "sameOwner" deliberately (see its catalog entry) —
//     using "allies" there would silently change behavior in team games
//     versus the pre-migration bespoke scan, which compared u.OwnerID ==
//     source.ownerID directly. "enemies" means
//     playersAreHostileLocked(emitter, candidate), unrelated to ownership.
//   - IncludeSelf         — when true, the emitting unit is itself a valid
//     recipient (meaningful with Targets == "allies" or "sameOwner" — a unit
//     is never its own enemy, so it's a no-op under "enemies").
//     zealous_march sets this true: a Cleric buffs itself. guardian_aura
//     leaves this false (the default): the legacy algorithm hard-excluded
//     the emitting unit from its own aura (u.ID == source.unitID skip), so
//     the Vanguard emitting guardian_aura does NOT benefit from its own
//     armor bonus.
//   - Stacking            — combination rule across multiple covering
//     sources. Only auraStackingMax ("max", the default when omitted) is
//     implemented today.
//   - PerAdditionalSource — RECIPIENT-side stacking: under "max" stacking,
//     the smaller bonus every covering source BEYOND the first contributes
//     on top of the strongest Value, when multiple DIFFERENT emitters cover
//     the SAME recipient. Zero (default) collapses to pure max-wins, no
//     stacking bonus. Distinct from SynergyRadiusPerCompanion /
//     PerkStatModifier.PerCompanion below, which is EMITTER-side: it grows
//     one emitter's own radius/value based on companion emitters near THAT
//     EMITTER, before the recipient fan-out even happens. guardian_aura uses
//     only the emitter-side form (PerAdditionalSource stays 0 — the legacy
//     algorithm took a strict per-dimension max across sources at the
//     recipient, never an additional recipient-side stacking bonus).
//   - SynergyRadiusPerCompanion — EMITTER-side companion synergy. When > 0,
//     rebuildAuraStatCacheLocked first counts, for THIS emitter, how many
//     OTHER emitters of the SAME aura (same owning perk id + aura index)
//     with the SAME OwnerID sit within this aura's BASE Radius (never the
//     companion-inflated effective radius — using the effective radius here
//     would create recursive radius inflation: more companions → bigger
//     radius → detects even more companions. Always compare against the
//     fixed base Radius). That companion count then scales BOTH this
//     emitter's effective radius (Radius + count×SynergyRadiusPerCompanion)
//     AND each of its StatModifiers entries' effective value
//     (Value + count×PerCompanion — see PerkStatModifier.PerCompanion).
//     Zero (default, and the only value zealous_march/mana_conduit/sanctuary
//     author) means no companion phase runs for this aura at all — behavior-
//     neutral for every aura migrated before guardian_aura. guardian_aura is
//     the pilot: nearby Guardian Vanguards amplify each other's auras.
//   - StatModifiers       — the stat changes granted to every covered
//     recipient, in the SAME Stat/Op/Value/Stage vocabulary
//     PerkDef.StatModifiers uses (validated by the same
//     validatePerkStatModifier). IMPORTANT: unlike PerkDef.StatModifiers,
//     these are NOT folded through unitPerkStatModifiersLocked /
//     applyStatStages — they are resolved via
//     unitAuraStatContributionLocked, and each stat's OWN fold site decides
//     where the returned value lands in its arithmetic. This exists because
//     folding an aura through the generic (base+add)×mul pipeline can
//     silently change WHEN it composes with other same-stat bonuses (see the
//     hawk_spirit / zealous_march "ordering trap" documented in
//     perk_aura_stat_cache.go and perk_aura_migration_test.go).
//     SCHEMA LIMITATION, ENFORCED: an aura StatModifiers entry's Op MUST be
//     "add" and its Stage MUST be omitted/"base" — validatePerkDef REJECTS
//     "multiply" and any non-base stage at catalog load. This is not a
//     stylistic preference: rebuildAuraStatCacheLocked (perk_aura_stat_cache.go)
//     reads only sm.Value into a raw additive accumulator and never inspects
//     sm.Op or sm.Stage at all, so an authored "multiply" or "final" stage
//     would silently behave exactly like "add"/"base" with no error — the
//     validator exists to turn that silent no-op into a load-time error
//     instead. Lift the restriction only once a real fold site is written
//     that actually consumes Op/Stage for an aura.
//   - RingColor            — PURELY PRESENTATIONAL. Optionally overrides the
//     color of this aura's radius ring in the HUD (CanvasRenderer's
//     drawAuraRing). Empty ⇒ the ring falls back to the owning player's
//     color, exactly as before this field existed. Never read by any
//     gameplay/simulation code — only by the client renderer, via the
//     GeneratedDescription-adjacent wire field on PerkAura. Must be a valid
//     CSS hex color (#rgb, #rrggbb, or #rrggbbaa) when set; validatePerkDef
//     rejects anything else rather than silently falling back to the player
//     color on a typo.
type PerkAura struct {
	Radius                    float64            `json:"radius"`
	Targets                   string             `json:"targets"`
	IncludeSelf               bool               `json:"includeSelf,omitempty"`
	Stacking                  string             `json:"stacking,omitempty"`
	PerAdditionalSource       float64            `json:"perAdditionalSource,omitempty"`
	SynergyRadiusPerCompanion float64            `json:"synergyRadiusPerCompanion,omitempty"`
	StatModifiers             []PerkStatModifier `json:"statModifiers"`
	// RingColor optionally overrides the color of the aura's radius ring in
	// the HUD. Empty ⇒ the ring falls back to the owning player's color,
	// exactly as before this field existed. Purely presentational: it never
	// affects gameplay.
	RingColor string `json:"ringColor,omitempty"`
}

// PerkDef is the static definition of a perk loaded from the catalog.
//
// Fields:
//   - ID           — unique string key; used by runtime handlers to dispatch behaviour
//   - DisplayName  — human-readable name shown in UI
//   - Description  — one-line flavour/tooltip text
//   - Path         — the perk's association: the promotion path whose folder it
//                    lives in under catalog/perks/<path>/<id>/. Empty means it
//                    lives in catalog/perks/generic/ (usable by any path).
//                    DERIVED FROM THE FOLDER at load — never read from the
//                    JSON body — and used only for editor picker filtering +
//                    display, NOT for rank-up selection (that is perksByRank).
//   - RequiresPerk — (optional) gate: this perk only appears in the pool when
//                    the unit already owns the named perk. Empty = no gate.
//                    Useful for Silver/Gold perks that only make sense alongside
//                    a specific Bronze perk (e.g. explosive_chain requires
//                    explosive_trap). Set in the JSON as "requiresPerk".
//   - RequiresAbility — (optional) gate: this perk only appears in the pool when
//                    the unit already KNOWS the named ability (unit.Abilities).
//                    The ability-era analogue of RequiresPerk, used by the
//                    trap-specific silver perks now that the four bronze traps
//                    are pool ABILITIES. Set in the JSON as "requiresAbility".
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
	Icon string `json:"icon,omitempty"`
	// Path is the perk's association: the promotion path whose folder it lives
	// in under catalog/perks/<path>/<id>/. Empty means it lives in
	// catalog/perks/generic/ (usable by any path). DERIVED FROM THE FOLDER at
	// load — never read from the JSON body — and used only for editor picker
	// filtering + display, NOT for rank-up selection (that is perksByRank).
	Path         string             `json:"path,omitempty"`
	RequiresPerk string             `json:"requiresPerk,omitempty"`
	// RequiresAbility gates this perk on the unit already KNOWING an ability
	// (unit.Abilities), the ability-era analogue of RequiresPerk. It exists so
	// a Silver/Gold perk that upgrades a specific trap can gate on the unit
	// having rolled that trap ABILITY from its bronze ability pool — the
	// mechanism that replaced the bronze trap perks (see the trapper path's
	// abilityPoolsByRank and the trap-specific silver perks barbed_field /
	// explosive_chain / exposed_weakness / lasting_flames). Empty = no gate.
	// Enforced alongside RequiresPerk in eligiblePerksAfterFiltersLocked.
	RequiresAbility string             `json:"requiresAbility,omitempty"`
	Config          map[string]float64 `json:"config"`
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
	// AbilityModifiers: scalar modifiers this perk applies to target abilities
	// (data-driven replacement for bespoke per-ability modifier hooks). Empty
	// for most perks. Read via abilityScalarModifiersForCasterLocked.
	AbilityModifiers []AbilityModifier `json:"abilityModifiers,omitempty"`
	// AbilityParams are this perk's contributions to target abilities' declared
	// PARAMETERS (AbilityDef.Params) — the preferred way for a perk to change a
	// number on an ability, and the same shape items/advancements use so no
	// source family is privileged. Target may be an ability id or "tag:<name>".
	// See ability_params.go and docs/design/ability_perk_interaction.md §3.4.
	// AbilityFields are this perk's PRECISE contributions: one field on one
	// action of one ability ({target, action, field}). This is the addressing a
	// perk wants — extended_setup extends a trap's ZONE duration but not the burn
	// status inside it, and only an action-level address can tell those apart.
	// See ability_field_mods.go.
	AbilityFields []AbilityFieldModifier `json:"abilityFields,omitempty"`
	// StatModifiers: typed, validated unit-stat changes this perk applies —
	// data-driven replacement for the freeform Config-map convention where a
	// perk's runtime handler had to know the exact key string to read (a
	// typo silently read 0 forever). Empty for every perk today; this is the
	// ENGINE (typed schema + validation + aggregation + fold sites), not a
	// migration — existing perks keep reading Config via their Go switch
	// arms until a follow-up task moves them over. See
	// perk_stat_modifiers.go's unitPerkStatModifiersLocked for how these are
	// resolved and stat_modifiers.go's applyStatStages for how they combine
	// with zone auras.
	StatModifiers []PerkStatModifier `json:"statModifiers,omitempty"`
	// Auras: radius-based stat effects this perk EMITS to OTHER units (see
	// PerkAura's doc comment for the full vocabulary). Empty for most perks.
	// Resolved every tick by the generic cache in perk_aura_stat_cache.go —
	// zero perk-specific Go required. Pilot: zealous_march.
	Auras []PerkAura `json:"auras,omitempty"`
	// AbilityRiders: action fragments this perk grafts onto a target
	// ability's existing trigger (data-driven "add behavior" sibling to
	// AbilityModifiers' "scale a number"). Empty for most perks. Schema +
	// validation only today — see AbilityRider's doc comment; the runtime
	// that executes a rider's Actions is a later task.
	AbilityRiders []AbilityRider `json:"abilityRiders,omitempty"`
	// Wired reports whether this perk actually does something in a match:
	// either a Go handler exists for its id, or it carries typed data
	// (StatModifiers/AbilityModifiers/AbilityRiders/GrantsAbilities) the
	// generic engine executes (spec §7.3) — see perk_wired.go's isWiredPerk
	// / perkHasTypedBehavior for exactly what counts. It is a derived,
	// presentation-only field: it is NEVER set on the
	// registry's own *PerkDef values (perkDefsByID / perkDefLookup /
	// snapshotPerkDefs all leave it at its zero value, false). ListPerkDefs
	// is the ONLY place that populates it, on the per-def COPY it returns —
	// the shape the /catalog/perks HTTP endpoint and the future editor UI
	// consume, so an editor-authored perk with no matching handler can be
	// labeled "inert" instead of silently doing nothing in a match.
	Wired bool `json:"wired"`
	// GeneratedDescription is the tooltip prose describePerk (perk_describe.go)
	// generates from this perk's typed authored fields (StatModifiers,
	// AbilityModifiers, AbilityRiders, GrantsAbilities) — the perk-catalog
	// sibling of AbilityDef.GeneratedDescription. It is a derived,
	// presentation-only field: it is NEVER set on the registry's own
	// *PerkDef values (perkDefsByID / perkDefLookup / snapshotPerkDefs all
	// leave it at its zero value, ""), mirroring Wired's exact discipline
	// above. ListPerkDefs is the ONLY place that populates it, on the
	// per-def COPY it returns, so a future client/editor can show the
	// generated text (and eventually treat it as the single source of
	// truth, once TooltipTemplate/Config are retired per perk).
	GeneratedDescription string `json:"generatedDescription,omitempty"`
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
// perkDefsMu guards perkDefsByID. Its var initializer (see below) populates it
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
//
// Initialized as a package-level VAR (not in an init() func): Go guarantees all
// var initializers complete before any init() runs, so the registry is
// populated before path_defs.go's init validates each path's perksByRank ids
// via perkDefLookup. A prior init()-based build raced path_defs.go's init
// (alphabetical file order runs path_defs' init first, leaving this nil).
// Overlay is empty at package-init; LoadPersistedPerksIntoOverlay refreshes it
// at startup. Mirrors the embeddedPerkDefs / unit-def var-initializer ordering.
var perkDefsMu sync.RWMutex
var perkDefsByID = buildPerkRegistry(nil)

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

// eligiblePerksForUnitAtRank returns the perks a unit may roll at the given
// rank. AUTHORITATIVE MODEL: the ONLY source is the unit's promotion path's
// explicit perksByRank list (pathPerkRefsForRank). A perk's own folder-derived
// association (PerkDef.Path) is used only for editor filtering and display — it
// no longer participates in rank-up selection. Unknown ids resolve fail-safe
// (skipped). The ID-sort keeps rngPerks.Intn deterministic regardless of the
// authored list order, preserving replay reproducibility (AI_RULES.md).
func eligiblePerksForUnitAtRank(unit *Unit, rank string) []*PerkDef {
	if unit == nil {
		return nil
	}
	refs := pathPerkRefsForRank(unit.ProgressionPath, rank)
	if len(refs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(refs))
	eligible := make([]*PerkDef, 0, len(refs))
	for _, perkID := range refs {
		if _, dup := seen[perkID]; dup {
			continue
		}
		if def, ok := perkDefLookup(perkID); ok {
			eligible = append(eligible, def)
			seen[perkID] = struct{}{}
		}
	}
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
		cp.Wired = isWiredPerk(cp)
		cp.GeneratedDescription = describePerk(cp)
		defs = append(defs, cp)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs
}
