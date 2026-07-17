package game

// ═════════════════════════════════════════════════════════════════════════════
// FROZEN LEGACY FIXTURES — composable-abilities migration (2026-07)
//
// heal / greater_heal / shatter / raise_skeleton / meteor / arcane_bolt /
// fireball / chain_lightning / arcane_orb / arcane_missiles / siphon_life
// were migrated from flat-field (legacy) AbilityDefs to schemaVersion:2 + an
// authored Program (see catalog/abilities/<id>/<id>.json and
// ConvertLegacyAbility). siphon_life was the last: every catalog ability is
// now schemaVersion:2.
// Once the catalog def is v2, getAbilityDef(id) no longer returns a legacy
// shape — its mechanic fields (HealAmount, DamageAmount, Radius,
// SlowMultiplier, SummonCount, ImpactDelaySeconds, Burn*, ChargeRequired,
// ...) are all cleared to zero, per the SchemaVersion 2 contract (the
// Program is the sole authority).
//
// Any test that wants to exercise/compare against the LEGACY per-field
// resolution path (resolveAbilityCastLocked's flat-field branch,
// compileLegacyAbility's compiler, describeLegacyAbility's prose generator)
// for one of these ten abilities can no longer read it live off the catalog
// — that state doesn't exist anymore. The functions below are a byte-for-byte
// snapshot of each ability's pre-migration catalog JSON, frozen as Go struct
// literals, purely so those legacy code paths still have something concrete
// to run against in tests.
//
// ⚠ POINT-IN-TIME GUARANTEE, NOT A LIVE SPEC ⚠
// These fixtures capture the ability as it was BALANCED at migration time.
// They are not re-derived from the catalog (the catalog no longer has this
// data) and they are not meant to track future rebalancing. If someone
// intentionally changes one of these five abilities' numbers by editing its
// shipped v2 Program (e.g. tuning greater_heal's heal amount or meteor's
// burn tick damage), the corresponding fixture below goes stale — tests
// comparing against it (the golden equivalence tests, the frozen-compiler-
// shape tests) will then fail. That failure is EXPECTED and correct: it is
// not a regression to "fix" by reverting the balance change. Update the
// fixture's numbers to match the new intended values instead, exactly as you
// would update a hardcoded balance number anywhere else — these fixtures are
// the one deliberate, documented exception to the "no hardcoded balance
// numbers in tests" convention, because their entire purpose is pinning a
// specific historical snapshot for an equivalence proof.
// ═════════════════════════════════════════════════════════════════════════════

// legacyHealFixture is catalog/abilities/heal/heal.json as it read before the
// schemaVersion:2 migration.
func legacyHealFixture() AbilityDef {
	return AbilityDef{
		ID:                     "heal",
		DisplayName:            "Heal",
		Type:                   AbilitySpell,
		Category:               AbilityCategoryHeal,
		TargetCount:            1,
		ManaCost:               5,
		HealAmount:             10,
		CastRange:              CastRangeMatchAttackRange,
		CastTime:               1.0,
		Cooldown:               0,
		DamageType:             DamageHoly,
		CanTargetSelf:          true,
		CanTargetAllies:        true,
		CanTargetEnemies:       false,
		CasterAnimation:        "Casting",
		EffectOnTarget:         "healing_glow",
		Icon:                   "TODO/abilities/heal.png",
		SupportsAutoCast:       true,
		AutoCastTargetSelector: "lowest_hp_percentage_ally_in_range",
		DefaultAutoCast:        true,
	}
}

// legacyGreaterHealFixture is catalog/abilities/greater_heal/greater_heal.json
// as it read before the schemaVersion:2 migration.
func legacyGreaterHealFixture() AbilityDef {
	return AbilityDef{
		ID:                     "greater_heal",
		DisplayName:            "Greater Heal",
		Type:                   AbilitySpell,
		Category:               AbilityCategoryHeal,
		TargetCount:            3,
		ManaCost:               10,
		HealAmount:             15,
		CastRange:              CastRangeMatchAttackRange,
		CastTime:               1.0,
		Cooldown:               3,
		DamageType:             DamageHoly,
		CanTargetSelf:          true,
		CanTargetAllies:        true,
		CanTargetEnemies:       false,
		CasterAnimation:        "Casting",
		EffectOnTarget:         "healing_glow",
		Icon:                   "TODO/abilities/greater_heal.png",
		SupportsAutoCast:       true,
		AutoCastTargetSelector: "lowest_hp_percentage_ally_in_range",
		DefaultAutoCast:        true,
	}
}

// legacyShatterFixture is catalog/abilities/shatter/shatter.json as it read
// before the schemaVersion:2 migration.
func legacyShatterFixture() AbilityDef {
	return AbilityDef{
		ID:                     "shatter",
		DisplayName:            "Shatter",
		Type:                   AbilitySpell,
		Category:               AbilityCategoryOffensive,
		ManaCost:               18,
		DamageAmount:           55,
		Radius:                 110,
		CastRange:              400,
		CastTime:               0,
		Cooldown:               7,
		DamageType:             DamageCold,
		SlowMultiplier:         0.5,
		SlowDurationSeconds:    3,
		Tags:                   []string{"aoe", "cc", "damage"},
		TargetsPoint:           true,
		CanTargetSelf:          false,
		CanTargetAllies:        false,
		CanTargetEnemies:       true,
		CasterAnimation:        "Attacking",
		EffectAtPoint:          "shatter",
		EffectScale:            1.8,
		SupportsAutoCast:       true,
		AutoCastTargetSelector: "closest_enemy_in_range",
		DefaultAutoCast:        true,
		Icon:                   "TODO/abilities/shatter.png",
	}
}

// legacyRaiseSkeletonFixture is
// catalog/abilities/raise_skeleton/raise_skeleton.json as it read before the
// schemaVersion:2 migration.
func legacyRaiseSkeletonFixture() AbilityDef {
	return AbilityDef{
		ID:                     "raise_skeleton",
		DisplayName:            "Raise Skeleton",
		Type:                   AbilitySpell,
		Category:               AbilityCategorySummon,
		CanTargetSelf:          true,
		CanTargetAllies:        false,
		CanTargetEnemies:       false,
		CastRange:              0,
		CastTime:               1.5,
		ManaCost:               30,
		Cooldown:               10,
		DamageType:             DamageShadow,
		CasterAnimation:        "Casting",
		SummonUnitType:         "skeleton_soldier",
		SummonCount:            3,
		SupportsAutoCast:       true,
		AutoCastTargetSelector: "self",
		DefaultAutoCast:        true,
	}
}

// legacyArcaneBoltFixture is catalog/abilities/arcane_bolt/arcane_bolt.json
// as it read before the schemaVersion:2 migration.
func legacyArcaneBoltFixture() AbilityDef {
	return AbilityDef{
		ID:                     "arcane_bolt",
		DisplayName:            "Arcane Bolt",
		Type:                   AbilitySpell,
		Category:               AbilityCategoryOffensive,
		ManaCost:               8,
		DamageAmount:           50,
		CastRange:              400,
		CastTime:               0.5,
		Cooldown:               2,
		DamageType:             DamageArcane,
		Projectile:             "arcane_bolt",
		ProjectileScale:        1.5,
		CanTargetSelf:          false,
		CanTargetAllies:        false,
		CanTargetEnemies:       true,
		CasterAnimation:        "Attacking",
		Icon:                   "TODO/abilities/arcane_bolt.png",
		SupportsAutoCast:       true,
		AutoCastTargetSelector: "closest_enemy_in_range",
		DefaultAutoCast:        true,
	}
}

// legacyFireballFixture is catalog/abilities/fireball/fireball.json as it
// read before the schemaVersion:2 migration.
func legacyFireballFixture() AbilityDef {
	return AbilityDef{
		ID:                     "fireball",
		DisplayName:            "Fireball",
		Type:                   AbilitySpell,
		Category:               AbilityCategoryOffensive,
		ManaCost:               18,
		DamageAmount:           90,
		Radius:                 100,
		CastRange:              400,
		CastTime:               0.6,
		Cooldown:               6,
		DamageType:             DamageFire,
		Projectile:             "fire_bolt",
		ProjectileScale:        2.5,
		Tags:                   []string{"aoe", "projectile", "damage"},
		CanTargetSelf:          false,
		CanTargetAllies:        false,
		CanTargetEnemies:       true,
		CasterAnimation:        "Attacking",
		Icon:                   "TODO/abilities/fireball.png",
		SupportsAutoCast:       true,
		AutoCastTargetSelector: "closest_enemy_in_range",
		DefaultAutoCast:        true,
	}
}

// legacyChainLightningFixture is
// catalog/abilities/chain_lightning/chain_lightning.json as it read before
// the schemaVersion:2 migration.
func legacyChainLightningFixture() AbilityDef {
	return AbilityDef{
		ID:                     "chain_lightning",
		DisplayName:            "Chain Lightning",
		Type:                   AbilitySpell,
		Category:               AbilityCategoryOffensive,
		ManaCost:               16,
		DamageAmount:           65,
		ChainCount:             2,
		BounceRange:            220,
		BounceDamageFalloff:    5,
		CastRange:              400,
		CastTime:               0.5,
		Cooldown:               5,
		DamageType:             DamageLightning,
		Projectile:             "lightning_bolt",
		Tags:                   []string{"chain", "damage"},
		CanTargetSelf:          false,
		CanTargetAllies:        false,
		CanTargetEnemies:       true,
		CasterAnimation:        "Attacking",
		Icon:                   "TODO/abilities/chain_lightning.png",
		SupportsAutoCast:       true,
		AutoCastTargetSelector: "closest_enemy_in_range",
		DefaultAutoCast:        true,
	}
}

// legacyArcaneOrbFixture is catalog/abilities/arcane_orb/arcane_orb.json as
// it read before the schemaVersion:2 migration.
func legacyArcaneOrbFixture() AbilityDef {
	return AbilityDef{
		ID:                     "arcane_orb",
		DisplayName:            "Arcane Orb",
		Type:                   AbilitySpell,
		Category:               AbilityCategoryOffensive,
		ManaCost:               20,
		DamagePerSecond:        16,
		Radius:                 130,
		PullStrength:           160,
		Projectile:             "arcane_orb",
		ProjectileSpeed:        150,
		ProjectileScale:        2.5,
		CastRange:              400,
		CastTime:               0,
		Cooldown:               8,
		DamageType:             DamageArcane,
		Tags:                   []string{"cc", "aoe"},
		TargetsPoint:           true,
		CanTargetSelf:          false,
		CanTargetAllies:        false,
		CanTargetEnemies:       false,
		CasterAnimation:        "Attacking",
		Icon:                   "TODO/abilities/arcane_orb.png",
		SupportsAutoCast:       true,
		AutoCastTargetSelector: "closest_enemy_in_range",
		DefaultAutoCast:        true,
	}
}

// legacyArcaneMissilesFixture is catalog/abilities/arcane_missiles/
// arcane_missiles.json as it read before the schemaVersion:2 migration.
func legacyArcaneMissilesFixture() AbilityDef {
	return AbilityDef{
		ID:                    "arcane_missiles",
		DisplayName:           "Arcane Missiles",
		Type:                  AbilityPassive,
		ChargeRequired:        30,
		ManaToChargeRatio:     1.0,
		MissileCount:          3,
		DamagePerMissile:      25,
		MissileDelayMs:        100,
		MinorDamage:           true,
		Projectile:            "arcane_missiles",
		ProjectileScale:       1.25,
		ProjectileSpeed:       280,
		DamageType:            DamageArcane,
		Targeting:             "random_enemy_in_range",
		AllowDuplicateTargets: true,
		Icon:                  "TODO/abilities/arcane_missiles.png",
	}
}

// legacySiphonLifeFixture is catalog/abilities/siphon_life/siphon_life.json
// as it read before the schemaVersion:2 migration.
func legacySiphonLifeFixture() AbilityDef {
	return AbilityDef{
		ID:                     "siphon_life",
		DisplayName:            "Siphon Life",
		Type:                   AbilitySpell,
		ChannelType:            "beam",
		CanTargetEnemies:       true,
		CastRange:              220,
		CastTime:               0,
		ManaCost:               0,
		Cooldown:               0,
		DamageType:             DamageShadow,
		DamageAmount:           0,
		Icon:                   "TODO/abilities/siphon_life.png",
		CasterAnimation:        "Casting",
		SupportsAutoCast:       true,
		AutoCastTargetSelector: "lowest_hp_percentage_enemy_in_range",
		DefaultAutoCast:        false,
		TickIntervalSeconds:    0.25,
		ManaCostPerTick:        1,
		DamagePerTick:          6,
		HealingMultiplier:      1.0,
		AllyHealRadius:         220,
	}
}

// legacyMeteorFixture is catalog/abilities/meteor/meteor.json as it read
// before the schemaVersion:2 migration.
func legacyMeteorFixture() AbilityDef {
	return AbilityDef{
		ID:                      "meteor",
		DisplayName:             "Meteor",
		Type:                    AbilitySpell,
		Category:                AbilityCategoryOffensive,
		ManaCost:                40,
		Cooldown:                12,
		CastTime:                0.8,
		CastRange:               400,
		DamageType:              DamageFire,
		TargetsPoint:            true,
		CanTargetSelf:           false,
		CanTargetAllies:         false,
		CanTargetEnemies:        true,
		DamageAmount:            140,
		Radius:                  230,
		ImpactDelaySeconds:      0.6,
		BurnDurationSeconds:     4.0,
		BurnDamagePerTick:       12,
		BurnTickIntervalSeconds: 0.5,
		BurnRadius:              120,
		CasterAnimation:         "Attacking",
		EffectAtPoint:           "meteor",
		BurnEffectAtPoint:       "burning_crater",
		EffectScale:             3.0,
		Tags:                    []string{"aoe", "damage", "dot"},
		SupportsAutoCast:        true,
		AutoCastTargetSelector:  "closest_enemy_in_range",
		DefaultAutoCast:         true,
		Icon:                    "TODO/abilities/meteor.png",
	}
}
