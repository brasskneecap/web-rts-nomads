package game

// statusIconIDs returns the curated list of overhead-icon ids apply_status's
// Icon field may be set to — the "icon" ProgramEnums() key, resolved by
// SchemaField.vue's forward-compat rule (a same-named key in the enums
// bundle) against the apply_status action's "icon" schema field
// (ability_exec_actions.go).
//
// SOURCE OF TRUTH INVESTIGATION (see this task's report for the full
// writeup): overhead status icons are NOT server-simulated — they are raw
// ids the client looks up client-side in ACTION_ICON_MAP, which is built
// from GET /catalog/action-icons (ListActionIconDefs, action_icon_defs.go),
// itself embedded from catalog/action-icons.json. That file is a FIXED,
// curated SVG-path catalog (not arbitrary strings, not per-ability
// uploadable) — every entry is hand-authored there before an ability/perk
// can reference it. Filtered here to ids carrying the "debuff-" or "buff-"
// prefix: these are the icons actually AUTHORED for the small 12px overhead
// circle (see drawUnitActiveBuffs/drawUnitActiveDebuffs, CanvasRenderer.ts);
// the much larger "perk-"/other-prefixed set in the same file is sized and
// authored for the selection-panel perk slots instead. This list is NOT
// exhaustive of every legal string apply_status.Icon could technically
// carry (any id present in action-icons.json renders, prefix or not) — it's
// the best-available curated subset an author should pick from today. A
// designer wanting a genuinely new overhead icon still needs a new SVG path
// added to action-icons.json (a catalog-data change, not a Go change) before
// it appears here.
func statusIconIDs() []string {
	var ids []string
	for _, def := range ListActionIconDefs() {
		if len(def.ID) > 7 && def.ID[:7] == "debuff-" {
			ids = append(ids, def.ID)
			continue
		}
		if len(def.ID) > 5 && def.ID[:5] == "buff-" {
			ids = append(ids, def.ID)
		}
	}
	return ids
}

// ProgramEnums returns the string value sets the editor's enum/multiselect controls
// draw from, sourced from the composable-model enum consts. actionTypes reuses
// allActionTypes directly and triggerTypes reuses allTriggerTypes directly
// (no parallel lists to drift).
func ProgramEnums() map[string][]string {
	actionTypeStrs := make([]string, len(allActionTypes))
	for i, t := range allActionTypes {
		actionTypeStrs[i] = string(t)
	}
	triggerTypeStrs := make([]string, len(allTriggerTypes))
	for i, t := range allTriggerTypes {
		triggerTypeStrs[i] = string(t)
	}
	damageCategoryStrs := make([]string, len(allDamageCategories))
	for i, c := range allDamageCategories {
		damageCategoryStrs[i] = string(c)
	}
	return map[string][]string{
		"entryTypes": {
			string(EntrySelf), string(EntryUnit), string(EntryGroundPoint),
			string(EntryDirection), string(EntryNoTarget), string(EntryPassive),
		},
		"relations": {
			string(RelSelf), string(RelAlly), string(RelEnemy), string(RelNeutral),
		},
		"triggerTypes": triggerTypeStrs,
		"actionTypes":  actionTypeStrs,
		// damageCategories backs the on_damage_dealt trigger's DamageScope.Categories
		// authoring control (ability_program.go). See allDamageCategories'
		// doc comment (damage_pipeline.go) for why DamageCategoryUnspecified
		// is deliberately excluded.
		"damageCategories": damageCategoryStrs,
		"targetSources": {
			string(SrcCaster), string(SrcInitialTarget), string(SrcPrevActionTargets),
			string(SrcCurrentEvent), string(SrcNamedContext), string(SrcSourceObject), string(SrcAllInScene),
		},
		"targetOrigins": {
			string(OriginCaster), string(OriginInitialTarget), string(OriginInitialTargetPos),
			string(OriginCastPoint), string(OriginImpactPosition), string(OriginCurrentEventPos),
			string(OriginProjectilePos), string(OriginZoneCenter), string(OriginStatusOwner),
			string(OriginSummonedUnit), string(OriginNamedContextValue), string(OriginTargetsCenter),
		},
		"targetOrderings": {
			string(OrderClosest), string(OrderFarthest), string(OrderLowestHealth),
			string(OrderLowestHealthPct), string(OrderHighestHealth), string(OrderRandom), string(OrderUnitID),
		},
		"zoneAnchors": {
			string(ZoneAnchorGround), string(ZoneAnchorUnit), string(ZoneAnchorObject),
		},
		"conditionOps": {
			"eq", "ne", "lt", "lte", "gt", "gte", "has", "not",
		},
		// "icon" backs apply_status's own "icon" schema field by forward-compat
		// name match (SchemaField.vue's resolveOptionList) — see statusIconIDs'
		// doc comment for the full source-of-truth writeup.
		"icon": statusIconIDs(),
		// "iconKind" is published alongside "icon" for completeness/documentation
		// even though the apply_status schema field inlines these same two
		// values as static Options (small, fixed set — same convention as
		// "stacking"'s refresh/stack Options).
		"iconKind": {"buff", "debuff"},
	}
}
