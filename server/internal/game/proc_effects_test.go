package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestResolveProcEffectParams_OverridePrecedence: a non-zero override field
// replaces the def's value; a zero field keeps the def's. Covers every
// overridable knob so a new field can't silently skip precedence.
func TestResolveProcEffectParams_OverridePrecedence(t *testing.T) {
	def := ProcEffectDef{
		ID: "test_effect",
		ProcEffectParams: ProcEffectParams{
			Damage: 25, DamageType: DamageLightning, ProjectileID: "lightning_bolt",
			ProjectileScale: 1, BounceCount: 2, BounceRange: 200, BounceDamageFalloff: 5,
			SlowMultiplier: 0.75, SlowDurationSeconds: 2,
			BurnDamagePerSecond: 8, BurnDurationSeconds: 3,
		},
	}

	// Zero overrides ⇒ params identical to the def's payload.
	if got := resolveProcEffectParams(def, ProcEffectOverrides{}); got != def.ProcEffectParams {
		t.Errorf("zero overrides must return the def's params verbatim:\n got %+v\nwant %+v", got, def.ProcEffectParams)
	}

	// Every knob overridden ⇒ every knob replaced; identity fields untouched.
	o := ProcEffectOverrides{
		Damage: 40, ProjectileScale: 3, BounceCount: 4, BounceRange: 300, BounceDamageFalloff: 10,
		SlowMultiplier: 0.5, SlowDurationSeconds: 4, BurnDamagePerSecond: 12, BurnDurationSeconds: 6,
	}
	got := resolveProcEffectParams(def, o)
	want := ProcEffectParams{
		Damage: 40, DamageType: DamageLightning, ProjectileID: "lightning_bolt",
		ProjectileScale: 3, BounceCount: 4, BounceRange: 300, BounceDamageFalloff: 10,
		SlowMultiplier: 0.5, SlowDurationSeconds: 4, BurnDamagePerSecond: 12, BurnDurationSeconds: 6,
	}
	if got != want {
		t.Errorf("full overrides:\n got %+v\nwant %+v", got, want)
	}

	// Partial override: one field replaced, the rest keep the def's values.
	partial := resolveProcEffectParams(def, ProcEffectOverrides{BounceCount: 4})
	if partial.BounceCount != 4 {
		t.Errorf("BounceCount override lost: got %d, want 4", partial.BounceCount)
	}
	if partial.Damage != 25 || partial.BounceRange != 200 || partial.SlowMultiplier != 0.75 {
		t.Errorf("non-overridden fields must keep def values, got %+v", partial)
	}
	// Identity fields can never change through overrides.
	if partial.DamageType != DamageLightning || partial.ProjectileID != "lightning_bolt" {
		t.Errorf("identity fields mutated: %+v", partial)
	}
}

// TestExecuteProcEffect_NonUnitSource_Projectile: a proc effect fired from a
// sourceless origin (OwnerUnitID == 0 — a future trap/building) spawns a bolt
// from the source coordinates, lands its damage, and never panics — even when
// the hit kills the target (no kill credit / XP to award).
func TestExecuteProcEffect_NonUnitSource_Projectile(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xA110)
	s.mu.Lock()
	defer s.mu.Unlock()

	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 20, MaxHP: 20, X: 50, Y: 60}
	s.nextUnitID++
	s.addUnitLocked(target)

	src := ProcSource{OwnerUnitID: 0, OwnerPlayerID: "p1", OriginX: 10, OriginY: 20}
	p := ProcEffectParams{Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}
	s.executeProcEffectLocked(src, target, p)

	if len(s.Projectiles) != 1 {
		t.Fatalf("expected 1 projectile, got %d", len(s.Projectiles))
	}
	proj := s.Projectiles[0]
	if proj.OwnerUnitID != 0 || proj.OwnerPlayerID != "p1" {
		t.Errorf("owner fields: unit=%d player=%q, want 0 / p1", proj.OwnerUnitID, proj.OwnerPlayerID)
	}
	if proj.OriginX != 10 || proj.OriginY != 20 {
		t.Errorf("origin = (%v,%v), want (10,20) — the source coords, not a unit", proj.OriginX, proj.OriginY)
	}
	if !proj.SkipOnHitEffects {
		t.Error("proc bolt must skip the on-hit hub")
	}

	// Landing a killing blow with no owner unit must not panic and must apply.
	dead := []int{}
	s.landProjectileLocked(proj, target, &dead)
	if target.HP != 0 {
		t.Errorf("target HP = %d, want 0 (25 damage vs 20 HP)", target.HP)
	}
	if len(dead) != 1 || dead[0] != target.ID {
		t.Errorf("dead list = %v, want [%d]", dead, target.ID)
	}
}

// TestExecuteProcEffect_NonUnitSource_BeamChain: a beam-kind effect from a
// non-unit source zaps from the source coords, defers its damage, and chains
// using the source's OWNER PLAYER for hostility. No unit anywhere on the
// firing side.
func TestExecuteProcEffect_NonUnitSource_BeamChain(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xA111)
	s.mu.Lock()
	defer s.mu.Unlock()

	t0 := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 500, MaxHP: 500, X: 100, Y: 0}
	s.nextUnitID++
	s.addUnitLocked(t0)
	t1 := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 500, MaxHP: 500, X: 150, Y: 0}
	s.nextUnitID++
	s.addUnitLocked(t1)

	src := ProcSource{OwnerUnitID: 0, OwnerPlayerID: "p1", OriginX: 0, OriginY: 0}
	p := ProcEffectParams{
		Damage: 25, DamageType: DamageLightning, ProjectileID: "lightning_bolt",
		BounceCount: 1, BounceRange: 200, BounceDamageFalloff: 5,
	}
	s.executeProcEffectLocked(src, t0, p)

	// Primary + one bounce = two momentary beams; primary leaves the source
	// coords with no caster unit.
	if len(s.Beams) != 2 {
		t.Fatalf("expected 2 beams (primary + bounce), got %d", len(s.Beams))
	}
	primary := s.Beams[0]
	if primary.CasterUnitID != 0 || primary.AttackerUnitID != 0 {
		t.Errorf("non-unit primary beam caster/attacker = %d/%d, want 0/0", primary.CasterUnitID, primary.AttackerUnitID)
	}
	if primary.OriginX != 0 || primary.OriginY != 0 {
		t.Errorf("primary origin = (%v,%v), want source coords (0,0)", primary.OriginX, primary.OriginY)
	}
	bounce := s.Beams[1]
	if bounce.TargetUnitID != t1.ID {
		t.Errorf("bounce target = %d, want %d", bounce.TargetUnitID, t1.ID)
	}
	if bounce.CasterUnitID != t0.ID {
		t.Errorf("bounce visually leaves the previous victim: caster = %d, want %d", bounce.CasterUnitID, t0.ID)
	}

	// Deferred damage lands without an owner unit and without panicking.
	hp0, hp1 := t0.HP, t1.HP
	s.tickBeamsLocked(beamProcDamageDelaySeconds + 0.01)
	if t0.HP != hp0-25 {
		t.Errorf("primary damage: HP %d→%d, want -25", hp0, t0.HP)
	}
	if t1.HP != hp1-20 {
		t.Errorf("bounce damage (falloff 5): HP %d→%d, want -20", hp1, t1.HP)
	}
}

// TestExecuteProcEffect_UnitSourceParity: procSourceFromUnit + execute spawns
// a projectile identical (owner, origin, scale fallback) to what the old
// equipment path produced — the migration must be behavior-preserving.
func TestExecuteProcEffect_UnitSourceParity(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xA112)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 5, Y: 7})
	attacker.ProjectileScale = 1.5
	target := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 500, MaxHP: 500, X: 50, Y: 0}
	s.nextUnitID++
	s.addUnitLocked(target)

	// Params with no scale of their own inherit the firing UNIT's scale.
	p := ProcEffectParams{Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"}
	s.executeProcEffectLocked(procSourceFromUnit(attacker), target, p)
	if len(s.Projectiles) != 1 {
		t.Fatalf("expected 1 projectile, got %d", len(s.Projectiles))
	}
	proj := s.Projectiles[0]
	if proj.OwnerUnitID != attacker.ID || proj.Scale != 1.5 {
		t.Errorf("owner=%d scale=%v, want %d / 1.5 (unit-scale fallback)", proj.OwnerUnitID, proj.Scale, attacker.ID)
	}
}
