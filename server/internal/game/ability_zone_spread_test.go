package game

import (
	"encoding/json"
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// zoneSpreadPosition: a create_zone with Count>1 fans its copies out to either
// side of the CAST LINE (perpendicular to caster->target), centered on the
// target. A horizontal cast fans the copies vertically (one above the target,
// one below); a cast from above fans them horizontally ("O Q O"). They alternate
// sides at growing distance — back and forth — rather than piling onto one side.
// These tests pin that perpendicular, alternating fan.
// ─────────────────────────────────────────────────────────────────────────────

func TestZoneSpread_PrimaryOnCenterExtrasAlternateSides(t *testing.T) {
	// Caster below, target straight above: the cast axis is +Y, so the
	// perpendicular fan axis is X. A copy's X offset from center therefore tells
	// us which side of the cast line it landed on (sign) and how far (magnitude).
	caster := protocol.Vec2{X: 0, Y: 0}
	center := protocol.Vec2{X: 0, Y: 100}
	const spread = 50.0

	// index 0 is the primary — it sits exactly on center (the target), never offset.
	if got := zoneSpreadPosition(caster, center, 0, spread); got != center {
		t.Fatalf("index 0 should sit on center %v, got %v", center, got)
	}

	// Collect the signed perpendicular (X here) offset of each extra copy.
	const extras = 6
	offsets := make([]float64, extras+1)
	for i := 1; i <= extras; i++ {
		p := zoneSpreadPosition(caster, center, i, spread)
		if math.Abs(p.Y-center.Y) > 1e-6 {
			t.Fatalf("index %d drifted off the fan axis: Y=%v want %v", i, p.Y, center.Y)
		}
		offsets[i] = p.X - center.X
	}

	// Every extra is actually offset (no copy silently stacks on center).
	for i := 1; i <= extras; i++ {
		if offsets[i] == 0 {
			t.Fatalf("index %d did not fan out (offset 0)", i)
		}
	}

	// The heart of the request: consecutive copies land on OPPOSITE sides —
	// right, left, right, left … back and forth.
	for i := 2; i <= extras; i++ {
		if math.Signbit(offsets[i]) == math.Signbit(offsets[i-1]) {
			t.Fatalf("index %d landed on the same side as index %d (offsets %v then %v) — the fan must alternate",
				i, i-1, offsets[i-1], offsets[i])
		}
	}

	// The magnitude grows outward (never shrinks) as more copies deploy, so the
	// fan widens instead of two copies overlapping on the same spot.
	for i := 2; i <= extras; i++ {
		if math.Abs(offsets[i]) < math.Abs(offsets[i-2]) {
			t.Fatalf("index %d (|%v|) fell inside index %d (|%v|) — the fan should grow outward",
				i, offsets[i], i-2, offsets[i-2])
		}
	}
}

func TestZoneSpread_NoOffsetWithoutSpreadOrDistance(t *testing.T) {
	caster := protocol.Vec2{X: 0, Y: 0}
	center := protocol.Vec2{X: 0, Y: 100}

	// A zero spread distance keeps every copy on center (spread disabled).
	if got := zoneSpreadPosition(caster, center, 3, 0); got != center {
		t.Fatalf("zero spread should keep copy on center %v, got %v", center, got)
	}
}

// TestExplosiveTrap_SpreadAlternates_EndToEnd casts a real explosive_trap with
// its create_zone Count bumped to 3, at the EXACT geometry of the editor
// preview scene (caster at (600,500), enemy at (720,500) — a horizontal cast).
// It proves that through the whole live cast → create_zone executor path the
// three armed traps fan perpendicular to the cast: one ON the enemy, one ABOVE,
// one BELOW — the "one above and one below" the trapper wants for a side cast.
func TestExplosiveTrap_SpreadAlternates_EndToEnd(t *testing.T) {
	base, ok := getAbilityDef("explosive_trap")
	if !ok {
		t.Fatal(`getAbilityDef("explosive_trap") = _, false`)
	}

	// Clone via JSON round-trip so we never mutate the shared catalog def, then
	// bump the "arm" create_zone's Count to 3 (spreadDistance stays 50).
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("marshal base def: %v", err)
	}
	var def AbilityDef
	if err := json.Unmarshal(raw, &def); err != nil {
		t.Fatalf("unmarshal clone def: %v", err)
	}
	def.ID = "explosive_trap_spread_test"
	if def.Program == nil || len(def.Program.Triggers) == 0 || len(def.Program.Triggers[0].Actions) == 0 {
		t.Fatal("cloned explosive_trap has no cast trigger / create_zone action")
	}
	arm := &def.Program.Triggers[0].Actions[0] // the "arm" create_zone
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(arm.Config, &cfg); err != nil {
		t.Fatalf("unmarshal create_zone config: %v", err)
	}
	cfg["count"] = json.RawMessage("3")
	if arm.Config, err = json.Marshal(cfg); err != nil {
		t.Fatalf("remarshal create_zone config: %v", err)
	}
	registerRuntimeTestAbility(t, def)

	s := newTrapState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	s.Players[enemyPlayerID] = &Player{ID: enemyPlayerID, Resources: map[string]int{}}

	// Match the preview scene layout exactly (previewScene.ts): caster at the
	// scene origin, enemy +120 in X at the same Y → a horizontal throw.
	caster := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 600, Y: 500})
	if caster == nil {
		t.Fatal("caster spawn failed")
	}
	grantTrapAbility(caster, def.ID)
	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 720, Y: 500})
	if enemy == nil {
		t.Fatal("enemy spawn failed")
	}
	enemy.Visible = true

	if ok, reason := s.beginAbilityCastLocked(caster, def.ID, enemy); !ok {
		t.Fatalf("beginAbilityCastLocked failed: %q", reason)
	}

	if len(s.AbilityZones) != 3 {
		t.Fatalf("expected 3 armed traps (count=3), got %d", len(s.AbilityZones))
	}

	var onEnemy, above, below int
	for _, z := range s.AbilityZones {
		// A horizontal cast fans perpendicular = vertically, so every copy stays
		// on the enemy's column (X unchanged) and splits above/below.
		if math.Abs(z.Center.X-enemy.X) > 1e-6 {
			t.Fatalf("trap left the target's column: Center=%v enemy.X=%v", z.Center, enemy.X)
		}
		switch {
		case math.Abs(z.Center.Y-enemy.Y) < 1e-6:
			onEnemy++
		case z.Center.Y < enemy.Y:
			above++
		case z.Center.Y > enemy.Y:
			below++
		}
	}
	if onEnemy != 1 || above != 1 || below != 1 {
		t.Fatalf("traps did not straddle the target: onEnemy=%d above=%d below=%d (want 1/1/1) — the fan must alternate above/below",
			onEnemy, above, below)
	}
}
