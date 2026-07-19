package game

import "testing"

// ═════════════════════════════════════════════════════════════════════════════
// Acceptance test — the user's authored frost_bolt split shape:
//
//	on_cast_complete -> launch_projectile
//	     on_projectile_impact -> select_targets(current_event) -> deal_damage
//	                          -> select_targets(all_in_scene, origin=current_event_position,
//	                                            relations=[enemy], radius=200, maxCount=2,
//	                                            excludeCurrentEvent=true)
//	                               on_action_complete -> launch_projectile(
//	                                   target: previous_action_targets,
//	                                   spawnOrigin=current_event_position)
//
// This builds that exact shape as a standalone test program (NOT a change to
// the shipped catalog/abilities/frost_bolt/frost_bolt.json — see the task's
// "no shipped ability may change" constraint) and proves, end to end:
//   - the primary bolt hits the initial target and deals damage
//   - the impact spawns exactly 2 split bolts
//   - both split bolts spawn AT the hit enemy's position (Gap 1), not the
//     caster's
//   - the split selection excludes the just-hit enemy itself (Gap 2) and an
//     out-of-radius bystander, landing on exactly the two OTHER nearby
//     enemies
//   - the original hit enemy is not re-hit by a split bolt
// ═════════════════════════════════════════════════════════════════════════════

// buildFrostBoltSplitProgram builds the user's split shape as an
// AbilityProgram: primaryDamage/splitDamage are this test's OWN authored
// config values (not a catalog balance number being asserted against), used
// both to configure the program and to check the exact damage each bolt
// dealt.
func buildFrostBoltSplitProgram(primaryDamage, splitDamage int) *AbilityProgram {
	splitImpact := AbilityTriggerDef{
		ID:   "splitimpact",
		Type: TriggerOnProjectileImpact,
		Actions: []AbilityActionDef{
			{ID: "selsplit", Type: ActionSelectTargets, Target: &TargetQueryDef{Source: SrcCurrentEvent}, Outputs: map[string]string{"targets": "splithit"}},
			{ID: "dmgsplit", Type: ActionDealDamage, Input: map[string]ContextRef{"targets": {Key: "splithit"}}, Config: marshalConfig(dealDamageConfig{Amount: splitDamage, Type: DamageCold})},
		},
	}
	relaunchCfg := launchProjectileConfig{
		Projectile:  "frost_bolt",
		SpawnOrigin: OriginCurrentEventPos, // Gap 1: split bolts spawn at the hit enemy, not the caster
		Triggers:    []AbilityTriggerDef{splitImpact},
	}
	primaryImpact := AbilityTriggerDef{
		ID:   "impact",
		Type: TriggerOnProjectileImpact,
		Actions: []AbilityActionDef{
			{ID: "sel", Type: ActionSelectTargets, Target: &TargetQueryDef{Source: SrcCurrentEvent}, Outputs: map[string]string{"targets": "hit"}},
			{ID: "dmg", Type: ActionDealDamage, Input: map[string]ContextRef{"targets": {Key: "hit"}}, Config: marshalConfig(dealDamageConfig{Amount: primaryDamage, Type: DamageCold})},
			{
				ID:   "splitsel",
				Type: ActionSelectTargets,
				Target: &TargetQueryDef{
					Source: SrcAllInScene, Origin: OriginCurrentEventPos,
					Relations: []TargetRelation{RelEnemy}, Radius: 200,
					ExcludeCurrentEvent: true, // Gap 2: don't re-select the enemy just hit
					MaxCount:            2,
					Ordering:            OrderClosest,
				},
				Outputs: map[string]string{"targets": "splits"},
				Children: []AbilityTriggerDef{
					{
						ID:   "onsplit",
						Type: TriggerOnActionComplete,
						Actions: []AbilityActionDef{
							{ID: "relaunch", Type: ActionLaunchProjectile, Input: map[string]ContextRef{"targets": {Key: "splits"}}, Config: marshalConfig(relaunchCfg)},
						},
					},
				},
			},
		},
	}
	rootCfg := launchProjectileConfig{
		Projectile: "frost_bolt",
		Triggers:   []AbilityTriggerDef{primaryImpact},
	}
	return &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryUnit, Relations: []TargetRelation{RelEnemy}},
		Triggers: []AbilityTriggerDef{
			{ID: "cast", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
				{ID: "proj", Type: ActionLaunchProjectile, Target: &TargetQueryDef{Source: SrcInitialTarget}, Config: marshalConfig(rootCfg)},
			}},
		},
	}
}

func TestFrostBoltSplitShape_EndToEnd_HitsEnemyThenSplitsToTwoOthers(t *testing.T) {
	const primaryDamage = 30
	const splitDamage = 20

	def := AbilityDef{
		ID: "frost_bolt_split_acceptance_test", DisplayName: "Frost Bolt Split Acceptance Test", Type: AbilitySpell,
		CanTargetEnemies: true, CastRange: 1000, SchemaVersion: 2,
		Program: buildFrostBoltSplitProgram(primaryDamage, splitDamage),
	}
	registerRuntimeTestAbility(t, def)

	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	caster.CurrentMana, caster.MaxMana = 100, 100

	primary := spawnProjTestUnit(t, s, enemyPlayerID, 300, 0)
	splitA := spawnProjTestUnit(t, s, enemyPlayerID, 350, 0)    // 50px from primary — within the 200px split radius
	splitB := spawnProjTestUnit(t, s, enemyPlayerID, 300, 150)  // 150px from primary — within the 200px split radius
	bystander := spawnProjTestUnit(t, s, enemyPlayerID, 0, 150) // 150px from the CASTER, ~335px from primary — must prove origin is primary's position, not the caster's

	primaryStart, splitAStart, splitBStart, bystanderStart := primary.HP, splitA.HP, splitB.HP, bystander.HP

	s.resolveAbilityCastLocked(caster, def, []*Unit{primary})
	if len(s.Projectiles) != 1 {
		t.Fatalf("expected 1 primary bolt after cast; got %d", len(s.Projectiles))
	}

	// Tick until the primary bolt lands — its impact spawns the 2 split bolts.
	for i := 0; i < 40 && len(s.Projectiles) == 1; i++ {
		s.tickProjectilesLocked(0.05)
	}
	if len(s.Projectiles) != 2 {
		t.Fatalf("want exactly 2 split bolts spawned from the primary's impact, got %d", len(s.Projectiles))
	}

	// Geometry (Gap 1): both split bolts must originate at the PRIMARY's hit
	// position, not the caster's — assert the actual origin coordinates, not
	// merely that the bolts exist.
	for _, p := range s.Projectiles {
		if p.OriginX != primary.X || p.OriginY != primary.Y {
			t.Errorf("split bolt origin = (%v,%v); want primary's hit position (%v,%v)", p.OriginX, p.OriginY, primary.X, primary.Y)
		}
		if p.OriginX == caster.X && p.OriginY == caster.Y {
			t.Errorf("split bolt origin coincides with the caster (%v,%v); spawnOrigin should have overridden it", caster.X, caster.Y)
		}
		// The client anchors the spawn SPRITE (its chest) to this unit — the hit
		// enemy, not the caster — so a size-mismatched caster's chest offset
		// can't push the spawn to the enemy's head. (current_event_position →
		// originUnitForSpawnLocked → the hit unit.)
		if p.OriginUnitID != primary.ID {
			t.Errorf("split bolt OriginUnitID = %d; want the hit enemy %d (chest anchor)", p.OriginUnitID, primary.ID)
		}
	}

	splitTargets := map[int]bool{}
	for _, p := range s.Projectiles {
		splitTargets[p.TargetUnitID] = true
	}
	if !splitTargets[splitA.ID] || !splitTargets[splitB.ID] {
		t.Fatalf("split bolts target %v; want exactly [%d %d]", splitTargets, splitA.ID, splitB.ID)
	}
	if splitTargets[primary.ID] {
		t.Fatal("a split bolt re-targeted the primary — current-event exclusion (Gap 2) failed")
	}
	if splitTargets[bystander.ID] {
		t.Fatal("a split bolt targeted the out-of-radius bystander")
	}

	// Let the split bolts land too.
	for i := 0; i < 40 && len(s.Projectiles) > 0; i++ {
		s.tickProjectilesLocked(0.05)
	}
	if len(s.Projectiles) != 0 {
		t.Fatal("split bolts never landed")
	}

	if got := primaryStart - primary.HP; got != primaryDamage {
		t.Errorf("primary damage = %d, want exactly %d (one impact hit; a re-hit would show up here as %d)", got, primaryDamage, primaryDamage+splitDamage)
	}
	if got := splitAStart - splitA.HP; got != splitDamage {
		t.Errorf("splitA damage = %d, want %d", got, splitDamage)
	}
	if got := splitBStart - splitB.HP; got != splitDamage {
		t.Errorf("splitB damage = %d, want %d", got, splitDamage)
	}
	if bystander.HP != bystanderStart {
		t.Errorf("bystander HP changed (%d -> %d); should be outside the split radius and never selected", bystanderStart, bystander.HP)
	}
}
