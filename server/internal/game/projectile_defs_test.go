package game

import (
	"math/rand"
	"testing"

	"webrts/server/pkg/protocol"
)

// newProjectileTestState returns a GameState with seed 42. Lock is NOT held
// on return (callers take s.mu themselves, matching the package convention).
func newProjectileTestState(t *testing.T) *GameState {
	t.Helper()
	return NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
}

// spawnProjTestUnit spawns a visible, full-HP soldier owned by owner at
// (x,y). Caller holds s.mu.
func spawnProjTestUnit(t *testing.T, s *GameState, owner string, x, y float64) *Unit {
	t.Helper()
	u := s.spawnPlayerUnitLocked("soldier", owner, "#3498db", protocol.Vec2{X: x, Y: y})
	u.MaxHP = 500
	u.HP = 500
	u.Visible = true
	u.AttackRange = 80
	u.Damage = 10
	u.AttackSpeed = 1.0
	u.MoveSpeed = 150
	s.initializeCombatUnitLocked(u)
	return u
}

// ── Registration & lookup ────────────────────────────────────────────────────

func TestProjectileDef_RegistrationAndLookup(t *testing.T) {
	def, ok := getProjectileDef("fire_bolt")
	if !ok {
		t.Fatal(`getProjectileDef("fire_bolt") = _, false; want the registered fire_bolt def`)
	}
	if def.ID != "fire_bolt" {
		t.Errorf("def.ID = %q; want %q", def.ID, "fire_bolt")
	}
	if def.FollowEffect != "" {
		t.Errorf("fire_bolt FollowEffect = %q; want \"\" (the bolt itself is the visual)", def.FollowEffect)
	}

	if _, ok := getProjectileDef("does_not_exist"); ok {
		t.Error(`getProjectileDef("does_not_exist") returned ok=true; want false for an unregistered id`)
	}

	all := ListProjectileDefs()
	found := false
	for i, d := range all {
		if d.ID == "fire_bolt" {
			found = true
		}
		if i > 0 && all[i-1].ID > d.ID {
			t.Errorf("ListProjectileDefs not sorted by id: %q before %q", all[i-1].ID, d.ID)
		}
	}
	if !found {
		t.Error("ListProjectileDefs() did not include fire_bolt")
	}
}

// fire_bolt's speed must track the codebase's existing projectile speed
// standard rather than an independently invented number.
func TestProjectileDef_FireBoltSpeedMatchesStandard(t *testing.T) {
	def, ok := getProjectileDef("fire_bolt")
	if !ok {
		t.Fatal("fire_bolt not registered")
	}
	if def.Speed != defaultProjectileSpeed {
		t.Errorf("fire_bolt Speed = %v; want defaultProjectileSpeed (%v) — the established standard", def.Speed, defaultProjectileSpeed)
	}
}

// ── Hit/miss: target HAS dodge/block stats ───────────────────────────────────

func TestProjectileHit_WithEvasionStats(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Note: the old "avoid >= 1.0 always misses" sub-case is gone — under the
	// 75% cap (see evasionCapTotal), total avoidance is impossible by design.
	// TestAttackHits_CapGuaranteesHits (evasion_stats_test.go) covers that
	// cap behavior directly.

	// A partial chance must be a single deterministic roll against the
	// per-match combat RNG: same seed → same outcome sequence.
	s2 := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	ev := TargetEvasion{DodgeChance: 0.5}
	for i := 0; i < 20; i++ {
		a, _ := s.attackHitsLocked(ev)
		b, _ := s2.attackHitsLocked(ev)
		if a != b {
			t.Fatalf("partial-evasion roll #%d not deterministic for equal seeds: %v vs %v", i, a, b)
		}
	}
}

// ── Hit/miss: target has NO dodge/block stats (always hits) ───────────────────

func TestProjectileHit_NoEvasionStatsAlwaysHits(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := 0; i < 100; i++ {
		if hit, _ := s.attackHitsLocked(TargetEvasion{}); !hit {
			t.Fatalf("attackHitsLocked(zero evasion) = false on iter %d; want always-hit when target has no dodge/block", i)
		}
	}

	// Every unit now carries the game-wide base dodge, so a real spawned
	// unit's profile is non-zero by design.
	u := spawnProjTestUnit(t, s, "p1", 400, 400)
	if ev := evasionForUnit(u); ev.DodgeChance != baseUnitDodgeChance || ev.BlockChance != 0 {
		t.Errorf("evasionForUnit(spawned soldier) = %+v; want base dodge %v / block 0", ev, baseUnitDodgeChance)
	}
}

// The no-evasion path must not consume the combat RNG, otherwise it would
// shift outcomes for any future evasion-bearing unit depending on call order
// (and is what keeps existing seeded behaviour byte-for-byte unchanged today).
func TestProjectileHit_NoEvasionDoesNotConsumeRNG(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := 0; i < 50; i++ {
		s.attackHitsLocked(TargetEvasion{})
	}
	got := s.rngCombat.Float64()

	// A fresh, untouched combat stream for the same seed.
	const saltCombat int64 = 0x5
	fresh := rand.New(rand.NewSource(int64(42) ^ saltCombat)).Float64()
	if got != fresh {
		t.Errorf("combat RNG advanced by no-evasion calls: got %v, untouched stream yields %v", got, fresh)
	}
}

// ── Damage application: target only, no AoE / pass-through ────────────────────

func TestApplyProjectileDamage_TargetOnlyNoAoE(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := spawnProjTestUnit(t, s, "p1", 100, 100)
	target := spawnProjTestUnit(t, s, "enemy", 400, 400)
	// Bystander adjacent to the target — would be caught by any splash/AoE.
	bystander := spawnProjTestUnit(t, s, "enemy", 401, 401)

	targetStart := target.HP
	bystanderStart := bystander.HP

	const dmg = 37
	s.applyProjectileDamageLocked(attacker, target, dmg)

	if got := targetStart - target.HP; got != dmg {
		t.Errorf("target took %d damage; want exactly %d", got, dmg)
	}
	if bystander.HP != bystanderStart {
		t.Errorf("adjacent bystander HP changed %d → %d; projectile damage must be single-target (no AoE)", bystanderStart, bystander.HP)
	}
}

func TestApplyProjectileDamage_NoopGuards(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := spawnProjTestUnit(t, s, "p1", 100, 100)
	target := spawnProjTestUnit(t, s, "enemy", 400, 400)
	start := target.HP

	s.applyProjectileDamageLocked(attacker, target, 0)
	s.applyProjectileDamageLocked(attacker, target, -5)
	s.applyProjectileDamageLocked(attacker, nil, 50) // nil target must not panic
	if target.HP != start {
		t.Errorf("target HP changed on no-op (zero/negative damage); %d → %d", start, target.HP)
	}

	// Anonymous attacker (nil) still lands damage on the target.
	s.applyProjectileDamageLocked(nil, target, 10)
	if target.HP != start-10 {
		t.Errorf("nil-attacker projectile: target HP = %d; want %d", target.HP, start-10)
	}
}

func TestValidateProjectileDef(t *testing.T) {
	t.Run("normalizes empty kind to projectile", func(t *testing.T) {
		def := ProjectileDef{ID: "x"}
		if err := validateProjectileDef(&def); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if def.Kind != EmitterKindProjectile {
			t.Fatalf("kind not normalized: %q", def.Kind)
		}
	})
	t.Run("rejects an invalid kind", func(t *testing.T) {
		if err := validateProjectileDef(&ProjectileDef{ID: "x", Kind: "laser"}); err == nil {
			t.Fatal("expected error for invalid kind")
		}
	})
	t.Run("defaults beam duration and projectile speed", func(t *testing.T) {
		beam := ProjectileDef{ID: "b", Kind: EmitterKindBeam}
		_ = validateProjectileDef(&beam)
		if beam.DurationMs != defaultBeamDurationMs {
			t.Fatalf("beam DurationMs=%d, want %d", beam.DurationMs, defaultBeamDurationMs)
		}
		proj := ProjectileDef{ID: "p"}
		_ = validateProjectileDef(&proj)
		if proj.Speed != defaultProjectileSpeed {
			t.Fatalf("Speed=%v, want %v", proj.Speed, defaultProjectileSpeed)
		}
	})
}
