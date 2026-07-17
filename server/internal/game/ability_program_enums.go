package game

// ProgramEnums returns the string value sets the editor's enum/multiselect controls
// draw from, sourced from the composable-model enum consts. actionTypes reuses
// allActionTypes directly (no parallel list to drift).
func ProgramEnums() map[string][]string {
	actionTypeStrs := make([]string, len(allActionTypes))
	for i, t := range allActionTypes {
		actionTypeStrs[i] = string(t)
	}
	return map[string][]string{
		"entryTypes": {
			string(EntrySelf), string(EntryUnit), string(EntryGroundPoint),
			string(EntryDirection), string(EntryNoTarget), string(EntryPassive),
		},
		"relations": {
			string(RelSelf), string(RelAlly), string(RelEnemy), string(RelNeutral),
		},
		"triggerTypes": {
			string(TriggerOnCastStart), string(TriggerOnCastComplete), string(TriggerOnAnimationMarker),
			string(TriggerOnProjectileImpact), string(TriggerOnZoneTick), string(TriggerOnZoneEnter),
			string(TriggerOnZoneExit), string(TriggerOnStatusTick), string(TriggerOnStatusExpire),
			string(TriggerOnTargetHit), string(TriggerOnDamageDealt), string(TriggerOnUnitDeath),
			string(TriggerOnActionComplete), string(TriggerOnChargeFull), string(TriggerCustom),
		},
		"actionTypes": actionTypeStrs,
		"targetSources": {
			string(SrcCaster), string(SrcInitialTarget), string(SrcPrevActionTargets),
			string(SrcCurrentEvent), string(SrcNamedContext), string(SrcSourceObject), string(SrcAllInScene),
		},
		"targetOrigins": {
			string(OriginCaster), string(OriginInitialTarget), string(OriginInitialTargetPos),
			string(OriginCastPoint), string(OriginImpactPosition), string(OriginCurrentEventPos),
			string(OriginProjectilePos), string(OriginZoneCenter), string(OriginStatusOwner),
			string(OriginSummonedUnit), string(OriginNamedContextValue),
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
	}
}
