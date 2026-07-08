package game

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

func dist(ax, ay, bx, by float64) float64 { return math.Hypot(ax-bx, ay-by) }

// A pulled unit is dragged toward the center each tick (via the real Update
// gate), monotonically closing the distance, and never overshoots.
func TestPull_DragsTowardCenterEachTick(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	u := spawnProjTestUnit(t, s, "p1", 300, 100)
	cx, cy := 100.0, 100.0
	s.applyPullLocked(u, cx, cy, 200, 1.0) // 200px/s for 1s
	startDist := dist(u.X, u.Y, cx, cy)
	s.mu.Unlock()

	prev := startDist
	for i := 0; i < 12; i++ {
		s.Update(0.1)
		s.mu.Lock()
		d := dist(u.X, u.Y, cx, cy)
		s.mu.Unlock()
		if d > prev+1e-6 {
			t.Fatalf("distance increased tick %d: %v → %v (pull should only close)", i, prev, d)
		}
		prev = d
	}
	if prev > 1.0 {
		t.Errorf("after full pull, distance to center = %v; want ~0 (reached, no overshoot)", prev)
	}
}

// Overshoot guard: a huge single-tick step snaps to the center exactly.
func TestPull_NoOvershoot(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	u := spawnProjTestUnit(t, s, "p1", 300, 100)
	s.applyPullLocked(u, 100, 100, 100000, 1.0)
	s.tickUnitPullLocked(u, 0.1)
	if u.X != 100 || u.Y != 100 {
		t.Errorf("unit at (%v,%v); want snapped to center (100,100)", u.X, u.Y)
	}
}

// Only hostiles (relative to the caster) within radius are pulled; allies and
// the caster are never displaced.
func TestPull_EnemiesOnly(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := spawnProjTestUnit(t, s, "p1", 100, 100)
	ally := spawnProjTestUnit(t, s, "p1", 120, 100)
	enemy := spawnProjTestUnit(t, s, enemyPlayerID, 130, 100)

	n := s.applyPullInRadiusLocked(caster, 125, 100, 100, 200, 1.0)
	if n != 1 {
		t.Errorf("affected = %d; want 1 (enemy only)", n)
	}
	if enemy.PullRemaining <= 0 {
		t.Error("enemy should be pulled")
	}
	if ally.PullRemaining != 0 || caster.PullRemaining != 0 {
		t.Error("ally/caster must never be pulled")
	}
}

// A dead unit inside the radius is not pulled (dropped from consideration).
func TestPull_SkipsDead(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	caster := spawnProjTestUnit(t, s, "p1", 100, 100)
	enemy := spawnProjTestUnit(t, s, enemyPlayerID, 120, 100)
	enemy.HP = 0
	if n := s.applyPullInRadiusLocked(caster, 120, 100, 100, 200, 1.0); n != 0 {
		t.Errorf("affected = %d; want 0 (dead enemy skipped)", n)
	}
}

// On expiry the pull clears and drops the stale path so the unit resumes from
// its displaced position with no snap-back.
func TestPull_ResumesCleanlyNoSnapBack(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	u := spawnProjTestUnit(t, s, "p1", 300, 100)
	u.Moving = true
	u.Path = []protocol.Vec2{{X: 500, Y: 100}} // stale pre-pull path
	s.applyPullLocked(u, 100, 100, 200, 0.1)
	s.tickUnitPullLocked(u, 0.1) // duration elapses → endUnitPullLocked
	if u.PullRemaining != 0 || u.PullStrength != 0 {
		t.Errorf("pull state not cleared: remaining=%v strength=%v", u.PullRemaining, u.PullStrength)
	}
	if u.Moving || u.Path != nil {
		t.Errorf("stale path/moving not dropped: moving=%v path=%v", u.Moving, u.Path)
	}
}

// Displacement is deterministic under a seed.
func TestPull_Deterministic(t *testing.T) {
	run := func() (float64, float64) {
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 7)
		s.mu.Lock()
		u := s.spawnPlayerUnitLocked("soldier", "p1", "#fff", protocol.Vec2{X: 300, Y: 250})
		u.Visible = true
		s.applyPullLocked(u, 100, 100, 175, 1.0)
		s.mu.Unlock()
		for i := 0; i < 8; i++ {
			s.Update(0.1)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		return u.X, u.Y
	}
	ax, ay := run()
	bx, by := run()
	if ax != bx || ay != by {
		t.Errorf("non-deterministic pull: (%v,%v) vs (%v,%v)", ax, ay, bx, by)
	}
}
