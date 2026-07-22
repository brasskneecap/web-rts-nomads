package game

// ═════════════════════════════════════════════════════════════════════════════
// Profile upgrade damage multiplier
// ═════════════════════════════════════════════════════════════════════════════

// applyProfileDamageMultiplierLocked multiplies rawDamage by the attacker
// owner's PhysicalDamageMultiplier or MagicDamageMultiplier, depending on
// the resolved damage type of the attack. Returns rawDamage unchanged when
// the attacker is the enemy AI or neutral faction, or has no real player entry.
//
// Applied once per damage event, after crit and perk multipliers, before
// applyArmorMitigation. Must be called under s.mu lock.
func (s *GameState) applyProfileDamageMultiplierLocked(attacker *Unit, rawDamage float64) float64 {
	if attacker == nil {
		return rawDamage
	}
	// Skip virtual AI/neutral players — they are never profile-upgraded.
	if attacker.OwnerID == enemyPlayerID || attacker.OwnerID == neutralPlayerID {
		return rawDamage
	}
	player, ok := s.Players[attacker.OwnerID]
	if !ok {
		return rawDamage
	}
	resolvedType := attacker.AttackDamageType.OrPhysical()
	if resolvedType == DamagePhysical {
		return rawDamage * player.PhysicalDamageMultiplier
	}
	return rawDamage * player.MagicDamageMultiplier
}

// ═════════════════════════════════════════════════════════════════════════════
// Centralized death pipeline
//
// Problem: indirect damage paths (Shared Pain, pain_share redirect,
// retaliation) kill units other than the primary target. The outer call site
// only checks HP on the primary, so secondarily-killed units sat at HP=0
// forever — the regen loop skips them (HP>0 gate) and they were never cleaned
// up. Players saw standing corpses.
//
// Solution: every damage entry point calls enqueueDeathLocked when a unit
// reaches HP<=0. drainPendingDeathsLocked runs once per tick from Update(),
// after all combat/trap/projectile ticks finish. It handles kill bookkeeping
// for attributed kills and removeUnitLocked for all enqueued deaths.
//
// Dedup rule: first-to-kill wins. pendingDeathsSet prevents double-XP for the
// same unit from both the call-site manual bookkeeping and the drain.
// ═════════════════════════════════════════════════════════════════════════════

// DamageCategory is the CLOSED, semantic classification of where a damage
// instance came from. Unlike Kind (a freeform human/telemetry label, below),
// this is the field gameplay logic may branch on — e.g. an ability trigger
// scoped to "basic attacks only" or "this specific ability" (the on_damage_
// dealt trigger's filter, composable-abilities design). Kind stays freeform
// text for debugging/telemetry; Category is the typed vocabulary a designer-
// facing filter can trust.
//
// DamageCategoryUnspecified (the zero value) is NOT a meaningful default —
// it means "this call site has not been classified yet," a gap to close, not
// "no category." Every DamageSource literal in production code should set a
// real category; see the classification sites listed on each constant below
// and damage_pipeline_category_test.go for the paths that are characterized
// end-to-end.
type DamageCategory string

const (
	// DamageCategoryUnspecified is the zero value. Set only by call sites that
	// have not been classified (see the task's report for the current list —
	// today: applyProjectileDamageLocked's generic single-target helper,
	// projectile_defs.go, which is unreachable from any production call site
	// and carries no signal to distinguish a basic attack from an ability) and
	// by the deliberately-anonymous DamageSource{} literal
	// (applyUnitDamageLocked, perks_defense.go) used by legacy call sites that
	// do their own kill bookkeeping.
	DamageCategoryUnspecified DamageCategory = ""
	// DamageCategoryBasicAttack is a unit's ordinary attack: melee swings,
	// ranged auto-attack arrows/bolts (including a pierce-shaped arrow — the
	// "pierce" perk changes the ARROW'S GEOMETRY, not what kind of damage it
	// is; every pierce hit lands through the same resolveAttackHitLocked hub
	// as a normal swing), and base-stat splash (a unit's own SplashRadius
	// property, e.g. raider_brute — inherent to the attack itself, not a
	// perk-granted bonus hit).
	DamageCategoryBasicAttack DamageCategory = "basic_attack"
	// DamageCategoryAbility is damage dealt by a cast/channeled/commander
	// ability: composable deal_damage actions, legacy instant-hit and AoE
	// ability resolution, ability-sourced projectiles and beams (including
	// chain_lightning's bounce hops, which route through the equipment-proc
	// beam mechanism but carry SourceAbilityID), and player commander
	// abilities (no unit attribution, but still an authored spell effect —
	// not a basic attack, trap, building, perk, or item).
	DamageCategoryAbility DamageCategory = "ability"
	// DamageCategoryTrap is damage dealt by a trap's own mechanics: zone DoT,
	// infusion bonus ticks, detonation bursts, Cataclysm/Reactive Flames/
	// Final Exposure/Overload payloads, and silver-tier trap burn stacks.
	// Distinct from an equipment weapon's burn stack (DamageCategoryItem),
	// which shares the same BurnStacks machinery but originates from gear,
	// not a trap.
	DamageCategoryTrap DamageCategory = "trap"
	// DamageCategoryBuilding is damage dealt by a building's own attack
	// (towers/keeps acquiring and firing on hostiles in range).
	DamageCategoryBuilding DamageCategory = "building"
	// DamageCategoryPerk is a NEW damage instance created by a perk hook
	// reacting to combat — a bonus hit layered on top of (not identical to)
	// the attack/ability that triggered it: savage_strikes, whirlwind,
	// cleave, explosive_tips, retaliation's reflected counter-hit, and
	// divine_judgement's AoE proc off a heal event. Contrast with a
	// REDIRECT/PROPAGATION of an existing instance (pain_share,
	// shared_pain), which is not its own category — see those calls sites'
	// comments for why they instead forward the origin's own Category.
	DamageCategoryPerk DamageCategory = "perk"
	// DamageCategoryItem is damage dealt by equipment: on-hit elemental
	// instances, rolled on-hit/on-struck procs (bolts and beams), and a
	// weapon's on-hit burn stack (e.g. fire_sword).
	DamageCategoryItem DamageCategory = "item"
)

// DamageSource identifies who caused a damage event for kill attribution.
// The zero value (anonymous) means the call site is doing its own kill
// bookkeeping — the drain will only do removal, not XP/stats.
type DamageSource struct {
	AttackerUnitID     int    // 0 if not from a unit
	AttackerBuildingID string // "" if not from a building
	AttackerTrapID     string // "" if not from a trap
	// Kind is a HUMAN-READABLE label used ONLY for debugging/telemetry —
	// examples: "melee", "projectile", "building", "savage_strikes",
	// "whirlwind", "cleave", "shared_pain", "pain_share_redirect",
	// "retaliation". It is freeform (~25 ad-hoc values in practice) and MUST
	// NOT be branched on by gameplay logic — Category (above) is the typed
	// field for that. The one pre-existing exception is
	// perks_defense.go's sanctuary check (src.Kind == "projectile"), kept
	// working as-is; do not add new logic keyed on Kind.
	Kind string
	// Category is the CLOSED classification gameplay logic should branch on
	// (DamageCategory, above). The zero value (DamageCategoryUnspecified)
	// means the call site hasn't been classified yet — a gap, not a
	// meaningful default. Every pre-existing DamageSource{} literal keeps
	// compiling and behaving exactly as before (nothing reads this field
	// yet); it is metadata only until a consumer is built.
	Category DamageCategory
	// DamageType is the element / school of this damage event, set from the
	// attacker's attack or the ability definition (NOT the projectile). The
	// zero value means "unspecified"; ResolvedDamageType() maps it to
	// DamagePhysical so the many existing DamageSource{} call sites keep
	// behaving as physical with no edits. Flavor/metadata only today —
	// see damage_type.go.
	DamageType DamageType
	// SuppressTypeHint stops applyUnitDamageWithSourceLocked from auto-emitting
	// the major-popup color hint for this instance. Set by callers that render
	// their own SEPARATE popup for the damage (e.g. equipment on-hit elemental,
	// which sprays a minor side-popup instead of tinting the main number). If
	// the hint were still emitted, its (unitID, amount) entry would mis-color
	// the physical remainder of the same-tick HP-diff.
	SuppressTypeHint bool
	// SourceAbilityID names the composable ability whose damage this instance
	// carries — "" means either "not from an ability" (a basic attack, a trap,
	// a building, an item proc, a perk-triggered counter like retaliation) or
	// "from an ability, but attribution wasn't threaded at this call site".
	// Widened the same way DamageType was (see its doc comment): the zero
	// value keeps every pre-existing DamageSource{} literal compiling and
	// behaving exactly as before.
	//
	// This is the sole attribution drainPendingDeathsLocked reads to decide
	// "was this unit killed BY a specific ability" for on_unit_death
	// (fireOnUnitDeathLocked, ability_unit_death.go) — the semantics the
	// composable-abilities design settled on for that trigger: it means "a
	// unit killed BY this ability", not merely "an ability was involved
	// upstream of this tick".
	//
	// THREADED at: deal_damage's Execute (ability_program_registry.go, from
	// ctx.AbilityID — the executing program's own id), landProjectileLocked's
	// SkipOnHitEffects branch (from Projectile.SourceAbilityID, which
	// launch_projectile/launch_vortex already stamp at spawn), the arcane_orb
	// vortex DoT tick (applyAbilitySplashDamageLocked's sourceAbilityID
	// param), the siphon_life channel tick + its chain/echo fan-outs
	// (ability_channel.go, perks_siphoner.go — all already had the ability id
	// in scope as unit.ChannelAbilityID / a passed-through abilityID param),
	// and chain_lightning's bounce delivery (fireAbilityChainLocked,
	// ability_cast.go, stamping def.ID onto ProcSource.SourceAbilityID, which
	// flows through fireProcBeamLocked → Beam.SourceAbilityID →
	// applyBeamPendingDamageLocked, beam.go — see ProcSource.SourceAbilityID's
	// doc comment, proc_effects.go, for the full thread). This closes what was
	// previously documented here as a KNOWN GAP: chain_lightning's bounce hops
	// (and any authored launch_projectile+chainCount ability) now attribute
	// bounce kills exactly like their primary-target kills. Equipment/item/
	// perk procs that ALSO route through fireProcBeamLocked (e.g. equipment's
	// lightning_chain proc) still stamp nothing — ProcSource.SourceAbilityID
	// is only ever set at the one ability call site above, so their
	// DamageSource.SourceAbilityID stays "" exactly as before.
	//
	// PROPAGATED (deliberately) through pain_share redirect
	// (perkRedirectIncomingDamageLocked, perks_auras.go) and Shared Pain
	// (perkShareDamageToMarkedLocked, trap.go): both already propagate
	// AttackerUnitID/AttackerBuildingID/AttackerTrapID "so if the absorbing
	// Vanguard dies, the kill credits the original attacker" — this is the
	// SAME instance of damage, just redirected/fanned-out to a different
	// victim, so it is still, transitively, this ability's damage. A Vanguard
	// killed by a redirected execute-ability hit correctly fires that
	// ability's on_unit_death.
	//
	// NOT propagated into retaliation's reflected counter-hit
	// (onPerkDamageTakenLocked's "retaliation" case, perks_defense.go): that
	// is a BRAND NEW damage instance the reflecting unit deals back with its
	// own armor stat — it was never "this ability's damage" to begin with
	// (retaliation already builds its DamageSource from scratch, crediting
	// the reflecting unit as AttackerUnitID, and never reads src at all).
	//
	// DELIBERATELY LEFT EMPTY at every non-ability damage entry point
	// (basic-attack melee/splash/pierce, buildings, traps, item-procs,
	// equipment elemental on-hit) — those sites have no ability id to carry
	// and must keep reading as "not from an ability".
	//
	// KNOWN GAP: chain_lightning's bounce delivery (fireAbilityChainLocked,
	// ability_cast.go, called from both the legacy resolver and
	// ability_exec_projectile.go's launch_projectile Execute) routes through
	// the equipment-proc beam-bounce mechanic (executeProcEffectLocked →
	// fireProcBeamLocked → Beam → applyBeamPendingDamageLocked), which has no
	// ability-attribution field on ProcSource/Beam today. The one call site
	// that would supply it sits inside ability_cast.go, which was off-limits
	// for this change (a concurrent edit was in flight there) — see the
	// composable-abilities-on-unit-death task notes for the follow-up.
	SourceAbilityID string
}

// ResolvedDamageType returns the damage event's element, defaulting an unset
// DamageType to DamagePhysical. Read damage type through this rather than the
// raw field so "unspecified" is always explicit.
func (d DamageSource) ResolvedDamageType() DamageType {
	return d.DamageType.OrPhysical()
}

// allDamageCategories is the canonical list of every REAL (authorable)
// DamageCategory — deliberately excluding DamageCategoryUnspecified, which is
// a gap marker ("this call site hasn't been classified yet"), never a
// meaningful filter value an author could intend. isKnownDamageCategory
// derives from it so the two cannot drift, mirroring allTriggerTypes /
// isKnownTriggerType (ability_program_validate.go). First (only, as of this
// writing) consumer: DamageTriggerScope.Categories validation
// (ability_program_validate.go).
var allDamageCategories = []DamageCategory{
	DamageCategoryBasicAttack, DamageCategoryAbility, DamageCategoryTrap,
	DamageCategoryBuilding, DamageCategoryPerk, DamageCategoryItem,
}

// knownDamageCategories is the lookup set derived from allDamageCategories.
var knownDamageCategories = func() map[DamageCategory]bool {
	m := make(map[DamageCategory]bool, len(allDamageCategories))
	for _, c := range allDamageCategories {
		m[c] = true
	}
	return m
}()

// isKnownDamageCategory reports whether c is one of the six real DamageCategory
// enum consts. DamageCategoryUnspecified ("") is deliberately NOT known — see
// allDamageCategories' doc comment.
func isKnownDamageCategory(c DamageCategory) bool {
	return knownDamageCategories[c]
}

// IsAnonymous returns true when the source carries no attacker attribution.
// Anonymous deaths are still cleaned up by the drain but do not award XP.
func (d DamageSource) IsAnonymous() bool {
	return d.AttackerUnitID == 0 && d.AttackerBuildingID == "" && d.AttackerTrapID == ""
}

// pendingDeath is a single entry in the per-tick death queue.
type pendingDeath struct {
	UnitID int
	Source DamageSource
}

// enqueueDeathLocked records a unit that hit HP<=0 during the current tick.
// First-to-enqueue wins (the pendingDeathsSet prevents double-entries so the
// XP credit goes to whoever killed the unit first).
//
// Safe to call on a nil target or a still-alive target — both are no-ops.
// Must be called under s.mu write lock.
func (s *GameState) enqueueDeathLocked(target *Unit, src DamageSource) {
	if target == nil || target.HP > 0 {
		return
	}
	if s.pendingDeathsSet[target.ID] {
		return // first-to-kill wins; ignore subsequent enqueues this tick
	}
	s.pendingDeathsSet[target.ID] = true
	s.pendingDeaths = append(s.pendingDeaths, pendingDeath{UnitID: target.ID, Source: src})
}

// unitIsAliveLocked is THE definition of "this unit is still a living host" —
// the question anything attached to a unit (a status, a status's visual, an
// aura the unit projects) must ask before it keeps running.
//
// It is deliberately stricter than a bare HP check, and it is the seam to change
// when units stop leaving the field the instant they die. A corpse that lingers
// as a `*Unit` — a death animation, a lootable body, a raiseable skeleton — is
// STILL NOT ALIVE, and every attached-to-a-unit system should stop the moment
// this returns false rather than each site inventing its own test. Today the
// three conditions are:
//
//   - the unit is gone from the registry entirely, or
//   - its HP has reached zero, or
//   - it is queued in this tick's pendingDeaths but not yet drained (the window
//     between taking lethal damage and being removed, which is most of an
//     Update pass — see drainPendingDeathsLocked's placement).
//
// A future explicit dead/alive flag belongs HERE, not at the call sites.
//
// Takes an already-resolved *Unit: it is a within-tick working value, matching
// the project's target-resolution convention. Must be called under s.mu.
func (s *GameState) unitIsAliveLocked(u *Unit) bool {
	return u != nil && !u.Dead && u.HP > 0 && !s.pendingDeathsSet[u.ID]
}

// drainPendingDeathsLocked processes the per-tick death queue built up by
// applyUnitDamageWithSourceLocked. For each entry:
//
//   - If the unit is already gone (call site ran removeUnitLocked itself before
//     the drain), skip — that call site already handled XP/stats. This is the
//     safe coexistence path for legacy call sites.
//   - If still present with HP<=0, run full kill bookkeeping using the
//     DamageSource attribution, then killUnitToCorpseLocked.
//   - If HP>0 (re-healed — hypothetical; no revive perk exists yet), skip.
//
// Must be called once per tick from Update(), AFTER all combat/trap/projectile
// ticks have run and BEFORE the per-unit loop that assumes dead units are gone.
// Placing it here prevents HP=0 units from entering the per-unit regen loop.
//
// Determinism: we iterate over the slice (insertion order). The set is only
// used for membership checks — it is never iterated.
func (s *GameState) drainPendingDeathsLocked() {
	if len(s.pendingDeaths) == 0 {
		return
	}
	// Snapshot and reset the queue so any re-entrant kills (none expected, but
	// defensively) would land in a fresh queue rather than extending our loop.
	deaths := s.pendingDeaths
	s.pendingDeaths = nil
	s.pendingDeathsSet = make(map[int]bool)

	for _, d := range deaths {
		target := s.getUnitByIDLocked(d.UnitID)
		if target == nil {
			// Already removed by the primary call site — skip.
			continue
		}
		if target.HP > 0 {
			// Re-healed before drain (no such perk exists today, but be safe).
			continue
		}

		// Siphoner repurposed_life: fire mana restore for every Siphoner that
		// was actively channeling Siphon Life on this victim at the moment of
		// death, regardless of who landed the killing blow. Called BEFORE
		// removeUnitLocked below so the dying unit's channel-target back-refs
		// on Siphoners are still resolvable. No-op when no Siphoner has the
		// perk + the dying unit as channel target.
		s.onSiphonVictimDeathLocked(target)

		// killerOwnerID is who landed the killing blow (empty when anonymous /
		// unresolved). Used below to gate neutral-camp loot: a camp wiped by the
		// enemy wave faction drops nothing.
		killerOwnerID := ""

		if !d.Source.IsAnonymous() {
			// Resolve attacker and run kill bookkeeping.
			if d.Source.AttackerUnitID != 0 {
				attackerUnit := s.getUnitByIDLocked(d.Source.AttackerUnitID)
				if attackerUnit != nil {
					killerOwnerID = attackerUnit.OwnerID
					s.awardUnitDeathXPLocked(target, attackerUnit)
					s.awardSoldierTankKillXPLocked(target.ID)
					s.onPerkKillLocked(attackerUnit)
					s.trackBattleKillLocked(battleSourceFromUnit(attackerUnit), target)
					s.rollDominionPointDropLocked(attackerUnit.OwnerID, target)
					// Legacy markObjectiveKillLocked(target.ObjectiveID) call
					// removed in §9 of campaign-objectives-and-metrics. Kill
					// counters now live on Player.Metrics and feed the new
					// objective evaluator (§8) directly.
					s.recordEnemyKillMetricLocked(attackerUnit.OwnerID, target.OwnerID)
				}
			} else if d.Source.AttackerBuildingID != "" {
				building := s.getBuildingByIDLocked(d.Source.AttackerBuildingID)
				if building != nil {
					s.trackBattleKillLocked(battleSourceFromBuilding(building), target)
					// Legacy markObjectiveKillLocked(target.ObjectiveID) call
					// removed in §9 of campaign-objectives-and-metrics. Kill
					// counters now live on Player.Metrics and feed the new
					// objective evaluator (§8) directly.
					if building.OwnerID != nil {
						killerOwnerID = *building.OwnerID
						s.recordEnemyKillMetricLocked(*building.OwnerID, target.OwnerID)
					}
				}
			} else if d.Source.AttackerTrapID != "" {
				// Resolve trap and its owner unit — mirrors the pattern used
				// in detonateExplosiveTrapLocked and other trap kill paths.
				var trap *Trap
				for _, t := range s.Traps {
					if t != nil && t.ID == d.Source.AttackerTrapID {
						trap = t
						break
					}
				}
				if trap != nil {
					ownerUnit := s.getUnitByIDLocked(trap.OwnerUnitID)
					if ownerUnit != nil && ownerUnit.HP <= 0 {
						ownerUnit = nil
					}
					if ownerUnit != nil {
						killerOwnerID = ownerUnit.OwnerID
						s.awardUnitDeathXPLocked(target, ownerUnit)
						s.awardSoldierTankKillXPLocked(target.ID)
						s.trackBattleKillLocked(battleSourceFromTrap(trap), target)
						s.recordEnemyKillMetricLocked(ownerUnit.OwnerID, target.OwnerID)
					} else {
						// Trapper died; still track the kill under the trap source
						// for battle telemetry. No metric credit when the trapper
						// is dead — there's no surviving owner to attribute to.
						s.trackBattleKillLocked(battleSourceFromTrap(trap), target)
					}
					// Legacy markObjectiveKillLocked(target.ObjectiveID) call
					// removed in §9 of campaign-objectives-and-metrics. Kill
					// counters now live on Player.Metrics and feed the new
					// objective evaluator (§8) directly.
				}
			}
		}
		// Metrics: a ranked unit's death drops the owner's UnitsByRank counts
		// (semantic: "at-or-above this rank"). Recompute before remove so the
		// dying unit (HP <= 0) is excluded by the scan.
		if target.Rank != unitRankBase {
			s.recomputeUnitsByRankForOwnerLocked(target.OwnerID)
		}
		// Neutral-camp loot gating: record whether the enemy faction landed this
		// kill BEFORE removeUnitLocked fires the camp's 0-units loot hook
		// (onUnitRemovedFromCampLocked → maybeDropChestForCampLocked).
		if target.NeutralCampID != "" {
			s.markCampKillerLocked(target.NeutralCampID, killerOwnerID)
		}
		// Composable on_unit_death: fire the killing ability's trigger(s), if
		// any (see DamageSource.SourceAbilityID's doc comment for exactly what
		// "killed by this ability" covers). MUST run before removeUnitLocked —
		// the dying unit is still resolvable by ID here (HP<=0, still in
		// s.Units), which is what lets an authored trigger's
		// select_targets{source:"current_event"} bind the corpse. A peer to
		// the reactions above, not a replacement for any of them.
		s.fireOnUnitDeathLocked(target, d.Source)
		// Anonymous or after bookkeeping: the unit becomes a CORPSE — torn
		// down exactly as a removal would tear it down, but left on the field
		// to decay (docs/design/death_and_corpses.md). It is still resolvable
		// by ID from here on, which is what a later revive/raise needs; every
		// system that must not act on it asks unitIsAliveLocked.
		s.killUnitToCorpseLocked(target)
	}
}
