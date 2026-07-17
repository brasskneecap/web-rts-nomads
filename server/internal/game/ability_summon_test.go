package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// raiseSkeletonDef returns the catalog-authored Raise Skeleton ability, with
// mechanic magnitudes RECOVERED from the compiled Program
// (abilityMechanicsShadow) — raise_skeleton is schemaVersion:2 as of the
// composable-abilities migration, so the raw catalog def's
// SummonUnitType/SummonCount are cleared and the shipped Program is the sole
// authority for them. Tests derive expected mana / cooldown / summon-count
// values from this rather than hardcoding numbers — tuning the catalog (or
// its compiled Program) never breaks a behavioural test, only a real
// regression in the cast logic does.
func raiseSkeletonDef(t *testing.T) AbilityDef {
	t.Helper()
	def, ok := getAbilityDef("raise_skeleton")
	if !ok {
		t.Fatal(`getAbilityDef("raise_skeleton") = _, false; want the catalog-authored Raise Skeleton`)
	}
	return abilityMechanicsShadow(def)
}

func countUnitsOfType(s *GameState, unitType string) int {
	n := 0
	for _, u := range s.unitsByID {
		if u != nil && u.UnitType == unitType {
			n++
		}
	}
	return n
}

// Happy path: a necromancer casting raise_skeleton spawns def.SummonCount
// skeleton_soldiers owned by the same player, deducts the catalog mana cost
// once (not per-summon), and arms the cooldown.
func TestRaiseSkeleton_SpawnsSkeletonAndDeductsMana(t *testing.T) {
	s := newProjectileTestState(t)
	def := raiseSkeletonDef(t)

	s.mu.Lock()
	necro := s.spawnPlayerUnitLocked("necromancer", enemyPlayerID, "#700070", protocol.Vec2{X: 400, Y: 400})
	if necro == nil {
		s.mu.Unlock()
		t.Fatal(`spawnPlayerUnitLocked("necromancer", ...) returned nil — is the witherborne catalog wired?`)
	}
	necro.Visible = true
	necroID := necro.ID
	startMana := necro.CurrentMana
	wantMana := startMana - def.ManaCost
	skeletonsBefore := countUnitsOfType(s, "skeleton_soldier")
	ok, reason := s.beginAbilityCastLocked(necro, "raise_skeleton", necro)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("beginAbilityCastLocked(raise_skeleton) failed: %q", reason)
	}

	advance(s, 35) // 35 × 0.05s = 1.75s, past the 1.5s catalog cast time

	s.mu.RLock()
	defer s.mu.RUnlock()

	skeletonsAfter := countUnitsOfType(s, "skeleton_soldier")
	if got, want := skeletonsAfter-skeletonsBefore, def.SummonCount; got != want {
		t.Errorf("skeleton_soldier count delta = %d; want %d (def.SummonCount)", got, want)
	}

	n := s.unitsByID[necroID]
	if n == nil {
		t.Fatalf("necromancer (id=%d) missing from unitsByID after cast", necroID)
	}
	if n.CurrentMana != wantMana {
		t.Errorf("caster mana = %d; want %d (catalog manaCost = %d)", n.CurrentMana, wantMana, def.ManaCost)
	}
	if cd, ok := n.AbilityCooldowns["raise_skeleton"]; !ok || cd <= 0 {
		t.Errorf("raise_skeleton cooldown = %v (ok=%v); want a positive remaining duration", cd, ok)
	}
}

// Ownership / colour propagation: the summoned skeleton inherits the
// caster's OwnerID and Color, not the player_test defaults.
func TestRaiseSkeleton_SummonInheritsCasterOwnerAndColor(t *testing.T) {
	s := newProjectileTestState(t)

	s.mu.Lock()
	const casterColor = "#abcdef"
	necro := s.spawnPlayerUnitLocked("necromancer", enemyPlayerID, casterColor, protocol.Vec2{X: 500, Y: 500})
	if necro == nil {
		s.mu.Unlock()
		t.Fatal(`spawnPlayerUnitLocked("necromancer", ...) returned nil`)
	}
	necro.Visible = true
	ok, reason := s.beginAbilityCastLocked(necro, "raise_skeleton", necro)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("beginAbilityCastLocked failed: %q", reason)
	}

	advance(s, 35)

	s.mu.RLock()
	defer s.mu.RUnlock()
	var summon *Unit
	for _, u := range s.unitsByID {
		if u != nil && u.UnitType == "skeleton_soldier" {
			summon = u
			break
		}
	}
	if summon == nil {
		t.Fatal("no skeleton_soldier found after cast")
	}
	if summon.OwnerID != enemyPlayerID {
		t.Errorf("summon OwnerID = %q; want %q (caster's owner)", summon.OwnerID, enemyPlayerID)
	}
	if summon.Color != casterColor {
		t.Errorf("summon Color = %q; want %q (caster's color)", summon.Color, casterColor)
	}
}

// Cancel on caster death: if the necromancer dies mid-cast, no skeleton is
// spawned and no mana is spent (mana is deducted at resolve, not cast start).
func TestRaiseSkeleton_CasterDeathCancelsCast(t *testing.T) {
	s := newProjectileTestState(t)

	s.mu.Lock()
	necro := s.spawnPlayerUnitLocked("necromancer", enemyPlayerID, "#700070", protocol.Vec2{X: 600, Y: 600})
	if necro == nil {
		s.mu.Unlock()
		t.Fatal(`spawnPlayerUnitLocked("necromancer", ...) returned nil`)
	}
	necro.Visible = true
	startMana := necro.CurrentMana
	skeletonsBefore := countUnitsOfType(s, "skeleton_soldier")
	ok, reason := s.beginAbilityCastLocked(necro, "raise_skeleton", necro)
	if !ok {
		s.mu.Unlock()
		t.Fatalf("beginAbilityCastLocked failed: %q", reason)
	}
	// Kill the caster mid-cast — tickUnitCastLocked's HP<=0 branch should
	// clear the cast bookkeeping without resolving the effect.
	necro.HP = 0
	s.mu.Unlock()

	advance(s, 35)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if got, want := countUnitsOfType(s, "skeleton_soldier")-skeletonsBefore, 0; got != want {
		t.Errorf("skeleton_soldier count delta = %d; want %d (no spawn on dead caster)", got, want)
	}
	// Caster mana should be unchanged — mana spend happens on resolve, and
	// a dying caster's cast is cancelled before resolve.
	if necro.CurrentMana != startMana {
		t.Errorf("caster mana = %d; want %d (mana must not be deducted for a cancelled cast)", necro.CurrentMana, startMana)
	}
}

// Catalog wiring: raise_skeleton MUST declare its summon unit so the loader
// has something to resolve. Cheap guard against accidental field rename in
// the JSON.
func TestRaiseSkeleton_CatalogWiring(t *testing.T) {
	def := raiseSkeletonDef(t)
	if def.SummonUnitType != "skeleton_soldier" {
		t.Errorf("raise_skeleton.summonUnitType = %q; want %q", def.SummonUnitType, "skeleton_soldier")
	}
	if def.SummonCount < 1 {
		t.Errorf("raise_skeleton.summonCount = %d; want >= 1 (loader normalisation expected)", def.SummonCount)
	}
	if !def.CanTargetSelf {
		t.Error("raise_skeleton must be self-targetable (a necromancer summons next to itself)")
	}
	if def.ManaCost <= 0 {
		t.Errorf("raise_skeleton.manaCost = %d; want > 0", def.ManaCost)
	}
	if _, ok := getUnitDef("skeleton_soldier"); !ok {
		t.Error(`getUnitDef("skeleton_soldier") = _, false; the catalog/units/witherborne/skeleton_soldier directory is missing or its JSON is broken`)
	}
	if _, ok := getUnitDef("necromancer"); !ok {
		t.Error(`getUnitDef("necromancer") = _, false; the catalog/units/witherborne/necromancer directory is missing or its JSON is broken`)
	}
}
