package game

import (
	"sync"
	"testing"
)

func TestCraft_FiresRecipeCraftedHandler(t *testing.T) {
	s, _ := setupCraft(t, 1000, []string{"broad_sword", "fire_ring"}) // helper from Task 6 test file

	var mu sync.Mutex
	var got [][2]string
	done := make(chan struct{}, 1)
	s.SetRecipeCraftedHandler(func(playerID, recipeID string) {
		mu.Lock()
		got = append(got, [2]string{playerID, recipeID})
		mu.Unlock()
		select {
		case done <- struct{}{}:
		default:
		}
	})

	if !s.CraftItem("p1", "fire_sword") {
		t.Fatal("craft should succeed")
	}
	// Handler is fire-and-forget (may run in a goroutine); wait briefly.
	<-done
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0][0] != "p1" || got[0][1] != "fire_sword" {
		t.Fatalf("handler got %v, want one (p1, fire_sword)", got)
	}
}

func TestCraft_NoHandlerFireOnFailure(t *testing.T) {
	s, _ := setupCraft(t, 10, []string{"broad_sword", "fire_ring"}) // gold 10 < 150
	fired := false
	s.SetRecipeCraftedHandler(func(playerID, recipeID string) { fired = true })
	if s.CraftItem("p1", "fire_sword") {
		t.Fatal("craft should fail (unaffordable)")
	}
	if fired {
		t.Fatal("handler must not fire on a failed craft")
	}
}
