package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// beamProcTarget spawns a hostile with plenty of HP so a proc can't kill it,
// letting damage-delta assertions stay clean. Position is offset from the
// attacker so the frozen beam endpoints are distinct.
func beamProcTarget(t *testing.T, s *GameState) *Unit {
	t.Helper()
	u := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 1_000_000, MaxHP: 1_000_000, X: 60, Y: 0}
	s.nextUnitID++
	s.addUnitLocked(u)
	return u
}

// TestOnHitProc_BeamKindFiresBeamNotProjectile asserts that a proc whose
// emitter def is EmitterKindBeam (lightning_bolt) spawns a momentary Beam with
// frozen endpoints and DEFERRED damage — and spawns NO flying projectile. This
// is the whole point of the kind switch.
func TestOnHitProc_BeamKindFiresBeamNotProjectile(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB0B)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Precondition: lightning_bolt must be a beam in the catalog, else this
	// test is asserting nothing meaningful.
	def, ok := getProjectileDef("lightning_bolt")
	if !ok || !def.IsBeam() {
		t.Fatalf("precondition: lightning_bolt must be an EmitterKindBeam def, got kind %q ok=%v", def.Kind, ok)
	}

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	target := beamProcTarget(t, s)

	procParams := ProcEffectParams{Damage: 25, DamageType: DamageLightning, ProjectileID: "lightning_bolt"}

	hpBefore := target.HP
	projBefore := len(s.Projectiles)
	s.executeProcEffectLocked(procSourceFromUnit(attacker), target, procParams)

	if len(s.Projectiles) != projBefore {
		t.Errorf("beam-kind proc must not spawn a flying projectile, got %d new", len(s.Projectiles)-projBefore)
	}
	if len(s.Beams) != 1 {
		t.Fatalf("beam-kind proc should spawn exactly one beam, got %d", len(s.Beams))
	}
	b := s.Beams[0]
	if !b.Momentary {
		t.Errorf("proc beam should be momentary")
	}
	if b.Variant != "lightning_bolt" {
		t.Errorf("proc beam variant = %q, want lightning_bolt", b.Variant)
	}
	if b.RemainingSeconds <= 0 {
		t.Errorf("momentary beam should start with a positive lifetime, got %v", b.RemainingSeconds)
	}
	// Endpoints frozen at the participants' positions.
	if b.OriginX != attacker.X || b.OriginY != attacker.Y {
		t.Errorf("beam origin (%v,%v) should be frozen at attacker (%v,%v)", b.OriginX, b.OriginY, attacker.X, attacker.Y)
	}
	if b.TargetX != target.X || b.TargetY != target.Y {
		t.Errorf("beam target (%v,%v) should be frozen at target (%v,%v)", b.TargetX, b.TargetY, target.X, target.Y)
	}
	// Damage is DEFERRED, not instant: the target's HP must be untouched at
	// fire time, with the proc's damage scheduled on the beam so it lands on a
	// later tick (and pops as its own number instead of merging into the hit).
	if target.HP != hpBefore {
		t.Errorf("beam proc damage must be deferred, but HP changed %d→%d at fire time", hpBefore, target.HP)
	}
	if b.PendingDamage != procParams.Damage {
		t.Errorf("beam should carry the deferred proc damage: PendingDamage=%d, want %d", b.PendingDamage, procParams.Damage)
	}
	if b.DamageDelayRemaining <= 0 {
		t.Errorf("beam should carry a positive damage delay, got %v", b.DamageDelayRemaining)
	}
}

// TestOnHitProc_BeamDamageLandsAfterDelay drives the deferral: the proc's damage
// stays pending until the delay elapses, then lands exactly once. The delay is
// read from the server constant (not hardcoded) so tuning it can't break this.
func TestOnHitProc_BeamDamageLandsAfterDelay(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB0F)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	target := beamProcTarget(t, s)
	procParams := ProcEffectParams{Damage: 25, DamageType: DamageLightning, ProjectileID: "lightning_bolt"}

	hpBefore := target.HP
	s.executeProcEffectLocked(procSourceFromUnit(attacker), target, procParams)

	// A tick shorter than the delay leaves the damage pending.
	s.tickBeamsLocked(beamProcDamageDelaySeconds / 2)
	if target.HP != hpBefore {
		t.Fatalf("damage landed too early: HP changed %d→%d before the delay elapsed", hpBefore, target.HP)
	}
	// Ticking past the remaining delay lands the proc's damage exactly once.
	s.tickBeamsLocked(beamProcDamageDelaySeconds)
	if got := hpBefore - target.HP; got != procParams.Damage {
		t.Fatalf("after the delay the beam should deal its damage once: HP dropped %d, want %d", got, procParams.Damage)
	}
	// Further ticks must not re-apply it.
	hpAfter := target.HP
	s.tickBeamsLocked(1.0)
	if target.HP != hpAfter {
		t.Fatalf("deferred beam damage must land exactly once: HP moved %d→%d on a later tick", hpAfter, target.HP)
	}
}

// TestMomentaryBeam_DecaysToRemoval verifies a momentary beam is culled once
// its lifetime elapses and survives a shorter tick. The lifetime is derived
// from the catalog def (durationMs), not pinned, so a balance tweak can't
// break this.
func TestMomentaryBeam_DecaysToRemoval(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB0C)
	s.mu.Lock()
	defer s.mu.Unlock()

	def, ok := getProjectileDef("lightning_bolt")
	if !ok || !def.IsBeam() {
		t.Fatalf("precondition: lightning_bolt must be a beam def")
	}
	lifetime := float64(def.DurationMs) / 1000.0

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	target := beamProcTarget(t, s)
	s.spawnMomentaryBeamLocked(attacker, target, "lightning_bolt", def.DurationMs)

	// A tick shorter than the lifetime keeps the flash on screen.
	s.tickBeamsLocked(lifetime / 2)
	if len(s.Beams) != 1 {
		t.Fatalf("beam should still be alive after half its lifetime, have %d", len(s.Beams))
	}
	// Ticking past the remaining lifetime removes it.
	s.tickBeamsLocked(lifetime)
	if len(s.Beams) != 0 {
		t.Fatalf("beam should be culled once its lifetime elapses, have %d", len(s.Beams))
	}
}

// TestMomentaryBeam_SurvivesParticipantRemoval asserts a momentary beam is NOT
// dropped when its target (or caster) is removed — a proc zap that kills its
// target must still flash. Channel beams keep the opposite behavior, which the
// existing siphon tests cover.
func TestMomentaryBeam_SurvivesParticipantRemoval(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB0D)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	target := beamProcTarget(t, s)
	s.spawnMomentaryBeamLocked(attacker, target, "lightning_bolt", 260)

	s.removeBeamForTargetLocked(target.ID)
	if len(s.Beams) != 1 {
		t.Fatalf("momentary beam must survive its target's removal, have %d", len(s.Beams))
	}
	s.removeBeamForUnitLocked(attacker.ID)
	if len(s.Beams) != 1 {
		t.Fatalf("momentary beam must survive its caster's removal, have %d", len(s.Beams))
	}
}

// spawnBeamEnemyAt drops a hostile with plenty of HP at (x,y) for chain tests.
func spawnBeamEnemyAt(t *testing.T, s *GameState, x, y float64) *Unit {
	t.Helper()
	u := &Unit{ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier", Visible: true, HP: 1_000_000, MaxHP: 1_000_000, X: x, Y: y}
	s.nextUnitID++
	s.addUnitLocked(u)
	return u
}

// TestOnHitProc_BeamBouncesToAdditionalTargets drives the chain: a beam proc
// with BounceCount=2 arcs off the primary target to two further enemies, each
// hop leaving the PREVIOUS victim and losing BounceDamageFalloff damage. Damage
// expectations are derived from the proc fields (not pinned), and every hop's
// kill credit stays with the original attacker.
func TestOnHitProc_BeamBouncesToAdditionalTargets(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB11)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	// A line of enemies within bounce range of each other: the chain must pick
	// the nearest not-yet-hit hostile from the last victim each hop.
	t0 := spawnBeamEnemyAt(t, s, 60, 0)  // primary
	e1 := spawnBeamEnemyAt(t, s, 120, 0) // nearest to t0
	e2 := spawnBeamEnemyAt(t, s, 200, 0) // nearest to e1

	procParams := ProcEffectParams{
		Damage: 25, DamageType: DamageLightning, ProjectileID: "lightning_bolt",
		BounceCount: 2, BounceRange: 200, BounceDamageFalloff: 5,
	}

	hp0, hp1, hp2 := t0.HP, e1.HP, e2.HP
	s.executeProcEffectLocked(procSourceFromUnit(attacker), t0, procParams)

	// One primary + two bounce beams, none of them projectiles.
	if len(s.Projectiles) != 0 {
		t.Errorf("beam proc must not spawn projectiles, got %d", len(s.Projectiles))
	}
	if len(s.Beams) != 3 {
		t.Fatalf("expected 3 beams (primary + 2 bounces), got %d", len(s.Beams))
	}

	byTarget := make(map[int]*Beam, 3)
	for _, b := range s.Beams {
		byTarget[b.TargetUnitID] = b
	}
	// Expected chain: attacker→t0 (25), t0→e1 (20), e1→e2 (15). The visual
	// origin (CasterUnitID) is the PREVIOUS unit; credit (AttackerUnitID) is
	// always the attacker.
	type hop struct {
		target       *Unit
		visualOrigin int
		wantDamage   int
	}
	hops := []hop{
		{t0, attacker.ID, procParams.Damage},                               // 25
		{e1, t0.ID, procParams.Damage - procParams.BounceDamageFalloff},   // 20
		{e2, e1.ID, procParams.Damage - procParams.BounceDamageFalloff*2}, // 15
	}
	for i, h := range hops {
		b := byTarget[h.target.ID]
		if b == nil {
			t.Fatalf("hop %d: no beam targeting unit %d", i, h.target.ID)
		}
		if b.PendingDamage != h.wantDamage {
			t.Errorf("hop %d: PendingDamage=%d, want %d", i, b.PendingDamage, h.wantDamage)
		}
		if b.CasterUnitID != h.visualOrigin {
			t.Errorf("hop %d: visual origin CasterUnitID=%d, want %d (previous victim)", i, b.CasterUnitID, h.visualOrigin)
		}
		if b.AttackerUnitID != attacker.ID {
			t.Errorf("hop %d: AttackerUnitID=%d, want %d (credit stays with the wielder)", i, b.AttackerUnitID, attacker.ID)
		}
	}

	// After the shared delay every hop's damage lands on its own target.
	s.tickBeamsLocked(beamProcDamageDelaySeconds)
	if got := hp0 - t0.HP; got != procParams.Damage {
		t.Errorf("primary took %d, want %d", got, procParams.Damage)
	}
	if got := hp1 - e1.HP; got != procParams.Damage-procParams.BounceDamageFalloff {
		t.Errorf("bounce 1 took %d, want %d", got, procParams.Damage-procParams.BounceDamageFalloff)
	}
	if got := hp2 - e2.HP; got != procParams.Damage-procParams.BounceDamageFalloff*2 {
		t.Errorf("bounce 2 took %d, want %d", got, procParams.Damage-procParams.BounceDamageFalloff*2)
	}
}

// TestOnHitProc_BeamBounceStopsWhenAttenuated asserts the chain ends early once
// falloff would drop a hop's damage to <= 0, rather than firing 0-damage arcs.
func TestOnHitProc_BeamBounceStopsWhenAttenuated(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB12)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	t0 := spawnBeamEnemyAt(t, s, 60, 0)
	spawnBeamEnemyAt(t, s, 120, 0)
	spawnBeamEnemyAt(t, s, 200, 0)

	// Damage 8, falloff 5: hop1 = 3 (fires), hop2 = -2 (stops). So primary + 1
	// bounce = 2 beams, even though BounceCount allows 2.
	s.executeProcEffectLocked(procSourceFromUnit(attacker), t0, ProcEffectParams{
		Damage: 8, DamageType: DamageLightning, ProjectileID: "lightning_bolt",
		BounceCount: 2, BounceRange: 200, BounceDamageFalloff: 5,
	})

	if len(s.Beams) != 2 {
		t.Fatalf("attenuated chain should stop after the affordable hop: want 2 beams, got %d", len(s.Beams))
	}
}

// TestLightningSword_ProcCastsChainLightning guards the catalog wiring: the
// shipped lightning_sword proc now CASTS the Chain Lightning ability (the
// full-circle wiring) instead of firing a bespoke chaining beam effect.
func TestLightningSword_ProcCastsChainLightning(t *testing.T) {
	def, ok := getItemDef("lightning_sword")
	if !ok {
		t.Fatal("lightning_sword not in catalog")
	}
	p := firstProcFor(t, def, ProcOnHit)
	if p.Ability == "" {
		t.Fatalf("lightning_sword proc should cast an ability, got ability=%q", p.Ability)
	}
	adef, ok := getAbilityDef(p.Ability)
	if !ok {
		t.Fatalf("lightning_sword proc ability %q is not a registered ability", p.Ability)
	}
	if adef.DamageType != DamageLightning {
		t.Errorf("lightning_sword proc ability %q damageType = %q, want lightning", p.Ability, adef.DamageType)
	}
}

// TestOnHitProc_ProjectileKindStillFiresProjectile guards the default path: a
// proc whose emitter def is a projectile (fire_bolt) — or names an unknown id —
// still spawns a flying projectile and no beam.
func TestOnHitProc_ProjectileKindStillFiresProjectile(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xB0E)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("acolyte", "p1", "#fff", protocol.Vec2{X: 0, Y: 0})
	target := beamProcTarget(t, s)

	s.executeProcEffectLocked(procSourceFromUnit(attacker), target, ProcEffectParams{Damage: 25, DamageType: DamageFire, ProjectileID: "fire_bolt"})

	if len(s.Projectiles) != 1 {
		t.Fatalf("projectile-kind proc should spawn one projectile, got %d", len(s.Projectiles))
	}
	if len(s.Beams) != 0 {
		t.Fatalf("projectile-kind proc must not spawn a beam, got %d", len(s.Beams))
	}
}
