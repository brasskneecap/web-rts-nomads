package game

// damage_pipeline_category_test.go — characterizes DamageSource.Category end
// to end. Purely additive: nothing in production code reads Category yet, so
// every test here proves WIRING (the right constant lands on the right real
// damage path), not gameplay behavior. See damage_pipeline.go's DamageCategory
// doc comment for the vocabulary and damage_pipeline_category_report.md-style
// rationale captured in the task's final report.
//
// Capture point: every path below ends in a LETHAL hit so the resulting
// DamageSource is observable via s.pendingDeaths (populated by
// enqueueDeathLocked inside applyUnitDamageWithSourceLocked — see
// damage_pipeline.go). Tests read s.pendingDeaths directly rather than
// calling drainPendingDeathsLocked, which would consume the queue before the
// assertion runs. This mirrors death_pipeline_test.go's existing convention.

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// lastPendingDeathSource returns the DamageSource of the most recently
// enqueued pending death, failing the test if the queue is empty.
func lastPendingDeathSource(t *testing.T, s *GameState) DamageSource {
	t.Helper()
	if len(s.pendingDeaths) == 0 {
		t.Fatal("expected at least one pending death; queue is empty")
	}
	return s.pendingDeaths[len(s.pendingDeaths)-1].Source
}

// pendingDeathSourceFor returns the DamageSource enqueued for unitID,
// failing the test if no such entry exists.
func pendingDeathSourceFor(t *testing.T, s *GameState, unitID int) DamageSource {
	t.Helper()
	for _, d := range s.pendingDeaths {
		if d.UnitID == unitID {
			return d.Source
		}
	}
	t.Fatalf("no pending death enqueued for unit %d; queue has %d entries", unitID, len(s.pendingDeaths))
	return DamageSource{}
}

// ─────────────────────────────────────────────────────────────────────────────
// Gap marker: the zero value is NOT a meaningful default
// ─────────────────────────────────────────────────────────────────────────────

func TestDamageCategory_Unspecified_IsZeroValueGapMarker(t *testing.T) {
	if DamageCategoryUnspecified != "" {
		t.Errorf("DamageCategoryUnspecified = %q; want the empty string (the zero value)", DamageCategoryUnspecified)
	}
	if got := (DamageSource{}).Category; got != DamageCategoryUnspecified {
		t.Errorf("DamageSource{}.Category = %q; want DamageCategoryUnspecified — every un-classified call site must compile and behave via the zero value", got)
	}
	// A real, fully-attributed source that simply omits Category (the shape of
	// a not-yet-migrated call site) must ALSO read as Unspecified — nothing
	// silently infers a category from Kind or AttackerUnitID.
	src := DamageSource{AttackerUnitID: 7, Kind: "melee"}
	if src.Category != DamageCategoryUnspecified {
		t.Errorf("DamageSource with Kind set but Category omitted = %q; want DamageCategoryUnspecified", src.Category)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Melee basic attack — resolveAttackHitLocked (state_combat.go)
// ─────────────────────────────────────────────────────────────────────────────

func TestDamageCategory_MeleeBasicAttack_IsBasicAttack(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := spawnProjTestUnit(t, s, "p1", 100, 100)
	target := spawnProjTestUnit(t, s, enemyPlayerID, 120, 100)
	target.HP = 10 // exact-kill: no overkill ambiguity

	var dead []int
	s.resolveAttackHitLocked(attacker, target, target.HP, &dead)

	got := pendingDeathSourceFor(t, s, target.ID)
	if got.Category != DamageCategoryBasicAttack {
		t.Errorf("melee kill Category = %q; want %q", got.Category, DamageCategoryBasicAttack)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Ranged basic attack — landProjectileLocked (projectile.go), the normal
// (attacker alive) path AND the "attacker died mid-flight" fallback, both of
// which this task classified as DamageCategoryBasicAttack.
// ─────────────────────────────────────────────────────────────────────────────

func TestDamageCategory_RangedProjectileBasicAttack_IsBasicAttack(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := spawnProjTestUnit(t, s, "p1", 100, 100)
	target := spawnProjTestUnit(t, s, enemyPlayerID, 400, 400)
	target.HP = 10

	proj := &Projectile{
		ID:           "ranged_test",
		OwnerUnitID:  attacker.ID,
		TargetUnitID: target.ID,
		Damage:       target.HP,
	}
	var dead []int
	s.landProjectileLocked(proj, target, &dead)

	got := pendingDeathSourceFor(t, s, target.ID)
	if got.Category != DamageCategoryBasicAttack {
		t.Errorf("ranged (attacker alive) kill Category = %q; want %q", got.Category, DamageCategoryBasicAttack)
	}
}

func TestDamageCategory_RangedProjectileBasicAttack_AttackerDiedMidFlight_IsBasicAttack(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	target := spawnProjTestUnit(t, s, enemyPlayerID, 400, 400)
	target.HP = 10

	proj := &Projectile{
		ID:           "ranged_dead_attacker_test",
		OwnerUnitID:  999_999, // nonexistent — getUnitByIDLocked resolves nil
		TargetUnitID: target.ID,
		Damage:       target.HP,
	}
	var dead []int
	s.landProjectileLocked(proj, target, &dead)

	got := pendingDeathSourceFor(t, s, target.ID)
	if got.Category != DamageCategoryBasicAttack {
		t.Errorf("ranged (attacker died mid-flight) kill Category = %q; want %q", got.Category, DamageCategoryBasicAttack)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Pierce arrow — projectile.go's tickPierceProjectileLocked, the "attacker
// gone" fallback (the literal this task set explicitly, distinct from the
// shared resolveAttackHitLocked hub the attacker-alive case reuses). Pierce
// reshapes the ARROW, not the damage's origin — still DamageCategoryBasicAttack.
// ─────────────────────────────────────────────────────────────────────────────

func TestDamageCategory_PierceBasicAttack_IsBasicAttack(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	target := spawnProjTestUnit(t, s, enemyPlayerID, 250, 100)
	target.HP = 10

	proj := &Projectile{
		ID:                  "pierce_test",
		OwnerUnitID:         999_999, // nonexistent — attacker resolves nil, exercising the fallback branch
		OwnerPlayerID:       "p1",
		TargetUnitID:        target.ID,
		Damage:              target.HP,
		Pierce:              true,
		PierceMaxHits:       5,
		PierceSecondaryMult: 1.0,
		PierceCorridorWidth: 100,
		PierceLength:        500,
		PierceDirX:          1,
		PierceDirY:          0,
		OriginX:             0,
		OriginY:             100,
		TotalSeconds:        1.0,
		RemainingSeconds:    1.0,
		DamageType:          DamagePhysical,
	}
	var dead []int
	s.tickPierceProjectileLocked(proj, 1.0, &dead)

	got := pendingDeathSourceFor(t, s, target.ID)
	if got.Category != DamageCategoryBasicAttack {
		t.Errorf("pierce kill Category = %q; want %q", got.Category, DamageCategoryBasicAttack)
	}
	if got.Kind != "pierce" {
		t.Fatalf("setup check failed: Kind = %q, want %q (wrong branch exercised)", got.Kind, "pierce")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Ability deal_damage — the full begin-cast → composable-executor →
// on_projectile_impact → deal_damage path (ability_program_registry.go),
// driven through arcane_bolt, a real catalog SchemaVersion:2 ability.
// ─────────────────────────────────────────────────────────────────────────────

func TestDamageCategory_AbilityDealDamage_IsAbility(t *testing.T) {
	boltDef, ok := getAbilityDef("arcane_bolt")
	if !ok {
		t.Fatal(`getAbilityDef("arcane_bolt") missing`)
	}

	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 100, 100)
	caster.Abilities = []string{"arcane_bolt"}
	caster.AttackRange = 300
	caster.CurrentMana = 100
	caster.MaxMana = 100

	enemy := spawnProjTestUnit(t, s, enemyPlayerID, 200, 100)
	enemy.Armor = 0
	enemy.HP = 1 // guaranteed lethal regardless of the exact authored damage

	if ok, reason := s.beginAbilityCastLocked(caster, "arcane_bolt", enemy); !ok {
		t.Fatalf("beginAbilityCastLocked failed: %q", reason)
	}
	s.tickUnitCastLocked(caster, boltDef.CastTime)
	for i := 0; i < 80 && len(s.Projectiles) > 0; i++ {
		s.tickProjectilesLocked(0.05)
	}
	if len(s.Projectiles) != 0 {
		t.Fatal("arcane_bolt projectile never landed")
	}

	got := pendingDeathSourceFor(t, s, enemy.ID)
	if got.Category != DamageCategoryAbility {
		t.Errorf("arcane_bolt kill Category = %q; want %q", got.Category, DamageCategoryAbility)
	}
	if got.SourceAbilityID != "arcane_bolt" {
		t.Errorf("arcane_bolt kill SourceAbilityID = %q; want %q", got.SourceAbilityID, "arcane_bolt")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Ability chain bounce vs. equipment/item chain bounce — both route through
// the SAME beam-bounce mechanism (beam.go's applyBeamPendingDamageLocked),
// distinguished only by whether Beam.SourceAbilityID is set. This is the
// "subtle one" the task called out — verify BOTH branches of the conditional.
// ─────────────────────────────────────────────────────────────────────────────

func TestDamageCategory_AbilityChainBounce_IsAbility(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	const (
		baseDamage  = 500
		falloff     = 10
		bounceRange = 150.0
	)
	ability := buildChainBounceTestAbility(t, "test_dmgcat_chain_ability", baseDamage, falloff, bounceRange)
	registerRuntimeTestAbility(t, ability)

	caster := spawnProjTestUnit(t, s, "p1", 100, 100)
	caster.Abilities = []string{ability.ID}
	caster.MaxMana, caster.CurrentMana = 100, 100

	primary := spawnProjTestUnit(t, s, enemyPlayerID, 300, 100)
	primary.MaxHP, primary.HP = 1_000_000, 1_000_000 // survives the primary hit
	primary.MoveSpeed = 0

	bounceVictim := spawnProjTestUnit(t, s, enemyPlayerID, 400, 100) // within bounceRange of primary
	bounceVictim.MaxHP, bounceVictim.HP = 5, 5                       // dies to the falloff-reduced bounce hit
	bounceVictim.MoveSpeed = 0

	ok, reason := s.beginAbilityCastLocked(caster, ability.ID, primary)
	if !ok {
		t.Fatalf("beginAbilityCastLocked(%q) failed: %q", ability.ID, reason)
	}
	for i := 0; i < 40 && len(s.Beams) > 0; i++ {
		s.tickBeamsLocked(0.05)
	}

	if primary.HP <= 0 {
		t.Fatalf("setup error: primary should have survived the direct hit (HP=%d)", primary.HP)
	}
	got := pendingDeathSourceFor(t, s, bounceVictim.ID)
	if got.Category != DamageCategoryAbility {
		t.Errorf("ability chain-bounce kill Category = %q; want %q", got.Category, DamageCategoryAbility)
	}
	if got.SourceAbilityID != ability.ID {
		t.Errorf("ability chain-bounce kill SourceAbilityID = %q; want %q", got.SourceAbilityID, ability.ID)
	}
}

func TestDamageCategory_NonAbilityChainBounce_IsItem(t *testing.T) {
	s := newDeathPipelineState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 0, Y: 0})
	attacker.Visible = true

	primary := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 100, Y: 0})
	primary.MaxHP, primary.HP = 1_000_000, 1_000_000
	primary.Visible = true

	bounceVictim := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 150, Y: 0})
	bounceVictim.MaxHP, bounceVictim.HP = 5, 5
	bounceVictim.Visible = true

	// Equipment-shaped proc fire: no ability id in the ProcSource (the
	// zero-value contract) — mirrors what a lightning_chain item proc does.
	src := procSourceFromUnit(attacker)
	s.executeProcEffectLocked(src, primary, ProcEffectParams{
		Damage: 500, DamageType: DamageLightning, ProjectileID: "lightning_bolt",
		BounceCount: 1, BounceRange: 200, BounceDamageFalloff: 10,
	})
	for i := 0; i < 40 && len(s.Beams) > 0; i++ {
		s.tickBeamsLocked(0.05)
	}

	if primary.HP <= 0 {
		t.Fatalf("setup error: primary should have survived the direct hit (HP=%d)", primary.HP)
	}
	got := pendingDeathSourceFor(t, s, bounceVictim.ID)
	if got.Category != DamageCategoryItem {
		t.Errorf("non-ability (equipment proc) chain-bounce kill Category = %q; want %q", got.Category, DamageCategoryItem)
	}
	if got.SourceAbilityID != "" {
		t.Errorf("non-ability chain-bounce kill SourceAbilityID = %q; want \"\"", got.SourceAbilityID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Equipment on-hit proc bolt — the SkipOnHitEffects branch of
// landProjectileLocked (projectile.go) when proj.SourceKind is empty (the
// non-ability half of that same conditional).
// ─────────────────────────────────────────────────────────────────────────────

func TestDamageCategory_EquipmentProcBolt_IsItem(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x5EC1)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	attacker.EquipmentBonus.OnHitProcs = []EquipmentProc{
		{Chance: 1.0, Params: ProcEffectParams{Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}},
	}
	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 25, MaxHP: 500}
	s.nextUnitID++
	s.addUnitLocked(target)

	s.rollEquipmentProcsLocked(attacker, target)
	if len(s.Projectiles) != 1 {
		t.Fatalf("expected 1 proc projectile, got %d", len(s.Projectiles))
	}
	proc := s.Projectiles[0]

	var dead []int
	s.landProjectileLocked(proc, target, &dead)

	got := pendingDeathSourceFor(t, s, target.ID)
	if got.Category != DamageCategoryItem {
		t.Errorf("equipment proc bolt kill Category = %q; want %q", got.Category, DamageCategoryItem)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Trap tick — tickTrapEffectsLocked's caltrops in-zone DoT (trap.go).
// ─────────────────────────────────────────────────────────────────────────────

func TestDamageCategory_TrapTick_IsTrap(t *testing.T) {
	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 400, Y: 400})
	enemy.Visible = true
	enemy.MaxHP = 500
	enemy.HP = 1 // guaranteed lethal from a single caltrops DoT proc, whatever the authored DPS

	def := perkDefByID("caltrops")
	if def == nil {
		t.Fatal("caltrops perk def not found")
	}
	trap := placeTrap(s, "caltrops", "p1", 0, 400, 400, def.Config["radius"], 12.0)
	trap.DamagePerSecond = def.Config["damagePerSecond"]

	s.tickTrapEffectsLocked(1.0) // large dt guarantees a DoT proc fires this tick

	got := pendingDeathSourceFor(t, s, enemy.ID)
	if got.Category != DamageCategoryTrap {
		t.Errorf("trap DoT kill Category = %q; want %q", got.Category, DamageCategoryTrap)
	}
	if got.AttackerTrapID != trap.ID {
		t.Errorf("trap DoT kill AttackerTrapID = %q; want %q", got.AttackerTrapID, trap.ID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Building attack — tickBuildingCombatLocked (state_combat.go), a player
// tower firing on a hostile unit within range.
// ─────────────────────────────────────────────────────────────────────────────

func TestDamageCategory_BuildingAttack_IsBuilding(t *testing.T) {
	def, ok := getBuildingDef("tower")
	if !ok || def.Damage <= 0 || def.AttackRange <= 0 || def.AttackSpeed <= 0 {
		t.Fatal(`getBuildingDef("tower") missing or unusable for this test`)
	}

	const cell = 64.0
	cols, rows := 40, 24
	owner := "p1"
	towerRightEdge := 7 * cell // grid (5,5), Width=2 → right edge at col 7
	unitY := (5 + 1) * cell    // vertically centred on the tower
	unitX := towerRightEdge + def.AttackRange*0.3

	tower := protocol.BuildingTile{
		GridCoord:    protocol.GridCoord{X: 5, Y: 5},
		ID:           "dmgcat-tower",
		BuildingType: "tower",
		Width:        2,
		Height:       2,
		Visible:      true,
		OwnerID:      &owner,
		Metadata:     map[string]interface{}{"hp": 500.0, "maxHp": 500.0},
	}
	cfg := protocol.MapConfig{
		ID: "damage-category-building-test", Name: "damage-category-building-test",
		Width: float64(cols) * cell, Height: float64(rows) * cell,
		GridCols: cols, GridRows: rows, CellSize: cell,
		Buildings: []protocol.BuildingTile{tower},
	}
	s := NewGameStateWithSeed(cfg, 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: unitX, Y: unitY})
	enemy.Visible = true
	enemy.MaxHP = 1000
	enemy.HP = def.Damage // exact-kill

	// First tick: no target locked yet, cooldown==0 → arms the windup.
	s.tickBuildingCombatLocked(0.05)
	// Second tick: dt covers the full windup → fires.
	windupSeconds := attackDamageDeliveryFraction * (1.0 / def.AttackSpeed)
	s.tickBuildingCombatLocked(windupSeconds + 0.01)

	got := pendingDeathSourceFor(t, s, enemy.ID)
	if got.Category != DamageCategoryBuilding {
		t.Errorf("tower kill Category = %q; want %q", got.Category, DamageCategoryBuilding)
	}
	if got.AttackerBuildingID != "dmgcat-tower" {
		t.Errorf("tower kill AttackerBuildingID = %q; want %q", got.AttackerBuildingID, "dmgcat-tower")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Redirect/propagation sites: pain_share's redirect does NOT hard-code a
// category — it forwards the ORIGIN damage's own Category, because it is the
// same damage instance fanned out to a different victim, not a new one. This
// is the rule that distinguishes it from a Perk-created bonus hit
// (savage_strikes, retaliation, etc.).
// ─────────────────────────────────────────────────────────────────────────────

func TestDamageCategory_PainShareRedirect_PropagatesOriginCategory(t *testing.T) {
	s, vanguard, ally := newPainShareState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	vanguard.HP = 1 // guaranteed lethal from any nonzero redirected share
	ally.HP = 500

	src := DamageSource{
		AttackerUnitID: ally.ID + vanguard.ID + 1, // arbitrary nonzero attribution, not itself under test
		Kind:           "melee",
		Category:       DamageCategoryBasicAttack,
	}
	s.applyUnitDamageWithSourceLocked(ally, 100, src)

	got := pendingDeathSourceFor(t, s, vanguard.ID)
	if got.Category != DamageCategoryBasicAttack {
		t.Errorf("pain_share redirect Category = %q; want %q (propagated from the origin hit, not DamageCategoryPerk)", got.Category, DamageCategoryBasicAttack)
	}
}
