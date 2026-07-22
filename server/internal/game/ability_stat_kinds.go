package game

import "sort"

// ═════════════════════════════════════════════════════════════════════════════
// ABILITY STAT KINDS — the semantic tag that lets a UNIT or an ITEM say
// "+15% radius" without naming a single ability.
//
// A perk generally knows which ability it is buffing, so it addresses a field
// precisely: {ability, action id, field}. An ITEM cannot — it does not know who
// equipped it or what they cast. Those broad sources instead target a KIND, and
// every schema field carrying that Kind is scaled.
//
// WHY A SEPARATE TAG RATHER THAN MATCHING ON Key OR Control:
//
//   - Key is inconsistent and would be both incomplete and dangerous. The
//     registry today spells radii as radius / explosionRadius / triggerRadius /
//     allyHealRadius, and durations as duration / durationMs / durationSeconds /
//     markDuration / buffDurationSeconds. A "name contains radius" rule misses
//     triggerRadius; a "name contains duration" rule happily matches
//     bindToStatusDuration, which is a BOOLEAN.
//   - Control cannot disambiguate either: create_zone's "duration" and its
//     "tickInterval" are BOTH Control "duration", and scaling tick interval
//     with a duration stat would silently change an ability's DPS.
//
// So participation is opt-IN. A field with no Kind is invisible to broad
// modifiers, and the set of Kind assignments IS the reviewable allow-list.
// castRange deliberately carries no Kind: a bigger fire pit should not also be
// throwable further — that is a different stat if it is ever wanted.
// ═════════════════════════════════════════════════════════════════════════════

const (
	// abilityStatKindRadius — an area of effect, in world units.
	abilityStatKindRadius = "radius"
	// abilityStatKindDuration — how long an effect persists, in seconds.
	abilityStatKindDuration = "duration"
	// abilityStatKindCount — a repetition count (bounces, projectiles, summons).
	abilityStatKindCount = "count"
	// abilityStatKindSpeed — travel speed, in world units per second.
	abilityStatKindSpeed = "speed"
	// abilityStatKindDamage / abilityStatKindHeal mark the magnitude fields that
	// the ability-power ratio attaches to. They are deliberately NOT offered as
	// "Ability Stats" grid rows: flat ability damage is expressed as abilityPower
	// (which is ratio-normalized, so a DoT tick and a burst nuke gain the same
	// TOTAL) and percentage ability damage is the existing abilityDamage stat.
	// Tagging them here still earns the precise-mode editor a typed field picker.
	abilityStatKindDamage = "damage"
	abilityStatKindHeal   = "heal"
)

// abilityStatKindLabels is every valid Kind and its designer-facing name. A Kind
// absent from this map is a load-time programming error (see TestAbilityStatKinds).
var abilityStatKindLabels = map[string]string{
	abilityStatKindRadius:   "Radius",
	abilityStatKindDuration: "Duration",
	abilityStatKindCount:    "Count",
	abilityStatKindSpeed:    "Speed",
	abilityStatKindDamage:   "Damage",
	abilityStatKindHeal:     "Heal",
}

// abilityStatGridKinds are the kinds offered as broad "Ability Stats" rows on a
// unit or an item, in display order. Damage/heal are excluded on purpose — see
// abilityStatKindDamage's comment.
var abilityStatGridKinds = []string{
	abilityStatKindRadius,
	abilityStatKindDuration,
	abilityStatKindCount,
	abilityStatKindSpeed,
}

// actionStatQualifier is the SHORT name an action contributes to a scoped stat
// label, so the row reads "Zone Duration" rather than "Create Zone Duration".
//
// This is the one hand-maintained table in the mechanism, and it rots the same
// way wiredPerkIDs does — so TestAbilityStatKinds asserts that EVERY action
// declaring a kinded schema field has an entry here. Adding a kinded field to an
// action without adding its qualifier is a test failure, not a silently ugly
// label.
var actionStatQualifier = map[ActionType]string{
	ActionCreateZone:          "Zone",
	ActionApplyStatusDuration: "Status",
	ActionApplyMark:           "Mark",
	ActionLaunchProjectile:    "Projectile",
	ActionBeam:                "Beam",
	ActionApplyForce:          "Force",
	ActionSummonUnit:          "Summon",
	ActionDealDamage:          "Damage",
	ActionRestoreHealth:       "Heal",
	ActionSelectTargets:       "Search",
	ActionApplyStatus:         "Legacy Status",
	ActionPlaceTrap:           "Trap",
	ActionRepeat:              "Repeat",
	ActionLoop:                "Loop",
	ActionWait:                "Wait",
}

// AbilityStatDef is one row offered by the Ability Stats editor: a stat id, the
// label to show, and (for a scoped row) the action it narrows to.
//
// ID is either the bare kind ("duration" — every kinded duration field in the
// program) or "<actionType>.<kind>" ("create_zone.duration" — only that action's
// duration). Both levels pool ADDITIVELY into the same two accumulators:
//
//	value = (base + Σflat_broad + Σflat_scoped) × (1 + Σpct_broad + Σpct_scoped)
//
// so a unit carrying "+2s duration" and an item carrying "+15% zone duration"
// compose the obvious way, and neither has to know the other exists.
type AbilityStatDef struct {
	ID     string     `json:"id"`
	Label  string     `json:"label"`
	Kind   string     `json:"kind"`
	Action ActionType `json:"action,omitempty"`
	// FlatOnly tells the editor to offer a Flat column ONLY for this row — a
	// percentage is meaningless for a whole quantity (see
	// abilityStatKindAllowsPct). Surfaced rather than re-derived client-side so
	// the rule lives in one place.
	FlatOnly bool `json:"flatOnly,omitempty"`
}

// scopedAbilityStatID builds the "<actionType>.<kind>" form.
func scopedAbilityStatID(action ActionType, kind string) string {
	return string(action) + "." + kind
}

// AbilityStatDefs returns every stat row the editor can offer: the broad grid
// kinds first, then one scoped row per (action, kind) pair that ACTUALLY EXISTS
// in the action registry, sorted for determinism.
//
// Deriving the scoped rows from the registry rather than hand-listing them is
// the point: an action registered tomorrow with a kinded duration field gets its
// own "X Duration" stat with no further authoring, and a stat can never be
// offered for a field that does not exist.
func AbilityStatDefs() []AbilityStatDef {
	// Which grid kinds are actually REACHABLE — i.e. at least one registered
	// action declares a field of that kind. Offering "Speed +15%" while no field
	// carries abilityStatKindSpeed would be the same "advertises a capability
	// that does not exist" bug SchemaField declarations were introduced to kill
	// (see launch_projectile's vortex-knob comment), so the grid is derived, not
	// hand-listed. A kind becomes offerable the moment a field claims it.
	reachable := map[string]bool{}
	for _, desc := range actionRegistry {
		for _, f := range desc.Schema.Fields {
			if f.Kind != "" {
				reachable[f.Kind] = true
			}
		}
	}

	out := make([]AbilityStatDef, 0, len(abilityStatGridKinds)+len(actionRegistry))
	for _, kind := range abilityStatGridKinds {
		if !reachable[kind] {
			continue
		}
		out = append(out, AbilityStatDef{
			ID: kind, Label: abilityStatKindLabels[kind], Kind: kind,
			FlatOnly: !abilityStatKindAllowsPct(kind),
		})
	}

	var scoped []AbilityStatDef
	for actionType, desc := range actionRegistry {
		seen := map[string]bool{}
		for _, f := range desc.Schema.Fields {
			if f.Kind == "" || seen[f.Kind] || !isAbilityStatGridKind(f.Kind) {
				continue
			}
			seen[f.Kind] = true
			qualifier := actionStatQualifier[actionType]
			if qualifier == "" {
				qualifier = string(actionType)
			}
			scoped = append(scoped, AbilityStatDef{
				ID:       scopedAbilityStatID(actionType, f.Kind),
				Label:    qualifier + " " + abilityStatKindLabels[f.Kind],
				Kind:     f.Kind,
				Action:   actionType,
				FlatOnly: !abilityStatKindAllowsPct(f.Kind),
			})
		}
	}
	sort.Slice(scoped, func(i, j int) bool { return scoped[i].ID < scoped[j].ID })
	return append(out, scoped...)
}

// isAbilityStatGridKind reports whether kind is offered as a broad grid row (and
// therefore also gets scoped rows). Damage/heal are kinded for the field picker
// but are served by abilityPower/abilityDamage, so they contribute no rows.
func isAbilityStatGridKind(kind string) bool {
	for _, k := range abilityStatGridKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// abilityStatKindIsIntegral reports whether a field of this kind is a WHOLE
// QUANTITY — a number of bounces, projectiles, summons. Two consequences follow,
// and they are really the same fact stated twice:
//
//  1. It must be ROUNDED after a fold. loop.iterations and summon_unit.count are
//     Go `int` fields; a config carrying 3.45 fails encoding/json's decode
//     outright ("cannot unmarshal number 3.45 into Go struct field ... of type
//     int"), which makes executeActionLocked bail with a validation_error and
//     skip the action entirely. An unrounded count stat would therefore DELETE a
//     loop rather than lengthen it.
//
//  2. It accepts FLAT contributions only — see abilityStatKindAllowsPct.
//
// Radius and duration are float64 fields and stay fractional on purpose: a 63.25
// radius is meaningful, and rounding them would quantise small percentage
// bonuses away to nothing.
func abilityStatKindIsIntegral(kind string) bool {
	switch kind {
	case abilityStatKindCount, abilityStatKindDamage, abilityStatKindHeal:
		return true
	default:
		return false
	}
}

// abilityStatKindAllowsPct reports whether a percentage contribution is
// meaningful for this kind.
//
// It is not, for a whole quantity. Real counts in this game are small — 3
// bounces, 2 summons — so a percentage is either a NO-OP or a CLIFF, with
// nothing in between: +15% of 3 is 3.45, which rounds straight back to 3 and
// does nothing at all, while +50% is 4.5, which rounds to 5 and adds two. A
// designer authoring "+15% count" would reasonably expect *something*, and gets
// silence. Flat is the only honest unit for these, so a pct on a count-like stat
// is a LOAD ERROR rather than a value that quietly rounds to nothing.
func abilityStatKindAllowsPct(kind string) bool { return !abilityStatKindIsIntegral(kind) }

// isKnownAbilityStatKind reports whether kind is a registered Kind at all.
func isKnownAbilityStatKind(kind string) bool {
	_, ok := abilityStatKindLabels[kind]
	return ok
}
