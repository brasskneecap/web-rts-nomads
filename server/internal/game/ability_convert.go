package game

import (
	"fmt"
	"sort"
	"strings"
)

// ═════════════════════════════════════════════════════════════════════════════
// LEGACY -> COMPOSABLE CONVERSION (Phase 5a, Task 3)
//
// ConvertLegacyAbility is the pure, non-persisting core of the editor's
// "Convert to Composable Ability" action (POST /abilities/{id}/convert). It
// compiles a legacy (schemaVersion absent/1) ability's flat mechanic fields
// into a composable *AbilityProgram via compileLegacyAbility, preserves the
// cast-setup fields (identity, targeting, cost/timing, presentation hooks)
// on a fresh AbilityDef, drops the legacy mechanic fields entirely, and
// reports HONEST degradation warnings when AbilityProgramRunnable finds part
// of the compiled program the composable executor cannot run yet.
//
// It never writes to the catalog — the editor reviews the result (including
// the warnings) and saves it explicitly via the existing POST /abilities
// path, same as any other edit.
// ═════════════════════════════════════════════════════════════════════════════

// ConvertLegacyAbility converts the legacy ability identified by id into its
// composable (schemaVersion 2) form. It returns the converted def (NOT
// saved), a list of degradation warnings (never nil — empty slice when
// nothing to warn about), and an error if id is unknown or the ability is
// already composable.
func ConvertLegacyAbility(id string) (AbilityDef, []string, error) {
	orig, ok := getAbilityDef(id)
	if !ok {
		return AbilityDef{}, nil, fmt.Errorf("unknown ability %q", id)
	}
	if orig.SchemaVersion >= 2 {
		return AbilityDef{}, nil, fmt.Errorf("ability %q is already composable (schemaVersion %d)", id, orig.SchemaVersion)
	}

	prog := compileLegacyAbility(orig)

	conv := AbilityDef{
		// ── Identity ──
		ID:          orig.ID,
		DisplayName: orig.DisplayName,
		Type:        orig.Type,
		Category:    orig.Category,
		DamageType:  orig.DamageType,
		Tags:        append([]string(nil), orig.Tags...), // fresh slice, no aliasing
		Icon:        orig.Icon,
		Description: orig.Description,

		// ── Targeting ──
		CanTargetSelf:    orig.CanTargetSelf,
		CanTargetAllies:  orig.CanTargetAllies,
		CanTargetEnemies: orig.CanTargetEnemies,
		TargetsPoint:     orig.TargetsPoint,
		CastRange:        orig.CastRange,

		// ── Cost / timing ──
		ManaCost: orig.ManaCost,
		Cooldown: orig.Cooldown,
		CastTime: orig.CastTime,

		// ── Auto-cast ──
		SupportsAutoCast:       orig.SupportsAutoCast,
		AutoCastTargetSelector: orig.AutoCastTargetSelector,
		DefaultAutoCast:        orig.DefaultAutoCast,

		// ── Presentation ──
		CasterAnimation: orig.CasterAnimation,

		// ── Composable program ──
		SchemaVersion: 2,
		Program:       prog,
	}
	// Every MECHANIC field (HealAmount, DamageAmount, Radius, Projectile,
	// ChainCount, ChannelType, ChargeRequired, ...) is intentionally left at
	// its zero value: the program above is now the sole authority for
	// behavior, per the SchemaVersion 2 contract on AbilityDef.

	warnings := degradationWarnings(prog)

	return conv, warnings, nil
}

// degradationWarnings inspects the compiled program and returns honest,
// specific warnings about behavior the composable executor cannot (yet) run.
// Never returns nil — an ability with nothing to warn about gets an empty,
// non-nil slice so the JSON response is always `"warnings": []`, never null.
func degradationWarnings(prog *AbilityProgram) []string {
	warnings := []string{}

	missing := unrunnableActionTypes(prog)
	// play_presentation is reported separately (second warning, about VFX /
	// perk hooks specifically) rather than folded into the generic mechanics
	// warning below, so an ability whose ONLY gap is play_presentation (e.g.
	// greater_heal's trailing healing_glow) doesn't get a misleading warning
	// about projectiles/chains/channels it doesn't actually use.
	var otherMissing []string
	hasPlayPresentation := false
	for _, t := range missing {
		if t == ActionPlayPresentation {
			hasPlayPresentation = true
			continue
		}
		otherMissing = append(otherMissing, string(t))
	}

	if len(otherMissing) > 0 {
		sort.Strings(otherMissing)
		warnings = append(warnings, fmt.Sprintf(
			"This ability uses mechanics the composable runtime does not execute yet (%s). "+
				"Converting now will make it inert in-game until a later phase.",
			strings.Join(otherMissing, ", "),
		))
	}
	if hasPlayPresentation {
		warnings = append(warnings,
			"On-target visual effects and per-target perk triggers are not yet reproduced by the "+
				"composable runtime; converting may change how this ability looks/interacts until a later phase.")
	}

	return warnings
}

// unrunnableActionTypes walks the same structurally-visible tree as
// AbilityProgramRunnable and returns the distinct action types that lack a
// registered, executable ActionDescriptor. Order is NOT guaranteed
// (NamedTriggers is a map); callers that need deterministic output (e.g. the
// warning message below) must sort the result themselves.
func unrunnableActionTypes(prog *AbilityProgram) []ActionType {
	if prog == nil {
		return nil
	}

	seen := map[ActionType]bool{}
	var out []ActionType
	note := func(t ActionType) {
		if d, ok := lookupActionDescriptor(t); ok && d.Execute != nil {
			return
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}

	var walkAction func(a AbilityActionDef)
	var walkTrigger func(trig AbilityTriggerDef)
	walkAction = func(a AbilityActionDef) {
		note(a.Type)
		for _, child := range a.Children {
			walkTrigger(child)
		}
	}
	walkTrigger = func(trig AbilityTriggerDef) {
		for _, a := range trig.Actions {
			walkAction(a)
		}
	}

	for _, trig := range prog.Triggers {
		walkTrigger(trig)
	}
	for _, pres := range prog.Presentations {
		for _, trig := range pres.Triggers {
			walkTrigger(trig)
		}
	}
	for _, trig := range prog.NamedTriggers {
		walkTrigger(trig)
	}

	return out
}
