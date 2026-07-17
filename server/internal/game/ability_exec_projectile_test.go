package game

import (
	"testing"
	"time"

	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// launch_projectile redesign tests: validator recursion into config.triggers,
// the shared cross-tick op budget (breadth+depth termination proof), and
// "direction" travelMode semantics. arcane_bolt/fireball parity itself is
// covered by ability_compile_golden_projectile_test.go, fireball_test.go, and
// adept_arcane_bolt_repro_test.go — not duplicated here.
// ═════════════════════════════════════════════════════════════════════════════

// ── Validator recursion into launch_projectile's config.triggers ───────────

// TestValidateProgram_NestedProjectileImpactAction_DuplicateIDDetected mirrors
// TestValidateProgram_NestedZoneAction_DuplicateIDDetected for
// launch_projectile: an id inside its on_projectile_impact config.triggers
// collides with a root action id and must be flagged, at the same path
// grammar create_zone/apply_status already use.
func TestValidateProgram_NestedProjectileImpactAction_DuplicateIDDetected(t *testing.T) {
	projCfg := launchProjectileConfig{
		Projectile: "arcane_bolt",
		Triggers: []AbilityTriggerDef{
			{
				ID:   "impact",
				Type: TriggerOnProjectileImpact,
				Actions: []AbilityActionDef{
					// Collides with the root "dmg" action id below.
					{ID: "dmg", Type: ActionDealDamage, Config: marshalConfig(dealDamageConfig{Amount: 1})},
				},
			},
		},
	}
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryUnit, Relations: []TargetRelation{RelEnemy}},
		Triggers: []AbilityTriggerDef{
			{ID: "t1", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
				{ID: "dmg", Type: ActionDealDamage, Config: marshalConfig(dealDamageConfig{Amount: 1})},
				{ID: "proj", Type: ActionLaunchProjectile, Target: &TargetQueryDef{Source: SrcInitialTarget}, Config: marshalConfig(projCfg)},
			}},
		},
	}
	issues := validateAbilityProgram(prog)
	wantPath := "triggers[0].actions[1].config.triggers[0].actions[0]"
	if got := issueAt(issues, wantPath, "duplicate_id"); got == nil {
		t.Fatalf("want duplicate_id at %q, got issues: %+v", wantPath, issues)
	}
}

// TestValidateProgram_MalformedLaunchProjectileConfig_SingleInvalidConfigNoRecursion
// mirrors the create_zone equivalent: a launch_projectile action whose Config
// fails to decode must report invalid_config exactly once, never attempting
// to recurse into a garbage (zero-value) config's Triggers.
func TestValidateProgram_MalformedLaunchProjectileConfig_SingleInvalidConfigNoRecursion(t *testing.T) {
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryUnit, Relations: []TargetRelation{RelEnemy}},
		Triggers: []AbilityTriggerDef{
			{ID: "t1", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
				{ID: "proj", Type: ActionLaunchProjectile, Config: []byte(`{"projectile": 5}`)}, // wrong type for a string field
			}},
		},
	}
	issues := validateAbilityProgram(prog)
	path := "triggers[0].actions[0]"
	var count int
	for _, iss := range issues {
		if iss.Path == path && iss.Code == "invalid_config" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("want exactly 1 invalid_config at %q, got %d (issues: %+v)", path, count, issues)
	}
}

// ── Shared cross-tick op budget: breadth+depth termination ─────────────────

// buildImpactRelaunchLevel builds one level of a synthetic "impact relaunches
// a projectile at every enemy in the scene" trigger, recursing `depth` more
// levels below it. Each level's select_targets has NO relations/count cap —
// it always selects every currently-alive enemy — so if the shared cross-tick
// op budget did NOT bound total work, this would fan out ~breadth^depth
// projectiles (astronomically more than maxExecutionOps long before depth=30
// is reached). depth==0 stops the recursion (a plain deal_damage, no further
// relaunch).
func buildImpactRelaunchLevel(depth int) AbilityTriggerDef {
	actions := []AbilityActionDef{
		{
			ID:   "sel",
			Type: ActionSelectTargets,
			Target: &TargetQueryDef{
				Source:    SrcAllInScene,
				Relations: []TargetRelation{RelEnemy},
			},
			Outputs: map[string]string{"targets": "foes"},
		},
	}
	if depth <= 0 {
		actions = append(actions, AbilityActionDef{
			ID:     "dmg",
			Type:   ActionDealDamage,
			Input:  map[string]ContextRef{"targets": {Key: "foes"}},
			Config: marshalConfig(dealDamageConfig{Amount: 1}),
		})
		return AbilityTriggerDef{ID: "impact", Type: TriggerOnProjectileImpact, Actions: actions}
	}
	nextCfg := launchProjectileConfig{
		Projectile: "arcane_bolt",
		Triggers:   []AbilityTriggerDef{buildImpactRelaunchLevel(depth - 1)},
	}
	actions = append(actions, AbilityActionDef{
		ID:     "relaunch",
		Type:   ActionLaunchProjectile,
		Input:  map[string]ContextRef{"targets": {Key: "foes"}},
		Config: marshalConfig(nextCfg),
	})
	return AbilityTriggerDef{ID: "impact", Type: TriggerOnProjectileImpact, Actions: actions}
}

// TestLaunchProjectile_ImpactRelaunchChain_SharedBudgetTerminates proves the
// CROSS-TICK OP BUDGET design (ability_exec_projectile.go doc comment): a
// projectile whose impact relaunches MORE projectiles at every enemy in the
// scene (breadth), for many levels (depth), must not hang or explode
// s.Projectiles — the SHARED budget (Projectile.ImpactOpsBudget) must cap
// total work across the whole lineage regardless of how many ticks it spans
// or how many projectiles branch off.
func TestLaunchProjectile_ImpactRelaunchChain_SharedBudgetTerminates(t *testing.T) {
	const depth = 30     // far deeper than the budget could ever fund at this breadth
	const numEnemies = 3 // breadth: 3^30 is astronomically larger than maxExecutionOps

	rootCfg := launchProjectileConfig{
		Projectile: "arcane_bolt",
		Triggers:   []AbilityTriggerDef{buildImpactRelaunchLevel(depth)},
	}
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryUnit, Relations: []TargetRelation{RelEnemy}},
		Triggers: []AbilityTriggerDef{
			{ID: "cast", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
				{ID: "proj", Type: ActionLaunchProjectile, Target: &TargetQueryDef{Source: SrcInitialTarget}, Config: marshalConfig(rootCfg)},
			}},
		},
	}
	def := AbilityDef{
		ID: "impact_relaunch_test", DisplayName: "Impact Relaunch Test", Type: AbilitySpell,
		CanTargetEnemies: true, CastRange: 1000, SchemaVersion: 2, Program: prog,
	}
	registerRuntimeTestAbility(t, def)

	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	caster.CurrentMana, caster.MaxMana = 100, 100
	primary := spawnProjTestUnit(t, s, enemyPlayerID, 10, 0)
	for i := 1; i < numEnemies; i++ {
		spawnProjTestUnit(t, s, enemyPlayerID, 10, float64(i*5))
	}

	// Every ctx built for this run (cast + every later impact) shares this
	// trace, letting the test count total executor work across the whole
	// lineage regardless of which tick/hop it happened on.
	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	s.resolveAbilityCastLocked(caster, def, []*Unit{primary})

	done := make(chan struct{})
	var panicked any
	go func() {
		defer close(done)
		defer func() { panicked = recover() }()
		for i := 0; i < 60; i++ {
			s.tickProjectilesLocked(0.05)
			// Safety valve: if the shared budget failed to bound this, fail
			// fast instead of exhausting test-process memory.
			if len(s.Projectiles) > 200_000 {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("impact-relaunch chain did not terminate within 10s; shared op budget failed to bound breadth+depth fan-out")
	}
	if panicked != nil {
		t.Fatalf("panic while ticking impact-relaunch chain: %v", panicked)
	}

	if len(s.Projectiles) > 200_000 {
		t.Fatalf("s.Projectiles grew to %d; shared op budget failed to cap the impact-relaunch chain", len(s.Projectiles))
	}

	// Total executor work across the ENTIRE lineage (cast + every impact, on
	// every tick) must be bounded near maxExecutionOps — not astronomically
	// higher, which is what an unshared (per-ctx-reset) budget would allow.
	actionsStarted := 0
	for _, ev := range tr.Events {
		if ev.Type == "action_started" {
			actionsStarted++
		}
	}
	if actionsStarted == 0 {
		t.Fatal("no actions ran at all; test setup problem")
	}
	if actionsStarted > maxExecutionOps+10000 {
		t.Fatalf("total action_started events = %d; want bounded near maxExecutionOps (%d) — the shared budget must cap the WHOLE lineage, not reset per projectile", actionsStarted, maxExecutionOps)
	}
	t.Logf("impact-relaunch chain: %d total actions dispatched, %d projectiles remaining", actionsStarted, len(s.Projectiles))
}

// buildSplitRelaunchLevel builds one level of the user's frost_bolt split
// shape — "hit an enemy, deal damage, then split to up to 2 OTHER nearby
// enemies" (select_targets(current_event) -> deal_damage, then
// select_targets(all_in_scene, origin: current_event_position, exclude
// current_event, maxCount 2) -> on_action_complete -> launch_projectile(
// spawnOrigin: current_event_position)) — recursing `depth` more levels
// below it, unlike buildImpactRelaunchLevel above (which relaunches at EVERY
// enemy with no per-level exclusion or spawn-origin concern). This is the
// specific shape Gap 1 (spawnOrigin) and Gap 2 (excludeCurrentEvent) exist
// for, so it's this shape — not the generic relaunch-at-everyone case above —
// that must be proven to terminate via the shared cross-tick op budget.
// depth==0 stops the recursion (deal_damage only, no further split).
func buildSplitRelaunchLevel(depth int) AbilityTriggerDef {
	actions := []AbilityActionDef{
		{ID: "sel", Type: ActionSelectTargets, Target: &TargetQueryDef{Source: SrcCurrentEvent}, Outputs: map[string]string{"targets": "hit"}},
		{ID: "dmg", Type: ActionDealDamage, Input: map[string]ContextRef{"targets": {Key: "hit"}}, Config: marshalConfig(dealDamageConfig{Amount: 1})},
	}
	if depth <= 0 {
		return AbilityTriggerDef{ID: "impact", Type: TriggerOnProjectileImpact, Actions: actions}
	}
	nextCfg := launchProjectileConfig{
		Projectile:  "arcane_bolt",
		SpawnOrigin: OriginCurrentEventPos,
		Triggers:    []AbilityTriggerDef{buildSplitRelaunchLevel(depth - 1)},
	}
	actions = append(actions, AbilityActionDef{
		ID:   "splitsel",
		Type: ActionSelectTargets,
		Target: &TargetQueryDef{
			Source: SrcAllInScene, Origin: OriginCurrentEventPos,
			Relations: []TargetRelation{RelEnemy}, Radius: 100000, // "every enemy in the scene"
			ExcludeCurrentEvent: true, MaxCount: 2, Ordering: OrderUnitID,
		},
		Outputs: map[string]string{"targets": "splits"},
		Children: []AbilityTriggerDef{
			{ID: "onsplit", Type: TriggerOnActionComplete, Actions: []AbilityActionDef{
				{ID: "relaunch", Type: ActionLaunchProjectile, Input: map[string]ContextRef{"targets": {Key: "splits"}}, Config: marshalConfig(nextCfg)},
			}},
		},
	})
	return AbilityTriggerDef{ID: "impact", Type: TriggerOnProjectileImpact, Actions: actions}
}

// TestFrostBoltStyleSplitChain_SharedBudgetTerminates proves the CROSS-TICK
// OP BUDGET design bounds a RECURSIVE 2-way split (buildSplitRelaunchLevel,
// Gap 1's spawnOrigin + Gap 2's excludeCurrentEvent together) — the exact
// "a split that splits again" runaway case the task calls out — not just the
// simpler unconditional relaunch-at-everyone shape
// TestLaunchProjectile_ImpactRelaunchChain_SharedBudgetTerminates above
// already covers. depth=30 with 2-way branching (2^30) is astronomically
// more than maxExecutionOps if the budget failed to bound it.
func TestFrostBoltStyleSplitChain_SharedBudgetTerminates(t *testing.T) {
	const depth = 30
	const numEnemies = 3

	rootCfg := launchProjectileConfig{
		Projectile: "arcane_bolt",
		Triggers:   []AbilityTriggerDef{buildSplitRelaunchLevel(depth)},
	}
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryUnit, Relations: []TargetRelation{RelEnemy}},
		Triggers: []AbilityTriggerDef{
			{ID: "cast", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
				{ID: "proj", Type: ActionLaunchProjectile, Target: &TargetQueryDef{Source: SrcInitialTarget}, Config: marshalConfig(rootCfg)},
			}},
		},
	}
	def := AbilityDef{
		ID: "split_relaunch_test", DisplayName: "Split Relaunch Test", Type: AbilitySpell,
		CanTargetEnemies: true, CastRange: 1000, SchemaVersion: 2, Program: prog,
	}
	registerRuntimeTestAbility(t, def)

	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	caster.CurrentMana, caster.MaxMana = 100, 100
	primary := spawnProjTestUnit(t, s, enemyPlayerID, 10, 0)
	// Effectively unkillable at this chain's 1-damage-per-hit rate: the point
	// of this test is proving the SHARED OP BUDGET is what stops the
	// recursion, not that the enemies happen to run out of HP first (a much
	// weaker, coincidental form of termination).
	primary.HP, primary.MaxHP = 1_000_000_000, 1_000_000_000
	for i := 1; i < numEnemies; i++ {
		u := spawnProjTestUnit(t, s, enemyPlayerID, 10, float64(i*5))
		u.HP, u.MaxHP = 1_000_000_000, 1_000_000_000
	}

	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	defer func() { s.previewTrace = nil }()

	s.resolveAbilityCastLocked(caster, def, []*Unit{primary})

	done := make(chan struct{})
	var panicked any
	go func() {
		defer close(done)
		defer func() { panicked = recover() }()
		for i := 0; i < 60; i++ {
			s.tickProjectilesLocked(0.05)
			// Safety valve: if the shared budget failed to bound this, fail
			// fast instead of exhausting test-process memory.
			if len(s.Projectiles) > 200_000 {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("split-relaunch chain did not terminate within 10s; shared op budget failed to bound the recursive split")
	}
	if panicked != nil {
		t.Fatalf("panic while ticking split-relaunch chain: %v", panicked)
	}

	if len(s.Projectiles) > 200_000 {
		t.Fatalf("s.Projectiles grew to %d; shared op budget failed to cap the split-relaunch chain", len(s.Projectiles))
	}

	actionsStarted := 0
	for _, ev := range tr.Events {
		if ev.Type == "action_started" {
			actionsStarted++
		}
	}
	if actionsStarted == 0 {
		t.Fatal("no actions ran at all; test setup problem")
	}
	if actionsStarted > maxExecutionOps+10000 {
		t.Fatalf("total action_started events = %d; want bounded near maxExecutionOps (%d) — the shared budget must cap a RECURSIVE split (spawnOrigin+excludeCurrentEvent), not just the simpler relaunch-at-everyone shape", actionsStarted, maxExecutionOps)
	}
	t.Logf("split-relaunch chain: %d total actions dispatched, %d projectiles remaining", actionsStarted, len(s.Projectiles))
}

// ── "direction" travelMode ──────────────────────────────────────────────────

// buildDirectionAbility registers a schemaVersion:2 ability whose single
// launch_projectile action flies "direction" mode toward the given distance,
// carrying a select_targets(current_event) -> deal_damage impact (the same
// non-splash shape compileProjectileImpactTrigger emits for a Radius<=0
// ability). Ground-point entry with NO resolved unit target anywhere in the
// program (the launch_projectile action has no Target/Input of its own
// either): every caller of this builder is deliberately exercising the
// "nothing resolved" side of directionalAimPointLocked (CastPoint fallback),
// not the aim-at-a-resolved-target path — see buildDirectionAbilityAimedAt
// SelectedTarget below for that one.
func buildDirectionAbility(t *testing.T, id string, distance float64) AbilityDef {
	t.Helper()
	cfg := launchProjectileConfig{
		Projectile: "arcane_bolt",
		TravelMode: travelModeDirection,
		Distance:   CastRange(distance),
		Triggers: []AbilityTriggerDef{
			{
				ID:   "impact",
				Type: TriggerOnProjectileImpact,
				Actions: []AbilityActionDef{
					{ID: "sel", Type: ActionSelectTargets, Target: &TargetQueryDef{Source: SrcCurrentEvent}, Outputs: map[string]string{"targets": "hit"}},
					{ID: "dmg", Type: ActionDealDamage, Input: map[string]ContextRef{"targets": {Key: "hit"}}, Config: marshalConfig(dealDamageConfig{Amount: 40, Type: DamageFire})},
				},
			},
		},
	}
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryGroundPoint, Range: CastRange(1000)},
		Triggers: []AbilityTriggerDef{
			{ID: "cast", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
				{ID: "proj", Type: ActionLaunchProjectile, Config: marshalConfig(cfg)},
			}},
		},
	}
	def := AbilityDef{
		ID: id, DisplayName: "Direction Bolt Test", Type: AbilitySpell,
		TargetsPoint: true, CastRange: 1000, SchemaVersion: 2, Program: prog,
	}
	registerRuntimeTestAbility(t, def)
	return def
}

// buildDirectionAbilityAimedAtSelectedTarget registers a schemaVersion:2
// ability that proves the aim-at-resolved-target half of Fix 2: its
// on_cast_complete trigger runs a select_targets(closest enemy within radius)
// FIRST, then feeds that selection into launch_projectile's Input["targets"]
// — the "select_targets (pick who) -> launch_projectile (fly at them)"
// composition Fix 1's narrowed schema is built around. Ground-point entry
// (same as buildDirectionAbility) so ctx.InitialTarget is always 0: the ONLY
// way this program's bolt can know who to aim at is by actually reading its
// own resolved `targets` parameter, which is exactly what
// directionalAimPointLocked must do post-fix (and did NOT do pre-fix, when
// it read ctx.InitialTarget directly and ignored `targets` for "direction"
// mode entirely).
func buildDirectionAbilityAimedAtSelectedTarget(t *testing.T, id string, distance float64) AbilityDef {
	t.Helper()
	cfg := launchProjectileConfig{
		Projectile: "arcane_bolt",
		TravelMode: travelModeDirection,
		Distance:   CastRange(distance),
		Triggers: []AbilityTriggerDef{
			{
				ID:   "impact",
				Type: TriggerOnProjectileImpact,
				Actions: []AbilityActionDef{
					{ID: "sel", Type: ActionSelectTargets, Target: &TargetQueryDef{Source: SrcCurrentEvent}, Outputs: map[string]string{"targets": "hit"}},
					{ID: "dmg", Type: ActionDealDamage, Input: map[string]ContextRef{"targets": {Key: "hit"}}, Config: marshalConfig(dealDamageConfig{Amount: 40, Type: DamageFire})},
				},
			},
		},
	}
	prog := &AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryGroundPoint, Range: CastRange(2000)},
		Triggers: []AbilityTriggerDef{
			{ID: "cast", Type: TriggerOnCastComplete, Actions: []AbilityActionDef{
				{
					ID:   "find",
					Type: ActionSelectTargets,
					Target: &TargetQueryDef{
						Source: SrcAllInScene, Origin: OriginCaster,
						Relations: []TargetRelation{RelEnemy}, Radius: 1000,
						Ordering: OrderClosest, MaxCount: 1,
					},
					Outputs: map[string]string{"targets": "aimAt"},
				},
				{ID: "proj", Type: ActionLaunchProjectile, Input: map[string]ContextRef{"targets": {Key: "aimAt"}}, Config: marshalConfig(cfg)},
			}},
		},
	}
	def := AbilityDef{
		ID: id, DisplayName: "Direction Bolt Aim Test", Type: AbilitySpell,
		TargetsPoint: true, CastRange: 2000, SchemaVersion: 2, Program: prog,
	}
	registerRuntimeTestAbility(t, def)
	return def
}

// TestDirectionMode_AimsAtResolvedTarget proves Fix 2's core claim: a
// "direction" travelMode bolt aims at the target resolved by a PRECEDING
// select_targets action (chained via Input["targets"]), not at the raw
// click point — the click point here is authored 90 degrees off from the
// enemies' actual bearing, so the old ("never touches the resolved targets
// slice, flies at ctx.CastPoint") behavior would send the bolt straight past
// both enemies and hit neither.
func TestDirectionMode_AimsAtResolvedTarget(t *testing.T) {
	def := buildDirectionAbilityAimedAtSelectedTarget(t, "direction_aim_at_selected_test", 400)

	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Caster placed away from the origin (position is otherwise arbitrary):
	// the clicked point below (500,100) is due NORTH of the caster, while
	// both enemies are due EAST — if the bolt fell back to aiming at the
	// click point (pre-fix behavior), it flies north and never comes near
	// x=600..800 at all.
	caster := spawnProjTestUnit(t, s, "p1", 500, 500)
	caster.CurrentMana, caster.MaxMana = 100, 100
	near := spawnProjTestUnit(t, s, enemyPlayerID, 600, 500) // closest enemy: due EAST of caster
	far := spawnProjTestUnit(t, s, enemyPlayerID, 800, 500)  // further east, same line
	nearStart, farStart := near.HP, far.HP

	// Click point is due NORTH of the caster (500,100) — 90 degrees away from
	// the enemies' actual bearing (due east). Proves the bolt's flight
	// direction comes from the resolved target, not this click.
	s.resolveAbilityCastAtPointLocked(caster, def, s.effectiveSpellLocked(caster, def), 500, 100)
	if len(s.Projectiles) != 1 {
		t.Fatalf("expected 1 directional bolt after cast; got %d", len(s.Projectiles))
	}
	for i := 0; i < 40 && len(s.Projectiles) > 0; i++ {
		s.tickProjectilesLocked(0.05)
	}
	if len(s.Projectiles) != 0 {
		t.Fatal("directional bolt never landed")
	}
	if near.HP >= nearStart {
		t.Errorf("near enemy (the select_targets-resolved aim target) HP %d unchanged from %d; the bolt should have flown toward it, not the unrelated click point", near.HP, nearStart)
	}
	if far.HP != farStart {
		t.Errorf("far enemy HP %d changed from %d; a direction bolt hits only the first enemy in its path", far.HP, farStart)
	}
}

// TestDirectionMode_HitsFirstEnemyInPath proves the "first hostile crossed"
// impact semantics: a bolt fired straight down a line of enemies damages only
// the FIRST one it reaches, not every enemy along the line (unlike a
// Marksman pierce arrow) — see launchDirectionalProjectileLocked's IMPACT
// SEMANTICS doc. Uses buildDirectionAbility's CastPoint-aimed shape (no
// preceding selection): TestDirectionMode_AimsAtResolvedTarget above is what
// proves the resolved-target aim path; this test's concern is purely the
// during-flight pierce/first-hit behavior, orthogonal to where the aim point
// came from.
func TestDirectionMode_HitsFirstEnemyInPath(t *testing.T) {
	def := buildDirectionAbility(t, "direction_hit_test", 400)

	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	caster.CurrentMana, caster.MaxMana = 100, 100
	near := spawnProjTestUnit(t, s, enemyPlayerID, 100, 0)
	far := spawnProjTestUnit(t, s, enemyPlayerID, 300, 0)
	nearStart, farStart := near.HP, far.HP

	s.resolveAbilityCastAtPointLocked(caster, def, s.effectiveSpellLocked(caster, def), 400, 0)
	if len(s.Projectiles) != 1 {
		t.Fatalf("expected 1 directional bolt after cast; got %d", len(s.Projectiles))
	}
	for i := 0; i < 40 && len(s.Projectiles) > 0; i++ {
		s.tickProjectilesLocked(0.05)
	}
	if len(s.Projectiles) != 0 {
		t.Fatal("directional bolt never landed")
	}
	if near.HP >= nearStart {
		t.Errorf("near enemy HP %d unchanged from %d; the FIRST enemy in the bolt's path should be hit", near.HP, nearStart)
	}
	if far.HP != farStart {
		t.Errorf("far enemy HP %d changed from %d; a direction bolt hits only the first enemy, not every enemy along the line", far.HP, farStart)
	}
}

// TestDirectionMode_WhiffFiresImpactAtEndpoint proves the no-hit case: a
// "direction" bolt fired where nothing is in its path still fires its
// on_projectile_impact once, at the flight endpoint, with no hit unit
// (CurrentEventUnitID 0) — this is what lets an author's impact trigger
// (e.g. a splash select_targets{origin: impact_position}) still resolve on a
// miss.
func TestDirectionMode_WhiffFiresImpactAtEndpoint(t *testing.T) {
	def := buildDirectionAbility(t, "direction_whiff_test", 200)

	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	caster.CurrentMana, caster.MaxMana = 100, 100
	// Enemy well outside the bolt's corridor/path — must NOT be hit, and must
	// NOT prevent the impact from firing at the endpoint.
	bystander := spawnProjTestUnit(t, s, enemyPlayerID, 100, 500)
	bystanderStart := bystander.HP

	s.resolveAbilityCastAtPointLocked(caster, def, s.effectiveSpellLocked(caster, def), 200, 0)
	if len(s.Projectiles) != 1 {
		t.Fatalf("expected 1 directional bolt after cast; got %d", len(s.Projectiles))
	}
	proj := s.Projectiles[0]
	if !proj.DirectionalImpact {
		t.Fatal("bolt is not marked DirectionalImpact")
	}

	for i := 0; i < 40 && len(s.Projectiles) > 0; i++ {
		s.tickProjectilesLocked(0.05)
	}
	if len(s.Projectiles) != 0 {
		t.Fatal("directional bolt never ended its flight")
	}
	if bystander.HP != bystanderStart {
		t.Errorf("bystander HP %d changed from %d; should be well outside the bolt's path", bystander.HP, bystanderStart)
	}
}

// TestDirectionMode_AimPointFallsBackToCastPoint proves
// directionalAimPointLocked's fallback: with no resolved unit target
// (ctx.InitialTarget == 0, the point-cast case), the bolt aims at
// ctx.CastPoint.
func TestDirectionMode_AimPointFallsBackToCastPoint(t *testing.T) {
	def := buildDirectionAbility(t, "direction_aim_test", 0) // 0 -> derive distance from the aim point

	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	caster.CurrentMana, caster.MaxMana = 100, 100

	s.resolveAbilityCastAtPointLocked(caster, def, s.effectiveSpellLocked(caster, def), 300, 400)
	if len(s.Projectiles) != 1 {
		t.Fatalf("expected 1 directional bolt after cast; got %d", len(s.Projectiles))
	}
	proj := s.Projectiles[0]
	wantX, wantY := 300.0, 400.0
	gotX := proj.OriginX + proj.PierceDirX*proj.PierceLength
	gotY := proj.OriginY + proj.PierceDirY*proj.PierceLength
	const eps = 1e-6
	if diff := (gotX-wantX)*(gotX-wantX) + (gotY-wantY)*(gotY-wantY); diff > eps {
		t.Errorf("bolt endpoint = (%v,%v); want (%v,%v) (cast point, since no unit target was resolved)", gotX, gotY, wantX, wantY)
	}
}

// ── Gap 1: launch_projectile spawnOrigin ────────────────────────────────────
//
// These call the registered ActionDescriptor's Execute directly (the
// ability_zone_test.go precedent) so the spawn geometry can be proven against
// a hand-built ctx.EventPosition/CastPoint, independent of any real cast/
// impact plumbing.

// TestLaunchProjectile_SpawnOrigin_CurrentEventPosition_ToTargetMode_SpawnsAtEventPosition
// proves Gap 1's core claim in "to_target" (homing) mode: a bolt configured
// with spawnOrigin "current_event_position" spawns AT ctx.EventPosition (the
// unit a preceding impact just hit), not at the caster's position — this is
// what lets a split bolt fly FROM the enemy it split off of.
func TestLaunchProjectile_SpawnOrigin_CurrentEventPosition_ToTargetMode_SpawnsAtEventPosition(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	target := spawnProjTestUnit(t, s, enemyPlayerID, 500, 0)

	eventPos := protocol.Vec2{X: 300, Y: 400} // deliberately NOT the caster's (0,0)
	ctx := &RuntimeAbilityContext{CasterID: caster.ID, EventPosition: eventPos, Named: map[string]ContextValue{}}
	cfg := launchProjectileConfig{Projectile: "arcane_bolt", SpawnOrigin: OriginCurrentEventPos}

	desc, ok := lookupActionDescriptor(ActionLaunchProjectile)
	if !ok {
		t.Fatal("launch_projectile action not registered")
	}
	desc.Execute(s, ctx, cfg, []int{target.ID})

	if len(s.Projectiles) != 1 {
		t.Fatalf("want exactly 1 projectile, got %d", len(s.Projectiles))
	}
	proj := s.Projectiles[0]
	if proj.OriginX != eventPos.X || proj.OriginY != eventPos.Y {
		t.Errorf("projectile origin = (%v,%v); want event position (%v,%v)", proj.OriginX, proj.OriginY, eventPos.X, eventPos.Y)
	}
	if proj.OriginX == caster.X && proj.OriginY == caster.Y {
		t.Errorf("projectile origin coincides with caster (%v,%v); spawnOrigin should have overridden it", caster.X, caster.Y)
	}
	if proj.TargetUnitID != target.ID {
		t.Errorf("projectile TargetUnitID = %d, want %d (spawnOrigin must not affect WHO it flies at)", proj.TargetUnitID, target.ID)
	}
}

// TestLaunchProjectile_SpawnOrigin_Unset_DefaultsToCasterPosition_ToTargetMode
// is the byte-identical guard: an unset (empty-string) spawnOrigin — every
// ability compiled/authored before this field existed — must still spawn at
// the caster's position, exactly like before Gap 1's fix.
func TestLaunchProjectile_SpawnOrigin_Unset_DefaultsToCasterPosition_ToTargetMode(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 123, 456)
	target := spawnProjTestUnit(t, s, enemyPlayerID, 500, 500)

	ctx := &RuntimeAbilityContext{CasterID: caster.ID, Named: map[string]ContextValue{}}
	cfg := launchProjectileConfig{Projectile: "arcane_bolt"} // SpawnOrigin left unset

	desc, ok := lookupActionDescriptor(ActionLaunchProjectile)
	if !ok {
		t.Fatal("launch_projectile action not registered")
	}
	desc.Execute(s, ctx, cfg, []int{target.ID})

	if len(s.Projectiles) != 1 {
		t.Fatalf("want exactly 1 projectile, got %d", len(s.Projectiles))
	}
	proj := s.Projectiles[0]
	if proj.OriginX != caster.X || proj.OriginY != caster.Y {
		t.Errorf("projectile origin = (%v,%v); want caster position (%v,%v) (default spawnOrigin)", proj.OriginX, proj.OriginY, caster.X, caster.Y)
	}
}

// TestLaunchProjectile_SpawnOrigin_CurrentEventPosition_DirectionMode_SpawnsAtEventPosition
// proves Gap 1 applies to "direction" travelMode too: the bolt's straight-
// line flight originates at ctx.EventPosition, and its direction vector is
// computed relative to THAT origin (not the caster), toward the aim point
// (here ctx.CastPoint, since no unit target is resolved).
func TestLaunchProjectile_SpawnOrigin_CurrentEventPosition_DirectionMode_SpawnsAtEventPosition(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)

	eventPos := protocol.Vec2{X: 100, Y: 100}
	castPoint := protocol.Vec2{X: 100, Y: 200} // due north of eventPos, NOT of the caster
	ctx := &RuntimeAbilityContext{CasterID: caster.ID, EventPosition: eventPos, CastPoint: castPoint, Named: map[string]ContextValue{}}
	cfg := launchProjectileConfig{
		Projectile:  "arcane_bolt",
		TravelMode:  travelModeDirection,
		SpawnOrigin: OriginCurrentEventPos,
		Distance:    50,
	}

	desc, ok := lookupActionDescriptor(ActionLaunchProjectile)
	if !ok {
		t.Fatal("launch_projectile action not registered")
	}
	desc.Execute(s, ctx, cfg, nil)

	if len(s.Projectiles) != 1 {
		t.Fatalf("want exactly 1 projectile, got %d", len(s.Projectiles))
	}
	proj := s.Projectiles[0]
	if proj.OriginX != eventPos.X || proj.OriginY != eventPos.Y {
		t.Errorf("projectile origin = (%v,%v); want event position (%v,%v)", proj.OriginX, proj.OriginY, eventPos.X, eventPos.Y)
	}
	wantEndX, wantEndY := eventPos.X, eventPos.Y+50 // straight toward castPoint, due north of eventPos
	gotEndX := proj.OriginX + proj.PierceDirX*proj.PierceLength
	gotEndY := proj.OriginY + proj.PierceDirY*proj.PierceLength
	const eps = 1e-6
	if diff := (gotEndX-wantEndX)*(gotEndX-wantEndX) + (gotEndY-wantEndY)*(gotEndY-wantEndY); diff > eps {
		t.Errorf("bolt endpoint = (%v,%v); want (%v,%v) (flying from event position toward cast point)", gotEndX, gotEndY, wantEndX, wantEndY)
	}
}
